package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/group_rate_limit_setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/tidwall/gjson"
	"golang.org/x/time/rate"
)

const (
	userRateLimitObservationKey = "user_rate_limit_observation"
	userStreamPacerKey          = "user_stream_pacer"

	userConcurrencyLeaseTTL      = 60 * time.Second
	userConcurrencyRenewEvery    = 20 * time.Second
	userConcurrencyWaitTimeout   = 30 * time.Second
	userConcurrencyHeartbeat     = 5 * time.Second
	userConcurrencyInitialPoll   = 100 * time.Millisecond
	userConcurrencyMaximumPoll   = time.Second
	userConcurrencyWaitingLimit  = 20
	groupConcurrencyWaitingLimit = 100
)

type userRateLimitRedisSource struct {
	client  *redis.Client
	enabled bool
}

var userRateLimitRedisOverride atomic.Pointer[userRateLimitRedisSource]

func getUserRateLimitRedisClient() (*redis.Client, bool) {
	if override := userRateLimitRedisOverride.Load(); override != nil {
		return override.client, override.enabled && override.client != nil
	}
	client := common.RDB
	return client, common.RedisEnabled && client != nil
}

type UserRateLimitPolicy struct {
	UserID int
	Group  string

	UserRPMLimit         int
	UserConcurrencyLimit int
	UserStreamTPSLimit   int

	MemberRPMLimit         int
	MemberConcurrencyLimit int
	MemberStreamTPSLimit   int

	RPMLimit         int
	ConcurrencyLimit int
	StreamTPSLimit   int

	SharedRPMLimit         int
	SharedConcurrencyLimit int
	SharedStreamTPSLimit   int
}

func (policy UserRateLimitPolicy) HasConcurrencyLimit() bool {
	return policy.ConcurrencyLimit > 0 || policy.SharedConcurrencyLimit > 0
}

type UserRateLimitObservation struct {
	mu sync.Mutex

	group                  string
	userRPMLimit           int
	userConcurrencyLimit   int
	userStreamTPSLimit     int
	memberRPMLimit         int
	memberConcurrencyLimit int
	memberStreamTPSLimit   int
	rpmLimit               int
	concurrencyLimit       int
	streamTPSLimit         int
	sharedRPMLimit         int
	sharedConcurrencyLimit int
	sharedStreamTPSLimit   int
	queueWait              time.Duration
	userQueueWait          time.Duration
	groupQueueWait         time.Duration
	pacingTokens           int64
	pacingWait             time.Duration
	sharedPacingTokens     int64
	sharedPacingWait       time.Duration
	rejectedLayer          string
	redisFailOpen          map[string]bool
}

func (o *UserRateLimitObservation) notePolicy(policy UserRateLimitPolicy) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.group = policy.Group
	o.userRPMLimit = policy.UserRPMLimit
	o.userConcurrencyLimit = policy.UserConcurrencyLimit
	o.userStreamTPSLimit = policy.UserStreamTPSLimit
	o.memberRPMLimit = policy.MemberRPMLimit
	o.memberConcurrencyLimit = policy.MemberConcurrencyLimit
	o.memberStreamTPSLimit = policy.MemberStreamTPSLimit
	o.rpmLimit = policy.RPMLimit
	o.concurrencyLimit = policy.ConcurrencyLimit
	o.streamTPSLimit = policy.StreamTPSLimit
	o.sharedRPMLimit = policy.SharedRPMLimit
	o.sharedConcurrencyLimit = policy.SharedConcurrencyLimit
	o.sharedStreamTPSLimit = policy.SharedStreamTPSLimit
}

func (o *UserRateLimitObservation) noteQueueWait(userWait, groupWait time.Duration) {
	if o == nil || (userWait <= 0 && groupWait <= 0) {
		return
	}
	o.mu.Lock()
	if groupWait > userWait {
		o.queueWait += groupWait
	} else {
		o.queueWait += userWait
	}
	o.userQueueWait += userWait
	o.groupQueueWait += groupWait
	o.mu.Unlock()
}

func (o *UserRateLimitObservation) notePacing(tokens int, wait time.Duration) {
	if o == nil || tokens <= 0 {
		return
	}
	o.mu.Lock()
	o.pacingTokens += int64(tokens)
	if wait > 0 {
		o.pacingWait += wait
	}
	o.mu.Unlock()
}

func (o *UserRateLimitObservation) noteSharedPacing(tokens int, wait time.Duration) {
	if o == nil || tokens <= 0 {
		return
	}
	o.mu.Lock()
	o.sharedPacingTokens += int64(tokens)
	if wait > 0 {
		o.sharedPacingWait += wait
	}
	o.mu.Unlock()
}

func (o *UserRateLimitObservation) noteRejectedLayer(layer string) {
	if o == nil || layer == "" {
		return
	}
	o.mu.Lock()
	o.rejectedLayer = layer
	o.mu.Unlock()
}

func (o *UserRateLimitObservation) noteRedisFailOpen(component string) {
	if o == nil || component == "" {
		return
	}
	o.mu.Lock()
	if o.redisFailOpen == nil {
		o.redisFailOpen = make(map[string]bool)
	}
	o.redisFailOpen[component] = true
	o.mu.Unlock()
}

