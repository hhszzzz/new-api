package helper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setStreamPingSettingForTest(t *testing.T, enabled bool, seconds int) {
	t.Helper()
	handled, err := config.GlobalConfig.Update("general_setting", map[string]string{
		"ping_interval_enabled": strconv.FormatBool(enabled),
		"ping_interval_seconds": strconv.Itoa(seconds),
	})
	require.NoError(t, err)
	require.True(t, handled)
}

func init() {
	gin.SetMode(gin.TestMode)
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

func setupStreamTest(t *testing.T, body io.Reader) (*gin.Context, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		Body: io.NopCloser(body),
	}

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	return c, resp, info
}

func buildSSEBody(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "data: {\"id\":%d,\"choices\":[{\"delta\":{\"content\":\"token_%d\"}}]}\n", i, i)
	}
	b.WriteString("data: [DONE]\n")
	return b.String()
}

// ---------- Basic correctness ----------

func TestStreamScannerHandler_NilInputs(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, nil, info, func(data string, sr *StreamResult) {})
	StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader(""))}, info, nil)
}

func TestNewStreamScanner_AllowsLargeStreamLine(t *testing.T) {
	oldBufferMB := constant.StreamScannerMaxBufferMB
	constant.StreamScannerMaxBufferMB = 1
	t.Cleanup(func() {
		constant.StreamScannerMaxBufferMB = oldBufferMB
	})

	payload := strings.Repeat("x", 128<<10)
	scanner := NewStreamScanner(strings.NewReader("data: " + payload + "\n"))
	scanner.Split(bufio.ScanLines)

	require.True(t, scanner.Scan())
	assert.Equal(t, "data: "+payload, scanner.Text())
	require.NoError(t, scanner.Err())
}

func TestStreamScannerHandler_EmptyBody(t *testing.T) {
	t.Parallel()

	c, resp, info := setupStreamTest(t, strings.NewReader(""))

	var called atomic.Bool
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		called.Store(true)
	})

	assert.False(t, called.Load(), "handler should not be called for empty body")
}

func TestStreamScannerHandler_1000Chunks(t *testing.T) {
	t.Parallel()

	const numChunks = 1000
	body := buildSSEBody(numChunks)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		count.Add(1)
	})

	assert.Equal(t, int64(numChunks), count.Load())
	assert.Equal(t, numChunks, info.ReceivedResponseCount)
}

func TestStreamScannerHandler_OrderPreserved(t *testing.T) {
	t.Parallel()

	const numChunks = 500
	body := buildSSEBody(numChunks)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var mu sync.Mutex
	received := make([]string, 0, numChunks)

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		mu.Lock()
		received = append(received, data)
		mu.Unlock()
	})

	require.Equal(t, numChunks, len(received))
	for i := 0; i < numChunks; i++ {
		expected := fmt.Sprintf("{\"id\":%d,\"choices\":[{\"delta\":{\"content\":\"token_%d\"}}]}", i, i)
		assert.Equal(t, expected, received[i], "chunk %d out of order", i)
	}
}

func TestStreamScannerHandler_DoneStopsScanner(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(50) + "data: should_not_appear\n"
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		count.Add(1)
	})

	assert.Equal(t, int64(50), count.Load(), "data after [DONE] must not be processed")
}

func TestStreamScannerHandler_StopStopsStream(t *testing.T) {
	t.Parallel()

	const numChunks = 200
	body := buildSSEBody(numChunks)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	const stopAt int64 = 50
	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		n := count.Add(1)
		if n >= stopAt {
			sr.Stop(fmt.Errorf("fatal at %d", n))
		}
	})

	assert.Equal(t, stopAt, count.Load())
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.Snapshot().EndReason)
}

func TestStreamScannerHandler_SkipsNonDataLines(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString(": comment line\n")
	b.WriteString("event: message\n")
	b.WriteString("id: 12345\n")
	b.WriteString("retry: 5000\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "data: payload_%d\n", i)
		b.WriteString(": interleaved comment\n")
	}
	b.WriteString("data: [DONE]\n")

	c, resp, info := setupStreamTest(t, strings.NewReader(b.String()))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		count.Add(1)
	})

	assert.Equal(t, int64(100), count.Load())
}

