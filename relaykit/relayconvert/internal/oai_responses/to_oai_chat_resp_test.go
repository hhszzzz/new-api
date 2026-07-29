package oairesponses

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesResponseToChatCompletionsPreservesTextAndToolCalls(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_1",
		CreatedAt: 123,
		Model:     "gpt-test",
		Status:    []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type: responsesOutputTypeMessage,
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "I will call a tool."},
				},
			},
			{
				Type:      responsesOutputTypeFunctionCall,
				ID:        "fc_1",
				CallId:    "call_1",
				Name:      "lookup",
				Arguments: []byte(`{"q":"x"}`),
			},
		},
		Usage: &dto.Usage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7},
	}

	chat, usage, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_1")
	require.NoError(t, err)
	require.NotNil(t, usage)

	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "tool_calls", chat.Choices[0].FinishReason)
	assert.Equal(t, "I will call a tool.", chat.Choices[0].Message.StringContent())
	toolCalls := chat.Choices[0].Message.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_1", toolCalls[0].ID)
	assert.Equal(t, "lookup", toolCalls[0].Function.Name)
	assert.Equal(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, 7, usage.TotalTokens)
}

func TestResponsesResponseToChatCompletionsWrapsCustomAndToolSearchArguments(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type:   responsesOutputTypeCustomToolCall,
				ID:     "ct_1",
				CallId: "call_custom",
				Name:   "apply_patch",
				Input:  "*** Begin Patch\n*** End Patch",
			},
			{
				Type:      responsesOutputTypeToolSearchCall,
				ID:        "ts_1",
				CallId:    "call_search",
				Execution: "client",
				Arguments: []byte(`{"query":"files"}`),
			},
		},
	}

	chat, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_tools")
	require.NoError(t, err)
	toolCalls := chat.Choices[0].Message.ParseToolCalls()
	require.Len(t, toolCalls, 2)
	assert.Equal(t, "apply_patch", toolCalls[0].Function.Name)
	assert.Equal(t, `{"input":"*** Begin Patch\n*** End Patch"}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, "tool_search", toolCalls[1].Function.Name)
	assert.Equal(t, `{"query":"files"}`, toolCalls[1].Function.Arguments)
	assert.Equal(t, "tool_calls", chat.Choices[0].FinishReason)
}

func TestResponsesResponseToChatCompletionsPreservesReasoningSummary(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:     "resp_1",
		Model:  "gpt-test",
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type: responsesOutputTypeReasoning,
				Summary: []dto.ResponsesReasoningSummaryPart{
					{Type: "summary_text", Text: "first summary"},
					{Type: "summary_text", Text: "\n\nsecond summary"},
				},
			},
			{
				Type: responsesOutputTypeMessage,
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "final"},
				},
			},
		},
	}

	chat, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_1")
	require.NoError(t, err)
	assert.Equal(t, "first summary\n\nsecond summary", chat.Choices[0].Message.GetReasoningContent())
	assert.Equal(t, "final", chat.Choices[0].Message.StringContent())
}

func TestResponsesResponseToChatCompletionsSupportsVisibleReasoningText(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{
			{
				Type: responsesOutputTypeReasoning,
				Content: []dto.ResponsesOutputContent{
					{Type: "reasoning_text", Text: "visible reasoning"},
				},
			},
		},
	}

	chat, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_1")
	require.NoError(t, err)
	assert.Equal(t, "visible reasoning", chat.Choices[0].Message.GetReasoningContent())
}

func TestResponsesResponseToChatCompletionsPreservesRefusalText(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{{
			Type: "message",
			Role: "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type:    "refusal",
				Refusal: "I cannot help with that.",
			}},
		}},
	}

	chat, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_refusal")
	require.NoError(t, err)
	assert.Equal(t, "I cannot help with that.", chat.Choices[0].Message.StringContent())
	assert.Equal(t, "stop", chat.Choices[0].FinishReason)
}

func TestResponsesResponseToChatCompletionsRejectsFailedTerminalEnvelope(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Status: []byte(`"failed"`),
		Error: map[string]any{
			"type":    "server_error",
			"message": "provider generation failed",
		},
	}

	_, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_failed")
	require.ErrorContains(t, err, "provider generation failed")
}

func TestResponsesFinishReasonFromIncompleteStatus(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{name: "max output", reason: responsesIncompleteReasonMaxTokens, want: "length"},
		{name: "content filter", reason: responsesIncompleteReasonContentFilter, want: "content_filter"},
		{name: "unknown", reason: "other", want: "length"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ResponsesFinishReasonFromStatus(&dto.OpenAIResponsesResponse{
				Status:            []byte(`"incomplete"`),
				IncompleteDetails: &dto.IncompleteDetails{Reason: tt.reason},
			})
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResponsesStreamEventToChatChunksUsesOutputIndexForToolArguments(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 1

	var chunks []dto.ChatCompletionsStreamResponse
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventCreated})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventOutputTextDelta, Delta: "text before tool"})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventFunctionArgsDelta,
		OutputIndex: &outputIndex,
		Delta:       `{"cmd":"ls"}`,
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeFunctionCall,
			ID:     "fc_1",
			CallId: "call_1",
			Name:   "exec",
		},
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type: responsesEventCompleted,
		Response: &dto.OpenAIResponsesResponse{
			Status: []byte(`"completed"`),
			Usage:  &dto.Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
		},
	})...)

	require.Len(t, chunks, 4)
	assert.Equal(t, "assistant", chunks[0].Choices[0].Delta.Role)
	assert.Equal(t, "text before tool", chunks[1].Choices[0].Delta.GetContentString())
	tool := chunks[2].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tool.Index)
	assert.Equal(t, 0, *tool.Index)
	assert.Equal(t, "call_1", tool.ID)
	assert.Equal(t, "exec", tool.Function.Name)
	assert.Equal(t, `{"cmd":"ls"}`, tool.Function.Arguments)
	require.NotNil(t, chunks[3].Choices[0].FinishReason)
	assert.Equal(t, "tool_calls", *chunks[3].Choices[0].FinishReason)
	assert.Equal(t, 3, state.Usage.TotalTokens)
}

func TestResponsesStreamEventToChatChunksPreservesRefusalDelta(t *testing.T) {
	state := newTestResponsesStreamState()
	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventCreated})
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:  responsesEventRefusalDelta,
		Delta: "I cannot help with that.",
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventRefusalDone})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:     responsesEventCompleted,
		Response: &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`)},
	})...)

	require.Len(t, chunks, 3)
	assert.Equal(t, "assistant", chunks[0].Choices[0].Delta.Role)
	assert.Equal(t, "I cannot help with that.", chunks[1].Choices[0].Delta.GetContentString())
	require.NotNil(t, chunks[2].Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunks[2].Choices[0].FinishReason)
}

