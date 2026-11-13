package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
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

func TestChannelPriorityQueueUsesDescendingPriorityAndFIFO(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               605,
		ConcurrencyLimit: channelLimitInt(1),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 501, Priority: 5},
			{UserId: 502, Priority: 4},
			{UserId: 503, Priority: 3},
			{UserId: 504, Priority: 3},
		},
	}
	occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 599), channel)
	require.True(t, allowed)
	require.NotNil(t, occupant)
	env.server.SetTime(time.Now().Truncate(time.Millisecond))

	type queuedResult struct {
		userID int
		guard  *ChannelRateLimitGuard
		ok     bool
	}
	results := make(chan queuedResult, 4)
	contexts := make([]context.CancelFunc, 4)
	for index, userID := range []int{501, 502, 503, 504} {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		contexts[index] = cancel
		go func(userID int, ctx context.Context) {
			guard, ok := AcquireChannelRateLimit(channelRateLimitTestContextForUser(ctx, userID), channel, ChannelRateLimitOptions{WaitTimeout: 2 * time.Second})
			results <- queuedResult{userID: userID, guard: guard, ok: ok}
		}(userID, ctx)
		require.Eventually(t, func() bool {
			count, err := env.client.ZCard(t.Context(), channelRateLimitKeys(channel.Id, userID)[4]).Result()
			return err == nil && count == int64(index+1)
		}, time.Second, 5*time.Millisecond)
	}
	defer func() {
		for _, cancel := range contexts {
			cancel()
		}
	}()

	occupant.Release()
	for _, expectedUser := range []int{501, 502, 503, 504} {
		select {
		case result := <-results:
			require.True(t, result.ok)
			require.Equal(t, expectedUser, result.userID)
			require.NotNil(t, result.guard)
			result.guard.Release()
		case <-time.After(3 * time.Second):
			t.Fatal("queued request did not acquire its slot")
		}
	}
}

func TestChannelPrioritySoupModesGrantNextPriorityWithinTheirBudget(t *testing.T) {
	tests := []struct {
		name          string
		admissionJSON string
		wantOrder     []int
		wantSoup      int64
	}{
		{
			name:          "interval",
			admissionJSON: `{"soup_mode":"interval","soup_after_ms":1000,"soup_interval_ms":60000}`,
			wantOrder:     []int{702, 701},
			wantSoup:      1,
		},
		{
			name:          "ratio",
			admissionJSON: `{"soup_mode":"ratio","soup_after_ms":1000,"soup_ratio_percent":50}`,
			wantOrder:     []int{702, 701},
			wantSoup:      1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := useChannelRateLimitMiniRedis(t)
			channel := &model.Channel{
				Id:               617,
				ConcurrencyLimit: channelLimitInt(1),
				OtherSettings:    `{"admission_control":` + test.admissionJSON + `}`,
				UserRateLimits: model.ChannelUserRateLimits{
					{UserId: 701, Priority: 5},
					{UserId: 702, Priority: 4},
				},
			}
			wantMode := dto.ChannelAdmissionSoupModeRatio
			if test.name == "interval" {
				wantMode = dto.ChannelAdmissionSoupModeInterval
			}
			assert.Equal(t, wantMode, channelAdmissionConfig(channel).SoupMode)
			occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 700), channel)
			require.True(t, allowed)
			require.NotNil(t, occupant)

			type queuedResult struct {
				userID int
				guard  *ChannelRateLimitGuard
				ok     bool
			}
			results := make(chan queuedResult, 2)
			contexts := make([]context.CancelFunc, 2)
			for index, userID := range []int{701, 702} {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				contexts[index] = cancel
				go func(userID int, ctx context.Context) {
					guard, ok := AcquireChannelRateLimit(channelRateLimitTestContextForUser(ctx, userID), channel, ChannelRateLimitOptions{WaitTimeout: 4 * time.Second})
					results <- queuedResult{userID: userID, guard: guard, ok: ok}
				}(userID, ctx)
				require.Eventually(t, func() bool {
					count, err := env.client.ZCard(t.Context(), channelRateLimitKeys(channel.Id, 0)[4]).Result()
					return err == nil && count == int64(index+1)
				}, time.Second, 5*time.Millisecond)
			}
			defer func() {
				for _, cancel := range contexts {
					cancel()
				}
			}()

			redisNow, err := env.client.Time(t.Context()).Result()
			require.NoError(t, err)
			env.server.SetTime(redisNow.Add(2 * time.Second))
			occupant.Release()

			order := make([]int, 0, len(test.wantOrder))
			for range len(test.wantOrder) {
				select {
				case result := <-results:
					require.True(t, result.ok)
					require.NotNil(t, result.guard)
					order = append(order, result.userID)
					result.guard.Release()
				case <-time.After(5 * time.Second):
					t.Fatal("queued request did not acquire its slot")
				}
			}
			assert.Equal(t, test.wantOrder, order)

			redisNow, err = env.client.Time(t.Context()).Result()
			require.NoError(t, err)
			stats, err := GetChannelAdmissionStats(t.Context(), channel.Id, redisNow.Add(-time.Minute), redisNow.Add(time.Minute))
			require.NoError(t, err)
			assert.Equal(t, test.wantSoup, stats.SoupGrants)
		})
	}
}

