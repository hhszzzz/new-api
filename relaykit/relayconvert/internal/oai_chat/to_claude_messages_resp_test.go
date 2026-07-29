package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseOpenAI2ClaudeToolUseInputIsObject(t *testing.T) {
	tests := []struct {
		name string
		args string
		want map[string]interface{}
	}{
		{name: "object", args: `{"q":"x"}`, want: map[string]interface{}{"q": "x"}},
		{name: "empty", args: "", want: map[string]interface{}{}},
		{name: "invalid", args: "{", want: map[string]interface{}{}},
		{name: "null", args: "null", want: map[string]interface{}{}},
		{name: "array", args: `["x"]`, want: map[string]interface{}{}},
		{name: "string", args: `"x"`, want: map[string]interface{}{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := dto.Message{Role: "assistant"}
			msg.SetToolCalls([]dto.ToolCallRequest{
				{
					ID:   "call_1",
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      "lookup",
						Arguments: tt.args,
					},
				},
			})

			resp := ResponseOpenAI2Claude(&dto.OpenAITextResponse{
				Id:    "chatcmpl_1",
				Model: "gpt-test",
				Choices: []dto.OpenAITextResponseChoice{
					{Message: msg, FinishReason: "tool_calls"},
				},
			}, nil)

			require.Len(t, resp.Content, 1)
			assert.Equal(t, "tool_use", resp.Content[0].Type)
			assert.Equal(t, tt.want, resp.Content[0].Input)
		})
	}
}

func TestResponseOpenAI2ClaudeUsageCarriesOpenAIBillingUsage(t *testing.T) {
	resp := ResponseOpenAI2Claude(&dto.OpenAITextResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.OpenAITextResponseChoice{
			{Message: dto.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"},
		},
		Usage: dto.Usage{
			PromptTokens:     11,
			CompletionTokens: 5,
			TotalTokens:      16,
		},
	}, nil)

	require.NotNil(t, resp.Usage)
	assert.Equal(t, 11, resp.Usage.InputTokens)
	assert.Equal(t, 5, resp.Usage.OutputTokens)
	require.NotNil(t, resp.Usage.BillingUsage)
	require.NotNil(t, resp.Usage.BillingUsage.OpenAIUsage)
	assert.Equal(t, dto.BillingUsageSourceOAIChat, resp.Usage.BillingUsage.Source)
	assert.Equal(t, dto.BillingUsageSemanticOpenAI, resp.Usage.BillingUsage.Semantic)
	assert.Equal(t, 11, resp.Usage.BillingUsage.OpenAIUsage.PromptTokens)
	assert.Equal(t, 5, resp.Usage.BillingUsage.OpenAIUsage.CompletionTokens)
	assert.Equal(t, 16, resp.Usage.BillingUsage.OpenAIUsage.TotalTokens)
	assert.Nil(t, resp.Usage.BillingUsage.OpenAIUsage.BillingUsage)
}

func TestResponseOpenAI2ClaudePreservesVisibleReasoningWithoutSignature(t *testing.T) {
	reasoning := "inspect the inputs"
	resp := ResponseOpenAI2Claude(&dto.OpenAITextResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message: dto.Message{
					Role:             "assistant",
					Content:          "answer",
					ReasoningContent: &reasoning,
				},
				FinishReason: "stop",
			},
		},
	}, nil)

	require.Len(t, resp.Content, 2)
	assert.Equal(t, "thinking", resp.Content[0].Type)
	require.NotNil(t, resp.Content[0].Thinking)
	assert.Equal(t, reasoning, *resp.Content[0].Thinking)
	assert.Empty(t, resp.Content[0].Signature)
	assert.Equal(t, "text", resp.Content[1].Type)
	assert.Equal(t, "answer", resp.Content[1].GetText())
}

