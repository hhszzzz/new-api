package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	hosttypes "github.com/QuantumNous/new-api/types"
)

// claudeCompactionPromptMarker 是 Claude Code 本地压缩（full / partial compact）
// 追加在会话末尾的合成 user 消息开头（NO_TOOLS_PREAMBLE）。
const claudeCompactionPromptMarker = "CRITICAL: Respond with TEXT ONLY"

// codexCompactionPromptMarker 是 Codex 本地压缩（手动 /compact 或自动压缩）追加在
// input 末尾的合成 user 消息开头（codex 仓库 prompts/templates/compact/prompt.md）。
const codexCompactionPromptMarker = "You are performing a CONTEXT CHECKPOINT COMPACTION"

// piSummarizationSystemPromptMarker 是 Pi（pi.dev）摘要请求的 system 提示词开头
// （Pi 源码 packages/coding-agent/src/core/compaction/utils.ts 的
// SUMMARIZATION_SYSTEM_PROMPT）。Pi 的自动/手动压缩、turn-prefix 压缩与分支摘要
// 共用该提示词，system 前缀可完整识别全部摘要形态。
const piSummarizationSystemPromptMarker = "You are a context summarization assistant."

// piSummarizationPromptMarker 是 Pi 摘要把序列化会话包进 user 消息的标签开头
// （compaction.ts / branch-summarization.ts：`<conversation>\n...\n</conversation>`）。
// 作为 system 提示词之外的兜底识别信号。
const piSummarizationPromptMarker = "<conversation>"

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

// isCodexCompactionRequest reports whether the Responses request is Codex's local
// context-compaction turn: the whole conversation plus an appended handoff-summary
// instruction. Like Claude Code's compact turn it must reach the upstream even
// when it exceeds the declared context window. Remote compaction
// (/v1/responses/compact) is a separate relay format and is never gated.
func isCodexCompactionRequest(request *dto.OpenAIResponsesRequest) bool {
	if request == nil || len(request.Input) == 0 {
		return false
	}
	var items []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := common.Unmarshal(request.Input, &items); err != nil {
		return false
	}
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Role != "user" {
			continue
		}
		return strings.HasPrefix(strings.TrimSpace(responsesContentText(items[i].Content)), codexCompactionPromptMarker)
	}
	return false
}

// responsesContentText 提取 Responses input 中一条消息的文本：content 要么是
// 纯字符串，要么是内容块数组（拼接全部 text 字段）。
func responsesContentText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var text string
	if common.Unmarshal(content, &text) == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if common.Unmarshal(content, &parts) == nil {
		var builder strings.Builder
		for _, part := range parts {
			builder.WriteString(part.Text)
		}
		return builder.String()
	}
	return ""
}

// piSummarizationText 判定 system 与最后一条 user 文本是否构成 Pi 摘要请求：
// system 命中 Pi 摘要提示词，或 user 消息以序列化会话包装开头。
func piSummarizationText(systemText, lastUserText string) bool {
	if strings.HasPrefix(strings.TrimSpace(systemText), piSummarizationSystemPromptMarker) {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(lastUserText), piSummarizationPromptMarker)
}

// isPiSummarizationClaudeRequest reports whether a Messages request is Pi's
// compaction / branch-summarization turn routed over the Anthropic protocol.
// Like Claude Code's compact turn it must reach the upstream even when the whole
// session (the serialized conversation inside the user message) exceeds the
// declared window; the summarization model can still produce the summary.
func isPiSummarizationClaudeRequest(request *dto.ClaudeRequest) bool {
	if request == nil {
		return false
	}
	systemText := request.GetStringSystem()
	if systemText == "" {
		var builder strings.Builder
		for _, media := range request.ParseSystem() {
			if media.Type == dto.ContentTypeText {
				builder.WriteString(media.GetText())
			}
		}
		systemText = builder.String()
	}
	lastUserText := ""
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role != "user" {
			continue
		}
		lastUserText = request.Messages[i].GetStringContent()
		break
	}
	return piSummarizationText(systemText, lastUserText)
}

