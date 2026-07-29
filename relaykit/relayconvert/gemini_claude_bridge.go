package relayconvert

// Gemini generateContent → Claude Messages is a composite bridge: it reuses the
// Gemini→Chat and Chat→Claude converters for the envelope and then rebuilds
// content from the Gemini parts directly (thought filtering, cumulative tool
// snapshots). It lives at the registry layer because only this layer may see
// every format; internal/<format> packages deliberately do not import each
// other.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	geminichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/gemini_chat"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

type geminiToClaudeStreamState struct {
	id              string
	model           string
	started         bool
	textStarted     bool
	textIndex       int
	nextIndex       int
	accumulatedText string
	tools           []geminiToClaudeToolSnapshot
	finishReason    string
	blockedText     string
	latestUsage     *dto.Usage
	done            bool
}

type geminiToClaudeToolSnapshot struct {
	id        string
	name      string
	arguments string
}

func convertGeminiChatResponseToClaudeMessages(c context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	geminiResponse, err := asGeminiChatResponse(response)
	if err != nil {
		return nil, nil, err
	}

	chatValue, usage, err := convertGeminiChatResponseToOAIChat(c, info, geminiResponse)
	if err != nil {
		return nil, nil, err
	}
	claudeValue, _, err := convertOAIChatResponseToClaudeMessages(c, info, chatValue)
	if err != nil {
		return nil, nil, err
	}
	claudeResponse := claudeValue.(*dto.ClaudeResponse)

	if geminiResponseBlockReason(geminiResponse) != "" {
		return claudeResponse, usage, nil
	}
	if len(geminiResponse.Candidates) == 0 {
		return nil, nil, errors.New("Gemini response has no candidates")
	}

	candidate := geminiResponse.Candidates[0]
	content := make([]dto.ClaudeMediaMessage, 0, len(candidate.Content.Parts))
	usedToolIDs := make(map[string]struct{})
	hasToolUse := false
	for _, part := range candidate.Content.Parts {
		if part.Thought {
			continue
		}
		if part.Text != "" {
			content = append(content, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer(part.Text)})
			continue
		}
		if part.FunctionCall == nil {
			continue
		}

		hasToolUse = true
		toolID := strings.TrimSpace(part.FunctionCall.ID)
		if toolID == "" {
			toolID = sharedgemini.SynthesizeToolCallID()
		}
		if _, exists := usedToolIDs[toolID]; exists {
			toolID = sharedgemini.SynthesizeToolCallID()
		}
		usedToolIDs[toolID] = struct{}{}
		input := part.FunctionCall.Arguments
		if input == nil {
			input = map[string]interface{}{}
		}
		content = append(content, dto.ClaudeMediaMessage{
			Type:  "tool_use",
			Id:    toolID,
			Name:  part.FunctionCall.FunctionName,
			Input: input,
		})
	}

	claudeResponse.Content = content
	claudeResponse.StopReason = geminiFinishReasonToClaude(candidate.FinishReason, hasToolUse, false)
	return claudeResponse, usage, nil
}

func newGeminiToClaudeStreamState(options ResponseStreamOptions) any {
	id := strings.TrimSpace(options.ID)
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%s", kitutil.GetUUID())
	}
	return &geminiToClaudeStreamState{
		id:    id,
		model: strings.TrimSpace(options.Model),
	}
}

