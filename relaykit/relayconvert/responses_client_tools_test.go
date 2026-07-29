package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLowerResponsesClientToolsPromotesAndRewritesCodexToolShapes(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "strict-responses-model",
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
			{"type": "custom", "name": "exec", "format": map[string]any{"type": "text"}},
			{"type": "tool_search", "execution": "client"},
			{
				"type":  "namespace",
				"name":  "workspace",
				"tools": []map[string]any{{"type": "function", "name": "read", "parameters": map[string]any{"type": "object"}}},
			},
		}),
		Input: protocolBridgeRaw(t, []map[string]any{
			{
				"type":  "additional_tools",
				"tools": []map[string]any{{"type": "custom", "name": "apply_patch"}},
			},
			{"type": "custom_tool_call", "id": "ctc_1", "call_id": "call_exec", "name": "exec", "input": "pwd"},
			{"type": "custom_tool_call_output", "call_id": "call_exec", "output": map[string]any{"ok": true}},
			{"type": "tool_search_call", "id": "tsc_1", "call_id": "call_search", "execution": "client", "arguments": map[string]any{"query": "mail"}},
			{"type": "tool_search_output", "call_id": "call_search", "tools": []map[string]any{{"type": "function", "name": "send"}}},
			{"type": "function_call", "id": "fc_1", "call_id": "call_read", "namespace": "workspace", "name": "read", "arguments": `{}`},
		}),
		ToolChoice: protocolBridgeRaw(t, map[string]any{"type": "custom", "name": "exec"}),
	}

	bridge, changed, err := LowerResponsesClientTools(request)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, bridge.HasMappings())

	var tools []map[string]any
	require.NoError(t, kitutil.Unmarshal(request.Tools, &tools))
	require.Len(t, tools, 6)
	assert.Equal(t, []string{"lookup", "exec", "tool_search", "workspace__read", "apply_patch", "send"}, responseToolNames(tools))
	for _, tool := range tools {
		assert.Equal(t, "function", tool["type"])
	}
	execSchema := tools[1]["parameters"].(map[string]any)
	assert.Equal(t, []any{"input"}, execSchema["required"])

	var input []map[string]any
	require.NoError(t, kitutil.Unmarshal(request.Input, &input))
	require.Len(t, input, 5)
	assert.Equal(t, "function_call", input[0]["type"])
	assert.JSONEq(t, `{"input":"pwd"}`, input[0]["arguments"].(string))
	assert.Equal(t, "function_call_output", input[1]["type"])
	assert.JSONEq(t, `{"ok":true}`, input[1]["output"].(string))
	assert.Equal(t, "function_call", input[2]["type"])
	assert.Equal(t, "tool_search", input[2]["name"])
	assert.Equal(t, "function_call_output", input[3]["type"])
	assert.JSONEq(t, `[{"type":"function","name":"send"}]`, input[3]["output"].(string))
	assert.Equal(t, "workspace__read", input[4]["name"])
	assert.NotContains(t, input[4], "namespace")

	var choice map[string]any
	require.NoError(t, kitutil.Unmarshal(request.ToolChoice, &choice))
	assert.Equal(t, map[string]any{"type": "function", "name": "exec"}, choice)
}

