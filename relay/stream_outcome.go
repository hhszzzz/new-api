package relay

import (
	"context"
	"fmt"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// normalizeStreamResult applies the downstream-delivery contract before quota
// settlement. A successful terminal delivery wins over a concurrent late
// cancellation; cancellation before terminal delivery is always a non-retryable
// client outcome and therefore refunds any pre-consume.
func normalizeStreamResult(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
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
	return types.NewOpenAIError(
		fmt.Errorf("client disconnected before stream terminal delivery: %w", endErr),
		types.ErrorCodeClientDisconnected,
		499,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}
