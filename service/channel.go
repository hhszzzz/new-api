package service

import (
	"fmt"
	hostdto "github.com/QuantumNous/new-api/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError hosttypes.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		if !IsChannelAvailableForActiveWebSocket(channelError.ChannelId) {
			CloseActiveWebSocketsForChannel(channelError.ChannelId, ChannelDisabledCloseReason)
		}
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelError.ChannelName, channelError.ChannelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason)
		NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}

// DisableChannelModel disables one (group, model) on a channel, leaving the rest
// of the channel usable. An empty group disables the model across every group of
// the channel. Source distinguishes auto-disabled from manually disabled entries.
func DisableChannelModel(channelId int, group string, modelName string, reason string, source string) bool {
	if strings.TrimSpace(modelName) == "" {
		return false
	}
	if source == "" {
		source = hostdto.DisabledModelSourceAuto
	}
	entry := hostdto.DisabledModelEntry{
		Group:  group,
		Model:  modelName,
		Source: source,
		Reason: reason,
		Time:   common.GetTimestamp(),
	}
	if err := model.SetChannelModelDisabled(channelId, entry, true); err != nil {
		common.SysLog(fmt.Sprintf("failed to disable model on channel: channel_id=%d, model=%s, error=%v", channelId, modelName, err))
		return false
	}
	subject := fmt.Sprintf("渠道 #%d 模型「%s」已被禁用", channelId, modelName)
	content := fmt.Sprintf("渠道 #%d 模型「%s」已被禁用，分组：%s，原因：%s", channelId, modelName, group, reason)
	NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusAutoDisabled), subject, content)
	return true
}

// EnableChannelModel clears a disabled (group, model) entry and restores
// routing when the channel itself is enabled.
func EnableChannelModel(channelId int, group string, modelName string) bool {
	if strings.TrimSpace(modelName) == "" {
		return false
	}
	entry := hostdto.DisabledModelEntry{Group: group, Model: modelName}
	if err := model.SetChannelModelDisabled(channelId, entry, false); err != nil {
		common.SysLog(fmt.Sprintf("failed to enable model on channel: channel_id=%d, model=%s, error=%v", channelId, modelName, err))
		return false
	}
	return true
}

// DisableChannelOrModel applies the channel's auto-disable policy. When
// DisableModelOnError is on, only the failing (group, model) is disabled;
// otherwise the legacy whole-channel disable runs. Multi-key channels keep the
// existing key/channel handling.
func DisableChannelOrModel(channelError hosttypes.ChannelError, group string, modelName string, reason string) {
	channel, err := model.GetChannelById(channelError.ChannelId, false)
	if err == nil && strings.TrimSpace(modelName) != "" && !channel.ChannelInfo.IsMultiKey &&
		channel.GetOtherSettings().DisableModelOnError {
		DisableChannelModel(channelError.ChannelId, group, modelName, reason, hostdto.DisabledModelSourceAuto)
		return
	}
	DisableChannel(channelError, reason)
}

func ShouldDisableChannel(err *hosttypes.NewAPIError) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if err == nil {
		return false
	}
	if hosttypes.IsChannelError(err) {
		return true
	}
	if hosttypes.IsSkipRetryError(err) {
		return false
	}
	if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywords, true)
	return search
}

func ShouldEnableChannel(newAPIError *hosttypes.NewAPIError, status int) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	return true
}