func TestChannelAdmissionQueueCapacityTimeoutAndCancellation(t *testing.T) {
	t.Run("capacity and cancellation", func(t *testing.T) {
		env := useChannelRateLimitMiniRedis(t)
		channel := &model.Channel{
			Id:               618,
			ConcurrencyLimit: channelLimitInt(1),
			OtherSettings:    `{"admission_control":{"max_waiting_total":1,"max_waiting_per_user":1}}`,
		}
		occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 710), channel)
		require.True(t, allowed)
		require.NotNil(t, occupant)

		waitCtx, cancelWait := context.WithCancel(t.Context())
		defer cancelWait()
		waitResult := make(chan bool, 1)
		go func() {
			_, ok := AcquireChannelRateLimit(
				channelRateLimitTestContextForUser(waitCtx, 711),
				channel,
				ChannelRateLimitOptions{WaitTimeout: 2 * time.Second},
			)
			waitResult <- ok
		}()
		queueKey := channelRateLimitKeys(channel.Id, 0)[4]
		require.Eventually(t, func() bool {
			count, err := env.client.ZCard(t.Context(), queueKey).Result()
			return err == nil && count == 1
		}, time.Second, 5*time.Millisecond)

		fullContext := channelRateLimitTestContextForUser(t.Context(), 712)
		fullGuard, fullAllowed := AcquireChannelRateLimit(
			fullContext,
			channel,
			ChannelRateLimitOptions{WaitTimeout: time.Second},
		)
		assert.False(t, fullAllowed)
		assert.Nil(t, fullGuard)
		assert.Equal(t, "30", fullContext.Writer.Header().Get("Retry-After"))

		cancelWait()
		select {
		case ok := <-waitResult:
			assert.False(t, ok)
		case <-time.After(time.Second):
			t.Fatal("cancelled request did not leave the admission queue")
		}
		require.Eventually(t, func() bool {
			count, err := env.client.ZCard(t.Context(), queueKey).Result()
			return err == nil && count == 0
		}, time.Second, 5*time.Millisecond)
		occupant.Release()

		stats, err := GetChannelAdmissionStats(t.Context(), channel.Id, time.Now().Add(-time.Minute), time.Now())
		require.NoError(t, err)
		assert.Equal(t, int64(1), stats.QueueFull)
		assert.Zero(t, stats.Waiting)
	})

	t.Run("timeout", func(t *testing.T) {
		useChannelRateLimitMiniRedis(t)
		channel := &model.Channel{Id: 619, ConcurrencyLimit: channelLimitInt(1)}
		occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 720), channel)
		require.True(t, allowed)
		require.NotNil(t, occupant)

		timeoutContext := channelRateLimitTestContextForUser(t.Context(), 721)
		guard, acquired := AcquireChannelRateLimit(
			timeoutContext,
			channel,
			ChannelRateLimitOptions{WaitTimeout: 30 * time.Millisecond},
		)
		assert.False(t, acquired)
		assert.Nil(t, guard)
		assert.Equal(t, "30", timeoutContext.Writer.Header().Get("Retry-After"))
		occupant.Release()

		stats, err := GetChannelAdmissionStats(t.Context(), channel.Id, time.Now().Add(-time.Minute), time.Now())
		require.NoError(t, err)
		assert.Equal(t, int64(1), stats.Timeouts)
		assert.Zero(t, stats.Waiting)
	})
}

