package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatCompletionsResponseToResponsesPreservesTextToolCallsAndUsage(t *testing.T) {
	chat := &dto.OpenAITextResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 456,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message:      assistantMessageWithTool("I will call.", "call_1", "lookup", `{"q":"x"}`),
				FinishReason: "tool_calls",
			},
		},
		Usage: dto.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
	}

	resp, usage, err := ChatCompletionsResponseToResponsesResponse(chat, "resp_1")
	require.NoError(t, err)
	require.NotNil(t, usage)

	assert.Equal(t, "resp_1", resp.ID)
	assert.Equal(t, "response", resp.Object)
	assert.Equal(t, `"completed"`, string(resp.Status))
	assert.Equal(t, 3, resp.Usage.InputTokens)
	assert.Equal(t, 5, resp.Usage.OutputTokens)
	require.Len(t, resp.Output, 2)
	assert.Equal(t, responsesOutputTypeMessage, resp.Output[0].Type)
	assert.Equal(t, "I will call.", resp.Output[0].Content[0].Text)
	assert.Equal(t, "commentary", resp.Output[0].Phase)
	assert.Equal(t, responsesOutputTypeFunctionCall, resp.Output[1].Type)
	assert.Equal(t, "call_1", resp.Output[1].CallId)
	assert.Equal(t, "lookup", resp.Output[1].Name)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(resp.Output[1].Arguments))
}

func TestChatCompletionsResponseToResponsesMapsIncompleteFinishReasons(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		wantReason   string
	}{
		{name: "length", finishReason: "length", wantReason: responsesIncompleteReasonMaxTokens},
		{name: "content filter", finishReason: "content_filter", wantReason: responsesIncompleteReasonContentFilter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _, err := ChatCompletionsResponseToResponsesResponse(&dto.OpenAITextResponse{
				Id:    "chatcmpl_1",
				Model: "gpt-test",
				Choices: []dto.OpenAITextResponseChoice{
					{
						Message:      dto.Message{Role: "assistant", Content: "partial"},
						FinishReason: tt.finishReason,
					},
				},
			}, "resp_1")
			require.NoError(t, err)

			assert.Equal(t, `"incomplete"`, string(resp.Status))
			require.NotNil(t, resp.IncompleteDetails)
			assert.Equal(t, tt.wantReason, resp.IncompleteDetails.Reason)
			require.Len(t, resp.Output, 1)
			assert.Equal(t, "incomplete", resp.Output[0].Status)
		})
	}
}

func TestChatCompletionsResponseToResponsesUsesReasoningSummaryContract(t *testing.T) {
	reasoning := "reasoning summary"
	resp, _, err := ChatCompletionsResponseToResponsesResponse(&dto.OpenAITextResponse{
		Choices: []dto.OpenAITextResponseChoice{
			{Message: dto.Message{Role: "assistant", ReasoningContent: &reasoning}},
		},
	}, "resp_1")
	require.NoError(t, err)

	encoded, err := kitutil.Marshal(resp)
	require.NoError(t, err)
	assert.Equal(t, "reasoning", gjson.GetBytes(encoded, "output.0.type").String())
	assert.Equal(t, "summary_text", gjson.GetBytes(encoded, "output.0.summary.0.type").String())
	assert.Equal(t, reasoning, gjson.GetBytes(encoded, "output.0.summary.0.text").String())
	assert.False(t, gjson.GetBytes(encoded, "output.0.content").Exists())
}

