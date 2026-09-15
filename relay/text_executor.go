package relay

import (
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
		candidate := &model.Channel{Id: info.ChannelId, Type: info.ChannelType, BaseURL: common.GetPointer(info.ChannelBaseUrl), OtherSettings: string(settings), Setting: common.GetPointer(string(channelSettings))}
		features, featureErr := channelcompat.ExtractRequestFeatureSet(relayconvert.ProtocolForFormat(info.RelayFormat), before)
		if featureErr != nil {
			return newConvertRequestFailedError(c, info, featureErr)
		}
		plan = channelcompat.PlanForRequest(candidate, relayconvert.ProtocolForFormat(info.RelayFormat), info.UpstreamModelName, c.Request.URL.Path, features)
		common.SetContextKey(c, constant.ContextKeyProtocolPlan, plan)
	}
	if plan.Status == channelcompat.StatusIncompatible {
		return newConvertRequestFailedError(c, info, fmt.Errorf("%s", plan.Reason))
	}
	if info.RelayMode == relayconstant.RelayModeResponsesCompact && !common.SupportsResponsesCompact(info.ChannelType, info.ApiType) {
		return newConvertRequestFailedError(c, info, fmt.Errorf("compact requires a native Responses compact upstream"))
	}
	if info.RelayMode == relayconstant.RelayModeCompletions && protocolPlanRequiresConversion(plan) {
		return newConvertRequestFailedError(c, info, fmt.Errorf("legacy completions require their native operation"))
	}
	info.ConversionLossPolicy = types.ConversionLossPolicySafe
	if plan.Conversion == hostdto.ProtocolConversionLossless {
		info.ConversionLossPolicy = types.ConversionLossPolicyStrict
	}
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
		if err := protocolstate.PrepareResponsesRequest(c, info, plan, req); err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
		applyResponsesInstructionsIfNeeded(c, info, req)
	case *dto.GeminiChatRequest:
		relayconvert.RecordGeminiReasoningEffort(req, info)
		applyGeminiLeadingSystemPrompt(c, info, req)
	}
	if info.RelayMode != relayconstant.RelayModeCompletions {
		restore := applyProtocolPlan(info, plan)
		defer restore()
	}
	var body io.Reader
	passthrough := info.ShouldPassThroughBody() && !protocolPlanRequiresConversion(plan) && !protocolPlanRequiresStructuredRequest(info, plan) && !protocolstate.Active(c) && !protocolstate.ResponsesRequestNormalized(c)
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
		if !protocolPlanRequiresConversion(plan) {
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
		outbound, closer, err := relaycommon.NewOutboundJSONBody(jsonBody)
		if err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
		defer closer.Close()
		body = outbound
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
		service.ResetStatusCode(apiError, statusMapping)
		return apiError
	}
	if usage == nil {
		return hosttypes.NewError(fmt.Errorf("text response returned no accounting usage"), hosttypes.ErrorCodeBadResponseBody)
	}
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		if _, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{}); err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeModelPriceError, hosttypes.ErrOptionWithSkipRetry())
		}
	}
	containsAudio := usage.CompletionTokenDetails.AudioTokens > 0 || usage.PromptTokensDetails.AudioTokens > 0
	audioPricing := ratio_setting.ContainsAudioRatio(info.OriginModelName) || ratio_setting.ContainsAudioCompletionRatio(info.OriginModelName)
	if containsAudio && audioPricing || info.RelayFormat == types.RelayFormatOpenAIResponses && strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usage, "")
	} else {
		service.PostTextConsumeQuota(c, info, usage, nil)
	}
	return nil
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
	if err != nil {
		return nil, err
	}
	usage, ok := value.(*dto.Usage)
	if !ok {
		return nil, hosttypes.NewError(fmt.Errorf("unexpected accounting usage %T", value), hosttypes.ErrorCodeBadResponseBody)
	}
	return usage, nil
}
