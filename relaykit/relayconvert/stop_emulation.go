package relayconvert

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// This state only changes client-visible output. It never cancels or finalizes
// the upstream stream: the host must drain it and settle the actual usage.
type stopEmulator struct {
	sequences    []string
	pending      string
	matched      string
	claudeBlocks map[int]bool
	logprobs     []dto.TokenLogprob
	logprobText  string
}

func newStopEmulator(protocol Protocol, body []byte) (*stopEmulator, error) {
	var request map[string]any
	if err := kitutil.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	value := request["stop"]
	if protocol == ProtocolMessages {
		value = request["stop_sequences"]
	}
	if protocol == ProtocolGemini {
		config, _ := request["generationConfig"].(map[string]any)
		value = config["stopSequences"]
		if value == nil {
			value = config["stop_sequences"]
		}
	}
	var sequences []string
	if text, ok := value.(string); ok {
		sequences = []string{text}
	} else {
		var err error
		sequences, err = kitutil.Any2Type[[]string](value)
		if err != nil {
			return nil, fmt.Errorf("stop sequences must be strings")
		}
	}
	for _, sequence := range sequences {
		if sequence == "" {
			return nil, fmt.Errorf("stop sequences must not be empty")
		}
	}
	return &stopEmulator{sequences: sequences, claudeBlocks: make(map[int]bool)}, nil
}

func (s *stopEmulator) text(text string, final bool) string {
	if s.matched != "" {
		return ""
	}
	text = s.pending + text
	s.pending = ""
	index := len(text)
	for _, sequence := range s.sequences {
		if found := strings.Index(text, sequence); found >= 0 && found < index {
			index, s.matched = found, sequence
		}
	}
	if s.matched != "" {
		return text[:index]
	}
	if final {
		return text
	}
	hold := 0
	for _, sequence := range s.sequences {
		for size := min(len(sequence)-1, len(text)); size > hold; size-- {
			if strings.HasSuffix(text, sequence[:size]) {
				hold = size
				break
			}
		}
	}
	s.pending = text[len(text)-hold:]
	return text[:len(text)-hold]
}

func (s *stopEmulator) visibleLogprobs(value *any, visibleText string) (*any, error) {
	if value != nil {
		var parsed struct {
			Content []dto.TokenLogprob `json:"content"`
		}
		raw, err := kitutil.Marshal(*value)
		if err != nil {
			return nil, err
		}
		if err := kitutil.Unmarshal(raw, &parsed); err != nil {
			return nil, err
		}
		s.logprobs = append(s.logprobs, parsed.Content...)
	}
	if len(s.logprobs) == 0 {
		s.logprobText = ""
		return nil, nil
	}
	s.logprobText += visibleText
	var visible []dto.TokenLogprob
	for _, token := range s.logprobs {
		text := token.Token
		if len(token.Bytes) > 0 {
			bytes := make([]byte, len(token.Bytes))
			for i, value := range token.Bytes {
				if value < 0 || value > 255 {
					return nil, fmt.Errorf("invalid logprob token byte")
				}
				bytes[i] = byte(value)
			}
			text = string(bytes)
		}
		if len(text) > len(s.logprobText) && strings.HasPrefix(text, s.logprobText) {
			break
		}
		if text == "" || !strings.HasPrefix(s.logprobText, text) {
			// Missing or out-of-order metadata cannot be aligned reliably. Do
			// not expose a token using bytes emitted by an unrelated chunk.
			s.logprobs, s.logprobText = nil, ""
			break
		}
		s.logprobText = s.logprobText[len(text):]
		visible = append(visible, token)
	}
	if len(s.logprobs) > 0 {
		s.logprobs = s.logprobs[len(visible):]
	}
	if len(s.logprobs) == 0 {
		s.logprobText = ""
	}
	if s.matched != "" {
		s.logprobs = nil
		s.logprobText = ""
	}
	if len(visible) == 0 {
		return nil, nil
	}
	var result any = map[string]any{"content": visible}
	return &result, nil
}

func (s *stopEmulator) filterResults(results []ResponseResult) ([]ResponseResult, error) {
	filtered := make([]ResponseResult, 0, len(results))
	for _, result := range results {
		values, err := s.filter(result.Value, true)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			copy := result
			copy.Value = value
			filtered = append(filtered, copy)
		}
	}
	return filtered, nil
}

