package claudemessages

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	sharedtoolmedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/toolmedia"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/toolconv"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func ClaudeMessagesRequestToOpenAIResponses(claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.OpenAIResponsesRequest, error) {
	return ClaudeMessagesRequestToOpenAIResponsesWithContext(context.Background(), claudeRequest, info)
}

func ClaudeMessagesRequestToOpenAIResponsesWithContext(c context.Context, claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.OpenAIResponsesRequest, error) {
	if strings.TrimSpace(claudeRequest.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if err := validateClaudeRequestConversion(&claudeRequest, "Responses"); err != nil {
		return nil, err
	}

	instructions, err := claudeSystemInstructions(claudeRequest.System)
	if err != nil {
		return nil, err
	}
	input, err := claudeMessagesToResponsesInput(claudeRequest.Messages)
	if err != nil {
		return nil, err
	}
	inputRaw, err := kitutil.Marshal(input)
	if err != nil {
		return nil, err
	}

	request := &dto.OpenAIResponsesRequest{
		Model:           claudeRequest.Model,
		Input:           inputRaw,
		MaxOutputTokens: claudeRequest.MaxTokens,
		Stream:          claudeRequest.Stream,
		Temperature:     claudeRequest.Temperature,
		TopP:            claudeRequest.TopP,
		Metadata:        append([]byte(nil), claudeRequest.Metadata...),
		ServiceTier:     claudeRequest.ServiceTier,
	}
	if convmeta.OptionsOf(info).IncludeReasoningEncryptedContent {
		request.Include, err = kitutil.Marshal([]string{"reasoning.encrypted_content"})
		if err != nil {
			return nil, err
		}
	}
	if instructions != "" {
		request.Instructions, err = kitutil.Marshal(instructions)
		if err != nil {
			return nil, err
		}
	}
	if request.MaxOutputTokens == nil {
		request.MaxOutputTokens = claudeRequest.MaxTokensToSample
	}

	reasoningIntent, effectiveEffort, err := claudeRequestReasoningIntent(c, &claudeRequest, info)
	if err != nil {
		return nil, reasoning.AsClientError(err)
	}
	if err := reasoning.ApplyToOpenAIResponses(request, reasoningIntent); err != nil {
		return nil, reasoning.AsClientError(err)
	}
	if info != nil && effectiveEffort != "" {
		info.SetReasoningEffort(string(effectiveEffort))
	}

	tools, err := sharedbridge.EnsureResponsesFunctionTools(nil, input)
	if err != nil {
		return nil, err
	}
	if len(tools) > 0 {
		request.Tools, err = kitutil.Marshal(tools)
		if err != nil {
			return nil, err
		}
	}
	if err := toolconv.RenderRequestTools(c, types.RelayFormatClaude, types.RelayFormatOpenAIResponses, &claudeRequest, request, convmeta.OptionsOf(info)); err != nil {
		return nil, err
	}
	return request, nil
}

func claudeSystemInstructions(system any) (string, error) {
	if system == nil {
		return "", nil
	}
	if text, ok := system.(string); ok {
		text = sharedclaude.StripLeadingBillingHeader(text)
		if strings.TrimSpace(text) == "" {
			return "", nil
		}
		return text, nil
	}
	parts, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](system)
	if err != nil {
		return "", fmt.Errorf("invalid Claude system content: %w", err)
	}
	texts := make([]string, 0, len(parts))
	for index, part := range parts {
		if part.Type != "text" {
			return "", fmt.Errorf("Claude system content %d type %q cannot be converted to Responses instructions", index, part.Type)
		}
		text := sharedclaude.StripLeadingBillingHeader(part.GetText())
		if strings.TrimSpace(text) != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n\n"), nil
}

func claudeMessagesToResponsesInput(messages []dto.ClaudeMessage) ([]map[string]any, error) {
	input := make([]map[string]any, 0, len(messages))
	for messageIndex, message := range messages {
		messageInputStart := len(input)
		role := strings.TrimSpace(message.Role)
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("Claude message %d has unsupported role %q", messageIndex, message.Role)
		}
		if role == "assistant" && len(message.ProviderResponsesRawOutput) > 0 {
			for outputIndex, output := range message.ProviderResponsesRawOutput {
				var restored map[string]any
				if convertErr := kitutil.Unmarshal(output, &restored); convertErr != nil {
					return nil, fmt.Errorf("Claude message %d raw provider Responses output %d: %w", messageIndex, outputIndex, convertErr)
				}
				if strings.TrimSpace(kitutil.Interface2String(restored["type"])) == "" {
					return nil, fmt.Errorf("Claude message %d raw provider Responses output %d is missing type", messageIndex, outputIndex)
				}
				input = append(input, restored)
			}
			continue
		}
		if role == "assistant" && len(message.ProviderResponsesOutput) > 0 {
			for outputIndex, output := range message.ProviderResponsesOutput {
				if strings.TrimSpace(output.Type) == "" {
					return nil, fmt.Errorf("Claude message %d provider Responses output %d is missing type", messageIndex, outputIndex)
				}
				restored, convertErr := kitutil.Any2Type[map[string]any](output)
				if convertErr != nil {
					return nil, fmt.Errorf("Claude message %d provider Responses output %d: %w", messageIndex, outputIndex, convertErr)
				}
				input = append(input, restored)
			}
			continue
		}
		if message.IsStringContent() {
			textType := "input_text"
			if role == "assistant" {
				textType = "output_text"
			}
			input = append(input, map[string]any{
				"type": "message",
				"role": role,
				"content": []map[string]any{{
					"type": textType,
					"text": message.GetStringContent(),
				}},
			})
			continue
		}

		blocks, err := message.ParseContent()
		if err != nil {
			return nil, fmt.Errorf("invalid Claude message %d content: %w", messageIndex, err)
		}
		messageContent := make([]map[string]any, 0, len(blocks))
		flushMessage := func() {
			if len(messageContent) == 0 {
				return
			}
			content := append([]map[string]any(nil), messageContent...)
			input = append(input, map[string]any{
				"type":    "message",
				"role":    role,
				"content": content,
			})
			messageContent = messageContent[:0]
		}

		for contentIndex, block := range blocks {
			switch block.Type {
			case "text", "input_text":
				textType := "input_text"
				if role == "assistant" {
					textType = "output_text"
				}
				messageContent = append(messageContent, map[string]any{
					"type": textType,
					"text": block.GetText(),
				})
			case "image":
				if role != "user" {
					return nil, fmt.Errorf("Claude assistant message %d image content %d cannot be converted to Responses input", messageIndex, contentIndex)
				}
				part, err := claudeImageToResponsesPart(block)
				if err != nil {
					return nil, fmt.Errorf("Claude message %d image content %d: %w", messageIndex, contentIndex, err)
				}
				messageContent = append(messageContent, part)
			case "document":
				if role != "user" {
					return nil, fmt.Errorf("Claude assistant message %d document content %d cannot be converted to Responses input", messageIndex, contentIndex)
				}
				part, err := claudeDocumentToResponsesPart(block)
				if err != nil {
					return nil, fmt.Errorf("Claude message %d document content %d: %w", messageIndex, contentIndex, err)
				}
				messageContent = append(messageContent, part)
			case "tool_use":
				flushMessage()
				callID := strings.TrimSpace(block.Id)
				name := strings.TrimSpace(block.Name)
				if callID == "" || name == "" {
					return nil, fmt.Errorf("Claude message %d tool_use content %d requires id and name", messageIndex, contentIndex)
				}
				arguments := block.Input
				if arguments == nil {
					arguments = map[string]any{}
				}
				argumentsRaw, err := kitutil.Marshal(arguments)
				if err != nil {
					return nil, fmt.Errorf("Claude message %d tool_use content %d input: %w", messageIndex, contentIndex, err)
				}
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      name,
					"arguments": string(argumentsRaw),
				})
			case "tool_result":
				flushMessage()
				callID := strings.TrimSpace(block.ToolUseId)
				if callID == "" {
					return nil, fmt.Errorf("Claude message %d tool_result content %d requires tool_use_id", messageIndex, contentIndex)
				}
				output, err := claudeToolResultToResponsesOutput(block)
				if err != nil {
					return nil, fmt.Errorf("Claude message %d tool_result content %d: %w", messageIndex, contentIndex, err)
				}
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  output,
				})
			case "thinking", "redacted_thinking":
				// Anthropic signatures and redacted thinking are provider-bound. A
				// Messages client must never be able to smuggle either value into a
				// Responses provider. Same-channel Responses state is restored only
				// from ProviderResponsesOutput, which the host keeps server-side.
				flushMessage()
				continue
			default:
				return nil, fmt.Errorf("Claude message %d content %d type %q cannot be converted to Responses", messageIndex, contentIndex, block.Type)
			}
		}
		flushMessage()
		if role == "assistant" {
			hasGeneratedFollower := false
			for index := len(input) - 1; index >= messageInputStart; index-- {
				itemType := strings.TrimSpace(kitutil.Interface2String(input[index]["type"]))
				if itemType == "reasoning" {
					if !hasGeneratedFollower {
						input = append(input[:index], input[index+1:]...)
					}
					continue
				}
				if itemType == "function_call" || strings.TrimSpace(kitutil.Interface2String(input[index]["role"])) == "assistant" {
					hasGeneratedFollower = true
				}
			}
		}
	}
	return input, nil
}

