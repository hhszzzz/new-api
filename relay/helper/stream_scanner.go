package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10  // 64KB (64*1024)
	DefaultMaxScannerBufferSize = 128 << 20 // 64MB (64*1024*1024) default SSE buffer size
	DefaultPingInterval         = 10 * time.Second
	DefaultStreamDrainTimeout   = 5 * time.Second
	// streamWriteTimeout bounds a single blocked write to a slow client so the
	// unconditional wg.Wait() in cleanup can always finish. Without it, a slow
	// but connected client (full TCP buffer, no server WriteTimeout) could hang
	// the handler forever.
	streamWriteTimeout = 30 * time.Second
	// DefaultPingMaxDuration bounds the heartbeat goroutine so it can never
	// outlive a stuck stream. Override with STREAM_PING_MAX_DURATION when the
	// upstream legitimately stays silent for longer (long agent workflows).
	DefaultPingMaxDuration = 30 * time.Minute
)

func getPingMaxDuration() time.Duration {
	if constant.StreamPingMaxDurationSeconds > 0 {
		return time.Duration(constant.StreamPingMaxDurationSeconds) * time.Second
	}
	return DefaultPingMaxDuration
}

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}

func copyCodexSSEHeaders(c *gin.Context, resp *http.Response) {
	if c == nil || c.Writer == nil || resp == nil {
		return
	}
	// codex
	for _, name := range []string{"X-Reasoning-Included", "X-Codex-Turn-State"} {
		values := resp.Header.Values(name)
		if !service.ShouldCopyUpstreamHeader(c, name, values) {
			continue
		}
		for _, value := range values {
			if value != "" {
				c.Writer.Header().Add(name, value)
			}
		}
	}
}

// ExtendWriteDeadline pushes the connection write deadline forward before each
// stream write. Best-effort: writers that don't support deadlines (e.g.
// httptest recorders) are silently ignored.
func ExtendWriteDeadline(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(streamWriteTimeout))
}

type StreamScannerOptions struct {
	// PingInterval forces SSE comment heartbeats for this stream even when the
	// global ping setting is disabled. A non-positive value keeps the global
	// setting behavior.
	PingInterval time.Duration
	// DrainTimeout bounds how long the scanner keeps consuming upstream data
	// after the downstream client disconnects. Zero uses the five-second default.
	DrainTimeout time.Duration
	// OnClientGone runs after downstream processing has stopped. It may run while
	// the scanner is finishing the bounded upstream drain, but always on the main
	// request goroutine.
	OnClientGone func()
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult)) {
	StreamScannerHandlerWithOptions(c, resp, info, StreamScannerOptions{}, dataHandler)
}

func observeDrainedStreamData(status *relaycommon.StreamStatus, data string) {
	if status == nil || data == "" {
		return
	}
	var event struct {
		Type          string `json:"type"`
		Event         string `json:"event"`
		Delta         string `json:"delta"`
		Answer        string `json:"answer"`
		Usage         any    `json:"usage"`
		UsageMetadata any    `json:"usageMetadata"`
		Metadata      struct {
			Usage any `json:"usage"`
		} `json:"metadata"`
		Response *struct {
			Usage  any   `json:"usage"`
			Output []any `json:"output"`
		} `json:"response"`
		Item       any `json:"item"`
		Candidates []struct {
			FinishReason *string `json:"finishReason"`
			Content      any     `json:"content"`
		} `json:"candidates"`
	}
	if err := common.UnmarshalJsonStr(data, &event); err != nil {
		return
	}
	if event.Usage != nil || event.UsageMetadata != nil || event.Metadata.Usage != nil || event.Response != nil && event.Response.Usage != nil {
		status.MarkUsageComplete()
	}
	if event.Delta != "" || event.Answer != "" || event.Item != nil || event.Response != nil && len(event.Response.Output) > 0 {
		status.MarkSemanticOutput()
	}
	for _, candidate := range event.Candidates {
		if candidate.Content != nil {
			status.MarkSemanticOutput()
		}
		if candidate.FinishReason != nil && strings.TrimSpace(*candidate.FinishReason) != "" {
			status.MarkTerminalSuccess()
		}
	}
	switch event.Type {
	case "response.completed", "response.done", "message_stop":
		status.MarkTerminalSuccess()
	case "error", "response.error", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		status.MarkTerminalFailure(fmt.Errorf("upstream terminal event %s", event.Type))
	}
	if event.Event == "message_end" {
		status.MarkTerminalSuccess()
	} else if event.Event == "error" {
		status.MarkTerminalFailure(fmt.Errorf("upstream terminal event %s", event.Event))
	}
}