func TestResponsesClientToolBridgeRestoresNonStreamCalls(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "custom", "name": "exec"},
			{"type": "tool_search", "execution": "client"},
			{"type": "namespace", "name": "workspace", "tools": []map[string]any{{"type": "function", "name": "read"}}},
		}),
	}
	bridge, _, err := LowerResponsesClientTools(request)
	require.NoError(t, err)

	payload := protocolBridgeRaw(t, map[string]any{
		"id": "resp_strict",
		"output": []map[string]any{
			{"type": "function_call", "id": "fc_exec", "call_id": "call_exec", "name": "exec", "arguments": `{"input":"pwd"}`},
			{"type": "function_call", "id": "fc_search", "call_id": "call_search", "name": "tool_search", "arguments": `{"query":"mail"}`},
			{"type": "function_call", "id": "fc_read", "call_id": "call_read", "name": "workspace__read", "arguments": `{}`},
		},
	})
	restored, err := bridge.RestoreResponseData(payload)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, kitutil.Unmarshal(restored, &response))
	output := response["output"].([]any)
	exec := output[0].(map[string]any)
	assert.Equal(t, "custom_tool_call", exec["type"])
	assert.Equal(t, "pwd", exec["input"])
	assert.NotContains(t, exec, "arguments")
	search := output[1].(map[string]any)
	assert.Equal(t, "tool_search_call", search["type"])
	assert.Equal(t, "client", search["execution"])
	assert.NotContains(t, search, "name")
	read := output[2].(map[string]any)
	assert.Equal(t, "read", read["name"])
	assert.Equal(t, "workspace", read["namespace"])
}

func TestResponsesClientToolStreamRestorerRebuildsCustomLifecycle(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Tools: protocolBridgeRaw(t, []map[string]any{{"type": "custom", "name": "exec"}})}
	bridge, _, err := LowerResponsesClientTools(request)
	require.NoError(t, err)
	restorer := bridge.NewStreamRestorer()

	upstream := []map[string]any{
		{
			"type": "response.output_item.added", "sequence_number": 10, "output_index": 0,
			"item": map[string]any{"type": "function_call", "id": "fc_exec", "call_id": "call_exec", "name": "exec", "arguments": ""},
		},
		{"type": "response.function_call_arguments.delta", "sequence_number": 11, "output_index": 0, "item_id": "fc_exec", "delta": `{"input":"patch `},
		{"type": "response.function_call_arguments.delta", "sequence_number": 12, "output_index": 0, "item_id": "fc_exec", "delta": `body"}`},
		{"type": "response.function_call_arguments.done", "sequence_number": 13, "output_index": 0, "item_id": "fc_exec", "arguments": `{"input":"patch body"}`},
		{
			"type": "response.output_item.done", "sequence_number": 14, "output_index": 0,
			"item": map[string]any{"type": "function_call", "id": "fc_exec", "call_id": "call_exec", "name": "exec", "arguments": `{"input":"patch body"}`},
		},
		{
			"type": "response.completed", "sequence_number": 15,
			"response": map[string]any{"output": []map[string]any{{"type": "function_call", "id": "fc_exec", "call_id": "call_exec", "name": "exec", "arguments": `{"input":"patch body"}`}}},
		},
	}

	var restored []map[string]any
	for _, event := range upstream {
		payloads, restoreErr := restorer.RestoreData(protocolBridgeRaw(t, event))
		require.NoError(t, restoreErr)
		for _, payload := range payloads {
			var decoded map[string]any
			require.NoError(t, kitutil.Unmarshal(payload, &decoded))
			restored = append(restored, decoded)
		}
	}

	require.Len(t, restored, 5)
	assert.Equal(t, []string{
		"response.output_item.added",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.output_item.done",
		"response.completed",
	}, responseEventTypes(restored))
	for index, event := range restored {
		assert.Equal(t, float64(10+index), event["sequence_number"])
	}
	added := restored[0]["item"].(map[string]any)
	assert.Equal(t, "custom_tool_call", added["type"])
	assert.Equal(t, "", added["input"])
	assert.Equal(t, "patch body", restored[1]["delta"])
	assert.Equal(t, "patch body", restored[2]["input"])
	done := restored[3]["item"].(map[string]any)
	assert.Equal(t, "custom_tool_call", done["type"])
	assert.Equal(t, "patch body", done["input"])
	completedOutput := restored[4]["response"].(map[string]any)["output"].([]any)[0].(map[string]any)
	assert.Equal(t, "custom_tool_call", completedOutput["type"])
	assert.Equal(t, "patch body", completedOutput["input"])
}

func responseToolNames(tools []map[string]any) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool["name"].(string))
	}
	return names
}

func responseEventTypes(events []map[string]any) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event["type"].(string))
	}
	return types
}
