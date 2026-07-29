package geminichat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func UsageFromGeminiMetadata(metadata *dto.GeminiUsageMetadata, fallbackPromptTokens int) *dto.Usage {
	if metadata == nil {
		if fallbackPromptTokens <= 0 {
			return nil
		}
		usage := &dto.Usage{PromptTokens: fallbackPromptTokens}
		usage.PromptTokensDetails.TextTokens = fallbackPromptTokens
		return usage
	}

	promptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	if promptTokens <= 0 && fallbackPromptTokens > 0 {
		promptTokens = fallbackPromptTokens
	}

	usage := &dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
		BillingUsage:     dto.CloneBillingUsage(metadata.BillingUsage),
	}
	if usage.BillingUsage == nil {
		usage.BillingUsage = dto.NewGeminiChatBillingUsage(metadata)
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount

	for _, detail := range metadata.PromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "IMAGE" {
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "IMAGE" {
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}

	if usage.TotalTokens > 0 && usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}

	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}

	return usage
}

func ResponseGeminiChat2OpenAI(id string, created int64, response *dto.GeminiChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	if blockReason := geminiPromptBlockReason(response); len(response.Candidates) == 0 && blockReason != "" {
		refusal := geminiPromptBlockText(blockReason)
		fullTextResponse.Choices = append(fullTextResponse.Choices, dto.OpenAITextResponseChoice{
			Index: 0,
			Message: dto.Message{
				Role:    "assistant",
				Content: "",
				Refusal: &refusal,
			},
			FinishReason: types.FinishReasonContentFilter,
		})
		return &fullTextResponse
	}
	for _, candidate := range response.Candidates {
		choice := dto.OpenAITextResponseChoice{
			Index: int(candidate.Index),
			Message: dto.Message{
				Role:    "assistant",
				Content: "",
			},
			FinishReason: types.FinishReasonStop,
		}
		hasToolCall := false
		allowToolFinish := candidate.FinishReason == nil
		if len(candidate.Content.Parts) > 0 {
			var content strings.Builder
			var reasoning strings.Builder
			var inlineGrow int
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					inlineGrow += len(part.InlineData.MimeType) + len(part.InlineData.Data) + 32
				}
			}
			if inlineGrow > 0 {
				content.Grow(inlineGrow)
			}
			appended := 0
			reasoningParts := 0
			var toolCalls []dto.ToolCallResponse
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					if strings.HasPrefix(part.InlineData.MimeType, "image") {
						if appended > 0 {
							content.WriteByte('\n')
						}
						appended++
						content.WriteString("![image](data:")
						content.WriteString(part.InlineData.MimeType)
						content.WriteString(";base64,")
						content.WriteString(part.InlineData.Data)
						content.WriteByte(')')
					} else {
						if appended > 0 {
							content.WriteByte('\n')
						}
						appended++
						content.WriteString("[media](data:")
						content.WriteString(part.InlineData.MimeType)
						content.WriteString(";base64,")
						content.WriteString(part.InlineData.Data)
						content.WriteByte(')')
					}
				} else if part.FunctionCall != nil {
					choice.FinishReason = types.FinishReasonToolCalls
					if call := geminiResponseToolCall(&part, sharedgemini.SynthesizeToolCallID()); call != nil {
						toolCalls = append(toolCalls, *call)
					}
				} else if part.Thought {
					if part.Text != "" && part.Text != "\n" {
						if reasoningParts > 0 {
							reasoning.WriteByte('\n')
						}
						reasoningParts++
						reasoning.WriteString(part.Text)
					}
				} else {
					if part.ExecutableCode != nil {
						if appended > 0 {
							content.WriteByte('\n')
						}
						appended++
						content.WriteString("```")
						content.WriteString(part.ExecutableCode.Language)
						content.WriteByte('\n')
						content.WriteString(part.ExecutableCode.Code)
						content.WriteString("\n```")
					} else if part.CodeExecutionResult != nil {
						if appended > 0 {
							content.WriteByte('\n')
						}
						appended++
						content.WriteString("```output\n")
						content.WriteString(part.CodeExecutionResult.Output)
						content.WriteString("\n```")
					} else if part.Text != "\n" {
						if appended > 0 {
							content.WriteByte('\n')
						}
						appended++
						content.WriteString(part.Text)
					}
				}
			}
			if len(toolCalls) > 0 {
				choice.Message.SetToolCalls(toolCalls)
				hasToolCall = true
			}
			choice.Message.SetStringContent(content.String())
			if reasoning.Len() > 0 {
				reasoningText := reasoning.String()
				choice.Message.ReasoningContent = &reasoningText
			}
		}
		if candidate.FinishReason != nil {
			choice.FinishReason, allowToolFinish = geminiFinishReasonToChat(*candidate.FinishReason)
			if refusal := geminiRefusalText(*candidate.FinishReason); refusal != "" {
				choice.Message.Refusal = &refusal
			}
		}
		if hasToolCall && allowToolFinish {
			choice.FinishReason = types.FinishReasonToolCalls
		}

		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func StreamResponseGeminiChat2OpenAI(geminiResponse *dto.GeminiChatResponse) (*dto.ChatCompletionsStreamResponse, bool) {
	if blockReason := geminiPromptBlockReason(geminiResponse); len(geminiResponse.Candidates) == 0 && blockReason != "" {
		refusal := geminiPromptBlockText(blockReason)
		finishReason := types.FinishReasonContentFilter
		return &dto.ChatCompletionsStreamResponse{
			Object: "chat.completion.chunk",
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						Refusal: &refusal,
					},
					FinishReason: &finishReason,
				},
			},
		}, false
	}
	choices := make([]dto.ChatCompletionsStreamResponseChoice, 0, len(geminiResponse.Candidates))
	isStop := false
	for _, candidate := range geminiResponse.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason == "STOP" {
			isStop = true
			candidate.FinishReason = nil
		}
		choice := dto.ChatCompletionsStreamResponseChoice{
			Index: int(candidate.Index),
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{},
		}
		var content strings.Builder
		var reasoning strings.Builder
		var inlineGrow int
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				inlineGrow += len(part.InlineData.MimeType) + len(part.InlineData.Data) + 32
			}
		}
		if inlineGrow > 0 {
			content.Grow(inlineGrow)
		}
		appended := 0
		reasoningParts := 0
		isTools := false
		allowToolFinish := candidate.FinishReason == nil
		if candidate.FinishReason != nil {
			finishReason, allowed := geminiFinishReasonToChat(*candidate.FinishReason)
			allowToolFinish = allowed
			choice.FinishReason = &finishReason
			if refusal := geminiRefusalText(*candidate.FinishReason); refusal != "" {
				choice.Delta.Refusal = &refusal
			}
		}
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				if strings.HasPrefix(part.InlineData.MimeType, "image") {
					if appended > 0 {
						content.WriteByte('\n')
					}
					appended++
					content.WriteString("![image](data:")
					content.WriteString(part.InlineData.MimeType)
					content.WriteString(";base64,")
					content.WriteString(part.InlineData.Data)
					content.WriteByte(')')
				}
			} else if part.FunctionCall != nil {
				isTools = true
				fallbackID := sharedgemini.SynthesizeToolCallID()
				if call := geminiResponseToolCall(&part, fallbackID); call != nil {
					call.SetIndex(len(choice.Delta.ToolCalls))
					choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, *call)
				}
			} else if part.Thought {
				if part.Text != "" && part.Text != "\n" {
					if reasoningParts > 0 {
						reasoning.WriteByte('\n')
					}
					reasoningParts++
					reasoning.WriteString(part.Text)
				}
			} else {
				if part.ExecutableCode != nil {
					if appended > 0 {
						content.WriteByte('\n')
					}
					appended++
					content.WriteString("```")
					content.WriteString(part.ExecutableCode.Language)
					content.WriteByte('\n')
					content.WriteString(part.ExecutableCode.Code)
					content.WriteString("\n```\n")
				} else if part.CodeExecutionResult != nil {
					if appended > 0 {
						content.WriteByte('\n')
					}
					appended++
					content.WriteString("```output\n")
					content.WriteString(part.CodeExecutionResult.Output)
					content.WriteString("\n```\n")
				} else if part.Text != "\n" {
					if appended > 0 {
						content.WriteByte('\n')
					}
					appended++
					content.WriteString(part.Text)
				}
			}
		}
		if reasoning.Len() > 0 {
			choice.Delta.SetReasoningContent(reasoning.String())
		}
		if content.Len() > 0 {
			choice.Delta.SetContentString(content.String())
		}
		if isTools && allowToolFinish {
			choice.FinishReason = &types.FinishReasonToolCalls
		}
		choices = append(choices, choice)
	}

	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Choices: choices,
	}
	return &response, isStop
}

