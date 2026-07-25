package model

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type perfMetricSQLRecorder struct {
	logger.Interface
	sql string
}

func (r *perfMetricSQLRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	r.sql, _ = fc()
}

func setupPerfMetricSQLiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()
	require.NoError(t, db.AutoMigrate(&PerfMetric{}, &PerfMetricInstance{}))

	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		initCol()
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestUpsertPerfMetricInstanceAddsCountersPerWriterBucket(t *testing.T) {
	setupPerfMetricSQLiteTestDB(t)

	require.NoError(t, UpsertPerfMetricInstance("writer-a", &PerfMetric{
		ModelName:      "model-a",
		Group:          "default",
		BucketTs:       600,
		RequestCount:   2,
		SuccessCount:   1,
		TotalLatencyMs: 120,
		TtftSumMs:      40,
		TtftCount:      1,
		OutputTokens:   20,
		GenerationMs:   200,
	}))
	require.NoError(t, UpsertPerfMetricInstance("writer-a", &PerfMetric{
		ModelName:      "model-a",
		Group:          "default",
		BucketTs:       600,
		RequestCount:   3,
		SuccessCount:   2,
		TotalLatencyMs: 330,
		TtftSumMs:      90,
		TtftCount:      2,
		OutputTokens:   60,
		GenerationMs:   600,
	}))
	require.NoError(t, UpsertPerfMetricInstance("writer-b", &PerfMetric{
		ModelName:      "model-a",
		Group:          "default",
		BucketTs:       600,
		RequestCount:   7,
		SuccessCount:   6,
		TotalLatencyMs: 700,
		TtftSumMs:      210,
		TtftCount:      7,
		OutputTokens:   140,
		GenerationMs:   1400,
	}))

	rows, err := GetPerfMetricInstancesForModels(500, 700, []string{"", "default", "default"}, []string{"model-a", "  "})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	assert.Equal(t, "writer-a", rows[0].WriterID)
	assert.Equal(t, int64(5), rows[0].RequestCount)
	assert.Equal(t, int64(3), rows[0].SuccessCount)
	assert.Equal(t, int64(450), rows[0].TotalLatencyMs)
	assert.Equal(t, int64(130), rows[0].TtftSumMs)
	assert.Equal(t, int64(3), rows[0].TtftCount)
	assert.Equal(t, int64(80), rows[0].OutputTokens)
	assert.Equal(t, int64(800), rows[0].GenerationMs)
	assert.Equal(t, "writer-b", rows[1].WriterID)
	assert.Equal(t, int64(7), rows[1].RequestCount)

	require.Error(t, UpsertPerfMetricInstance("  ", &PerfMetric{RequestCount: 1}))
	require.NoError(t, UpsertPerfMetricInstance("", nil))
	require.NoError(t, UpsertPerfMetricInstance("", &PerfMetric{}))
}

