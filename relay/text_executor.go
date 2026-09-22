package relay

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	claudechannel "github.com/QuantumNous/new-api/relay/channel/claude"
	geminichannel "github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/output"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// executeText is the sole text attempt lifecycle. The controller owns the
// logical request's pre-consume/retry/refund; this function owns one upstream
// request, its conversion session, response delivery and successful settlement.
func executeText(c *gin.Context, info *relaycommon.RelayInfo) *hosttypes.NewAPIError {
	info.InitChannelMeta(c)
	defer info.CloseConversionSession()
	outputAuditSetting := prompt_audit_setting.GetSetting()
	baseWriter := c.Writer
	var outputAuditWriter *promptAuditResponseWriter
	if outputAuditSetting.OutputMode != prompt_audit_setting.ModeOff && !info.IsChannelTest && service.PromptAuditAppliesToGroup(c, outputAuditSetting, outputAuditSetting.OutputMode) {
		outputAuditWriter = newPromptAuditResponseWriter(baseWriter, outputAuditSetting)
		c.Writer = outputAuditWriter
		defer func() {
			_ = outputAuditWriter.capture.Close()
			c.Writer = baseWriter
		}()
	}
	isCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact
	originalFormat := info.RelayFormat
	defer func() { info.RelayFormat = originalFormat }()
	clientStream := info.IsStream
	request, err := cloneTextRequest(info.Request)
	if err != nil {
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	before, err := common.Marshal(request)
	if err != nil {
		return newConvertRequestFailedError(c, info, err)
	}
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeChannelModelMappedError, hosttypes.ErrOptionWithSkipRetry())
	}
	if err := helper.ApplyReasoningModelSuffix(c, info, request); err != nil {
		return newConvertRequestFailedError(c, info, err)
	}
	plan, ok := selectedProtocolPlan(c)
	if !ok {
		settings, _ := common.Marshal(info.ChannelOtherSettings)
		channelSettings, _ := common.Marshal(info.ChannelSetting)
		candidate := &model.Channel{Id: info.ChannelId, Type: info.ChannelType, BaseURL: common.GetPointer(info.ChannelBaseUrl), OtherSettings: string(settings), Setting: common.GetPointer(string(channelSettings)), ModelMapping: common.GetPointer(c.GetString("model_mapping"))}
		features, featureErr := channelcompat.ExtractRequestFeatureSet(relayconvert.ProtocolForFormat(info.RelayFormat), before)
		if featureErr != nil {
			return newConvertRequestFailedError(c, info, featureErr)
		}
		plan = channelcompat.PlanForRequest(candidate, relayconvert.ProtocolForFormat(info.RelayFormat), info.SelectionModelName(), c.Request.URL.Path, features)
		common.SetContextKey(c, constant.ContextKeyProtocolPlan, plan)
	}
	if plan.Status == channelcompat.StatusIncompatible {
		return newConvertRequestFailedError(c, info, fmt.Errorf("%s", plan.Reason))
	}
	info.CompactionMode = plan.CompactionMode
	summaryCompact := isCompact && plan.CompactionMode == relayconvert.CompactionSummary
	if summaryCompact {
		info.RelayFormat = types.RelayFormatOpenAIResponses
	}
	if info.RelayMode == relayconstant.RelayModeCompletions && protocolPlanRequiresConversion(plan) {
		return newConvertRequestFailedError(c, info, fmt.Errorf("legacy completions require their native operation"))
	}
	info.SetConversionLossPolicy(conversionLossPolicyForPlan(plan))
	// The plan already decided whether best-effort directives (e.g. Messages
	// context_management) may be dropped for this channel; mirror that decision
	// into the execution-time conversion check. Setting it unconditionally keeps
	// retries honest: a retry on a stricter channel must not inherit a previous
	// attempt's lossy permission.
	info.SetAllowDirectiveDrop(plan.Conversion == hostdto.ProtocolConversionLossy)
	adaptor := GetAdaptorForProtocol(info.ApiType, plan.UpstreamProtocol)
	if adaptor == nil {
		return hosttypes.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), hosttypes.ErrorCodeInvalidApiType, hosttypes.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	if info.RelayFormat == types.RelayFormatOpenAI && info.RelayMode != relayconstant.RelayModeCompletions && (info.ChannelSetting.ForceFormat || info.ChannelSetting.ThinkingToContent) {
		writer := c.Writer
		c.Writer = &output.ChatWriter{ResponseWriter: writer, ForceFormat: info.ChannelSetting.ForceFormat, ThinkingToContent: info.ChannelSetting.ThinkingToContent}
		defer func() { c.Writer = writer }()
	}
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if req.WebSearchOptions != nil {
			c.Set("chat_completion_web_search_context_size", req.WebSearchOptions.SearchContextSize)
		}
		info.ShouldIncludeUsage = req.StreamOptions == nil || req.StreamOptions.IncludeUsage
		if !info.SupportStreamOptions || !clientStream {
			req.StreamOptions = nil
		} else if constant.ForceStreamOption {
			req.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
		}
		applySystemPromptIfNeeded(c, info, req)
	case *dto.ClaudeRequest:
		if req.MaxTokens == nil {
			tokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(req.Model))
			req.MaxTokens = &tokens
		}
		applyClaudeLeadingSystemPrompt(c, info, req)
		if err := protocolstate.PrepareMessagesRequest(c, info, plan, req); err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
	case *dto.OpenAIResponsesRequest:
		publicModel := info.OriginModelName
		if isCompact {
			info.OriginModelName = strings.TrimSuffix(publicModel, ratio_setting.CompactModelSuffix)
		}
		if summaryCompact {
			req.Store = []byte("false")
			common.SetContextKey(c, constant.ContextKeyProtocolStateForceReplay, true)
		}
		stateErr := protocolstate.PrepareResponsesRequest(c, info, plan, req)
		info.OriginModelName = publicModel
		if stateErr != nil {
			return newConvertRequestFailedError(c, info, stateErr)
		}
		applyResponsesInstructionsIfNeeded(c, info, req)
		if summaryCompact {
			request, err = relayconvert.BuildCompactionSummaryRequest(req)
			if err != nil {
				return newConvertRequestFailedError(c, info, err)
			}
		}
	case *dto.GeminiChatRequest:
		relayconvert.RecordGeminiReasoningEffort(req, info)
		applyGeminiLeadingSystemPrompt(c, info, req)
	}
	restorePlan := func() {}
	if info.RelayMode != relayconstant.RelayModeCompletions {
		restorePlan = applyProtocolPlan(info, plan)
		defer restorePlan()
	}
	var body io.Reader
	passthrough := !summaryCompact && info.ShouldPassThroughBody() && !protocolPlanRequiresConversion(plan) && !protocolPlanRequiresStructuredRequest(info, plan) && !protocolstate.Active(c) && !protocolstate.ResponsesRequestNormalized(c)
	if passthrough {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		body = common.NewReplayableBodyReader(storage)
	} else {
		converted, err := convertRequestForProtocolPlan(c, info, adaptor, plan, request)
		if err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
		relaycommon.AppendRequestConversionFromRequest(info, converted)
		jsonBody, err := common.Marshal(converted)
		if err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
		if !summaryCompact && !protocolPlanRequiresConversion(plan) {
			if storage, storageErr := common.GetBodyStorage(c); storageErr == nil {
				if original, readErr := storage.Bytes(); readErr == nil && len(original) > 0 {
					jsonBody, err = relaycommon.MergeNativeRequestBody(original, before, jsonBody)
					if err != nil {
						return newConvertRequestFailedError(c, info, err)
					}
				}
			}
		}
		jsonBody, err = relaycommon.RemoveDisabledFields(jsonBody, info.ChannelOtherSettings, info.ShouldPassThroughBody())
		if err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
		if len(info.ParamOverride) > 0 {
			jsonBody, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonBody, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}
		if summaryCompact {
			jsonBody, err = enforceCompactionSummaryRequest(jsonBody, plan.UpstreamProtocol)
			if err != nil {
				return newConvertRequestFailedError(c, info, err)
			}
		}
		outbound, closer, err := relaycommon.NewOutboundJSONBody(jsonBody)
		if err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
		defer closer.Close()
		body = outbound
	}
	// Mapping and channel overrides have now fixed the compact billing model.
	// Resolve its price before an upstream call or a successful response is sent.
	if isCompact {
		if _, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{}); err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeModelPriceError, hosttypes.ErrOptionWithSkipRetry())
		}
	}
	response, err := adaptor.DoRequest(c, info, body)
	if err != nil {
		return hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResponse *http.Response
	if response != nil {
		if response.WebSocket != nil {
			_ = response.WebSocket.Close()
			return hosttypes.NewError(fmt.Errorf("text generation requires an HTTP or SDK response body"), hosttypes.ErrorCodeBadResponseBody)
		}
		httpResponse = response.Response
		if response.Format != "" {
			info.FinalRequestRelayFormat = response.Format
		}
		if response.Transport != "" {
			plan.Transport = response.Transport
			common.SetContextKey(c, constant.ContextKeyProtocolPlan, plan)
		}
	}
	if httpResponse != nil {
		defer service.CloseResponseBodyGracefully(httpResponse)
	}
	statusMapping := c.GetString("status_code_mapping")
	writer := c.Writer
	var summaryWriter *compactSummaryWriter
	if summaryCompact {
		summaryWriter = &compactSummaryWriter{ResponseWriter: writer, header: writer.Header().Clone(), status: http.StatusOK}
		c.Writer = summaryWriter
		defer func() { c.Writer = writer }()
	}
	var usage *dto.Usage
	var apiError *hosttypes.NewAPIError
	if httpResponse != nil {
		if httpResponse.StatusCode != http.StatusOK {
			apiError = service.RelayErrorHandler(c.Request.Context(), httpResponse, false)
			service.ResetStatusCode(apiError, statusMapping)
			return apiError
		}
		if plan.SelectionMode == hostdto.ProtocolSelectionModeAuto {
			if envelopeError := service.DetectProtocolUnsupportedSuccessEnvelope(httpResponse); envelopeError != nil {
				return envelopeError
			}
		}
		upstreamStream := service.ResponseIsEventStream(httpResponse)
		info.IsStream = clientStream || upstreamStream
		if clientStream && !upstreamStream && service.ResponseIsJSON(httpResponse) {
			if err := helper.PromoteJSONResponseToSSE(httpResponse, info.GetFinalRequestRelayFormat()); err != nil {
				return hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			upstreamStream = true
		}
		if !clientStream && upstreamStream {
			info.IsStream = false
			var handled bool
			usage, handled, apiError = handleBufferedStreamResponse(c, info, httpResponse, info.GetFinalRequestRelayFormat(), statusMapping)
			if apiError != nil {
				return apiError
			}
			if !handled {
				usage = nil
			}
		}
	}
	if usage == nil {
		usage, apiError = handleTextResponse(c, info, adaptor, httpResponse)
	}
	apiError = normalizeStreamResult(c, info, apiError)
	if apiError != nil {
		if outputAuditWriter != nil && outputAuditWriter.blocking && outputAuditWriter.capture.failure != nil {
			body, _ := outputAuditWriter.capture.Bytes()
			outputText, _ := extractPromptAuditOutput(body)
			failure := "output_capture_failed"
			if outputAuditWriter.capture.overflow {
				failure = "output_buffer_limit"
			}
			service.RecordOutputAuditUnavailable(c, service.PromptAuditRequest{
				Snapshot: dto.PromptAuditSnapshotOf(info.Request), Protocol: string(info.RelayFormat), Model: info.OriginModelName,
				Stage: "text_executor", Direction: service.PromptAuditDirectionOutput, Output: outputText,
				DeliveryStatus: "not_delivered", CoverageComplete: false, Stream: info.IsStream,
			}, failure)
			settleTextUsage(c, info, usage)
			return hosttypes.NewErrorWithStatusCode(errors.New("output audit buffer is unavailable"), hosttypes.ErrorCodeOutputAuditUnavailable, http.StatusServiceUnavailable, hosttypes.ErrOptionWithSkipRetry())
		}
		if outputAuditWriter != nil && !outputAuditWriter.blocking && outputAuditWriter.capture.size > 0 {
			auditIncompleteTextOutput(c, info, outputAuditWriter, "delivered_incomplete")
		}
		// Responses upstreams charge generation that was produced before an
		// interruption. Keep the failure visible, settle once, and never retry
		// an attempt whose usage has already been charged.
		if info.IsStream && usage != nil &&
			(info.RelayFormat == types.RelayFormatOpenAIResponses || info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAIResponses) {
			billable := usage.TotalTokens > 0 || usage.PromptTokensDetails.CachedTokens > 0 ||
				usage.PromptTokensDetails.CachedCreationTokens > 0 ||
				(info.StreamStatus.ResponseOutcome() != "" && info.StreamStatus.Snapshot().ResponseAccepted)
			if info.ResponsesUsageInfo != nil {
				for _, tool := range info.ResponsesUsageInfo.BuiltInTools {
					billable = billable || tool != nil && tool.CallCount > 0
				}
			}
			if billable {
				if info.IsChannelTest {
					info.TestUsage = usage
				} else {
					ConsumeResponsesQuota(c, info, usage)
				}
				hosttypes.ErrOptionWithSkipRetry()(apiError)
			}
		}
		service.ResetStatusCode(apiError, statusMapping)
		return apiError
	}
	if usage == nil {
		return hosttypes.NewError(fmt.Errorf("text response returned no accounting usage"), hosttypes.ErrorCodeBadResponseBody)
	}
	if summaryWriter != nil {
		body, err := summaryWriter.compactResponse(usage)
		if err != nil {
			return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway, hosttypes.ErrOptionWithSkipRetry())
		}
		c.Writer = writer
		c.Header("X-New-Api-Compaction", relayconvert.CompactionSummary)
		c.Data(http.StatusOK, "application/json", body)
	}
	if outputAuditWriter != nil {
		body, captureErr := outputAuditWriter.capture.Bytes()
		outputText, extractErr := extractPromptAuditOutput(body)
		coverageComplete := !outputAuditWriter.capture.overflow && captureErr == nil && extractErr == nil
		if outputAuditWriter.blocking && !coverageComplete {
			service.RecordOutputAuditUnavailable(c, service.PromptAuditRequest{
				Snapshot: dto.PromptAuditSnapshotOf(info.Request), Protocol: string(info.RelayFormat), Model: info.OriginModelName,
				Stage: "text_executor", Direction: service.PromptAuditDirectionOutput, Output: outputText,
				DeliveryStatus: "not_delivered", CoverageComplete: false, Stream: info.IsStream,
			}, "output_capture_incomplete")
			settleTextUsage(c, info, usage)
			return hosttypes.NewErrorWithStatusCode(errors.New("output audit is unavailable"), hosttypes.ErrorCodeOutputAuditUnavailable, http.StatusServiceUnavailable, hosttypes.ErrOptionWithSkipRetry())
		}
		if outputText != "" {
			deliveryStatus := "delivered"
			if outputAuditWriter.blocking {
				deliveryStatus = "not_delivered"
			}
			result, auditErr := service.InspectOutput(c, service.PromptAuditRequest{
				Snapshot: dto.PromptAuditSnapshotOf(info.Request), Protocol: string(info.RelayFormat), Model: info.OriginModelName,
				Stage: "text_executor", Direction: service.PromptAuditDirectionOutput, Output: outputText,
				DeliveryStatus: deliveryStatus, CoverageComplete: coverageComplete, Stream: info.IsStream,
			})
			if outputAuditWriter.blocking && auditErr != nil {
				_ = model.UpdatePromptAuditDelivery(result.AuditID, "not_delivered")
				settleTextUsage(c, info, usage)
				code := hosttypes.ErrorCodeOutputAuditUnavailable
				status := http.StatusServiceUnavailable
				message := "output audit is unavailable"
				if result.Blocked && result.Decision == service.PromptAuditDecisionBlock {
					code, status, message = hosttypes.ErrorCodeOutputAuditBlocked, http.StatusForbidden, "generated output blocked by content audit"
				}
				options := []hosttypes.NewAPIErrorOptions{hosttypes.ErrOptionWithSkipRetry()}
				if code == hosttypes.ErrorCodeOutputAuditBlocked {
					// Blocked output is already recorded in the prompt audit
					// log; it must not pollute the user-visible error log.
					options = append(options, hosttypes.ErrOptionWithNoRecordErrorLog())
				}
				return hosttypes.NewErrorWithStatusCode(errors.New(message), code, status, options...)
			}
			if outputAuditWriter.blocking {
				if err := outputAuditWriter.commit(); err != nil {
					_ = model.UpdatePromptAuditDelivery(result.AuditID, "delivery_failed")
					settleTextUsage(c, info, usage)
					return hosttypes.NewErrorWithStatusCode(errors.New("output delivery failed after audit"), hosttypes.ErrorCodeOutputAuditUnavailable, http.StatusBadGateway, hosttypes.ErrOptionWithSkipRetry())
				}
				_ = model.UpdatePromptAuditDelivery(result.AuditID, "delivered")
			}
		} else if !outputAuditWriter.blocking {
			service.RecordOutputAuditUnavailable(c, service.PromptAuditRequest{
				Snapshot: dto.PromptAuditSnapshotOf(info.Request), Protocol: string(info.RelayFormat), Model: info.OriginModelName,
				Stage: "text_executor", Direction: service.PromptAuditDirectionOutput,
				DeliveryStatus: "delivered", CoverageComplete: false, Stream: info.IsStream,
			}, "output_extract_failed")
		}
	}
	if isCompact {
		restorePlan()
		info.RelayFormat = originalFormat
	}
	if info.IsChannelTest {
		info.TestUsage = usage
		return nil
	}
	settleTextUsage(c, info, usage)
	return nil
}

func settleTextUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	if usage == nil || info.IsChannelTest {
		return
	}
	containsAudio := usage.CompletionTokenDetails.AudioTokens > 0 || usage.PromptTokensDetails.AudioTokens > 0
	audioPricing := ratio_setting.ContainsAudioRatio(info.OriginModelName) || ratio_setting.ContainsAudioCompletionRatio(info.OriginModelName)
	if containsAudio && audioPricing || info.RelayFormat == types.RelayFormatOpenAIResponses && strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usage, "")
	} else {
		service.PostTextConsumeQuota(c, info, usage, nil)
	}
}

func auditIncompleteTextOutput(c *gin.Context, info *relaycommon.RelayInfo, writer *promptAuditResponseWriter, deliveryStatus string) {
	body, err := writer.capture.Bytes()
	if err != nil {
		service.RecordOutputAuditUnavailable(c, service.PromptAuditRequest{
			Snapshot: dto.PromptAuditSnapshotOf(info.Request), Protocol: string(info.RelayFormat), Model: info.OriginModelName,
			Stage: "text_executor", Direction: service.PromptAuditDirectionOutput,
			DeliveryStatus: deliveryStatus, CoverageComplete: false, Stream: info.IsStream,
		}, "output_capture_failed")
		return
	}
	outputText, err := extractPromptAuditOutput(body)
	if err != nil || outputText == "" {
		service.RecordOutputAuditUnavailable(c, service.PromptAuditRequest{
			Snapshot: dto.PromptAuditSnapshotOf(info.Request), Protocol: string(info.RelayFormat), Model: info.OriginModelName,
			Stage: "text_executor", Direction: service.PromptAuditDirectionOutput,
			DeliveryStatus: deliveryStatus, CoverageComplete: false, Stream: info.IsStream,
		}, "output_extract_failed")
		return
	}
	_, _ = service.InspectOutput(c, service.PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshotOf(info.Request), Protocol: string(info.RelayFormat), Model: info.OriginModelName,
		Stage: "text_executor", Direction: service.PromptAuditDirectionOutput, Output: outputText,
		DeliveryStatus: deliveryStatus, CoverageComplete: false, Stream: info.IsStream,
	})
}

