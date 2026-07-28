package openai

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

func TestResponsesUpstreamReturnsMessagesJSONWithPublicModel(t *testing.T) {
	body := `{"id":"resp_upstream","object":"response","created_at":1710000000,"model":"provider-responses-model","status":"completed","output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11,"input_tokens_details":{"cached_tokens":2}}}`
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info.RelayFormat = types.RelayFormatClaude
	info.OriginModelName = "claude-public"
	info.ChannelMeta.UpstreamModelName = "provider-responses-model"

	usage, apiErr := OaiResponsesToChatHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 8, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)
	assert.Equal(t, 11, usage.TotalTokens)

	var response dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "message", response.Type)
	assert.Equal(t, "claude-public", response.Model)
	require.Len(t, response.Content, 1)
	assert.Equal(t, "hello", response.Content[0].GetText())
	require.NotNil(t, response.Usage)
	assert.Equal(t, 8, response.Usage.InputTokens)
	assert.Equal(t, 2, response.Usage.CacheReadInputTokens)
	assert.Equal(t, 3, response.Usage.OutputTokens)
}

func TestResponsesUpstreamReturnsMessagesSSEWithPublicModel(t *testing.T) {
	withOpenAIProtocolStreamTestMode(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_upstream","model":"provider-responses-model","created_at":1710000000}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11,"input_tokens_details":{"cached_tokens":2}}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info.RelayFormat = types.RelayFormatClaude
	info.OriginModelName = "claude-public"
	info.ChannelMeta.UpstreamModelName = "provider-responses-model"

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)

	got := recorder.Body.String()
	require.Contains(t, got, `event: message_start`)
	require.Contains(t, got, `"model":"claude-public"`)
	require.Contains(t, got, `event: content_block_delta`)
	require.Contains(t, got, `"text":"hello"`)
	require.Contains(t, got, `event: message_delta`)
	require.Contains(t, got, `"input_tokens":8`)
	require.Contains(t, got, `"cache_read_input_tokens":2`)
	require.Contains(t, got, `event: message_stop`)
	requireOrderedSubstrings(t, got,
		`event: message_start`,
		`event: content_block_start`,
		`event: content_block_delta`,
		`event: content_block_stop`,
		`event: message_delta`,
		`event: message_stop`,
	)
}

func TestChatUpstreamReturnsMessagesJSONWithPublicModel(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := `{"id":"chatcmpl_upstream","object":"chat.completion","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":2}}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-public",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-chat-model",
		},
	}

	usage, apiErr := OpenaiHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)

	var response dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "claude-public", response.Model)
	require.Len(t, response.Content, 1)
	assert.Equal(t, "hello", response.Content[0].GetText())
	require.NotNil(t, response.Usage)
	assert.Equal(t, 2, response.Usage.CacheReadInputTokens)
}

func TestChatUpstreamReturnsMessagesSSEWithPublicModel(t *testing.T) {
	withOpenAIProtocolStreamTestMode(t)
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":2}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.RelayFormat = types.RelayFormatClaude
	info.OriginModelName = "claude-public"
	info.ChannelMeta.UpstreamModelName = "provider-chat-model"
	info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)

	got := recorder.Body.String()
	require.Contains(t, got, `event: message_start`)
	require.Contains(t, got, `"model":"claude-public"`)
	require.Contains(t, got, `event: content_block_delta`)
	require.Contains(t, got, `"text":"hello"`)
	require.Contains(t, got, `event: message_delta`)
	require.Contains(t, got, `"cache_read_input_tokens":2`)
	require.Contains(t, got, `event: message_stop`)
}

func TestChatUpstreamReturnsResponsesJSONWithPublicModel(t *testing.T) {
	body := `{"id":"chatcmpl_upstream","object":"chat.completion","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":2}}}`
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.OriginModelName = "gpt-public"
	info.ChannelMeta.UpstreamModelName = "provider-chat-model"
	info.ChannelMeta.IsModelMapped = true

	usage, apiErr := OaiChatToResponsesHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "response", response.Object)
	assert.Equal(t, "gpt-public", response.Model)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "hello", response.Output[0].Content[0].Text)
	require.NotNil(t, response.Usage)
	assert.Equal(t, 2, response.Usage.InputTokensDetails.CachedTokens)
}

func TestNativeResponsesJSONKeepsPublicModel(t *testing.T) {
	body := `{"id":"resp_upstream","object":"response","created_at":1710000000,"model":"provider-responses-model","status":"completed","output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11}}`
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.OriginModelName = "gpt-public"
	info.ChannelMeta.UpstreamModelName = "provider-responses-model"
	info.ChannelMeta.IsModelMapped = true

	usage, apiErr := OaiResponsesHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "gpt-public", response.Model)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "hello", response.Output[0].Content[0].Text)
}

