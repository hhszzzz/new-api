package service

import (
	"context"
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

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"golang.org/x/time/rate"
)

const (
	userRateLimitObservationKey = "user_rate_limit_observation"
	userStreamPacerKey          = "user_stream_pacer"

	userConcurrencyLeaseTTL     = 60 * time.Second
	userConcurrencyRenewEvery   = 20 * time.Second
	userConcurrencyWaitTimeout  = 30 * time.Second
	userConcurrencyHeartbeat    = 5 * time.Second
	userConcurrencyInitialPoll  = 100 * time.Millisecond
	userConcurrencyMaximumPoll  = time.Second
	userConcurrencyWaitingLimit = 20
)

type UserRateLimitPolicy struct {
	UserID           int
	RPMLimit         int
	ConcurrencyLimit int
	StreamTPSLimit   int
}

type UserRateLimitObservation struct {
	mu sync.Mutex

	rpmLimit         int
	concurrencyLimit int
	streamTPSLimit   int
	queueWait        time.Duration
	pacingTokens     int64
	pacingWait       time.Duration
	redisFailOpen    map[string]bool
}

func (o *UserRateLimitObservation) notePolicy(policy UserRateLimitPolicy) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.rpmLimit = policy.RPMLimit
	o.concurrencyLimit = policy.ConcurrencyLimit
	o.streamTPSLimit = policy.StreamTPSLimit
}

