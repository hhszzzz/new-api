package service

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/group_rate_limit_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userRateLimitRedisTestEnv struct {
	server *miniredis.Miniredis
	client *redis.Client
}

func useUserRateLimitMiniRedis(t *testing.T) *userRateLimitRedisTestEnv {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previous := userRateLimitRedisOverride.Load()
	userRateLimitRedisOverride.Store(&userRateLimitRedisSource{client: client, enabled: true})
	t.Cleanup(func() {
		userRateLimitRedisOverride.Store(previous)
		_ = client.Close()
	})
	return &userRateLimitRedisTestEnv{server: server, client: client}
}

func newUserRateLimitTestContext(ctx context.Context) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	c.Set(common.RequestIdKey, common.NewRequestId())
	return c
}

func useGroupRateLimitSetting(t *testing.T, setting group_rate_limit_setting.Setting) {
	t.Helper()
	previous := group_rate_limit_setting.GetSettingSnapshot()
	publishGroupRateLimitSetting(t, setting)
	t.Cleanup(func() {
		publishGroupRateLimitSetting(t, *previous)
	})
}

func publishGroupRateLimitSetting(t *testing.T, setting group_rate_limit_setting.Setting) {
	t.Helper()
	policies, err := common.Marshal(setting.Policies)
	require.NoError(t, err)
	handled, err := config.GlobalConfig.Update(group_rate_limit_setting.ConfigName, map[string]string{
		"member_enabled":      strconv.FormatBool(setting.MemberEnabled),
		"shared_pool_enabled": strconv.FormatBool(setting.SharedPoolEnabled),
		"policies":            string(policies),
	})
	require.True(t, handled)
	require.NoError(t, err)
}

func rateLimitInt(value int) *int {
	return &value
}

func TestUserRateLimitPolicyMergesUserMemberAndSharedLimits(t *testing.T) {
	useGroupRateLimitSetting(t, group_rate_limit_setting.Setting{
		MemberEnabled:     true,
		SharedPoolEnabled: true,
		Policies: map[string]group_rate_limit_setting.GroupPolicy{
			"vip": {
				MemberLimits: group_rate_limit_setting.Limits{
					RPMLimit:         rateLimitInt(60),
					ConcurrencyLimit: rateLimitInt(2),
					StreamTPSLimit:   rateLimitInt(12),
				},
				SharedPool: group_rate_limit_setting.Limits{
					RPMLimit:         rateLimitInt(3000),
					ConcurrencyLimit: rateLimitInt(100),
					StreamTPSLimit:   rateLimitInt(1000),
				},
			},
		},
	})

	policy := buildUserRateLimitPolicy(7, "vip", 30, 4, 0)
	assert.Equal(t, 30, policy.RPMLimit, "a user override may only tighten the member limit")
	assert.Equal(t, 2, policy.ConcurrencyLimit)
	assert.Equal(t, 12, policy.StreamTPSLimit, "clearing a user override falls back to the member limit")
	assert.Equal(t, 3000, policy.SharedRPMLimit)
	assert.Equal(t, 100, policy.SharedConcurrencyLimit)
	assert.Equal(t, 1000, policy.SharedStreamTPSLimit)

	defaultPolicy := buildUserRateLimitPolicy(8, "default", 10, 1, 5)
	assert.Equal(t, 10, defaultPolicy.RPMLimit)
	assert.Zero(t, defaultPolicy.SharedRPMLimit, "groups without a policy remain unrestricted")
}

