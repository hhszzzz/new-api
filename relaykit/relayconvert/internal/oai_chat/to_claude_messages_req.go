package oaichat

import (
	"fmt"
	"strings"

	"context"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/toolconv"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func OpenAIChatRequestToClaudeMessages(c context.Context, info convmeta.Meta, textRequest dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	opts := convmeta.OptionsOf(info)

	claudeRequest := dto.ClaudeRequest{
		Model:         textRequest.Model,
		StopSequences: nil,
		Temperature:   textRequest.Temperature,
	}
	if textRequest.MaxCompletionTokens != nil {
		claudeRequest.MaxTokens = kitutil.GetPointer(*textRequest.MaxCompletionTokens)
	} else if textRequest.MaxTokens != nil {
		claudeRequest.MaxTokens = kitutil.GetPointer(*textRequest.MaxTokens)
	}
	if textRequest.TopP != nil {
		claudeRequest.TopP = kitutil.GetPointer(*textRequest.TopP)
	}
	if textRequest.TopK != nil {
		claudeRequest.TopK = kitutil.GetPointer(*textRequest.TopK)
	}
	if textRequest.IsStream(nil) {
		claudeRequest.Stream = kitutil.GetPointer(true)
	}

	sourceReasoning, diagnostics, err := reasoning.FromOpenAIChat(&textRequest)
	if err != nil {
		return nil, reasoning.AsClientError(err)
	}
	convdiag.Add(c, diagnostics...)
	if err := sharedclaude.ApplyReasoning(c, &claudeRequest, info, sourceReasoning, true); err != nil {
		return nil, reasoning.AsClientError(err)
	}
	if err := sharedclaude.ApplyOutputFormat(&claudeRequest, textRequest.ResponseFormat); err != nil {
		return nil, err
	}
	if len(textRequest.ServiceTier) > 0 {
		var tier string
		if err := kitutil.Unmarshal(textRequest.ServiceTier, &tier); err != nil {
			return nil, err
		}
		switch tier {
		case "auto":
			claudeRequest.ServiceTier = "auto"
		case "default":
			claudeRequest.ServiceTier = "standard_only"
		default:
			return nil, fmt.Errorf("service_tier %q has no Messages mapping", tier)
		}
	}
	if claudeRequest.MaxTokens == nil {
		if defaultMaxTokens, configured := opts.Claude.DefaultMaxTokensFor(claudeRequest.Model); configured {
			value := uint(defaultMaxTokens)
			claudeRequest.MaxTokens = &value
		}
	}

	if textRequest.Stop != nil {
		switch stop := textRequest.Stop.(type) {
		case string:
			claudeRequest.StopSequences = []string{stop}
		case []string:
			claudeRequest.StopSequences = append([]string(nil), stop...)
		case []any:
			stopSequences := make([]string, 0)
			for _, item := range stop {
				value, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("stop sequences must be strings")
				}
				stopSequences = append(stopSequences, value)
			}
			claudeRequest.StopSequences = stopSequences
		}
	}

	normalizedMessages := append([]dto.Message(nil), textRequest.Messages...)
	pendingLegacyCallID := ""
	for index := range normalizedMessages {
		role := strings.TrimSpace(normalizedMessages[index].Role)
		if role == "" {
			role = "user"
			normalizedMessages[index].Role = role
		}
		switch role {
		case "assistant":
			if normalizedMessages[index].FunctionCall != nil {
				toolCalls := normalizedMessages[index].ParseToolCalls()
				if len(toolCalls) > 0 {
					pendingLegacyCallID = toolCalls[0].ID
				}
			}
		case "function":
			normalizedMessages[index].Role = "tool"
			if strings.TrimSpace(normalizedMessages[index].ToolCallId) == "" {
				normalizedMessages[index].ToolCallId = pendingLegacyCallID
			}
			pendingLegacyCallID = ""
		case "tool":
			pendingLegacyCallID = ""
		default:
			pendingLegacyCallID = ""
		}
	}

	formatMessages := make([]dto.Message, 0)
	lastMessage := dto.Message{
		Role: "tool",
	}
	for _, message := range normalizedMessages {
		if message.Role == "assistant" && message.GetReasoningContent() != "" &&
			(message.Content == nil || (message.IsStringContent() && message.StringContent() == "")) &&
			len(message.ParseToolCalls()) == 0 {
			continue
		}
		switch message.Role {
		case "":
			message.Role = "user"
		case "developer":
			message.Role = "system"
		case "function":
			if message.ToolCallId != "" {
				message.Role = "tool"
			} else {
				return nil, fmt.Errorf("tool message is missing tool_call_id and cannot be matched to a tool use")
			}
		case "tool":
			if message.ToolCallId == "" {
				return nil, fmt.Errorf("tool message is missing tool_call_id and cannot be matched to a tool use")
			}
		case "system", "user", "assistant":
		default:
			message.Role = "user"
		}
		fmtMessage := dto.Message{
			Role:    message.Role,
			Content: message.Content,
		}
		if message.Role == "tool" {
			fmtMessage.ToolCallId = message.ToolCallId
		}
		if message.Role == "assistant" {
			if toolCalls := message.ParseToolCalls(); len(toolCalls) > 0 {
				fmtMessage.SetToolCalls(toolCalls)
			}
		}
		if lastMessage.Role == message.Role && lastMessage.Role != "tool" {
			if lastMessage.IsStringContent() && message.IsStringContent() {
				fmtMessage.SetStringContent(fmt.Sprintf("%s %s", lastMessage.StringContent(), message.StringContent()))
				formatMessages = formatMessages[:len(formatMessages)-1]
			}
		}
		if len(fmtMessage.ParseToolCalls()) == 0 &&
			(fmtMessage.Content == nil || (fmtMessage.IsStringContent() && fmtMessage.StringContent() == "")) {
			fmtMessage.SetStringContent("...")
		}
		formatMessages = append(formatMessages, fmtMessage)
		lastMessage = fmtMessage
	}

	claudeMessages := make([]dto.ClaudeMessage, 0)
	isFirstMessage := true
	var systemMessages []dto.ClaudeMediaMessage
	placeholderUserMessage := dto.ClaudeMessage{
		Role: "user",
		Content: []dto.ClaudeMediaMessage{
			{
				Type: "text",
				Text: kitutil.GetPointer[string]("..."),
			},
		},
	}

	for _, message := range formatMessages {
		if message.Role == "system" {
			if message.IsStringContent() {
				if text := message.StringContent(); text != "" {
					systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
						Type: "text",
						Text: kitutil.GetPointer[string](text),
					})
				}
			} else {
				for _, ctx := range message.ParseContent() {
					if ctx.Type == "text" && ctx.Text != "" {
						systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
							Type: "text",
							Text: kitutil.GetPointer[string](ctx.Text),
						})
					}
				}
			}
			continue
		}

		if isFirstMessage {
			isFirstMessage = false
			if message.Role != "user" {
				claudeMessages = append(claudeMessages, placeholderUserMessage)
			}
		}

		claudeMessage := dto.ClaudeMessage{
			Role: message.Role,
		}
		if message.Role == "tool" {
			if strings.TrimSpace(message.ToolCallId) == "" {
				return nil, fmt.Errorf("tool message is missing tool_call_id and cannot be matched to a tool use")
			}
			if len(claudeMessages) > 0 && claudeMessages[len(claudeMessages)-1].Role == "user" {
				lastClaudeMessage := claudeMessages[len(claudeMessages)-1]
				if content, ok := lastClaudeMessage.Content.(string); ok {
					lastClaudeMessage.Content = []dto.ClaudeMediaMessage{
						{
							Type: "text",
							Text: kitutil.GetPointer[string](content),
						},
					}
				}
				lastClaudeMessage.Content = append(lastClaudeMessage.Content.([]dto.ClaudeMediaMessage), dto.ClaudeMediaMessage{
					Type:      "tool_result",
					ToolUseId: message.ToolCallId,
					Content:   message.Content,
				})
				claudeMessages[len(claudeMessages)-1] = lastClaudeMessage
				continue
			}

			claudeMessage.Role = "user"
			claudeMessage.Content = []dto.ClaudeMediaMessage{
				{
					Type:      "tool_result",
					ToolUseId: message.ToolCallId,
					Content:   message.Content,
				},
			}
		} else if message.IsStringContent() && len(message.ParseToolCalls()) == 0 {
			text := message.StringContent()
			if text == "" {
				text = "..."
			}
			claudeMessage.Content = text
		} else {
			claudeMediaMessages := make([]dto.ClaudeMediaMessage, 0)
			for _, mediaMessage := range message.ParseContent() {
				switch mediaMessage.Type {
				case "text":
					if mediaMessage.Text != "" {
						claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
							Type: "text",
							Text: kitutil.GetPointer[string](mediaMessage.Text),
						})
					}
				default:
					source := mediaMessage.ToFileSource()
					if source == nil {
						return nil, fmt.Errorf("content type %q is missing supported media data", mediaMessage.Type)
					}
					base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting image for Claude")
					if err != nil {
						return nil, fmt.Errorf("get file data failed: %s", err.Error())
					}
					claudeMediaMessage := dto.ClaudeMediaMessage{
						Source: &dto.ClaudeMessageSource{
							Type: "base64",
						},
					}
					if strings.HasPrefix(mimeType, "application/pdf") {
						claudeMediaMessage.Type = "document"
					} else if strings.HasPrefix(mimeType, "image/") {
						claudeMediaMessage.Type = "image"
					} else {
						return nil, fmt.Errorf("media type %q cannot be represented as a Messages image or document", mimeType)
					}

					claudeMediaMessage.Source.MediaType = mimeType
					claudeMediaMessage.Source.Data = base64Data
					claudeMediaMessages = append(claudeMediaMessages, claudeMediaMessage)
					continue
				}
			}

			if toolCalls := message.ParseToolCalls(); len(toolCalls) > 0 {
				for _, toolCall := range toolCalls {
					inputObj := make(map[string]any)
					if args := toolCall.Function.Arguments; args != "" {
						if err := kitutil.Unmarshal([]byte(args), &inputObj); err != nil {
							return nil, fmt.Errorf("tool call %q arguments must be a JSON object", toolCall.ID)
						}
					}
					claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
						Type:  "tool_use",
						Id:    toolCall.ID,
						Name:  toolCall.Function.Name,
						Input: inputObj,
					})
				}
			}
			claudeMessage.Content = claudeMediaMessages
		}
		claudeMessages = append(claudeMessages, claudeMessage)
	}
	if len(claudeMessages) == 0 && len(systemMessages) > 0 {
		claudeMessages = append(claudeMessages, placeholderUserMessage)
	}

	if len(systemMessages) > 0 {
		claudeRequest.System = systemMessages
	}

	claudeRequest.Prompt = ""
	claudeRequest.Messages = claudeMessages
	// Checked last so every injection path (default hook, thinking adapter
	// floor) has had its chance to satisfy the required field.
	if claudeRequest.MaxTokens == nil {
		return nil, sharedclaude.ErrMissingMaxTokens
	}
	if err := toolconv.RenderRequestTools(c, types.RelayFormatOpenAI, types.RelayFormatClaude, &textRequest, &claudeRequest, opts); err != nil {
		return nil, err
	}
	return &claudeRequest, nil
}
