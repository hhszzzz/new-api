package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/i18n"
	hosttypes "github.com/QuantumNous/new-api/types"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	channelRateLimitObservationKey = "channel_rate_limit_observation"

	channelConcurrencyLeaseTTL    = 60 * time.Second
	channelConcurrencyRenewEvery  = 20 * time.Second
	channelAdmissionWaitTTL       = 5 * time.Minute
	channelAdmissionMetricsTTL    = 7 * 24 * time.Hour
	channelAdmissionInitialPoll   = 100 * time.Millisecond
	channelAdmissionMaximumPoll   = time.Second
	channelAdmissionMaximumPrunes = 64
)

type channelRateLimitRedisSource struct {
	client  *redis.Client
	enabled bool
}

var channelRateLimitRedisOverride atomic.Pointer[channelRateLimitRedisSource]

func getChannelRateLimitRedisClient() (*redis.Client, bool) {
	if override := channelRateLimitRedisOverride.Load(); override != nil {
		return override.client, override.enabled && override.client != nil
	}
	client := common.RDB
	return client, common.RedisEnabled && client != nil
}

type channelRateLimitSnapshot struct {
	ChannelID            int
	UserId               int
	RPMLimit             int
	ConcurrencyLimit     int
	UserRPMLimit         int
	UserConcurrencyLimit int
	Priority             int
}

type channelRateLimitRejection struct {
	ChannelID int
	UserId    int
	Layer     string
	Pool      string
}

type ChannelRateLimitObservation struct {
	mu sync.Mutex

	channels      map[int]channelRateLimitSnapshot
	skipped       []channelRateLimitRejection
	leaseAcquired int
	leaseRenewed  int
	leaseReleased int
	queueWait     time.Duration
	queueWaited   int
	soupGrants    int
	redisFailOpen map[string]bool
}

func (o *ChannelRateLimitObservation) noteQueueWait(wait time.Duration) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.queueWait += wait
	o.queueWaited++
	o.mu.Unlock()
}

func (o *ChannelRateLimitObservation) noteSoupGrant() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.soupGrants++
	o.mu.Unlock()
}

func channelRateLimitObservation(c *gin.Context) *ChannelRateLimitObservation {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(channelRateLimitObservationKey); ok {
		if observation, valid := value.(*ChannelRateLimitObservation); valid && observation != nil {
			return observation
		}
	}
	observation := &ChannelRateLimitObservation{}
	c.Set(channelRateLimitObservationKey, observation)
	return observation
}

func (o *ChannelRateLimitObservation) noteSnapshot(snapshot channelRateLimitSnapshot) {
	if o == nil || snapshot.ChannelID <= 0 {
		return
	}
	o.mu.Lock()
	if o.channels == nil {
		o.channels = make(map[int]channelRateLimitSnapshot)
	}
	o.channels[snapshot.ChannelID] = snapshot
	o.mu.Unlock()
}

func (o *ChannelRateLimitObservation) noteRejected(channelID, userId int, layer, pool string) {
	if o == nil || channelID <= 0 || layer == "" {
		return
	}
	o.mu.Lock()
	o.skipped = append(o.skipped, channelRateLimitRejection{ChannelID: channelID, UserId: userId, Layer: layer, Pool: pool})
	o.mu.Unlock()
}

func (o *ChannelRateLimitObservation) noteLeaseAcquired() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.leaseAcquired++
	o.mu.Unlock()
}

func (o *ChannelRateLimitObservation) noteLeaseRenewed() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.leaseRenewed++
	o.mu.Unlock()
}

func (o *ChannelRateLimitObservation) noteLeaseReleased() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.leaseReleased++
	o.mu.Unlock()
}