func TestUserRateLimitPolicyUsesTokenGroupAndIndependentSwitches(t *testing.T) {
	useGroupRateLimitSetting(t, group_rate_limit_setting.Setting{
		MemberEnabled:     true,
		SharedPoolEnabled: false,
		Policies: map[string]group_rate_limit_setting.GroupPolicy{
			"default": {
				MemberLimits: group_rate_limit_setting.Limits{RPMLimit: rateLimitInt(10)},
				SharedPool:   group_rate_limit_setting.Limits{RPMLimit: rateLimitInt(100)},
			},
			"vip": {
				MemberLimits: group_rate_limit_setting.Limits{RPMLimit: rateLimitInt(60)},
				SharedPool:   group_rate_limit_setting.Limits{RPMLimit: rateLimitInt(600)},
			},
		},
	})
	c := newUserRateLimitTestContext(t.Context())
	common.SetContextKey(c, constant.ContextKeyUserId, 9)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyTokenGroup, "vip")

	policy := UserRateLimitPolicyFromContext(c)
	assert.Equal(t, "vip", policy.Group)
	assert.Equal(t, 60, policy.RPMLimit)
	assert.Zero(t, policy.SharedRPMLimit, "the disabled shared-pool switch must preserve but not enforce its values")

	publishGroupRateLimitSetting(t, group_rate_limit_setting.Setting{
		MemberEnabled:     false,
		SharedPoolEnabled: true,
		Policies: map[string]group_rate_limit_setting.GroupPolicy{
			"vip": {
				MemberLimits: group_rate_limit_setting.Limits{RPMLimit: rateLimitInt(60)},
				SharedPool:   group_rate_limit_setting.Limits{RPMLimit: rateLimitInt(600)},
			},
		},
	})
	policy = UserRateLimitPolicyFromContext(c)
	assert.Zero(t, policy.RPMLimit, "the disabled member switch must preserve but not enforce its values")
	assert.Equal(t, 600, policy.SharedRPMLimit)
}

func TestUserRPMFixedMinuteWindowAndSharedUserCounter(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)

	for range 2 {
		allowed, err := checkUserRPM(t.Context(), 77, 2)
		require.NoError(t, err)
		assert.True(t, allowed)
	}
	allowed, err := checkUserRPM(t.Context(), 77, 2)
	require.NoError(t, err)
	assert.False(t, allowed, "all tokens for one user must share the same RPM counter")

	keys := env.server.Keys()
	require.Len(t, keys, 1)
	assert.True(t, strings.HasPrefix(keys[0], "user_rate_limit:rpm:77:"))
	assert.Equal(t, 120*time.Second, env.server.TTL(keys[0]))

	redisNow, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	env.server.SetTime(redisNow.Add(61 * time.Second))
	allowed, err = checkUserRPM(t.Context(), 77, 2)
	require.NoError(t, err)
	assert.True(t, allowed, "a new Redis-server minute must use a fresh counter")
}

func TestUserRPMRedisFailureIsFailOpen(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	require.NoError(t, env.client.Close())

	allowed, err := checkUserRPM(t.Context(), 91, 1)
	assert.True(t, allowed)
	assert.Error(t, err)
}

func TestGroupRPMIsSharedAndPreservesIndividualCountingOrder(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	context := newUserRateLimitTestContext(t.Context())
	policy := UserRateLimitPolicy{UserID: 101, Group: "vip", RPMLimit: 1, SharedRPMLimit: 5}

	guard, apiErr := BeginUserRequestRateLimit(context, policy, "gpt-4o", UserConcurrencyWaitOptions{})
	require.Nil(t, apiErr)
	require.NotNil(t, guard)
	guard.Release()

	guard, apiErr = BeginUserRequestRateLimit(newUserRateLimitTestContext(t.Context()), policy, "gpt-4o", UserConcurrencyWaitOptions{})
	assert.Nil(t, guard)
	require.NotNil(t, apiErr)

	now, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	sharedCount, err := env.client.Get(t.Context(), groupRPMKey("vip", now.Unix()/60)).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 1, sharedCount, "an individual rejection must not consume the shared pool")

	secondUser := UserRateLimitPolicy{UserID: 102, Group: "vip", RPMLimit: 5, SharedRPMLimit: 1}
	guard, apiErr = BeginUserRequestRateLimit(newUserRateLimitTestContext(t.Context()), secondUser, "gpt-4o", UserConcurrencyWaitOptions{})
	assert.Nil(t, guard)
	require.NotNil(t, apiErr, "another user in the same group must share the same pool")
	individualCount, err := env.client.Get(t.Context(), fmt.Sprintf("user_rate_limit:rpm:%d:%d", secondUser.UserID, now.Unix()/60)).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 1, individualCount, "a shared-pool rejection still counts as an individual attempt")

	otherGroup := UserRateLimitPolicy{UserID: 103, Group: "enterprise", RPMLimit: 5, SharedRPMLimit: 1}
	guard, apiErr = BeginUserRequestRateLimit(newUserRateLimitTestContext(t.Context()), otherGroup, "gpt-4o", UserConcurrencyWaitOptions{})
	require.Nil(t, apiErr)
	require.NotNil(t, guard, "different groups must use isolated shared counters")
	guard.Release()
}

