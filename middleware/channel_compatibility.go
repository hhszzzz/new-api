package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/gin-gonic/gin"
)

type autoProtocolAttempt struct {
	Channel                *model.Channel
	ChannelID              int
	EffectiveUpstreamModel string
	RequestProtocol        channelcompat.Protocol
	ProtocolOrder          []channelcompat.Protocol
	CurrentIndex           int
	RetryPending           bool
}

func BuildChannelCandidateFilter(c *gin.Context, modelName string) model.ChannelCandidateFilter {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil
	}
	protocol := channelcompat.DetectRequestProtocol(c.Request.URL.Path)
	requestPath := c.Request.URL.Path
	features := requestProtocolFeatures(c, protocol)
	modelName = strings.TrimSpace(modelName)
	client := requestClient(c)
	bridgeClassifierOwnsProtocolFiltering := protocol == channelcompat.ProtocolResponses || protocol == channelcompat.ProtocolMessages
	return func(channel *model.Channel) bool {
		if !clientpolicy.IsChannelAllowed(channel, client) {
			return false
		}
		if bridgeClassifierOwnsProtocolFiltering {
			return true
		}
		return protocol == "" || channelcompat.PlanForRequest(channel, protocol, modelName, requestPath, features).Status != channelcompat.StatusIncompatible
	}
}

func BuildChannelCandidateClassifier(c *gin.Context, modelName string) model.ChannelCandidateClassifier {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil
	}
	protocol := channelcompat.DetectRequestProtocol(c.Request.URL.Path)
	if protocol != channelcompat.ProtocolResponses && protocol != channelcompat.ProtocolMessages {
		return nil
	}
	requestPath := c.Request.URL.Path
	features := requestProtocolFeatures(c, protocol)
	modelName = strings.TrimSpace(modelName)
	return func(channel *model.Channel) model.ChannelCandidateClass {
		plans := channelcompat.PlansForRequest(channel, protocol, modelName, requestPath, features)
		if len(plans) == 0 {
			return model.ChannelCandidateIncompatible
		}
		plan := plans[0]
		if plan.SelectionMode == dto.ProtocolSelectionModeAuto && plan.Status != channelcompat.StatusIncompatible {
			if preferred, found := channelcompat.LookupProtocolAffinity(channel, plan.EffectiveUpstreamModel, protocol); found {
				if preferredPlan, ok := findAutomaticProtocolPlan(plans, preferred); ok {
					plan = preferredPlan
				}
			} else if capabilities := channel.GetOtherSettings().ProtocolCapabilities; capabilities != nil {
				configuredProtocols, _ := capabilities.Resolve(plan.EffectiveUpstreamModel)
				if len(configuredProtocols) == 0 {
					// An unprobed automatic channel has no evidence that the entry
					// protocol is native. Keep it eligible, but do not let it outrank
					// a channel that explicitly declares native support.
					return model.ChannelCandidateConvertible
				}
			}
		}
		switch plan.Status {
		case channelcompat.StatusNative:
			return model.ChannelCandidateNative
		case channelcompat.StatusConvertible:
			return model.ChannelCandidateConvertible
		default:
			// Selection reports a single aggregate error when every candidate is
			// incompatible; remember one concrete reason so the client sees why.
			if plan.Reason != "" {
				if _, exists := common.GetContextKeyType[string](c, constant.ContextKeyProtocolIncompatibleReason); !exists {
					common.SetContextKey(c, constant.ContextKeyProtocolIncompatibleReason, plan.Reason)
				}
			}
			return model.ChannelCandidateIncompatible
		}
	}
}

func requestProtocolFeatures(c *gin.Context, protocol channelcompat.Protocol) channelcompat.RequestFeatureSet {
	if c == nil || protocol == "" {
		return channelcompat.RequestFeatureSet{}
	}
	if cached, ok := common.GetContextKeyType[channelcompat.RequestFeatureSet](c, constant.ContextKeyRequestFeatureSet); ok {
		return cached
	}
	features := channelcompat.RequestFeatureSet{}
	storage, err := common.GetBodyStorage(c)
	if err == nil {
		if body, bytesErr := storage.Bytes(); bytesErr == nil {
			if extracted, extractErr := channelcompat.ExtractRequestFeatureSet(protocol, body); extractErr == nil {
				features = extracted
			}
		}
	}
	common.SetContextKey(c, constant.ContextKeyRequestFeatureSet, features)
	return features
}

