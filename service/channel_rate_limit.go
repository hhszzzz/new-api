package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	channelRateLimitObservationKey = "channel_rate_limit_observation"

	channelConcurrencyLeaseTTL   = 60 * time.Second
	channelConcurrencyRenewEvery = 20 * time.Second
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
	ChannelID        int
	RPMLimit         int
	ConcurrencyLimit int
}

type channelRateLimitRejection struct {
	ChannelID int
	Layer     string
}

type ChannelRateLimitObservation struct {
	mu sync.Mutex

	channels      map[int]channelRateLimitSnapshot
	skipped       []channelRateLimitRejection
	leaseAcquired int
	leaseRenewed  int
	leaseReleased int
	redisFailOpen map[string]bool
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

func (o *ChannelRateLimitObservation) noteRejected(channelID int, layer string) {
	if o == nil || channelID <= 0 || layer == "" {
		return
	}
	o.mu.Lock()
	o.skipped = append(o.skipped, channelRateLimitRejection{ChannelID: channelID, Layer: layer})
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

func AppendChannelRateLimitAdminInfo(c *gin.Context, other map[string]interface{}) {
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
	if len(observation.channels) == 0 && len(observation.skipped) == 0 && len(observation.redisFailOpen) == 0 {
		return
	}

	info := make(map[string]interface{})
	if len(observation.channels) > 0 {
		ids := make([]int, 0, len(observation.channels))
		for id := range observation.channels {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		channels := make([]map[string]interface{}, 0, len(ids))
		for _, id := range ids {
			snapshot := observation.channels[id]
			entry := map[string]interface{}{"channel_id": snapshot.ChannelID}
			if snapshot.RPMLimit > 0 {
				entry["rpm_limit"] = snapshot.RPMLimit
			}
			if snapshot.ConcurrencyLimit > 0 {
				entry["concurrency_limit"] = snapshot.ConcurrencyLimit
			}
			channels = append(channels, entry)
		}
		info["channels"] = channels
	}
	if len(observation.skipped) > 0 {
		skipped := make([]map[string]interface{}, 0, len(observation.skipped))
		for _, rejection := range observation.skipped {
			skipped = append(skipped, map[string]interface{}{
				"channel_id": rejection.ChannelID,
				"layer":      rejection.Layer,
			})
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

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo["channel_rate_limits"] = info
}

type ChannelRateLimitGuard struct {
	channelID   int
	leaseID     string
	requestID   string
	leaseTTL    time.Duration
	renewEvery  time.Duration
	observation *ChannelRateLimitObservation
	stop        chan struct{}
	done        chan struct{}
	releaseOnce sync.Once
}

func TryAcquireChannelRateLimit(c *gin.Context, channel *model.Channel) (*ChannelRateLimitGuard, bool) {
	if c == nil || channel == nil || channel.Id <= 0 {
		return nil, true
	}
	rpmLimit := configuredLimit(channel.RpmLimit)
	concurrencyLimit := configuredLimit(channel.ConcurrencyLimit)
	if rpmLimit <= 0 && concurrencyLimit <= 0 {
		return nil, true
	}

	observation := channelRateLimitObservation(c)
	snapshot := channelRateLimitSnapshot{
		ChannelID:        channel.Id,
		RPMLimit:         rpmLimit,
		ConcurrencyLimit: concurrencyLimit,
	}
	observation.noteSnapshot(snapshot)

	client, enabled := getChannelRateLimitRedisClient()
	if !enabled {
		observation.noteRedisFailOpen("acquire")
		logger.LogWarn(c, fmt.Sprintf("channel rate limiter failed open: channel=%d request=%s redis disabled", channel.Id, c.GetString(common.RequestIdKey)))
		return nil, true
	}

	leaseID := common.NewRequestId()
	result, err := client.Eval(c.Request.Context(), `
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
local minute = math.floor(tonumber(now[1]) / 60)
local rpm_limit = tonumber(ARGV[2])
local concurrency_limit = tonumber(ARGV[3])
local lease_ttl = tonumber(ARGV[4])
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now_ms - lease_ttl)
local rpm_count = 0
if rpm_limit > 0 then
  local stored_minute = tonumber(redis.call('HGET', KEYS[1], 'minute'))
  if stored_minute == minute then
    rpm_count = tonumber(redis.call('HGET', KEYS[1], 'count')) or 0
  end
  if rpm_count >= rpm_limit then
    return {0, 1, 0}
  end
end
if concurrency_limit > 0 and redis.call('ZCARD', KEYS[2]) >= concurrency_limit then
  return {0, 0, 1}
end
if rpm_limit > 0 then
  redis.call('HSET', KEYS[1], 'minute', minute, 'count', rpm_count + 1)
  redis.call('PEXPIRE', KEYS[1], 120000)
end
if concurrency_limit > 0 then
  redis.call('ZADD', KEYS[2], now_ms, ARGV[1])
  redis.call('PEXPIRE', KEYS[2], lease_ttl * 2)
end
return {1, 0, 0}`, channelRateLimitKeys(channel.Id), leaseID, rpmLimit, concurrencyLimit, channelConcurrencyLeaseTTL.Milliseconds()).Int64Slice()
	if err != nil || len(result) != 3 {
		if c.Request.Context().Err() != nil {
			return nil, false
		}
		observation.noteRedisFailOpen("acquire")
		errText := "invalid Redis response"
		if err != nil {
			errText = err.Error()
		}
		logger.LogWarn(c, fmt.Sprintf("channel rate limiter failed open: channel=%d request=%s error=%s", channel.Id, c.GetString(common.RequestIdKey), errText))
		return nil, true
	}
	if result[0] != 1 {
		layer := "concurrency"
		if result[1] == 1 {
			layer = "rpm"
		}
		observation.noteRejected(channel.Id, layer)
		return nil, false
	}

	guard := &ChannelRateLimitGuard{
		channelID:   channel.Id,
		requestID:   c.GetString(common.RequestIdKey),
		leaseTTL:    channelConcurrencyLeaseTTL,
		renewEvery:  channelConcurrencyRenewEvery,
		observation: observation,
	}
	if concurrencyLimit > 0 {
		guard.leaseID = leaseID
		guard.stop = make(chan struct{})
		guard.done = make(chan struct{})
		observation.noteLeaseAcquired()
		guard.startRenewal(c)
	}
	return guard, true
}

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
		if err := client.ZRem(ctx, channelConcurrencyKey(g.channelID), g.leaseID).Err(); err != nil {
			g.observation.noteRedisFailOpen("release")
			common.SysError(fmt.Sprintf("failed to release channel concurrency lease: channel=%d request=%s error=%s", g.channelID, g.requestID, err.Error()))
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
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 0 end
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
redis.call('ZADD', KEYS[1], now_ms, ARGV[1])
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]) * 2)
return 1`, []string{channelConcurrencyKey(g.channelID)}, g.leaseID, g.leaseTTL.Milliseconds()).Int()
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

func NewChannelRateLimitError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("rate_limit_exceeded"),
		types.ErrorCode("rate_limit_exceeded"),
		http.StatusTooManyRequests,
		types.ErrOptionWithSkipRetry(),
	)
}

func channelRateLimitKeys(channelID int) []string {
	tag := fmt.Sprintf("{channel:%d}", channelID)
	return []string{
		"channel_rate_limit:" + tag + ":rpm",
		"channel_rate_limit:" + tag + ":active",
	}
}

func channelConcurrencyKey(channelID int) string {
	return channelRateLimitKeys(channelID)[1]
}
