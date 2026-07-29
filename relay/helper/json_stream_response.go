package helper

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
)

// PromoteJSONResponseToSSE converts a complete upstream JSON response into the
// equivalent SSE wire lifecycle. It is used only when a streaming client hits
// an upstream that ignored stream=true and returned JSON instead.
func PromoteJSONResponseToSSE(resp *http.Response, format relaytypes.RelayFormat) error {
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("upstream JSON response body is unavailable")
	}

	originalBody := resp.Body
	body, err := io.ReadAll(originalBody)
	_ = originalBody.Close()
	if err != nil {
		return fmt.Errorf("read upstream JSON response: %w", err)
	}
	if message := upstreamJSONErrorMessage(body); message != "" {
		return fmt.Errorf("upstream returned an error response: %s", message)
	}

	var stream bytes.Buffer
	switch format {
	case relaytypes.RelayFormatOpenAI:
		err = appendChatJSONAsSSE(&stream, body)
	case relaytypes.RelayFormatClaude:
		err = appendClaudeJSONAsSSE(&stream, body)
	case relaytypes.RelayFormatOpenAIResponses:
		err = appendResponsesJSONAsSSE(&stream, body)
	case relaytypes.RelayFormatGemini:
		err = appendGeminiJSONAsSSE(&stream, body)
	default:
		err = fmt.Errorf("unsupported upstream response format %q", format)
	}
	if err != nil {
		return err
	}

	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	resp.Header.Set("Content-Type", "text/event-stream")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	resp.Body = io.NopCloser(bytes.NewReader(stream.Bytes()))
	return nil
}

func upstreamJSONErrorMessage(body []byte) string {
	var envelope dto.GeneralErrorResponse
	if err := common.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	rawError := strings.TrimSpace(string(envelope.Error))
	if rawError == "" || rawError == "null" || rawError == "{}" {
		return ""
	}
	return strings.TrimSpace(envelope.ToMessage())
}

func appendChatJSONAsSSE(stream *bytes.Buffer, body []byte) error {
	var response dto.OpenAITextResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode Chat Completions JSON response: %w", err)
	}
	if len(response.Choices) == 0 {
		return fmt.Errorf("Chat Completions JSON response has no choices")
	}

	created := int64(0)
	if value := strings.TrimSpace(common.Interface2String(response.Created)); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			created = int64(parsed)
		}
	}
	contentChunk := dto.ChatCompletionsStreamResponse{
		Id:      response.Id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   response.Model,
		Choices: make([]dto.ChatCompletionsStreamResponseChoice, 0, len(response.Choices)),
	}
	finishChunk := dto.ChatCompletionsStreamResponse{
		Id:      response.Id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   response.Model,
		Choices: make([]dto.ChatCompletionsStreamResponseChoice, 0, len(response.Choices)),
	}
	for _, choice := range response.Choices {
		finishReason := strings.TrimSpace(choice.FinishReason)
		if finishReason == "" {
			return fmt.Errorf("Chat Completions JSON response choice %d has no finish_reason", choice.Index)
		}
		role := strings.TrimSpace(choice.Message.Role)
		if role == "" {
			role = "assistant"
		}
		delta := dto.ChatCompletionsStreamResponseChoiceDelta{
			Role:             role,
			ReasoningContent: choice.Message.ReasoningContent,
			Reasoning:        choice.Message.Reasoning,
			ReasoningDetails: choice.Message.ReasoningDetails,
			Refusal:          choice.Message.Refusal,
			FunctionCall:     choice.Message.FunctionCall,
		}
		if content := choice.Message.StringContent(); content != "" {
			delta.Content = &content
		}
		if len(choice.Message.ToolCalls) > 0 {
			if err := common.Unmarshal(choice.Message.ToolCalls, &delta.ToolCalls); err != nil {
				return fmt.Errorf("decode Chat Completions tool calls: %w", err)
			}
			for index := range delta.ToolCalls {
				if delta.ToolCalls[index].Index == nil {
					delta.ToolCalls[index].SetIndex(index)
				}
			}
		}
		contentChunk.Choices = append(contentChunk.Choices, dto.ChatCompletionsStreamResponseChoice{
			Index: choice.Index,
			Delta: delta,
		})
		finishChunk.Choices = append(finishChunk.Choices, dto.ChatCompletionsStreamResponseChoice{
			Index:        choice.Index,
			FinishReason: &finishReason,
		})
	}
	if err := appendSSEJSON(stream, &contentChunk); err != nil {
		return err
	}
	if err := appendSSEJSON(stream, &finishChunk); err != nil {
		return err
	}
	usage := response.Usage
	if err := appendSSEJSON(stream, &dto.ChatCompletionsStreamResponse{
		Id:      response.Id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   response.Model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{},
		Usage:   &usage,
	}); err != nil {
		return err
	}
	stream.WriteString("data: [DONE]\n\n")
	return nil
}

