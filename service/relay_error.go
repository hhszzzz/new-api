package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// ShouldRetryRelayError applies retry policy when the caller owns its response
// transport and therefore cannot use Gin's Writer.Written state.
func ShouldRetryRelayError(c *gin.Context, apiErr *types.NewAPIError, retryTimes int) bool {
	if apiErr == nil || ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if c != nil {
		if _, ok := c.Get("specific_channel_id"); ok {
			return false
		}
	}
	if types.IsChannelError(apiErr) {
		return true
	}
	if types.IsSkipRetryError(apiErr) || retryTimes <= 0 {
		return false
	}
	code := apiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(apiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func ProcessChannelError(c *gin.Context, channelError types.ChannelError, apiErr *types.NewAPIError, relayInfo *relaycommon.RelayInfo) {
	if apiErr == nil {
		return
	}
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, apiErr.StatusCode, common.LocalLogPreview(apiErr.Error())))
	if ShouldDisableChannel(apiErr) && channelError.AutoBan {
		gopool.Go(func() {
			DisableChannel(channelError, apiErr.ErrorWithStatusCode())
		})
	}

	if !constant.ErrorLogEnabled || !types.IsRecordErrorLog(apiErr) || c == nil {
		return
	}
	userID := c.GetInt("id")
	tokenName := c.GetString("token_name")
	modelName := c.GetString("original_model")
	tokenID := c.GetInt("token_id")
	userGroup := c.GetString("group")
	channelID := c.GetInt("channel_id")
	other := make(map[string]interface{})
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	other["error_type"] = apiErr.GetErrorType()
	other["error_code"] = apiErr.GetErrorCode()
	other["status_code"] = apiErr.StatusCode
	other["channel_id"] = channelID
	other["channel_name"] = c.GetString("channel_name")
	other["channel_type"] = c.GetInt("channel_type")
	adminInfo := map[string]interface{}{
		"use_channel": c.GetStringSlice("use_channel"),
	}
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		adminInfo["is_multi_key"] = true
		adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	AppendChannelAffinityAdminInfo(c, adminInfo)
	other["admin_info"] = adminInfo
	if relayInfo != nil {
		AppendModelRoutingAdminInfo(other, relayInfo.HasModelRouting(), relayInfo.UpstreamModelName)
	}
	AppendDifyWorkflowAdminInfo(relayInfo, other)
	AppendParamOverrideAdminInfo(relayInfo, other)
	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	model.RecordErrorLog(c, userID, channelID, modelName, tokenName, apiErr.MaskSensitiveErrorWithStatusCode(), tokenID, int(time.Since(startTime).Seconds()), common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
}
