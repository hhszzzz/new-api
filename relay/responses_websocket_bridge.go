package relay

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	appmodel "github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/output"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"

	"github.com/gorilla/websocket"
)

// startHTTPBridgeCall serves one response.create over the HTTP relay pipeline
// when no channel can carry a native Responses WebSocket. The full protocol
// bridge (chat/messages/gemini upstreams) stays available because the call goes
// through the same plan selection and converters as POST /v1/responses; the
// protocol events are delivered directly to the WebSocket output sink.
func (s *responsesWSSession) startHTTPBridgeCall(create responsesWSCreateRequest, eventID string, commitRate middleware.ModelRequestRateLimitCommit) *hosttypes.NewAPIError {
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
		return hosttypes.NewError(err, hosttypes.ErrorCodeReadRequestBodyFailed, hosttypes.ErrOptionWithSkipRetry())
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
		return hosttypes.NewErrorWithStatusCode(errors.New("another response.create is already in progress on this websocket connection"), hosttypes.ErrorCodeInvalidRequest, http.StatusConflict, hosttypes.ErrOptionWithSkipRetry())
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
	forwarder := newResponsesWSEventWriter(func(payload []byte) error {
		if state.rateGuard != nil {
			if err := state.rateGuard.Pace(callCtx, payload); err != nil {
				return err
			}
		}
		return s.writeClient(websocket.TextMessage, payload)
	}, state.cancelHTTP)
	c.Writer = forwarder

	var finalErr *hosttypes.NewAPIError
	var relayInfo *relaycommon.RelayInfo
	defer func() {
		if r := recover(); r != nil {
			logger.LogError(c, fmt.Sprintf("responses websocket http bridge panic: %v", r))
			if relayInfo != nil && relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			if finalErr == nil {
				finalErr = hosttypes.NewError(fmt.Errorf("responses websocket http bridge panic: %v", r), hosttypes.ErrorCodeDoRequestFailed, hosttypes.ErrOptionWithSkipRetry())
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
			apiErr           *hosttypes.NewAPIError
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

func (s *responsesWSSession) selectHTTPBridgeChannel(publicModel string, retryParam *service.RetryParam) (*appmodel.Channel, *hosttypes.NewAPIError) {
	if channelID, retrySameChannel := middleware.PendingAutoProtocolRetryChannelID(s.c); retrySameChannel && !retryParam.IsChannelExcluded(channelID) {
		channel, err := appmodel.CacheGetChannel(channelID)
		if err != nil {
			return nil, hosttypes.NewError(fmt.Errorf("failed to reload channel %d for automatic protocol retry: %w", channelID, err), hosttypes.ErrorCodeGetChannelFailed, hosttypes.ErrOptionWithSkipRetry())
		}
		if channel == nil || channel.Status != common.ChannelStatusEnabled || !channel.IsSchedulableAt(time.Now()) {
			return nil, hosttypes.NewError(fmt.Errorf("channel %d is unavailable for automatic protocol retry", channelID), hosttypes.ErrorCodeGetChannelFailed, hosttypes.ErrOptionWithSkipRetry())
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

// responsesWSEventWriter receives typed protocol events directly. It holds the
// terminal event until settlement and slot release, so the next response.create
// cannot race cleanup. No SSE serialization or parsing occurs in this bridge.
type responsesWSEventWriter struct {
	send              func([]byte) error
	cancel            context.CancelFunc
	header            http.Header
	held              [][]byte
	status            int
	size              int
	forwarded         bool
	terminalDelivered bool
	sendErr           error
}

func newResponsesWSEventWriter(send func([]byte) error, cancel context.CancelFunc) *responsesWSEventWriter {
	return &responsesWSEventWriter{send: send, cancel: cancel, header: make(http.Header)}
}
func (w *responsesWSEventWriter) Header() http.Header { return w.header }
func (w *responsesWSEventWriter) WriteMessage(message output.Message) error {
	if w.sendErr != nil {
		return w.sendErr
	}
	if len(message.Data) == 0 || bytes.Equal(message.Data, []byte("[DONE]")) {
		return nil
	}
	var event struct {
		Type string `json:"type"`
	}
	if err := common.Unmarshal(message.Data, &event); err != nil || event.Type == "" {
		return errors.New("WebSocket output requires a complete protocol event")
	}
	if len(w.held) > 0 || w.terminalDelivered {
		return errors.New("protocol event received after terminal event")
	}
	w.size += len(message.Data)
	if responsesWSTerminalEvent(message.Data) {
		w.held = append(w.held, bytes.Clone(message.Data))
		w.forwarded = true
		return nil
	}
	if err := (output.WebSocket{Send: w.send}).WriteMessage(message); err != nil {
		w.sendErr = err
		if w.cancel != nil {
			w.cancel()
		}
		return err
	}
	w.forwarded = true
	return nil
}
func (w *responsesWSEventWriter) Write(data []byte) (int, error) {
	if err := w.WriteMessage(output.Message{Data: data}); err != nil {
		return 0, err
	}
	return len(data), nil
}
func (w *responsesWSEventWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }
func (w *responsesWSEventWriter) flushHeldEvents() {
	for _, payload := range w.held {
		if err := w.send(payload); err != nil {
			w.sendErr = err
			if w.cancel != nil {
				w.cancel()
			}
			break
		}
		w.terminalDelivered = true
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

func (w *responsesWSEventWriter) WriteHeader(code int) {
	if code > 0 && w.status == 0 {
		w.status = code
	}
}

func (w *responsesWSEventWriter) WriteHeaderNow() {}

func (w *responsesWSEventWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *responsesWSEventWriter) Size() int { return w.size }

func (w *responsesWSEventWriter) Written() bool { return w.forwarded || w.status != 0 }

func (w *responsesWSEventWriter) Flush() {}

func (w *responsesWSEventWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack is not supported by the responses websocket bridge")
}

func (w *responsesWSEventWriter) CloseNotify() <-chan bool { return make(chan bool) }

func (w *responsesWSEventWriter) Pusher() http.Pusher { return nil }
