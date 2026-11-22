package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/distribution_risk_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	distributionRiskEvidenceTTL = 32 * 24 * time.Hour
	distributionRiskHoldTTL     = time.Minute
	distributionRiskThrottleTTL = 5 * time.Minute
	distributionRiskDBCacheTTL  = 5 * time.Second
)

type DistributionRiskDecision struct {
	Blocked bool
	Release func()
}

type distributionRiskSample struct {
	IPs            int `json:"ips"`
	UserAgents     int `json:"user_agents"`
	Sessions       int `json:"sessions"`
	PeakRate       int `json:"peak_rate"`
	PeakConcurrent int `json:"peak_concurrent"`
	Requests       int `json:"requests"`
}

type distributionRiskBaseline struct {
	Days          int `json:"days"`
	Requests      int `json:"requests"`
	P95IPs        int `json:"p95_ips"`
	P99IPs        int `json:"p99_ips"`
	P95UserAgents int `json:"p95_user_agents"`
	P99UserAgents int `json:"p99_user_agents"`
	P95Sessions   int `json:"p95_sessions"`
	P99Sessions   int `json:"p99_sessions"`
	P95PeakRate   int `json:"p95_peak_rate"`
	P99PeakRate   int `json:"p99_peak_rate"`
	P95Concurrent int `json:"p95_peak_concurrent"`
	P99Concurrent int `json:"p99_peak_concurrent"`
}

type distributionRiskEvidence struct {
	IPs        []string `json:"ips"`
	UserAgents []string `json:"user_agents"`
	Sessions   []string `json:"session_hashes"`
}

type distributionRiskScopeResult struct {
	ScopeKey       string
	DayKey         string
	ScopeKind      string
	ScopeID        int
	UserID         int
	TriggerTokenID int
	Current        distributionRiskSample
	Score          int
	Level          string
	CategoryScores map[string]int
	TokenIDs       []int
}

type distributionRiskHoldCacheEntry struct {
	held      bool
	expiresAt time.Time
}

var distributionRiskHoldCache sync.Map
var distributionRiskLastPrune atomic.Int64

const distributionRiskObserveScript = `
local requests = redis.call('HINCRBY', KEYS[1], 'requests', 1)
local rate = redis.call('INCR', KEYS[2])
redis.call('EXPIRE', KEYS[2], ARGV[1])
local inflight = redis.call('INCR', KEYS[3])
redis.call('EXPIRE', KEYS[3], ARGV[3])
local peak_rate = tonumber(redis.call('HGET', KEYS[1], 'peak_rate') or '0')
local peak_concurrent = tonumber(redis.call('HGET', KEYS[1], 'peak_concurrent') or '0')
if rate > peak_rate then
  peak_rate = rate
  redis.call('HSET', KEYS[1], 'peak_rate', peak_rate)
end
if inflight > peak_concurrent then
  peak_concurrent = inflight
  redis.call('HSET', KEYS[1], 'peak_concurrent', peak_concurrent)
end
redis.call('EXPIRE', KEYS[1], ARGV[2])
return {requests, peak_rate, peak_concurrent}`

const distributionRiskReleaseScript = `
local value = redis.call('DECR', KEYS[1])
if value < 0 then
  redis.call('SET', KEYS[1], 0)
end
return math.max(value, 0)`

