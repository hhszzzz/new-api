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
	assert.Equal(t, []string{"query"}, chatRequest.Tools[4].Function.Parameters.(map[string]any)["required"])
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

func TestResponsesBridgesPreserveStringCustomMetadataAndNamespaceChildren(t *testing.T) {
	for _, target := range []struct {
		name   string
		format types.RelayFormat
	}{
		{name: "Chat", format: types.RelayFormatOpenAI},
		{name: "Messages", format: types.RelayFormatClaude},
	} {
		t.Run(target.name, func(t *testing.T) {
			maxOutputTokens := uint(1024)
			request := &dto.OpenAIResponsesRequest{
				Model:           "gpt-test",
				Input:           protocolBridgeRaw(t, "hello"),
				MaxOutputTokens: &maxOutputTokens,
				Tools: protocolBridgeRaw(t, []any{
					"apply_patch",
					map[string]any{
						"type":        "custom",
						"name":        "shell",
						"description": "Run a command",
						"format": map[string]any{
							"type":       "grammar",
							"syntax":     "lark",
							"definition": "start: command",
						},
					},
					map[string]any{
						"type": "namespace",
						"name": "workspace",
						"children": []any{
							map[string]any{"type": "function", "name": "read_file"},
						},
					},
					map[string]any{
						"type": "function",
						"function": map[string]any{
							"name":        "nested_lookup",
							"description": "Nested Chat-style function declaration",
							"parameters":  map[string]any{"properties": map[string]any{}},
						},
						"strict": true,
					},
				}),
			}

			result, err := ConvertRequest(WithProtocolBridgeContext(context.Background()), &convmeta.Values{}, target.format, request)
			require.NoError(t, err)

			var names []string
			var shellDescription string
			switch converted := result.Value.(type) {
			case *dto.GeneralOpenAIRequest:
				for _, tool := range converted.Tools {
					names = append(names, tool.Function.Name)
					if tool.Function.Name == "shell" {
						shellDescription = tool.Function.Description
					}
				}
			case *dto.ClaudeRequest:
				tools, convertErr := kitutil.Any2Type[[]*dto.Tool](converted.Tools)
				require.NoError(t, convertErr)
				for _, tool := range tools {
					names = append(names, tool.Name)
					if tool.Name == "shell" {
						shellDescription = tool.Description
					}
				}
			default:
				require.Failf(t, "unexpected converted request", "%T", result.Value)
			}

			assert.Equal(t, []string{"apply_patch", "shell", "workspace__read_file", "nested_lookup"}, names)
			assert.Contains(t, shellDescription, "Original tool definition:")
			assert.Contains(t, shellDescription, `"format":{"definition":"start: command","syntax":"lark","type":"grammar"}`)
		})
	}
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

func TestResponsesChatBridgeDeduplicatesRepeatedToolDeclarations(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: protocolBridgeRaw(t, []map[string]any{
			{
				"type": "tool_search_output",
				"tools": []map[string]any{
					{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
					{
						"type": "namespace",
						"name": "crm",
						"tools": []map[string]any{
							{"type": "function", "name": "customer", "parameters": map[string]any{"type": "object"}},
						},
					},
				},
			},
			{"role": "user", "content": "hello"},
		}),
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
			{
				"type": "namespace",
				"name": "crm",
				"tools": []map[string]any{
					{"type": "function", "name": "customer", "parameters": map[string]any{"type": "object"}},
				},
			},
		}),
	}

	result, err := ConvertRequest(WithProtocolBridgeContext(context.Background()), &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	converted, ok := result.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, converted.Tools, 2)
	assert.Equal(t, "lookup", converted.Tools[0].Function.Name)
	assert.Equal(t, "crm__customer", converted.Tools[1].Function.Name)
}

