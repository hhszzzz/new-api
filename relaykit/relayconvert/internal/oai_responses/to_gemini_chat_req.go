package oairesponses

import (
	"fmt"
	"strings"

	"context"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	sharedtoolmedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/toolmedia"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func convertOpenAIResponsesRequestToGeminiChat(c context.Context, info convmeta.Meta, request any) (any, error) {
	responsesRequest, err := OpenAIResponsesRequestFromAny(request)
	if err != nil {
		return nil, err
	}
	return OpenAIResponsesRequestToGeminiChat(c, responsesRequest, info)
}

func OpenAIResponsesRequestToGeminiChat(c context.Context, req *dto.OpenAIResponsesRequest, info convmeta.Meta) (*dto.GeminiChatRequest, error) {
	opts := convmeta.OptionsOf(info)
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if err := ValidateRequestChatUnsupportedFields(req); err != nil {
		return nil, err
	}

	geminiRequest := &dto.GeminiChatRequest{
		GenerationConfig: dto.GeminiChatGenerationConfig{
			Temperature: req.Temperature,
		},
	}
	if req.TopP != nil {
		geminiRequest.GenerationConfig.TopP = kitutil.GetPointer(*req.TopP)
	}
	if req.MaxOutputTokens != nil {
		geminiRequest.GenerationConfig.MaxOutputTokens = kitutil.GetPointer(*req.MaxOutputTokens)
	}

	upstreamModelName := req.Model
	if modelName := convmeta.UpstreamModelName(info); modelName != "" {
		upstreamModelName = modelName
	}
	if opts.Gemini.SupportsImagineModel(upstreamModelName) {
		geminiRequest.GenerationConfig.ResponseModalities = []string{"TEXT", "IMAGE"}
	}
	if err := applyResponsesTextToGemini(req.Text, geminiRequest); err != nil {
		return nil, err
	}
	sharedgemini.ApplyThinkingConfig(geminiRequest, info, dto.GeneralOpenAIRequest{
		Model:               req.Model,
		MaxCompletionTokens: req.MaxOutputTokens,
		ReasoningEffort:     ReasoningEffort(req),
	})

	var safetySettings []dto.GeminiChatSafetySettings
	for _, category := range sharedgemini.SafetySettingCategories {
		threshold := opts.Gemini.SafetySettingFor(category)
		if threshold == "" {
			continue
		}
		safetySettings = append(safetySettings, dto.GeminiChatSafetySettings{
			Category:  category,
			Threshold: threshold,
		})
	}
	if len(safetySettings) > 0 {
		geminiRequest.SafetySettings = safetySettings
	}

	// The shared tool bridge lowers custom/freeform/tool_search/namespace tools
	// into function declarations with reversible names and records the mapping
	// in the context tool state, so Gemini responses restore the original
	// Responses shapes exactly like the Chat and Claude upstream paths.
	chatTools, toolState, err := prepareResponsesToolsForChat(c, req)
	if err != nil {
		return nil, err
	}
	functions := make([]dto.FunctionRequest, 0, len(chatTools))
	for _, tool := range chatTools {
		if tool.Type != "function" {
			return nil, fmt.Errorf("Responses tool type %q cannot be converted to Gemini", tool.Type)
		}
		functions = append(functions, tool.Function)
	}
	for i := range functions {
		sharedgemini.PrepareFunctionDeclaration(&functions[i])
	}
	if len(functions) > 0 {
		geminiRequest.SetTools([]dto.GeminiChatTool{
			{FunctionDeclarations: functions},
		})
	}

	toolChoice, err := responsesRequestToolChoiceToChat(req.ToolChoice, toolState)
	if err != nil {
		return nil, err
	}
	if toolChoice != nil {
		if choice, ok := toolChoice.(map[string]any); ok && choice["type"] == "function" {
			function, _ := choice["function"].(map[string]any)
			selectedName := strings.TrimSpace(kitutil.Interface2String(function["name"]))
			declared := false
			for _, function := range functions {
				if function.Name == selectedName {
					declared = true
					break
				}
			}
			if selectedName != "" && !declared {
				return nil, fmt.Errorf("Responses tool_choice references undeclared tool %q", selectedName)
			}
		}
		geminiRequest.ToolConfig = sharedgemini.OpenAIToolChoiceToConfig(toolChoice)
	}

	systemTexts := make([]string, 0)
	if RawJSONPresent(req.Instructions) {
		instructions, err := JSONString(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("invalid instructions: %w", err)
		}
		if strings.TrimSpace(instructions) != "" {
			systemTexts = append(systemTexts, instructions)
		}
	}

	inputItems, err := InputItems(req.Input)
	if err != nil {
		return nil, err
	}
	// Gemini requires strict functionCall/functionResponse pairing, so drop
	// incomplete call batches and orphaned outputs the same way the Claude
	// conversion does (Codex history compaction produces both).
	inputItems, err = filterIncompleteResponsesToolHistory(inputItems, true)
	if err != nil {
		return nil, err
	}
	callNames := make(map[string]string)
	for _, item := range inputItems {
		itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
		switch itemType {
		case ResponsesInputTypeFunctionCall:
			part, callID, err := responsesFunctionCallItemToGeminiPart(item)
			if err != nil {
				return nil, err
			}
			sharedgemini.AttachFunctionCallThoughtSignature(opts, &part)
			if callID != "" {
				callNames[callID] = part.FunctionCall.FunctionName
			}
			appendGeminiContentPart(geminiRequest, "model", part)
		case ResponsesInputTypeFunctionCallOutput:
			parts, err := responsesFunctionOutputItemToGeminiParts(item, callNames, sharedgemini.IsGemini3Series(upstreamModelName))
			if err != nil {
				return nil, err
			}
			for _, part := range parts {
				appendGeminiContentPart(geminiRequest, "user", part)
			}
		case ResponsesInputTypeCustomToolCall, "tool_search_call", "local_shell_call":
			var toolCall dto.ToolCallRequest
			var convErr error
			switch itemType {
			case "tool_search_call":
				toolCall, convErr = responsesToolSearchCallItemToChatToolCall(item, toolState)
			case "local_shell_call":
				toolCall, convErr = responsesLocalShellCallItemToChatToolCall(item, toolState)
			default:
				toolCall, convErr = responsesCustomToolCallItemToChatToolCall(item, toolState)
			}
			if convErr != nil {
				return nil, convErr
			}
			part, callID, err := responsesFunctionCallItemToGeminiPart(map[string]any{
				"name":      toolCall.Function.Name,
				"call_id":   toolCall.ID,
				"arguments": toolCall.Function.Arguments,
			})
			if err != nil {
				return nil, err
			}
			sharedgemini.AttachFunctionCallThoughtSignature(opts, &part)
			if callID != "" {
				callNames[callID] = part.FunctionCall.FunctionName
			}
			appendGeminiContentPart(geminiRequest, "model", part)
		case ResponsesInputTypeCustomToolOutput, "tool_search_output", "local_shell_call_output":
			callID := CallID(item)
			if _, converted := callNames[callID]; !converted {
				continue
			}
			// Pair with the lowered call under its bridge-encoded function name.
			item["name"] = callNames[callID]
			if itemType == "tool_search_output" {
				item["output"] = item["tools"]
			}
			parts, err := responsesFunctionOutputItemToGeminiParts(item, callNames, sharedgemini.IsGemini3Series(upstreamModelName))
			if err != nil {
				return nil, err
			}
			for _, part := range parts {
				appendGeminiContentPart(geminiRequest, "user", part)
			}
		case "reasoning", "additional_tools":
			// Cross-provider reasoning state cannot replay into Gemini, and
			// additional_tools declarations were already lifted into the tools
			// list by prepareResponsesToolsForChat.
		case "", "message":
			role := responsesGeminiRole(item)
			parts, err := responsesInputContentToGeminiParts(c, item["content"])
			if err != nil {
				return nil, err
			}
			if role == "system" {
				for _, part := range parts {
					if part.Text != "" {
						systemTexts = append(systemTexts, part.Text)
					}
				}
				continue
			}
			if len(parts) > 0 {
				geminiRequest.Contents = append(geminiRequest.Contents, dto.GeminiChatContent{
					Role:  role,
					Parts: parts,
				})
			}
		default:
			return nil, fmt.Errorf("Responses input item type %q cannot be converted losslessly to Gemini", itemType)
		}
	}

	if len(systemTexts) > 0 {
		geminiRequest.SystemInstructions = &dto.GeminiChatContent{
			Parts: []dto.GeminiPart{{Text: strings.Join(systemTexts, "\n\n")}},
		}
	}

	return geminiRequest, nil
}

