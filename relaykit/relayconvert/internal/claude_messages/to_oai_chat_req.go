package claudemessages

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	sharedchat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/chat"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	sharedtoolmedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/toolmedia"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const (
	webSearchMaxUsesLow    = 1
	webSearchMaxUsesMedium = 5
	webSearchMaxUsesHigh   = 10
)

type openRouterRequestReasoning struct {
	Enabled   bool   `json:"enabled"`
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Exclude   bool   `json:"exclude,omitempty"`
}

func ClaudeMessagesRequestToOpenAIChat(claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	return ClaudeMessagesRequestToOpenAIChatWithContext(context.Background(), claudeRequest, info)
}

func ClaudeMessagesRequestToOpenAIChatWithContext(c context.Context, claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	return claudeMessagesRequestToOpenAIChat(c, claudeRequest, info, sharedtoolmedia.AllSupported)
}

func ClaudeMessagesRequestToOpenAIChatForGeminiBridge(c context.Context, claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	return claudeMessagesRequestToOpenAIChat(c, claudeRequest, info, sharedtoolmedia.InlineImagesOnly)
}

func claudeMessagesRequestToOpenAIChat(c context.Context, claudeRequest dto.ClaudeRequest, info convmeta.Meta, toolMediaScope sharedtoolmedia.Scope) (*dto.GeneralOpenAIRequest, error) {
	if err := validateClaudeRequestConversion(&claudeRequest, "Chat Completions"); err != nil {
		return nil, err
	}
	openAIRequest := dto.GeneralOpenAIRequest{
		Model:       claudeRequest.Model,
		Temperature: claudeRequest.Temperature,
		Metadata:    append([]byte(nil), claudeRequest.Metadata...),
	}
	if claudeRequest.MaxTokens != nil {
		if sharedchat.UsesMaxCompletionTokens(claudeRequest.Model) {
			openAIRequest.MaxCompletionTokens = kitutil.GetPointer(*claudeRequest.MaxTokens)
		} else {
			openAIRequest.MaxTokens = kitutil.GetPointer(*claudeRequest.MaxTokens)
		}
	}
	if claudeRequest.TopP != nil {
		openAIRequest.TopP = kitutil.GetPointer(*claudeRequest.TopP)
	}
	if claudeRequest.Stream != nil {
		openAIRequest.Stream = kitutil.GetPointer(*claudeRequest.Stream)
		if *claudeRequest.Stream {
			openAIRequest.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
		}
	}

	options := convmeta.OptionsOf(info)
	isOpenRouter := options.OpenRouterDialect
	// cache_control is an Anthropic-only content field; strict OpenAI-compatible
	// vendors reject it. OpenRouter's anthropic/claude chat surface is the one
	// upstream that understands it, so only that dialect keeps the markers.
	isOpenRouterClaude := isOpenRouter && strings.HasPrefix(convmeta.UpstreamModelName(info), "anthropic/claude")
	preserveReasoningContent := options.PreserveChatReasoningContent
	if isOpenRouter {
		if effort := claudeRequest.GetEfforts(); effort != "" {
			effortBytes, _ := kitutil.Marshal(effort)
			openAIRequest.Verbosity = effortBytes
		}
		if claudeRequest.Thinking != nil {
			var reasoningConfig openRouterRequestReasoning
			if claudeRequest.Thinking.Type == "enabled" {
				reasoningConfig = openRouterRequestReasoning{
					Enabled:   true,
					MaxTokens: claudeRequest.Thinking.GetBudgetTokens(),
				}
			} else if claudeRequest.Thinking.Type == "adaptive" {
				reasoningConfig = openRouterRequestReasoning{
					Enabled: true,
				}
			}
			reasoningJSON, err := kitutil.Marshal(reasoningConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal reasoning: %w", err)
			}
			openAIRequest.Reasoning = reasoningJSON
		}
	} else {
		effort := claudeRequestReasoningEffort(&claudeRequest)
		if effort == "" && claudeRequest.Thinking != nil && claudeRequest.Thinking.Type == "disabled" {
			// Thinking-by-default models (Kimi/GLM/DeepSeek) need the explicit
			// disable; an absent thinking block stays a no-op.
			effort = "none"
		}
		sharedchat.ApplyReasoningEffort(&openAIRequest, effort)
		if info != nil {
			thinkingSuffix := "-thinking"
			if strings.HasSuffix(convmeta.UpstreamModelName(info), thinkingSuffix) &&
				!strings.HasSuffix(openAIRequest.Model, thinkingSuffix) {
				openAIRequest.Model = openAIRequest.Model + thinkingSuffix
			}
		}
	}

	if len(claudeRequest.StopSequences) == 1 {
		openAIRequest.Stop = claudeRequest.StopSequences[0]
	} else if len(claudeRequest.StopSequences) > 1 {
		openAIRequest.Stop = claudeRequest.StopSequences
	}
	responseTools, declaredTools, err := claudeToolsToResponses(claudeRequest.Tools)
	if err != nil {
		return nil, err
	}
	openAITools := make([]dto.ToolCallRequest, 0, len(responseTools))
	for _, tool := range responseTools {
		var strict *bool
		if value, ok := tool["strict"].(bool); ok {
			strict = kitutil.GetPointer(value)
		}
		openAITools = append(openAITools, dto.ToolCallRequest{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        kitutil.Interface2String(tool["name"]),
				Description: kitutil.Interface2String(tool["description"]),
				Parameters:  tool["parameters"],
				Strict:      strict,
			},
		})
	}
	openAIRequest.Tools = openAITools

	if claudeRequest.ToolChoice != nil {
		var toolChoice dto.ClaudeToolChoice
		if value, ok := claudeRequest.ToolChoice.(string); ok {
			toolChoice.Type = value
		} else {
			converted, err := kitutil.Any2Type[dto.ClaudeToolChoice](claudeRequest.ToolChoice)
			if err != nil {
				return nil, fmt.Errorf("invalid Claude tool_choice: %w", err)
			}
			toolChoice = converted
		}

		switch toolChoice.Type {
		case "auto":
			openAIRequest.ToolChoice = "auto"
		case "any":
			openAIRequest.ToolChoice = "required"
		case "none":
			openAIRequest.ToolChoice = "none"
		case "tool":
			toolName := strings.TrimSpace(toolChoice.Name)
			if toolName == "" {
				return nil, fmt.Errorf("Claude tool_choice type tool requires a name")
			}
			if len(declaredTools) > 0 {
				if _, exists := declaredTools[toolName]; !exists {
					return nil, fmt.Errorf("Claude tool_choice references undeclared tool %q", toolName)
				}
			}
			openAIRequest.ToolChoice = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": toolName,
				},
			}
		default:
			return nil, fmt.Errorf("unsupported Claude tool_choice type %q", toolChoice.Type)
		}
		if toolChoice.Type != "none" {
			openAIRequest.ParallelTooCalls = kitutil.GetPointer(!toolChoice.DisableParallelToolUse)
		}
	}
	if len(openAITools) == 0 {
		openAIRequest.ToolChoice = nil
		openAIRequest.ParallelTooCalls = nil
	}

	openAIMessages := make([]dto.Message, 0)
	if claudeRequest.System != nil {
		if claudeRequest.IsStringSystem() && claudeRequest.GetStringSystem() != "" {
			systemText := sharedclaude.StripLeadingBillingHeader(claudeRequest.GetStringSystem())
			if systemText != "" {
				openAIMessage := dto.Message{Role: "system"}
				openAIMessage.SetStringContent(systemText)
				openAIMessages = append(openAIMessages, openAIMessage)
			}
		} else {
			systems := claudeRequest.ParseSystem()
			if len(systems) > 0 {
				for index, system := range systems {
					if system.Type != "text" {
						return nil, fmt.Errorf("Claude system content %d type %q cannot be converted to Chat Completions", index, system.Type)
					}
				}
				if isOpenRouterClaude {
					systemMediaMessages := make([]dto.MediaContent, 0, len(systems))
					for _, system := range systems {
						text := sharedclaude.StripLeadingBillingHeader(system.GetText())
						if text == "" {
							continue
						}
						message := dto.MediaContent{
							Type:         "text",
							Text:         text,
							CacheControl: system.CacheControl,
						}
						systemMediaMessages = append(systemMediaMessages, message)
					}
					if len(systemMediaMessages) > 0 {
						openAIMessage := dto.Message{Role: "system"}
						openAIMessage.SetMediaContent(systemMediaMessages)
						openAIMessages = append(openAIMessages, openAIMessage)
					}
				} else {
					systemParts := make([]string, 0, len(systems))
					for _, system := range systems {
						text := sharedclaude.StripLeadingBillingHeader(system.GetText())
						if strings.TrimSpace(text) != "" {
							systemParts = append(systemParts, text)
						}
					}
					if len(systemParts) > 0 {
						openAIMessage := dto.Message{Role: "system"}
						openAIMessage.SetStringContent(strings.Join(systemParts, "\n\n"))
						openAIMessages = append(openAIMessages, openAIMessage)
					}
				}
			}
		}
	}

	for messageIndex, claudeMessage := range claudeRequest.Messages {
		openAIMessage := dto.Message{
			Role: claudeMessage.Role,
		}
		if claudeMessage.IsStringContent() {
			openAIMessage.SetStringContent(claudeMessage.GetStringContent())
		} else {
			content, err := claudeMessage.ParseContent()
			if err != nil {
				return nil, err
			}
			var toolCalls []dto.ToolCallRequest
			mediaMessages := make([]dto.MediaContent, 0, len(content))
			pendingToolMedia := make([]dto.MediaContent, 0)
			reasoningParts := make([]string, 0)

			for contentIndex, mediaMsg := range content {
				switch mediaMsg.Type {
				case "text", "input_text":
					message := dto.MediaContent{
						Type: "text",
						Text: mediaMsg.GetText(),
					}
					if isOpenRouterClaude {
						message.CacheControl = mediaMsg.CacheControl
					}
					mediaMessages = append(mediaMessages, message)
				case "image", "document":
					mediaMessage, err := claudeMediaToChatContent(c, mediaMsg)
					if err != nil {
						return nil, fmt.Errorf("Claude message %d %s content %d: %w", messageIndex, mediaMsg.Type, contentIndex, err)
					}
					mediaMessages = append(mediaMessages, mediaMessage)
				case "thinking":
					if preserveReasoningContent && mediaMsg.Thinking != nil && strings.TrimSpace(*mediaMsg.Thinking) != "" {
						reasoningParts = append(reasoningParts, *mediaMsg.Thinking)
					}
				case "redacted_thinking":
					if preserveReasoningContent {
						reasoningParts = append(reasoningParts, "[redacted thinking]")
					}
				case "tool_use":
					toolCall := dto.ToolCallRequest{
						ID:   mediaMsg.Id,
						Type: "function",
						Function: dto.FunctionRequest{
							Name:      mediaMsg.Name,
							Arguments: requestToJSONString(mediaMsg.Input),
						},
					}
					toolCalls = append(toolCalls, toolCall)
				case "tool_result":
					toolName := mediaMsg.Name
					if toolName == "" {
						toolName = claudeRequest.SearchToolNameByToolCallId(mediaMsg.ToolUseId)
					}
					oaiToolMessage := dto.Message{
						Role:       "tool",
						Name:       &toolName,
						ToolCallId: mediaMsg.ToolUseId,
					}
					toolText, toolMedia, err := claudeToolResultToChatContent(mediaMsg, toolMediaScope)
					if err != nil {
						return nil, fmt.Errorf("Claude message %d tool_result content %d: %w", messageIndex, contentIndex, err)
					}
					oaiToolMessage.SetStringContent(toolText)
					openAIMessages = append(openAIMessages, oaiToolMessage)
					if len(toolMedia) > 0 {
						pendingToolMedia = append(pendingToolMedia, dto.MediaContent{
							Type: "text",
							Text: fmt.Sprintf("[new-api: media output of tool call %s]", mediaMsg.ToolUseId),
						})
						pendingToolMedia = append(pendingToolMedia, toolMedia...)
					}
				}
			}
			if len(pendingToolMedia) > 0 {
				mediaMessage := dto.Message{Role: "user"}
				mediaMessage.SetMediaContent(pendingToolMedia)
				openAIMessages = append(openAIMessages, mediaMessage)
			}

			if len(toolCalls) > 0 {
				openAIMessage.SetToolCalls(toolCalls)
			}
			if len(mediaMessages) > 0 {
				openAIMessage.SetMediaContent(mediaMessages)
			}
			if preserveReasoningContent && claudeMessage.Role == "assistant" && len(toolCalls) > 0 {
				reasoningContent := "tool call"
				if len(reasoningParts) > 0 {
					reasoningContent = strings.Join(reasoningParts, "\n")
				}
				openAIMessage.ReasoningContent = &reasoningContent
			}
		}
		if len(openAIMessage.ParseContent()) > 0 || len(openAIMessage.ToolCalls) > 0 {
			openAIMessages = append(openAIMessages, openAIMessage)
		}
	}

	openAIRequest.Messages = openAIMessages
	return &openAIRequest, nil
}

