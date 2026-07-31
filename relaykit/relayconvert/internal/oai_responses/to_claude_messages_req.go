package oairesponses

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	sharedtoolmedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/toolmedia"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func convertOpenAIResponsesRequestToClaudeMessages(c context.Context, info convmeta.Meta, request any) (any, error) {
	responsesRequest, err := OpenAIResponsesRequestFromAny(request)
	if err != nil {
		return nil, err
	}
	return OpenAIResponsesRequestToClaudeMessages(c, info, responsesRequest)
}

func OpenAIResponsesRequestToClaudeMessages(c context.Context, info convmeta.Meta, req *dto.OpenAIResponsesRequest) (*dto.ClaudeRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if err := ValidateRequestChatUnsupportedFields(req); err != nil {
		return nil, err
	}

	claudeRequest := &dto.ClaudeRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}
	if req.MaxOutputTokens != nil && *req.MaxOutputTokens > 0 {
		claudeRequest.MaxTokens = kitutil.GetPointer(*req.MaxOutputTokens)
	}
	if claudeRequest.MaxTokens == nil || *claudeRequest.MaxTokens == 0 {
		if defaultMaxTokens, configured := convmeta.OptionsOf(info).Claude.DefaultMaxTokensFor(req.Model); configured {
			value := uint(defaultMaxTokens)
			claudeRequest.MaxTokens = &value
		}
	}

	tools, toolState, err := prepareResponsesToolsForChat(c, req)
	if err != nil {
		return nil, err
	}
	if len(tools) > 0 {
		functions := make([]dto.FunctionRequest, 0, len(tools))
		for _, tool := range tools {
			if tool.Type != "function" {
				return nil, fmt.Errorf("Responses tool type %q cannot be converted to Claude Messages", tool.Type)
			}
			functions = append(functions, tool.Function)
		}
		claudeRequest.Tools = responsesFunctionDeclarationsToClaudeTools(functions)
	}

	toolChoice, err := responsesRequestToolChoiceToChat(req.ToolChoice, toolState)
	if err != nil {
		return nil, err
	}
	if len(tools) > 0 && (toolChoice != nil || RawJSONPresent(req.ParallelToolCalls)) {
		claudeRequest.ToolChoice = sharedclaude.MapOpenAIToolChoice(toolChoice, ParallelToolCalls(req.ParallelToolCalls))
	}
	systemMessages := make([]dto.ClaudeMediaMessage, 0)
	if RawJSONPresent(req.Instructions) {
		instructions, err := JSONString(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("invalid instructions: %w", err)
		}
		if strings.TrimSpace(instructions) != "" {
			systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
				Type: "text",
				Text: kitutil.GetPointer(instructions),
			})
		}
	}

	inputItems, err := InputItems(req.Input)
	if err != nil {
		return nil, err
	}
	inputItems, err = filterIncompleteResponsesToolHistory(inputItems, true)
	if err != nil {
		return nil, err
	}
	for _, item := range inputItems {
		itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
		if (itemType == ResponsesInputTypeFunctionCall || itemType == ResponsesInputTypeCustomToolCall || itemType == "tool_search_call" || itemType == "local_shell_call") &&
			strings.TrimSpace(kitutil.Interface2String(item["status"])) == "incomplete" {
			continue
		}
		switch itemType {
		case ResponsesInputTypeFunctionCall:
			toolUse, err := responsesCallItemToClaudeToolUse(item, "arguments", sharedbridge.ToolKindFunction, toolState)
			if err != nil {
				return nil, err
			}
			claudeRequest.Messages = appendClaudeToolUse(claudeRequest.Messages, toolUse)
		case ResponsesInputTypeCustomToolCall:
			toolUse, err := responsesCallItemToClaudeToolUse(item, "input", sharedbridge.ToolKindCustom, toolState)
			if err != nil {
				return nil, err
			}
			claudeRequest.Messages = appendClaudeToolUse(claudeRequest.Messages, toolUse)
		case "tool_search_call":
			toolUse, err := responsesCallItemToClaudeToolUse(item, "arguments", sharedbridge.ToolKindToolSearch, toolState)
			if err != nil {
				return nil, err
			}
			claudeRequest.Messages = appendClaudeToolUse(claudeRequest.Messages, toolUse)
		case "local_shell_call":
			toolUse, err := responsesCallItemToClaudeToolUse(item, "action", sharedbridge.ToolKindLocalShell, toolState)
			if err != nil {
				return nil, err
			}
			claudeRequest.Messages = appendClaudeToolUse(claudeRequest.Messages, toolUse)
		case ResponsesInputTypeFunctionCallOutput, ResponsesInputTypeCustomToolOutput, "local_shell_call_output":
			toolResult, err := responsesFunctionOutputItemToClaudeToolResult(c, item, "output")
			if err != nil {
				return nil, err
			}
			claudeRequest.Messages = appendClaudeToolResult(claudeRequest.Messages, toolResult)
		case "tool_search_output":
			toolResult, err := responsesFunctionOutputItemToClaudeToolResult(c, item, "tools")
			if err != nil {
				return nil, err
			}
			claudeRequest.Messages = appendClaudeToolResult(claudeRequest.Messages, toolResult)
		case "additional_tools":
			continue
		case "reasoning":
			if info == nil || !info.HasChannelMeta() {
				continue
			}
			block, ok, err := sharedclaude.DecodeThinkingBlock(
				kitutil.Interface2String(item["encrypted_content"]),
				info.GetChannelID(),
				convmeta.OptionsOf(info).ProviderStateSecret,
			)
			if err != nil {
				return nil, fmt.Errorf("invalid Anthropic reasoning state: %w", err)
			}
			if ok {
				claudeRequest.Messages = appendClaudeConversationContent(
					claudeRequest.Messages,
					"assistant",
					[]dto.ClaudeMediaMessage{block},
				)
			}
			continue
		default:
			if isResponsesHostedHistoryItem(itemType) {
				return nil, fmt.Errorf("Responses server tool history item %q cannot be converted to Anthropic Messages without losing context", itemType)
			}
			role := responsesClaudeRole(item)
			if role == "system" {
				parts, err := responsesSystemContentToClaudeText(item["content"])
				if err != nil {
					return nil, err
				}
				systemMessages = append(systemMessages, parts...)
				continue
			}
			parts, err := responsesInputContentToClaudeMediaMessages(c, item["content"])
			if err != nil {
				return nil, err
			}
			if len(parts) == 0 {
				continue
			}
			claudeRequest.Messages = appendClaudeConversationContent(claudeRequest.Messages, role, parts)
		}
	}

	if len(systemMessages) > 0 {
		claudeRequest.System = systemMessages
	}
	claudeRequest.Messages = normalizeResponsesClaudeMessages(claudeRequest.Messages)
	if len(claudeRequest.Messages) == 0 {
		return nil, fmt.Errorf("cannot convert Responses request: empty Messages input")
	}
	if err := applyResponsesReasoningToClaude(req, claudeRequest); err != nil {
		return nil, err
	}
	// Checked last so every injection path has had its chance to satisfy the
	// required field.
	if claudeRequest.MaxTokens == nil {
		return nil, sharedclaude.ErrMissingMaxTokens
	}
	return claudeRequest, nil
}

