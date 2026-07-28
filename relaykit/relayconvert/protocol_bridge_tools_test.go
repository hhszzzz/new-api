package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesChatBridgeRestoresExtendedToolKinds(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: protocolBridgeRaw(t, []map[string]any{
			{
				"type": "additional_tools",
				"role": "developer",
				"tools": []map[string]any{
					{
						"type":       "function",
						"name":       "added_lookup",
						"parameters": map[string]any{"type": "object"},
					},
				},
			},
			{"role": "user", "content": "hello"},
		}),
		Tools: protocolBridgeRaw(t, []map[string]any{
			{
				"type":       "function",
				"name":       "lookup",
				"parameters": map[string]any{"type": "object"},
			},
			{
				"type":        "custom",
				"name":        "apply_patch",
				"description": "Apply a patch",
			},
			{
				"type":        "namespace",
				"name":        "crm",
				"description": "CRM tools",
				"tools": []map[string]any{
					{
						"type":       "function",
						"name":       "get_customer",
						"parameters": map[string]any{"type": "object"},
					},
					{
						"type": "custom",
						"name": "render_customer",
					},
				},
			},
			{
				"type":      "tool_search",
				"execution": "client",
			},
		}),
	}

	requestResult, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	chatRequest, ok := requestResult.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Tools, 6)
	assert.Equal(t, "lookup", chatRequest.Tools[0].Function.Name)
	assert.Equal(t, "apply_patch", chatRequest.Tools[1].Function.Name)
	assert.Equal(t, "crm__get_customer", chatRequest.Tools[2].Function.Name)
	assert.Equal(t, "crm__render_customer", chatRequest.Tools[3].Function.Name)
	assert.Equal(t, "tool_search", chatRequest.Tools[4].Function.Name)
	assert.Equal(t, "added_lookup", chatRequest.Tools[5].Function.Name)
	assert.Equal(t, []string{"input"}, chatRequest.Tools[1].Function.Parameters.(map[string]any)["required"])
	require.Len(t, chatRequest.Messages, 1)
	assert.Equal(t, "hello", chatRequest.Messages[0].StringContent())

	upstream := &dto.OpenAITextResponse{
		Id:    "resp-tools",
		Model: "provider-model",
		Choices: []dto.OpenAITextResponseChoice{
			{
				FinishReason: "tool_calls",
				Message:      dto.Message{Role: "assistant"},
			},
		},
	}
	upstream.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_custom", Type: "function", Function: dto.FunctionRequest{Name: "apply_patch", Arguments: `{"input":"patch body"}`}},
		{ID: "call_ns", Type: "function", Function: dto.FunctionRequest{Name: "crm__get_customer", Arguments: `{"id":"1"}`}},
		{ID: "call_ns_custom", Type: "function", Function: dto.FunctionRequest{Name: "crm__render_customer", Arguments: `{"input":"customer 1"}`}},
		{ID: "call_search", Type: "function", Function: dto.FunctionRequest{Name: "tool_search", Arguments: `{"query":"billing"}`}},
	})

	responseResult, err := ConvertResponse(ctx, &convmeta.Values{}, types.RelayFormatOpenAIResponses, upstream)
	require.NoError(t, err)
	response, ok := responseResult.Value.(*dto.OpenAIResponsesResponse)
	require.True(t, ok)
	require.Len(t, response.Output, 4)

	assert.Equal(t, "custom_tool_call", response.Output[0].Type)
	assert.Equal(t, "apply_patch", response.Output[0].Name)
	assert.Equal(t, "patch body", response.Output[0].Input)

	assert.Equal(t, "function_call", response.Output[1].Type)
	assert.Equal(t, "crm", response.Output[1].Namespace)
	assert.Equal(t, "get_customer", response.Output[1].Name)

	assert.Equal(t, "custom_tool_call", response.Output[2].Type)
	assert.Equal(t, "crm", response.Output[2].Namespace)
	assert.Equal(t, "render_customer", response.Output[2].Name)
	assert.Equal(t, "customer 1", response.Output[2].Input)

	assert.Equal(t, "tool_search_call", response.Output[3].Type)
	assert.Equal(t, "client", response.Output[3].Execution)
	assert.JSONEq(t, `{"query":"billing"}`, string(response.Output[3].Arguments))
}

func TestResponsesChatBridgeRejectsToolNameCollisions(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Model: "gpt-test", Tools: protocolBridgeRaw(t, []map[string]any{
		{"type": "function", "name": "crm__lookup"},
		{
			"type": "namespace",
			"name": "crm",
			"tools": []map[string]any{
				{"type": "function", "name": "lookup"},
			},
		},
	})}

	_, err := ConvertRequest(nil, &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts after Chat name encoding")
}

