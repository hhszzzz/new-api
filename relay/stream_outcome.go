package relay

import (
	"context"
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// normalizeStreamResult applies the downstream-delivery contract before quota
// settlement. A successful terminal delivery wins over a concurrent late
// cancellation; cancellation before terminal delivery is always a non-retryable
// client outcome and therefore refunds any pre-consume.
func normalizeStreamResult(c *gin.Context, info *relaycommon.RelayInfo, apiErr *hosttypes.NewAPIError) *hosttypes.NewAPIError {
	if info == nil || !info.IsStream || info.StreamStatus == nil {
		return apiErr
	}
	snapshot := info.StreamStatus.Snapshot()
	if snapshot.TerminalDelivered {
		return nil
	}
	if !snapshot.ClientGone {
		return apiErr
	}
	endErr := snapshot.EndError
	if endErr == nil && c != nil && c.Request != nil {
		endErr = c.Request.Context().Err()
	}
	if endErr == nil {
		endErr = context.Canceled
	}
	return hosttypes.NewOpenAIError(
		fmt.Errorf("client disconnected before stream terminal delivery: %w", endErr),
		hosttypes.ErrorCodeClientDisconnected,
		499,
		hosttypes.ErrOptionWithSkipRetry(),
		hosttypes.ErrOptionWithNoRecordErrorLog(),
	)
}
