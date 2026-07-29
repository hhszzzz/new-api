package openai

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type bufferedChatStreamChoice struct {
	Delta        dto.ChatCompletionsStreamResponseChoiceDelta `json:"delta"`
	Message      *dto.Message                                 `json:"message,omitempty"`
	FinishReason *string                                      `json:"finish_reason"`
	Index        int                                          `json:"index"`
}

type bufferedChatStreamChunk struct {
	ID                string                     `json:"id"`
	Object            string                     `json:"object"`
	Created           int64                      `json:"created"`
	Model             string                     `json:"model"`
	SystemFingerprint *string                    `json:"system_fingerprint"`
	Choices           []bufferedChatStreamChoice `json:"choices"`
	Usage             *dto.Usage                 `json:"usage"`
	Error             any                        `json:"error,omitempty"`
}

type bufferedChatChoiceState struct {
	role           string
	content        strings.Builder
	reasoning      strings.Builder
	refusal        strings.Builder
	tools          map[int]*dto.ToolCallResponse
	finishReason   string
	messageContent any
}

// OaiChatBufferedStreamHandler aggregates an unexpectedly streamed Chat
// Completions response and then reuses the normal non-stream response path. It
// keeps a stream:false Responses or Messages client on its requested JSON
// contract even when a compatible upstream forces SSE.
func OaiChatBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	choices := make(map[int]*bufferedChatChoiceState)
	responseID := ""
	created := int64(0)
	model := ""
	var usage *dto.Usage

	err := helper.ScanJSONSSE(helper.BoundedStreamReader(resp.Body), func(data string) (bool, error) {
		if data == "[DONE]" {
			return true, nil
		}

		var chunk bufferedChatStreamChunk
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			return false, err
		}
		if chunk.Error != nil {
			errorResponse := dto.OpenAITextResponse{Error: chunk.Error}
			if openAIError := errorResponse.GetOpenAIError(); openAIError != nil && openAIError.Type != "" {
				streamError := types.WithOpenAIError(*openAIError, http.StatusInternalServerError)
				service.MarkProtocolUnsupportedStreamError(streamError)
				return false, streamError
			}
		}
		if responseID == "" && chunk.ID != "" {
			responseID = chunk.ID
		}
		if created == 0 && chunk.Created != 0 {
			created = chunk.Created
		}
		if model == "" && chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usageCopy := *chunk.Usage
			usage = &usageCopy
		}

		for _, choice := range chunk.Choices {
			state := choices[choice.Index]
			if state == nil {
				state = &bufferedChatChoiceState{tools: make(map[int]*dto.ToolCallResponse)}
				choices[choice.Index] = state
			}

			deltaEmpty := choice.Delta.Content == nil &&
				choice.Delta.ReasoningContent == nil &&
				len(choice.Delta.Reasoning) == 0 &&
				len(choice.Delta.ReasoningDetails) == 0 &&
				choice.Delta.Refusal == nil &&
				choice.Delta.Role == "" &&
				len(choice.Delta.ParseToolCalls()) == 0
			if choice.Message != nil && deltaEmpty {
				state.role = choice.Message.Role
				state.content.Reset()
				state.reasoning.Reset()
				state.refusal.Reset()
				state.tools = make(map[int]*dto.ToolCallResponse)
				state.messageContent = choice.Message.Content
				if text := choice.Message.GetReasoningContent(); text != "" {
					state.reasoning.WriteString(text)
				}
				if refusal := choice.Message.GetRefusalContent(); refusal != "" {
					state.refusal.WriteString(refusal)
				}
				for toolIndex, toolCall := range choice.Message.ParseToolCalls() {
					state.tools[toolIndex] = &dto.ToolCallResponse{
						ID:   toolCall.ID,
						Type: toolCall.Type,
						Function: dto.FunctionResponse{
							Name:      toolCall.Function.Name,
							Arguments: toolCall.Function.Arguments,
						},
					}
				}
			} else {
				if choice.Delta.Role != "" {
					state.role = choice.Delta.Role
				}
				if choice.Delta.Content != nil {
					state.messageContent = nil
					state.content.WriteString(*choice.Delta.Content)
				}
				if reasoning := choice.Delta.GetReasoningContent(); reasoning != "" {
					state.reasoning.WriteString(reasoning)
				}
				if refusal := choice.Delta.GetRefusalContent(); refusal != "" {
					state.refusal.WriteString(refusal)
				}
				for position, toolCall := range choice.Delta.ParseToolCalls() {
					toolIndex := position
					if toolCall.Index != nil && *toolCall.Index >= 0 {
						toolIndex = *toolCall.Index
					}
					current := state.tools[toolIndex]
					if current == nil {
						current = &dto.ToolCallResponse{}
						state.tools[toolIndex] = current
					}
					if toolCall.ID != "" {
						current.ID = toolCall.ID
					}
					if toolCall.Type != nil {
						current.Type = toolCall.Type
					}
					if toolCall.Function.Name != "" {
						current.Function.Name = toolCall.Function.Name
					}
					current.Function.Arguments += toolCall.Function.Arguments
				}
			}
			if choice.FinishReason != nil && state.finishReason == "" {
				state.finishReason = *choice.FinishReason
			}
		}
		return false, nil
	})
	if err != nil {
		if apiError, ok := err.(*types.NewAPIError); ok {
			return nil, apiError
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(choices) == 0 {
		return nil, types.NewOpenAIError(fmt.Errorf("Chat Completions stream ended without a response choice"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	choiceIndexes := make([]int, 0, len(choices))
	for index := range choices {
		choiceIndexes = append(choiceIndexes, index)
	}
	sort.Ints(choiceIndexes)
	responseChoices := make([]dto.OpenAITextResponseChoice, 0, len(choiceIndexes))
	for _, choiceIndex := range choiceIndexes {
		state := choices[choiceIndex]
		if state.finishReason == "" {
			return nil, types.NewOpenAIError(
				fmt.Errorf("Chat Completions stream ended without a terminal finish_reason"),
				types.ErrorCodeBadResponse,
				http.StatusInternalServerError,
			)
		}
		message := dto.Message{Role: state.role, Content: state.messageContent}
		if message.Role == "" {
			message.Role = "assistant"
		}
		if message.Content == nil && state.content.Len() > 0 {
			message.SetStringContent(state.content.String())
		}
		if state.reasoning.Len() > 0 {
			reasoning := state.reasoning.String()
			message.ReasoningContent = &reasoning
		}
		if state.refusal.Len() > 0 {
			refusal := state.refusal.String()
			message.Refusal = &refusal
		}
		if len(state.tools) > 0 {
			toolIndexes := make([]int, 0, len(state.tools))
			for index := range state.tools {
				toolIndexes = append(toolIndexes, index)
			}
			sort.Ints(toolIndexes)
			toolCalls := make([]dto.ToolCallResponse, 0, len(toolIndexes))
			for _, toolIndex := range toolIndexes {
				toolCall := *state.tools[toolIndex]
				toolCall.Index = nil
				if toolCall.ID == "" {
					toolCall.ID = fmt.Sprintf("call_%d_%d", choiceIndex, toolIndex)
				}
				if toolCall.Type == nil {
					toolCall.Type = "function"
				}
				if toolCall.Function.Name == "" {
					return nil, types.NewOpenAIError(
						fmt.Errorf("Chat Completions tool call %d is missing a function name", toolIndex),
						types.ErrorCodeBadResponseBody,
						http.StatusInternalServerError,
					)
				}
				toolCalls = append(toolCalls, toolCall)
			}
			encodedToolCalls, err := common.Marshal(toolCalls)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			}
			message.ToolCalls = encodedToolCalls
		}
		responseChoices = append(responseChoices, dto.OpenAITextResponseChoice{
			Index:        choiceIndex,
			Message:      message,
			FinishReason: state.finishReason,
		})
	}

	if responseID == "" {
		responseID = helper.GetResponseID(c)
	}
	if responseID == "" {
		responseID = "chatcmpl-buffered"
	}
	chatResponse := dto.OpenAITextResponse{
		Id:      responseID,
		Model:   model,
		Object:  "chat.completion",
		Created: created,
		Choices: responseChoices,
	}
	if usage != nil {
		chatResponse.Usage = *usage
	}
	encodedResponse, err := common.Marshal(chatResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	header := resp.Header.Clone()
	header.Set("Content-Type", "application/json")
	bufferedResponse := &http.Response{
		StatusCode: resp.StatusCode,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(encodedResponse))),
	}
	if info.RelayFormat == types.RelayFormatOpenAIResponses {
		return OaiChatToResponsesHandler(c, info, bufferedResponse)
	}
	return OpenaiHandler(c, info, bufferedResponse)
}
