package service

import (
	"context"
	"strconv"
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

func TestChannelNumericPriorityDrainsLowerPriority(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               605,
		ConcurrencyLimit: channelLimitInt(2),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 84, Priority: 100},
		},
	}

	low1, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 85), channel)
	require.True(t, allowed)
	require.NotNil(t, low1)
	low2, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 86), channel)
	require.True(t, allowed)
	require.NotNil(t, low2)

	high, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 84), channel)
	assert.False(t, allowed, "high priority waits for a channel slot")
	assert.Nil(t, high)

	low1.Release()
	newLow, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 85), channel)
	assert.False(t, allowed, "a higher-priority demand blocks new lower-priority requests")
	assert.Nil(t, newLow)

	high, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 84), channel)
	assert.True(t, allowed)
	require.NotNil(t, high)

	low2.Release()
	secondHigh, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 84), channel)
	assert.True(t, allowed, "high priority may use the full channel limit")
	require.NotNil(t, secondHigh)

	thirdHighContext := channelRateLimitTestContextForUser(t.Context(), 84)
	thirdHigh, allowed := TryAcquireChannelRateLimit(thirdHighContext, channel)
	assert.False(t, allowed, "the channel limit still applies to high priority")
	assert.Nil(t, thirdHigh)

	high.Release()
	secondHigh.Release()
	blockedLow, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 85), channel)
	assert.False(t, allowed, "a still-waiting high-priority request keeps its demand marker")
	assert.Nil(t, blockedLow)

	thirdHigh, allowed = TryAcquireChannelRateLimit(thirdHighContext, channel)
	assert.True(t, allowed, "the waiting high-priority request can acquire the released capacity")
	require.NotNil(t, thirdHigh)
	thirdHigh.Release()

	low, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 85), channel)
	assert.True(t, allowed, "lower-priority requests can use capacity when no higher-priority request is waiting")
	if low != nil {
		low.Release()
	}
	demandExists, err := env.client.Exists(t.Context(), channelRateLimitKeys(channel.Id, 0)[4]).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), demandExists, "admission clears the default-tier marker too")
}

func TestChannelNumericPriorityOrdersLevels(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               606,
		ConcurrencyLimit: channelLimitInt(2),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 91, Priority: 200},
			{UserId: 92, Priority: 100},
			{UserId: 94, Priority: 100},
			{UserId: 95, Priority: -100},
		},
	}

	lowA, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 93), channel)
	require.True(t, allowed)
	require.NotNil(t, lowA)
	lowB, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 93), channel)
	require.True(t, allowed)
	require.NotNil(t, lowB)

	p100, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 92), channel)
	assert.False(t, allowed)
	assert.Nil(t, p100)
	p100Other, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 94), channel)
	assert.False(t, allowed)
	assert.Nil(t, p100Other)
	p200, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 91), channel)
	assert.False(t, allowed)
	assert.Nil(t, p200)

	lowA.Release()
	p100, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 92), channel)
	assert.False(t, allowed, "a lower priority must yield to a waiting higher priority")
	assert.Nil(t, p100)
	priority100Demand, err := env.client.ZScore(t.Context(), channelRateLimitKeys(channel.Id, 0)[4], "100|92").Result()
	require.NoError(t, err)
	assert.Greater(t, priority100Demand, float64(0), "a lower tier blocked by priority 200 must retain its demand")

	p200, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 91), channel)
	assert.True(t, allowed, "the highest waiting priority takes the free slot")
	require.NotNil(t, p200)

	p200.Release()
	blockedLow, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 93), channel)
	assert.False(t, allowed, "priority 100 demand continues to hold off the default tier")
	assert.Nil(t, blockedLow)
	blockedDefault, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 97), channel)
	assert.False(t, allowed, "another default-tier request waits behind priority 100")
	assert.Nil(t, blockedDefault)

	p100, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 92), channel)
	assert.True(t, allowed, "the next waiting priority tier takes the available slot")
	require.NotNil(t, p100)
	p100.Release()

	blockedLow, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 93), channel)
	assert.False(t, allowed, "one admitted user must not clear another user's same-tier demand")
	assert.Nil(t, blockedLow)

	p100Other, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 94), channel)
	assert.True(t, allowed, "the other user at priority 100 can acquire the next free slot")
	require.NotNil(t, p100Other)
	p100Other.Release()

	lowB.Release()
	defaultTier, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 93), channel)
	assert.True(t, allowed, "the default tier resumes after all higher-priority demand is served")
	require.NotNil(t, defaultTier)
	defaultTier.Release()

	negativeTier, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 95), channel)
	assert.False(t, allowed, "one default-tier admission must not clear another user's pending demand")
	assert.Nil(t, negativeTier)
	defaultTier, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 97), channel)
	assert.True(t, allowed)
	require.NotNil(t, defaultTier)
	defaultTier.Release()

	negativeTier, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 95), channel)
	assert.True(t, allowed, "lower priorities resume after each default-tier demand is served")
	if negativeTier != nil {
		negativeTier.Release()
	}
}