func TestStreamScannerHandler_DataWithExtraSpaces(t *testing.T) {
	t.Parallel()

	body := "data:   {\"trimmed\":true}  \ndata: [DONE]\n"
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var got string
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		got = data
	})

	assert.Equal(t, "{\"trimmed\":true}", got)
}

// TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns pins the
// disconnect contract: when the client goes away, the handler must return
// promptly (all goroutines joined, so the gin.Context can never leak into a
// pooled reuse), the upstream body must be closed to stop token generation,
// and no data received after the disconnect may be processed or written.
func TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	resp := &http.Response{Body: pr}
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	var count atomic.Int64
	var clientGoneCallbacks atomic.Int64
	firstHandled := make(chan struct{})
	done := make(chan struct{})
	go func() {
		StreamScannerHandlerWithOptions(c, resp, info, StreamScannerOptions{
			DrainTimeout: 25 * time.Millisecond,
			OnClientGone: func() {
				clientGoneCallbacks.Add(1)
			},
		}, func(data string, sr *StreamResult) {
			count.Add(1)
			_ = StringData(c, data)
			if data == "first" {
				close(firstHandled)
			}
		})
		close(done)
	}()

	_, err := fmt.Fprint(pw, "data: first\n")
	require.NoError(t, err)

	select {
	case <-firstHandled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	cancel()

	// The test uses a short configurable drain bound. Once it expires, cleanup
	// closes resp.Body and unblocks the scanner goroutine.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after client disconnect")
	}

	// Upstream read side must be closed so the provider stops generating
	// (and billing) for a request nobody is listening to.
	_, err = fmt.Fprint(pw, "data: second\n")
	require.ErrorIs(t, err, io.ErrClosedPipe, "upstream body should be closed after client disconnect")

	assert.Equal(t, int64(1), count.Load(), "no chunk after disconnect should be processed")
	require.NotNil(t, info.StreamStatus)
	snapshot := info.StreamStatus.Snapshot()
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, snapshot.EndReason)
	assert.Equal(t, relaycommon.StreamDrainTimedOut, snapshot.DrainResult)
	assert.Equal(t, int64(1), clientGoneCallbacks.Load(), "client disconnect callback should run exactly once")

	body := recorder.Body.String()
	assert.Contains(t, body, "first")
	assert.NotContains(t, body, "second")
}

func TestStreamScannerHandlerTerminalDeliveredBeforeCancelWins(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, resp, info := setupStreamTest(t, strings.NewReader("data: terminal\ndata: [DONE]\n"))
	c.Request = c.Request.WithContext(requestContext)

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		info.StreamStatus.MarkTerminalSuccess()
		info.StreamStatus.MarkTerminalDelivered()
		cancel()
		sr.Done()
	})

	snapshot := info.StreamStatus.Snapshot()
	assert.Equal(t, relaycommon.StreamEndReasonDone, snapshot.EndReason)
	assert.True(t, snapshot.TerminalDelivered)
	assert.True(t, snapshot.ClientGone)
	assert.Equal(t, relaycommon.StreamDrainNotNeeded, snapshot.DrainResult)
}

type stagedDrainReader struct {
	release <-chan struct{}
	stage   int
	pending []byte
	closed  atomic.Bool
}

