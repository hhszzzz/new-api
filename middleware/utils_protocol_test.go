package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbortWithProtocolMessageUsesAnthropicErrorEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx.Set(common.RequestIdKey, "req_test")

	abortWithProtocolMessage(ctx, http.StatusTooManyRequests, "rate limited", types.ErrorCodeGetChannelFailed)

	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	var body struct {
		Type  string            `json:"type"`
		Error types.ClaudeError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, "error", body.Type)
	assert.Equal(t, "rate_limit_error", body.Error.Type)
	assert.Contains(t, body.Error.Message, "rate limited")
	assert.True(t, ctx.IsAborted())
}

func TestAbortWithProtocolMessageKeepsOpenAIEnvelopeForResponses(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	abortWithProtocolMessage(ctx, http.StatusBadRequest, "invalid bridge", types.ErrorCodeInvalidRequest)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	var body struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Contains(t, body.Error.Message, "invalid bridge")
	assert.Equal(t, "new_api_error", body.Error.Type)
	assert.Equal(t, string(types.ErrorCodeInvalidRequest), body.Error.Code)
}

func TestApplySelectedChannelCompatibilityRecomputesProtocolPlanForEachChannel(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	originalPolicy := settings.ProtocolBridgePolicy
	settings.ProtocolBridgePolicy.Enabled = true
	settings.ProtocolBridgePolicy.DefaultAllowConversion = false
	t.Cleanup(func() { settings.ProtocolBridgePolicy = originalPolicy })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{"model":"public-model","stream":true}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	allowConversion := true
	chatMapping := `{"public-model":"provider-chat-model"}`
	chatChannel := &model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, ModelMapping: &chatMapping}
	chatChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
		AllowConversion:   &allowConversion,
	}})
	messagesMapping := `{"public-model":"provider-messages-model"}`
	messagesChannel := &model.Channel{Id: 2, Type: constant.ChannelTypeAnthropic, ModelMapping: &messagesMapping}
	messagesChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityMessages},
		AllowConversion:   &allowConversion,
	}})

	require.NoError(t, applySelectedChannelCompatibility(ctx, chatChannel, "public-model"))
	firstPlan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.ProtocolChat, firstPlan.UpstreamProtocol)
	assert.Equal(t, relayconvert.ConverterOpenAIResponsesToOpenAIChat, firstPlan.RequestConverter)
	assert.Equal(t, "provider-chat-model", firstPlan.EffectiveUpstreamModel)

	require.NoError(t, applySelectedChannelCompatibility(ctx, messagesChannel, "public-model"))
	secondPlan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.ProtocolMessages, secondPlan.UpstreamProtocol)
	assert.NotEqual(t, firstPlan.RequestConverter, secondPlan.RequestConverter)
	assert.Equal(t, "provider-messages-model", secondPlan.EffectiveUpstreamModel)
	assert.Equal(t, string(channelcompat.ProtocolMessages), common.GetContextKeyString(ctx, constant.ContextKeyUpstreamProtocol))
}

func TestBuildChannelCandidateClassifierAlwaysClassifiesResponsesAndMessages(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	originalPolicy := settings.ProtocolBridgePolicy
	t.Cleanup(func() { settings.ProtocolBridgePolicy = originalPolicy })

	newContext := func(path string) *gin.Context {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"public-model"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		return ctx
	}

	settings.ProtocolBridgePolicy.Enabled = false
	assert.NotNil(t, BuildChannelCandidateClassifier(newContext("/v1/responses"), "public-model"))

	settings.ProtocolBridgePolicy.Enabled = true
	assert.Nil(t, BuildChannelCandidateClassifier(newContext("/v1/chat/completions"), "public-model"))
	assert.NotNil(t, BuildChannelCandidateClassifier(newContext("/v1/responses"), "public-model"))
	assert.NotNil(t, BuildChannelCandidateClassifier(newContext("/v1/messages"), "public-model"))
}

func TestBuildChannelCandidateClassifierUsesExplicitChatCapabilityWhenGlobalBridgeIsDisabled(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	originalPolicy := settings.ProtocolBridgePolicy
	settings.ProtocolBridgePolicy.Enabled = false
	settings.ProtocolBridgePolicy.DefaultAllowConversion = false
	t.Cleanup(func() { settings.ProtocolBridgePolicy = originalPolicy })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"public-model"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})

	classifier := BuildChannelCandidateClassifier(ctx, "public-model")
	require.NotNil(t, classifier)
	assert.Equal(t, model.ChannelCandidateConvertible, classifier(channel))
}

