package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type perfMetricsSummaryResponse struct {
	Success bool                         `json:"success"`
	Data    perfmetrics.SummaryAllResult `json:"data"`
}

type perfMetricsDetailResponse struct {
	Success bool                    `json:"success"`
	Data    perfmetrics.QueryResult `json:"data"`
}

type perfMetricsErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func setupPerfMetricsVisibilityFixture(t *testing.T) *gorm.DB {
	t.Helper()

	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"private":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))
	require.NoError(t, db.Create(&model.Channel{
		Id:     901,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "performance-visibility-channel",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "public-performance-model", ChannelId: 901, Enabled: true},
		{Group: "private", Model: "public-performance-model", ChannelId: 901, Enabled: true},
		{Group: "private", Model: "private-upstream-model", ChannelId: 901, Enabled: true},
	}).Error)
	for _, modelName := range []string{"public-performance-model", "private-upstream-model"} {
		require.NoError(t, (&model.Model{
			ModelName:    modelName,
			Status:       1,
			SyncOfficial: 1,
		}).Insert())
	}

	bucketTs := time.Now().Add(-time.Hour).Unix()
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: "public-performance-model", Group: "default", BucketTs: bucketTs, RequestCount: 2, SuccessCount: 2},
		{ModelName: "public-performance-model", Group: "private", BucketTs: bucketTs, RequestCount: 4, SuccessCount: 0},
		{ModelName: "private-upstream-model", Group: "private", BucketTs: bucketTs, RequestCount: 3, SuccessCount: 3},
	}).Error)
	model.InvalidatePricingCache()
	return db
}

func TestGetPerfMetricsSummaryExcludesModelsOutsideVisiblePricing(t *testing.T) {
	setupPerfMetricsVisibilityFixture(t)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/summary", nil)

	GetPerfMetricsSummary(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload perfMetricsSummaryResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Models, 1)
	assert.Equal(t, "public-performance-model", payload.Data.Models[0].ModelName)
	assert.Equal(t, 100.0, payload.Data.Models[0].SuccessRate)
	assert.NotContains(t, recorder.Body.String(), "private-upstream-model")
}

func TestGetPerfMetricsOnlyReturnsGroupsUsableByAnonymousVisitor(t *testing.T) {
	setupPerfMetricsVisibilityFixture(t)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics?model=public-performance-model", nil)

	GetPerfMetrics(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload perfMetricsDetailResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Groups, 1)
	assert.Equal(t, "default", payload.Data.Groups[0].Group)
	assert.Equal(t, 100.0, payload.Data.Groups[0].SuccessRate)
	assert.NotContains(t, recorder.Body.String(), `"group":"private"`)
}

func TestGetPerfMetricsRejectsGroupOutsideVisitorAccessWithoutExistenceLeak(t *testing.T) {
	setupPerfMetricsVisibilityFixture(t)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics?model=public-performance-model&group=private", nil)

	GetPerfMetrics(context)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	var payload perfMetricsErrorResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	assert.False(t, payload.Success)
	assert.Equal(t, "model is not available", payload.Message)
	assert.NotContains(t, recorder.Body.String(), "private")
}

func TestGetPerfMetricsRejectsModelsOutsideVisiblePricingWithoutExistenceLeak(t *testing.T) {
	setupPerfMetricsVisibilityFixture(t)

	for _, modelName := range []string{"private-upstream-model", "missing-model"} {
		t.Run(modelName, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics?model="+modelName, nil)

			GetPerfMetrics(context)

			require.Equal(t, http.StatusNotFound, recorder.Code)
			var payload perfMetricsErrorResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			assert.False(t, payload.Success)
			assert.Equal(t, "model is not available", payload.Message)
			assert.NotContains(t, recorder.Body.String(), modelName)
		})
	}
}

func TestGetPerfMetricsStatusDoesNotExposeInternalQueryErrors(t *testing.T) {
	db := setupPerfMetricsVisibilityFixture(t)
	// Prime pricing before injecting a database error so the handler can still
	// resolve the public model allowlist and exercise the metrics query path.
	require.Equal(t, []string{"public-performance-model"}, getVisibleModelNames(&gin.Context{}))

	callbackName := "test:perf-metrics-query-error:" + strings.ReplaceAll(t.Name(), "/", "-")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "perf_metrics" {
			tx.AddError(errors.New("private database connection detail"))
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove(callbackName))
	})

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/status", nil)

	GetPerfMetricsStatus(context)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var payload perfMetricsErrorResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	assert.False(t, payload.Success)
	assert.Equal(t, "performance metrics are temporarily unavailable", payload.Message)
	assert.NotContains(t, recorder.Body.String(), "private database connection detail")
}
