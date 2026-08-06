package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	appmodel "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const responsesWSEventTypeResponseCreate = "response.create"

const responsesWSBetaHeader = "responses_websockets=2026-02-06"

type responsesWSCreateEvent struct {
	Type    string          `json:"type"`
	EventID string          `json:"event_id,omitempty"`
	Request json.RawMessage `json:"response,omitempty"`
}

type responsesWSCreateRequest struct {
	Request   dto.OpenAIResponsesRequest
	Generate  json.RawMessage
	rateGuard *service.UserRequestRateGuard
}

type responsesWSErrorEvent struct {
	Type    string             `json:"type"`
	Status  int                `json:"status"`
	EventID string             `json:"event_id,omitempty"`
	Error   *types.OpenAIError `json:"error"`
}

type responsesWSCallState struct {
	info       *relaycommon.RelayInfo
	usage      *dto.Usage
	outputText strings.Builder
	images     relaycommon.ImageGenerationCallCounter
	commitRate middleware.ModelRequestRateLimitCommit
	// cancelHTTP is set only for calls served by the HTTP transport bridge.
	// Those calls settle their own billing in runHTTPBridgeCall; outside
	// observers may only cancel them.
	cancelHTTP       context.CancelFunc
	rateGuard        *service.UserRequestRateGuard
	channelRateGuard *service.ChannelRateLimitGuard
}

type responsesWSSession struct {
	c              *gin.Context
	client         *websocket.Conn
	target         *websocket.Conn
	unregister     func()
	baseRequestID  string
	lockedModel    string
	lockedChannel  *appmodel.Channel
	nextEventIndex int
	closeOnce      sync.Once
	// nativeTransportFailed records that native WebSocket attempts were
	// exhausted on this connection; later creates go straight to the HTTP
	// transport bridge instead of re-dialing a broken upstream per request.
	nativeTransportFailed bool
	// bridgeWG tracks in-flight HTTP bridge goroutines; the handler must not
	// return (releasing the pooled gin context) while one still runs.
	bridgeWG sync.WaitGroup

	clientWriteMu sync.Mutex
	targetWriteMu sync.Mutex
	stateMu       sync.Mutex
	current       *responsesWSCallState
}

func ResponsesWebSocketHelper(c *gin.Context, client *websocket.Conn) *types.NewAPIError {
	session := &responsesWSSession{
		c:      c,
		client: client,
	}
	defer session.closeTarget()
	defer common.CleanupBodyStorage(c)
	// failCurrent runs first and cancels any in-flight HTTP bridge call, so the
	// Wait below is bounded; body storage stays alive until the bridge exits.
	defer session.bridgeWG.Wait()
	defer session.failCurrent()

	for {
		messageType, message, err := client.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return types.NewError(err, types.ErrorCodeBadRequestBody, types.ErrOptionWithSkipRetry())
		}

		eventType, eventErr := responsesWSEventType(message)
		if eventErr != nil {
			session.sendError("", newResponsesWSInvalidRequestError(eventErr))
			continue
		}

		if eventType != responsesWSEventTypeResponseCreate {
			if !session.hasTarget() {
				if session.cancelHTTPBridgeCall(eventType) {
					continue
				}
				session.sendError("", newResponsesWSInvalidRequestError(errors.New("first responses websocket event must be response.create")))
				continue
			}
			if err := session.writeTarget(messageType, message); err != nil {
				return session.handleControlEventWriteFailure(err)
			}
			continue
		}

		create, eventID, err := normalizeResponsesWSCreateEvent(message)
		if err != nil {
			session.sendError("", newResponsesWSInvalidRequestError(err))
			continue
		}
		if create.Request.Model == "" {
			session.sendError(eventID, newResponsesWSInvalidRequestError(errors.New("model is required")))
			continue
		}
		if err := session.handleResponseCreate(create, eventID); err != nil {
			session.sendError(eventID, err)
		}
	}
}

func responsesWSEventType(message []byte) (string, error) {
	var event struct {
		Type string `json:"type"`
	}
	if err := common.Unmarshal(message, &event); err != nil {
		return "", fmt.Errorf("invalid websocket event json: %w", err)
	}
	if strings.TrimSpace(event.Type) == "" {
		return "", errors.New("websocket event type is required")
	}
	return event.Type, nil
}