func requestClient(c *gin.Context) string {
	if c == nil {
		return clientpolicy.ClientUnknown
	}
	if client := common.GetContextKeyString(c, constant.ContextKeyClientName); client != "" {
		return client
	}
	client := clientpolicy.Detect(c.Request)
	common.SetContextKey(c, constant.ContextKeyClientName, client)
	return client
}

func groupAllowsRequestClient(c *gin.Context, group string) bool {
	return clientpolicy.IsGroupAllowed(group, requestClient(c))
}

func applySelectedChannelCompatibility(c *gin.Context, channel *model.Channel, modelName string) error {
	if c == nil || c.Request == nil || c.Request.URL == nil || channel == nil {
		return nil
	}
	c.Request = c.Request.WithContext(relayconvert.WithProtocolBridgeContext(c.Request.Context()))
	common.SetContextKey(c, constant.ContextKeyProtocolResponseStreamState, nil)
	if !clientpolicy.IsChannelAllowed(channel, requestClient(c)) {
		return fmt.Errorf("channel %d does not allow client %s", channel.Id, requestClient(c))
	}
	protocol := channelcompat.DetectRequestProtocol(c.Request.URL.Path)
	if protocol == "" {
		return nil
	}
	plans := channelcompat.PlansForRequest(channel, protocol, modelName, c.Request.URL.Path, requestProtocolFeatures(c, protocol))
	plan := plans[0]
	if plan.SelectionMode == dto.ProtocolSelectionModeAuto && plan.Status != channelcompat.StatusIncompatible {
		plan = selectAutomaticProtocolPlan(c, channel, plans)
	} else {
		common.SetContextKey(c, constant.ContextKeyProtocolAutoAttempt, nil)
	}
	if plan.Status == channelcompat.StatusIncompatible {
		reason := plan.Reason
		if reason == "" {
			reason = "protocol or request features are incompatible"
		}
		return fmt.Errorf(
			"channel %d does not support %s requests for model %s: %s",
			channel.Id,
			protocol,
			modelName,
			reason,
		)
	}
	common.SetContextKey(c, constant.ContextKeyRequestProtocol, string(protocol))
	common.SetContextKey(c, constant.ContextKeyUpstreamProtocol, string(plan.UpstreamProtocol))
	common.SetContextKey(c, constant.ContextKeyProtocolConverter, plan.RequestConverter)
	common.SetContextKey(c, constant.ContextKeyProtocolLossyConversion, strings.Join(plan.LossyContentTypes, ","))
	common.SetContextKey(c, constant.ContextKeyProtocolStateMode, plan.StateMode)
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, plan)
	return nil
}

func selectAutomaticProtocolPlan(c *gin.Context, channel *model.Channel, plans []channelcompat.ProtocolPlan) channelcompat.ProtocolPlan {
	firstPlan := plans[0]
	if attempt, ok := common.GetContextKeyType[*autoProtocolAttempt](c, constant.ContextKeyProtocolAutoAttempt); ok &&
		attempt != nil && attempt.ChannelID == channel.Id &&
		attempt.EffectiveUpstreamModel == firstPlan.EffectiveUpstreamModel &&
		attempt.RequestProtocol == firstPlan.RequestProtocol &&
		attempt.CurrentIndex >= 0 && attempt.CurrentIndex < len(attempt.ProtocolOrder) {
		if plan, found := findAutomaticProtocolPlan(plans, attempt.ProtocolOrder[attempt.CurrentIndex]); found {
			attempt.Channel = channel
			attempt.RetryPending = false
			common.SetContextKey(c, constant.ContextKeyProtocolAutoAttempt, attempt)
			return plan
		}
	}

	protocolOrder := make([]channelcompat.Protocol, 0, len(plans))
	for _, plan := range plans {
		if plan.Status == channelcompat.StatusIncompatible {
			continue
		}
		protocolOrder = append(protocolOrder, plan.UpstreamProtocol)
	}
	if preferred, found := channelcompat.LookupProtocolAffinity(channel, firstPlan.EffectiveUpstreamModel, firstPlan.RequestProtocol); found {
		protocolOrder = prioritizeAutomaticProtocol(protocolOrder, preferred)
	}
	if binding, ok := common.GetContextKeyType[*protocolstate.SelectionBinding](c, constant.ContextKeyProtocolStateBinding); ok &&
		binding != nil && binding.ChannelID == channel.Id && binding.UpstreamProtocol != "" {
		protocolOrder = prioritizeAutomaticProtocol(protocolOrder, binding.UpstreamProtocol)
	}

	attempt := &autoProtocolAttempt{
		Channel:                channel,
		ChannelID:              channel.Id,
		EffectiveUpstreamModel: firstPlan.EffectiveUpstreamModel,
		RequestProtocol:        firstPlan.RequestProtocol,
		ProtocolOrder:          protocolOrder,
	}
	common.SetContextKey(c, constant.ContextKeyProtocolAutoAttempt, attempt)
	if len(protocolOrder) > 0 {
		if plan, found := findAutomaticProtocolPlan(plans, protocolOrder[0]); found {
			return plan
		}
	}
	return firstPlan
}

