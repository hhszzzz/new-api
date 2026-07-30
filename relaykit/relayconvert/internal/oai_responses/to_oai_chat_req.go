package oairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	sharedchat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/chat"
	sharedtoolmedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/toolmedia"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const (
	responsesInputTypeFunctionCall       = "function_call"
	responsesInputTypeFunctionCallOutput = "function_call_output"
	responsesInputTypeCustomToolCall     = "custom_tool_call"
	responsesInputTypeCustomToolOutput   = "custom_tool_call_output"
)

const (
	ResponsesInputTypeFunctionCall       = responsesInputTypeFunctionCall
	ResponsesInputTypeFunctionCallOutput = responsesInputTypeFunctionCallOutput
	ResponsesInputTypeCustomToolCall     = responsesInputTypeCustomToolCall
	ResponsesInputTypeCustomToolOutput   = responsesInputTypeCustomToolOutput
)

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return ResponsesRequestToChatCompletionsRequestWithContext(context.Background(), req)
}

func ResponsesRequestToChatCompletionsRequestWithContext(c context.Context, req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if err := validateResponsesRequestChatUnsupportedFields(req); err != nil {
		return nil, err
	}

	tools, toolState, err := prepareResponsesToolsForChat(c, req)
	if err != nil {
		return nil, err
	}

	messages, err := responsesRequestMessagesToChat(c, req, toolState)
	if err != nil {
		return nil, err
	}

	toolChoice, err := responsesRequestToolChoiceToChat(req.ToolChoice, toolState)
	if err != nil {
		return nil, err
	}

	responseFormat, err := responsesRequestTextToChatResponseFormat(req.Text)
	if err != nil {
		return nil, err
	}

	out := &dto.GeneralOpenAIRequest{
		Model:                req.Model,
		Messages:             messages,
		Stream:               req.Stream,
		StreamOptions:        req.StreamOptions,
		Temperature:          req.Temperature,
		TopP:                 req.TopP,
		TopLogProbs:          req.TopLogProbs,
		ResponseFormat:       responseFormat,
		Tools:                tools,
		ToolChoice:           toolChoice,
		User:                 req.User,
		Store:                req.Store,
		Metadata:             req.Metadata,
		SafetyIdentifier:     req.SafetyIdentifier,
		PromptCacheRetention: req.PromptCacheRetention,
	}
	// Qwen models consume these vendor fields; anywhere else they are
	// non-standard and must not leak to the upstream request.
	if dto.IsQwenThinkingBudgetModel(req.Model) {
		out.EnableThinking = req.EnableThinking
		out.ThinkingBudget = req.ThinkingBudget
	}
	if sharedchat.UsesMaxCompletionTokens(req.Model) {
		out.MaxCompletionTokens = req.MaxOutputTokens
	} else {
		out.MaxTokens = req.MaxOutputTokens
	}
	if req.Stream != nil && *req.Stream {
		if out.StreamOptions == nil {
			out.StreamOptions = &dto.StreamOptions{}
		}
		out.StreamOptions.IncludeUsage = true
	}

	if req.Reasoning != nil {
		sharedchat.ApplyReasoningEffort(out, req.Reasoning.Effort)
	}
	if req.ServiceTier != "" {
		out.ServiceTier, _ = kitutil.Marshal(req.ServiceTier)
	}
	if len(req.ParallelToolCalls) > 0 && kitutil.GetJsonType(req.ParallelToolCalls) == "boolean" {
		var parallelToolCalls bool
		if err := kitutil.Unmarshal(req.ParallelToolCalls, &parallelToolCalls); err == nil {
			out.ParallelTooCalls = &parallelToolCalls
		}
	}
	if len(out.Tools) == 0 {
		out.ToolChoice = nil
		out.ParallelTooCalls = nil
	}

	return out, nil
}

func validateResponsesRequestChatUnsupportedFields(req *dto.OpenAIResponsesRequest) error {
	unsupported := make([]string, 0, 4)
	if rawJSONPresent(req.Conversation) {
		unsupported = append(unsupported, "conversation")
	}
	if strings.TrimSpace(req.PreviousResponseID) != "" {
		unsupported = append(unsupported, "previous_response_id")
	}
	if rawJSONPresent(req.Prompt) {
		unsupported = append(unsupported, "prompt")
	}
	if rawJSONPresent(req.ContextManagement) {
		unsupported = append(unsupported, "context_management")
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("responses to chat conversion does not support stateful fields: %s", strings.Join(unsupported, ", "))
	}
	return nil
}

func ValidateRequestChatUnsupportedFields(req *dto.OpenAIResponsesRequest) error {
	return validateResponsesRequestChatUnsupportedFields(req)
}