func responsesFunctionDeclarationsToClaudeTools(functions []dto.FunctionRequest) []any {
	tools := make([]any, 0, len(functions))
	for _, function := range functions {
		tools = append(tools, &dto.Tool{
			Name:        function.Name,
			Description: function.Description,
			InputSchema: responsesFunctionParametersToClaudeInputSchema(function.Parameters),
			Strict:      function.Strict,
		})
	}
	return tools
}

func responsesFunctionParametersToClaudeInputSchema(parameters any) map[string]interface{} {
	if params, ok := parameters.(map[string]any); ok {
		schema := make(map[string]interface{}, len(params))
		for key, value := range params {
			schema[key] = value
		}
		if schema["type"] == nil {
			schema["type"] = "object"
		}
		if schema["properties"] == nil {
			schema["properties"] = map[string]interface{}{}
		}
		return schema
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func applyResponsesReasoningToClaude(req *dto.OpenAIResponsesRequest, claudeRequest *dto.ClaudeRequest) error {
	if claudeRequest == nil || claudeRequest.MaxTokens == nil || *claudeRequest.MaxTokens == 0 {
		return nil
	}
	effort := ReasoningEffort(req)
	adaptive := sharedclaude.UsesAdaptiveThinking(claudeRequest.Model)
	adaptiveByDefault := sharedclaude.AdaptiveThinkingIsDefault(claudeRequest.Model)
	cannotDisable := sharedclaude.ThinkingCannotBeDisabled(claudeRequest.Model)
	adaptiveEffort, hasAdaptiveEffort := sharedclaude.AdaptiveEffort(effort)
	explicitlyDisabled := sharedclaude.ReasoningExplicitlyDisabled(effort)
	adaptiveShouldThink := adaptive && (adaptiveByDefault || hasAdaptiveEffort)

	if !claudeMessagesSupportThinking(claudeRequest.Messages) {
		if cannotDisable {
			return fmt.Errorf("cannot convert Responses request: Anthropic model %q requires thinking, but the tool history has no signed thinking block to replay", claudeRequest.Model)
		}
		if adaptiveShouldThink {
			claudeRequest.Thinking = &dto.Thinking{Type: "disabled"}
		}
		return nil
	}

	if claudeToolChoiceForcesCall(claudeRequest.ToolChoice) {
		if adaptiveShouldThink {
			if cannotDisable {
				return fmt.Errorf("cannot convert Responses request: Anthropic model %q requires adaptive thinking and cannot honor a forced tool_choice", claudeRequest.Model)
			}
			claudeRequest.Thinking = &dto.Thinking{Type: "disabled"}
		}
		return nil
	}

	if adaptiveShouldThink && (!explicitlyDisabled || cannotDisable) {
		claudeRequest.Thinking = &dto.Thinking{Type: "adaptive"}
		if hasAdaptiveEffort {
			outputConfig, err := kitutil.Marshal(map[string]string{"effort": adaptiveEffort})
			if err != nil {
				return fmt.Errorf("marshal Claude output_config: %w", err)
			}
			claudeRequest.OutputConfig = outputConfig
		} else if explicitlyDisabled && cannotDisable {
			claudeRequest.OutputConfig = []byte(`{"effort":"low"}`)
		}
		claudeRequest.Temperature = nil
		claudeRequest.TopP = nil
		return nil
	}
	if adaptive && explicitlyDisabled {
		claudeRequest.Thinking = &dto.Thinking{Type: "disabled"}
		return nil
	}

	var desiredBudget uint
	switch effort {
	case "minimal":
		desiredBudget = 2048
	case "low":
		desiredBudget = 2048
	case "medium":
		desiredBudget = 8192
	case "high":
		desiredBudget = 16384
	case "xhigh", "max":
		desiredBudget = 24576
	default:
		return nil
	}
	ceiling := *claudeRequest.MaxTokens / 2
	if ceiling < 1024 {
		return nil
	}
	if desiredBudget > ceiling {
		desiredBudget = ceiling
	}
	budget := int(desiredBudget)
	claudeRequest.Thinking = &dto.Thinking{
		Type:         "enabled",
		BudgetTokens: &budget,
	}
	claudeRequest.Temperature = nil
	claudeRequest.TopP = nil
	return nil
}

func claudeMessagesSupportThinking(messages []dto.ClaudeMessage) bool {
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" {
		return false
	}
	if messages[len(messages)-1].IsStringContent() {
		return true
	}
	lastParts, err := messages[len(messages)-1].ParseContent()
	if err != nil {
		return false
	}
	toolResultIDs := make([]string, 0)
	for _, part := range lastParts {
		if part.Type != "tool_result" {
			continue
		}
		toolUseID := strings.TrimSpace(part.ToolUseId)
		if toolUseID == "" {
			return false
		}
		toolResultIDs = append(toolResultIDs, toolUseID)
	}
	if len(toolResultIDs) == 0 {
		return true
	}
	if len(messages) < 2 || messages[len(messages)-2].Role != "assistant" {
		return false
	}
	assistantParts, err := messages[len(messages)-2].ParseContent()
	if err != nil {
		return false
	}
	toolUseIDs := make(map[string]struct{})
	hasSignedThinking := false
	for _, part := range assistantParts {
		switch part.Type {
		case "thinking":
			hasSignedThinking = hasSignedThinking || strings.TrimSpace(part.Signature) != ""
		case "redacted_thinking":
			hasSignedThinking = hasSignedThinking || strings.TrimSpace(part.Data) != ""
		case "tool_use":
			if id := strings.TrimSpace(part.Id); id != "" {
				toolUseIDs[id] = struct{}{}
			}
		}
	}
	if !hasSignedThinking {
		return false
	}
	for _, id := range toolResultIDs {
		if _, ok := toolUseIDs[id]; !ok {
			return false
		}
	}
	return true
}

func claudeToolChoiceForcesCall(value any) bool {
	choice, ok := value.(*dto.ClaudeToolChoice)
	if !ok || choice == nil {
		return false
	}
	return choice.Type == "any" || choice.Type == "tool"
}

func responsesInputContentToClaudeMediaMessages(c context.Context, content any) ([]dto.ClaudeMediaMessage, error) {
	contentParts, err := ContentParts(content)
	if err != nil {
		return nil, err
	}

	parts := make([]dto.ClaudeMediaMessage, 0, len(contentParts))
	for _, contentPart := range contentParts {
		partType := strings.TrimSpace(kitutil.Interface2String(contentPart["type"]))
		switch partType {
		case "input_text", "output_text", "text":
			text := kitutil.Interface2String(contentPart["text"])
			if text != "" {
				parts = append(parts, dto.ClaudeMediaMessage{
					Type: "text",
					Text: kitutil.GetPointer(text),
				})
			}
		case "input_audio", "input_video":
			return nil, fmt.Errorf("Responses content type %q cannot be converted to Claude Messages", partType)
		case "input_image", "input_file":
			source := ContentPartToFileSource(contentPart)
			if source == nil {
				return nil, fmt.Errorf(
					"Responses %s content requires URL or base64 data for Claude Messages conversion; provider file IDs cannot be reused across protocols",
					partType,
				)
			}
			base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting Responses input for Claude")
			if err != nil {
				return nil, fmt.Errorf("get file data failed: %s", err.Error())
			}
			if partType == "input_image" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "image/") {
				return nil, fmt.Errorf("Responses input_image resolved to non-image MIME type %q", mimeType)
			}
			claudePart := dto.ClaudeMediaMessage{
				Source: &dto.ClaudeMessageSource{
					Type:      "base64",
					MediaType: mimeType,
					Data:      base64Data,
				},
			}
			if partType == "input_file" {
				claudePart.Type = "document"
			} else {
				claudePart.Type = "image"
			}
			claudePart.Title = strings.TrimSpace(kitutil.Interface2String(contentPart["filename"]))
			claudePart.Filename = claudePart.Title
			parts = append(parts, claudePart)
		case "encrypted_content":
			// Opaque provider-bound reasoning state: unreadable by any other
			// provider, dropped when lossy conversion admitted the request.
			continue
		default:
			return nil, fmt.Errorf("Responses content type %q cannot be converted to Claude Messages", partType)
		}
	}
	return parts, nil
}

