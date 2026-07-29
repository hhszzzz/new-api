package oaichat

import (
	"errors"
	"fmt"
	"strings"

	"context"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func OpenAIChatRequestToGeminiGenerateContent(c context.Context, textRequest dto.GeneralOpenAIRequest, info convmeta.Meta) (*dto.GeminiChatRequest, error) {
	opts := convmeta.OptionsOf(info)
	geminiRequest := dto.GeminiChatRequest{
		Contents: make([]dto.GeminiChatContent, 0, len(textRequest.Messages)),
		GenerationConfig: dto.GeminiChatGenerationConfig{
			Temperature: textRequest.Temperature,
		},
	}

	if textRequest.TopP != nil {
		geminiRequest.GenerationConfig.TopP = kitutil.GetPointer(*textRequest.TopP)
	}
	if textRequest.MaxCompletionTokens != nil {
		geminiRequest.GenerationConfig.MaxOutputTokens = kitutil.GetPointer(*textRequest.MaxCompletionTokens)
	} else if textRequest.MaxTokens != nil {
		geminiRequest.GenerationConfig.MaxOutputTokens = kitutil.GetPointer(*textRequest.MaxTokens)
	}
	if textRequest.Seed != nil {
		geminiRequest.GenerationConfig.Seed = kitutil.GetPointer(int64(*textRequest.Seed))
	}

	upstreamModelName := textRequest.Model
	if modelName := convmeta.UpstreamModelName(info); modelName != "" {
		upstreamModelName = modelName
	}

	if opts.Gemini.SupportsImagineModel(upstreamModelName) {
		geminiRequest.GenerationConfig.ResponseModalities = []string{
			"TEXT",
			"IMAGE",
		}
	}
	if stopSequences := sharedgemini.ParseStopSequences(textRequest.Stop); len(stopSequences) > 0 {
		if len(stopSequences) > 5 {
			stopSequences = stopSequences[:5]
		}
		geminiRequest.GenerationConfig.StopSequences = stopSequences
	}

	adaptorWithExtraBody := false
	if len(textRequest.ExtraBody) > 0 {
		var extraBody map[string]interface{}
		if err := kitutil.Unmarshal(textRequest.ExtraBody, &extraBody); err != nil {
			return nil, fmt.Errorf("invalid extra body: %w", err)
		}

		if googleBody, ok := extraBody["google"].(map[string]interface{}); ok {
			if !strings.HasSuffix(upstreamModelName, "-nothinking") {
				adaptorWithExtraBody = true
				if _, hasErrorParam := googleBody["thinkingConfig"]; hasErrorParam {
					return nil, errors.New("extra_body.google.thinkingConfig is not supported, use extra_body.google.thinking_config instead")
				}

				if thinkingConfig, ok := googleBody["thinking_config"].(map[string]interface{}); ok {
					if _, hasErrorParam := thinkingConfig["thinkingBudget"]; hasErrorParam {
						return nil, errors.New("extra_body.google.thinking_config.thinkingBudget is not supported, use extra_body.google.thinking_config.thinking_budget instead")
					}
					var hasThinkingConfig bool
					var tempThinkingConfig dto.GeminiThinkingConfig

					if thinkingBudget, exists := thinkingConfig["thinking_budget"]; exists {
						switch v := thinkingBudget.(type) {
						case float64:
							budgetInt := int(v)
							tempThinkingConfig.ThinkingBudget = kitutil.GetPointer(budgetInt)
							tempThinkingConfig.IncludeThoughts = budgetInt > 0
							hasThinkingConfig = true
						default:
							return nil, errors.New("extra_body.google.thinking_config.thinking_budget must be an integer")
						}
					}

					if includeThoughts, exists := thinkingConfig["include_thoughts"]; exists {
						if v, ok := includeThoughts.(bool); ok {
							tempThinkingConfig.IncludeThoughts = v
							hasThinkingConfig = true
						} else {
							return nil, errors.New("extra_body.google.thinking_config.include_thoughts must be a boolean")
						}
					}
					if thinkingLevel, exists := thinkingConfig["thinking_level"]; exists {
						if v, ok := thinkingLevel.(string); ok {
							tempThinkingConfig.ThinkingLevel = v
							hasThinkingConfig = true
						} else {
							return nil, errors.New("extra_body.google.thinking_config.thinking_level must be a string")
						}
					}

					if hasThinkingConfig {
						if geminiRequest.GenerationConfig.ThinkingConfig == nil {
							geminiRequest.GenerationConfig.ThinkingConfig = &tempThinkingConfig
						} else {
							if tempThinkingConfig.ThinkingBudget != nil {
								geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget = tempThinkingConfig.ThinkingBudget
							}
							geminiRequest.GenerationConfig.ThinkingConfig.IncludeThoughts = tempThinkingConfig.IncludeThoughts
							if tempThinkingConfig.ThinkingLevel != "" {
								geminiRequest.GenerationConfig.ThinkingConfig.ThinkingLevel = tempThinkingConfig.ThinkingLevel
							}
						}
					}
				}
			}

			if _, hasErrorParam := googleBody["imageConfig"]; hasErrorParam {
				return nil, errors.New("extra_body.google.imageConfig is not supported, use extra_body.google.image_config instead")
			}

			if imageConfig, ok := googleBody["image_config"].(map[string]interface{}); ok {
				if _, hasErrorParam := imageConfig["aspectRatio"]; hasErrorParam {
					return nil, errors.New("extra_body.google.image_config.aspectRatio is not supported, use extra_body.google.image_config.aspect_ratio instead")
				}
				if _, hasErrorParam := imageConfig["imageSize"]; hasErrorParam {
					return nil, errors.New("extra_body.google.image_config.imageSize is not supported, use extra_body.google.image_config.image_size instead")
				}

				geminiImageConfig := make(map[string]interface{})
				if aspectRatio, ok := imageConfig["aspect_ratio"]; ok {
					geminiImageConfig["aspectRatio"] = aspectRatio
				}
				if imageSize, ok := imageConfig["image_size"]; ok {
					geminiImageConfig["imageSize"] = imageSize
				}

				if len(geminiImageConfig) > 0 {
					imageConfigBytes, err := kitutil.Marshal(geminiImageConfig)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal image_config: %w", err)
					}
					geminiRequest.GenerationConfig.ImageConfig = imageConfigBytes
				}
			}
		}
	}

	if !adaptorWithExtraBody {
		sharedgemini.ApplyThinkingConfig(&geminiRequest, info, textRequest)
	}

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

	if textRequest.Tools != nil {
		functions := make([]dto.FunctionRequest, 0, len(textRequest.Tools))
		googleSearch := false
		codeExecution := false
		urlContext := false
		for _, tool := range textRequest.Tools {
			if tool.Function.Name == "googleSearch" {
				googleSearch = true
				continue
			}
			if tool.Function.Name == "codeExecution" {
				codeExecution = true
				continue
			}
			if tool.Function.Name == "urlContext" {
				urlContext = true
				continue
			}
			sharedgemini.PrepareFunctionDeclaration(&tool.Function)
			functions = append(functions, tool.Function)
		}
		geminiTools := geminiRequest.GetTools()
		if codeExecution {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				CodeExecution: make(map[string]string),
			})
		}
		if googleSearch {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				GoogleSearch: make(map[string]string),
			})
		}
		if urlContext {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				URLContext: make(map[string]string),
			})
		}
		if len(functions) > 0 {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				FunctionDeclarations: functions,
			})
		}
		geminiRequest.SetTools(geminiTools)

		if textRequest.ToolChoice != nil {
			geminiRequest.ToolConfig = sharedgemini.OpenAIToolChoiceToConfig(textRequest.ToolChoice)
		}
	}

	if textRequest.ResponseFormat != nil && (textRequest.ResponseFormat.Type == "json_schema" || textRequest.ResponseFormat.Type == "json_object") {
		geminiRequest.GenerationConfig.ResponseMimeType = "application/json"

		if len(textRequest.ResponseFormat.JsonSchema) > 0 {
			var jsonSchema dto.FormatJsonSchema
			if err := kitutil.Unmarshal(textRequest.ResponseFormat.JsonSchema, &jsonSchema); err == nil {
				cleanedSchema := sharedgemini.RemoveAdditionalProperties(jsonSchema.Schema, 0)
				geminiRequest.GenerationConfig.ResponseSchema = cleanedSchema
			}
		}
	}

	toolCallIDs := make(map[string]string)
	var systemContent []string
	for _, message := range textRequest.Messages {
		if message.Role == "system" || message.Role == "developer" {
			systemContent = append(systemContent, message.StringContent())
			continue
		}
		if message.Role == "tool" || message.Role == "function" {
			if len(geminiRequest.Contents) == 0 || geminiRequest.Contents[len(geminiRequest.Contents)-1].Role == "model" {
				geminiRequest.Contents = append(geminiRequest.Contents, dto.GeminiChatContent{
					Role: "user",
				})
			}
			parts := &geminiRequest.Contents[len(geminiRequest.Contents)-1].Parts
			name := ""
			if message.Name != nil {
				name = strings.TrimSpace(*message.Name)
			}
			if name == "" {
				if val, exists := toolCallIDs[message.ToolCallId]; exists {
					name = val
				}
			}
			if name == "" {
				return nil, fmt.Errorf("unable to resolve Gemini functionResponse.name for tool_call_id %q", message.ToolCallId)
			}
			var contentMap map[string]interface{}
			contentStr := message.StringContent()

			if err := kitutil.Unmarshal([]byte(contentStr), &contentMap); err != nil {
				var contentSlice []interface{}
				if err := kitutil.Unmarshal([]byte(contentStr), &contentSlice); err == nil {
					textParts := make([]string, 0, len(contentSlice))
					for _, item := range contentSlice {
						part, ok := item.(map[string]any)
						if !ok || strings.TrimSpace(kitutil.Interface2String(part["type"])) != "text" {
							continue
						}
						if text := kitutil.Interface2String(part["text"]); text != "" {
							textParts = append(textParts, text)
						}
					}
					if len(textParts) > 0 {
						contentMap = map[string]interface{}{"content": strings.Join(textParts, "\n")}
					} else {
						contentMap = map[string]interface{}{"content": contentSlice}
					}
				} else {
					contentMap = map[string]interface{}{"content": contentStr}
				}
			}

			functionResp := &dto.GeminiFunctionResponse{
				Name:     name,
				Response: contentMap,
			}
			if message.ToolCallId != "" && !sharedgemini.IsSynthesizedToolCallID(message.ToolCallId) {
				encodedID, err := kitutil.Marshal(message.ToolCallId)
				if err != nil {
					return nil, fmt.Errorf("marshal Gemini function response id: %w", err)
				}
				functionResp.ID = encodedID
			}

			*parts = append(*parts, dto.GeminiPart{
				FunctionResponse: functionResp,
			})
			continue
		}

		var parts []dto.GeminiPart
		content := dto.GeminiChatContent{
			Role: message.Role,
		}
		shouldAttachThoughtSignature := (message.Role == "assistant" || message.Role == "model") && sharedgemini.ShouldAttachThoughtSignature(opts)
		signatureAttached := false
		if message.ToolCalls != nil {
			for _, call := range message.ParseToolCalls() {
				args := map[string]interface{}{}
				if call.Function.Arguments != "" {
					if kitutil.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
						return nil, fmt.Errorf("invalid arguments for function %s, args: %s", call.Function.Name, call.Function.Arguments)
					}
				}
				callID := call.ID
				if sharedgemini.IsSynthesizedToolCallID(callID) {
					callID = ""
				}
				toolCall := dto.GeminiPart{
					FunctionCall: &dto.FunctionCall{
						ID:           callID,
						FunctionName: call.Function.Name,
						Arguments:    args,
					},
				}
				if shouldAttachThoughtSignature && !signatureAttached && sharedgemini.AttachFunctionCallThoughtSignature(opts, &toolCall) {
					signatureAttached = true
				}
				parts = append(parts, toolCall)
				toolCallIDs[call.ID] = call.Function.Name
			}
		}

		openaiContent := message.ParseContent()
		for _, part := range openaiContent {
			if part.Type == dto.ContentTypeText {
				if part.Text == "" {
					continue
				}
				text := part.Text
				hasMarkdownImage := false
				for {
					startIdx := strings.Index(text, "![")
					if startIdx == -1 {
						break
					}
					bracketIdx := strings.Index(text[startIdx:], "](data:")
					if bracketIdx == -1 {
						break
					}
					bracketIdx += startIdx
					closeIdx := strings.Index(text[bracketIdx+2:], ")")
					if closeIdx == -1 {
						break
					}
					closeIdx += bracketIdx + 2

					hasMarkdownImage = true
					if startIdx > 0 {
						textBefore := text[:startIdx]
						if textBefore != "" {
							parts = append(parts, dto.GeminiPart{
								Text: textBefore,
							})
						}
					}

					dataURL := text[bracketIdx+2 : closeIdx]
					format, base64String, err := relaymedia.DecodeBase64FileData(dataURL)
					if err != nil {
						return nil, fmt.Errorf("decode markdown base64 image data failed: %s", err.Error())
					}
					imgPart := dto.GeminiPart{
						InlineData: &dto.GeminiInlineData{
							MimeType: format,
							Data:     base64String,
						},
					}
					if shouldAttachThoughtSignature {
						sharedgemini.AttachThoughtSignatureBypass(opts, &imgPart)
					}
					parts = append(parts, imgPart)
					text = text[closeIdx+1:]
				}
				if !hasMarkdownImage {
					parts = append(parts, dto.GeminiPart{
						Text: part.Text,
					})
				}
			} else {
				source := part.ToFileSource()
				if source == nil {
					continue
				}
				base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting image for Gemini")
				if err != nil {
					return nil, fmt.Errorf("get file data from '%s' failed: %w", source.GetIdentifier(), err)
				}

				if _, ok := sharedgemini.SupportedMimeTypes[strings.ToLower(mimeType)]; !ok {
					return nil, fmt.Errorf("mime type is not supported by Gemini: '%s', url: '%s', supported types are: %v", mimeType, source.GetIdentifier(), sharedgemini.SupportedMimeTypesList())
				}

				parts = append(parts, dto.GeminiPart{
					InlineData: &dto.GeminiInlineData{
						MimeType: mimeType,
						Data:     base64Data,
					},
				})
			}
		}

		if shouldAttachThoughtSignature && !signatureAttached && len(parts) > 0 {
			sharedgemini.AttachFirstTextThoughtSignature(opts, parts)
		}

		content.Parts = parts
		if content.Role == "assistant" {
			content.Role = "model"
		}
		if len(content.Parts) > 0 {
			mergeToolMedia := false
			if content.Role == "user" && len(geminiRequest.Contents) > 0 && geminiRequest.Contents[len(geminiRequest.Contents)-1].Role == "user" {
				last := &geminiRequest.Contents[len(geminiRequest.Contents)-1]
				hasFunctionResponse := false
				for _, part := range last.Parts {
					if part.FunctionResponse != nil {
						hasFunctionResponse = true
						break
					}
				}
				mergeToolMedia = hasFunctionResponse && strings.HasPrefix(content.Parts[0].Text, "[new-api: media output of tool call ")
			}
			if mergeToolMedia {
				last := &geminiRequest.Contents[len(geminiRequest.Contents)-1]
				if sharedgemini.IsGemini3Series(upstreamModelName) && attachGemini3FunctionResponseMedia(last, content.Parts) {
					continue
				}
				for _, part := range last.Parts {
					if part.FunctionResponse != nil {
						replaceToolMediaMarker(part.FunctionResponse.Response)
					}
				}
				last.Parts = append(last.Parts, content.Parts...)
			} else {
				geminiRequest.Contents = append(geminiRequest.Contents, content)
			}
		}
	}

	if len(systemContent) > 0 {
		geminiRequest.SystemInstructions = &dto.GeminiChatContent{
			Parts: []dto.GeminiPart{
				{
					Text: strings.Join(systemContent, "\n"),
				},
			},
		}
	}

	return &geminiRequest, nil
}

