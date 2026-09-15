package openai

import (
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"

	"github.com/gin-gonic/gin"
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
	usage := relayconvert.NormalizeResponsesUsage(responsesResponse.Usage)
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

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	imageCommitted := false
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
		if streamResponse.Type == "error" || streamResponse.Type == "response.error" || streamResponse.Type == "response.failed" {
			if openAIError := streamResponse.GetOpenAIError(); openAIError != nil {
				streamErr = hosttypes.WithOpenAIError(*openAIError, http.StatusInternalServerError)
			} else {
				streamErr = hosttypes.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResponse.Type), hosttypes.ErrorCodeBadResponse, http.StatusInternalServerError)
			}
			info.StreamStatus.MarkTerminalFailure(streamErr)
			service.MarkProtocolUnsupportedStreamError(streamErr)
			sr.Stop(streamErr)
			return
		}
		switch streamResponse.Type {
		case "response.completed", "response.done":
			terminalSeen = true
		case "response.incomplete", "response.cancelled", "response.canceled":
			terminalSeen = true
			streamErr = hosttypes.NewOpenAIError(
				fmt.Errorf("Responses stream ended with terminal event %s", streamResponse.Type),
				hosttypes.ErrorCodeBadResponse,
				http.StatusBadGateway,
			)
			info.StreamStatus.MarkTerminalFailure(streamErr)
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
		if (streamResponse.Type == "response.output_text.delta" && streamResponse.Delta != "") ||
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
		switch streamResponse.Type {
		case "response.completed", "response.done":
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					incomingUsage := relayconvert.NormalizeResponsesUsage(streamResponse.Response.Usage)
					usage = dto.MergeUsageNonZero(usage, incomingUsage)
				}
				if !imageCommitted {
					if relaycommon.IsNonBillableResponsesStatus(streamResponse.Response.Status) {
						imageCounter.Reset()
						imageCounter.Commit(info)
						imageCommitted = true
					} else {
						for i := range streamResponse.Response.Output {
							idx := i
							imageCounter.Observe(&streamResponse.Response.Output[i], &idx)
						}
						imageCounter.Commit(info)
						imageCommitted = true
					}
				}
			} else if !imageCommitted {
				imageCounter.Commit(info)
				imageCommitted = true
			}
		case "response.incomplete", "response.cancelled", "response.canceled":
			if !imageCommitted {
				imageCounter.Reset()
				imageCounter.Commit(info)
				imageCommitted = true
			}
			sr.Stop(streamErr)
			return
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
				case dto.BuildInCallFileSearchCall:
					info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
				case dto.BuildInCallFunctionCall:
					info.CountBillableToolCall(dto.BuildInCallFunctionCall, streamResponse.Item.Name)
				case dto.ResponsesOutputTypeImageGenerationCall:
					if !imageCommitted {
						imageCounter.Observe(streamResponse.Item, streamResponse.OutputIndex)
					}
				}
			}
		}
		if streamResponse.Type == "response.completed" || streamResponse.Type == "response.done" {
			terminalSucceeded = true
			info.StreamStatus.MarkTerminalSuccess()
			info.StreamStatus.MarkTerminalDelivered()
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}
	if err := streamStatusError(info); err != nil {
		return nil, hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if !terminalSeen || !terminalSucceeded {
		terminalErr := fmt.Errorf("Responses stream ended without a terminal response event")
		info.StreamStatus.MarkTerminalFailure(terminalErr)
		return nil, hosttypes.NewOpenAIError(
			terminalErr,
			hosttypes.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	if usage.BillingUsage != nil {
		usage.BillingUsage = dto.CloneBillingUsageWithEstimatedCompletion(usage.BillingUsage, usage.CompletionTokens)
	}
	protocolstate.MarkStreamCompleted(c)

	return usage, nil
}