type GeminiToChatStreamState struct {
	id            string
	created       int64
	sawToolCall   bool
	finishEmitted bool
	latestUsage   *dto.Usage
	toolCalls     map[int]map[int]*geminiStreamToolState
	content       map[int]string
	reasoning     map[int]string
	refusals      map[int]string
}

type geminiStreamToolState struct {
	id        string
	name      string
	arguments string
}

func NewGeminiToChatStreamState(id string, created int64) *GeminiToChatStreamState {
	id = strings.TrimSpace(id)
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%s", kitutil.GetUUID())
	}
	if created == 0 {
		created = kitutil.GetTimestamp()
	}
	return &GeminiToChatStreamState{
		id:        id,
		created:   created,
		toolCalls: make(map[int]map[int]*geminiStreamToolState),
		content:   make(map[int]string),
		reasoning: make(map[int]string),
		refusals:  make(map[int]string),
	}
}

func (s *GeminiToChatStreamState) ConvertChunk(geminiResponse *dto.GeminiChatResponse, model string, usage *dto.Usage) []*dto.ChatCompletionsStreamResponse {
	if s == nil || geminiResponse == nil {
		return nil
	}
	hasNonStopFinish := false
	for _, candidate := range geminiResponse.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason != "" && *candidate.FinishReason != "STOP" {
			hasNonStopFinish = true
			break
		}
	}
	response, isStop := StreamResponseGeminiChat2OpenAI(geminiResponse)
	if response == nil {
		return nil
	}
	response.Id = s.id
	response.Created = s.created
	response.Model = model
	response.Usage = usage
	s.stabilizeToolCallIDs(geminiResponse, response)
	s.stabilizeTextDeltas(response)

	if response.IsToolCall() {
		s.sawToolCall = true
		if !hasNonStopFinish {
			for i := range response.Choices {
				if response.Choices[i].FinishReason != nil && *response.Choices[i].FinishReason == types.FinishReasonToolCalls {
					response.Choices[i].FinishReason = nil
				}
			}
		}
	}
	if usage != nil {
		s.latestUsage = usage
	}
	for _, choice := range response.Choices {
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			s.finishEmitted = true
			break
		}
	}

	responses := []*dto.ChatCompletionsStreamResponse{response}
	if isStop && !s.finishEmitted {
		responses = append(responses, s.terminalChunk(model))
	}
	return responses
}

