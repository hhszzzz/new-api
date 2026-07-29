package oairesponses

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestToGeminiPreservesToolCallIDs(t *testing.T) {
	converted, err := OpenAIResponsesRequestToGeminiChat(context.Background(), &dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: []byte(`[
			{"type":"function_call","call_id":"call_lookup","name":"lookup","arguments":"{\"q\":\"x\"}"},
			{"type":"function_call_output","call_id":"call_lookup","output":{"result":"ok"}}
		]`),
	}, &convmeta.Values{UpstreamModelName: "gemini-test"})
	require.NoError(t, err)
	require.Len(t, converted.Contents, 2)
	require.NotNil(t, converted.Contents[0].Parts[0].FunctionCall)
	assert.Equal(t, "call_lookup", converted.Contents[0].Parts[0].FunctionCall.ID)
	require.NotNil(t, converted.Contents[1].Parts[0].FunctionResponse)
	assert.Equal(t, "call_lookup", kitutil.JsonRawMessageToString(converted.Contents[1].Parts[0].FunctionResponse.ID))
}

func TestOpenAIResponsesRequestToGeminiPreservesExplicitZeroScalars(t *testing.T) {
	topP := 0.0
	maxOutputTokens := uint(0)
	converted, err := OpenAIResponsesRequestToGeminiChat(context.Background(), &dto.OpenAIResponsesRequest{
		Model:           "gemini-test",
		Input:           []byte(`"hello"`),
		TopP:            &topP,
		MaxOutputTokens: &maxOutputTokens,
	}, &convmeta.Values{UpstreamModelName: "gemini-test"})
	require.NoError(t, err)
	require.NotNil(t, converted.GenerationConfig.TopP)
	assert.Zero(t, *converted.GenerationConfig.TopP)
	require.NotNil(t, converted.GenerationConfig.MaxOutputTokens)
	assert.Zero(t, *converted.GenerationConfig.MaxOutputTokens)
}

func TestOpenAIResponsesRequestToGeminiStripsSynthesizedToolCallIDs(t *testing.T) {
	synthesizedID := "gemini_synth_0123456789abcdef0123456789abcdef"
	converted, err := OpenAIResponsesRequestToGeminiChat(context.Background(), &dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": synthesizedID, "name": "lookup", "arguments": `{}`},
			{"type": "function_call_output", "call_id": synthesizedID, "output": "done"},
		}),
	}, &convmeta.Values{UpstreamModelName: "gemini-test"})
	require.NoError(t, err)
	require.Len(t, converted.Contents, 2)
	assert.Empty(t, converted.Contents[0].Parts[0].FunctionCall.ID)
	assert.Empty(t, converted.Contents[1].Parts[0].FunctionResponse.ID)
}

func TestOpenAIResponsesRequestToGeminiSeparatesSystemInstructions(t *testing.T) {
	converted, err := OpenAIResponsesRequestToGeminiChat(context.Background(), &dto.OpenAIResponsesRequest{
		Model:        "gemini-test",
		Instructions: mustRawMessage(t, "first"),
		Input: mustRawMessage(t, []map[string]any{
			{"role": "developer", "content": "second"},
			{"role": "user", "content": "hello"},
		}),
	}, &convmeta.Values{UpstreamModelName: "gemini-test"})
	require.NoError(t, err)
	require.NotNil(t, converted.SystemInstructions)
	require.Len(t, converted.SystemInstructions.Parts, 1)
	assert.Equal(t, "first\n\nsecond", converted.SystemInstructions.Parts[0].Text)
}

func TestOpenAIResponsesRequestToGemini3KeepsToolImageInFunctionResponseParts(t *testing.T) {
	converted, err := OpenAIResponsesRequestToGeminiChat(context.Background(), &dto.OpenAIResponsesRequest{
		Model: "gemini-3.6-flash",
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_image", "name": "inspect", "arguments": `{}`},
			{
				"type":    "function_call_output",
				"call_id": "call_image",
				"output": []any{
					map[string]any{"type": "text", "text": "caption"},
					map[string]any{
						"type":     "image",
						"mimeType": "image/jpeg",
						"data":     "GEMINI_3_IMAGE_SENTINEL",
					},
				},
			},
		}),
	}, &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-3.6-flash"})
	require.NoError(t, err)
	require.Len(t, converted.Contents, 2)
	require.Len(t, converted.Contents[1].Parts, 1)
	functionResponse := converted.Contents[1].Parts[0].FunctionResponse
	require.NotNil(t, functionResponse)
	assert.Contains(t, functionResponse.Response["content"], "caption")
	assert.Contains(t, functionResponse.Response["content"], "tool result media attached")
	assert.NotContains(t, functionResponse.Response["content"], "GEMINI_3_IMAGE_SENTINEL")
	var mediaParts []dto.GeminiPart
	require.NoError(t, kitutil.Unmarshal(functionResponse.Parts, &mediaParts))
	require.Len(t, mediaParts, 1)
	require.NotNil(t, mediaParts[0].InlineData)
	assert.Equal(t, "image/jpeg", mediaParts[0].InlineData.MimeType)
	assert.Equal(t, "GEMINI_3_IMAGE_SENTINEL", mediaParts[0].InlineData.Data)
}
