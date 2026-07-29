package xai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXAIChatUpstreamReturnsResponsesJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := xaiProtocolBridgeInfo(types.RelayFormatOpenAIResponses, relayconstant.RelayModeChatCompletions, false)

	usage, apiErr := (&Adaptor{}).DoResponse(ctx, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl_xai","object":"chat.completion","created":1710000000,"model":"grok-upstream",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":7}
		}`)),
	}, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	usageDto, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 2, usageDto.CompletionTokens)
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "response", response.Object)
	assert.Equal(t, "public-model", response.Model)
	assert.Equal(t, 2, response.Usage.OutputTokens)
	assert.Equal(t, "hello", response.Output[0].Content[0].Text)
}

func TestXAIChatUpstreamReturnsMessagesSSE(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := xaiProtocolBridgeInfo(types.RelayFormatClaude, relayconstant.RelayModeChatCompletions, true)
	info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_xai","object":"chat.completion.chunk","created":1710000000,"model":"grok-upstream","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_xai","object":"chat.completion.chunk","created":1710000000,"model":"grok-upstream","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_xai","object":"chat.completion.chunk","created":1710000000,"model":"grok-upstream","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"chatcmpl_xai","object":"chat.completion.chunk","created":1710000000,"model":"grok-upstream","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":7}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	usage, apiErr := (&Adaptor{}).DoResponse(ctx, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	usageDto, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 2, usageDto.CompletionTokens)
	got := recorder.Body.String()
	assert.Contains(t, got, `event: message_start`)
	assert.Contains(t, got, `"model":"public-model"`)
	assert.Contains(t, got, `"text":"hello"`)
	assert.Contains(t, got, `"output_tokens":2`)
	assert.Contains(t, got, `event: message_stop`)
}

func TestXAIResponsesUpstreamReturnsMessagesJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := xaiProtocolBridgeInfo(types.RelayFormatClaude, relayconstant.RelayModeResponses, false)

	usage, apiErr := (&Adaptor{}).DoResponse(ctx, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_xai","object":"response","created_at":1710000000,"model":"grok-upstream","status":"completed",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]}],
			"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}
		}`)),
	}, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	var response dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "message", response.Type)
	assert.Equal(t, "public-model", response.Model)
	assert.Equal(t, "hello", response.Content[0].GetText())
}

func TestNormalizeXAIUsageNeverProducesNegativeTokens(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:           10,
		TotalTokens:            5,
		CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 3},
	}

	normalizeXAIUsage(usage)

	assert.Zero(t, usage.CompletionTokens)
	assert.Zero(t, usage.CompletionTokenDetails.TextTokens)
}

func TestNormalizeXAIUsageKeepsCompletionTokensWhenTotalOmitted(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 7,
	}

	normalizeXAIUsage(usage)

	assert.Equal(t, 7, usage.CompletionTokens)
	assert.Equal(t, 17, usage.TotalTokens)
	assert.Equal(t, 7, usage.CompletionTokenDetails.TextTokens)
}

func xaiProtocolBridgeInfo(entryFormat types.RelayFormat, relayMode int, stream bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode:       relayMode,
		RelayFormat:     entryFormat,
		IsStream:        stream,
		DisablePing:     true,
		OriginModelName: "public-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeXai,
			UpstreamModelName: "grok-upstream",
			IsModelMapped:     true,
		},
	}
}
