package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const claudeTestCompactionPrompt = "CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.\n- Your entire response must be plain text."

const codexTestCompactionPrompt = "You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task."

// Pi 摘要请求的真实形态（Pi 源码 compaction/utils.ts 的 SUMMARIZATION_SYSTEM_PROMPT
// 与 compaction.ts 的 <conversation> 序列化包装）。
const piTestSystemPrompt = "You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.\n\nDo NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary."

const piTestSummarizationPrompt = "<conversation>\n[User]: one\n[Assistant]: big reply\n[Tool result]: output\n</conversation>\n\nThe messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work."

func TestIsClaudeCompactionRequest(t *testing.T) {
	cases := []struct {
		name     string
		request  *dto.ClaudeRequest
		expected bool
	}{
		{"nil request", nil, false},
		{"empty messages", &dto.ClaudeRequest{}, false},
		{
			"compact prompt as last user message",
			&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
				{Role: "user", Content: claudeTestCompactionPrompt},
			}},
			true,
		},
		{
			"compact prompt in text blocks with padding",
			&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "hello"},
				{Role: "user", Content: []any{
					map[string]any{"type": "text", "text": "\n  " + claudeTestCompactionPrompt},
				}},
			}},
			true,
		},
		{
			"marker only in an earlier turn",
			&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: claudeTestCompactionPrompt},
				{Role: "assistant", Content: "the summary"},
				{Role: "user", Content: "continue"},
			}},
			false,
		},
		{
			"normal conversation",
			&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "hi"},
			}},
			false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, isClaudeCompactionRequest(testCase.request))
		})
	}
}

func TestIsCodexCompactionRequest(t *testing.T) {
	cases := []struct {
		name     string
		request  *dto.OpenAIResponsesRequest
		expected bool
	}{
		{"nil request", nil, false},
		{"empty input", &dto.OpenAIResponsesRequest{}, false},
		{
			"compact prompt as last user item",
			&dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"one"}]},
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"` + codexTestCompactionPrompt + `"}]}
			]`)},
			true,
		},
		{
			"compact prompt as plain string content",
			&dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
				{"type":"message","role":"user","content":"` + codexTestCompactionPrompt + `"}
			]`)},
			true,
		},
		{
			"marker only in an earlier turn",
			&dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"` + codexTestCompactionPrompt + `"}]},
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"the summary"}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`)},
			false,
		},
		{
			"non-message input items are skipped",
			&dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
				{"type":"function_call_output","call_id":"call_1","output":"result"}
			]`)},
			false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, isCodexCompactionRequest(testCase.request))
		})
	}
}

func TestIsPiSummarizationRequest(t *testing.T) {
	marshalItems := func(items []map[string]any) json.RawMessage {
		raw, err := json.Marshal(items)
		require.NoError(t, err)
		return raw
	}
	responsesSummarizeInput := func(systemRole string) json.RawMessage {
		return marshalItems([]map[string]any{
			{"type": "message", "role": systemRole, "content": []map[string]any{{"type": "input_text", "text": piTestSystemPrompt}}},
			{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": piTestSummarizationPrompt}}},
		})
	}

	// Chat：前导 system 消息携带 Pi 摘要提示词（Pi summarize 请求的唯一 system）。
	chatSummarize := &dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "system", Content: piTestSystemPrompt},
		{Role: "user", Content: []any{map[string]any{"type": "text", "text": piTestSummarizationPrompt}}},
	}}
	assert.True(t, isPiSummarizationChatRequest(chatSummarize))

	// system 提示词缺失/变更时，<conversation> 包装兜底。
	assert.True(t, isPiSummarizationChatRequest(&dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "user", Content: piTestSummarizationPrompt},
	}}))

	// 普通对话不命中。
	assert.False(t, isPiSummarizationChatRequest(&dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "system", Content: "You are Pi, a coding agent."},
		{Role: "user", Content: "hello"},
	}}))
	assert.False(t, isPiSummarizationChatRequest(nil))

	// Messages：system 为字符串或内容块数组两种形态。
	assert.True(t, isPiSummarizationClaudeRequest(&dto.ClaudeRequest{
		System: piTestSystemPrompt,
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: []any{map[string]any{"type": "text", "text": piTestSummarizationPrompt}}},
		},
	}))
	assert.True(t, isPiSummarizationClaudeRequest(&dto.ClaudeRequest{
		System:   []any{map[string]any{"type": "text", "text": piTestSystemPrompt}},
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "<conversation>\n[User]: hi\n</conversation>\n\nsummary please"}},
	}))
	assert.False(t, isPiSummarizationClaudeRequest(&dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}))
	assert.False(t, isPiSummarizationClaudeRequest(nil))

	// Responses：前导 developer/system input 项携带摘要提示词。
	assert.True(t, isPiSummarizationResponsesRequest(&dto.OpenAIResponsesRequest{Input: responsesSummarizeInput("developer")}))
	assert.True(t, isPiSummarizationResponsesRequest(&dto.OpenAIResponsesRequest{Input: responsesSummarizeInput("system")}))
	assert.False(t, isPiSummarizationResponsesRequest(&dto.OpenAIResponsesRequest{Input: marshalItems([]map[string]any{
		{"type": "message", "role": "developer", "content": []map[string]any{{"type": "input_text", "text": "You are Pi, a coding agent."}}},
		{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "hello"}}},
	})}))
	assert.False(t, isPiSummarizationResponsesRequest(nil))
}