func claudeImageToResponsesPart(block dto.ClaudeMediaMessage) (map[string]any, error) {
	if block.Source == nil {
		return nil, fmt.Errorf("image source is missing")
	}
	switch strings.TrimSpace(block.Source.Type) {
	case "url":
		url := strings.TrimSpace(block.Source.Url)
		if url == "" {
			return nil, fmt.Errorf("image URL is empty")
		}
		return map[string]any{"type": "input_image", "image_url": url}, nil
	case "base64", "":
		data := strings.TrimSpace(kitutil.Interface2String(block.Source.Data))
		if data == "" {
			return nil, fmt.Errorf("base64 image data is empty")
		}
		mediaType := strings.TrimSpace(block.Source.MediaType)
		if mediaType == "" {
			mediaType = "image/png"
		}
		return map[string]any{
			"type":      "input_image",
			"image_url": fmt.Sprintf("data:%s;base64,%s", mediaType, data),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported image source type %q", block.Source.Type)
	}
}

func claudeDocumentToResponsesPart(block dto.ClaudeMediaMessage) (map[string]any, error) {
	if block.Source == nil {
		return nil, fmt.Errorf("document source is missing")
	}
	filename := strings.TrimSpace(block.Title)
	if filename == "" {
		filename = strings.TrimSpace(block.Filename)
	}
	if filename == "" {
		filename = "document.pdf"
	}
	switch strings.TrimSpace(block.Source.Type) {
	case "url":
		url := strings.TrimSpace(block.Source.Url)
		if url == "" {
			return nil, fmt.Errorf("document URL is empty")
		}
		return map[string]any{
			"type":     "input_file",
			"file_url": url,
			"filename": filename,
		}, nil
	case "base64":
		data := strings.TrimSpace(kitutil.Interface2String(block.Source.Data))
		if data == "" {
			return nil, fmt.Errorf("base64 document data is empty")
		}
		mediaType := strings.TrimSpace(block.Source.MediaType)
		if mediaType == "" {
			mediaType = "application/pdf"
		}
		return map[string]any{
			"type":      "input_file",
			"file_data": fmt.Sprintf("data:%s;base64,%s", mediaType, data),
			"filename":  filename,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported document source type %q", block.Source.Type)
	}
}

func claudeToolResultToResponsesOutput(block dto.ClaudeMediaMessage) (any, error) {
	isError := block.IsError != nil && *block.IsError
	cleaned, media, changed, err := sharedtoolmedia.StripAndClamp(
		block.Content,
		sharedtoolmedia.ImagesOnly,
		map[string]any{"type": "input_text", "text": sharedtoolmedia.ToolResultMediaAttachedMarker},
		sharedtoolmedia.ToolResultMediaAttachedMarker,
	)
	if err != nil {
		return nil, err
	}
	if changed {
		output := make([]map[string]any, 0, len(media)+2)
		if isError {
			output = append(output, map[string]any{
				"type": "input_text",
				"text": sharedbridge.ClaudeToolResultErrorMarker,
			})
		}
		if err := appendResponsesToolOutputValue(&output, cleaned); err != nil {
			return nil, err
		}
		for _, item := range media {
			url := sharedtoolmedia.ImageURL(item)
			if url == "" {
				continue
			}
			part := map[string]any{"type": "input_image", "image_url": url}
			if detail := sharedtoolmedia.ImageDetail(item); detail != nil {
				part["detail"] = detail
			}
			output = append(output, part)
		}
		return output, nil
	}
	if block.IsStringContent() && !isError {
		return block.GetStringContent(), nil
	}

	output := make([]map[string]any, 0)
	if isError {
		output = append(output, map[string]any{
			"type": "input_text",
			"text": sharedbridge.ClaudeToolResultErrorMarker,
		})
	}
	if block.Content == nil {
		return output, nil
	}
	if block.IsStringContent() {
		output = append(output, map[string]any{"type": "input_text", "text": block.GetStringContent()})
		return output, nil
	}

	parts, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](block.Content)
	if err != nil {
		encoded, marshalErr := kitutil.Marshal(block.Content)
		if marshalErr != nil {
			return nil, marshalErr
		}
		output = append(output, map[string]any{"type": "input_text", "text": string(encoded)})
		return output, nil
	}
	for _, part := range parts {
		switch part.Type {
		case "text", "input_text":
			output = append(output, map[string]any{"type": "input_text", "text": part.GetText()})
		case "image":
			converted, err := claudeImageToResponsesPart(part)
			if err != nil {
				return nil, err
			}
			output = append(output, converted)
		case "document":
			converted, err := claudeDocumentToResponsesPart(part)
			if err != nil {
				return nil, err
			}
			output = append(output, converted)
		default:
			encoded, err := kitutil.Marshal(part)
			if err != nil {
				return nil, err
			}
			output = append(output, map[string]any{"type": "input_text", "text": string(encoded)})
		}
	}
	return output, nil
}

func appendResponsesToolOutputValue(output *[]map[string]any, value any) error {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if typed != "" {
			*output = append(*output, map[string]any{"type": "input_text", "text": typed})
		}
		return nil
	case []any:
		for _, item := range typed {
			part, ok := item.(map[string]any)
			if ok {
				partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
				if partType == "input_text" || partType == "output_text" || partType == "text" {
					*output = append(*output, map[string]any{
						"type": "input_text",
						"text": kitutil.Interface2String(part["text"]),
					})
					continue
				}
			}
			raw, err := kitutil.Marshal(item)
			if err != nil {
				return err
			}
			*output = append(*output, map[string]any{"type": "input_text", "text": string(raw)})
		}
		return nil
	case map[string]any:
		partType := strings.TrimSpace(kitutil.Interface2String(typed["type"]))
		if partType == "input_text" || partType == "output_text" || partType == "text" {
			*output = append(*output, map[string]any{
				"type": "input_text",
				"text": kitutil.Interface2String(typed["text"]),
			})
			return nil
		}
	}
	raw, err := kitutil.Marshal(value)
	if err != nil {
		return err
	}
	*output = append(*output, map[string]any{"type": "input_text", "text": string(raw)})
	return nil
}

func claudeRequestReasoningIntent(ctx context.Context, claudeRequest *dto.ClaudeRequest, info convmeta.Meta) (reasoning.Intent, reasoning.Effort, error) {
	reasoningIntent, diagnostics, err := reasoning.FromClaude(claudeRequest)
	if err != nil {
		return reasoning.Intent{}, "", err
	}
	convdiag.Add(ctx, diagnostics...)
	sourceModel := claudeRequest.Model
	if info != nil && info.GetOriginModelName() != "" {
		sourceModel = info.GetOriginModelName()
	}
	if suffix := reasoning.IntentFromState(convmeta.ReasoningStateOf(info)); !suffix.IsEmpty() {
		reasoningIntent, diagnostics, err = reasoning.MergeExplicitAndSuffix(reasoningIntent, suffix, sourceModel)
		if err != nil {
			return reasoning.Intent{}, "", err
		}
		convdiag.Add(ctx, diagnostics...)
	}
	reasoningIntent = reasoning.ResolveClaudeDefault(sourceModel, reasoningIntent)
	return reasoningIntent, reasoning.EffectiveEffort(reasoningIntent), nil
}