func newResponsesWSInvalidRequestError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func normalizeResponsesWSCreateEvent(message []byte) (responsesWSCreateRequest, string, error) {
	var event responsesWSCreateEvent
	if err := common.Unmarshal(message, &event); err != nil {
		return responsesWSCreateRequest{}, "", err
	}
	if event.Type != responsesWSEventTypeResponseCreate {
		return responsesWSCreateRequest{}, event.EventID, fmt.Errorf("unsupported event type %q", event.Type)
	}

	var generate json.RawMessage
	var raw map[string]json.RawMessage
	if err := common.Unmarshal(message, &raw); err == nil {
		if generateRaw, ok := raw["generate"]; ok {
			generate = generateRaw
		}
	}

	payload := event.Request
	if len(payload) == 0 {
		if err := common.Unmarshal(message, &raw); err != nil {
			return responsesWSCreateRequest{}, event.EventID, err
		}
		delete(raw, "type")
		delete(raw, "event_id")
		delete(raw, "background")
		delete(raw, "generate")
		delete(raw, "stream")
		delete(raw, "stream_options")
		var err error
		payload, err = common.Marshal(raw)
		if err != nil {
			return responsesWSCreateRequest{}, event.EventID, err
		}
	} else {
		var responseMap map[string]json.RawMessage
		if err := common.Unmarshal(payload, &responseMap); err == nil {
			if len(generate) == 0 {
				if generateRaw, ok := responseMap["generate"]; ok {
					generate = generateRaw
				}
			}
			if _, exists := responseMap["generate"]; exists {
				delete(responseMap, "generate")
				if merged, err := common.Marshal(responseMap); err == nil {
					payload = merged
				}
			}
		}
	}

	var req dto.OpenAIResponsesRequest
	if err := common.Unmarshal(payload, &req); err != nil {
		return responsesWSCreateRequest{}, event.EventID, err
	}
	req.Stream = nil
	req.StreamOptions = nil
	return responsesWSCreateRequest{
		Request:  req,
		Generate: generate,
	}, event.EventID, nil
}

