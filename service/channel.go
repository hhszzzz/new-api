package service

import (
	"fmt"
	hostdto "github.com/QuantumNous/new-api/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

func shouldCloseActiveWebSocketsAfterDisable(channelId int) bool {
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.SysLog(common.LogText("failed to check channel status before closing active websockets: channel_id=%d, error=%v", channelId, err))
		return true
	}
	return channel.Status != common.ChannelStatusEnabled
}

// disable & notify
func DisableChannel(channelError hosttypes.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(common.LogText("channel %q (#%d) has automatic disabling turned off, skipping", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		if !IsChannelAvailableForActiveWebSocket(channelError.ChannelId) {
			CloseActiveWebSocketsForChannel(channelError.ChannelId, ChannelDisabledCloseReason)
		}
		params := map[string]any{"Name": channelError.ChannelName, "Id": channelError.ChannelId, "Reason": reason}
		NotifyChannelWatchers(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), func(lang string) (string, string) {
			return i18n.Translate(lang, i18n.MsgChannelNotifyDisabledSubject, params), i18n.Translate(lang, i18n.MsgChannelNotifyDisabledContent, params)
		})
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		params := map[string]any{"Name": channelName, "Id": channelId}
		NotifyChannelWatchers(formatNotifyType(channelId, common.ChannelStatusEnabled), func(lang string) (string, string) {
			return i18n.Translate(lang, i18n.MsgChannelNotifyEnabledSubject, params), i18n.Translate(lang, i18n.MsgChannelNotifyEnabledContent, params)
		})
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
	params := map[string]any{"Id": channelId, "Model": modelName, "Group": group, "Reason": reason}
	NotifyChannelWatchers(formatNotifyType(channelId, common.ChannelStatusAutoDisabled), func(lang string) (string, string) {
		return i18n.Translate(lang, i18n.MsgChannelModelDisabledSubject, params), i18n.Translate(lang, i18n.MsgChannelModelDisabledContent, params)
	})
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