func (s *GeminiToChatStreamState) Finalize(model string) []*dto.ChatCompletionsStreamResponse {
	if s == nil || s.finishEmitted {
		return nil
	}
	return []*dto.ChatCompletionsStreamResponse{s.terminalChunk(model)}
}

func (s *GeminiToChatStreamState) Usage() *dto.Usage {
	if s == nil {
		return nil
	}
	return s.latestUsage
}

func (s *GeminiToChatStreamState) terminalChunk(model string) *dto.ChatCompletionsStreamResponse {
	finishReason := types.FinishReasonStop
	if s.sawToolCall {
		finishReason = types.FinishReasonToolCalls
	}
	s.finishEmitted = true
	return &dto.ChatCompletionsStreamResponse{
		Id:      s.id,
		Object:  "chat.completion.chunk",
		Created: s.created,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{},
				FinishReason: &finishReason,
			},
		},
		Usage: s.latestUsage,
	}
}

func (s *GeminiToChatStreamState) stabilizeToolCallIDs(geminiResponse *dto.GeminiChatResponse, response *dto.ChatCompletionsStreamResponse) {
	if s == nil || geminiResponse == nil || response == nil {
		return
	}
	candidates := make(map[int64]dto.GeminiChatCandidate, len(geminiResponse.Candidates))
	for _, candidate := range geminiResponse.Candidates {
		candidates[candidate.Index] = candidate
	}
	for choiceIndex := range response.Choices {
		choice := &response.Choices[choiceIndex]
		candidate := candidates[int64(choice.Index)]
		upstreamIDs := make([]string, 0, len(choice.Delta.ToolCalls))
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				upstreamIDs = append(upstreamIDs, strings.TrimSpace(part.FunctionCall.ID))
			}
		}
		toolsBySlot := s.toolCalls[choice.Index]
		if toolsBySlot == nil {
			toolsBySlot = make(map[int]*geminiStreamToolState)
			s.toolCalls[choice.Index] = toolsBySlot
		}
		for toolPosition := range choice.Delta.ToolCalls {
			tool := &choice.Delta.ToolCalls[toolPosition]
			slot := toolPosition
			if tool.Index != nil && *tool.Index >= 0 {
				slot = *tool.Index
			}
			upstreamID := ""
			if toolPosition < len(upstreamIDs) {
				upstreamID = upstreamIDs[toolPosition]
			}
			toolState := toolsBySlot[slot]
			if toolState == nil || (upstreamID != "" && toolState.id != upstreamID) {
				stableID := upstreamID
				if stableID == "" {
					stableID = sharedgemini.SynthesizeToolCallID()
				}
				toolState = &geminiStreamToolState{
					id:        stableID,
					name:      tool.Function.Name,
					arguments: tool.Function.Arguments,
				}
				toolsBySlot[slot] = toolState
			} else {
				if tool.Function.Name == toolState.name {
					tool.Function.Name = ""
				} else if tool.Function.Name != "" {
					toolState.name = tool.Function.Name
				}
				if tool.Function.Arguments == toolState.arguments {
					tool.Function.Arguments = ""
				} else if strings.HasPrefix(tool.Function.Arguments, toolState.arguments) {
					delta := strings.TrimPrefix(tool.Function.Arguments, toolState.arguments)
					toolState.arguments = tool.Function.Arguments
					tool.Function.Arguments = delta
				} else if tool.Function.Arguments != "" {
					toolState.arguments = tool.Function.Arguments
				}
			}
			tool.ID = toolState.id
			tool.SetIndex(slot)
		}
	}
}

