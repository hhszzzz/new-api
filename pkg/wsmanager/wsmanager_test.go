package wsmanager

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetRegistryForTest(t *testing.T) {
	t.Helper()
	mu.Lock()
	registry = map[int]map[uint64]*entry{}
	nextID = 0
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		registry = map[int]map[uint64]*entry{}
		mu.Unlock()
	})
}

func TestCloseChannelsFiltersIDsAndUsesDefaultReason(t *testing.T) {
	resetRegistryForTest(t)

	reasons := make(chan string, 2)
	Register(10, KindRealtime, func(reason string) { reasons <- reason })
	Register(10, KindResponses, func(reason string) { reasons <- reason })
	otherCalls := 0
	Register(20, KindRealtime, func(string) { otherCalls++ })

	assert.Equal(t, 2, CloseChannels([]int{0, 10, 10, -1}, ""))
	assert.Equal(t, defaultCloseReason, <-reasons)
	assert.Equal(t, defaultCloseReason, <-reasons)
	assert.Equal(t, 0, otherCalls)
	assert.Equal(t, 0, CloseChannel(10, "again"))
	assert.Equal(t, 1, CloseChannel(20, "cleanup"))
}

func TestRegisterAndUnregisterAreIdempotent(t *testing.T) {
	resetRegistryForTest(t)

	var calls atomic.Int32
	unregister := Register(10, KindRealtime, func(string) { calls.Add(1) })
	unregister()
	unregister()

	assert.Equal(t, 0, CloseChannel(10, "test"))
	assert.Equal(t, int32(0), calls.Load())
	assert.NotPanics(t, func() {
		Register(0, KindRealtime, nil)()
	})
}

func TestConcurrentCloseAndUnregisterInvokeCallbackAtMostOnce(t *testing.T) {
	resetRegistryForTest(t)

	var calls atomic.Int32
	unregister := Register(10, KindResponses, func(string) { calls.Add(1) })
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		unregister()
	}()
	go func() {
		defer wg.Done()
		<-start
		CloseChannel(10, "test")
	}()
	close(start)
	wg.Wait()

	assert.LessOrEqual(t, calls.Load(), int32(1))
	assert.Equal(t, 0, CloseChannel(10, "again"))
}

func TestPublishCloseChannelsNoopsWithoutRedisClient(t *testing.T) {
	resetRegistryForTest(t)

	previousEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousRDB
	})

	require.NoError(t, PublishCloseChannels(context.Background(), []int{10}, "test"))
}

func TestSubscriberClosesRemoteEventsAndStopsOnCancellation(t *testing.T) {
	resetRegistryForTest(t)

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	subscriberOnce = sync.Once{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		require.NoError(t, client.Close())
		common.RedisEnabled = previousEnabled
		common.RDB = previousRDB
		subscriberOnce = sync.Once{}
	})

	closed := make(chan string, 1)
	Register(30, KindResponses, func(reason string) { closed <- reason })
	StartSubscriber(ctx)
	require.Eventually(t, func() bool {
		return server.PubSubNumSub(redisChannel)[redisChannel] == 1
	}, time.Second, 10*time.Millisecond)

	sameOriginPayload, err := common.Marshal(closeEvent{
		ChannelIDs: []int{30},
		Reason:     "same origin",
		Origin:     getOriginID(),
	})
	require.NoError(t, err)
	require.NoError(t, client.Publish(context.Background(), redisChannel, sameOriginPayload).Err())
	select {
	case reason := <-closed:
		t.Fatalf("same-origin event unexpectedly closed connection: %s", reason)
	case <-time.After(100 * time.Millisecond):
	}

	remotePayload, err := common.Marshal(closeEvent{
		ChannelIDs: []int{30},
		Reason:     "remote disable",
		Origin:     "remote-node",
	})
	require.NoError(t, err)
	require.NoError(t, client.Publish(context.Background(), redisChannel, remotePayload).Err())
	select {
	case reason := <-closed:
		assert.Equal(t, "remote disable", reason)
	case <-time.After(time.Second):
		t.Fatal("remote close event was not delivered")
	}

	cancel()
	require.Eventually(t, func() bool {
		return server.PubSubNumSub(redisChannel)[redisChannel] == 0
	}, time.Second, 10*time.Millisecond)
}
