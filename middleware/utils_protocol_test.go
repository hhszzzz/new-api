package middleware

import (
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

func TestBuildChannelCandidateClassifierPreservesLegacyAndChatOrdering(t *testing.T) {
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
	assert.Nil(t, BuildChannelCandidateClassifier(newContext("/v1/responses"), "public-model"))

	settings.ProtocolBridgePolicy.Enabled = true
	assert.Nil(t, BuildChannelCandidateClassifier(newContext("/v1/chat/completions"), "public-model"))
	assert.NotNil(t, BuildChannelCandidateClassifier(newContext("/v1/responses"), "public-model"))
	assert.NotNil(t, BuildChannelCandidateClassifier(newContext("/v1/messages"), "public-model"))
}

func TestBridgeCandidateFilterLeavesProtocolIncompatibilityToClassifier(t *testing.T) {
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
	assert.Equal(t, model.ChannelCandidateIncompatible, classifier(chatOnly))
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
