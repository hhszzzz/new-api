package perfmetrics

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupStatusTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousIsMasterNode := common.IsMasterNode
	previousSQLitePath := common.SQLitePath
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.IsMasterNode = false
	common.SQLitePath = dsn
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Setenv("SQL_DSN", "local")
	require.NoError(t, model.InitDB())
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))
	hotBuckets = sync.Map{}
	redisDirtyBuckets = sync.Map{}

	t.Cleanup(func() {
		hotBuckets = sync.Map{}
		redisDirtyBuckets = sync.Map{}
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.IsMasterNode = previousIsMasterNode
		common.SQLitePath = previousSQLitePath
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func resetStatusResultCacheForTest(t *testing.T) {
	t.Helper()
	statusResultCache.Lock()
	clear(statusResultCache.entries)
	clear(statusResultCache.calls)
	statusResultCache.Unlock()
	t.Cleanup(func() {
		statusResultCache.Lock()
		clear(statusResultCache.entries)
		clear(statusResultCache.calls)
		statusResultCache.Unlock()
	})
}

func setupStatusTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()

	redisServer := miniredis.RunT(t)
	previousRedisEnabled, previousRedis := common.RedisEnabled, common.RDB
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRedis
	})
	return redisServer
}

func setStatusRedisCounters(t *testing.T, key bucketKey, values map[string]interface{}) {
	t.Helper()
	require.NoError(t, common.RDB.HSet(context.Background(), redisBucketKey(key), values).Err())
}

func setStatusRedisInstanceCounters(t *testing.T, key bucketKey, instanceID string, values map[string]interface{}) {
	t.Helper()
	prefixed := make(map[string]interface{}, len(values))
	for metric, value := range values {
		prefixed[redisInstanceField(instanceID, metric)] = value
	}
	require.NoError(t, common.RDB.HSet(context.Background(), redisInstanceBucketKey(key), prefixed).Err())
	require.NoError(t, common.RDB.ZAdd(context.Background(), redisBucketIndexKey, &redis.Z{
		Score:  float64(key.bucketTs),
		Member: redisInstanceBucketKey(key),
	}).Err())
}

func withStatusBucketTime(t *testing.T, bucketTime string) {
	t.Helper()
	originalBucketTime := perf_metrics_setting.GetSetting().BucketTime
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"perf_metrics_setting.bucket_time": bucketTime}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"perf_metrics_setting.bucket_time": originalBucketTime}))
	})
}