func TestUserConcurrencyWaitsThenAcquiresReleasedSlot(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	config := userConcurrencyConfig{
		leaseTTL:     200 * time.Millisecond,
		renewEvery:   50 * time.Millisecond,
		waitTimeout:  300 * time.Millisecond,
		heartbeat:    20 * time.Millisecond,
		initialPoll:  time.Millisecond,
		maximumPoll:  5 * time.Millisecond,
		waitingLimit: 20,
	}

	first, apiErr := acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 14, 1, UserConcurrencyWaitOptions{}, &UserRateLimitObservation{}, config)
	require.Nil(t, apiErr)
	require.NotNil(t, first)

	type acquireResult struct {
		lease  *UserConcurrencyLease
		apiErr error
		wait   time.Duration
	}
	result := make(chan acquireResult, 1)
	go func() {
		observation := &UserRateLimitObservation{}
		lease, err := acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 14, 1, UserConcurrencyWaitOptions{}, observation, config)
		var resultErr error
		if err != nil {
			resultErr = err
		}
		result <- acquireResult{lease: lease, apiErr: resultErr, wait: observation.queueWait}
	}()

	require.Eventually(t, func() bool {
		count, err := env.client.ZCard(t.Context(), userConcurrencyWaitingKey(14)).Result()
		return err == nil && count == 1
	}, 100*time.Millisecond, time.Millisecond)
	first.Release()

	select {
	case acquired := <-result:
		require.NoError(t, acquired.apiErr)
		require.NotNil(t, acquired.lease)
		assert.Positive(t, acquired.wait)
		acquired.lease.Release()
	case <-time.After(time.Second):
		t.Fatal("waiting request did not acquire the released concurrency slot")
	}
	activeCount, err := env.client.ZCard(t.Context(), userConcurrencyActiveKey(14)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 0, activeCount)
}

func TestUserConcurrencyWaitingPoolLimitAndTimeout(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	now, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	score := float64(now.UnixMilli())
	env.server.ZAdd(userConcurrencyActiveKey(22), score, "active")
	for index := range userConcurrencyWaitingLimit {
		env.server.ZAdd(userConcurrencyWaitingKey(22), score, string(rune('a'+index)))
	}
	config := userConcurrencyConfig{
		leaseTTL:     time.Second,
		renewEvery:   time.Second,
		waitTimeout:  25 * time.Millisecond,
		heartbeat:    10 * time.Millisecond,
		initialPoll:  time.Millisecond,
		maximumPoll:  2 * time.Millisecond,
		waitingLimit: userConcurrencyWaitingLimit,
	}

	lease, apiErr := acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 22, 1, UserConcurrencyWaitOptions{}, &UserRateLimitObservation{}, config)
	assert.Nil(t, lease)
	require.NotNil(t, apiErr)
	assert.Equal(t, 429, apiErr.StatusCode)
	assert.EqualValues(t, "rate_limit_exceeded", apiErr.ToOpenAIError().Code)

	env.server.Del(userConcurrencyWaitingKey(22))
	observation := &UserRateLimitObservation{}
	started := time.Now()
	lease, apiErr = acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 22, 1, UserConcurrencyWaitOptions{}, observation, config)
	assert.Nil(t, lease)
	require.NotNil(t, apiErr)
	assert.GreaterOrEqual(t, time.Since(started), config.waitTimeout)
	assert.Positive(t, observation.queueWait)
	waitingCount, err := env.client.ZCard(t.Context(), userConcurrencyWaitingKey(22)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 0, waitingCount)
}