func TestChannelPriorityHonorsUserConcurrency(t *testing.T) {
	useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               607,
		ConcurrencyLimit: channelLimitInt(3),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 95, Priority: 100, ConcurrencyLimit: channelLimitInt(1)},
		},
	}

	high, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 95), channel)
	require.True(t, allowed)
	require.NotNil(t, high)

	blockedHigh, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 95), channel)
	assert.False(t, allowed, "high priority still obeys the user's own concurrency limit")
	assert.Nil(t, blockedHigh)

	low, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 96), channel)
	assert.True(t, allowed, "a user-level rejection must not create channel priority demand")
	if low != nil {
		low.Release()
	}
	high.Release()
}

func TestChannelPriorityHonorsUserRPM(t *testing.T) {
	useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               609,
		ConcurrencyLimit: channelLimitInt(2),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 931, Priority: 100, RPMLimit: channelLimitInt(1)},
		},
	}

	high, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 931), channel)
	require.True(t, allowed)
	require.NotNil(t, high)

	blockedHigh, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 931), channel)
	assert.False(t, allowed, "high priority still obeys the user's own RPM limit")
	assert.Nil(t, blockedHigh)

	low, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 932), channel)
	assert.True(t, allowed, "a user-level RPM rejection must not create channel priority demand")
	if low != nil {
		low.Release()
	}
	high.Release()
}

func TestChannelPriorityDemandExpires(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               608,
		ConcurrencyLimit: channelLimitInt(1),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 97, Priority: 100},
		},
	}

	low, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 98), channel)
	require.True(t, allowed)
	require.NotNil(t, low)
	high, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 97), channel)
	assert.False(t, allowed)
	assert.Nil(t, high)

	low.Release()
	blockedLow, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 98), channel)
	assert.False(t, allowed, "lower priority must wait out the demand window")
	assert.Nil(t, blockedLow)

	now, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	env.server.SetTime(now.Add(11 * time.Second))
	low, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 98), channel)
	assert.True(t, allowed, "lower priority resumes after the demand marker expires")
	if low != nil {
		low.Release()
	}
}

func TestChannelPriorityDemandMarkerLimitPreservesHigherPriorities(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               615,
		ConcurrencyLimit: channelLimitInt(1),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 1, Priority: 200},
			{UserId: 2, Priority: 100},
		},
	}
	demandKey := channelRateLimitKeys(channel.Id, 0)[4]
	redisTime, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	expiresAt := float64(redisTime.UnixMilli() + channelPriorityDemandTTL.Milliseconds())
	demands := make([]*redis.Z, channelPriorityDemandMaxMembers)
	for userID := range channelPriorityDemandMaxMembers {
		demands[userID] = &redis.Z{Score: expiresAt, Member: "100|" + strconv.Itoa(10_000+userID)}
	}
	require.NoError(t, env.client.ZAdd(t.Context(), demandKey, demands...).Err())

	lower, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 2), channel)
	require.True(t, allowed)
	require.NotNil(t, lower)
	higher, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 1), channel)
	assert.False(t, allowed)
	assert.Nil(t, higher)
	require.NoError(t, env.client.ZScore(t.Context(), demandKey, "200|1").Err())

	count, err := env.client.ZCard(t.Context(), demandKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(channelPriorityDemandMaxMembers), count)

	lower.Release()
	lower, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 2), channel)
	assert.False(t, allowed, "a full marker set must retain the higher-priority demand")
	assert.Nil(t, lower)
}