func TestChatCompletionsResponseToResponsesPreservesCompatibleReasoningRefusalAndLegacyFunctionCall(t *testing.T) {
	var chat dto.OpenAITextResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"id":"chatcmpl_compat",
		"model":"compat-model",
		"choices":[{
			"message":{
				"role":"assistant",
				"content":null,
				"reasoning":{"summary":"Need to refuse and call a guard."},
				"refusal":"I cannot help with that.",
				"function_call":{"name":"report_refusal","arguments":"{\"safe\":true}"}
			},
			"finish_reason":"function_call"
		}]
	}`), &chat))

	resp, _, err := ChatCompletionsResponseToResponsesResponse(&chat, "resp_compat")
	require.NoError(t, err)
	require.Len(t, resp.Output, 3)

	assert.Equal(t, responsesOutputTypeReasoning, resp.Output[0].Type)
	assert.Equal(t, "Need to refuse and call a guard.", resp.Output[0].Summary[0].Text)
	assert.Equal(t, responsesOutputTypeMessage, resp.Output[1].Type)
	require.Len(t, resp.Output[1].Content, 1)
	assert.Equal(t, "refusal", resp.Output[1].Content[0].Type)
	assert.Equal(t, "I cannot help with that.", resp.Output[1].Content[0].Refusal)
	assert.Equal(t, responsesOutputTypeFunctionCall, resp.Output[2].Type)
	assert.Equal(t, "call_0", resp.Output[2].CallId)
	assert.Equal(t, "report_refusal", resp.Output[2].Name)
}

func TestChatCompletionsStreamToResponsesEmitsRefusalLifecycle(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_refusal", "compat-model")
	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"choices":[{"index":0,"delta":{"reasoning_details":[{"text":"Unsafe request."}]}}]
	}`), &chunk))
	events := mustResponsesEventsFromChatChunk(t, state, &chunk)

	chunk = dto.ChatCompletionsStreamResponse{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"choices":[{"index":0,"delta":{"refusal":"I cannot help."}}]
	}`), &chunk))
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &chunk)...)
	finishReason := "content_filter"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finishReason}},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	var refusalDelta *dto.ResponsesStreamResponse
	var refusalDone *dto.ResponsesStreamResponse
	for index := range events {
		switch events[index].Type {
		case responsesEventRefusalDelta:
			refusalDelta = &events[index].Payload
		case responsesEventRefusalDone:
			refusalDone = &events[index].Payload
		}
	}
	require.NotNil(t, refusalDelta)
	assert.Equal(t, "I cannot help.", refusalDelta.Delta)
	require.NotNil(t, refusalDone)
	assert.Equal(t, "I cannot help.", refusalDone.Refusal)

	completed := events[len(events)-1].Payload.Response
	require.NotNil(t, completed)
	assert.Equal(t, `"incomplete"`, string(completed.Status))
	require.Len(t, completed.Output, 2)
	assert.Equal(t, responsesOutputTypeReasoning, completed.Output[0].Type)
	assert.Equal(t, "Unsafe request.", completed.Output[0].Summary[0].Text)
	assert.Equal(t, responsesOutputTypeMessage, completed.Output[1].Type)
	require.Len(t, completed.Output[1].Content, 1)
	assert.Equal(t, "refusal", completed.Output[1].Content[0].Type)
	assert.Equal(t, "I cannot help.", completed.Output[1].Content[0].Refusal)
}

func TestChatCompletionsStreamToResponsesNormalizesLegacyFunctionCall(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_legacy", "compat-model")
	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"choices":[{"index":0,"delta":{"function_call":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}}]
	}`), &chunk))
	events := mustResponsesEventsFromChatChunk(t, state, &chunk)
	finishReason := "function_call"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finishReason}},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	completed := events[len(events)-1].Payload.Response
	require.NotNil(t, completed)
	require.Len(t, completed.Output, 1)
	assert.Equal(t, responsesOutputTypeFunctionCall, completed.Output[0].Type)
	assert.Equal(t, "call_0", completed.Output[0].CallId)
	assert.Equal(t, "lookup", completed.Output[0].Name)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(completed.Output[0].Arguments))
}