func appendClaudeJSONAsSSE(stream *bytes.Buffer, body []byte) error {
	var response dto.ClaudeResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode Anthropic Messages JSON response: %w", err)
	}
	stopReason := strings.TrimSpace(response.StopReason)
	if stopReason == "" {
		return fmt.Errorf("Anthropic Messages JSON response has no stop_reason")
	}
	role := strings.TrimSpace(response.Role)
	if role == "" {
		role = "assistant"
	}
	var startUsage *dto.ClaudeUsage
	if response.Usage != nil {
		usageCopy := *response.Usage
		usageCopy.OutputTokens = 0
		startUsage = &usageCopy
	}
	if err := appendSSEJSON(stream, &dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Id:      response.Id,
			Type:    "message",
			Role:    role,
			Model:   response.Model,
			Content: []dto.ClaudeMediaMessage{},
			Usage:   startUsage,
		},
	}); err != nil {
		return err
	}

	for index := range response.Content {
		block := response.Content[index]
		startBlock := block
		switch block.Type {
		case "text":
			startBlock.SetText("")
		case "thinking":
			startBlock.Thinking = common.GetPointer("")
			startBlock.Signature = ""
		case "tool_use":
			startBlock.Input = map[string]any{}
		}
		if err := appendSSEJSON(stream, &dto.ClaudeResponse{
			Type:         "content_block_start",
			Index:        common.GetPointer(index),
			ContentBlock: &startBlock,
		}); err != nil {
			return err
		}

		switch block.Type {
		case "text":
			if text := block.GetText(); text != "" {
				if err := appendSSEJSON(stream, &dto.ClaudeResponse{
					Type:  "content_block_delta",
					Index: common.GetPointer(index),
					Delta: &dto.ClaudeMediaMessage{Type: "text_delta", Text: common.GetPointer(text)},
				}); err != nil {
					return err
				}
			}
		case "thinking":
			if block.Thinking != nil && *block.Thinking != "" {
				if err := appendSSEJSON(stream, &dto.ClaudeResponse{
					Type:  "content_block_delta",
					Index: common.GetPointer(index),
					Delta: &dto.ClaudeMediaMessage{Type: "thinking_delta", Thinking: block.Thinking},
				}); err != nil {
					return err
				}
			}
			if block.Signature != "" {
				if err := appendSSEJSON(stream, &dto.ClaudeResponse{
					Type:  "content_block_delta",
					Index: common.GetPointer(index),
					Delta: &dto.ClaudeMediaMessage{Type: "signature_delta", Signature: block.Signature},
				}); err != nil {
					return err
				}
			}
		case "tool_use":
			partialJSON := "{}"
			if block.Input != nil {
				encoded, err := common.Marshal(block.Input)
				if err != nil {
					return fmt.Errorf("encode Anthropic tool input: %w", err)
				}
				partialJSON = string(encoded)
			}
			if err := appendSSEJSON(stream, &dto.ClaudeResponse{
				Type:  "content_block_delta",
				Index: common.GetPointer(index),
				Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: &partialJSON},
			}); err != nil {
				return err
			}
		}

		if err := appendSSEJSON(stream, &dto.ClaudeResponse{
			Type:  "content_block_stop",
			Index: common.GetPointer(index),
		}); err != nil {
			return err
		}
	}

	if err := appendSSEJSON(stream, &dto.ClaudeResponse{
		Type:  "message_delta",
		Delta: &dto.ClaudeMediaMessage{StopReason: &stopReason},
		Usage: response.Usage,
	}); err != nil {
		return err
	}
	return appendSSEJSON(stream, &dto.ClaudeResponse{Type: "message_stop"})
}

