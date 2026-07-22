package perfmetrics

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisPublisherStoresIdempotentPerInstanceSnapshots(t *testing.T) {
	setupStatusTestDB(t)
	setupStatusTestRedis(t)
	previousWriterID := redisWriterID
	redisWriterID = "local-test-instance"
	t.Cleanup(func() {
		redisWriterID = previousWriterID
	})

	key := bucketKey{model: "model:with:separator", group: "group:with:separator", bucketTs: 123}
	bucket := &atomicBucket{}
	bucket.add(Sample{
		Success:      true,
		LatencyMs:    120,
		HasTtft:      true,
		TtftMs:       30,
		OutputTokens: 24,
		GenerationMs: 600,
	})
	markRedisBucketDirty(key, bucket)
	publishRedisDirtyBuckets()

	values, err := common.RDB.HGetAll(context.Background(), redisInstanceBucketKey(key)).Result()
	require.NoError(t, err)
	assert.Equal(t, counters{
		requestCount:   1,
		successCount:   1,
		totalLatencyMs: 120,
		ttftSumMs:      30,
		ttftCount:      1,
		outputTokens:   24,
		generationMs:   600,
	}, redisInstanceCounters(values)[redisWriterID])

	bucket.add(Sample{LatencyMs: 80})
	markRedisBucketDirty(key, bucket)
	publishRedisDirtyBuckets()

	values, err = common.RDB.HGetAll(context.Background(), redisInstanceBucketKey(key)).Result()
	require.NoError(t, err)
	assert.Equal(t, counters{
		requestCount:   2,
		successCount:   1,
		totalLatencyMs: 200,
		ttftSumMs:      30,
		ttftCount:      1,
		outputTokens:   24,
		generationMs:   600,
	}, redisInstanceCounters(values)[redisWriterID])
	assert.False(t, common.RDB.Exists(context.Background(), redisBucketKey(key)).Val() > 0)
}

func TestRedisPublisherRetainsDirtyBucketAfterWriteFailure(t *testing.T) {
	setupStatusTestDB(t)
	redisServer := setupStatusTestRedis(t)
	redisDirtyBuckets = sync.Map{}

	key := bucketKey{model: "retry-model", group: "default", bucketTs: 456}
	bucket := &atomicBucket{}
	bucket.add(Sample{Success: true, LatencyMs: 100})
	markRedisBucketDirty(key, bucket)
	redisServer.Close()

	publishRedisDirtyBuckets()
	stored, ok := redisDirtyBuckets.Load(key)
	require.True(t, ok)
	assert.Same(t, bucket, stored)
}

func TestRedisInstanceBucketKeyDoesNotCollideAcrossModelAndGroupSeparators(t *testing.T) {
	leftKey := bucketKey{model: "model:a", group: "b", bucketTs: 123}
	rightKey := bucketKey{model: "model", group: "a:b", bucketTs: 123}
	left := redisInstanceBucketKey(leftKey)
	right := redisInstanceBucketKey(rightKey)
	assert.NotEqual(t, left, right)
	parsedLeft, ok := parseRedisInstanceBucketKey(left)
	require.True(t, ok)
	assert.Equal(t, leftKey, parsedLeft)
	parsedRight, ok := parseRedisInstanceBucketKey(right)
	require.True(t, ok)
	assert.Equal(t, rightKey, parsedRight)
}

