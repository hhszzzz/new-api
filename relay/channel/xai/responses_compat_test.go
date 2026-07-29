package xai

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareXAIResponsesRequestLowersCodexToolsAndSanitizesFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	request := &dto.OpenAIResponsesRequest{
		Model:                "grok-4.5",
		PromptCacheRetention: xaiRaw(t, "24h"),
		SafetyIdentifier:     xaiRaw(t, "user-1"),
		Tools: xaiRaw(t, []map[string]any{
			{"type": "web_search_preview"},
			{"type": "custom", "name": "exec", "external_web_access": true},
		}),
		Input: xaiRaw(t, []map[string]any{
			{"type": "reasoning", "content": nil, "summary": []any{}},
			{
				"type": "additional_tools",
				"tools": []map[string]any{{
					"type":  "namespace",
					"name":  "workspace",
					"tools": []map[string]any{{"type": "function", "name": "read", "parameters": map[string]any{"type": "object"}}},
				}},
			},
			{"type": "custom_tool_call", "call_id": "call_exec", "name": "exec", "input": "pwd"},
			{"type": "function_call", "call_id": "call_read", "namespace": "workspace", "name": "read", "arguments": `{}`},
		}),
	}

	require.NoError(t, prepareXAIResponsesRequest(ctx, request))
	assert.Empty(t, request.PromptCacheRetention)
	assert.Empty(t, request.SafetyIdentifier)
	assert.NotContains(t, string(request.Tools), "external_web_access")
	assert.NotContains(t, string(request.Input), `"content":null`)
	assert.NotContains(t, string(request.Input), "additional_tools")

	var tools []map[string]any
	require.NoError(t, common.Unmarshal(request.Tools, &tools))
	require.Len(t, tools, 3)
	assert.Equal(t, "web_search", tools[0]["type"])
	assert.Equal(t, "function", tools[1]["type"])
	assert.Equal(t, "exec", tools[1]["name"])
	assert.Equal(t, "workspace__read", tools[2]["name"])

	var input []map[string]any
	require.NoError(t, common.Unmarshal(request.Input, &input))
	require.Len(t, input, 3)
	assert.NotContains(t, input[0], "content")
	assert.Equal(t, "function_call", input[1]["type"])
	assert.JSONEq(t, `{"input":"pwd"}`, input[1]["arguments"].(string))
	assert.Equal(t, "workspace__read", input[2]["name"])
	assert.NotContains(t, input[2], "namespace")
	assert.NotNil(t, xaiResponsesClientToolBridge(ctx))
}

func TestPrepareXAIResponsesRequestRejectsUnsupportedHostedTool(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "grok-4.5",
		Tools: xaiRaw(t, []map[string]any{{"type": "computer_use_preview"}}),
	}

	err := prepareXAIResponsesRequest(nil, request)
	require.ErrorContains(t, err, `does not support tool type "computer_use_preview"`)
}

func TestPrepareXAIResponsesRequestRemovesReasoningForComposer(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model:     "provider/grok-composer-2.5-fast",
		Reasoning: &dto.Reasoning{Effort: "high"},
	}

	require.NoError(t, prepareXAIResponsesRequest(nil, request))
	assert.Nil(t, request.Reasoning)
}

func TestRestoreXAIResponsesBodyRestoresCustomCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	request := &dto.OpenAIResponsesRequest{Tools: xaiRaw(t, []map[string]any{{"type": "custom", "name": "exec"}})}
	require.NoError(t, prepareXAIResponsesRequest(ctx, request))
	response := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"id":"resp_1","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"exec","arguments":"{\"input\":\"pwd\"}"}]}`)),
	}

	require.NoError(t, restoreXAIResponsesBody(ctx, response, false))
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	call := payload["output"].([]any)[0].(map[string]any)
	assert.Equal(t, "custom_tool_call", call["type"])
	assert.Equal(t, "pwd", call["input"])
	assert.NotContains(t, call, "arguments")
}

func TestRestoreXAIResponsesBodyRestoresStreamingCustomLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	request := &dto.OpenAIResponsesRequest{Tools: xaiRaw(t, []map[string]any{{"type": "custom", "name": "exec"}})}
	require.NoError(t, prepareXAIResponsesRequest(ctx, request))
	stream := strings.Join([]string{
		`data: {"type":"response.output_item.added","sequence_number":0,"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"exec","arguments":""}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","sequence_number":1,"output_index":0,"item_id":"fc_1","delta":"{\"input\":\"pwd\"}"}`,
		"",
		`data: {"type":"response.function_call_arguments.done","sequence_number":2,"output_index":0,"item_id":"fc_1","arguments":"{\"input\":\"pwd\"}"}`,
		"",
		`data: {"type":"response.output_item.done","sequence_number":3,"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"exec","arguments":"{\"input\":\"pwd\"}"}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	response := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader(stream)),
	}

	require.NoError(t, restoreXAIResponsesBody(ctx, response, true))
	var eventTypes []string
	err := helper.ScanJSONSSE(response.Body, func(data string) (bool, error) {
		if data == "[DONE]" {
			return true, nil
		}
		var event map[string]any
		if err := common.Unmarshal([]byte(data), &event); err != nil {
			return false, err
		}
		eventTypes = append(eventTypes, event["type"].(string))
		return false, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"response.output_item.added",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.output_item.done",
	}, eventTypes)
}

func xaiRaw(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}