func TestResponsesBridgesRejectHostedToolHistory(t *testing.T) {
	for _, target := range []struct {
		name   string
		format types.RelayFormat
	}{
		{name: "Chat", format: types.RelayFormatOpenAI},
		{name: "Messages", format: types.RelayFormatClaude},
	} {
		t.Run(target.name, func(t *testing.T) {
			stream := true
			request := &dto.OpenAIResponsesRequest{
				Model:  "gpt-test",
				Stream: &stream,
				Input: protocolBridgeRaw(t, []map[string]any{
					{"type": "web_search_call", "id": "ws_1", "status": "completed"},
					{"role": "user", "content": "hello"},
				}),
			}

			result, err := ConvertRequest(context.Background(), &convmeta.Values{}, target.format, request)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "web_search_call")
			assert.Contains(t, err.Error(), "without losing context")
		})
	}
}

func TestResponsesBridgesDropHostedToolDeclarations(t *testing.T) {
	targets := []struct {
		name   string
		format types.RelayFormat
	}{
		{name: "Chat", format: types.RelayFormatOpenAI},
		{name: "Messages", format: types.RelayFormatClaude},
	}
	hostedTools := []struct {
		name string
		tool map[string]any
	}{
		{name: "web search", tool: map[string]any{"type": "web_search_preview"}},
		{name: "file search", tool: map[string]any{"type": "file_search"}},
	}

	for _, target := range targets {
		for _, test := range hostedTools {
			t.Run(target.name+"/"+test.name, func(t *testing.T) {
				maxOutputTokens := uint(512)
				request := &dto.OpenAIResponsesRequest{
					Model:           "gpt-test",
					MaxOutputTokens: &maxOutputTokens,
					Input:           protocolBridgeRaw(t, "hello"),
					Tools: protocolBridgeRaw(t, []map[string]any{
						test.tool,
						{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
					}),
				}

				result, err := ConvertRequest(context.Background(), &convmeta.Values{}, target.format, request)

				// CC Switch semantics: hosted tools are dropped, the rest of the
				// request converts and keeps its convertible tools.
				require.NoError(t, err)
				switch converted := result.Value.(type) {
				case *dto.GeneralOpenAIRequest:
					require.Len(t, converted.Tools, 1)
					assert.Equal(t, "lookup", converted.Tools[0].Function.Name)
				case *dto.ClaudeRequest:
					require.Len(t, converted.Tools, 1)
				default:
					t.Fatalf("unexpected converted request type %T", result.Value)
				}
			})
		}

		t.Run(target.name+"/server tool search", func(t *testing.T) {
			maxOutputTokens := uint(512)
			request := &dto.OpenAIResponsesRequest{
				Model:           "gpt-test",
				MaxOutputTokens: &maxOutputTokens,
				Input:           protocolBridgeRaw(t, "hello"),
				Tools: protocolBridgeRaw(t, []map[string]any{
					{"type": "tool_search", "execution": "server"},
				}),
			}

			_, err := ConvertRequest(context.Background(), &convmeta.Values{}, target.format, request)

			// Server-executed tool_search cannot be dropped without changing
			// semantics the client explicitly asked for.
			require.ErrorContains(t, err, "native Responses upstream")
		})
	}
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
		Input:           protocolBridgeRaw(t, "use the tool"),
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

func TestResponsesMessagesBridgeReplaysSignedThinkingOnlyOnOriginChannel(t *testing.T) {
	const stateSecret = "protocol-bridge-test-secret"
	ctx := WithProtocolBridgeContext(context.Background())
	meta := &convmeta.Values{
		ChannelMetaAttached: true,
		ChannelID:           17,
		Options:             &convmeta.Options{ProviderStateSecret: stateSecret},
	}
	upstream := &dto.ClaudeResponse{
		Id:         "msg_signed_tool",
		Model:      "claude-test",
		StopReason: "tool_use",
		Content: []dto.ClaudeMediaMessage{
			{Type: "thinking", Thinking: kitutil.GetPointer("inspect inputs"), Signature: "signed-thinking-state"},
			{Type: "tool_use", Id: "call_1", Name: "lookup", Input: map[string]any{"q": "x"}},
		},
	}

	responseResult, err := ConvertResponse(ctx, meta, types.RelayFormatOpenAIResponses, upstream)
	require.NoError(t, err)
	response, ok := responseResult.Value.(*dto.OpenAIResponsesResponse)
	require.True(t, ok)
	require.Len(t, response.Output, 2)
	assert.Equal(t, "reasoning", response.Output[0].Type)
	assert.Equal(t, "inspect inputs", response.Output[0].Summary[0].Text)
	require.NotEmpty(t, response.Output[0].EncryptedContent)
	assert.Equal(t, "function_call", response.Output[1].Type)

	maxOutputTokens := uint(4096)
	nextRequest := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Reasoning:       &dto.Reasoning{Effort: "medium"},
		Input: protocolBridgeRaw(t, []any{
			map[string]any{"role": "user", "content": "run it"},
			response.Output[0],
			response.Output[1],
			map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	requestResult, err := ConvertRequest(ctx, meta, types.RelayFormatClaude, nextRequest)
	require.NoError(t, err)
	converted, ok := requestResult.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.Len(t, converted.Messages, 3)
	assistant, err := converted.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Len(t, assistant, 2)
	assert.Equal(t, "thinking", assistant[0].Type)
	assert.Equal(t, "inspect inputs", *assistant[0].Thinking)
	assert.Equal(t, "signed-thinking-state", assistant[0].Signature)
	assert.Equal(t, "tool_use", assistant[1].Type)
	require.NotNil(t, converted.Thinking)
	assert.Equal(t, "enabled", converted.Thinking.Type)

	otherChannel := &convmeta.Values{
		ChannelMetaAttached: true,
		ChannelID:           18,
		Options:             &convmeta.Options{ProviderStateSecret: stateSecret},
	}
	requestResult, err = ConvertRequest(WithProtocolBridgeContext(context.Background()), otherChannel, types.RelayFormatClaude, nextRequest)
	require.NoError(t, err)
	converted, ok = requestResult.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	assistant, err = converted.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Len(t, assistant, 1)
	assert.Equal(t, "tool_use", assistant[0].Type)
	assert.Nil(t, converted.Thinking)
}

func TestResponsesMessagesBridgePreservesSignedThinkingInStream(t *testing.T) {
	const stateSecret = "protocol-bridge-test-secret"
	ctx := WithProtocolBridgeContext(context.Background())
	meta := &convmeta.Values{
		ChannelMetaAttached: true,
		ChannelID:           17,
		Options:             &convmeta.Options{ProviderStateSecret: stateSecret},
	}
	state, err := NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
		ID:    "resp-signed-thinking",
		Model: "claude-test",
	})
	require.NoError(t, err)

	thinkingIndex := 0
	toolIndex := 1
	stopReason := "tool_use"
	chunks := []*dto.ClaudeResponse{
		{Type: "message_start", Message: &dto.ClaudeMediaMessage{Id: "msg_stream", Model: "claude-test"}},
		{Type: "content_block_start", Index: &thinkingIndex, ContentBlock: &dto.ClaudeMediaMessage{Type: "thinking", Thinking: kitutil.GetPointer("")}},
		{Type: "content_block_delta", Index: &thinkingIndex, Delta: &dto.ClaudeMediaMessage{Type: "thinking_delta", Thinking: kitutil.GetPointer("inspect inputs")}},
		{Type: "content_block_delta", Index: &thinkingIndex, Delta: &dto.ClaudeMediaMessage{Type: "signature_delta", Signature: "signed-thinking-state"}},
		{Type: "content_block_stop", Index: &thinkingIndex},
		{Type: "content_block_start", Index: &toolIndex, ContentBlock: &dto.ClaudeMediaMessage{Type: "tool_use", Id: "call_1", Name: "lookup", Input: map[string]any{}}},
		{Type: "content_block_delta", Index: &toolIndex, Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: kitutil.GetPointer(`{"q":"x"}`)}},
		{Type: "content_block_stop", Index: &toolIndex},
		{Type: "message_delta", Delta: &dto.ClaudeMediaMessage{StopReason: &stopReason}},
		{Type: "message_stop"},
	}

	events := make([]ChatToResponsesStreamEvent, 0)
	for _, chunk := range chunks {
		results, err := ConvertStreamResponseChunk(ctx, meta, state, chunk)
		require.NoError(t, err)
		for _, result := range results {
			event, ok := result.Value.(ChatToResponsesStreamEvent)
			require.True(t, ok)
			events = append(events, event)
		}
	}
	final, err := FinalizeStreamResponse(ctx, meta, state)
	require.NoError(t, err)
	for _, result := range final {
		event, ok := result.Value.(ChatToResponsesStreamEvent)
		require.True(t, ok)
		events = append(events, event)
	}

	var completed *dto.OpenAIResponsesResponse
	for _, event := range events {
		if event.Type == "response.output_item.done" && event.Payload.Item != nil && event.Payload.Item.Type == "reasoning" {
			require.NotEmpty(t, event.Payload.Item.EncryptedContent)
			assert.Equal(t, "inspect inputs", event.Payload.Item.Summary[0].Text)
		}
		if event.Type == "response.completed" {
			completed = event.Payload.Response
		}
	}
	require.NotNil(t, completed)
	require.Len(t, completed.Output, 2)
	assert.Equal(t, "reasoning", completed.Output[0].Type)
	require.NotEmpty(t, completed.Output[0].EncryptedContent)
	assert.Equal(t, "function_call", completed.Output[1].Type)

	maxOutputTokens := uint(4096)
	nextRequest := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Reasoning:       &dto.Reasoning{Effort: "medium"},
		Input: protocolBridgeRaw(t, []any{
			map[string]any{"role": "user", "content": "run it"},
			completed.Output[0],
			completed.Output[1],
			map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}
	requestResult, err := ConvertRequest(WithProtocolBridgeContext(context.Background()), meta, types.RelayFormatClaude, nextRequest)
	require.NoError(t, err)
	converted, ok := requestResult.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	assistant, err := converted.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Len(t, assistant, 2)
	assert.Equal(t, "thinking", assistant[0].Type)
	assert.Equal(t, "signed-thinking-state", assistant[0].Signature)
}

func TestMessagesResponsesBridgePreservesMultipleThinkingBlocksInStream(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	meta := &convmeta.Values{
		ChannelMetaAttached: true,
		ChannelID:           17,
		Options:             &convmeta.Options{ProviderStateSecret: "protocol-bridge-test-secret"},
	}
	state, err := NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
		ID:    "resp-multiple-thinking",
		Model: "claude-test",
	})
	require.NoError(t, err)

	firstIndex := 0
	secondIndex := 1
	stopReason := "end_turn"
	chunks := []*dto.ClaudeResponse{
		{Type: "message_start", Message: &dto.ClaudeMediaMessage{Id: "msg_stream", Model: "claude-test"}},
		{Type: "content_block_start", Index: &firstIndex, ContentBlock: &dto.ClaudeMediaMessage{Type: "thinking", Thinking: kitutil.GetPointer("")}},
		{Type: "content_block_delta", Index: &firstIndex, Delta: &dto.ClaudeMediaMessage{Type: "thinking_delta", Thinking: kitutil.GetPointer("first thought")}},
		{Type: "content_block_delta", Index: &firstIndex, Delta: &dto.ClaudeMediaMessage{Type: "signature_delta", Signature: "first-signature"}},
		{Type: "content_block_stop", Index: &firstIndex},
		{Type: "content_block_start", Index: &secondIndex, ContentBlock: &dto.ClaudeMediaMessage{Type: "thinking", Thinking: kitutil.GetPointer("")}},
		{Type: "content_block_delta", Index: &secondIndex, Delta: &dto.ClaudeMediaMessage{Type: "thinking_delta", Thinking: kitutil.GetPointer("second thought")}},
		{Type: "content_block_delta", Index: &secondIndex, Delta: &dto.ClaudeMediaMessage{Type: "signature_delta", Signature: "second-signature"}},
		{Type: "content_block_stop", Index: &secondIndex},
		{Type: "message_delta", Delta: &dto.ClaudeMediaMessage{StopReason: &stopReason}, Usage: &dto.ClaudeUsage{OutputTokens: 4}},
		{Type: "message_stop"},
	}

	events := make([]ChatToResponsesStreamEvent, 0)
	for _, chunk := range chunks {
		results, convertErr := ConvertStreamResponseChunk(ctx, meta, state, chunk)
		require.NoError(t, convertErr)
		for _, result := range results {
			event, ok := result.Value.(ChatToResponsesStreamEvent)
			require.True(t, ok)
			events = append(events, event)
		}
	}
	final, err := FinalizeStreamResponse(ctx, meta, state)
	require.NoError(t, err)
	for _, result := range final {
		event, ok := result.Value.(ChatToResponsesStreamEvent)
		require.True(t, ok)
		events = append(events, event)
	}

	var completed *dto.OpenAIResponsesResponse
	for _, event := range events {
		if event.Type == "response.completed" {
			completed = event.Payload.Response
		}
	}
	require.NotNil(t, completed)
	require.Len(t, completed.Output, 2)
	assert.Equal(t, "reasoning", completed.Output[0].Type)
	assert.Equal(t, "first thought", completed.Output[0].Summary[0].Text)
	require.NotEmpty(t, completed.Output[0].EncryptedContent)
	assert.Equal(t, "reasoning", completed.Output[1].Type)
	assert.Equal(t, "second thought", completed.Output[1].Summary[0].Text)
	require.NotEmpty(t, completed.Output[1].EncryptedContent)
	assert.NotEqual(t, completed.Output[0].EncryptedContent, completed.Output[1].EncryptedContent)
}

