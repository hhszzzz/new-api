package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptAuditSnapshotOpenAIIncludesClientContextLatestUserFirst(t *testing.T) {
	reasoning := "visible assistant reasoning"
	refusal := "assistant refusal"
	request := &GeneralOpenAIRequest{Messages: []Message{
		{Role: "system", Content: "system instruction"},
		{Role: "user", Content: "old user turn"},
		{Role: "assistant", Content: "assistant reply", ReasoningContent: &reasoning, Refusal: &refusal, ToolCalls: json.RawMessage(`[{"type":"function","function":{"name":"lookup","arguments":"{\"query\":\"tool argument\",\"metadata\":\"SECRET_METADATA\",\"image\":\"data:image/png;base64,BINARY\"}"}}]`)},
		{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "latest part one"},
			map[string]any{"type": "image_url", "image_url": "data:image/png;base64,SECRET"},
			map[string]any{"type": "text", "text": "latest part two"},
		}},
		{Role: "tool", Content: `{"result":"tool output","metadata":"SECRET_TOOL_METADATA","audio":"data:audio/wav;base64,BINARY"}`},
	}}

	assert.Equal(t,
		"latest part one\nlatest part two\n\nsystem instruction\n\nold user turn\n\nassistant reply\nvisible assistant reasoning\nassistant refusal\ntool argument\n\ntool output",
		PromptAuditText(request),
	)
	for _, excluded := range []string{"base64", "SECRET_METADATA", "SECRET_TOOL_METADATA", "BINARY"} {
		assert.NotContains(t, PromptAuditText(request), excluded)
	}
}

func TestPromptAuditSnapshotClaudeIncludesSystemAssistantAndToolResult(t *testing.T) {
	request := &ClaudeRequest{
		System: "system instruction",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "old user turn"},
			{Role: "assistant", Content: []any{
				map[string]any{"type": "text", "text": "assistant reply"},
				map[string]any{"type": "thinking", "thinking": "visible assistant reasoning", "signature": "SECRET_SIGNATURE"},
				map[string]any{"type": "tool_use", "input": map[string]any{"query": "tool call text", "image_url": "data:image/png;base64,BINARY"}},
			}},
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "latest user turn"},
				map[string]any{"type": "tool_result", "content": "tool result text"},
				map[string]any{"type": "image", "source": map[string]any{"data": "BINARY"}},
			}},
		},
	}

	assert.Equal(t,
		"latest user turn\ntool result text\n\nsystem instruction\n\nold user turn\n\nassistant reply\nvisible assistant reasoning\ntool call text",
		PromptAuditText(request),
	)
	assert.NotContains(t, PromptAuditText(request), "BINARY")
	assert.NotContains(t, PromptAuditText(request), "SECRET_SIGNATURE")
}

func TestPromptAuditSnapshotGeminiIncludesPlaintextThoughtsAndExcludesSignatures(t *testing.T) {
	request := &GeminiChatRequest{
		SystemInstructions: &GeminiChatContent{Parts: []GeminiPart{{Text: "system instruction"}}},
		Contents: []GeminiChatContent{
			{Role: "user", Parts: []GeminiPart{{Text: "old user turn"}}},
			{Role: "model", Parts: []GeminiPart{{
				Text: "assistant reply",
				FunctionCall: &FunctionCall{Arguments: map[string]any{
					"query": "gemini tool argument", "inline_data": map[string]any{"data": "BINARY"},
				}},
			}}},
			{Role: "user", Parts: []GeminiPart{{Text: "latest user turn"}, {Text: "visible thought", Thought: true, ThoughtSignature: json.RawMessage(`"SECRET_SIGNATURE"`)}}},
		},
	}

	assert.Equal(t,
		"latest user turn\nvisible thought\n\nsystem instruction\n\nold user turn\n\nassistant reply\ngemini tool argument",
		PromptAuditText(request),
	)
	assert.NotContains(t, PromptAuditText(request), "SECRET_SIGNATURE")
}

