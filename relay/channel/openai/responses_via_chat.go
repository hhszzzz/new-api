package openai

import (
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net/http"

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

func OaiChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *hosttypes.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, hosttypes.NewOpenAIError(fmt.Errorf("invalid response"), hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var chatResp dto.OpenAITextResponse
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, hosttypes.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	info.ObserveResponseModel(chatResp.Model)
	upstreamResponseID := chatResp.Id
	if responseID := protocolstate.PublicResponseID(c, helper.GetResponseID(c)); responseID != "" {
		chatResp.Id = responseID
	}
	convertResult, err := service.ConvertResponse(c, info, types.RelayFormatOpenAIResponses, &chatResp)
	if err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	responsesResp, ok := convertResult.Value.(*dto.OpenAIResponsesResponse)
	if !ok {
		return nil, hosttypes.NewOpenAIError(fmt.Errorf("expected OpenAI responses response, got %T", convertResult.Value), hosttypes.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	responsesResp.Model = info.PublicResponseModelName()
	usage := convertResult.Usage
	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(responsesResp)
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		responsesResp.Usage = relayconvert.UsageFromChatUsage(usage)
	}
	protocolstate.CaptureResponsesResponse(c, upstreamResponseID, responsesResp)

	responseBody, err := common.Marshal(responsesResp)
	if err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	if err := service.IOCopyBytesGracefully(c, resp, responseBody); err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	return usage, nil
}

func OaiChatToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *hosttypes.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, hosttypes.NewOpenAIError(fmt.Errorf("invalid response"), hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseID := protocolstate.PublicResponseID(c, helper.GetResponseID(c))
	state, err := info.ConversionSession().StreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, relayconvert.ResponseStreamOptions{
		ID:                 responseID,
		Model:              info.PublicResponseModelName(),
		EmitSequenceNumber: true,
	})
	if err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	streamErr := (*hosttypes.NewAPIError)(nil)
	upstreamCompleted := false

	sendEvent := func(event relayconvert.ChatToResponsesStreamEvent) bool {
		protocolstate.ObserveResponsesStream(c, &event.Payload)
		data, err := common.Marshal(event.Payload)
		if err != nil {
			streamErr = hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		if err := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: event.Type}, string(data)); err != nil {
			streamErr = hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
			info.StreamStatus.MarkWriteError(err)
			return false
		}
		return true
	}
	failResponsesStream := func(err error) bool {
		failureResults, handled := state.FailResponsesStream("server_error", err.Error(), "")
		if !handled {
			return false
		}
		for _, result := range failureResults {
			event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
			if !ok {
				streamErr = hosttypes.NewOpenAIError(fmt.Errorf("expected OAI responses stream event, got %T", result.Value), hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
				return true
			}
			if !sendEvent(event) {
				return true
			}
		}
		if streamErr == nil {
			streamErr = hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		info.StreamStatus.MarkTerminalFailure(streamErr)
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		var errorResp dto.OpenAITextResponse
		if err := common.UnmarshalJsonStr(data, &errorResp); err == nil {
			if oaiError := errorResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
				upstreamErr := hosttypes.WithOpenAIError(*oaiError, resp.StatusCode)
				service.MarkProtocolUnsupportedStreamError(upstreamErr)
				if info.StreamStatus == nil || !info.StreamStatus.Snapshot().ResponseCommitted {
					streamErr = upstreamErr
					info.StreamStatus.MarkTerminalFailure(streamErr)
					sr.Stop(streamErr)
					return
				}
				if failResponsesStream(fmt.Errorf("%s", oaiError.Message)) {
					streamErr = upstreamErr
					info.StreamStatus.MarkTerminalFailure(streamErr)
					sr.Stop(streamErr)
					return
				}
				streamErr = upstreamErr
				info.StreamStatus.MarkTerminalFailure(streamErr)
				sr.Stop(streamErr)
				return
			}
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			logger.LogError(c, "failed to unmarshal chat stream response: "+err.Error())
			if failResponsesStream(err) {
				sr.Stop(streamErr)
				return
			}
			streamErr = hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}
		if chunk.IsFinished() {
			upstreamCompleted = true
			info.StreamStatus.MarkTerminalSuccess()
		}
		if service.ValidUsage(chunk.Usage) {
			info.StreamStatus.MarkUsageComplete()
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.GetContentString() != "" || choice.Delta.GetReasoningContent() != "" || len(choice.Delta.ParseToolCalls()) > 0 {
				info.StreamStatus.MarkSemanticOutput()
			}
		}
		protocolstate.SetUpstreamResponseID(c, chunk.Id)

		info.ObserveResponseModel(chunk.Model)
		results, err := service.ConvertStreamResponseChunk(c, info, state, &chunk)
		if err != nil {
			if failResponsesStream(err) {
				sr.Stop(streamErr)
				return
			}
			streamErr = hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}
		for _, result := range results {
			event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
			if !ok {
				streamErr = hosttypes.NewOpenAIError(fmt.Errorf("expected OAI responses stream event, got %T", result.Value), hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
				sr.Stop(streamErr)
				return
			}
			if !sendEvent(event) {
				sr.Stop(streamErr)
				return
			}
		}
	})

	usage := state.Usage()
	if (usage == nil || usage.TotalTokens == 0) && (upstreamCompleted || state.UsageText() != "") {
		usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		state.SetUsage(usage)
	}
	if streamErr != nil {
		return usage, streamErr
	}
	if err := streamStatusError(info); err != nil {
		return usage, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if !upstreamCompleted {
		info.StreamStatus.MarkTerminalFailure(fmt.Errorf("Chat Completions stream ended without a terminal finish_reason"))
		return usage, hosttypes.NewOpenAIError(
			fmt.Errorf("Chat Completions stream ended without a terminal finish_reason"),
			hosttypes.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}

	finalResults, err := service.FinalizeStreamResponse(c, info, state)
	if err != nil {
		if failResponsesStream(err) {
			return usage, streamErr
		}
		return usage, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	for _, result := range finalResults {
		event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
		if !ok {
			return usage, hosttypes.NewOpenAIError(fmt.Errorf("expected OAI responses stream event, got %T", result.Value), hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		if !sendEvent(event) {
			return usage, streamErr
		}
	}
	info.StreamStatus.MarkTerminalDelivered()
	protocolstate.MarkStreamCompleted(c)

	return usage, nil
}
