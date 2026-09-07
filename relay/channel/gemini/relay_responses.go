package gemini

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

func GeminiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	logger.LogDebug(c, "Gemini responses response body: %s", responseBody)

	var geminiResponse dto.GeminiChatResponse
	if err := common.Unmarshal(responseBody, &geminiResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	markGeminiGoogleSearchCall(c, &geminiResponse)
	countGeminiBillableFunctionCalls(info, &geminiResponse)
	blockReason := geminiPromptBlockReason(&geminiResponse)
	if blockReason != "" {
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", blockReason))
	}
	if len(geminiResponse.Candidates) == 0 && blockReason == "" {
		usage := buildUsageFromGeminiResponse(c, info, &geminiResponse)
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")
		return &usage, types.NewOpenAIError(
			errors.New("empty response from Gemini API"),
			types.ErrorCodeEmptyResponse,
			http.StatusInternalServerError,
		)
	}

	usage := buildUsageFromGeminiResponse(c, info, &geminiResponse)

	convertResult, err := service.ConvertResponse(c, info, types.RelayFormatOpenAIResponses, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	responsesResp, ok := convertResult.Value.(*dto.OpenAIResponsesResponse)
	if !ok {
		return nil, types.NewOpenAIError(fmt.Errorf("expected OpenAI responses response, got %T", convertResult.Value), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if responseID := helper.GetResponseID(c); responseID != "" {
		responsesResp.ID = responseID
	}
	responsesResp.Model = info.PublicResponseModelName()
	responsesResp.Usage = relayconvert.UsageFromChatUsage(&usage)
	protocolstate.CaptureResponsesResponse(c, responsesResp.ID, responsesResp)

	responseBody, err = common.Marshal(responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	if err := service.IOCopyBytesGracefully(c, resp, responseBody); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	return &usage, nil
}

func GeminiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseID := protocolstate.PublicResponseID(c, helper.GetResponseID(c))
	created := common.GetTimestamp()
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatOpenAIResponses, relayconvert.ResponseStreamOptions{
		ID:                 responseID,
		Model:              info.PublicResponseModelName(),
		Created:            created,
		EmitSequenceNumber: true,
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	hostedBridge := relayconvert.NewGeminiHostedStreamBridge()
	var streamErr *types.NewAPIError
	upstreamCompleted := false

	sendEvent := func(event relayconvert.ChatToResponsesStreamEvent) bool {
		protocolstate.ObserveResponsesStream(c, &event.Payload)
		data, err := common.Marshal(event.Payload)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			info.StreamStatus.MarkTerminalFailure(streamErr)
			return false
		}
		if err := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: event.Type}, string(data)); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
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
				streamErr = types.NewOpenAIError(fmt.Errorf("expected OAI responses stream event, got %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return true
			}
			if !sendEvent(event) {
				return true
			}
		}
		return true
	}
	sendChunk := func(chunk *dto.GeminiChatResponse) bool {
		results, err := service.ConvertStreamResponseChunk(c, info, state, chunk)
		if err != nil {
			if failResponsesStream(err) {
				return false
			}
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			info.StreamStatus.MarkTerminalFailure(streamErr)
			return false
		}
		for _, result := range results {
			event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
			if !ok {
				streamErr = types.NewOpenAIError(fmt.Errorf("expected OAI responses stream event, got %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
				info.StreamStatus.MarkTerminalFailure(streamErr)
				return false
			}
			if !sendEvent(event) {
				return false
			}
		}
		return true
	}

	usage, streamAPIError := geminiStreamHandler(c, info, resp, func(data string, geminiResponse *dto.GeminiChatResponse) bool {
		if geminiPromptBlockReason(geminiResponse) != "" {
			upstreamCompleted = true
			info.StreamStatus.MarkTerminalSuccess()
		}
		for _, candidate := range geminiResponse.Candidates {
			if candidate.FinishReason != nil && *candidate.FinishReason != "" {
				upstreamCompleted = true
				info.StreamStatus.MarkTerminalSuccess()
				break
			}
		}
		hostedBridge.Observe(geminiResponse)
		return sendChunk(geminiResponse)
	})
	if streamAPIError != nil {
		return usage, streamAPIError
	}
	if info.StreamStatus != nil && !info.StreamStatus.IsNormalEnd() {
		if info.StreamStatus.Snapshot().EndReason == relaycommon.StreamEndReasonClientGone {
			return usage, nil
		}
		return usage, types.NewOpenAIError(
			fmt.Errorf("gemini stream ended unexpectedly: %s", info.StreamStatus.Summary()),
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	if streamErr != nil {
		return nil, streamErr
	}
	if !upstreamCompleted {
		terminalErr := fmt.Errorf("Gemini stream ended without a terminal finish reason")
		info.StreamStatus.MarkTerminalFailure(terminalErr)
		return nil, types.NewOpenAIError(
			terminalErr,
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	hostedEvents, err := hostedBridge.Finalize(state)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	for _, event := range hostedEvents {
		if !sendEvent(event) {
			if streamErr != nil {
				return usage, streamErr
			}
			return usage, nil
		}
	}

	if usage != nil {
		state.SetUsage(usage)
	}
	finalResults, err := service.FinalizeStreamResponse(c, info, state)
	if err != nil {
		info.StreamStatus.MarkTerminalFailure(err)
		if failResponsesStream(err) {
			return usage, streamErr
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	for _, result := range finalResults {
		event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
		if !ok {
			terminalErr := fmt.Errorf("expected OAI responses stream event, got %T", result.Value)
			info.StreamStatus.MarkTerminalFailure(terminalErr)
			return nil, types.NewOpenAIError(terminalErr, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		if !sendEvent(event) {
			if streamErr != nil {
				return usage, streamErr
			}
			return usage, nil
		}
	}
	info.StreamStatus.MarkTerminalDelivered()
	protocolstate.MarkStreamCompleted(c)
	return usage, nil
}
