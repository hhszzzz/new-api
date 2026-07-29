package oairesponses

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestToClaudeMessagesDisablesThinkingAfterUnsignedToolResult(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Reasoning:       &dto.Reasoning{Effort: "medium"},
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "run it"},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{"q":"x"}`},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	assert.Nil(t, got.Thinking)
}

func TestOpenAIResponsesRequestToClaudeMessagesKeepsThinkingDisabledWhenUnsignedToolResultHasAdditionalUserText(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Reasoning:       &dto.Reasoning{Effort: "medium"},
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "run it"},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{"q":"x"}`},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
			{"role": "user", "content": "now explain"},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	assert.Nil(t, got.Thinking)
}

func TestOpenAIResponsesRequestToClaudeMessagesReenablesThinkingAfterCompletedToolRound(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Reasoning:       &dto.Reasoning{Effort: "medium"},
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "run it"},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{"q":"x"}`},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
			{"role": "assistant", "content": "finished"},
			{"role": "user", "content": "now explain"},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.NotNil(t, got.Thinking)
	assert.Equal(t, "enabled", got.Thinking.Type)
	assert.Equal(t, 2048, got.Thinking.GetBudgetTokens())
}

func TestOpenAIResponsesRequestToClaudeMessagesUsesCCSwitchThinkingBudgets(t *testing.T) {
	maxOutputTokens := uint(60000)
	tests := []struct {
		effort string
		want   int
	}{
		{effort: "minimal", want: 2048},
		{effort: "low", want: 2048},
		{effort: "medium", want: 8192},
		{effort: "high", want: 16384},
		{effort: "xhigh", want: 24576},
		{effort: "max", want: 24576},
	}

	for _, test := range tests {
		t.Run(test.effort, func(t *testing.T) {
			request := &dto.OpenAIResponsesRequest{
				Model:           "claude-test",
				MaxOutputTokens: &maxOutputTokens,
				Reasoning:       &dto.Reasoning{Effort: test.effort},
				Input:           mustRawMessage(t, "hello"),
			}

			got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
			require.NoError(t, err)
			require.NotNil(t, got.Thinking)
			assert.Equal(t, "enabled", got.Thinking.Type)
			assert.Equal(t, test.want, got.Thinking.GetBudgetTokens())
		})
	}
}

func TestOpenAIResponsesRequestToClaudeMessagesUsesAdaptiveThinking(t *testing.T) {
	maxOutputTokens := uint(4096)
	temperature := 0.4
	topP := 0.8
	for _, model := range []string{
		"claude-sonnet-4-6",
		"anthropic/claude-opus-4.8",
		"anthropic.claude_opus_4_7_20260701-v1:0",
	} {
		request := &dto.OpenAIResponsesRequest{
			Model:           model,
			MaxOutputTokens: &maxOutputTokens,
			Temperature:     &temperature,
			TopP:            &topP,
			Reasoning:       &dto.Reasoning{Effort: "xhigh"},
			Input:           mustRawMessage(t, "hello"),
		}

		got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
		require.NoError(t, err, "model=%s", model)
		require.NotNil(t, got.Thinking, "model=%s", model)
		assert.Equal(t, "adaptive", got.Thinking.Type, "model=%s", model)
		assert.Zero(t, got.Thinking.GetBudgetTokens(), "model=%s", model)
		assert.JSONEq(t, `{"effort":"max"}`, string(got.OutputConfig), "model=%s", model)
		assert.Nil(t, got.Temperature, "model=%s", model)
		assert.Nil(t, got.TopP, "model=%s", model)
	}
}

func TestOpenAIResponsesRequestToClaudeMessagesUsesDefaultAdaptiveThinking(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-sonnet-5",
		MaxOutputTokens: &maxOutputTokens,
		Input:           mustRawMessage(t, "hello"),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.NotNil(t, got.Thinking)
	assert.Equal(t, "adaptive", got.Thinking.Type)
	assert.Empty(t, got.OutputConfig)
}

func TestOpenAIResponsesRequestToClaudeMessagesUsesLowestEffortWhenThinkingCannotBeDisabled(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-fable-5",
		MaxOutputTokens: &maxOutputTokens,
		Reasoning:       &dto.Reasoning{Effort: "none"},
		Input:           mustRawMessage(t, "hello"),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.NotNil(t, got.Thinking)
	assert.Equal(t, "adaptive", got.Thinking.Type)
	assert.JSONEq(t, `{"effort":"low"}`, string(got.OutputConfig))
}

func TestOpenAIResponsesRequestToClaudeMessagesDisablesAdaptiveThinkingAfterUnsignedToolResult(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-opus-4-8",
		MaxOutputTokens: &maxOutputTokens,
		Reasoning:       &dto.Reasoning{Effort: "high"},
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "run it"},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{}`},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.NotNil(t, got.Thinking)
	assert.Equal(t, "disabled", got.Thinking.Type)
	assert.Empty(t, got.OutputConfig)

	request.Model = "claude-fable-5"
	_, err = OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.ErrorContains(t, err, "requires thinking")
}