func AppendUserRateLimitAdminInfo(c *gin.Context, other map[string]interface{}) {
	if c == nil || other == nil {
		return
	}
	value, ok := c.Get(userRateLimitObservationKey)
	if !ok {
		return
	}
	observation, ok := value.(*UserRateLimitObservation)
	if !ok || observation == nil {
		return
	}
	observation.mu.Lock()
	defer observation.mu.Unlock()
	if observation.rpmLimit <= 0 && observation.concurrencyLimit <= 0 && observation.streamTPSLimit <= 0 &&
		observation.sharedRPMLimit <= 0 && observation.sharedConcurrencyLimit <= 0 && observation.sharedStreamTPSLimit <= 0 &&
		len(observation.redisFailOpen) == 0 {
		return
	}
	limits := make(map[string]interface{})
	if observation.group != "" {
		limits["group"] = observation.group
	}
	userLimits := rateLimitValuesMap(observation.userRPMLimit, observation.userConcurrencyLimit, observation.userStreamTPSLimit)
	if len(userLimits) > 0 {
		limits["user_limits"] = userLimits
	}
	memberLimits := rateLimitValuesMap(observation.memberRPMLimit, observation.memberConcurrencyLimit, observation.memberStreamTPSLimit)
	if len(memberLimits) > 0 {
		limits["member_limits"] = memberLimits
	}
	if observation.rpmLimit > 0 {
		limits["rpm_limit"] = observation.rpmLimit
	}
	if observation.concurrencyLimit > 0 {
		limits["concurrency_limit"] = observation.concurrencyLimit
	}
	if observation.streamTPSLimit > 0 {
		limits["stream_tps_limit"] = observation.streamTPSLimit
	}
	sharedPool := rateLimitValuesMap(observation.sharedRPMLimit, observation.sharedConcurrencyLimit, observation.sharedStreamTPSLimit)
	if len(sharedPool) > 0 {
		limits["shared_pool"] = sharedPool
	}
	if observation.userQueueWait > 0 {
		limits["user_queue_wait_ms"] = observation.userQueueWait.Milliseconds()
	}
	if observation.groupQueueWait > 0 {
		limits["group_queue_wait_ms"] = observation.groupQueueWait.Milliseconds()
	}
	if observation.queueWait > 0 {
		limits["queue_wait_ms"] = observation.queueWait.Milliseconds()
	}
	if observation.pacingTokens > 0 {
		limits["pacing_tokens"] = observation.pacingTokens
		limits["pacing_wait_ms"] = observation.pacingWait.Milliseconds()
	}
	if observation.sharedPacingTokens > 0 {
		limits["shared_pacing_tokens"] = observation.sharedPacingTokens
		limits["shared_pacing_wait_ms"] = observation.sharedPacingWait.Milliseconds()
	}
	if observation.rejectedLayer != "" {
		limits["rejected_layer"] = observation.rejectedLayer
	}
	if len(observation.redisFailOpen) > 0 {
		failOpen := make(map[string]bool, len(observation.redisFailOpen))
		for component, enabled := range observation.redisFailOpen {
			failOpen[component] = enabled
		}
		limits["redis_fail_open"] = failOpen
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo["user_rate_limits"] = limits
}

func rateLimitValuesMap(rpm, concurrency, streamTPS int) map[string]interface{} {
	values := make(map[string]interface{}, 3)
	if rpm > 0 {
		values["rpm_limit"] = rpm
	}
	if concurrency > 0 {
		values["concurrency_limit"] = concurrency
	}
	if streamTPS > 0 {
		values["stream_tps_limit"] = streamTPS
	}
	return values
}

func UserRateLimitPolicyFromContext(c *gin.Context) UserRateLimitPolicy {
	if c == nil {
		return UserRateLimitPolicy{}
	}
	return buildUserRateLimitPolicy(
		common.GetContextKeyInt(c, constant.ContextKeyUserId),
		requestRateLimitGroup(c),
		common.GetContextKeyInt(c, constant.ContextKeyUserRpmLimit),
		common.GetContextKeyInt(c, constant.ContextKeyUserConcurrencyLimit),
		common.GetContextKeyInt(c, constant.ContextKeyUserStreamTpsLimit),
	)
}

func LoadUserRateLimitPolicy(userID int, groupOverride ...string) (UserRateLimitPolicy, error) {
	if userID <= 0 {
		return UserRateLimitPolicy{}, errors.New("invalid user id")
	}
	var user model.User
	if err := model.DB.Where("id = ?", userID).Take(&user).Error; err != nil {
		return UserRateLimitPolicy{}, err
	}
	group := user.Group
	if len(groupOverride) > 0 && strings.TrimSpace(groupOverride[0]) != "" {
		group = groupOverride[0]
	}
	userRPM := 0
	userConcurrency := 0
	userStreamTPS := 0
	if user.RpmLimit != nil {
		userRPM = *user.RpmLimit
	}
	if user.ConcurrencyLimit != nil {
		userConcurrency = *user.ConcurrencyLimit
	}
	if user.StreamTpsLimit != nil {
		userStreamTPS = *user.StreamTpsLimit
	}
	return buildUserRateLimitPolicy(userID, group, userRPM, userConcurrency, userStreamTPS), nil
}

func requestRateLimitGroup(c *gin.Context) string {
	group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	if group == "" {
		group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}
	return strings.TrimSpace(group)
}

func buildUserRateLimitPolicy(userID int, group string, userRPM, userConcurrency, userStreamTPS int) UserRateLimitPolicy {
	policy := UserRateLimitPolicy{
		UserID:               userID,
		Group:                strings.TrimSpace(group),
		UserRPMLimit:         userRPM,
		UserConcurrencyLimit: userConcurrency,
		UserStreamTPSLimit:   userStreamTPS,
		RPMLimit:             userRPM,
		ConcurrencyLimit:     userConcurrency,
		StreamTPSLimit:       userStreamTPS,
	}
	snapshot := group_rate_limit_setting.GetSettingSnapshot()
	if snapshot == nil || policy.Group == "" {
		return policy
	}
	groupPolicy, found := snapshot.Policies[policy.Group]
	if !found {
		return policy
	}
	if snapshot.MemberEnabled {
		policy.MemberRPMLimit = configuredLimit(groupPolicy.MemberLimits.RPMLimit)
		policy.MemberConcurrencyLimit = configuredLimit(groupPolicy.MemberLimits.ConcurrencyLimit)
		policy.MemberStreamTPSLimit = configuredLimit(groupPolicy.MemberLimits.StreamTPSLimit)
		policy.RPMLimit = minimumPositive(policy.UserRPMLimit, policy.MemberRPMLimit)
		policy.ConcurrencyLimit = minimumPositive(policy.UserConcurrencyLimit, policy.MemberConcurrencyLimit)
		policy.StreamTPSLimit = minimumPositive(policy.UserStreamTPSLimit, policy.MemberStreamTPSLimit)
	}
	if snapshot.SharedPoolEnabled {
		policy.SharedRPMLimit = configuredLimit(groupPolicy.SharedPool.RPMLimit)
		policy.SharedConcurrencyLimit = configuredLimit(groupPolicy.SharedPool.ConcurrencyLimit)
		policy.SharedStreamTPSLimit = configuredLimit(groupPolicy.SharedPool.StreamTPSLimit)
	}
	return policy
}

func configuredLimit(value *int) int {
	if value == nil || *value <= 0 {
		return 0
	}
	return *value
}

func minimumPositive(left, right int) int {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}

type UserConcurrencyWaitOptions struct {
	Heartbeat func() error
}

type userConcurrencyConfig struct {
	leaseTTL          time.Duration
	renewEvery        time.Duration
	waitTimeout       time.Duration
	heartbeat         time.Duration
	initialPoll       time.Duration
	maximumPoll       time.Duration
	waitingLimit      int
	groupWaitingLimit int
}

func defaultUserConcurrencyConfig() userConcurrencyConfig {
	return userConcurrencyConfig{
		leaseTTL:          userConcurrencyLeaseTTL,
		renewEvery:        userConcurrencyRenewEvery,
		waitTimeout:       userConcurrencyWaitTimeout,
		heartbeat:         userConcurrencyHeartbeat,
		initialPoll:       userConcurrencyInitialPoll,
		maximumPoll:       userConcurrencyMaximumPoll,
		waitingLimit:      userConcurrencyWaitingLimit,
		groupWaitingLimit: groupConcurrencyWaitingLimit,
	}
}

type UserConcurrencyLease struct {
	policy      UserRateLimitPolicy
	leaseID     string
	requestID   string
	leaseTTL    time.Duration
	renewEvery  time.Duration
	stop        chan struct{}
	done        chan struct{}
	releaseOnce sync.Once
}

func (l *UserConcurrencyLease) Release() {
	if l == nil || l.leaseID == "" {
		return
	}
	l.releaseOnce.Do(func() {
		close(l.stop)
		<-l.done
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, enabled := getUserRateLimitRedisClient()
		if !enabled {
			return
		}
		if err := client.Eval(ctx, `
	if tonumber(ARGV[2]) == 1 then
	  redis.call('ZREM', KEYS[1], ARGV[1])
	  redis.call('ZREM', KEYS[2], ARGV[1])
	end
	if tonumber(ARGV[3]) == 1 then
	  redis.call('ZREM', KEYS[3], ARGV[1])
	  redis.call('ZREM', KEYS[4], ARGV[1])
	end
	return 1`, concurrencyKeys(l.policy), l.leaseID, boolInt(l.policy.ConcurrencyLimit > 0), boolInt(l.policy.SharedConcurrencyLimit > 0)).Err(); err != nil {
			common.SysError(fmt.Sprintf("failed to release concurrency lease: user=%d group=%s request=%s error=%s", l.policy.UserID, l.policy.Group, l.requestID, err.Error()))
		}
	})
}

func (l *UserConcurrencyLease) startRenewal(c *gin.Context, observation *UserRateLimitObservation) {
	if l == nil || l.leaseID == "" {
		return
	}
	go func() {
		defer close(l.done)
		ticker := time.NewTicker(l.renewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				client, enabled := getUserRateLimitRedisClient()
				if !enabled {
					observation.noteRedisFailOpen("concurrency_renew")
					logger.LogWarn(c, fmt.Sprintf("concurrency lease renewal failed open: user=%d group=%s request=%s redis disabled", l.policy.UserID, l.policy.Group, l.requestID))
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				result, err := client.Eval(ctx, `
	if tonumber(ARGV[3]) == 1 and not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 0 end
	if tonumber(ARGV[4]) == 1 and not redis.call('ZSCORE', KEYS[3], ARGV[1]) then return 0 end
	local now = redis.call('TIME')
	local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
	if tonumber(ARGV[3]) == 1 then
	  redis.call('ZADD', KEYS[1], now_ms, ARGV[1])
	  redis.call('PEXPIRE', KEYS[1], ARGV[2])
	end
	if tonumber(ARGV[4]) == 1 then
	  redis.call('ZADD', KEYS[3], now_ms, ARGV[1])
	  redis.call('PEXPIRE', KEYS[3], ARGV[2])
	end
	return 1`, concurrencyKeys(l.policy), l.leaseID, l.leaseTTL.Milliseconds()*2,
					boolInt(l.policy.ConcurrencyLimit > 0), boolInt(l.policy.SharedConcurrencyLimit > 0)).Int()
				cancel()
				if err != nil || result != 1 {
					observation.noteRedisFailOpen("concurrency_renew")
					errText := "lease missing"
					if err != nil {
						errText = err.Error()
					}
					logger.LogWarn(c, fmt.Sprintf("concurrency lease renewal failed open: user=%d group=%s request=%s error=%s", l.policy.UserID, l.policy.Group, l.requestID, errText))
				}
			case <-l.stop:
				return
			}
		}
	}()
}

type UserRequestRateGuard struct {
	Policy UserRateLimitPolicy
	Lease  *UserConcurrencyLease
	Pacer  *UserStreamPacer

	claimed atomic.Bool
}

func (g *UserRequestRateGuard) Claim() {
	if g != nil {
		g.claimed.Store(true)
	}
}

func (g *UserRequestRateGuard) Unclaim() {
	if g != nil {
		g.claimed.Store(false)
	}
}

func (g *UserRequestRateGuard) Claimed() bool {
	return g != nil && g.claimed.Load()
}

func (g *UserRequestRateGuard) Release() {
	if g != nil && g.Lease != nil {
		g.Lease.Release()
	}
}

func (g *UserRequestRateGuard) Pace(ctx context.Context, payload []byte) error {
	if g == nil || g.Pacer == nil {
		return nil
	}
	return g.Pacer.PacePayload(ctx, payload)
}

func BeginUserRequestRateLimit(c *gin.Context, policy UserRateLimitPolicy, modelName string, options UserConcurrencyWaitOptions) (*UserRequestRateGuard, *types.NewAPIError) {
	observation := &UserRateLimitObservation{}
	c.Set(userRateLimitObservationKey, observation)
	observation.notePolicy(policy)
	if policy.UserID <= 0 {
		return &UserRequestRateGuard{Policy: policy}, nil
	}
	if policy.RPMLimit > 0 {
		allowed, failOpen := checkUserRPM(c.Request.Context(), policy.UserID, policy.RPMLimit)
		if failOpen != nil {
			if c.Request.Context().Err() != nil {
				return nil, types.NewError(c.Request.Context().Err(), types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
			}
			observation.noteRedisFailOpen("rpm")
			logger.LogWarn(c, fmt.Sprintf("user RPM limiter failed open: user=%d request=%s error=%s", policy.UserID, c.GetString(common.RequestIdKey), failOpen.Error()))
		} else if !allowed {
			observation.noteRejectedLayer("individual_rpm")
			return nil, newUserRateLimitError()
		}
	}
	if policy.SharedRPMLimit > 0 {
		allowed, failOpen := checkGroupRPM(c.Request.Context(), policy.Group, policy.SharedRPMLimit)
		if failOpen != nil {
			if c.Request.Context().Err() != nil {
				return nil, types.NewError(c.Request.Context().Err(), types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
			}
			observation.noteRedisFailOpen("shared_rpm")
			logger.LogWarn(c, fmt.Sprintf("group shared RPM limiter failed open: user=%d group=%s request=%s error=%s", policy.UserID, policy.Group, c.GetString(common.RequestIdKey), failOpen.Error()))
		} else if !allowed {
			observation.noteRejectedLayer("group_shared_rpm")
			return nil, newUserRateLimitError()
		}
	}
	guard := &UserRequestRateGuard{Policy: policy}
	if policy.HasConcurrencyLimit() {
		lease, apiErr := acquireRequestConcurrency(c, policy, options, observation, defaultUserConcurrencyConfig())
		if apiErr != nil {
			return nil, apiErr
		}
		guard.Lease = lease
	}
	if policy.StreamTPSLimit > 0 || policy.SharedStreamTPSLimit > 0 {
		guard.Pacer = newRequestStreamPacer(c, policy, modelName, observation)
	}
	return guard, nil
}

func InstallUserStreamPacer(c *gin.Context, pacer *UserStreamPacer) {
	if c == nil {
		return
	}
	if pacer == nil {
		c.Set(userStreamPacerKey, nil)
		return
	}
	c.Set(userStreamPacerKey, pacer)
}

func PaceUserStreamPayload(c *gin.Context, payload []byte) error {
	if c == nil {
		return nil
	}
	value, ok := c.Get(userStreamPacerKey)
	if !ok {
		return nil
	}
	pacer, ok := value.(*UserStreamPacer)
	if !ok || pacer == nil {
		return nil
	}
	return pacer.PacePayload(c.Request.Context(), payload)
}

func IsUserStreamPacing(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(userStreamPacerKey)
	if !ok {
		return false
	}
	pacer, ok := value.(*UserStreamPacer)
	return ok && pacer != nil && pacer.pacing.Load()
}

func newUserRateLimitError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("rate_limit_exceeded"),
		types.ErrorCode("rate_limit_exceeded"),
		http.StatusTooManyRequests,
		types.ErrOptionWithSkipRetry(),
	)
}

func checkUserRPM(ctx context.Context, userID, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	client, enabled := getUserRateLimitRedisClient()
	if !enabled {
		return true, errors.New("redis disabled")
	}
	now, err := client.Time(ctx).Result()
	if err != nil {
		return true, err
	}
	key := fmt.Sprintf("user_rate_limit:rpm:%d:%d", userID, now.Unix()/60)
	pipe := client.TxPipeline()
	count := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 120*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, err
	}
	return count.Val() <= int64(limit), nil
}