func TestPerfMetricQueriesMergeLegacyAndPerWriterRows(t *testing.T) {
	db := setupPerfMetricSQLiteTestDB(t)
	require.NoError(t, db.Create(&[]PerfMetric{
		{ModelName: "model-a", Group: "default", BucketTs: 600, RequestCount: 2, SuccessCount: 1, TotalLatencyMs: 200, TtftSumMs: 30, TtftCount: 1, OutputTokens: 20, GenerationMs: 200},
		{ModelName: "model-a", Group: "default", BucketTs: 900, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 50},
		{ModelName: "model-a", Group: "premium", BucketTs: 600, RequestCount: 50, SuccessCount: 50},
	}).Error)
	require.NoError(t, db.Create(&[]PerfMetricInstance{
		{WriterID: "writer-a", ModelName: "model-a", Group: "default", BucketTs: 600, RequestCount: 3, SuccessCount: 2, TotalLatencyMs: 300, TtftSumMs: 60, TtftCount: 2, OutputTokens: 30, GenerationMs: 300},
		{WriterID: "writer-b", ModelName: "model-a", Group: "default", BucketTs: 600, RequestCount: 4, SuccessCount: 4, TotalLatencyMs: 400, TtftSumMs: 80, TtftCount: 4, OutputTokens: 40, GenerationMs: 400},
		{WriterID: "writer-a", ModelName: "model-b", Group: "default", BucketTs: 600, RequestCount: 5, SuccessCount: 3, TotalLatencyMs: 500, OutputTokens: 50, GenerationMs: 500},
		{WriterID: "writer-a", ModelName: "model-b", Group: "premium", BucketTs: 600, RequestCount: 70, SuccessCount: 70},
	}).Error)

	metrics, err := GetPerfMetrics("model-a", "default", 500, 1000)
	require.NoError(t, err)
	require.Len(t, metrics, 2)
	assert.Equal(t, int64(600), metrics[0].BucketTs)
	assert.Equal(t, int64(9), metrics[0].RequestCount)
	assert.Equal(t, int64(7), metrics[0].SuccessCount)
	assert.Equal(t, int64(900), metrics[0].TotalLatencyMs)
	assert.Equal(t, int64(170), metrics[0].TtftSumMs)
	assert.Equal(t, int64(7), metrics[0].TtftCount)
	assert.Equal(t, int64(90), metrics[0].OutputTokens)
	assert.Equal(t, int64(900), metrics[0].GenerationMs)
	assert.Equal(t, int64(900), metrics[1].BucketTs)
	assert.Equal(t, int64(1), metrics[1].RequestCount)

	summaries, err := GetPerfMetricsSummaryAll(500, 1000, []string{"default"})
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	assert.Equal(t, "model-a", summaries[0].ModelName)
	assert.Equal(t, int64(10), summaries[0].RequestCount)
	assert.Equal(t, "model-b", summaries[1].ModelName)
	assert.Equal(t, int64(5), summaries[1].RequestCount)

	buckets, err := GetPerfMetricsSummaryBucketsAll(500, 1000, []string{"default"})
	require.NoError(t, err)
	require.Len(t, buckets, 3)
	assert.Equal(t, PerfMetricSummaryBucket{ModelName: "model-a", BucketTs: 600, RequestCount: 9, SuccessCount: 7, TotalLatencyMs: 900, OutputTokens: 90, GenerationMs: 900}, buckets[0])
	assert.Equal(t, PerfMetricSummaryBucket{ModelName: "model-b", BucketTs: 600, RequestCount: 5, SuccessCount: 3, TotalLatencyMs: 500, OutputTokens: 50, GenerationMs: 500}, buckets[1])
	assert.Equal(t, PerfMetricSummaryBucket{ModelName: "model-a", BucketTs: 900, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 50}, buckets[2])

	legacyStatusRows, err := GetPerfMetricsHourlySummaryBucketsForModels(0, 1000, -1, []string{"default"}, []string{"model-a"})
	require.NoError(t, err)
	require.Len(t, legacyStatusRows, 1)
	assert.Equal(t, int64(3), legacyStatusRows[0].RequestCount)
}

