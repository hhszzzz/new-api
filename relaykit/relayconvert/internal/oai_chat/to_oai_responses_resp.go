package oaichat

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const (
	chatFinishReasonLength        = "length"
	chatFinishReasonContentFilter = "content_filter"

	responsesEventCreated                  = "response.created"
	responsesEventInProgress               = "response.in_progress"
	responsesEventCompleted                = "response.completed"
	responsesEventIncomplete               = "response.incomplete"
	responsesEventContentPartAdded         = "response.content_part.added"
	responsesEventContentPartDone          = "response.content_part.done"
	responsesEventOutputTextDelta          = "response.output_text.delta"
	responsesEventOutputTextDone           = "response.output_text.done"
	responsesEventRefusalDelta             = "response.refusal.delta"
	responsesEventRefusalDone              = "response.refusal.done"
	responsesEventOutputItemAdded          = "response.output_item.added"
	responsesEventOutputItemDone           = "response.output_item.done"
	responsesEventFunctionArgsDelta        = "response.function_call_arguments.delta"
	responsesEventFunctionArgsDone         = "response.function_call_arguments.done"
	responsesEventReasoningSummaryDelta    = "response.reasoning_summary_text.delta"
	responsesEventReasoningSummaryDone     = "response.reasoning_summary_text.done"
	responsesEventReasoningPartAdded       = "response.reasoning_summary_part.added"
	responsesEventReasoningPartDone        = "response.reasoning_summary_part.done"
	responsesOutputTypeFunctionCall        = "function_call"
	responsesOutputTypeMessage             = "message"
	responsesOutputTypeReasoning           = "reasoning"
	responsesIncompleteReasonContentFilter = "content_filter"
	responsesIncompleteReasonMaxTokens     = "max_output_tokens"
)

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse, id string) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return ChatCompletionsResponseToResponsesResponseWithToolState(resp, id, nil)
}

func ChatCompletionsResponseToResponsesResponseWithToolState(resp *dto.OpenAITextResponse, id string, toolState *sharedbridge.ToolState) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return ChatCompletionsResponseToResponsesResponseWithBridgeState(resp, id, toolState, nil)
}

func ChatCompletionsResponseToResponsesResponseWithBridgeState(resp *dto.OpenAITextResponse, id string, toolState *sharedbridge.ToolState, outputState *sharedbridge.ResponseOutputState) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	usage := UsageFromChatUsage(&resp.Usage)
	responseID := normalizeResponsesResponseID(id)
	out := &dto.OpenAIResponsesResponse{
		ID:        responseID,
		Object:    "response",
		CreatedAt: chatCreatedAt(resp.Created),
		Status:    []byte(`"completed"`),
		Model:     resp.Model,
		Output:    make([]dto.ResponsesOutput, 0),
		Usage:     usage,
	}

	if len(resp.Choices) == 0 {
		return out, usage, nil
	}

	choice := resp.Choices[0]
	toolCalls := choice.Message.ParseToolCalls()
	if status, details := ResponsesStatusFromChatFinishReason(choice.FinishReason); status != "" {
		out.Status = []byte(fmt.Sprintf("%q", status))
		out.IncompleteDetails = details
	}

	toolOutputs := make([]dto.ResponsesOutput, 0, len(toolCalls))
	usedToolCallIDs := make(map[string]struct{}, len(toolCalls))
	for i, toolCall := range toolCalls {
		toolOutput, err := chatToolCallToResponsesOutput(toolCall, responseID, i, responseOutputStatus(out), toolState, usedToolCallIDs)
		if err != nil {
			return nil, nil, err
		}
		toolOutputs = append(toolOutputs, toolOutput)
	}

	text := choice.Message.StringContent()
	refusal := choice.Message.GetRefusalContent()
	reasoning := choice.Message.GetReasoningContent()
	if outputState == nil || len(outputState.Items) == 0 {
		if reasoning != "" {
			out.Output = append(out.Output, chatResponseReasoningOutput(responseID, 0, reasoning, "", responseOutputStatus(out)))
		}
		if text != "" || refusal != "" {
			out.Output = append(out.Output, chatResponseMessageOutput(responseID, 0, text, refusal, len(toolCalls) > 0, responseOutputStatus(out)))
		}
		out.Output = append(out.Output, toolOutputs...)
		return out, usage, nil
	}

	usedTools := make([]bool, len(toolOutputs))
	messageIndex := 0
	reasoningIndex := 0
	usedMessage := false
	usedRefusal := false
	usedReasoning := false
	for _, item := range outputState.Items {
		switch item.Kind {
		case sharedbridge.ResponseOutputKindMessage:
			messageRefusal := ""
			if !usedRefusal {
				messageRefusal = refusal
				usedRefusal = refusal != ""
			}
			if item.Text == "" && messageRefusal == "" {
				continue
			}
			out.Output = append(out.Output, chatResponseMessageOutput(responseID, messageIndex, item.Text, messageRefusal, len(toolCalls) > 0, responseOutputStatus(out)))
			messageIndex++
			usedMessage = true
		case sharedbridge.ResponseOutputKindReasoning:
			if item.Text == "" && item.EncryptedContent == "" {
				continue
			}
			out.Output = append(out.Output, chatResponseReasoningOutput(responseID, reasoningIndex, item.Text, item.EncryptedContent, responseOutputStatus(out)))
			reasoningIndex++
			usedReasoning = true
		case sharedbridge.ResponseOutputKindTool:
			if item.ToolIndex < 0 || item.ToolIndex >= len(toolOutputs) || usedTools[item.ToolIndex] {
				continue
			}
			out.Output = append(out.Output, toolOutputs[item.ToolIndex])
			usedTools[item.ToolIndex] = true
		}
	}
	if reasoning != "" && !usedReasoning {
		out.Output = append(out.Output, chatResponseReasoningOutput(responseID, reasoningIndex, reasoning, "", responseOutputStatus(out)))
	}
	if (text != "" && !usedMessage) || (refusal != "" && !usedRefusal) {
		messageText := ""
		if !usedMessage {
			messageText = text
		}
		messageRefusal := ""
		if !usedRefusal {
			messageRefusal = refusal
		}
		out.Output = append(out.Output, chatResponseMessageOutput(responseID, messageIndex, messageText, messageRefusal, len(toolCalls) > 0, responseOutputStatus(out)))
	}
	for i := range toolOutputs {
		if !usedTools[i] {
			out.Output = append(out.Output, toolOutputs[i])
		}
	}

	return out, usage, nil
}

