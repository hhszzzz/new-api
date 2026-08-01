package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func ResponsesWebSocket(c *gin.Context) {
	requestID := c.GetString(common.RequestIdKey)
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.LogError(c, "failed to upgrade Responses WebSocket: "+err.Error())
		return
	}
	defer ws.Close()

	if apiErr := relay.ResponsesWebSocketHelper(c, ws); apiErr != nil {
		logger.LogError(c, "responses websocket relay error: "+common.LocalLogPreview(apiErr.Error()))
		privacyInfo := &relaycommon.RelayInfo{
			OriginModelName:      common.GetContextKeyString(c, constant.ContextKeyOriginalModel),
			UserModelRouteId:     common.GetContextKeyInt(c, constant.ContextKeyUserModelRouteId),
			RouteTargetModelName: common.GetContextKeyString(c, constant.ContextKeyUserModelRouteTarget),
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: common.GetContextKeyString(c, constant.ContextKeyUserModelRouteTarget),
			},
		}
		publicError := relaycommon.SanitizeUserModelRouteOpenAIError(apiErr.ToOpenAIError(), privacyInfo)
		publicError.Message = common.MessageWithRequestId(publicError.Message, requestID)
		helper.WssError(c, ws, publicError)
	}
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		relayInfo   *relaycommon.RelayInfo
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			publicMessage := newAPIError.Error()
			privacyInfo := relayInfo
			if privacyInfo == nil {
				if routeTarget := common.GetContextKeyString(c, constant.ContextKeyUserModelRouteTarget); routeTarget != "" {
					privacyInfo = &relaycommon.RelayInfo{
						UserModelRouteId:     common.GetContextKeyInt(c, constant.ContextKeyUserModelRouteId),
						OriginModelName:      common.GetContextKeyString(c, constant.ContextKeyOriginalModel),
						RouteTargetModelName: routeTarget,
						ChannelMeta:          &relaycommon.ChannelMeta{UpstreamModelName: routeTarget},
					}
				}
			}
			publicMessage = relaycommon.RedactUserModelRouteText(publicMessage, privacyInfo)
			newAPIError.SetMessage(common.MessageWithRequestId(publicMessage, requestId))
			writeRelayErrorResponse(c, relayFormat, newAPIError, privacyInfo, ws)
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}

	relayInfo, err = relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	if relayFormat == types.RelayFormatGemini {
		relay.ConfigureGeminiBillingModel(relayInfo)
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Sensitive filtering has its own user-text extractor, so only exact token
	// counting needs the potentially large combined request text.
	var meta *types.TokenCountMeta
	if needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck {
		contains, words := service.CheckSensitiveText(request.GetSensitiveText())
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewError(errors.New("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected,
				types.ErrOptionWithStatusCode(http.StatusBadRequest), types.ErrOptionWithSkipRetry())
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := buildRelayRetryParam(c, relayInfo)
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		protocolstate.ResetAttempt(c)
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		addUsedChannel(c, channel.Id)
		if billingErr := service.PrepareTieredBillingForSelectedGroup(c, relayInfo); billingErr != nil {
			newAPIError = billingErr
			break
		}

		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			middleware.CommitAutoProtocolAffinity(c)
			if commitErr := protocolstate.Commit(c); commitErr != nil {
				logger.LogError(c, "failed to persist protocol bridge state: "+commitErr.Error())
			}
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError

		replayFallback := protocolstate.EnableReplayFallback(c, newAPIError)
		if replayFallback {
			retryParam.SetRetry(0)
			retryParam.ResetRetryNextTry()
			continue
		}
		if middleware.AdvanceAutoProtocolAttempt(c, newAPIError) {
			retryParam.ResetRetryNextTry()
			continue
		}

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError, relayInfo)

		if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

func writeRelayErrorResponse(c *gin.Context, relayFormat types.RelayFormat, apiError *types.NewAPIError, privacyInfo *relaycommon.RelayInfo, ws *websocket.Conn) {
	if apiError == nil {
		return
	}
	switch relayFormat {
	case types.RelayFormatOpenAIRealtime:
		publicError := relaycommon.SanitizeUserModelRouteOpenAIError(apiError.ToOpenAIError(), privacyInfo)
		helper.WssError(c, ws, publicError)
	case types.RelayFormatClaude:
		publicError := relaycommon.SanitizeUserModelRouteClaudeError(apiError.ToClaudeError(), privacyInfo)
		if c.Writer.Written() || privacyInfo != nil && privacyInfo.IsStream {
			helper.SetEventStreamHeaders(c)
			if err := helper.ClaudeData(c, dto.ClaudeResponse{Type: "error", Error: publicError}); err != nil {
				logger.LogError(c, "failed to write Messages stream error: "+err.Error())
			}
			return
		}
		c.JSON(apiError.StatusCode, gin.H{
			"type":  "error",
			"error": publicError,
		})
	case types.RelayFormatOpenAIResponses:
		publicError := relaycommon.SanitizeUserModelRouteOpenAIError(apiError.ToOpenAIError(), privacyInfo)
		if c.Writer.Written() || privacyInfo != nil && privacyInfo.IsStream {
			helper.SetEventStreamHeaders(c)
			if err := helper.ResponsesErrorData(c, publicError); err != nil {
				logger.LogError(c, "failed to write Responses stream error: "+err.Error())
			}
			return
		}
		c.JSON(apiError.StatusCode, gin.H{"error": publicError})
	default:
		publicError := relaycommon.SanitizeUserModelRouteOpenAIError(apiError.ToOpenAIError(), privacyInfo)
		c.JSON(apiError.StatusCode, gin.H{"error": publicError})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime", "responses"},
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	if channelID, retrySameChannel := middleware.PendingAutoProtocolRetryChannelID(c); retrySameChannel {
		channel, err := model.CacheGetChannel(channelID)
		if err != nil {
			return nil, types.NewError(fmt.Errorf("failed to reload channel %d for automatic protocol retry: %w", channelID, err), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if channel == nil || channel.Status != common.ChannelStatusEnabled || !channel.IsSchedulableAt(time.Now()) {
			return nil, types.NewError(fmt.Errorf("channel %d is unavailable for automatic protocol retry", channelID), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if setupErr := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName, true); setupErr != nil {
			return nil, setupErr
		}
		return channel, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		if errors.Is(err, model.ErrNoCompatibleChannel) {
			if reason, ok := common.GetContextKeyType[string](c, constant.ContextKeyProtocolIncompatibleReason); ok && reason != "" {
				err = fmt.Errorf("%w: %s", err, reason)
			}
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if common.GetContextKeyInt(c, constant.ContextKeyUserModelRouteId) > 0 {
			return nil, types.NewError(fmt.Errorf("用户模型路由没有可用渠道"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		if common.GetContextKeyInt(c, constant.ContextKeyUserModelRouteId) > 0 {
			return nil, types.NewError(fmt.Errorf("用户模型路由没有可用渠道"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	if routeGroup := common.GetContextKeyString(c, constant.ContextKeyUserModelRouteGroup); routeGroup != "" {
		info.RouteExecutionGroup = routeGroup
	}

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName, true)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if c != nil && c.Writer != nil && c.Writer.Written() {
		return false
	}
	return service.ShouldRetryRelayError(c, openaiErr, retryTimes)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, relayInfo *relaycommon.RelayInfo) {
	service.ProcessChannelError(c, channelError, err, relayInfo)
}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *taskdto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	// Origin-task submissions (notably video remix) are resolved after the
	// distributor has run. Apply the same account route policy here before a
	// locked origin channel can be used.
	if relayInfo.OriginRouteSnapshotVersion == 0 {
		route, routeErr := middleware.ApplyUserModelRoute(c, relayInfo.OriginModelName, relayInfo.UsingGroup)
		if routeErr != nil {
			respondTaskError(c, service.TaskErrorWrapperLocal(routeErr, "user_model_route_unavailable", http.StatusServiceUnavailable))
			return
		}
		if route != nil {
			relayInfo.UserModelRouteId = route.Id
			relayInfo.RouteTargetModelName = strings.TrimSpace(route.TargetModel)
			relayInfo.RouteExecutionGroup = strings.TrimSpace(route.ExecutionGroup)
		}
	}
	if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
		if err := middleware.ValidateSelectedRouteChannel(c, lockedCh, c.Request.URL.Path); err != nil {
			respondTaskError(c, service.TaskErrorWrapperLocal(err, "task_channel_outside_user_route", http.StatusForbidden))
			return
		}
		if routeGroup := common.GetContextKeyString(c, constant.ContextKeyUserModelRouteGroup); routeGroup != "" {
			relayInfo.RouteExecutionGroup = routeGroup
		}
		if setupErr := middleware.SetupContextForSelectedChannel(c, lockedCh, relayInfo.OriginModelName, true); setupErr != nil {
			respondTaskError(c, service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError))
			return
		}
	}

	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := buildRelayRetryParam(c, relayInfo)

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName, true); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode), relayInfo)
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		service.LogTaskConsumption(c, relayInfo)

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios(),
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func buildRelayRetryParam(c *gin.Context, info *relaycommon.RelayInfo) *service.RetryParam {
	modelName := info.OriginModelName
	if routedModel := common.GetContextKeyString(c, constant.ContextKeyUserModelRouteTarget); routedModel != "" {
		modelName = routedModel
	}
	group := info.TokenGroup
	executionGroups, _ := common.GetContextKeyType[[]string](c, constant.ContextKeyUserModelRouteGroups)
	if len(executionGroups) > 1 {
		group = "auto"
	} else if routedGroup := common.GetContextKeyString(c, constant.ContextKeyUserModelRouteGroup); routedGroup != "" {
		group = routedGroup
	}
	channelIds, _ := common.GetContextKeyType[[]int](c, constant.ContextKeyUserModelRouteChannel)
	return &service.RetryParam{
		Ctx:                 c,
		TokenGroup:          group,
		ModelName:           modelName,
		RequestPath:         c.Request.URL.Path,
		AllowedChannelIds:   channelIds,
		AllowedGroups:       executionGroups,
		CandidateFilter:     middleware.BuildChannelCandidateFilter(c, modelName),
		CandidateClassifier: middleware.BuildChannelCandidateClassifier(c, modelName),
		Retry:               common.GetPointer(0),
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *taskdto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *taskdto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
