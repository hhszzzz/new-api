package relay

import (
	"errors"
	hosttypes "github.com/QuantumNous/new-api/types"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func newConvertRequestFailedError(c *gin.Context, info *relaycommon.RelayInfo, err error) *hosttypes.NewAPIError {
	// A request plugin's rejection or failure keeps its own status and code.
	var pluginErr *types.NewAPIError
	if errors.As(err, &pluginErr) && (pluginErr.GetErrorCode() == types.ErrorCodeRequestRejected || pluginErr.GetErrorCode() == types.ErrorCodeRequestPluginFailed) {
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCode(pluginErr.GetErrorCode()), pluginErr.StatusCode, hosttypes.ErrOptionWithSkipRetry())
	}
	var loss *types.ConversionLossError
	if errors.As(err, &loss) {
		info.RecordConversionDiagnostics(c, loss.Diagnostics)
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeConvertRequestFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	if kitreasoning.IsClientError(err) {
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeConvertRequestFailed, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	return hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
}
