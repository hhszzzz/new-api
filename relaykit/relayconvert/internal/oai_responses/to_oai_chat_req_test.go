package oairesponses

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesRequestToChatCompletionsRequestInstructionsAndScalarInput(t *testing.T) {
	stream := true
	temperature := 0.0
	topP := 0.9
	maxOutputTokens := uint(128)
	parallelToolCalls := true

	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:                "gpt-test",
		Instructions:         mustRawMessage(t, "system rules"),
		Input:                mustRawMessage(t, "hello"),
		Stream:               &stream,
		StreamOptions:        &dto.StreamOptions{IncludeUsage: true},
		MaxOutputTokens:      &maxOutputTokens,
		Temperature:          &temperature,
		TopP:                 &topP,
		User:                 mustRawMessage(t, "user-1"),
		Store:                mustRawMessage(t, false),
		Metadata:             mustRawMessage(t, map[string]any{"trace": "abc"}),
		ParallelToolCalls:    mustRawMessage(t, parallelToolCalls),
		PromptCacheKey:       mustRawMessage(t, "cache-key"),
		PromptCacheRetention: mustRawMessage(t, "24h"),
		Reasoning:            &dto.Reasoning{Effort: "medium"},
	})
	require.NoError(t, err)

	assert.Equal(t, "gpt-test", got.Model)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, dto.Message{Role: "system", Content: "system rules"}, got.Messages[0])
	assert.Equal(t, dto.Message{Role: "user", Content: "hello"}, got.Messages[1])
	assert.Same(t, &stream, got.Stream)
	require.NotNil(t, got.StreamOptions)
	assert.True(t, got.StreamOptions.IncludeUsage)
	assert.Equal(t, maxOutputTokens, lo.FromPtr(got.MaxTokens))
	assert.Nil(t, got.MaxCompletionTokens)
	assert.Equal(t, 0.0, lo.FromPtr(got.Temperature))
	assert.Equal(t, 0.9, lo.FromPtr(got.TopP))
	assert.Nil(t, got.ParallelTooCalls)
	assert.Empty(t, got.PromptCacheKey)
	assert.Empty(t, got.ReasoningEffort)
	assert.Equal(t, `"user-1"`, string(got.User))
	assert.Equal(t, "false", string(got.Store))
	assert.Equal(t, `"24h"`, string(got.PromptCacheRetention))
	assert.Equal(t, "abc", gjson.GetBytes(got.Metadata, "trace").String())
}

func TestResponsesRequestToChatCompletionsRequestPreservesQwenThinkingBudget(t *testing.T) {
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
			got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
				Model:          "qwen-plus",
				Input:          mustRawMessage(t, "hello"),
				EnableThinking: json.RawMessage(`true`),
				ThinkingBudget: tt.budget,
			})
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

func TestResponsesRequestToChatCompletionsRequestUsesModelCompatibleTokenAndReasoningFields(t *testing.T) {
	maxOutputTokens := uint(256)
	tests := []struct {
		name                    string
		model                   string
		wantMaxTokens           bool
		wantMaxCompletionTokens bool
		wantReasoningEffort     string
	}{
		{name: "ordinary chat model", model: "openpangu-2.0-flash", wantMaxTokens: true},
		{name: "gpt five", model: "gpt-5.4", wantMaxTokens: true, wantReasoningEffort: "high"},
		{name: "o series", model: "o3-mini", wantMaxCompletionTokens: true, wantReasoningEffort: "high"},
		{name: "grok build", model: "grok-4.5-fast", wantMaxTokens: true, wantReasoningEffort: "high"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
				Model:           test.model,
				Input:           mustRawMessage(t, "hello"),
				MaxOutputTokens: &maxOutputTokens,
				Reasoning:       &dto.Reasoning{Effort: "high"},
			})
			require.NoError(t, err)

			if test.wantMaxTokens {
				require.NotNil(t, got.MaxTokens)
				assert.Equal(t, maxOutputTokens, *got.MaxTokens)
			} else {
				assert.Nil(t, got.MaxTokens)
			}
			if test.wantMaxCompletionTokens {
				require.NotNil(t, got.MaxCompletionTokens)
				assert.Equal(t, maxOutputTokens, *got.MaxCompletionTokens)
			} else {
				assert.Nil(t, got.MaxCompletionTokens)
			}
			assert.Equal(t, test.wantReasoningEffort, got.ReasoningEffort)
		})
	}
}