func (o *UserRateLimitObservation) noteQueueWait(wait time.Duration) {
	if o == nil || wait <= 0 {
		return
	}
	o.mu.Lock()
	o.queueWait += wait
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
	if observation.rpmLimit <= 0 && observation.concurrencyLimit <= 0 && observation.streamTPSLimit <= 0 && len(observation.redisFailOpen) == 0 {
		return
	}
	limits := make(map[string]interface{})
	if observation.rpmLimit > 0 {
		limits["rpm_limit"] = observation.rpmLimit
	}
	if observation.concurrencyLimit > 0 {
		limits["concurrency_limit"] = observation.concurrencyLimit
	}
	if observation.streamTPSLimit > 0 {
		limits["stream_tps_limit"] = observation.streamTPSLimit
	}
	if observation.queueWait > 0 {
		limits["queue_wait_ms"] = observation.queueWait.Milliseconds()
	}
	if observation.pacingTokens > 0 {
		limits["pacing_tokens"] = observation.pacingTokens
		limits["pacing_wait_ms"] = observation.pacingWait.Milliseconds()
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

func UserRateLimitPolicyFromContext(c *gin.Context) UserRateLimitPolicy {
	if c == nil {
		return UserRateLimitPolicy{}
	}
	return UserRateLimitPolicy{
		UserID:           common.GetContextKeyInt(c, constant.ContextKeyUserId),
		RPMLimit:         common.GetContextKeyInt(c, constant.ContextKeyUserRpmLimit),
		ConcurrencyLimit: common.GetContextKeyInt(c, constant.ContextKeyUserConcurrencyLimit),
		StreamTPSLimit:   common.GetContextKeyInt(c, constant.ContextKeyUserStreamTpsLimit),
	}
}

func LoadUserRateLimitPolicy(userID int) (UserRateLimitPolicy, error) {
	if userID <= 0 {
		return UserRateLimitPolicy{}, errors.New("invalid user id")
	}
	var user struct {
		RPMLimit         *int `gorm:"column:rpm_limit"`
		ConcurrencyLimit *int `gorm:"column:concurrency_limit"`
		StreamTPSLimit   *int `gorm:"column:stream_tps_limit"`
	}
	if err := model.DB.Model(&model.User{}).
		Select("rpm_limit", "concurrency_limit", "stream_tps_limit").
		Where("id = ?", userID).Take(&user).Error; err != nil {
		return UserRateLimitPolicy{}, err
	}
	policy := UserRateLimitPolicy{UserID: userID}
	if user.RPMLimit != nil {
		policy.RPMLimit = *user.RPMLimit
	}
	if user.ConcurrencyLimit != nil {
		policy.ConcurrencyLimit = *user.ConcurrencyLimit
	}
	if user.StreamTPSLimit != nil {
		policy.StreamTPSLimit = *user.StreamTPSLimit
	}
	return policy, nil
}

type UserConcurrencyWaitOptions struct {
	Heartbeat func() error
}

type userConcurrencyConfig struct {
	leaseTTL     time.Duration
	renewEvery   time.Duration
	waitTimeout  time.Duration
	heartbeat    time.Duration
	initialPoll  time.Duration
	maximumPoll  time.Duration
	waitingLimit int
}

func defaultUserConcurrencyConfig() userConcurrencyConfig {
	return userConcurrencyConfig{
		leaseTTL:     userConcurrencyLeaseTTL,
		renewEvery:   userConcurrencyRenewEvery,
		waitTimeout:  userConcurrencyWaitTimeout,
		heartbeat:    userConcurrencyHeartbeat,
		initialPoll:  userConcurrencyInitialPoll,
		maximumPoll:  userConcurrencyMaximumPoll,
		waitingLimit: userConcurrencyWaitingLimit,
	}
}

type UserConcurrencyLease struct {
	userID      int
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
		if !common.RedisEnabled || common.RDB == nil {
			return
		}
		if err := common.RDB.Eval(ctx, `
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
return 1`, []string{userConcurrencyActiveKey(l.userID), userConcurrencyWaitingKey(l.userID)}, l.leaseID).Err(); err != nil {
			common.SysError(fmt.Sprintf("failed to release user concurrency lease: user=%d request=%s error=%s", l.userID, l.requestID, err.Error()))
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
				if !common.RedisEnabled || common.RDB == nil {
					observation.noteRedisFailOpen("concurrency_renew")
					logger.LogWarn(c, fmt.Sprintf("user concurrency lease renewal failed open: user=%d request=%s redis disabled", l.userID, l.requestID))
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := common.RDB.Eval(ctx, `
local score = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not score then return 0 end
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
redis.call('ZADD', KEYS[1], now_ms, ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1`, []string{userConcurrencyActiveKey(l.userID)}, l.leaseID, l.leaseTTL.Milliseconds()*2).Err()
				cancel()
				if err != nil {
					observation.noteRedisFailOpen("concurrency_renew")
					logger.LogWarn(c, fmt.Sprintf("user concurrency lease renewal failed open: user=%d request=%s error=%s", l.userID, l.requestID, err.Error()))
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
			return nil, newUserRateLimitError()
		}
	}
	guard := &UserRequestRateGuard{Policy: policy}
	if policy.ConcurrencyLimit > 0 {
		lease, apiErr := acquireUserConcurrency(c, policy.UserID, policy.ConcurrencyLimit, options, observation, defaultUserConcurrencyConfig())
		if apiErr != nil {
			return nil, apiErr
		}
		guard.Lease = lease
	}
	if policy.StreamTPSLimit > 0 {
		guard.Pacer = NewUserStreamPacer(policy.StreamTPSLimit, modelName, observation)
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
	if !common.RedisEnabled || common.RDB == nil {
		return true, errors.New("redis disabled")
	}
	now, err := common.RDB.Time(ctx).Result()
	if err != nil {
		return true, err
	}
	key := fmt.Sprintf("user_rate_limit:rpm:%d:%d", userID, now.Unix()/60)
	pipe := common.RDB.TxPipeline()
	count := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 120*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, err
	}
	return count.Val() <= int64(limit), nil
}

func userConcurrencyActiveKey(userID int) string {
	return fmt.Sprintf("user_rate_limit:concurrency:active:%d", userID)
}

func userConcurrencyWaitingKey(userID int) string {
	return fmt.Sprintf("user_rate_limit:concurrency:waiting:%d", userID)
}

func acquireUserConcurrency(c *gin.Context, userID, limit int, options UserConcurrencyWaitOptions, observation *UserRateLimitObservation, config userConcurrencyConfig) (*UserConcurrencyLease, *types.NewAPIError) {
	if limit <= 0 {
		return nil, nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		observation.noteRedisFailOpen("concurrency")
		logger.LogWarn(c, fmt.Sprintf("user concurrency limiter failed open: user=%d request=%s redis disabled", userID, c.GetString(common.RequestIdKey)))
		return nil, nil
	}
	requestID := c.GetString(common.RequestIdKey)
	leaseID := common.NewRequestId()
	start := time.Now()
	deadline := start.Add(config.waitTimeout)
	poll := config.initialPoll
	queued := false
	var heartbeat <-chan time.Time
	var heartbeatTicker *time.Ticker
	if options.Heartbeat != nil {
		heartbeatTicker = time.NewTicker(config.heartbeat)
		defer heartbeatTicker.Stop()
		heartbeat = heartbeatTicker.C
	}
	for {
		if queued && !time.Now().Before(deadline) {
			removeUserConcurrencyWaiter(userID, leaseID)
			observation.noteQueueWait(time.Since(start))
			return nil, newUserRateLimitError()
		}
		result, err := common.RDB.Eval(c.Request.Context(), `
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now_ms - tonumber(ARGV[3]))
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now_ms - tonumber(ARGV[4]))
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 1 end
if redis.call('ZCARD', KEYS[1]) < tonumber(ARGV[2]) then
  redis.call('ZADD', KEYS[1], now_ms, ARGV[1])
  redis.call('ZREM', KEYS[2], ARGV[1])
  redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[3]) * 2)
  return 1
end
if redis.call('ZSCORE', KEYS[2], ARGV[1]) then return 0 end
if redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[5]) then return -1 end
redis.call('ZADD', KEYS[2], now_ms, ARGV[1])
redis.call('PEXPIRE', KEYS[2], tonumber(ARGV[4]) * 2)
return 0`, []string{userConcurrencyActiveKey(userID), userConcurrencyWaitingKey(userID)},
			leaseID, limit, config.leaseTTL.Milliseconds(), config.waitTimeout.Milliseconds(), config.waitingLimit).Int()
		if err != nil {
			if c.Request.Context().Err() != nil {
				removeUserConcurrencyWaiter(userID, leaseID)
				return nil, types.NewError(c.Request.Context().Err(), types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
			}
			observation.noteRedisFailOpen("concurrency")
			logger.LogWarn(c, fmt.Sprintf("user concurrency limiter failed open: user=%d request=%s error=%s", userID, requestID, err.Error()))
			return nil, nil
		}
		switch result {
		case 1:
			if queued {
				observation.noteQueueWait(time.Since(start))
			}
			lease := &UserConcurrencyLease{
				userID: userID, leaseID: leaseID, requestID: requestID,
				leaseTTL: config.leaseTTL, renewEvery: config.renewEvery,
				stop: make(chan struct{}), done: make(chan struct{}),
			}
			lease.startRenewal(c, observation)
			return lease, nil
		case -1:
			removeUserConcurrencyWaiter(userID, leaseID)
			return nil, newUserRateLimitError()
		}
		queued = true

		remaining := time.Until(deadline)
		if remaining <= 0 {
			removeUserConcurrencyWaiter(userID, leaseID)
			observation.noteQueueWait(time.Since(start))
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
				removeUserConcurrencyWaiter(userID, leaseID)
				return nil, types.NewError(err, types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
			}
		case <-c.Request.Context().Done():
			if !timer.Stop() {
				<-timer.C
			}
			removeUserConcurrencyWaiter(userID, leaseID)
			return nil, types.NewError(c.Request.Context().Err(), types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
		}
	}
}

func removeUserConcurrencyWaiter(userID int, leaseID string) {
	if !common.RedisEnabled || common.RDB == nil || userID <= 0 || leaseID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := common.RDB.ZRem(ctx, userConcurrencyWaitingKey(userID), leaseID).Err(); err != nil {
		common.SysError(fmt.Sprintf("failed to remove user concurrency waiter: user=%d error=%s", userID, err.Error()))
	}
}

type streamRateWaiter interface {
	WaitN(context.Context, int) error
}

type UserStreamPacer struct {
	limit       int
	modelName   string
	waiter      streamRateWaiter
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
		waiter:      rate.NewLimiter(rate.Limit(limit), limit),
		observation: observation,
	}
}

func newUserStreamPacerWithWaiter(limit int, modelName string, observation *UserRateLimitObservation, waiter streamRateWaiter) *UserStreamPacer {
	return &UserStreamPacer{limit: limit, modelName: modelName, observation: observation, waiter: waiter}
}

func (p *UserStreamPacer) PacePayload(ctx context.Context, payload []byte) error {
	if p == nil || p.limit <= 0 || p.waiter == nil {
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
		if err := p.waiter.WaitN(ctx, chunk); err != nil {
			return err
		}
		remaining -= chunk
	}
	p.observation.notePacing(tokens, time.Since(started))
	return nil
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