func TestChatCompletionsStreamToResponsesEventsAggregatesUsageAndToolArgs(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_1", "gpt-test")
	state.Created = 123
	toolIndex := 0

	var events []ChatToResponsesStreamEvent
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 123,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: lo.ToPtr("hello")}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, ID: "call_1", Type: "function", Function: dto.FunctionResponse{Name: "lookup"}},
			}}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, Function: dto.FunctionResponse{Arguments: `{"q":"x"}`}},
			}}},
		},
	})...)
	finishReason := "tool_calls"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, FinishReason: &finishReason},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{PromptTokens: 2, CompletionTokens: 4, TotalTokens: 6},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	require.Len(t, events, 13)
	assert.Equal(t, responsesEventCreated, events[0].Type)
	assert.Equal(t, responsesEventInProgress, events[1].Type)
	assert.Equal(t, responsesEventOutputItemAdded, events[2].Type)
	assert.Equal(t, responsesEventContentPartAdded, events[3].Type)
	assert.Equal(t, responsesEventOutputTextDelta, events[4].Type)
	assert.Equal(t, "hello", events[4].Payload.Delta)
	assert.Equal(t, responsesEventFunctionArgsDelta, events[6].Type)
	assert.Equal(t, `{"q":"x"}`, events[6].Payload.Delta)
	assert.Equal(t, responsesEventOutputTextDone, events[7].Type)
	assert.Equal(t, "hello", events[7].Payload.Text)
	assert.Equal(t, responsesEventContentPartDone, events[8].Type)
	assert.Equal(t, responsesEventOutputItemDone, events[9].Type)
	assert.Equal(t, "commentary", events[9].Payload.Item.Phase)
	assert.Equal(t, responsesEventCompleted, events[12].Type)
	require.NotNil(t, events[12].Payload.Response)
	assert.Equal(t, 6, events[12].Payload.Response.Usage.TotalTokens)
	require.Len(t, events[12].Payload.Response.Output, 2)
	assert.Equal(t, "hello", events[12].Payload.Response.Output[0].Content[0].Text)
	assert.Equal(t, "commentary", events[12].Payload.Response.Output[0].Phase)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(events[12].Payload.Response.Output[1].Arguments))
	for index, event := range events {
		require.NotNil(t, event.Payload.SequenceNumber)
		assert.Equal(t, index, *event.Payload.SequenceNumber)
	}
}

func TestChatCompletionsStreamToResponsesEventsEmitsReasoningLifecycle(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_reasoning", "gpt-test")
	state.Created = 123

	events := mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ReasoningContent: lo.ToPtr("reasoning summary"),
				},
			},
		},
	})
	finishReason := "stop"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, FinishReason: &finishReason},
		},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	require.Len(t, events, 9)
	wantTypes := []string{
		responsesEventCreated,
		responsesEventInProgress,
		responsesEventOutputItemAdded,
		responsesEventReasoningPartAdded,
		responsesEventReasoningSummaryDelta,
		responsesEventReasoningSummaryDone,
		responsesEventReasoningPartDone,
		responsesEventOutputItemDone,
		responsesEventCompleted,
	}
	for index, event := range events {
		assert.Equal(t, wantTypes[index], event.Type)
		require.NotNil(t, event.Payload.SequenceNumber)
		assert.Equal(t, index, *event.Payload.SequenceNumber)
	}

	addedItem := events[2].Payload.Item
	require.NotNil(t, addedItem)
	assert.Equal(t, responsesOutputTypeReasoning, addedItem.Type)
	assert.Equal(t, "rs_reasoning_0", addedItem.ID)
	assert.Equal(t, "in_progress", addedItem.Status)

	addedPart, ok := events[3].Payload.Part.(*dto.ResponsesReasoningSummaryPart)
	require.True(t, ok)
	assert.Equal(t, "summary_text", addedPart.Type)
	assert.Empty(t, addedPart.Text)
	require.NotNil(t, events[3].Payload.OutputIndex)
	assert.Equal(t, 0, *events[3].Payload.OutputIndex)
	require.NotNil(t, events[3].Payload.SummaryIndex)
	assert.Equal(t, 0, *events[3].Payload.SummaryIndex)

	assert.Equal(t, "reasoning summary", events[4].Payload.Delta)
	assert.Equal(t, "reasoning summary", events[5].Payload.Text)
	donePart, ok := events[6].Payload.Part.(*dto.ResponsesReasoningSummaryPart)
	require.True(t, ok)
	assert.Equal(t, "reasoning summary", donePart.Text)

	doneItem := events[7].Payload.Item
	require.NotNil(t, doneItem)
	assert.Equal(t, "completed", doneItem.Status)
	require.Len(t, doneItem.Summary, 1)
	assert.Equal(t, "summary_text", doneItem.Summary[0].Type)
	assert.Equal(t, "reasoning summary", doneItem.Summary[0].Text)

	completed := events[8].Payload.Response
	require.NotNil(t, completed)
	require.Len(t, completed.Output, 1)
	assert.Equal(t, "reasoning summary", completed.Output[0].Summary[0].Text)
}