func TestUserOnlyConcurrencyQueueDoesNotConsumeAnotherUsersCapacity(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:            620,
		OtherSettings: `{"admission_control":{"max_waiting_total":1,"max_waiting_per_user":1}}`,
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 730, Priority: 100, ConcurrencyLimit: channelLimitInt(1)},
			{UserId: 731, ConcurrencyLimit: channelLimitInt(1)},
		},
	}
	occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 730), channel)
	require.True(t, allowed)
	require.NotNil(t, occupant)

	waitResult := make(chan bool, 1)
	go func() {
		guard, ok := AcquireChannelRateLimit(
			channelRateLimitTestContextForUser(t.Context(), 730),
			channel,
			ChannelRateLimitOptions{WaitTimeout: time.Second},
		)
		if guard != nil {
			guard.Release()
		}
		waitResult <- ok
	}()
	queueKey := channelRateLimitKeys(channel.Id, 0)[4]
	require.Eventually(t, func() bool {
		count, err := env.client.ZCard(t.Context(), queueKey).Result()
		return err == nil && count == 1
	}, time.Second, 5*time.Millisecond)

	otherUser, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 731), channel)
	assert.True(t, allowed, "a per-user queue must not block another user's available slot without a channel-wide limit")
	require.NotNil(t, otherUser)
	otherUser.Release()
	occupant.Release()

	select {
	case ok := <-waitResult:
		assert.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("waiting user did not acquire the released user slot")
	}
}

func TestQueuedChannelAdmissionUsesCurrentConcurrencyLimit(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{Id: 621, ConcurrencyLimit: channelLimitInt(1)}
	occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 740), channel)
	require.True(t, allowed)
	require.NotNil(t, occupant)

	currentChannel := &model.Channel{Id: channel.Id, ConcurrencyLimit: channelLimitInt(2)}
	result := make(chan *ChannelRateLimitGuard, 1)
	go func() {
		guard, ok := AcquireChannelRateLimit(
			channelRateLimitTestContextForUser(t.Context(), 741),
			channel,
			ChannelRateLimitOptions{
				WaitTimeout: time.Second,
				RefreshChannel: func() (*model.Channel, error) {
					return currentChannel, nil
				},
			},
		)
		if ok {
			result <- guard
		}
	}()
	require.Eventually(t, func() bool {
		count, err := env.client.ZCard(t.Context(), channelRateLimitKeys(channel.Id, 0)[4]).Result()
		return err == nil && count == 1
	}, time.Second, 5*time.Millisecond)

	select {
	case guard := <-result:
		require.NotNil(t, guard)
		guard.Release()
	case <-time.After(time.Second):
		t.Fatal("queued request did not use the increased channel concurrency limit")
	}
	occupant.Release()
}

func TestQueuedChannelAdmissionUsesCurrentUserConcurrencyLimit(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               623,
		ConcurrencyLimit: channelLimitInt(3),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 743, ConcurrencyLimit: channelLimitInt(1)},
		},
	}
	occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 743), channel)
	require.True(t, allowed)
	require.NotNil(t, occupant)

	currentChannel := &model.Channel{
		Id:               channel.Id,
		ConcurrencyLimit: channelLimitInt(3),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 743, ConcurrencyLimit: channelLimitInt(2)},
		},
	}
	result := make(chan *ChannelRateLimitGuard, 1)
	go func() {
		guard, ok := AcquireChannelRateLimit(
			channelRateLimitTestContextForUser(t.Context(), 743),
			channel,
			ChannelRateLimitOptions{
				WaitTimeout: time.Second,
				RefreshChannel: func() (*model.Channel, error) {
					return currentChannel, nil
				},
			},
		)
		if ok {
			result <- guard
		}
	}()
	require.Eventually(t, func() bool {
		count, err := env.client.ZCard(t.Context(), channelRateLimitKeys(channel.Id, 743)[4]).Result()
		return err == nil && count == 1
	}, time.Second, 5*time.Millisecond)

	select {
	case guard := <-result:
		require.NotNil(t, guard)
		guard.Release()
	case <-time.After(time.Second):
		t.Fatal("queued request did not use the increased user concurrency limit")
	}
	occupant.Release()
}

