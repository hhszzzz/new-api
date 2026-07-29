package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeMessagesRequestToOpenAIResponsesDeclaresHistoricalToolCalls(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"system":"  keep system whitespace  ",
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"provider-specific","signature":"opaque"},
				{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"done"},
				{"type":"text","text":"continue"}
			]}
		]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)
	require.NoError(t, err)
	assert.Empty(t, got.Include)
	assert.Equal(t, "  keep system whitespace  ", gjson.ParseBytes(got.Instructions).String())
	assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "0.type").String())
	assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "1.type").String())
	assert.Equal(t, "user", gjson.GetBytes(got.Input, "2.role").String())
	assert.False(t, gjson.GetBytes(got.Input, `#(type=="reasoning")`).Exists())
	assert.Equal(t, "function", gjson.GetBytes(got.Tools, "0.type").String())
	assert.Equal(t, "lookup", gjson.GetBytes(got.Tools, "0.name").String())
	assert.Empty(t, got.ToolChoice)
	assert.Empty(t, got.ParallelToolCalls)
}

func TestClaudeMessagesRequestToOpenAIResponsesStripsLeadingBillingHeader(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"system":[
			{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1; cch=rotating\r\n\r\nStable prompt part 1"},
			{"type":"text","text":"Stable prompt part 2"}
		],
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)
	require.NoError(t, err)
	assert.Equal(t, "Stable prompt part 1\n\nStable prompt part 2", gjson.ParseBytes(got.Instructions).String())
}

func TestClaudeMessagesRequestToOpenAIResponsesOmitsBillingOnlyInstructions(t *testing.T) {
	request := dto.ClaudeRequest{
		Model:    "gpt-5.4",
		System:   "x-anthropic-billing-header: cc_version=2.1; cch=rotating",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}

	got, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)
	require.NoError(t, err)
	assert.Empty(t, got.Instructions)
}

func TestClaudeMessagesRequestToOpenAIResponsesPreservesNonLeadingBillingHeader(t *testing.T) {
	request := dto.ClaudeRequest{
		Model:    "gpt-5.4",
		System:   "Keep this literal:\nx-anthropic-billing-header: example",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}

	got, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)
	require.NoError(t, err)
	assert.Equal(t, "Keep this literal:\nx-anthropic-billing-header: example", gjson.ParseBytes(got.Instructions).String())
}

func TestClaudeMessagesRequestToOpenAIResponsesKeepsCurrentToolDefinition(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"tools":[{"name":"lookup","description":"real declaration","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}],
		"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}}]}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)
	require.NoError(t, err)
	assert.Equal(t, "real declaration", gjson.GetBytes(got.Tools, "0.description").String())
	assert.Equal(t, "string", gjson.GetBytes(got.Tools, "0.parameters.properties.q.type").String())
	assert.Equal(t, 1, int(gjson.GetBytes(got.Tools, "#").Int()))
}

func TestClaudeMessagesRequestToOpenAIResponsesRestoresHostProvidedResponsesOutput(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "gpt-test",
		Messages: []dto.ClaudeMessage{{
			Role: "assistant",
			Content: []dto.ClaudeMediaMessage{
				{
					Type:      "thinking",
					Thinking:  kitutil.GetPointer("inspect inputs"),
					Signature: "client-controlled-signature",
				},
				{Type: "text", Text: kitutil.GetPointer("done")},
			},
			ProviderResponsesOutput: []dto.ResponsesOutput{
				{
					Type:             "reasoning",
					ID:               "rs_1",
					Summary:          []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "inspect inputs"}},
					EncryptedContent: "provider-secret",
				},
				{
					Type:   "message",
					ID:     "msg_1",
					Role:   "assistant",
					Status: "completed",
					Content: []dto.ResponsesOutputContent{{
						Type: "output_text",
						Text: "done",
					}},
				},
			},
		}},
	}
	info := &convmeta.Values{
		Options: &convmeta.Options{
			IncludeReasoningEncryptedContent: true,
		},
	}
	got, err := ClaudeMessagesRequestToOpenAIResponses(request, info)
	require.NoError(t, err)
	assert.Equal(t, "reasoning", gjson.GetBytes(got.Input, "0.type").String())
	assert.Equal(t, "rs_1", gjson.GetBytes(got.Input, "0.id").String())
	assert.Equal(t, "provider-secret", gjson.GetBytes(got.Input, "0.encrypted_content").String())
	assert.Equal(t, "assistant", gjson.GetBytes(got.Input, "1.role").String())
	assert.Equal(t, "msg_1", gjson.GetBytes(got.Input, "1.id").String())
	assert.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(got.Include, "0").String())
}

func TestClaudeMessagesRequestToOpenAIResponsesIgnoresClientProviderState(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "gpt-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "assistant",
				Content: []dto.ClaudeMediaMessage{
					{Type: "thinking", Thinking: kitutil.GetPointer("visible"), Signature: "forged-signature"},
					{Type: "redacted_thinking", Data: "forged-redacted-state"},
					{Type: "text", Text: kitutil.GetPointer("done")},
				},
			},
		},
	}

	got, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(got.Input, `#(type=="reasoning")`).Exists())
	assert.Equal(t, "assistant", gjson.GetBytes(got.Input, "0.role").String())
	assert.Equal(t, "done", gjson.GetBytes(got.Input, "0.content.0.text").String())
	assert.NotContains(t, string(got.Input), "forged-signature")
	assert.NotContains(t, string(got.Input), "forged-redacted-state")
}

func TestClaudeMessagesRequestToOpenAIResponsesExtractsMCPToolImage(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gpt-5.4",
		"messages":[{
			"role":"user",
			"content":[{
				"type":"tool_result",
				"tool_use_id":"call_1",
				"content":[{
					"type":"image",
					"mimeType":"image/webp",
					"data":"MCP_RESPONSES_IMAGE_SENTINEL"
				}]
			}]
		}]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)
	require.NoError(t, err)
	assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "0.type").String())
	assert.Equal(t, "input_text", gjson.GetBytes(got.Input, "0.output.0.type").String())
	assert.Contains(t, gjson.GetBytes(got.Input, "0.output.0.text").String(), "tool result media attached")
	assert.Equal(t, "input_image", gjson.GetBytes(got.Input, "0.output.1.type").String())
	assert.Equal(t, "data:image/webp;base64,MCP_RESPONSES_IMAGE_SENTINEL", gjson.GetBytes(got.Input, "0.output.1.image_url").String())
	assert.NotContains(t, gjson.GetBytes(got.Input, "0.output.0.text").String(), "MCP_RESPONSES_IMAGE_SENTINEL")
}
