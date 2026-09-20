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
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	appmodel "github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/output"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// runHTTPBridgeCall runs in the same authenticated request worker as native WS.
// The typed output sink holds its terminal until billing and middleware finish.
func (s *responsesWSSession) runHTTPBridgeCall(c *gin.Context, state *responsesWSCallState, create responsesWSCreateRequest, rateGuard *service.UserRequestRateGuard) (apiErr *hosttypes.NewAPIError) {
	state.info = nil
	if len(create.Generate) > 0 {
		var generate bool
		if err := common.Unmarshal(create.Generate, &generate); err != nil || !generate {
			return newResponsesWSInvalidRequestError(errors.New("generate:false requires a native Responses WebSocket channel"))
		}
	}
	s.stateMu.Lock()
	s.privacy = nil
	s.stateMu.Unlock()
	constraints := service.GetChannelConstraints(c)
	constraints.Filters = slices.DeleteFunc(constraints.Filters, func(filter appdto.ChannelFilter) bool { return filter.Kind == appdto.FilterResponsesWebSocket })
	create.Request.Stream = common.GetPointer(true)
	create.Request.StreamOptions = nil
	body, err := common.Marshal(&create.Request)
	if err != nil {
		return newResponsesWSInvalidRequestError(err)
	}
	common.CleanupBodyStorage(c)
	storage, err := common.CreateBodyStorage(body)
	if err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeReadRequestBodyFailed, hosttypes.ErrOptionWithSkipRetry())
	}
	c.Set(common.KeyBodyStorage, storage)
	callCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	originalRequest, originalWriter := c.Request, c.Writer
	c.Request = c.Request.Clone(callCtx)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Method = http.MethodPost
	forwarder := newResponsesWSEventWriter(func(payload []byte) error {
		if err := rateGuard.Pace(callCtx, payload); err != nil {
			return err
		}
		payload, err := responsesWSBridgeEvent(payload, create.StreamID)
		if err != nil {
			return err
		}
		return s.writeClient(websocket.TextMessage, payload)
	}, cancel)
	c.Writer = forwarder
	defer func() {
		if recovered := recover(); recovered != nil {
			apiErr = hosttypes.NewError(fmt.Errorf("responses websocket HTTP bridge panic: %v", recovered), hosttypes.ErrorCodeBadResponse, hosttypes.ErrOptionWithSkipRetry())
			state.closeAfter = true
		}
		c.Request, c.Writer = originalRequest, originalWriter
		if state.info != nil {
			apiErr = RefundFailedRequestBilling(c, state.info, apiErr)
		}
		if apiErr == nil && len(forwarder.held) > 0 && callCtx.Err() == nil {
			payload, err := responsesWSBridgeEvent(forwarder.held[0], create.StreamID)
			if err != nil {
				apiErr = newResponsesWSInvalidRequestError(err)
			} else {
				state.terminal = &responsesWSMessage{kind: websocket.TextMessage, body: payload}
			}
		}
	}()
	controlDone := make(chan struct{})
	controlCtx, stopControl := context.WithCancel(callCtx)
	defer func() { stopControl(); <-controlDone }()
	go func() {
		defer close(controlDone)
		for {
			select {
			case control := <-state.controls:
				if control.streamID != "" && control.streamID != create.StreamID {
					s.sendError(control.eventID, control.streamID, newResponsesWSInvalidRequestError(errors.New("response is not active on this stream")))
					continue
				}
				cancel()
				return
			case <-controlCtx.Done():
				return
			}
		}
	}()
	retry := middleware.NewResponsesBridgeRetryParam(c, create.Request.Model)
	for ; retry.GetRetry() <= common.RetryTimes; retry.IncreaseRetry() {
		protocolstate.ResetAttempt(c)
		channel, selectErr := s.selectHTTPBridgeChannel(c, create.Request.Model, retry)
		if selectErr != nil {
			return selectErr
		}
		guard, allowed := service.TryAcquireChannelRateLimit(c, channel)
		if !allowed {
			retry.ExcludeChannel(channel.Id)
			retry.ResetRetryNextTry()
			continue
		}
		service.AppendUsedChannel(c, channel.Id)
		defer guard.Release()
		if state.info == nil {
			state.info = relaycommon.GenRelayInfoResponses(c, &create.Request)
			state.info.IsStream = true
			apiErr = PrepareRequestBilling(c, state.info)
		} else {
			state.info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, state.info)
			apiErr = service.PrepareTieredBillingForSelectedGroup(c, state.info)
		}
		if apiErr != nil {
			guard.Release()
			return apiErr
		}
		service.RequestPolicy(c).BeginAttempt(channel, state.info.UsingGroup)
		apiErr = ResponsesHelper(c, state.info)
		guard.Release()
		if apiErr == nil {
			middleware.CommitAutoProtocolAffinity(c)
			if err := protocolstate.Commit(c); err != nil {
				logger.LogError(c, "failed to persist Responses WebSocket protocol state: "+err.Error())
			}
			service.RecordChannelAffinity(c, channel.Id)
			return nil
		}
		if callCtx.Err() != nil || forwarder.Written() {
			return apiErr
		}
		if protocolstate.EnableReplayFallback(c, apiErr) {
			retry.SetRetry(0)
			retry.ResetRetryNextTry()
			continue
		}
		if middleware.AdvanceAutoProtocolAttempt(c, apiErr) {
			retry.ResetRetryNextTry()
			continue
		}
		apiErr = service.NormalizeViolationFeeError(apiErr)
		decision := service.DecideRelayRetry(c, apiErr, common.RetryTimes-retry.GetRetry())
		service.RecordPolicyFailure(c, channel.Id, apiErr, decision)
		service.ProcessChannelError(c, *hosttypes.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, state.info.ApiKey, channel.GetAutoBan()), apiErr, state.info)
		if decision.Action != "retry" {
			return apiErr
		}
	}
	return apiErr
}

func responsesWSBridgeEvent(payload []byte, streamID string) ([]byte, error) {
	if streamID == "" {
		return payload, nil
	}
	var event map[string]common.RawMessage
	if err := common.Unmarshal(payload, &event); err != nil {
		return nil, err
	}
	event["stream_id"], _ = common.Marshal(streamID)
	return common.Marshal(event)
}

func (s *responsesWSSession) selectHTTPBridgeChannel(c *gin.Context, publicModel string, retry *service.RetryParam) (*appmodel.Channel, *hosttypes.NewAPIError) {
	if channelID, sameChannel := middleware.PendingAutoProtocolRetryChannelID(c); sameChannel && !retry.IsChannelExcluded(channelID) {
		channel, err := appmodel.CacheGetChannel(channelID)
		if err != nil || channel == nil || !channel.IsSchedulableAt(time.Now()) {
			return nil, hosttypes.NewError(fmt.Errorf("channel %d is unavailable for automatic protocol retry", channelID), hosttypes.ErrorCodeGetChannelFailed, hosttypes.ErrOptionWithSkipRetry())
		}
		if apiErr := middleware.SetupContextForSelectedChannel(c, channel, publicModel, true); apiErr != nil {
			return nil, apiErr
		}
		return channel, nil
	}
	return middleware.SelectResponsesBridgeChannel(c, publicModel, retry)
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
