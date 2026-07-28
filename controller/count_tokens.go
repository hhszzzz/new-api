package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// CountTokens serves Anthropic's auxiliary endpoint without pre-consumption,
// settlement, or consumption-log side effects.
func CountTokens(c *gin.Context) {
	var (
		newAPIError *types.NewAPIError
		relayInfo   *relaycommon.RelayInfo
	)
	defer func() {
		if newAPIError == nil {
			return
		}
		logger.LogError(c, "count_tokens error: "+common.LocalLogPreview(newAPIError.Error()))
		publicMessage := relaycommon.RedactUserModelRouteText(newAPIError.Error(), relayInfo)
		newAPIError.SetMessage(common.MessageWithRequestId(publicMessage, c.GetString(common.RequestIdKey)))
		publicError := relaycommon.SanitizeUserModelRouteClaudeError(newAPIError.ToClaudeError(), relayInfo)
		statusCode := newAPIError.StatusCode
		if statusCode < 100 || statusCode > 599 {
			statusCode = http.StatusInternalServerError
		}
		c.JSON(statusCode, gin.H{
			"type":  "error",
			"error": publicError,
		})
	}()

	request, err := helper.GetAndValidateClaudeRequest(c)
	if err != nil {
		statusCode := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			statusCode = http.StatusRequestEntityTooLarge
		}
		newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, statusCode, types.ErrOptionWithSkipRetry())
		return
	}
	request.Stream = nil

	relayInfo, err = relaycommon.GenRelayInfo(c, types.RelayFormatClaude, request, nil)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed, types.ErrOptionWithSkipRetry())
		return
	}
	newAPIError = relay.CountTokensHelper(c, relayInfo)
}