func (r *stagedDrainReader) Read(p []byte) (int, error) {
	if r.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if len(r.pending) == 0 {
		switch r.stage {
		case 0:
			r.pending = []byte("data: partial\n")
			r.stage++
		case 1:
			<-r.release
			r.pending = []byte("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"total_tokens\":3},\"output\":[{\"type\":\"message\"}]}}\ndata: [DONE]\n")
			r.stage++
		default:
			return 0, io.EOF
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *stagedDrainReader) Close() error {
	r.closed.Store(true)
	return nil
}

func TestStreamScannerHandlerCancelBeforeTerminalDrainsDiagnosticFacts(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	releaseDrain := make(chan struct{})
	body := &stagedDrainReader{release: releaseDrain}
	resp := &http.Response{Body: body}

	StreamScannerHandlerWithOptions(c, resp, info, StreamScannerOptions{
		DrainTimeout: time.Second,
		OnClientGone: func() {
			close(releaseDrain)
		},
	}, func(data string, sr *StreamResult) {
		info.StreamStatus.MarkSemanticOutput()
		cancel()
	})

	snapshot := info.StreamStatus.Snapshot()
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, snapshot.EndReason)
	assert.True(t, snapshot.ClientGone)
	assert.False(t, snapshot.TerminalDelivered)
	assert.Equal(t, relaycommon.StreamTerminalSuccess, snapshot.TerminalState)
	assert.True(t, snapshot.UsageComplete)
	assert.True(t, snapshot.SemanticOutput)
	assert.Equal(t, relaycommon.StreamDrainCompleted, snapshot.DrainResult)
}

func TestStreamScannerHandlerDropsBufferedChunksAfterClientCancel(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, resp, info := setupStreamTest(t, strings.NewReader(buildSSEBody(50)))
	c.Request = c.Request.WithContext(requestContext)
	info.DisablePing = true

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	done := make(chan struct{})
	var handled atomic.Int64
	go func() {
		StreamScannerHandlerWithOptions(c, resp, info, StreamScannerOptions{DrainTimeout: time.Second}, func(data string, sr *StreamResult) {
			if handled.Add(1) == 1 {
				close(firstStarted)
				<-releaseFirst
			}
		})
		close(done)
	}()

	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first buffered chunk")
	}
	cancel()
	close(releaseFirst)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream scanner did not finish after cancellation")
	}

	assert.Equal(t, int64(1), handled.Load(), "queued chunks must not reach the downstream handler after cancellation")
	snapshot := info.StreamStatus.Snapshot()
	assert.True(t, snapshot.ClientGone)
	assert.False(t, snapshot.TerminalDelivered)
	assert.Equal(t, relaycommon.StreamDrainCompleted, snapshot.DrainResult)
}

type cancelErrorReader struct {
	release <-chan struct{}
	stage   atomic.Int64
	err     error
}

func (r *cancelErrorReader) Read(p []byte) (int, error) {
	if r.stage.Add(1) == 1 {
		return copy(p, "data: partial\n"), nil
	}
	<-r.release
	return 0, r.err
}

func (r *cancelErrorReader) Close() error { return nil }

func TestStreamScannerHandlerClientCancelBeatsConcurrentReadError(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	releaseRead := make(chan struct{})
	readErr := errors.New("upstream connection reset")
	resp := &http.Response{Body: &cancelErrorReader{release: releaseRead, err: readErr}}
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandlerWithOptions(c, resp, info, StreamScannerOptions{DrainTimeout: time.Second}, func(data string, sr *StreamResult) {
		cancel()
		close(releaseRead)
	})

	snapshot := info.StreamStatus.Snapshot()
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, snapshot.EndReason)
	assert.ErrorIs(t, snapshot.EndError, context.Canceled)
	assert.True(t, snapshot.ClientGone)
	assert.False(t, snapshot.TerminalDelivered)
}

// ---------- Ping tests ----------

func TestStreamScannerHandler_PingSentDuringSlowUpstream(t *testing.T) {
	setting := operation_setting.GetGeneralSetting()
	t.Cleanup(func() {
		setStreamPingSettingForTest(t, setting.PingIntervalEnabled, setting.PingIntervalSeconds)
	})
	setStreamPingSettingForTest(t, true, 1)

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for i := 0; i < 4; i++ {
			fmt.Fprintf(pw, "data: chunk_%d\n", i)
			time.Sleep(400 * time.Millisecond)
		}
		fmt.Fprint(pw, "data: [DONE]\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{Body: pr}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	var count atomic.Int64
	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
			count.Add(1)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream to finish")
	}

	assert.Equal(t, int64(4), count.Load())

	body := recorder.Body.String()
	pingCount := strings.Count(body, ": PING")
	assert.GreaterOrEqual(t, pingCount, 1,
		"expected at least 1 ping during slow stream with 1s interval; got %d", pingCount)
}

func TestStreamScannerHandler_PingDisabledByRelayInfo(t *testing.T) {
	setting := operation_setting.GetGeneralSetting()
	t.Cleanup(func() {
		setStreamPingSettingForTest(t, setting.PingIntervalEnabled, setting.PingIntervalSeconds)
	})
	setStreamPingSettingForTest(t, true, 1)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{Body: io.NopCloser(strings.NewReader(buildSSEBody(5)))}
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	var count atomic.Int64
	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
			count.Add(1)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	assert.Equal(t, int64(5), count.Load())

	body := recorder.Body.String()
	pingCount := strings.Count(body, ": PING")
	assert.Equal(t, 0, pingCount, "pings should be disabled when DisablePing=true")
}