func (s *responsesWSSession) handleResponseCreate(create responsesWSCreateRequest, eventID string) *types.NewAPIError {
	req := create.Request
	if s.lockedModel != "" && req.Model != s.lockedModel {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("responses websocket connection is locked to model %q; got %q", s.lockedModel, req.Model),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	if s.hasCurrent() {
		return types.NewErrorWithStatusCode(
			errors.New("another response.create is already in progress on this websocket connection"),
			types.ErrorCodeInvalidRequest,
			http.StatusConflict,
			types.ErrOptionWithSkipRetry(),
		)
	}
	protocolstate.ResetLogicalRequest(s.c)
	common.SetContextKey(s.c, appconstant.ContextKeyRequestStartTime, time.Now())

	validated, requestBody, apiErr := installResponsesWSRequestBody(s.c, &req)
	if apiErr != nil {
		return apiErr
	}
	create.Request = *validated
	if apiErr := middleware.PrepareResponsesWebSocketRequest(s.c, validated.Model, requestBody); apiErr != nil {
		return apiErr
	}
	if s.lockedChannel != nil {
		if binding, ok := common.GetContextKeyType[*protocolstate.SelectionBinding](s.c, appconstant.ContextKeyProtocolStateBinding); ok &&
			binding != nil && binding.ChannelID > 0 && binding.ChannelID != s.lockedChannel.Id {
			return types.NewErrorWithStatusCode(
				errors.New("the referenced response is bound to a different channel than this websocket connection"),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}

	commitRate, apiErr := middleware.CheckModelRequestRateLimit(s.c)
	if apiErr != nil {
		return apiErr
	}

	group := common.GetContextKeyString(s.c, appconstant.ContextKeyTokenGroup)
	if group == "" {
		group = common.GetContextKeyString(s.c, appconstant.ContextKeyUserGroup)
	}
	policy, err := service.LoadUserRateLimitPolicy(common.GetContextKeyInt(s.c, appconstant.ContextKeyUserId), group)
	if err != nil {
		commitRate(false)
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	rateGuard, apiErr := service.BeginUserRequestRateLimit(s.c, policy, validated.Model, service.UserConcurrencyWaitOptions{
		Heartbeat: func() error {
			return s.writeClientControl(websocket.PingMessage, nil)
		},
	})
	if apiErr != nil {
		commitRate(false)
		return apiErr
	}
	create.rateGuard = rateGuard
	defer func() {
		if rateGuard != nil && !rateGuard.Claimed() {
			rateGuard.Release()
		}
	}()

	if !s.hasTarget() {
		return s.connectAndSendFirst(create, eventID, commitRate)
	}
	channelRateGuard, allowed := service.TryAcquireChannelRateLimit(s.c, s.lockedChannel)
	if !allowed {
		s.closeTarget()
		s.lockedModel = ""
		s.lockedChannel = nil
		return s.connectAndSendFirst(create, eventID, commitRate)
	}

	state, payload, apiErr := s.prepareCall(create, commitRate)
	if apiErr != nil {
		channelRateGuard.Release()
		commitRate(false)
		return apiErr
	}
	state.channelRateGuard = channelRateGuard
	if !s.tryReserveCurrent(state) {
		channelRateGuard.Release()
		state.refund(s.c)
		commitRate(false)
		return types.NewErrorWithStatusCode(
			errors.New("another response.create is already in progress on this websocket connection"),
			types.ErrorCodeInvalidRequest,
			http.StatusConflict,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
		return s.handleTargetWriteFailureWithState(state, err)
	}
	return nil
}

func installResponsesWSRequestBody(c *gin.Context, request *dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, []byte, *types.NewAPIError) {
	requestBody, err := common.Marshal(request)
	if err != nil {
		return nil, nil, newResponsesWSInvalidRequestError(err)
	}
	common.CleanupBodyStorage(c)
	storage, err := common.CreateBodyStorage(requestBody)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	c.Set(common.KeyBodyStorage, storage)
	c.Request.Body = io.NopCloser(bytes.NewReader(requestBody))
	c.Request.ContentLength = int64(len(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")

	validated, err := helper.GetAndValidateResponsesRequest(c)
	if err != nil {
		return nil, nil, newResponsesWSInvalidRequestError(err)
	}
	validated.Stream = nil
	validated.StreamOptions = nil
	validatedBody, err := common.Marshal(validated)
	if err != nil {
		return nil, nil, newResponsesWSInvalidRequestError(err)
	}
	return validated, validatedBody, nil
}

func (s *responsesWSSession) handleControlEventWriteFailure(err error) *types.NewAPIError {
	apiErr := s.handleTargetWriteFailure(err)
	s.sendError("", apiErr)
	return nil
}

func (s *responsesWSSession) handleTargetWriteFailure(err error) *types.NewAPIError {
	state := s.getCurrent()
	var relayInfo *relaycommon.RelayInfo
	if state != nil {
		relayInfo = state.info
	}
	s.closeTarget()
	apiErr := types.NewError(err, types.ErrorCodeBadResponse)
	apiErr, _ = s.processChannelError(s.lockedChannel, apiErr, nil, relayInfo)
	return apiErr
}

func (s *responsesWSSession) handleTargetWriteFailureWithState(state *responsesWSCallState, err error) *types.NewAPIError {
	s.finishCall(state, false)
	return s.handleTargetWriteFailure(err)
}

func (s *responsesWSSession) connectAndSendFirst(create responsesWSCreateRequest, eventID string, commitRate middleware.ModelRequestRateLimitCommit) *types.NewAPIError {
	req := create.Request
	if s.nativeTransportFailed {
		return s.startHTTPBridgeCall(create, eventID, commitRate)
	}
	retryParam := middleware.NewResponsesWebSocketRetryParam(s.c, req.Model)

	var lastErr *types.NewAPIError
	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		protocolstate.ResetAttempt(s.c)
		retryParam.ClearChannelExclusions()
		var (
			channel          *appmodel.Channel
			channelRateGuard *service.ChannelRateLimitGuard
			apiErr           *types.NewAPIError
		)
		for {
			channel, apiErr = middleware.SelectResponsesWebSocketChannel(s.c, req.Model, retryParam)
			if apiErr != nil {
				break
			}
			var allowed bool
			channelRateGuard, allowed = service.TryAcquireChannelRateLimit(s.c, channel)
			if allowed {
				break
			}
			retryParam.ExcludeChannel(channel.Id)
		}
		retryParam.ClearChannelExclusions()
		if apiErr != nil {
			if retryParam.GetRetry() == 0 {
				// No channel can serve a native Responses WebSocket. Serve this
				// logical request over the HTTP relay pipeline instead, which
				// keeps protocol bridging (chat/messages/gemini upstreams)
				// available to WebSocket clients.
				return s.startHTTPBridgeCall(create, eventID, commitRate)
			}
			lastErr = apiErr
			break
		}
		addResponsesWSUsedChannel(s.c, channel.Id)

		state, payload, apiErr := s.prepareCall(create, commitRate)
		if apiErr != nil {
			channelRateGuard.Release()
			commitRate(false)
			return apiErr
		}
		state.channelRateGuard = channelRateGuard

		adaptor := GetAdaptorForProtocol(state.info.ApiType, channelcompat.ProtocolResponses)
		if adaptor == nil {
			channelRateGuard.Release()
			state.refund(s.c)
			apiErr = types.NewError(fmt.Errorf("invalid api type: %d", state.info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
			var shouldRetry bool
			lastErr, shouldRetry = s.processChannelError(channel, apiErr, retryParam, state.info)
			if !shouldRetry {
				break
			}
			continue
		}
		adaptor.Init(state.info)
		target, apiErr := dialResponsesWebSocketUpstream(s.c, adaptor, state.info)
		if apiErr != nil {
			channelRateGuard.Release()
			state.refund(s.c)
			var shouldRetry bool
			lastErr, shouldRetry = s.processChannelError(channel, apiErr, retryParam, state.info)
			if !shouldRetry {
				break
			}
			continue
		}

		s.setTarget(target)
		if !s.tryReserveCurrent(state) {
			s.closeTarget()
			channelRateGuard.Release()
			state.refund(s.c)
			commitRate(false)
			return types.NewErrorWithStatusCode(errors.New("another response.create is already in progress on this websocket connection"), types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry())
		}
		s.lockedModel = req.Model
		s.lockedChannel = channel
		s.registerChannelClose(channel.Id)
		if !service.IsChannelAvailableForActiveWebSocket(channel.Id) {
			s.closeForPolicy(service.ChannelDisabledCloseReason)
			return types.NewError(fmt.Errorf("channel %d is disabled or deleted", channel.Id), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
			s.abortRetryableCall(state)
			s.closeTarget()
			apiErr = types.NewError(err, types.ErrorCodeBadResponse)
			var shouldRetry bool
			lastErr, shouldRetry = s.processChannelError(channel, apiErr, retryParam, state.info)
			if !shouldRetry {
				break
			}
			continue
		}

		s.startTargetReader()
		return nil
	}

	// Native transport attempts are exhausted and nothing reached the client
	// (the target reader never started). Fall back to the HTTP relay pipeline,
	// which also covers channels that serve Responses over HTTP but do not
	// speak the WebSocket transport.
	if lastErr != nil {
		logger.LogWarn(s.c, "responses websocket native transport unavailable, falling back to HTTP transport: "+lastErr.Error())
	}
	s.lockedModel = ""
	s.lockedChannel = nil
	s.nativeTransportFailed = true
	return s.startHTTPBridgeCall(create, eventID, commitRate)
}

// abortRetryableCall clears one failed upstream attempt without releasing the
// logical request's user concurrency lease. The same response.create may still
// retry another channel or fall back to the HTTP bridge.
func (s *responsesWSSession) abortRetryableCall(state *responsesWSCallState) {
	if state == nil || !s.clearCurrent(state) {
		return
	}
	state.refund(s.c)
	state.channelRateGuard.Release()
	if state.commitRate != nil {
		state.commitRate(false)
	}
	state.rateGuard.Unclaim()
}

func (s *responsesWSSession) processChannelError(channel *appmodel.Channel, apiErr *types.NewAPIError, retryParam *service.RetryParam, relayInfo *relaycommon.RelayInfo) (*types.NewAPIError, bool) {
	if apiErr == nil {
		return nil, false
	}
	apiErr = service.NormalizeViolationFeeError(apiErr)
	statusCodeMapping := ""
	if s.c != nil {
		statusCodeMapping = s.c.GetString("status_code_mapping")
	}
	service.ResetStatusCode(apiErr, statusCodeMapping)
	if channel != nil && s.c != nil {
		service.ProcessChannelError(s.c, *types.NewChannelError(
			channel.Id,
			channel.Type,
			channel.Name,
			channel.ChannelInfo.IsMultiKey,
			common.GetContextKeyString(s.c, appconstant.ContextKeyChannelKey),
			channel.GetAutoBan(),
		), apiErr, relayInfo)
	}
	if retryParam == nil {
		return apiErr, false
	}
	return apiErr, service.ShouldRetryRelayError(s.c, apiErr, common.RetryTimes-retryParam.GetRetry())
}

func (s *responsesWSSession) prepareCall(create responsesWSCreateRequest, commitRate middleware.ModelRequestRateLimitCommit) (*responsesWSCallState, []byte, *types.NewAPIError) {
	state, apiErr := s.prepareCallState(create)
	if apiErr != nil {
		return nil, nil, apiErr
	}
	state.commitRate = commitRate
	state.rateGuard = create.rateGuard

	payload, apiErr := buildResponsesWSCreatePayload(s.c, state.info, create.Request, create.Generate)
	if apiErr != nil {
		state.refund(s.c)
		return nil, nil, apiErr
	}
	return state, payload, nil
}

// prepareCallState performs the per-attempt request accounting shared by the
// native WebSocket transport and the HTTP bridge: relay info, sensitive check,
// token estimate, pricing, and pre-consume.
func (s *responsesWSSession) prepareCallState(create responsesWSCreateRequest) (*responsesWSCallState, *types.NewAPIError) {
	req := create.Request
	if s.baseRequestID == "" {
		s.baseRequestID = s.c.GetString(common.RequestIdKey)
		if s.baseRequestID == "" {
			s.baseRequestID = common.NewRequestId()
		}
	}
	eventRequestID := fmt.Sprintf("%s-ws-%d", s.baseRequestID, s.nextEventIndex)
	s.c.Set(common.RequestIdKey, eventRequestID)
	relayInfo := relaycommon.GenRelayInfoResponses(s.c, &req)
	relayInfo.InitRequestConversionChain()
	relayInfo.IsStream = true
	common.SetContextKey(s.c, appconstant.ContextKeyIsStream, true)
	relayInfo.RequestId = eventRequestID
	s.nextEventIndex++

	meta := req.GetTokenCountMeta()
	if setting.ShouldCheckPromptSensitive() {
		contains, words := service.CheckSensitiveText(req.GetSensitiveText())
		if contains {
			logger.LogWarn(s.c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			return nil, types.NewErrorWithStatusCode(errors.New("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}

	tokens, err := service.EstimateRequestToken(s.c, meta, relayInfo)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(s.c, relayInfo, tokens, meta)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if !priceData.FreeModel {
		if apiErr := service.PreConsumeBilling(s.c, priceData.QuotaToPreConsume, relayInfo); apiErr != nil {
			return nil, apiErr
		}
	}

	return &responsesWSCallState{
		info:  relayInfo,
		usage: &dto.Usage{},
	}, nil
}

func buildResponsesWSCreatePayload(c *gin.Context, relayInfo *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest, generate json.RawMessage) ([]byte, *types.NewAPIError) {
	relayInfo.InitChannelMeta(c)
	request, err := common.DeepCopy(&req)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("failed to copy responses request: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ModelMappedHelper(c, relayInfo, request); err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	plan, ok := selectedProtocolPlan(c)
	if !ok || plan.Status != channelcompat.StatusNative || plan.UpstreamProtocol != channelcompat.ProtocolResponses {
		return nil, types.NewErrorWithStatusCode(errors.New("Responses WebSocket requires a native Responses protocol plan"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	adaptor := GetAdaptorForProtocol(relayInfo.ApiType, channelcompat.ProtocolResponses)
	if adaptor == nil {
		return nil, types.NewError(fmt.Errorf("invalid api type: %d", relayInfo.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(relayInfo)
	if err := protocolstate.PrepareResponsesRequest(c, relayInfo, plan, request); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	applyResponsesInstructionsIfNeeded(c, relayInfo, request)
	convertedRequest, err := convertRequestForProtocolPlan(c, relayInfo, adaptor, plan, request)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.AppendRequestConversionFromRequest(relayInfo, convertedRequest)
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, relayInfo.ChannelOtherSettings, relayInfo.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err = removeResponsesWSTransportFields(jsonData)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(relayInfo.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, relayInfo)
		if err != nil {
			return nil, newAPIErrorFromParamOverride(err)
		}
	}

	event, err := buildResponsesWSCreateEvent(jsonData, generate)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	return event, nil
}

func buildResponsesWSCreateEvent(jsonData []byte, generate json.RawMessage) ([]byte, error) {
	var event map[string]json.RawMessage
	if err := common.Unmarshal(jsonData, &event); err != nil {
		return nil, err
	}
	typeData, err := common.Marshal(responsesWSEventTypeResponseCreate)
	if err != nil {
		return nil, err
	}
	event["type"] = typeData
	delete(event, "event_id")
	delete(event, "background")
	delete(event, "stream")
	delete(event, "stream_options")
	if len(generate) > 0 {
		event["generate"] = generate
	}
	return common.Marshal(event)
}

func removeResponsesWSTransportFields(jsonData []byte) ([]byte, error) {
	var data map[string]any
	if err := common.Unmarshal(jsonData, &data); err != nil {
		return jsonData, err
	}
	delete(data, "stream")
	delete(data, "stream_options")
	delete(data, "background")
	return common.Marshal(data)
}

func dialResponsesWebSocketUpstream(c *gin.Context, adaptor relaychannel.Adaptor, info *relaycommon.RelayInfo) (*websocket.Conn, *types.NewAPIError) {
	fullRequestURL, err := adaptor.GetRequestURL(info)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("get request url failed: %w", err), types.ErrorCodeDoRequestFailed)
	}
	fullRequestURL = toWebSocketURL(fullRequestURL)

	targetHeader := http.Header{}
	if err := adaptor.SetupRequestHeader(c, &targetHeader, info); err != nil {
		return nil, types.NewError(fmt.Errorf("setup request header failed: %w", err), types.ErrorCodeDoRequestFailed)
	}
	headerOverride, err := relaychannel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid)
	}
	for key, value := range headerOverride {
		targetHeader.Set(key, value)
	}
	targetHeader.Set("OpenAI-Beta", mergeResponsesWSBetaHeader(targetHeader.Get("OpenAI-Beta")))
	targetHeader.Del("Sec-WebSocket-Protocol")

	dialer := *websocket.DefaultDialer
	dialer.Subprotocols = []string{"responses"}
	targetConn, resp, err := dialer.Dial(fullRequestURL, targetHeader)
	if err != nil {
		if resp != nil {
			return nil, service.RelayErrorHandler(c.Request.Context(), resp, false)
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("dial failed to %s: %w", fullRequestURL, err), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	return targetConn, nil
}

func mergeResponsesWSBetaHeader(existing string) string {
	for _, value := range strings.Split(existing, ",") {
		if strings.TrimSpace(value) == responsesWSBetaHeader {
			return existing
		}
	}
	if strings.TrimSpace(existing) == "" {
		return responsesWSBetaHeader
	}
	return existing + ", " + responsesWSBetaHeader
}

func toWebSocketURL(raw string) string {
	switch {
	case strings.HasPrefix(raw, "https://"):
		return "wss://" + strings.TrimPrefix(raw, "https://")
	case strings.HasPrefix(raw, "http://"):
		return "ws://" + strings.TrimPrefix(raw, "http://")
	default:
		return raw
	}
}

func (s *responsesWSSession) startTargetReader() {
	target := s.getTarget()
	if target == nil {
		return
	}
	go func() {
		for {
			messageType, message, err := target.ReadMessage()
			if err != nil {
				var closeErr *websocket.CloseError
				if errors.As(err, &closeErr) {
					_ = s.writeClientControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeErr.Code, closeErr.Text))
				} else {
					logger.LogError(s.c, "responses websocket upstream read failed: "+err.Error())
				}
				s.failCurrent()
				_ = s.client.Close()
				return
			}
			state := s.getCurrent()
			publicMessage, apiErr := s.observeUpstreamMessage(message)
			if apiErr != nil {
				s.sendError("", apiErr)
				s.failCurrent()
				s.closeTarget()
				return
			}
			if state != nil && state.rateGuard != nil {
				if err := state.rateGuard.Pace(s.c.Request.Context(), publicMessage); err != nil {
					logger.LogError(s.c, "responses websocket pacing failed: "+err.Error())
					s.failCurrent()
					s.closeTarget()
					return
				}
			}
			if err := s.writeClient(messageType, publicMessage); err != nil {
				logger.LogError(s.c, "responses websocket client write failed: "+err.Error())
				s.failCurrent()
				s.closeTarget()
				return
			}
		}
	}()
}

func (s *responsesWSSession) observeUpstreamMessage(message []byte) ([]byte, *types.NewAPIError) {
	state := s.getCurrent()
	if state == nil {
		return message, nil
	}
	state.info.SetFirstResponseTime()

	var streamResponse dto.ResponsesStreamResponse
	if err := common.Unmarshal(message, &streamResponse); err != nil {
		return message, nil
	}
	if streamResponse.Response != nil {
		if err := protocolstate.ValidateResponsesContinuation(s.c, streamResponse.Response.PreviousResponseID); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
	}

	publicMessage := message
	if protocolstate.PublicResponseID(s.c, "") != "" {
		encoded, err := protocolstate.ObserveResponsesStreamData(s.c, &streamResponse, message)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		publicMessage = encoded
	} else {
		protocolstate.ObserveResponsesStream(s.c, &streamResponse)
	}
	if state.info.HasModelRouting() {
		redacted, err := relaycommon.RedactUserModelRouteJSON(publicMessage, state.info)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		publicMessage = redacted
	}

	switch streamResponse.Type {
	case "response.completed", "response.done", "response.incomplete":
		s.applyTerminalResponseUsage(state, streamResponse.Response)
		if streamResponse.Type == "response.incomplete" {
			state.images.Reset()
		}
		protocolstate.MarkStreamCompleted(s.c)
		s.finishCall(state, true)
	case "response.failed", "response.cancelled", "response.canceled":
		s.finishCall(state, false)
	case "response.output_text.delta":
		state.outputText.WriteString(streamResponse.Delta)
	case dto.ResponsesOutputTypeItemDone:
		if streamResponse.Item != nil {
			switch streamResponse.Item.Type {
			case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall:
				state.info.CountBillableToolCall(streamResponse.Item.Type, "")
			case dto.BuildInCallFunctionCall:
				state.info.CountBillableToolCall(streamResponse.Item.Type, streamResponse.Item.Name)
			case dto.ResponsesOutputTypeImageGenerationCall:
				state.images.Observe(streamResponse.Item, streamResponse.OutputIndex)
			}
		}
	case "error":
		s.finishCall(state, false)
	}
	return publicMessage, nil
}

func (s *responsesWSSession) applyTerminalResponseUsage(state *responsesWSCallState, response *dto.OpenAIResponsesResponse) {
	if state == nil || response == nil {
		return
	}
	if response.Usage != nil {
		service.ApplyResponsesUsage(state.usage, response.Usage)
	}
	if relaycommon.IsNonBillableResponsesStatus(response.Status) {
		state.images.Reset()
		return
	}
	for i := range response.Output {
		output := &response.Output[i]
		if output.Type == dto.ResponsesOutputTypeImageGenerationCall {
			index := i
			state.images.Observe(output, &index)
		}
	}
}

func (s *responsesWSSession) finishCall(state *responsesWSCallState, success bool) {
	if state == nil {
		return
	}
	if state.cancelHTTP != nil {
		// HTTP bridge calls settle billing in runHTTPBridgeCall; external
		// finishers may only abort the in-flight HTTP request.
		state.cancelHTTP()
		return
	}
	if !s.clearCurrent(state) {
		return
	}
	defer state.rateGuard.Release()
	defer state.channelRateGuard.Release()
	if !success {
		state.refund(s.c)
		if state.commitRate != nil {
			state.commitRate(false)
		}
		return
	}

	finalizeResponsesWSUsage(state)
	state.images.Commit(state.info)
	service.PostTextConsumeQuota(s.c, state.info, state.usage, nil)
	service.RecordChannelAffinity(s.c, state.info.ChannelId)
	middleware.CommitAutoProtocolAffinity(s.c)
	if err := protocolstate.Commit(s.c); err != nil {
		logger.LogError(s.c, "failed to persist Responses WebSocket protocol state: "+err.Error())
	}
	if state.commitRate != nil {
		state.commitRate(true)
	}
}

func finalizeResponsesWSUsage(state *responsesWSCallState) {
	if state == nil || state.usage == nil || state.info == nil {
		return
	}
	if state.usage.CompletionTokens == 0 {
		if output := state.outputText.String(); output != "" {
			state.usage.CompletionTokens = service.CountTextToken(output, state.info.UpstreamModelName)
		}
	}
	if state.usage.PromptTokens == 0 && state.usage.CompletionTokens != 0 {
		state.usage.PromptTokens = state.info.GetEstimatePromptTokens()
	}
	if state.usage.TotalTokens == 0 {
		state.usage.TotalTokens = state.usage.PromptTokens + state.usage.CompletionTokens
	}
}

func (state *responsesWSCallState) refund(c *gin.Context) {
	if state != nil && state.info != nil && state.info.Billing != nil {
		state.info.Billing.Refund(c)
	}
}

func (s *responsesWSSession) tryReserveCurrent(state *responsesWSCallState) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.current != nil {
		return false
	}
	s.current = state
	if state != nil && state.rateGuard != nil {
		state.rateGuard.Claim()
	}
	return true
}

func (s *responsesWSSession) hasCurrent() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.current != nil
}

func (s *responsesWSSession) clearCurrent(state *responsesWSCallState) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if state != nil && s.current != state {
		return false
	}
	s.current = nil
	return true
}

func (s *responsesWSSession) getCurrent() *responsesWSCallState {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.current
}

func (s *responsesWSSession) failCurrent() {
	state := s.getCurrent()
	if state != nil {
		s.finishCall(state, false)
	}
}

func (s *responsesWSSession) writeClient(messageType int, message []byte) error {
	s.clientWriteMu.Lock()
	defer s.clientWriteMu.Unlock()
	return s.client.WriteMessage(messageType, message)
}

func (s *responsesWSSession) writeClientControl(messageType int, message []byte) error {
	s.clientWriteMu.Lock()
	defer s.clientWriteMu.Unlock()
	return s.client.WriteControl(messageType, message, time.Now().Add(time.Second))
}

func (s *responsesWSSession) hasTarget() bool {
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	return s.target != nil
}

func (s *responsesWSSession) getTarget() *websocket.Conn {
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	return s.target
}

func (s *responsesWSSession) setTarget(target *websocket.Conn) {
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	s.target = target
}

func (s *responsesWSSession) writeTarget(messageType int, message []byte) error {
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	if s.target == nil {
		return errors.New("responses websocket upstream is not connected")
	}
	return s.target.WriteMessage(messageType, message)
}

func (s *responsesWSSession) writeTargetControl(messageType int, message []byte) error {
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	if s.target == nil {
		return nil
	}
	return s.target.WriteControl(messageType, message, time.Now().Add(time.Second))
}

func (s *responsesWSSession) sendError(eventID string, apiErr *types.NewAPIError) {
	if apiErr == nil {
		return
	}
	payload, err := buildResponsesWSErrorPayloadWithInfo(eventID, apiErr, s.privacyInfo())
	if err != nil {
		return
	}
	_ = s.writeClient(websocket.TextMessage, payload)
}

func buildResponsesWSErrorPayload(eventID string, apiErr *types.NewAPIError) ([]byte, error) {
	return buildResponsesWSErrorPayloadWithInfo(eventID, apiErr, nil)
}

func buildResponsesWSErrorPayloadWithInfo(eventID string, apiErr *types.NewAPIError, info *relaycommon.RelayInfo) ([]byte, error) {
	if apiErr == nil {
		return nil, errors.New("api error is nil")
	}
	status := apiErr.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}
	openaiErr := apiErr.ToOpenAIError()
	openaiErr = relaycommon.SanitizeUserModelRouteOpenAIError(openaiErr, info)
	return common.Marshal(&responsesWSErrorEvent{
		Type:    "error",
		Status:  status,
		EventID: eventID,
		Error:   &openaiErr,
	})
}

func (s *responsesWSSession) privacyInfo() *relaycommon.RelayInfo {
	s.stateMu.Lock()
	var info *relaycommon.RelayInfo
	if s.current != nil {
		info = s.current.info
	}
	s.stateMu.Unlock()
	if info != nil {
		return info
	}
	target := common.GetContextKeyString(s.c, appconstant.ContextKeyUserModelRouteTarget)
	origin := common.GetContextKeyString(s.c, appconstant.ContextKeyOriginalModel)
	if target == "" || origin == "" {
		return nil
	}
	return &relaycommon.RelayInfo{
		OriginModelName:      origin,
		UserModelRouteId:     common.GetContextKeyInt(s.c, appconstant.ContextKeyUserModelRouteId),
		RouteTargetModelName: target,
		ChannelMeta:          &relaycommon.ChannelMeta{UpstreamModelName: target},
	}
}

func (s *responsesWSSession) closeTarget() {
	var target *websocket.Conn
	var unregister func()
	s.targetWriteMu.Lock()
	target = s.target
	s.target = nil
	unregister = s.unregister
	s.unregister = nil
	s.targetWriteMu.Unlock()
	if unregister != nil {
		unregister()
	}
	if target != nil {
		_ = target.Close()
	}
}

func (s *responsesWSSession) registerChannelClose(channelID int) {
	unregister := wsmanager.Register(channelID, wsmanager.KindResponses, func(reason string) {
		s.closeForPolicy(reason)
	})
	s.targetWriteMu.Lock()
	if s.unregister != nil {
		s.unregister()
	}
	s.unregister = unregister
	s.targetWriteMu.Unlock()
}

func (s *responsesWSSession) closeForPolicy(reason string) {
	s.closeOnce.Do(func() {
		s.failCurrent()
		closeMessage := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason)
		_ = s.writeClientControl(websocket.CloseMessage, closeMessage)
		_ = s.writeTargetControl(websocket.CloseMessage, closeMessage)
		s.closeTarget()
		_ = s.client.Close()
	})
}

func addResponsesWSUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}
