package relay

import (
	"errors"
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelaws "github.com/QuantumNous/new-api/relay/channel/aws"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/gin-gonic/gin"
)

const maxCountTokensResponseBytes int64 = 1 << 20

var countAWSInputTokens = channelaws.CountTokens

// CountTokensHelper implements Anthropic's token-counting endpoint without
// entering the billing or usage-log lifecycle.
func CountTokensHelper(c *gin.Context, info *relaycommon.RelayInfo) *hosttypes.NewAPIError {
	if info == nil {
		return hosttypes.NewErrorWithStatusCode(fmt.Errorf("relay info is nil"), hosttypes.ErrorCodeGenRelayInfoFailed, http.StatusInternalServerError, hosttypes.ErrOptionWithSkipRetry())
	}
	info.InitChannelMeta(c)
	info.IsStream = false
	common.SetContextKey(c, constant.ContextKeyIsStream, false)

	claudeRequest, ok := info.Request.(*dto.ClaudeRequest)
	if !ok {
		return hosttypes.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected *dto.ClaudeRequest, got %T", info.Request), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	request, err := common.DeepCopy(claudeRequest)
	if err != nil {
		return hosttypes.NewErrorWithStatusCode(fmt.Errorf("failed to copy count_tokens request: %w", err), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	request.Stream = nil
	request.Temperature = nil
	request.TopP = nil
	request.TopK = nil
	request.StopSequences = nil

	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeChannelModelMappedError, hosttypes.ErrOptionWithSkipRetry())
	}
	applyClaudeLeadingSystemPrompt(c, info, request)

	plan, hasPlan := selectedProtocolPlan(c)
	if !hasPlan {
		plan = channelcompat.ProtocolPlan{
			RequestProtocol:  channelcompat.ProtocolMessages,
			UpstreamProtocol: channelcompat.ProtocolMessages,
			Status:           channelcompat.StatusNative,
		}
	}

	if plan.Status == channelcompat.StatusNative &&
		plan.UpstreamProtocol == channelcompat.ProtocolMessages &&
		info.ApiType == constant.APITypeAws {
		tokens, awsError := countAWSInputTokens(c, info, request)
		if awsError == nil {
			c.JSON(http.StatusOK, dto.ClaudeCountTokensResponse{InputTokens: tokens})
			return nil
		}
		if !errors.Is(awsError.Err, channelaws.ErrCountTokensUnsupported) {
			return awsError
		}
	}

	canForwardNativeMessages := false
	switch info.ApiType {
	case constant.APITypeAnthropic,
		constant.APITypeAli,
		constant.APITypeDeepSeek,
		constant.APITypeMoonshot,
		constant.APITypeMiniMax,
		constant.APITypeVolcEngine,
		constant.APITypeZhipuV4,
		constant.APITypeSub2API,
		constant.APITypeNewAPI:
		canForwardNativeMessages = true
	}
	if plan.ExplicitCapabilities {
		switch info.ApiType {
		case constant.APITypeOpenAI,
			constant.APITypeOpenRouter,
			constant.APITypeXinference,
			constant.APITypeGemini:
			canForwardNativeMessages = true
		}
	}
	if info.ApiType == constant.APITypeAdvancedCustom && info.ChannelOtherSettings.AdvancedCustom != nil {
		route, matched := info.ChannelOtherSettings.AdvancedCustom.MatchPathForModel(c.Request.URL.Path, request.Model)
		target, targetErr := route.ResolveTarget()
		canForwardNativeMessages = matched && targetErr == nil && target == relayconvert.ProtocolMessages
	}

	if plan.Status == channelcompat.StatusNative &&
		plan.UpstreamProtocol == channelcompat.ProtocolMessages &&
		canForwardNativeMessages {
		adaptor := GetAdaptorForProtocol(info.ApiType, plan.UpstreamProtocol)
		if adaptor == nil {
			return hosttypes.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), hosttypes.ErrorCodeInvalidApiType, hosttypes.ErrOptionWithSkipRetry())
		}
		adaptor.Init(info)
		convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
		}
		requestData, err := common.Marshal(convertedRequest)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
		}
		requestData, err = relaycommon.RemoveDisabledFields(requestData, info.ChannelOtherSettings, info.ShouldPassThroughBody())
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
		}
		if len(info.ParamOverride) > 0 {
			requestData, err = relaycommon.ApplyParamOverrideWithRelayInfo(requestData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		requestBody, closer, err := relaycommon.NewOutboundJSONBody(requestData)
		if err != nil {
			return hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
		}
		defer closer.Close()

		responseValue, err := adaptor.DoRequest(c, info, requestBody)
		if err != nil {
			return hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}
		if responseValue == nil || responseValue.Response == nil {
			return hosttypes.NewOpenAIError(fmt.Errorf("invalid count_tokens upstream response %T", responseValue), hosttypes.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		response := responseValue.Response

		switch response.StatusCode {
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
			service.CloseResponseBodyGracefully(response)
		default:
			if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
				newAPIError := service.RelayErrorHandler(c.Request.Context(), response, false)
				service.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))
				return newAPIError
			}

			defer service.CloseResponseBodyGracefully(response)
			responseData, err := io.ReadAll(io.LimitReader(response.Body, maxCountTokensResponseBytes+1))
			if err != nil {
				return hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
			}
			if int64(len(responseData)) > maxCountTokensResponseBytes {
				return hosttypes.NewOpenAIError(fmt.Errorf("count_tokens upstream response exceeds %d bytes", maxCountTokensResponseBytes), hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			var upstream struct {
				InputTokens *int `json:"input_tokens"`
			}
			if err := common.Unmarshal(responseData, &upstream); err != nil {
				return hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if upstream.InputTokens == nil || *upstream.InputTokens < 0 {
				return hosttypes.NewOpenAIError(fmt.Errorf("count_tokens upstream response has invalid input_tokens"), hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			c.JSON(http.StatusOK, dto.ClaudeCountTokensResponse{InputTokens: *upstream.InputTokens})
			return nil
		}
	}

	tokens, err := service.EstimateRequestTokenForCount(c, request.GetTokenCountMeta(), info)
	if err != nil {
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeCountTokenFailed, http.StatusInternalServerError, hosttypes.ErrOptionWithSkipRetry())
	}
	c.JSON(http.StatusOK, dto.ClaudeCountTokensResponse{InputTokens: tokens})
	return nil
}
