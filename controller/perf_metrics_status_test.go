package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type perfMetricsStatusResponse struct {
	Success bool                     `json:"success"`
	Data    perfmetrics.StatusResult `json:"data"`
}

func TestPerfMetricsStatusCatalogScopesDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}, &model.UserGroupMembership{}))
			previousUsable, previousRatios := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString()
			require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","private":"Private"}`))
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2,"private":3}`))
			t.Cleanup(func() {
				require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable))
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
				model.InvalidatePricingCache()
			})
			user := model.User{Username: "status-matrix-user", Group: "default", Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			manual := true
			require.NoError(t, db.Create(&[]model.UserGroupMembership{
				{UserId: user.Id, GroupName: "default", SortOrder: 0, Manual: &manual},
				{UserId: user.Id, GroupName: "vip", SortOrder: 1, Manual: &manual},
			}).Error)
			channel := model.Channel{Type: constant.ChannelTypeOpenAI, Name: "status-matrix", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			name := "status-matrix-" + dialect.kind
			emptyName := name + "-no-data"
			require.NoError(t, db.Create(&[]model.Ability{
				{Group: "default", Model: name, ChannelId: channel.Id, Enabled: true},
				{Group: "vip", Model: name, ChannelId: channel.Id, Enabled: true},
				{Group: "default", Model: emptyName, ChannelId: channel.Id, Enabled: true},
			}).Error)
			for _, modelName := range []string{name, emptyName} {
				require.NoError(t, (&model.Model{ModelName: modelName, Status: 1, SyncOfficial: 1}).Insert())
			}
			bucketTs := time.Now().Add(-time.Hour).Truncate(time.Hour).Unix()
			require.NoError(t, db.Create(&[]model.PerfMetric{
				{ModelName: name, Group: "default", BucketTs: bucketTs, RequestCount: 99, SuccessCount: 99, TotalLatencyMs: 99000, TtftSumMs: 9000, TtftCount: 90, OutputTokens: 990, GenerationMs: 99000},
				{ModelName: name, Group: "vip", BucketTs: bucketTs, RequestCount: 1, SuccessCount: 0, TotalLatencyMs: 9000, TtftSumMs: 1000, TtftCount: 1, OutputTokens: 100, GenerationMs: 1000},
				{ModelName: name, Group: "private", BucketTs: bucketTs, RequestCount: 100, SuccessCount: 0},
			}).Error)
			model.InvalidatePricingCache()
			for _, tc := range []struct {
				name, query  string
				anonymous    bool
				code, models int
				requests     int64
				rate         *float64
			}{
				{name: "weighted visible groups", query: "?model=" + name, code: 200, models: 1, requests: 100, rate: lo.ToPtr[float64](99)},
				{name: "default group", query: "?model=" + name + "&group=default", code: 200, models: 1, requests: 99, rate: lo.ToPtr[float64](100)},
				{name: "failed VIP group", query: "?model=" + name + "&group=vip", code: 200, models: 1, requests: 1, rate: lo.ToPtr[float64](0)},
				{name: "all visible models", code: 200, models: 2, requests: 100, rate: lo.ToPtr[float64](99)},
				{name: "VIP excludes default-only model", query: "?group=vip", code: 200, models: 1, requests: 1, rate: lo.ToPtr[float64](0)},
				{name: "visible model without traffic", query: "?model=" + emptyName, code: 200, models: 1},
				{name: "unknown model", query: "?model=nonexistent-model", code: 404},
				{name: "unavailable model group pair", query: "?model=" + emptyName + "&group=vip", code: 404},
				{name: "private group", query: "?group=private", code: 404},
				{name: "inactive group", query: "?group=retired", code: 404},
				{name: "anonymous default only", query: "?model=" + name, anonymous: true, code: 200, models: 1, requests: 99, rate: lo.ToPtr[float64](100)},
				{name: "anonymous cannot choose VIP", query: "?group=vip", anonymous: true, code: 404},
			} {
				t.Run(tc.name, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					context, _ := gin.CreateTestContext(recorder)
					context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/status"+tc.query, nil)
					if !tc.anonymous {
						context.Set("id", user.Id)
					}
					GetPerfMetricsStatus(context)
					require.Equal(t, tc.code, recorder.Code)
					if tc.code != http.StatusOK {
						return
					}
					var payload struct {
						Success bool                  `json:"success"`
						Data    perfMetricsStatusView `json:"data"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
					require.True(t, payload.Success)
					require.Len(t, payload.Data.Models, tc.models)
					result := payload.Data.Models[0]
					require.Len(t, result.Timeline, 24)
					assert.Equal(t, tc.rate, result.SuccessRate)
					require.NotNil(t, result.RequestCount)
					assert.Equal(t, tc.requests, *result.RequestCount)
					for _, point := range result.Timeline {
						assert.NotNil(t, point.RequestCount)
						assert.NotNil(t, point.SuccessCount)
					}
					if tc.name == "weighted visible groups" {
						require.NotNil(t, result.AvgLatencyMs)
						assert.EqualValues(t, 1080, *result.AvgLatencyMs)
						require.NotNil(t, result.AvgTps)
						assert.Equal(t, 10.9, *result.AvgTps)
					}
				})
			}
		})
	}
}

