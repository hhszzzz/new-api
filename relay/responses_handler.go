package relay

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func ResponsesHelper(c *gin.Context, info *relaycommon.RelayInfo) *hosttypes.NewAPIError {
	isCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact
	apiErr := executeText(c, info)
	if !isCompact || apiErr == nil || c.Writer.Written() || !channelcompat.IsProtocolUnsupportedError(apiErr) {
		return apiErr
	}
	switch apiErr.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity, http.StatusNotImplemented:
	default:
		return apiErr
	}
	plan, ok := selectedProtocolPlan(c)
	if !ok || plan.CompactionMode != relayconvert.CompactionNative || plan.Conversion == hostdto.ProtocolConversionNative || plan.AdvancedCustomRoute != nil {
		return apiErr
	}
	if err := relayconvert.ValidateCompactionSummaryFeatures(plan.Features); err != nil {
		return apiErr
	}
	// The native endpoint rejected the operation before writing a response.
	// Reuse this request's pre-consumption; successful usage is settled once.
	plan.CompactionMode = relayconvert.CompactionSummary
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, plan)
	protocolstate.ResetAttempt(c)
	return executeText(c, info)
}

// ConsumeResponsesQuota applies the same settlement dispatch to HTTP and
// WebSocket Responses usage. Compact requests keep their separate repricing.
func ConsumeResponsesQuota(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usage, "")
		return
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
}