func responsesRequestMessagesToChat(c context.Context, req *dto.OpenAIResponsesRequest, toolState *sharedbridge.ToolState) ([]dto.Message, error) {
	messages := make([]dto.Message, 0)
	pendingToolMedia := make([]dto.MediaContent, 0)
	pendingReasoning := ""
	if rawJSONPresent(req.Instructions) {
		instructions, err := responsesJSONString(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("invalid instructions: %w", err)
		}
		if strings.TrimSpace(instructions) != "" {
			messages = append(messages, dto.Message{Role: "system", Content: instructions})
		}
	}

	if !rawJSONPresent(req.Input) {
		return collapseChatSystemMessagesToHead(messages), nil
	}

	switch kitutil.GetJsonType(req.Input) {
	case "string":
		input, err := responsesJSONString(req.Input)
		if err != nil {
			return nil, fmt.Errorf("invalid input string: %w", err)
		}
		messages = append(messages, dto.Message{Role: "user", Content: input})
		return collapseChatSystemMessagesToHead(messages), nil
	case "array":
		var items []map[string]any
		if err := kitutil.Unmarshal(req.Input, &items); err != nil {
			return nil, fmt.Errorf("invalid input array: %w", err)
		}
		items, err := filterIncompleteResponsesToolHistory(items, false)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			nextMessages, err := responsesInputItemToChatMessages(c, item, messages, &pendingToolMedia, &pendingReasoning, toolState)
			if err != nil {
				return nil, err
			}
			messages = nextMessages
		}
		attachPendingResponsesReasoningToLastAssistant(messages, &pendingReasoning)
		messages = flushResponsesToolMedia(messages, &pendingToolMedia)
		messages = normalizeResponsesChatToolTurns(messages)
		backfillResponsesChatToolCallReasoning(messages)
		return collapseChatSystemMessagesToHead(messages), nil
	default:
		return nil, fmt.Errorf("unsupported responses input type %q", kitutil.GetJsonType(req.Input))
	}
}

func responsesInputItemToChatMessages(c context.Context, item map[string]any, messages []dto.Message, pendingToolMedia *[]dto.MediaContent, pendingReasoning *string, toolState *sharedbridge.ToolState) ([]dto.Message, error) {
	itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
	switch itemType {
	case "reasoning":
		appendPendingResponsesReasoning(pendingReasoning, responsesReasoningItemText(item))
		return messages, nil
	case responsesInputTypeFunctionCall:
		appendPendingResponsesReasoning(pendingReasoning, responsesItemReasoningText(item))
		messages = flushResponsesToolMedia(messages, pendingToolMedia)
		toolCall, err := responsesFunctionCallItemToChatToolCall(item, toolState)
		if err != nil {
			return nil, err
		}
		messages = appendToolCallToLastAssistant(messages, toolCall)
		attachPendingResponsesReasoningToMessage(&messages[len(messages)-1], pendingReasoning)
		return messages, nil
	case responsesInputTypeCustomToolCall:
		appendPendingResponsesReasoning(pendingReasoning, responsesItemReasoningText(item))
		messages = flushResponsesToolMedia(messages, pendingToolMedia)
		toolCall, err := responsesCustomToolCallItemToChatToolCall(item, toolState)
		if err != nil {
			return nil, err
		}
		messages = appendToolCallToLastAssistant(messages, toolCall)
		attachPendingResponsesReasoningToMessage(&messages[len(messages)-1], pendingReasoning)
		return messages, nil
	case "tool_search_call":
		appendPendingResponsesReasoning(pendingReasoning, responsesItemReasoningText(item))
		messages = flushResponsesToolMedia(messages, pendingToolMedia)
		toolCall, err := responsesToolSearchCallItemToChatToolCall(item, toolState)
		if err != nil {
			return nil, err
		}
		messages = appendToolCallToLastAssistant(messages, toolCall)
		attachPendingResponsesReasoningToMessage(&messages[len(messages)-1], pendingReasoning)
		return messages, nil
	case "local_shell_call":
		appendPendingResponsesReasoning(pendingReasoning, responsesItemReasoningText(item))
		messages = flushResponsesToolMedia(messages, pendingToolMedia)
		toolCall, err := responsesLocalShellCallItemToChatToolCall(item, toolState)
		if err != nil {
			return nil, err
		}
		messages = appendToolCallToLastAssistant(messages, toolCall)
		attachPendingResponsesReasoningToMessage(&messages[len(messages)-1], pendingReasoning)
		return messages, nil
	case responsesInputTypeFunctionCallOutput, responsesInputTypeCustomToolOutput, "tool_search_output", "local_shell_call_output":
		attachPendingResponsesReasoningToLastAssistant(messages, pendingReasoning)
		callID := responsesCallID(item)
		if !responsesChatToolCallExists(messages, callID) {
			return messages, nil
		}
		output := item["output"]
		if itemType == "tool_search_output" {
			output = item["tools"]
		}
		content, media, err := responseToolOutputToChatContent(c, output)
		if err != nil {
			return nil, err
		}
		if len(media) > 0 {
			*pendingToolMedia = append(*pendingToolMedia, dto.MediaContent{
				Type: dto.ContentTypeText,
				Text: fmt.Sprintf("[new-api: media output of tool call %s]", callID),
			})
			*pendingToolMedia = append(*pendingToolMedia, media...)
		}
		return insertResponsesChatToolResult(messages, dto.Message{Role: "tool", ToolCallId: callID, Content: content}), nil
	case "additional_tools":
		return messages, nil
	}
	if isResponsesHostedHistoryItem(itemType) {
		return nil, fmt.Errorf("Responses server tool history item %q cannot be converted to Chat Completions without losing context", itemType)
	}
	role := responsesRoleToChatRole(kitutil.Interface2String(item["role"]))
	if role != "assistant" {
		attachPendingResponsesReasoningToLastAssistant(messages, pendingReasoning)
	}
	messages = flushResponsesToolMedia(messages, pendingToolMedia)

	content, err := responsesInputContentToChatContent(c, item["content"])
	if err != nil {
		return nil, err
	}
	if role == "system" {
		if _, ok := content.(string); !ok {
			return nil, errors.New("system and developer message content must be text-only for chat conversion")
		}
	}
	embeddedReasoning := ""
	if role == "assistant" {
		embeddedReasoning = responsesItemReasoningText(item)
	}
	if role == "assistant" && len(messages) > 0 {
		last := &messages[len(messages)-1]
		if last.Role == "assistant" && last.Content == nil && len(last.ParseToolCalls()) == 0 {
			appendResponsesReasoningToMessage(last, embeddedReasoning)
			attachPendingResponsesReasoningToMessage(last, pendingReasoning)
			last.Content = content
			return messages, nil
		}
	}
	message := dto.Message{Role: role, Content: content}
	appendResponsesReasoningToMessage(&message, embeddedReasoning)
	messages = append(messages, message)
	if role == "assistant" {
		attachPendingResponsesReasoningToMessage(&messages[len(messages)-1], pendingReasoning)
	}
	return messages, nil
}