func TestGetPerfMetricsStatusReturnsOnlyVisibleEnabledModels(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))
	vendor := model.Vendor{Name: "Visible Vendor", Icon: "Anthropic.Color", Status: 1}
	require.NoError(t, db.Create(&vendor).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 801, Type: constant.ChannelTypeOpenAI, Name: "status-channel", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "visible-status-model", ChannelId: 801, Enabled: true},
		{Group: "default", Model: "visible-no-data-model", ChannelId: 801, Enabled: true},
		{Group: "vip", Model: "hidden-group-model", ChannelId: 801, Enabled: true},
		{Group: "default", Model: "disabled-status-model", ChannelId: 801, Enabled: true},
	}).Error)
	for _, item := range []*model.Model{
		{ModelName: "visible-status-model", Icon: "Claude.Color", VendorID: vendor.Id, Status: 1, SyncOfficial: 1},
		{ModelName: "visible-no-data-model", VendorID: vendor.Id, Status: 1, SyncOfficial: 1},
		{ModelName: "hidden-group-model", VendorID: vendor.Id, Status: 1, SyncOfficial: 1},
		{ModelName: "disabled-status-model", VendorID: vendor.Id, Status: 0, SyncOfficial: 1},
	} {
		require.NoError(t, item.Insert())
	}

	completedHour := time.Now().Add(-time.Hour).Truncate(time.Hour).Unix()
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: "visible-status-model", Group: "default", BucketTs: completedHour, RequestCount: 2, SuccessCount: 1, TotalLatencyMs: 600, TtftSumMs: 300, TtftCount: 1, OutputTokens: 30, GenerationMs: 1000},
		{ModelName: "visible-status-model", Group: "vip", BucketTs: completedHour, RequestCount: 1, SuccessCount: 0},
		{ModelName: "visible-status-model", Group: "retired", BucketTs: completedHour, RequestCount: 1, SuccessCount: 0},
		{ModelName: "hidden-group-model", Group: "vip", BucketTs: completedHour, RequestCount: 1, SuccessCount: 0},
		{ModelName: "disabled-status-model", Group: "default", BucketTs: completedHour, RequestCount: 1, SuccessCount: 0},
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/status", nil)

	GetPerfMetricsStatus(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload perfMetricsStatusResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Models, 2)
	assert.Equal(t, "visible-status-model", payload.Data.Models[0].ModelName)
	assert.Equal(t, perfmetrics.StatusFailed, payload.Data.Models[0].Status)
	assert.Equal(t, "Anthropic.Color", payload.Data.Models[0].Icon)
	assert.Equal(t, int64(2), payload.Data.Models[0].RequestCount)
	assert.Equal(t, int64(1), payload.Data.Models[0].SuccessCount)
	assert.Contains(t, recorder.Body.String(), `"request_count":2`)
	assert.Contains(t, recorder.Body.String(), `"success_count":1`)
	require.NotNil(t, payload.Data.Models[0].SuccessRate)
	require.NotNil(t, payload.Data.Models[0].AvgTtftMs)
	require.NotNil(t, payload.Data.Models[0].AvgLatencyMs)
	require.NotNil(t, payload.Data.Models[0].AvgTps)
	assert.Equal(t, 50.0, *payload.Data.Models[0].SuccessRate)
	assert.Equal(t, int64(300), *payload.Data.Models[0].AvgTtftMs)
	assert.Equal(t, int64(300), *payload.Data.Models[0].AvgLatencyMs)
	assert.Equal(t, 30.0, *payload.Data.Models[0].AvgTps)
	assert.Equal(t, "Visible Vendor", payload.Data.Models[0].Vendor)
	metricPointIndex := -1
	for index, point := range payload.Data.Models[0].Timeline {
		if point.Ts == completedHour {
			metricPointIndex = index
			break
		}
	}
	require.NotEqual(t, -1, metricPointIndex)
	metricPoint := payload.Data.Models[0].Timeline[metricPointIndex]
	assert.Equal(t, int64(2), metricPoint.RequestCount)
	assert.Equal(t, int64(1), metricPoint.SuccessCount)
	require.NotNil(t, metricPoint.AvgTtftMs)
	require.NotNil(t, metricPoint.AvgLatencyMs)
	require.NotNil(t, metricPoint.AvgTps)
	assert.Equal(t, int64(300), *metricPoint.AvgTtftMs)
	assert.Equal(t, int64(300), *metricPoint.AvgLatencyMs)
	assert.Equal(t, 30.0, *metricPoint.AvgTps)
	assert.Equal(t, "visible-no-data-model", payload.Data.Models[1].ModelName)
	assert.Equal(t, perfmetrics.StatusNoData, payload.Data.Models[1].Status)
	assert.Equal(t, "Anthropic.Color", payload.Data.Models[1].Icon)
	assert.Zero(t, payload.Data.Models[1].RequestCount)
	assert.Zero(t, payload.Data.Models[1].SuccessCount)

	authenticatedRecorder := httptest.NewRecorder()
	authenticatedContext, _ := gin.CreateTestContext(authenticatedRecorder)
	authenticatedContext.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/status", nil)
	authenticatedContext.Set("id", 1)
	GetPerfMetricsStatus(authenticatedContext)
	require.Equal(t, http.StatusOK, authenticatedRecorder.Code)
	var authenticatedPayload perfMetricsStatusResponse
	require.NoError(t, common.Unmarshal(authenticatedRecorder.Body.Bytes(), &authenticatedPayload))
	require.Len(t, authenticatedPayload.Data.Models, 2)
	assert.Equal(t, int64(2), authenticatedPayload.Data.Models[0].RequestCount)
	assert.Equal(t, int64(1), authenticatedPayload.Data.Models[0].SuccessCount)
	metricPointIndex = -1
	for index, point := range authenticatedPayload.Data.Models[0].Timeline {
		if point.Ts == completedHour {
			metricPointIndex = index
			break
		}
	}
	require.NotEqual(t, -1, metricPointIndex)
	assert.Equal(t, int64(2), authenticatedPayload.Data.Models[0].Timeline[metricPointIndex].RequestCount)
	assert.Equal(t, int64(1), authenticatedPayload.Data.Models[0].Timeline[metricPointIndex].SuccessCount)
}