func responsesSystemContentToClaudeText(content any) ([]dto.ClaudeMediaMessage, error) {
	contentParts, err := ContentParts(content)
	if err != nil {
		return nil, err
	}
	parts := make([]dto.ClaudeMediaMessage, 0, len(contentParts))
	for _, contentPart := range contentParts {
		partType := strings.TrimSpace(kitutil.Interface2String(contentPart["type"]))
		if partType != "input_text" && partType != "output_text" && partType != "text" {
			return nil, fmt.Errorf("Responses system and developer message content must be text-only for Claude conversion")
		}
		text := kitutil.Interface2String(contentPart["text"])
		if strings.TrimSpace(text) != "" {
			parts = append(parts, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer(text)})
		}
	}
	return parts, nil
}

func responsesCallItemToClaudeToolUse(item map[string]any, inputKey string, kind sharedbridge.ToolKind, toolState *sharedbridge.ToolState) (dto.ClaudeMediaMessage, error) {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	namespace := strings.TrimSpace(kitutil.Interface2String(item["namespace"]))
	if kind == sharedbridge.ToolKindToolSearch {
		name = "tool_search"
		namespace = ""
	}
	if kind == sharedbridge.ToolKindLocalShell {
		name = sharedbridge.LocalShellToolName
		namespace = ""
	}
	if name == "" {
		return dto.ClaudeMediaMessage{}, fmt.Errorf("Responses tool call is missing name")
	}
	upstreamName, err := upstreamToolName(toolState, kind, namespace, name)
	if err != nil {
		return dto.ClaudeMediaMessage{}, err
	}
	input, err := responsesFunctionArgumentsObject(item[inputKey])
	if kind == sharedbridge.ToolKindCustom {
		input = ObjectValue(customInputArguments(item[inputKey]), inputKey)
	} else if kind == sharedbridge.ToolKindToolSearch {
		input = ObjectValue(toolSearchArguments(item[inputKey]), inputKey)
	} else if kind == sharedbridge.ToolKindLocalShell {
		input = ObjectValue(sharedbridge.LocalShellCallArguments(item[inputKey]), inputKey)
	} else if err != nil {
		return dto.ClaudeMediaMessage{}, fmt.Errorf("function_call %q: %w", CallID(item), err)
	}
	return dto.ClaudeMediaMessage{
		Type:  "tool_use",
		Id:    CallID(item),
		Name:  upstreamName,
		Input: input,
	}, nil
}