func TestNativeResponsesSSEKeepsPublicModel(t *testing.T) {
	withOpenAIProtocolStreamTestMode(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_upstream","object":"response","model":"provider-responses-model","created_at":1710000000,"status":"in_progress"}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_upstream","object":"response","model":"provider-responses-model","status":"completed","usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.OriginModelName = "gpt-public"
	info.ChannelMeta.UpstreamModelName = "provider-responses-model"
	info.ChannelMeta.IsModelMapped = true

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	got := recorder.Body.String()
	assert.Contains(t, got, `event: response.created`)
	assert.Contains(t, got, `event: response.output_text.delta`)
	assert.Contains(t, got, `"model":"gpt-public"`)
	assert.NotContains(t, got, `"model":"provider-responses-model"`)
	assert.Contains(t, got, `event: response.completed`)
}

func TestResponsesFlatErrorStopsMessagesStream(t *testing.T) {
	withOpenAIProtocolStreamTestMode(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_upstream","model":"provider-responses-model","created_at":1710000000,"sequence_number":0}}`,
		`data: {"type":"error","code":"server_error","message":"provider failed","param":null,"sequence_number":1}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info.RelayFormat = types.RelayFormatClaude
	info.OriginModelName = "claude-public"
	info.ChannelMeta.UpstreamModelName = "provider-responses-model"

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	openAIError := apiErr.ToOpenAIError()
	assert.Equal(t, "server_error", openAIError.Type)
	assert.Equal(t, "server_error", openAIError.Code)
	assert.Equal(t, "provider failed", openAIError.Message)
	assert.Contains(t, recorder.Body.String(), `event: message_start`)
	assert.NotContains(t, recorder.Body.String(), `event: message_stop`)
}

func TestChatToResponsesMalformedChunkAfterStreamStartIsFatal(t *testing.T) {
	withOpenAIProtocolStreamTestMode(t)
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`,
		`data: {not-json`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.OriginModelName = "gpt-public"
	info.ChannelMeta.UpstreamModelName = "provider-chat-model"

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Contains(t, recorder.Body.String(), `event: response.created`)
	assert.Contains(t, recorder.Body.String(), `"delta":"partial"`)
	assert.NotContains(t, recorder.Body.String(), `event: response.completed`)
}

func TestChatToMessagesMalformedChunkAfterStreamStartIsFatal(t *testing.T) {
	withOpenAIProtocolStreamTestMode(t)
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {not-json`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.RelayFormat = types.RelayFormatClaude
	info.OriginModelName = "claude-public"
	info.ChannelMeta.UpstreamModelName = "provider-chat-model"
	info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Contains(t, recorder.Body.String(), `event: message_start`)
	assert.NotContains(t, recorder.Body.String(), `event: message_stop`)
}

func TestNativeResponsesFlatErrorIsFatal(t *testing.T) {
	withOpenAIProtocolStreamTestMode(t)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"partial","sequence_number":0}`,
		`data: {"type":"error","code":"server_error","message":"provider failed","param":null,"sequence_number":1}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info.RelayFormat = types.RelayFormatOpenAIResponses

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, "provider failed", apiErr.ToOpenAIError().Message)
	assert.Contains(t, recorder.Body.String(), `event: response.output_text.delta`)
	assert.NotContains(t, recorder.Body.String(), `event: response.error`)
	assert.NotContains(t, recorder.Body.String(), `event: error`)
}

func TestBufferedResponsesFlatErrorIsFatal(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"error","code":"server_error","message":"provider failed","param":null,"sequence_number":0}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)

	usage, apiErr := OaiResponsesToChatBufferedStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, "provider failed", apiErr.ToOpenAIError().Message)
	assert.Empty(t, recorder.Body.String())
}

func withOpenAIProtocolStreamTestMode(t *testing.T) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
}
