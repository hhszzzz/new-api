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
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
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
		err         error
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			if newAPIError.GetErrorCode() == types.ErrorCodeClientDisconnected {
				logger.LogInfo(c, "relay client disconnected before stream completion")
				return
			}
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

	request, requestCached := common.GetContextKeyType[dto.Request](c, constant.ContextKeyValidatedRelayRequest)
	if !requestCached {
		request, err = helper.GetAndValidateRequest(c, relayFormat)
	}
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithStatusCode(http.StatusBadRequest), types.ErrOptionWithSkipRetry())
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

	if shouldApplyUserRateLimits(c, relayInfo) && !common.GetContextKeyBool(c, constant.ContextKeyUserRateLimitApplied) {
		policy := service.UserRateLimitPolicyFromContext(c)
		waitOptions := service.UserConcurrencyWaitOptions{}
		if policy.HasConcurrencyLimit() && relayInfo.IsStream {
			helper.EnsureStreamWriteMutex(c)
			if relayFormat == types.RelayFormatOpenAIRealtime {
				waitOptions.Heartbeat = func() error {
					if ws == nil {
						return errors.New("websocket connection is nil")
					}
					return ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second))
				}
			} else {
				waitOptions.Heartbeat = func() error {
					helper.SetEventStreamHeaders(c)
					return helper.PingData(c)
				}
			}
		}
		guard, apiErr := service.BeginUserRequestRateLimit(c, policy, relayInfo.OriginModelName, waitOptions)
		if apiErr != nil {
			newAPIError = apiErr
			return
		}
		defer guard.Release()
		if relayInfo.IsStream && guard.Pacer != nil {
			service.InstallUserStreamPacer(c, guard.Pacer)
			defer service.InstallUserStreamPacer(c, nil)
		}
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive() && !common.GetContextKeyBool(c, constant.ContextKeyPromptAuditChecked)
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
		contains, _ := service.CheckSensitiveText(request.GetSensitiveText())
		if contains {
			logger.LogWarn(c, "user sensitive words detected")
			newAPIError = types.NewError(errors.New("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected,
				types.ErrOptionWithStatusCode(http.StatusBadRequest), types.ErrOptionWithSkipRetry())
			return
		}
	}

	if !common.GetContextKeyBool(c, constant.ContextKeyPromptAuditChecked) {
		promptAuditResult, promptAuditErr := service.CheckPromptAudit(c, service.PromptAuditRequest{
			Snapshot: dto.PromptAuditSnapshotOf(request),
			Protocol: string(relayInfo.RelayFormat),
			Model:    relayInfo.OriginModelName,
			Stage:    "http",
			Stream:   relayInfo.IsStream,
		})
		if promptAuditErr != nil {
			service.RecordPromptAuditError(c, promptAuditResult, promptAuditErr, relayInfo.OriginModelName, relayInfo.IsStream)
			newAPIError = promptAuditErr
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
		var (
			channel          *model.Channel
			channelRateGuard *service.ChannelRateLimitGuard
			channelErr       *types.NewAPIError
		)
		if relayInfo.IsChannelTest {
			channel, channelErr = getChannel(c, relayInfo, retryParam)
		} else {
			channel, channelRateGuard, channelErr = getChannelAttempt(c, relayInfo, retryParam)
		}
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		forwarded := false
		func() {
			if channelRateGuard != nil {
				defer channelRateGuard.Release()
			}
			addUsedChannel(c, channel.Id)
			if billingErr := service.PrepareTieredBillingForSelectedGroup(c, relayInfo); billingErr != nil {
				newAPIError = billingErr
				return
			}

			bodyStorage, bodyErr := common.GetBodyStorage(c)
			if bodyErr != nil {
				// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
				if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
				} else {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				}
				return
			}
			c.Request.Body = io.NopCloser(bodyStorage)
			forwarded = true

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
		}()
		if !forwarded {
			break
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

		if newAPIError.GetErrorCode() != types.ErrorCodeClientDisconnected {
			processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError, relayInfo)
		}

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
			perfmetrics.RecordRelaySample(relayInfo, false, 0, 0, 0)
		})
	}
}