// CheckDistributionRisk observes relay traffic after the request has been
// validated. The returned release function closes the small concurrency
// measurement window even when the relay exits early.
func CheckDistributionRisk(c *gin.Context, userID, tokenID int, ip, userAgent, sessionID string) (*DistributionRiskDecision, error) {
	decision := &DistributionRiskDecision{}
	if tokenID <= 0 || userID <= 0 {
		return decision, nil
	}
	if blocked, err := distributionRiskTokenBlocked(c.Request.Context(), tokenID); err != nil {
		common.SysLog(common.LogText("distribution risk hold lookup failed: %s", err.Error()))
	} else if blocked {
		decision.Blocked = true
		return decision, nil
	}
	if common.RedisEnabled && common.RDB != nil {
		throttled, err := common.RDB.Exists(c.Request.Context(), distributionRiskThrottleKey(tokenID)).Result()
		if err == nil && throttled > 0 {
			decision.Blocked = true
			return decision, nil
		}
		if err != nil {
			common.SysLog(common.LogText("distribution risk throttle lookup failed: %s", err.Error()))
		}
	}
	maybePruneDistributionRiskData()
	setting := distribution_risk_setting.GetSnapshot()
	if !setting.Enabled || !common.RedisEnabled || common.RDB == nil {
		return decision, nil
	}

	now := time.Now().UTC()
	date := now.Format("20060102")
	suppressed, err := common.RDB.Exists(c.Request.Context(), distributionRiskSuppressKey("user", userID), distributionRiskSuppressKey("token", tokenID)).Result()
	if err == nil && suppressed > 0 {
		return decision, nil
	}
	if err != nil {
		common.SysLog(common.LogText("distribution risk suppression lookup failed: %s", err.Error()))
	}
	hashedSession := hashDistributionRiskSession(sessionID)
	scopes := []struct {
		kind string
		id   int
	}{
		{kind: "user", id: userID},
		{kind: "token", id: tokenID},
	}
	var releaseOnce sync.Once
	var scopeKeys []string
	results := make([]distributionRiskScopeResult, 0, len(scopes))
	for _, scope := range scopes {
		result, releaseKey, err := observeDistributionRiskScope(c.Request.Context(), scope.kind, scope.id, tokenID, userID, date, now, ip, userAgent, hashedSession)
		if err != nil {
			for _, key := range scopeKeys {
				_ = common.RDB.Eval(context.Background(), distributionRiskReleaseScript, []string{key}).Err()
			}
			common.SysLog(common.LogText("distribution risk observation failed: %s", err.Error()))
			return decision, nil
		}
		scopeKeys = append(scopeKeys, releaseKey)
		baseline, err := loadDistributionRiskBaseline(c.Request.Context(), result.ScopeKey, now)
		if err != nil {
			common.SysLog(common.LogText("distribution risk baseline lookup failed: %s", err.Error()))
			continue
		}
		if baseline.Days < setting.MinActiveDays || baseline.Requests < setting.MinRequests {
			continue
		}
		evaluateDistributionRisk(&result, baseline, setting)
		if result.Score >= 60 {
			_ = common.RDB.Set(c.Request.Context(), distributionRiskFlaggedDayKey(result.ScopeKey, date), "1", distributionRiskEvidenceTTL).Err()
			results = append(results, result)
		}
	}
	decision.Release = func() {
		releaseOnce.Do(func() {
			for _, key := range scopeKeys {
				if err := common.RDB.Eval(context.Background(), distributionRiskReleaseScript, []string{key}).Err(); err != nil {
					common.SysLog(common.LogText("distribution risk concurrency release failed: %s", err.Error()))
				}
			}
		})
	}
	if len(results) == 0 {
		return decision, nil
	}
	mode := distribution_risk_setting.CurrentMode()
	for _, result := range results {
		persistent := result.Level == "high" && result.Score >= 80 && countPositiveDistributionRiskCategories(result.CategoryScores) >= 2
		if _, err := persistDistributionRiskEvent(c.Request.Context(), result, mode, persistent && mode == distribution_risk_setting.ModeEnforce); err != nil {
			common.SysLog(common.LogText("distribution risk event write failed: %s", err.Error()))
			continue
		}
		if mode != distribution_risk_setting.ModeEnforce {
			continue
		}
		if persistent {
			for _, id := range result.TokenIDs {
				distributionRiskHoldCache.Delete(id)
				if err := common.RDB.Set(c.Request.Context(), distributionRiskHoldKey(id), "1", distributionRiskHoldTTL).Err(); err != nil {
					common.SysLog(common.LogText("distribution risk hold cache write failed: %s", err.Error()))
				}
			}
			decision.Blocked = slicesContainsInt(result.TokenIDs, tokenID)
		} else if result.Score >= 60 {
			currentTokenThrottled := false
			for _, id := range result.TokenIDs {
				if err := common.RDB.Set(c.Request.Context(), distributionRiskThrottleKey(id), "1", distributionRiskThrottleTTL).Err(); err != nil {
					common.SysLog(common.LogText("distribution risk throttle write failed: %s", err.Error()))
				} else if id == tokenID {
					currentTokenThrottled = true
				}
			}
			decision.Blocked = decision.Blocked || currentTokenThrottled
		}
	}
	return decision, nil
}