func TestResponsesStreamEventToChatChunksPreservesUpstreamErrorMessage(t *testing.T) {
	state := newTestResponsesStreamState()
	_, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
		Type: responsesEventFailed,
		Response: &dto.OpenAIResponsesResponse{Error: map[string]any{
			"type":    "server_error",
			"message": "provider stream failed",
		}},
	}, state)

	require.ErrorContains(t, err, "provider stream failed")
}

func TestResponsesStreamEventToChatChunksDoesNotDuplicatePendingArgsWithOutputIndexAndItemID(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 1

	var chunks []dto.ChatCompletionsStreamResponse
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventCreated})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventFunctionArgsDelta,
		OutputIndex: &outputIndex,
		ItemID:      "fc_1",
		Delta:       `{"q":"x"}`,
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		ItemID:      "fc_1",
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeFunctionCall,
			ID:     "fc_1",
			CallId: "call_1",
			Name:   "lookup",
		},
	})...)

	require.Len(t, chunks, 2)
	tool := chunks[1].Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, "call_1", tool.ID)
	assert.Equal(t, "lookup", tool.Function.Name)
	assert.Equal(t, `{"q":"x"}`, tool.Function.Arguments)
	assert.Empty(t, state.pendingArgsByOutputIndex)
	assert.Empty(t, state.pendingArgsByItemID)
}

func TestResponsesStreamEventToChatChunksDrainsItemOnlyPendingArgsWhenOutputIndexArrives(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 1

	var chunks []dto.ChatCompletionsStreamResponse
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventCreated})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:   responsesEventFunctionArgsDelta,
		ItemID: "fc_1",
		Delta:  `{"q":"x"}`,
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		ItemID:      "fc_1",
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeFunctionCall,
			CallId: "call_1",
			Name:   "lookup",
		},
	})...)

	require.Len(t, chunks, 2)
	tool := chunks[1].Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, "call_1", tool.ID)
	assert.Equal(t, "lookup", tool.Function.Name)
	assert.Equal(t, `{"q":"x"}`, tool.Function.Arguments)
	assert.Empty(t, state.pendingArgsByOutputIndex)
	assert.Empty(t, state.pendingArgsByItemID)
}

