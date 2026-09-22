package dto

import (
	"encoding/json"
	"strings"
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
		"latest part one\nlatest part two\n\nsystem instruction\n\nold user turn\n\nassistant reply\nvisible assistant reasoning\nassistant refusal\n\ntool argument\n\ntool output",
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
		"latest user turn\n\nsystem instruction\n\nold user turn\n\nassistant reply\nvisible assistant reasoning\n\ntool call text\n\ntool result text",
		PromptAuditText(request),
	)
	assert.NotContains(t, PromptAuditText(request), "BINARY")
	assert.NotContains(t, PromptAuditText(request), "SECRET_SIGNATURE")
	segments := request.GetPromptAuditSnapshot().PrioritizedSegments()
	require.Len(t, segments, 6)
	assert.Equal(t, PromptScopeUser, segments[0].SourceScope())
	assert.Equal(t, PromptScopeToolCall, segments[4].SourceScope())
	assert.Equal(t, PromptScopeToolResult, segments[5].SourceScope())
	assert.False(t, segments[5].User)
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
		"latest user turn\n\nsystem instruction\n\nold user turn\n\nassistant reply\n\ngemini tool argument\n\nvisible thought",
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

func TestPromptAuditSnapshotPreservesClientTagsForModelInspection(t *testing.T) {
	for name, texts := range map[string][]string{
		"plain":       {"今晚打老虎"},
		"prefix":      {"<system-reminder>context</system-reminder> 今晚打老虎"},
		"suffix":      {"今晚打老虎 <system-reminder>context</system-reminder>"},
		"separate":    {"<system-reminder>context</system-reminder>", "今晚打老虎"},
		"inside":      {"<system-reminder>今晚打老虎</system-reminder>"},
		"environment": {"<environment_details>今晚打老虎</environment_details>"},
		"uppercase":   {"<SYSTEM-REMINDER>今晚打老虎</SYSTEM-REMINDER>"},
		"unclosed":    {"<system-reminder>今晚打老虎"},
	} {
		t.Run(name, func(t *testing.T) {
			// Anthropic content blocks are the wire shape agents actually send,
			// and one block is one independently filterable part.
			blocks := make([]any, 0, len(texts))
			for _, text := range texts {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
			request := &ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: blocks}}}
			snapshot := request.GetPromptAuditSnapshot()

			// Keyword track keeps every client-supplied part verbatim.
			keyword := strings.Join(orderedSegmentTexts(snapshot), "\n\n")
			assert.Contains(t, keyword, "今晚打老虎")

			// Tags in client-supplied text cannot exempt any part from inspection.
			semantic := snapshot.SemanticSegments()
			assert.Equal(t, orderedSegmentTexts(snapshot), orderedSegmentTexts(semantic))
		})
	}
}

func TestPromptAuditSnapshotPreservesPairedClientTags(t *testing.T) {
	for name, text := range map[string]string{
		"context paired":       "<context>repo layout</context> 今晚打老虎",
		"file paired":          `<file path="a.go">package main</file> 今晚打老虎`,
		"open and close split": "<context>opening\n</context> 今晚打老虎",
	} {
		t.Run(name, func(t *testing.T) {
			request := &ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": text},
			}}}}
			snapshot := request.GetPromptAuditSnapshot()

			assert.Contains(t, strings.Join(orderedSegmentTexts(snapshot), "\n\n"), "今晚打老虎")
			assert.Equal(t, []string{text}, orderedSegmentTexts(snapshot.SemanticSegments()))
		})
	}
}

func TestPromptAuditSnapshotPairedAgentTagsSurviveAcrossBlocks(t *testing.T) {
	// Two individually clean parts must not combine into a paired tag, otherwise a
	// payload sandwiched between an opener block and a closer block would vanish
	// from model classification while a single-block payload would not.
	request := &ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: []any{
		map[string]any{"type": "text", "text": "<context>"},
		map[string]any{"type": "text", "text": "今晚打老虎"},
		map[string]any{"type": "text", "text": "</context>"},
	}}}}
	snapshot := request.GetPromptAuditSnapshot()

	segments := orderedSegmentTexts(snapshot.SemanticSegments())
	assert.Contains(t, strings.Join(segments, "\n"), "今晚打老虎")
	assert.Contains(t, strings.Join(segments, "\n"), "</context>")
	assert.Equal(t, orderedSegmentTexts(snapshot), segments)
}

