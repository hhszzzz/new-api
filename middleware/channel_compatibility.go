package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
)

func BuildChannelCandidateFilter(c *gin.Context, modelName string) model.ChannelCandidateFilter {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil
	}
	protocol := channelcompat.DetectRequestProtocol(c.Request.URL.Path)
	requestPath := c.Request.URL.Path
	features := requestProtocolFeatures(c, protocol)
	modelName = strings.TrimSpace(modelName)
	client := requestClient(c)
	bridgeClassifierOwnsProtocolFiltering := model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled &&
		(protocol == channelcompat.ProtocolResponses || protocol == channelcompat.ProtocolMessages)
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
	if !model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled ||
		(protocol != channelcompat.ProtocolResponses && protocol != channelcompat.ProtocolMessages) {
		return nil
	}
	requestPath := c.Request.URL.Path
	features := requestProtocolFeatures(c, protocol)
	modelName = strings.TrimSpace(modelName)
	return func(channel *model.Channel) model.ChannelCandidateClass {
		plan := channelcompat.PlanForRequest(channel, protocol, modelName, requestPath, features)
		switch plan.Status {
		case channelcompat.StatusNative:
			return model.ChannelCandidateNative
		case channelcompat.StatusConvertible:
			return model.ChannelCandidateConvertible
		default:
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
	relayconvert.ResetProtocolBridgeContext(c)
	if !clientpolicy.IsChannelAllowed(channel, requestClient(c)) {
		return fmt.Errorf("channel %d does not allow client %s", channel.Id, requestClient(c))
	}
	protocol := channelcompat.DetectRequestProtocol(c.Request.URL.Path)
	if protocol == "" {
		return nil
	}
	plan := channelcompat.PlanForRequest(channel, protocol, modelName, c.Request.URL.Path, requestProtocolFeatures(c, protocol))
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
	common.SetContextKey(c, constant.ContextKeyProtocolStateMode, plan.StateMode)
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, plan)
	return nil
}