func appendPendingResponsesReasoning(pending *string, reasoning string) {
	if pending == nil {
		return
	}
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return
	}
	if strings.TrimSpace(*pending) == "" {
		*pending = reasoning
		return
	}
	if strings.Contains(*pending, reasoning) {
		return
	}
	*pending += "\n\n" + reasoning
}

func appendResponsesReasoningToMessage(message *dto.Message, reasoning string) {
	if message == nil {
		return
	}
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return
	}
	existing := strings.TrimSpace(message.GetReasoningContent())
	if existing == "" {
		message.ReasoningContent = &reasoning
		message.Reasoning = nil
		return
	}
	if strings.Contains(existing, reasoning) {
		return
	}
	combined := existing + "\n\n" + reasoning
	message.ReasoningContent = &combined
	message.Reasoning = nil
}

func attachPendingResponsesReasoningToMessage(message *dto.Message, pending *string) {
	if message == nil || pending == nil {
		return
	}
	reasoning := strings.TrimSpace(*pending)
	if reasoning == "" {
		*pending = ""
		return
	}
	appendResponsesReasoningToMessage(message, reasoning)
	*pending = ""
}

func attachPendingResponsesReasoningToLastAssistant(messages []dto.Message, pending *string) {
	if pending == nil || strings.TrimSpace(*pending) == "" {
		return
	}
	for i := len(messages) - 1; i >= 0; i-- {
		switch messages[i].Role {
		case "assistant":
			attachPendingResponsesReasoningToMessage(&messages[i], pending)
			return
		case "tool":
			continue
		default:
			*pending = ""
			return
		}
	}
	*pending = ""
}

func responsesRoleToChatRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "developer":
		return "system"
	case "assistant":
		return "assistant"
	case "tool":
		return "tool"
	case "user", "latest_reminder":
		return "user"
	default:
		return "user"
	}
}

func collapseChatSystemMessagesToHead(messages []dto.Message) []dto.Message {
	if len(messages) == 0 {
		return messages
	}

	systemContent := make([]string, 0)
	rest := make([]dto.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role != "system" {
			rest = append(rest, message)
			continue
		}
		content := message.StringContent()
		if strings.TrimSpace(content) != "" {
			systemContent = append(systemContent, content)
		}
	}

	if len(systemContent) == 0 {
		return rest
	}
	result := make([]dto.Message, 0, len(rest)+1)
	result = append(result, dto.Message{
		Role:    "system",
		Content: strings.Join(systemContent, "\n\n"),
	})
	return append(result, rest...)
}

func responsesReasoningItemText(item map[string]any) string {
	if item == nil {
		return ""
	}
	summary := responsesReasoningPartsText(item["summary"], "summary_text")
	visible := responsesReasoningPartsText(item["content"], "reasoning_text", "summary_text")
	if visible == "" || visible == summary {
		return summary
	}
	if summary == "" {
		return visible
	}
	return summary + "\n\n" + visible
}