func TestEffectiveContextLimit(t *testing.T) {
	assert.Equal(t, 0, effectiveContextLimit(0, 0))
	assert.Equal(t, 400000, effectiveContextLimit(400000, 0))
	assert.Equal(t, 128000, effectiveContextLimit(0, 128000))
	assert.Equal(t, 128000, effectiveContextLimit(400000, 128000))
	assert.Equal(t, 400000, effectiveContextLimit(400000, 600000))
}

func TestClaudeContextLimitRejection(t *testing.T) {
	assert.Nil(t, claudeContextLimitRejection(0, 400000))
	assert.Nil(t, claudeContextLimitRejection(985000, 0))
	assert.Nil(t, claudeContextLimitRejection(400000, 400000))
	assert.Nil(t, claudeContextLimitRejection(399999, 400000))

	rejection := claudeContextLimitRejection(985000, 400000)
	require.NotNil(t, rejection)
	assert.Equal(t, http.StatusBadRequest, rejection.StatusCode)
	assert.Equal(t, hosttypes.ErrorCodeContextLimitExceeded, rejection.GetErrorCode())
	assert.Equal(t, "prompt is too long: 985000 tokens > 400000 maximum", rejection.Error())
	assert.True(t, hosttypes.IsSkipRetryError(rejection))

	toClaude := rejection.ToClaudeError()
	assert.Equal(t, "invalid_request_error", toClaude.Type)
	assert.Equal(t, "prompt is too long: 985000 tokens > 400000 maximum", toClaude.Message)
}

func TestOpenAIContextLimitRejection(t *testing.T) {
	assert.Nil(t, openAIContextLimitRejection(0, 400000))
	assert.Nil(t, openAIContextLimitRejection(985000, 0))
	assert.Nil(t, openAIContextLimitRejection(400000, 400000))

	rejection := openAIContextLimitRejection(985000, 400000)
	require.NotNil(t, rejection)
	assert.Equal(t, http.StatusBadRequest, rejection.StatusCode)
	assert.Equal(t, hosttypes.ErrorCodeContextLimitExceeded, rejection.GetErrorCode())
	assert.True(t, hosttypes.IsSkipRetryError(rejection))

	expectedMessage := "This model's maximum context length is 400000 tokens. However, your messages resulted in 985000 tokens. Please reduce the length of the messages."
	toOpenAI := rejection.ToOpenAIError()
	assert.Equal(t, "invalid_request_error", toOpenAI.Type)
	assert.Equal(t, "context_length_exceeded", toOpenAI.Code)
	assert.Equal(t, expectedMessage, toOpenAI.Message)
}

