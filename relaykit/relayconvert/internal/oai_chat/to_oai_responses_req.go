package oaichat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/toolconv"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/samber/lo"
)

func normalizeChatImageURLToString(v any) any {
	switch vv := v.(type) {
	case string:
		return vv
	case map[string]any:
		if url := kitutil.Interface2String(vv["url"]); url != "" {
			return url
		}
		return v
	case dto.MessageImageUrl:
		if vv.Url != "" {
			return vv.Url
		}
		return v
	case *dto.MessageImageUrl:
		if vv != nil && vv.Url != "" {
			return vv.Url
		}
		return v
	default:
		return v
	}
}

func convertChatResponseFormatToResponsesText(reqFormat *dto.ResponseFormat) json.RawMessage {
	if reqFormat == nil || strings.TrimSpace(reqFormat.Type) == "" {
		return nil
	}

	format := map[string]any{
		"type": reqFormat.Type,
	}

	if reqFormat.Type == "json_schema" && len(reqFormat.JsonSchema) > 0 {
		var chatSchema map[string]any
		if err := kitutil.Unmarshal(reqFormat.JsonSchema, &chatSchema); err == nil {
			for key, value := range chatSchema {
				if key == "type" {
					continue
				}
				format[key] = value
			}

			if nested, ok := format["json_schema"].(map[string]any); ok {
				for key, value := range nested {
					if _, exists := format[key]; !exists {
						format[key] = value
					}
				}
				delete(format, "json_schema")
			}
		} else {
			format["json_schema"] = reqFormat.JsonSchema
		}
	}

	textRaw, _ := kitutil.Marshal(map[string]any{
		"format": format,
	})
	return textRaw
}

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	return ChatCompletionsRequestToResponsesRequestWithContext(context.Background(), req)
}

