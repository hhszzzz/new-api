package helper

import (
	"strings"

	"github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service/modelmapping"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func ModelMappedHelper(c *gin.Context, info *common.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &common.ChannelMeta{}
	}

	isResponsesCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact
	originModelName := info.OriginModelName
	mappingModelName := info.SelectionModelName()
	if isResponsesCompact && strings.HasSuffix(mappingModelName, ratio_setting.CompactModelSuffix) {
		mappingModelName = strings.TrimSuffix(mappingModelName, ratio_setting.CompactModelSuffix)
	}
	info.UpstreamModelName = mappingModelName
	info.IsModelMapped = info.HasUserModelRoute() && mappingModelName != strings.TrimSuffix(originModelName, ratio_setting.CompactModelSuffix)

	// map model name
	modelMapping := c.GetString("model_mapping")
	resolved, err := modelmapping.Resolve(modelMapping, mappingModelName)
	if err != nil {
		return err
	}
	if resolved.Mapped {
		info.IsModelMapped = true
	}
	info.UpstreamModelName = resolved.Model

	if isResponsesCompact {
		finalUpstreamModelName := mappingModelName
		if info.IsModelMapped && info.UpstreamModelName != "" {
			finalUpstreamModelName = info.UpstreamModelName
		}
		info.UpstreamModelName = finalUpstreamModelName
		if info.HasUserModelRoute() {
			info.BillingModelName = originModelName
		} else {
			info.BillingModelName = ratio_setting.WithCompactModelSuffix(finalUpstreamModelName)
		}
	}
	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