func TestResponsesChatBridgeNormalizesFunctionsAndDropsHostedTools(t *testing.T) {
	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-test",
		Stream: &stream,
		Input: protocolBridgeRaw(t, []map[string]any{
			{"type": "web_search_call", "id": "ws_1", "status": "completed"},
			{"role": "user", "content": "hello"},
		}),
		Tools: protocolBridgeRaw(t, []map[string]any{
			{
				"type":   "function",
				"name":   "lookup",
				"strict": true,
				"parameters": map[string]any{
					"type": nil,
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
			},
			{"type": "web_search_preview"},
			{"type": "file_search"},
			{"type": "tool_search"},
			{"type": "tool_search", "execution": "server"},
		}),
	}

	result, err := ConvertRequest(context.Background(), &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	chatRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Tools, 2)
	require.Len(t, chatRequest.Messages, 1)
	assert.Equal(t, "hello", chatRequest.Messages[0].StringContent())
	require.NotNil(t, chatRequest.StreamOptions)
	assert.True(t, chatRequest.StreamOptions.IncludeUsage)

	encoded, err := kitutil.Marshal(chatRequest.Tools[0])
	require.NoError(t, err)
	var tool map[string]any
	require.NoError(t, kitutil.Unmarshal(encoded, &tool))
	function := tool["function"].(map[string]any)
	assert.Equal(t, true, function["strict"])
	parameters := function["parameters"].(map[string]any)
	assert.Equal(t, "object", parameters["type"])
	assert.Equal(t, "string", parameters["properties"].(map[string]any)["query"].(map[string]any)["type"])
	assert.Equal(t, "tool_search", chatRequest.Tools[1].Function.Name)
}

func TestResponsesChatBridgeLoadsToolsFromToolSearchOutput(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "tool_search"},
		}),
		Input: protocolBridgeRaw(t, []map[string]any{
			{
				"type":      "tool_search_call",
				"call_id":   "call_search",
				"execution": "client",
				"arguments": map[string]any{"query": "Gmail"},
			},
			{
				"type":      "tool_search_output",
				"call_id":   "call_search",
				"execution": "client",
				"tools": []map[string]any{
					{
						"type": "namespace",
						"name": "gmail",
						"tools": []map[string]any{
							{
								"type":       "function",
								"name":       "search_emails",
								"parameters": map[string]any{"type": "object"},
							},
						},
					},
				},
			},
			{"role": "user", "content": "Find the latest message"},
		}),
	}

	requestResult, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	chatRequest, ok := requestResult.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Tools, 2)
	assert.Equal(t, "tool_search", chatRequest.Tools[0].Function.Name)
	assert.Equal(t, "gmail__search_emails", chatRequest.Tools[1].Function.Name)

	upstream := &dto.OpenAITextResponse{
		Id:    "resp-loaded-tool",
		Model: "provider-model",
		Choices: []dto.OpenAITextResponseChoice{
			{FinishReason: "tool_calls", Message: dto.Message{Role: "assistant"}},
		},
	}
	upstream.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_gmail",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "gmail__search_emails",
				Arguments: `{"query":"latest"}`,
			},
		},
	})

	responseResult, err := ConvertResponse(ctx, &convmeta.Values{}, types.RelayFormatOpenAIResponses, upstream)
	require.NoError(t, err)
	response, ok := responseResult.Value.(*dto.OpenAIResponsesResponse)
	require.True(t, ok)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "function_call", response.Output[0].Type)
	assert.Equal(t, "gmail", response.Output[0].Namespace)
	assert.Equal(t, "search_emails", response.Output[0].Name)
}

func TestResponsesBridgeWrapsFreeformToolsAndRestoresCustomCalls(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "freeform", "name": "shell", "description": "Run a shell command"},
		}),
	}

	chatResult, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	chatRequest, ok := chatResult.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Tools, 1)
	assert.Equal(t, "shell", chatRequest.Tools[0].Function.Name)
	assert.Equal(t, []string{"input"}, chatRequest.Tools[0].Function.Parameters.(map[string]any)["required"])

	upstream := &dto.OpenAITextResponse{
		Id:    "resp-freeform",
		Model: "provider-model",
		Choices: []dto.OpenAITextResponseChoice{
			{FinishReason: "tool_calls", Message: dto.Message{Role: "assistant"}},
		},
	}
	upstream.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_shell", Type: "function", Function: dto.FunctionRequest{Name: "shell", Arguments: `{"input":"pwd"}`}},
	})

	responseResult, err := ConvertResponse(ctx, &convmeta.Values{}, types.RelayFormatOpenAIResponses, upstream)
	require.NoError(t, err)
	response, ok := responseResult.Value.(*dto.OpenAIResponsesResponse)
	require.True(t, ok)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "custom_tool_call", response.Output[0].Type)
	assert.Equal(t, "shell", response.Output[0].Name)
	assert.Equal(t, "pwd", response.Output[0].Input)
}