func chatResponseMessageOutput(responseID string, index int, text string, refusal string, hasToolCalls bool, status string) dto.ResponsesOutput {
	phase := "final_answer"
	if hasToolCalls {
		phase = "commentary"
	}
	content := make([]dto.ResponsesOutputContent, 0, 2)
	if text != "" {
		content = append(content, dto.ResponsesOutputContent{
			Type:        "output_text",
			Text:        text,
			Annotations: []interface{}{},
		})
	}
	if refusal != "" {
		content = append(content, dto.ResponsesOutputContent{
			Type:    "refusal",
			Refusal: refusal,
		})
	}
	return dto.ResponsesOutput{
		Type:    responsesOutputTypeMessage,
		ID:      responsesSyntheticItemID("msg", responseID, index),
		Status:  status,
		Role:    "assistant",
		Phase:   phase,
		Content: content,
	}
}

func chatResponseReasoningOutput(responseID string, index int, reasoning string, encryptedContent string, status string) dto.ResponsesOutput {
	output := dto.ResponsesOutput{
		Type:             responsesOutputTypeReasoning,
		ID:               responsesSyntheticItemID("rs", responseID, index),
		Status:           status,
		EncryptedContent: encryptedContent,
	}
	if reasoning != "" {
		output.Summary = []dto.ResponsesReasoningSummaryPart{
			{
				Type: "summary_text",
				Text: reasoning,
			},
		}
	}
	return output
}

func ResponsesStatusFromChatFinishReason(finishReason string) (string, *dto.IncompleteDetails) {
	switch strings.TrimSpace(finishReason) {
	case chatFinishReasonLength:
		return "incomplete", &dto.IncompleteDetails{Reason: responsesIncompleteReasonMaxTokens}
	case chatFinishReasonContentFilter:
		return "incomplete", &dto.IncompleteDetails{Reason: responsesIncompleteReasonContentFilter}
	default:
		return "completed", nil
	}
}

func UsageFromChatUsage(src *dto.Usage) *dto.Usage {
	usage := &dto.Usage{}
	if src == nil {
		return usage
	}
	usage.UsageSemantic = src.UsageSemantic
	usage.UsageSource = src.UsageSource
	usage.BillingUsage = dto.CloneBillingUsage(src.BillingUsage)
	if usage.BillingUsage == nil {
		usage.BillingUsage = dto.NewOpenAIChatBillingUsage(src)
	}
	usage.Cost = src.Cost
	if src.PromptTokens != 0 {
		usage.PromptTokens = src.PromptTokens
		usage.InputTokens = src.PromptTokens
	}
	if src.CompletionTokens != 0 {
		usage.CompletionTokens = src.CompletionTokens
		usage.OutputTokens = src.CompletionTokens
	}
	if src.TotalTokens != 0 {
		usage.TotalTokens = src.TotalTokens
	} else {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if src.PromptTokensDetails.CachedTokens != 0 ||
		src.PromptTokensDetails.ImageTokens != 0 ||
		src.PromptTokensDetails.AudioTokens != 0 ||
		src.PromptTokensDetails.CachedCreationTokens != 0 ||
		src.PromptTokensDetails.CacheWriteTokens != 0 ||
		src.PromptTokensDetails.TextTokens != 0 {
		details := src.PromptTokensDetails
		usage.InputTokensDetails = &details
	}
	if src.CompletionTokenDetails.ReasoningTokens != 0 ||
		src.CompletionTokenDetails.TextTokens != 0 ||
		src.CompletionTokenDetails.AudioTokens != 0 ||
		src.CompletionTokenDetails.ImageTokens != 0 {
		usage.CompletionTokenDetails = src.CompletionTokenDetails
	}
	usage.ClaudeCacheCreation5mTokens = src.ClaudeCacheCreation5mTokens
	usage.ClaudeCacheCreation1hTokens = src.ClaudeCacheCreation1hTokens
	return usage
}

func responseOutputStatus(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || responseStatusString(resp) != "incomplete" {
		return "completed"
	}
	return "incomplete"
}

func responseStatusString(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Status) == 0 {
		return ""
	}
	var status string
	_ = kitutil.Unmarshal(resp.Status, &status)
	return strings.TrimSpace(status)
}