func TestQueryStatusBuildsFixedHourlyTimelineAndClassifiesModels(t *testing.T) {
	db := setupStatusTestDB(t)
	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	currentHour := hourStart(now.Unix())

	require.NoError(t, db.Create(&[]model.PerfMetric{
		{
			ModelName:      "failed-model",
			Group:          "default",
			BucketTs:       currentHour - 3*3600,
			RequestCount:   2,
			SuccessCount:   0,
			TotalLatencyMs: 400,
		},
		{
			ModelName:      "degraded-model",
			Group:          "default",
			BucketTs:       currentHour - 2*3600 + 60,
			RequestCount:   2,
			SuccessCount:   2,
			TotalLatencyMs: 600,
			OutputTokens:   40,
			GenerationMs:   2000,
		},
		{
			ModelName:      "degraded-model",
			Group:          "default",
			BucketTs:       currentHour - 2*3600 + 300,
			RequestCount:   2,
			SuccessCount:   1,
			TotalLatencyMs: 1000,
			OutputTokens:   40,
			GenerationMs:   2000,
		},
		{
			ModelName:    "not-visible-model",
			Group:        "default",
			BucketTs:     currentHour,
			RequestCount: 1,
			SuccessCount: 0,
		},
		{
			ModelName:    "operational-model",
			Group:        "default",
			BucketTs:     currentHour - 24*3600,
			RequestCount: 1,
			SuccessCount: 0,
		},
	}).Error)

	hot := &atomicBucket{}
	hot.add(Sample{Success: true, LatencyMs: 100, OutputTokens: 20, GenerationMs: 1000})
	hot.add(Sample{Success: true, LatencyMs: 300, OutputTokens: 20, GenerationMs: 1000})
	hotBuckets.Store(bucketKey{model: "operational-model", group: "default", bucketTs: currentHour}, hot)

	result, err := queryStatusAt([]StatusModelSource{
		{ModelName: "no-data-model", Vendor: "No Data Vendor"},
		{ModelName: "operational-model", Vendor: "Operational Vendor"},
		{ModelName: "degraded-model", Vendor: "Degraded Vendor"},
		{ModelName: "failed-model", Vendor: "Failed Vendor"},
	}, []string{"default"}, now)
	require.NoError(t, err)

	assert.Equal(t, now.Unix(), result.GeneratedAt)
	assert.Equal(t, 24, result.WindowHours)
	require.Len(t, result.Models, 4)
	assert.Equal(t, []string{"failed-model", "degraded-model", "operational-model", "no-data-model"}, []string{
		result.Models[0].ModelName,
		result.Models[1].ModelName,
		result.Models[2].ModelName,
		result.Models[3].ModelName,
	})

	failed := result.Models[0]
	assert.Equal(t, StatusFailed, failed.Status)
	require.NotNil(t, failed.SuccessRate)
	assert.Equal(t, 0.0, *failed.SuccessRate)

	degraded := result.Models[1]
	assert.Equal(t, StatusDegraded, degraded.Status)
	require.NotNil(t, degraded.SuccessRate)
	require.NotNil(t, degraded.AvgLatencyMs)
	require.NotNil(t, degraded.AvgTps)
	assert.Equal(t, 75.0, *degraded.SuccessRate)
	assert.Equal(t, int64(400), *degraded.AvgLatencyMs)
	assert.Equal(t, 20.0, *degraded.AvgTps)
	assert.Equal(t, StatusDegraded, degraded.Timeline[21].Status)
	require.NotNil(t, degraded.Timeline[21].SuccessRate)
	assert.Equal(t, 75.0, *degraded.Timeline[21].SuccessRate)

	operational := result.Models[2]
	assert.Equal(t, StatusOperational, operational.Status)
	assert.Equal(t, StatusOperational, operational.Timeline[23].Status)
	require.NotNil(t, operational.AvgLatencyMs)
	assert.Equal(t, int64(200), *operational.AvgLatencyMs)

	noData := result.Models[3]
	assert.Equal(t, StatusNoData, noData.Status)
	assert.Nil(t, noData.SuccessRate)
	assert.Nil(t, noData.AvgLatencyMs)
	assert.Nil(t, noData.AvgTps)
	require.Len(t, noData.Timeline, 24)
	assert.Equal(t, currentHour-23*3600, noData.Timeline[0].Ts)
	assert.Equal(t, currentHour, noData.Timeline[23].Ts)
	for _, point := range noData.Timeline {
		assert.Equal(t, StatusNoData, point.Status)
		assert.Nil(t, point.SuccessRate)
	}

	encoded, err := common.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "request_count")
}

