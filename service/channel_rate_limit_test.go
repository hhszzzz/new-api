package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
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

func TestChannelRateLimitErrorDistinguishesRPMFromConcurrency(t *testing.T) {
	rpmContext := newUserRateLimitTestContext(t.Context())
	channelRateLimitObservation(rpmContext).noteRejected(1, 0, "rpm", "total")
	assert.Equal(t, "Request rate limit reached. Please try again later", NewChannelRateLimitError(rpmContext).Error())

	concurrencyContext := newUserRateLimitTestContext(t.Context())
	channelRateLimitObservation(concurrencyContext).noteRejected(1, 0, "concurrency", "total")
	assert.Equal(t, "Concurrency limit reached. Please try again later", NewChannelRateLimitError(concurrencyContext).Error())

	mixedContext := newUserRateLimitTestContext(t.Context())
	observation := channelRateLimitObservation(mixedContext)
	observation.noteRejected(1, 0, "rpm", "total")
	observation.noteRejected(2, 0, "concurrency", "total")
	assert.Equal(t, "Request rate or concurrency limit reached. Please try again later", NewChannelRateLimitError(mixedContext).Error())
}

func channelRateLimitTestContextForUser(ctx context.Context, userId int) *gin.Context {
	c := newUserRateLimitTestContext(ctx)
	c.Set("id", userId)
	return c
}

func TestChannelUserRateLimitOverridesAreIsolatedPerUser(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id: 601,
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 71, RPMLimit: channelLimitInt(1), ConcurrencyLimit: channelLimitInt(1)},
			{UserId: 72, ConcurrencyLimit: channelLimitInt(1)},
		},
	}

	first, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 71), channel)
	require.True(t, allowed)
	require.NotNil(t, first)
	second, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 71), channel)
	assert.False(t, allowed)
	assert.Nil(t, second)

	otherUser, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 72), channel)
	assert.True(t, allowed, "another user's override must use a separate pool")
	require.NotNil(t, otherUser)
	first.Release()
	otherUser.Release()

	count, err := env.client.HGet(t.Context(), channelRateLimitKeys(channel.Id, 71)[2], "count").Int()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "a rejected reservation must not consume the user's RPM")

	unconfigured, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 73), channel)
	assert.True(t, allowed, "users without an override must not be limited by another user's values")
	if unconfigured != nil {
		unconfigured.Release()
	}
}

func TestChannelUserRateLimitStillHonorsChannelTotal(t *testing.T) {
	useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               602,
		ConcurrencyLimit: channelLimitInt(1),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 73, ConcurrencyLimit: channelLimitInt(5)},
			{UserId: 74, ConcurrencyLimit: channelLimitInt(5)},
		},
	}

	first, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 73), channel)
	require.True(t, allowed)
	require.NotNil(t, first)
	second, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 74), channel)
	assert.False(t, allowed, "the channel total must still cap users with an override")
	assert.Nil(t, second)
	first.Release()
}

func TestChannelUserRateLimitIsTighterThanChannelTotal(t *testing.T) {
	useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               603,
		ConcurrencyLimit: channelLimitInt(5),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 75, ConcurrencyLimit: channelLimitInt(1)},
		},
	}

	first, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 75), channel)
	require.True(t, allowed)
	require.NotNil(t, first)
	second, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 75), channel)
	assert.False(t, allowed, "a user override may be tighter than the channel total")
	assert.Nil(t, second)
	first.Release()
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

	count, err := env.client.HGet(t.Context(), channelRateLimitKeys(channel.Id, 0)[0], "count").Int()
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
		totalLease:  true,
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
	keys := channelRateLimitKeys(99, 7)
	require.Len(t, keys, 4)
	for _, key := range keys {
		assert.Contains(t, key, "{channel:99}")
	}
	assert.Contains(t, keys[2], ":rpm:user:7")
	assert.Contains(t, keys[3], ":active:user:7")
}
