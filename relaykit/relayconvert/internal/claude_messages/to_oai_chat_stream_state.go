package claudemessages

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

type ClaudeToOpenAIStreamState struct {
	ID       string
	Model    string
	thinking map[int]*dto.ClaudeMediaMessage
}

func NewClaudeToOpenAIStreamState() *ClaudeToOpenAIStreamState {
	return &ClaudeToOpenAIStreamState{thinking: make(map[int]*dto.ClaudeMediaMessage)}
}

func (s *ClaudeToOpenAIStreamState) ConvertChunk(response *dto.ClaudeResponse, info convmeta.Meta) ([]*dto.ChatCompletionsStreamResponse, error) {
	if s == nil || response == nil {
		return nil, nil
	}
	if response.Type == "message_start" && response.Message != nil {
		s.ID = strings.TrimSpace(response.Message.Id)
		s.Model = strings.TrimSpace(response.Message.Model)
	}
	if model := strings.TrimSpace(response.Model); model != "" {
		s.Model = model
	}

	index := response.GetIndex()
	switch response.Type {
	case "content_block_start":
		if response.ContentBlock != nil && (response.ContentBlock.Type == "thinking" || response.ContentBlock.Type == "redacted_thinking") {
			block := *response.ContentBlock
			if response.ContentBlock.Thinking != nil {
				block.Thinking = kitutil.GetPointer(*response.ContentBlock.Thinking)
			}
			s.thinking[index] = &block
			if block.Type == "thinking" && block.Thinking != nil && *block.Thinking != "" {
				return []*dto.ChatCompletionsStreamResponse{s.reasoningChunk(*block.Thinking, "")}, nil
			}
		}
	case "content_block_delta":
		if block := s.thinking[index]; block != nil && response.Delta != nil {
			switch response.Delta.Type {
			case "thinking_delta":
				delta := ""
				if response.Delta.Thinking != nil {
					delta = *response.Delta.Thinking
				}
				current := ""
				if block.Thinking != nil {
					current = *block.Thinking
				}
				block.Thinking = kitutil.GetPointer(current + delta)
			case "signature_delta":
				block.Signature += response.Delta.Signature
			}
		}
	case "content_block_stop":
		block := s.thinking[index]
		delete(s.thinking, index)
		if block != nil && info != nil && info.HasChannelMeta() {
			encoded, ok, err := sharedclaude.EncodeThinkingBlock(*block, info.GetChannelID(), convmeta.OptionsOf(info).ProviderStateSecret)
			if err != nil {
				return nil, err
			}
			if ok {
				return []*dto.ChatCompletionsStreamResponse{s.reasoningChunk("", encoded)}, nil
			}
		}
	}

	converted := StreamResponseClaude2OpenAI(response)
	if converted == nil {
		return nil, nil
	}
	return []*dto.ChatCompletionsStreamResponse{converted}, nil
}

func (s *ClaudeToOpenAIStreamState) reasoningChunk(reasoning string, encryptedContent string) *dto.ChatCompletionsStreamResponse {
	chunk := &dto.ChatCompletionsStreamResponse{
		Id:                        s.ID,
		Object:                    "chat.completion.chunk",
		Model:                     s.Model,
		ReasoningEncryptedContent: encryptedContent,
	}
	if reasoning != "" {
		chunk.Choices = []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ReasoningContent: kitutil.GetPointer(reasoning),
			},
		}}
	}
	return chunk
}