func TestOpenAIResponsesRequestToClaudeMessagesDisablesAdaptiveThinkingForForcedTool(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-sonnet-5",
		MaxOutputTokens: &maxOutputTokens,
		Input:           mustRawMessage(t, "hello"),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
		ToolChoice: mustRawMessage(t, "required"),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.NotNil(t, got.Thinking)
	assert.Equal(t, "disabled", got.Thinking.Type)

	request.Model = "claude-fable-5"
	_, err = OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.ErrorContains(t, err, "cannot honor a forced tool_choice")
}

func TestOpenAIResponsesRequestToClaudeMessagesDropsIncompleteParallelToolTurn(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "run both"},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{}`},
			{"type": "function_call", "call_id": "call_2", "name": "lookup", "arguments": `{}`},
			{"type": "function_call_output", "call_id": "call_1", "output": "one"},
			{"role": "user", "content": "continue"},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	for _, message := range got.Messages {
		parts, parseErr := message.ParseContent()
		require.NoError(t, parseErr)
		for _, part := range parts {
			assert.NotEqual(t, "tool_use", part.Type)
			assert.NotEqual(t, "tool_result", part.Type)
		}
	}
	lastParts, err := got.Messages[0].ParseContent()
	require.NoError(t, err)
	require.Len(t, lastParts, 2)
	assert.Equal(t, "run both", lastParts[0].GetText())
	assert.Equal(t, "continue", lastParts[1].GetText())
}

func TestOpenAIResponsesRequestToClaudeMessagesOrdersToolResultsBeforeUserContent(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "run it"},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{}`},
			{"role": "user", "content": "then explain"},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	parts, err := got.Messages[2].ParseContent()
	require.NoError(t, err)
	require.Len(t, parts, 2)
	assert.Equal(t, "tool_result", parts[0].Type)
	assert.Equal(t, "call_1", parts[0].ToolUseId)
	assert.Equal(t, "text", parts[1].Type)
	assert.Equal(t, "then explain", parts[1].GetText())
}

func TestOpenAIResponsesRequestToClaudeMessagesDropsOrphanToolResults(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call_output", "call_id": "missing", "output": "ignored"},
			{"role": "user", "content": "hello"},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	parts, err := got.Messages[0].ParseContent()
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "hello", parts[0].GetText())

	request.Input = mustRawMessage(t, []map[string]any{
		{"type": "function_call_output", "call_id": "missing", "output": "ignored"},
	})
	_, err = OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.ErrorContains(t, err, "empty Messages input")
}

func TestOpenAIResponsesRequestToClaudeMessagesDropsEmptyTextAndTrimsAssistantPrefill(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": []map[string]any{
				{"type": "output_text", "text": ""},
				{"type": "output_text", "text": "done   \n"},
			}},
			{"role": "user", "content": "   "},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	parts, err := got.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "done", parts[0].GetText())
}

