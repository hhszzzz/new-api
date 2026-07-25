package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/gin-gonic/gin"
)

func BuildChannelCandidateFilter(c *gin.Context, modelName string) model.ChannelCandidateFilter {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil
	}
	protocol := channelcompat.DetectRequestProtocol(c.Request.URL.Path)
	requestPath := c.Request.URL.Path
	modelName = strings.TrimSpace(modelName)
	client := requestClient(c)
	return func(channel *model.Channel) bool {
		if !clientpolicy.IsChannelAllowed(channel, client) {
			return false
		}
		return protocol == "" || channelcompat.IsCompatible(channel, protocol, modelName, requestPath)
	}
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
	if !clientpolicy.IsChannelAllowed(channel, requestClient(c)) {
		return fmt.Errorf("channel %d does not allow client %s", channel.Id, requestClient(c))
	}
	protocol := channelcompat.DetectRequestProtocol(c.Request.URL.Path)
	if protocol == "" {
		return nil
	}
	compatibility := channelcompat.ForRequest(channel, protocol, modelName, c.Request.URL.Path)
	if compatibility.Status == channelcompat.StatusIncompatible {
		return fmt.Errorf(
			"channel %d does not support %s requests for model %s",
			channel.Id,
			protocol,
			modelName,
		)
	}
	common.SetContextKey(c, constant.ContextKeyRequestProtocol, string(protocol))
	common.SetContextKey(c, constant.ContextKeyUpstreamProtocol, string(compatibility.UpstreamProtocol))
	common.SetContextKey(c, constant.ContextKeyProtocolConverter, compatibility.Converter)
	return nil
}
