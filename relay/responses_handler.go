package relay

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"

	"github.com/gin-gonic/gin"
)

func ResponsesHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	clientStream := info.IsStream
	plan, hasPlan := selectedProtocolPlan(c)
	if !hasPlan {
		plan = channelcompat.ProtocolPlan{
			RequestProtocol:  channelcompat.ProtocolResponses,
			UpstreamProtocol: channelcompat.ProtocolResponses,
			Status:           channelcompat.StatusNative,
		}
	}
	if info.RelayMode == relayconstant.RelayModeResponsesCompact &&
		!protocolPlanRequiresConversion(plan) &&
		!common.IsResponsesCompactAPIType(info.ApiType) {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		// Only fields documented for POST /v1/responses/compact are forwarded:
		// model, input, instructions, previous_response_id, prompt_cache_key,
		// prompt_cache_options, prompt_cache_retention, service_tier.
		// Undocumented Codex-parity fields (tools, reasoning, text) are parsed
		// for client compatibility but intentionally not sent upstream.
		responsesReq = &dto.OpenAIResponsesRequest{
			Model:                req.Model,
			Input:                req.Input,
			Instructions:         req.Instructions,
			PreviousResponseID:   req.PreviousResponseID,
			ParallelToolCalls:    req.ParallelToolCalls,
			ServiceTier:          req.ServiceTier,
			PromptCacheKey:       req.PromptCacheKey,
			PromptCacheOptions:   req.PromptCacheOptions,
			PromptCacheRetention: req.PromptCacheRetention,
		}
		if protocolPlanRequiresConversion(plan) {
			// CC Switch-compatible compact bridging: a Chat/Messages upstream has
			// no compact endpoint, so treat the compact body as a regular Responses
			// request and retain the Codex tool/reasoning fields needed by the
			// converter. Native compact requests keep the documented field filter.
			responsesReq.Tools = req.Tools
			responsesReq.Reasoning = req.Reasoning
			responsesReq.Text = req.Text
		}
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected dto.OpenAIResponsesRequest or dto.OpenAIResponsesCompactionRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	request, err := common.DeepCopy(responsesReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptorForProtocol(info.ApiType, plan.UpstreamProtocol)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	if err := protocolstate.PrepareResponsesRequest(c, info, plan, request); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	restoreProtocolPlan := applyProtocolPlan(info, plan)
	defer restoreProtocolPlan()
	var requestBody io.Reader
	if info.ShouldPassThroughBody() &&
		!protocolPlanRequiresConversion(plan) &&
		!protocolPlanRequiresStructuredRequest(info, plan) &&
		!protocolstate.Active(c) &&
		!protocolstate.ResponsesRequestNormalized(c) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.NewReplayableBodyReader(storage)
	} else {
		// Most adaptors never compose system prompts for this endpoint, so apply
		// the configured layers here. The helper is guarded against reapplying,
		// which keeps adaptors that do compose them (Codex) from injecting twice.
		applyResponsesInstructionsIfNeeded(c, info, request)

		convertedRequest, err := convertRequestForProtocolPlan(c, info, adaptor, plan, request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for OpenAI Responses API
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		logger.LogDebug(c, "requestBody: %s", jsonData)
		body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		jsonData = nil
		requestBody = body
	}

	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	var usage any
	bufferedResponseHandled := false
	if resp != nil {
		httpResp = resp.(*http.Response)
		upstreamStream := service.ResponseIsEventStream(httpResp)
		info.IsStream = clientStream || upstreamStream

		if httpResp.StatusCode != http.StatusOK {
			newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
		if plan.SelectionMode == dto.ProtocolSelectionModeAuto {
			if envelopeError := service.DetectProtocolUnsupportedSuccessEnvelope(httpResp); envelopeError != nil {
				return envelopeError
			}
		}
		if clientStream && !upstreamStream && service.ResponseIsJSON(httpResp) {
			if err := helper.PromoteJSONResponseToSSE(httpResp, protocolFormatForPlan(plan)); err != nil {
				newAPIError = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
			upstreamStream = true
			info.IsStream = true
		}
		if !clientStream && upstreamStream {
			info.IsStream = false
			bufferedUsage, handled, bufferedError := handleBufferedStreamResponse(c, info, httpResp, protocolFormatForPlan(plan), statusCodeMappingStr)
			if bufferedError != nil {
				return bufferedError
			}
			if handled {
				bufferedResponseHandled = true
				usage = bufferedUsage
			}
		}
	}

	if !bufferedResponseHandled {
		usage, newAPIError = adaptor.DoResponse(c, httpResp, info)
		if newAPIError != nil {
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usageDto := usage.(*dto.Usage)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		_, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{})
		if err != nil {
			return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry(), types.ErrOptionWithStatusCode(http.StatusBadRequest))
		}
		service.PostTextConsumeQuota(c, info, usageDto, nil)
		return nil
	}

	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usageDto, "")
	} else {
		service.PostTextConsumeQuota(c, info, usageDto, nil)
	}
	return nil
}
