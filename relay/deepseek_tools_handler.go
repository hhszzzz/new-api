package relay

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// DeepSeekToolsHelper relays the DeepSeek Search and Fetch APIs like
// /v1/alpha/search: the raw body is forwarded, the response is returned
// unchanged, and the call is billed at the routing name's request price with
// no tokens.
func DeepSeekToolsHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *hosttypes.NewAPIError) {
	info.InitChannelMeta(c)

	switch info.ChannelType {
	case constant.ChannelTypeDeepSeek,
		constant.ChannelTypeNewAPI,
		constant.ChannelTypeSub2API,
		constant.ChannelTypeAdvancedCustom:
	default:
		// Allow retry onto another channel that may support this endpoint.
		return hosttypes.NewError(
			fmt.Errorf("channel does not support %s", c.Request.URL.Path),
			hosttypes.ErrorCodeInvalidRequest,
		)
	}

	request, ok := info.Request.(*dto.DeepSeekToolsRequest)
	if !ok {
		return hosttypes.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected *dto.DeepSeekToolsRequest, got %T", info.Request),
			hosttypes.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			hosttypes.ErrOptionWithSkipRetry(),
		)
	}

	err := helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeChannelModelMappedError, hosttypes.ErrOptionWithSkipRetry())
	}

	jsonData, err := buildAlphaSearchRequestBody(request.RawBody, info.OriginModelName, info.UpstreamModelName)
	if err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
	}

	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return newAPIErrorFromParamOverride(err)
		}
	}

	logger.LogDebug(c, "requestBody: %s", jsonData)
	body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
	}
	defer closer.Close()

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return hosttypes.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), hosttypes.ErrorCodeInvalidApiType, hosttypes.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	resp, err := adaptor.DoRequest(c, info, body)
	if err != nil {
		return hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	if resp == nil || resp.WebSocket != nil || resp.Response == nil || resp.Response.Body == nil {
		return hosttypes.NewOpenAIError(errors.New("invalid http response"), hosttypes.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResp := resp.Response
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	responseBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return hosttypes.NewOpenAIError(err, hosttypes.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError, hosttypes.ErrOptionWithSkipRetry())
	}
	service.IOCopyBytesGracefully(c, httpResp, responseBody)

	// DeepSeek bills a successful request only when it returns something: a
	// non-empty results array for Search, page content for Fetch. A billable
	// call settles at the routing name's request price; any other response
	// releases the reservation.
	var billable bool
	switch info.RelayMode {
	case relayconstant.RelayModeToolsFetch:
		billable = gjson.GetBytes(responseBody, "content").String() != ""
	default:
		results := gjson.GetBytes(responseBody, "results")
		billable = results.IsArray() && len(results.Array()) > 0
	}
	if !billable {
		if info.Billing != nil {
			info.Billing.Refund(c)
		}
		return nil
	}

	service.PostTextConsumeQuota(c, info, &dto.Usage{}, nil)
	return nil
}