func TestBridgeCandidateFilterTreatsExplicitChatCapabilityAsConvertible(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	originalPolicy := settings.ProtocolBridgePolicy
	settings.ProtocolBridgePolicy.Enabled = true
	settings.ProtocolBridgePolicy.DefaultAllowConversion = false
	t.Cleanup(func() { settings.ProtocolBridgePolicy = originalPolicy })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"public-model"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	chatOnly := &model.Channel{Id: 11, Type: constant.ChannelTypeOpenAI}
	chatOnly.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})

	filter := BuildChannelCandidateFilter(ctx, "public-model")
	classifier := BuildChannelCandidateClassifier(ctx, "public-model")
	require.NotNil(t, filter)
	require.NotNil(t, classifier)
	assert.True(t, filter(chatOnly))
	assert.Equal(t, model.ChannelCandidateConvertible, classifier(chatOnly))
}

func TestAdvancedCustomCountTokensPathUsesMessagesRouteAsSelectionFallback(t *testing.T) {
	mapping := `{"public-model":"provider-model"}`
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, ModelMapping: &mapping}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/provider/chat",
				Converter:    relayconvert.ConverterClaudeMessagesToOpenAIChat,
				Models:       []string{"provider-model"},
			},
		}},
	})

	assert.True(t, channelSupportsRequestPath(channel, "/v1/messages/count_tokens", "public-model"))
	assert.False(t, channelSupportsRequestPath(channel, "/v1/messages/count_tokens", "other-model"))
}

func TestAutomaticProtocolSelectionUsesAffinityThenRetriesSameChannel(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"public-model"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	channel := &model.Channel{Id: 9901, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})
	channelcompat.RememberProtocolAffinity(channel, "public-model", channelcompat.ProtocolResponses, channelcompat.ProtocolChat)

	require.NoError(t, applySelectedChannelCompatibility(ctx, channel, "public-model"))
	plan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.ProtocolChat, plan.UpstreamProtocol)

	unsupported := types.NewErrorWithStatusCode(errors.New("unknown endpoint"), types.ErrorCodeBadResponseStatusCode, http.StatusNotFound)
	assert.True(t, AdvanceAutoProtocolAttempt(ctx, unsupported))
	retryChannelID, retrySameChannel := PendingAutoProtocolRetryChannelID(ctx)
	assert.True(t, retrySameChannel)
	assert.Equal(t, channel.Id, retryChannelID)

	require.NoError(t, applySelectedChannelCompatibility(ctx, channel, "public-model"))
	retriedPlan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.ProtocolResponses, retriedPlan.UpstreamProtocol)
	_, retryStillPending := PendingAutoProtocolRetryChannelID(ctx)
	assert.False(t, retryStillPending)
}

func TestAutomaticProtocolAffinityDoesNotCrossResponsesAndMessagesEntries(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	channel := &model.Channel{Id: 9904, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})
	channelcompat.RememberProtocolAffinity(channel, "public-model", channelcompat.ProtocolResponses, channelcompat.ProtocolChat)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"public-model","messages":[]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	require.NoError(t, applySelectedChannelCompatibility(ctx, channel, "public-model"))
	plan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.ProtocolMessages, plan.UpstreamProtocol)
}

func TestAutomaticProtocolSelectionDoesNotAdvanceForOrdinaryErrorsOrWrittenStreams(t *testing.T) {
	newContext := func() *gin.Context {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"public-model","messages":[]}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		return ctx
	}
	channel := &model.Channel{Id: 9902, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})

	ordinaryContext := newContext()
	require.NoError(t, applySelectedChannelCompatibility(ordinaryContext, channel, "public-model"))
	ordinaryError := types.NewErrorWithStatusCode(errors.New("unsupported parameter"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)
	assert.False(t, AdvanceAutoProtocolAttempt(ordinaryContext, ordinaryError))

	writtenContext := newContext()
	require.NoError(t, applySelectedChannelCompatibility(writtenContext, channel, "public-model"))
	writtenContext.String(http.StatusOK, "partial stream")
	unsupported := types.NewErrorWithStatusCode(errors.New("unknown endpoint"), types.ErrorCodeBadResponseStatusCode, http.StatusNotFound)
	assert.False(t, AdvanceAutoProtocolAttempt(writtenContext, unsupported))
}

func TestCommitAutomaticProtocolAffinityRemembersSuccessfulWireFormat(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"public-model","messages":[]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	channel := &model.Channel{Id: 9903, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})

	require.NoError(t, applySelectedChannelCompatibility(ctx, channel, "public-model"))
	CommitAutoProtocolAffinity(ctx)

	protocol, found := channelcompat.LookupProtocolAffinity(channel, "public-model", channelcompat.ProtocolMessages)
	require.True(t, found)
	assert.Equal(t, channelcompat.ProtocolMessages, protocol)
}