func shouldApplyUserRateLimits(c *gin.Context, relayInfo *relaycommon.RelayInfo) bool {
	if c == nil || relayInfo == nil || relayInfo.IsPlayground || relayInfo.IsChannelTest {
		return false
	}
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeChatCompletions,
		relayconstant.RelayModeCompletions,
		relayconstant.RelayModeResponses,
		relayconstant.RelayModeResponsesCompact,
		relayconstant.RelayModeRealtime:
		return true
	case relayconstant.RelayModeGemini:
		path := strings.ToLower(c.Request.URL.Path)
		return strings.HasSuffix(path, ":generatecontent") || strings.HasSuffix(path, ":streamgeneratecontent")
	default:
		return relayInfo.RelayFormat == types.RelayFormatClaude && strings.HasSuffix(c.Request.URL.Path, "/messages")
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
		if c.Writer.Written() || privacyInfo != nil && privacyInfo.IsStream {
			helper.SetEventStreamHeaders(c)
			if err := helper.ObjectData(c, gin.H{"error": publicError}); err != nil {
				logger.LogError(c, "failed to write Chat Completions stream error: "+err.Error())
			}
			return
		}
		c.JSON(apiError.StatusCode, gin.H{"error": publicError})
	}
}

// CountClaudeTokens implements Anthropic's token-counting utility endpoint.
// It deliberately skips upstream generation and billing; callers use this
// endpoint to size prompts before creating a Message.
func CountClaudeTokens(c *gin.Context) {
	request, err := helper.GetAndValidateClaudeRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": common.MessageWithRequestId(err.Error(), c.GetString(common.RequestIdKey)),
			},
		})
		return
	}

	info := relaycommon.GenRelayInfoClaude(c, request)
	inputTokens, err := service.CountRequestToken(c, request.GetTokenCountMeta(), info)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "api_error",
				"message": common.MessageWithRequestId(err.Error(), c.GetString(common.RequestIdKey)),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"input_tokens": inputTokens})
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