func responsesItemReasoningText(item map[string]any) string {
	if item == nil {
		return ""
	}
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if text := responsesReasoningFieldText(item[key]); text != "" {
			return text
		}
	}
	return responsesReasoningFieldText(item["reasoning_details"])
}

func responsesReasoningFieldText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"content", "text", "summary"} {
			if text := responsesReasoningFieldText(typed[key]); text != "" {
				return text
			}
		}
		return responsesReasoningFieldText(typed["parts"])
	case []any:
		parts := make([]string, 0, len(typed))
		for _, part := range typed {
			if text := responsesReasoningFieldText(part); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	case []map[string]any:
		parts := make([]any, 0, len(typed))
		for _, part := range typed {
			parts = append(parts, part)
		}
		return responsesReasoningFieldText(parts)
	default:
		return ""
	}
}

func responsesReasoningPartsText(value any, acceptedTypes ...string) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	accepted := make(map[string]struct{}, len(acceptedTypes))
	for _, partType := range acceptedTypes {
		accepted[partType] = struct{}{}
	}
	parts, ok := value.([]any)
	if !ok {
		if typedParts, typedOK := value.([]map[string]any); typedOK {
			parts = make([]any, 0, len(typedParts))
			for _, part := range typedParts {
				parts = append(parts, part)
			}
		}
	}
	var text strings.Builder
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := accepted[strings.TrimSpace(kitutil.Interface2String(part["type"]))]; ok {
			text.WriteString(kitutil.Interface2String(part["text"]))
		}
	}
	return text.String()
}

func backfillResponsesChatToolCallReasoning(messages []dto.Message) {
	for index := range messages {
		message := &messages[index]
		if message.Role != "assistant" || len(message.ParseToolCalls()) == 0 || strings.TrimSpace(message.GetReasoningContent()) != "" {
			continue
		}
		placeholder := "tool call"
		message.ReasoningContent = &placeholder
		message.Reasoning = nil
	}
}

func responsesInputContentToChatContent(c context.Context, content any) (any, error) {
	if content == nil {
		return "", nil
	}

	switch value := content.(type) {
	case string:
		return value, nil
	case []any:
		return responsesContentPartsToChatContent(c, value)
	case []map[string]any:
		parts := make([]any, 0, len(value))
		for _, part := range value {
			parts = append(parts, part)
		}
		return responsesContentPartsToChatContent(c, parts)
	default:
		return content, nil
	}
}

func responsesContentPartsToChatContent(c context.Context, parts []any) (any, error) {
	chatParts := make([]any, 0, len(parts))
	var textOnly strings.Builder
	onlyText := true

	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			onlyText = false
			chatParts = append(chatParts, rawPart)
			continue
		}

		partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
		switch partType {
		case "input_text", "output_text", "text":
			text := kitutil.Interface2String(part["text"])
			textOnly.WriteString(text)
			chatParts = append(chatParts, map[string]any{
				"type": dto.ContentTypeText,
				"text": text,
			})
		case "input_image":
			onlyText = false
			chatParts = append(chatParts, map[string]any{
				"type":      dto.ContentTypeImageURL,
				"image_url": responsesImagePartToChatImageURL(part),
			})
		case "input_file":
			onlyText = false
			file, err := responsesFilePartToChatFile(c, part)
			if err != nil {
				return nil, err
			}
			chatParts = append(chatParts, map[string]any{
				"type": dto.ContentTypeFile,
				"file": file,
			})
		case "input_audio":
			onlyText = false
			chatParts = append(chatParts, map[string]any{
				"type":        dto.ContentTypeInputAudio,
				"input_audio": responsesPartPayload(part, "input_audio"),
			})
		case "input_video":
			onlyText = false
			chatParts = append(chatParts, map[string]any{
				"type":      dto.ContentTypeVideoUrl,
				"video_url": responsesVideoPartToChatVideoURL(part),
			})
		default:
			onlyText = false
			chatParts = append(chatParts, part)
		}
	}

	if onlyText {
		return textOnly.String(), nil
	}
	return chatParts, nil
}

func responsesFunctionCallItemToChatToolCall(item map[string]any, toolState *sharedbridge.ToolState) (dto.ToolCallRequest, error) {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	if name == "" {
		return dto.ToolCallRequest{}, errors.New("function_call item is missing name")
	}
	namespace := strings.TrimSpace(kitutil.Interface2String(item["namespace"]))
	upstreamName, err := upstreamToolName(toolState, sharedbridge.ToolKindFunction, namespace, name)
	if err != nil {
		return dto.ToolCallRequest{}, err
	}
	arguments, err := responsesFunctionArgumentsString(item["arguments"])
	if err != nil {
		return dto.ToolCallRequest{}, fmt.Errorf("function_call %q: %w", responsesCallID(item), err)
	}
	return dto.ToolCallRequest{
		ID:   responsesCallID(item),
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      upstreamName,
			Arguments: arguments,
		},
	}, nil
}

