package common

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type StreamEndReason string

const (
	StreamEndReasonNone          StreamEndReason = ""
	StreamEndReasonDone          StreamEndReason = "done"
	StreamEndReasonTimeout       StreamEndReason = "timeout"
	StreamEndReasonClientGone    StreamEndReason = "client_gone"
	StreamEndReasonScannerErr    StreamEndReason = "scanner_error"
	StreamEndReasonHandlerStop   StreamEndReason = "handler_stop"
	StreamEndReasonEOF           StreamEndReason = "eof"
	StreamEndReasonPanic         StreamEndReason = "panic"
	StreamEndReasonPingFail      StreamEndReason = "ping_fail"
	StreamEndReasonProtocolError StreamEndReason = "protocol_error"
)

type StreamTerminalState string

const (
	StreamTerminalNone    StreamTerminalState = ""
	StreamTerminalSuccess StreamTerminalState = "success"
	StreamTerminalFailure StreamTerminalState = "failure"
)

type StreamDrainResult string

const (
	StreamDrainNotNeeded StreamDrainResult = "not_needed"
	StreamDrainCompleted StreamDrainResult = "completed"
	StreamDrainTimedOut  StreamDrainResult = "timed_out"
)

const maxStreamErrorEntries = 20

type StreamErrorEntry struct {
	Message   string
	Timestamp time.Time
}

// StreamSnapshot is an immutable, internally consistent view of a stream.
// Transport, protocol, and downstream-delivery facts are deliberately kept
// separate so the final result never depends on which goroutine won a race.
type StreamSnapshot struct {
	EndReason         StreamEndReason
	EndError          error
	TerminalState     StreamTerminalState
	TerminalSeen      bool
	TerminalDelivered bool
	ClientGone        bool
	ResponseCommitted bool
	UsageComplete     bool
	SemanticOutput    bool
	DrainResult       StreamDrainResult
	WriteError        error
	Errors            []StreamErrorEntry
	ErrorCount        int
}

type StreamStatus struct {
	mu sync.RWMutex

	observedReasons   map[StreamEndReason]error
	terminalState     StreamTerminalState
	terminalError     error
	terminalSeen      bool
	terminalDelivered bool
	clientGone        bool
	clientError       error
	responseCommitted bool
	usageComplete     bool
	semanticOutput    bool
	drainResult       StreamDrainResult
	writeError        error
	errors            []StreamErrorEntry
	errorCount        int
}

func NewStreamStatus() *StreamStatus {
	return &StreamStatus{observedReasons: make(map[StreamEndReason]error)}
}

// SetEndReason records an observed transport/handler event. It does not choose
// a winner. Snapshot applies a fixed precedence over all observed facts.
func (s *StreamStatus) SetEndReason(reason StreamEndReason, err error) {
	if s == nil || reason == StreamEndReasonNone {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observedReasons == nil {
		s.observedReasons = make(map[StreamEndReason]error)
	}
	previous, exists := s.observedReasons[reason]
	if !exists || previous == nil && err != nil {
		s.observedReasons[reason] = err
	}
	if reason == StreamEndReasonClientGone {
		s.clientGone = true
		if s.clientError == nil && err != nil {
			s.clientError = err
		}
	}
}

func (s *StreamStatus) MarkTerminalSuccess() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.terminalSeen = true
	// An explicit failed/incomplete/cancelled terminal is final. A later
	// success-looking transport marker must not turn that protocol failure into
	// a billable completion.
	if s.terminalState != StreamTerminalFailure {
		s.terminalState = StreamTerminalSuccess
		s.terminalError = nil
	}
	s.mu.Unlock()
}

func (s *StreamStatus) MarkTerminalFailure(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.terminalSeen = true
	s.terminalState = StreamTerminalFailure
	if s.terminalError == nil {
		s.terminalError = err
	}
	s.mu.Unlock()
}

// MarkTerminalDelivered records that the protocol's successful terminal event
// was written and flushed to the downstream client.
func (s *StreamStatus) MarkTerminalDelivered() {
	if s == nil {
		return
	}
	s.mu.Lock()
	// Delivery only counts if the success terminal was accepted before a known
	// client disconnect and no explicit failure terminal was seen. This makes
	// cancel-before-terminal and terminal-before-cancel deterministic.
	if !s.clientGone && s.terminalState != StreamTerminalFailure {
		s.terminalSeen = true
		s.terminalState = StreamTerminalSuccess
		s.terminalError = nil
		s.terminalDelivered = true
		s.responseCommitted = true
	}
	s.mu.Unlock()
}

func (s *StreamStatus) MarkClientGone(err error) {
	s.SetEndReason(StreamEndReasonClientGone, err)
}