func TestPromptAuditSnapshotResponsesIncludesInstructionsRolesAndToolOutput(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"message","role":"system","content":[{"type":"input_text","text":"system message"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"old user turn"}]},
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"assistant reply"}],"encrypted_content":"SECRET"},
				{"type":"reasoning","summary":[{"type":"summary_text","text":"visible reasoning"}],"encrypted_content":"SECRET_REASONING"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"latest part one"},{"type":"input_image","image_url":"data:image/png;base64,BINARY"},{"type":"input_text","text":"latest part two"}]},
				{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"function argument\",\"metadata\":\"SECRET_METADATA\"}"},
				{"type":"function_call_output","call_id":"call_1","output":"{\"result\":\"tool output\",\"metadata\":\"SECRET_TOOL_METADATA\"}"}
	]`)
	request := &OpenAIResponsesRequest{
		Instructions: json.RawMessage(`"top instructions"`),
		Input:        raw,
		Metadata:     json.RawMessage(`{"private":"METADATA"}`),
		Tools:        json.RawMessage(`[{"description":"TOOL_DEFINITION"}]`),
	}

	assert.Equal(t,
		"latest part one\nlatest part two\n\ntop instructions\n\nsystem message\n\nold user turn\n\nassistant reply\n\nvisible reasoning\n\nfunction argument\n\ntool output",
		PromptAuditText(request),
	)
	for _, excluded := range []string{"BINARY", "SECRET", "METADATA", "TOOL_DEFINITION", "SECRET_REASONING"} {
		assert.NotContains(t, PromptAuditText(request), excluded)
	}
}

func TestPromptAuditSnapshotNoUserPreservesWireOrder(t *testing.T) {
	request := &GeneralOpenAIRequest{Messages: []Message{
		{Role: "system", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "tool", Content: "third"},
	}}
	assert.Equal(t, "first\n\nsecond\n\nthird", PromptAuditText(request))
}

func TestPromptAuditSnapshotNonConversationalInputs(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		want    string
	}{
		{name: "image", request: &ImageRequest{Prompt: "draw a lighthouse"}, want: "draw a lighthouse"},
		{name: "audio", request: &AudioRequest{Input: "speak this", AuditPrompt: "transcription context", Instructions: "sound calm", RefText: json.RawMessage(`"reference voice text"`)}, want: "reference voice text\n\nsound calm\n\nspeak this\n\ntranscription context"},
		{name: "embedding", request: &EmbeddingRequest{Input: []any{"first", "second", 3}}, want: "second\n\nfirst"},
		{name: "rerank", request: &RerankRequest{Query: "needle", Documents: []any{"hay", map[string]any{"text": "stack", "metadata": "ignore"}}}, want: "stack\n\nneedle\n\nhay"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.request)
			assert.Equal(t, tt.want, PromptAuditText(tt.request))
		})
	}
}

func TestPromptAuditSnapshotAdditionalProtocolInputs(t *testing.T) {
	t.Run("alpha search", func(t *testing.T) {
		request := &AlphaSearchRequest{RawBody: json.RawMessage(`{
			"instructions":"search policy",
			"input":[{"role":"user","content":[{"type":"input_text","text":"question"}]}],
			"commands":{"search_query":[{"q":"first query"},{"q":"latest query"}]},
			"metadata":{"private":"SECRET_METADATA"}
		}`)}

		assert.Equal(t, "latest query\n\nsearch policy\n\nquestion\n\nfirst query", PromptAuditText(request))
		assert.NotContains(t, PromptAuditText(request), "SECRET_METADATA")
	})

	t.Run("gemini embedding", func(t *testing.T) {
		request := &GeminiEmbeddingRequest{Content: GeminiChatContent{Parts: []GeminiPart{{Text: "embed one"}, {Text: "embed two"}}}}
		assert.Equal(t, "embed one\nembed two", PromptAuditText(request))
	})

	t.Run("gemini batch embedding", func(t *testing.T) {
		request := &GeminiBatchEmbeddingRequest{Requests: []*GeminiEmbeddingRequest{
			{Content: GeminiChatContent{Parts: []GeminiPart{{Text: "first batch"}}}},
			{Content: GeminiChatContent{Parts: []GeminiPart{{Text: "second batch"}}}},
		}}
		assert.Equal(t, "second batch\n\nfirst batch", PromptAuditText(request))
	})

	t.Run("responses compaction", func(t *testing.T) {
		request := &OpenAIResponsesCompactionRequest{
			Instructions: json.RawMessage(`"compact policy"`),
			Input:        json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"compact this"}]}]`),
			Tools:        json.RawMessage(`[{"description":"SECRET_TOOL_DEFINITION"}]`),
		}
		assert.Equal(t, "compact this\n\ncompact policy", PromptAuditText(request))
		assert.NotContains(t, PromptAuditText(request), "SECRET_TOOL_DEFINITION")
	})
}