// isPiSummarizationChatRequest reports whether a Chat request is Pi's compaction /
// branch-summarization turn routed over the OpenAI Chat Completions protocol.
// Pi's summarize request is a leading system message (Pi's summarization system
// prompt) plus a single user message carrying the serialized conversation; it
// must be admitted even when over the window or Pi's overflow recovery deadlocks
// ("Context overflow recovery failed after one compact-and-retry attempt").
func isPiSummarizationChatRequest(request *dto.GeneralOpenAIRequest) bool {
	if request == nil {
		return false
	}
	systemText := ""
	for _, message := range request.Messages {
		if !strings.EqualFold(message.Role, "system") && !strings.EqualFold(message.Role, "developer") {
			continue
		}
		systemText = chatMessageText(message)
		break
	}
	lastUserText := ""
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if !strings.EqualFold(request.Messages[i].Role, "user") {
			continue
		}
		lastUserText = chatMessageText(request.Messages[i])
		break
	}
	return piSummarizationText(systemText, lastUserText)
}

// isPiSummarizationResponsesRequest reports whether a Responses request is Pi's
// summarization turn routed over the OpenAI Responses protocol (Pi puts the
// summarization system prompt in the leading developer/system input item).
func isPiSummarizationResponsesRequest(request *dto.OpenAIResponsesRequest) bool {
	if request == nil || len(request.Input) == 0 {
		return false
	}
	var items []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := common.Unmarshal(request.Input, &items); err != nil {
		return false
	}
	systemText := ""
	if len(request.Instructions) > 0 {
		var instructions string
		if common.Unmarshal(request.Instructions, &instructions) == nil {
			systemText = instructions
		}
	}
	if systemText == "" {
		for _, item := range items {
			if !strings.EqualFold(item.Role, "system") && !strings.EqualFold(item.Role, "developer") {
				continue
			}
			systemText = responsesContentText(item.Content)
			break
		}
	}
	lastUserText := ""
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Role != "user" {
			continue
		}
		lastUserText = responsesContentText(items[i].Content)
		break
	}
	return piSummarizationText(systemText, lastUserText)
}