func chatToolCallToResponsesOutput(toolCall dto.ToolCallRequest, responseID string, index int, status string, toolState *sharedbridge.ToolState, usedCallIDs map[string]struct{}) (dto.ResponsesOutput, error) {
	callID := uniqueResponsesToolCallID(usedCallIDs, toolCall.ID, responseID, index)
	if toolCall.Type == "" || toolCall.Type == "function" {
		if identity, ok := toolState.ResolveUpstream(toolCall.Function.Name); ok {
			switch identity.Kind {
			case sharedbridge.ToolKindCustom:
				return dto.ResponsesOutput{
					Type:      "custom_tool_call",
					ID:        responsesSyntheticItemID("ctc", responseID, index),
					Status:    status,
					CallId:    callID,
					Name:      identity.Name,
					Namespace: identity.Namespace,
					Input:     sharedbridge.DecodeCustomInput(toolCall.Function.Arguments),
				}, nil
			case sharedbridge.ToolKindToolSearch:
				return dto.ResponsesOutput{
					Type:      "tool_search_call",
					ID:        responsesSyntheticItemID("tsc", responseID, index),
					Status:    status,
					CallId:    callID,
					Execution: "client",
					Arguments: sharedbridge.ToolSearchArgumentsRaw(toolCall.Function.Arguments),
				}, nil
			case sharedbridge.ToolKindFunction:
				return dto.ResponsesOutput{
					Type:      responsesOutputTypeFunctionCall,
					ID:        responsesSyntheticItemID("fc", responseID, index),
					Status:    status,
					CallId:    callID,
					Name:      identity.Name,
					Namespace: identity.Namespace,
					Arguments: chatArgumentsRawMessage(toolCall.Function.Arguments),
				}, nil
			}
		}
		return dto.ResponsesOutput{
			Type:      responsesOutputTypeFunctionCall,
			ID:        responsesSyntheticItemID("fc", responseID, index),
			Status:    status,
			CallId:    callID,
			Name:      toolCall.Function.Name,
			Arguments: chatArgumentsRawMessage(toolCall.Function.Arguments),
		}, nil
	}
	return dto.ResponsesOutput{
		Type:      toolCall.Type,
		ID:        responsesSyntheticItemID("tc", responseID, index),
		Status:    status,
		CallId:    callID,
		Arguments: toolCall.Custom,
	}, nil
}

func uniqueResponsesToolCallID(used map[string]struct{}, candidate string, responseID string, index int) string {
	if used == nil {
		used = make(map[string]struct{})
	}
	candidate = strings.TrimSpace(candidate)
	if candidate != "" {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate
		}
	}

	base := responsesSyntheticItemID("call", responseID, index)
	candidate = base
	for suffix := 1; ; suffix++ {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", base, suffix)
	}
}

func normalizeResponsesResponseID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "resp_") && len(id) > len("resp_") {
		return id
	}
	seed := responsesIDSeed(id)
	if seed == "" {
		seed = responsesIDSeed(kitutil.GetUUID())
	}
	return "resp_" + seed
}

func responsesSyntheticItemID(prefix, responseID string, index int) string {
	seed := strings.TrimPrefix(normalizeResponsesResponseID(responseID), "resp_")
	return fmt.Sprintf("%s_%s_%d", prefix, seed, index)
}

func responsesIDSeed(id string) string {
	id = strings.TrimSpace(id)
	var seed strings.Builder
	seed.Grow(len(id))
	for _, char := range id {
		switch {
		case char >= 'a' && char <= 'z':
			seed.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			seed.WriteRune(char)
		case char >= '0' && char <= '9':
			seed.WriteRune(char)
		case char == '_' || char == '-':
			seed.WriteRune(char)
		default:
			seed.WriteByte('_')
		}
	}
	return strings.Trim(seed.String(), "_-")
}

func chatArgumentsRawMessage(arguments string) []byte {
	raw, err := kitutil.Marshal(arguments)
	if err != nil {
		return []byte(`""`)
	}
	return raw
}

func chatCreatedAt(created any) int {
	switch v := created.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		if parsed := kitutil.String2Int(v); parsed != 0 {
			return parsed
		}
	}
	return int(time.Now().Unix())
}

func responsesStreamEvent(eventType string, payload dto.ResponsesStreamResponse) ChatToResponsesStreamEvent {
	payload.Type = eventType
	return ChatToResponsesStreamEvent{
		Type:    eventType,
		Payload: payload,
	}
}

func intPtr(v int) *int {
	return &v
}