func responsesChatToolCallExists(messages []dto.Message, callID string) bool {
	if strings.TrimSpace(callID) == "" {
		return false
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "assistant" {
			continue
		}
		for _, call := range messages[index].ParseToolCalls() {
			if call.ID == callID {
				return true
			}
		}
	}
	return false
}

func insertResponsesChatToolResult(messages []dto.Message, result dto.Message) []dto.Message {
	assistantIndex := -1
	for index := len(messages) - 1; index >= 0 && assistantIndex < 0; index-- {
		if messages[index].Role != "assistant" {
			continue
		}
		for _, call := range messages[index].ParseToolCalls() {
			if call.ID == result.ToolCallId {
				assistantIndex = index
				break
			}
		}
	}
	if assistantIndex < 0 {
		return messages
	}
	insertAt := assistantIndex + 1
	for insertAt < len(messages) && messages[insertAt].Role == "tool" {
		insertAt++
	}
	messages = append(messages, dto.Message{})
	copy(messages[insertAt+1:], messages[insertAt:])
	messages[insertAt] = result
	return messages
}

func normalizeResponsesChatToolTurns(messages []dto.Message) []dto.Message {
	normalized := make([]dto.Message, 0, len(messages))
	for index := 0; index < len(messages); {
		message := messages[index]
		if message.Role == "tool" {
			index++
			continue
		}
		calls := message.ParseToolCalls()
		if message.Role != "assistant" || len(calls) == 0 {
			normalized = append(normalized, message)
			index++
			continue
		}

		resultsEnd := index + 1
		resultIDs := make(map[string]struct{}, len(calls))
		for resultsEnd < len(messages) && messages[resultsEnd].Role == "tool" {
			resultIDs[messages[resultsEnd].ToolCallId] = struct{}{}
			resultsEnd++
		}
		complete := len(resultIDs) == 0 || len(resultIDs) == len(calls)
		callIDs := make(map[string]struct{}, len(calls))
		for _, call := range calls {
			if call.ID == "" {
				complete = false
				continue
			}
			if _, duplicate := callIDs[call.ID]; duplicate {
				complete = false
			}
			callIDs[call.ID] = struct{}{}
			if len(resultIDs) > 0 {
				if _, found := resultIDs[call.ID]; !found {
					complete = false
				}
			}
		}
		if complete {
			normalized = append(normalized, messages[index:resultsEnd]...)
		}
		index = resultsEnd
	}
	return normalized
}

func responsesCustomToolCallItemToChatToolCall(item map[string]any, toolState *sharedbridge.ToolState) (dto.ToolCallRequest, error) {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	if name == "" {
		return dto.ToolCallRequest{}, errors.New("custom_tool_call item is missing name")
	}
	namespace := strings.TrimSpace(kitutil.Interface2String(item["namespace"]))
	upstreamName, err := upstreamToolName(toolState, sharedbridge.ToolKindCustom, namespace, name)
	if err != nil {
		return dto.ToolCallRequest{}, err
	}
	return dto.ToolCallRequest{
		ID:   responsesCallID(item),
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      upstreamName,
			Arguments: customInputArguments(item["input"]),
		},
	}, nil
}

func responsesToolSearchCallItemToChatToolCall(item map[string]any, toolState *sharedbridge.ToolState) (dto.ToolCallRequest, error) {
	upstreamName, err := upstreamToolName(toolState, sharedbridge.ToolKindToolSearch, "", "tool_search")
	if err != nil {
		return dto.ToolCallRequest{}, err
	}
	arguments := toolSearchArguments(item["arguments"])
	if arguments == "" {
		arguments = "{}"
	}
	return dto.ToolCallRequest{
		ID:   responsesCallID(item),
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      upstreamName,
			Arguments: arguments,
		},
	}, nil
}

func responsesLocalShellCallItemToChatToolCall(item map[string]any, toolState *sharedbridge.ToolState) (dto.ToolCallRequest, error) {
	upstreamName, err := upstreamToolName(toolState, sharedbridge.ToolKindLocalShell, "", sharedbridge.LocalShellToolName)
	if err != nil {
		return dto.ToolCallRequest{}, err
	}
	return dto.ToolCallRequest{
		ID:   responsesCallID(item),
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      upstreamName,
			Arguments: sharedbridge.LocalShellCallArguments(item["action"]),
		},
	}, nil
}

func appendToolCallToLastAssistant(messages []dto.Message, toolCall dto.ToolCallRequest) []dto.Message {
	if len(messages) == 0 || messages[len(messages)-1].Role != "assistant" {
		messages = append(messages, dto.Message{Role: "assistant"})
	}

	idx := len(messages) - 1
	toolCalls := messages[idx].ParseToolCalls()
	toolCalls = append(toolCalls, toolCall)
	toolCallsRaw, _ := kitutil.Marshal(toolCalls)
	messages[idx].ToolCalls = toolCallsRaw
	return messages
}

