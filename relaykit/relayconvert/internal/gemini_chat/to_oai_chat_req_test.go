package geminichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiGenerateContentRequestToOpenAIChatPreservesToolCallIDs(t *testing.T) {
	responseID, err := kitutil.Marshal("call_lookup")
	require.NoError(t, err)
	converted, err := GeminiGenerateContentRequestToOpenAIChat(&dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role: "model",
				Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{
					ID:           "call_lookup",
					FunctionName: "lookup",
					Arguments:    map[string]any{"q": "x"},
				}}},
			},
			{
				Role: "user",
				Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{
					ID:       responseID,
					Name:     "lookup",
					Response: map[string]any{"result": "ok"},
				}}},
			},
		},
	}, &convmeta.Values{UpstreamModelName: "chat-test"})
	require.NoError(t, err)
	require.Len(t, converted.Messages, 2)
	toolCalls := converted.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_lookup", toolCalls[0].ID)
	assert.Equal(t, "call_lookup", converted.Messages[1].ToolCallId)
	require.NotNil(t, converted.Messages[1].Name)
	assert.Equal(t, "lookup", *converted.Messages[1].Name)
}

func TestGeminiGenerateContentRequestToOpenAIChatPairsParallelMissingIDsByName(t *testing.T) {
	converted, err := GeminiGenerateContentRequestToOpenAIChat(&dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role: "model",
				Parts: []dto.GeminiPart{
					{FunctionCall: &dto.FunctionCall{FunctionName: "lookup", Arguments: map[string]any{"q": "a"}}},
					{FunctionCall: &dto.FunctionCall{FunctionName: "lookup", Arguments: map[string]any{"q": "b"}}},
				},
			},
			{
				Role: "user",
				Parts: []dto.GeminiPart{
					{FunctionResponse: &dto.GeminiFunctionResponse{Name: "lookup", Response: map[string]any{"result": "a"}}},
					{FunctionResponse: &dto.GeminiFunctionResponse{Name: "lookup", Response: map[string]any{"result": "b"}}},
				},
			},
		},
	}, &convmeta.Values{UpstreamModelName: "chat-test"})

	require.NoError(t, err)
	require.Len(t, converted.Messages, 3)
	toolCalls := converted.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 2)
	assert.Equal(t, "call_1", toolCalls[0].ID)
	assert.Equal(t, "call_2", toolCalls[1].ID)
	assert.Equal(t, "call_1", converted.Messages[1].ToolCallId)
	assert.Equal(t, "call_2", converted.Messages[2].ToolCallId)
}

func TestGeminiGenerateContentRequestToOpenAIChatRejectsUnmatchedToolResult(t *testing.T) {
	_, err := GeminiGenerateContentRequestToOpenAIChat(&dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role: "user",
			Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{
				Name:     "missing",
				Response: map[string]any{"result": "x"},
			}}},
		}},
	}, &convmeta.Values{UpstreamModelName: "chat-test"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unable to resolve OpenAI tool_call_id for Gemini functionResponse "missing"`)
}

func TestGeminiGenerateContentRequestToOpenAIChatPreservesZeroScalarsAndDropsTopK(t *testing.T) {
	topP := 0.0
	topK := 0.0
	maxOutputTokens := uint(0)
	candidateCount := 0
	converted, err := GeminiGenerateContentRequestToOpenAIChat(&dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}},
		GenerationConfig: dto.GeminiChatGenerationConfig{
			TopP:            &topP,
			TopK:            &topK,
			MaxOutputTokens: &maxOutputTokens,
			CandidateCount:  &candidateCount,
		},
	}, &convmeta.Values{UpstreamModelName: "chat-test"})

	require.NoError(t, err)
	require.NotNil(t, converted.TopP)
	assert.Zero(t, *converted.TopP)
	assert.Nil(t, converted.TopK)
	require.NotNil(t, converted.MaxTokens)
	assert.Zero(t, *converted.MaxTokens)
	require.NotNil(t, converted.N)
	assert.Zero(t, *converted.N)
}

func TestGeminiGenerateContentRequestToOpenAIChatRestoresParametersJSONSchema(t *testing.T) {
	converted, err := GeminiGenerateContentRequestToOpenAIChat(&dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}},
		Tools: mustGeminiRawMessage(t, []dto.GeminiChatTool{{
			FunctionDeclarations: []dto.FunctionRequest{{
				Name: "lookup",
				ParametersJsonSchema: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
				},
			}},
		}}),
	}, &convmeta.Values{UpstreamModelName: "chat-test"})

	require.NoError(t, err)
	require.Len(t, converted.Tools, 1)
	parameters, ok := converted.Tools[0].Function.Parameters.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", parameters["type"])
	assert.Equal(t, false, parameters["additionalProperties"])
}

func mustGeminiRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return raw
}