func TestResponsesRequestToChatCompletionsRequestMultimodalInput(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "look"},
					{"type": "input_image", "image_url": "https://example.test/a.png", "detail": "low"},
					{"type": "input_file", "file_id": "file_1", "filename": "a.txt"},
					{"type": "input_audio", "input_audio": map[string]any{"data": "abc", "format": "wav"}},
					{"type": "input_video", "video_url": map[string]any{"url": "https://example.test/v.mp4"}},
				},
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 1)
	assert.Equal(t, "user", got.Messages[0].Role)
	parts := got.Messages[0].ParseContent()
	require.Len(t, parts, 5)
	assert.Equal(t, dto.ContentTypeText, parts[0].Type)
	assert.Equal(t, "look", parts[0].Text)
	assert.Equal(t, dto.ContentTypeImageURL, parts[1].Type)
	assert.Equal(t, "https://example.test/a.png", parts[1].GetImageMedia().Url)
	assert.Equal(t, dto.ContentTypeFile, parts[2].Type)
	assert.Equal(t, "file_1", parts[2].GetFile().FileId)
	assert.Equal(t, dto.ContentTypeInputAudio, parts[3].Type)
	assert.Equal(t, "wav", parts[3].GetInputAudio().Format)
	assert.Equal(t, dto.ContentTypeVideoUrl, parts[4].Type)
	assert.Equal(t, "https://example.test/v.mp4", parts[4].GetVideoUrl().Url)
}

func TestResponsesRequestToChatCompletionsRequestNormalizesCodexInternalRoles(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role": "developer",
				"content": []map[string]any{
					{"type": "input_text", "text": "Follow project instructions."},
				},
			},
			{"role": "latest_reminder", "content": "Keep the reply brief."},
			{"role": "unknown_codex_role", "content": "Fallback content."},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 3)
	assert.Equal(t, "system", got.Messages[0].Role)
	assert.Equal(t, "Follow project instructions.", got.Messages[0].StringContent())
	assert.Equal(t, "user", got.Messages[1].Role)
	assert.Equal(t, "Keep the reply brief.", got.Messages[1].StringContent())
	assert.Equal(t, "user", got.Messages[2].Role)
	assert.Equal(t, "Fallback content.", got.Messages[2].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestCollapsesSystemMessagesToHead(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:        "gpt-test",
		Instructions: mustRawMessage(t, "You are Codex."),
		Input: mustRawMessage(t, []map[string]any{
			{"role": "developer", "content": "Permissions block"},
			{"role": "user", "content": "First user message"},
			{"role": "assistant", "content": "First answer"},
			{"role": "developer", "content": "Collaboration mode"},
			{"role": "user", "content": "Second user message"},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 4)
	assert.Equal(t, "system", got.Messages[0].Role)
	assert.Equal(t, "You are Codex.\n\nPermissions block\n\nCollaboration mode", got.Messages[0].StringContent())
	assert.Equal(t, "user", got.Messages[1].Role)
	assert.Equal(t, "First user message", got.Messages[1].StringContent())
	assert.Equal(t, "assistant", got.Messages[2].Role)
	assert.Equal(t, "First answer", got.Messages[2].StringContent())
	assert.Equal(t, "user", got.Messages[3].Role)
	assert.Equal(t, "Second user message", got.Messages[3].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestRejectsNonTextSystemContent(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role": "developer",
				"content": []map[string]any{
					{"type": "input_image", "image_url": "https://example.test/instruction.png"},
				},
			},
		}),
	})

	require.ErrorContains(t, err, "must be text-only")
}