func TestResponseOpenAI2ClaudePreservesCompatibleReasoningRefusalAndLegacyFunctionCall(t *testing.T) {
	var response dto.OpenAITextResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"id":"chatcmpl_compat",
		"model":"compat-model",
		"choices":[{
			"message":{
				"role":"assistant",
				"reasoning_details":[{"text":"Check policy."}],
				"refusal":"I cannot help.",
				"function_call":{"name":"audit","arguments":"{\"safe\":true}"}
			},
			"finish_reason":"function_call"
		}]
	}`), &response))

	converted := ResponseOpenAI2Claude(&response, nil)
	require.Len(t, converted.Content, 3)
	assert.Equal(t, "thinking", converted.Content[0].Type)
	assert.Equal(t, "Check policy.", *converted.Content[0].Thinking)
	assert.Equal(t, "text", converted.Content[1].Type)
	assert.Equal(t, "I cannot help.", converted.Content[1].GetText())
	assert.Equal(t, "tool_use", converted.Content[2].Type)
	assert.Equal(t, "call_0", converted.Content[2].Id)
	assert.Equal(t, "audit", converted.Content[2].Name)
	assert.Equal(t, "tool_use", converted.StopReason)
}

func TestStreamResponseOpenAI2ClaudePreservesRefusalDelta(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 1,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}
	var response dto.ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"id":"chatcmpl_refusal",
		"model":"compat-model",
		"choices":[{"index":0,"delta":{"refusal":"I cannot help."}}]
	}`), &response))

	converted := StreamResponseOpenAI2Claude(&response, info)
	require.Len(t, converted, 3)
	assert.Equal(t, "message_start", converted[0].Type)
	assert.Equal(t, "content_block_start", converted[1].Type)
	assert.Equal(t, "text", converted[1].ContentBlock.Type)
	assert.Equal(t, "content_block_delta", converted[2].Type)
	assert.Equal(t, "text_delta", converted[2].Delta.Type)
	assert.Equal(t, "I cannot help.", *converted[2].Delta.Text)
}

func TestStreamResponseOpenAI2ClaudeNormalizesLegacyFunctionCall(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 1,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}
	var response dto.ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"id":"chatcmpl_legacy",
		"model":"compat-model",
		"choices":[{"index":0,"delta":{"function_call":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}}]
	}`), &response))

	converted := StreamResponseOpenAI2Claude(&response, info)
	require.Len(t, converted, 3)
	assert.Equal(t, "message_start", converted[0].Type)
	assert.Equal(t, "content_block_start", converted[1].Type)
	assert.Equal(t, "tool_use", converted[1].ContentBlock.Type)
	assert.Equal(t, "call_0", converted[1].ContentBlock.Id)
	assert.Equal(t, "lookup", converted[1].ContentBlock.Name)
}

func TestStreamResponseOpenAI2ClaudeMapsLegacyFunctionCallFinishReason(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 1,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}
	var callChunk dto.ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"id":"chatcmpl_legacy",
		"model":"compat-model",
		"choices":[{"index":0,"delta":{"function_call":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}}]
	}`), &callChunk))
	require.NotEmpty(t, StreamResponseOpenAI2Claude(&callChunk, info))

	finishReason := "function_call"
	response := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finishReason}},
		Usage: &dto.Usage{
			PromptTokens:     3,
			CompletionTokens: 1,
			TotalTokens:      4,
		},
	}

	converted := StreamResponseOpenAI2Claude(response, info)
	require.NotEmpty(t, converted)
	messageDelta := converted[len(converted)-2]
	assert.Equal(t, "message_delta", messageDelta.Type)
	require.NotNil(t, messageDelta.Delta)
	require.NotNil(t, messageDelta.Delta.StopReason)
	assert.Equal(t, "tool_use", *messageDelta.Delta.StopReason)
	assert.Equal(t, "message_stop", converted[len(converted)-1].Type)
}

