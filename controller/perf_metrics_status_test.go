package controller

import (
	"net/http"
	"net/http/httptest"
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
)

type perfMetricsStatusResponse struct {
	Success bool                     `json:"success"`
	Data    perfmetrics.StatusResult `json:"data"`
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
	assert.Equal(t, perfmetrics.StatusDegraded, payload.Data.Models[0].Status)
	assert.Equal(t, "Claude.Color", payload.Data.Models[0].Icon)
	assert.Equal(t, int64(2), payload.Data.Models[0].RequestCount)
	assert.Equal(t, int64(1), payload.Data.Models[0].SuccessCount)
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

func TestGetPerfMetricsStatusUsesLoggedInUsersSpecialGroups(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.ReadAll()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	specialGroups.Clear()
	specialGroups.Set("member", map[string]string{
		"+:vip":     "VIP",
		"-:default": "",
	})
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecialGroups)
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))
	require.NoError(t, db.Create(&model.User{
		Id:          805,
		Username:    "status-special-group-user",
		Password:    "password",
		Group:       "member",
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
