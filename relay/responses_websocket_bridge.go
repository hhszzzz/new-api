package relay

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	appmodel "github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"

	"github.com/gorilla/websocket"
)

// startHTTPBridgeCall serves one response.create over the HTTP relay pipeline
// when no channel can carry a native Responses WebSocket. The full protocol
// bridge (chat/messages/gemini upstreams) stays available because the call goes
// through the same plan selection and converters as POST /v1/responses; the
// resulting SSE events are forwarded to the client as WebSocket messages.
func (s *responsesWSSession) startHTTPBridgeCall(create responsesWSCreateRequest, eventID string, commitRate middleware.ModelRequestRateLimitCommit) *types.NewAPIError {
	req := create.Request
	req.Stream = common.GetPointer(true)
	req.StreamOptions = nil
	requestBody, err := common.Marshal(&req)
	if err != nil {
		commitRate(false)
		return newResponsesWSInvalidRequestError(err)
	}
	common.CleanupBodyStorage(s.c)
	storage, err := common.CreateBodyStorage(requestBody)
	if err != nil {
		commitRate(false)
		return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	s.c.Set(common.KeyBodyStorage, storage)
	s.c.Request.Body = io.NopCloser(bytes.NewReader(requestBody))
	s.c.Request.ContentLength = int64(len(requestBody))
	create.Request = req

	callCtx, cancel := context.WithCancel(s.c.Request.Context())
	state := &responsesWSCallState{
		usage:      &dto.Usage{},
		commitRate: commitRate,
		cancelHTTP: cancel,
		rateGuard:  create.rateGuard,
	}
	if !s.tryReserveCurrent(state) {
		cancel()
		commitRate(false)
		return types.NewErrorWithStatusCode(errors.New("another response.create is already in progress on this websocket connection"), types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry())
	}
	s.bridgeWG.Add(1)
	go s.runHTTPBridgeCall(state, create, eventID, callCtx)
	return nil
}

func (s *responsesWSSession) runHTTPBridgeCall(state *responsesWSCallState, create responsesWSCreateRequest, eventID string, callCtx context.Context) {
	defer s.bridgeWG.Done()
	c := s.c
	originalRequest := c.Request
	originalWriter := c.Writer
	bridgedRequest := originalRequest.WithContext(callCtx)
	// Upstream adaptors reuse the client request method; the WebSocket upgrade
	// arrived as a GET, while every relay endpoint expects a POST.
	bridgedRequest.Method = http.MethodPost
	c.Request = bridgedRequest
	forwarder := newResponsesWSSSEForwarder(func(payload []byte) error {
		if state.rateGuard != nil {
			if err := state.rateGuard.Pace(callCtx, payload); err != nil {
				return err
			}
		}
		return s.writeClient(websocket.TextMessage, payload)
	}, state.cancelHTTP)
	c.Writer = forwarder

	var finalErr *types.NewAPIError
	var relayInfo *relaycommon.RelayInfo
	defer func() {
		if r := recover(); r != nil {
			logger.LogError(c, fmt.Sprintf("responses websocket http bridge panic: %v", r))
			if relayInfo != nil && relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			if finalErr == nil {
				finalErr = types.NewError(fmt.Errorf("responses websocket http bridge panic: %v", r), types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
			}
		}
		// Snapshot before cancelHTTP below cancels callCtx: a non-nil error here
		// means the client cancelled or disconnected mid-call.
		clientGone := callCtx.Err() != nil
		c.Writer = originalWriter
		c.Request = originalRequest
		state.cancelHTTP()
		if finalErr != nil && !clientGone {
			service.ChargeViolationFeeIfNeeded(c, relayInfo, finalErr)
			s.sendError(eventID, finalErr)
		}
		if state.commitRate != nil {
			state.commitRate(finalErr == nil)
		}
		if s.clearCurrent(state) {
			state.channelRateGuard.Release()
			state.rateGuard.Release()
		}
		if finalErr == nil && !clientGone {
			forwarder.flushHeldEvents()
		}
	}()

	retryParam := middleware.NewResponsesBridgeRetryParam(c, create.Request.Model)
	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		protocolstate.ResetAttempt(c)
		retryParam.ClearChannelExclusions()
		var (
			channel          *appmodel.Channel
			channelRateGuard *service.ChannelRateLimitGuard
			apiErr           *types.NewAPIError
			rejected         bool
		)
		for {
			channel, apiErr = s.selectHTTPBridgeChannel(create.Request.Model, retryParam)
			if apiErr != nil {
				break
			}
			var allowed bool
			channelRateGuard, allowed = service.TryAcquireChannelRateLimit(c, channel)
			if allowed {
				break
			}
			rejected = true
			retryParam.ExcludeChannel(channel.Id)
		}
		retryParam.ClearChannelExclusions()
		if apiErr != nil {
			if rejected {
				finalErr = service.NewChannelRateLimitError()
			} else {
				finalErr = apiErr
			}
			break
		}
		addResponsesWSUsedChannel(c, channel.Id)

		attempt, apiErr := s.prepareCallState(create)
		if apiErr != nil {
			channelRateGuard.Release()
			finalErr = apiErr
			break
		}
		relayInfo = attempt.info
		s.stateMu.Lock()
		state.info = relayInfo
		state.channelRateGuard = channelRateGuard
		s.stateMu.Unlock()

		apiErr = ResponsesHelper(c, relayInfo)
		channelRateGuard.Release()
		s.stateMu.Lock()
		state.channelRateGuard = nil
		s.stateMu.Unlock()
		if apiErr == nil {
			middleware.CommitAutoProtocolAffinity(c)
			if commitErr := protocolstate.Commit(c); commitErr != nil {
				logger.LogError(c, "failed to persist Responses WebSocket protocol state: "+commitErr.Error())
			}
			service.RecordChannelAffinity(c, relayInfo.ChannelId)
			finalErr = nil
			break
		}

		apiErr = service.NormalizeViolationFeeError(apiErr)
		if relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
		if callCtx.Err() != nil {
			// The client cancelled or disconnected; this is not a channel failure.
			finalErr = apiErr
			break
		}
		if protocolstate.EnableReplayFallback(c, apiErr) {
			retryParam.SetRetry(0)
			retryParam.ResetRetryNextTry()
			continue
		}
		if middleware.AdvanceAutoProtocolAttempt(c, apiErr) {
			retryParam.ResetRetryNextTry()
			continue
		}
		var shouldRetry bool
		finalErr, shouldRetry = s.processChannelError(channel, apiErr, retryParam, relayInfo)
		if !shouldRetry || forwarder.Written() {
			break
		}
	}
}

func (s *responsesWSSession) selectHTTPBridgeChannel(publicModel string, retryParam *service.RetryParam) (*appmodel.Channel, *types.NewAPIError) {
	if channelID, retrySameChannel := middleware.PendingAutoProtocolRetryChannelID(s.c); retrySameChannel && !retryParam.IsChannelExcluded(channelID) {
		channel, err := appmodel.CacheGetChannel(channelID)
		if err != nil {
			return nil, types.NewError(fmt.Errorf("failed to reload channel %d for automatic protocol retry: %w", channelID, err), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if channel == nil || channel.Status != common.ChannelStatusEnabled || !channel.IsSchedulableAt(time.Now()) {
			return nil, types.NewError(fmt.Errorf("channel %d is unavailable for automatic protocol retry", channelID), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if setupErr := middleware.SetupContextForSelectedChannel(s.c, channel, publicModel, true); setupErr != nil {
			return nil, setupErr
		}
		return channel, nil
	}
	return middleware.SelectResponsesBridgeChannel(s.c, publicModel, retryParam)
}

// cancelHTTPBridgeCall applies a client response.cancel to an in-flight HTTP
// bridge call. Native transport forwards the event upstream instead.
func (s *responsesWSSession) cancelHTTPBridgeCall(eventType string) bool {
	if eventType != "response.cancel" {
		return false
	}
	state := s.getCurrent()
	if state == nil || state.cancelHTTP == nil {
		return false
	}
	state.cancelHTTP()
	return true
}

// responsesWSSSEForwarder adapts the HTTP relay pipeline's SSE output to a
// Responses WebSocket client. Every SSE data payload written by the relay is a
// complete Responses event, which is exactly what the WebSocket protocol
// carries, so frames are forwarded verbatim. Terminal events are held back
// until the bridge has settled billing and released the session slot, matching
// the native transport where finishCall runs before the terminal event reaches
// the client; otherwise a client that pipelines its next response.create right
// after response.completed would race the cleanup and get a conflict error.
type responsesWSSSEForwarder struct {
	send      func([]byte) error
	cancel    context.CancelFunc
	header    http.Header
	buffer    bytes.Buffer
	held      [][]byte
	status    int
	size      int
	forwarded bool
	sendErr   error
}

func newResponsesWSSSEForwarder(send func([]byte) error, cancel context.CancelFunc) *responsesWSSSEForwarder {
	return &responsesWSSSEForwarder{send: send, cancel: cancel, header: make(http.Header)}
}

func (w *responsesWSSSEForwarder) Header() http.Header { return w.header }

func (w *responsesWSSSEForwarder) Write(data []byte) (int, error) {
	if w.sendErr != nil {
		return 0, w.sendErr
	}
	w.buffer.Write(data)
	w.size += len(data)
	w.forwardCompleteFrames()
	return len(data), w.sendErr
}

func (w *responsesWSSSEForwarder) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *responsesWSSSEForwarder) forwardCompleteFrames() {
	for {
		frame := w.buffer.Bytes()
		end := bytes.Index(frame, []byte("\n\n"))
		if end < 0 {
			return
		}
		payload := responsesWSSSEFrameData(frame[:end])
		w.buffer.Next(end + 2)
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		if responsesWSTerminalEvent(payload) {
			w.held = append(w.held, payload)
			w.forwarded = true
			continue
		}
		if err := w.send(payload); err != nil {
			w.sendErr = err
			if w.cancel != nil {
				w.cancel()
			}
			return
		}
		w.forwarded = true
	}
}

// flushHeldEvents delivers the terminal events retained by
// forwardCompleteFrames. The bridge calls it after billing settlement and
// session-slot release.
func (w *responsesWSSSEForwarder) flushHeldEvents() {
	for _, payload := range w.held {
		if err := w.send(payload); err != nil {
			w.sendErr = err
			break
		}
	}
	w.held = nil
}

func responsesWSTerminalEvent(payload []byte) bool {
	var event struct {
		Type string `json:"type"`
	}
	if common.Unmarshal(payload, &event) != nil {
		return false
	}
	switch event.Type {
	case "response.completed", "response.done", "response.incomplete",
		"response.failed", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

// responsesWSSSEFrameData extracts the data payload of one SSE frame; multiple
// data lines are joined with newlines per the SSE specification. Event, id,
// retry, and comment lines carry no payload for the WebSocket protocol.
func responsesWSSSEFrameData(frame []byte) []byte {
	var payload []byte
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		value := bytes.TrimPrefix(line, []byte("data:"))
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		if payload != nil {
			payload = append(payload, '\n')
		}
		payload = append(payload, value...)
	}
	return payload
}

func (w *responsesWSSSEForwarder) WriteHeader(code int) {
	if code > 0 && w.status == 0 {
		w.status = code
	}
}

func (w *responsesWSSSEForwarder) WriteHeaderNow() {}

func (w *responsesWSSSEForwarder) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *responsesWSSSEForwarder) Size() int { return w.size }

func (w *responsesWSSSEForwarder) Written() bool { return w.forwarded || w.status != 0 }

func (w *responsesWSSSEForwarder) Flush() { w.forwardCompleteFrames() }

func (w *responsesWSSSEForwarder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack is not supported by the responses websocket bridge")
}

func (w *responsesWSSSEForwarder) CloseNotify() <-chan bool { return make(chan bool) }

func (w *responsesWSSSEForwarder) Pusher() http.Pusher { return nil }