func TestChannelAdmissionPrunesExpiredQueueEntriesBeforeCapacityCheck(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               624,
		ConcurrencyLimit: channelLimitInt(1),
		OtherSettings:    `{"admission_control":{"max_waiting_total":1,"max_waiting_per_user":1}}`,
	}
	occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 750), channel)
	require.True(t, allowed)
	require.NotNil(t, occupant)

	oldEnqueued := time.Now().Add(-channelAdmissionWaitTTL - time.Second).UnixMilli()
	staleMember := fmt.Sprintf("%013d|%020d|stale", oldEnqueued, 1)
	keys := channelRateLimitKeys(channel.Id, 749)
	require.NoError(t, env.client.ZAdd(t.Context(), keys[4], &redis.Z{Score: 0, Member: staleMember}).Err())
	require.NoError(t, env.client.ZAdd(t.Context(), keys[6], &redis.Z{Score: float64(oldEnqueued), Member: staleMember}).Err())
	require.NoError(t, env.client.HSet(t.Context(), keys[5], "stale", fmt.Sprintf("749|0|%d|1", oldEnqueued)).Err())

	result := make(chan *ChannelRateLimitGuard, 1)
	go func() {
		guard, ok := AcquireChannelRateLimit(
			channelRateLimitTestContextForUser(t.Context(), 751),
			channel,
			ChannelRateLimitOptions{WaitTimeout: time.Second},
		)
		if ok {
			result <- guard
		}
	}()
	queueKey := channelRateLimitKeys(channel.Id, 0)[4]
	require.Eventually(t, func() bool {
		members, err := env.client.ZRange(t.Context(), queueKey, 0, -1).Result()
		return err == nil && len(members) == 1 && members[0] != staleMember
	}, time.Second, 5*time.Millisecond)
	occupant.Release()

	select {
	case guard := <-result:
		require.NotNil(t, guard)
		guard.Release()
	case <-time.After(time.Second):
		t.Fatal("request did not enter after the expired queue entry was pruned")
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
	require.Len(t, keys, 12)
	for _, key := range keys {
		assert.Contains(t, key, "{channel:99}")
	}
	assert.Contains(t, keys[2], ":rpm:user:7")
	assert.Contains(t, keys[3], ":active:user:7")
	assert.Contains(t, keys[4], ":waiting")
	assert.Contains(t, keys[8], ":metrics:values")
	assert.Contains(t, keys[11], ":waiting:sequence")
}

func TestChannelAdmissionQueueWaitUpdatesMinuteMetrics(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channel := &model.Channel{
		Id:               616,
		ConcurrencyLimit: channelLimitInt(1),
		UserRateLimits: model.ChannelUserRateLimits{
			{UserId: 611, Priority: 3},
		},
	}
	occupant, allowed := TryAcquireChannelRateLimit(channelRateLimitTestContextForUser(t.Context(), 610), channel)
	require.True(t, allowed)
	require.NotNil(t, occupant)

	queued := make(chan *ChannelRateLimitGuard, 1)
	go func() {
		guard, ok := AcquireChannelRateLimit(
			channelRateLimitTestContextForUser(t.Context(), 611),
			channel,
			ChannelRateLimitOptions{WaitTimeout: time.Second},
		)
		if ok {
			queued <- guard
		}
	}()
	require.Eventually(t, func() bool {
		count, err := env.client.ZCard(t.Context(), channelRateLimitKeys(channel.Id, 0)[4]).Result()
		return err == nil && count == 1
	}, time.Second, 5*time.Millisecond)
	occupant.Release()

	select {
	case guard := <-queued:
		require.NotNil(t, guard)
		guard.Release()
	case <-time.After(time.Second):
		t.Fatal("waiting request did not acquire the released channel slot")
	}

	stats, err := GetChannelAdmissionStats(t.Context(), channel.Id, time.Now().Add(-time.Minute), time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.Grants)
	assert.Equal(t, int64(1), stats.PriorityGrants)
	assert.Equal(t, int64(1), stats.WaitedGrants)
	assert.Equal(t, int64(60), stats.BucketSeconds)
	assert.Equal(t, int64(0), stats.Active)
	assert.Equal(t, int64(0), stats.Waiting)
	assert.NotEmpty(t, stats.MinuteSeries)
	assert.Positive(t, stats.WaitP50Ms)
	assert.LessOrEqual(t, stats.WaitP50Ms, int64(5_000))
}

