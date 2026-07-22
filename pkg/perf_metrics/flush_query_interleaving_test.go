package perfmetrics

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func requirePerfMetricTestSignal(t *testing.T, signal <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", stage)
	}
}

func requirePerfMetricFlushWriterQueued(t *testing.T) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		if !bucketFlushMu.TryRLock() {
			return
		}
		bucketFlushMu.RUnlock()
		select {
		case <-timer.C:
			t.Fatal("timed out waiting for completed-bucket flush to wait on query snapshot")
		default:
			runtime.Gosched()
		}
	}
}

func TestQueryKeepsCompletedHotBucketWhileFlushWaitsForSnapshot(t *testing.T) {
	db := setupStatusTestDB(t)
	withStatusBucketTime(t, "minute")
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})

	key := bucketKey{
		model:    "flush-query-detail-model",
		group:    "default",
		bucketTs: bucketStart(time.Now().Add(-2 * time.Minute).Unix()),
	}
	bucket := &atomicBucket{}
	bucket.add(Sample{Success: true, LatencyMs: 100})
	bucket.add(Sample{Success: false, LatencyMs: 300})
	hotBuckets.Store(key, bucket)

	queryReached := make(chan struct{})
	allowQuery := make(chan struct{})
	var blockOnce sync.Once
	var releaseOnce sync.Once
	releaseQuery := func() {
		releaseOnce.Do(func() {
			close(allowQuery)
		})
	}
	const callbackName = "test:pause_perf_metric_detail_query"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != (model.PerfMetricInstance{}).TableName() {
			return
		}
		blockOnce.Do(func() {
			close(queryReached)
			select {
			case <-allowQuery:
			case <-time.After(5 * time.Second):
				t.Error("timed out waiting to release the perf metric query")
			}
		})
	}))
	t.Cleanup(func() {
		releaseQuery()
		require.NoError(t, db.Callback().Query().Remove(callbackName))
	})

	type queryResponse struct {
		result QueryResult
		err    error
	}
	queryDone := make(chan queryResponse, 1)
	go func() {
		result, err := Query(QueryParams{Model: key.model, Group: key.group, Hours: 1})
		queryDone <- queryResponse{result: result, err: err}
	}()
	requirePerfMetricTestSignal(t, queryReached, "the detail query database read")

	flushStarted := make(chan struct{})
	flushDone := make(chan struct{})
	go func() {
		close(flushStarted)
		flushCompletedBuckets()
		close(flushDone)
	}()
	requirePerfMetricTestSignal(t, flushStarted, "the completed bucket flush start")
	requirePerfMetricFlushWriterQueued(t)
	releaseQuery()

	var response queryResponse
	select {
	case response = <-queryDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the detail query")
	}
	require.NoError(t, response.err)
	require.Len(t, response.result.Groups, 1)
	assert.Equal(t, int64(200), response.result.Groups[0].AvgLatencyMs)
	assert.Equal(t, 50.0, response.result.Groups[0].SuccessRate)
	requirePerfMetricTestSignal(t, flushDone, "the completed bucket flush")

	var persisted model.PerfMetricInstance
	require.NoError(t, db.Where(&model.PerfMetricInstance{
		WriterID:  redisWriterID,
		ModelName: key.model,
		Group:     key.group,
		BucketTs:  key.bucketTs,
	}).First(&persisted).Error)
	assert.Equal(t, int64(2), persisted.RequestCount)
	assert.Zero(t, bucket.snapshot().requestCount)
}

func TestQuerySummaryAllKeepsCompletedHotBucketWhileFlushWaitsForSnapshot(t *testing.T) {
	db := setupStatusTestDB(t)
	withStatusBucketTime(t, "minute")
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})

	key := bucketKey{
		model:    "flush-query-summary-model",
		group:    "default",
		bucketTs: bucketStart(time.Now().Add(-2 * time.Minute).Unix()),
	}
	bucket := &atomicBucket{}
	bucket.add(Sample{Success: true, LatencyMs: 100})
	bucket.add(Sample{Success: false, LatencyMs: 300})
	hotBuckets.Store(key, bucket)

	queryReached := make(chan struct{})
	allowQuery := make(chan struct{})
	var blockOnce sync.Once
	var releaseOnce sync.Once
	releaseQuery := func() {
		releaseOnce.Do(func() {
			close(allowQuery)
		})
	}
	const callbackName = "test:pause_perf_metric_summary_query"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != (model.PerfMetricInstance{}).TableName() {
			return
		}
		blockOnce.Do(func() {
			close(queryReached)
			select {
			case <-allowQuery:
			case <-time.After(5 * time.Second):
				t.Error("timed out waiting to release the perf metric summary query")
			}
		})
	}))
	t.Cleanup(func() {
		releaseQuery()
		require.NoError(t, db.Callback().Query().Remove(callbackName))
	})

	type queryResponse struct {
		result SummaryAllResult
		err    error
	}
	queryDone := make(chan queryResponse, 1)
	go func() {
		result, err := QuerySummaryAll(1, []string{key.group})
		queryDone <- queryResponse{result: result, err: err}
	}()
	requirePerfMetricTestSignal(t, queryReached, "the summary query database read")

	flushStarted := make(chan struct{})
	flushDone := make(chan struct{})
	go func() {
		close(flushStarted)
		flushCompletedBuckets()
		close(flushDone)
	}()
	requirePerfMetricTestSignal(t, flushStarted, "the completed bucket flush start")
	requirePerfMetricFlushWriterQueued(t)
	releaseQuery()

	var response queryResponse
	select {
	case response = <-queryDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the summary query")
	}
	require.NoError(t, response.err)
	require.Len(t, response.result.Models, 1)
	assert.Equal(t, int64(2), response.result.Models[0].RequestCount)
	assert.Equal(t, int64(200), response.result.Models[0].AvgLatencyMs)
	assert.Equal(t, 50.0, response.result.Models[0].SuccessRate)
	requirePerfMetricTestSignal(t, flushDone, "the completed bucket flush")

	var persisted model.PerfMetricInstance
	require.NoError(t, db.Where(&model.PerfMetricInstance{
		WriterID:  redisWriterID,
		ModelName: key.model,
		Group:     key.group,
		BucketTs:  key.bucketTs,
	}).First(&persisted).Error)
	assert.Equal(t, int64(2), persisted.RequestCount)
	assert.Zero(t, bucket.snapshot().requestCount)
}