func TestUserConcurrencyReclaimsExpiredLeaseAndRenewsActiveLease(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	now, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	config := userConcurrencyConfig{
		leaseTTL:     30 * time.Millisecond,
		renewEvery:   5 * time.Millisecond,
		waitTimeout:  50 * time.Millisecond,
		heartbeat:    10 * time.Millisecond,
		initialPoll:  time.Millisecond,
		maximumPoll:  2 * time.Millisecond,
		waitingLimit: 20,
	}
	env.server.ZAdd(userConcurrencyActiveKey(31), float64(now.UnixMilli()-config.leaseTTL.Milliseconds()-1), "expired")

	lease, apiErr := acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 31, 1, UserConcurrencyWaitOptions{}, &UserRateLimitObservation{}, config)
	require.Nil(t, apiErr)
	require.NotNil(t, lease)
	initialScore, err := env.client.ZScore(t.Context(), userConcurrencyActiveKey(31), lease.leaseID).Result()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		currentScore, scoreErr := env.client.ZScore(t.Context(), userConcurrencyActiveKey(31), lease.leaseID).Result()
		return scoreErr == nil && currentScore > initialScore
	}, 100*time.Millisecond, 2*time.Millisecond)
	lease.Release()
}

func TestUserConcurrencyCancellationAndRedisFailure(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	now, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	require.NoError(t, env.client.ZAdd(t.Context(), userConcurrencyActiveKey(44), &redis.Z{Score: float64(now.UnixMilli()), Member: "active"}).Err())
	config := userConcurrencyConfig{
		leaseTTL:     time.Second,
		renewEvery:   time.Second,
		waitTimeout:  time.Second,
		heartbeat:    10 * time.Millisecond,
		initialPoll:  time.Millisecond,
		maximumPoll:  2 * time.Millisecond,
		waitingLimit: 20,
	}
	ctx, cancel := context.WithCancel(t.Context())
	c := newUserRateLimitTestContext(ctx)
	cancel()
	lease, apiErr := acquireUserConcurrency(c, 44, 1, UserConcurrencyWaitOptions{}, &UserRateLimitObservation{}, config)
	assert.Nil(t, lease)
	require.NotNil(t, apiErr)
	assert.EqualValues(t, "client_disconnected", apiErr.ToOpenAIError().Code)

	require.NoError(t, env.client.Close())
	observation := &UserRateLimitObservation{}
	lease, apiErr = acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 45, 1, UserConcurrencyWaitOptions{}, observation, config)
	assert.Nil(t, lease)
	assert.Nil(t, apiErr)
	assert.True(t, observation.redisFailOpen["concurrency"])
}

func TestGroupConcurrencyLeaseIsAtomicAcrossUsersAndReleasesBothLayers(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	config := userConcurrencyConfig{
		leaseTTL:          200 * time.Millisecond,
		renewEvery:        50 * time.Millisecond,
		waitTimeout:       300 * time.Millisecond,
		heartbeat:         20 * time.Millisecond,
		initialPoll:       time.Millisecond,
		maximumPoll:       5 * time.Millisecond,
		waitingLimit:      userConcurrencyWaitingLimit,
		groupWaitingLimit: groupConcurrencyWaitingLimit,
	}
	firstPolicy := UserRateLimitPolicy{UserID: 201, Group: "vip", ConcurrencyLimit: 1, SharedConcurrencyLimit: 1}
	first, apiErr := acquireRequestConcurrency(newUserRateLimitTestContext(t.Context()), firstPolicy, UserConcurrencyWaitOptions{}, &UserRateLimitObservation{}, config)
	require.Nil(t, apiErr)
	require.NotNil(t, first)

	secondPolicy := UserRateLimitPolicy{UserID: 202, Group: "vip", ConcurrencyLimit: 1, SharedConcurrencyLimit: 1}
	type groupAcquireResult struct {
		lease  *UserConcurrencyLease
		apiErr *types.NewAPIError
	}
	result := make(chan groupAcquireResult, 1)
	go func() {
		lease, err := acquireRequestConcurrency(newUserRateLimitTestContext(t.Context()), secondPolicy, UserConcurrencyWaitOptions{}, &UserRateLimitObservation{}, config)
		result <- groupAcquireResult{lease: lease, apiErr: err}
	}()

	require.Eventually(t, func() bool {
		userWaiting, userErr := env.client.ZCard(t.Context(), userConcurrencyWaitingKey(secondPolicy.UserID)).Result()
		groupWaiting, groupErr := env.client.ZCard(t.Context(), groupConcurrencyWaitingKey(secondPolicy.Group)).Result()
		return userErr == nil && groupErr == nil && userWaiting == 1 && groupWaiting == 1
	}, 100*time.Millisecond, time.Millisecond)
	first.Release()

	acquired := <-result
	require.Nil(t, acquired.apiErr)
	require.NotNil(t, acquired.lease)
	userActive, err := env.client.ZCard(t.Context(), userConcurrencyActiveKey(secondPolicy.UserID)).Result()
	require.NoError(t, err)
	groupActive, err := env.client.ZCard(t.Context(), groupConcurrencyActiveKey(secondPolicy.Group)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 1, userActive)
	assert.EqualValues(t, 1, groupActive)
	acquired.lease.Release()
	userActive, err = env.client.ZCard(t.Context(), userConcurrencyActiveKey(secondPolicy.UserID)).Result()
	require.NoError(t, err)
	groupActive, err = env.client.ZCard(t.Context(), groupConcurrencyActiveKey(secondPolicy.Group)).Result()
	require.NoError(t, err)
	assert.Zero(t, userActive)
	assert.Zero(t, groupActive)
}

