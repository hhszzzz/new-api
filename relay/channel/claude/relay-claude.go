package claude

import (
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net/http"
	"strings"

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

func stopReasonClaude2OpenAI(reason string) string {
	return relayconvert.StopReasonClaudeToOpenAI(reason)
}

func maybeMarkClaudeRefusal(c *gin.Context, info *relaycommon.RelayInfo, stopReason string) {
	if c == nil {
		return
	}
	if strings.EqualFold(stopReason, "refusal") {
		info.PerformanceBusinessRejection = true
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "claude_stop_reason=refusal")
	}
}

func StreamResponseClaude2OpenAI(claudeResponse *dto.ClaudeResponse) *dto.ChatCompletionsStreamResponse {
	return relayconvert.StreamResponseClaude2OpenAI(claudeResponse)
}

func ResponseClaude2OpenAI(claudeResponse *dto.ClaudeResponse) *dto.OpenAITextResponse {
	return relayconvert.ResponseClaude2OpenAI(claudeResponse)
}

type ClaudeResponseInfo = relayconvert.ClaudeResponseInfo

func cacheCreationTokensForOpenAIUsage(usage *dto.Usage) int {
	if usage == nil {
		return 0
	}
	openAIUsage := relayconvert.UsageFromClaudeUsage(usage)
	if openAIUsage == nil {
		return 0
	}
	return openAIUsage.PromptTokens - usage.PromptTokens - usage.PromptTokensDetails.CachedTokens
}

func buildOpenAIStyleUsageFromClaudeUsage(usage *dto.Usage) dto.Usage {
	mapped := relayconvert.UsageFromClaudeUsage(usage)
	if mapped == nil {
		return dto.Usage{}
	}
	return *mapped
}

func buildMessageDeltaPatchUsage(claudeResponse *dto.ClaudeResponse, claudeInfo *ClaudeResponseInfo) *dto.ClaudeUsage {
	return relayconvert.BuildMessageDeltaPatchUsage(claudeResponse, claudeInfo)
}

func shouldSkipClaudeMessageDeltaUsagePatch(info *relaycommon.RelayInfo) bool {
	return info.ShouldPassThroughBody()
}

func patchClaudeMessageDeltaUsageData(data string, usage *dto.ClaudeUsage) string {
	return relayconvert.PatchClaudeMessageDeltaUsageData(data, usage)
}

func FormatClaudeResponseInfo(claudeResponse *dto.ClaudeResponse, oaiResponse *dto.ChatCompletionsStreamResponse, claudeInfo *ClaudeResponseInfo) bool {
	return relayconvert.FormatClaudeResponseInfo(claudeResponse, oaiResponse, claudeInfo)
}