func applyResponsesTextToGemini(raw []byte, geminiRequest *dto.GeminiChatRequest) error {
	responseFormat, err := RequestTextToChatResponseFormat(raw)
	if err != nil {
		return err
	}
	if responseFormat == nil || (responseFormat.Type != "json_schema" && responseFormat.Type != "json_object") {
		return nil
	}

	geminiRequest.GenerationConfig.ResponseMimeType = "application/json"
	if len(responseFormat.JsonSchema) == 0 {
		return nil
	}

	var jsonSchema dto.FormatJsonSchema
	if err := kitutil.Unmarshal(responseFormat.JsonSchema, &jsonSchema); err != nil {
		return nil
	}
	geminiRequest.GenerationConfig.ResponseSchema = sharedgemini.RemoveAdditionalProperties(jsonSchema.Schema, 0)
	return nil
}

func responsesInputContentToGeminiParts(c context.Context, content any) ([]dto.GeminiPart, error) {
	contentParts, err := ContentParts(content)
	if err != nil {
		return nil, err
	}

	parts := make([]dto.GeminiPart, 0, len(contentParts))
	for _, contentPart := range contentParts {
		nextParts, err := responsesContentPartToGeminiParts(c, contentPart)
		if err != nil {
			return nil, err
		}
		parts = append(parts, nextParts...)
	}
	return parts, nil
}

