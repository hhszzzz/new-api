package openai

import (
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *hosttypes.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, hosttypes.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	info.ObserveResponseModel(responsesResponse.Model)
	responseBody = rewriteSGLangResponsesCreatedAt(info, responseBody, "created_at", responsesResponse.CreatedAt)
	if err := protocolstate.ValidateResponsesContinuation(c, responsesResponse.PreviousResponseID); err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if protocolstate.PublicResponseID(c, "") != "" {
		upstreamResponseID := responsesResponse.ID
		responseBody, err = protocolstate.CaptureResponsesResponseData(c, upstreamResponseID, &responsesResponse, responseBody)
		if err != nil {
			return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
		}
	}
	if info.HasModelRouting() {
		responsesResponse.Model = info.PublicResponseModelName()
		responseBody, err = relaycommon.RedactUserModelRouteJSON(responseBody, info)
		if err != nil {
			return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
	}

	// 写入新的 response body
	if err := service.IOCopyBytesGracefully(c, resp, responseBody); err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	// compute usage
	usage := &dto.Usage{}
	service.ApplyResponsesUsage(usage, responsesResponse.Usage)
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	if !relaycommon.IsNonBillableResponsesStatus(responsesResponse.Status) {
		for i := range responsesResponse.Output {
			idx := i
			imageCounter.Observe(&responsesResponse.Output[i], &idx)
		}
	}
	imageCounter.Commit(info)

	return usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *hosttypes.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, hosttypes.NewError(fmt.Errorf("invalid response"), hosttypes.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	accumulator := service.NewResponsesUsageAccumulator(info)
	var streamErr *hosttypes.NewAPIError
	terminalSeen := false
	terminalSucceeded := false
	semanticOutputSeen := false
	usageComplete := false

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			streamErr = hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway)
			info.StreamStatus.MarkTerminalFailure(streamErr)
			sr.Stop(streamErr)
			return
		}
		accumulator.Observe(&streamResponse)
		if streamResponse.Response != nil {
			data = string(rewriteSGLangResponsesCreatedAt(info, []byte(data), "response.created_at", streamResponse.Response.CreatedAt))
		}
		if streamResponse.Type == "error" || streamResponse.Type == "response.error" || streamResponse.Type == "response.failed" {
			if openAIError := streamResponse.GetOpenAIError(); openAIError != nil {
				streamErr = hosttypes.WithOpenAIError(*openAIError, http.StatusInternalServerError)
			} else {
				streamErr = hosttypes.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResponse.Type), hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
			}
			// Keep upstream messages in the client error only; stream diagnostics
			// are persisted with consume logs and must contain classification only.
			outcome := info.StreamStatus.OutcomeSnapshot()
			statusErr := fmt.Errorf("Responses stream failed: code=%s type=%s", outcome.ErrorCode, outcome.ErrorType)
			info.StreamStatus.MarkTerminalFailure(statusErr)
			service.MarkProtocolUnsupportedStreamError(streamErr)
			sr.Stop(statusErr)
			return
		}
		switch streamResponse.Type {
		case "response.completed", "response.done":
			terminalSeen = true
		case "response.incomplete", "response.cancelled", "response.canceled":
			terminalSeen = true
			if streamResponse.Type == "response.incomplete" && streamResponse.Response != nil && streamResponse.Response.IncompleteDetails != nil &&
				(streamResponse.Response.IncompleteDetails.Reason == "max_output_tokens" || streamResponse.Response.IncompleteDetails.Reason == "max_tokens") {
				// Reaching the requested output limit is a valid terminal result.
				// Preserve its incomplete status instead of emitting a second error.
				terminalSucceeded = true
			} else {
				streamErr = hosttypes.NewOpenAIError(
					fmt.Errorf("Responses stream ended with terminal event %s", streamResponse.Type),
					hosttypes.ErrorCodeBadResponse,
					http.StatusBadGateway,
				)
				info.StreamStatus.MarkTerminalFailure(streamErr)
			}
		}
		if streamResponse.Response != nil {
			if streamResponse.Response.Usage != nil {
				usageComplete = true
				info.StreamStatus.MarkUsageComplete()
			}
			if len(streamResponse.Response.Output) > 0 {
				semanticOutputSeen = true
				info.StreamStatus.MarkSemanticOutput()
			}
		}
		if (streamResponse.Delta != "") ||
			(streamResponse.Type == dto.ResponsesOutputTypeItemDone && streamResponse.Item != nil) {
			semanticOutputSeen = true
			info.StreamStatus.MarkSemanticOutput()
		}
		if (streamResponse.Type == "response.completed" || streamResponse.Type == "response.done") && !usageComplete && !semanticOutputSeen {
			streamErr = hosttypes.NewOpenAIError(
				fmt.Errorf("Responses stream completed without usage or semantic output"),
				hosttypes.ErrorCodeEmptyResponse,
				http.StatusBadGateway,
			)
			info.StreamStatus.MarkTerminalFailure(streamErr)
			sr.Stop(streamErr)
			return
		}
		if streamResponse.Response != nil {
			if err := protocolstate.ValidateResponsesContinuation(c, streamResponse.Response.PreviousResponseID); err != nil {
				streamErr = hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway)
				info.StreamStatus.MarkTerminalFailure(streamErr)
				sr.Stop(streamErr)
				return
			}
		}
		if protocolstate.PublicResponseID(c, "") != "" {
			encoded, encodeErr := protocolstate.ObserveResponsesStreamData(c, &streamResponse, []byte(data))
			if encodeErr != nil {
				streamErr = hosttypes.NewOpenAIError(encodeErr, hosttypes.ErrorCodeBadResponseBody, http.StatusInternalServerError)
				info.StreamStatus.MarkTerminalFailure(streamErr)
				sr.Stop(streamErr)
				return
			}
			data = string(encoded)
		} else {
			protocolstate.ObserveResponsesStream(c, &streamResponse)
		}
		if info.HasModelRouting() {
			if streamResponse.Response != nil {
				streamResponse.Response.Model = info.PublicResponseModelName()
			}
			redacted, redactErr := relaycommon.RedactUserModelRouteJSON([]byte(data), info)
			if redactErr != nil {
				streamErr = hosttypes.NewOpenAIError(redactErr, hosttypes.ErrorCodeBadResponseBody, http.StatusInternalServerError)
				info.StreamStatus.MarkTerminalFailure(streamErr)
				sr.Stop(streamErr)
				return
			}
			data = string(redacted)
		}
		if err := sendResponsesStreamData(c, streamResponse, data); err != nil {
			streamErr = hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
			info.StreamStatus.MarkWriteError(err)
			sr.Stop(streamErr)
			return
		}
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}
		if streamResponse.Type == "response.completed" || streamResponse.Type == "response.done" || terminalSucceeded {
			terminalSucceeded = true
			info.StreamStatus.MarkTerminalSuccess()
			info.StreamStatus.MarkTerminalDelivered()
		}
	})
	common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, info.StreamStatus)
	info.StreamStatus.RequireTerminal()
	usage := accumulator.Finish()
	if streamErr != nil {
		return usage, streamErr
	}
	if err := streamStatusError(info); err != nil {
		return usage, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if !terminalSeen || !terminalSucceeded {
		terminalErr := fmt.Errorf("Responses stream ended without a terminal response event")
		info.StreamStatus.MarkTerminalFailure(terminalErr)
		return usage, hosttypes.NewOpenAIError(
			terminalErr,
			hosttypes.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}

	if info.StreamStatus.ResponseOutcome() == string(relaycommon.ResponseOutcomeCompleted) {
		protocolstate.MarkStreamCompleted(c)
	}

	return usage, nil
}

func rewriteSGLangResponsesCreatedAt(info *relaycommon.RelayInfo, payload []byte, path string, createdAt dto.IntValue) []byte {
	if info.GetChannelType() != constant.ChannelTypeSGLang || !gjson.GetBytes(payload, path).Exists() {
		return payload
	}
	patched, err := sjson.SetBytes(payload, path, int(createdAt))
	if err != nil {
		return payload
	}
	return patched
}