func attachGemini3FunctionResponseMedia(content *dto.GeminiChatContent, mediaParts []dto.GeminiPart) bool {
	if content == nil || len(mediaParts) < 2 {
		return false
	}

	type mediaGroup struct {
		callID string
		parts  []dto.GeminiPart
	}
	groups := make([]mediaGroup, 0)
	for _, part := range mediaParts {
		if strings.HasPrefix(part.Text, "[new-api: media output of tool call ") && strings.HasSuffix(part.Text, "]") {
			callID := strings.TrimSuffix(strings.TrimPrefix(part.Text, "[new-api: media output of tool call "), "]")
			groups = append(groups, mediaGroup{callID: callID})
			continue
		}
		if len(groups) == 0 || part.InlineData == nil || !strings.HasPrefix(strings.ToLower(part.InlineData.MimeType), "image/") {
			return false
		}
		groups[len(groups)-1].parts = append(groups[len(groups)-1].parts, part)
	}
	if len(groups) == 0 {
		return false
	}

	type mediaAssignment struct {
		index int
		raw   []byte
	}
	used := make(map[int]struct{}, len(groups))
	assignments := make([]mediaAssignment, 0, len(groups))
	for _, group := range groups {
		if len(group.parts) == 0 {
			return false
		}
		match := -1
		for index := range content.Parts {
			if _, exists := used[index]; exists || content.Parts[index].FunctionResponse == nil {
				continue
			}
			callID := kitutil.JsonRawMessageToString(content.Parts[index].FunctionResponse.ID)
			if callID == group.callID {
				match = index
				break
			}
			if callID == "" && match == -1 {
				match = index
			}
		}
		if match == -1 {
			return false
		}
		used[match] = struct{}{}
		raw, err := kitutil.Marshal(group.parts)
		if err != nil {
			return false
		}
		assignments = append(assignments, mediaAssignment{index: match, raw: raw})
	}
	for _, assignment := range assignments {
		response := content.Parts[assignment.index].FunctionResponse
		response.Parts = assignment.raw
		replaceToolMediaMarker(response.Response)
	}
	return true
}

func replaceToolMediaMarker(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if text, ok := item.(string); ok {
				typed[key] = strings.ReplaceAll(
					text,
					"[new-api: tool result media moved to the following user message]",
					"[new-api: tool result media attached as native media]",
				)
				continue
			}
			replaceToolMediaMarker(item)
		}
	case []any:
		for index, item := range typed {
			if text, ok := item.(string); ok {
				typed[index] = strings.ReplaceAll(
					text,
					"[new-api: tool result media moved to the following user message]",
					"[new-api: tool result media attached as native media]",
				)
				continue
			}
			replaceToolMediaMarker(item)
		}
	}
}
