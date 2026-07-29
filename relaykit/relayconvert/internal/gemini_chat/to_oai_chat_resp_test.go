package geminichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseGeminiChat2OpenAISeparatesReasoningAndVisibleContent(t *testing.T) {
	response := ResponseGeminiChat2OpenAI("chatcmpl_test", 1, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
				{Thought: true, Text: "inspect"},
				{Thought: true, Text: "verify"},
				{Text: "answer"},
			}},
		}},
	})

	require.Len(t, response.Choices, 1)
	assert.Equal(t, "inspect\nverify", response.Choices[0].Message.GetReasoningContent())
	assert.Equal(t, "answer", response.Choices[0].Message.StringContent())
}

func TestResponseGeminiChat2OpenAIToolFinishDoesNotLeakAcrossCandidates(t *testing.T) {
	stop := "STOP"
	response := ResponseGeminiChat2OpenAI("chatcmpl_test", 1, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Index: 0,
				Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{
					FunctionCall: &dto.FunctionCall{ID: "call_lookup", FunctionName: "lookup", Arguments: map[string]any{"q": "x"}},
				}}},
			},
			{
				Index:        1,
				FinishReason: &stop,
				Content:      dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "done"}}},
			},
		},
	})

	require.Len(t, response.Choices, 2)
	assert.Equal(t, types.FinishReasonToolCalls, response.Choices[0].FinishReason)
	assert.Equal(t, types.FinishReasonStop, response.Choices[1].FinishReason)
}

func TestResponseGeminiChat2OpenAIMarksMissingToolCallIDAsSynthesized(t *testing.T) {
	response := ResponseGeminiChat2OpenAI("chatcmpl_test", 1, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{
				FunctionCall: &dto.FunctionCall{FunctionName: "lookup", Arguments: map[string]any{}},
			}}},
		}},
	})

	require.Len(t, response.Choices, 1)
	calls := response.Choices[0].Message.ParseToolCalls()
	require.Len(t, calls, 1)
	assert.True(t, sharedgemini.IsSynthesizedToolCallID(calls[0].ID))
}

func TestStreamResponseGeminiChat2OpenAIKeepsReasoningContentAndRefusalSemantics(t *testing.T) {
	t.Run("mixed reasoning and content", func(t *testing.T) {
		response, _ := StreamResponseGeminiChat2OpenAI(&dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{{
				Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
					{Thought: true, Text: "inspect"},
					{Text: "answer"},
				}},
			}},
		})

		require.Len(t, response.Choices, 1)
		assert.Equal(t, "inspect", response.Choices[0].Delta.GetReasoningContent())
		assert.Equal(t, "answer", response.Choices[0].Delta.GetContentString())
	})

	t.Run("safety refusal", func(t *testing.T) {
		safety := "SAFETY"
		response, _ := StreamResponseGeminiChat2OpenAI(&dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{{FinishReason: &safety}},
		})

		require.Len(t, response.Choices, 1)
		require.NotNil(t, response.Choices[0].FinishReason)
		assert.Equal(t, types.FinishReasonContentFilter, *response.Choices[0].FinishReason)
		assert.Contains(t, response.Choices[0].Delta.GetRefusalContent(), "safety")
	})
}

func TestGeminiToChatStreamStateKeepsToolCallIDStable(t *testing.T) {
	state := NewGeminiToChatStreamState("chatcmpl_test", 1)
	chunk := func(id string) *dto.GeminiChatResponse {
		return &dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{{
				Index: 0,
				Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{
					FunctionCall: &dto.FunctionCall{ID: id, FunctionName: "lookup", Arguments: map[string]any{"q": "x"}},
				}}},
			}},
		}
	}

	first := state.ConvertChunk(chunk(""), "gemini-test", nil)
	second := state.ConvertChunk(chunk(""), "gemini-test", nil)
	require.Len(t, first, 1)
	require.Len(t, second, 1)
	require.Len(t, first[0].Choices[0].Delta.ToolCalls, 1)
	require.Len(t, second[0].Choices[0].Delta.ToolCalls, 1)
	firstCall := first[0].Choices[0].Delta.ToolCalls[0]
	secondCall := second[0].Choices[0].Delta.ToolCalls[0]
	assert.NotEmpty(t, firstCall.ID)
	assert.Equal(t, firstCall.ID, secondCall.ID)
	assert.JSONEq(t, `{"q":"x"}`, firstCall.Function.Arguments)
	assert.Empty(t, secondCall.Function.Arguments)
	assert.Empty(t, secondCall.Function.Name)
	require.NotNil(t, firstCall.Index)
	require.NotNil(t, secondCall.Index)
	assert.Equal(t, *firstCall.Index, *secondCall.Index)
	assert.True(t, sharedgemini.IsSynthesizedToolCallID(firstCall.ID))

	explicit := NewGeminiToChatStreamState("chatcmpl_explicit", 1).ConvertChunk(chunk("provider_call_1"), "gemini-test", nil)
	require.Len(t, explicit, 1)
	assert.Equal(t, "provider_call_1", explicit[0].Choices[0].Delta.ToolCalls[0].ID)
}