func TestGetPerfMetricInstancesForModelsRequiresExactAllowlists(t *testing.T) {
	db := setupPerfMetricSQLiteTestDB(t)
	commonGroupCol = ""
	require.NoError(t, db.Create(&[]PerfMetricInstance{
		{WriterID: "writer-a", ModelName: "visible", Group: "default", BucketTs: 100, RequestCount: 1},
		{WriterID: "writer-b", ModelName: "visible", Group: "premium", BucketTs: 100, RequestCount: 2},
		{WriterID: "writer-c", ModelName: "hidden", Group: "default", BucketTs: 100, RequestCount: 3},
		{WriterID: "writer-d", ModelName: "visible", Group: "default", BucketTs: 50, RequestCount: 4},
	}).Error)

	rows, err := GetPerfMetricInstancesForModels(75, 125, []string{"default"}, []string{"visible"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "writer-a", rows[0].WriterID)

	for _, test := range []struct {
		name   string
		groups []string
		models []string
	}{
		{name: "nil groups", groups: nil, models: []string{"visible"}},
		{name: "empty groups", groups: []string{}, models: []string{"visible"}},
		{name: "blank groups", groups: []string{"", "  "}, models: []string{"visible"}},
		{name: "nil models", groups: []string{"default"}, models: nil},
		{name: "blank models", groups: []string{"default"}, models: []string{"", "  "}},
	} {
		t.Run(test.name, func(t *testing.T) {
			filtered, queryErr := GetPerfMetricInstancesForModels(0, 200, test.groups, test.models)
			require.NoError(t, queryErr)
			assert.Empty(t, filtered)
		})
	}
}

func TestDeletePerfMetricsBeforeDeletesLegacyAndPerWriterRows(t *testing.T) {
	db := setupPerfMetricSQLiteTestDB(t)
	require.NoError(t, db.Create(&[]PerfMetric{
		{ModelName: "legacy-old", Group: "default", BucketTs: 100, RequestCount: 1},
		{ModelName: "legacy-new", Group: "default", BucketTs: 200, RequestCount: 1},
	}).Error)
	require.NoError(t, db.Create(&[]PerfMetricInstance{
		{WriterID: "writer-a", ModelName: "instance-old", Group: "default", BucketTs: 100, RequestCount: 1},
		{WriterID: "writer-a", ModelName: "instance-new", Group: "default", BucketTs: 200, RequestCount: 1},
	}).Error)

	require.NoError(t, DeletePerfMetricsBefore(150))
	var legacyRows []PerfMetric
	var instanceRows []PerfMetricInstance
	require.NoError(t, db.Order("bucket_ts ASC").Find(&legacyRows).Error)
	require.NoError(t, db.Order("bucket_ts ASC").Find(&instanceRows).Error)
	require.Len(t, legacyRows, 1)
	require.Len(t, instanceRows, 1)
	assert.Equal(t, "legacy-new", legacyRows[0].ModelName)
	assert.Equal(t, "instance-new", instanceRows[0].ModelName)
}

func TestGetPerfMetricsHourlySummaryBucketsForModelsAggregatesSQLiteBuckets(t *testing.T) {
	db := setupPerfMetricSQLiteTestDB(t)
	firstHour := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC).Unix()
	excludedBucket := firstHour + 3600 + 300
	require.NoError(t, db.Create(&[]PerfMetric{
		{ModelName: "visible-model", Group: "default", BucketTs: firstHour + 60, RequestCount: 2, SuccessCount: 2, TotalLatencyMs: 200, TtftSumMs: 240, TtftCount: 2, OutputTokens: 20, GenerationMs: 1000},
		{ModelName: "visible-model", Group: "auto", BucketTs: firstHour + 300, RequestCount: 3, SuccessCount: 1, TotalLatencyMs: 900, TtftSumMs: 300, TtftCount: 1, OutputTokens: 60, GenerationMs: 3000},
		{ModelName: "visible-model", Group: "retired", BucketTs: firstHour + 600, RequestCount: 50, SuccessCount: 0},
		{ModelName: "visible-model", Group: "default", BucketTs: firstHour + 3600 + 60, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 400, TtftSumMs: 180, TtftCount: 1},
		{ModelName: "visible-model", Group: "default", BucketTs: excludedBucket, RequestCount: 20, SuccessCount: 0},
		{ModelName: "hidden-model", Group: "default", BucketTs: firstHour + 120, RequestCount: 40, SuccessCount: 0},
		{ModelName: "visible-model", Group: "default", BucketTs: firstHour - 60, RequestCount: 30, SuccessCount: 0},
	}).Error)

	rows, err := GetPerfMetricsHourlySummaryBucketsForModels(
		firstHour,
		firstHour+2*3600,
		excludedBucket,
		[]string{"default", "auto"},
		[]string{"", "visible-model", "visible-model", "  "},
	)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	assert.Equal(t, "visible-model", rows[0].ModelName)
	assert.Equal(t, firstHour, rows[0].BucketTs)
	assert.Equal(t, int64(5), rows[0].RequestCount)
	assert.Equal(t, int64(3), rows[0].SuccessCount)
	assert.Equal(t, int64(1100), rows[0].TotalLatencyMs)
	assert.Equal(t, int64(540), rows[0].TtftSumMs)
	assert.Equal(t, int64(3), rows[0].TtftCount)
	assert.Equal(t, int64(80), rows[0].OutputTokens)
	assert.Equal(t, int64(4000), rows[0].GenerationMs)

	assert.Equal(t, firstHour+3600, rows[1].BucketTs)
	assert.Equal(t, int64(1), rows[1].RequestCount)
	assert.Equal(t, int64(1), rows[1].SuccessCount)
	assert.Equal(t, int64(400), rows[1].TotalLatencyMs)
	assert.Equal(t, int64(180), rows[1].TtftSumMs)
	assert.Equal(t, int64(1), rows[1].TtftCount)

	emptyGroups, err := GetPerfMetricsHourlySummaryBucketsForModels(firstHour, firstHour+3600, 0, []string{}, []string{"visible-model"})
	require.NoError(t, err)
	assert.Empty(t, emptyGroups)

	nilGroups, err := GetPerfMetricsHourlySummaryBucketsForModels(firstHour, firstHour+3600, 0, nil, []string{"visible-model"})
	require.NoError(t, err)
	assert.Empty(t, nilGroups)

	blankGroups, err := GetPerfMetricsHourlySummaryBucketsForModels(firstHour, firstHour+3600, 0, []string{"", "  "}, []string{"visible-model"})
	require.NoError(t, err)
	assert.Empty(t, blankGroups)

	emptyModels, err := GetPerfMetricsHourlySummaryBucketsForModels(firstHour, firstHour+3600, 0, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, emptyModels)

	blankModels, err := GetPerfMetricsHourlySummaryBucketsForModels(firstHour, firstHour+3600, 0, []string{"default"}, []string{"", "  "})
	require.NoError(t, err)
	assert.Empty(t, blankModels)
}

