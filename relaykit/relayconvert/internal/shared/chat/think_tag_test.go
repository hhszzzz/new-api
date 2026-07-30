package chat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func splitAll(t *testing.T, chunks []string) (string, string) {
	t.Helper()
	splitter := &ThinkTagSplitter{}
	var reasoning, text string
	for _, chunk := range chunks {
		r, x := splitter.Process(chunk)
		reasoning += r
		text += x
	}
	r, x := splitter.Flush()
	return reasoning + r, text + x
}

func TestThinkTagSplitterStreaming(t *testing.T) {
	tests := []struct {
		name          string
		chunks        []string
		wantReasoning string
		wantText      string
	}{
		{
			name:          "tag split across chunks",
			chunks:        []string{"<thi", "nk>let me", " reason</th", "ink>\n\nanswer"},
			wantReasoning: "let me reason",
			wantText:      "answer",
		},
		{
			name:          "plain content passes through",
			chunks:        []string{"hello", " world"},
			wantReasoning: "",
			wantText:      "hello world",
		},
		{
			name:          "leading whitespace before tag",
			chunks:        []string{"\n <think>a</think>b"},
			wantReasoning: "a",
			wantText:      "b",
		},
		{
			name:          "angle bracket text is not swallowed",
			chunks:        []string{"<thanks for asking"},
			wantReasoning: "",
			wantText:      "<thanks for asking",
		},
		{
			name:          "unterminated think flushes as reasoning",
			chunks:        []string{"<think>never closed"},
			wantReasoning: "never closed",
			wantText:      "",
		},
		{
			name:          "unresolved tag prefix flushes as text",
			chunks:        []string{"<thin"},
			wantReasoning: "",
			wantText:      "<thin",
		},
		{
			name:          "close tag lookalike stays in reasoning",
			chunks:        []string{"<think>a </thing> b</think>c"},
			wantReasoning: "a </thing> b",
			wantText:      "c",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reasoning, text := splitAll(t, test.chunks)
			assert.Equal(t, test.wantReasoning, reasoning)
			assert.Equal(t, test.wantText, text)
		})
	}
}

func TestThinkTagSplitterDisablesOnNativeReasoning(t *testing.T) {
	splitter := &ThinkTagSplitter{}
	splitter.DisableOnNativeReasoning()
	reasoning, text := splitter.Process("<think>not a tag anymore</think>x")
	assert.Empty(t, reasoning)
	assert.Equal(t, "<think>not a tag anymore</think>x", text)
}

func TestSplitThinkTagStreamDeltaRewritesDelta(t *testing.T) {
	splitter := &ThinkTagSplitter{}
	delta := &dto.ChatCompletionsStreamResponseChoiceDelta{}
	delta.SetContentString("<think>why</think>because")

	SplitThinkTagStreamDelta(splitter, delta, false)

	assert.Equal(t, "why", delta.GetReasoningContent())
	assert.Equal(t, "because", delta.GetContentString())
}

func TestSplitThinkTagStreamDeltaKeepsNativeReasoningUntouched(t *testing.T) {
	splitter := &ThinkTagSplitter{}
	delta := &dto.ChatCompletionsStreamResponseChoiceDelta{}
	delta.SetReasoningContent("native")
	delta.SetContentString("<think>text")

	SplitThinkTagStreamDelta(splitter, delta, false)

	assert.Equal(t, "native", delta.GetReasoningContent())
	assert.Equal(t, "<think>text", delta.GetContentString())
}

func TestSplitThinkTagStreamDeltaFlushesOnFinish(t *testing.T) {
	splitter := &ThinkTagSplitter{}
	delta := &dto.ChatCompletionsStreamResponseChoiceDelta{}
	delta.SetContentString("<think>tail")

	SplitThinkTagStreamDelta(splitter, delta, true)

	assert.Equal(t, "tail", delta.GetReasoningContent())
	assert.Empty(t, delta.GetContentString())
}

func TestSplitThinkTagText(t *testing.T) {
	reasoning, text, ok := SplitThinkTagText("<think> plan it </think>\n\nfinal answer")
	require.True(t, ok)
	assert.Equal(t, "plan it", reasoning)
	assert.Equal(t, "final answer", text)

	_, text, ok = SplitThinkTagText("no tags here")
	assert.False(t, ok)
	assert.Equal(t, "no tags here", text)

	_, text, ok = SplitThinkTagText("<think>unterminated")
	assert.False(t, ok)
	assert.Equal(t, "<think>unterminated", text)
}