func TestGroupConcurrencyWaitingPoolRejectsWithoutPartialUserWaiter(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	now, err := env.client.Time(t.Context()).Result()
	require.NoError(t, err)
	score := float64(now.UnixMilli())
	group := "vip"
	env.server.ZAdd(groupConcurrencyActiveKey(group), score, "active")
	for index := range groupConcurrencyWaitingLimit {
		env.server.ZAdd(groupConcurrencyWaitingKey(group), score, fmt.Sprintf("waiting-%d", index))
	}
	config := userConcurrencyConfig{
		leaseTTL:          time.Second,
		renewEvery:        time.Second,
		waitTimeout:       25 * time.Millisecond,
		heartbeat:         10 * time.Millisecond,
		initialPoll:       time.Millisecond,
		maximumPoll:       2 * time.Millisecond,
		waitingLimit:      userConcurrencyWaitingLimit,
		groupWaitingLimit: groupConcurrencyWaitingLimit,
	}
	policy := UserRateLimitPolicy{UserID: 301, Group: group, ConcurrencyLimit: 1, SharedConcurrencyLimit: 1}

	lease, apiErr := acquireRequestConcurrency(newUserRateLimitTestContext(t.Context()), policy, UserConcurrencyWaitOptions{}, &UserRateLimitObservation{}, config)
	assert.Nil(t, lease)
	require.NotNil(t, apiErr)
	userWaiting, err := env.client.ZCard(t.Context(), userConcurrencyWaitingKey(policy.UserID)).Result()
	require.NoError(t, err)
	assert.Zero(t, userWaiting, "the atomic script must not leave a user waiter when the group queue is full")
}

type recordingStreamWaiter struct {
	mu    sync.Mutex
	calls []int
	err   error
}

