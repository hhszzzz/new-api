package common

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamStatusTerminalDeliveryOrdering(t *testing.T) {
	t.Parallel()

	t.Run("terminal delivered before cancel remains successful", func(t *testing.T) {
		status := NewStreamStatus()
		status.MarkTerminalSuccess()
		status.MarkTerminalDelivered()
		status.MarkClientGone(context.Canceled)

		snapshot := status.Snapshot()
		assert.Equal(t, StreamEndReasonDone, snapshot.EndReason)
		assert.True(t, snapshot.TerminalDelivered)
		assert.True(t, snapshot.ClientGone)
	})

	t.Run("cancel before terminal cannot become delivered", func(t *testing.T) {
		status := NewStreamStatus()
		status.MarkClientGone(context.Canceled)
		status.MarkTerminalSuccess()
		status.MarkTerminalDelivered()

		snapshot := status.Snapshot()
		assert.Equal(t, StreamEndReasonClientGone, snapshot.EndReason)
		assert.False(t, snapshot.TerminalDelivered)
		assert.Equal(t, StreamTerminalSuccess, snapshot.TerminalState)
	})
}

func TestStreamStatusDeterministicPrecedence(t *testing.T) {
	t.Parallel()

	readErr := errors.New("connection reset")
	status := NewStreamStatus()
	status.SetEndReason(StreamEndReasonEOF, nil)
	status.SetEndReason(StreamEndReasonScannerErr, readErr)
	status.SetEndReason(StreamEndReasonDone, nil)
	status.MarkClientGone(context.Canceled)

	snapshot := status.Snapshot()
	assert.Equal(t, StreamEndReasonClientGone, snapshot.EndReason)
	assert.ErrorIs(t, snapshot.EndError, context.Canceled)
}

func TestStreamStatusExplicitFailureIsSticky(t *testing.T) {
	t.Parallel()

	failure := errors.New("response.incomplete")
	status := NewStreamStatus()
	status.MarkTerminalFailure(failure)
	status.SetEndReason(StreamEndReasonDone, nil)
	status.MarkTerminalSuccess()
	status.MarkTerminalDelivered()

	snapshot := status.Snapshot()
	assert.Equal(t, StreamEndReasonProtocolError, snapshot.EndReason)
	assert.Equal(t, StreamTerminalFailure, snapshot.TerminalState)
	assert.ErrorIs(t, snapshot.EndError, failure)
	assert.False(t, snapshot.TerminalDelivered)
}

func TestStreamStatusWriteFailureBeatsEOF(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("broken pipe")
	status := NewStreamStatus()
	status.SetEndReason(StreamEndReasonEOF, nil)
	status.MarkWriteError(writeErr)

	snapshot := status.Snapshot()
	assert.Equal(t, StreamEndReasonHandlerStop, snapshot.EndReason)
	assert.ErrorIs(t, snapshot.EndError, writeErr)
}

func TestStreamStatusConcurrentFactsHaveStableClassification(t *testing.T) {
	t.Parallel()

	status := NewStreamStatus()
	reasons := []StreamEndReason{
		StreamEndReasonDone,
		StreamEndReasonTimeout,
		StreamEndReasonScannerErr,
		StreamEndReasonHandlerStop,
		StreamEndReasonEOF,
		StreamEndReasonPanic,
		StreamEndReasonPingFail,
	}

	var wg sync.WaitGroup
	for _, reason := range reasons {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status.SetEndReason(reason, nil)
		}()
	}
	wg.Wait()

	assert.Equal(t, StreamEndReasonPanic, status.Snapshot().EndReason)
}

func TestStreamStatusSnapshotContainsIndependentFacts(t *testing.T) {
	t.Parallel()

	status := NewStreamStatus()
	status.MarkResponseCommitted()
	status.MarkUsageComplete()
	status.MarkSemanticOutput()
	status.SetDrainResult(StreamDrainCompleted)
	status.RecordError("malformed optional event")

	snapshot := status.Snapshot()
	assert.True(t, snapshot.ResponseCommitted)
	assert.True(t, snapshot.UsageComplete)
	assert.True(t, snapshot.SemanticOutput)
	assert.Equal(t, StreamDrainCompleted, snapshot.DrainResult)
	assert.Equal(t, 1, snapshot.ErrorCount)
	require.Len(t, snapshot.Errors, 1)
	assert.Equal(t, "malformed optional event", snapshot.Errors[0].Message)

	// A caller cannot mutate the status through the immutable snapshot.
	snapshot.Errors[0].Message = "changed"
	assert.Equal(t, "malformed optional event", status.Snapshot().Errors[0].Message)
}

func TestStreamStatusRecordErrorCapsStoredEntries(t *testing.T) {
	t.Parallel()

	status := NewStreamStatus()
	for i := 0; i < 30; i++ {
		status.RecordError(fmt.Sprintf("error_%d", i))
	}

	snapshot := status.Snapshot()
	assert.Equal(t, 30, snapshot.ErrorCount)
	assert.Len(t, snapshot.Errors, maxStreamErrorEntries)
}

func TestStreamStatusRecordErrorConcurrent(t *testing.T) {
	t.Parallel()

	status := NewStreamStatus()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			status.RecordError(fmt.Sprintf("error_%d", index))
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, status.TotalErrorCount())
	assert.Len(t, status.Snapshot().Errors, maxStreamErrorEntries)
}

func TestStreamStatusIsNormalEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reason StreamEndReason
		err    error
		normal bool
	}{
		{name: "done", reason: StreamEndReasonDone, normal: true},
		{name: "eof legacy compatibility", reason: StreamEndReasonEOF, normal: true},
		{name: "clean handler stop", reason: StreamEndReasonHandlerStop, normal: true},
		{name: "failed handler stop", reason: StreamEndReasonHandlerStop, err: errors.New("failed"), normal: false},
		{name: "timeout", reason: StreamEndReasonTimeout, normal: false},
		{name: "client gone", reason: StreamEndReasonClientGone, normal: false},
		{name: "scanner error", reason: StreamEndReasonScannerErr, normal: false},
		{name: "panic", reason: StreamEndReasonPanic, normal: false},
		{name: "ping failure", reason: StreamEndReasonPingFail, normal: false},
		{name: "unset", reason: StreamEndReasonNone, normal: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := NewStreamStatus()
			status.SetEndReason(test.reason, test.err)
			assert.Equal(t, test.normal, status.IsNormalEnd())
		})
	}
}

func TestStreamStatusNilSafe(t *testing.T) {
	t.Parallel()

	var status *StreamStatus
	status.SetEndReason(StreamEndReasonDone, nil)
	status.RecordError("ignored")
	status.MarkTerminalSuccess()
	status.MarkTerminalFailure(errors.New("ignored"))
	status.MarkTerminalDelivered()
	assert.True(t, status.IsNormalEnd())
	assert.False(t, status.HasErrors())
	assert.Zero(t, status.TotalErrorCount())
	assert.Equal(t, "StreamStatus<nil>", status.Summary())
}

func TestStreamStatusSummary(t *testing.T) {
	t.Parallel()

	status := NewStreamStatus()
	status.MarkTerminalSuccess()
	status.MarkTerminalDelivered()
	status.MarkClientGone(context.Canceled)
	status.SetDrainResult(StreamDrainNotNeeded)
	status.RecordError("optional event")

	summary := status.Summary()
	assert.Contains(t, summary, "reason=done")
	assert.Contains(t, summary, "terminal=success delivered=true")
	assert.Contains(t, summary, "client_gone=true")
	assert.Contains(t, summary, "drain=not_needed")
	assert.Contains(t, summary, "soft_errors=1")
}
