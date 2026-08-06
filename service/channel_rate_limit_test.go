package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelRateLimitRedisTestEnv struct {
	server *miniredis.Miniredis
	client *redis.Client
}

func useChannelRateLimitMiniRedis(t *testing.T) *channelRateLimitRedisTestEnv {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previous := channelRateLimitRedisOverride.Load()
	channelRateLimitRedisOverride.Store(&channelRateLimitRedisSource{client: client, enabled: true})
	t.Cleanup(func() {
		channelRateLimitRedisOverride.Store(previous)
		_ = client.Close()
	})
	return &channelRateLimitRedisTestEnv{server: server, client: client}
}

func channelLimitInt(value int) *int {
	return &value
}

func TestChannelRateLimitRPMUsesRedisMinuteAndIsolatesChannels(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{Id: 501, RpmLimit: channelLimitInt(2)}

	for range 2 {
		guard, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
		assert.True(t, allowed)
		require.NotNil(t, guard)
		guard.Release()
	}
	guard, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
	assert.False(t, allowed)
	assert.Nil(t, guard)

	other, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), &model.Channel{Id: 502, RpmLimit: channelLimitInt(2)})
	assert.True(t, allowed, "different channels must use isolated counters")
	require.NotNil(t, other)
	other.Release()

	redisNow, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	env.server.SetTime(redisNow.Add(61 * time.Second))
	guard, allowed = TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
	assert.True(t, allowed, "the Redis-server minute boundary must restore RPM capacity")
	require.NotNil(t, guard)
	guard.Release()
}

func TestChannelRateLimitConcurrencyRejectionDoesNotConsumeRPM(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               503,
		RpmLimit:         channelLimitInt(2),
		ConcurrencyLimit: channelLimitInt(1),
	}

	first, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
	require.True(t, allowed)
	require.NotNil(t, first)
	second, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
	assert.False(t, allowed)
	assert.Nil(t, second)

	count, err := env.client.HGet(t.Context(), channelRateLimitKeys(channel.Id)[0], "count").Int()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "a rejected reservation must not modify the channel RPM state")
	first.Release()

	third, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
	require.True(t, allowed)
	require.NotNil(t, third)
	third.Release()
	fourth, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
	assert.False(t, allowed, "the successful attempt after release must consume the second RPM slot")
	assert.Nil(t, fourth)
}

func TestChannelRateLimitAtomicConcurrencyAndRelease(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{Id: 504, ConcurrencyLimit: channelLimitInt(1)}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var guards []*ChannelRateLimitGuard
	allowedCount := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			guard, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
			mu.Lock()
			defer mu.Unlock()
			if allowed {
				allowedCount++
				guards = append(guards, guard)
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.Equal(t, 1, allowedCount)
	require.Len(t, guards, 1)
	guards[0].Release()
	active, err := env.client.ZCard(t.Context(), channelConcurrencyKey(channel.Id)).Result()
	require.NoError(t, err)
	assert.Zero(t, active)
}

func TestChannelRateLimitReclaimsExpiredLeaseAndFailsOpen(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{Id: 505, ConcurrencyLimit: channelLimitInt(1)}
	now, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	require.NoError(t, env.client.ZAdd(t.Context(), channelConcurrencyKey(channel.Id), &redis.Z{
		Score:  float64(now.Add(-channelConcurrencyLeaseTTL - time.Millisecond).UnixMilli()),
		Member: "expired",
	}).Err())

	guard, allowed := TryAcquireChannelRateLimit(newUserRateLimitTestContext(t.Context()), channel)
	require.True(t, allowed)
	require.NotNil(t, guard)
	guard.Release()

	require.NoError(t, env.client.Close())
	c := newUserRateLimitTestContext(t.Context())
	guard, allowed = TryAcquireChannelRateLimit(c, channel)
	assert.True(t, allowed)
	assert.Nil(t, guard)
	observation := channelRateLimitObservation(c)
	assert.True(t, observation.redisFailOpen["acquire"])
}

func TestChannelConcurrencyLeaseRenewsWithServerTime(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channelID := 506
	leaseID := "renew-me"
	now, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	require.NoError(t, env.client.ZAdd(t.Context(), channelConcurrencyKey(channelID), &redis.Z{Score: float64(now.UnixMilli()), Member: leaseID}).Err())
	observation := &ChannelRateLimitObservation{}
	guard := &ChannelRateLimitGuard{
		channelID:   channelID,
		leaseID:     leaseID,
		requestID:   "test",
		leaseTTL:    100 * time.Millisecond,
		renewEvery:  5 * time.Millisecond,
		observation: observation,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	initialScore, err := env.client.ZScore(t.Context(), channelConcurrencyKey(channelID), leaseID).Result()
	require.NoError(t, err)
	env.server.SetTime(now.Add(20 * time.Millisecond))
	guard.startRenewal(newUserRateLimitTestContext(context.Background()))
	require.Eventually(t, func() bool {
		score, scoreErr := env.client.ZScore(t.Context(), channelConcurrencyKey(channelID), leaseID).Result()
		return scoreErr == nil && score > initialScore
	}, 100*time.Millisecond, time.Millisecond)
	guard.Release()
	assert.Positive(t, observation.leaseRenewed)
}

func TestChannelRateLimitKeysShareRedisClusterHashTag(t *testing.T) {
	keys := channelRateLimitKeys(99)
	require.Len(t, keys, 2)
	assert.Contains(t, keys[0], "{channel:99}")
	assert.Contains(t, keys[1], "{channel:99}")
}