type gatedStreamWaiter struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (w *gatedStreamWaiter) WaitN(ctx context.Context, _ int) error {
	w.started <- struct{}{}
	select {
	case <-w.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *recordingStreamWaiter) WaitN(ctx context.Context, tokens int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.err != nil {
		return w.err
	}
	w.mu.Lock()
	w.calls = append(w.calls, tokens)
	w.mu.Unlock()
	return nil
}

func TestUserStreamPacerUsesOneSecondBurstWithoutSplittingPayload(t *testing.T) {
	waiter := &recordingStreamWaiter{}
	observation := &UserRateLimitObservation{}
	pacer := newUserStreamPacerWithWaiter(2, "gpt-4o", observation, waiter)
	payload := []byte(`{"choices":[{"delta":{"content":"one two three four five six"}}]}`)

	require.NoError(t, pacer.PacePayload(t.Context(), payload))
	require.Greater(t, len(waiter.calls), 1)
	total := 0
	for _, call := range waiter.calls {
		assert.LessOrEqual(t, call, 2)
		total += call
	}
	assert.EqualValues(t, total, observation.pacingTokens)
	assert.NotEmpty(t, payload, "pacing waits must not mutate or split the protocol event")
}

func TestUserStreamPacerBypassesNonTextAndHonorsCancellation(t *testing.T) {
	waiter := &recordingStreamWaiter{}
	pacer := newUserStreamPacerWithWaiter(1, "gpt-4o", &UserRateLimitObservation{}, waiter)
	for _, payload := range [][]byte{
		[]byte("[DONE]"),
		[]byte(`{"type":"response.completed","response":{"usage":{"output_tokens":3}}}`),
		[]byte(`{"type":"response.audio.delta","delta":"base64-audio"}`),
		[]byte(`{"choices":[{"delta":{"role":"assistant"},"finish_reason":"stop"}]}`),
	} {
		require.NoError(t, pacer.PacePayload(t.Context(), payload))
	}
	assert.Empty(t, waiter.calls)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := pacer.PacePayload(ctx, []byte(`{"choices":[{"delta":{"content":"cancel me"}}]}`))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestStreamPayloadTextExtractsClientVisibleTextOnly(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "chat content and tool arguments", payload: `{"choices":[{"delta":{"content":"hello","tool_calls":[{"function":{"arguments":"{\"x\":1}"}}]}}]}`, want: `hello{"x":1}`},
		{name: "responses text delta", payload: `{"type":"response.output_text.delta","delta":"answer"}`, want: "answer"},
		{name: "claude thinking and partial json", payload: `{"type":"content_block_delta","delta":{"thinking":"reason","partial_json":"{\"x\":"}}`, want: `reason{"x":`},
		{name: "gemini text and function args", payload: `{"candidates":[{"content":{"parts":[{"text":"gemini","functionCall":{"args":{"city":"Paris"}}}]}}]}`, want: `gemini{"city":"Paris"}`},
		{name: "realtime audio bytes", payload: `{"type":"response.audio.delta","delta":"ignored"}`, want: ""},
		{name: "usage only", payload: `{"usage":{"output_tokens":10}}`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, streamPayloadText([]byte(test.payload)))
		})
	}
}

func TestUserStreamPacerPropagatesWaiterError(t *testing.T) {
	wantErr := errors.New("wait failed")
	pacer := newUserStreamPacerWithWaiter(1, "gpt-4o", &UserRateLimitObservation{}, &recordingStreamWaiter{err: wantErr})
	assert.ErrorIs(t, pacer.PacePayload(t.Context(), []byte(`{"choices":[{"delta":{"content":"text"}}]}`)), wantErr)
}

func TestStreamRateLayersWaitConcurrently(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	waiters := []streamRateWaiter{
		&gatedStreamWaiter{started: started, release: release},
		&gatedStreamWaiter{started: started, release: release},
	}
	result := make(chan error, 1)
	go func() {
		result <- waitForStreamRates(t.Context(), waiters, 1)
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("both rate layers must begin waiting before either is released")
		}
	}
	close(release)
	assert.NoError(t, <-result)
}

func TestGroupStreamTokenBucketSharesBurstAndIsolatesGroups(t *testing.T) {
	useUserRateLimitMiniRedis(t)

	wait, err := reserveGroupStreamTokens(t.Context(), "vip", 10, 10)
	require.NoError(t, err)
	assert.Zero(t, wait)
	wait, err = reserveGroupStreamTokens(t.Context(), "vip", 10, 10)
	require.NoError(t, err)
	assert.InDelta(t, time.Second, wait, float64(10*time.Millisecond))

	wait, err = reserveGroupStreamTokens(t.Context(), "enterprise", 10, 10)
	require.NoError(t, err)
	assert.Zero(t, wait, "different groups must have independent distributed buckets")
}

func TestGroupStreamTokenBucketRedisFailureIsFailOpen(t *testing.T) {
	env := useUserRateLimitMiniRedis(t)
	require.NoError(t, env.client.Close())
	observation := &UserRateLimitObservation{}
	waiter := &groupStreamRateWaiter{
		c:           newUserRateLimitTestContext(t.Context()),
		group:       "vip",
		limit:       10,
		observation: observation,
	}
	require.NoError(t, waiter.WaitN(t.Context(), 1))
	assert.True(t, observation.redisFailOpen["shared_tps"])
}