func responsesFunctionOutputItemToClaudeToolResult(c context.Context, item map[string]any, outputKey string) (dto.ClaudeMediaMessage, error) {
	content, isError, err := responsesToolOutputValue(c, item[outputKey])
	if err != nil {
		return dto.ClaudeMediaMessage{}, err
	}
	return dto.ClaudeMediaMessage{
		Type:      "tool_result",
		ToolUseId: CallID(item),
		Content:   content,
		IsError:   isError,
	}, nil
}

func responsesToolOutputValue(c context.Context, value any) (any, *bool, error) {
	if value == nil {
		return "", nil, nil
	}
	cleaned, media, changed, err := sharedtoolmedia.StripAndClamp(
		value,
		sharedtoolmedia.ImagesOnly,
		map[string]any{"type": "input_text", "text": sharedtoolmedia.ToolResultMediaAttachedMarker},
		sharedtoolmedia.ToolResultMediaAttachedMarker,
	)
	if err != nil {
		return nil, nil, err
	}
	if changed {
		parts := make([]dto.ClaudeMediaMessage, 0, len(media)+2)
		isError := false
		if err := appendClaudeToolOutputValue(&parts, &isError, cleaned); err != nil {
			return nil, nil, err
		}
		for _, item := range media {
			image, ok := claudeImageFromToolMedia(item)
			if ok {
				parts = append(parts, image)
			}
		}
		if isError {
			return parts, kitutil.GetPointer(true), nil
		}
		return parts, nil, nil
	}
	if text, ok := value.(string); ok {
		return text, nil, nil
	}
	values, ok := value.([]any)
	if !ok {
		encoded, err := kitutil.Marshal(value)
		if err != nil {
			return nil, nil, err
		}
		return string(encoded), nil, nil
	}
	parts := make([]dto.ClaudeMediaMessage, 0, len(values))
	isError := false
	for _, value := range values {
		part, ok := value.(map[string]any)
		if !ok {
			encoded, err := kitutil.Marshal(value)
			if err != nil {
				return nil, nil, err
			}
			parts = append(parts, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer(string(encoded))})
			continue
		}
		partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
		if partType == "input_text" || partType == "output_text" || partType == "text" {
			text := kitutil.Interface2String(part["text"])
			if text == sharedbridge.ClaudeToolResultErrorMarker {
				isError = true
				continue
			}
			parts = append(parts, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer(text)})
			continue
		}
		if partType == "input_image" || partType == "input_file" {
			converted, err := responsesInputContentToClaudeMediaMessages(c, []map[string]any{part})
			if err != nil {
				return nil, nil, err
			}
			if len(converted) > 0 {
				parts = append(parts, converted...)
				continue
			}
		}
		encoded, err := kitutil.Marshal(part)
		if err != nil {
			return nil, nil, err
		}
		parts = append(parts, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer(string(encoded))})
	}
	if isError {
		return parts, kitutil.GetPointer(true), nil
	}
	return parts, nil, nil
}