func observeDistributionRiskScope(ctx context.Context, scopeKind string, scopeID, tokenID, userID int, date string, now time.Time, ip, userAgent, session string) (distributionRiskScopeResult, string, error) {
	scopeKey := fmt.Sprintf("dr:v1:{%s:%d}", scopeKind, scopeID)
	dayKey := scopeKey + ":day:" + date
	minuteKey := scopeKey + ":minute:" + strconv.FormatInt(now.Unix()/60, 10)
	inflightKey := scopeKey + ":inflight"
	pipe := common.RDB.Pipeline()
	if ip != "" {
		pipe.SAdd(ctx, dayKey+":ips", ip)
		pipe.Expire(ctx, dayKey+":ips", distributionRiskEvidenceTTL)
	}
	if userAgent != "" {
		userAgent = trimDistributionRiskValue(userAgent, 512)
		pipe.SAdd(ctx, dayKey+":agents", userAgent)
		pipe.Expire(ctx, dayKey+":agents", distributionRiskEvidenceTTL)
	}
	if session != "" {
		pipe.SAdd(ctx, dayKey+":sessions", session)
		pipe.Expire(ctx, dayKey+":sessions", distributionRiskEvidenceTTL)
	}
	pipe.Expire(ctx, dayKey+":stats", distributionRiskEvidenceTTL)
	if scopeKind == "user" {
		pipe.SAdd(ctx, dayKey+":tokens", tokenID)
		pipe.Expire(ctx, dayKey+":tokens", distributionRiskEvidenceTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return distributionRiskScopeResult{}, "", err
	}
	observed, err := common.RDB.Eval(ctx, distributionRiskObserveScript, []string{dayKey + ":stats", minuteKey, inflightKey}, 120, int(distributionRiskEvidenceTTL.Seconds()), int((36 * time.Hour).Seconds())).Slice()
	if err != nil || len(observed) != 3 {
		if err == nil {
			err = errors.New("invalid distribution risk counter response")
		}
		return distributionRiskScopeResult{}, "", err
	}
	countsPipe := common.RDB.Pipeline()
	ipCount := countsPipe.SCard(ctx, dayKey+":ips")
	uaCount := countsPipe.SCard(ctx, dayKey+":agents")
	sessionCount := countsPipe.SCard(ctx, dayKey+":sessions")
	var tokenIDsCmd *redis.StringSliceCmd
	if scopeKind == "user" {
		tokenIDsCmd = countsPipe.SMembers(ctx, dayKey+":tokens")
	}
	if _, err := countsPipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		_ = common.RDB.Eval(context.Background(), distributionRiskReleaseScript, []string{inflightKey}).Err()
		return distributionRiskScopeResult{}, "", err
	}
	requests := int(toDistributionRiskInt(observed[0]))
	peakRate := int(toDistributionRiskInt(observed[1]))
	peakConcurrent := int(toDistributionRiskInt(observed[2]))
	ids := []int{tokenID}
	if tokenIDsCmd != nil {
		values, err := tokenIDsCmd.Result()
		if err != nil {
			_ = common.RDB.Eval(context.Background(), distributionRiskReleaseScript, []string{inflightKey}).Err()
			return distributionRiskScopeResult{}, "", err
		}
		ids = make([]int, 0, len(values))
		for _, value := range values {
			id, err := strconv.Atoi(value)
			if err == nil && id > 0 && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return distributionRiskScopeResult{
		ScopeKey: scopeKey, DayKey: dayKey, ScopeKind: scopeKind, ScopeID: scopeID,
		UserID: userID, TriggerTokenID: tokenID,
		Current: distributionRiskSample{
			IPs: int(ipCount.Val()), UserAgents: int(uaCount.Val()), Sessions: int(sessionCount.Val()),
			Requests: requests, PeakRate: peakRate, PeakConcurrent: peakConcurrent,
		},
		TokenIDs: ids,
	}, inflightKey, nil
}

func distributionRiskSetValues(ctx context.Context, key string) []string {
	values, err := common.RDB.SMembers(ctx, key).Result()
	if err != nil {
		return nil
	}
	sort.Strings(values)
	return values
}

func loadDistributionRiskBaseline(ctx context.Context, scopeKey string, now time.Time) (distributionRiskBaseline, error) {
	cacheKey := scopeKey + ":baseline"
	if cached, err := common.RDB.Get(ctx, cacheKey).Bytes(); err == nil {
		var baseline distributionRiskBaseline
		if common.Unmarshal(cached, &baseline) == nil {
			return baseline, nil
		}
	} else if !errors.Is(err, redis.Nil) {
		return distributionRiskBaseline{}, err
	}
	type sampleCommands struct {
		ips, agents, sessions              *redis.IntCmd
		requests, peakRate, peakConcurrent *redis.StringCmd
		flagged                            *redis.IntCmd
	}
	pipe := common.RDB.Pipeline()
	commands := make([]sampleCommands, 0, 30)
	for daysAgo := 1; daysAgo <= 30; daysAgo++ {
		day := now.AddDate(0, 0, -daysAgo).Format("20060102")
		dayKey := scopeKey + ":day:" + day
		commands = append(commands, sampleCommands{
			ips: pipe.SCard(ctx, dayKey+":ips"), agents: pipe.SCard(ctx, dayKey+":agents"), sessions: pipe.SCard(ctx, dayKey+":sessions"),
			requests: pipe.HGet(ctx, dayKey+":stats", "requests"), peakRate: pipe.HGet(ctx, dayKey+":stats", "peak_rate"),
			peakConcurrent: pipe.HGet(ctx, dayKey+":stats", "peak_concurrent"), flagged: pipe.Exists(ctx, distributionRiskFlaggedDayKey(scopeKey, day)),
		})
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return distributionRiskBaseline{}, err
	}
	samples := make([]distributionRiskSample, 0, 30)
	for _, item := range commands {
		flagged, err := item.flagged.Result()
		if err == nil && flagged > 0 {
			continue
		}
		sample := distributionRiskSample{
			IPs: int(item.ips.Val()), UserAgents: int(item.agents.Val()), Sessions: int(item.sessions.Val()),
		}
		sample.Requests, _ = strconv.Atoi(item.requests.Val())
		sample.PeakRate, _ = strconv.Atoi(item.peakRate.Val())
		sample.PeakConcurrent, _ = strconv.Atoi(item.peakConcurrent.Val())
		if sample.Requests > 0 {
			samples = append(samples, sample)
		}
	}
	baseline := distributionRiskBaseline{Days: len(samples)}
	for _, sample := range samples {
		baseline.Requests += sample.Requests
	}
	if len(samples) > 0 {
		baseline.P95IPs, baseline.P99IPs = distributionRiskPercentiles(samples, func(s distributionRiskSample) int { return s.IPs })
		baseline.P95UserAgents, baseline.P99UserAgents = distributionRiskPercentiles(samples, func(s distributionRiskSample) int { return s.UserAgents })
		baseline.P95Sessions, baseline.P99Sessions = distributionRiskPercentiles(samples, func(s distributionRiskSample) int { return s.Sessions })
		baseline.P95PeakRate, baseline.P99PeakRate = distributionRiskPercentiles(samples, func(s distributionRiskSample) int { return s.PeakRate })
		baseline.P95Concurrent, baseline.P99Concurrent = distributionRiskPercentiles(samples, func(s distributionRiskSample) int { return s.PeakConcurrent })
	}
	encoded, err := common.Marshal(baseline)
	if err != nil {
		return distributionRiskBaseline{}, err
	}
	if err := common.RDB.Set(ctx, cacheKey, encoded, 10*time.Minute).Err(); err != nil {
		return distributionRiskBaseline{}, err
	}
	return baseline, nil
}

func distributionRiskPercentiles(samples []distributionRiskSample, value func(distributionRiskSample) int) (int, int) {
	values := make([]int, 0, len(samples))
	for _, sample := range samples {
		values = append(values, value(sample))
	}
	sort.Ints(values)
	return values[max(0, int(math.Ceil(float64(len(values))*0.95))-1)], values[max(0, int(math.Ceil(float64(len(values))*0.99))-1)]
}

func evaluateDistributionRisk(result *distributionRiskScopeResult, baseline distributionRiskBaseline, setting distribution_risk_setting.DistributionRiskSetting) {
	currentStats := result.Current
	categories := map[string]int{
		"network": assessDistributionRiskCategory(currentStats.IPs, baseline.P95IPs, baseline.P99IPs, setting.IPFloor),
		"client":  assessDistributionRiskCategory(currentStats.UserAgents, baseline.P95UserAgents, baseline.P99UserAgents, setting.UserAgentFloor),
		"session": assessDistributionRiskCategory(currentStats.Sessions, baseline.P95Sessions, baseline.P99Sessions, setting.SessionFloor),
	}
	rateRisk := assessDistributionRiskCategory(currentStats.PeakRate, baseline.P95PeakRate, baseline.P99PeakRate, setting.RequestsPerMinute)
	concurrencyRisk := assessDistributionRiskCategory(currentStats.PeakConcurrent, baseline.P95Concurrent, baseline.P99Concurrent, setting.ConcurrencyFloor)
	categories["activity"] = max(rateRisk, concurrencyRisk)
	score := 0
	categoryCount := 0
	for _, severity := range categories {
		score += severity
		if severity > 0 {
			categoryCount++
		}
	}
	result.Score = min(score, 100)
	result.CategoryScores = categories
	if result.Score >= 80 && categoryCount >= 2 {
		result.Level = "high"
	} else if result.Score >= 60 {
		result.Level = "medium"
	} else {
		result.Level = "low"
	}
}

func assessDistributionRiskCategory(value, p95, p99, floor int) int {
	if value >= floor*3 || (p99 > 0 && value > p99*2) {
		return 50
	}
	if value >= floor*2 || (p99 > 0 && value > p99) {
		return 30
	}
	if value >= floor || (p95 > 0 && value > p95) {
		return 15
	}
	return 0
}

func persistDistributionRiskEvent(ctx context.Context, result distributionRiskScopeResult, mode string, hold bool) (bool, error) {
	modeKey := "shadow"
	if mode == distribution_risk_setting.ModeEnforce {
		modeKey = "enforce"
	}
	dedupeKey := result.ScopeKey + ":event:" + strings.TrimPrefix(result.DayKey, result.ScopeKey+":day:") + ":" + modeKey + ":" + strconv.Itoa(result.TriggerTokenID)
	created, err := common.RDB.SetNX(ctx, dedupeKey, "1", 48*time.Hour).Result()
	if err != nil {
		return false, err
	}
	if !created {
		return false, nil
	}
	evidence := distributionRiskEvidence{
		IPs:        distributionRiskSetValues(ctx, result.DayKey+":ips"),
		UserAgents: distributionRiskSetValues(ctx, result.DayKey+":agents"),
		Sessions:   distributionRiskSetValues(ctx, result.DayKey+":sessions"),
	}
	tokenIDsJSON, err := common.Marshal(result.TokenIDs)
	if err != nil {
		_ = common.RDB.Del(ctx, dedupeKey).Err()
		return false, err
	}
	categoriesJSON, err := common.Marshal(result.CategoryScores)
	if err != nil {
		_ = common.RDB.Del(ctx, dedupeKey).Err()
		return false, err
	}
	evidenceJSON, err := common.Marshal(evidence)
	if err != nil {
		_ = common.RDB.Del(ctx, dedupeKey).Err()
		return false, err
	}
	event := &model.DistributionRiskEvent{
		UserID:         result.UserID,
		ScopeKind:      result.ScopeKind,
		ScopeID:        result.ScopeID,
		TokenIDs:       model.DistributionRiskJSON(tokenIDsJSON),
		Score:          result.Score,
		Level:          result.Level,
		CategoryScores: model.DistributionRiskJSON(categoriesJSON),
		Evidence:       model.DistributionRiskJSON(evidenceJSON),
	}
	if err := model.CreateDistributionRiskEvent(event, hold, result.TokenIDs); err != nil {
		_ = common.RDB.Del(ctx, dedupeKey).Err()
		return false, err
	}
	if hold {
		for _, id := range result.TokenIDs {
			distributionRiskHoldCache.Delete(id)
		}
	}
	return true, nil
}

func distributionRiskTokenBlocked(ctx context.Context, tokenID int) (bool, error) {
	key := distributionRiskHoldKey(tokenID)
	if common.RedisEnabled && common.RDB != nil {
		_, err := common.RDB.Get(ctx, key).Result()
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, redis.Nil) {
			return model.IsDistributionRiskTokenHeld(tokenID)
		}
		if cached, ok := cachedDistributionRiskHoldValue(tokenID); ok && !cached.held {
			return false, nil
		}
	}
	return cachedDistributionRiskHold(tokenID)
}

func cachedDistributionRiskHold(tokenID int) (bool, error) {
	if cached, ok := cachedDistributionRiskHoldValue(tokenID); ok {
		return cached.held, nil
	}
	held, err := model.IsDistributionRiskTokenHeld(tokenID)
	if err != nil {
		return false, err
	}
	distributionRiskHoldCache.Store(tokenID, distributionRiskHoldCacheEntry{held: held, expiresAt: time.Now().Add(distributionRiskDBCacheTTL)})
	if held && common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Set(context.Background(), distributionRiskHoldKey(tokenID), "1", distributionRiskHoldTTL).Err()
	}
	return held, nil
}

