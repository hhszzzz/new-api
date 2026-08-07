package relay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeStreamResultDeliveredTerminalWinsLateCancellation(t *testing.T) {
	t.Parallel()

	status := relaycommon.NewStreamStatus()
	status.MarkTerminalSuccess()
	status.MarkTerminalDelivered()
	status.MarkClientGone(context.Canceled)
	info := &relaycommon.RelayInfo{IsStream: true, StreamStatus: status}
	upstreamErr := types.NewOpenAIError(errors.New("late context cancellation"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	assert.Nil(t, normalizeStreamResult(nil, info, upstreamErr))
}

func TestNormalizeStreamResultClientGoneBeforeDeliveryAlwaysRefunds(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	cancel()

	status := relaycommon.NewStreamStatus()
	status.MarkResponseCommitted()
	status.MarkSemanticOutput()
	status.MarkUsageComplete()
	status.MarkClientGone(context.Canceled)
	status.SetDrainResult(relaycommon.StreamDrainCompleted)
	info := &relaycommon.RelayInfo{IsStream: true, StreamStatus: status}

	apiErr := normalizeStreamResult(c, info, nil)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeClientDisconnected, apiErr.GetErrorCode())
	assert.Equal(t, 499, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, types.IsRecordErrorLog(apiErr))

	snapshot := status.Snapshot()
	assert.True(t, snapshot.ResponseCommitted)
	assert.True(t, snapshot.SemanticOutput)
	assert.True(t, snapshot.UsageComplete, "drained usage is diagnostic only and must not turn the result into a charge")
	assert.False(t, snapshot.TerminalDelivered)
}

func TestNormalizeStreamResultPreservesUpstreamFailureWithoutDisconnect(t *testing.T) {
	t.Parallel()

	status := relaycommon.NewStreamStatus()
	status.MarkTerminalFailure(errors.New("response.failed"))
	info := &relaycommon.RelayInfo{IsStream: true, StreamStatus: status}
	upstreamErr := types.NewOpenAIError(errors.New("provider failed"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	assert.Same(t, upstreamErr, normalizeStreamResult(nil, info, upstreamErr))
}
