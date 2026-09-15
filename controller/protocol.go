package controller

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolpolicy"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
)

func GetProtocolCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"catalog":       relayconvert.Catalog(),
		"defaults":      hostdto.DefaultProtocolPolicy(),
		"global_policy": model_setting.GetGlobalSettings().EffectiveProtocolPolicy(),
	}})
}

func NormalizeProtocolConfiguration(c *gin.Context) {
	var channel model.Channel
	if err := c.ShouldBindJSON(&channel); err != nil {
		common.ApiError(c, err)
		return
	}
	settings, differences, err := protocolpolicy.NormalizeChannel(&channel, *model_setting.GetGlobalSettings())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"settings": settings, "differences": differences}})
}

func PreviewProtocolPlan(c *gin.Context) {
	var input struct {
		Channel  model.Channel         `json:"channel"`
		Protocol relayconvert.Protocol `json:"protocol"`
		Model    string                `json:"model"`
		Path     string                `json:"path"`
		Request  json.RawMessage       `json:"request"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	features, err := channelcompat.ExtractRequestFeatureSet(input.Protocol, input.Request)
	if err != nil {
		common.ApiError(c, errors.New("invalid protocol request"))
		return
	}
	if input.Path == "" {
		input.Path = relayconvert.CanonicalPath(input.Protocol, input.Model)
	}
	plans := channelcompat.PlansForRequest(&input.Channel, input.Protocol, input.Model, input.Path, features)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": plans})
}

func PreviewProtocolMigration(c *gin.Context) {
	manifest, err := protocolpolicy.Preflight(model.DB)
	if err != nil {
		common.ApiError(c, errors.New("cannot read protocol configuration"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": manifest.Review()})
}
