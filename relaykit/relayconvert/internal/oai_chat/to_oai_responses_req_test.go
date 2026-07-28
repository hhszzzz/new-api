package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatCompletionsRequestToResponsesRequestInstructionsAndTools(t *testing.T) {
	strict := true
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(1),
		Messages: []dto.Message{
			{Role: "system", Content: "system rules"},
			{Role: "developer", Content: "developer rules"},
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "look"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/a.png"}},
			}},
			assistantMessageWithTool("partial text", "call_1", "lookup", `{"q":"x"}`),
			{Role: "tool", ToolCallId: "call_1", Content: "tool result"},
		},
		Tools: []dto.ToolCallRequest{
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:       "lookup",
					Parameters: map[string]any{"type": "object"},
					Strict:     &strict,
				},
			},
		},
	}

	got, err := ChatCompletionsRequestToResponsesRequest(req)
	require.NoError(t, err)

	assert.Equal(t, "gpt-test", got.Model)
	assert.Equal(t, `"system rules\n\ndeveloper rules"`, string(got.Instructions))
	assert.Equal(t, "input_image", gjson.GetBytes(got.Input, "0.content.1.type").String())
	assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "2.type").String())
	assert.Equal(t, "call_1", gjson.GetBytes(got.Input, "2.call_id").String())
	assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "3.type").String())
	assert.True(t, gjson.GetBytes(got.Tools, "0.strict").Bool())
}

func TestChatCompletionsRequestToResponsesRequestRejectsMultipleChoices(t *testing.T) {
	_, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(2),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n>1")
}

func TestChatCompletionsRequestToResponsesRequestPreservesAssistantReasoning(t *testing.T) {
	reasoning := "inspect the inputs"
	assistant := assistantMessageWithTool("I will call lookup.", "call_1", "lookup", `{"q":"x"}`)
	assistant.ReasoningContent = &reasoning

	got, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model:    "gpt-test",
		Messages: []dto.Message{assistant},
	})
	require.NoError(t, err)

	assert.Equal(t, "reasoning", gjson.GetBytes(got.Input, "0.type").String())
	assert.Equal(t, "rs_bridge_0", gjson.GetBytes(got.Input, "0.id").String())
	assert.Equal(t, "completed", gjson.GetBytes(got.Input, "0.status").String())
	assert.Equal(t, "summary_text", gjson.GetBytes(got.Input, "0.summary.0.type").String())
	assert.Equal(t, reasoning, gjson.GetBytes(got.Input, "0.summary.0.text").String())
	assert.Equal(t, "reasoning_text", gjson.GetBytes(got.Input, "0.content.0.type").String())
	assert.Equal(t, reasoning, gjson.GetBytes(got.Input, "0.content.0.text").String())
	assert.False(t, gjson.GetBytes(got.Input, "0.encrypted_content").Exists())
	assert.Equal(t, "assistant", gjson.GetBytes(got.Input, "1.role").String())
	assert.Equal(t, "I will call lookup.", gjson.GetBytes(got.Input, "1.content").String())
	assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "2.type").String())
}

func assistantMessageWithTool(content string, id string, name string, args string) dto.Message {
	msg := dto.Message{Role: "assistant", Content: content}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   id,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      name,
				Arguments: args,
			},
		},
	})
	return msg
}