// chatMessageText 拼接一条 Chat 消息的全部文本内容（字符串或内容块数组）。
func chatMessageText(message dto.Message) string {
	var builder strings.Builder
	for _, part := range message.ParseContent() {
		if part.Type == dto.ContentTypeText {
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}

// responsesEnforcementTokens 统计 Responses 请求的完整输入规模。平台估算
// （GetTokenCountMeta → ParseInput）只覆盖 input_text/input_image/input_file，
// 不含 assistant 输出（output_text）与 function_call_output（工具结果）；
// 对 Codex 这类工具型会话会严重低估，长会话将漏过闸门。这里是执行侧专用的
// 全量文本估算：instructions + 每个 input 项的 content/output/arguments 里
// 的全部文本，交给分词器计数。
func responsesEnforcementTokens(request *dto.OpenAIResponsesRequest, modelName string) int {
	if request == nil {
		return 0
	}
	var builder strings.Builder
	if len(request.Instructions) > 0 {
		var instructions string
		if common.Unmarshal(request.Instructions, &instructions) == nil {
			builder.WriteString(instructions)
			builder.WriteByte('\n')
		}
	}
	if len(request.Input) > 0 {
		var input string
		if common.Unmarshal(request.Input, &input) == nil {
			builder.WriteString(input)
		} else {
			var items []struct {
				Content   json.RawMessage `json:"content"`
				Output    json.RawMessage `json:"output"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if common.Unmarshal(request.Input, &items) == nil {
				for _, item := range items {
					for _, raw := range []json.RawMessage{item.Content, item.Output, item.Arguments} {
						if text := responsesContentText(raw); text != "" {
							builder.WriteString(text)
							builder.WriteByte('\n')
						}
					}
				}
			}
		}
	}
	return service.CountTextToken(builder.String(), modelName)
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
// prompt-too-long 400（Claude Code 据此触发本地压缩）。上限未声明或无法估算
// （estimate<=0）时不干预。
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

// openAIContextLimitRejection 返回 OpenAI 协议原生的 context_length_exceeded 400：
// 客户端可见的 code 经由 RelayError 渲染，内部识别码保持 ErrorCodeContextLimitExceeded
// （不能与上游透传的同名 code 混淆）。Responses 入口的该错误会在
// writeRelayErrorResponse 中改写为流内 response.failed 事件——客户端（Codex）
// 只有在流内事件里才能识别该 code 并触发本地压缩。
func openAIContextLimitRejection(estimate, limit int) *hosttypes.NewAPIError {
	if estimate <= 0 || limit <= 0 || estimate <= limit {
		return nil
	}
	message := fmt.Sprintf(
		"This model's maximum context length is %d tokens. However, your messages resulted in %d tokens. Please reduce the length of the messages.",
		limit, estimate,
	)
	return hosttypes.WithOpenAIError(hosttypes.OpenAIError{
		Message: message,
		Type:    "invalid_request_error",
		Code:    "context_length_exceeded",
	}, http.StatusBadRequest,
		hosttypes.ErrOptionWithSkipRetry(),
		hosttypes.ErrOptionWithErrorCode(hosttypes.ErrorCodeContextLimitExceeded),
	)
}

// enforceContextLimit 是文本入口的上下文上限闸门：普通超限请求以各协议原生的
// 超限错误拒绝（触发客户端本地压缩、不换渠道重试），压缩/摘要请求本身放行。
// Messages（Claude Code）与 Responses（Codex）各有本地压缩标记可识别；Pi 的
// 压缩/分支摘要请求在三类文本入口都以自身 system 提示词（或 <conversation>
// 包装）识别并放行——Pi 的溢出恢复只做一次 compact-and-retry，摘要请求若被拒
// 会话即死锁（"Context overflow recovery failed"）。其余入口（含
// /v1/responses/compact 远程压缩通道）不在拦截范围。
func enforceContextLimit(relayFormat types.RelayFormat, request dto.Request, info *relaycommon.RelayInfo, channel *model.Channel) *hosttypes.NewAPIError {
	if info == nil || channel == nil {
		return nil
	}
	estimate := info.GetEstimatePromptTokens()
	if estimate <= 0 {
		return nil
	}
	rejection := claudeContextLimitRejection
	switch relayFormat {
	case types.RelayFormatClaude:
		if claudeRequest, ok := request.(*dto.ClaudeRequest); ok &&
			(isClaudeCompactionRequest(claudeRequest) || isPiSummarizationClaudeRequest(claudeRequest)) {
			return nil
		}
	case types.RelayFormatOpenAIResponses:
		responsesRequest, ok := request.(*dto.OpenAIResponsesRequest)
		if ok && (isCodexCompactionRequest(responsesRequest) || isPiSummarizationResponsesRequest(responsesRequest)) {
			return nil
		}
		// 平台估算不含 assistant 输出与工具结果；闸门以全量文本估算兜底。
		if ok {
			if full := responsesEnforcementTokens(responsesRequest, info.OriginModelName); full > estimate {
				estimate = full
			}
		}
		rejection = openAIContextLimitRejection
	case types.RelayFormatOpenAI:
		if chatRequest, ok := request.(*dto.GeneralOpenAIRequest); ok && isPiSummarizationChatRequest(chatRequest) {
			return nil
		}
		rejection = openAIContextLimitRejection
	default:
		return nil
	}
	globalLimit, _ := model.GetModelContextLimit(info.OriginModelName)
	channelSettings := channel.GetOtherSettings()
	channelLimit, _ := channelSettings.ResolveContextLimit(info.OriginModelName)
	return rejection(estimate, effectiveContextLimit(globalLimit, channelLimit))
}