func TestCompletedBucketFlushPersistsWriterWithoutDoubleCountingRedisSnapshot(t *testing.T) {
	db := setupStatusTestDB(t)
	withStatusBucketTime(t, "minute")
	setupStatusTestRedis(t)
	previousWriterID := redisWriterID
	redisWriterID = "flush-test-instance"
	t.Cleanup(func() {
		redisWriterID = previousWriterID
	})

	key := bucketKey{
		model:    "flush-instance-model",
		group:    "default",
		bucketTs: bucketStart(time.Now().Add(-2 * time.Minute).Unix()),
	}
	bucket := &atomicBucket{}
	bucket.add(Sample{Success: true, LatencyMs: 100})
	bucket.add(Sample{Success: false, LatencyMs: 300})
	hotBuckets.Store(key, bucket)
	markRedisBucketDirty(key, bucket)
	publishRedisDirtyBuckets()

	flushCompletedBuckets()

	var persisted model.PerfMetricInstance
	require.NoError(t, db.Where(&model.PerfMetricInstance{
		WriterID:  redisWriterID,
		ModelName: key.model,
		Group:     key.group,
		BucketTs:  key.bucketTs,
	}).First(&persisted).Error)
	assert.Equal(t, int64(2), persisted.RequestCount)
	assert.Equal(t, int64(1), persisted.SuccessCount)

	values, err := common.RDB.HGetAll(context.Background(), redisInstanceBucketKey(key)).Result()
	require.NoError(t, err)
	redisValue := redisInstanceCounters(values)[redisWriterID]
	assert.Equal(t, int64(2), redisValue.requestCount)
	merged := mergeStatusBuckets(
		statusInstanceBuckets{key: {redisWriterID: {
			requestCount:   persisted.RequestCount,
			successCount:   persisted.SuccessCount,
			totalLatencyMs: persisted.TotalLatencyMs,
		}}},
		map[bucketKey]redisBucketValues{key: {instances: map[string]counters{redisWriterID: redisValue}}},
		nil,
	)
	assert.Equal(t, int64(2), merged[key].requestCount)
	assert.Equal(t, int64(1), merged[key].successCount)
	assert.Equal(t, int64(400), merged[key].totalLatencyMs)
}

func TestRedisPublisherKeepsLifetimeTotalAfterDrain(t *testing.T) {
	setupStatusTestDB(t)
	setupStatusTestRedis(t)
	previousWriterID := redisWriterID
	redisWriterID = "late-record-instance"
	t.Cleanup(func() {
		redisWriterID = previousWriterID
	})

	key := bucketKey{model: "late-record-model", group: "default", bucketTs: 789}
	bucket := &atomicBucket{}
	bucket.add(Sample{Success: true, LatencyMs: 100})
	markRedisBucketDirty(key, bucket)
	publishRedisDirtyBuckets()

	require.Equal(t, int64(1), bucket.drain().requestCount)
	bucket.add(Sample{Success: false, LatencyMs: 300})
	markRedisBucketDirty(key, bucket)
	publishRedisDirtyBuckets()

	values, err := common.RDB.HGetAll(context.Background(), redisInstanceBucketKey(key)).Result()
	require.NoError(t, err)
	assert.Equal(t, counters{
		requestCount:   2,
		successCount:   1,
		totalLatencyMs: 400,
	}, redisInstanceCounters(values)[redisWriterID])
}

func TestCrashedWriterRedisSnapshotCoversEntireStatusWindow(t *testing.T) {
	setupStatusTestDB(t)
	withStatusBucketTime(t, "minute")
	redisServer := setupStatusTestRedis(t)
	previousWriterID := redisWriterID
	redisWriterID = "crashed-writer"
	t.Cleanup(func() {
		redisWriterID = previousWriterID
	})

	now := time.Now().UTC()
	key := bucketKey{
		model:    "crash-before-flush-model",
		group:    "default",
		bucketTs: bucketStart(now.Add(-time.Minute).Unix()),
	}
	bucket := &atomicBucket{}
	bucket.add(Sample{Success: true, LatencyMs: 250})
	markRedisBucketDirty(key, bucket)
	publishRedisDirtyBuckets()

	// Simulate another instance reading after the writer died before its DB
	// flush. Only the published Redis lifetime snapshot remains.
	hotBuckets = sync.Map{}
	redisWriterID = "status-reader"
	result, err := queryStatusAt([]StatusModelSource{{ModelName: key.model}}, []string{key.group}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	assert.Equal(t, StatusOperational, result.Models[0].Status)
	require.NotNil(t, result.Models[0].AvgLatencyMs)
	assert.Equal(t, int64(250), *result.Models[0].AvgLatencyMs)
	assert.GreaterOrEqual(t, redisServer.TTL(redisInstanceBucketKey(key)), 24*time.Hour)
	assert.GreaterOrEqual(t, redisServer.TTL(redisBucketIndexKey), 24*time.Hour)
}
