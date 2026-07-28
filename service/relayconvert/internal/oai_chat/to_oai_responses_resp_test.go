package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
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

	encoded, err := common.Marshal(resp)
	require.NoError(t, err)
	assert.Equal(t, "reasoning", gjson.GetBytes(encoded, "output.0.type").String())
	assert.Equal(t, "summary_text", gjson.GetBytes(encoded, "output.0.summary.0.type").String())
	assert.Equal(t, reasoning, gjson.GetBytes(encoded, "output.0.summary.0.text").String())
	assert.False(t, gjson.GetBytes(encoded, "output.0.content").Exists())
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
	assert.Equal(t, "resp_reasoning_reasoning_0", addedItem.ID)
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

func mustResponsesEventsFromChatChunk(t *testing.T, state *ChatToResponsesStreamState, chunk *dto.ChatCompletionsStreamResponse) []ChatToResponsesStreamEvent {
	t.Helper()
	events, err := ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
	require.NoError(t, err)
	return events
}