func convertGeminiChatStreamResponseChunkToClaudeMessages(c context.Context, info convmeta.Meta, response any, state any) ([]any, *dto.Usage, error) {
	geminiResponse, err := asGeminiChatResponse(response)
	if err != nil {
		return nil, nil, err
	}
	streamState, ok := state.(*geminiToClaudeStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("Gemini chat to Claude Messages stream state is required")
	}
	if streamState.done {
		return nil, streamState.latestUsage, nil
	}

	usage := UsageFromGeminiMetadata(geminiResponse.GetUsageMetadata(), fallbackPromptTokens(info))
	if usage != nil {
		usage = UsageFromChatUsage(usage)
		streamState.latestUsage = usage
	}
	if model := publicResponseModelName(info); model != "" {
		streamState.model = model
	}

	responses := make([]*dto.ClaudeResponse, 0, 3)
	if !streamState.started {
		message := &dto.ClaudeMediaMessage{
			Id:    streamState.id,
			Type:  "message",
			Role:  "assistant",
			Model: streamState.model,
			Usage: geminiClaudeUsage(usage),
		}
		message.SetContent(make([]any, 0))
		responses = append(responses, &dto.ClaudeResponse{Type: "message_start", Message: message})
		streamState.started = true
	}

	if blockReason := geminiResponseBlockReason(geminiResponse); blockReason != "" {
		streamState.blockedText = fmt.Sprintf("Request blocked by Gemini safety filters: %s", blockReason)
	}

	if len(geminiResponse.Candidates) > 0 {
		candidate := geminiResponse.Candidates[0]
		if candidate.FinishReason != nil && strings.TrimSpace(*candidate.FinishReason) != "" {
			streamState.finishReason = *candidate.FinishReason
		}

		var visibleText strings.Builder
		toolSlot := 0
		for _, part := range candidate.Content.Parts {
			if !part.Thought {
				visibleText.WriteString(part.Text)
			}
			if part.FunctionCall == nil {
				continue
			}

			// CC Switch semantics: each chunk is a cumulative snapshot of
			// content.parts, so tool calls are matched by position within the
			// snapshot; a later snapshot updates the same slot in place.
			for len(streamState.tools) <= toolSlot {
				streamState.tools = append(streamState.tools, geminiToClaudeToolSnapshot{id: sharedgemini.SynthesizeToolCallID()})
			}
			tool := &streamState.tools[toolSlot]
			if toolID := strings.TrimSpace(part.FunctionCall.ID); toolID != "" {
				tool.id = toolID
			}
			tool.name = part.FunctionCall.FunctionName
			arguments, marshalErr := kitutil.Marshal(part.FunctionCall.Arguments)
			if marshalErr != nil {
				return nil, nil, marshalErr
			}
			tool.arguments = string(arguments)
			toolSlot++
		}

		delta, accumulated := geminichat.GeminiStreamDelta(streamState.accumulatedText, visibleText.String())
		streamState.accumulatedText = accumulated
		if delta != "" {
			if !streamState.textStarted {
				streamState.textIndex = streamState.nextIndex
				streamState.nextIndex++
				streamState.textStarted = true
				index := streamState.textIndex
				responses = append(responses, &dto.ClaudeResponse{
					Type:  "content_block_start",
					Index: &index,
					ContentBlock: &dto.ClaudeMediaMessage{
						Type: "text",
						Text: kitutil.GetPointer(""),
					},
				})
			}
			index := streamState.textIndex
			responses = append(responses, &dto.ClaudeResponse{
				Type:  "content_block_delta",
				Index: &index,
				Delta: &dto.ClaudeMediaMessage{
					Type: "text_delta",
					Text: &delta,
				},
			})
		}
	}

	if streamState.blockedText != "" || (streamState.finishReason != "" && streamState.latestUsage != nil) {
		finalized, finalUsage, finalizeErr := finalizeGeminiChatStreamResponseToClaudeMessages(c, info, streamState)
		if finalizeErr != nil {
			return nil, nil, finalizeErr
		}
		for _, value := range finalized {
			claudeResponse, ok := value.(*dto.ClaudeResponse)
			if !ok {
				return nil, nil, fmt.Errorf("expected Claude stream response, got %T", value)
			}
			responses = append(responses, claudeResponse)
		}
		if finalUsage != nil {
			usage = finalUsage
		}
	}

	return streamValuesFromAny(responses), usage, nil
}