func TestGeminiToChatStreamStateConvertsCumulativeSnapshotsToDeltas(t *testing.T) {
	state := NewGeminiToChatStreamState("chatcmpl_test", 1)
	chunk := func(text string, reasoning string) *dto.GeminiChatResponse {
		parts := make([]dto.GeminiPart, 0, 2)
		if reasoning != "" {
			parts = append(parts, dto.GeminiPart{Thought: true, Text: reasoning})
		}
		if text != "" {
			parts = append(parts, dto.GeminiPart{Text: text})
		}
		return &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{
			Index:   0,
			Content: dto.GeminiChatContent{Parts: parts},
		}}}
	}

	first := state.ConvertChunk(chunk("Hel", "think"), "gemini-test", nil)
	second := state.ConvertChunk(chunk("Hello", "thinking"), "gemini-test", nil)
	third := state.ConvertChunk(chunk("Hello", "thinking"), "gemini-test", nil)

	require.Len(t, first, 1)
	require.Len(t, second, 1)
	require.Len(t, third, 1)
	assert.Equal(t, "Hel", first[0].Choices[0].Delta.GetContentString())
	assert.Equal(t, "think", first[0].Choices[0].Delta.GetReasoningContent())
	assert.Equal(t, "lo", second[0].Choices[0].Delta.GetContentString())
	assert.Equal(t, "ing", second[0].Choices[0].Delta.GetReasoningContent())
	assert.Nil(t, third[0].Choices[0].Delta.Content)
	assert.Nil(t, third[0].Choices[0].Delta.ReasoningContent)
}

func TestResponseGeminiChat2OpenAIMapsSafetyToRefusal(t *testing.T) {
	safety := "SAFETY"
	response := ResponseGeminiChat2OpenAI("chatcmpl_test", 1, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{FinishReason: &safety}},
	})

	require.Len(t, response.Choices, 1)
	assert.Equal(t, types.FinishReasonContentFilter, response.Choices[0].FinishReason)
	assert.Contains(t, response.Choices[0].Message.GetRefusalContent(), "safety")
}

func TestResponseGeminiChat2OpenAIMapsBlockedPromptToRefusal(t *testing.T) {
	blockReason := "SAFETY"
	response := ResponseGeminiChat2OpenAI("chatcmpl_test", 1, &dto.GeminiChatResponse{
		PromptFeedback: &dto.GeminiChatPromptFeedback{BlockReason: &blockReason},
	})

	require.Len(t, response.Choices, 1)
	assert.Equal(t, types.FinishReasonContentFilter, response.Choices[0].FinishReason)
	assert.Equal(t, "Request blocked by Gemini safety filters: SAFETY", response.Choices[0].Message.GetRefusalContent())
	assert.Empty(t, response.Choices[0].Message.StringContent())
}

func TestStreamResponseGeminiChat2OpenAIMapsBlockedPromptToRefusal(t *testing.T) {
	blockReason := "BLOCKLIST"
	response, isStop := StreamResponseGeminiChat2OpenAI(&dto.GeminiChatResponse{
		PromptFeedback: &dto.GeminiChatPromptFeedback{BlockReason: &blockReason},
	})

	assert.False(t, isStop)
	require.Len(t, response.Choices, 1)
	require.NotNil(t, response.Choices[0].FinishReason)
	assert.Equal(t, types.FinishReasonContentFilter, *response.Choices[0].FinishReason)
	assert.Equal(t, "Request blocked by Gemini safety filters: BLOCKLIST", response.Choices[0].Delta.GetRefusalContent())
}