func cachedDistributionRiskHoldValue(tokenID int) (distributionRiskHoldCacheEntry, bool) {
	value, ok := distributionRiskHoldCache.Load(tokenID)
	if !ok {
		return distributionRiskHoldCacheEntry{}, false
	}
	entry, ok := value.(distributionRiskHoldCacheEntry)
	if !ok || time.Now().After(entry.expiresAt) {
		distributionRiskHoldCache.Delete(tokenID)
		return distributionRiskHoldCacheEntry{}, false
	}
	return entry, true
}

func InvalidateDistributionRiskHoldCache(tokenIDs []int) {
	for _, tokenID := range tokenIDs {
		distributionRiskHoldCache.Delete(tokenID)
		if common.RedisEnabled && common.RDB != nil {
			if err := common.RDB.Del(context.Background(), distributionRiskHoldKey(tokenID)).Err(); err != nil {
				common.SysLog(common.LogText("distribution risk hold cache clear failed: %s", err.Error()))
			}
		}
	}
}

func SuppressDistributionRisk(event *model.DistributionRiskEvent, tokenIDs []int) {
	if event == nil {
		return
	}
	const suppressionTTL = 48 * time.Hour
	if common.RedisEnabled && common.RDB != nil && event.ScopeKind != "" && event.ScopeID > 0 {
		if err := common.RDB.Set(context.Background(), distributionRiskSuppressKey(event.ScopeKind, event.ScopeID), "1", suppressionTTL).Err(); err != nil {
			common.SysLog(common.LogText("distribution risk suppression write failed: %s", err.Error()))
		}
	}
	for _, tokenID := range tokenIDs {
		distributionRiskHoldCache.Delete(tokenID)
		if common.RedisEnabled && common.RDB != nil {
			if err := common.RDB.Del(context.Background(), distributionRiskThrottleKey(tokenID)).Err(); err != nil {
				common.SysLog(common.LogText("distribution risk throttle clear failed: %s", err.Error()))
			}
			if event.ScopeKind == "user" {
				if err := common.RDB.Set(context.Background(), distributionRiskSuppressKey("token", tokenID), "1", suppressionTTL).Err(); err != nil {
					common.SysLog(common.LogText("distribution risk token suppression write failed: %s", err.Error()))
				}
			}
		}
	}
}