func TestResponsesRequestToChatCompletionsRequestAssistantTextAndFunctionCallCoexist(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will call."},
				},
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  map[string]any{"ok": true},
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 2)
	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Equal(t, "I will call.", got.Messages[0].StringContent())
	toolCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_1", toolCalls[0].ID)
	assert.Equal(t, "function", toolCalls[0].Type)
	assert.Equal(t, "lookup", toolCalls[0].Function.Name)
	assert.JSONEq(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, "tool", got.Messages[1].Role)
	assert.Equal(t, "call_1", got.Messages[1].ToolCallId)
	assert.JSONEq(t, `{"ok":true}`, got.Messages[1].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestBatchesParallelToolMedia(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "screenshot", "arguments": `{}`},
			{"type": "function_call", "call_id": "call_2", "name": "inspect", "arguments": `{}`},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output": []map[string]any{
					{"type": "input_text", "text": "first image"},
					{"type": "input_image", "image_url": "data:image/png;base64,FIRST"},
				},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_2",
				"output": map[string]any{
					"type":     "image",
					"mimeType": "image/webp",
					"data":     "SECOND",
				},
			},
			{"type": "function_call", "call_id": "call_3", "name": "continue", "arguments": `{}`},
			{"type": "function_call_output", "call_id": "call_3", "output": "done"},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 6)
	assert.Equal(t, []string{"assistant", "tool", "tool", "user", "assistant", "tool"}, []string{
		got.Messages[0].Role,
		got.Messages[1].Role,
		got.Messages[2].Role,
		got.Messages[3].Role,
		got.Messages[4].Role,
		got.Messages[5].Role,
	})
	assert.NotContains(t, got.Messages[1].StringContent(), "data:image/png")
	assert.NotContains(t, got.Messages[2].StringContent(), "SECOND")
	media := got.Messages[3].ParseContent()
	require.Len(t, media, 4)
	assert.Equal(t, "[new-api: media output of tool call call_1]", media[0].Text)
	assert.Equal(t, "data:image/png;base64,FIRST", media[1].GetImageMedia().Url)
	assert.Equal(t, "[new-api: media output of tool call call_2]", media[2].Text)
	assert.Equal(t, "data:image/webp;base64,SECOND", media[3].GetImageMedia().Url)
	assert.Equal(t, "call_3", got.Messages[4].ParseToolCalls()[0].ID)
}

func TestResponsesRequestToChatCompletionsRequestKeepsParallelToolResultsAdjacentWhenReasoningInterleaves(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "screenshot", "arguments": `{}`},
			{"type": "function_call", "call_id": "call_2", "name": "inspect", "arguments": `{}`},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output": []map[string]any{
					{"type": "input_text", "text": "first image"},
					{"type": "input_image", "image_url": "data:image/png;base64,FIRST"},
				},
			},
			{
				"type":    "reasoning",
				"summary": []map[string]any{{"type": "summary_text", "text": "inspect both results"}},
			},
			{"type": "function_call_output", "call_id": "call_2", "output": "done"},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 4)
	assert.Equal(t, []string{"assistant", "tool", "tool", "user"}, []string{
		got.Messages[0].Role,
		got.Messages[1].Role,
		got.Messages[2].Role,
		got.Messages[3].Role,
	})
	assert.Equal(t, "inspect both results", got.Messages[0].GetReasoningContent())
	assert.Equal(t, "call_1", got.Messages[1].ToolCallId)
	assert.Equal(t, "call_2", got.Messages[2].ToolCallId)
	media := got.Messages[3].ParseContent()
	require.Len(t, media, 2)
	assert.Equal(t, "[new-api: media output of tool call call_1]", media[0].Text)
	assert.Equal(t, "data:image/png;base64,FIRST", media[1].GetImageMedia().Url)
}

func TestResponsesRequestToChatCompletionsRequestResolvesDocumentURL(t *testing.T) {
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{
		GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
			require.True(t, source.IsURL())
			assert.Equal(t, "https://example.test/report.pdf", source.GetRawData())
			return "PDF_BASE64", "application/pdf", nil
		},
	})
	defer relaymedia.SetMediaResolver(relaymedia.MediaResolver{})

	got, err := ResponsesRequestToChatCompletionsRequestWithContext(context.Background(), &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type":     "input_file",
						"file_url": "https://example.test/report.pdf",
						"filename": "report.pdf",
					},
				},
			},
		}),
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	parts := got.Messages[0].ParseContent()
	require.Len(t, parts, 1)
	assert.Equal(t, "report.pdf", parts[0].GetFile().FileName)
	assert.Equal(t, "data:application/pdf;base64,PDF_BASE64", parts[0].GetFile().FileData)
}

