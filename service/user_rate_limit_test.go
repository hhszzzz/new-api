package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useUserRateLimitMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled := common.RedisEnabled
	previousClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
	})
	return server
}

func newUserRateLimitTestContext(ctx context.Context) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	c.Set(common.RequestIdKey, common.NewRequestId())
	return c
}

func TestUserRPMFixedMinuteWindowAndSharedUserCounter(t *testing.T) {
	server := useUserRateLimitMiniRedis(t)

	for range 2 {
		allowed, err := checkUserRPM(t.Context(), 77, 2)
		require.NoError(t, err)
		assert.True(t, allowed)
	}
	allowed, err := checkUserRPM(t.Context(), 77, 2)
	require.NoError(t, err)
	assert.False(t, allowed, "all tokens for one user must share the same RPM counter")

	keys := server.Keys()
	require.Len(t, keys, 1)
	assert.True(t, strings.HasPrefix(keys[0], "user_rate_limit:rpm:77:"))
	assert.Equal(t, 120*time.Second, server.TTL(keys[0]))

	redisNow, err := common.RDB.Time(t.Context()).Result()
	require.NoError(t, err)
	server.SetTime(redisNow.Add(61 * time.Second))
	allowed, err = checkUserRPM(t.Context(), 77, 2)
	require.NoError(t, err)
	assert.True(t, allowed, "a new Redis-server minute must use a fresh counter")
}

func TestUserRPMRedisFailureIsFailOpen(t *testing.T) {
	useUserRateLimitMiniRedis(t)
	require.NoError(t, common.RDB.Close())

	allowed, err := checkUserRPM(t.Context(), 91, 1)
	assert.True(t, allowed)
	assert.Error(t, err)
}

func TestUserConcurrencyWaitsThenAcquiresReleasedSlot(t *testing.T) {
	useUserRateLimitMiniRedis(t)
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
		count, err := common.RDB.ZCard(t.Context(), userConcurrencyWaitingKey(14)).Result()
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
	activeCount, err := common.RDB.ZCard(t.Context(), userConcurrencyActiveKey(14)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 0, activeCount)
}

func TestUserConcurrencyWaitingPoolLimitAndTimeout(t *testing.T) {
	server := useUserRateLimitMiniRedis(t)
	now, err := common.RDB.Time(t.Context()).Result()
	require.NoError(t, err)
	score := float64(now.UnixMilli())
	server.ZAdd(userConcurrencyActiveKey(22), score, "active")
	for index := range userConcurrencyWaitingLimit {
		server.ZAdd(userConcurrencyWaitingKey(22), score, string(rune('a'+index)))
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

	server.Del(userConcurrencyWaitingKey(22))
	observation := &UserRateLimitObservation{}
	started := time.Now()
	lease, apiErr = acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 22, 1, UserConcurrencyWaitOptions{}, observation, config)
	assert.Nil(t, lease)
	require.NotNil(t, apiErr)
	assert.GreaterOrEqual(t, time.Since(started), config.waitTimeout)
	assert.Positive(t, observation.queueWait)
	waitingCount, err := common.RDB.ZCard(t.Context(), userConcurrencyWaitingKey(22)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 0, waitingCount)
}

func TestUserConcurrencyReclaimsExpiredLeaseAndRenewsActiveLease(t *testing.T) {
	server := useUserRateLimitMiniRedis(t)
	now, err := common.RDB.Time(t.Context()).Result()
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
	server.ZAdd(userConcurrencyActiveKey(31), float64(now.UnixMilli()-config.leaseTTL.Milliseconds()-1), "expired")

	lease, apiErr := acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 31, 1, UserConcurrencyWaitOptions{}, &UserRateLimitObservation{}, config)
	require.Nil(t, apiErr)
	require.NotNil(t, lease)
	initialScore, err := common.RDB.ZScore(t.Context(), userConcurrencyActiveKey(31), lease.leaseID).Result()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		currentScore, scoreErr := common.RDB.ZScore(t.Context(), userConcurrencyActiveKey(31), lease.leaseID).Result()
		return scoreErr == nil && currentScore > initialScore
	}, 100*time.Millisecond, 2*time.Millisecond)
	lease.Release()
}

func TestUserConcurrencyCancellationAndRedisFailure(t *testing.T) {
	useUserRateLimitMiniRedis(t)
	now, err := common.RDB.Time(t.Context()).Result()
	require.NoError(t, err)
	require.NoError(t, common.RDB.ZAdd(t.Context(), userConcurrencyActiveKey(44), &redis.Z{Score: float64(now.UnixMilli()), Member: "active"}).Err())
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

	require.NoError(t, common.RDB.Close())
	observation := &UserRateLimitObservation{}
	lease, apiErr = acquireUserConcurrency(newUserRateLimitTestContext(t.Context()), 45, 1, UserConcurrencyWaitOptions{}, observation, config)
	assert.Nil(t, lease)
	assert.Nil(t, apiErr)
	assert.True(t, observation.redisFailOpen["concurrency"])
}

type recordingStreamWaiter struct {
	mu    sync.Mutex
	calls []int
	err   error
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
