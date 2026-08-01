package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostWssConsumeQuotaKeepsRoutedModelAdminOnly(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000)
	seedChannel(t, 1)

	oldDataExportEnabled := common.DataExportEnabled
	common.DataExportEnabled = true
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	ctx.Set("username", "test_user")
	ctx.Set("token_name", "test_token")
	now := time.Now()
	streamStatus := relaycommon.NewStreamStatus()
	streamStatus.RecordError("private-upstream-model frame failed")
	streamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, errors.New("private-upstream-model timed out"))
	relayInfo := &relaycommon.RelayInfo{
		UserId:            1,
		UsingGroup:        "default",
		OriginModelName:   "requested-realtime-model",
		StartTime:         now,
		FirstResponseTime: now,
		IsStream:          true,
		StreamStatus:      streamStatus,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         1,
			IsModelMapped:     true,
			UpstreamModelName: "private-upstream-model",
		},
		PriceData: types.PriceData{
			ModelRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}

	PostWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{}, "")

	var savedLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 1, model.LogTypeConsume).First(&savedLog).Error)
	assert.Equal(t, "requested-realtime-model", savedLog.ModelName)
	rawOther, err := common.StrToMap(savedLog.Other)
	require.NoError(t, err)
	rawAdminInfo, ok := rawOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "private-upstream-model", rawAdminInfo["upstream_model_name"])
	rawStreamStatus, ok := rawOther["stream_status"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "error", rawStreamStatus["status"])

	publicLogs, total, err := model.GetUserLogs(1, model.LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "", false)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, publicLogs, 1)
	assert.Equal(t, "requested-realtime-model", publicLogs[0].ModelName)
	assert.NotContains(t, publicLogs[0].Other, "private-upstream-model")
	publicOther, err := common.StrToMap(publicLogs[0].Other)
	require.NoError(t, err)
	publicStreamStatus, ok := publicOther["stream_status"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "timeout", publicStreamStatus["end_reason"])
	assert.Equal(t, "requested-realtime-model timed out", publicStreamStatus["end_error"])
	assert.Equal(t, []interface{}{"requested-realtime-model frame failed"}, publicStreamStatus["errors"])

	adminLogs, total, err := model.GetUserLogs(1, model.LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "", true)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminLogs, 1)
	adminOther, err := common.StrToMap(adminLogs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := adminOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "private-upstream-model", adminInfo["upstream_model_name"])
	adminStreamStatus, ok := adminOther["stream_status"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "private-upstream-model timed out", adminStreamStatus["end_error"])

	model.CacheQuotaDataLock.Lock()
	quotaModels := make([]string, 0, len(model.CacheQuotaData))
	for _, quotaData := range model.CacheQuotaData {
		quotaModels = append(quotaModels, quotaData.ModelName)
	}
	model.CacheQuotaDataLock.Unlock()
	require.Equal(t, []string{"requested-realtime-model"}, quotaModels)
}
