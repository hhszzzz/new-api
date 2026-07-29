package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiChatBufferedStreamHandlerReturnsMessagesJSON(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"content":"checking "},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_lookup","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":2}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info.RelayFormat = types.RelayFormatClaude
	info.OriginModelName = "claude-public"
	info.ChannelMeta.UpstreamModelName = "provider-chat-model"
	info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}

	usage, apiError := OaiChatBufferedStreamHandler(c, info, resp)

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.NotContains(t, recorder.Body.String(), "data:")
	var response dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "claude-public", response.Model)
	require.Len(t, response.Content, 2)
	assert.Equal(t, "checking ", response.Content[0].GetText())
	assert.Equal(t, "tool_use", response.Content[1].Type)
	assert.Equal(t, "call_lookup", response.Content[1].Id)
	assert.Equal(t, "lookup", response.Content[1].Name)
	toolInput, ok := response.Content[1].Input.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "x", toolInput["q"])
}

func TestOaiChatBufferedStreamHandlerReturnsResponsesJSON(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.OriginModelName = "gpt-public"
	info.ChannelMeta.UpstreamModelName = "provider-chat-model"

	usage, apiError := OaiChatBufferedStreamHandler(c, info, resp)

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "gpt-public", response.Model)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "hello", response.Output[0].Content[0].Text)
}

func TestOaiChatBufferedStreamHandlerPreservesCompatibleReasoningRefusalAndLegacyFunctionCall(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_compat","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"role":"assistant","reasoning":{"summary":"Check policy."}},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_compat","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"refusal":"I cannot help."},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_compat","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"function_call":{"name":"audit","arguments":"{\"safe\":true}"}},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_compat","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{},"finish_reason":"function_call"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.OriginModelName = "gpt-public"
	info.ChannelMeta.UpstreamModelName = "provider-chat-model"

	usage, apiError := OaiChatBufferedStreamHandler(c, info, resp)

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Output, 3)
	assert.Equal(t, "reasoning", response.Output[0].Type)
	assert.Equal(t, "Check policy.", response.Output[0].Summary[0].Text)
	assert.Equal(t, "message", response.Output[1].Type)
	require.Len(t, response.Output[1].Content, 1)
	assert.Equal(t, "refusal", response.Output[1].Content[0].Type)
	assert.Equal(t, "I cannot help.", response.Output[1].Content[0].Refusal)
	assert.Equal(t, "function_call", response.Output[2].Type)
	assert.Equal(t, "call_0", response.Output[2].CallId)
	assert.Equal(t, "audit", response.Output[2].Name)
}

func TestOaiChatBufferedStreamHandlerRestoresResponsesExtendedTools(t *testing.T) {
	tools, err := common.Marshal([]map[string]any{
		{"type": "custom", "name": "apply_patch"},
		{
			"type": "namespace",
			"name": "workspace",
			"tools": []map[string]any{{
				"type":       "function",
				"name":       "exec",
				"parameters": map[string]any{"type": "object"},
			}},
		},
		{"type": "tool_search", "execution": "client"},
	})
	require.NoError(t, err)
	input, err := common.Marshal([]map[string]any{
		{"role": "user", "content": "work"},
		{
			"type": "additional_tools",
			"tools": []map[string]any{{
				"type":       "function",
				"name":       "late_lookup",
				"parameters": map[string]any{"type": "object"},
			}},
		},
	})
	require.NoError(t, err)

	c, recorder, _, info := newResponsesChatTestContext(t, "", false)
	c.Request = c.Request.WithContext(relayconvert.WithProtocolBridgeContext(c.Request.Context()))
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.OriginModelName = "gpt-public"
	requestResult, err := relayconvert.ConvertRequest(c, info, types.RelayFormatOpenAI, &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: input,
		Tools: tools,
	})
	require.NoError(t, err)
	chatRequest, ok := requestResult.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Tools, 4)

	arguments := []string{
		`{"input":"patch body"}`,
		`{"cmd":"pwd"}`,
		`{"query":"files"}`,
		`{"q":"x"}`,
	}
	lines := make([]string, 0, len(chatRequest.Tools)+3)
	for index, tool := range chatRequest.Tools {
		chunk, err := common.Marshal(map[string]any{
			"id":      "chatcmpl_upstream",
			"object":  "chat.completion.chunk",
			"created": 1710000000,
			"model":   "provider-chat-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"index": index,
						"id":    "call_" + tool.Function.Name,
						"type":  "function",
						"function": map[string]any{
							"name":      tool.Function.Name,
							"arguments": arguments[index],
						},
					}},
				},
				"finish_reason": nil,
			}},
		})
		require.NoError(t, err)
		lines = append(lines, "data: "+string(chunk))
	}
	lines = append(lines,
		`data: {"id":"chatcmpl_upstream","model":"provider-chat-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chatcmpl_upstream","model":"provider-chat-model","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`,
		`data: [DONE]`,
		``,
	)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))),
	}

	usage, apiError := OaiChatBufferedStreamHandler(c, info, resp)

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	assert.Equal(t, 12, usage.TotalTokens)
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Output, 4)
	assert.Equal(t, "custom_tool_call", response.Output[0].Type)
	assert.Equal(t, "apply_patch", response.Output[0].Name)
	assert.Equal(t, "patch body", response.Output[0].Input)
	assert.Equal(t, "function_call", response.Output[1].Type)
	assert.Equal(t, "workspace", response.Output[1].Namespace)
	assert.Equal(t, "exec", response.Output[1].Name)
	assert.Equal(t, "tool_search_call", response.Output[2].Type)
	assert.Equal(t, "client", response.Output[2].Execution)
	assert.Equal(t, "function_call", response.Output[3].Type)
	assert.Equal(t, "late_lookup", response.Output[3].Name)
}

func TestOaiChatBufferedStreamHandlerMarksFirstProtocolError(t *testing.T) {
	body := strings.Join([]string{
		`data: {"error":{"message":"unsupported endpoint /v1/chat/completions","type":"unsupported_endpoint","code":"unsupported_endpoint"}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)

	usage, apiError := OaiChatBufferedStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiError)
	assert.True(t, apiError.HasProtocolUnsupportedEvidence())
	assert.Empty(t, recorder.Body.String())
}

func TestOaiChatBufferedStreamHandlerRejectsTruncatedStream(t *testing.T) {
	body := "data: {\"id\":\"chatcmpl_upstream\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n"
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)

	usage, apiError := OaiChatBufferedStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiError)
	assert.Contains(t, apiError.Error(), "terminal finish_reason")
	assert.Empty(t, recorder.Body.String())
}

func TestOaiChatBufferedStreamHandlerRejectsDoneWithoutFinishReason(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_upstream","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)

	usage, apiError := OaiChatBufferedStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiError)
	assert.Contains(t, apiError.Error(), "terminal finish_reason")
	assert.Empty(t, recorder.Body.String())
}