func TestChannelPriorityDemandClearsWhenRequestUsesAnotherChannel(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	limitedChannel := &model.Channel{
		Id:               610,
		ConcurrencyLimit: channelLimitInt(1),
		UserRateLimits:   model.ChannelUserRateLimits{{UserId: 101, Priority: 100}},
	}
	secondLimitedChannel := &model.Channel{
		Id:               613,
		ConcurrencyLimit: channelLimitInt(1),
		UserRateLimits:   model.ChannelUserRateLimits{{UserId: 101, Priority: 100}},
	}

	low, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 102), limitedChannel)
	require.True(t, allowed)
	require.NotNil(t, low)
	secondLow, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 103), secondLimitedChannel)
	require.True(t, allowed)
	require.NotNil(t, secondLow)

	highContext := channelRateLimitTestContextForUser(t.Context(), 101)
	high, allowed := TryAcquireChannelRateLimit(highContext, limitedChannel)
	assert.False(t, allowed)
	assert.Nil(t, high)

	markerKey := channelRateLimitKeys(limitedChannel.Id, 0)[4]
	markers, err := env.client.ZRange(t.Context(), markerKey, 0, -1).Result()
	require.NoError(t, err)
	assert.Contains(t, markers, "100|101")
	_, allowed = TryAcquireChannelRateLimit(highContext, secondLimitedChannel)
	assert.False(t, allowed)
	secondMarkerKey := channelRateLimitKeys(secondLimitedChannel.Id, 0)[4]
	markers, err = env.client.ZRange(t.Context(), secondMarkerKey, 0, -1).Result()
	require.NoError(t, err)
	assert.Contains(t, markers, "100|101")

	_, allowed = TryAcquireChannelRateLimit(highContext, &model.Channel{Id: 611})
	require.True(t, allowed, "an admitted fallback channel satisfies this request's blocked candidate demands")

	markers, err = env.client.ZRange(t.Context(), markerKey, 0, -1).Result()
	require.NoError(t, err)
	assert.NotContains(t, markers, "100|101")
	markers, err = env.client.ZRange(t.Context(), secondMarkerKey, 0, -1).Result()
	require.NoError(t, err)
	assert.NotContains(t, markers, "100|101")

	low.Release()
	low, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 102), limitedChannel)
	assert.True(t, allowed, "the previously blocked channel can immediately serve lower priority")
	if low != nil {
		low.Release()
	}
	secondLow.Release()
	secondLow, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 103), secondLimitedChannel)
	assert.True(t, allowed, "the other previously blocked channel can immediately serve lower priority")
	if secondLow != nil {
		secondLow.Release()
	}
}

func TestClearChannelPriorityDemandMarkers(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	key := channelRateLimitKeys(614, 0)[4]
	require.NoError(t, env.client.ZAdd(t.Context(), key,
		&redis.Z{Score: 1, Member: "100|one"},
		&redis.Z{Score: 2, Member: "50|two"},
	).Err())

	ClearChannelPriorityDemandMarkers(614)

	count, err := env.client.Exists(t.Context(), key).Result()
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestChannelPriorityChangeDoesNotBlockTheSameUser(t *testing.T) {
	useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               612,
		ConcurrencyLimit: channelLimitInt(1),
		UserRateLimits:   model.ChannelUserRateLimits{{UserId: 103, Priority: 200}},
	}

	low, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 104), channel)
	require.True(t, allowed)
	require.NotNil(t, low)

	high, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 103), channel)
	assert.False(t, allowed)
	assert.Nil(t, high)

	channel.UserRateLimits[0].Priority = 100
	low.Release()
	high, allowed = TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 103), channel)
	assert.True(t, allowed, "a stale marker from the user's previous priority must not block the user")
	require.NotNil(t, high)
	high.Release()
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
	require.Len(t, keys, 5)
	for _, key := range keys {
		assert.Contains(t, key, "{channel:99}")
	}
	assert.Contains(t, keys[2], ":rpm:user:7")
	assert.Contains(t, keys[3], ":active:user:7")
	assert.Contains(t, keys[4], ":priority:demand")
}