func TestEnforceContextLimit(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	require.NoError(t, db.Create(&model.Channel{
		Id: 31, Type: 1, Key: "key", Name: "ctx-guard-channel", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "ctx-guard-model", ChannelId: 31, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "ctx-guard-model", ContextLimit: 400000, Status: 1}).Error)
	model.RefreshPricing()

	plainChannel := &model.Channel{Id: 31, Type: 1, Key: "key", Name: "ctx-guard-channel", Status: common.ChannelStatusEnabled}
	strictChannel := &model.Channel{Id: 32, Type: 1, Key: "key", Name: "ctx-guard-strict", Status: common.ChannelStatusEnabled}
	strictChannel.SetOtherSettings(hostdto.ChannelOtherSettings{ContextLimits: []hostdto.ChannelContextLimitRule{
		{ModelPattern: "^ctx-guard-model$", ContextLimit: 128000},
	}})

	normalRequest := &dto.ClaudeRequest{Model: "ctx-guard-model", Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}}
	compactRequest := &dto.ClaudeRequest{Model: "ctx-guard-model", Messages: []dto.ClaudeMessage{
		{Role: "user", Content: "hello"},
		{Role: "user", Content: claudeTestCompactionPrompt},
	}}
	newInfo := func(modelName string, estimate int) *relaycommon.RelayInfo {
		info := &relaycommon.RelayInfo{OriginModelName: modelName}
		info.SetEstimatePromptTokens(estimate)
		return info
	}

	// 全局声明生效：超限的普通 Messages 请求被拒，错误文案与 Anthropic 原生一致。
	rejection := enforceContextLimit(types.RelayFormatClaude, normalRequest, newInfo("ctx-guard-model", 985000), plainChannel)
	require.NotNil(t, rejection)
	assert.Equal(t, "prompt is too long: 985000 tokens > 400000 maximum", rejection.Error())

	// 压缩请求本身放行（压缩需要完整历史）。
	assert.Nil(t, enforceContextLimit(types.RelayFormatClaude, compactRequest, newInfo("ctx-guard-model", 985000), plainChannel))

	// 渠道声明更严时取渠道值。
	rejection = enforceContextLimit(types.RelayFormatClaude, normalRequest, newInfo("ctx-guard-model", 985000), strictChannel)
	require.NotNil(t, rejection)
	assert.Equal(t, "prompt is too long: 985000 tokens > 128000 maximum", rejection.Error())

	// 未超限、无法估算、其他协议入口、未声明上限的模型都不拦截。
	assert.Nil(t, enforceContextLimit(types.RelayFormatClaude, normalRequest, newInfo("ctx-guard-model", 1000), plainChannel))
	assert.Nil(t, enforceContextLimit(types.RelayFormatClaude, normalRequest, newInfo("ctx-guard-model", 0), plainChannel))
	unlimitedRequest := &dto.ClaudeRequest{Model: "ctx-guard-unlimited", Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}}
	assert.Nil(t, enforceContextLimit(types.RelayFormatClaude, unlimitedRequest, newInfo("ctx-guard-unlimited", 985000), plainChannel))

	// Responses（Codex）入口：普通超限请求以 OpenAI 原生文案拒绝。
	responsesRequest := &dto.OpenAIResponsesRequest{Model: "ctx-guard-model", Input: json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
	]`)}
	responsesCompactRequest := &dto.OpenAIResponsesRequest{Model: "ctx-guard-model", Input: json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"` + codexTestCompactionPrompt + `"}]}
	]`)}
	rejection = enforceContextLimit(types.RelayFormatOpenAIResponses, responsesRequest, newInfo("ctx-guard-model", 985000), plainChannel)
	require.NotNil(t, rejection)
	assert.Equal(t, "This model's maximum context length is 400000 tokens. However, your messages resulted in 985000 tokens. Please reduce the length of the messages.", rejection.Error())
	assert.Equal(t, "context_length_exceeded", rejection.ToOpenAIError().Code)

	// Codex 本地压缩请求放行；远程压缩（/v1/responses/compact）入口整体不拦截。
	assert.Nil(t, enforceContextLimit(types.RelayFormatOpenAIResponses, responsesCompactRequest, newInfo("ctx-guard-model", 985000), plainChannel))
	assert.Nil(t, enforceContextLimit(types.RelayFormatOpenAIResponsesCompaction, responsesRequest, newInfo("ctx-guard-model", 985000), plainChannel))

	// Chat 入口同样拦截（OpenAI 原生报文）。
	chatRejection := enforceContextLimit(types.RelayFormatOpenAI, responsesRequest, newInfo("ctx-guard-model", 985000), plainChannel)
	require.NotNil(t, chatRejection)
	require.Nil(t, enforceContextLimit(types.RelayFormatOpenAI, responsesRequest, newInfo("ctx-guard-model", 1000), plainChannel))

	// Pi 摘要请求在三类文本入口都放行（超限会话的溢出恢复唯一通路）。
	piChatSummarize := &dto.GeneralOpenAIRequest{Model: "ctx-guard-model", Messages: []dto.Message{
		{Role: "system", Content: piTestSystemPrompt},
		{Role: "user", Content: piTestSummarizationPrompt},
	}}
	assert.Nil(t, enforceContextLimit(types.RelayFormatOpenAI, piChatSummarize, newInfo("ctx-guard-model", 985000), plainChannel))

	piClaudeSummarize := &dto.ClaudeRequest{Model: "ctx-guard-model", System: piTestSystemPrompt, Messages: []dto.ClaudeMessage{
		{Role: "user", Content: piTestSummarizationPrompt},
	}}
	assert.Nil(t, enforceContextLimit(types.RelayFormatClaude, piClaudeSummarize, newInfo("ctx-guard-model", 985000), plainChannel))

	piResponsesInput, err := json.Marshal([]map[string]any{
		{"type": "message", "role": "developer", "content": []map[string]any{{"type": "input_text", "text": piTestSystemPrompt}}},
		{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "<conversation>\n[User]: one\n</conversation>\n\nsummarize"}}},
	})
	require.NoError(t, err)
	piResponsesSummarize := &dto.OpenAIResponsesRequest{Model: "ctx-guard-model", Input: piResponsesInput}
	assert.Nil(t, enforceContextLimit(types.RelayFormatOpenAIResponses, piResponsesSummarize, newInfo("ctx-guard-model", 985000), plainChannel))

	// 回归：普通 Pi 会话请求照常拦截。
	piNormalChat := &dto.GeneralOpenAIRequest{Model: "ctx-guard-model", Messages: []dto.Message{
		{Role: "system", Content: "You are Pi, a coding agent."},
		{Role: "user", Content: "hello"},
	}}
	piNormalRejection := enforceContextLimit(types.RelayFormatOpenAI, piNormalChat, newInfo("ctx-guard-model", 985000), plainChannel)
	require.NotNil(t, piNormalRejection)
	assert.Equal(t, "context_length_exceeded", piNormalRejection.ToOpenAIError().Code)
}