func TestChatCompletionsStreamToResponsesPreservesMultipleReasoningStates(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_reasoning_multi", "gpt-test")

	var events []ChatToResponsesStreamEvent
	for _, item := range []struct {
		text      string
		encrypted string
	}{
		{text: "first thought", encrypted: "state-1"},
		{text: "second thought", encrypted: "state-2"},
	} {
		events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
			Choices: []dto.ChatCompletionsStreamResponseChoice{{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: lo.ToPtr(item.text)},
			}},
		})...)
		events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
			ReasoningEncryptedContent: item.encrypted,
		})...)
	}

	finishReason := "stop"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finishReason}},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	completed := events[len(events)-1].Payload.Response
	require.NotNil(t, completed)
	require.Len(t, completed.Output, 2)
	assert.Equal(t, "first thought", completed.Output[0].Summary[0].Text)
	assert.Equal(t, "state-1", completed.Output[0].EncryptedContent)
	assert.Equal(t, "second thought", completed.Output[1].Summary[0].Text)
	assert.Equal(t, "state-2", completed.Output[1].EncryptedContent)
	assert.NotEqual(t, completed.Output[0].ID, completed.Output[1].ID)

	var doneItems []*dto.ResponsesOutput
	for index, event := range events {
		require.NotNil(t, event.Payload.SequenceNumber)
		assert.Equal(t, index, *event.Payload.SequenceNumber)
		if event.Type == responsesEventOutputItemDone && event.Payload.Item != nil && event.Payload.Item.Type == responsesOutputTypeReasoning {
			doneItems = append(doneItems, event.Payload.Item)
		}
	}
	require.Len(t, doneItems, 2)
	assert.Equal(t, "state-1", doneItems[0].EncryptedContent)
	assert.Equal(t, "state-2", doneItems[1].EncryptedContent)
}

func TestChatCompletionsStreamToResponsesWaitsForRealToolCallID(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_delayed", "gpt-test")
	toolIndex := 0

	events := mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: &toolIndex,
				Type:  "function",
				Function: dto.FunctionResponse{
					Name:      "lookup",
					Arguments: `{"q":`,
				},
			}}},
		}},
	})
	require.Len(t, events, 2)
	assert.Equal(t, responsesEventCreated, events[0].Type)
	assert.Equal(t, responsesEventInProgress, events[1].Type)

	events = mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: &toolIndex,
				ID:    "call_real",
				Function: dto.FunctionResponse{
					Arguments: `"x"}`,
				},
			}}},
		}},
	})
	require.Len(t, events, 2)
	assert.Equal(t, responsesEventOutputItemAdded, events[0].Type)
	require.NotNil(t, events[0].Payload.Item)
	assert.Equal(t, "call_real", events[0].Payload.Item.CallId)
	itemID := events[0].Payload.Item.ID
	assert.Equal(t, "fc_delayed_0", itemID)
	assert.Equal(t, responsesEventFunctionArgsDelta, events[1].Type)
	assert.Equal(t, itemID, events[1].Payload.ItemID)
	assert.Equal(t, `{"q":"x"}`, events[1].Payload.Delta)

	finishReason := "tool_calls"
	events = mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finishReason}},
	})
	final := FinalizeChatCompletionsStreamToResponses(state)
	events = append(events, final...)

	for _, event := range events {
		if event.Payload.ItemID != "" {
			assert.Equal(t, itemID, event.Payload.ItemID)
		}
		if event.Payload.Item != nil {
			assert.Equal(t, itemID, event.Payload.Item.ID)
			assert.Equal(t, "call_real", event.Payload.Item.CallId)
		}
	}
	completed := events[len(events)-1].Payload.Response
	require.NotNil(t, completed)
	require.Len(t, completed.Output, 1)
	assert.Equal(t, itemID, completed.Output[0].ID)
	assert.Equal(t, "call_real", completed.Output[0].CallId)
}