func TestFlushKeepsStatusReadsConsistentWhileMovingCompletedBucket(t *testing.T) {
	db := setupStatusTestDB(t)
	withStatusBucketTime(t, "minute")
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})

	now := time.Now().UTC()
	key := bucketKey{
		model:    "flush-query-model",
		group:    "default",
		bucketTs: bucketStart(now.Add(-2 * time.Minute).Unix()),
	}
	bucket := &atomicBucket{}
	bucket.add(Sample{Success: true, LatencyMs: 100})
	bucket.add(Sample{Success: false, LatencyMs: 300})
	hotBuckets.Store(key, bucket)

	before, err := queryStatusAt([]StatusModelSource{{ModelName: key.model}}, []string{key.group}, now)
	require.NoError(t, err)
	require.Len(t, before.Models, 1)
	require.NotNil(t, before.Models[0].SuccessRate)
	require.NotNil(t, before.Models[0].AvgLatencyMs)
	assert.Equal(t, StatusDegraded, before.Models[0].Status)
	assert.Equal(t, 50.0, *before.Models[0].SuccessRate)
	assert.Equal(t, int64(200), *before.Models[0].AvgLatencyMs)

	createReached := make(chan struct{})
	allowCreate := make(chan struct{})
	var blockOnce sync.Once
	var releaseOnce sync.Once
	releaseCreate := func() {
		releaseOnce.Do(func() {
			close(allowCreate)
		})
	}
	const callbackName = "test:pause_perf_metric_flush"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != (model.PerfMetricInstance{}).TableName() {
			return
		}
		blockOnce.Do(func() {
			close(createReached)
			select {
			case <-allowCreate:
			case <-time.After(5 * time.Second):
				t.Error("timed out waiting to release the completed bucket upsert")
			}
		})
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Create().Remove(callbackName))
	})
	t.Cleanup(releaseCreate)

	flushDone := make(chan struct{})
	go func() {
		flushCompletedBuckets()
		close(flushDone)
	}()
	requirePerfMetricTestSignal(t, createReached, "the completed bucket upsert")

	if bucketFlushMu.TryRLock() {
		bucketFlushMu.RUnlock()
		assert.Fail(t, "status read entered after a bucket was drained but before it was persisted")
	}
	assert.Zero(t, bucket.snapshot().requestCount)
	releaseCreate()
	requirePerfMetricTestSignal(t, flushDone, "the completed bucket flush")

	require.True(t, bucketFlushMu.TryRLock())
	bucketFlushMu.RUnlock()

	var persisted model.PerfMetricInstance
	require.NoError(t, db.Where(&model.PerfMetricInstance{
		WriterID:  redisWriterID,
		ModelName: key.model,
		Group:     key.group,
		BucketTs:  key.bucketTs,
	}).First(&persisted).Error)
	assert.Equal(t, int64(2), persisted.RequestCount)
	assert.Equal(t, int64(1), persisted.SuccessCount)
	assert.Equal(t, int64(400), persisted.TotalLatencyMs)
	assert.Zero(t, bucket.snapshot().requestCount)

	after, err := queryStatusAt([]StatusModelSource{{ModelName: key.model}}, []string{key.group}, now)
	require.NoError(t, err)
	require.Len(t, after.Models, 1)
	require.NotNil(t, after.Models[0].SuccessRate)
	require.NotNil(t, after.Models[0].AvgLatencyMs)
	assert.Equal(t, before.Models[0].Status, after.Models[0].Status)
	assert.Equal(t, *before.Models[0].SuccessRate, *after.Models[0].SuccessRate)
	assert.Equal(t, *before.Models[0].AvgLatencyMs, *after.Models[0].AvgLatencyMs)
}
