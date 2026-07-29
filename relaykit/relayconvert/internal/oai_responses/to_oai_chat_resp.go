package oairesponses

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const (
	responsesEventCreated                  = "response.created"
	responsesEventCompleted                = "response.completed"
	responsesEventDone                     = "response.done"
	responsesEventIncomplete               = "response.incomplete"
	responsesEventCancelled                = "response.cancelled"
	responsesEventCanceled                 = "response.canceled"
	responsesEventFailed                   = "response.failed"
	responsesEventError                    = "error"
	responsesEventLegacyError              = "response.error"
	responsesEventOutputTextDelta          = "response.output_text.delta"
	responsesEventOutputTextDone           = "response.output_text.done"
	responsesEventOutputItemAdded          = "response.output_item.added"
	responsesEventOutputItemDone           = "response.output_item.done"
	responsesEventFunctionArgsDelta        = "response.function_call_arguments.delta"
	responsesEventFunctionArgsDone         = "response.function_call_arguments.done"
	responsesEventCustomToolInputDelta     = "response.custom_tool_call_input.delta"
	responsesEventCustomToolInputDone      = "response.custom_tool_call_input.done"
	responsesEventReasoningSummaryDelta    = "response.reasoning_summary_text.delta"
	responsesEventReasoningSummaryDone     = "response.reasoning_summary_text.done"
	responsesEventReasoningTextDelta       = "response.reasoning_text.delta"
	responsesEventReasoningTextDone        = "response.reasoning_text.done"
	responsesEventRefusalDelta             = "response.refusal.delta"
	responsesEventRefusalDone              = "response.refusal.done"
	responsesOutputTypeFunctionCall        = "function_call"
	responsesOutputTypeCustomToolCall      = "custom_tool_call"
	responsesOutputTypeToolSearchCall      = "tool_search_call"
	responsesOutputTypeMessage             = "message"
	responsesOutputTypeReasoning           = "reasoning"
	responsesIncompleteReasonContentFilter = "content_filter"
	responsesIncompleteReasonMaxTokens     = "max_output_tokens"
)

func ResponsesFinishReasonFromStatus(resp *dto.OpenAIResponsesResponse) (string, bool) {
	if resp == nil {
		return "", false
	}

	status := responseStatusString(resp)
	if status != "incomplete" {
		return "", false
	}

	reason := ""
	if resp.IncompleteDetails != nil {
		reason = strings.TrimSpace(resp.IncompleteDetails.Reason)
	}
	if reason == responsesIncompleteReasonContentFilter {
		return "content_filter", true
	}
	return "length", true
}

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}
	if err := validateResponsesTerminalResponse(resp); err != nil {
		return nil, nil, err
	}

	text := ExtractOutputTextFromResponses(resp)
	reasoning := ExtractReasoningTextFromResponses(resp)

	usage := UsageFromResponsesUsage(resp.Usage)

	created := resp.CreatedAt

	var toolCalls []dto.ToolCallResponse
	if len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if !isResponsesToolOutputType(out.Type) {
				continue
			}
			name := responsesToolCallName(&out)
			if name == "" {
				continue
			}
			callId := strings.TrimSpace(out.CallId)
			if callId == "" {
				callId = strings.TrimSpace(out.ID)
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID:   callId,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: responsesToolCallArguments(&out),
				},
			})
		}
	}

	finishReason := "stop"
	if mappedReason, ok := ResponsesFinishReasonFromStatus(resp); ok {
		finishReason = mappedReason
	} else if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if reasoning != "" {
		msg.ReasoningContent = &reasoning
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

func UsageFromResponsesUsage(src *dto.Usage) *dto.Usage {
	usage := &dto.Usage{}
	if src == nil {
		return usage
	}
	usage.UsageSemantic = src.UsageSemantic
	usage.UsageSource = src.UsageSource
	usage.BillingUsage = dto.CloneBillingUsage(src.BillingUsage)
	if usage.BillingUsage == nil {
		usage.BillingUsage = dto.NewOpenAIResponsesBillingUsage(src)
	}
	usage.Cost = src.Cost
	if src.InputTokens != 0 {
		usage.PromptTokens = src.InputTokens
		usage.InputTokens = src.InputTokens
	}
	if src.OutputTokens != 0 {
		usage.CompletionTokens = src.OutputTokens
		usage.OutputTokens = src.OutputTokens
	}
	if src.TotalTokens != 0 {
		usage.TotalTokens = src.TotalTokens
	} else {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if src.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = src.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.CachedCreationTokens = src.InputTokensDetails.CachedCreationTokens
		usage.PromptTokensDetails.CacheWriteTokens = src.InputTokensDetails.CacheWriteTokens
		usage.PromptTokensDetails.TextTokens = src.InputTokensDetails.TextTokens
		usage.PromptTokensDetails.ImageTokens = src.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.AudioTokens = src.InputTokensDetails.AudioTokens
	}
	if src.CompletionTokenDetails.ReasoningTokens != 0 ||
		src.CompletionTokenDetails.TextTokens != 0 ||
		src.CompletionTokenDetails.AudioTokens != 0 ||
		src.CompletionTokenDetails.ImageTokens != 0 {
		usage.CompletionTokenDetails.ReasoningTokens = src.CompletionTokenDetails.ReasoningTokens
		usage.CompletionTokenDetails.TextTokens = src.CompletionTokenDetails.TextTokens
		usage.CompletionTokenDetails.AudioTokens = src.CompletionTokenDetails.AudioTokens
		usage.CompletionTokenDetails.ImageTokens = src.CompletionTokenDetails.ImageTokens
	}
	usage.ClaudeCacheCreation5mTokens = src.ClaudeCacheCreation5mTokens
	usage.ClaudeCacheCreation1hTokens = src.ClaudeCacheCreation1hTokens
	return usage
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder

	// Prefer assistant message outputs.
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			} else if c.Type == "refusal" && c.Refusal != "" {
				sb.WriteString(c.Refusal)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		if out.Type == responsesOutputTypeReasoning {
			// Reasoning text must never leak into visible message content.
			continue
		}
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			} else if c.Refusal != "" {
				sb.WriteString(c.Refusal)
			}
		}
	}
	return sb.String()
}

func ExtractReasoningTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, out := range resp.Output {
		if out.Type != responsesOutputTypeReasoning {
			continue
		}
		sb.WriteString(extractReasoningTextFromOutput(&out))
	}
	return sb.String()
}

func extractReasoningTextFromOutput(out *dto.ResponsesOutput) string {
	if out == nil || out.Type != responsesOutputTypeReasoning {
		return ""
	}

	var summary strings.Builder
	for _, part := range out.Summary {
		if part.Type == "summary_text" && part.Text != "" {
			summary.WriteString(part.Text)
		}
	}

	var visible strings.Builder
	for _, part := range out.Content {
		if (part.Type == "reasoning_text" || part.Type == "summary_text") && part.Text != "" {
			visible.WriteString(part.Text)
		}
	}
	if visible.Len() == 0 || visible.String() == summary.String() {
		return summary.String()
	}
	if summary.Len() == 0 {
		return visible.String()
	}
	return summary.String() + "\n\n" + visible.String()
}

func responseStatusString(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Status) == 0 {
		return ""
	}
	var status string
	_ = kitutil.Unmarshal(resp.Status, &status)
	return strings.TrimSpace(status)
}

func validateResponsesTerminalResponse(resp *dto.OpenAIResponsesResponse) error {
	if resp == nil {
		return errors.New("response is nil")
	}
	status := responseStatusString(resp)
	if status != "failed" && status != "cancelled" && status != "canceled" && resp.Error == nil {
		return nil
	}
	fallback := "Responses upstream returned an error"
	if status == "failed" {
		fallback = "Responses upstream failed"
	} else if status == "cancelled" || status == "canceled" {
		fallback = "Responses upstream cancelled the response"
	}
	if upstreamError := resp.GetOpenAIError(); upstreamError != nil {
		if message := strings.TrimSpace(upstreamError.Message); message != "" {
			return fmt.Errorf("%s: %s", fallback, message)
		}
	}
	return errors.New(fallback)
}

func ensureIncompleteResponse(resp *dto.OpenAIResponsesResponse) *dto.OpenAIResponsesResponse {
	if resp == nil {
		resp = &dto.OpenAIResponsesResponse{}
	}
	if len(resp.Status) == 0 {
		resp.Status = []byte(`"incomplete"`)
	}
	return resp
}

func isResponsesToolOutputType(outputType string) bool {
	return outputType == responsesOutputTypeFunctionCall || outputType == responsesOutputTypeCustomToolCall || outputType == responsesOutputTypeToolSearchCall
}

func responsesToolCallName(output *dto.ResponsesOutput) string {
	if output == nil {
		return ""
	}
	if output.Type == responsesOutputTypeToolSearchCall {
		return "tool_search"
	}
	return strings.TrimSpace(output.Name)
}

func responsesToolCallArguments(output *dto.ResponsesOutput) string {
	if output == nil {
		return "{}"
	}
	switch output.Type {
	case responsesOutputTypeCustomToolCall:
		return customInputArguments(output.Input)
	case responsesOutputTypeToolSearchCall:
		if args := toolSearchArguments(output.Arguments); args != "" {
			return args
		}
		return "{}"
	default:
		arguments := output.ArgumentsString()
		if strings.TrimSpace(arguments) == "" {
			return "{}"
		}
		return arguments
	}
}

func responseStreamEventItemID(event *dto.ResponsesStreamResponse) string {
	if event == nil {
		return ""
	}
	if event.Item != nil {
		if itemID := strings.TrimSpace(event.Item.ID); itemID != "" {
			return itemID
		}
	}
	return strings.TrimSpace(event.ItemID)
}

func fallbackToolKey(itemID string, callID string, outputIndex *int) string {
	if outputIndex != nil {
		return fmt.Sprintf("output:%d", *outputIndex)
	}
	if strings.TrimSpace(itemID) != "" {
		return "item:" + strings.TrimSpace(itemID)
	}
	if strings.TrimSpace(callID) != "" {
		return "call:" + strings.TrimSpace(callID)
	}
	return ""
}

func fallbackCallID(event *dto.ResponsesStreamResponse) string {
	if event == nil {
		return ""
	}
	if strings.TrimSpace(event.ItemID) != "" {
		return strings.TrimSpace(event.ItemID)
	}
	if event.OutputIndex != nil {
		return fmt.Sprintf("call_output_%d", *event.OutputIndex)
	}
	return ""
}
