package chat

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	thinkOpenTag  = "<think>"
	thinkCloseTag = "</think>"
)

const (
	thinkModeUndecided = iota
	thinkModePassthrough
	thinkModeInThink
	thinkModeAfterThink
)

// ThinkTagSplitter incrementally lifts a leading <think>...</think> block out
// of chat content into reasoning. Bare vLLM/Ollama-style deployments of
// DeepSeek-R1-family models emit reasoning inline this way instead of using
// reasoning_content; without the split, bridged Messages/Responses clients
// would render the raw tags as visible answer text.
type ThinkTagSplitter struct {
	mode        int
	buffer      string
	textStarted bool
}

// DisableOnNativeReasoning turns the splitter into a passthrough when the
// upstream demonstrates it uses reasoning_content natively, but only while no
// content has committed to a think block.
func (s *ThinkTagSplitter) DisableOnNativeReasoning() {
	if s.mode == thinkModeUndecided && s.buffer == "" {
		s.mode = thinkModePassthrough
	}
}

// Process consumes one content chunk and returns the reasoning and answer
// text portions that are safe to emit now; either may be empty while the
// splitter buffers an ambiguous tag prefix.
func (s *ThinkTagSplitter) Process(chunk string) (string, string) {
	switch s.mode {
	case thinkModePassthrough:
		return "", chunk
	case thinkModeAfterThink:
		return "", s.afterThinkText(chunk)
	case thinkModeInThink:
		return s.consumeThink(chunk)
	default:
		s.buffer += chunk
		trimmed := strings.TrimLeft(s.buffer, " \t\r\n")
		if trimmed == "" || (len(trimmed) < len(thinkOpenTag) && strings.HasPrefix(thinkOpenTag, trimmed)) {
			return "", ""
		}
		if !strings.HasPrefix(trimmed, thinkOpenTag) {
			out := s.buffer
			s.buffer = ""
			s.mode = thinkModePassthrough
			return "", out
		}
		rest := trimmed[len(thinkOpenTag):]
		s.buffer = ""
		s.mode = thinkModeInThink
		return s.consumeThink(rest)
	}
}

// Flush drains whatever the splitter is still buffering at stream end: an
// unterminated think block becomes reasoning, an unresolved "<thin"-style
// prefix becomes answer text.
func (s *ThinkTagSplitter) Flush() (string, string) {
	buffered := s.buffer
	s.buffer = ""
	switch s.mode {
	case thinkModeInThink:
		s.mode = thinkModeAfterThink
		return buffered, ""
	case thinkModeUndecided:
		s.mode = thinkModePassthrough
		return "", buffered
	default:
		return "", ""
	}
}

func (s *ThinkTagSplitter) consumeThink(chunk string) (string, string) {
	s.buffer += chunk
	if index := strings.Index(s.buffer, thinkCloseTag); index >= 0 {
		reasoning := s.buffer[:index]
		text := s.buffer[index+len(thinkCloseTag):]
		s.buffer = ""
		s.mode = thinkModeAfterThink
		return reasoning, s.afterThinkText(text)
	}
	keep := partialThinkCloseSuffix(s.buffer)
	reasoning := s.buffer[:len(s.buffer)-keep]
	s.buffer = s.buffer[len(s.buffer)-keep:]
	return reasoning, ""
}

// afterThinkText strips the whitespace separator between the closing tag and
// the first real answer character, then passes everything else through.
func (s *ThinkTagSplitter) afterThinkText(text string) string {
	if s.textStarted {
		return text
	}
	text = strings.TrimLeft(text, " \t\r\n")
	if text != "" {
		s.textStarted = true
	}
	return text
}

// partialThinkCloseSuffix returns the length of the longest suffix of s that
// is a proper prefix of "</think>", i.e. the tail that must stay buffered
// because the closing tag may be split across chunks.
func partialThinkCloseSuffix(s string) int {
	max := len(thinkCloseTag) - 1
	if len(s) < max {
		max = len(s)
	}
	for k := max; k > 0; k-- {
		if strings.HasSuffix(s, thinkCloseTag[:k]) {
			return k
		}
	}
	return 0
}

// SplitThinkTagStreamDelta rewrites one bridged stream delta in place, moving
// leading think-tag content into reasoning_content. finished marks the chunk
// carrying the finish reason, where any buffered remainder must drain.
func SplitThinkTagStreamDelta(splitter *ThinkTagSplitter, delta *dto.ChatCompletionsStreamResponseChoiceDelta, finished bool) {
	if splitter == nil || delta == nil {
		return
	}
	if delta.GetReasoningContent() != "" {
		splitter.DisableOnNativeReasoning()
		return
	}
	original := delta.GetContentString()
	reasoning, text := splitter.Process(original)
	if finished {
		flushedReasoning, flushedText := splitter.Flush()
		reasoning += flushedReasoning
		text += flushedText
	}
	if reasoning == "" && text == original {
		return
	}
	if reasoning != "" {
		delta.SetReasoningContent(reasoning)
	}
	delta.SetContentString(text)
}

// SplitThinkTagText extracts a complete leading <think> block from non-stream
// content. An absent or unterminated block returns ok=false and the text
// untouched.
func SplitThinkTagText(text string) (string, string, bool) {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(trimmed, thinkOpenTag) {
		return "", text, false
	}
	body := trimmed[len(thinkOpenTag):]
	index := strings.Index(body, thinkCloseTag)
	if index < 0 {
		return "", text, false
	}
	answer := strings.TrimLeft(body[index+len(thinkCloseTag):], " \t\r\n")
	return strings.TrimSpace(body[:index]), answer, true
}
