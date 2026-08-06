package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// PrepareResponsesWebSocketRequest applies the model authorization and routing
// work that Distribute normally performs after reading an HTTP request body.
func PrepareResponsesWebSocketRequest(c *gin.Context, modelName string, requestBody []byte) *types.NewAPIError {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return types.NewErrorWithStatusCode(errors.New("invalid responses websocket request context"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if modelName == "" {
		return types.NewErrorWithStatusCode(errors.New("model is required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)

	matchName := ratio_setting.FormatMatchingModelName(modelName)
	if common.GetContextKeyBool(c, constant.ContextKeyUserModelLimitEnabled) {
		allowed, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyUserModelLimit)
		if !allowed[matchName] {
			return types.NewErrorWithStatusCode(errors.New("The requested model does not exist or you do not have access to it"), types.ErrorCodeModelNotFound, http.StatusNotFound, types.ErrOptionWithSkipRetry())
		}
	}
	if common.GetContextKeyBool(c, constant.ContextKeyUserModelBlocklistEnabled) {
		blocked, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyUserModelBlocklist)
		if blocked[matchName] {
			return types.NewErrorWithStatusCode(errors.New("The requested model does not exist or you do not have access to it"), types.ErrorCodeModelNotFound, http.StatusNotFound, types.ErrOptionWithSkipRetry())
		}
	}
	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		allowed, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
		if !allowed[matchName] {
			return types.NewErrorWithStatusCode(fmt.Errorf("token is not allowed to use model %s", modelName), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
	}

	userGroups := common.GetContextKeyStringSlice(c, constant.ContextKeyUserGroups)
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if usingGroup == "" && len(userGroups) > 0 {
		usingGroup = userGroups[0]
		common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
	}
	if usingGroup != "auto" && !groupAllowsRequestClient(c, usingGroup) {
		return types.NewErrorWithStatusCode(errors.New("group does not allow this client"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	if _, err := applyUserModelRoute(c, modelName, usingGroup); err != nil {
		return types.NewErrorWithStatusCode(errors.New("user model route is temporarily unavailable"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}

	common.SetContextKey(c, constant.ContextKeyProtocolIncompatibleReason, nil)
	common.SetContextKey(c, constant.ContextKeyRequestFeatureSet, nil)
	binding, err := protocolstate.ResolveSelectionBinding(c, c.Request.URL.Path, modelName, requestBody)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
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

func SelectResponsesWebSocketChannel(c *gin.Context, modelName string, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if retryParam == nil {
		return nil, types.NewError(errors.New("invalid responses websocket retry parameters"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	selectionModel := retryParam.ModelName
	selectionGroup := retryParam.TokenGroup

	if channelIDRaw, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
		channelID, ok := channelIDRaw.(string)
		if !ok {
			return nil, types.NewErrorWithStatusCode(errors.New("invalid specified channel id"), types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		id, err := strconv.Atoi(channelID)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		channel, err := model.GetChannelById(id, true)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if !channel.IsSchedulableAt(time.Now()) {
			return nil, types.NewErrorWithStatusCode(errors.New("specified channel is disabled or outside its schedule"), types.ErrorCodeGetChannelFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
		if err := validateSelectedRouteChannel(c, channel, c.Request.URL.Path); err != nil || !responsesWebSocketChannelAllowed(retryParam, channel) {
			return nil, types.NewErrorWithStatusCode(errors.New("specified channel cannot serve native Responses WebSocket requests"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if apiErr := setupResponsesWebSocketChannel(c, channel, modelName, selectionModel); apiErr != nil {
			return nil, apiErr
		}
		return channel, nil
	}

	if binding, ok := common.GetContextKeyType[*protocolstate.SelectionBinding](c, constant.ContextKeyProtocolStateBinding); ok && binding != nil && binding.ChannelID > 0 {
		if binding.UpstreamProtocol != "" && binding.UpstreamProtocol != channelcompat.ProtocolResponses {
			return nil, types.NewErrorWithStatusCode(errors.New("the referenced response is not bound to a native Responses channel"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		bound, err := model.CacheGetChannel(binding.ChannelID)
		if err != nil || bound == nil || !bound.IsSchedulableAt(time.Now()) || !responsesWebSocketChannelAllowed(retryParam, bound) {
			return nil, types.NewErrorWithStatusCode(errors.New("the channel bound to the referenced response cannot serve Responses WebSocket requests"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		resolvedGroup, groupUsable := resolveAffinitySelectionGroup(c, selectionGroup, selectionModel, bound.Id)
		if !routeChannelAllowed(c, bound.Id) || !groupUsable {
			return nil, types.NewErrorWithStatusCode(errors.New("the channel bound to the referenced response is unavailable"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
		commitRouteSelectionGroup(c, selectionGroup, resolvedGroup)
		if apiErr := setupResponsesWebSocketChannel(c, bound, modelName, selectionModel); apiErr != nil {
			return nil, apiErr
		}
		return bound, nil
	}

	if retryParam.GetRetry() == 0 {
		if preferredChannelID, found := service.GetPreferredChannelByAffinity(c, selectionModel, selectionGroup); found {
			preferred, err := model.CacheGetChannel(preferredChannelID)
			if err == nil && preferred != nil && preferred.IsSchedulableAt(time.Now()) && responsesWebSocketChannelAllowed(retryParam, preferred) {
				resolvedGroup, groupUsable := resolveAffinitySelectionGroup(c, selectionGroup, selectionModel, preferred.Id)
				if routeChannelAllowed(c, preferred.Id) && groupUsable {
					commitRouteSelectionGroup(c, selectionGroup, resolvedGroup)
					service.MarkChannelAffinityUsed(c, resolvedGroup, preferred.Id)
					if apiErr := setupResponsesWebSocketChannel(c, preferred, modelName, selectionModel); apiErr != nil {
						return nil, apiErr
					}
					return preferred, nil
				}
			}
			service.ClearCurrentChannelAffinityCache(c)
		}
	}

	channel, selectedGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		statusCode := http.StatusServiceUnavailable
		errorCode := types.ErrorCodeGetChannelFailed
		if errors.Is(err, model.ErrNoCompatibleChannel) {
			statusCode = http.StatusBadRequest
			errorCode = types.ErrorCodeInvalidRequest
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("failed to select a native Responses WebSocket channel from group %s: %w", selectedGroup, err), errorCode, statusCode, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("no native Responses WebSocket channel is available in group %s", selectedGroup), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
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
func SelectResponsesBridgeChannel(c *gin.Context, modelName string, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if retryParam == nil {
		return nil, types.NewError(errors.New("invalid responses websocket retry parameters"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	selectionModel := retryParam.ModelName
	selectionGroup := retryParam.TokenGroup

	if channelIDRaw, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
		channelID, ok := channelIDRaw.(string)
		if !ok {
			return nil, types.NewErrorWithStatusCode(errors.New("invalid specified channel id"), types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		id, err := strconv.Atoi(channelID)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		channel, err := model.GetChannelById(id, true)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if !channel.IsSchedulableAt(time.Now()) {
			return nil, types.NewErrorWithStatusCode(errors.New("specified channel is disabled or outside its schedule"), types.ErrorCodeGetChannelFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
		if err := validateSelectedRouteChannel(c, channel, c.Request.URL.Path); err != nil || !responsesBridgeChannelAllowed(retryParam, channel) {
			return nil, types.NewErrorWithStatusCode(errors.New("specified channel cannot serve Responses requests"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
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
			return nil, types.NewErrorWithStatusCode(errors.New("the channel bound to the referenced response cannot serve Responses requests"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		resolvedGroup, groupUsable := resolveAffinitySelectionGroup(c, selectionGroup, selectionModel, bound.Id)
		if !routeChannelAllowed(c, bound.Id) || !groupUsable {
			return nil, types.NewErrorWithStatusCode(errors.New("the channel bound to the referenced response is unavailable"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
		commitRouteSelectionGroup(c, selectionGroup, resolvedGroup)
		if apiErr := SetupContextForSelectedChannel(c, bound, modelName, true); apiErr != nil {
			return nil, apiErr
		}
		return bound, nil
	}

	if retryParam.GetRetry() == 0 {
		if preferredChannelID, found := service.GetPreferredChannelByAffinity(c, selectionModel, selectionGroup); found {
			preferred, err := model.CacheGetChannel(preferredChannelID)
			if err == nil && preferred != nil && preferred.IsSchedulableAt(time.Now()) &&
				responsesBridgeChannelAllowed(retryParam, preferred) &&
				channelSupportsRequestPath(preferred, c.Request.URL.Path, selectionModel) {
				resolvedGroup, groupUsable := resolveAffinitySelectionGroup(c, selectionGroup, selectionModel, preferred.Id)
				if routeChannelAllowed(c, preferred.Id) && groupUsable {
					commitRouteSelectionGroup(c, selectionGroup, resolvedGroup)
					service.MarkChannelAffinityUsed(c, resolvedGroup, preferred.Id)
					if apiErr := SetupContextForSelectedChannel(c, preferred, modelName, true); apiErr != nil {
						return nil, apiErr
					}
					return preferred, nil
				}
			}
			service.ClearCurrentChannelAffinityCache(c)
		}
	}

	channel, selectedGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		if errors.Is(err, model.ErrNoCompatibleChannel) {
			message := err.Error()
			if reason, ok := common.GetContextKeyType[string](c, constant.ContextKeyProtocolIncompatibleReason); ok && reason != "" {
				message = fmt.Sprintf("%s: %s", message, reason)
			}
			return nil, types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("failed to select a channel for model %s from group %s: %w", selectionModel, selectedGroup, err), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("no channel is available for model %s in group %s", selectionModel, selectedGroup), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
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
	if channel == nil || (channel.Type != constant.ChannelTypeOpenAI && channel.Type != constant.ChannelTypeCodex) {
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

func setupResponsesWebSocketChannel(c *gin.Context, channel *model.Channel, publicModel, selectionModel string) *types.NewAPIError {
	plan := responsesWebSocketNativePlan(c, channel, selectionModel)
	if plan == nil {
		return types.NewErrorWithStatusCode(errors.New("channel does not support native Responses WebSocket transport"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
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