func getChannelAttempt(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *service.ChannelRateLimitGuard, *types.NewAPIError) {
	if retryParam == nil {
		return nil, nil, types.NewError(errors.New("invalid channel selection parameters"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	rejected := false
	defer retryParam.ClearChannelExclusions()
	for {
		channel, apiErr := getChannel(c, info, retryParam)
		if apiErr != nil {
			if rejected {
				return nil, nil, service.NewChannelRateLimitError()
			}
			return nil, nil, apiErr
		}
		guard, allowed := service.TryAcquireChannelRateLimit(c, channel)
		if allowed {
			return channel, guard, nil
		}
		if c.Request.Context().Err() != nil {
			return nil, nil, types.NewError(c.Request.Context().Err(), types.ErrorCodeClientDisconnected, types.ErrOptionWithSkipRetry())
		}
		rejected = true
		retryParam.ExcludeChannel(channel.Id)
	}
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		channelID := c.GetInt("channel_id")
		if !retryParam.IsChannelExcluded(channelID) {
			channel, err := model.CacheGetChannel(channelID)
			if err != nil {
				return nil, types.NewError(fmt.Errorf("failed to reload channel %d: %w", channelID, err), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
			}
			return channel, nil
		}
	}
	if channelID, retrySameChannel := middleware.PendingAutoProtocolRetryChannelID(c); retrySameChannel && !retryParam.IsChannelExcluded(channelID) {
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
	applyChannelRateLimit := true
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify,
		relayconstant.RelayModeMidjourneyTaskFetch,
		relayconstant.RelayModeMidjourneyTaskFetchByCondition,
		relayconstant.RelayModeMidjourneyTaskImageSeed:
		applyChannelRateLimit = false
	}
	if applyChannelRateLimit {
		retryParam := buildRelayRetryParam(c, relayInfo)
		channel, guard, apiErr := getChannelAttempt(c, relayInfo, retryParam)
		if apiErr != nil {
			statusCode := apiErr.StatusCode
			if statusCode == 0 {
				statusCode = http.StatusServiceUnavailable
			}
			message := apiErr.Error()
			if statusCode == http.StatusTooManyRequests {
				message = "rate_limit_exceeded"
				c.JSON(statusCode, gin.H{"description": message, "type": "rate_limit_exceeded", "code": 30})
				return
			}
			c.JSON(statusCode, gin.H{"description": message, "type": "upstream_error", "code": 4})
			return
		}
		if guard != nil {
			defer guard.Release()
		}
		addUsedChannel(c, channel.Id)
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
	// The web fallback may already have applied static-asset cache headers.
	// A missing API or asset can appear after an upgrade; never cache its 404.
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
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

// RelayTaskPluginEndpoint keeps unclaimed shared-endpoint traffic on its
// existing handler while claimed requests enter the generation-pinned
// host-owned protocol bridge.
func RelayTaskPluginEndpoint(c *gin.Context, fallback gin.HandlerFunc) {
	pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint)
	if !exists {
		fallback(c)
		return
	}
	pinned, ok := pinnedValue.(pluginruntime.PinnedEndpoint)
	if !ok || pinned.Plugin == nil || pinned.Generation == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "Task protocol request failed",
				"type":    "new_api_error",
				"code":    "task_protocol_error",
			},
		})
		return
	}
	if pinned.Protocol != "openai_responses" {
		fallback(c)
		return
	}
	serveTaskPluginProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
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

type taskSubmissionOutcome struct {
	Result    *relay.TaskSubmitResult
	Task      *model.Task
	RelayInfo *relaycommon.RelayInfo
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskSubmissionError(c, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if action := c.GetString("task_action"); action != "" {
		relayInfo.Action = action
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	if taskErr := relay.ApplyOriginTaskAffinity(c, relayInfo); taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
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

	outcome, taskErr := executeTaskSubmission(c, relayInfo)
	if taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	presentTaskSubmission(c, outcome)
}

// executeTaskSubmission owns the retry, billing, and persistence lifecycle.
// It deliberately performs no client response writes so JSON and protocol
// presenters share the same durable task barrier. Its cancellation semantics
// come from c.Request.Context: native task endpoints use the client context,
// while the Responses bridge supplies an independently bounded context.
func executeTaskSubmission(c *gin.Context, relayInfo *relaycommon.RelayInfo) (*taskSubmissionOutcome, *taskdto.TaskError) {
	return executeTaskSubmissionWith(c, relayInfo, relay.RelayTaskSubmit)
}

type taskSubmitAttempt func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *taskdto.TaskError)

func executeTaskSubmissionWith(
	c *gin.Context,
	relayInfo *relaycommon.RelayInfo,
	submit taskSubmitAttempt,
) (*taskSubmissionOutcome, *taskdto.TaskError) {
	diagnostics := newTaskPluginSubmitDiagnostics(c)
	diagnostics.start(relayInfo)
	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	durable := false
	stage := "start"
	defer func() {
		if !durable && relayInfo.Billing != nil {
			diagnostics.refund(stage)
			relayInfo.Billing.Refund(c)
		}
	}()
	stage = "before_attempt"
	if requestErr := c.Request.Context().Err(); requestErr != nil {
		diagnostics.cancelled("before_attempt", 0)
		return nil, service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
	}

	retryParam := buildRelayRetryParam(c, relayInfo)

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		stage = "select_channel"
		if requestErr := c.Request.Context().Err(); requestErr != nil {
			diagnostics.cancelled("before_attempt", retryParam.GetRetry()+1)
			taskErr = service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
			break
		}
		var (
			channel          *model.Channel
			channelRateGuard *service.ChannelRateLimitGuard
		)

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName, true); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
			var allowed bool
			channelRateGuard, allowed = service.TryAcquireChannelRateLimit(c, channel)
			if !allowed {
				taskErr = service.TaskErrorWrapperLocal(errors.New("rate_limit_exceeded"), "rate_limit_exceeded", http.StatusTooManyRequests)
				break
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelRateGuard, channelErr = getChannelAttempt(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				statusCode := channelErr.StatusCode
				if statusCode == 0 {
					statusCode = http.StatusInternalServerError
				}
				errorCode := "get_channel_failed"
				if statusCode == http.StatusTooManyRequests {
					errorCode = "rate_limit_exceeded"
				}
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, errorCode, statusCode)
				break
			}
		}
		diagnostics.attempt(retryParam.GetRetry()+1, channel, relayInfo.LockedChannel != nil)

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if channelRateGuard != nil {
				channelRateGuard.Release()
			}
			stage = "read_body"
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		stage = "submit"
		func() {
			if channelRateGuard != nil {
				defer channelRateGuard.Release()
			}
			result, taskErr = submit(c, relayInfo)
		}()
		if requestErr := c.Request.Context().Err(); requestErr != nil {
			diagnostics.cancelled("after_submit", retryParam.GetRetry()+1)
			taskErr = service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
			break
		}
		if taskErr == nil {
			diagnostics.attemptSucceeded(retryParam.GetRetry()+1, result)
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode), relayInfo)
		}

		willRetry := shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry())
		diagnostics.attemptFailed(retryParam.GetRetry()+1, channel, taskErr, willRetry)
		if !willRetry {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	if taskErr != nil {
		diagnostics.failed(stage, "task_error", taskErr, false)
		return nil, taskErr
	}
	if result == nil {
		taskErr = service.TaskErrorWrapperLocal(errors.New("task submission returned no result"), "task_submit_failed", http.StatusInternalServerError)
		diagnostics.failed("submit", "missing_result", taskErr, false)
		return nil, taskErr
	}
	if requestErr := c.Request.Context().Err(); requestErr != nil {
		diagnostics.cancelled("before_reserve", retryParam.GetRetry()+1)
		return nil, service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
	}

	// Reserve any submit-time upward billing adjustment before persistence.
	// This keeps insertion failures fully refundable while ensuring settlement
	// after the barrier normally has a zero positive delta.
	if relayInfo.Billing != nil {
		stage = "reserve"
		diagnostics.reserve("reserve_start", result.Quota)
		if reserveErr := relayInfo.Billing.Reserve(result.Quota); reserveErr != nil {
			common.SysError("reserve adjusted task billing error: " + reserveErr.Error())
			taskErr = service.TaskErrorWrapperLocal(errors.New("insufficient quota for adjusted task cost"), string(types.ErrorCodeInsufficientUserQuota), http.StatusForbidden)
			diagnostics.failed("reserve", "insufficient_quota", taskErr, false)
			return nil, taskErr
		}
		diagnostics.reserve("reserve_complete", result.Quota)
	}
	if requestErr := c.Request.Context().Err(); requestErr != nil {
		diagnostics.cancelled("before_insert", retryParam.GetRetry()+1)
		return nil, service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
	}

	stage = "insert"
	task := model.InitTask(result.Platform, relayInfo)
	task.PrivateData.Execution = service.TaskExecutionSnapshotFromContext(c)
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
		TieredSnapshot:  relayInfo.TieredBillingSnapshot,
	}
	task.Quota = result.Quota
	task.Data = result.TaskData
	if len(result.PluginState) > 0 {
		task.PrivateData.PluginState = result.PluginState
	}
	task.Action = relayInfo.Action
	if immediate := result.Immediate; immediate != nil {
		task.Status = model.TaskStatus(immediate.Status)
		task.Progress = immediate.Progress
		if immediate.Status == model.TaskStatusSuccess || immediate.Status == model.TaskStatusFailure {
			task.FinishTime = time.Now().Unix()
		}
		if immediate.Status == model.TaskStatusFailure {
			task.FailReason = immediate.Reason
		}
		if immediate.Url != "" {
			task.PrivateData.ResultURL = immediate.Url
		} else if immediate.Status == model.TaskStatusSuccess {
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
	}
	diagnostics.insertStart(task)
	if insertErr := task.InsertWithContext(c.Request.Context()); insertErr != nil {
		common.SysError("insert task error: " + insertErr.Error())
		taskErr = service.TaskErrorWrapperLocal(errors.New("failed to persist task"), "task_insert_failed", http.StatusInternalServerError)
		diagnostics.failed("insert", "database_error", taskErr, false)
		return nil, taskErr
	}
	durable = true
	stage = "settle"
	diagnostics.durable(task)
	diagnostics.settleStart(task, result.Quota)

	if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
		common.SysError("settle task billing error: " + settleErr.Error())
		taskErr = service.TaskErrorWrapperLocal(errors.New("failed to settle task billing"), "task_billing_settlement_failed", http.StatusInternalServerError)
		diagnostics.failed("settle", "billing_error", taskErr, true)
		return nil, taskErr
	}
	service.LogTaskConsumption(c, relayInfo, task)
	diagnostics.complete(task, result.Quota)

	return &taskSubmissionOutcome{Result: result, Task: task, RelayInfo: relayInfo}, nil
}

