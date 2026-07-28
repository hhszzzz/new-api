package relayconvert

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	sharedbridge "github.com/QuantumNous/new-api/service/relayconvert/internal/shared/bridge"
	"github.com/gin-gonic/gin"
)

// ResetProtocolBridgeContext drops response-conversion state owned by one
// upstream attempt. Request conversion will build fresh state for the newly
// selected channel while continuation replay state remains untouched.
func ResetProtocolBridgeContext(c *gin.Context) {
	if c == nil {
		return
	}
	sharedbridge.ResetContext(c)
	common.SetContextKey(c, constant.ContextKeyProtocolResponseStreamState, nil)
}