func appendClaudeToolOutputValue(parts *[]dto.ClaudeMediaMessage, isError *bool, value any) error {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if typed == sharedbridge.ClaudeToolResultErrorMarker {
			*isError = true
		} else if typed != "" {
			*parts = append(*parts, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer(typed)})
		}
		return nil
	case []any:
		for _, item := range typed {
			part, ok := item.(map[string]any)
			if ok {
				partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
				if partType == "input_text" || partType == "output_text" || partType == "text" {
					text := kitutil.Interface2String(part["text"])
					if text == sharedbridge.ClaudeToolResultErrorMarker {
						*isError = true
					} else {
						*parts = append(*parts, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer(text)})
					}
					continue
				}
			}
			raw, err := kitutil.Marshal(item)
			if err != nil {
				return err
			}
			text := string(raw)
			*parts = append(*parts, dto.ClaudeMediaMessage{Type: "text", Text: &text})
		}
		return nil
	case map[string]any:
		partType := strings.TrimSpace(kitutil.Interface2String(typed["type"]))
		if partType == "input_text" || partType == "output_text" || partType == "text" {
			text := kitutil.Interface2String(typed["text"])
			if text == sharedbridge.ClaudeToolResultErrorMarker {
				*isError = true
			} else {
				*parts = append(*parts, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer(text)})
			}
			return nil
		}
	}
	raw, err := kitutil.Marshal(value)
	if err != nil {
		return err
	}
	text := string(raw)
	*parts = append(*parts, dto.ClaudeMediaMessage{Type: "text", Text: &text})
	return nil
}