func checkGroupRPM(ctx context.Context, group string, limit int) (bool, error) {
	if limit <= 0 || strings.TrimSpace(group) == "" {
		return true, nil
	}
	client, enabled := getUserRateLimitRedisClient()
	if !enabled {
		return true, errors.New("redis disabled")
	}
	now, err := client.Time(ctx).Result()
	if err != nil {
		return true, err
	}
	key := groupRPMKey(group, now.Unix()/60)
	pipe := client.TxPipeline()
	count := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 120*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, err
	}
	return count.Val() <= int64(limit), nil
}

func groupRPMKey(group string, minute int64) string {
	return fmt.Sprintf("group_rate_limit:rpm:%s:%d", groupRateLimitKeyID(group), minute)
}

func userConcurrencyActiveKey(userID int) string {
	return fmt.Sprintf("user_rate_limit:concurrency:active:%d", userID)
}

func userConcurrencyWaitingKey(userID int) string {
	return fmt.Sprintf("user_rate_limit:concurrency:waiting:%d", userID)
}

func groupConcurrencyActiveKey(group string) string {
	return "group_rate_limit:concurrency:active:" + groupRateLimitKeyID(group)
}

func groupConcurrencyWaitingKey(group string) string {
	return "group_rate_limit:concurrency:waiting:" + groupRateLimitKeyID(group)
}

