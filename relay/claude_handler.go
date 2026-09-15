package relay

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"strings"
)

func ClaudeHelper(c *gin.Context, info *relaycommon.RelayInfo) *hosttypes.NewAPIError {
	return executeText(c, info)
}

func applyClaudeLeadingSystemPrompt(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) {
	if request == nil || info == nil {
		return
	}
	// The route-injected prompt always leads, so it outranks both the channel
	// system prompt and any system prompt the client sent.
	leadingPrompt := info.LeadingSystemPrompt(request.System != nil)
	if leadingPrompt == "" {
		return
	}
	if request.System == nil {
		request.SetStringSystem(leadingPrompt)
		return
	}

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
	if request.IsStringSystem() {
		existing := strings.TrimSpace(request.GetStringSystem())
		if existing == "" {
			request.SetStringSystem(leadingPrompt)
		} else {
			request.SetStringSystem(leadingPrompt + "\n" + existing)
		}
		return
	}

	systemContents := request.ParseSystem()
	newSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
	newSystem.SetText(leadingPrompt)
	if len(systemContents) == 0 {
		request.System = []dto.ClaudeMediaMessage{newSystem}
		return
	}
	request.System = append([]dto.ClaudeMediaMessage{newSystem}, systemContents...)
}