func claudeImageFromToolMedia(media dto.MediaContent) (dto.ClaudeMediaMessage, bool) {
	url := sharedtoolmedia.ImageURL(media)
	if url == "" {
		return dto.ClaudeMediaMessage{}, false
	}
	lower := strings.ToLower(url)
	if strings.HasPrefix(lower, "data:") {
		rest := url[5:]
		meta, data, ok := strings.Cut(rest, ",")
		if !ok || data == "" {
			return dto.ClaudeMediaMessage{}, false
		}
		mediaType := strings.SplitN(meta, ";", 2)[0]
		if mediaType == "" {
			mediaType = "image/png"
		}
		return dto.ClaudeMediaMessage{
			Type: "image",
			Source: &dto.ClaudeMessageSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      data,
			},
		}, true
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return dto.ClaudeMediaMessage{
			Type: "image",
			Source: &dto.ClaudeMessageSource{
				Type: "url",
				Url:  url,
			},
		}, true
	}
	return dto.ClaudeMediaMessage{}, false
}

func appendClaudeToolUse(messages []dto.ClaudeMessage, toolUse dto.ClaudeMediaMessage) []dto.ClaudeMessage {
	return appendClaudeConversationContent(messages, "assistant", []dto.ClaudeMediaMessage{toolUse})
}

