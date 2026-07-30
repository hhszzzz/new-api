package claudemessages

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	sharedchat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/chat"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	sharedtoolmedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/toolmedia"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func ClaudeMessagesRequestToOpenAIResponses(claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.OpenAIResponsesRequest, error) {
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
	if sharedchat.SupportsReasoningEffort(claudeRequest.Model) {
		if effort := claudeRequestReasoningEffort(&claudeRequest); effort != "" {
			request.Reasoning = &dto.Reasoning{Effort: effort}
		}
	}

	tools, declaredTools, err := claudeToolsToResponses(claudeRequest.Tools)
	if err != nil {
		return nil, err
	}
	hasCurrentTools := len(tools) > 0
	tools, err = sharedbridge.EnsureResponsesFunctionTools(tools, input)
	if err != nil {
		return nil, err
	}
	if len(tools) > 0 {
		request.Tools, err = kitutil.Marshal(tools)
		if err != nil {
			return nil, err
		}
	}
	toolChoice, parallelToolCalls, err := claudeToolChoiceToResponses(claudeRequest.ToolChoice, declaredTools)
	if err != nil {
		return nil, err
	}
	if toolChoice != nil && hasCurrentTools {
		request.ToolChoice, err = kitutil.Marshal(toolChoice)
		if err != nil {
			return nil, err
		}
	}
	if parallelToolCalls != nil && hasCurrentTools {
		request.ParallelToolCalls, err = kitutil.Marshal(*parallelToolCalls)
		if err != nil {
			return nil, err
		}
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

func claudeToolsToResponses(value any) ([]map[string]any, map[string]struct{}, error) {
	declared := make(map[string]struct{})
	if value == nil {
		return nil, declared, nil
	}
	tools, err := kitutil.Any2Type[[]map[string]any](value)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Claude tools: %w", err)
	}
	converted := make([]map[string]any, 0, len(tools))
	for index, tool := range tools {
		toolType := strings.TrimSpace(kitutil.Interface2String(tool["type"]))
		if toolType == "BatchTool" {
			continue
		}
		if toolType != "" && toolType != "custom" {
			if isClaudeServerToolType(toolType) {
				// Server-executed Anthropic tools (web_search, code_execution,
				// ...) cannot run on a converted upstream. Drop them, CC Switch
				// style, and let the model work without them.
				continue
			}
			// Client-executed typed tools (bash_*, text_editor_*, memory_*)
			// lower to plain functions: the client still executes the calls,
			// and Claude-family models know these tool shapes by name.
		}
		name := strings.TrimSpace(kitutil.Interface2String(tool["name"]))
		if name == "" {
			return nil, nil, fmt.Errorf("Claude tool %d is missing name", index)
		}
		if _, exists := declared[name]; exists {
			return nil, nil, fmt.Errorf("Claude tool name %q is declared more than once", name)
		}
		declared[name] = struct{}{}
		schema, ok := tool["input_schema"].(map[string]any)
		if !ok || len(schema) == 0 {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		} else {
			schema = cloneStringAnyMap(schema)
			if strings.TrimSpace(kitutil.Interface2String(schema["type"])) == "" {
				schema["type"] = "object"
			}
			if _, exists := schema["properties"]; !exists {
				schema["properties"] = map[string]any{}
			}
		}
		responseTool := map[string]any{
			"type":        "function",
			"name":        name,
			"description": kitutil.Interface2String(tool["description"]),
			"parameters":  schema,
		}
		if strict, ok := tool["strict"].(bool); ok {
			responseTool["strict"] = strict
		}
		converted = append(converted, responseTool)
	}
	return converted, declared, nil
}

// isClaudeServerToolType reports whether an Anthropic typed tool executes on
// Anthropic's servers, which a converted upstream can never reproduce.
func isClaudeServerToolType(toolType string) bool {
	for _, marker := range []string{"web_search", "web_fetch", "computer", "code_execution", "tool_search"} {
		if strings.Contains(toolType, marker) {
			return true
		}
	}
	return false
}

func claudeToolChoiceToResponses(value any, declared map[string]struct{}) (any, *bool, error) {
	if value == nil {
		return nil, nil, nil
	}
	choice := dto.ClaudeToolChoice{}
	if typed, ok := value.(string); ok {
		choice.Type = typed
	} else {
		converted, err := kitutil.Any2Type[dto.ClaudeToolChoice](value)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid Claude tool_choice: %w", err)
		}
		choice = converted
	}

	parallel := !choice.DisableParallelToolUse
	switch choice.Type {
	case "auto":
		return "auto", &parallel, nil
	case "any":
		if len(declared) == 0 {
			return nil, nil, fmt.Errorf("Claude tool_choice type any requires at least one declared tool")
		}
		return "required", &parallel, nil
	case "none":
		return "none", nil, nil
	case "tool":
		name := strings.TrimSpace(choice.Name)
		if name == "" {
			return nil, nil, fmt.Errorf("Claude tool_choice type tool requires a name")
		}
		if _, exists := declared[name]; !exists {
			return nil, nil, fmt.Errorf("Claude tool_choice references undeclared tool %q", name)
		}
		return map[string]any{"type": "function", "name": name}, &parallel, nil
	default:
		return nil, nil, fmt.Errorf("unsupported Claude tool_choice type %q", choice.Type)
	}
}

func claudeRequestReasoningEffort(request *dto.ClaudeRequest) string {
	if request == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(request.GetEfforts())) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(request.GetEfforts()))
	case "max", "xhigh":
		return "xhigh"
	}
	if request.Thinking == nil {
		return ""
	}
	switch request.Thinking.Type {
	case "adaptive":
		return "xhigh"
	case "enabled":
		budget := request.Thinking.GetBudgetTokens()
		switch {
		case budget == 0:
			return "high"
		case budget < 4000:
			return "low"
		case budget < 16000:
			return "medium"
		default:
			return "high"
		}
	default:
		return ""
	}
}

func cloneStringAnyMap(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}