func appendResponsesJSONAsSSE(stream *bytes.Buffer, body []byte) error {
	var response dto.OpenAIResponsesResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode OpenAI Responses JSON response: %w", err)
	}
	status := ""
	if len(response.Status) > 0 {
		_ = common.Unmarshal(response.Status, &status)
	}
	status = strings.TrimSpace(status)
	switch status {
	case "completed", "incomplete":
	case "failed", "cancelled", "canceled":
		message := status
		if upstreamError := response.GetOpenAIError(); upstreamError != nil && strings.TrimSpace(upstreamError.Message) != "" {
			message = upstreamError.Message
		}
		return fmt.Errorf("OpenAI Responses JSON response %s: %s", status, message)
	case "in_progress", "queued":
		return fmt.Errorf("OpenAI Responses JSON response is not terminal: %s", status)
	case "":
		return fmt.Errorf("OpenAI Responses JSON response has no terminal status")
	default:
		return fmt.Errorf("OpenAI Responses JSON response has unknown status %q", status)
	}

	sequence := 0
	appendEvent := func(event *dto.ResponsesStreamResponse) error {
		event.SequenceNumber = common.GetPointer(sequence)
		sequence++
		return appendSSEJSON(stream, event)
	}
	createdResponse := response
	createdResponse.Output = nil
	createdResponse.Usage = nil
	createdResponse.Status = []byte(`"in_progress"`)
	if err := appendEvent(&dto.ResponsesStreamResponse{Type: "response.created", Response: &createdResponse}); err != nil {
		return err
	}

	for index := range response.Output {
		item := response.Output[index]
		startItem := item
		startItem.Status = "in_progress"
		switch item.Type {
		case "message":
			startItem.Content = nil
		case "reasoning":
			startItem.Summary = nil
			startItem.Content = nil
		case "function_call":
			startItem.Arguments = nil
		case "custom_tool_call":
			startItem.Input = ""
		}
		if err := appendEvent(&dto.ResponsesStreamResponse{
			Type:        "response.output_item.added",
			OutputIndex: common.GetPointer(index),
			Item:        &startItem,
		}); err != nil {
			return err
		}

		switch item.Type {
		case "message":
			for contentIndex, content := range item.Content {
				event := &dto.ResponsesStreamResponse{
					OutputIndex:  common.GetPointer(index),
					ContentIndex: common.GetPointer(contentIndex),
					ItemID:       item.ID,
				}
				switch content.Type {
				case "output_text":
					event.Type = "response.output_text.delta"
					event.Delta = content.Text
					if err := appendEvent(event); err != nil {
						return err
					}
					event.Type = "response.output_text.done"
					event.Delta = ""
					event.Text = content.Text
				case "refusal":
					event.Type = "response.refusal.delta"
					event.Delta = content.Refusal
					if err := appendEvent(event); err != nil {
						return err
					}
					event.Type = "response.refusal.done"
					event.Delta = ""
					event.Text = content.Refusal
					event.Refusal = content.Refusal
				default:
					continue
				}
				if err := appendEvent(event); err != nil {
					return err
				}
			}
		case "reasoning":
			for summaryIndex, summary := range item.Summary {
				event := &dto.ResponsesStreamResponse{
					Type:         "response.reasoning_summary_text.delta",
					OutputIndex:  common.GetPointer(index),
					SummaryIndex: common.GetPointer(summaryIndex),
					ItemID:       item.ID,
					Delta:        summary.Text,
				}
				if err := appendEvent(event); err != nil {
					return err
				}
				event.Type = "response.reasoning_summary_text.done"
				event.Delta = ""
				event.Text = summary.Text
				if err := appendEvent(event); err != nil {
					return err
				}
			}
			for contentIndex, content := range item.Content {
				if content.Text == "" {
					continue
				}
				event := &dto.ResponsesStreamResponse{
					Type:         "response.reasoning_text.delta",
					OutputIndex:  common.GetPointer(index),
					ContentIndex: common.GetPointer(contentIndex),
					ItemID:       item.ID,
					Delta:        content.Text,
				}
				if err := appendEvent(event); err != nil {
					return err
				}
				event.Type = "response.reasoning_text.done"
				event.Delta = ""
				event.Text = content.Text
				if err := appendEvent(event); err != nil {
					return err
				}
			}
		case "function_call":
			arguments := item.ArgumentsString()
			if err := appendEvent(&dto.ResponsesStreamResponse{
				Type:        "response.function_call_arguments.delta",
				OutputIndex: common.GetPointer(index),
				ItemID:      item.ID,
				Delta:       arguments,
			}); err != nil {
				return err
			}
			if err := appendEvent(&dto.ResponsesStreamResponse{
				Type:        "response.function_call_arguments.done",
				OutputIndex: common.GetPointer(index),
				ItemID:      item.ID,
				Arguments:   arguments,
			}); err != nil {
				return err
			}
		case "custom_tool_call":
			if err := appendEvent(&dto.ResponsesStreamResponse{
				Type:        "response.custom_tool_call_input.delta",
				OutputIndex: common.GetPointer(index),
				ItemID:      item.ID,
				Delta:       item.Input,
			}); err != nil {
				return err
			}
			if err := appendEvent(&dto.ResponsesStreamResponse{
				Type:        "response.custom_tool_call_input.done",
				OutputIndex: common.GetPointer(index),
				ItemID:      item.ID,
				Input:       item.Input,
			}); err != nil {
				return err
			}
		}

		doneItem := item
		if doneItem.Status == "" {
			doneItem.Status = "completed"
		}
		if err := appendEvent(&dto.ResponsesStreamResponse{
			Type:        "response.output_item.done",
			OutputIndex: common.GetPointer(index),
			Item:        &doneItem,
		}); err != nil {
			return err
		}
	}

	terminalType := "response.completed"
	if status == "incomplete" {
		terminalType = "response.incomplete"
	}
	return appendEvent(&dto.ResponsesStreamResponse{Type: terminalType, Response: &response})
}

func appendGeminiJSONAsSSE(stream *bytes.Buffer, body []byte) error {
	var response dto.GeminiChatResponse
	if err := common.Unmarshal(body, &response); err == nil && (len(response.Candidates) > 0 || response.PromptFeedback != nil || response.HasUsageMetadata) {
		return appendSSEJSON(stream, &response)
	}

	var responses []dto.GeminiChatResponse
	if err := common.Unmarshal(body, &responses); err != nil {
		return fmt.Errorf("decode Gemini JSON response: %w", err)
	}
	if len(responses) == 0 {
		return fmt.Errorf("Gemini JSON response is empty")
	}
	for index := range responses {
		if err := appendSSEJSON(stream, &responses[index]); err != nil {
			return err
		}
	}
	return nil
}

func appendSSEJSON(stream *bytes.Buffer, value any) error {
	data, err := common.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode synthetic SSE event: %w", err)
	}
	stream.WriteString("data: ")
	stream.Write(data)
	stream.WriteString("\n\n")
	return nil
}
