package openai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"

	"github.com/gin-gonic/gin"
)

func OaiResponsesToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var responsesResp dto.OpenAIResponsesResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if err := common.Unmarshal(body, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	var responseFields map[string]json.RawMessage
	_ = common.Unmarshal(body, &responseFields)

	if oaiError := responsesResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	if err := protocolstate.ValidateResponsesContinuation(c, responsesResp.PreviousResponseID); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	chatResult, err := relayconvert.ConvertResponse(c, info, types.RelayFormatOpenAI, &responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	chatResp, ok := chatResult.Value.(*dto.OpenAITextResponse)
	if !ok {
		return nil, types.NewOpenAIError(fmt.Errorf("expected OpenAI chat response, got %T", chatResult.Value), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if chatID := helper.GetResponseID(c); chatID != "" {
		chatResp.Id = chatID
	}
	if info.RelayFormat == types.RelayFormatClaude {
		chatResp.Model = info.PublicResponseModelName()
	}
	usage := chatResult.Usage

	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(&responsesResp)
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		chatResp.Usage = *usage
	}

	responseValue := any(chatResp)
	if info.RelayFormat != types.RelayFormatOpenAI {
		targetResult, err := relayconvert.ConvertResponse(c, info, info.RelayFormat, chatResp)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		responseValue = targetResult.Value
	}
	if claudeResponse, ok := responseValue.(*dto.ClaudeResponse); ok {
		protocolstate.CaptureMessagesResponseData(c, &responsesResp, responseFields["output"], claudeResponse)
	}
	responseBody, err := common.Marshal(responseValue)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	resp.Header = resp.Header.Clone()
	resp.Header.Set("Content-Type", "application/json")
	if err := service.IOCopyBytesGracefully(c, resp, responseBody); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	return usage, nil
}

func OaiResponsesToChatBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	accumulator := relayconvert.NewResponsesBufferedAccumulator()
	var finalResponse *dto.OpenAIResponsesResponse
	var streamErr *types.NewAPIError

	scanner := helper.NewStreamScanner(helper.BoundedStreamReader(resp.Body))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 6 || line[:5] != "data:" {
			continue
		}
		data := line[5:]
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, "failed to unmarshal buffered responses stream event: "+err.Error())
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			break
		}
		if streamResp.Response != nil {
			if err := protocolstate.ValidateResponsesContinuation(c, streamResp.Response.PreviousResponseID); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				break
			}
		}
		if _, observeErr := protocolstate.ObserveResponsesStreamData(c, &streamResp, []byte(data)); observeErr != nil {
			streamErr = types.NewOpenAIError(observeErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			break
		}
		accumulator.ProcessEvent(&streamResp)
		switch streamResp.Type {
		case "response.completed", "response.done", "response.incomplete", "response.cancelled", "response.canceled":
			finalResponse = streamResp.Response
			if streamResp.Type == "response.incomplete" || streamResp.Type == "response.cancelled" || streamResp.Type == "response.canceled" {
				if finalResponse == nil {
					finalResponse = &dto.OpenAIResponsesResponse{}
				}
				if len(finalResponse.Status) == 0 {
					status := "incomplete"
					if streamResp.Type == "response.cancelled" || streamResp.Type == "response.canceled" {
						status = "cancelled"
					}
					finalResponse.Status, _ = common.Marshal(status)
				}
			}
		case "response.failed", "response.error", "error":
			if oaiErr := streamResp.GetOpenAIError(); oaiErr != nil {
				streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
				service.MarkProtocolUnsupportedStreamError(streamErr)
				break
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		if streamErr != nil || finalResponse != nil {
			break
		}
	}
	if streamErr != nil {
		return nil, streamErr
	}
	if err := scanner.Err(); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	if finalResponse == nil {
		return nil, types.NewOpenAIError(
			fmt.Errorf("Responses stream ended without a terminal response event"),
			types.ErrorCodeBadResponse,
			http.StatusInternalServerError,
		)
	}
	accumulator.SupplementResponseOutput(finalResponse)

	chatResult, err := relayconvert.ConvertResponse(c, info, types.RelayFormatOpenAI, finalResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	chatResp, ok := chatResult.Value.(*dto.OpenAITextResponse)
	if !ok {
		return nil, types.NewOpenAIError(fmt.Errorf("expected OpenAI chat response, got %T", chatResult.Value), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if chatID := helper.GetResponseID(c); chatID != "" {
		chatResp.Id = chatID
	}
	if info.RelayFormat == types.RelayFormatClaude {
		chatResp.Model = info.PublicResponseModelName()
	}
	usage := chatResult.Usage
	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(finalResponse)
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		chatResp.Usage = *usage
	}

	responseValue := any(chatResp)
	if info.RelayFormat != types.RelayFormatOpenAI {
		targetResult, err := relayconvert.ConvertResponse(c, info, info.RelayFormat, chatResp)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		responseValue = targetResult.Value
	}
	if claudeResponse, ok := responseValue.(*dto.ClaudeResponse); ok {
		protocolstate.CaptureMessagesResponse(c, finalResponse, claudeResponse)
	}
	responseBody, err := common.Marshal(responseValue)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	resp.Header = resp.Header.Clone()
	resp.Header.Set("Content-Type", "application/json")
	if err := service.IOCopyBytesGracefully(c, resp, responseBody); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	return usage, nil
}

func OaiResponsesToChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	responseId := helper.GetResponseID(c)
	createAt := time.Now().Unix()
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAIResponses, info.RelayFormat, relayconvert.ResponseStreamOptions{
		ID:           responseId,
		Model:        info.PublicResponseModelName(),
		Created:      createAt,
		IncludeUsage: info.ShouldIncludeUsage || info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatGemini,
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	streamErr := (*types.NewAPIError)(nil)
	terminalSeen := false

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}

	sendGeminiResponse := func(geminiResponse *dto.GeminiChatResponse) bool {
		if geminiResponse == nil {
			return true
		}
		geminiResponseStr, err := common.Marshal(geminiResponse)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		if err := helper.StringData(c, string(geminiResponseStr)); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		return true
	}

	sendStreamResult := func(result relayconvert.ResponseResult) bool {
		switch value := result.Value.(type) {
		case dto.ChatCompletionsStreamResponse:
			if len(value.Choices) == 0 && value.Usage == nil {
				return true
			}
			if err := helper.ObjectData(c, &value); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		case *dto.ChatCompletionsStreamResponse:
			if value == nil || (len(value.Choices) == 0 && value.Usage == nil) {
				return true
			}
			if err := helper.ObjectData(c, value); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		case dto.ClaudeResponse:
			protocolstate.ObserveClaudeStream(c, &value)
			if err := helper.ClaudeData(c, value); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		case *dto.ClaudeResponse:
			if value == nil {
				return true
			}
			protocolstate.ObserveClaudeStream(c, value)
			if err := helper.ClaudeData(c, *value); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		case dto.GeminiChatResponse:
			return sendGeminiResponse(&value)
		case *dto.GeminiChatResponse:
			return sendGeminiResponse(value)
		default:
			streamErr = types.NewOpenAIError(fmt.Errorf("unsupported converted stream response type %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, "failed to unmarshal responses stream event: "+err.Error())
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}

		if streamResp.Type == "error" || streamResp.Type == "response.error" || streamResp.Type == "response.failed" {
			if oaiErr := streamResp.GetOpenAIError(); oaiErr != nil {
				streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
				service.MarkProtocolUnsupportedStreamError(streamErr)
				sr.Stop(streamErr)
				return
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}
		switch streamResp.Type {
		case "response.completed", "response.done", "response.incomplete":
			terminalSeen = true
		case "response.cancelled", "response.canceled":
			streamErr = types.NewOpenAIError(
				fmt.Errorf("Responses upstream cancelled the response"),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
			)
			sr.Stop(streamErr)
			return
		}
		if streamResp.Response != nil {
			if err := protocolstate.ValidateResponsesContinuation(c, streamResp.Response.PreviousResponseID); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				sr.Stop(streamErr)
				return
			}
		}
		if _, observeErr := protocolstate.ObserveResponsesStreamData(c, &streamResp, []byte(data)); observeErr != nil {
			streamErr = types.NewOpenAIError(observeErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}

		results, err := relayconvert.ConvertStreamResponseChunk(c, info, state, &streamResp)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}
		for _, result := range results {
			if !sendStreamResult(result) {
				sr.Stop(streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}
	if err := streamStatusError(info); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	if !terminalSeen {
		return nil, types.NewOpenAIError(
			fmt.Errorf("Responses stream ended without a terminal response event"),
			types.ErrorCodeBadResponse,
			http.StatusInternalServerError,
		)
	}

	usage := state.Usage()
	if usage == nil || usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		state.SetUsage(usage)
	}

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
		info.ClaudeConvertInfo.Usage = usage
	}
	finalResults, err := relayconvert.FinalizeStreamResponse(c, info, state)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	for _, result := range finalResults {
		if !sendStreamResult(result) {
			return nil, streamErr
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.ShouldIncludeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseId, createAt, info.PublicResponseModelName(), *usage)); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		if err := helper.Done(c); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	}
	protocolstate.MarkStreamCompleted(c)
	return usage, nil
}