func requestToJSONString(v interface{}) string {
	b, err := kitutil.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func claudeMediaToChatContent(c context.Context, media dto.ClaudeMediaMessage) (dto.MediaContent, error) {
	if media.Source == nil {
		return dto.MediaContent{}, fmt.Errorf("%s source is missing", media.Type)
	}
	var data string
	switch strings.TrimSpace(media.Source.Type) {
	case "base64":
		mediaType := strings.TrimSpace(media.Source.MediaType)
		payload := strings.TrimSpace(kitutil.Interface2String(media.Source.Data))
		if mediaType == "" || payload == "" {
			return dto.MediaContent{}, fmt.Errorf("base64 source is incomplete")
		}
		data = fmt.Sprintf("data:%s;base64,%s", mediaType, payload)
	case "url":
		data = strings.TrimSpace(media.Source.Url)
		if data == "" {
			return dto.MediaContent{}, fmt.Errorf("URL source is empty")
		}
	default:
		return dto.MediaContent{}, fmt.Errorf("unsupported source type %q", media.Source.Type)
	}
	if media.Type == "image" {
		return dto.MediaContent{
			Type:     dto.ContentTypeImageURL,
			ImageUrl: &dto.MessageImageUrl{Url: data},
		}, nil
	}
	filename := strings.TrimSpace(media.Title)
	if filename == "" {
		filename = strings.TrimSpace(media.Filename)
	}
	if filename == "" {
		filename = "document.pdf"
	}
	if strings.TrimSpace(media.Source.Type) == "url" {
		base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, media.ToFileSource(), "formatting Claude document for Chat Completions")
		if err != nil {
			return dto.MediaContent{}, fmt.Errorf("get document data failed: %w", err)
		}
		if strings.TrimSpace(mimeType) == "" {
			mimeType = "application/pdf"
		}
		data = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
	}
	return dto.MediaContent{
		Type: dto.ContentTypeFile,
		File: &dto.MessageFile{
			FileName: filename,
			FileData: data,
		},
	}, nil
}

func claudeToolResultToChatContent(result dto.ClaudeMediaMessage, scope sharedtoolmedia.Scope) (string, []dto.MediaContent, error) {
	prefix := ""
	if result.IsError != nil && *result.IsError {
		prefix = sharedbridge.ClaudeToolResultErrorMarker
	}
	if result.Content == nil {
		return prefix, nil, nil
	}
	plan, err := sharedtoolmedia.PlanChatToolOutputWithScope(result.Content, scope)
	if err != nil {
		return "", nil, err
	}
	if plan != nil {
		content := plan.Content
		if prefix != "" && content != "" {
			content = prefix + "\n" + content
		} else if prefix != "" {
			content = prefix
		}
		return content, plan.Media, nil
	}

	content := ""
	if result.IsStringContent() {
		content = result.GetStringContent()
	} else {
		encoded, err := kitutil.Marshal(result.Content)
		if err != nil {
			return "", nil, err
		}
		content = string(encoded)
	}
	if prefix != "" && content != "" {
		content = prefix + "\n" + content
	} else if prefix != "" {
		content = prefix
	}
	return content, nil, nil
}
