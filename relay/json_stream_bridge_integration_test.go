package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	claudechannel "github.com/QuantumNous/new-api/relay/channel/claude"
	geminichannel "github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromotedChatJSONRunsThroughResponsesStreamHandler(t *testing.T) {
	withProtocolBridgeStreamTestMode(t)
	response := jsonProtocolResponse(`{
		"id":"chatcmpl_upstream","object":"chat.completion","created":1710000000,"model":"provider-chat-model",
		"choices":[{"index":0,"message":{"role":"assistant","content":"hello","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":2}}
	}`)
	require.NoError(t, helper.PromoteJSONResponseToSSE(response, types.RelayFormatOpenAI))

	ctx, recorder := protocolBridgeStreamContext("/v1/responses")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-public",
		IsStream:        true,
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-chat-model",
			IsModelMapped:     true,
		},
	}

	usage, apiErr := openai.OaiChatToResponsesStreamHandler(ctx, info, response)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	got := recorder.Body.String()
	assert.Contains(t, got, `event: response.created`)
	assert.Contains(t, got, `"model":"gpt-public"`)
	assert.Contains(t, got, `event: response.function_call_arguments.done`)
	assert.Contains(t, got, `"name":"lookup"`)
	assert.Contains(t, got, `"arguments":"{\"q\":\"x\"}"`)
	assert.Contains(t, got, `event: response.completed`)
	assert.Contains(t, got, `"cached_tokens":2`)
}

func TestPromotedResponsesJSONRunsThroughMessagesStreamHandler(t *testing.T) {
	withProtocolBridgeStreamTestMode(t)
	response := jsonProtocolResponse(`{
		"id":"resp_upstream","object":"response","created_at":1710000000,"model":"provider-responses-model","status":"completed",
		"output":[
			{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]},
			{"id":"fc_1","type":"function_call","status":"completed","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}
		],
		"usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11,"input_tokens_details":{"cached_tokens":2}}
	}`)
	require.NoError(t, helper.PromoteJSONResponseToSSE(response, types.RelayFormatOpenAIResponses))

	ctx, recorder := protocolBridgeStreamContext("/v1/messages")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-public",
		IsStream:        true,
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-responses-model",
			IsModelMapped:     true,
		},
	}

	usage, apiErr := openai.OaiResponsesToChatStreamHandler(ctx, info, response)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	got := recorder.Body.String()
	assert.Contains(t, got, `event: message_start`)
	assert.Contains(t, got, `"model":"claude-public"`)
	assert.Contains(t, got, `"text":"hello"`)
	assert.Contains(t, got, `"type":"tool_use"`)
	assert.Contains(t, got, `"name":"lookup"`)
	assert.Contains(t, got, `"cache_read_input_tokens":2`)
	assert.Contains(t, got, `event: message_stop`)
}

func TestPromotedClaudeJSONRunsThroughResponsesStreamHandler(t *testing.T) {
	withProtocolBridgeStreamTestMode(t)
	response := jsonProtocolResponse(`{
		"id":"msg_upstream","type":"message","role":"assistant","model":"provider-claude-model",
		"content":[
			{"type":"thinking","thinking":"inspect"},
			{"type":"text","text":"hello"},
			{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}}
		],
		"stop_reason":"tool_use","usage":{"input_tokens":8,"cache_read_input_tokens":2,"output_tokens":3}
	}`)
	require.NoError(t, helper.PromoteJSONResponseToSSE(response, types.RelayFormatClaude))

	ctx, recorder := protocolBridgeStreamContext("/v1/responses")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeUnknown,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-public",
		IsStream:        true,
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-claude-model",
			IsModelMapped:     true,
		},
	}

	usage, apiErr := claudechannel.ClaudeStreamHandler(ctx, response, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	got := recorder.Body.String()
	assert.Contains(t, got, `event: response.created`)
	assert.Contains(t, got, `"model":"gpt-public"`)
	assert.Contains(t, got, `response.reasoning_summary_text.delta`)
	assert.Contains(t, got, `"delta":"inspect"`)
	assert.Contains(t, got, `response.output_text.delta`)
	assert.Contains(t, got, `"delta":"hello"`)
	assert.Contains(t, got, `response.function_call_arguments.done`)
	assert.Contains(t, got, `event: response.completed`)
}

func TestPromotedGeminiJSONRunsThroughResponsesStreamHandler(t *testing.T) {
	withProtocolBridgeStreamTestMode(t)
	response := jsonProtocolResponse(`{
		"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3,"totalTokenCount":11,"cachedContentTokenCount":2}
	}`)
	require.NoError(t, helper.PromoteJSONResponseToSSE(response, types.RelayFormatGemini))

	ctx, recorder := protocolBridgeStreamContext("/v1/responses")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-public",
		IsStream:        true,
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-gemini-model",
			IsModelMapped:     true,
		},
	}

	usage, apiErr := geminichannel.GeminiResponsesStreamHandler(ctx, info, response)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	got := recorder.Body.String()
	assert.Contains(t, got, `event: response.created`)
	assert.Contains(t, got, `"model":"gpt-public"`)
	assert.Contains(t, got, `event: response.output_text.delta`)
	assert.Contains(t, got, `"delta":"hello"`)
	assert.Contains(t, got, `event: response.completed`)
	assert.Contains(t, got, `"cached_tokens":2`)
}

func jsonProtocolResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func protocolBridgeStreamContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, path, nil)
	return ctx, recorder
}

func withProtocolBridgeStreamTestMode(t *testing.T) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
}