func TestResponsesChatBridgeRestoresCustomToolStreamLifecycle(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "custom", "name": "apply_patch"},
		}),
	}
	_, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)

	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
		ID:    "resp-stream-tools",
		Model: "gpt-test",
	})
	require.NoError(t, err)
	finishReason := "tool_calls"
	chunks := []*dto.ChatCompletionsStreamResponse{
		{
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ToolCalls: []dto.ToolCallResponse{
							{ID: "call_1", Type: "function", Function: dto.FunctionResponse{Name: "apply_patch", Arguments: `{"input":"patch `}},
						},
					},
				},
			},
		},
		{
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ToolCalls: []dto.ToolCallResponse{
							{Function: dto.FunctionResponse{Arguments: `body"}`}},
						},
					},
					FinishReason: &finishReason,
				},
			},
		},
	}

	var events []ChatToResponsesStreamEvent
	for _, chunk := range chunks {
		results, err := ConvertStreamResponseChunk(ctx, &convmeta.Values{}, state, chunk)
		require.NoError(t, err)
		for _, result := range results {
			event, ok := result.Value.(ChatToResponsesStreamEvent)
			require.True(t, ok)
			events = append(events, event)
		}
	}
	final, err := FinalizeStreamResponse(ctx, &convmeta.Values{}, state)
	require.NoError(t, err)
	for _, result := range final {
		event, ok := result.Value.(ChatToResponsesStreamEvent)
		require.True(t, ok)
		events = append(events, event)
	}

	var eventTypes []string
	for i, event := range events {
		eventTypes = append(eventTypes, event.Type)
		require.NotNil(t, event.Payload.SequenceNumber)
		assert.Equal(t, i, *event.Payload.SequenceNumber)
	}
	assert.Equal(t, []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.output_item.done",
		"response.completed",
	}, eventTypes)
	assert.Equal(t, "custom_tool_call", events[2].Payload.Item.Type)
	assert.Empty(t, events[2].Payload.ItemID)
	assert.Equal(t, "patch body", events[3].Payload.Delta)
	assert.Equal(t, "patch body", events[4].Payload.Input)
	assert.Equal(t, "patch body", events[5].Payload.Item.Input)
}

func TestResponsesMessagesBridgeRestoresExtendedToolKinds(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	maxOutputTokens := uint(1024)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input: protocolBridgeRaw(t, []map[string]any{
			{
				"type": "additional_tools",
				"tools": []map[string]any{
					{"type": "function", "name": "added_lookup", "parameters": map[string]any{"type": "object"}},
				},
			},
			{"role": "user", "content": "hello"},
		}),
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "custom", "name": "apply_patch"},
			{
				"type": "namespace",
				"name": "crm",
				"tools": []map[string]any{
					{"type": "function", "name": "get_customer", "parameters": map[string]any{"type": "object"}},
					{"type": "custom", "name": "render_customer"},
				},
			},
			{"type": "tool_search", "execution": "client"},
		}),
	}

	requestResult, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatClaude, request)
	require.NoError(t, err)
	claudeRequest, ok := requestResult.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	tools, err := kitutil.Any2Type[[]*dto.Tool](claudeRequest.Tools)
	require.NoError(t, err)
	require.Len(t, tools, 5)
	assert.Equal(t, "apply_patch", tools[0].Name)
	assert.Equal(t, "crm__get_customer", tools[1].Name)
	assert.Equal(t, "crm__render_customer", tools[2].Name)
	assert.Equal(t, "tool_search", tools[3].Name)
	assert.Equal(t, "added_lookup", tools[4].Name)
	assert.Equal(t, []any{"input"}, tools[0].InputSchema["required"])

	upstream := &dto.ClaudeResponse{
		Id:         "msg_tools",
		Model:      "provider-model",
		StopReason: "tool_use",
		Content: []dto.ClaudeMediaMessage{
			{Type: "tool_use", Id: "call_custom", Name: "apply_patch", Input: map[string]any{"input": "patch body"}},
			{Type: "tool_use", Id: "call_namespace", Name: "crm__get_customer", Input: map[string]any{"id": "1"}},
			{Type: "tool_use", Id: "call_search", Name: "tool_search", Input: map[string]any{"query": "billing"}},
		},
	}
	responseResult, err := ConvertResponse(ctx, &convmeta.Values{}, types.RelayFormatOpenAIResponses, upstream)
	require.NoError(t, err)
	response, ok := responseResult.Value.(*dto.OpenAIResponsesResponse)
	require.True(t, ok)
	require.Len(t, response.Output, 3)
	assert.Equal(t, "custom_tool_call", response.Output[0].Type)
	assert.Equal(t, "apply_patch", response.Output[0].Name)
	assert.Equal(t, "patch body", response.Output[0].Input)
	assert.Equal(t, "function_call", response.Output[1].Type)
	assert.Equal(t, "crm", response.Output[1].Namespace)
	assert.Equal(t, "get_customer", response.Output[1].Name)
	assert.Equal(t, "tool_search_call", response.Output[2].Type)
	assert.Equal(t, "client", response.Output[2].Execution)
	assert.JSONEq(t, `{"query":"billing"}`, string(response.Output[2].Arguments))
}