func TestChannelAdmissionStatsBucketAggregationZeroFillsGaps(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channelID := 617
	keys := channelRateLimitKeys(channelID, 0)
	now := time.Now()
	firstMinute := now.Add(-10*time.Minute).UnixMilli() / 60_000
	secondMinute := now.Add(-5*time.Minute).UnixMilli() / 60_000

	seed := func(minute int64, values map[string]int64) {
		for metric, value := range values {
			member := fmt.Sprintf("%d|%s", minute, metric)
			require.NoError(t, env.client.HSet(t.Context(), keys[8], member, value).Err())
			require.NoError(t, env.client.ZAdd(t.Context(), keys[9], &redis.Z{
				Score:  float64(minute*60_000 + 1_000),
				Member: member,
			}).Err())
		}
	}
	seed(firstMinute, map[string]int64{
		"grants":        3,
		"wait_le_500ms": 2,
		"timeout":       1,
	})
	seed(secondMinute, map[string]int64{
		"grants":        2,
		"wait_le_100ms": 2,
		"queue_full":    1,
	})

	stats, err := GetChannelAdmissionStats(t.Context(), channelID, now.Add(-time.Hour), now)
	require.NoError(t, err)
	assert.Equal(t, int64(60), stats.BucketSeconds)
	assert.GreaterOrEqual(t, len(stats.MinuteSeries), 60)
	assert.LessOrEqual(t, len(stats.MinuteSeries), 62)
	assert.Equal(t, int64(5), stats.Grants)
	assert.Equal(t, int64(3), stats.WaitedGrants)
	assert.Equal(t, int64(1), stats.Timeouts)
	assert.Equal(t, int64(1), stats.QueueFull)

	byTimestamp := make(map[int64]ChannelAdmissionMetricPoint, len(stats.MinuteSeries))
	for _, point := range stats.MinuteSeries {
		byTimestamp[point.TimestampMs] = point
	}
	first, ok := byTimestamp[firstMinute*60_000]
	require.True(t, ok)
	assert.Equal(t, int64(3), first.Grants)
	assert.Equal(t, int64(1), first.Waited)
	assert.Equal(t, int64(1), first.Timeouts)
	assert.Equal(t, int64(500), first.WaitP50Ms)

	second, ok := byTimestamp[secondMinute*60_000]
	require.True(t, ok)
	assert.Equal(t, int64(2), second.Grants)
	assert.Equal(t, int64(2), second.Waited)
	assert.Equal(t, int64(1), second.QueueFull)
	assert.Equal(t, int64(100), second.WaitP95Ms)

	emptyPoints := 0
	for _, point := range stats.MinuteSeries {
		if point.Grants == 0 && point.Waited == 0 && point.Timeouts == 0 && point.QueueFull == 0 {
			emptyPoints++
		}
	}
	assert.Greater(t, emptyPoints, 50)
}

func TestChannelAdmissionBucketSecondsFollowsWindow(t *testing.T) {
	assert.Equal(t, int64(60), channelAdmissionBucketSeconds(time.Hour.Milliseconds()))
	assert.Equal(t, int64(300), channelAdmissionBucketSeconds(24*time.Hour.Milliseconds()))
	assert.Equal(t, int64(3600), channelAdmissionBucketSeconds(7*24*time.Hour.Milliseconds()))
}