func (o *ChannelRateLimitObservation) rejectionLayer() string {
	if o == nil {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	hasRPM := false
	hasConcurrency := false
	for _, rejection := range o.skipped {
		switch rejection.Layer {
		case "rpm":
			hasRPM = true
		case "concurrency":
			hasConcurrency = true
		}
	}
	switch {
	case hasRPM && hasConcurrency:
		return "mixed"
	case hasRPM:
		return "rpm"
	case hasConcurrency:
		return "concurrency"
	default:
		return ""
	}
}

func ChannelRateLimitWasConcurrencyBlocked(c *gin.Context, channelID int) bool {
	if c == nil || channelID <= 0 {
		return false
	}
	value, ok := c.Get(channelRateLimitObservationKey)
	if !ok {
		return false
	}
	observation, ok := value.(*ChannelRateLimitObservation)
	if !ok || observation == nil {
		return false
	}
	observation.mu.Lock()
	defer observation.mu.Unlock()
	for _, rejection := range observation.skipped {
		if rejection.ChannelID == channelID && rejection.Layer == "concurrency" {
			return true
		}
	}
	return false
}

func (o *ChannelRateLimitObservation) noteRedisFailOpen(component string) {
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

func AppendChannelRateLimitAdminInfo(c *gin.Context, other *model.LogOther) {
	if c == nil || other == nil {
		return
	}
	value, ok := c.Get(channelRateLimitObservationKey)
	if !ok {
		return
	}
	observation, ok := value.(*ChannelRateLimitObservation)
	if !ok || observation == nil {
		return
	}

	observation.mu.Lock()
	defer observation.mu.Unlock()
	if len(observation.channels) == 0 && len(observation.skipped) == 0 && len(observation.redisFailOpen) == 0 && observation.queueWaited == 0 && observation.soupGrants == 0 {
		return
	}

	info := make(map[string]any)
	if len(observation.channels) > 0 {
		ids := make([]int, 0, len(observation.channels))
		for id := range observation.channels {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		channels := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			snapshot := observation.channels[id]
			entry := map[string]any{"channel_id": snapshot.ChannelID}
			if snapshot.UserId > 0 {
				entry["user_id"] = snapshot.UserId
			}
			if snapshot.RPMLimit > 0 {
				entry["rpm_limit"] = snapshot.RPMLimit
			}
			if snapshot.ConcurrencyLimit > 0 {
				entry["concurrency_limit"] = snapshot.ConcurrencyLimit
			}
			if snapshot.UserRPMLimit > 0 {
				entry["user_rpm_limit"] = snapshot.UserRPMLimit
			}
			if snapshot.UserConcurrencyLimit > 0 {
				entry["user_concurrency_limit"] = snapshot.UserConcurrencyLimit
			}
			if snapshot.Priority != 0 {
				entry["priority"] = snapshot.Priority
			}
			channels = append(channels, entry)
		}
		info["channels"] = channels
	}
	if len(observation.skipped) > 0 {
		skipped := make([]map[string]any, 0, len(observation.skipped))
		for _, rejection := range observation.skipped {
			entry := map[string]any{
				"channel_id": rejection.ChannelID,
				"layer":      rejection.Layer,
			}
			if rejection.UserId > 0 {
				entry["user_id"] = rejection.UserId
			}
			if rejection.Pool != "" {
				entry["pool"] = rejection.Pool
			}
			skipped = append(skipped, entry)
		}
		info["skipped_candidates"] = skipped
		info["fallback_count"] = len(skipped)
	}
	if observation.leaseAcquired > 0 || observation.leaseRenewed > 0 || observation.leaseReleased > 0 {
		info["lease_lifecycle"] = map[string]int{
			"acquired": observation.leaseAcquired,
			"renewed":  observation.leaseRenewed,
			"released": observation.leaseReleased,
		}
	}
	if len(observation.redisFailOpen) > 0 {
		failOpen := make(map[string]bool, len(observation.redisFailOpen))
		for component, enabled := range observation.redisFailOpen {
			failOpen[component] = enabled
		}
		info["redis_fail_open"] = failOpen
	}
	if observation.queueWaited > 0 || observation.soupGrants > 0 {
		admission := map[string]any{
			"queued_requests": observation.queueWaited,
			"average_wait_ms": observation.queueWait.Milliseconds() / max(1, int64(observation.queueWaited)),
			"soup_grants":     observation.soupGrants,
		}
		for _, rejection := range observation.skipped {
			if rejection.Layer != "concurrency" {
				continue
			}
			switch rejection.Pool {
			case "queue_timeout":
				admission["timeouts"] = 1
			case "queue_full":
				admission["queue_full"] = 1
			}
		}
		info["admission"] = admission
	}

	other.SetAdmin("channel_rate_limits", info)
}

type ChannelRateLimitGuard struct {
	channelID   int
	userId      int
	leaseID     string
	requestID   string
	leaseTTL    time.Duration
	renewEvery  time.Duration
	totalLease  bool
	userLease   bool
	observation *ChannelRateLimitObservation
	stop        chan struct{}
	done        chan struct{}
	releaseOnce sync.Once
}

type ChannelRateLimitOptions struct {
	WaitTimeout    time.Duration
	Heartbeat      func() error
	RefreshChannel func() (*model.Channel, error)
	NoWait         bool
}

func channelAdmissionConfig(channel *model.Channel) dto.ChannelAdmissionControl {
	settings := dto.ChannelOtherSettings{}
	if channel != nil && channel.OtherSettings != "" {
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
			return (dto.ChannelAdmissionControl{}).Normalized()
		}
	}
	if settings.AdmissionControl == nil {
		return (dto.ChannelAdmissionControl{}).Normalized()
	}
	return settings.AdmissionControl.Normalized()
}

func TryAcquireChannelRateLimit(c *gin.Context, channel *model.Channel) (*ChannelRateLimitGuard, bool) {
	return AcquireChannelRateLimit(c, channel, ChannelRateLimitOptions{NoWait: true})
}

func AcquireChannelRateLimit(c *gin.Context, channel *model.Channel, options ChannelRateLimitOptions) (*ChannelRateLimitGuard, bool) {
	if c == nil || channel == nil || channel.Id <= 0 {
		return nil, true
	}
	rpmLimit := configuredLimit(channel.RpmLimit)
	concurrencyLimit := configuredLimit(channel.ConcurrencyLimit)
	userId := c.GetInt("id")
	userRPMLimit := 0
	userConcurrencyLimit := 0
	priority := 0
	if override, ok := channel.UserRateLimits.ForUser(userId); ok {
		userRPMLimit = configuredLimit(override.RPMLimit)
		userConcurrencyLimit = configuredLimit(override.ConcurrencyLimit)
		priority = override.Priority
	}
	if rpmLimit <= 0 && concurrencyLimit <= 0 && userRPMLimit <= 0 && userConcurrencyLimit <= 0 {
		return nil, true
	}

	observation := channelRateLimitObservation(c)
	snapshot := channelRateLimitSnapshot{
		ChannelID:            channel.Id,
		UserId:               userId,
		RPMLimit:             rpmLimit,
		ConcurrencyLimit:     concurrencyLimit,
		UserRPMLimit:         userRPMLimit,
		UserConcurrencyLimit: userConcurrencyLimit,
		Priority:             priority,
	}
	observation.noteSnapshot(snapshot)

	client, enabled := getChannelRateLimitRedisClient()
	if !enabled {
		observation.noteRedisFailOpen("acquire")
		logger.LogWarn(c, fmt.Sprintf("channel rate limiter failed open: channel=%d request=%s redis disabled", channel.Id, c.GetString(common.RequestIdKey)))
		return nil, true
	}

	policy := channelAdmissionConfig(channel)
	waitTimeout := time.Duration(policy.WaitTimeoutMs) * time.Millisecond
	if options.WaitTimeout > 0 && options.WaitTimeout < waitTimeout {
		waitTimeout = options.WaitTimeout
	}
	if options.NoWait {
		waitTimeout = 0
	}
	leaseID := common.NewRequestId()
	start := time.Now()
	deadline := start.Add(waitTimeout)
	poll := channelAdmissionInitialPoll
	queued := false
	queueMember := ""
	refreshWarned := false
	var heartbeat <-chan time.Time
	var heartbeatTicker *time.Ticker
	if options.Heartbeat != nil && waitTimeout > 0 {
		heartbeatTicker = time.NewTicker(time.Duration(policy.HeartbeatIntervalMs) * time.Millisecond)
		defer heartbeatTicker.Stop()
		heartbeat = heartbeatTicker.C
	}
	for {
		if queued && !time.Now().Before(deadline) {
			removeChannelAdmissionWaiter(client, channel, userId, leaseID, queueMember, "timeout")
			setChannelAdmissionRetryAfter(c, policy)
			observation.noteQueueWait(time.Since(start))
			observation.noteRejected(channel.Id, userId, "concurrency", "queue_timeout")
			return nil, false
		}
		currentChannel := channel
		if queued && options.RefreshChannel != nil {
			refreshedChannel, refreshErr := options.RefreshChannel()
			if refreshErr == nil && refreshedChannel != nil {
				currentChannel = refreshedChannel
			} else if !refreshWarned {
				errText := "channel no longer exists"
				if refreshErr != nil {
					errText = refreshErr.Error()
				}
				logger.LogWarn(c, fmt.Sprintf("channel concurrency refresh failed while queued; using selected snapshot: channel=%d request=%s error=%s", channel.Id, c.GetString(common.RequestIdKey), errText))
				refreshWarned = true
			}
		}
		currentConcurrencyLimit := configuredLimit(currentChannel.ConcurrencyLimit)
		currentUserConcurrencyLimit := 0
		if override, ok := currentChannel.UserRateLimits.ForUser(userId); ok {
			currentUserConcurrencyLimit = configuredLimit(override.ConcurrencyLimit)
		}
		userConcurrencyLimits := make(map[int]int, len(currentChannel.UserRateLimits))
		for _, override := range currentChannel.UserRateLimits {
			userConcurrencyLimits[override.UserId] = configuredLimit(override.ConcurrencyLimit)
		}
		userConcurrencyLimitsJSON, marshalErr := common.Marshal(userConcurrencyLimits)
		if marshalErr != nil {
			common.SysError(fmt.Sprintf("failed to encode channel user concurrency limits: channel=%d error=%v", channel.Id, marshalErr))
			return nil, true
		}
		ctx := c.Request.Context()
		resultValue, err := client.Eval(ctx, channelAdmissionAcquireScript, channelRateLimitKeys(channel.Id, userId),
			leaseID, rpmLimit, currentConcurrencyLimit, userRPMLimit, currentUserConcurrencyLimit,
			channelConcurrencyLeaseTTL.Milliseconds(), priority, userId,
			policy.MaxWaitingTotal, policy.MaxWaitingPerUser, channelAdmissionWaitTTL.Milliseconds(),
			policy.SoupMode, *policy.SoupAfterMs, policy.SoupIntervalMs, policy.SoupRatioPercent,
			boolInt(options.NoWait), fmt.Sprintf("channel_rate_limit:{channel:%d}", channel.Id), channelAdmissionMetricsTTL.Milliseconds(), channelAdmissionMaximumPrunes, string(userConcurrencyLimitsJSON)).Result()
		result, validResult := resultValue.([]any)
		if err != nil || !validResult || len(result) != 6 {
			if ctx.Err() != nil {
				if queued {
					removeChannelAdmissionWaiter(client, channel, userId, leaseID, queueMember, "cancelled")
				}
				return nil, false
			}
			observation.noteRedisFailOpen("acquire")
			if queued {
				removeChannelAdmissionWaiter(client, channel, userId, leaseID, queueMember, "redis_error")
			}
			errText := "invalid Redis response"
			if err != nil {
				errText = err.Error()
			}
			logger.LogWarn(c, fmt.Sprintf("channel rate limiter failed open: channel=%d request=%s error=%s", channel.Id, c.GetString(common.RequestIdKey), errText))
			return nil, true
		}
		queueMember = channelAdmissionResultString(result, 5)
		switch channelAdmissionResultInt(result, 0) {
		case 1:
			if channelAdmissionResultInt(result, 2) == 1 {
				observation.noteRejected(channel.Id, userId, "rpm", "total")
			}
			if channelAdmissionResultInt(result, 3) == 1 {
				observation.noteRejected(channel.Id, userId, "rpm", "user")
			}
			guard := &ChannelRateLimitGuard{
				channelID:   channel.Id,
				userId:      userId,
				requestID:   c.GetString(common.RequestIdKey),
				leaseTTL:    channelConcurrencyLeaseTTL,
				renewEvery:  channelConcurrencyRenewEvery,
				totalLease:  currentConcurrencyLimit > 0 || currentUserConcurrencyLimit > 0,
				userLease:   currentUserConcurrencyLimit > 0,
				observation: observation,
			}
			if guard.totalLease {
				guard.leaseID = leaseID
				guard.stop = make(chan struct{})
				guard.done = make(chan struct{})
				observation.noteLeaseAcquired()
				guard.startRenewal(c)
			}
			if queued {
				observation.noteQueueWait(time.Since(start))
			}
			if channelAdmissionResultInt(result, 1) == 2 {
				observation.noteSoupGrant()
			}
			return guard, true
		case -1:
			if channelAdmissionResultInt(result, 2) == 1 {
				observation.noteRejected(channel.Id, userId, "rpm", "total")
			}
			if channelAdmissionResultInt(result, 3) == 1 {
				observation.noteRejected(channel.Id, userId, "rpm", "user")
			}
			if queued {
				removeChannelAdmissionWaiter(client, channel, userId, leaseID, queueMember, "rpm")
			}
			return nil, false
		case -2:
			setChannelAdmissionRetryAfter(c, policy)
			observation.noteRejected(channel.Id, userId, "concurrency", "queue_full")
			return nil, false
		case 0:
			if options.NoWait {
				observation.noteRejected(channel.Id, userId, "concurrency", "total")
				return nil, false
			}
			queued = true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			removeChannelAdmissionWaiter(client, channel, userId, leaseID, queueMember, "timeout")
			setChannelAdmissionRetryAfter(c, policy)
			observation.noteQueueWait(time.Since(start))
			observation.noteRejected(channel.Id, userId, "concurrency", "queue_timeout")
			return nil, false
		}
		wait := min(poll, remaining)
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			poll = min(poll*2, channelAdmissionMaximumPoll)
		case <-heartbeat:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if err := options.Heartbeat(); err != nil {
				removeChannelAdmissionWaiter(client, channel, userId, leaseID, queueMember, "cancelled")
				return nil, false
			}
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			removeChannelAdmissionWaiter(client, channel, userId, leaseID, queueMember, "cancelled")
			return nil, false
		}
	}
}

