package relay

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelaws "github.com/QuantumNous/new-api/relay/channel/aws"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/gin-gonic/gin"
)

const maxCountTokensResponseBytes int64 = 1 << 20

var countAWSInputTokens = channelaws.CountTokens

// CountTokensHelper implements Anthropic's token-counting endpoint without
// entering the billing or usage-log lifecycle.
func CountTokensHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("relay info is nil"), types.ErrorCodeGenRelayInfoFailed, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	info.InitChannelMeta(c)
	info.IsStream = false
	common.SetContextKey(c, constant.ContextKeyIsStream, false)

	claudeRequest, ok := info.Request.(*dto.ClaudeRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected *dto.ClaudeRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	request, err := common.DeepCopy(claudeRequest)
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("failed to copy count_tokens request: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	request.Stream = nil
	request.Temperature = nil
	request.TopP = nil
	request.TopK = nil
	request.StopSequences = nil

	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
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
		converter := strings.TrimSpace(route.Converter)
		canForwardNativeMessages = matched && (converter == "" || converter == relayconvert.ConverterNone)
	}

	if plan.Status == channelcompat.StatusNative &&
		plan.UpstreamProtocol == channelcompat.ProtocolMessages &&
		canForwardNativeMessages {
		adaptor := GetAdaptorForProtocol(info.ApiType, plan.UpstreamProtocol)
		if adaptor == nil {
			return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
		}
		adaptor.Init(info)
		convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		requestData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		requestData, err = relaycommon.RemoveDisabledFields(requestData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		if len(info.ParamOverride) > 0 {
			requestData, err = relaycommon.ApplyParamOverrideWithRelayInfo(requestData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		requestBody, size, closer, err := relaycommon.NewOutboundJSONBody(requestData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		info.UpstreamRequestBodySize = size

		responseValue, err := adaptor.DoRequest(c, info, requestBody)
		if err != nil {
			return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}
		response, ok := responseValue.(*http.Response)
		if !ok || response == nil {
			return types.NewOpenAIError(fmt.Errorf("invalid count_tokens upstream response %T", responseValue), types.ErrorCodeBadResponse, http.StatusBadGateway)
		}

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
				return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
			}
			if int64(len(responseData)) > maxCountTokensResponseBytes {
				return types.NewOpenAIError(fmt.Errorf("count_tokens upstream response exceeds %d bytes", maxCountTokensResponseBytes), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			var upstream struct {
				InputTokens *int `json:"input_tokens"`
			}
			if err := common.Unmarshal(responseData, &upstream); err != nil {
				return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if upstream.InputTokens == nil || *upstream.InputTokens < 0 {
				return types.NewOpenAIError(fmt.Errorf("count_tokens upstream response has invalid input_tokens"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			c.JSON(http.StatusOK, dto.ClaudeCountTokensResponse{InputTokens: *upstream.InputTokens})
			return nil
		}
	}

	tokens, err := service.EstimateRequestTokenForCount(c, request.GetTokenCountMeta(), info)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeCountTokenFailed, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	c.JSON(http.StatusOK, dto.ClaudeCountTokensResponse{InputTokens: tokens})
	return nil
}