func TestChatCompletionsStreamToResponsesPreservesParallelToolOrder(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_parallel", "gpt-test")
	firstIndex := 0
	secondIndex := 1

	mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: &firstIndex,
				ID:    "call_first",
				Function: dto.FunctionResponse{
					Arguments: `{"value":`,
				},
			}}},
		}},
	})
	secondEvents := mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: &secondIndex,
				ID:    "call_second",
				Function: dto.FunctionResponse{
					Name:      "second_tool",
					Arguments: `{"value":2}`,
				},
			}}},
		}},
	})
	assert.Empty(t, secondEvents)

	finishReason := "tool_calls"
	events := mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: &firstIndex,
				Function: dto.FunctionResponse{
					Name:      "first_tool",
					Arguments: `1}`,
				},
			}}},
			FinishReason: &finishReason,
		}},
	})
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	added := make([]*dto.ResponsesOutput, 0, 2)
	for _, event := range events {
		if event.Type == responsesEventOutputItemAdded {
			added = append(added, event.Payload.Item)
		}
	}
	require.Len(t, added, 2)
	assert.Equal(t, "first_tool", added[0].Name)
	assert.Equal(t, "second_tool", added[1].Name)

	terminal := events[len(events)-1].Payload.Response
	require.NotNil(t, terminal)
	require.Len(t, terminal.Output, 2)
	assert.Equal(t, "first_tool", terminal.Output[0].Name)
	assert.Equal(t, "call_first", terminal.Output[0].CallId)
	assert.Equal(t, "second_tool", terminal.Output[1].Name)
	assert.Equal(t, "call_second", terminal.Output[1].CallId)
}

func TestChatCompletionsStreamToResponsesDropsUnnamedToolAndRenumbersSparseTool(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_sparse", "gpt-test")
	missingIndex := 0
	validIndex := 2
	finishReason := "tool_calls"
	events := mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &missingIndex, ID: "call_missing", Function: dto.FunctionResponse{Arguments: `{}`}},
				{Index: &validIndex, ID: "call_valid", Function: dto.FunctionResponse{Name: "read_file", Arguments: `{"path":"README.md"}`}},
			}},
			FinishReason: &finishReason,
		}},
	})
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	var addedEvent *ChatToResponsesStreamEvent
	for index := range events {
		if events[index].Type == responsesEventOutputItemAdded {
			addedEvent = &events[index]
			break
		}
	}
	require.NotNil(t, addedEvent)
	require.NotNil(t, addedEvent.Payload.OutputIndex)
	assert.Equal(t, 0, *addedEvent.Payload.OutputIndex)
	assert.Equal(t, "read_file", addedEvent.Payload.Item.Name)
	assert.Equal(t, "call_valid", addedEvent.Payload.Item.CallId)

	terminal := events[len(events)-1].Payload.Response
	require.NotNil(t, terminal)
	require.Len(t, terminal.Output, 1)
	assert.Equal(t, "read_file", terminal.Output[0].Name)
	assert.Equal(t, "call_valid", terminal.Output[0].CallId)
}