func TestStreamResponseOpenAI2ClaudeDowngradesToolUseStopWithoutToolBlocks(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 1,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}
	// Malformed upstream: tool call argument deltas that never carry a name.
	unnamedChunk := &dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_unnamed",
		Model: "compat-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ToolCalls: []dto.ToolCallResponse{
						{
							Index:    ptr(0),
							Type:     "function",
							Function: dto.FunctionResponse{Arguments: `{"q":"x"}`},
						},
					},
				},
			},
		},
	}
	finishChunk := &dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_unnamed",
		Model: "compat-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{FinishReason: ptr("tool_calls")},
		},
		Usage: &dto.Usage{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4},
	}

	converted := StreamResponseOpenAI2Claude(unnamedChunk, info)
	info.SendResponseCount = 2
	converted = append(converted, StreamResponseOpenAI2Claude(finishChunk, info)...)
	converted = append(converted, FinalizeStreamResponseOpenAI2Claude(info)...)

	var stopReason string
	for _, event := range converted {
		if event.ContentBlock != nil {
			assert.NotEqual(t, "tool_use", event.ContentBlock.Type)
		}
		if event.Type == "message_delta" && event.Delta != nil && event.Delta.StopReason != nil {
			stopReason = *event.Delta.StopReason
		}
	}
	assert.Equal(t, "end_turn", stopReason)
}

func TestBuildClaudeUsageFromOpenAICacheWriteUsage(t *testing.T) {
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens:     3619,
		CompletionTokens: 36,
		TotalTokens:      3655,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:     2921,
			CacheWriteTokens: 3616,
		},
	})

	require.NotNil(t, usage)
	// Claude semantics reports input_tokens excluding cache read/write; the
	// overlapping unadjusted prefixes drive the remainder negative, clamp to 0.
	assert.Equal(t, 0, usage.InputTokens)
	assert.Equal(t, 2921, usage.CacheReadInputTokens)
	assert.Equal(t, 3616, usage.CacheCreationInputTokens)
	assert.Equal(t, 36, usage.OutputTokens)
	require.NotNil(t, usage.BillingUsage)
	require.NotNil(t, usage.BillingUsage.OpenAIUsage)
	assert.Equal(t, dto.BillingUsageSemanticOpenAI, usage.BillingUsage.Semantic)
	assert.Equal(t, 3616, usage.BillingUsage.OpenAIUsage.PromptTokensDetails.CacheWriteTokens)
}

func TestBuildClaudeUsageFromOpenAICacheReadUsageExcludesCachedPrefix(t *testing.T) {
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 7,
		TotalTokens:      107,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 80,
		},
	})

	require.NotNil(t, usage)
	assert.Equal(t, 20, usage.InputTokens)
	assert.Equal(t, 80, usage.CacheReadInputTokens)
	assert.Zero(t, usage.CacheCreationInputTokens)
	assert.Equal(t, 7, usage.OutputTokens)
	require.NotNil(t, usage.BillingUsage)
	require.NotNil(t, usage.BillingUsage.OpenAIUsage)
	assert.Equal(t, 100, usage.BillingUsage.OpenAIUsage.PromptTokens)
	assert.Equal(t, 80, usage.BillingUsage.OpenAIUsage.PromptTokensDetails.CachedTokens)
}