func TestStreamScannerHandler_OptionForcesPingWhenGlobalSettingDisabled(t *testing.T) {
	setting := operation_setting.GetGeneralSetting()
	t.Cleanup(func() {
		setStreamPingSettingForTest(t, setting.PingIntervalEnabled, setting.PingIntervalSeconds)
	})
	setStreamPingSettingForTest(t, false, 60)

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		time.Sleep(50 * time.Millisecond)
		_, _ = fmt.Fprint(pw, "data: [DONE]\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{Body: pr}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandlerWithOptions(c, resp, info, StreamScannerOptions{
		PingInterval: 10 * time.Millisecond,
	}, func(data string, sr *StreamResult) {})

	assert.GreaterOrEqual(t, strings.Count(recorder.Body.String(), ": PING"), 1)
}

// ---------- StreamStatus integration ----------

func TestStreamScannerHandler_StreamStatus_DoneReason(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(10)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.Snapshot().EndReason)
	assert.Nil(t, info.StreamStatus.Snapshot().EndError)
	assert.True(t, info.StreamStatus.IsNormalEnd())
	assert.False(t, info.StreamStatus.HasErrors())
}

func TestStreamScannerHandler_StreamStatus_EOFWithoutDone(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, "data: {\"id\":%d}\n", i)
	}
	c, resp, info := setupStreamTest(t, strings.NewReader(b.String()))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonEOF, info.StreamStatus.Snapshot().EndReason)
	assert.True(t, info.StreamStatus.IsNormalEnd())
}

func TestStreamScannerHandler_StreamStatus_HandlerStop(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(100)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		n := count.Add(1)
		if n >= 10 {
			sr.Stop(fmt.Errorf("stop at 10"))
		}
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.Snapshot().EndReason)
	assert.True(t, info.StreamStatus.HasErrors())
}

func TestStreamScannerHandler_StreamStatus_HandlerDone(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(20)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		n := count.Add(1)
		if n >= 5 {
			sr.Done()
		}
	})

	assert.Equal(t, int64(5), count.Load())
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.Snapshot().EndReason)
	assert.False(t, info.StreamStatus.HasErrors())
}

func TestStreamScannerHandler_StreamStatus_Timeout(t *testing.T) {
	// Not parallel: modifies global constant.StreamingTimeout
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	pr, pw := io.Pipe()
	go func() {
		fmt.Fprint(pw, "data: {\"id\":1}\n")
		time.Sleep(2 * time.Second)
		pw.Close()
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{Body: pr}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream timeout")
	}

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.Snapshot().EndReason)
	assert.False(t, info.StreamStatus.IsNormalEnd())
}

func TestStreamScannerHandler_StreamStatus_SoftErrors(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(10)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		sr.Error(fmt.Errorf("soft error for chunk"))
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.Snapshot().EndReason)
	assert.True(t, info.StreamStatus.HasErrors())
	assert.Equal(t, 10, info.StreamStatus.TotalErrorCount())
}

func TestStreamScannerHandler_StreamStatus_MultipleErrorsPerChunk(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(5)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		sr.Error(fmt.Errorf("error A"))
		sr.Error(fmt.Errorf("error B"))
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.Snapshot().EndReason)
	assert.Equal(t, 10, info.StreamStatus.TotalErrorCount())
}

func TestStreamScannerHandler_StreamStatus_ErrorThenStop(t *testing.T) {
	t.Parallel()

	// Use a large body without [DONE] so the handler-stop path also exercises
	// scanner cancellation with queued data.
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "data: {\"id\":%d}\n", i)
	}
	c, resp, info := setupStreamTest(t, strings.NewReader(b.String()))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		count.Add(1)
		sr.Error(fmt.Errorf("soft error"))
		sr.Stop(fmt.Errorf("fatal"))
	})

	assert.Equal(t, int64(1), count.Load())
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.Snapshot().EndReason)
	assert.Equal(t, 2, info.StreamStatus.TotalErrorCount())
}

func TestStreamScannerHandler_StreamStatus_InitializedIfNil(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(1)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	assert.Nil(t, info.StreamStatus)

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	assert.NotNil(t, info.StreamStatus)
}

func TestStreamScannerHandler_StreamStatus_ReplacesPreInitialized(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(5)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.RecordError("pre-existing error")

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.Snapshot().EndReason)
	assert.Equal(t, 0, info.StreamStatus.TotalErrorCount())
}