func responsesRequestToolChoiceToChat(raw json.RawMessage, toolState *sharedbridge.ToolState) (any, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}
	if kitutil.GetJsonType(raw) == "string" {
		var choice string
		if err := kitutil.Unmarshal(raw, &choice); err != nil {
			return nil, fmt.Errorf("invalid tool_choice: %w", err)
		}
		return choice, nil
	}

	var choice map[string]any
	if err := kitutil.Unmarshal(raw, &choice); err != nil {
		return nil, fmt.Errorf("invalid tool_choice: %w", err)
	}
	choiceType := strings.TrimSpace(kitutil.Interface2String(choice["type"]))
	if choiceType == "function" || choiceType == "custom" || choiceType == "freeform" || choiceType == "tool_search" {
		name := strings.TrimSpace(kitutil.Interface2String(choice["name"]))
		namespace := strings.TrimSpace(kitutil.Interface2String(choice["namespace"]))
		kind := sharedbridge.ToolKindFunction
		if choiceType == "custom" || choiceType == "freeform" {
			kind = sharedbridge.ToolKindCustom
		} else if choiceType == "tool_search" {
			kind = sharedbridge.ToolKindToolSearch
			name = "tool_search"
		}
		if name != "" {
			upstreamName := name
			if toolState == nil {
				if kind != sharedbridge.ToolKindFunction || namespace != "" {
					return nil, fmt.Errorf("Responses tool_choice references undeclared tool %q", qualifiedToolName(namespace, name))
				}
			} else {
				var err error
				upstreamName, err = declaredUpstreamToolName(toolState, kind, namespace, name)
				if err != nil {
					return nil, err
				}
			}
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": upstreamName,
				},
			}, nil
		}
	}
	return choice, nil
}

func responsesRequestTextToChatResponseFormat(raw json.RawMessage) (*dto.ResponseFormat, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}

	var textConfig map[string]any
	if err := kitutil.Unmarshal(raw, &textConfig); err != nil {
		return nil, fmt.Errorf("invalid text config: %w", err)
	}
	format, ok := textConfig["format"].(map[string]any)
	if !ok {
		return nil, nil
	}

	formatType := strings.TrimSpace(kitutil.Interface2String(format["type"]))
	if formatType == "" {
		return nil, nil
	}

	out := &dto.ResponseFormat{Type: formatType}
	if formatType == "json_schema" {
		schemaRaw, err := kitutil.Marshal(format)
		if err != nil {
			return nil, err
		}
		out.JsonSchema = schemaRaw
	}
	return out, nil
}

func RequestTextToChatResponseFormat(raw json.RawMessage) (*dto.ResponseFormat, error) {
	return responsesRequestTextToChatResponseFormat(raw)
}

func responsesImagePartToChatImageURL(part map[string]any) any {
	if imageURL, ok := part["image_url"]; ok {
		return imageURL
	}
	imageURL := map[string]any{}
	for _, key := range []string{"url", "file_id", "detail"} {
		if value, ok := part[key]; ok {
			imageURL[key] = value
		}
	}
	if len(imageURL) == 0 {
		return part
	}
	return imageURL
}

func responsesFilePartToChatFile(c context.Context, part map[string]any) (any, error) {
	file := map[string]any{}
	if nested, ok := part["file"].(map[string]any); ok {
		for key, value := range nested {
			file[key] = value
		}
	}
	for _, key := range []string{"file_id", "file_data", "filename", "file_url"} {
		if value, ok := part[key]; ok {
			file[key] = value
		}
	}
	if source, ok := part["source"].(map[string]any); ok {
		sourceType := strings.TrimSpace(kitutil.Interface2String(source["type"]))
		switch sourceType {
		case "url":
			file["file_url"] = source["url"]
		case "base64":
			data := strings.TrimSpace(kitutil.Interface2String(source["data"]))
			mimeType := strings.TrimSpace(kitutil.Interface2String(source["media_type"]))
			if data != "" && mimeType != "" && !strings.HasPrefix(strings.ToLower(data), "data:") {
				data = fmt.Sprintf("data:%s;base64,%s", mimeType, data)
			}
			if data != "" {
				file["file_data"] = data
			}
		}
		if _, ok := file["filename"]; !ok {
			if title := strings.TrimSpace(kitutil.Interface2String(part["title"])); title != "" {
				file["filename"] = title
			}
		}
	}
	if len(file) == 0 {
		return part, nil
	}

	fileURL := strings.TrimSpace(kitutil.Interface2String(file["file_url"]))
	if fileURL == "" {
		return file, nil
	}
	base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, ContentPartToFileSource(map[string]any{
		"type":     "input_file",
		"file_url": fileURL,
	}), "formatting Responses document for Chat Completions")
	if err != nil {
		return nil, fmt.Errorf("get document data failed: %w", err)
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "application/octet-stream"
	}
	file["file_data"] = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
	delete(file, "file_url")
	return file, nil
}