func TestResponsesRequestToChatCompletionsRequestPreservesVisibleReasoningWithoutEncryptedContent(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":              "reasoning",
				"summary":           []map[string]any{{"type": "summary_text", "text": "inspect inputs"}},
				"content":           []map[string]any{{"type": "reasoning_text", "text": "inspect inputs"}},
				"encrypted_content": "opaque-provider-state",
			},
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will call."},
				},
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 1)
	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Equal(t, "inspect inputs", got.Messages[0].GetReasoningContent())
	assert.Equal(t, "I will call.", got.Messages[0].StringContent())
	require.Len(t, got.Messages[0].ParseToolCalls(), 1)
	encoded, err := kitutil.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "encrypted_content")
	assert.NotContains(t, string(encoded), "opaque-provider-state")
}

func TestResponsesRequestToChatCompletionsRequestPreservesEmbeddedAndStringReasoning(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "kimi-k2-thinking",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":              "message",
				"role":              "assistant",
				"reasoning_content": "embedded thought",
				"content":           "first answer",
			},
			{
				"type":    "reasoning",
				"summary": "tool thought",
			},
			{
				"type":              "function_call",
				"call_id":           "call_1",
				"name":              "lookup",
				"arguments":         `{}`,
				"reasoning_content": "tool thought",
			},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)

	assert.Equal(t, "embedded thought\n\ntool thought", got.Messages[0].GetReasoningContent())
	assert.Equal(t, "first answer", got.Messages[0].StringContent())
	require.Len(t, got.Messages[0].ParseToolCalls(), 1)
	assert.Equal(t, "call_1", got.Messages[0].ParseToolCalls()[0].ID)
	assert.Equal(t, "tool", got.Messages[1].Role)
}