func appendClaudeConversationContent(messages []dto.ClaudeMessage, role string, parts []dto.ClaudeMediaMessage) []dto.ClaudeMessage {
	if len(parts) == 0 {
		return messages
	}
	if len(messages) > 0 && messages[len(messages)-1].Role == role {
		last := messages[len(messages)-1]
		lastParts := claudeMessageContentParts(last.Content)
		last.Content = append(lastParts, parts...)
		messages[len(messages)-1] = last
		return messages
	}
	return append(messages, dto.ClaudeMessage{Role: role, Content: parts})
}

func appendClaudeToolResult(messages []dto.ClaudeMessage, toolResult dto.ClaudeMediaMessage) []dto.ClaudeMessage {
	if len(messages) > 0 && messages[len(messages)-1].Role == "user" {
		last := messages[len(messages)-1]
		parts := claudeMessageContentParts(last.Content)
		insertAt := len(parts)
		for index, part := range parts {
			if part.Type != "tool_result" {
				insertAt = index
				break
			}
		}
		parts = append(parts, dto.ClaudeMediaMessage{})
		copy(parts[insertAt+1:], parts[insertAt:])
		parts[insertAt] = toolResult
		last.Content = parts
		messages[len(messages)-1] = last
		return messages
	}
	return append(messages, dto.ClaudeMessage{
		Role:    "user",
		Content: []dto.ClaudeMediaMessage{toolResult},
	})
}

func claudeMessageContentParts(content any) []dto.ClaudeMediaMessage {
	switch typed := content.(type) {
	case []dto.ClaudeMediaMessage:
		return typed
	case string:
		if typed == "" {
			return nil
		}
		return []dto.ClaudeMediaMessage{
			{
				Type: "text",
				Text: kitutil.GetPointer(typed),
			},
		}
	default:
		parts, _ := kitutil.Any2Type[[]dto.ClaudeMediaMessage](content)
		return parts
	}
}

func responsesClaudeRole(item map[string]any) string {
	switch strings.TrimSpace(kitutil.Interface2String(item["role"])) {
	case "assistant":
		return "assistant"
	case "system", "developer":
		return "system"
	default:
		return "user"
	}
}

func ensureClaudeMessagesStartWithUser(messages []dto.ClaudeMessage) []dto.ClaudeMessage {
	if len(messages) == 0 || messages[0].Role == "user" {
		return messages
	}
	return append([]dto.ClaudeMessage{
		{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{
				{
					Type: "text",
					Text: kitutil.GetPointer("..."),
				},
			},
		},
	}, messages...)
}

func normalizeResponsesClaudeMessages(messages []dto.ClaudeMessage) []dto.ClaudeMessage {
	messages = removeEmptyClaudeMessageContent(messages)
	messages = dropIncompleteClaudeToolTurns(messages)
	messages = removeEmptyClaudeMessageContent(messages)
	messages = ensureClaudeMessagesStartWithUser(messages)
	trimTrailingClaudeAssistantText(messages)
	return removeEmptyClaudeMessageContent(messages)
}

func removeEmptyClaudeMessageContent(messages []dto.ClaudeMessage) []dto.ClaudeMessage {
	sanitized := make([]dto.ClaudeMessage, 0, len(messages))
	for _, message := range messages {
		parts := claudeMessageContentParts(message.Content)
		filtered := make([]dto.ClaudeMediaMessage, 0, len(parts))
		for _, part := range parts {
			if part.Type == "text" && strings.TrimSpace(part.GetText()) == "" {
				continue
			}
			filtered = append(filtered, part)
		}
		if len(filtered) == 0 {
			continue
		}
		message.Content = filtered
		sanitized = append(sanitized, message)
	}
	return sanitized
}