func responsesVideoPartToChatVideoURL(part map[string]any) any {
	if videoURL, ok := part["video_url"]; ok {
		if videoURLMap, ok := videoURL.(map[string]any); ok {
			if url := kitutil.Interface2String(videoURLMap["url"]); url != "" {
				return url
			}
		}
		return videoURL
	}
	if url := kitutil.Interface2String(part["url"]); url != "" {
		return url
	}
	return responsesPartPayload(part, "video_url")
}

func responsesPartPayload(part map[string]any, key string) any {
	if value, ok := part[key]; ok {
		return value
	}
	payload := make(map[string]any, len(part))
	for k, value := range part {
		if k == "type" {
			continue
		}
		payload[k] = value
	}
	return payload
}

func responsesCallID(item map[string]any) string {
	callID := strings.TrimSpace(kitutil.Interface2String(item["call_id"]))
	if callID != "" {
		return callID
	}
	return strings.TrimSpace(kitutil.Interface2String(item["id"]))
}

func CallID(item map[string]any) string {
	return responsesCallID(item)
}

const (
	responsesToolMediaMovedMarker = "[new-api: tool result media moved to the following user message]"
	responsesToolMediaMaxDepth    = 32
)

func flushResponsesToolMedia(messages []dto.Message, pendingMedia *[]dto.MediaContent) []dto.Message {
	if pendingMedia == nil || len(*pendingMedia) == 0 {
		return messages
	}
	message := dto.Message{Role: "user"}
	message.SetMediaContent(*pendingMedia)
	messages = append(messages, message)
	*pendingMedia = nil
	return messages
}

func responseToolOutputToChatContent(c context.Context, value any) (string, []dto.MediaContent, error) {
	plan, err := sharedtoolmedia.PlanChatToolOutput(value)
	if err != nil {
		return "", nil, err
	}
	if plan != nil {
		return plan.Content, plan.Media, nil
	}
	switch v := value.(type) {
	case nil:
		return "", nil, nil
	case string:
		transformed, media, changed, err := stripResponsesToolMedia(c, v, 0)
		if err != nil {
			return "", nil, err
		}
		if !changed {
			return v, nil, nil
		}
		return transformed.(string), media, nil
	default:
		transformed, media, _, err := stripResponsesToolMedia(c, v, 0)
		if err != nil {
			return "", nil, err
		}
		raw, err := kitutil.Marshal(transformed)
		if err != nil {
			return "", nil, err
		}
		return string(raw), media, nil
	}
}

func stripResponsesToolMedia(c context.Context, value any, depth int) (any, []dto.MediaContent, bool, error) {
	if depth > responsesToolMediaMaxDepth {
		return value, nil, false, nil
	}

	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if len(trimmed) >= 8*1024 && strings.HasPrefix(strings.ToLower(trimmed), "data:image/") && strings.Contains(trimmed, ";base64,") {
			return responsesToolMediaMovedMarker, []dto.MediaContent{{
				Type:     dto.ContentTypeImageURL,
				ImageUrl: &dto.MessageImageUrl{Url: trimmed},
			}}, true, nil
		}
		if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
			return value, nil, false, nil
		}
		var parsed any
		if err := kitutil.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return value, nil, false, nil
		}
		transformed, media, changed, err := stripResponsesToolMedia(c, parsed, depth+1)
		if err != nil || !changed {
			return value, media, changed, err
		}
		raw, err := kitutil.Marshal(transformed)
		if err != nil {
			return nil, nil, false, err
		}
		return string(raw), media, true, nil
	case []any:
		out := make([]any, len(typed))
		media := make([]dto.MediaContent, 0)
		changed := false
		for i, item := range typed {
			transformed, itemMedia, itemChanged, err := stripResponsesToolMedia(c, item, depth+1)
			if err != nil {
				return nil, nil, false, err
			}
			out[i] = transformed
			media = append(media, itemMedia...)
			changed = changed || itemChanged
		}
		return out, media, changed, nil
	case []map[string]any:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = item
		}
		return stripResponsesToolMedia(c, items, depth)
	case map[string]any:
		media, recognized, err := responsesToolMediaPart(c, typed)
		if err != nil {
			return nil, nil, false, err
		}
		if recognized {
			return map[string]any{
				"type": "text",
				"text": responsesToolMediaMovedMarker,
			}, []dto.MediaContent{media}, true, nil
		}
		content, ok := typed["content"]
		if !ok {
			return value, nil, false, nil
		}
		transformed, nestedMedia, changed, err := stripResponsesToolMedia(c, content, depth+1)
		if err != nil || !changed {
			return value, nestedMedia, changed, err
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		out["content"] = transformed
		return out, nestedMedia, true, nil
	default:
		return value, nil, false, nil
	}
}