func HandleStreamResponseData(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, data string) *hosttypes.NewAPIError {
	var claudeResponse dto.ClaudeResponse
	err := common.UnmarshalJsonStr(data, &claudeResponse)
	if err != nil {
		common.SysLog("error unmarshalling stream response: " + err.Error())
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
		apiError := hosttypes.WithClaudeError(*claudeError, http.StatusInternalServerError)
		if info != nil && info.StreamStatus != nil {
			info.StreamStatus.MarkTerminalFailure(apiError)
		}
		service.MarkProtocolUnsupportedStreamError(apiError)
		return apiError
	}
	if claudeResponse.Type == "message_start" && claudeResponse.Message != nil {
		info.ObserveResponseModel(claudeResponse.Message.Model)
	}
	if claudeResponse.StopReason != "" {
		maybeMarkClaudeRefusal(c, info, claudeResponse.StopReason)
	}
	if claudeResponse.Delta != nil && claudeResponse.Delta.StopReason != nil {
		maybeMarkClaudeRefusal(c, info, *claudeResponse.Delta.StopReason)
	}
	if claudeResponse.Type == "message_stop" {
		info.StreamStatus.MarkCompleted()
	}
	if info != nil && info.StreamStatus != nil {
		if claudeResponse.Usage != nil {
			info.StreamStatus.MarkUsageComplete()
		}
		if strings.HasPrefix(claudeResponse.Type, "content_block_") {
			info.StreamStatus.MarkSemanticOutput()
		}
	}
	if info.RelayFormat == types.RelayFormatClaude {
		FormatClaudeResponseInfo(&claudeResponse, nil, claudeInfo)

		if claudeResponse.Type == "message_start" {
			// message_start, 获取usage
			if claudeResponse.Message != nil {
				info.UpstreamModelName = claudeResponse.Message.Model
				if info.HasUserModelRoute() || info.RelayFormat == types.RelayFormatClaude {
					claudeResponse.Message.Model = info.PublicResponseModelName()
				}
			}
		} else if claudeResponse.Type == "message_delta" {
			// 确保 message_delta 的 usage 包含完整的 input_tokens 和 cache 相关字段
			// 解决 AWS Bedrock 等上游返回的 message_delta 缺少这些字段的问题
			if !shouldSkipClaudeMessageDeltaUsagePatch(info) {
				data = patchClaudeMessageDeltaUsageData(data, buildMessageDeltaPatchUsage(&claudeResponse, claudeInfo))
			}
		}
		countClaudeStreamBillableTools(c, info, &claudeResponse)
		if info.PublicResponseModelName() != "" {
			redacted, redactErr := relaycommon.RedactUserModelRouteJSON([]byte(data), info)
			if redactErr != nil {
				return hosttypes.NewError(redactErr, hosttypes.ErrorCodeBadResponseBody)
			}
			data = string(redacted)
		}
		if err := helper.ClaudeChunkData(c, claudeResponse, data); err != nil {
			info.StreamStatus.MarkWriteError(err)
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
		}
	} else if info.RelayFormat == types.RelayFormatOpenAI {
		state, err := claudeToChatStreamState(info)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
		}
		response, err := state.ConvertChunk(&claudeResponse)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
		}

		if !FormatClaudeResponseInfo(&claudeResponse, response, claudeInfo) {
			return nil
		}

		countClaudeStreamBillableTools(c, info, &claudeResponse)

		if response == nil {
			return nil
		}
		err = helper.ObjectData(c, response)
		if err != nil {
			info.StreamStatus.MarkWriteError(err)
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
		}
	} else if info.RelayFormat == types.RelayFormatOpenAIResponses {
		FormatClaudeResponseInfo(&claudeResponse, nil, claudeInfo)
		state, ok := common.GetContextKeyType[*relayconvert.ResponseStreamState](c, constant.ContextKeyProtocolResponseStreamState)
		if !ok || state == nil {
			state, err = info.ConversionSession().StreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, relayconvert.ResponseStreamOptions{
				ID:    protocolstate.PublicResponseID(c, helper.GetResponseID(c)),
				Model: info.PublicResponseModelName(),
			})
			if err != nil {
				return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
			}
			common.SetContextKey(c, constant.ContextKeyProtocolResponseStreamState, state)
		}
		if claudeResponse.Type == "message_start" && claudeResponse.Message != nil {
			if claudeResponse.Message.Model != "" {
				info.UpstreamModelName = claudeResponse.Message.Model
			}
			protocolstate.SetUpstreamResponseID(c, claudeResponse.Message.Id)
		}
		countClaudeStreamBillableTools(c, info, &claudeResponse)
		results, convertErr := service.ConvertStreamResponseChunk(c, info, state, &claudeResponse)
		if convertErr != nil {
			return hosttypes.NewError(convertErr, hosttypes.ErrorCodeBadResponse)
		}
		state.SetUsage(claudeInfo.Usage)
		for _, result := range results {
			event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
			if !ok {
				return hosttypes.NewError(fmt.Errorf("expected Responses stream event, got %T", result.Value), hosttypes.ErrorCodeBadResponse)
			}
			if sendErr := sendClaudeResponsesStreamEvent(c, event); sendErr != nil {
				info.StreamStatus.MarkWriteError(sendErr)
				return hosttypes.NewError(sendErr, hosttypes.ErrorCodeBadResponse)
			}
		}
	} else if info.RelayFormat == types.RelayFormatGemini {
		state, err := claudeToGeminiStreamState(info)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
		}
		results, err := service.ConvertStreamResponseChunk(c, info, state, &claudeResponse)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
		}
		FormatClaudeResponseInfo(&claudeResponse, nil, claudeInfo)
		countClaudeStreamBillableTools(c, info, &claudeResponse)
		if sendErr := sendGeminiStreamResults(c, results); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

func claudeToChatStreamState(info *relaycommon.RelayInfo) (*relayconvert.ClaudeToChatStreamState, error) {
	if info != nil && info.ClaudeToChatStreamState != nil {
		state, ok := info.ClaudeToChatStreamState.(*relayconvert.ClaudeToChatStreamState)
		if !ok || state == nil {
			return nil, fmt.Errorf("invalid Claude-to-Chat stream state %T", info.ClaudeToChatStreamState)
		}
		return state, nil
	}

	state := relayconvert.NewClaudeToChatStreamState()
	if info != nil {
		info.ClaudeToChatStreamState = state
	}
	return state, nil
}

