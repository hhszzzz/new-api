package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	appmodel "github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const responsesWSEventTypeResponseCreate = "response.create"
const responsesWSWriteTimeout = 30 * time.Second

// ResponsesWSRequestRunner executes the existing authentication and rate-limit
// middleware around one complete request, without a second HTTP connection.
type ResponsesWSRequestRunner func(*http.Request, string, func(*gin.Context) *types.NewAPIError) *types.NewAPIError

type responsesWSCreateEvent struct {
	Type     string            `json:"type"`
	EventID  string            `json:"event_id,omitempty"`
	StreamID common.RawMessage `json:"stream_id,omitempty"`
	Generate common.RawMessage `json:"generate,omitempty"`
	Request  common.RawMessage `json:"response,omitempty"`
}

type responsesWSCreateRequest struct {
	Request  dto.OpenAIResponsesRequest
	Body     []byte
	Generate common.RawMessage
	StreamID string
}

type responsesWSErrorEvent struct {
	Type       string             `json:"type"`
	Status     int                `json:"status"`
	EventID    string             `json:"event_id,omitempty"`
	StreamID   string             `json:"stream_id,omitempty"`
	ResponseID string             `json:"response_id,omitempty"`
	Error      *types.OpenAIError `json:"error"`
}

type responsesWSMessage struct {
	kind int
	body []byte
	err  error
}

// responsesWSControl is a client control event (response.cancel) whose
// envelope the read loop already parsed.
type responsesWSControl struct {
	body              []byte
	eventID, streamID string
}

// Only the request worker reads or changes billing state. Socket readers pass
// bounded messages to it; cancellation never performs an independent refund.
type responsesWSCallState struct {
	info       *relaycommon.RelayInfo
	inbox      chan responsesWSMessage
	controls   chan responsesWSControl
	done       chan struct{}
	terminal   *responsesWSMessage
	closeAfter bool
}

type responsesWSSession struct {
	ctx            context.Context
	cancel         context.CancelFunc
	client         *websocket.Conn
	runner         ResponsesWSRequestRunner
	request        *http.Request
	requestID      string
	nextEventIndex int
	workers        sync.WaitGroup

	clientWriteMu         sync.Mutex
	targetWriteMu         sync.Mutex
	connectionMu          sync.Mutex
	target                *websocket.Conn
	unregister            func()
	stateMu               sync.Mutex
	current               *responsesWSCallState
	privacy               *relaycommon.RelayInfo
	nativeTransportFailed bool

	// These fields belong to the serial request worker and describe the actual
	// established connection. Per-request token/user data is never stored here.
	lastResponseID  string
	lockedModel     string
	lockedChannelID int
	lockedGroup     string
	lockedKey       string
	lockedKeyIndex  int
	lockedContext   map[appconstant.ContextKey]any
	lockedRoute     appdto.AdvancedCustomRoute
}

func ResponsesWebSocketHelper(c *gin.Context, client *websocket.Conn, runner ResponsesWSRequestRunner) *types.NewAPIError {
	ctx, cancel := context.WithCancel(c.Request.Context())
	s := &responsesWSSession{ctx: ctx, cancel: cancel, client: client, runner: runner,
		request: c.Request.Clone(ctx), requestID: c.GetString(common.RequestIdKey)}
	if s.requestID == "" {
		s.requestID = common.NewRequestId()
	}
	maxMB := appconstant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128
	}
	client.SetReadLimit(int64(maxMB) << 20)
	defer func() {
		s.shutdown()
		s.workers.Wait()
	}()

	for {
		_, message, err := client.ReadMessage()
		if err != nil {
			return nil
		}
		envelope, streamID, err := parseResponsesWSEnvelope(message)
		eventType := envelope.Type
		if err != nil {
			s.sendError(envelope.EventID, streamID, newResponsesWSInvalidRequestError(err))
			continue
		}
		if eventType != responsesWSEventTypeResponseCreate {
			// Controls are owned by the active request too. In particular a cancel
			// arriving during authentication must not precede its upstream create.
			state := s.getCurrent()
			if eventType != "response.cancel" || state == nil {
				s.sendError(envelope.EventID, streamID, newResponsesWSInvalidRequestError(fmt.Errorf("unsupported websocket event %q", eventType)))
				continue
			}
			select {
			case state.controls <- responsesWSControl{body: message, eventID: envelope.EventID, streamID: streamID}:
			case <-state.done:
			case <-s.ctx.Done():
				return nil
			default:
				s.sendError(envelope.EventID, streamID, newResponsesWSInvalidRequestError(errors.New("a response control event is already pending")))
			}
			continue
		}
		state := &responsesWSCallState{inbox: make(chan responsesWSMessage), controls: make(chan responsesWSControl, 1), done: make(chan struct{})}
		if !s.tryReserveCurrent(state) {
			s.sendError(envelope.EventID, streamID, types.NewErrorWithStatusCode(errors.New("another response.create is already in progress on this websocket connection"), types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry()))
			continue
		}
		requestID := fmt.Sprintf("%s-ws-%d", s.requestID, s.nextEventIndex)
		s.nextEventIndex++
		s.workers.Go(func() { s.runRequest(state, message, envelope, streamID, requestID) })
	}
}