func TestResponsesEnforcementTokensCountsOutputsAndToolResults(t *testing.T) {
	assistantText := strings.Repeat("important assistant production narrative ", 200)
	toolOutput := strings.Repeat("tool result payload line ", 200)
	request := &dto.OpenAIResponsesRequest{
		Model:        "ctx-enforce-model",
		Instructions: json.RawMessage(`"follow the instructions"`),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + assistantText + `"}]},
			{"type":"function_call_output","call_id":"c1","output":"` + toolOutput + `"}
		]`),
	}
	tokens := responsesEnforcementTokens(request, "ctx-enforce-model")
	// 平台估算会跳过 assistant 输出与工具结果；闸门估算必须覆盖它们。
	assistantOnly := service.CountTextToken(assistantText, "ctx-enforce-model")
	require.Positive(t, assistantOnly)
	assert.GreaterOrEqual(t, tokens, assistantOnly)

	assert.Equal(t, 0, responsesEnforcementTokens(nil, "ctx-enforce-model"))
	plain := &dto.OpenAIResponsesRequest{Input: json.RawMessage(`"just some plain text input"`)}
	assert.Positive(t, responsesEnforcementTokens(plain, "ctx-enforce-model"))
}

func TestWriteRelayErrorResponseContextLimitAlwaysJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 上下文上限拦截模拟真实 API 的流前校验：Messages 的 stream 请求也返回 400 JSON。
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	rejection := claudeContextLimitRejection(985000, 400000)
	require.NotNil(t, rejection)
	writeRelayErrorResponse(context, types.RelayFormatClaude, rejection, &relaycommon.RelayInfo{IsStream: true}, nil)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	var parsed struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &parsed))
	assert.Equal(t, "error", parsed.Type)
	assert.Equal(t, "invalid_request_error", parsed.Error.Type)
	assert.Equal(t, "prompt is too long: 985000 tokens > 400000 maximum", parsed.Error.Message)

	// 回归：其余错误的流式路径保持 SSE 事件通道不变。
	otherRecorder := httptest.NewRecorder()
	otherContext, _ := gin.CreateTestContext(otherRecorder)
	otherError := hosttypes.NewErrorWithStatusCode(errors.New("boom"), hosttypes.ErrorCodeBadRequestBody, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	writeRelayErrorResponse(otherContext, types.RelayFormatClaude, otherError, &relaycommon.RelayInfo{IsStream: true}, nil)
	assert.Contains(t, otherRecorder.Header().Get("Content-Type"), "text/event-stream")
}

