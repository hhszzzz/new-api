package middleware

import (
	"errors"
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// PrepareResponsesWebSocketRequest applies the model authorization and routing
// work that Distribute normally performs after reading an HTTP request body.
func PrepareResponsesWebSocketRequest(c *gin.Context, modelName string, requestBody []byte) *hosttypes.NewAPIError {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return hosttypes.NewErrorWithStatusCode(errors.New("invalid responses websocket request context"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	if modelName == "" {
		return hosttypes.NewErrorWithStatusCode(errors.New("model is required"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)

	matchName := ratio_setting.FormatMatchingModelName(modelName)
	if common.GetContextKeyBool(c, constant.ContextKeyUserModelLimitEnabled) {
		allowed, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyUserModelLimit)
		if !allowed[matchName] {
			return hosttypes.NewErrorWithStatusCode(errors.New("The requested model does not exist or you do not have access to it"), hosttypes.ErrorCodeModelNotFound, http.StatusNotFound, hosttypes.ErrOptionWithSkipRetry())
		}
	}
	if common.GetContextKeyBool(c, constant.ContextKeyUserModelBlocklistEnabled) {
		blocked, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyUserModelBlocklist)
		if blocked[matchName] {
			return hosttypes.NewErrorWithStatusCode(errors.New("The requested model does not exist or you do not have access to it"), hosttypes.ErrorCodeModelNotFound, http.StatusNotFound, hosttypes.ErrOptionWithSkipRetry())
		}
	}
	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		allowed, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
		if !TokenModelLimitAllows(allowed, modelName) {
			return hosttypes.NewErrorWithStatusCode(fmt.Errorf("token is not allowed to use model %s", modelName), hosttypes.ErrorCodeAccessDenied, http.StatusForbidden, hosttypes.ErrOptionWithSkipRetry())
		}
	}

	userGroups := common.GetContextKeyStringSlice(c, constant.ContextKeyUserGroups)
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if usingGroup == "" && len(userGroups) > 0 {
		usingGroup = userGroups[0]
		common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
	}
	if usingGroup != "auto" && !groupAllowsRequestClient(c, usingGroup) {
		return hosttypes.NewErrorWithStatusCode(errors.New("group does not allow this client"), hosttypes.ErrorCodeAccessDenied, http.StatusForbidden, hosttypes.ErrOptionWithSkipRetry())
	}
	if _, err := applyUserModelRoute(c, modelName, usingGroup); err != nil {
		return hosttypes.NewErrorWithStatusCode(errors.New("user model route is temporarily unavailable"), hosttypes.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, hosttypes.ErrOptionWithSkipRetry())
	}

	common.SetContextKey(c, constant.ContextKeyProtocolIncompatibleReason, nil)
	common.SetContextKey(c, constant.ContextKeyRequestFeatureSet, nil)
	binding, err := protocolstate.ResolveSelectionBinding(c, c.Request.URL.Path, modelName, requestBody)
	if err != nil {
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	common.SetContextKey(c, constant.ContextKeyProtocolStateBinding, binding)
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
	return nil
}

func NewResponsesWebSocketRetryParam(c *gin.Context, modelName string) *service.RetryParam {
	selectionModel := routeSelectionModel(c, modelName)
	selectionGroup := routeSelectionGroup(c, common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
	baseFilter := BuildChannelCandidateFilter(c, selectionModel)
	return &service.RetryParam{
		Ctx:               c,
		TokenGroup:        selectionGroup,
		ModelName:         selectionModel,
		RequestPath:       c.Request.URL.Path,
		AllowedChannelIds: routeSelectionChannelIds(c),
		AllowedGroups:     routeSelectionExecutionGroups(c),
		CandidateFilter: func(channel *model.Channel) bool {
			return (baseFilter == nil || baseFilter(channel)) && channelSupportsRequestPath(channel, c.Request.URL.Path, selectionModel)
		},
		CandidateClassifier: func(channel *model.Channel) model.ChannelCandidateClass {
			if responsesWebSocketNativePlan(c, channel, selectionModel) == nil {
				return model.ChannelCandidateIncompatible
			}
			return model.ChannelCandidateNative
		},
		Retry: common.GetPointer(0),
	}
}

func SelectResponsesWebSocketChannel(c *gin.Context, modelName string, retryParam *service.RetryParam) (*model.Channel, *hosttypes.NewAPIError) {
	if retryParam == nil {
		return nil, hosttypes.NewError(errors.New("invalid responses websocket retry parameters"), hosttypes.ErrorCodeGetChannelFailed, hosttypes.ErrOptionWithSkipRetry())
	}
	selectionModel := retryParam.ModelName
	selectionGroup := retryParam.TokenGroup

	if channelIDRaw, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
		channelID, ok := channelIDRaw.(string)
		if !ok {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("invalid specified channel id"), hosttypes.ErrorCodeGetChannelFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		id, err := strconv.Atoi(channelID)
		if err != nil {
			return nil, hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeGetChannelFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		channel, err := model.GetChannelById(id, true)
		if err != nil {
			return nil, hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeGetChannelFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		if !channel.IsSchedulableAt(time.Now()) {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("specified channel is disabled or outside its schedule"), hosttypes.ErrorCodeGetChannelFailed, http.StatusForbidden, hosttypes.ErrOptionWithSkipRetry())
		}
		if err := validateSelectedRouteChannel(c, channel, c.Request.URL.Path); err != nil || !responsesWebSocketChannelAllowed(retryParam, channel) {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("specified channel cannot serve native Responses WebSocket requests"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		if apiErr := setupResponsesWebSocketChannel(c, channel, modelName, selectionModel); apiErr != nil {
			return nil, apiErr
		}
		return channel, nil
	}

	if binding, ok := common.GetContextKeyType[*protocolstate.SelectionBinding](c, constant.ContextKeyProtocolStateBinding); ok && binding != nil && binding.ChannelID > 0 {
		if binding.UpstreamProtocol != "" && binding.UpstreamProtocol != channelcompat.ProtocolResponses {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("the referenced response is not bound to a native Responses channel"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		bound, err := model.CacheGetChannel(binding.ChannelID)
		if err != nil || bound == nil || !bound.IsSchedulableAt(time.Now()) || !responsesWebSocketChannelAllowed(retryParam, bound) {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("the channel bound to the referenced response cannot serve Responses WebSocket requests"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		resolvedGroup, groupUsable := resolveAffinitySelectionGroup(c, selectionGroup, selectionModel, bound.Id)
		if !routeChannelAllowed(c, bound.Id) || !groupUsable {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("the channel bound to the referenced response is unavailable"), hosttypes.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, hosttypes.ErrOptionWithSkipRetry())
		}
		commitRouteSelectionGroup(c, selectionGroup, resolvedGroup)
		if apiErr := setupResponsesWebSocketChannel(c, bound, modelName, selectionModel); apiErr != nil {
			return nil, apiErr
		}
		return bound, nil
	}

	channel, selectedGroup, selectErr := service.SelectChannelForRequest(c, selectionModel, retryParam)
	if selectErr != nil {
		message := selectErr.Message
		if selectErr.MessageID != "" {
			message = i18n.T(c, selectErr.MessageID, selectErr.Params)
		}
		return nil, hosttypes.NewErrorWithStatusCode(errors.New(message), selectErr.Code, selectErr.StatusCode, hosttypes.ErrOptionWithSkipRetry())
	}
	if selectedGroup != "" {
		commitRouteSelectionGroup(c, selectionGroup, selectedGroup)
	}
	if apiErr := setupResponsesWebSocketChannel(c, channel, modelName, selectionModel); apiErr != nil {
		return nil, apiErr
	}
	return channel, nil
}

func responsesWebSocketChannelAllowed(retryParam *service.RetryParam, channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	if !retryParam.AllowsChannel(channel) {
		return false
	}
	return retryParam.CandidateClassifier == nil || retryParam.CandidateClassifier(channel) == model.ChannelCandidateNative
}

// NewResponsesBridgeRetryParam mirrors the HTTP distributor's candidate policy:
// convertible channels stay eligible, so a Responses WebSocket connection can be
// served by bridging each response.create onto the HTTP relay pipeline.
func NewResponsesBridgeRetryParam(c *gin.Context, modelName string) *service.RetryParam {
	selectionModel := routeSelectionModel(c, modelName)
	selectionGroup := routeSelectionGroup(c, common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
	return &service.RetryParam{
		Ctx:                 c,
		TokenGroup:          selectionGroup,
		ModelName:           selectionModel,
		RequestPath:         c.Request.URL.Path,
		AllowedChannelIds:   routeSelectionChannelIds(c),
		AllowedGroups:       routeSelectionExecutionGroups(c),
		CandidateFilter:     BuildChannelCandidateFilter(c, selectionModel),
		CandidateClassifier: BuildChannelCandidateClassifier(c, selectionModel),
		Retry:               common.GetPointer(0),
	}
}

// SelectResponsesBridgeChannel selects a channel for a Responses WebSocket
// logical request that will be executed over HTTP. Unlike the native WebSocket
// selection it accepts convertible channels and non-Responses state bindings;
// SetupContextForSelectedChannel applies the protocol plan exactly as the HTTP
// distributor does.
func SelectResponsesBridgeChannel(c *gin.Context, modelName string, retryParam *service.RetryParam) (*model.Channel, *hosttypes.NewAPIError) {
	if retryParam == nil {
		return nil, hosttypes.NewError(errors.New("invalid responses websocket retry parameters"), hosttypes.ErrorCodeGetChannelFailed, hosttypes.ErrOptionWithSkipRetry())
	}
	selectionModel := retryParam.ModelName
	selectionGroup := retryParam.TokenGroup

	if channelIDRaw, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
		channelID, ok := channelIDRaw.(string)
		if !ok {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("invalid specified channel id"), hosttypes.ErrorCodeGetChannelFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		id, err := strconv.Atoi(channelID)
		if err != nil {
			return nil, hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeGetChannelFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		channel, err := model.GetChannelById(id, true)
		if err != nil {
			return nil, hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeGetChannelFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		if !channel.IsSchedulableAt(time.Now()) {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("specified channel is disabled or outside its schedule"), hosttypes.ErrorCodeGetChannelFailed, http.StatusForbidden, hosttypes.ErrOptionWithSkipRetry())
		}
		if err := validateSelectedRouteChannel(c, channel, c.Request.URL.Path); err != nil || !responsesBridgeChannelAllowed(retryParam, channel) {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("specified channel cannot serve Responses requests"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		if apiErr := SetupContextForSelectedChannel(c, channel, modelName, true); apiErr != nil {
			return nil, apiErr
		}
		return channel, nil
	}

	if binding, ok := common.GetContextKeyType[*protocolstate.SelectionBinding](c, constant.ContextKeyProtocolStateBinding); ok && binding != nil && binding.ChannelID > 0 {
		bound, err := model.CacheGetChannel(binding.ChannelID)
		if err != nil || bound == nil || !bound.IsSchedulableAt(time.Now()) ||
			!responsesBridgeChannelAllowed(retryParam, bound) ||
			!channelSupportsRequestPath(bound, c.Request.URL.Path, selectionModel) {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("the channel bound to the referenced response cannot serve Responses requests"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
		}
		resolvedGroup, groupUsable := resolveAffinitySelectionGroup(c, selectionGroup, selectionModel, bound.Id)
		if !routeChannelAllowed(c, bound.Id) || !groupUsable {
			return nil, hosttypes.NewErrorWithStatusCode(errors.New("the channel bound to the referenced response is unavailable"), hosttypes.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, hosttypes.ErrOptionWithSkipRetry())
		}
		commitRouteSelectionGroup(c, selectionGroup, resolvedGroup)
		if apiErr := SetupContextForSelectedChannel(c, bound, modelName, true); apiErr != nil {
			return nil, apiErr
		}
		return bound, nil
	}

	channel, selectedGroup, selectErr := service.SelectChannelForRequest(c, selectionModel, retryParam)
	if selectErr != nil {
		message := selectErr.Message
		if selectErr.MessageID != "" {
			message = i18n.T(c, selectErr.MessageID, selectErr.Params)
		}
		return nil, hosttypes.NewErrorWithStatusCode(errors.New(message), selectErr.Code, selectErr.StatusCode, hosttypes.ErrOptionWithSkipRetry())
	}
	if selectedGroup != "" {
		commitRouteSelectionGroup(c, selectionGroup, selectedGroup)
	}
	if apiErr := SetupContextForSelectedChannel(c, channel, modelName, true); apiErr != nil {
		return nil, apiErr
	}
	return channel, nil
}

func responsesBridgeChannelAllowed(retryParam *service.RetryParam, channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	if !retryParam.AllowsChannel(channel) {
		return false
	}
	return channelMatchesCandidateClassifier(channel, retryParam.CandidateClassifier)
}

func responsesWebSocketNativePlan(c *gin.Context, channel *model.Channel, modelName string) *channelcompat.ProtocolPlan {
	if channel == nil {
		return nil
	}
	if allowed, _ := model.ChannelSatisfiesFilters(channel, modelName, []dto.ChannelFilter{{Kind: dto.FilterResponsesWebSocket}}); !allowed {
		return nil
	}
	plans := channelcompat.PlansForRequest(channel, channelcompat.ProtocolResponses, modelName, c.Request.URL.Path, requestProtocolFeatures(c, channelcompat.ProtocolResponses))
	for i := range plans {
		if plans[i].Status == channelcompat.StatusNative && plans[i].UpstreamProtocol == channelcompat.ProtocolResponses {
			plan := plans[i]
			return &plan
		}
	}
	return nil
}

// RestoreResponsesWebSocketChannel rechecks the current route and group while
// retaining the credential of an established upstream connection.
func RestoreResponsesWebSocketChannel(c *gin.Context, channel *model.Channel, publicModel, lockedGroup string) *hosttypes.NewAPIError {
	retry := NewResponsesWebSocketRetryParam(c, publicModel)
	if err := validateSelectedRouteChannel(c, channel, c.Request.URL.Path); err != nil || !responsesWebSocketChannelAllowed(retry, channel) {
		return hosttypes.NewErrorWithStatusCode(errors.New("the connection channel is no longer allowed for this request"), hosttypes.ErrorCodeAccessDenied, http.StatusForbidden, hosttypes.ErrOptionWithSkipRetry())
	}
	if binding, ok := common.GetContextKeyType[*protocolstate.SelectionBinding](c, constant.ContextKeyProtocolStateBinding); ok && binding != nil && binding.ChannelID > 0 && binding.ChannelID != channel.Id {
		return hosttypes.NewErrorWithStatusCode(errors.New("the referenced response is bound to a different channel"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	if _, pinned, _ := service.GetChannelConstraints(c).ResolvedPin(); !pinned {
		group, allowed := resolveAffinitySelectionGroup(c, retry.TokenGroup, retry.ModelName, channel.Id)
		if !allowed || group != lockedGroup {
			return hosttypes.NewErrorWithStatusCode(errors.New("the connection group is no longer allowed"), hosttypes.ErrorCodeAccessDenied, http.StatusForbidden, hosttypes.ErrOptionWithSkipRetry())
		}
		commitRouteSelectionGroup(c, retry.TokenGroup, group)
	}
	if err := applySelectedChannelCompatibility(c, channel, retry.ModelName); err != nil {
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	return nil
}

func setupResponsesWebSocketChannel(c *gin.Context, channel *model.Channel, publicModel, selectionModel string) *hosttypes.NewAPIError {
	plan := responsesWebSocketNativePlan(c, channel, selectionModel)
	if plan == nil {
		return hosttypes.NewErrorWithStatusCode(errors.New("channel does not support native Responses WebSocket transport"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	if apiErr := SetupContextForSelectedChannel(c, channel, publicModel, true); apiErr != nil {
		return apiErr
	}
	common.SetContextKey(c, constant.ContextKeyProtocolAutoAttempt, nil)
	common.SetContextKey(c, constant.ContextKeyRequestProtocol, string(channelcompat.ProtocolResponses))
	common.SetContextKey(c, constant.ContextKeyUpstreamProtocol, string(channelcompat.ProtocolResponses))
	common.SetContextKey(c, constant.ContextKeyProtocolConverter, "")
	common.SetContextKey(c, constant.ContextKeyProtocolLossyConversion, "")
	common.SetContextKey(c, constant.ContextKeyProtocolStateMode, plan.StateMode)
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, *plan)
	return nil
}