func TestGetPerfMetricsStatusReturnsEmptyWhenNoModelsAreVisible(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))
	require.NoError(t, db.Create(&model.Channel{
		Id:     802,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "hidden-status-key",
		Name:   "hidden-status-channel",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "vip",
		Model:     "hidden-status-model",
		ChannelId: 802,
		Enabled:   true,
	}).Error)
	require.NoError(t, (&model.Model{
		ModelName:    "hidden-status-model",
		Status:       1,
		SyncOfficial: 1,
	}).Insert())
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/status", nil)

	GetPerfMetricsStatus(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload perfMetricsStatusResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.Empty(t, payload.Data.Models)
	assert.Equal(t, 24, payload.Data.WindowHours)
	assert.Positive(t, payload.Data.GeneratedAt)
}

func TestGetPerfMetricsStatusExcludesDisabledAbilitiesAndUnavailableChannels(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 803, Type: constant.ChannelTypeOpenAI, Key: "enabled-status-key", Name: "enabled-status-channel", Status: common.ChannelStatusEnabled},
		{Id: 804, Type: constant.ChannelTypeOpenAI, Key: "disabled-status-key", Name: "disabled-status-channel", Status: common.ChannelStatusManuallyDisabled},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "available-model", ChannelId: 803, Enabled: true},
		{Group: "default", Model: "disabled-ability-model", ChannelId: 803, Enabled: false},
		{Group: "default", Model: "disabled-channel-model", ChannelId: 804, Enabled: true},
		{Group: "default", Model: "missing-channel-model", ChannelId: 899, Enabled: true},
	}).Error)
	for _, modelName := range []string{
		"available-model",
		"disabled-ability-model",
		"disabled-channel-model",
		"missing-channel-model",
	} {
		require.NoError(t, (&model.Model{
			ModelName:    modelName,
			Status:       1,
			SyncOfficial: 1,
		}).Insert())
	}
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/status", nil)

	GetPerfMetricsStatus(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload perfMetricsStatusResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Models, 1)
	assert.Equal(t, "available-model", payload.Data.Models[0].ModelName)
	assert.Equal(t, perfmetrics.StatusNoData, payload.Data.Models[0].Status)
}