func (s *GeminiToChatStreamState) stabilizeTextDeltas(response *dto.ChatCompletionsStreamResponse) {
	if s == nil || response == nil {
		return
	}
	for index := range response.Choices {
		choice := &response.Choices[index]
		choiceIndex := choice.Index

		if content := choice.Delta.GetContentString(); content != "" {
			delta, accumulated := GeminiStreamDelta(s.content[choiceIndex], content)
			s.content[choiceIndex] = accumulated
			if delta == "" {
				choice.Delta.Content = nil
			} else {
				choice.Delta.SetContentString(delta)
			}
		}

		if reasoning := choice.Delta.GetReasoningContent(); reasoning != "" {
			delta, accumulated := GeminiStreamDelta(s.reasoning[choiceIndex], reasoning)
			s.reasoning[choiceIndex] = accumulated
			if delta == "" {
				choice.Delta.ReasoningContent = nil
				choice.Delta.Reasoning = nil
				choice.Delta.ReasoningDetails = nil
			} else {
				choice.Delta.SetReasoningContent(delta)
			}
		}

		if refusal := choice.Delta.GetRefusalContent(); refusal != "" {
			delta, accumulated := GeminiStreamDelta(s.refusals[choiceIndex], refusal)
			s.refusals[choiceIndex] = accumulated
			if delta == "" {
				choice.Delta.Refusal = nil
			} else {
				choice.Delta.Refusal = &delta
			}
		}
	}
}

// GeminiStreamDelta follows CC Switch's Gemini stream handling: chunks are
// treated as cumulative snapshots when the incoming text extends everything
// accumulated so far, and as incremental deltas otherwise.
func GeminiStreamDelta(accumulated string, current string) (string, string) {
	if strings.HasPrefix(current, accumulated) {
		return strings.TrimPrefix(current, accumulated), current
	}
	return current, accumulated + current
}

func geminiResponseToolCall(item *dto.GeminiPart, fallbackID string) *dto.ToolCallResponse {
	argsBytes, err := kitutil.Marshal(item.FunctionCall.Arguments)
	if err != nil {
		return nil
	}
	callID := strings.TrimSpace(item.FunctionCall.ID)
	if callID == "" {
		callID = fallbackID
	}
	return &dto.ToolCallResponse{
		ID:   callID,
		Type: "function",
		Function: dto.FunctionResponse{
			Arguments: string(argsBytes),
			Name:      item.FunctionCall.FunctionName,
		},
	}
}

func geminiFinishReasonToChat(reason string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "", "STOP":
		return types.FinishReasonStop, true
	case "MAX_TOKENS":
		return types.FinishReasonLength, false
	default:
		return types.FinishReasonContentFilter, false
	}
}

func geminiRefusalText(reason string) string {
	switch normalized := strings.ToUpper(strings.TrimSpace(reason)); normalized {
	case "", "STOP", "MAX_TOKENS":
		return ""
	case "SAFETY":
		return "Gemini blocked the response for safety reasons."
	case "RECITATION":
		return "Gemini blocked the response because of recitation concerns."
	case "BLOCKLIST":
		return "Gemini blocked the response because it matched a blocklist."
	case "PROHIBITED_CONTENT":
		return "Gemini blocked the response because it contained prohibited content."
	case "SPII":
		return "Gemini blocked the response because it may contain sensitive personal information."
	case "OTHER":
		return "Gemini blocked the response for an unspecified reason."
	default:
		return fmt.Sprintf("Gemini ended the response with finish reason %s.", normalized)
	}
}

func geminiPromptBlockReason(response *dto.GeminiChatResponse) string {
	if response == nil || response.PromptFeedback == nil || response.PromptFeedback.BlockReason == nil {
		return ""
	}
	return strings.TrimSpace(*response.PromptFeedback.BlockReason)
}

func geminiPromptBlockText(reason string) string {
	return fmt.Sprintf("Request blocked by Gemini safety filters: %s", strings.TrimSpace(reason))
}