func TestStreamResponseOpenAI2ClaudeClosesTextThinkingAndToolBlocks(t *testing.T) {
	info := &convmeta.Values{
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}

	info.SendResponseCount = 1
	textResponses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Content: ptr("hello"),
				},
			},
		},
	}, info)
	require.Len(t, textResponses, 3)
	assert.Equal(t, "message_start", textResponses[0].Type)
	assert.Equal(t, "content_block_start", textResponses[1].Type)
	assert.Equal(t, 0, textResponses[1].GetIndex())
	assert.Equal(t, "content_block_delta", textResponses[2].Type)

	info.SendResponseCount = 2
	thinkingResponses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ReasoningContent: ptr("thinking"),
				},
			},
		},
	}, info)
	require.Len(t, thinkingResponses, 3)
	assert.Equal(t, "content_block_stop", thinkingResponses[0].Type)
	assert.Equal(t, 0, thinkingResponses[0].GetIndex())
	assert.Equal(t, "content_block_start", thinkingResponses[1].Type)
	assert.Equal(t, 1, thinkingResponses[1].GetIndex())
	assert.Equal(t, "thinking", thinkingResponses[1].ContentBlock.Type)
	assert.Equal(t, "content_block_delta", thinkingResponses[2].Type)

	info.SendResponseCount = 3
	toolResponses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ToolCalls: []dto.ToolCallResponse{
						{
							Index: ptr(0),
							ID:    "call_1",
							Type:  "function",
							Function: dto.FunctionResponse{
								Name:      "lookup",
								Arguments: `{"q":"x"}`,
							},
						},
					},
				},
			},
		},
	}, info)
	require.Len(t, toolResponses, 3)
	assert.Equal(t, "content_block_stop", toolResponses[0].Type)
	assert.Equal(t, 1, toolResponses[0].GetIndex())
	assert.Equal(t, "content_block_start", toolResponses[1].Type)
	assert.Equal(t, 2, toolResponses[1].GetIndex())
	assert.Equal(t, "tool_use", toolResponses[1].ContentBlock.Type)
	assert.Equal(t, "content_block_delta", toolResponses[2].Type)

	info.SendResponseCount = 4
	finishResponses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{FinishReason: ptr("tool_calls")},
		},
		Usage: &dto.Usage{
			PromptTokens:     7,
			CompletionTokens: 3,
			TotalTokens:      10,
		},
	}, info)
	require.Len(t, finishResponses, 3)
	assert.Equal(t, "content_block_stop", finishResponses[0].Type)
	assert.Equal(t, 2, finishResponses[0].GetIndex())
	assert.Equal(t, "message_delta", finishResponses[1].Type)
	assert.Equal(t, "tool_use", *finishResponses[1].Delta.StopReason)
	require.NotNil(t, finishResponses[1].Usage)
	require.NotNil(t, finishResponses[1].Usage.BillingUsage)
	require.NotNil(t, finishResponses[1].Usage.BillingUsage.OpenAIUsage)
	assert.Equal(t, 7, finishResponses[1].Usage.BillingUsage.OpenAIUsage.PromptTokens)
	assert.Equal(t, 3, finishResponses[1].Usage.BillingUsage.OpenAIUsage.CompletionTokens)
	assert.Equal(t, "message_stop", finishResponses[2].Type)
}

func TestStreamResponseOpenAI2ClaudeTreatsProviderStateAsInternalBoundary(t *testing.T) {
	info := &convmeta.Values{
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}

	info.SendResponseCount = 1
	visible := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: ptr("first thought")},
		}},
	}, info)
	require.Len(t, visible, 3)
	assert.Equal(t, "content_block_start", visible[1].Type)
	assert.Equal(t, 0, visible[1].GetIndex())

	info.SendResponseCount = 2
	boundary := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:                        "chatcmpl_1",
		Model:                     "gpt-test",
		ReasoningEncryptedContent: "bridge-state-1",
	}, info)
	require.Len(t, boundary, 1)
	assert.Equal(t, "content_block_stop", boundary[0].Type)
	assert.Equal(t, 0, boundary[0].GetIndex())
	assert.Equal(t, convmeta.LastMessageTypeNone, info.ClaudeConvertInfo.LastMessagesType)

	info.SendResponseCount = 3
	next := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: ptr("second thought")},
		}},
	}, info)
	require.Len(t, next, 2)
	assert.Equal(t, "content_block_start", next[0].Type)
	assert.Equal(t, 1, next[0].GetIndex())
	assert.Equal(t, "thinking", next[0].ContentBlock.Type)
	assert.Equal(t, "content_block_delta", next[1].Type)
}

func TestStreamResponseOpenAI2ClaudeUsesHandlerFallbackUsageOnFinish(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 2,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeText,
			Usage: &dto.Usage{
				PromptTokens:     7,
				CompletionTokens: 3,
				TotalTokens:      10,
			},
		},
	}

	responses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{FinishReason: ptr("stop")},
		},
	}, info)

	require.Len(t, responses, 3)
	assert.Equal(t, "content_block_stop", responses[0].Type)
	assert.Equal(t, "message_delta", responses[1].Type)
	require.NotNil(t, responses[1].Usage)
	assert.Equal(t, 7, responses[1].Usage.InputTokens)
	assert.Equal(t, 3, responses[1].Usage.OutputTokens)
	assert.Equal(t, "message_stop", responses[2].Type)
}