func maybePruneDistributionRiskData() {
	if model.DB == nil {
		return
	}
	now := time.Now()
	last := distributionRiskLastPrune.Load()
	if now.Unix()-last < int64(time.Hour/time.Second) {
		return
	}
	if common.RedisEnabled && common.RDB != nil {
		acquired, err := common.RDB.SetNX(context.Background(), "dr:v1:prune-lock", "1", time.Hour).Result()
		if err != nil || !acquired {
			return
		}
	} else if !distributionRiskLastPrune.CompareAndSwap(last, now.Unix()) {
		return
	}
	distributionRiskLastPrune.Store(now.Unix())
	if err := model.PruneDistributionRiskData(now); err != nil {
		common.SysLog(common.LogText("distribution risk data pruning failed: %s", err.Error()))
	}
}

func distributionRiskThrottleKey(tokenID int) string {
	return fmt.Sprintf("dr:v1:throttle:token:%d", tokenID)
}

func distributionRiskSuppressKey(scopeKind string, scopeID int) string {
	return fmt.Sprintf("dr:v1:suppress:%s:%d", scopeKind, scopeID)
}

func distributionRiskHoldKey(tokenID int) string {
	return fmt.Sprintf("dr:v1:hold:token:%d", tokenID)
}

func distributionRiskFlaggedDayKey(scopeKey, date string) string {
	return scopeKey + ":flagged:" + date
}

func hashDistributionRiskSession(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 4096 {
		return ""
	}
	key := []byte("distribution-risk-session-v1:" + common.SessionSecret)
	return common.GenerateHMACWithKey(key, sessionID)
}

func trimDistributionRiskValue(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxBytes {
		return value
	}
	return strings.ToValidUTF8(value[:maxBytes], "")
}

func countPositiveDistributionRiskCategories(categories map[string]int) int {
	count := 0
	for _, score := range categories {
		if score > 0 {
			count++
		}
	}
	return count
}

func slicesContainsInt(values []int, target int) bool {
	return slices.Contains(values, target)
}

func toDistributionRiskInt(value any) int64 {
	switch result := value.(type) {
	case int64:
		return result
	case string:
		parsed, _ := strconv.ParseInt(result, 10, 64)
		return parsed
	case []byte:
		parsed, _ := strconv.ParseInt(string(result), 10, 64)
		return parsed
	default:
		return 0
	}
}