func markResponseCommittedIfWritten(c *gin.Context, status *relaycommon.StreamStatus) {
	if c == nil || c.Writer == nil || status == nil {
		return
	}
	// Gin's responseWriter is not safe for a concurrent Written read while the
	// heartbeat goroutine writes. Use the same lock as every stream write.
	_ = withStreamWriteLock(c, func() error {
		if c.Writer.Written() {
			status.MarkResponseCommitted()
		}
		return nil
	})
}

func StreamScannerHandlerWithOptions(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, options StreamScannerOptions, dataHandler func(data string, sr *StreamResult)) {
	if resp == nil || resp.Body == nil || info == nil || dataHandler == nil {
		return
	}

	// 无条件新建 StreamStatus
	info.StreamStatus = relaycommon.NewStreamStatus()

	ctx, cancel := context.WithCancel(context.Background())
	handlerCtx, stopHandler := context.WithCancel(context.Background())

	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second

	var (
		scanner     = NewStreamScanner(resp.Body)
		ticker      = time.NewTicker(streamingTimeout)
		pingTicker  *time.Ticker
		wg          sync.WaitGroup
		cleanupOnce sync.Once
		draining    atomic.Bool
		scannerDone = make(chan struct{})
		handlerDone = make(chan struct{})
		pingFailure = make(chan error, 1)
	)

	generalSettings := operation_setting.GetGeneralSetting()
	pingInterval := options.PingInterval
	forcedPing := pingInterval > 0
	if !forcedPing {
		pingInterval = time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	}
	pingEnabled := !info.DisablePing && (generalSettings.PingIntervalEnabled || forcedPing)
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}

	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
	}

	logger.LogDebug(c, "relay timeout seconds: %d", common.RelayTimeout)
	logger.LogDebug(c, "relay max idle conns: %d", common.RelayMaxIdleConns)
	logger.LogDebug(c, "relay max idle conns per host: %d", common.RelayMaxIdleConnsPerHost)
	logger.LogDebug(c, "streaming timeout seconds: %d", int64(streamingTimeout.Seconds()))
	logger.LogDebug(c, "ping interval seconds: %d", int64(pingInterval.Seconds()))

	cleanup := func() {
		cleanupOnce.Do(func() {
			stopHandler()
			cancel()
			if resp.Body != nil {
				_ = resp.Body.Close()
			}

			ticker.Stop()
			if pingTicker != nil {
				pingTicker.Stop()
			}

			wg.Wait()
		})
	}
	// Ensure gin.Context is not returned to Gin's pool while any stream goroutine can still use it.
	defer cleanup()

	scanner.Split(bufio.ScanLines)
	copyCodexSSEHeaders(c, resp)
	SetEventStreamHeaders(c)
	EnsureStreamWriteMutex(c)

	// Handle ping data sending with improved error handling
	if pingEnabled && pingTicker != nil {
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					panicErr := fmt.Errorf("ping panic: %v", r)
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, panicErr)
					select {
					case pingFailure <- panicErr:
					default:
					}
				}
				logger.LogDebug(c, "ping goroutine exited")
				wg.Done()
			}()

			// 添加超时保护，防止 goroutine 无限运行
			maxPingDuration := getPingMaxDuration()
			pingTimeout := time.NewTimer(maxPingDuration)
			defer pingTimeout.Stop()

			for {
				select {
				case <-pingTicker.C:
					var err error
					err = PingData(c)
					if err != nil {
						logger.LogError(c, "ping data error: "+err.Error())
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
						info.StreamStatus.MarkWriteError(err)
						select {
						case pingFailure <- err:
						default:
						}
						return
					}
					// PingData returning nil means the heartbeat was written and flushed.
					info.StreamStatus.MarkResponseCommitted()
					logger.LogDebug(c, "ping data sent")
				case <-ctx.Done():
					return
				case <-c.Request.Context().Done():
					// 监听客户端断开连接
					return
				case <-pingTimeout.C:
					logger.LogError(c, "ping goroutine max duration reached")
					return
				}
			}
		})
	}

	dataChan := make(chan string, 10)

	wg.Add(1)
	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
			}
			close(handlerDone)
			wg.Done()
		}()
		sr := newStreamResult(info.StreamStatus)
		for {
			// Prefer shutdown over a buffered chunk when both are ready. The second
			// check below closes the small select race after receiving from dataChan.
			select {
			case <-handlerCtx.Done():
				return
			default:
			}
			select {
			case data, ok := <-dataChan:
				if !ok {
					return
				}
				select {
				case <-handlerCtx.Done():
					return
				default:
				}
				sr.reset()
				dataHandler(data, sr)
				markResponseCommittedIfWritten(c, info.StreamStatus)
				if sr.IsStopped() {
					return
				}
			case <-handlerCtx.Done():
				return
			}
		}
	})

	// Scanner goroutine with improved error handling
	wg.Add(1)
	common.RelayCtxGo(ctx, func() {
		defer func() {
			close(dataChan)
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
			}
			close(scannerDone)
			logger.LogDebug(c, "scanner goroutine exited")
			wg.Done()
		}()

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			ticker.Reset(streamingTimeout)
			data := scanner.Text()
			logger.LogDebug(c, "stream scanner data: %s", data)

			if len(data) < 6 {
				continue
			}
			if data[:5] != "data:" && data[:6] != "[DONE]" {
				continue
			}
			data = data[5:]
			data = strings.TrimSpace(data)
			if data == "" {
				continue
			}
			if !strings.HasPrefix(data, "[DONE]") {
				info.SetFirstResponseTime()
				info.ReceivedResponseCount++
				if draining.Load() {
					observeDrainedStreamData(info.StreamStatus, data)
					continue
				}

				select {
				case dataChan <- data:
				case <-handlerCtx.Done():
					if draining.Load() {
						observeDrainedStreamData(info.StreamStatus, data)
						continue
					}
					return
				case <-ctx.Done():
					return
				}
			} else {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				logger.LogDebug(c, "received [DONE], stopping scanner")
				return
			}
		}

		if err := scanner.Err(); err != nil {
			// Closing the response body is the scanner cancellation mechanism. Do
			// not let that expected cleanup error overwrite the timeout/client fact
			// which caused cleanup.
			if err != io.EOF && ctx.Err() == nil {
				logger.LogError(c, "scanner error: "+err.Error())
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			}
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	})

	// Main loop resolves transport, handler, and downstream state independently.
	// User pacing does not count as upstream idleness. After downstream cancel,
	// scanner keeps consuming (without invoking handlers) for at most five seconds.
	clientGone := false
	scannerFinished := false
	handlerFinished := false
	callbackCalled := false
	finished := false
	clientDone := c.Request.Context().Done()
	drainTimeout := options.DrainTimeout
	if drainTimeout <= 0 {
		drainTimeout = DefaultStreamDrainTimeout
	}
	var drainTimer *time.Timer
	callClientGone := func() {
		if callbackCalled || options.OnClientGone == nil || info.StreamStatus.HasTerminalDelivered() {
			return
		}
		callbackCalled = true
		options.OnClientGone()
	}
	for !finished {
		if clientGone && info.StreamStatus.HasTerminalDelivered() {
			info.StreamStatus.SetDrainResult(relaycommon.StreamDrainNotNeeded)
			break
		}
		if clientGone && scannerFinished && handlerFinished {
			info.StreamStatus.SetDrainResult(relaycommon.StreamDrainCompleted)
			callClientGone()
			break
		}
		if !clientGone && handlerFinished {
			break
		}
		select {
		case <-ticker.C:
			if service.IsUserStreamPacing(c) {
				ticker.Reset(streamingTimeout)
				continue
			}
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
			finished = true
		case <-scannerDone:
			scannerFinished = true
			scannerDone = nil
		case <-handlerDone:
			handlerFinished = true
			handlerDone = nil
			if clientGone {
				info.StreamStatus.MarkClientGone(c.Request.Context().Err())
				callClientGone()
			}
		case err := <-pingFailure:
			if !clientGone {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
				finished = true
			}
		case <-clientDone:
			if !clientGone {
				clientGone = true
				draining.Store(true)
				// Stop downstream handling immediately while leaving the scanner's
				// independent context alive for the bounded diagnostic drain. Record
				// client_gone only after an in-flight handler returns, so a terminal
				// write that already completed is ordered before the cancellation.
				stopHandler()
				if handlerFinished {
					info.StreamStatus.MarkClientGone(c.Request.Context().Err())
				}
				drainTimer = time.NewTimer(drainTimeout)
				clientDone = nil
			}
		case <-func() <-chan time.Time {
			if drainTimer == nil {
				return nil
			}
			return drainTimer.C
		}():
			info.StreamStatus.SetDrainResult(relaycommon.StreamDrainTimedOut)
			finished = true
		}
	}
	if drainTimer != nil {
		drainTimer.Stop()
	}
	// Cancellation may become visible in the same scheduler turn as handler
	// completion. Record it after the loop; delivered terminal success still wins
	// in StreamStatus.Snapshot.
	if c.Request.Context().Err() != nil {
		clientGone = true
		info.StreamStatus.MarkClientGone(c.Request.Context().Err())
		if info.StreamStatus.HasTerminalDelivered() && info.StreamStatus.Snapshot().DrainResult == "" {
			info.StreamStatus.SetDrainResult(relaycommon.StreamDrainNotNeeded)
		}
	}

	cleanup()
	if clientGone {
		callClientGone()
	}
	snapshot := info.StreamStatus.Snapshot()
	if snapshot.ClientGone && !snapshot.TerminalDelivered {
		logger.LogInfo(c, fmt.Sprintf("stream client disconnected: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	} else if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
}
