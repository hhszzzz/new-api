package claudemessages

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeMessagesRequestToOpenAIChatPreservesMixedAssistantContentAndRequestControls(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"tools":[
			{"name":"lookup","input_schema":{"type":"object"}},
			{"name":"fetch","input_schema":{"type":"object"}}
		],
		"tool_choice":{"type":"tool","name":"lookup","disable_parallel_tool_use":true},
		"messages":[
			{
				"role":"user",
				"content":[
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"YWJj"}},
					{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}}
				]
			},
			{
				"role":"assistant",
				"content":[
					{"type":"thinking","thinking":"inspect the inputs","signature":"anthropic-signature"},
					{"type":"text","text":"I will use both tools."},
					{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}},
					{"type":"tool_use","id":"toolu_2","name":"fetch","input":{"id":2}}
				]
			}
		]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, &convmeta.Values{
		Options: &convmeta.Options{PreserveChatReasoningContent: true},
	})
	require.NoError(t, err)

	require.NotNil(t, got.ParallelTooCalls)
	assert.False(t, *got.ParallelTooCalls)
	assert.Equal(t, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "lookup",
		},
	}, got.ToolChoice)

	require.Len(t, got.Messages, 2)
	userParts := got.Messages[0].ParseContent()
	require.Len(t, userParts, 2)
	assert.Equal(t, "data:image/png;base64,YWJj", userParts[0].GetImageMedia().Url)
	assert.Equal(t, "https://example.test/image.png", userParts[1].GetImageMedia().Url)

	assistant := got.Messages[1]
	assert.Equal(t, "inspect the inputs", assistant.GetReasoningContent())
	assistantParts := assistant.ParseContent()
	require.Len(t, assistantParts, 1)
	assert.Equal(t, "I will use both tools.", assistantParts[0].Text)
	toolCalls := assistant.ParseToolCalls()
	require.Len(t, toolCalls, 2)
	assert.Equal(t, "toolu_1", toolCalls[0].ID)
	assert.Equal(t, "lookup", toolCalls[0].Function.Name)
	assert.JSONEq(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, "toolu_2", toolCalls[1].ID)
	assert.Equal(t, "fetch", toolCalls[1].Function.Name)
	assert.JSONEq(t, `{"id":2}`, toolCalls[1].Function.Arguments)
}