func TestQueryStatusUsesPerInstanceRedisBucketsWithoutDoubleCountingLocalHourBucket(t *testing.T) {
	db := setupStatusTestDB(t)
	withStatusBucketTime(t, "hour")
	setupStatusTestRedis(t)

	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	active := bucketKey{model: "shared-model", group: "default", bucketTs: bucketStart(now.Unix())}
	require.NoError(t, db.Create(&model.PerfMetric{
		ModelName:      active.model,
		Group:          active.group,
		BucketTs:       active.bucketTs,
		RequestCount:   10,
		SuccessCount:   0,
		TotalLatencyMs: 1000,
	}).Error)
	local := &atomicBucket{}
	local.add(Sample{Success: true, LatencyMs: 100, OutputTokens: 20, GenerationMs: 1000})
	hotBuckets.Store(active, local)
	setStatusRedisInstanceCounters(t, active, redisWriterID, map[string]interface{}{
		"req": 1, "ok": 1, "lat": 100, "out": 20, "gen_ms": 1000,
	})
	setStatusRedisInstanceCounters(t, active, "remote-instance", map[string]interface{}{
		"req": 1, "ok": 0, "lat": 400, "out": 20, "gen_ms": 1000,
	})

	result, err := queryStatusAt([]StatusModelSource{{ModelName: "shared-model"}}, []string{"default"}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	status := result.Models[0]
	assert.Equal(t, StatusDegraded, status.Status)
	require.NotNil(t, status.SuccessRate)
	require.NotNil(t, status.AvgLatencyMs)
	require.NotNil(t, status.AvgTps)
	assert.Equal(t, 50.0, *status.SuccessRate)
	assert.Equal(t, int64(250), *status.AvgLatencyMs)
	assert.Equal(t, 20.0, *status.AvgTps)
}

func TestQueryStatusIncludesCompletedRedisBucketsFromEveryUnflushedWriter(t *testing.T) {
	setupStatusTestDB(t)
	withStatusBucketTime(t, "minute")
	setupStatusTestRedis(t)

	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	completed := bucketKey{
		model:    "completed-remote-model",
		group:    "default",
		bucketTs: bucketStart(now.Unix()) - 60,
	}
	setStatusRedisInstanceCounters(t, completed, "remote-writer-a", map[string]interface{}{
		"req": 1, "ok": 1, "lat": 100,
	})
	setStatusRedisInstanceCounters(t, completed, "remote-writer-b", map[string]interface{}{
		"req": 1, "ok": 0, "lat": 300,
	})

	result, err := queryStatusAt([]StatusModelSource{{ModelName: completed.model}}, []string{completed.group}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	status := result.Models[0]
	assert.Equal(t, StatusDegraded, status.Status)
	require.NotNil(t, status.SuccessRate)
	require.NotNil(t, status.AvgLatencyMs)
	assert.Equal(t, 50.0, *status.SuccessRate)
	assert.Equal(t, int64(200), *status.AvgLatencyMs)
}

func TestQueryStatusKeepsRemoteCompletedBucketVisibleAfterBucketSwitch(t *testing.T) {
	setupStatusTestDB(t)
	withStatusBucketTime(t, "minute")
	setupStatusTestRedis(t)

	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	active := bucketKey{model: "remote-switch-model", group: "default", bucketTs: bucketStart(now.Unix())}
	completed := active
	completed.bucketTs -= 60
	setStatusRedisInstanceCounters(t, completed, "remote-writer", map[string]interface{}{
		"req": 1, "ok": 1, "lat": 100,
	})
	setStatusRedisInstanceCounters(t, active, "remote-writer", map[string]interface{}{
		"req": 1, "ok": 0, "lat": 300,
	})

	result, err := queryStatusAt([]StatusModelSource{{ModelName: active.model}}, []string{active.group}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	status := result.Models[0]
	assert.Equal(t, StatusDegraded, status.Status)
	require.NotNil(t, status.SuccessRate)
	require.NotNil(t, status.AvgLatencyMs)
	assert.Equal(t, 50.0, *status.SuccessRate)
	assert.Equal(t, int64(200), *status.AvgLatencyMs)
}

func TestQueryStatusDeduplicatesPersistedWriterWithoutHidingOtherWriters(t *testing.T) {
	db := setupStatusTestDB(t)
	withStatusBucketTime(t, "minute")
	setupStatusTestRedis(t)

	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	completed := bucketKey{
		model:    "completed-dedup-model",
		group:    "default",
		bucketTs: bucketStart(now.Unix()) - 60,
	}
	require.NoError(t, db.Create(&model.PerfMetricInstance{
		WriterID:       "persisted-writer",
		ModelName:      completed.model,
		Group:          completed.group,
		BucketTs:       completed.bucketTs,
		RequestCount:   1,
		SuccessCount:   1,
		TotalLatencyMs: 100,
	}).Error)
	setStatusRedisInstanceCounters(t, completed, "persisted-writer", map[string]interface{}{
		"req": 1, "ok": 1, "lat": 100,
	})
	setStatusRedisInstanceCounters(t, completed, "unflushed-writer", map[string]interface{}{
		"req": 1, "ok": 0, "lat": 300,
	})

	result, err := queryStatusAt([]StatusModelSource{{ModelName: completed.model}}, []string{completed.group}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	status := result.Models[0]
	assert.Equal(t, StatusDegraded, status.Status)
	require.NotNil(t, status.SuccessRate)
	require.NotNil(t, status.AvgLatencyMs)
	assert.Equal(t, 50.0, *status.SuccessRate)
	assert.Equal(t, int64(200), *status.AvgLatencyMs)
}

func TestQueryStatusMergesOnlyActiveRedisBucketWithCompletedLocalBuckets(t *testing.T) {
	tests := []struct {
		bucketTime    string
		bucketSeconds int64
	}{
		{bucketTime: "minute", bucketSeconds: 60},
		{bucketTime: "5min", bucketSeconds: 300},
	}

	for _, test := range tests {
		t.Run(test.bucketTime, func(t *testing.T) {
			db := setupStatusTestDB(t)
			withStatusBucketTime(t, test.bucketTime)
			require.Equal(t, test.bucketSeconds, perf_metrics_setting.GetBucketSeconds())
			setupStatusTestRedis(t)

			now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
			active := bucketKey{
				model:    test.bucketTime + "-model",
				group:    "default",
				bucketTs: bucketStart(now.Unix()),
			}
			completedDatabaseBucket := active.bucketTs - 2*test.bucketSeconds
			require.NoError(t, db.Create(&model.PerfMetric{
				ModelName:      active.model,
				Group:          active.group,
				BucketTs:       completedDatabaseBucket,
				RequestCount:   2,
				SuccessCount:   2,
				TotalLatencyMs: 200,
				OutputTokens:   40,
				GenerationMs:   2000,
			}).Error)

			completedHotKey := bucketKey{
				model:    active.model,
				group:    active.group,
				bucketTs: active.bucketTs - test.bucketSeconds,
			}
			completedHot := &atomicBucket{}
			completedHot.add(Sample{Success: false, LatencyMs: 400, OutputTokens: 20, GenerationMs: 1000})
			hotBuckets.Store(completedHotKey, completedHot)
			localActive := &atomicBucket{}
			localActive.add(Sample{Success: true, LatencyMs: 100, OutputTokens: 20, GenerationMs: 1000})
			hotBuckets.Store(active, localActive)

			setStatusRedisInstanceCounters(t, active, redisWriterID, map[string]interface{}{
				"req": 1, "ok": 1, "lat": 100, "out": 20, "gen_ms": 1000,
			})
			setStatusRedisInstanceCounters(t, active, "remote-instance", map[string]interface{}{
				"req": 1, "ok": 0, "lat": 500, "out": 20, "gen_ms": 1000,
			})
			setStatusRedisCounters(t, completedHotKey, map[string]interface{}{
				"req": 99,
				"lat": 9900,
			})

			result, err := queryStatusAt([]StatusModelSource{{ModelName: active.model}}, []string{active.group}, now)
			require.NoError(t, err)
			require.Len(t, result.Models, 1)
			status := result.Models[0]
			assert.Equal(t, StatusDegraded, status.Status)
			require.NotNil(t, status.SuccessRate)
			require.NotNil(t, status.AvgLatencyMs)
			require.NotNil(t, status.AvgTps)
			assert.Equal(t, 60.0, *status.SuccessRate)
			assert.Equal(t, int64(240), *status.AvgLatencyMs)
			assert.Equal(t, 20.0, *status.AvgTps)
			assert.Equal(t, StatusDegraded, status.Timeline[23].Status)
		})
	}
}

func TestQueryStatusFallsBackToConsistentHotSnapshotWhenRedisIsUnavailable(t *testing.T) {
	setupStatusTestDB(t)
	withStatusBucketTime(t, "hour")
	redisServer := setupStatusTestRedis(t)
	redisServer.Close()

	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	key := bucketKey{model: "fallback-model", group: "default", bucketTs: hourStart(now.Unix())}
	hot := &atomicBucket{}
	hot.add(Sample{Success: true, LatencyMs: 250})
	hotBuckets.Store(key, hot)

	result, err := queryStatusAt([]StatusModelSource{{ModelName: "fallback-model"}}, []string{"default"}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	status := result.Models[0]
	assert.Equal(t, StatusOperational, status.Status)
	require.NotNil(t, status.SuccessRate)
	require.NotNil(t, status.AvgLatencyMs)
	assert.Equal(t, 100.0, *status.SuccessRate)
	assert.Equal(t, int64(250), *status.AvgLatencyMs)
}

func TestQueryStatusKeepsMoreCompleteLocalActiveBucketWhenRedisBucketIsPartial(t *testing.T) {
	setupStatusTestDB(t)
	withStatusBucketTime(t, "hour")
	setupStatusTestRedis(t)

	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	active := bucketKey{model: "partial-redis-model", group: "default", bucketTs: bucketStart(now.Unix())}
	local := &atomicBucket{}
	local.add(Sample{Success: true, LatencyMs: 200})
	local.add(Sample{Success: true, LatencyMs: 300})
	local.add(Sample{Success: false, LatencyMs: 400})
	hotBuckets.Store(active, local)
	setStatusRedisInstanceCounters(t, active, redisWriterID, map[string]interface{}{
		"req": 1, "ok": 0, "lat": 100,
	})
	setStatusRedisInstanceCounters(t, active, "remote-instance", map[string]interface{}{
		"req": 2, "ok": 1, "lat": 300,
	})

	result, err := queryStatusAt([]StatusModelSource{{ModelName: "partial-redis-model"}}, []string{"default"}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	status := result.Models[0]
	assert.Equal(t, StatusDegraded, status.Status)
	require.NotNil(t, status.SuccessRate)
	require.NotNil(t, status.AvgLatencyMs)
	assert.Equal(t, 60.0, *status.SuccessRate)
	assert.Equal(t, int64(240), *status.AvgLatencyMs)
}

func TestQueryStatusMergesLegacyAndPerInstanceBucketsDuringRollingUpgrade(t *testing.T) {
	setupStatusTestDB(t)
	withStatusBucketTime(t, "hour")
	setupStatusTestRedis(t)

	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	active := bucketKey{model: "rolling-model", group: "default", bucketTs: bucketStart(now.Unix())}
	local := &atomicBucket{}
	local.add(Sample{Success: true, LatencyMs: 100})
	hotBuckets.Store(active, local)
	setStatusRedisCounters(t, active, map[string]interface{}{
		"req": 2, "ok": 1, "lat": 500,
	})
	setStatusRedisInstanceCounters(t, active, redisWriterID, map[string]interface{}{
		"req": 1, "ok": 1, "lat": 100,
	})
	setStatusRedisInstanceCounters(t, active, "remote-v2-instance", map[string]interface{}{
		"req": 2, "ok": 2, "lat": 400,
	})

	result, err := queryStatusAt([]StatusModelSource{{ModelName: active.model}}, []string{active.group}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	status := result.Models[0]
	require.NotNil(t, status.SuccessRate)
	require.NotNil(t, status.AvgLatencyMs)
	assert.Equal(t, 80.0, *status.SuccessRate)
	assert.Equal(t, int64(200), *status.AvgLatencyMs)
}

func TestQueryStatusUsesNewerLocalRedisSnapshotCapturedAfterHotSnapshot(t *testing.T) {
	key := bucketKey{model: "concurrent-model", group: "default", bucketTs: 123}
	redisBuckets := map[bucketKey]redisBucketValues{
		key: {
			instances: map[string]counters{
				redisWriterID: {requestCount: 2, successCount: 1, totalLatencyMs: 500},
			},
		},
	}
	localBuckets := map[bucketKey]counters{
		key: {requestCount: 1, successCount: 1, totalLatencyMs: 100},
	}

	merged := mergeStatusBuckets(nil, redisBuckets, localBuckets)
	assert.Equal(t, counters{requestCount: 2, successCount: 1, totalLatencyMs: 500}, merged[key])
}

func TestStatusInstanceDedupIsIndependentOfDBAndRedisCommitOrder(t *testing.T) {
	key := bucketKey{model: "interleaving-model", group: "default", bucketTs: 456}
	older := counters{requestCount: 1, successCount: 1, totalLatencyMs: 100}
	newer := counters{requestCount: 2, successCount: 1, totalLatencyMs: 400}

	tests := []struct {
		name  string
		db    counters
		redis counters
	}{
		{name: "database commit visible before redis publication", db: newer, redis: older},
		{name: "redis publication visible before database commit", db: older, redis: newer},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			merged := mergeStatusBuckets(
				statusInstanceBuckets{key: {"writer-a": test.db}},
				map[bucketKey]redisBucketValues{key: {instances: map[string]counters{"writer-a": test.redis}}},
				nil,
			)
			assert.Equal(t, newer, merged[key])
		})
	}
}

func TestQueryStatusRejectsBlankModelsAndRequiresExplicitGroups(t *testing.T) {
	db := setupStatusTestDB(t)
	withStatusBucketTime(t, "hour")

	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	completedHour := hourStart(now.Unix()) - 3600
	require.NoError(t, db.Create(&model.PerfMetric{
		ModelName:      "visible-model",
		Group:          "default",
		BucketTs:       completedHour,
		RequestCount:   1,
		SuccessCount:   1,
		TotalLatencyMs: 120,
	}).Error)

	models := []StatusModelSource{
		{ModelName: ""},
		{ModelName: " \t "},
		{ModelName: "visible-model", Vendor: "Visible Vendor"},
		{ModelName: "visible-model", Vendor: "Duplicate Vendor"},
	}
	for _, groups := range [][]string{nil, {}, {"", "  "}} {
		result, err := queryStatusAt(models, groups, now)
		require.NoError(t, err)
		require.Len(t, result.Models, 1)
		assert.Equal(t, "visible-model", result.Models[0].ModelName)
		assert.Equal(t, "Visible Vendor", result.Models[0].Vendor)
		assert.Equal(t, StatusNoData, result.Models[0].Status)
		assert.Nil(t, result.Models[0].SuccessRate)
	}

	result, err := queryStatusAt(models, []string{"", "default", "default", "  "}, now)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	assert.Equal(t, StatusOperational, result.Models[0].Status)
	require.NotNil(t, result.Models[0].SuccessRate)
	assert.Equal(t, 100.0, *result.Models[0].SuccessRate)
}

func TestRecordIgnoresWhitespaceOnlyModelNames(t *testing.T) {
	setupStatusTestDB(t)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})

	Record(Sample{Model: " \t ", Group: "default", Success: true, LatencyMs: 100})
	bucketCount := 0
	hotBuckets.Range(func(_, _ any) bool {
		bucketCount++
		return true
	})
	assert.Zero(t, bucketCount)
}

func TestQueryStatusCacheReturnsDeepCopiesAndIsolatesVisibilityKeys(t *testing.T) {
	resetStatusResultCacheForTest(t)
	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	var loadCount atomic.Int64
	load := func(models []StatusModelSource, groups []string, generatedAt time.Time) (StatusResult, error) {
		loadCount.Add(1)
		status := StatusOperational
		if len(groups) > 0 && groups[0] == "vip" {
			status = StatusFailed
		}
		successRate := 100.0
		avgLatencyMs := int64(120)
		avgTps := 30.0
		pointSuccessRate := 100.0
		return StatusResult{
			GeneratedAt: generatedAt.Unix(),
			WindowHours: statusWindowHours,
			Models: []ModelStatus{{
				ModelName:    models[0].ModelName,
				Vendor:       models[0].Vendor,
				SuccessRate:  &successRate,
				AvgLatencyMs: &avgLatencyMs,
				AvgTps:       &avgTps,
				Status:       status,
				Timeline: []StatusPoint{{
					Ts:          generatedAt.Unix(),
					Status:      status,
					SuccessRate: &pointSuccessRate,
				}},
			}},
		}, nil
	}

	first, err := queryStatusCachedAt(
		[]StatusModelSource{{ModelName: "visible-model", Vendor: "Vendor A"}},
		[]string{"default"},
		now,
		load,
	)
	require.NoError(t, err)
	require.Len(t, first.Models, 1)
	first.Models[0].Vendor = "mutated"
	*first.Models[0].SuccessRate = 1
	*first.Models[0].AvgLatencyMs = 999
	*first.Models[0].AvgTps = 1
	first.Models[0].Timeline[0].Status = StatusFailed
	*first.Models[0].Timeline[0].SuccessRate = 1

	second, err := queryStatusCachedAt(
		[]StatusModelSource{
			{ModelName: "visible-model", Vendor: "Vendor A"},
			{ModelName: "visible-model", Vendor: "ignored duplicate"},
		},
		[]string{"default", "default"},
		now.Add(time.Second),
		load,
	)
	require.NoError(t, err)
	require.Len(t, second.Models, 1)
	assert.Equal(t, int64(1), loadCount.Load())
	assert.Equal(t, "Vendor A", second.Models[0].Vendor)
	require.NotNil(t, second.Models[0].SuccessRate)
	require.NotNil(t, second.Models[0].AvgLatencyMs)
	require.NotNil(t, second.Models[0].AvgTps)
	assert.Equal(t, 100.0, *second.Models[0].SuccessRate)
	assert.Equal(t, int64(120), *second.Models[0].AvgLatencyMs)
	assert.Equal(t, 30.0, *second.Models[0].AvgTps)
	require.Len(t, second.Models[0].Timeline, 1)
	assert.Equal(t, StatusOperational, second.Models[0].Timeline[0].Status)
	require.NotNil(t, second.Models[0].Timeline[0].SuccessRate)
	assert.Equal(t, 100.0, *second.Models[0].Timeline[0].SuccessRate)

	differentVendor, err := queryStatusCachedAt(
		[]StatusModelSource{{ModelName: "visible-model", Vendor: "Vendor B"}},
		[]string{"default"},
		now.Add(2*time.Second),
		load,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(2), loadCount.Load())
	assert.Equal(t, "Vendor B", differentVendor.Models[0].Vendor)

	differentGroup, err := queryStatusCachedAt(
		[]StatusModelSource{{ModelName: "visible-model", Vendor: "Vendor B"}},
		[]string{"vip"},
		now.Add(3*time.Second),
		load,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(3), loadCount.Load())
	assert.Equal(t, StatusFailed, differentGroup.Models[0].Status)

	differentVisibleModel, err := queryStatusCachedAt(
		[]StatusModelSource{{ModelName: "other-visible-model", Vendor: "Vendor A"}},
		[]string{"default"},
		now.Add(4*time.Second),
		load,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(4), loadCount.Load())
	assert.Equal(t, "other-visible-model", differentVisibleModel.Models[0].ModelName)

	_, err = queryStatusCachedAt(
		[]StatusModelSource{{ModelName: "visible-model", Vendor: "Vendor A"}},
		[]string{"default"},
		now.Add(statusCacheTTL),
		load,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(5), loadCount.Load())
}

func TestQueryStatusCacheDeduplicatesConcurrentLoads(t *testing.T) {
	resetStatusResultCacheForTest(t)
	now := time.Date(2026, time.July, 22, 15, 37, 42, 0, time.UTC)
	var loadCount atomic.Int64
	load := func(models []StatusModelSource, _ []string, generatedAt time.Time) (StatusResult, error) {
		loadCount.Add(1)
		return StatusResult{
			GeneratedAt: generatedAt.Unix(),
			WindowHours: statusWindowHours,
			Models: []ModelStatus{{
				ModelName: models[0].ModelName,
				Vendor:    models[0].Vendor,
				Status:    StatusNoData,
			}},
		}, nil
	}

	const workers = 32
	start := make(chan struct{})
	errors := make(chan error, workers)
	results := make(chan StatusResult, workers)
	var waitGroup sync.WaitGroup
	for i := 0; i < workers; i++ {
		waitGroup.Add(1)
		go func(reverse bool) {
			defer waitGroup.Done()
			<-start
			models := []StatusModelSource{
				{ModelName: "model-a", Vendor: "Vendor A"},
				{ModelName: "model-b", Vendor: "Vendor B"},
			}
			groups := []string{"default", "vip"}
			if reverse {
				models[0], models[1] = models[1], models[0]
				groups[0], groups[1] = groups[1], groups[0]
			}
			result, err := queryStatusCachedAt(models, groups, now, load)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(i%2 == 0)
	}
	close(start)
	waitGroup.Wait()
	close(errors)
	close(results)

	for err := range errors {
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), loadCount.Load())
	assert.Len(t, results, workers)
	for result := range results {
		require.Len(t, result.Models, 1)
		assert.Equal(t, "model-a", result.Models[0].ModelName)
	}
}
