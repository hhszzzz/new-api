package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProcessChannelErrorKeepsMappedModelOutOfPublicLog(t *testing.T) {
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousGinMode := gin.Mode()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	constant.ErrorLogEnabled = true
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		common.RedisEnabled = previousRedisEnabled
		constant.ErrorLogEnabled = previousErrorLogEnabled
		gin.SetMode(previousGinMode)
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.Create(&model.User{
		Id:       7,
		Username: "privacy-test-user",
		Status:   common.UserStatusEnabled,
	}).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("id", 7)
	ctx.Set("username", "privacy-test-user")
	ctx.Set("token_name", "privacy-test-token")
	ctx.Set("token_id", 9)
	ctx.Set("original_model", "requested-model")
	ctx.Set("group", "default")
	ctx.Set("channel_id", 3)
	ctx.Set("channel_name", "private-channel")
	ctx.Set("channel_type", 1)
	ctx.Set("use_channel", []string{"3"})

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "requested-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         3,
			IsModelMapped:     true,
			UpstreamModelName: "private-upstream-model",
		},
	}
	apiErr := types.NewErrorWithStatusCode(
		errors.New("model private-upstream-model is unavailable"),
		types.ErrorCodeModelNotFound,
		http.StatusNotFound,
	)

	processChannelError(ctx, *types.NewChannelError(3, 1, "private-channel", false, "", false), apiErr, relayInfo)

	var stored model.Log
	require.NoError(t, db.First(&stored).Error)
	assert.Contains(t, stored.Content, "private-upstream-model")
	storedOther, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	storedAdminInfo, ok := storedOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "private-upstream-model", storedAdminInfo["upstream_model_name"])
	assert.Equal(t, true, storedAdminInfo["model_routing_checked"])
	assert.Equal(t, "requested", storedAdminInfo["model_name_scope"])

	publicLogs, total, err := model.GetUserLogs(7, model.LogTypeError, 0, 0, "", "", 0, 10, "", "", "", false)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, publicLogs, 1)
	assert.Equal(t, "requested-model", publicLogs[0].ModelName)
	assert.Equal(t, "status_code=404, model requested-model is unavailable", publicLogs[0].Content)
	assert.NotContains(t, publicLogs[0].Content, "private-upstream-model")
	assert.NotContains(t, publicLogs[0].Other, "private-upstream-model")
	assert.NotContains(t, publicLogs[0].Other, "model_name_scope")
	publicOther, err := common.StrToMap(publicLogs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, string(types.ErrorCodeModelNotFound), publicOther["error_code"])

	adminLogs, total, err := model.GetUserLogs(7, model.LogTypeError, 0, 0, "", "", 0, 10, "", "", "", true)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminLogs, 1)
	assert.Contains(t, adminLogs[0].Content, "private-upstream-model")

	ctx.Set("original_model", "requested-override-model")
	overrideInfo := &relaycommon.RelayInfo{
		OriginModelName: "requested-override-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         3,
			UpstreamModelName: "requested-override-model",
			ParamOverride: map[string]interface{}{
				"model": "private-override-model",
			},
		},
	}
	_, err = relaycommon.ApplyParamOverrideWithRelayInfo(
		[]byte(`{"model":"requested-override-model"}`),
		overrideInfo,
	)
	require.NoError(t, err)
	require.True(t, overrideInfo.IsModelMapped)
	require.Equal(t, "private-override-model", overrideInfo.UpstreamModelName)

	overrideErr := types.WithOpenAIError(types.OpenAIError{
		Message: "model private-override-model: unavailable",
		Type:    "private-override-model-error",
		Code:    "private-override-model",
	}, http.StatusNotFound)
	processChannelError(ctx, *types.NewChannelError(3, 1, "private-channel", false, "", false), overrideErr, overrideInfo)

	var overrideStored model.Log
	require.NoError(t, db.Where("model_name = ?", "requested-override-model").First(&overrideStored).Error)
	overrideStoredOther, err := common.StrToMap(overrideStored.Other)
	require.NoError(t, err)
	overrideAdminInfo, ok := overrideStoredOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "private-override-model", overrideAdminInfo["upstream_model_name"])
	assert.Equal(t, true, overrideAdminInfo["model_routing_checked"])
	assert.Equal(t, "requested", overrideAdminInfo["model_name_scope"])
	assert.Contains(t, overrideAdminInfo, "po")

	publicOverrideLogs, total, err := model.GetUserLogs(7, model.LogTypeError, 0, 0, "requested-override-model", "", 0, 10, "", "", "", false)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, publicOverrideLogs, 1)
	assert.Equal(t, "status_code=404, model requested-override-model: unavailable", publicOverrideLogs[0].Content)
	assert.NotContains(t, publicOverrideLogs[0].Content, "private-override-model")
	assert.NotContains(t, publicOverrideLogs[0].Other, "private-override-model")
	publicOverrideOther, err := common.StrToMap(publicOverrideLogs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, "requested-override-model", publicOverrideOther["error_code"])
	assert.Equal(t, "openai_error", publicOverrideOther["error_type"])
	assert.NotContains(t, publicOverrideOther["error_code"], "private-override-model")
	assert.NotContains(t, publicOverrideOther["error_type"], "private-override-model")

	adminOverrideLogs, total, err := model.GetUserLogs(7, model.LogTypeError, 0, 0, "requested-override-model", "", 0, 10, "", "", "", true)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminOverrideLogs, 1)
	assert.Contains(t, adminOverrideLogs[0].Content, "private-override-model")
	assert.Contains(t, adminOverrideLogs[0].Other, "private-override-model")
	adminOverrideOther, err := common.StrToMap(adminOverrideLogs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, "private-override-model", adminOverrideOther["error_code"])
}