func TestClaudeMessagesRequestToOpenAIChatUsesCCSwitchReasoningContentGate(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"provider-model",
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"first"},
				{"type":"thinking","thinking":"second"},
				{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}
			]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"not sent without a tool"},
				{"type":"text","text":"done"}
			]},
			{"role":"assistant","content":[
				{"type":"redacted_thinking","data":"opaque"},
				{"type":"tool_use","id":"toolu_2","name":"lookup","input":{}}
			]}
		]
	}`), &request))

	generic, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, generic.Messages, 3)
	assert.Empty(t, generic.Messages[0].GetReasoningContent())
	assert.Empty(t, generic.Messages[1].GetReasoningContent())
	assert.Empty(t, generic.Messages[2].GetReasoningContent())

	vendor, err := ClaudeMessagesRequestToOpenAIChat(request, &convmeta.Values{
		Options: &convmeta.Options{PreserveChatReasoningContent: true},
	})
	require.NoError(t, err)
	require.Len(t, vendor.Messages, 3)
	assert.Equal(t, "first\nsecond", vendor.Messages[0].GetReasoningContent())
	assert.Empty(t, vendor.Messages[1].GetReasoningContent())
	assert.Equal(t, "[redacted thinking]", vendor.Messages[2].GetReasoningContent())
}

func TestClaudeMessagesRequestToOpenAIChatBackfillsVendorToolCallReasoning(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "provider-model",
		Messages: []dto.ClaudeMessage{{
			Role: "assistant",
			Content: []any{
				map[string]any{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": map[string]any{}},
			},
		}},
	}

	got, err := ClaudeMessagesRequestToOpenAIChat(request, &convmeta.Values{
		Options: &convmeta.Options{PreserveChatReasoningContent: true},
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "tool call", got.Messages[0].GetReasoningContent())
}

func TestClaudeMessagesRequestToOpenAIChatOmitsAnthropicTopK(t *testing.T) {
	topK := 0
	request := dto.ClaudeRequest{
		Model:    "openpangu-2.0-flash",
		TopK:     &topK,
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	assert.Nil(t, got.TopK)
	encoded, err := kitutil.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), `"top_k"`)
}

func TestClaudeMessagesRequestToOpenAIChatDropsToolControlsWithoutTools(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"tool_choice":{"type":"tool","name":"lookup","disable_parallel_tool_use":true},
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)

	assert.Empty(t, got.Tools)
	assert.Nil(t, got.ToolChoice)
	assert.Nil(t, got.ParallelTooCalls)
}

func TestClaudeMessagesRequestToOpenAIChatPreservesSystemWhitespace(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"system":[
			{"type":"text","text":"  first  "},
			{"type":"text","text":"\tsecond\n"}
		],
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "  first  \n\n\tsecond\n", got.Messages[0].StringContent())
}

func TestClaudeMessagesRequestToOpenAIChatStripsLeadingBillingHeader(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"system":[
			{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1; cch=rotating\r\n\r\nStable prompt part 1"},
			{"type":"text","text":"Stable prompt part 2"}
		],
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "Stable prompt part 1\n\nStable prompt part 2", got.Messages[0].StringContent())
}

func TestClaudeMessagesRequestToOpenAIChatOmitsBillingOnlySystem(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1; cch=rotating"}],
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "user", got.Messages[0].Role)
}

func TestClaudeMessagesRequestToOpenAIChatPreservesNonLeadingBillingHeader(t *testing.T) {
	request := dto.ClaudeRequest{
		Model:    "gpt-5.4",
		System:   "Keep this literal:\nx-anthropic-billing-header: example",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "Keep this literal:\nx-anthropic-billing-header: example", got.Messages[0].StringContent())
}

func TestClaudeMessagesRequestToOpenAIChatResolvesDocumentURL(t *testing.T) {
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{
		GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
			require.True(t, source.IsURL())
			assert.Equal(t, "https://example.test/report.pdf", source.GetRawData())
			return "PDF_BASE64", "application/pdf", nil
		},
	})
	defer relaymedia.SetMediaResolver(relaymedia.MediaResolver{})

	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"messages":[{"role":"user","content":[{
			"type":"document",
			"source":{"type":"url","url":"https://example.test/report.pdf"},
			"title":"report.pdf"
		}]}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChatWithContext(context.Background(), request, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	parts := got.Messages[0].ParseContent()
	require.Len(t, parts, 1)
	assert.Equal(t, dto.ContentTypeFile, parts[0].Type)
	assert.Equal(t, "report.pdf", parts[0].GetFile().FileName)
	assert.Equal(t, "data:application/pdf;base64,PDF_BASE64", parts[0].GetFile().FileData)
}

func TestClaudeMessagesRequestToOpenAIChatRejectsImageWithoutSource(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "image"},
				},
			},
		},
	}

	_, err := ClaudeMessagesRequestToOpenAIChat(request, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image source")
}

func TestClaudeMessagesRequestToOpenAIChatRejectsMalformedTools(t *testing.T) {
	request := dto.ClaudeRequest{
		Model:    "claude-test",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
		Tools:    map[string]any{"name": "not-an-array"},
	}

	_, err := ClaudeMessagesRequestToOpenAIChat(request, nil)

	require.ErrorContains(t, err, "invalid Claude tools")
}

func TestClaudeMessagesRequestToOpenAIChatUsesModelCompatibleFields(t *testing.T) {
	maxTokens := uint(4096)
	budget := 8000
	stream := true
	tests := []struct {
		name                    string
		model                   string
		wantMaxTokens           bool
		wantMaxCompletionTokens bool
		wantReasoningEffort     string
	}{
		{name: "ordinary chat model", model: "openpangu-2.0-flash", wantMaxTokens: true},
		{name: "gpt five", model: "gpt-5.4", wantMaxTokens: true, wantReasoningEffort: "medium"},
		{name: "o series", model: "o3-mini", wantMaxCompletionTokens: true, wantReasoningEffort: "medium"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ClaudeMessagesRequestToOpenAIChat(dto.ClaudeRequest{
				Model:     test.model,
				MaxTokens: &maxTokens,
				Stream:    &stream,
				Thinking:  &dto.Thinking{Type: "enabled", BudgetTokens: &budget},
				Messages:  []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
			}, nil)
			require.NoError(t, err)

			if test.wantMaxTokens {
				require.NotNil(t, got.MaxTokens)
				assert.Equal(t, maxTokens, *got.MaxTokens)
			} else {
				assert.Nil(t, got.MaxTokens)
			}
			if test.wantMaxCompletionTokens {
				require.NotNil(t, got.MaxCompletionTokens)
				assert.Equal(t, maxTokens, *got.MaxCompletionTokens)
			} else {
				assert.Nil(t, got.MaxCompletionTokens)
			}
			assert.Equal(t, test.wantReasoningEffort, got.ReasoningEffort)
			require.NotNil(t, got.StreamOptions)
			assert.True(t, got.StreamOptions.IncludeUsage)
		})
	}
}

func TestClaudeMessagesRequestToOpenAIChatUsesCCSwitchReasoningEffortMapping(t *testing.T) {
	tests := []struct {
		name         string
		outputConfig string
		thinking     *dto.Thinking
		want         string
	}{
		{
			name:     "adaptive uses xhigh",
			thinking: &dto.Thinking{Type: "adaptive"},
			want:     "xhigh",
		},
		{
			name:         "explicit output config wins",
			outputConfig: `{"effort":"low"}`,
			thinking:     &dto.Thinking{Type: "adaptive"},
			want:         "low",
		},
		{
			name:     "missing budget defaults high",
			thinking: &dto.Thinking{Type: "enabled"},
			want:     "high",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ClaudeMessagesRequestToOpenAIChat(dto.ClaudeRequest{
				Model:        "gpt-5.4",
				OutputConfig: []byte(test.outputConfig),
				Thinking:     test.thinking,
				Messages:     []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
			}, nil)
			require.NoError(t, err)
			assert.Equal(t, test.want, got.ReasoningEffort)
		})
	}
}

func TestClaudeMessagesRequestToOpenAIChatNormalizesToolSchemas(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"tools":[
			{"name":"missing_type","input_schema":{"properties":{"q":{"type":"string"}}}},
			{"name":"empty_schema","input_schema":{}}
		],
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, got.Tools, 2)
	assert.Equal(t, "object", got.Tools[0].Function.Parameters.(map[string]any)["type"])
	assert.Equal(t, map[string]any{"type": "object", "properties": map[string]any{}}, got.Tools[1].Function.Parameters)
}

func TestClaudeMessagesRequestToOpenAIChatDropsServerToolsAndLowersClientTypedTools(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"tools":[
			{"type":"web_search_20250305","name":"web_search"},
			{"type":"bash_20250124","name":"bash"},
			{"name":"lookup","input_schema":{"type":"object"}}
		],
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, got.Tools, 2)
	assert.Equal(t, "bash", got.Tools[0].Function.Name)
	assert.Equal(t, "lookup", got.Tools[1].Function.Name)
}

func TestClaudeMessagesRequestToOpenAIChatRejectsUndeclaredToolChoice(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"tools":[{"name":"lookup","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"tool","name":"missing"},
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))

	_, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.ErrorContains(t, err, `references undeclared tool "missing"`)
}

func TestClaudeMessagesRequestToOpenAIChatPreservesNoMediaToolResultRepresentation(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"messages":[{
			"role":"user",
			"content":[{
				"type":"tool_result",
				"tool_use_id":"call_1",
				"content":[{"type":"text","text":"plain"}]
			}]
		}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "tool", got.Messages[0].Role)
	assert.JSONEq(t, `[{"type":"text","text":"plain"}]`, got.Messages[0].StringContent())
}

func TestClaudeMessagesRequestToOpenAIChatBatchesToolResultMediaBeforeUserContent(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"messages":[
			{"role":"assistant","content":[
				{"type":"tool_use","id":"call_1","name":"inspect","input":{}},
				{"type":"tool_use","id":"call_2","name":"inspect","input":{}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"call_1","content":[
					{"type":"text","text":"caption"},
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"ONE"}}
				]},
				{"type":"tool_result","tool_use_id":"call_2","content":[
					{"type":"image","mimeType":"image/jpeg","data":"TWO"}
				]},
				{"type":"text","text":"continue"}
			]}
		]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)
	require.Len(t, got.Messages, 5)
	assert.Equal(t, []string{"assistant", "tool", "tool", "user", "user"}, []string{
		got.Messages[0].Role,
		got.Messages[1].Role,
		got.Messages[2].Role,
		got.Messages[3].Role,
		got.Messages[4].Role,
	})
	assert.Contains(t, got.Messages[1].StringContent(), "tool result media moved")
	assert.NotContains(t, got.Messages[1].StringContent(), "ONE")
	assert.Contains(t, got.Messages[2].StringContent(), "tool result media moved")
	assert.NotContains(t, got.Messages[2].StringContent(), "TWO")

	media := got.Messages[3].ParseContent()
	require.Len(t, media, 4)
	assert.Equal(t, "[new-api: media output of tool call call_1]", media[0].Text)
	assert.Equal(t, "data:image/png;base64,ONE", media[1].GetImageMedia().Url)
	assert.Equal(t, "[new-api: media output of tool call call_2]", media[2].Text)
	assert.Equal(t, "data:image/jpeg;base64,TWO", media[3].GetImageMedia().Url)
	userContent := got.Messages[4].ParseContent()
	require.Len(t, userContent, 1)
	assert.Equal(t, "continue", userContent[0].Text)
}

func TestClaudeMessagesRequestToOpenAIChatMapsThinkingToVendorParams(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		thinking     string
		wantEffort   string
		wantThinking string
	}{
		{name: "kimi thinking enabled", model: "kimi-k2.5", thinking: `{"type":"enabled","budget_tokens":20000}`, wantThinking: `{"type":"enabled"}`},
		{name: "glm explicit disable", model: "glm-5.2", thinking: `{"type":"disabled"}`, wantThinking: `{"type":"disabled"}`},
		{name: "deepseek budget maps to clamped effort", model: "deepseek-v4", thinking: `{"type":"enabled","budget_tokens":32000}`, wantEffort: "high", wantThinking: `{"type":"enabled"}`},
		{name: "openai style keeps reasoning_effort", model: "gpt-5.4", thinking: `{"type":"enabled","budget_tokens":32000}`, wantEffort: "high"},
		{name: "unknown model drops thinking", model: "openpangu-2.0-flash", thinking: `{"type":"enabled","budget_tokens":32000}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var request dto.ClaudeRequest
			require.NoError(t, kitutil.Unmarshal([]byte(`{
				"model":"`+test.model+`",
				"max_tokens":512,
				"thinking":`+test.thinking+`,
				"messages":[{"role":"user","content":"hi"}]
			}`), &request))

			got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
			require.NoError(t, err)

			assert.Equal(t, test.wantEffort, got.ReasoningEffort)
			assert.Equal(t, test.wantThinking, string(got.THINKING))
		})
	}
}

func TestClaudeMessagesRequestToOpenAIChatStripsCacheControlForNonOpenRouterUpstreams(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"deepseek-v4",
		"messages":[{"role":"user","content":[
			{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}},
			{"type":"text","text":"world"}
		]}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)

	encoded, err := kitutil.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "cache_control")
}