func TestResponseOpenAI2ClaudeSynthesizesMissingAndDuplicateToolUseIDs(t *testing.T) {
	message := dto.Message{Role: "assistant"}
	message.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_same", Type: "function", Function: dto.FunctionRequest{Name: "first", Arguments: `{}`}},
		{ID: "call_same", Type: "function", Function: dto.FunctionRequest{Name: "second", Arguments: `{}`}},
		{Type: "function", Function: dto.FunctionRequest{Name: "third", Arguments: `{}`}},
	})

	response := ResponseOpenAI2Claude(&dto.OpenAITextResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.OpenAITextResponseChoice{
			{Message: message, FinishReason: "tool_calls"},
		},
	}, nil)

	require.Len(t, response.Content, 3)
	assert.Equal(t, "call_same", response.Content[0].Id)
	assert.NotEmpty(t, response.Content[1].Id)
	assert.NotEmpty(t, response.Content[2].Id)
	assert.NotEqual(t, response.Content[0].Id, response.Content[1].Id)
	assert.NotEqual(t, response.Content[1].Id, response.Content[2].Id)
	assert.Contains(t, response.Content[1].Id, "toolu_")
	assert.Contains(t, response.Content[2].Id, "toolu_")
}

func TestStreamResponseOpenAI2ClaudeHandlesParallelToolsInFirstChunk(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 1,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: ptr(0), Type: "function", Function: dto.FunctionResponse{Name: "lookup", Arguments: `{"q":`}},
				{Index: ptr(1), ID: "call_same", Type: "function", Function: dto.FunctionResponse{Name: "fetch", Arguments: `{"id":`}},
			}},
		}},
	}, info)

	require.Len(t, first, 5)
	assert.Equal(t, "message_start", first[0].Type)
	assert.Equal(t, "content_block_start", first[1].Type)
	assert.Equal(t, 0, first[1].GetIndex())
	assert.NotEmpty(t, first[1].ContentBlock.Id)
	assert.Equal(t, "content_block_delta", first[2].Type)
	assert.Equal(t, `{"q":`, *first[2].Delta.PartialJson)
	assert.Equal(t, "content_block_start", first[3].Type)
	assert.Equal(t, 1, first[3].GetIndex())
	assert.Equal(t, "call_same", first[3].ContentBlock.Id)
	assert.NotEqual(t, first[1].ContentBlock.Id, first[3].ContentBlock.Id)
	assert.Equal(t, "content_block_delta", first[4].Type)

	info.SendResponseCount = 2
	continuation := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: ptr(0), Type: "function", Function: dto.FunctionResponse{Name: "lookup", Arguments: `"x"}`}},
				{Index: ptr(1), ID: "call_same", Type: "function", Function: dto.FunctionResponse{Name: "fetch", Arguments: `2}`}},
			}},
		}},
	}, info)

	require.Len(t, continuation, 2)
	assert.Equal(t, "content_block_delta", continuation[0].Type)
	assert.Equal(t, "content_block_delta", continuation[1].Type)

	info.SendResponseCount = 3
	finish := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: ptr("tool_calls")}},
		Usage: &dto.Usage{
			PromptTokens:     5,
			CompletionTokens: 2,
			TotalTokens:      7,
		},
	}, info)

	require.Len(t, finish, 4)
	assert.Equal(t, "content_block_stop", finish[0].Type)
	assert.Equal(t, 0, finish[0].GetIndex())
	assert.Equal(t, "content_block_stop", finish[1].Type)
	assert.Equal(t, 1, finish[1].GetIndex())
	assert.Equal(t, "message_delta", finish[2].Type)
	assert.Equal(t, "message_stop", finish[3].Type)
}