func TestChannelAdmissionStatsReportsLiveQueueUsers(t *testing.T) {
	env := useChannelRateLimitMiniRedis(t)
	channelID := 618
	keys := channelRateLimitKeys(channelID, 0)
	now := time.Now().UnixMilli()
	require.NoError(t, env.client.ZAdd(t.Context(), keys[1], &redis.Z{
		Score: float64(now), Member: "active-lease",
	}).Err())
	require.NoError(t, env.client.HSet(t.Context(), keys[7], "active-lease", "5|0").Err())

	enqueuedLow := now - 5_000
	lowMember := fmt.Sprintf("%013d|%020d|%s", enqueuedLow, 1, "waiting-lease")
	require.NoError(t, env.client.ZAdd(t.Context(), keys[4], &redis.Z{
		Score: 0, Member: lowMember,
	}).Err())
	require.NoError(t, env.client.HSet(t.Context(), keys[5], "waiting-lease",
		fmt.Sprintf("5|0|%d|1", enqueuedLow)).Err())
	enqueuedLater := now - 2_000
	laterMember := fmt.Sprintf("%013d|%020d|%s", enqueuedLater, 3, "waiting-later")
	require.NoError(t, env.client.ZAdd(t.Context(), keys[4], &redis.Z{
		Score: 0, Member: laterMember,
	}).Err())
	require.NoError(t, env.client.HSet(t.Context(), keys[5], "waiting-later",
		fmt.Sprintf("5|0|%d|3", enqueuedLater)).Err())
	enqueuedHigh := now - 1_000
	highMember := fmt.Sprintf("%013d|%020d|%s", enqueuedHigh, 2, "waiting-high")
	require.NoError(t, env.client.ZAdd(t.Context(), keys[4], &redis.Z{
		Score: -2, Member: highMember,
	}).Err())
	require.NoError(t, env.client.HSet(t.Context(), keys[5], "waiting-high",
		fmt.Sprintf("6|2|%d|2", enqueuedHigh)).Err())

	stats, err := GetChannelAdmissionStats(
		t.Context(),
		channelID,
		time.UnixMilli(now).Add(-time.Minute),
		time.UnixMilli(now),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Active)
	assert.Equal(t, int64(1), stats.ActiveUsers)
	require.Len(t, stats.ActiveUserList, 1)
	assert.Equal(t, 5, stats.ActiveUserList[0].UserID)
	assert.Equal(t, int64(1), stats.ActiveUserList[0].Count)
	assert.Equal(t, int64(3), stats.Waiting)
	require.Len(t, stats.WaitingUsers, 2)
	assert.Equal(t, 6, stats.WaitingUsers[0].UserID)
	assert.Equal(t, 2, stats.WaitingUsers[0].Priority)
	assert.Equal(t, 1, stats.WaitingUsers[0].Position)
	assert.Equal(t, 5, stats.WaitingUsers[1].UserID)
	assert.Equal(t, 0, stats.WaitingUsers[1].Priority)
	assert.Equal(t, 2, stats.WaitingUsers[1].Position)
	assert.Equal(t, int64(2), stats.WaitingUsers[1].Count)
	assert.GreaterOrEqual(t, stats.WaitingUsers[1].OldestWaitMs, int64(5_000))
	require.Len(t, stats.WaitingRequests, 3)
	assert.Equal(t, 6, stats.WaitingRequests[0].UserID)
	assert.Equal(t, 1, stats.WaitingRequests[0].Position)
	assert.Equal(t, 5, stats.WaitingRequests[1].UserID)
	assert.Equal(t, 2, stats.WaitingRequests[1].Position)
	assert.GreaterOrEqual(t, stats.WaitingRequests[1].WaitMs, int64(5_000))
	assert.Equal(t, 5, stats.WaitingRequests[2].UserID)
	assert.Equal(t, 3, stats.WaitingRequests[2].Position)
	assert.GreaterOrEqual(t, stats.WaitingRequests[2].WaitMs, int64(2_000))
	assert.NotEmpty(t, stats.WaitingRequests[0].ID)
	assert.NotEqual(t, stats.WaitingRequests[0].ID, stats.WaitingRequests[1].ID)
	assert.NotEqual(t, stats.WaitingRequests[1].ID, stats.WaitingRequests[2].ID)
	assert.GreaterOrEqual(t, stats.WaitingRequests[1].WaitMs, stats.WaitingRequests[2].WaitMs)
	assert.GreaterOrEqual(t, stats.OldestWaitMs, int64(5_000))
}

func TestChannelAdmissionObservationUsesAdminLogJSON(t *testing.T) {
	c := newUserRateLimitTestContext(t.Context())
	observation := channelRateLimitObservation(c)
	observation.noteSnapshot(channelRateLimitSnapshot{ChannelID: 622, ConcurrencyLimit: 2, Priority: 5})
	observation.noteRejected(622, 740, "concurrency", "queue_timeout")
	observation.noteQueueWait(2 * time.Second)
	observation.noteSoupGrant()

	other := model.NewLogOther()
	AppendChannelRateLimitAdminInfo(c, other)
	encoded, err := common.Marshal(other)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(encoded, &decoded))

	adminInfo, ok := decoded["admin_info"].(map[string]any)
	require.True(t, ok)
	rateLimits, ok := adminInfo["channel_rate_limits"].(map[string]any)
	require.True(t, ok)
	admission, ok := rateLimits["admission"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), admission["queued_requests"])
	assert.Equal(t, float64(2_000), admission["average_wait_ms"])
	assert.Equal(t, float64(1), admission["soup_grants"])
	assert.Equal(t, float64(1), admission["timeouts"])
}