func claudeToGeminiStreamState(info *relaycommon.RelayInfo) (*relayconvert.ResponseStreamState, error) {
	if info != nil && info.ChatToGeminiStreamState != nil {
		state, ok := info.ChatToGeminiStreamState.(*relayconvert.ResponseStreamState)
		if !ok || state == nil {
			return nil, fmt.Errorf("invalid Claude-to-Gemini stream state %T", info.ChatToGeminiStreamState)
		}
		return state, nil
	}

	state, err := info.ConversionSession().StreamState(types.RelayFormatClaude, types.RelayFormatGemini, relayconvert.ResponseStreamOptions{Model: info.PublicResponseModelName()})
	if err != nil {
		return nil, err
	}
	if info != nil {
		info.ChatToGeminiStreamState = state
	}
	return state, nil
}

func sendGeminiStreamResults(c *gin.Context, results []relayconvert.ResponseResult) *hosttypes.NewAPIError {
	for _, result := range results {
		geminiResponse, ok := result.Value.(*dto.GeminiChatResponse)
		if !ok {
			return hosttypes.NewError(fmt.Errorf("expected Gemini stream response, got %T", result.Value), hosttypes.ErrorCodeBadResponseBody)
		}
		if geminiResponse == nil {
			continue
		}
		data, err := common.Marshal(geminiResponse)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
		}
		if err := helper.StringData(c, string(data)); err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
		}
	}
	return nil
}

func countClaudeStreamBillableTools(c *gin.Context, info *relaycommon.RelayInfo, claudeResponse *dto.ClaudeResponse) {
	if claudeResponse == nil {
		return
	}
	if claudeResponse.Type == "content_block_start" &&
		claudeResponse.ContentBlock != nil &&
		claudeResponse.ContentBlock.Type == "tool_use" {
		info.CountBillableToolCall(dto.BuildInCallToolUse, claudeResponse.ContentBlock.Name)
	}
	if claudeResponse.Type == "message_delta" &&
		claudeResponse.Usage != nil &&
		claudeResponse.Usage.ServerToolUse != nil &&
		claudeResponse.Usage.ServerToolUse.WebSearchRequests > 0 {
		c.Set("claude_web_search_requests", claudeResponse.Usage.ServerToolUse.WebSearchRequests)
	}
}

func HandleStreamFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo) *hosttypes.NewAPIError {
	if info.RelayFormat == types.RelayFormatOpenAIResponses {
		state, ok := common.GetContextKeyType[*relayconvert.ResponseStreamState](c, constant.ContextKeyProtocolResponseStreamState)
		if !ok || state == nil {
			return hosttypes.NewError(fmt.Errorf("Claude Responses stream ended without conversion state"), hosttypes.ErrorCodeBadResponse)
		}
		usage := state.Usage()
		if usage == nil || usage.TotalTokens == 0 {
			usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
			state.SetUsage(usage)
		}
		finalResults, err := service.FinalizeStreamResponse(c, info, state)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
		}
		for _, result := range finalResults {
			event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
			if !ok {
				return hosttypes.NewError(fmt.Errorf("expected Responses stream event, got %T", result.Value), hosttypes.ErrorCodeBadResponse)
			}
			if sendErr := sendClaudeResponsesStreamEvent(c, event); sendErr != nil {
				return hosttypes.NewError(sendErr, hosttypes.ErrorCodeBadResponse)
			}
		}
		claudeInfo.Usage = usage
		return nil
	}
	if claudeInfo.Usage.PromptTokens == 0 {
		//上游出错
	}
	if claudeInfo.Usage.CompletionTokens == 0 || !claudeInfo.Done {
		if common.DebugEnabled {
			common.SysLog("claude response usage is not complete, maybe upstream error")
		}
		// 只补缺失字段，不整份覆盖——保留 message_start 已拿到的 cache 字段
		fallback := service.ResponseText2Usage(c, claudeInfo.ResponseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		if claudeInfo.Usage.CompletionTokens == 0 ||
			(!claudeInfo.Done && fallback.CompletionTokens > claudeInfo.Usage.CompletionTokens) {
			claudeInfo.Usage.CompletionTokens = fallback.CompletionTokens
		}
		if claudeInfo.Usage.PromptTokens == 0 {
			claudeInfo.Usage.PromptTokens = fallback.PromptTokens
		}
		claudeInfo.Usage.TotalTokens = claudeInfo.Usage.PromptTokens + claudeInfo.Usage.CompletionTokens
	}
	if claudeInfo.Usage != nil {
		claudeInfo.Usage.UsageSemantic = "anthropic"
	}
	relayconvert.FinalizeClaudeStreamBillingUsage(claudeInfo)

	if info.RelayFormat == types.RelayFormatClaude {
		//
	} else if info.RelayFormat == types.RelayFormatOpenAI {
		if info.ShouldIncludeUsage {
			openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
			response := helper.GenerateFinalUsageResponse(claudeInfo.ResponseId, claudeInfo.Created, info.PublicResponseModelName(), openAIUsage)
			err := helper.ObjectData(c, response)
			if err != nil {
				return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
			}
		}
		if err := helper.Done(c); err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
		}
	} else if info.RelayFormat == types.RelayFormatGemini {
		state, err := claudeToGeminiStreamState(info)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
		}
		results, err := service.FinalizeStreamResponse(c, info, state)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
		}
		if sendErr := sendGeminiStreamResults(c, results); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