func TestResponsesStreamEventToChatChunksCustomToolAndReasoning(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 0

	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:  responsesEventReasoningTextDelta,
		Delta: "thinking",
	})
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeCustomToolCall,
			ID:     "ct_1",
			CallId: "call_custom",
			Name:   "apply_patch",
		},
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventCustomToolInputDelta,
		OutputIndex: &outputIndex,
		Delta:       "patch body",
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventCustomToolInputDone,
		OutputIndex: &outputIndex,
		Input:       "patch body",
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type: responsesEventIncomplete,
		Response: &dto.OpenAIResponsesResponse{
			IncompleteDetails: &dto.IncompleteDetails{Reason: responsesIncompleteReasonContentFilter},
		},
	})...)

	require.Len(t, chunks, 5)
	assert.Equal(t, "thinking", chunks[1].Choices[0].Delta.GetReasoningContent())
	assert.Equal(t, "apply_patch", chunks[2].Choices[0].Delta.ToolCalls[0].Function.Name)
	assert.Equal(t, `{"input":"patch body"}`, chunks[3].Choices[0].Delta.ToolCalls[0].Function.Arguments)
	require.NotNil(t, chunks[4].Choices[0].FinishReason)
	assert.Equal(t, "content_filter", *chunks[4].Choices[0].FinishReason)
}

