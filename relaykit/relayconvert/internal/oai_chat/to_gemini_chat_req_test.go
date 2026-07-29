package oaichat

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToGeminiPreservesToolCallIDs(t *testing.T) {
	assistant := dto.Message{Role: "assistant", Content: ""}
	assistant.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_lookup",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{"q":"x"}`,
		},
	}})
	tool := dto.Message{Role: "tool", ToolCallId: "call_lookup"}
	tool.SetStringContent(`{"result":"ok"}`)

	converted, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{
		Model:    "gemini-test",
		Messages: []dto.Message{assistant, tool},
	}, &convmeta.Values{UpstreamModelName: "gemini-test"})
	require.NoError(t, err)
	require.Len(t, converted.Contents, 2)
	require.Len(t, converted.Contents[0].Parts, 1)
	require.NotNil(t, converted.Contents[0].Parts[0].FunctionCall)
	assert.Equal(t, "call_lookup", converted.Contents[0].Parts[0].FunctionCall.ID)
	require.Len(t, converted.Contents[1].Parts, 1)
	require.NotNil(t, converted.Contents[1].Parts[0].FunctionResponse)
	assert.Equal(t, "lookup", converted.Contents[1].Parts[0].FunctionResponse.Name)
	assert.Equal(t, "call_lookup", kitutil.JsonRawMessageToString(converted.Contents[1].Parts[0].FunctionResponse.ID))
}

func TestOpenAIChatRequestToGeminiRejectsToolResultWithoutResolvableName(t *testing.T) {
	tool := dto.Message{Role: "tool", ToolCallId: "call_missing"}
	tool.SetStringContent("ok")

	_, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{
		Model:    "gemini-test",
		Messages: []dto.Message{tool},
	}, &convmeta.Values{UpstreamModelName: "gemini-test"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unable to resolve Gemini functionResponse.name for tool_call_id "call_missing"`)
}

func TestOpenAIChatRequestToGeminiStripsSynthesizedToolCallIDs(t *testing.T) {
	synthesizedID := sharedgemini.SynthesizeToolCallID()
	assistant := dto.Message{Role: "assistant", Content: ""}
	assistant.SetToolCalls([]dto.ToolCallRequest{{
		ID:   synthesizedID,
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{}`,
		},
	}})
	tool := dto.Message{Role: "tool", ToolCallId: synthesizedID}
	tool.SetStringContent("ok")

	converted, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{
		Model:    "gemini-test",
		Messages: []dto.Message{assistant, tool},
	}, &convmeta.Values{UpstreamModelName: "gemini-test"})

	require.NoError(t, err)
	require.Len(t, converted.Contents, 2)
	require.Len(t, converted.Contents[0].Parts, 1)
	require.NotNil(t, converted.Contents[0].Parts[0].FunctionCall)
	assert.Empty(t, converted.Contents[0].Parts[0].FunctionCall.ID)
	require.Len(t, converted.Contents[1].Parts, 1)
	require.NotNil(t, converted.Contents[1].Parts[0].FunctionResponse)
	assert.Empty(t, converted.Contents[1].Parts[0].FunctionResponse.ID)
	assert.Equal(t, "lookup", converted.Contents[1].Parts[0].FunctionResponse.Name)
}

func TestOpenAIChatRequestToGeminiPreservesExplicitZeroScalars(t *testing.T) {
	topP := 0.0
	maxTokens := uint(0)
	seed := 0.0

	converted, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{
		Model:     "gemini-test",
		TopP:      &topP,
		MaxTokens: &maxTokens,
		Seed:      &seed,
		Messages:  []dto.Message{{Role: "user", Content: "hello"}},
	}, &convmeta.Values{UpstreamModelName: "gemini-test"})
	require.NoError(t, err)

	require.NotNil(t, converted.GenerationConfig.TopP)
	assert.Zero(t, *converted.GenerationConfig.TopP)
	require.NotNil(t, converted.GenerationConfig.MaxOutputTokens)
	assert.Zero(t, *converted.GenerationConfig.MaxOutputTokens)
	require.NotNil(t, converted.GenerationConfig.Seed)
	assert.Zero(t, *converted.GenerationConfig.Seed)
}