func responsesContentPartToGeminiParts(c context.Context, part map[string]any) ([]dto.GeminiPart, error) {
	partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
	switch partType {
	case "input_text", "output_text", "text":
		text := kitutil.Interface2String(part["text"])
		if text == "" {
			return nil, nil
		}
		return []dto.GeminiPart{{Text: text}}, nil
	case "input_image", "input_file", "input_audio", "input_video":
		source := ContentPartToFileSource(part)
		if source == nil {
			return nil, fmt.Errorf("Responses %s content is missing inline data or URL", partType)
		}
		base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting Responses input for Gemini")
		if err != nil {
			return nil, fmt.Errorf("get file data from '%s' failed: %w", source.GetIdentifier(), err)
		}
		if _, ok := sharedgemini.SupportedMimeTypes[strings.ToLower(mimeType)]; !ok {
			return nil, fmt.Errorf("mime type is not supported by Gemini: '%s', url: '%s', supported types are: %v", mimeType, source.GetIdentifier(), sharedgemini.SupportedMimeTypesList())
		}
		return []dto.GeminiPart{
			{
				InlineData: &dto.GeminiInlineData{
					MimeType: mimeType,
					Data:     base64Data,
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("Responses content type %q cannot be converted losslessly to Gemini", partType)
	}
}

func responsesFunctionCallItemToGeminiPart(item map[string]any) (dto.GeminiPart, string, error) {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	if name == "" {
		return dto.GeminiPart{}, "", fmt.Errorf("function_call item is missing name")
	}
	callID := CallID(item)
	arguments, err := responsesFunctionArgumentsObject(item["arguments"])
	if err != nil {
		return dto.GeminiPart{}, "", fmt.Errorf("function_call %q: %w", callID, err)
	}
	upstreamCallID := callID
	if sharedgemini.IsSynthesizedToolCallID(upstreamCallID) {
		upstreamCallID = ""
	}
	return dto.GeminiPart{
		FunctionCall: &dto.FunctionCall{
			ID:           upstreamCallID,
			FunctionName: name,
			Arguments:    arguments,
		},
	}, callID, nil
}

func responsesFunctionOutputItemToGeminiParts(item map[string]any, callNames map[string]string, gemini3 bool) ([]dto.GeminiPart, error) {
	callID := CallID(item)
	if callID == "" {
		return nil, fmt.Errorf("function_call_output item is missing call_id")
	}
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	if name == "" {
		name = callNames[callID]
	}
	if name == "" {
		return nil, fmt.Errorf("function_call_output references unknown call_id %q", callID)
	}
	var responseID []byte
	if !sharedgemini.IsSynthesizedToolCallID(callID) {
		var err error
		responseID, err = kitutil.Marshal(callID)
		if err != nil {
			return nil, fmt.Errorf("marshal Gemini function response id: %w", err)
		}
	}
	output := item["output"]
	cleaned, media, changed, err := sharedtoolmedia.StripAndClamp(
		output,
		sharedtoolmedia.InlineImagesOnly,
		map[string]any{"type": "text", "text": sharedtoolmedia.ToolResultMediaAttachedMarker},
		sharedtoolmedia.ToolResultMediaAttachedMarker,
	)
	if err != nil {
		return nil, err
	}
	response := GeminiResponseMap(output)
	mediaParts := make([]dto.GeminiPart, 0, len(media))
	if changed {
		response = geminiToolResponseMap(cleaned)
		for _, item := range media {
			if part, ok := geminiInlineImagePart(item); ok {
				mediaParts = append(mediaParts, part)
			}
		}
	}
	functionResponse := &dto.GeminiFunctionResponse{
		Name:     name,
		Response: response,
		ID:       responseID,
	}
	if gemini3 && len(mediaParts) > 0 {
		raw, err := kitutil.Marshal(mediaParts)
		if err != nil {
			return nil, err
		}
		functionResponse.Parts = raw
		return []dto.GeminiPart{{FunctionResponse: functionResponse}}, nil
	}
	parts := []dto.GeminiPart{{
		FunctionResponse: &dto.GeminiFunctionResponse{
			Name:     functionResponse.Name,
			Response: functionResponse.Response,
			ID:       functionResponse.ID,
		},
	}}
	if len(mediaParts) > 0 {
		parts = append(parts, dto.GeminiPart{Text: fmt.Sprintf("[new-api: media output of tool call %s]", callID)})
		parts = append(parts, mediaParts...)
	}
	return parts, nil
}

func geminiToolResponseMap(value any) map[string]interface{} {
	if text, ok := value.(string); ok {
		return map[string]interface{}{"content": text}
	}
	if items, ok := value.([]any); ok {
		texts := make([]string, 0, len(items))
		for _, item := range items {
			part, ok := item.(map[string]any)
			if !ok || strings.TrimSpace(kitutil.Interface2String(part["type"])) != "text" {
				continue
			}
			if text := kitutil.Interface2String(part["text"]); text != "" {
				texts = append(texts, text)
			}
		}
		if len(texts) > 0 {
			return map[string]interface{}{"content": strings.Join(texts, "\n")}
		}
	}
	return map[string]interface{}{"content": value}
}

func geminiInlineImagePart(media dto.MediaContent) (dto.GeminiPart, bool) {
	url := sharedtoolmedia.ImageURL(media)
	if len(url) < 6 || !strings.EqualFold(url[:5], "data:") {
		return dto.GeminiPart{}, false
	}
	meta, data, ok := strings.Cut(url[5:], ",")
	if !ok || data == "" || !strings.HasSuffix(strings.ToLower(meta), ";base64") {
		return dto.GeminiPart{}, false
	}
	mimeType := strings.SplitN(meta, ";", 2)[0]
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return dto.GeminiPart{}, false
	}
	return dto.GeminiPart{InlineData: &dto.GeminiInlineData{MimeType: mimeType, Data: data}}, true
}

func appendGeminiContentPart(req *dto.GeminiChatRequest, role string, part dto.GeminiPart) {
	if len(req.Contents) > 0 && req.Contents[len(req.Contents)-1].Role == role {
		if role == "model" && part.FunctionCall != nil {
			parts := req.Contents[len(req.Contents)-1].Parts
			insertAt := 0
			for insertAt < len(parts) && parts[insertAt].FunctionCall != nil {
				insertAt++
			}
			parts = append(parts, dto.GeminiPart{})
			copy(parts[insertAt+1:], parts[insertAt:])
			parts[insertAt] = part
			req.Contents[len(req.Contents)-1].Parts = parts
			return
		}
		req.Contents[len(req.Contents)-1].Parts = append(req.Contents[len(req.Contents)-1].Parts, part)
		return
	}
	req.Contents = append(req.Contents, dto.GeminiChatContent{
		Role:  role,
		Parts: []dto.GeminiPart{part},
	})
}

func responsesGeminiRole(item map[string]any) string {
	switch strings.TrimSpace(kitutil.Interface2String(item["role"])) {
	case "assistant":
		return "model"
	case "system", "developer":
		return "system"
	case "model":
		return "model"
	default:
		return "user"
	}
}