func TestGetPerfMetricsStatusUsesLoggedInUsersAssignedGroups(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))
	require.NoError(t, db.Create(&model.User{
		Id:          805,
		Username:    "status-assigned-group-user",
		Password:    "password",
		Group:       "vip",
		Status:      common.UserStatusEnabled,
		AuthVersion: 1,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     805,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "special-group-status-key",
		Name:   "special-group-status-channel",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "special-vip-model", ChannelId: 805, Enabled: true},
		{Group: "default", Model: "removed-default-model", ChannelId: 805, Enabled: true},
	}).Error)
	for _, modelName := range []string{"special-vip-model", "removed-default-model"} {
		require.NoError(t, (&model.Model{
			ModelName:    modelName,
			Status:       1,
			SyncOfficial: 1,
		}).Insert())
	}
	completedHour := time.Now().Add(-time.Hour).Truncate(time.Hour).Unix()
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: "special-vip-model", Group: "vip", BucketTs: completedHour, RequestCount: 1, SuccessCount: 1},
		{ModelName: "special-vip-model", Group: "default", BucketTs: completedHour, RequestCount: 3, SuccessCount: 0},
		{ModelName: "removed-default-model", Group: "default", BucketTs: completedHour, RequestCount: 1, SuccessCount: 0},
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/status", nil)
	context.Set("id", 805)

	GetPerfMetricsStatus(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload perfMetricsStatusResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Models, 1)
	assert.Equal(t, "special-vip-model", payload.Data.Models[0].ModelName)
	assert.Equal(t, perfmetrics.StatusOperational, payload.Data.Models[0].Status)
}

func TestGetPerfMetricsStatusExcludesGroupsOutsideAnonymousAccess(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))
	require.NoError(t, db.Create(&model.Channel{
		Id:     806,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "auto-status-key",
		Name:   "auto-status-channel",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "auto-status-model",
		ChannelId: 806,
		Enabled:   true,
	}).Error)
	require.NoError(t, (&model.Model{
		ModelName:    "auto-status-model",
		Status:       1,
		SyncOfficial: 1,
	}).Insert())
	completedHour := time.Now().Add(-time.Hour).Truncate(time.Hour).Unix()
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: "auto-status-model", Group: "auto", BucketTs: completedHour, RequestCount: 2, SuccessCount: 1},
		{ModelName: "auto-status-model", Group: "retired", BucketTs: completedHour, RequestCount: 2, SuccessCount: 0},
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/status", nil)

	GetPerfMetricsStatus(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload perfMetricsStatusResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Models, 1)
	assert.Nil(t, payload.Data.Models[0].SuccessRate)
	assert.Equal(t, perfmetrics.StatusNoData, payload.Data.Models[0].Status)
}