func cloneTextRequest(request dto.Request) (dto.Request, error) {
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return common.DeepCopy(req)
	case *dto.ClaudeRequest:
		return common.DeepCopy(req)
	case *dto.GeminiChatRequest:
		return common.DeepCopy(req)
	case *dto.OpenAIResponsesRequest:
		return common.DeepCopy(req)
	case *dto.OpenAIResponsesCompactionRequest:
		return common.DeepCopy(&dto.OpenAIResponsesRequest{Model: req.Model, Input: req.Input, Instructions: req.Instructions, PreviousResponseID: req.PreviousResponseID, Tools: req.Tools, ParallelToolCalls: req.ParallelToolCalls, Reasoning: req.Reasoning, Text: req.Text, ServiceTier: req.ServiceTier, PromptCacheKey: req.PromptCacheKey, PromptCacheOptions: req.PromptCacheOptions, PromptCacheRetention: req.PromptCacheRetention})
	default:
		return nil, fmt.Errorf("invalid text request type %T", request)
	}
}

func handleTextResponse(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, resp *http.Response) (*dto.Usage, *hosttypes.NewAPIError) {
	// Vendor-specific wire dialects retain their parser. The four public text
	// protocols share parsing/conversion regardless of which provider called them.
	switch info.ApiType {
	case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference, constant.APITypeAnthropic, constant.APITypeGemini, constant.APITypeAdvancedCustom, constant.APITypeNewAPI, constant.APITypeSub2API, constant.APITypeAws:
		if info.RelayMode == relayconstant.RelayModeResponsesCompact {
			return openai.OaiResponsesCompactionHandlerWithInfo(c, resp, info)
		}
		switch info.GetFinalRequestRelayFormat() {
		case types.RelayFormatOpenAI:
			if info.RelayFormat == types.RelayFormatOpenAIResponses {
				if info.IsStream {
					return openai.OaiChatToResponsesStreamHandler(c, info, resp)
				}
				return openai.OaiChatToResponsesHandler(c, info, resp)
			}
			if info.IsStream {
				return openai.OaiStreamHandler(c, info, resp)
			}
			return openai.OpenaiHandler(c, info, resp)
		case types.RelayFormatOpenAIResponses:
			if info.RelayFormat != types.RelayFormatOpenAIResponses {
				if info.IsStream {
					return openai.OaiResponsesToChatStreamHandler(c, info, resp)
				}
				return openai.OaiResponsesToChatHandler(c, info, resp)
			}
			if info.IsStream {
				return openai.OaiResponsesStreamHandler(c, info, resp)
			}
			return openai.OaiResponsesHandler(c, info, resp)
		case types.RelayFormatClaude:
			if info.IsStream {
				if info.RelayFormat == types.RelayFormatOpenAIResponses {
					return claudechannel.ClaudeResponsesStreamHandler(c, resp, info)
				}
				return claudechannel.ClaudeStreamHandler(c, resp, info)
			}
			return claudechannel.ClaudeHandler(c, resp, info)
		case types.RelayFormatGemini:
			if info.RelayFormat == types.RelayFormatOpenAIResponses {
				if info.IsStream {
					return geminichannel.GeminiResponsesStreamHandler(c, info, resp)
				}
				return geminichannel.GeminiResponsesHandler(c, info, resp)
			}
			if info.RelayFormat == types.RelayFormatGemini {
				if info.IsStream {
					return geminichannel.GeminiTextGenerationStreamHandler(c, info, resp)
				}
				return geminichannel.GeminiTextGenerationHandler(c, info, resp)
			}
			if info.IsStream {
				return geminichannel.GeminiChatStreamHandler(c, info, resp)
			}
			return geminichannel.GeminiChatHandler(c, info, resp)
		}
	}
	value, err := adaptor.DoResponse(c, resp, info)
	usage, ok := value.(*dto.Usage)
	if err != nil {
		return usage, err
	}
	if !ok {
		return nil, hosttypes.NewError(fmt.Errorf("unexpected accounting usage %T", value), hosttypes.ErrorCodeBadResponseBody)
	}
	return usage, nil
}
