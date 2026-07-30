package gemini

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponsesRequestToGeminiInstructionsAndInput(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model:        "gemini-test",
		Instructions: mustGeminiRawMessage(t, "system rules"),
		Input:        mustGeminiRawMessage(t, "hello"),
	})

	require.NotNil(t, got.SystemInstructions)
	require.Len(t, got.SystemInstructions.Parts, 1)
	assert.Equal(t, "system rules", got.SystemInstructions.Parts[0].Text)
	require.Len(t, got.Contents, 1)
	assert.Equal(t, "user", got.Contents[0].Role)
	require.Len(t, got.Contents[0].Parts, 1)
	assert.Equal(t, "hello", got.Contents[0].Parts[0].Text)
}

func TestConvertOpenAIResponsesRequestToGeminiFunctionToolAndChoice(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, "lookup weather"),
		Tools: mustGeminiRawMessage(t, []map[string]any{
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
		ToolChoice: mustGeminiRawMessage(t, map[string]any{
			"type": "function",
			"name": "lookup",
		}),
	})

	tools := got.GetTools()
	require.Len(t, tools, 1)
	assert.Equal(t, "lookup", gjson.GetBytes(got.Tools, "0.functionDeclarations.0.name").String())
	assert.Equal(t, "Lookup data", gjson.GetBytes(got.Tools, "0.functionDeclarations.0.description").String())
	require.NotNil(t, got.ToolConfig)
	require.NotNil(t, got.ToolConfig.FunctionCallingConfig)
	assert.Equal(t, dto.FunctionCallingConfigMode("ANY"), got.ToolConfig.FunctionCallingConfig.Mode)
	assert.Equal(t, []string{"lookup"}, got.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)
}

func TestConvertOpenAIResponsesRequestToGeminiRejectsUndeclaredToolChoice(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, "lookup weather"),
		ToolChoice: mustGeminiRawMessage(t, map[string]any{
			"type": "function",
			"name": "lookup",
		}),
	}

	info := &relaycommon.RelayInfo{
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: request.Model,
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, request)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `undeclared tool "lookup"`)
}

func TestConvertOpenAIResponsesRequestToGeminiFunctionCallConversation(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, []map[string]any{
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
		Tools: mustGeminiRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	})

	require.Len(t, got.Contents, 2)
	assert.Equal(t, "model", got.Contents[0].Role)
	require.Len(t, got.Contents[0].Parts, 2)
	require.NotNil(t, got.Contents[0].Parts[0].FunctionCall)
	assert.Equal(t, "lookup", got.Contents[0].Parts[0].FunctionCall.FunctionName)
	assert.Equal(t, map[string]interface{}{"q": "x"}, got.Contents[0].Parts[0].FunctionCall.Arguments)
	assert.Equal(t, "I will call.", got.Contents[0].Parts[1].Text)

	assert.Equal(t, "user", got.Contents[1].Role)
	require.Len(t, got.Contents[1].Parts, 1)
	require.NotNil(t, got.Contents[1].Parts[0].FunctionResponse)
	assert.Equal(t, "lookup", got.Contents[1].Parts[0].FunctionResponse.Name)
	assert.Equal(t, map[string]interface{}{"ok": true}, got.Contents[1].Parts[0].FunctionResponse.Response)
}

func TestConvertOpenAIResponsesRequestToGeminiLowersClientToolHistory(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		// Hosted web_search drops (CC Switch semantics); client-executed
		// local_shell lowers to a function declaration.
		Tools: mustGeminiRawMessage(t, []map[string]any{
			{"type": "web_search"},
			{"type": "local_shell"},
		}),
		Input: mustGeminiRawMessage(t, []map[string]any{
			{
				"type":    "reasoning",
				"summary": []map[string]any{{"type": "summary_text", "text": "inspect"}},
			},
			{
				"type":  "additional_tools",
				"tools": []map[string]any{{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}},
			},
			{
				"type":      "tool_search_call",
				"call_id":   "ts_1",
				"arguments": map[string]any{"queries": []string{"look"}},
			},
			{
				"type":    "tool_search_output",
				"call_id": "ts_1",
				"tools":   []map[string]any{{"type": "function", "name": "lookup"}},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_orphan",
				"output":  "zombie output",
			},
			{
				"role":    "user",
				"content": "hi",
			},
		}),
	})

	// additional_tools declarations are lifted into the Gemini tools list; the
	// dropped web_search leaves nothing behind while client-executed
	// local_shell lowers to a callable function declaration.
	assert.Equal(t, "local_shell", gjson.GetBytes(got.Tools, "0.functionDeclarations.0.name").String())
	assert.Equal(t, "lookup", gjson.GetBytes(got.Tools, "0.functionDeclarations.1.name").String())
	assert.Len(t, gjson.GetBytes(got.Tools, "0.functionDeclarations").Array(), 2)

	require.Len(t, got.Contents, 3)
	assert.Equal(t, "model", got.Contents[0].Role)
	functionCall := got.Contents[0].Parts[0].FunctionCall
	require.NotNil(t, functionCall)
	assert.Equal(t, "tool_search", functionCall.FunctionName)
	assert.Equal(t, map[string]any{"queries": []any{"look"}}, functionCall.Arguments)

	assert.Equal(t, "user", got.Contents[1].Role)
	functionResponse := got.Contents[1].Parts[0].FunctionResponse
	require.NotNil(t, functionResponse)
	assert.Equal(t, "tool_search", functionResponse.Name)
	require.NotNil(t, functionResponse.Response)

	assert.Equal(t, "user", got.Contents[2].Role)
	assert.Equal(t, "hi", got.Contents[2].Parts[0].Text)
}

func TestConvertOpenAIResponsesRequestToGeminiRejectsLossyToolsAndHistory(t *testing.T) {
	tests := []struct {
		name  string
		tools any
		input any
		want  string
	}{
		{
			name:  "namespace tool without children",
			tools: []map[string]any{{"type": "namespace", "name": "workspace", "tools": []any{}}},
			input: "hello",
			want:  `namespace tool "workspace" has no child tools`,
		},
		{
			name: "hosted history item",
			input: []map[string]any{{
				"type": "web_search_call",
			}},
			want: `input item type "web_search_call" cannot be converted losslessly`,
		},
		{
			name: "unsupported content block",
			input: []map[string]any{{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{{
					"type":    "refusal",
					"refusal": "cannot comply",
				}},
			}},
			want: `content type "refusal" cannot be converted losslessly`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := dto.OpenAIResponsesRequest{
				Model: "gemini-test",
				Input: mustGeminiRawMessage(t, test.input),
			}
			if test.tools != nil {
				request.Tools = mustGeminiRawMessage(t, test.tools)
			}
			info := &relaycommon.RelayInfo{
				OriginModelName: request.Model,
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: request.Model,
				},
			}

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, request)

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func mustConvertResponsesToGemini(t *testing.T, req dto.OpenAIResponsesRequest) *dto.GeminiChatRequest {
	t.Helper()
	info := &relaycommon.RelayInfo{
		OriginModelName: req.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: req.Model,
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, req)
	require.NoError(t, err)
	geminiReq, ok := got.(*dto.GeminiChatRequest)
	require.True(t, ok)
	return geminiReq
}

func mustGeminiRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}