func TestWriteRelayErrorResponseResponsesContextLimitUsesFailedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Responses 入口：超限拦截以流内失败事件（response.failed）下发，Codex 依据
	// error.code=context_length_exceeded 触发下一轮本地压缩。
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	rejection := openAIContextLimitRejection(985000, 400000)
	require.NotNil(t, rejection)
	writeRelayErrorResponse(context, types.RelayFormatOpenAIResponses, rejection, &relaycommon.RelayInfo{IsStream: true}, nil)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
	body := recorder.Body.String()
	assert.Contains(t, body, "event: response.failed")
	var parsed struct {
		Type           string `json:"type"`
		SequenceNumber int    `json:"sequence_number"`
		Response       struct {
			ID     string `json:"id"`
			Object string `json:"object"`
			Status string `json:"status"`
			Error  struct {
				Type    string `json:"type"`
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
	}
	var dataLine string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	require.NotEmpty(t, dataLine)
	require.NoError(t, common.UnmarshalJsonStr(dataLine, &parsed))
	assert.Equal(t, "response.failed", parsed.Type)
	assert.True(t, strings.HasPrefix(parsed.Response.ID, "resp_"))
	assert.Equal(t, "response", parsed.Response.Object)
	assert.Equal(t, "failed", parsed.Response.Status)
	assert.Equal(t, "invalid_request_error", parsed.Response.Error.Type)
	assert.Equal(t, "context_length_exceeded", parsed.Response.Error.Code)
	assert.Equal(t, "This model's maximum context length is 400000 tokens. However, your messages resulted in 985000 tokens. Please reduce the length of the messages.", parsed.Response.Error.Message)

	// 回归：Responses 的其余流内错误仍走 error 事件通道。
	otherRecorder := httptest.NewRecorder()
	otherContext, _ := gin.CreateTestContext(otherRecorder)
	otherError := hosttypes.NewErrorWithStatusCode(errors.New("boom"), hosttypes.ErrorCodeBadRequestBody, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	writeRelayErrorResponse(otherContext, types.RelayFormatOpenAIResponses, otherError, &relaycommon.RelayInfo{IsStream: true}, nil)
	assert.Contains(t, otherRecorder.Header().Get("Content-Type"), "text/event-stream")
	assert.Contains(t, otherRecorder.Body.String(), `"type":"error"`)
	assert.NotContains(t, otherRecorder.Body.String(), "response.failed")
}

func TestWriteRelayErrorResponseChatContextLimitAlwaysJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Chat 入口：超限拦截模拟真实 OpenAI API 的流前校验，stream 请求也返回 400 JSON。
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	rejection := openAIContextLimitRejection(985000, 400000)
	require.NotNil(t, rejection)
	writeRelayErrorResponse(context, types.RelayFormatOpenAI, rejection, &relaycommon.RelayInfo{IsStream: true}, nil)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	var parsed struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &parsed))
	assert.Equal(t, "invalid_request_error", parsed.Error.Type)
	assert.Equal(t, "context_length_exceeded", parsed.Error.Code)
	assert.Equal(t, "This model's maximum context length is 400000 tokens. However, your messages resulted in 985000 tokens. Please reduce the length of the messages.", parsed.Error.Message)
}