func presentTaskSubmission(c *gin.Context, outcome *taskSubmissionOutcome) {
	diagnostics := newTaskPluginSubmitDiagnostics(c)
	otherRatios := outcome.RelayInfo.PriceData.OtherRatios()
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	if ratiosJSON, err := common.Marshal(otherRatios); err == nil {
		c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))
	}
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedRoute); exists {
		if pinned, ok := pinnedValue.(pluginruntime.PinnedRoute); ok && pinned.Plugin != nil && pinned.Route.Render != "" {
			view, err := service.BuildTaskPluginView(outcome.Task)
			requestValue, _ := c.Get(pluginruntime.ContextKeyRouteRequest)
			requestContext, _ := requestValue.(pluginruntime.RouteRequestContext)
			if err == nil {
				viewValue, valueErr := taskPluginProtocolJSONValue(view)
				if valueErr == nil {
					if body, callErr := pinned.Plugin.Engine.CallPath(c.Request.Context(), "native", []string{pinned.Route.Render}, requestContext.JSValue(), viewValue); callErr == nil {
						diagnostics.present(outcome.Task, "native_presenter")
						c.JSON(http.StatusOK, body)
						return
					} else {
						logger.LogError(c, "task plugin native submit presenter failed: "+callErr.Error())
					}
				} else {
					logger.LogError(c, "encode task plugin native submit view failed: "+valueErr.Error())
				}
			} else {
				logger.LogError(c, "build task plugin native submit view failed: "+err.Error())
			}
		}
	}
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint); exists {
		if pinned, ok := pinnedValue.(pluginruntime.PinnedEndpoint); ok && pinned.Protocol == "openai_video" && pinned.Operation.Name == "create" {
			diagnostics.present(outcome.Task, "openai_video_create")
			c.JSON(http.StatusOK, outcome.Task.ToOpenAIVideo())
			return
		}
	}
	createdAt := outcome.Task.CreatedAt
	if createdAt == 0 {
		createdAt = outcome.Task.SubmitTime
	}
	diagnostics.present(outcome.Task, "host_fallback")
	c.JSON(http.StatusOK, map[string]any{
		"id":         outcome.Task.TaskID,
		"task_id":    outcome.Task.TaskID,
		"status":     "queued",
		"model":      outcome.RelayInfo.OriginModelName,
		"created_at": createdAt,
	})
}

func respondTaskSubmissionError(c *gin.Context, taskErr *taskdto.TaskError) {
	newTaskPluginSubmitDiagnostics(c).presentError(taskErr)
	if middleware.RespondTaskPluginError(c, taskErr) {
		return
	}
	respondTaskError(c, taskErr)
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
		if taskErr.Code == "rate_limit_exceeded" {
			taskErr.Message = "rate_limit_exceeded"
		} else {
			taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
		}
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
	if service.GetChannelConstraints(c).SuppressesRetry() {
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