func TestChatCompletionsResponsesSynthesizesDuplicateToolCallIDs(t *testing.T) {
	message := dto.Message{Role: "assistant"}
	message.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_same", Type: "function", Function: dto.FunctionRequest{Name: "first", Arguments: `{}`}},
		{ID: "call_same", Type: "function", Function: dto.FunctionRequest{Name: "second", Arguments: `{}`}},
		{Type: "function", Function: dto.FunctionRequest{Name: "third", Arguments: `{}`}},
	})

	response, _, err := ChatCompletionsResponseToResponsesResponse(&dto.OpenAITextResponse{
		Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}},
	}, "resp_duplicate")
	require.NoError(t, err)
	require.Len(t, response.Output, 3)
	assert.Equal(t, "call_same", response.Output[0].CallId)
	assert.NotEmpty(t, response.Output[1].CallId)
	assert.NotEmpty(t, response.Output[2].CallId)
	assert.NotEqual(t, response.Output[0].CallId, response.Output[1].CallId)
	assert.NotEqual(t, response.Output[1].CallId, response.Output[2].CallId)
}

func TestFinalizeChatCompletionsStreamToResponsesCheckedMarksTextOnlyEOFIncomplete(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_truncated", "gpt-test")
	mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: kitutil.GetPointer("partial")},
		}},
	})

	events, err := FinalizeChatCompletionsStreamToResponsesChecked(state)
	require.NoError(t, err)
	require.NotEmpty(t, events)
	terminal := events[len(events)-1].Payload.Response
	require.NotNil(t, terminal)
	assert.Equal(t, `"incomplete"`, string(terminal.Status))
	require.NotNil(t, terminal.IncompleteDetails)
	assert.Equal(t, "max_output_tokens", terminal.IncompleteDetails.Reason)
}

func TestChatCompletionsStreamToResponsesKeepsClosedReasoningCompletedWhenResponseIsIncomplete(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_partial_after_reasoning", "gpt-test")
	mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: kitutil.GetPointer("finished thought")},
		}},
	})
	textEvents := mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: kitutil.GetPointer("partial answer")},
		}},
	})

	require.Len(t, textEvents, 6)
	assert.Equal(t, responsesEventReasoningSummaryDone, textEvents[0].Type)
	assert.Equal(t, responsesEventReasoningPartDone, textEvents[1].Type)
	assert.Equal(t, responsesEventOutputItemDone, textEvents[2].Type)
	require.NotNil(t, textEvents[2].Payload.Item)
	assert.Equal(t, "completed", textEvents[2].Payload.Item.Status)
	assert.Equal(t, responsesEventOutputItemAdded, textEvents[3].Type)

	terminalEvents, err := FinalizeChatCompletionsStreamToResponsesChecked(state)
	require.NoError(t, err)
	require.NotEmpty(t, terminalEvents)
	terminal := terminalEvents[len(terminalEvents)-1].Payload.Response
	require.NotNil(t, terminal)
	assert.Equal(t, `"incomplete"`, string(terminal.Status))
	require.Len(t, terminal.Output, 2)
	assert.Equal(t, responsesOutputTypeReasoning, terminal.Output[0].Type)
	assert.Equal(t, "completed", terminal.Output[0].Status)
	assert.Equal(t, responsesOutputTypeMessage, terminal.Output[1].Type)
	assert.Equal(t, "incomplete", terminal.Output[1].Status)
}

func TestFinalizeChatCompletionsStreamToResponsesCheckedRejectsOpenToolCall(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_truncated_tool", "gpt-test")
	toolIndex := 0
	mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: &toolIndex,
				ID:    "call_1",
				Type:  "function",
				Function: dto.FunctionResponse{
					Name:      "lookup",
					Arguments: `{"q":`,
				},
			}}},
		}},
	})

	_, err := FinalizeChatCompletionsStreamToResponsesChecked(state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool calls")
}

func mustResponsesEventsFromChatChunk(t *testing.T, state *ChatToResponsesStreamState, chunk *dto.ChatCompletionsStreamResponse) []ChatToResponsesStreamEvent {
	t.Helper()
	events, err := ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
	require.NoError(t, err)
	return events
}