func finalizeGeminiChatStreamResponseToClaudeMessages(_ context.Context, info convmeta.Meta, state any) ([]any, *dto.Usage, error) {
	streamState, ok := state.(*geminiToClaudeStreamState)
	if !ok || streamState == nil {
		return nil, nil, errors.New("Gemini chat to Claude Messages stream state is required")
	}
	if streamState.done {
		return nil, streamState.latestUsage, nil
	}

	responses := make([]*dto.ClaudeResponse, 0, len(streamState.tools)*3+5)
	if !streamState.started {
		message := &dto.ClaudeMediaMessage{
			Id:    streamState.id,
			Type:  "message",
			Role:  "assistant",
			Model: streamState.model,
			Usage: geminiClaudeUsage(streamState.latestUsage),
		}
		message.SetContent(make([]any, 0))
		responses = append(responses, &dto.ClaudeResponse{Type: "message_start", Message: message})
		streamState.started = true
	}

	if streamState.accumulatedText == "" && streamState.blockedText != "" {
		streamState.textIndex = streamState.nextIndex
		streamState.nextIndex++
		streamState.textStarted = true
		index := streamState.textIndex
		blockedText := streamState.blockedText
		responses = append(responses,
			&dto.ClaudeResponse{
				Type:  "content_block_start",
				Index: &index,
				ContentBlock: &dto.ClaudeMediaMessage{
					Type: "text",
					Text: kitutil.GetPointer(""),
				},
			},
			&dto.ClaudeResponse{
				Type:  "content_block_delta",
				Index: &index,
				Delta: &dto.ClaudeMediaMessage{
					Type: "text_delta",
					Text: &blockedText,
				},
			},
		)
	}
	if streamState.textStarted {
		index := streamState.textIndex
		responses = append(responses, &dto.ClaudeResponse{Type: "content_block_stop", Index: &index})
	}

	for _, tool := range streamState.tools {
		index := streamState.nextIndex
		streamState.nextIndex++
		arguments := tool.arguments
		if arguments == "" || arguments == "null" {
			arguments = "{}"
		}
		responses = append(responses,
			&dto.ClaudeResponse{
				Type:  "content_block_start",
				Index: &index,
				ContentBlock: &dto.ClaudeMediaMessage{
					Type:  "tool_use",
					Id:    tool.id,
					Name:  tool.name,
					Input: map[string]interface{}{},
				},
			},
			&dto.ClaudeResponse{
				Type:  "content_block_delta",
				Index: &index,
				Delta: &dto.ClaudeMediaMessage{
					Type:        "input_json_delta",
					PartialJson: &arguments,
				},
			},
			&dto.ClaudeResponse{Type: "content_block_stop", Index: &index},
		)
	}

	stopReason := geminiFinishReasonToClaude(nil, len(streamState.tools) > 0, streamState.blockedText != "")
	if streamState.finishReason != "" {
		stopReason = geminiFinishReasonToClaude(&streamState.finishReason, len(streamState.tools) > 0, streamState.blockedText != "")
	}
	responses = append(responses,
		&dto.ClaudeResponse{
			Type:  "message_delta",
			Usage: geminiClaudeUsage(streamState.latestUsage),
			Delta: &dto.ClaudeMediaMessage{StopReason: &stopReason},
		},
		&dto.ClaudeResponse{Type: "message_stop"},
	)
	streamState.done = true
	if info != nil {
		claudeInfo := info.EnsureClaudeConvertInfo()
		claudeInfo.Usage = streamState.latestUsage
		claudeInfo.Done = true
	}
	return streamValuesFromAny(responses), streamState.latestUsage, nil
}

func geminiFinishReasonToClaude(reason *string, hasToolUse bool, blocked bool) string {
	if blocked {
		return "refusal"
	}
	if reason != nil {
		switch strings.ToUpper(strings.TrimSpace(*reason)) {
		case "MAX_TOKENS":
			return "max_tokens"
		case "SAFETY", "RECITATION", "SPII", "BLOCKLIST", "PROHIBITED_CONTENT":
			return "refusal"
		}
	}
	if hasToolUse {
		return "tool_use"
	}
	return "end_turn"
}

func geminiResponseBlockReason(response *dto.GeminiChatResponse) string {
	if response == nil || response.PromptFeedback == nil || response.PromptFeedback.BlockReason == nil {
		return ""
	}
	return strings.TrimSpace(*response.PromptFeedback.BlockReason)
}

func geminiClaudeUsage(usage *dto.Usage) *dto.ClaudeUsage {
	if usage == nil {
		return &dto.ClaudeUsage{}
	}
	chatUsage := *usage
	chatUsage.InputTokens = 0
	chatUsage.OutputTokens = 0
	if chatUsage.InputTokensDetails != nil {
		chatUsage.PromptTokensDetails = *chatUsage.InputTokensDetails
		chatUsage.InputTokensDetails = nil
	}
	claudeUsage := oaichat.ClaudeUsageFromOpenAIUsage(&chatUsage)
	if claudeUsage != nil {
		return claudeUsage
	}
	return &dto.ClaudeUsage{}
}