func setChannelAdmissionRetryAfter(c *gin.Context, policy dto.ChannelAdmissionControl) {
	if c != nil {
		c.Header("Retry-After", strconv.Itoa(policy.RetryAfterSeconds))
	}
}

func channelAdmissionResultInt(result []any, index int) int64 {
	if index >= len(result) {
		return 0
	}
	switch value := result[index].(type) {
	case int64:
		return value
	case string:
		parsed, _ := strconv.ParseInt(value, 10, 64)
		return parsed
	case []byte:
		parsed, _ := strconv.ParseInt(string(value), 10, 64)
		return parsed
	default:
		return 0
	}
}

func channelAdmissionResultString(result []any, index int) string {
	if index >= len(result) || result[index] == nil {
		return ""
	}
	switch value := result[index].(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}

func removeChannelAdmissionWaiter(client *redis.Client, channel *model.Channel, userID int, leaseID, member, reason string) {
	if client == nil || channel == nil || leaseID == "" || member == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = client.Eval(ctx, channelAdmissionRemoveWaiterScript, channelRateLimitKeys(channel.Id, userID), leaseID, member, reason, channelAdmissionMetricsTTL.Milliseconds()).Err()
}

const channelAdmissionAcquireScript = `
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
local minute = math.floor(now_ms / 60000)
local lease_id = ARGV[1]
local rpm_limit = tonumber(ARGV[2])
local channel_limit = tonumber(ARGV[3])
local user_rpm_limit = tonumber(ARGV[4])
local user_limit = tonumber(ARGV[5])
local lease_ttl = tonumber(ARGV[6])
local priority = tonumber(ARGV[7])
local user_id = ARGV[8]
local max_waiting = tonumber(ARGV[9])
local max_waiting_user = tonumber(ARGV[10])
local wait_ttl = tonumber(ARGV[11])
local soup_mode = ARGV[12]
local soup_after = tonumber(ARGV[13])
local soup_interval = tonumber(ARGV[14])
local soup_ratio = tonumber(ARGV[15])
local no_wait = tonumber(ARGV[16]) == 1
local prefix = ARGV[17]
local user_concurrency_limits = cjson.decode(ARGV[20])
local active_user_key = prefix .. ':active:user:' .. user_id
local waiting_user_key = prefix .. ':waiting:user:' .. user_id
local function metric(name, amount)
  local member = tostring(minute) .. '|' .. name
  redis.call('HINCRBY', KEYS[9], member, amount or 1)
  redis.call('ZADD', KEYS[10], now_ms, member)
  redis.call('PEXPIRE', KEYS[9], tonumber(ARGV[18]))
  redis.call('PEXPIRE', KEYS[10], tonumber(ARGV[18]))
end
local function wait_bucket(elapsed)
  if elapsed <= 100 then return 'wait_le_100ms' end
  if elapsed <= 500 then return 'wait_le_500ms' end
  if elapsed <= 1000 then return 'wait_le_1s' end
  if elapsed <= 5000 then return 'wait_le_5s' end
  if elapsed <= 15000 then return 'wait_le_15s' end
  if elapsed <= 30000 then return 'wait_le_30s' end
  if elapsed <= 60000 then return 'wait_le_60s' end
  if elapsed <= 120000 then return 'wait_le_120s' end
  if elapsed <= 300000 then return 'wait_le_300s' end
  return 'wait_gt_300s'
end
local expired = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now_ms - lease_ttl, 'LIMIT', 0, tonumber(ARGV[19]))
for _, id in ipairs(expired) do
  local meta = redis.call('HGET', KEYS[8], id)
  if meta then
    local uid = string.match(meta, '^([^|]+)')
    if uid then redis.call('ZREM', prefix .. ':active:user:' .. uid, id) end
    redis.call('HDEL', KEYS[8], id)
  end
  redis.call('ZREM', KEYS[2], id)
end
redis.call('ZREMRANGEBYSCORE', active_user_key, '-inf', now_ms - lease_ttl)
local existing_active = redis.call('ZSCORE', KEYS[2], lease_id)
if existing_active then return {1, 0, 0, 0, 0, 0} end
local total_rpm_blocked = false
if rpm_limit > 0 and tonumber(redis.call('HGET', KEYS[1], 'minute')) == minute then
  total_rpm_blocked = (tonumber(redis.call('HGET', KEYS[1], 'count')) or 0) >= rpm_limit
end
local user_rpm_blocked = false
if user_rpm_limit > 0 and tonumber(redis.call('HGET', KEYS[3], 'minute')) == minute then
  user_rpm_blocked = (tonumber(redis.call('HGET', KEYS[3], 'count')) or 0) >= user_rpm_limit
end
if total_rpm_blocked or user_rpm_blocked then return {-1, 0, total_rpm_blocked and 1 or 0, user_rpm_blocked and 1 or 0, 0, 0} end
local stale_members = redis.call('ZRANGE', KEYS[5], 0, -1)
for _, member in ipairs(stale_members) do
  local queued_id = string.match(member, '^%d+|%d+|(.+)$')
  local meta = queued_id and redis.call('HGET', KEYS[6], queued_id) or nil
  local enqueued = tonumber(string.match(member, '^(%d+)|'))
  if not meta or not enqueued or now_ms - enqueued >= wait_ttl then
    local uid = meta and tonumber(string.match(meta, '^([^|]+)')) or nil
    redis.call('ZREM', KEYS[5], member)
    if uid then redis.call('ZREM', prefix .. ':waiting:user:' .. uid, member) end
    if queued_id then redis.call('HDEL', KEYS[6], queued_id) end
  end
end
local stored_wait_meta = redis.call('HGET', KEYS[6], lease_id)
local waiting_member = nil
local was_waiting = stored_wait_meta ~= nil
if stored_wait_meta then
  local enqueued, sequence = string.match(stored_wait_meta, '([^|]+)|([^|]+)$')
  if enqueued and sequence then waiting_member = string.format('%013d|%020d|%s', tonumber(enqueued), tonumber(sequence), lease_id) end
end
local total_active = tonumber(redis.call('ZCARD', KEYS[2]))
local total_blocked = channel_limit > 0 and total_active >= channel_limit
local own_active = tonumber(redis.call('ZCARD', active_user_key))
local user_blocked = user_limit > 0 and own_active >= user_limit
local waiting_count = tonumber(redis.call('ZCARD', KEYS[5]))
if not waiting_member then
  local needs_wait = total_blocked or user_blocked or (channel_limit > 0 and waiting_count > 0)
  if no_wait and needs_wait then return {0, 0, 0, 0, 0, 0} end
  if needs_wait then
    local user_wait_count = tonumber(redis.call('ZCARD', waiting_user_key))
    if waiting_count >= max_waiting or user_wait_count >= max_waiting_user then
      metric('queue_full')
      return {-2, 0, 0, 0, 0, 0}
    end
  end
  local sequence = redis.call('INCR', KEYS[12])
  waiting_member = string.format('%013d|%020d|%s', now_ms, sequence, lease_id)
  redis.call('ZADD', KEYS[5], -priority, waiting_member)
  redis.call('ZADD', waiting_user_key, now_ms, waiting_member)
	  redis.call('HSET', KEYS[6], lease_id, user_id .. '|' .. priority .. '|' .. now_ms .. '|' .. sequence)
  redis.call('PEXPIRE', KEYS[5], wait_ttl * 2)
  redis.call('PEXPIRE', waiting_user_key, wait_ttl * 2)
  redis.call('PEXPIRE', KEYS[6], wait_ttl * 2)
  redis.call('PEXPIRE', KEYS[12], wait_ttl * 2)
  waiting_count = waiting_count + 1
end
local queue = redis.call('ZRANGE', KEYS[5], 0, -1, 'WITHSCORES')
local eligible = {}
local index = 1
while index <= #queue do
  local member = queue[index]
  local queued_id = string.match(member, '^%d+|%d+|(.+)$')
  local meta = queued_id and redis.call('HGET', KEYS[6], queued_id) or nil
  if not meta then
    redis.call('ZREM', KEYS[5], member)
    if queued_id then redis.call('HDEL', KEYS[6], queued_id) end
  else
	    local uid, queued_priority, enqueued, sequence = string.match(meta, '^([^|]+)|([^|]+)|([^|]+)|([^|]+)$')
    uid = tonumber(uid)
    queued_priority = tonumber(queued_priority)
    enqueued = tonumber(enqueued)
    sequence = tonumber(sequence)
	    if not uid or not queued_priority or not enqueued or not sequence or now_ms - enqueued >= wait_ttl then
      redis.call('ZREM', KEYS[5], member)
      if uid then redis.call('ZREM', prefix .. ':waiting:user:' .. uid, member) end
      if queued_id then redis.call('HDEL', KEYS[6], queued_id) end
    else
      local user_active_key = prefix .. ':active:user:' .. uid
      redis.call('ZREMRANGEBYSCORE', user_active_key, '-inf', now_ms - lease_ttl)
      local user_active = tonumber(redis.call('ZCARD', user_active_key))
	      local queued_limit = tonumber(user_concurrency_limits[tostring(uid)]) or 0
      local total_ok = channel_limit <= 0 or total_active < channel_limit
      local user_ok = queued_limit <= 0 or user_active < queued_limit
      if total_ok and user_ok then
        table.insert(eligible, {member=member, id=queued_id, uid=uid, priority=queued_priority, enqueued=enqueued})
      end
    end
  end
  index = index + 2
end
local winner = eligible[1]
local soup = false
if winner and soup_mode ~= 'off' then
  local low = nil
  for i = 2, #eligible do
    if eligible[i].priority < winner.priority then
      low = eligible[i]
      break
    end
  end
  if low and now_ms - low.enqueued >= soup_after then
local soup_counts = redis.call('HMGET', KEYS[11], 'last_soup', 'grants', 'soup_grants')
    local last_soup = tonumber(soup_counts[1]) or 0
    local grants = tonumber(soup_counts[2]) or 0
    local soup_grants = tonumber(soup_counts[3]) or 0
    if soup_mode == 'interval' and (last_soup == 0 or now_ms - last_soup >= soup_interval) then
      winner = low
      soup = true
	elseif soup_mode == 'ratio' and (soup_grants + 1) * 100 <= (grants + soup_grants + 1) * soup_ratio then
      winner = low
      soup = true
    end
  end
end
if not winner or winner.id ~= lease_id then
  if no_wait and waiting_member then
    redis.call('ZREM', KEYS[5], waiting_member)
    redis.call('ZREM', waiting_user_key, waiting_member)
    redis.call('HDEL', KEYS[6], lease_id)
  end
  return {0, 0, 0, 0, waiting_count, waiting_member or ''}
end
local enqueued = tonumber(string.match(redis.call('HGET', KEYS[6], lease_id), '([^|]+)$')) or now_ms
local elapsed = math.max(0, now_ms - enqueued)
if rpm_limit > 0 then
  local count = tonumber(redis.call('HGET', KEYS[1], 'count')) or 0
  if tonumber(redis.call('HGET', KEYS[1], 'minute')) ~= minute then count = 0 end
  redis.call('HSET', KEYS[1], 'minute', minute, 'count', count + 1)
  redis.call('PEXPIRE', KEYS[1], 120000)
end
if user_rpm_limit > 0 then
  local count = tonumber(redis.call('HGET', KEYS[3], 'count')) or 0
  if tonumber(redis.call('HGET', KEYS[3], 'minute')) ~= minute then count = 0 end
  redis.call('HSET', KEYS[3], 'minute', minute, 'count', count + 1)
  redis.call('PEXPIRE', KEYS[3], 120000)
end
if channel_limit > 0 or user_limit > 0 then
  redis.call('ZADD', KEYS[2], now_ms, lease_id)
  redis.call('HSET', KEYS[8], lease_id, user_id .. '|' .. priority)
  redis.call('PEXPIRE', KEYS[2], lease_ttl * 2)
  redis.call('PEXPIRE', KEYS[8], lease_ttl * 2)
end
if user_limit > 0 then
  redis.call('ZADD', active_user_key, now_ms, lease_id)
  redis.call('PEXPIRE', active_user_key, lease_ttl * 2)
end
if waiting_member then
  redis.call('ZREM', KEYS[5], waiting_member)
  redis.call('ZREM', waiting_user_key, waiting_member)
  redis.call('HDEL', KEYS[6], lease_id)
  redis.call('HINCRBY', KEYS[11], 'grants', soup and 0 or 1)
  redis.call('HINCRBY', KEYS[11], 'soup_grants', soup and 1 or 0)
  if soup then redis.call('HSET', KEYS[11], 'last_soup', now_ms) end
  redis.call('PEXPIRE', KEYS[11], tonumber(ARGV[18]))
  if was_waiting then metric(wait_bucket(elapsed)) end
end
metric('grants')
if soup then metric('soup_grants') end
return {1, soup and 2 or 0, 0, 0, waiting_count, waiting_member or ''}
`

const channelAdmissionRemoveWaiterScript = `
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
local member = ARGV[2]
if member ~= '' then
  redis.call('ZREM', KEYS[5], member)
  redis.call('ZREM', KEYS[7], member)
  redis.call('HDEL', KEYS[6], ARGV[1])
end
local minute = math.floor(now_ms / 60000)
if ARGV[3] == 'timeout' then
  local metric_member = tostring(minute) .. '|timeout'
  redis.call('HINCRBY', KEYS[9], metric_member, 1)
  redis.call('ZADD', KEYS[10], now_ms, metric_member)
  local enqueued = tonumber(string.match(member, '^(%d+)|'))
  if enqueued then
    local elapsed = math.max(0, now_ms - enqueued)
    local wait_metric = 'wait_gt_300s'
    if elapsed <= 100 then wait_metric = 'wait_le_100ms'
    elseif elapsed <= 500 then wait_metric = 'wait_le_500ms'
    elseif elapsed <= 1000 then wait_metric = 'wait_le_1s'
    elseif elapsed <= 5000 then wait_metric = 'wait_le_5s'
    elseif elapsed <= 15000 then wait_metric = 'wait_le_15s'
    elseif elapsed <= 30000 then wait_metric = 'wait_le_30s'
    elseif elapsed <= 60000 then wait_metric = 'wait_le_60s'
    elseif elapsed <= 120000 then wait_metric = 'wait_le_120s'
    elseif elapsed <= 300000 then wait_metric = 'wait_le_300s' end
    local wait_member = tostring(minute) .. '|' .. wait_metric
    redis.call('HINCRBY', KEYS[9], wait_member, 1)
    redis.call('ZADD', KEYS[10], now_ms, wait_member)
  end
  redis.call('PEXPIRE', KEYS[9], tonumber(ARGV[4]))
  redis.call('PEXPIRE', KEYS[10], tonumber(ARGV[4]))
end
return 1
`

func (g *ChannelRateLimitGuard) Release() {
	if g == nil || g.leaseID == "" {
		return
	}
	g.releaseOnce.Do(func() {
		close(g.stop)
		<-g.done
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, enabled := getChannelRateLimitRedisClient()
		if !enabled {
			g.observation.noteRedisFailOpen("release")
			return
		}
		if err := client.Eval(ctx, `
if tonumber(ARGV[2]) == 1 then redis.call('ZREM', KEYS[1], ARGV[1]) end
if tonumber(ARGV[3]) == 1 then redis.call('ZREM', KEYS[2], ARGV[1]) end
redis.call('HDEL', KEYS[3], ARGV[1])
return 1`, channelRateLimitActiveKeys(g.channelID, g.userId), g.leaseID, boolInt(g.totalLease), boolInt(g.userLease)).Err(); err != nil {
			g.observation.noteRedisFailOpen("release")
			common.SysError(fmt.Sprintf("failed to release channel concurrency lease: channel=%d user=%d request=%s error=%s", g.channelID, g.userId, g.requestID, err.Error()))
			return
		}
		g.observation.noteLeaseReleased()
	})
}

func (g *ChannelRateLimitGuard) startRenewal(c *gin.Context) {
	go func() {
		defer close(g.done)
		ticker := time.NewTicker(g.renewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				client, enabled := getChannelRateLimitRedisClient()
				if !enabled {
					g.observation.noteRedisFailOpen("renew")
					logger.LogWarn(c, fmt.Sprintf("channel concurrency renewal failed open: channel=%d request=%s redis disabled", g.channelID, g.requestID))
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				result, err := client.Eval(ctx, `
if tonumber(ARGV[3]) == 1 and not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 0 end
if tonumber(ARGV[4]) == 1 and not redis.call('ZSCORE', KEYS[2], ARGV[1]) then return 0 end
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
if tonumber(ARGV[3]) == 1 then
  redis.call('ZADD', KEYS[1], now_ms, ARGV[1])
  redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]) * 2)
end
if tonumber(ARGV[4]) == 1 then
  redis.call('ZADD', KEYS[2], now_ms, ARGV[1])
  redis.call('PEXPIRE', KEYS[2], tonumber(ARGV[2]) * 2)
end
return 1`, channelRateLimitActiveKeys(g.channelID, g.userId), g.leaseID, g.leaseTTL.Milliseconds(), boolInt(g.totalLease), boolInt(g.userLease)).Int()
				cancel()
				if err != nil || result != 1 {
					g.observation.noteRedisFailOpen("renew")
					errText := "lease missing"
					if err != nil {
						errText = err.Error()
					}
					logger.LogWarn(c, fmt.Sprintf("channel concurrency renewal failed open: channel=%d request=%s error=%s", g.channelID, g.requestID, errText))
					continue
				}
				g.observation.noteLeaseRenewed()
			case <-g.stop:
				return
			}
		}
	}()
}

func NewChannelRateLimitError(c *gin.Context) *hosttypes.NewAPIError {
	key := i18n.MsgChannelUpstreamSaturated
	if c != nil {
		if value, ok := c.Get(channelRateLimitObservationKey); ok {
			if observation, valid := value.(*ChannelRateLimitObservation); valid && observation != nil {
				switch observation.rejectionLayer() {
				case "rpm":
					key = i18n.MsgRateLimitRPMExceeded
				case "concurrency":
					key = i18n.MsgRateLimitConcurrencyExceeded
				}
			}
		}
	}
	return hosttypes.NewErrorWithStatusCode(
		errors.New(i18n.T(c, key)),
		hosttypes.ErrorCode("rate_limit_exceeded"),
		http.StatusTooManyRequests,
		hosttypes.ErrOptionWithSkipRetry(),
	)
}

func channelRateLimitKeys(channelID, userId int) []string {
	tag := fmt.Sprintf("{channel:%d}", channelID)
	return []string{
		"channel_rate_limit:" + tag + ":rpm",
		"channel_rate_limit:" + tag + ":active",
		fmt.Sprintf("channel_rate_limit:%s:rpm:user:%d", tag, userId),
		fmt.Sprintf("channel_rate_limit:%s:active:user:%d", tag, userId),
		"channel_rate_limit:" + tag + ":waiting",
		"channel_rate_limit:" + tag + ":waiting:meta",
		fmt.Sprintf("channel_rate_limit:%s:waiting:user:%d", tag, userId),
		"channel_rate_limit:" + tag + ":lease:meta",
		"channel_rate_limit:" + tag + ":metrics:values",
		"channel_rate_limit:" + tag + ":metrics:index",
		"channel_rate_limit:" + tag + ":soup",
		"channel_rate_limit:" + tag + ":waiting:sequence",
	}
}

func channelRateLimitActiveKeys(channelID, userId int) []string {
	tag := fmt.Sprintf("{channel:%d}", channelID)
	return []string{
		"channel_rate_limit:" + tag + ":active",
		fmt.Sprintf("channel_rate_limit:%s:active:user:%d", tag, userId),
		"channel_rate_limit:" + tag + ":lease:meta",
	}
}

func channelConcurrencyKey(channelID int) string {
	return channelRateLimitActiveKeys(channelID, 0)[0]
}