func TestResponsesRequestToChatCompletionsRequestBackfillsToolCallReasoning(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "kimi-k2-thinking",
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{}`},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)

	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Equal(t, "tool call", got.Messages[0].GetReasoningContent())
	require.Len(t, got.Messages[0].ParseToolCalls(), 1)
	assert.Equal(t, "tool", got.Messages[1].Role)
}

func TestResponsesRequestToChatCompletionsRequestOnlyFunctionCallCreatesAssistant(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": `{"q":"x"}`,
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 1)
	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Nil(t, got.Messages[0].Content)
	toolCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
}

func TestResponsesRequestToChatCompletionsRequestOmitsNonStandardEnableThinking(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:          "openpangu-2.0-flash",
		Input:          mustRawMessage(t, "hello"),
		EnableThinking: mustRawMessage(t, true),
	})
	require.NoError(t, err)
	assert.Empty(t, got.EnableThinking)

	encoded, err := kitutil.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), `"enable_thinking"`)
}

func TestResponsesRequestToChatCompletionsRequestDropsIncompleteToolCallAndOutput(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "keep me"},
			{"type": "function_call", "status": "incomplete", "call_id": "call_1", "name": "lookup", "arguments": `{`},
			{"type": "function_call_output", "call_id": "call_1", "output": "orphaned"},
		}),
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "user", got.Messages[0].Role)
	assert.Equal(t, "keep me", got.Messages[0].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestDropsPartialParallelToolTurn(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "run both"},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{}`},
			{"type": "function_call", "call_id": "call_2", "name": "lookup", "arguments": `{}`},
			{"type": "function_call_output", "call_id": "call_1", "output": "one"},
			{"role": "user", "content": "continue"},
		}),
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, []string{"user", "user"}, []string{got.Messages[0].Role, got.Messages[1].Role})
	assert.Equal(t, "continue", got.Messages[1].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestOrdersToolResultBeforeUserContent(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "run it"},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{}`},
			{"role": "user", "content": "then explain"},
			{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		}),
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 4)
	assert.Equal(t, []string{"user", "assistant", "tool", "user"}, []string{
		got.Messages[0].Role,
		got.Messages[1].Role,
		got.Messages[2].Role,
		got.Messages[3].Role,
	})
	assert.Equal(t, "call_1", got.Messages[2].ToolCallId)
	assert.Equal(t, "then explain", got.Messages[3].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestValidatesFunctionArguments(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": ""},
		}),
	}
	got, err := ResponsesRequestToChatCompletionsRequest(request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	require.Len(t, got.Messages[0].ParseToolCalls(), 1)
	assert.JSONEq(t, `{}`, got.Messages[0].ParseToolCalls()[0].Function.Arguments)

	for _, arguments := range []any{`{broken`, `[1,2]`, []any{1, 2}} {
		request.Input = mustRawMessage(t, []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": arguments},
		})
		_, err = ResponsesRequestToChatCompletionsRequest(request)
		require.ErrorContains(t, err, "arguments must be a JSON object")
	}
}

func TestResponsesRequestToChatCompletionsRequestToolsToolChoiceAndTextFormat(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, "hello"),
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"q": map[string]any{"type": "string"},
					},
				},
			},
		}),
		ToolChoice: mustRawMessage(t, map[string]any{
			"type": "function",
			"name": "lookup",
		}),
		Text: mustRawMessage(t, map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "answer",
				"schema": map[string]any{"type": "object"},
				"strict": true,
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Tools, 1)
	assert.Equal(t, "function", got.Tools[0].Type)
	assert.Equal(t, "lookup", got.Tools[0].Function.Name)
	assert.Equal(t, "Lookup data", got.Tools[0].Function.Description)
	assert.Equal(t, "object", got.Tools[0].Function.Parameters.(map[string]any)["type"])
	assert.Equal(t, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "lookup",
		},
	}, got.ToolChoice)
	require.NotNil(t, got.ResponseFormat)
	assert.Equal(t, "json_schema", got.ResponseFormat.Type)
	assert.Equal(t, "answer", gjson.GetBytes(got.ResponseFormat.JsonSchema, "name").String())
	assert.True(t, gjson.GetBytes(got.ResponseFormat.JsonSchema, "strict").Bool())
}

func TestResponsesRequestToChatCompletionsRequestCustomToolCallUsesTemporaryFunctionShape(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":    "custom_tool_call",
				"call_id": "call_custom",
				"name":    "apply_patch",
				"input":   "patch body",
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 1)
	toolCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "function", toolCalls[0].Type)
	assert.Equal(t, "call_custom", toolCalls[0].ID)
	assert.Equal(t, "apply_patch", toolCalls[0].Function.Name)
	assert.Equal(t, "patch body", gjson.Get(toolCalls[0].Function.Arguments, "input").String())
	assert.Empty(t, toolCalls[0].Custom)
}

func TestResponsesRequestToChatCompletionsRequestRejectsEncodedToolNameCollision(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, "hello"),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "workspace__exec", "parameters": map[string]any{"type": "object"}},
			{
				"type": "namespace",
				"name": "workspace",
				"tools": []map[string]any{
					{"type": "function", "name": "exec", "parameters": map[string]any{"type": "object"}},
				},
			},
		}),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts after Chat name encoding")
}

func TestResponsesRequestToChatCompletionsRequestRejectsStatefulFields(t *testing.T) {
	tests := []struct {
		name string
		req  *dto.OpenAIResponsesRequest
		want string
	}{
		{
			name: "conversation",
			req:  &dto.OpenAIResponsesRequest{Model: "gpt-test", Conversation: mustRawMessage(t, "conv_1")},
			want: "conversation",
		},
		{
			name: "previous response",
			req:  &dto.OpenAIResponsesRequest{Model: "gpt-test", PreviousResponseID: "resp_1"},
			want: "previous_response_id",
		},
		{
			name: "prompt",
			req:  &dto.OpenAIResponsesRequest{Model: "gpt-test", Prompt: mustRawMessage(t, map[string]any{"id": "pmpt_1"})},
			want: "prompt",
		},
		{
			name: "context management",
			req:  &dto.OpenAIResponsesRequest{Model: "gpt-test", ContextManagement: mustRawMessage(t, map[string]any{"type": "auto"})},
			want: "context_management",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResponsesRequestToChatCompletionsRequest(tt.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, err.Error(), "stateful fields")
		})
	}
}

func mustRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return raw
}