func prioritizeAutomaticProtocol(protocols []channelcompat.Protocol, preferred channelcompat.Protocol) []channelcompat.Protocol {
	for index, protocol := range protocols {
		if protocol != preferred || index == 0 {
			continue
		}
		reordered := make([]channelcompat.Protocol, 0, len(protocols))
		reordered = append(reordered, preferred)
		reordered = append(reordered, protocols[:index]...)
		reordered = append(reordered, protocols[index+1:]...)
		return reordered
	}
	return protocols
}

func findAutomaticProtocolPlan(plans []channelcompat.ProtocolPlan, protocol channelcompat.Protocol) (channelcompat.ProtocolPlan, bool) {
	for _, plan := range plans {
		if plan.Status != channelcompat.StatusIncompatible && plan.UpstreamProtocol == protocol {
			return plan, true
		}
	}
	return channelcompat.ProtocolPlan{}, false
}

func AdvanceAutoProtocolAttempt(c *gin.Context, apiError *types.NewAPIError) bool {
	if c == nil || c.Writer == nil || c.Writer.Written() || !channelcompat.IsProtocolUnsupportedError(apiError) {
		return false
	}
	attempt, ok := common.GetContextKeyType[*autoProtocolAttempt](c, constant.ContextKeyProtocolAutoAttempt)
	if !ok || attempt == nil || attempt.CurrentIndex < 0 || attempt.CurrentIndex >= len(attempt.ProtocolOrder) {
		return false
	}
	plan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](c, constant.ContextKeyProtocolPlan)
	if !ok || plan.SelectionMode != dto.ProtocolSelectionModeAuto || plan.UpstreamProtocol != attempt.ProtocolOrder[attempt.CurrentIndex] {
		return false
	}

	channelcompat.ForgetProtocolAffinity(attempt.Channel, attempt.EffectiveUpstreamModel, attempt.RequestProtocol)
	if attempt.CurrentIndex+1 >= len(attempt.ProtocolOrder) {
		return false
	}
	attempt.CurrentIndex++
	attempt.RetryPending = true
	common.SetContextKey(c, constant.ContextKeyProtocolAutoAttempt, attempt)
	return true
}

func PendingAutoProtocolRetryChannelID(c *gin.Context) (int, bool) {
	if c == nil {
		return 0, false
	}
	attempt, ok := common.GetContextKeyType[*autoProtocolAttempt](c, constant.ContextKeyProtocolAutoAttempt)
	if !ok || attempt == nil || !attempt.RetryPending || attempt.ChannelID <= 0 {
		return 0, false
	}
	return attempt.ChannelID, true
}

func CommitAutoProtocolAffinity(c *gin.Context) {
	if c == nil || !protocolstate.AttemptCompleted(c) {
		return
	}
	attempt, ok := common.GetContextKeyType[*autoProtocolAttempt](c, constant.ContextKeyProtocolAutoAttempt)
	if !ok || attempt == nil || attempt.CurrentIndex < 0 || attempt.CurrentIndex >= len(attempt.ProtocolOrder) {
		return
	}
	plan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](c, constant.ContextKeyProtocolPlan)
	if !ok || plan.SelectionMode != dto.ProtocolSelectionModeAuto || plan.UpstreamProtocol != attempt.ProtocolOrder[attempt.CurrentIndex] {
		return
	}
	channelcompat.RememberProtocolAffinity(attempt.Channel, attempt.EffectiveUpstreamModel, attempt.RequestProtocol, plan.UpstreamProtocol)
}