func TestPerfMetricGroupFiltersDoNotDependOnInitializedCommonColumn(t *testing.T) {
	db := setupPerfMetricSQLiteTestDB(t)
	commonGroupCol = ""

	const startTs int64 = 3600
	require.NoError(t, db.Create(&[]PerfMetric{
		{ModelName: "model-a", Group: "default", BucketTs: startTs + 60, RequestCount: 2, SuccessCount: 2},
		{ModelName: "model-a", Group: "premium", BucketTs: startTs + 120, RequestCount: 9, SuccessCount: 1},
		{ModelName: "model-b", Group: "default", BucketTs: startTs + 180, RequestCount: 4, SuccessCount: 3},
	}).Error)

	metrics, err := GetPerfMetrics("model-a", "default", startTs, startTs+3600)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	assert.Equal(t, int64(2), metrics[0].RequestCount)

	summaries, err := GetPerfMetricsSummaryAll(startTs, startTs+3600, []string{"default"})
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	requestCounts := make(map[string]int64, len(summaries))
	for _, summary := range summaries {
		requestCounts[summary.ModelName] = summary.RequestCount
	}
	assert.Equal(t, map[string]int64{"model-a": 2, "model-b": 4}, requestCounts)

	buckets, err := GetPerfMetricsSummaryBucketsAll(startTs, startTs+3600, []string{"default"})
	require.NoError(t, err)
	require.Len(t, buckets, 2)
	for _, bucket := range buckets {
		assert.Equal(t, requestCounts[bucket.ModelName], bucket.RequestCount)
	}

	hourly, err := GetPerfMetricsHourlySummaryBucketsForModels(
		startTs,
		startTs+3600,
		0,
		[]string{"default"},
		[]string{"model-a"},
	)
	require.NoError(t, err)
	require.Len(t, hourly, 1)
	assert.Equal(t, "model-a", hourly[0].ModelName)
	assert.Equal(t, startTs, hourly[0].BucketTs)
	assert.Equal(t, int64(2), hourly[0].RequestCount)
}

func TestGetPerfMetricsHourlySummaryBucketsForModelsBuildsCompatibleDialectSQL(t *testing.T) {
	previousDB := DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		initCol()
	})

	testCases := []struct {
		name                 string
		databaseType         common.DatabaseType
		dialector            gorm.Dialector
		wantBucketExpression string
		wantGroupColumn      string
	}{
		{
			name:                 "sqlite",
			databaseType:         common.DatabaseTypeSQLite,
			dialector:            sqlite.Open("file:perf_metric_dry_run?mode=memory&cache=shared"),
			wantBucketExpression: "(bucket_ts / 3600) * 3600",
			wantGroupColumn:      "`group` IN",
		},
		{
			name:         "mysql",
			databaseType: common.DatabaseTypeMySQL,
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
			wantBucketExpression: "FLOOR(bucket_ts / 3600) * 3600",
			wantGroupColumn:      "`group` IN",
		},
		{
			name:         "postgresql",
			databaseType: common.DatabaseTypePostgreSQL,
			dialector: postgres.New(postgres.Config{
				DSN: "host=localhost user=gorm dbname=gorm port=9920 sslmode=disable",
			}),
			wantBucketExpression: "(bucket_ts / 3600) * 3600",
			wantGroupColumn:      `"group" IN`,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			recorder := &perfMetricSQLRecorder{Interface: logger.Discard}
			dryRunDB, err := gorm.Open(test.dialector, &gorm.Config{
				DryRun:               true,
				DisableAutomaticPing: true,
				Logger:               recorder,
			})
			require.NoError(t, err)
			sqlDB, err := dryRunDB.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			DB = dryRunDB
			common.SetDatabaseTypes(test.databaseType, test.databaseType)
			initCol()

			_, err = GetPerfMetricsHourlySummaryBucketsForModels(100, 200, 150, []string{"default"}, []string{"visible-model"})
			require.NoError(t, err)
			sql := strings.Join(strings.Fields(recorder.sql), " ")
			require.NotEmpty(t, sql)
			assert.Contains(t, sql, test.wantBucketExpression)
			assert.Contains(t, sql, test.wantGroupColumn)
			assert.Contains(t, sql, "model_name IN")
			assert.Contains(t, sql, "bucket_ts <> 150")
			assert.Contains(t, sql, "SUM(ttft_sum_ms) as ttft_sum_ms")
			assert.Contains(t, sql, "SUM(ttft_count) as ttft_count")
		})
	}
}