func TestOpenAIResponsesRequestToClaudeMessagesDropsIncompleteToolCallItem(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "keep me"},
			{"type": "function_call", "status": "incomplete", "call_id": "call_1", "name": "lookup", "arguments": `{`},
			{"type": "function_call_output", "call_id": "call_1", "output": "orphaned"},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	parts, err := got.Messages[0].ParseContent()
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "keep me", parts[0].GetText())
}

func TestOpenAIResponsesRequestToClaudeMessagesValidatesFunctionArguments(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": ""},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	parts, err := got.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Empty(t, parts[0].Input)

	for _, arguments := range []any{`{broken`, `[1,2]`, []any{1, 2}} {
		request.Input = mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": arguments},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		})
		_, err = OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
		require.ErrorContains(t, err, "arguments must be a JSON object")
	}
}

func TestOpenAIResponsesRequestToClaudeMessagesRejectsUnsupportedMedia(t *testing.T) {
	maxOutputTokens := uint(4096)
	tests := []struct {
		name    string
		content map[string]any
		want    string
	}{
		{
			name: "audio",
			content: map[string]any{
				"type":        "input_audio",
				"input_audio": map[string]any{"data": "abc", "format": "wav"},
			},
			want: `content type "input_audio"`,
		},
		{
			name: "video",
			content: map[string]any{
				"type":      "input_video",
				"video_url": "https://example.test/video.mp4",
			},
			want: `content type "input_video"`,
		},
		{
			name: "provider image ID",
			content: map[string]any{
				"type":    "input_image",
				"file_id": "file_provider_scoped",
			},
			want: "provider file IDs cannot be reused",
		},
		{
			name: "unknown content block",
			content: map[string]any{
				"type": "future_content",
			},
			want: `content type "future_content"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := &dto.OpenAIResponsesRequest{
				Model:           "claude-test",
				MaxOutputTokens: &maxOutputTokens,
				Input: mustRawMessage(t, []map[string]any{
					{
						"role":    "user",
						"content": []map[string]any{test.content},
					},
				}),
			}

			_, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestOpenAIResponsesRequestToClaudeMessagesPreservesStrictToolSchema(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input:           mustRawMessage(t, "hello"),
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type":       "function",
				"name":       "lookup",
				"strict":     true,
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	tools, ok := got.Tools.([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	tool, ok := tools[0].(*dto.Tool)
	require.True(t, ok)
	require.NotNil(t, tool.Strict)
	assert.True(t, *tool.Strict)
}

func TestOpenAIResponsesRequestToClaudeMessagesExtractsMCPToolImage(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "inspect", "arguments": `{}`},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output": []any{
					map[string]any{
						"type":     "image",
						"mimeType": "image/webp",
						"data":     "MCP_ANTHROPIC_IMAGE_SENTINEL",
					},
				},
			},
		}),
	}

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	content, err := got.Messages[2].ParseContent()
	require.NoError(t, err)
	require.Len(t, content, 1)
	assert.Equal(t, "tool_result", content[0].Type)
	toolOutput, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](content[0].Content)
	require.NoError(t, err)
	require.Len(t, toolOutput, 2)
	assert.Equal(t, "text", toolOutput[0].Type)
	assert.Contains(t, toolOutput[0].GetText(), "tool result media attached")
	assert.NotContains(t, toolOutput[0].GetText(), "MCP_ANTHROPIC_IMAGE_SENTINEL")
	assert.Equal(t, "image", toolOutput[1].Type)
	require.NotNil(t, toolOutput[1].Source)
	assert.Equal(t, "image/webp", toolOutput[1].Source.MediaType)
	assert.Equal(t, "MCP_ANTHROPIC_IMAGE_SENTINEL", toolOutput[1].Source.Data)
}