func (s *responsesWSSession) runRequest(state *responsesWSCallState, message []byte, envelope responsesWSCreateEvent, streamID string, requestID string) {
	create, parseErr := normalizeResponsesWSCreateEvent(message, envelope, streamID)
	var apiErr *types.NewAPIError
	defer func() {
		if recovered := recover(); recovered != nil {
			apiErr = types.NewError(fmt.Errorf("responses websocket request panic: %v", recovered), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			state.closeAfter = true
		}
		// Finish the middleware count before publishing the terminal event. Hold
		// admission while writing it so an immediate next create cannot race
		// the current request's release; socket close needs neither lock.
		outgoing := state.terminal
		if apiErr != nil {
			if body, err := buildResponsesWSErrorPayload(envelope.EventID, streamID, apiErr); err == nil {
				outgoing = &responsesWSMessage{kind: websocket.TextMessage, body: body}
			}
		}
		if outgoing != nil {
			redacted, err := relaycommon.RedactUserModelRouteJSON(outgoing.body, state.info)
			if err != nil {
				outgoing = nil
				state.closeAfter = true
			} else {
				outgoing.body = redacted
			}
		}
		s.clientWriteMu.Lock()
		s.stateMu.Lock()
		if outgoing != nil {
			if err := s.client.SetWriteDeadline(time.Now().Add(responsesWSWriteTimeout)); err != nil {
				state.closeAfter = true
			} else if err := s.client.WriteMessage(outgoing.kind, outgoing.body); err != nil {
				state.closeAfter = true
			}
		}
		s.current = nil
		close(state.done)
		s.stateMu.Unlock()
		s.clientWriteMu.Unlock()
		if state.closeAfter {
			s.shutdown()
		}
	}()
	request := s.request.Clone(s.ctx)
	request.Body = io.NopCloser(bytes.NewReader(message))
	request.ContentLength = int64(len(message))
	request.Header.Set("Content-Type", "application/json")
	apiErr = s.runner(request, requestID, func(c *gin.Context) *types.NewAPIError {
		if parseErr != nil {
			return newResponsesWSInvalidRequestError(parseErr)
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(create.Body))
		c.Request.ContentLength = int64(len(create.Body))
		return s.runCall(c, state, create)
	})
	// Prompt inspection rejects one response.create, not the authenticated
	// WebSocket session. Keep the connection available so the client can submit
	// a corrected request; credential and account authorization failures still
	// invalidate the session.
	if apiErr != nil && (apiErr.StatusCode == http.StatusUnauthorized ||
		apiErr.StatusCode == http.StatusForbidden && apiErr.GetErrorCode() != types.ErrorCodePromptAuditBlocked) {
		state.closeAfter = true
	}
}

func (s *responsesWSSession) runCall(c *gin.Context, state *responsesWSCallState, create responsesWSCreateRequest) (apiErr *types.NewAPIError) {
	policy := service.RequestPolicy(c)
	modelName := create.Request.Model
	started := time.Now()
	var info *relaycommon.RelayInfo
	billingPrepared := false
	var accumulator *service.ResponsesUsageAccumulator
	accepted := false
	defer func() {
		if recovered := recover(); recovered != nil {
			apiErr = types.NewError(fmt.Errorf("responses websocket call panic: %v", recovered), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			state.closeAfter = true
		}
		if info == nil {
			info = state.info
		}
		if info == nil && modelName != "" {
			info = &relaycommon.RelayInfo{OriginModelName: modelName, UsingGroup: common.GetContextKeyString(c, appconstant.ContextKeyUsingGroup), StartTime: started}
		}
		// Once accepted, every exit (including cancellation or protocol-state
		// failure) settles the usage already produced by upstream exactly once.
		if accumulator != nil && (accepted || apiErr == nil) {
			ConsumeResponsesQuota(c, info, accumulator.Finish())
		}
		perfmetrics.RecordRelayResult(c.Request.Context(), info, apiErr)
		// Settlement already marks the request policy successful, and nothing
		// reads a termination decision after this point on the WebSocket path,
		// so neither policy record belongs here.
		if info != nil && billingPrepared {
			apiErr = RefundFailedRequestBilling(c, info, apiErr)
		}
		if info != nil {
			info.CloseConversionSession()
		}
	}()
	if modelName == "" {
		return newResponsesWSInvalidRequestError(errors.New("model is required"))
	}
	if s.lockedModel != "" && modelName != s.lockedModel {
		return newResponsesWSInvalidRequestError(fmt.Errorf("responses websocket connection is locked to model %q", s.lockedModel))
	}
	if apiErr = checkResponsesWSModelAccess(c, modelName); apiErr != nil {
		return apiErr
	}
	validated, validateErr := helper.GetAndValidateResponsesRequest(c)
	if validateErr != nil {
		return newResponsesWSInvalidRequestError(validateErr)
	}
	// Keep streaming semantics throughout the shared HTTP request preparation;
	// buildResponsesWSCreateEvent removes HTTP-only stream fields on the wire.
	validated.Stream, validated.StreamOptions = common.GetPointer(true), nil
	create.Request = *validated
	if apiErr = middleware.PrepareResponsesWebSocketRequest(c, modelName, create.Body); apiErr != nil {
		return apiErr
	}
	result, auditErr := service.InspectPrompt(c, service.PromptAuditRequest{Snapshot: dto.PromptAuditSnapshotOf(validated), Protocol: "openai_responses", Model: modelName, Stage: "responses_websocket", CoverageIncomplete: strings.TrimSpace(validated.PreviousResponseID) != "", Stream: true})
	if auditErr != nil {
		service.RecordPromptAuditError(c, result, auditErr, modelName, true)
		return auditErr
	}
	common.SetContextKey(c, appconstant.ContextKeyPromptAuditChecked, true)
	outputAuditSetting := prompt_audit_setting.GetSetting()
	outputAuditMode := outputAuditSetting.OutputMode
	if !service.PromptAuditAppliesToGroup(c, outputAuditSetting, outputAuditMode) {
		outputAuditMode = prompt_audit_setting.ModeOff
	}
	var outputAuditFrames *promptOutputCapture
	if outputAuditMode == prompt_audit_setting.ModeBlocking {
		outputAuditFrames = &promptOutputCapture{maxBytes: outputAuditSetting.OutputMaxBytes, memoryBytes: outputAuditSetting.OutputMemoryBytes}
		defer outputAuditFrames.Close()
	}
	outputAuditCollector := newPromptAuditTextCollector()
	group := common.GetContextKeyString(c, appconstant.ContextKeyTokenGroup)
	if group == "" {
		group = common.GetContextKeyString(c, appconstant.ContextKeyUserGroup)
	}
	userPolicy, policyErr := service.LoadUserRateLimitPolicy(common.GetContextKeyInt(c, appconstant.ContextKeyUserId), group)
	if policyErr != nil {
		return types.NewError(policyErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	rateGuard, rateErr := service.BeginUserRequestRateLimit(c, userPolicy, modelName, service.UserConcurrencyWaitOptions{Heartbeat: func() error {
		return s.client.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second))
	}})
	if rateErr != nil {
		return rateErr
	}
	defer rateGuard.Release()
	service.InstallUserStreamPacer(c, rateGuard.Pacer)
	defer service.InstallUserStreamPacer(c, nil)
	if s.nativeTransportFailed {
		return s.runHTTPBridgeCall(c, state, create, rateGuard)
	}
	common.SetContextKey(c, appconstant.ContextKeyOriginalModel, modelName)
	common.SetContextKey(c, appconstant.ContextKeyRequestStartTime, time.Now())
	service.GetChannelConstraints(c).AddFilter(appdto.ChannelFilter{Kind: appdto.FilterRequestPath, RequestPath: c.Request.URL.Path})

	if s.lockedChannelID != 0 {
		if apiErr = s.restoreConnectionContext(c, modelName); apiErr != nil {
			return apiErr
		}
		lockedChannel, err := appmodel.CacheGetChannel(s.lockedChannelID)
		if err != nil {
			return types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		channelGuard, allowed := service.TryAcquireChannelRateLimit(c, lockedChannel)
		if !allowed {
			return service.NewChannelRateLimitError()
		}
		defer channelGuard.Release()
		info = relaycommon.GenRelayInfoResponses(c, &create.Request)
		state.info = info
		info.IsStream = true
		common.SetContextKey(c, appconstant.ContextKeyIsStream, true)
		if apiErr = PrepareRequestBilling(c, info); apiErr != nil {
			return apiErr
		}
		billingPrepared = true
		var payload []byte
		payload, apiErr = buildResponsesWSCreatePayload(c, info, create.Request, create.Generate, create.StreamID)
		if apiErr != nil {
			return apiErr
		}
		if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
			state.closeAfter = true
			return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
	} else {
		retry := middleware.NewResponsesWebSocketRetryParam(c, modelName)
		for ; retry.GetRetry() <= common.RetryTimes; retry.IncreaseRetry() {
			var channel *appmodel.Channel
			channel, apiErr = selectResponsesWSChannel(c, modelName, retry)
			if apiErr != nil {
				break
			}
			channelGuard, allowed := service.TryAcquireChannelRateLimit(c, channel)
			if !allowed {
				retry.ExcludeChannel(channel.Id)
				retry.ResetRetryNextTry()
				continue
			}
			defer channelGuard.Release()
			service.AppendUsedChannel(c, channel.Id)
			if info == nil {
				info = relaycommon.GenRelayInfoResponses(c, &create.Request)
				state.info = info
				info.IsStream = true
				common.SetContextKey(c, appconstant.ContextKeyIsStream, true)
				if apiErr = PrepareRequestBilling(c, info); apiErr != nil {
					return apiErr
				}
				billingPrepared = true
			} else {
				info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)
				if apiErr = service.PrepareTieredBillingForSelectedGroup(c, info); apiErr != nil {
					return apiErr
				}
			}
			info.RetryIndex = retry.GetRetry()
			policy.BeginAttempt(channel, info.UsingGroup)
			var payload []byte
			payload, apiErr = buildResponsesWSCreatePayload(c, info, create.Request, create.Generate, create.StreamID)
			if apiErr != nil {
				return apiErr
			}
			adaptor := GetAdaptor(info.ApiType)
			adaptor.Init(info)
			target, dialErr := relaychannel.DoWssRequest(adaptor, c, info, nil)
			if dialErr != nil {
				channelGuard.Release()
				apiErr = service.NormalizeViolationFeeError(types.NewError(dialErr, types.ErrorCodeDoRequestFailed))
				service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
				info.LastError = apiErr
				decision := service.DecideRelayRetry(c, apiErr, common.RetryTimes-retry.GetRetry())
				service.RecordPolicyFailure(c, channel.Id, apiErr, decision)
				service.ProcessChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, info.ApiKey, channel.GetAutoBan()), apiErr, info)
				if decision.Action == "retry" {
					continue
				}
				break
			}
			if !s.setTarget(target) {
				return types.NewError(context.Canceled, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
			if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
				state.closeAfter = true
				return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
			s.lockedModel, s.lockedChannelID, s.lockedGroup = modelName, channel.Id, info.UsingGroup
			s.lockedKey, s.lockedKeyIndex = info.ApiKey, info.ChannelMultiKeyIndex
			s.lockedRoute, _ = channel.GetOtherSettings().AdvancedCustom.MatchPathForModel(c.Request.URL.Path, info.SelectionModelName())
			s.lockedContext = make(map[appconstant.ContextKey]any)
			for _, key := range []appconstant.ContextKey{
				appconstant.ContextKeyChannelId, appconstant.ContextKeyChannelName, appconstant.ContextKeyChannelType,
				appconstant.ContextKeyChannelCreateTime, appconstant.ContextKeyChannelSetting, appconstant.ContextKeyChannelOtherSetting,
				appconstant.ContextKeyChannelParamOverride, appconstant.ContextKeyChannelHeaderOverride, appconstant.ContextKeyChannelOrganization,
				appconstant.ContextKeyChannelAutoBan, appconstant.ContextKeyChannelModelMapping, appconstant.ContextKeyChannelStatusCodeMapping,
				appconstant.ContextKeyChannelIsMultiKey, appconstant.ContextKeyChannelMultiKeyIndex, appconstant.ContextKeyChannelKey, appconstant.ContextKeyChannelBaseUrl,
			} {
				if value, ok := c.Get(string(key)); ok {
					s.lockedContext[key] = value
				}
			}
			s.registerChannelClose(channel.Id)
			if !service.IsChannelAvailableForActiveWebSocket(channel.Id) {
				s.closeForPolicy(service.ChannelDisabledCloseReason)
				return types.NewError(errors.New("channel is unavailable"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
			}
			s.startTargetReader(target)
			apiErr = nil
			break
		}
		if apiErr != nil {
			if info != nil && info.Billing != nil {
				info.Billing.Refund(c)
			}
			billingPrepared = false
			info = nil
			s.nativeTransportFailed = true
			return s.runHTTPBridgeCall(c, state, create, rateGuard)
		}
	}
	s.stateMu.Lock()
	s.privacy = &relaycommon.RelayInfo{OriginModelName: info.OriginModelName, UserModelRouteId: info.UserModelRouteId, RouteTargetModelName: info.RouteTargetModelName, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: info.UpstreamModelName, IsModelMapped: info.IsModelMapped}}
	s.stateMu.Unlock()

	accumulator = service.NewResponsesUsageAccumulator(info)
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.RequireTerminal()
	common.SetContextKey(c, appconstant.ContextKeyResponseStreamStatus, info.StreamStatus)
	timeout := time.Duration(appconstant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	idle := time.NewTimer(timeout)
	defer idle.Stop()
	var pendingControl []byte
	var sentControl []byte
	var responseID string
	for {
		select {
		case incoming := <-state.inbox:
			idle.Reset(timeout)
			if incoming.err != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, incoming.err)
				state.closeAfter = true
				return nil
			}
			info.SetFirstResponseTime()
			var event struct {
				dto.ResponsesStreamResponse
				StreamID string `json:"stream_id"`
			}
			bufferForOutputAudit := outputAuditMode == prompt_audit_setting.ModeBlocking
			if err := common.Unmarshal(incoming.body, &event); err != nil {
				info.StreamStatus.RecordError("invalid upstream websocket event")
			} else {
				bufferForOutputAudit = outputAuditMode == prompt_audit_setting.ModeBlocking && strings.HasPrefix(event.Type, "response.")
				if event.Type != "error" && event.StreamID != "" && event.StreamID != create.StreamID {
					if err := s.writeClient(incoming.kind, incoming.body); err != nil {
						s.shutdown()
					}
					continue
				}
				// A repeated terminal from the previous response must never finish
				// a subsequent request on this persistent connection.
				if event.Response != nil && event.Response.ID != "" && event.Response.ID == s.lastResponseID {
					continue
				}
				if event.Type == "error" {
					var rejection responsesWSErrorEvent
					_ = common.Unmarshal(incoming.body, &rejection)
					if rejection.ResponseID != "" && rejection.ResponseID == s.lastResponseID {
						continue
					}
					terminal, ambiguous, controlError := responsesWSErrorEndsRequest(rejection, create.StreamID, responseID, sentControl)
					if !terminal {
						if err := s.writeClient(incoming.kind, incoming.body); err != nil {
							s.shutdown()
						}
						// Only a control error in this stream resolves its pending control.
						if controlError {
							sentControl = nil
						}
						continue
					}
					if accepted {
						if rejection.Error != nil {
							code := ""
							if rejection.Error.Code != nil {
								code = fmt.Sprint(rejection.Error.Code)
							}
							info.StreamStatus.MarkFailed(code, rejection.Error.Type, rejection.Status)
						}
						accumulator.Observe(&event.ResponsesStreamResponse)
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
						s.lastResponseID = responseID
						state.terminal, state.closeAfter = &incoming, ambiguous
						return nil
					}
					if rejection.Error == nil {
						// An error frame without an error object still describes the
						// rejected request; keep the client-facing type stable.
						rejection.Error = &types.OpenAIError{Type: "invalid_request_error", Message: event.Message, Code: event.Code}
					}
					rejected := types.WithOpenAIError(*rejection.Error, rejection.Status, types.ErrOptionWithSkipRetry())
					if rejected.StatusCode < 400 || rejected.StatusCode > 599 {
						rejected.StatusCode = http.StatusBadRequest
					}
					return rejected
				}
				if strings.HasPrefix(event.Type, "response.") {
					if !accepted {
						// Like HTTP, bind the session only once upstream accepted the request.
						service.RecordChannelAffinity(c, s.lockedChannelID)
					}
					accepted = true
					if event.Response != nil && event.Response.ID != "" {
						responseID = event.Response.ID
					}
				}
				accumulator.Observe(&event.ResponsesStreamResponse)
			}
			if event.Response != nil {
				if err := protocolstate.ValidateResponsesContinuation(c, event.Response.PreviousResponseID); err != nil {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonProtocolError, err)
					return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
			}
			if protocolstate.PublicResponseID(c, "") != "" {
				var err error
				incoming.body, err = protocolstate.ObserveResponsesStreamData(c, &event.ResponsesStreamResponse, incoming.body)
				if err != nil {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonProtocolError, err)
					return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
			} else {
				protocolstate.ObserveResponsesStream(c, &event.ResponsesStreamResponse)
			}
			if outputAuditMode != prompt_audit_setting.ModeOff && strings.HasPrefix(event.Type, "response.") {
				var visible any
				if common.Unmarshal(incoming.body, &visible) == nil {
					outputAuditCollector.Collect(visible, true)
				}
				if outputAuditFrames != nil {
					if err := outputAuditFrames.WriteFrame(incoming.kind, incoming.body); err != nil {
						service.RecordOutputAuditUnavailable(c, service.PromptAuditRequest{
							Snapshot: dto.PromptAuditSnapshotOf(validated), Protocol: "openai_responses", Model: modelName,
							Stage: "responses_websocket", Direction: service.PromptAuditDirectionOutput,
							GenerationID: responseID, DeliveryStatus: "not_delivered", CoverageComplete: false,
							Output: outputAuditCollector.String(), Stream: true,
						}, "output_buffer_limit")
						state.closeAfter = true
						return types.NewErrorWithStatusCode(errors.New("output audit buffer limit exceeded"), types.ErrorCodeOutputAuditUnavailable, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
					}
				}
			}
			switch event.Type {
			case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled":
				coverageComplete := event.Type == "response.completed" || event.Type == "response.done"
				if event.Response != nil {
					s.lastResponseID = event.Response.ID
				}
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				state.terminal = &incoming
				if coverageComplete {
					protocolstate.MarkStreamCompleted(c)
					if err := protocolstate.Commit(c); err != nil {
						logger.LogError(c, "failed to persist Responses WebSocket protocol state: "+err.Error())
					}
				}
				if outputAuditMode != prompt_audit_setting.ModeOff {
					outputText := outputAuditCollector.String()
					if (!coverageComplete || outputText == "") && outputAuditMode == prompt_audit_setting.ModeBlocking {
						service.RecordOutputAuditUnavailable(c, service.PromptAuditRequest{
							Snapshot: dto.PromptAuditSnapshotOf(validated), Protocol: "openai_responses", Model: modelName,
							Stage: "responses_websocket", Direction: service.PromptAuditDirectionOutput,
							GenerationID: responseID, DeliveryStatus: "not_delivered", CoverageComplete: false,
							Output: outputText, Stream: true,
						}, "output_incomplete")
						state.closeAfter = true
						return types.NewErrorWithStatusCode(errors.New("generated output could not be extracted for audit"), types.ErrorCodeOutputAuditUnavailable, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
					}
					if outputText == "" {
						service.RecordOutputAuditUnavailable(c, service.PromptAuditRequest{
							Snapshot: dto.PromptAuditSnapshotOf(validated), Protocol: "openai_responses", Model: modelName,
							Stage: "responses_websocket", Direction: service.PromptAuditDirectionOutput,
							GenerationID: responseID, DeliveryStatus: "delivered_incomplete", CoverageComplete: false, Stream: true,
						}, "output_extract_failed")
						return nil
					}
					deliveryStatus := "delivered"
					if outputAuditMode == prompt_audit_setting.ModeBlocking {
						deliveryStatus = "not_delivered"
					}
					result, outputErr := service.InspectOutput(c, service.PromptAuditRequest{
						Snapshot: dto.PromptAuditSnapshotOf(validated), Protocol: "openai_responses", Model: modelName,
						Stage: "responses_websocket", Direction: service.PromptAuditDirectionOutput,
						GenerationID: responseID, DeliveryStatus: deliveryStatus, CoverageComplete: coverageComplete,
						Output: outputText, Stream: true,
					})
					if outputAuditMode == prompt_audit_setting.ModeBlocking && outputErr != nil {
						_ = appmodel.UpdatePromptAuditDelivery(result.AuditID, "not_delivered")
						state.closeAfter = true
						code, status, message := types.ErrorCodeOutputAuditUnavailable, http.StatusServiceUnavailable, "output audit is unavailable"
						if result.Blocked && result.Decision == service.PromptAuditDecisionBlock {
							code, status, message = types.ErrorCodeOutputAuditBlocked, http.StatusForbidden, "generated output blocked by content audit"
						}
						return types.NewErrorWithStatusCode(errors.New(message), code, status, types.ErrOptionWithSkipRetry())
					}
					if outputAuditFrames != nil {
						reader, err := outputAuditFrames.Reader()
						if err == nil {
							err = readPromptOutputFrames(reader, func(kind int, body []byte) error { return s.writeClient(kind, body) })
						}
						if err != nil {
							_ = appmodel.UpdatePromptAuditDelivery(result.AuditID, "delivery_failed")
							state.closeAfter = true
							return types.NewErrorWithStatusCode(errors.New("output delivery failed after audit"), types.ErrorCodeOutputAuditUnavailable, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
						}
						_ = appmodel.UpdatePromptAuditDelivery(result.AuditID, "delivered")
						state.terminal = nil
					}
				}
				return nil
			}
			if err := rateGuard.Pace(c.Request.Context(), incoming.body); err != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
				return types.NewError(err, types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
			}
			if accepted && pendingControl != nil {
				if err := s.writeTarget(websocket.TextMessage, pendingControl); err != nil {
					s.shutdown()
				}
				sentControl = pendingControl
				pendingControl = nil
			}
			if bufferForOutputAudit {
				continue
			}
			if err := s.writeClient(incoming.kind, incoming.body); err != nil {
				s.shutdown()
			}
		case control := <-state.controls:
			if pendingControl != nil || sentControl != nil {
				s.sendError(control.eventID, control.streamID, newResponsesWSInvalidRequestError(errors.New("a response control event is already pending")))
				continue
			}
			if !accepted {
				pendingControl = control.body
				continue
			}
			if err := s.writeTarget(websocket.TextMessage, control.body); err != nil {
				s.shutdown()
			}
			sentControl = control.body
		case <-idle.C:
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, context.DeadlineExceeded)
			state.closeAfter = true
			return nil
		case <-s.ctx.Done():
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, s.ctx.Err())
			return nil
		}
	}
}

// A single generation is active, but a cancel can fail independently. Correlate
// available IDs first; uncorrelated control/server errors require a bounded
// connection close because continuing could leave the generation stuck forever.
func responsesWSErrorEndsRequest(event responsesWSErrorEvent, streamID, responseID string, control []byte) (terminal, ambiguous, controlError bool) {
	if len(control) > 0 {
		var pending struct {
			EventID    string `json:"event_id"`
			StreamID   string `json:"stream_id"`
			ResponseID string `json:"response_id"`
		}
		_ = common.Unmarshal(control, &pending)
		if event.EventID != "" && event.EventID == pending.EventID {
			return false, false, true
		}
		if event.ResponseID != "" && event.ResponseID == pending.ResponseID && event.ResponseID != responseID {
			return false, false, true
		}
		if (event.StreamID == "" || event.StreamID == pending.StreamID) && event.Error != nil && event.Error.Type == "invalid_request_error" {
			switch event.Error.Code {
			case "response_not_found", "response_not_active", "response_already_completed":
				return false, false, true
			}
		}
	}
	if event.StreamID != "" && event.StreamID != streamID || responseID != "" && event.ResponseID != "" && event.ResponseID != responseID {
		return false, false, false
	}
	return true, len(control) > 0 && event.ResponseID == "", false
}

func (s *responsesWSSession) restoreConnectionContext(c *gin.Context, model string) *types.NewAPIError {
	// The channel type is not rechecked: a locked channel keeps its type, and
	// the FilterResponsesWebSocket rules were applied when it was selected.
	channel, err := appmodel.CacheGetChannel(s.lockedChannelID)
	if err != nil || channel == nil || !channel.IsSchedulableAt(time.Now()) || !channel.GetSetting().ResponsesWebSocketEnabled {
		return types.NewErrorWithStatusCode(errors.New("Responses WebSocket is disabled for this channel"), types.ErrorCode(appdto.FilterResponsesWebSocket), http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	keyEnabled := channel.Key == s.lockedKey
	if channel.ChannelInfo.IsMultiKey {
		keys := channel.GetKeys()
		status := channel.ChannelInfo.MultiKeyStatusList[s.lockedKeyIndex]
		keyEnabled = s.lockedKeyIndex >= 0 && s.lockedKeyIndex < len(keys) && keys[s.lockedKeyIndex] == s.lockedKey && (status == 0 || status == common.ChannelStatusEnabled)
	}
	if !keyEnabled {
		return types.NewErrorWithStatusCode(errors.New("the upstream connection credential is no longer enabled"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	// Changes that alter the physical upstream connection require a new
	// handshake. Request-level settings can be refreshed without rotating keys.
	if channel.GetBaseURL() != s.lockedContext[appconstant.ContextKeyChannelBaseUrl] ||
		!reflect.DeepEqual(channel.GetHeaderOverride(), s.lockedContext[appconstant.ContextKeyChannelHeaderOverride]) {
		return types.NewErrorWithStatusCode(errors.New("upstream connection settings changed; reconnect required"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	if previous, ok := s.lockedContext[appconstant.ContextKeyChannelSetting].(appdto.ChannelSettings); ok && previous.Proxy != channel.GetSetting().Proxy {
		return types.NewErrorWithStatusCode(errors.New("upstream proxy changed; reconnect required"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	// An advanced custom connection was dialed through the route matched for
	// this model. Only the fields that shape the upstream handshake force a
	// reconnect: target path, protocol conversion and credential placement.
	// Other channel types match the zero route on both sides.
	retry := middleware.NewResponsesWebSocketRetryParam(c, model)
	route, _ := channel.GetOtherSettings().AdvancedCustom.MatchPathForModel(c.Request.URL.Path, retry.ModelName)
	if strings.TrimSpace(route.UpstreamPath) != strings.TrimSpace(s.lockedRoute.UpstreamPath) ||
		route.IsNative() != s.lockedRoute.IsNative() ||
		!reflect.DeepEqual(route.Auth, s.lockedRoute.Auth) {
		return types.NewErrorWithStatusCode(errors.New("upstream route changed; reconnect required"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	if pin, found, _ := service.GetChannelConstraints(c).ResolvedPin(); found {
		if pin.ChannelId != s.lockedChannelID {
			return types.NewErrorWithStatusCode(errors.New("channel pin changed; reconnect required"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
	}
	if apiErr := middleware.RestoreResponsesWebSocketChannel(c, channel, model, s.lockedGroup); apiErr != nil {
		return apiErr
	}
	for key, value := range s.lockedContext {
		c.Set(string(key), value)
	}
	common.SetContextKey(c, appconstant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, appconstant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	common.SetContextKey(c, appconstant.ContextKeyChannelParamOverride, channel.GetParamOverride())
	common.SetContextKey(c, appconstant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, appconstant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())
	policy := service.RequestPolicy(c)
	policy.BeginAttempt(channel, s.lockedGroup)
	policy.AddEvent(service.PolicyEvent{ChannelID: channel.Id, Decision: service.PolicyDecision{Action: "select", Reason: "pinned_channel", Source: "channel_constraint"}})
	return nil
}

func buildResponsesWSCreatePayload(c *gin.Context, info *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest, generate common.RawMessage, streamID string) ([]byte, *types.NewAPIError) {
	_, body, closer, apiErr := PrepareResponsesRequest(c, info, &req)
	if apiErr != nil {
		return nil, apiErr
	}
	defer closer.Close()
	jsonData, err := io.ReadAll(body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	event, err := buildResponsesWSCreateEvent(jsonData, generate, streamID)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	return event, nil
}

func (s *responsesWSSession) startTargetReader(target *websocket.Conn) {
	limit := int64(helper.DefaultMaxScannerBufferSize)
	if appconstant.StreamScannerMaxBufferMB > 0 {
		limit = int64(appconstant.StreamScannerMaxBufferMB) << 20
	}
	target.SetReadLimit(limit)
	s.workers.Go(func() {
		for {
			kind, body, err := target.ReadMessage()
			incoming := responsesWSMessage{kind: kind, body: body, err: err}
			if state := s.getCurrent(); state != nil {
				select {
				case state.inbox <- incoming:
				case <-state.done:
					if err == nil {
						if writeErr := s.writeClient(kind, body); writeErr != nil {
							s.shutdown()
							return
						}
					} else {
						s.shutdown()
					}
				case <-s.ctx.Done():
					return
				}
			} else if err == nil {
				if writeErr := s.writeClient(kind, body); writeErr != nil {
					s.shutdown()
					return
				}
			} else {
				s.shutdown()
			}
			if err != nil {
				return
			}
		}
	})
}

func (s *responsesWSSession) getCurrent() *responsesWSCallState {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.current
}

func (s *responsesWSSession) tryReserveCurrent(state *responsesWSCallState) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.current != nil {
		return false
	}
	s.current = state
	return true
}

func (s *responsesWSSession) getTarget() *websocket.Conn {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	return s.target
}

func (s *responsesWSSession) setTarget(target *websocket.Conn) bool {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	if s.ctx.Err() != nil {
		_ = target.Close()
		return false
	}
	s.target = target
	return true
}

func (s *responsesWSSession) writeTarget(kind int, message []byte) error {
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	target := s.getTarget()
	if target == nil {
		return errors.New("responses websocket upstream is not connected")
	}
	if err := target.SetWriteDeadline(time.Now().Add(responsesWSWriteTimeout)); err != nil {
		return err
	}
	return target.WriteMessage(kind, message)
}

func (s *responsesWSSession) writeClient(kind int, message []byte) error {
	s.stateMu.Lock()
	privacy := s.privacy
	s.stateMu.Unlock()
	var err error
	message, err = relaycommon.RedactUserModelRouteJSON(message, privacy)
	if err != nil {
		return err
	}
	s.clientWriteMu.Lock()
	defer s.clientWriteMu.Unlock()
	if err := s.client.SetWriteDeadline(time.Now().Add(responsesWSWriteTimeout)); err != nil {
		return err
	}
	return s.client.WriteMessage(kind, message)
}

func (s *responsesWSSession) sendError(eventID, streamID string, apiErr *types.NewAPIError) {
	if apiErr == nil {
		return
	}
	payload, err := buildResponsesWSErrorPayload(eventID, streamID, apiErr)
	if err == nil {
		_ = s.writeClient(websocket.TextMessage, payload)
	}
}

func (s *responsesWSSession) closeTarget() {
	s.connectionMu.Lock()
	target, unregister := s.target, s.unregister
	s.target, s.unregister = nil, nil
	s.connectionMu.Unlock()
	if unregister != nil {
		unregister()
	}
	if target != nil {
		_ = target.Close()
	}
}

func (s *responsesWSSession) shutdown() {
	s.cancel()
	s.closeTarget()
	_ = s.client.Close()
}

func (s *responsesWSSession) registerChannelClose(channelID int) {
	unregister := wsmanager.Register(channelID, wsmanager.KindResponses, s.closeForPolicy)
	s.connectionMu.Lock()
	if s.ctx.Err() != nil {
		s.connectionMu.Unlock()
		unregister()
		return
	}
	s.unregister = unregister
	s.connectionMu.Unlock()
}

// closeForPolicy tells both peers why the connection ends, like the realtime
// relay does; WriteControl is safe alongside a blocked data writer.
func (s *responsesWSSession) closeForPolicy(reason string) {
	closeMessage := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason)
	deadline := time.Now().Add(time.Second)
	_ = s.client.WriteControl(websocket.CloseMessage, closeMessage, deadline)
	if target := s.getTarget(); target != nil {
		_ = target.WriteControl(websocket.CloseMessage, closeMessage, deadline)
	}
	s.shutdown()
}

// Stream identity belongs to the WebSocket envelope. For the legacy wrapped
// input, the top-level field takes precedence over response.stream_id.
func parseResponsesWSEnvelope(message []byte) (responsesWSCreateEvent, string, error) {
	var event responsesWSCreateEvent
	if err := common.Unmarshal(message, &event); err != nil {
		return event, "", fmt.Errorf("invalid websocket event json: %w", err)
	}
	streamRaw := event.StreamID
	if len(streamRaw) == 0 && len(event.Request) > 0 {
		var wrapped struct {
			StreamID common.RawMessage `json:"stream_id"`
		}
		if err := common.Unmarshal(event.Request, &wrapped); err == nil {
			streamRaw = wrapped.StreamID
		}
	}
	var streamID string
	if len(streamRaw) > 0 {
		if err := common.Unmarshal(streamRaw, &streamID); err != nil || len(streamID) < 1 || len(streamID) > 256 {
			return event, "", errors.New("stream_id must contain 1-256 ASCII letters, digits, underscores, hyphens, or periods")
		}
		for _, char := range streamID {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.') {
				return event, "", errors.New("stream_id must contain only ASCII letters, digits, underscores, hyphens, or periods")
			}
		}
	}
	if strings.TrimSpace(event.Type) == "" {
		return event, streamID, errors.New("websocket event type is required")
	}
	return event, streamID, nil
}

func newResponsesWSInvalidRequestError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

// normalizeResponsesWSCreateEvent builds the HTTP-shaped request body from a
// response.create whose envelope the read loop already parsed and validated.
// The message is decoded once more as a raw map (the body source) and once as
// the typed request; the envelope is not parsed again.
func normalizeResponsesWSCreateEvent(message []byte, event responsesWSCreateEvent, streamID string) (responsesWSCreateRequest, error) {
	create := responsesWSCreateRequest{StreamID: streamID, Generate: event.Generate}
	// Decode the wrapped request on its own so outer fields cannot enter the HTTP body.
	source := message
	if len(event.Request) > 0 {
		source = event.Request
	}
	var raw map[string]common.RawMessage
	if err := common.Unmarshal(source, &raw); err != nil {
		return create, err
	}
	if len(event.Request) > 0 {
		if len(create.Generate) == 0 {
			create.Generate = raw["generate"]
		}
	} else {
		for _, key := range []string{"type", "event_id", "background", "stream", "stream_options"} {
			delete(raw, key)
		}
	}
	delete(raw, "generate")
	delete(raw, "stream_id")
	payload, err := common.Marshal(raw)
	if err != nil {
		return create, err
	}
	if err := common.Unmarshal(payload, &create.Request); err != nil {
		return create, err
	}
	if helper.ExceedsMaxTokensLimit(create.Request.MaxOutputTokens) {
		return create, errors.New("max_output_tokens is invalid")
	}
	create.Request.Stream = nil
	create.Request.StreamOptions = nil
	create.Body = payload
	return create, nil
}

func buildResponsesWSCreateEvent(jsonData []byte, generate common.RawMessage, streamID string) ([]byte, error) {
	var event map[string]common.RawMessage
	if err := common.Unmarshal(jsonData, &event); err != nil {
		return nil, err
	}
	typeData, err := common.Marshal(responsesWSEventTypeResponseCreate)
	if err != nil {
		return nil, err
	}
	event["type"] = typeData
	// The normalized body never carries stream_id; a channel parameter
	// override may inject one, and it must not shadow the envelope's value.
	delete(event, "stream_id")
	if streamID != "" {
		streamData, err := common.Marshal(streamID)
		if err != nil {
			return nil, err
		}
		event["stream_id"] = streamData
	}
	delete(event, "event_id")
	delete(event, "background")
	delete(event, "stream")
	delete(event, "stream_options")
	if len(generate) > 0 {
		event["generate"] = generate
	}
	return common.Marshal(event)
}

func buildResponsesWSErrorPayload(eventID, streamID string, apiErr *types.NewAPIError) ([]byte, error) {
	if apiErr == nil {
		return nil, errors.New("api error is nil")
	}
	status := apiErr.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}
	openaiErr := apiErr.ToOpenAIError()
	return common.Marshal(&responsesWSErrorEvent{
		Type:     "error",
		Status:   status,
		EventID:  eventID,
		StreamID: streamID,
		Error:    &openaiErr,
	})
}

// checkResponsesWSModelAccess applies the token model limit with the same name
// matching as the HTTP distributor. Unlike HTTP, it also runs for requests
// pinned to a channel: the check is stricter there on purpose, because a
// persistent connection keeps serving the model after the pin was resolved.
func checkResponsesWSModelAccess(c *gin.Context, modelName string) *types.NewAPIError {
	if !common.GetContextKeyBool(c, appconstant.ContextKeyTokenModelLimitEnabled) {
		return nil
	}
	raw, ok := common.GetContextKey(c, appconstant.ContextKeyTokenModelLimit)
	if !ok {
		return types.NewErrorWithStatusCode(errors.New("token has no model access"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	tokenModelLimit, ok := raw.(map[string]bool)
	if !ok {
		tokenModelLimit = map[string]bool{}
	}
	if !middleware.TokenModelLimitAllows(tokenModelLimit, modelName) {
		return types.NewErrorWithStatusCode(fmt.Errorf("token is not allowed to use model %s", modelName), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	return nil
}

// selectResponsesWSChannel narrows the shared HTTP selection to channels that
// speak the Responses WebSocket protocol and renders its outcome as a
// non-retryable NewAPIError.
func selectResponsesWSChannel(c *gin.Context, modelName string, retryParam *service.RetryParam) (*appmodel.Channel, *types.NewAPIError) {
	constraints := service.GetChannelConstraints(c)
	if !slices.ContainsFunc(constraints.Filters, func(filter appdto.ChannelFilter) bool {
		return filter.Kind == appdto.FilterResponsesWebSocket
	}) {
		constraints.AddFilter(appdto.ChannelFilter{Kind: appdto.FilterResponsesWebSocket})
	}
	return middleware.SelectResponsesWebSocketChannel(c, modelName, retryParam)
}