func TestResponsesStreamEventToChatChunksBuffersCustomInputAsValidFunctionArguments(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 0
	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeCustomToolCall,
			ID:     "ct_1",
			CallId: "call_custom",
			Name:   "apply_patch",
		},
	})
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventCustomToolInputDelta,
		OutputIndex: &outputIndex,
		ItemID:      "ct_1",
		Delta:       "line 1\n",
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventCustomToolInputDone,
		OutputIndex: &outputIndex,
		ItemID:      "ct_1",
		Input:       "line 1\nline 2",
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemDone,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeCustomToolCall,
			ID:     "ct_1",
			CallId: "call_custom",
			Name:   "apply_patch",
			Input:  "line 1\nline 2",
		},
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:     responsesEventCompleted,
		Response: &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`)},
	})...)

	var name string
	var arguments strings.Builder
	for _, chunk := range chunks {
		if len(chunk.Choices) == 0 {
			continue
		}
		for _, toolCall := range chunk.Choices[0].Delta.ToolCalls {
			if toolCall.Function.Name != "" {
				name = toolCall.Function.Name
			}
			arguments.WriteString(toolCall.Function.Arguments)
		}
	}
	assert.Equal(t, "apply_patch", name)
	assert.Equal(t, `{"input":"line 1\nline 2"}`, arguments.String())
}

func TestResponsesStreamEventToChatChunksRestoresToolSearchCall(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 0
	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:      responsesOutputTypeToolSearchCall,
			ID:        "ts_1",
			CallId:    "call_search",
			Execution: "client",
			Arguments: []byte(`{"query":"files"}`),
		},
	})

	require.Len(t, chunks, 2)
	toolCall := chunks[1].Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, "call_search", toolCall.ID)
	assert.Equal(t, "tool_search", toolCall.Function.Name)
	assert.Equal(t, `{"query":"files"}`, toolCall.Function.Arguments)
}

func TestResponsesStreamEventToChatChunksPreservesMultipleReasoningItems(t *testing.T) {
	state := newTestResponsesStreamState()
	firstOutputIndex := 0
	secondOutputIndex := 1

	firstDone := &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemDone,
		OutputIndex: &firstOutputIndex,
		Item: &dto.ResponsesOutput{
			Type:    responsesOutputTypeReasoning,
			ID:      "rs_first",
			Summary: []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "first thought"}},
		},
	}
	secondDone := &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemDone,
		OutputIndex: &secondOutputIndex,
		Item: &dto.ResponsesOutput{
			Type:    responsesOutputTypeReasoning,
			ID:      "rs_second",
			Summary: []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "second thought"}},
		},
	}

	chunks := mustStreamChunks(t, state, firstDone)
	chunks = append(chunks, mustStreamChunks(t, state, secondDone)...)

	var reasoning strings.Builder
	for _, chunk := range chunks {
		if len(chunk.Choices) == 0 {
			continue
		}
		reasoning.WriteString(chunk.Choices[0].Delta.GetReasoningContent())
	}
	assert.Equal(t, "first thoughtsecond thought", reasoning.String())
}

func TestResponsesStreamEventToChatChunksUsesTerminalDoneOutput(t *testing.T) {
	state := newTestResponsesStreamState()
	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type: responsesEventDone,
		Response: &dto.OpenAIResponsesResponse{
			Status: []byte(`"completed"`),
			Output: []dto.ResponsesOutput{
				{
					Type: responsesOutputTypeMessage,
					Role: "assistant",
					Content: []dto.ResponsesOutputContent{
						{Type: "output_text", Text: "terminal text"},
					},
				},
				{
					Type:      responsesOutputTypeFunctionCall,
					ID:        "fc_1",
					CallId:    "call_1",
					Name:      "lookup",
					Arguments: []byte(`{"q":"x"}`),
				},
			},
		},
	})

	require.Len(t, chunks, 4)
	assert.Equal(t, "assistant", chunks[0].Choices[0].Delta.Role)
	assert.Equal(t, "terminal text", chunks[1].Choices[0].Delta.GetContentString())
	tool := chunks[2].Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, "lookup", tool.Function.Name)
	assert.Equal(t, `{"q":"x"}`, tool.Function.Arguments)
	require.NotNil(t, chunks[3].Choices[0].FinishReason)
	assert.Equal(t, "tool_calls", *chunks[3].Choices[0].FinishReason)
}

func TestResponsesStreamEventToChatChunksEmitsEveryTerminalMessageItem(t *testing.T) {
	state := newTestResponsesStreamState()
	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type: responsesEventDone,
		Response: &dto.OpenAIResponsesResponse{
			Status: []byte(`"completed"`),
			Output: []dto.ResponsesOutput{
				{
					Type: responsesOutputTypeMessage,
					ID:   "msg_first",
					Role: "assistant",
					Content: []dto.ResponsesOutputContent{
						{Type: "output_text", Text: "first"},
					},
				},
				{
					Type: responsesOutputTypeMessage,
					ID:   "msg_second",
					Role: "assistant",
					Content: []dto.ResponsesOutputContent{
						{Type: "output_text", Text: "second"},
					},
				},
			},
		},
	})

	require.Len(t, chunks, 4)
	assert.Equal(t, "assistant", chunks[0].Choices[0].Delta.Role)
	assert.Equal(t, "first", chunks[1].Choices[0].Delta.GetContentString())
	assert.Equal(t, "second", chunks[2].Choices[0].Delta.GetContentString())
	require.NotNil(t, chunks[3].Choices[0].FinishReason)
}

func TestResponsesStreamEventToChatChunksSupplementsTextSuffixAndLaterMessage(t *testing.T) {
	state := newTestResponsesStreamState()
	firstIndex := 0
	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputTextDelta,
		OutputIndex: &firstIndex,
		ItemID:      "msg_first",
		Delta:       "hel",
	})
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type: responsesEventCompleted,
		Response: &dto.OpenAIResponsesResponse{
			Status: []byte(`"completed"`),
			Output: []dto.ResponsesOutput{
				{
					Type: responsesOutputTypeMessage,
					ID:   "msg_first",
					Role: "assistant",
					Content: []dto.ResponsesOutputContent{
						{Type: "output_text", Text: "hello"},
					},
				},
				{
					Type: responsesOutputTypeMessage,
					ID:   "msg_second",
					Role: "assistant",
					Content: []dto.ResponsesOutputContent{
						{Type: "output_text", Text: "world"},
					},
				},
			},
		},
	})...)

	var text strings.Builder
	for _, chunk := range chunks {
		if len(chunk.Choices) > 0 {
			text.WriteString(chunk.Choices[0].Delta.GetContentString())
		}
	}
	assert.Equal(t, "helloworld", text.String())
}

func TestResponsesStreamEventToChatChunksSupplementsReasoningSuffixWithoutFalseBreak(t *testing.T) {
	state := newTestResponsesStreamState()
	reasoningIndex := 0
	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventReasoningSummaryDelta,
		OutputIndex: &reasoningIndex,
		ItemID:      "rs_first",
		Delta:       "think",
	})
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventReasoningSummaryDone,
		OutputIndex: &reasoningIndex,
		ItemID:      "rs_first",
		Text:        "thinking",
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type: responsesEventCompleted,
		Response: &dto.OpenAIResponsesResponse{
			Status: []byte(`"completed"`),
			Output: []dto.ResponsesOutput{
				{
					Type:    responsesOutputTypeReasoning,
					ID:      "rs_first",
					Summary: []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "thinking"}},
				},
				{
					Type:    responsesOutputTypeMessage,
					ID:      "msg_answer",
					Role:    "assistant",
					Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "answer"}},
				},
			},
		},
	})...)

	var reasoning strings.Builder
	var text strings.Builder
	for _, chunk := range chunks {
		if len(chunk.Choices) == 0 {
			continue
		}
		reasoning.WriteString(chunk.Choices[0].Delta.GetReasoningContent())
		text.WriteString(chunk.Choices[0].Delta.GetContentString())
	}
	assert.Equal(t, "thinking", reasoning.String())
	assert.Equal(t, "answer", text.String())
}

func TestResponsesStreamEventToChatChunksDoesNotResendToolOnTerminalOutput(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 0

	var chunks []dto.ChatCompletionsStreamResponse
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventCreated})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeFunctionCall,
			ID:     "fc_1",
			CallId: "call_1",
			Name:   "lookup",
		},
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventFunctionArgsDelta,
		OutputIndex: &outputIndex,
		Delta:       `{"q":"x"}`,
	})...)
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type: responsesEventCompleted,
		Response: &dto.OpenAIResponsesResponse{
			Status: []byte(`"completed"`),
			Output: []dto.ResponsesOutput{
				{
					Type:      responsesOutputTypeFunctionCall,
					ID:        "fc_1",
					CallId:    "call_1",
					Name:      "lookup",
					Arguments: []byte(`{"q":"x"}`),
				},
			},
		},
	})...)

	totalArgs := ""
	toolIndexes := map[int]bool{}
	var finishReason string
	for _, chunk := range chunks {
		for _, choice := range chunk.Choices {
			for _, tc := range choice.Delta.ToolCalls {
				require.NotNil(t, tc.Index)
				toolIndexes[*tc.Index] = true
				totalArgs += tc.Function.Arguments
			}
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
		}
	}

	assert.Equal(t, map[int]bool{0: true}, toolIndexes)
	assert.Equal(t, `{"q":"x"}`, totalArgs)
	assert.Equal(t, "tool_calls", finishReason)
}

func TestFinalizeResponsesToChatStreamSkipsUnnamedPendingTool(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 2
	_, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
		Type:        responsesEventFunctionArgsDelta,
		OutputIndex: &outputIndex,
		Delta:       `{"pending":true}`,
	}, state)
	require.NoError(t, err)

	chunks := FinalizeResponsesToChatStream(state)
	require.Len(t, chunks, 2)
	assert.Empty(t, chunks[0].Choices[0].Delta.ToolCalls)
	require.NotNil(t, chunks[1].Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunks[1].Choices[0].FinishReason)
}

func TestResponsesStreamEventToChatChunksFailedEventReturnsError(t *testing.T) {
	_, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{Type: responsesEventFailed}, newTestResponsesStreamState())
	require.Error(t, err)
}

func TestFinalizeResponsesToChatStreamCheckedMarksTextOnlyEOFAsLength(t *testing.T) {
	state := newTestResponsesStreamState()
	mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventOutputTextDelta, Delta: "partial"})

	chunks, err := FinalizeResponsesToChatStreamChecked(state)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	finish := chunks[len(chunks)-1].Choices[0].FinishReason
	require.NotNil(t, finish)
	assert.Equal(t, "length", *finish)
}

func TestFinalizeResponsesToChatStreamCheckedRejectsOpenToolItem(t *testing.T) {
	state := newTestResponsesStreamState()
	outputIndex := 0
	mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeFunctionCall,
			ID:     "fc_1",
			CallId: "call_1",
			Name:   "lookup",
		},
	})
	mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:        responsesEventFunctionArgsDelta,
		OutputIndex: &outputIndex,
		ItemID:      "fc_1",
		Delta:       `{"q":`,
	})

	_, err := FinalizeResponsesToChatStreamChecked(state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete reasoning or tool-call")
}

func TestResponsesBufferedAccumulatorSupplementsEmptyTerminalOutput(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	outputIndex := 1
	acc.ProcessEvent(&dto.ResponsesStreamResponse{Type: responsesEventOutputTextDelta, Delta: "buffered text"})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeFunctionCall,
			ID:     "fc_1",
			CallId: "call_1",
			Name:   "lookup",
		},
	})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventFunctionArgsDelta,
		OutputIndex: &outputIndex,
		Delta:       `{"q":"x"}`,
	})

	resp := &dto.OpenAIResponsesResponse{
		Status: []byte(`"completed"`),
		Model:  "gpt-test",
	}
	acc.SupplementResponseOutput(resp)

	chat, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_1")
	require.NoError(t, err)
	assert.Equal(t, "buffered text", chat.Choices[0].Message.StringContent())
	toolCalls := chat.Choices[0].Message.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
}

func TestResponsesBufferedAccumulatorDoesNotDuplicatePendingArgsWithOutputIndexAndItemID(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	outputIndex := 1
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventFunctionArgsDelta,
		OutputIndex: &outputIndex,
		ItemID:      "fc_1",
		Delta:       `{"q":"x"}`,
	})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &outputIndex,
		ItemID:      "fc_1",
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeFunctionCall,
			ID:     "fc_1",
			CallId: "call_1",
			Name:   "lookup",
		},
	})

	resp := &dto.OpenAIResponsesResponse{
		Status: []byte(`"completed"`),
		Model:  "gpt-test",
	}
	acc.SupplementResponseOutput(resp)

	chat, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_1")
	require.NoError(t, err)
	toolCalls := chat.Choices[0].Message.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
	assert.Empty(t, acc.pendingByOutputIndex)
	assert.Empty(t, acc.pendingByItemID)
}

func TestResponsesBufferedAccumulatorMergesPartialTerminalOutput(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	reasoningIndex := 0
	messageIndex := 1
	toolIndex := 2
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &reasoningIndex,
		Item:        &dto.ResponsesOutput{Type: responsesOutputTypeReasoning, ID: "rs_1"},
	})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventReasoningSummaryDelta,
		OutputIndex: &reasoningIndex,
		ItemID:      "rs_1",
		Delta:       "inspect",
	})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &messageIndex,
		Item:        &dto.ResponsesOutput{Type: responsesOutputTypeMessage, ID: "msg_1", Role: "assistant"},
	})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventOutputTextDelta,
		OutputIndex: &messageIndex,
		ItemID:      "msg_1",
		Delta:       "answer",
	})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: &toolIndex,
		Item: &dto.ResponsesOutput{
			Type:   responsesOutputTypeFunctionCall,
			ID:     "fc_1",
			CallId: "call_1",
			Name:   "lookup",
		},
	})
	acc.ProcessEvent(&dto.ResponsesStreamResponse{
		Type:        responsesEventFunctionArgsDelta,
		OutputIndex: &toolIndex,
		ItemID:      "fc_1",
		Delta:       `{"q":"x"}`,
	})

	resp := &dto.OpenAIResponsesResponse{
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type:   responsesOutputTypeFunctionCall,
				ID:     "fc_1",
				CallId: "call_1",
				Name:   "lookup",
			},
		},
	}
	acc.SupplementResponseOutput(resp)

	require.Len(t, resp.Output, 3)
	assert.Equal(t, []string{responsesOutputTypeReasoning, responsesOutputTypeMessage, responsesOutputTypeFunctionCall}, []string{
		resp.Output[0].Type,
		resp.Output[1].Type,
		resp.Output[2].Type,
	})
	assert.Equal(t, "inspect", resp.Output[0].Summary[0].Text)
	assert.Equal(t, "answer", resp.Output[1].Content[0].Text)
	assert.Equal(t, `{"q":"x"}`, resp.Output[2].ArgumentsString())
}

func TestResponsesBufferedAccumulatorPreservesIndexedOutputOrderAndReasoningBlocks(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	events := []dto.ResponsesStreamResponse{
		{Type: responsesEventOutputItemAdded, OutputIndex: intPointer(0), Item: &dto.ResponsesOutput{Type: responsesOutputTypeMessage, ID: "msg_1", Role: "assistant"}},
		{Type: responsesEventOutputTextDelta, OutputIndex: intPointer(0), ItemID: "msg_1", Delta: "before"},
		{Type: responsesEventOutputItemAdded, OutputIndex: intPointer(1), Item: &dto.ResponsesOutput{Type: responsesOutputTypeReasoning, ID: "rs_1"}},
		{Type: responsesEventReasoningSummaryDelta, OutputIndex: intPointer(1), ItemID: "rs_1", Delta: "first"},
		{Type: responsesEventOutputItemAdded, OutputIndex: intPointer(2), Item: &dto.ResponsesOutput{Type: responsesOutputTypeFunctionCall, ID: "fc_1", CallId: "call_1", Name: "lookup"}},
		{Type: responsesEventFunctionArgsDelta, OutputIndex: intPointer(2), ItemID: "fc_1", Delta: `{}`},
		{Type: responsesEventOutputItemAdded, OutputIndex: intPointer(3), Item: &dto.ResponsesOutput{Type: responsesOutputTypeReasoning, ID: "rs_2"}},
		{Type: responsesEventReasoningSummaryDelta, OutputIndex: intPointer(3), ItemID: "rs_2", Delta: "second"},
		{Type: responsesEventOutputItemAdded, OutputIndex: intPointer(4), Item: &dto.ResponsesOutput{Type: responsesOutputTypeMessage, ID: "msg_2", Role: "assistant"}},
		{Type: responsesEventOutputTextDelta, OutputIndex: intPointer(4), ItemID: "msg_2", Delta: "after"},
	}
	for index := range events {
		acc.ProcessEvent(&events[index])
	}

	output := acc.BuildOutput()
	require.Len(t, output, 5)
	assert.Equal(t, []string{
		responsesOutputTypeMessage,
		responsesOutputTypeReasoning,
		responsesOutputTypeFunctionCall,
		responsesOutputTypeReasoning,
		responsesOutputTypeMessage,
	}, []string{output[0].Type, output[1].Type, output[2].Type, output[3].Type, output[4].Type})
	assert.Equal(t, "before", output[0].Content[0].Text)
	assert.Equal(t, "first", output[1].Summary[0].Text)
	assert.Equal(t, "second", output[3].Summary[0].Text)
	assert.Equal(t, "after", output[4].Content[0].Text)
}

func intPointer(value int) *int {
	return &value
}

func newTestResponsesStreamState() *ResponsesToChatStreamState {
	state := NewResponsesToChatStreamState("gpt-test", false)
	state.ID = "chatcmpl_test"
	state.Created = 123
	return state
}

func mustStreamChunks(t *testing.T, state *ResponsesToChatStreamState, event *dto.ResponsesStreamResponse) []dto.ChatCompletionsStreamResponse {
	t.Helper()
	chunks, err := ResponsesStreamEventToChatChunks(event, state)
	require.NoError(t, err)
	return chunks
}

func TestResponsesToolCallArgumentsToolSearchWithoutPayloadIsEmptyObject(t *testing.T) {
	output := &dto.ResponsesOutput{
		Type:   responsesOutputTypeToolSearchCall,
		CallId: "call_1",
		Name:   "tool_search",
	}

	assert.Equal(t, "{}", responsesToolCallArguments(output))
	assert.Equal(t, "", toolSearchArguments(output.Arguments))
	assert.Equal(t, `{"query":"x"}`, toolSearchArguments([]byte(`{"query":"x"}`)))
}

func TestResponsesStreamRefusalDoneWithoutDeltasUsesRefusalField(t *testing.T) {
	state := newTestResponsesStreamState()
	chunks := mustStreamChunks(t, state, &dto.ResponsesStreamResponse{Type: responsesEventCreated})
	chunks = append(chunks, mustStreamChunks(t, state, &dto.ResponsesStreamResponse{
		Type:    responsesEventRefusalDone,
		Refusal: "I cannot help with that.",
	})...)

	require.Len(t, chunks, 2)
	assert.Equal(t, "I cannot help with that.", chunks[1].Choices[0].Delta.GetContentString())
}

func TestExtractOutputTextFromResponsesSkipsReasoningOnlyOutput(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{
			{
				Type: responsesOutputTypeReasoning,
				Content: []dto.ResponsesOutputContent{
					{Type: "reasoning_text", Text: "secret chain of thought"},
				},
			},
		},
	}

	assert.Empty(t, ExtractOutputTextFromResponses(resp))
}