func responsesToolMediaPart(c context.Context, part map[string]any) (dto.MediaContent, bool, error) {
	partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
	switch partType {
	case "input_image", "image_url":
		imageURL := responsesImagePartToChatImageURL(part)
		if responsesChatImageURLString(imageURL) == "" {
			return dto.MediaContent{}, false, nil
		}
		return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: responsesChatImageURLValue(imageURL)}, true, nil
	case "image":
		imageURL := responsesTypedImageToChatImageURL(part)
		if imageURL == nil {
			return dto.MediaContent{}, false, nil
		}
		return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: imageURL}, true, nil
	case "input_file", "document":
		file, err := responsesFilePartToChatFile(c, part)
		if err != nil {
			return dto.MediaContent{}, false, err
		}
		fileMap, ok := file.(map[string]any)
		if !ok {
			return dto.MediaContent{}, false, nil
		}
		fileID := strings.TrimSpace(kitutil.Interface2String(fileMap["file_id"]))
		fileData := strings.TrimSpace(kitutil.Interface2String(fileMap["file_data"]))
		if fileID == "" && fileData == "" {
			return dto.MediaContent{}, false, nil
		}
		return dto.MediaContent{Type: dto.ContentTypeFile, File: fileMap}, true, nil
	case "input_audio":
		inputAudio, ok := part["input_audio"]
		if !ok {
			return dto.MediaContent{}, false, nil
		}
		return dto.MediaContent{Type: dto.ContentTypeInputAudio, InputAudio: inputAudio}, true, nil
	case "input_video":
		videoURL := responsesVideoPartToChatVideoURL(part)
		url := responsesChatMediaURLString(videoURL)
		if url == "" {
			return dto.MediaContent{}, false, nil
		}
		return dto.MediaContent{Type: dto.ContentTypeVideoUrl, VideoUrl: &dto.MessageVideoUrl{Url: url}}, true, nil
	default:
		if partType == "" {
			imageURL := responsesImagePartToChatImageURL(part)
			if url := responsesChatImageURLString(imageURL); strings.HasPrefix(strings.ToLower(url), "data:image/") {
				return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: imageURL}, true, nil
			}
		}
		return dto.MediaContent{}, false, nil
	}
}

func responsesTypedImageToChatImageURL(part map[string]any) any {
	if source, ok := part["source"].(map[string]any); ok {
		if url := strings.TrimSpace(kitutil.Interface2String(source["url"])); url != "" {
			return map[string]any{"url": url}
		}
		data := strings.TrimSpace(kitutil.Interface2String(source["data"]))
		if data != "" {
			mimeType := strings.TrimSpace(kitutil.Interface2String(source["media_type"]))
			if mimeType == "" {
				mimeType = strings.TrimSpace(kitutil.Interface2String(source["mime_type"]))
			}
			if mimeType == "" {
				mimeType = "image/png"
			}
			if strings.HasPrefix(strings.ToLower(data), "data:image/") {
				return map[string]any{"url": data}
			}
			return map[string]any{"url": fmt.Sprintf("data:%s;base64,%s", mimeType, data)}
		}
	}
	data := strings.TrimSpace(kitutil.Interface2String(part["data"]))
	if data == "" {
		return nil
	}
	mimeType := strings.TrimSpace(kitutil.Interface2String(part["mimeType"]))
	if mimeType == "" {
		mimeType = strings.TrimSpace(kitutil.Interface2String(part["mime_type"]))
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil
	}
	return map[string]any{"url": fmt.Sprintf("data:%s;base64,%s", mimeType, data)}
}

func responsesChatImageURLString(imageURL any) string {
	switch typed := imageURL.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return strings.TrimSpace(kitutil.Interface2String(typed["url"]))
	default:
		return ""
	}
}

func responsesChatImageURLValue(imageURL any) any {
	if url, ok := imageURL.(string); ok {
		return &dto.MessageImageUrl{Url: strings.TrimSpace(url)}
	}
	return imageURL
}

func responsesChatMediaURLString(mediaURL any) string {
	switch typed := mediaURL.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return strings.TrimSpace(kitutil.Interface2String(typed["url"]))
	default:
		return ""
	}
}

func responsesJSONString(raw json.RawMessage) (string, error) {
	if kitutil.GetJsonType(raw) != "string" {
		return string(raw), nil
	}
	var value string
	if err := kitutil.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

func rawJSONPresent(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	return kitutil.GetJsonType(raw) != "null"
}

func JSONString(raw json.RawMessage) (string, error) {
	return responsesJSONString(raw)
}

func RawJSONPresent(raw json.RawMessage) bool {
	return rawJSONPresent(raw)
}