func dropIncompleteClaudeToolTurns(messages []dto.ClaudeMessage) []dto.ClaudeMessage {
	sanitized := make([]dto.ClaudeMessage, 0, len(messages))
	for index := 0; index < len(messages); {
		message := messages[index]
		toolUseIDs := claudeMessageBlockIDs(message, "tool_use")
		if message.Role == "assistant" && len(toolUseIDs) > 0 {
			var pairedUser *dto.ClaudeMessage
			if index+1 < len(messages) && messages[index+1].Role == "user" {
				paired := messages[index+1]
				pairedUser = &paired
			}
			if pairedUser != nil && claudeToolTurnIsComplete(toolUseIDs, claudeMessageBlockIDs(*pairedUser, "tool_result")) {
				sanitized = append(sanitized, message, *pairedUser)
				index += 2
				continue
			}
			if pairedUser != nil {
				if cleaned, ok := removeClaudeToolResultBlocks(*pairedUser); ok {
					sanitized = append(sanitized, cleaned)
				}
				index += 2
				continue
			}
			index++
			continue
		}
		if message.Role == "user" {
			if cleaned, ok := removeClaudeToolResultBlocks(message); ok {
				sanitized = append(sanitized, cleaned)
			}
			index++
			continue
		}
		sanitized = append(sanitized, message)
		index++
	}
	return sanitized
}

func claudeMessageBlockIDs(message dto.ClaudeMessage, blockType string) []string {
	parts := claudeMessageContentParts(message.Content)
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		switch blockType {
		case "tool_use":
			if part.Type == blockType {
				ids = append(ids, strings.TrimSpace(part.Id))
			}
		case "tool_result":
			if part.Type == blockType {
				ids = append(ids, strings.TrimSpace(part.ToolUseId))
			}
		}
	}
	return ids
}

func claudeToolTurnIsComplete(toolUseIDs []string, toolResultIDs []string) bool {
	if len(toolUseIDs) == 0 || len(toolUseIDs) != len(toolResultIDs) {
		return false
	}
	uses := make(map[string]struct{}, len(toolUseIDs))
	for _, id := range toolUseIDs {
		if id == "" {
			return false
		}
		if _, exists := uses[id]; exists {
			return false
		}
		uses[id] = struct{}{}
	}
	results := make(map[string]struct{}, len(toolResultIDs))
	for _, id := range toolResultIDs {
		if id == "" {
			return false
		}
		if _, exists := results[id]; exists {
			return false
		}
		results[id] = struct{}{}
	}
	if len(uses) != len(results) {
		return false
	}
	for id := range uses {
		if _, ok := results[id]; !ok {
			return false
		}
	}
	return true
}

func removeClaudeToolResultBlocks(message dto.ClaudeMessage) (dto.ClaudeMessage, bool) {
	parts := claudeMessageContentParts(message.Content)
	filtered := make([]dto.ClaudeMediaMessage, 0, len(parts))
	for _, part := range parts {
		if part.Type != "tool_result" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		return dto.ClaudeMessage{}, false
	}
	message.Content = filtered
	return message, true
}

func trimTrailingClaudeAssistantText(messages []dto.ClaudeMessage) {
	if len(messages) == 0 || messages[len(messages)-1].Role != "assistant" {
		return
	}
	last := messages[len(messages)-1]
	parts := claudeMessageContentParts(last.Content)
	if len(parts) == 0 || parts[len(parts)-1].Type != "text" {
		return
	}
	text := strings.TrimRight(parts[len(parts)-1].GetText(), " \t\r\n")
	if text == "" {
		parts = parts[:len(parts)-1]
	} else {
		parts[len(parts)-1].Text = kitutil.GetPointer(text)
	}
	last.Content = parts
	messages[len(messages)-1] = last
}