func TestResponsesMessagesBridgeReplaysRedactedThinkingWithoutVisibleSummary(t *testing.T) {
	const stateSecret = "protocol-bridge-test-secret"
	ctx := WithProtocolBridgeContext(context.Background())
	meta := &convmeta.Values{
		ChannelMetaAttached: true,
		ChannelID:           17,
		Options:             &convmeta.Options{ProviderStateSecret: stateSecret},
	}
	responseResult, err := ConvertResponse(ctx, meta, types.RelayFormatOpenAIResponses, &dto.ClaudeResponse{
		Id:         "msg_redacted_tool",
		Model:      "claude-test",
		StopReason: "tool_use",
		Content: []dto.ClaudeMediaMessage{
			{Type: "redacted_thinking", Data: "opaque-redacted-state"},
			{Type: "tool_use", Id: "call_1", Name: "lookup", Input: map[string]any{}},
		},
	})
	require.NoError(t, err)
	response, ok := responseResult.Value.(*dto.OpenAIResponsesResponse)
	require.True(t, ok)
	require.Len(t, response.Output, 2)
	assert.Equal(t, "reasoning", response.Output[0].Type)
	assert.Empty(t, response.Output[0].Summary)
	require.NotEmpty(t, response.Output[0].EncryptedContent)

	maxOutputTokens := uint(4096)
	requestResult, err := ConvertRequest(WithProtocolBridgeContext(context.Background()), meta, types.RelayFormatClaude, &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Reasoning:       &dto.Reasoning{Effort: "medium"},
		Input: protocolBridgeRaw(t, []any{
			map[string]any{"role": "user", "content": "run it"},
			response.Output[0],
			response.Output[1],
			map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	})
	require.NoError(t, err)
	converted, ok := requestResult.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	assistant, err := converted.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Len(t, assistant, 2)
	assert.Equal(t, "redacted_thinking", assistant[0].Type)
	assert.Equal(t, "opaque-redacted-state", assistant[0].Data)
	require.NotNil(t, converted.Thinking)
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

func TestResponsesChatBridgeLowersAndRestoresLocalShell(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5-codex",
		Tools: protocolBridgeRaw(t, []map[string]any{
			{"type": "local_shell"},
			{"type": "web_search"},
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
		Input: protocolBridgeRaw(t, []map[string]any{
			{
				"type":    "local_shell_call",
				"call_id": "lsh_1",
				"status":  "completed",
				"action":  map[string]any{"type": "exec", "command": []string{"bash", "-lc", "ls"}},
			},
			{
				"type":    "local_shell_call_output",
				"call_id": "lsh_1",
				"output":  "main.go",
			},
			{"role": "user", "content": "run the tests"},
		}),
	}

	requestResult, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	chatRequest, ok := requestResult.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)

	// Declaration: local_shell lowers to a function tool, web_search drops.
	require.Len(t, chatRequest.Tools, 2)
	assert.Equal(t, "local_shell", chatRequest.Tools[0].Function.Name)
	assert.Equal(t, []string{"command"}, chatRequest.Tools[0].Function.Parameters.(map[string]any)["required"])
	assert.Equal(t, "lookup", chatRequest.Tools[1].Function.Name)

	// History: local_shell_call lowers to a paired assistant tool call.
	require.Len(t, chatRequest.Messages, 3)
	toolCalls := chatRequest.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "local_shell", toolCalls[0].Function.Name)
	assert.JSONEq(t, `{"command":["bash","-lc","ls"]}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, "tool", chatRequest.Messages[1].Role)
	assert.Equal(t, "lsh_1", chatRequest.Messages[1].ToolCallId)

	upstream := &dto.OpenAITextResponse{
		Id:    "resp-shell",
		Model: "provider-model",
		Choices: []dto.OpenAITextResponseChoice{
			{FinishReason: "tool_calls", Message: dto.Message{Role: "assistant"}},
		},
	}
	upstream.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_shell", Type: "function", Function: dto.FunctionRequest{Name: "local_shell", Arguments: `{"command":["bash","-lc","go test ./..."],"timeout_ms":60000}`}},
	})

	responseResult, err := ConvertResponse(ctx, &convmeta.Values{}, types.RelayFormatOpenAIResponses, upstream)
	require.NoError(t, err)
	response, ok := responseResult.Value.(*dto.OpenAIResponsesResponse)
	require.True(t, ok)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "local_shell_call", response.Output[0].Type)
	assert.Equal(t, "call_shell", response.Output[0].CallId)
	assert.Empty(t, response.Output[0].Name)
	assert.JSONEq(t, `{"type":"exec","command":["bash","-lc","go test ./..."],"timeout_ms":60000}`, string(response.Output[0].Action))
}

func TestResponsesChatBridgeStreamRestoresLocalShellCall(t *testing.T) {
	ctx := WithProtocolBridgeContext(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5-codex",
		Tools: protocolBridgeRaw(t, []map[string]any{{"type": "local_shell"}}),
		Input: protocolBridgeRaw(t, "list files"),
	}
	_, err := ConvertRequest(ctx, &convmeta.Values{}, types.RelayFormatOpenAI, request)
	require.NoError(t, err)

	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
		ID:    "resp_shell",
		Model: "gpt-5.5-codex",
	})
	require.NoError(t, err)

	index := 0
	_, err = ConvertStreamResponseChunk(ctx, nil, state, &dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "provider-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index: &index,
					ID:    "call_shell",
					Type:  "function",
					Function: dto.FunctionResponse{
						Name:      "local_shell",
						Arguments: `{"command":["bash","-lc","ls"]}`,
					},
				}},
			},
		}},
	})
	require.NoError(t, err)

	finishReason := "tool_calls"
	finishResults, err := ConvertStreamResponseChunk(ctx, nil, state, &dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_1",
		Model:   "provider-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finishReason}},
	})
	require.NoError(t, err)

	finalResults, err := FinalizeStreamResponse(ctx, nil, state)
	require.NoError(t, err)

	var restored *dto.ResponsesOutput
	for _, result := range append(finishResults, finalResults...) {
		event, ok := result.Value.(ChatToResponsesStreamEvent)
		if !ok || event.Payload.Item == nil || event.Payload.Item.Type != "local_shell_call" {
			continue
		}
		if event.Type == "response.output_item.done" {
			restored = event.Payload.Item
		}
	}
	require.NotNil(t, restored, "stream must emit a completed local_shell_call item")
	assert.Equal(t, "call_shell", restored.CallId)
	assert.Empty(t, restored.Name)
	assert.Nil(t, restored.Arguments)
	assert.JSONEq(t, `{"type":"exec","command":["bash","-lc","ls"]}`, string(restored.Action))
}