func TestStreamResponseOpenAI2ClaudeBuffersToolArgumentsUntilNameArrives(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 1,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl_1",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: ptr(0),
				Type:  "function",
				Function: dto.FunctionResponse{
					Arguments: `{"q":`,
				},
			}}},
		}},
	}, info)
	require.Len(t, first, 1)
	assert.Equal(t, "message_start", first[0].Type)

	info.SendResponseCount = 2
	second := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl_1",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: ptr(0),
				Type:  "function",
				Function: dto.FunctionResponse{
					Name:      "lookup",
					Arguments: `"x"}`,
				},
			}}},
		}},
	}, info)

	require.Len(t, second, 2)
	assert.Equal(t, "content_block_start", second[0].Type)
	assert.NotEmpty(t, second[0].ContentBlock.Id)
	assert.Equal(t, "lookup", second[0].ContentBlock.Name)
	assert.Equal(t, "content_block_delta", second[1].Type)
	assert.Equal(t, `{"q":"x"}`, *second[1].Delta.PartialJson)
}

func TestStreamResponseOpenAI2ClaudePreservesToolOrderWhenEarlierNameArrivesLate(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 1,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl_1",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: ptr(1),
				ID:    "call_second",
				Type:  "function",
				Function: dto.FunctionResponse{
					Name:      "second",
					Arguments: `{"b":2}`,
				},
			}}},
		}},
	}, info)

	require.Len(t, first, 1)
	assert.Equal(t, "message_start", first[0].Type)

	info.SendResponseCount = 2
	second := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl_1",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: ptr(0),
				ID:    "call_first",
				Type:  "function",
				Function: dto.FunctionResponse{
					Name:      "first",
					Arguments: `{"a":1}`,
				},
			}}},
		}},
	}, info)

	require.Len(t, second, 4)
	assert.Equal(t, "content_block_start", second[0].Type)
	assert.Equal(t, 0, second[0].GetIndex())
	assert.Equal(t, "first", second[0].ContentBlock.Name)
	assert.Equal(t, "content_block_delta", second[1].Type)
	assert.Equal(t, "content_block_start", second[2].Type)
	assert.Equal(t, 1, second[2].GetIndex())
	assert.Equal(t, "second", second[2].ContentBlock.Name)
	assert.Equal(t, "content_block_delta", second[3].Type)
}

func TestFinalizeStreamResponseOpenAI2ClaudeCompactsSparseToolsAndSkipsUnnamedTools(t *testing.T) {
	info := &convmeta.Values{
		SendResponseCount: 1,
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl_1",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{
					Index:    ptr(0),
					Type:     "function",
					Function: dto.FunctionResponse{Arguments: `{"discarded":`},
				},
				{
					Index: ptr(2),
					ID:    "call_valid",
					Type:  "function",
					Function: dto.FunctionResponse{
						Name:      "valid",
						Arguments: `{"kept":true}`,
					},
				},
			}},
		}},
	}, info)

	require.Len(t, first, 1)
	assert.Equal(t, "message_start", first[0].Type)

	final := FinalizeStreamResponseOpenAI2Claude(info)
	require.Len(t, final, 5)
	assert.Equal(t, "content_block_start", final[0].Type)
	assert.Equal(t, 0, final[0].GetIndex())
	assert.Equal(t, "valid", final[0].ContentBlock.Name)
	assert.Equal(t, "content_block_delta", final[1].Type)
	assert.Equal(t, `{"kept":true}`, *final[1].Delta.PartialJson)
	assert.Equal(t, "content_block_stop", final[2].Type)
	assert.Equal(t, 0, final[2].GetIndex())
	assert.Equal(t, "message_delta", final[3].Type)
	assert.Equal(t, "message_stop", final[4].Type)
}

func TestNormalizeCacheCreationSplit(t *testing.T) {
	cache5m, cache1h := NormalizeCacheCreationSplit(10, 3, 2)
	assert.Equal(t, 8, cache5m)
	assert.Equal(t, 2, cache1h)

	cache5m, cache1h = NormalizeCacheCreationSplit(3, 5, 1)
	assert.Equal(t, 5, cache5m)
	assert.Equal(t, 1, cache1h)
}

func ptr[T any](value T) *T {
	return &value
}