func groupRateLimitKeyID(group string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(group))))
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func concurrencyKeys(policy UserRateLimitPolicy) []string {
	return []string{
		userConcurrencyActiveKey(policy.UserID),
		userConcurrencyWaitingKey(policy.UserID),
		groupConcurrencyActiveKey(policy.Group),
		groupConcurrencyWaitingKey(policy.Group),
	}
}

func acquireUserConcurrency(c *gin.Context, userID, limit int, options UserConcurrencyWaitOptions, observation *UserRateLimitObservation, config userConcurrencyConfig) (*UserConcurrencyLease, *types.NewAPIError) {
	return acquireRequestConcurrency(c, UserRateLimitPolicy{UserID: userID, ConcurrencyLimit: limit}, options, observation, config)
}

func acquireRequestConcurrency(c *gin.Context, policy UserRateLimitPolicy, options UserConcurrencyWaitOptions, observation *UserRateLimitObservation, config userConcurrencyConfig) (*UserConcurrencyLease, *types.NewAPIError) {
	if !policy.HasConcurrencyLimit() {
		return nil, nil
	}
	client, enabled := getUserRateLimitRedisClient()
	if !enabled {
		observation.noteRedisFailOpen("concurrency")
		logger.LogWarn(c, fmt.Sprintf("concurrency limiter failed open: user=%d group=%s request=%s redis disabled", policy.UserID, policy.Group, c.GetString(common.RequestIdKey)))
		return nil, nil
	}
	if config.groupWaitingLimit <= 0 {
		config.groupWaitingLimit = groupConcurrencyWaitingLimit
	}
	requestID := c.GetString(common.RequestIdKey)
	leaseID := common.NewRequestId()
	start := time.Now()
	deadline := start.Add(config.waitTimeout)
	poll := config.initialPoll
	queued := false
	waitedOnUser := false
	waitedOnGroup := false
	var heartbeat <-chan time.Time
	var heartbeatTicker *time.Ticker
	if options.Heartbeat != nil {
		heartbeatTicker = time.NewTicker(config.heartbeat)
		defer heartbeatTicker.Stop()
		heartbeat = heartbeatTicker.C
	}
	for {
		if queued && !time.Now().Before(deadline) {
			removeConcurrencyWaiter(policy, leaseID)
			noteConcurrencyQueueWait(observation, start, waitedOnUser, waitedOnGroup)
			observation.noteRejectedLayer(concurrencyRejectedLayer(waitedOnUser, waitedOnGroup))
			return nil, newUserRateLimitError()
		}
		result, err := client.Eval(c.Request.Context(), `
	local now = redis.call('TIME')
	local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
	local user_limit = tonumber(ARGV[2])
	local group_limit = tonumber(ARGV[3])
	local lease_ttl = tonumber(ARGV[4])
	local wait_ttl = tonumber(ARGV[5])
	local user_enabled = user_limit > 0
	local group_enabled = group_limit > 0
	if user_enabled then
	  redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now_ms - lease_ttl)
	  redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now_ms - wait_ttl)
	end
	if group_enabled then
	  redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', now_ms - lease_ttl)
	  redis.call('ZREMRANGEBYSCORE', KEYS[4], '-inf', now_ms - wait_ttl)
	end
	local user_active = (not user_enabled) or redis.call('ZSCORE', KEYS[1], ARGV[1])
	local group_active = (not group_enabled) or redis.call('ZSCORE', KEYS[3], ARGV[1])
	if user_active and group_active then return {1, 0, 0} end
	if user_enabled and user_active then redis.call('ZREM', KEYS[1], ARGV[1]) end
	if group_enabled and group_active then redis.call('ZREM', KEYS[3], ARGV[1]) end
	local user_blocked = user_enabled and redis.call('ZCARD', KEYS[1]) >= user_limit
	local group_blocked = group_enabled and redis.call('ZCARD', KEYS[3]) >= group_limit
	if not user_blocked and not group_blocked then
	  if user_enabled then
	    redis.call('ZADD', KEYS[1], now_ms, ARGV[1])
	    redis.call('ZREM', KEYS[2], ARGV[1])
	    redis.call('PEXPIRE', KEYS[1], lease_ttl * 2)
	  end
	  if group_enabled then
	    redis.call('ZADD', KEYS[3], now_ms, ARGV[1])
	    redis.call('ZREM', KEYS[4], ARGV[1])
	    redis.call('PEXPIRE', KEYS[3], lease_ttl * 2)
	  end
	  return {1, 0, 0}
	end
	local user_waiting = (not user_enabled) or redis.call('ZSCORE', KEYS[2], ARGV[1])
	local group_waiting = (not group_enabled) or redis.call('ZSCORE', KEYS[4], ARGV[1])
	if (user_enabled and not user_waiting and redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[6])) or
	   (group_enabled and not group_waiting and redis.call('ZCARD', KEYS[4]) >= tonumber(ARGV[7])) then
	  return {-1, user_blocked and 1 or 0, group_blocked and 1 or 0}
	end
	if user_enabled then
	  redis.call('ZADD', KEYS[2], now_ms, ARGV[1])
	  redis.call('PEXPIRE', KEYS[2], wait_ttl * 2)
	end
	if group_enabled then
	  redis.call('ZADD', KEYS[4], now_ms, ARGV[1])
	  redis.call('PEXPIRE', KEYS[4], wait_ttl * 2)
	end
	return {0, user_blocked and 1 or 0, group_blocked and 1 or 0}`, concurrencyKeys(policy),
			leaseID, policy.ConcurrencyLimit, policy.SharedConcurrencyLimit, config.leaseTTL.Milliseconds(), config.waitTimeout.Milliseconds(), config.waitingLimit, config.groupWaitingLimit).Int64Slice()
		if err != nil {
			if c.Request.Context().Err() != nil {
				removeConcurrencyWaiter(policy, leaseID)
				return nil, types.NewError(c.Request.Context().Err(), types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
			}
			observation.noteRedisFailOpen("concurrency")
			removeConcurrencyWaiter(policy, leaseID)
			logger.LogWarn(c, fmt.Sprintf("concurrency limiter failed open: user=%d group=%s request=%s error=%s", policy.UserID, policy.Group, requestID, err.Error()))
			return nil, nil
		}
		if len(result) != 3 {
			observation.noteRedisFailOpen("concurrency")
			removeConcurrencyWaiter(policy, leaseID)
			logger.LogWarn(c, fmt.Sprintf("concurrency limiter failed open: user=%d group=%s request=%s invalid Redis response", policy.UserID, policy.Group, requestID))
			return nil, nil
		}
		waitedOnUser = waitedOnUser || result[1] == 1
		waitedOnGroup = waitedOnGroup || result[2] == 1
		switch result[0] {
		case 1:
			if queued {
				noteConcurrencyQueueWait(observation, start, waitedOnUser, waitedOnGroup)
			}
			lease := &UserConcurrencyLease{
				policy: policy, leaseID: leaseID, requestID: requestID,
				leaseTTL: config.leaseTTL, renewEvery: config.renewEvery,
				stop: make(chan struct{}), done: make(chan struct{}),
			}
			lease.startRenewal(c, observation)
			return lease, nil
		case -1:
			removeConcurrencyWaiter(policy, leaseID)
			observation.noteRejectedLayer(concurrencyRejectedLayer(result[1] == 1, result[2] == 1))
			return nil, newUserRateLimitError()
		}
		queued = true

		remaining := time.Until(deadline)
		if remaining <= 0 {
			removeConcurrencyWaiter(policy, leaseID)
			noteConcurrencyQueueWait(observation, start, waitedOnUser, waitedOnGroup)
			observation.noteRejectedLayer(concurrencyRejectedLayer(waitedOnUser, waitedOnGroup))
			return nil, newUserRateLimitError()
		}
		wait := poll
		if wait > remaining {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			if poll < config.maximumPoll {
				poll *= 2
				if poll > config.maximumPoll {
					poll = config.maximumPoll
				}
			}
		case <-heartbeat:
			if !timer.Stop() {
				<-timer.C
			}
			if err := options.Heartbeat(); err != nil {
				removeConcurrencyWaiter(policy, leaseID)
				return nil, types.NewError(err, types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
			}
		case <-c.Request.Context().Done():
			if !timer.Stop() {
				<-timer.C
			}
			removeConcurrencyWaiter(policy, leaseID)
			return nil, types.NewError(c.Request.Context().Err(), types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
		}
	}
}

func noteConcurrencyQueueWait(observation *UserRateLimitObservation, start time.Time, user, group bool) {
	wait := time.Since(start)
	var userWait time.Duration
	var groupWait time.Duration
	if user {
		userWait = wait
	}
	if group {
		groupWait = wait
	}
	observation.noteQueueWait(userWait, groupWait)
}

func concurrencyRejectedLayer(user, group bool) string {
	switch {
	case user && group:
		return "individual_and_group_shared_concurrency"
	case group:
		return "group_shared_concurrency"
	default:
		return "individual_concurrency"
	}
}

func removeUserConcurrencyWaiter(userID int, leaseID string) {
	removeConcurrencyWaiter(UserRateLimitPolicy{UserID: userID, ConcurrencyLimit: 1}, leaseID)
}

func removeConcurrencyWaiter(policy UserRateLimitPolicy, leaseID string) {
	client, enabled := getUserRateLimitRedisClient()
	if !enabled || leaseID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Eval(ctx, `
	if tonumber(ARGV[2]) == 1 then redis.call('ZREM', KEYS[1], ARGV[1]) end
	if tonumber(ARGV[3]) == 1 then redis.call('ZREM', KEYS[2], ARGV[1]) end
	return 1`, []string{userConcurrencyWaitingKey(policy.UserID), groupConcurrencyWaitingKey(policy.Group)},
		leaseID, boolInt(policy.ConcurrencyLimit > 0), boolInt(policy.SharedConcurrencyLimit > 0)).Err(); err != nil {
		common.SysError(fmt.Sprintf("failed to remove concurrency waiter: user=%d group=%s error=%s", policy.UserID, policy.Group, err.Error()))
	}
}

type streamRateWaiter interface {
	WaitN(context.Context, int) error
}

type UserStreamPacer struct {
	limit       int
	modelName   string
	waiters     []streamRateWaiter
	observation *UserRateLimitObservation
	pacing      atomic.Bool
}

func NewUserStreamPacer(limit int, modelName string, observation *UserRateLimitObservation) *UserStreamPacer {
	if limit <= 0 {
		return nil
	}
	return &UserStreamPacer{
		limit:       limit,
		modelName:   modelName,
		waiters:     []streamRateWaiter{rate.NewLimiter(rate.Limit(limit), limit)},
		observation: observation,
	}
}

func newUserStreamPacerWithWaiter(limit int, modelName string, observation *UserRateLimitObservation, waiter streamRateWaiter) *UserStreamPacer {
	return &UserStreamPacer{limit: limit, modelName: modelName, observation: observation, waiters: []streamRateWaiter{waiter}}
}

func newRequestStreamPacer(c *gin.Context, policy UserRateLimitPolicy, modelName string, observation *UserRateLimitObservation) *UserStreamPacer {
	limit := minimumPositive(policy.StreamTPSLimit, policy.SharedStreamTPSLimit)
	if limit <= 0 {
		return nil
	}
	waiters := make([]streamRateWaiter, 0, 2)
	if policy.StreamTPSLimit > 0 {
		waiters = append(waiters, rate.NewLimiter(rate.Limit(policy.StreamTPSLimit), policy.StreamTPSLimit))
	}
	if policy.SharedStreamTPSLimit > 0 && policy.Group != "" {
		waiters = append(waiters, &groupStreamRateWaiter{
			c:           c,
			group:       policy.Group,
			limit:       policy.SharedStreamTPSLimit,
			observation: observation,
		})
	}
	return &UserStreamPacer{
		limit:       limit,
		modelName:   modelName,
		waiters:     waiters,
		observation: observation,
	}
}

func (p *UserStreamPacer) PacePayload(ctx context.Context, payload []byte) error {
	if p == nil || p.limit <= 0 || len(p.waiters) == 0 {
		return nil
	}
	text := streamPayloadText(payload)
	if text == "" {
		return nil
	}
	tokens := CountTextToken(text, p.modelName)
	if tokens < 1 {
		tokens = 1
	}
	started := time.Now()
	p.pacing.Store(true)
	defer p.pacing.Store(false)
	remaining := tokens
	for remaining > 0 {
		chunk := remaining
		if chunk > p.limit {
			chunk = p.limit
		}
		if err := waitForStreamRates(ctx, p.waiters, chunk); err != nil {
			return err
		}
		remaining -= chunk
	}
	p.observation.notePacing(tokens, time.Since(started))
	return nil
}

func waitForStreamRates(ctx context.Context, waiters []streamRateWaiter, tokens int) error {
	if len(waiters) == 1 {
		return waiters[0].WaitN(ctx, tokens)
	}
	waitContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, len(waiters))
	for _, waiter := range waiters {
		go func(waiter streamRateWaiter) {
			results <- waiter.WaitN(waitContext, tokens)
		}(waiter)
	}
	var firstError error
	for range waiters {
		if err := <-results; err != nil && firstError == nil {
			firstError = err
			cancel()
		}
	}
	return firstError
}

type groupStreamRateWaiter struct {
	c           *gin.Context
	group       string
	limit       int
	observation *UserRateLimitObservation
}

func (w *groupStreamRateWaiter) WaitN(ctx context.Context, tokens int) error {
	if tokens <= 0 || w == nil || w.limit <= 0 || w.group == "" {
		return nil
	}
	started := time.Now()
	wait, err := reserveGroupStreamTokens(ctx, w.group, w.limit, tokens)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.observation.noteRedisFailOpen("shared_tps")
		logger.LogWarn(w.c, fmt.Sprintf("group shared TPS limiter failed open: group=%s request=%s error=%s", w.group, w.c.GetString(common.RequestIdKey), err.Error()))
		return nil
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	w.observation.noteSharedPacing(tokens, time.Since(started))
	return nil
}

func reserveGroupStreamTokens(ctx context.Context, group string, limit, tokens int) (time.Duration, error) {
	if limit <= 0 || tokens <= 0 || strings.TrimSpace(group) == "" {
		return 0, nil
	}
	client, enabled := getUserRateLimitRedisClient()
	if !enabled {
		return 0, errors.New("redis disabled")
	}
	delayMicros, err := client.Eval(ctx, `
	local now = redis.call('TIME')
	local now_us = tonumber(now[1]) * 1000000 + tonumber(now[2])
	local rate = tonumber(ARGV[1])
	local requested = tonumber(ARGV[2])
	local tokens = tonumber(redis.call('HGET', KEYS[1], 'tokens'))
	local last_us = tonumber(redis.call('HGET', KEYS[1], 'last_us'))
	if not tokens or not last_us then
	  tokens = rate
	  last_us = now_us
	end
	if now_us > last_us then
	  tokens = math.min(rate, tokens + ((now_us - last_us) * rate / 1000000))
	  last_us = now_us
	end
	local delay_us = 0
	if tokens >= requested then
	  tokens = tokens - requested
	else
	  local deficit = requested - tokens
	  tokens = 0
	  last_us = last_us + math.ceil(deficit * 1000000 / rate)
	  delay_us = math.max(0, last_us - now_us)
	end
	redis.call('HSET', KEYS[1], 'tokens', tokens, 'last_us', last_us)
	redis.call('PEXPIRE', KEYS[1], math.ceil(delay_us / 1000) + 2000)
	return math.ceil(delay_us)`, []string{groupStreamTPSKey(group)}, limit, tokens).Int64()
	if err != nil {
		return 0, err
	}
	return time.Duration(delayMicros) * time.Microsecond, nil
}

func groupStreamTPSKey(group string) string {
	return "group_rate_limit:stream_tps:" + groupRateLimitKeyID(group)
}

func streamPayloadText(payload []byte) string {
	raw := strings.TrimSpace(string(payload))
	if raw == "" || raw == "[DONE]" || !gjson.Valid(raw) {
		return ""
	}
	root := gjson.Parse(raw)
	eventType := root.Get("type").String()
	var texts []string
	appendString := func(result gjson.Result) {
		if result.Type == gjson.String && result.String() != "" {
			texts = append(texts, result.String())
		}
	}
	appendJSON := func(result gjson.Result) {
		if result.Exists() && result.Raw != "" && result.Raw != "null" && result.Raw != "{}" {
			texts = append(texts, result.Raw)
		}
	}

	if strings.Contains(eventType, "delta") && !strings.Contains(eventType, "audio.delta") {
		switch {
		case strings.Contains(eventType, "output_text"),
			strings.Contains(eventType, "reasoning"),
			strings.Contains(eventType, "thinking"),
			strings.Contains(eventType, "transcript"),
			strings.Contains(eventType, "function_call_arguments"),
			strings.Contains(eventType, "tool"):
			appendString(root.Get("delta"))
		}
	}

	root.Get("choices").ForEach(func(_, choice gjson.Result) bool {
		delta := choice.Get("delta")
		appendString(delta.Get("content"))
		appendString(delta.Get("reasoning_content"))
		appendString(delta.Get("reasoning"))
		appendString(delta.Get("function_call.arguments"))
		delta.Get("tool_calls").ForEach(func(_, tool gjson.Result) bool {
			appendString(tool.Get("function.arguments"))
			return true
		})
		appendString(choice.Get("text"))
		return true
	})

	appendString(root.Get("delta.text"))
	appendString(root.Get("delta.thinking"))
	appendString(root.Get("delta.partial_json"))
	appendString(root.Get("content_block.text"))

	root.Get("candidates").ForEach(func(_, candidate gjson.Result) bool {
		candidate.Get("content.parts").ForEach(func(_, part gjson.Result) bool {
			appendString(part.Get("text"))
			appendJSON(part.Get("functionCall.args"))
			return true
		})
		return true
	})

	return strings.Join(texts, "")
}