func (s *stopEmulator) filter(value any, stream bool) ([]any, error) {
	switch response := value.(type) {
	case *dto.OpenAITextResponse:
		copy := *response
		copy.Choices = slices.Clone(response.Choices)
		for i := range copy.Choices {
			choice := &copy.Choices[i]
			text := s.text(choice.Message.StringContent(), true)
			choice.Message.Content = text
			var err error
			choice.Logprobs, err = s.visibleLogprobs(choice.Logprobs, text)
			if err != nil {
				return nil, err
			}
			if s.matched != "" {
				choice.FinishReason = "stop"
				choice.Message.ToolCalls = nil
				choice.Message.FunctionCall = nil
				choice.Message.Annotations = nil
			}
		}
		return []any{&copy}, nil
	case dto.ChatCompletionsStreamResponse:
		return s.filter(&response, stream)
	case *dto.ChatCompletionsStreamResponse:
		copy := *response
		copy.Choices = slices.Clone(response.Choices)
		for i := range copy.Choices {
			choice := &copy.Choices[i]
			alreadyStopped := s.matched != ""
			final := choice.FinishReason != nil || len(choice.Delta.ToolCalls) > 0 || choice.Delta.FunctionCall != nil || choice.Delta.Refusal != nil || choice.Delta.ReasoningContent != nil || choice.Delta.Reasoning != nil
			text := s.text(choice.Delta.GetContentString(), final)
			choice.Delta.Content = nil
			if text != "" {
				choice.Delta.Content = kitutil.GetPointer(text)
			}
			var err error
			choice.Logprobs, err = s.visibleLogprobs(choice.Logprobs, text)
			if err != nil {
				return nil, err
			}
			if s.matched != "" {
				choice.Delta.ToolCalls, choice.Delta.FunctionCall, choice.Delta.Annotations = nil, nil, nil
				if alreadyStopped {
					choice.Delta.ReasoningContent, choice.Delta.Reasoning, choice.Delta.ReasoningDetails, choice.Delta.Refusal = nil, nil, nil, nil
				}
				if choice.FinishReason != nil {
					choice.FinishReason = kitutil.GetPointer("stop")
				}
			}
		}
		return []any{&copy}, nil
	case *dto.ClaudeResponse:
		copy := *response
		if !stream {
			copy.Content = make([]dto.ClaudeMediaMessage, 0, len(response.Content))
			for _, block := range response.Content {
				if s.matched != "" {
					break
				}
				if block.Type == "text" {
					block.Text = kitutil.GetPointer(s.text(block.GetText(), true))
				}
				copy.Content = append(copy.Content, block)
			}
			if s.matched != "" {
				copy.StopReason, copy.StopSequence = "stop_sequence", kitutil.GetPointer(s.matched)
			}
			return []any{&copy}, nil
		}
		index := -1
		if copy.Index != nil {
			index = *copy.Index
		}
		switch copy.Type {
		case "content_block_start":
			if s.matched != "" {
				return nil, nil
			}
			s.claudeBlocks[index] = true
			if copy.ContentBlock != nil && copy.ContentBlock.Type == "text" {
				block := *copy.ContentBlock
				block.Text = kitutil.GetPointer(s.text(block.GetText(), false))
				copy.ContentBlock = &block
			}
		case "content_block_delta":
			if !s.claudeBlocks[index] || s.matched != "" {
				return nil, nil
			}
			if copy.Delta != nil && copy.Delta.Type == "text_delta" {
				delta := *copy.Delta
				text := s.text(delta.GetText(), false)
				if text == "" {
					return nil, nil
				}
				delta.Text, copy.Delta = &text, &delta
			}
		case "content_block_stop":
			if !s.claudeBlocks[index] {
				return nil, nil
			}
			delete(s.claudeBlocks, index)
			if text := s.text("", true); text != "" {
				return []any{&dto.ClaudeResponse{Type: "content_block_delta", Index: copy.Index, Delta: &dto.ClaudeMediaMessage{Type: "text_delta", Text: &text}}, &copy}, nil
			}
		case "message_delta":
			if s.matched != "" && copy.Delta != nil {
				delta := *copy.Delta
				delta.StopReason, delta.StopSequence = kitutil.GetPointer("stop_sequence"), kitutil.GetPointer(s.matched)
				copy.Delta = &delta
			}
		}
		return []any{&copy}, nil
	case *dto.GeminiChatResponse:
		copy := *response
		copy.Candidates = slices.Clone(response.Candidates)
		for i := range copy.Candidates {
			candidate := &copy.Candidates[i]
			parts := make([]dto.GeminiPart, 0, len(candidate.Content.Parts)+1)
			for _, part := range candidate.Content.Parts {
				if s.matched != "" {
					break
				}
				if !part.Thought && part.Text != "" {
					part.Text = s.text(part.Text, !stream)
				} else if text := s.text("", true); text != "" {
					parts = append(parts, dto.GeminiPart{Text: text})
				}
				parts = append(parts, part)
			}
			if candidate.FinishReason != nil {
				if text := s.text("", true); text != "" {
					parts = append(parts, dto.GeminiPart{Text: text})
				}
				if s.matched != "" {
					candidate.FinishReason = kitutil.GetPointer("STOP")
				}
			}
			candidate.Content.Parts = parts
		}
		return []any{&copy}, nil
	default:
		return nil, fmt.Errorf("unsupported stop emulation response %T", value)
	}
}
