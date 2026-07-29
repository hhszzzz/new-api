package geminichat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/jsonutil"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func GeminiGenerateContentRequestToOpenAIChat(geminiRequest *dto.GeminiChatRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	modelName := ""
	isStream := false
	if info != nil {
		isStream = info.GetIsStream()
	}
	modelName = convmeta.UpstreamModelName(info)
	openaiRequest := &dto.GeneralOpenAIRequest{
		Model:  modelName,
		Stream: kitutil.GetPointer(isStream),
	}

	var messages []dto.Message
	callNames := make(map[string]string)
	pendingCallIDs := make([]string, 0)
	pendingCallIDsByName := make(map[string][]string)
	pendingCallIDSet := make(map[string]struct{})
	consumePendingCallID := func(explicitID string, name string) string {
		if explicitID != "" {
			if _, exists := pendingCallIDSet[explicitID]; !exists {
				return ""
			}
			delete(pendingCallIDSet, explicitID)
			return explicitID
		}
		if name != "" {
			ids := pendingCallIDsByName[name]
			for len(ids) > 0 {
				callID := ids[0]
				ids = ids[1:]
				pendingCallIDsByName[name] = ids
				if _, exists := pendingCallIDSet[callID]; exists {
					delete(pendingCallIDSet, callID)
					return callID
				}
			}
			return ""
		}
		for len(pendingCallIDs) > 0 {
			callID := pendingCallIDs[0]
			pendingCallIDs = pendingCallIDs[1:]
			if _, exists := pendingCallIDSet[callID]; exists {
				delete(pendingCallIDSet, callID)
				return callID
			}
		}
		return ""
	}
	fallbackCallIndex := 0
	for _, content := range geminiRequest.Contents {
		message := dto.Message{
			Role: convertGeminiRoleToOpenAI(content.Role),
		}

		var mediaContents []dto.MediaContent
		var toolCalls []dto.ToolCallRequest
		for _, part := range content.Parts {
			if part.Text != "" {
				mediaContent := dto.MediaContent{
					Type: "text",
					Text: part.Text,
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.InlineData != nil {
				mediaContent := dto.MediaContent{
					Type: "image_url",
					ImageUrl: &dto.MessageImageUrl{
						Url:      fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data),
						Detail:   "auto",
						MimeType: part.InlineData.MimeType,
					},
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.FileData != nil {
				mediaContent := dto.MediaContent{
					Type: "image_url",
					ImageUrl: &dto.MessageImageUrl{
						Url:      part.FileData.FileUri,
						Detail:   "auto",
						MimeType: part.FileData.MimeType,
					},
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.FunctionCall != nil {
				callID := strings.TrimSpace(part.FunctionCall.ID)
				if callID == "" {
					for {
						fallbackCallIndex++
						callID = fmt.Sprintf("call_%d", fallbackCallIndex)
						if _, exists := callNames[callID]; !exists {
							break
						}
					}
				}
				if _, exists := callNames[callID]; exists {
					return nil, fmt.Errorf("duplicate Gemini functionCall id %q", callID)
				}
				functionName := strings.TrimSpace(part.FunctionCall.FunctionName)
				toolCall := dto.ToolCallRequest{
					ID:   callID,
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      functionName,
						Arguments: jsonutil.ToJSONString(part.FunctionCall.Arguments),
					},
				}
				toolCalls = append(toolCalls, toolCall)
				callNames[callID] = functionName
				pendingCallIDs = append(pendingCallIDs, callID)
				pendingCallIDsByName[functionName] = append(pendingCallIDsByName[functionName], callID)
				pendingCallIDSet[callID] = struct{}{}
			} else if part.FunctionResponse != nil {
				callID := strings.TrimSpace(kitutil.JsonRawMessageToString(part.FunctionResponse.ID))
				functionName := strings.TrimSpace(part.FunctionResponse.Name)
				callID = consumePendingCallID(callID, functionName)
				if callID == "" {
					return nil, fmt.Errorf("unable to resolve OpenAI tool_call_id for Gemini functionResponse %q", functionName)
				}
				toolMessage := dto.Message{
					Role:       "tool",
					ToolCallId: callID,
				}
				if functionName == "" {
					functionName = callNames[callID]
				}
				if functionName != "" {
					toolMessage.Name = &functionName
				}
				toolMessage.SetStringContent(jsonutil.ToJSONString(part.FunctionResponse.Response))
				messages = append(messages, toolMessage)
			}
		}

		if len(toolCalls) > 0 {
			message.SetToolCalls(toolCalls)
		} else if len(mediaContents) == 1 && mediaContents[0].Type == "text" {
			message.Content = mediaContents[0].Text
		} else if len(mediaContents) > 0 {
			message.SetMediaContent(mediaContents)
		}

		if len(message.ParseContent()) > 0 || len(message.ToolCalls) > 0 {
			messages = append(messages, message)
		}
	}

	openaiRequest.Messages = messages

	if geminiRequest.GenerationConfig.Temperature != nil {
		openaiRequest.Temperature = geminiRequest.GenerationConfig.Temperature
	}
	if geminiRequest.GenerationConfig.TopP != nil {
		openaiRequest.TopP = kitutil.GetPointer(*geminiRequest.GenerationConfig.TopP)
	}
	// OpenAI Chat Completions has no standard top_k field. CC Switch follows the
	// same strict-compatible rule for Anthropic conversion and omits it rather
	// than sending an extension that many Chat-only upstreams reject.
	if geminiRequest.GenerationConfig.MaxOutputTokens != nil {
		openaiRequest.MaxTokens = kitutil.GetPointer(*geminiRequest.GenerationConfig.MaxOutputTokens)
	}
	if len(geminiRequest.GenerationConfig.StopSequences) > 0 {
		openaiRequest.Stop = geminiRequest.GenerationConfig.StopSequences[:min(len(geminiRequest.GenerationConfig.StopSequences), 4)]
	}
	if geminiRequest.GenerationConfig.CandidateCount != nil {
		openaiRequest.N = kitutil.GetPointer(*geminiRequest.GenerationConfig.CandidateCount)
	}

	if len(geminiRequest.GetTools()) > 0 {
		var tools []dto.ToolCallRequest
		for _, tool := range geminiRequest.GetTools() {
			if tool.FunctionDeclarations == nil {
				continue
			}
			functionDeclarations, err := kitutil.Any2Type[[]dto.FunctionRequest](tool.FunctionDeclarations)
			if err != nil {
				kitutil.LogSystemError(fmt.Sprintf("failed to parse gemini function declarations: %v (type=%T)", err, tool.FunctionDeclarations))
				continue
			}
			for _, function := range functionDeclarations {
				parameters := function.Parameters
				if function.ParametersJsonSchema != nil {
					parameters = function.ParametersJsonSchema
				}
				openAITool := dto.ToolCallRequest{
					Type: "function",
					Function: dto.FunctionRequest{
						Name:        function.Name,
						Description: function.Description,
						Parameters:  parameters,
					},
				}
				tools = append(tools, openAITool)
			}
		}
		if len(tools) > 0 {
			openaiRequest.Tools = tools
		}
	}

	if geminiRequest.SystemInstructions != nil {
		systemMessage := dto.Message{
			Role:    "system",
			Content: extractTextFromGeminiParts(geminiRequest.SystemInstructions.Parts),
		}
		openaiRequest.Messages = append([]dto.Message{systemMessage}, openaiRequest.Messages...)
	}

	return openaiRequest, nil
}

func convertGeminiRoleToOpenAI(geminiRole string) string {
	switch geminiRole {
	case "user":
		return "user"
	case "model":
		return "assistant"
	case "function":
		return "function"
	default:
		return "user"
	}
}

func extractTextFromGeminiParts(parts []dto.GeminiPart) string {
	texts := make([]string, 0)
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}
