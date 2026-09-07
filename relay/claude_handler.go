package relay

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

func ClaudeHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {

	info.InitChannelMeta(c)
	clientStream := info.IsStream

	claudeReq, ok := info.Request.(*dto.ClaudeRequest)

	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected *dto.ClaudeRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(claudeReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ClaudeRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ApplyReasoningModelSuffix(c, info, request); err != nil {
		return newConvertRequestFailedError(c, info, err)
	}

	plan, hasPlan := selectedProtocolPlan(c)
	if !hasPlan {
		plan = channelcompat.ProtocolPlan{
			RequestProtocol:  channelcompat.ProtocolMessages,
			UpstreamProtocol: channelcompat.ProtocolMessages,
			Status:           channelcompat.StatusNative,
		}
	}
	adaptor := GetAdaptorForProtocol(info.ApiType, plan.UpstreamProtocol)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	if request.MaxTokens == nil {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(request.Model))
		request.MaxTokens = &defaultMaxTokens
	}

	applyClaudeLeadingSystemPrompt(c, info, request)
	if err := protocolstate.PrepareMessagesRequest(c, info, plan, request); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	if !plan.ExplicitCapabilities &&
		!model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled &&
		!info.ShouldPassThroughBody() &&
		service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.UpstreamModelName) {
		usage, newApiErr := textRequestViaResponses(c, info, adaptor, request)
		newApiErr = normalizeStreamResult(c, info, newApiErr)
		if newApiErr != nil {
			return newApiErr
		}

		service.PostTextConsumeQuota(c, info, usage, nil)
		return nil
	}

	restoreProtocolPlan := applyProtocolPlan(info, plan)
	defer restoreProtocolPlan()

	var requestBody io.Reader
	if info.ShouldPassThroughBody() && !protocolPlanRequiresConversion(plan) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.NewReplayableBodyReader(storage)
	} else {
		convertedRequest, err := convertRequestForProtocolPlan(c, info, adaptor, plan, request)
		if err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for Claude API
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

	statusCodeMappingStr := c.GetString("status_code_mapping")
	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

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
			usage, handled, bufferedError := handleBufferedStreamResponse(c, info, httpResp, protocolFormatForPlan(plan), statusCodeMappingStr)
			if bufferedError != nil {
				return bufferedError
			}
			if handled {
				service.PostTextConsumeQuota(c, info, usage, nil)
				return nil
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	newAPIError = normalizeStreamResult(c, info, newAPIError)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}

func applyClaudeLeadingSystemPrompt(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) {
	if request == nil || info == nil {
		return
	}
	// The route-injected prompt always leads, so it outranks both the channel
	// system prompt and any system prompt the client sent.
	leadingPrompt := info.LeadingSystemPrompt(request.System != nil)
	if leadingPrompt == "" {
		return
	}
	if request.System == nil {
		request.SetStringSystem(leadingPrompt)
		return
	}

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
	if request.IsStringSystem() {
		existing := strings.TrimSpace(request.GetStringSystem())
		if existing == "" {
			request.SetStringSystem(leadingPrompt)
		} else {
			request.SetStringSystem(leadingPrompt + "\n" + existing)
		}
		return
	}

	systemContents := request.ParseSystem()
	newSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
	newSystem.SetText(leadingPrompt)
	if len(systemContents) == 0 {
		request.System = []dto.ClaudeMediaMessage{newSystem}
		return
	}
	request.System = append([]dto.ClaudeMediaMessage{newSystem}, systemContents...)
}
