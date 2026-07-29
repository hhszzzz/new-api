package oaichat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
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

func TestChatCompletionsRequestToResponsesRequestPreservesQwenThinkingBudget(t *testing.T) {
	tests := []struct {
		name   string
		budget json.RawMessage
		want   int64
	}{
		{name: "positive budget", budget: json.RawMessage(`128`), want: 128},
		{name: "zero budget", budget: json.RawMessage(`0`), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{
				Model:          "qwen-plus",
				EnableThinking: json.RawMessage(`true`),
				ThinkingBudget: tt.budget,
				Messages: []dto.Message{
					{Role: "user", Content: "hello"},
				},
			}

			got, err := ChatCompletionsRequestToResponsesRequest(req)
			require.NoError(t, err)
			assert.Equal(t, tt.budget, got.ThinkingBudget)

			encoded, err := kitutil.Marshal(got)
			require.NoError(t, err)

			assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
			value := gjson.GetBytes(encoded, "thinking_budget")
			assert.True(t, value.Exists())
			assert.Equal(t, tt.want, value.Int())
		})
	}
}

func TestChatCompletionsRequestToResponsesRequestRejectsMultipleChoices(t *testing.T) {
	_, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(2),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n>1")
}

func TestChatCompletionsRequestToResponsesRequestDropsUntrustedAssistantReasoning(t *testing.T) {
	reasoning := "inspect the inputs"
	assistant := assistantMessageWithTool("I will call lookup.", "call_1", "lookup", `{"q":"x"}`)
	assistant.ReasoningContent = &reasoning

	got, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model:    "gpt-test",
		Messages: []dto.Message{assistant},
	})
	require.NoError(t, err)

	assert.Equal(t, "assistant", gjson.GetBytes(got.Input, "0.role").String())
	assert.Equal(t, "I will call lookup.", gjson.GetBytes(got.Input, "0.content").String())
	assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "1.type").String())
	assert.False(t, gjson.GetBytes(got.Input, `#(type=="reasoning")`).Exists())
	assert.Equal(t, "lookup", gjson.GetBytes(got.Tools, "0.name").String())
	assert.Empty(t, got.ToolChoice)
	assert.Empty(t, got.ParallelToolCalls)
}

func TestChatCompletionsRequestToResponsesRequestLinksLegacyFunctionResult(t *testing.T) {
	var req dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-test",
		"messages":[
			{"role":"assistant","content":null,"function_call":{"name":"lookup","arguments":"{\"q\":\"x\"}"}},
			{"role":"function","name":"lookup","content":"found"}
		]
	}`), &req))

	got, err := ChatCompletionsRequestToResponsesRequest(&req)
	require.NoError(t, err)
	assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "1.type").String())
	assert.Equal(t, "call_0", gjson.GetBytes(got.Input, "1.call_id").String())
	assert.Equal(t, "lookup", gjson.GetBytes(got.Input, "1.name").String())
	assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "2.type").String())
	assert.Equal(t, "call_0", gjson.GetBytes(got.Input, "2.call_id").String())
	assert.Equal(t, "found", gjson.GetBytes(got.Input, "2.output").String())
}

func TestChatCompletionsRequestToResponsesRequestRejectsUnmatchedLegacyFunctionResult(t *testing.T) {
	_, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{{
			Role:    "function",
			Content: "orphaned result",
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing tool_call_id")
}

func TestChatCompletionsRequestToResponsesRequestPreservesSystemWhitespace(t *testing.T) {
	got, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "system", Content: "  first  "},
			{Role: "developer", Content: "\tsecond\n"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "  first  \n\n\tsecond\n", gjson.ParseBytes(got.Instructions).String())
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