func ChatCompletionsRequestToResponsesRequestWithContext(c context.Context, req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if lo.FromPtrOr(req.N, 1) > 1 {
		return nil, fmt.Errorf("n>1 is not supported in responses compatibility mode")
	}

	messages := append([]dto.Message(nil), req.Messages...)
	pendingLegacyCallID := ""
	for index := range messages {
		role := strings.TrimSpace(messages[index].Role)
		switch role {
		case "assistant":
			if messages[index].FunctionCall != nil {
				toolCalls := messages[index].ParseToolCalls()
				if len(toolCalls) > 0 {
					pendingLegacyCallID = toolCalls[0].ID
				}
			}
		case "function":
			if strings.TrimSpace(messages[index].ToolCallId) == "" {
				messages[index].ToolCallId = pendingLegacyCallID
			}
			pendingLegacyCallID = ""
		case "tool":
			pendingLegacyCallID = ""
		default:
			pendingLegacyCallID = ""
		}
	}

	var instructionsParts []string
	inputItems := make([]map[string]any, 0, len(messages))

	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}

		if role == "tool" || role == "function" {
			callID := strings.TrimSpace(msg.ToolCallId)

			var output any
			if msg.Content == nil {
				output = ""
			} else if msg.IsStringContent() {
				output = msg.StringContent()
			} else {
				if b, err := kitutil.Marshal(msg.Content); err == nil {
					output = string(b)
				} else {
					output = fmt.Sprintf("%v", msg.Content)
				}
			}

			if callID == "" {
				return nil, fmt.Errorf("%s message is missing tool_call_id and cannot be matched to a function call", role)
			}

			inputItems = append(inputItems, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  output,
			})
			continue
		}

		// Prefer mapping system/developer messages into `instructions`.
		if role == "system" || role == "developer" {
			if msg.Content == nil {
				continue
			}
			if msg.IsStringContent() {
				content := msg.StringContent()
				if strings.TrimSpace(content) != "" {
					instructionsParts = append(instructionsParts, content)
				}
				continue
			}
			parts := msg.ParseContent()
			var sb strings.Builder
			for _, part := range parts {
				if part.Type == dto.ContentTypeText && strings.TrimSpace(part.Text) != "" {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(part.Text)
				}
			}
			content := sb.String()
			if strings.TrimSpace(content) != "" {
				instructionsParts = append(instructionsParts, content)
			}
			continue
		}

		item := map[string]any{
			"role": role,
		}

		if msg.Content == nil {
			item["content"] = ""
			inputItems = append(inputItems, item)

			if role == "assistant" {
				for _, tc := range msg.ParseToolCalls() {
					if strings.TrimSpace(tc.ID) == "" {
						continue
					}
					if tc.Type != "" && tc.Type != "function" {
						continue
					}
					name := strings.TrimSpace(tc.Function.Name)
					if name == "" {
						continue
					}
					inputItems = append(inputItems, map[string]any{
						"type":      "function_call",
						"call_id":   tc.ID,
						"name":      name,
						"arguments": tc.Function.Arguments,
					})
				}
			}
			continue
		}

		if msg.IsStringContent() {
			item["content"] = msg.StringContent()
			inputItems = append(inputItems, item)

			if role == "assistant" {
				for _, tc := range msg.ParseToolCalls() {
					if strings.TrimSpace(tc.ID) == "" {
						continue
					}
					if tc.Type != "" && tc.Type != "function" {
						continue
					}
					name := strings.TrimSpace(tc.Function.Name)
					if name == "" {
						continue
					}
					inputItems = append(inputItems, map[string]any{
						"type":      "function_call",
						"call_id":   tc.ID,
						"name":      name,
						"arguments": tc.Function.Arguments,
					})
				}
			}
			continue
		}

		parts := msg.ParseContent()
		contentParts := make([]map[string]any, 0, len(parts))
		for _, part := range parts {
			switch part.Type {
			case dto.ContentTypeText:
				textType := "input_text"
				if role == "assistant" {
					textType = "output_text"
				}
				contentParts = append(contentParts, map[string]any{
					"type": textType,
					"text": part.Text,
				})
			case dto.ContentTypeImageURL:
				contentParts = append(contentParts, map[string]any{
					"type":      "input_image",
					"image_url": normalizeChatImageURLToString(part.ImageUrl),
				})
			case dto.ContentTypeInputAudio:
				contentParts = append(contentParts, map[string]any{
					"type":        "input_audio",
					"input_audio": part.InputAudio,
				})
			case dto.ContentTypeFile:
				contentParts = append(contentParts, map[string]any{
					"type": "input_file",
					"file": part.File,
				})
			case dto.ContentTypeVideoUrl:
				contentParts = append(contentParts, map[string]any{
					"type":      "input_video",
					"video_url": part.VideoUrl,
				})
			default:
				contentParts = append(contentParts, map[string]any{
					"type": part.Type,
				})
			}
		}
		item["content"] = contentParts
		inputItems = append(inputItems, item)

		if role == "assistant" {
			for _, tc := range msg.ParseToolCalls() {
				if strings.TrimSpace(tc.ID) == "" {
					continue
				}
				if tc.Type != "" && tc.Type != "function" {
					continue
				}
				name := strings.TrimSpace(tc.Function.Name)
				if name == "" {
					continue
				}
				inputItems = append(inputItems, map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      name,
					"arguments": tc.Function.Arguments,
				})
			}
		}
	}

	inputRaw, err := kitutil.Marshal(inputItems)
	if err != nil {
		return nil, err
	}

	var instructionsRaw json.RawMessage
	if len(instructionsParts) > 0 {
		instructions := strings.Join(instructionsParts, "\n\n")
		instructionsRaw, _ = kitutil.Marshal(instructions)
	}

	var toolsRaw json.RawMessage
	responsesTools, err := sharedbridge.EnsureResponsesFunctionTools(nil, inputItems)
	if err != nil {
		return nil, err
	}
	if len(responsesTools) > 0 {
		toolsRaw, _ = kitutil.Marshal(responsesTools)
	}

	textRaw := convertChatResponseFormatToResponsesText(req.ResponseFormat)
	if len(req.Verbosity) > 0 {
		config := map[string]json.RawMessage{}
		if len(textRaw) > 0 {
			if err := kitutil.Unmarshal(textRaw, &config); err != nil {
				return nil, err
			}
		}
		config["verbosity"] = req.Verbosity
		textRaw, err = kitutil.Marshal(config)
		if err != nil {
			return nil, err
		}
	}

	maxOutputTokens := lo.FromPtrOr(req.MaxTokens, uint(0))
	if req.MaxCompletionTokens != nil {
		maxOutputTokens = *req.MaxCompletionTokens
	}
	// OpenAI Responses API rejects max_output_tokens < 16 when explicitly provided.
	//if maxOutputTokens > 0 && maxOutputTokens < 16 {
	//	maxOutputTokens = 16
	//}

	var topP *float64
	if req.TopP != nil {
		topP = kitutil.GetPointer(lo.FromPtr(req.TopP))
	}

	var frequencyPenaltyRaw, presencePenaltyRaw json.RawMessage
	if req.FrequencyPenalty != nil {
		frequencyPenaltyRaw, _ = kitutil.Marshal(req.FrequencyPenalty)
	}
	if req.PresencePenalty != nil {
		presencePenaltyRaw, _ = kitutil.Marshal(req.PresencePenalty)
	}

	var promptCacheKeyRaw json.RawMessage
	if req.PromptCacheKey != "" {
		promptCacheKeyRaw, err = kitutil.Marshal(req.PromptCacheKey)
		if err != nil {
			return nil, fmt.Errorf("marshal prompt_cache_key: %w", err)
		}
	}

	out := &dto.OpenAIResponsesRequest{
		Model:                req.Model,
		Input:                inputRaw,
		Instructions:         instructionsRaw,
		Stream:               req.Stream,
		Temperature:          req.Temperature,
		Text:                 textRaw,
		Tools:                toolsRaw,
		TopP:                 topP,
		FrequencyPenalty:     frequencyPenaltyRaw,
		PresencePenalty:      presencePenaltyRaw,
		User:                 req.User,
		Store:                req.Store,
		Metadata:             req.Metadata,
		PromptCacheKey:       promptCacheKeyRaw,
		EnableThinking:       req.EnableThinking,
		ThinkingBudget:       req.ThinkingBudget,
		TopLogProbs:          req.TopLogProbs,
		SafetyIdentifier:     req.SafetyIdentifier,
		PromptCacheRetention: req.PromptCacheRetention,
	}
	if req.LogProbs != nil && *req.LogProbs {
		out.Include = []byte(`["message.output_text.logprobs"]`)
	}
	if len(req.ServiceTier) > 0 {
		if err := kitutil.Unmarshal(req.ServiceTier, &out.ServiceTier); err != nil {
			return nil, err
		}
	}
	if req.MaxTokens != nil || req.MaxCompletionTokens != nil {
		out.MaxOutputTokens = lo.ToPtr(maxOutputTokens)
	}

	reasoningIntent, diagnostics, err := reasoning.FromOpenAIChat(req)
	if err != nil {
		return nil, reasoning.AsClientError(err)
	}
	convdiag.Add(c, diagnostics...)
	if err := reasoning.ApplyToOpenAIResponses(out, reasoningIntent); err != nil {
		return nil, reasoning.AsClientError(err)
	}
	if err := toolconv.RenderRequestTools(c, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, req, out, nil); err != nil {
		return nil, err
	}

	return out, nil
}
