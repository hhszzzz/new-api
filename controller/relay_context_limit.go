package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
)

// claudeCompactionPromptMarker 是 Claude Code 本地压缩（full / partial compact）
// 追加在会话末尾的合成 user 消息开头（NO_TOOLS_PREAMBLE）。
const claudeCompactionPromptMarker = "CRITICAL: Respond with TEXT ONLY"

// isClaudeCompactionRequest reports whether the Messages request is Claude Code's
// local context-compaction turn: the whole conversation plus an appended summary
// instruction. Such requests must reach the upstream even when they exceed the
// declared context window, because compacting the full history is exactly how
// the session recovers; the upstream model can still summarize at that length.
func isClaudeCompactionRequest(request *dto.ClaudeRequest) bool {
	if request == nil {
		return false
	}
	for i := len(request.Messages) - 1; i >= 0; i-- {
		message := request.Messages[i]
		if message.Role != "user" {
			continue
		}
		return strings.HasPrefix(strings.TrimSpace(message.GetStringContent()), claudeCompactionPromptMarker)
	}
	return false
}

// effectiveContextLimit 合并全局（模型元数据）与渠道级声明：两者都声明时取更严者，
// 只声明其一时用该值，都未声明返回 0（不限制）。
func effectiveContextLimit(globalLimit, channelLimit int) int {
	if channelLimit > 0 && (globalLimit <= 0 || channelLimit < globalLimit) {
		return channelLimit
	}
	return globalLimit
}

// claudeContextLimitRejection 在估算输入超过生效上限时返回 Anthropic 协议原生的
// prompt-too-long 400（客户端据此触发本地压缩）。上限未声明或无法估算（estimate<=0）
// 时不干预。
func claudeContextLimitRejection(estimate, limit int) *hosttypes.NewAPIError {
	if estimate <= 0 || limit <= 0 || estimate <= limit {
		return nil
	}
	return hosttypes.NewErrorWithStatusCode(
		fmt.Errorf("prompt is too long: %d tokens > %d maximum", estimate, limit),
		hosttypes.ErrorCodeContextLimitExceeded,
		http.StatusBadRequest,
		hosttypes.ErrOptionWithSkipRetry(),
	)
}

// enforceClaudeContextLimit 是 Messages 入口的上下文上限闸门：普通超限请求以
// prompt-too-long 400 拒绝（触发客户端本地压缩、不重试其他渠道），压缩请求本身
// 放行。仅作用于 Anthropic Messages 入口，其余协议不受影响。
func enforceClaudeContextLimit(relayFormat types.RelayFormat, request dto.Request, info *relaycommon.RelayInfo, channel *model.Channel) *hosttypes.NewAPIError {
	if relayFormat != types.RelayFormatClaude || info == nil || channel == nil {
		return nil
	}
	estimate := info.GetEstimatePromptTokens()
	if estimate <= 0 {
		return nil
	}
	claudeRequest, ok := request.(*dto.ClaudeRequest)
	if !ok || isClaudeCompactionRequest(claudeRequest) {
		return nil
	}
	globalLimit, _ := model.GetModelContextLimit(info.OriginModelName)
	channelSettings := channel.GetOtherSettings()
	channelLimit, _ := channelSettings.ResolveContextLimit(info.OriginModelName)
	return claudeContextLimitRejection(estimate, effectiveContextLimit(globalLimit, channelLimit))
}