func TestResponsesMessagesBridgeRestoresCustomToolStreamLifecycle(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	maxOutputTokens := uint(1024)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "custom", "name": "apply_patch"},
		}),
	}
	_, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatClaude, request)
	require.NoError(t, err)

	state, err := NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
		ID:    "resp-claude-tools",
		Model: "claude-test",
	})
	require.NoError(t, err)
	index := 0
	stopReason := "tool_use"
	chunks := []*dto.ClaudeResponse{
		{
			Type: "message_start",
			Message: &dto.ClaudeMediaMessage{
				Id:    "msg_tools",
				Model: "provider-model",
				Usage: &dto.ClaudeUsage{InputTokens: 4},
			},
		},
		{
			Type:  "content_block_start",
			Index: &index,
			ContentBlock: &dto.ClaudeMediaMessage{
				Type:  "tool_use",
				Id:    "call_1",
				Name:  "apply_patch",
				Input: map[string]any{},
			},
		},
		{
			Type:  "content_block_delta",
			Index: &index,
			Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: kitutil.GetPointer(`{"input":"patch `)},
		},
		{
			Type:  "content_block_delta",
			Index: &index,
			Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: kitutil.GetPointer(`body"}`)},
		},
		{Type: "content_block_stop", Index: &index},
		{
			Type:  "message_delta",
			Delta: &dto.ClaudeMediaMessage{StopReason: &stopReason},
			Usage: &dto.ClaudeUsage{OutputTokens: 2},
		},
		{Type: "message_stop"},
	}

	events := make([]ChatToResponsesStreamEvent, 0)
	for _, chunk := range chunks {
		results, err := ConvertStreamResponseChunk(ctx, &convmeta.Values{}, state, chunk)
		require.NoError(t, err)
		for _, result := range results {
			event, ok := result.Value.(ChatToResponsesStreamEvent)
			require.True(t, ok)
			events = append(events, event)
		}
	}
	final, err := FinalizeStreamResponse(ctx, &convmeta.Values{}, state)
	require.NoError(t, err)
	for _, result := range final {
		event, ok := result.Value.(ChatToResponsesStreamEvent)
		require.True(t, ok)
		events = append(events, event)
	}

	eventTypes := make([]string, 0, len(events))
	for i, event := range events {
		eventTypes = append(eventTypes, event.Type)
		require.NotNil(t, event.Payload.SequenceNumber)
		assert.Equal(t, i, *event.Payload.SequenceNumber)
	}
	assert.Equal(t, []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.output_item.done",
		"response.completed",
	}, eventTypes)
	assert.Equal(t, "custom_tool_call", events[2].Payload.Item.Type)
	assert.Empty(t, events[2].Payload.ItemID)
	assert.Equal(t, "patch body", events[3].Payload.Delta)
	assert.Equal(t, "patch body", events[4].Payload.Input)
	assert.Equal(t, "patch body", events[5].Payload.Item.Input)
}

func TestResetProtocolBridgeContextDropsAttemptScopedToolState(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "custom", "name": "apply_patch"},
		}),
	}
	_, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)

	ResetProtocolBridgeContext(ctx)

	upstream := &dto.OpenAITextResponse{
		Id:    "resp-reset",
		Model: "provider-model",
		Choices: []dto.OpenAITextResponseChoice{
			{FinishReason: "tool_calls", Message: dto.Message{Role: "assistant"}},
		},
	}
	upstream.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_1", Type: "function", Function: dto.FunctionRequest{Name: "apply_patch", Arguments: `{"input":"patch body"}`}},
	})
	result, err := ConvertResponse(ctx, &convmeta.Values{}, types.RelayFormatOpenAIResponses, upstream)
	require.NoError(t, err)
	response := result.Value.(*dto.OpenAIResponsesResponse)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "function_call", response.Output[0].Type)
}

func protocolBridgeRaw(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return raw
}