// CompleteClaudeStream validates the terminal Anthropic Messages lifecycle,
// emits any protocol-specific final response, and only then marks the stream as
// safe for affinity/state commit. Bedrock and HTTP Anthropic transports share
// this contract even though they receive events through different scanners.
func CompleteClaudeStream(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, streamErr error) *hosttypes.NewAPIError {
	if streamErr != nil {
		if info != nil && info.StreamStatus != nil && !info.StreamStatus.IsClientGone() {
			info.StreamStatus.MarkTerminalFailure(streamErr)
		}
		return hosttypes.NewErrorWithStatusCode(streamErr, hosttypes.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if claudeInfo == nil || !claudeInfo.Done {
		terminalErr := fmt.Errorf("Claude Messages stream ended without a terminal stop_reason")
		if info != nil && info.StreamStatus != nil {
			info.StreamStatus.MarkTerminalFailure(terminalErr)
		}
		return hosttypes.NewErrorWithStatusCode(
			terminalErr,
			hosttypes.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	if !claudeInfo.MessageStop {
		terminalErr := fmt.Errorf("Claude Messages stream ended without message_stop")
		if info != nil && info.StreamStatus != nil {
			info.StreamStatus.MarkTerminalFailure(terminalErr)
		}
		return hosttypes.NewErrorWithStatusCode(
			terminalErr,
			hosttypes.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	if info != nil && info.StreamStatus != nil {
		info.StreamStatus.MarkTerminalSuccess()
		if service.ValidUsage(claudeInfo.Usage) {
			info.StreamStatus.MarkUsageComplete()
		}
		if claudeInfo.ResponseText.Len() > 0 {
			info.StreamStatus.MarkSemanticOutput()
		}
	}
	if finalErr := HandleStreamFinalResponse(c, info, claudeInfo); finalErr != nil {
		if info != nil && info.StreamStatus != nil {
			info.StreamStatus.MarkWriteError(finalErr)
		}
		return finalErr
	}
	if info != nil && info.StreamStatus != nil {
		info.StreamStatus.MarkTerminalDelivered()
	}
	protocolstate.MarkStreamCompleted(c)
	return nil
}

func ClaudeStreamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *hosttypes.NewAPIError) {
	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.PublicResponseModelName(),
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	var err *hosttypes.NewAPIError
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		err = HandleStreamResponseData(c, info, claudeInfo, data)
		if err != nil {
			sr.Stop(err)
		}
	})
	info.StreamStatus.RequireTerminal()
	if err != nil {
		return nil, err
	}
	var streamErr error
	if info.StreamStatus != nil && !info.StreamStatus.IsNormalEnd() {
		_, streamErr = info.StreamStatus.EndState()
		if streamErr == nil {
			streamErr = fmt.Errorf("Claude stream ended abnormally: %s", info.StreamStatus.Summary())
		}
	}
	if finalErr := CompleteClaudeStream(c, info, claudeInfo, streamErr); finalErr != nil {
		return nil, finalErr
	}
	return claudeInfo.Usage, nil
}

func sendClaudeResponsesStreamEvent(c *gin.Context, event relayconvert.ChatToResponsesStreamEvent) error {
	protocolstate.ObserveResponsesStream(c, &event.Payload)
	data, err := common.Marshal(event.Payload)
	if err != nil {
		return err
	}
	return helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: event.Type}, string(data))
}

func HandleClaudeResponseData(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, httpResp *http.Response, data []byte) *hosttypes.NewAPIError {
	var claudeResponse dto.ClaudeResponse
	err := common.Unmarshal(data, &claudeResponse)
	if err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
	}
	if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
		apiError := hosttypes.WithClaudeError(*claudeError, http.StatusInternalServerError)
		service.MarkProtocolUnsupportedStreamError(apiError)
		return apiError
	}
	info.ObserveResponseModel(claudeResponse.Model)
	maybeMarkClaudeRefusal(c, info, claudeResponse.StopReason)
	if claudeInfo.Usage == nil {
		claudeInfo.Usage = &dto.Usage{}
	}
	if claudeResponse.Usage != nil {
		claudeInfo.Usage.PromptTokens = claudeResponse.Usage.InputTokens
		claudeInfo.Usage.CompletionTokens = claudeResponse.Usage.OutputTokens
		claudeInfo.Usage.TotalTokens = claudeResponse.Usage.InputTokens + claudeResponse.Usage.OutputTokens
		claudeInfo.Usage.UsageSemantic = "anthropic"
		claudeInfo.Usage.BillingUsage = dto.CloneBillingUsage(claudeResponse.Usage.BillingUsage)
		if claudeInfo.Usage.BillingUsage == nil {
			claudeInfo.Usage.BillingUsage = dto.NewClaudeMessagesBillingUsage(claudeResponse.Usage)
		}
		claudeInfo.Usage.PromptTokensDetails.CachedTokens = claudeResponse.Usage.CacheReadInputTokens
		claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens = claudeResponse.Usage.CacheCreationInputTokens
		claudeInfo.Usage.ClaudeCacheCreation5mTokens = claudeResponse.Usage.GetCacheCreation5mTokens()
		claudeInfo.Usage.ClaudeCacheCreation1hTokens = claudeResponse.Usage.GetCacheCreation1hTokens()
	}
	if info.HasUserModelRoute() || info.RelayFormat == types.RelayFormatClaude {
		claudeResponse.Model = info.PublicResponseModelName()
	}
	var responseData []byte
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		openaiResponse := ResponseClaude2OpenAI(&claudeResponse)
		openaiResponse.Usage = buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
		responseData, err = common.Marshal(openaiResponse)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatOpenAIResponses:
		convertResult, err := service.ConvertResponse(c, info, types.RelayFormatOpenAIResponses, &claudeResponse)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
		}
		responsesResponse, ok := convertResult.Value.(*dto.OpenAIResponsesResponse)
		if !ok {
			return hosttypes.NewError(fmt.Errorf("expected OpenAI Responses response, got %T", convertResult.Value), hosttypes.ErrorCodeBadResponseBody)
		}
		if responseID := helper.GetResponseID(c); responseID != "" {
			responsesResponse.ID = responseID
		}
		responsesResponse.Model = info.PublicResponseModelName()
		protocolstate.CaptureResponsesResponse(c, claudeResponse.Id, responsesResponse)
		responseData, err = common.Marshal(responsesResponse)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		if info.PublicResponseModelName() != "" {
			responseData, err = relaycommon.RedactUserModelRouteJSON(data, info)
			if err != nil {
				return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
			}
		} else {
			responseData = data
		}
	case types.RelayFormatGemini:
		{
			convertResult, convertErr := service.ConvertResponse(c, info, types.RelayFormatGemini, &claudeResponse)
			if convertErr != nil {
				return hosttypes.NewError(convertErr, hosttypes.ErrorCodeBadResponseBody)
			}
			geminiResponse, ok := convertResult.Value.(*dto.GeminiChatResponse)
			if !ok {
				return hosttypes.NewError(fmt.Errorf("expected Gemini generateContent response, got %T", convertResult.Value), hosttypes.ErrorCodeBadResponseBody)
			}
			responseData, err = common.Marshal(geminiResponse)
			if err != nil {
				return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
			}
		}
	}

	if claudeResponse.Usage != nil && claudeResponse.Usage.ServerToolUse != nil && claudeResponse.Usage.ServerToolUse.WebSearchRequests > 0 {
		c.Set("claude_web_search_requests", claudeResponse.Usage.ServerToolUse.WebSearchRequests)
	}

	for _, block := range claudeResponse.Content {
		if block.Type == "tool_use" {
			info.CountBillableToolCall(dto.BuildInCallToolUse, block.Name)
		}
	}

	if err := service.IOCopyBytesGracefully(c, httpResp, responseData); err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeBadResponse)
	}
	return nil
}

func ClaudeHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *hosttypes.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.PublicResponseModelName(),
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, hosttypes.NewError(err, hosttypes.ErrorCodeBadResponseBody)
	}
	logger.LogDebug(c, "responseBody: %s", responseBody)
	handleErr := HandleClaudeResponseData(c, info, claudeInfo, resp, responseBody)
	if handleErr != nil {
		return nil, handleErr
	}
	return claudeInfo.Usage, nil
}