func (s *StreamStatus) MarkResponseCommitted() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.responseCommitted = true
	s.mu.Unlock()
}

func (s *StreamStatus) MarkUsageComplete() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.usageComplete = true
	s.mu.Unlock()
}

func (s *StreamStatus) MarkSemanticOutput() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.semanticOutput = true
	s.mu.Unlock()
}

func (s *StreamStatus) MarkWriteError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	if s.writeError == nil {
		s.writeError = err
	}
	s.mu.Unlock()
}

func (s *StreamStatus) SetDrainResult(result StreamDrainResult) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.drainResult = result
	s.mu.Unlock()
}

func (s *StreamStatus) RecordError(msg string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorCount++
	if len(s.errors) < maxStreamErrorEntries {
		s.errors = append(s.errors, StreamErrorEntry{
			Message:   msg,
			Timestamp: time.Now(),
		})
	}
}

func (s *StreamStatus) Snapshot() StreamSnapshot {
	if s == nil {
		return StreamSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := StreamSnapshot{
		TerminalState:     s.terminalState,
		TerminalSeen:      s.terminalSeen,
		TerminalDelivered: s.terminalDelivered,
		ClientGone:        s.clientGone,
		ResponseCommitted: s.responseCommitted,
		UsageComplete:     s.usageComplete,
		SemanticOutput:    s.semanticOutput,
		DrainResult:       s.drainResult,
		WriteError:        s.writeError,
		Errors:            append([]StreamErrorEntry(nil), s.errors...),
		ErrorCount:        s.errorCount,
	}

	if s.terminalDelivered && s.terminalState == StreamTerminalSuccess {
		snapshot.EndReason = StreamEndReasonDone
		return snapshot
	}
	if s.clientGone {
		snapshot.EndReason = StreamEndReasonClientGone
		snapshot.EndError = s.clientError
		return snapshot
	}
	if s.terminalState == StreamTerminalFailure {
		snapshot.EndReason = StreamEndReasonProtocolError
		snapshot.EndError = s.terminalError
		return snapshot
	}
	if s.writeError != nil {
		snapshot.EndReason = StreamEndReasonHandlerStop
		snapshot.EndError = s.writeError
		return snapshot
	}

	for _, reason := range []StreamEndReason{
		StreamEndReasonPanic,
		StreamEndReasonHandlerStop,
		StreamEndReasonScannerErr,
		StreamEndReasonPingFail,
		StreamEndReasonTimeout,
		StreamEndReasonDone,
		StreamEndReasonEOF,
	} {
		if err, ok := s.observedReasons[reason]; ok {
			snapshot.EndReason = reason
			snapshot.EndError = err
			return snapshot
		}
	}
	return snapshot
}

func (s *StreamStatus) EndState() (StreamEndReason, error) {
	snapshot := s.Snapshot()
	return snapshot.EndReason, snapshot.EndError
}

func (s *StreamStatus) IsClientGone() bool {
	return s.Snapshot().ClientGone
}

func (s *StreamStatus) HasTerminalDelivered() bool {
	return s.Snapshot().TerminalDelivered
}

func (s *StreamStatus) HasErrors() bool {
	return s.Snapshot().ErrorCount > 0
}

func (s *StreamStatus) TotalErrorCount() int {
	return s.Snapshot().ErrorCount
}

func (s *StreamStatus) IsNormalEnd() bool {
	if s == nil {
		return true
	}
	snapshot := s.Snapshot()
	return snapshot.EndReason == StreamEndReasonDone ||
		snapshot.EndReason == StreamEndReasonEOF ||
		snapshot.EndReason == StreamEndReasonHandlerStop && snapshot.EndError == nil
}

func (s *StreamStatus) Summary() string {
	if s == nil {
		return "StreamStatus<nil>"
	}
	snapshot := s.Snapshot()
	b := &strings.Builder{}
	fmt.Fprintf(b, "reason=%s", snapshot.EndReason)
	if snapshot.EndError != nil {
		fmt.Fprintf(b, " end_error=%q", snapshot.EndError.Error())
	}
	if snapshot.TerminalSeen {
		fmt.Fprintf(b, " terminal=%s delivered=%t", snapshot.TerminalState, snapshot.TerminalDelivered)
	}
	if snapshot.ClientGone {
		fmt.Fprintf(b, " client_gone=true")
	}
	if snapshot.DrainResult != "" {
		fmt.Fprintf(b, " drain=%s", snapshot.DrainResult)
	}
	if snapshot.ErrorCount > 0 {
		fmt.Fprintf(b, " soft_errors=%d", snapshot.ErrorCount)
	}
	return b.String()
}