func TestPromptAuditSnapshotBareGenericTagKeptInSemanticTrack(t *testing.T) {
	// A lone <context> or </file> must NOT drop the segment: an unpaired opener or
	// a closer before its opener is exactly the shape a client could use to hide a
	// payload from model classification at zero local cost.
	for name, text := range map[string]string{
		"unpaired open":              "<context> 今晚打老虎",
		"unpaired close":             "今晚打老虎 </context>",
		"close before open":          "</context> 今晚打老虎 <context>",
		"bare file open":             "<file 今晚打老虎",
		"filename is not a file tag": "<filename>notes</filename> 今晚打老虎",
	} {
		t.Run(name, func(t *testing.T) {
			request := &ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": text},
			}}}}
			snapshot := request.GetPromptAuditSnapshot()

			assert.Equal(t, []string{text}, orderedSegmentTexts(snapshot.SemanticSegments()))
		})
	}
}

func TestPromptAuditSnapshotSemanticTrackRetainsAllClientParts(t *testing.T) {
	request := &ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: []any{
		map[string]any{"type": "text", "text": "<environment_details>VSCode Linux</environment_details>"},
		map[string]any{"type": "text", "text": "Tell me a story"},
		map[string]any{"type": "text", "text": "<SYSTEM-REMINDER>rules</SYSTEM-REMINDER> another question"},
	}}}}
	snapshot := request.GetPromptAuditSnapshot()

	assert.Equal(t, orderedSegmentTexts(snapshot), orderedSegmentTexts(snapshot.SemanticSegments()))
	assert.Contains(t, strings.Join(orderedSegmentTexts(snapshot), "\n\n"), "another question")
}

func TestPromptAuditSnapshotBlockingSnapshotNarrowsToLatestTurn(t *testing.T) {
	request := &GeneralOpenAIRequest{
		Messages: []Message{
			{Role: "system", Content: "initial system"},
			{Role: "user", Content: "turn 1 question"},
			{Role: "assistant", Content: "turn 1 answer"},
			{Role: "user", Content: "turn 2 question"},
			{Role: "assistant", Content: "turn 2 answer"},
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "turn 3 part one"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,SECRET"},
				map[string]any{"type": "text", "text": "turn 3 part two"},
			}},
		},
	}

	snapshot := request.GetPromptAuditSnapshot()
	require.Len(t, snapshot.OrderedSegments(), 6)

	blocking := snapshot.BlockingSnapshot()
	blockingSegments := blocking.OrderedSegments()
	require.Len(t, blockingSegments, 3)
	assert.Equal(t, []string{"initial system", "turn 2 answer", "turn 3 part one\nturn 3 part two"}, orderedSegmentTexts(blocking))
	assert.Equal(t, "user", blockingSegments[2].Role)
	assert.True(t, blockingSegments[2].User)
	assert.Equal(t, "assistant", blockingSegments[1].Role)
	assert.False(t, blockingSegments[1].User)
}

func TestPromptAuditSnapshotBlockingPreservesNonConversationSourcesAndCurrentToolRound(t *testing.T) {
	snapshot := PromptAuditSnapshot{Segments: []PromptAuditSegment{
		{Role: "system", Text: "system policy"},
		{Role: "developer", Text: "developer policy"},
		{Role: "user", User: true, Text: "old user"},
		{Role: "assistant", Text: "previous answer"},
		{Role: "user", User: true, Text: "latest user"},
		{Role: "assistant", Text: "current reasoning"},
		{Role: "assistant", Scope: PromptScopeToolCall, Text: "tool arguments"},
		{Role: "tool", Scope: PromptScopeToolResult, Text: "tool result"},
		{Role: "task", Scope: PromptScopeTask, User: true, Text: "task prompt"},
	}}
	assert.Equal(t, []string{"system policy", "developer policy", "previous answer", "latest user", "current reasoning", "tool arguments", "tool result", "task prompt"}, orderedSegmentTexts(snapshot.BlockingSnapshot()))
}

func TestPromptAuditSnapshotBlockingSnapshotKeepsFullSnapshotWithoutUserTurn(t *testing.T) {
	request := &GeneralOpenAIRequest{Messages: []Message{
		{Role: "system", Content: "system instruction"},
		{Role: "assistant", Content: "assistant only"},
	}}
	snapshot := request.GetPromptAuditSnapshot()

	blocking := snapshot.BlockingSnapshot()
	assert.Equal(t, orderedSegmentTexts(snapshot), orderedSegmentTexts(blocking))
}

// orderedSegmentTexts collects the text of every segment in wire order so tests
// can compare keyword and semantic tracks without depending on the snapshot
// struct's internal layout.
func orderedSegmentTexts(snapshot PromptAuditSnapshot) []string {
	segments := snapshot.OrderedSegments()
	texts := make([]string, 0, len(segments))
	for _, segment := range segments {
		texts = append(texts, segment.Text)
	}
	return texts
}
