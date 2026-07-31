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
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// enableProtocolBridgePolicyForTest opens the global bridge hard gate so plans
// driven by per-channel protocol capabilities (including auto probing) apply.
func enableProtocolBridgePolicyForTest(t *testing.T) {
	t.Helper()
	settings := model_setting.GetGlobalSettings()
	originalPolicy := settings.ProtocolBridgePolicy
	settings.ProtocolBridgePolicy.Enabled = true
	settings.ProtocolBridgePolicy.DefaultAllowConversion = false
	t.Cleanup(func() { settings.ProtocolBridgePolicy = originalPolicy })
}

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
	customTools, err := common.Marshal([]map[string]any{{
		"type": "custom",
		"name": "local/tool",
	}})
	require.NoError(t, err)
	firstRequest := &dto.OpenAIResponsesRequest{
		Model: "public-model",
		Tools: customTools,
	}
	firstConversion, err := relayconvert.ConvertRequest(ctx, nil, types.RelayFormatOpenAI, firstRequest)
	require.NoError(t, err)
	chatRequest, ok := firstConversion.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Tools, 1)
	firstAttemptToolName := chatRequest.Tools[0].Function.Name
	assert.NotEqual(t, "local/tool", firstAttemptToolName)

	require.NoError(t, applySelectedChannelCompatibility(ctx, messagesChannel, "public-model"))
	secondPlan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.ProtocolMessages, secondPlan.UpstreamProtocol)
	assert.NotEqual(t, firstPlan.RequestConverter, secondPlan.RequestConverter)
	assert.Equal(t, "provider-messages-model", secondPlan.EffectiveUpstreamModel)
	assert.Equal(t, string(channelcompat.ProtocolMessages), common.GetContextKeyString(ctx, constant.ContextKeyUpstreamProtocol))

	secondAttemptResponse := &dto.ClaudeResponse{
		Id:         "msg_retry",
		Type:       "message",
		Role:       "assistant",
		Model:      "provider-messages-model",
		StopReason: "tool_use",
		Content: []dto.ClaudeMediaMessage{{
			Type:  "tool_use",
			Id:    "call_retry",
			Name:  firstAttemptToolName,
			Input: map[string]any{"input": "must stay a normal function"},
		}},
	}
	secondConversion, err := relayconvert.ConvertResponse(ctx, nil, types.RelayFormatOpenAIResponses, secondAttemptResponse)
	require.NoError(t, err)
	responsesOutput, ok := secondConversion.Value.(*dto.OpenAIResponsesResponse)
	require.True(t, ok)
	require.Len(t, responsesOutput.Output, 1)
	assert.Equal(t, "function_call", responsesOutput.Output[0].Type)
	assert.Equal(t, firstAttemptToolName, responsesOutput.Output[0].Name)
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

func TestBuildChannelCandidateClassifierGlobalDisableIgnoresExplicitCapabilities(t *testing.T) {
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

	// The global switch is a hard gate: with it off the configured chat-only
	// capability is ignored and Responses passes through natively.
	classifier := BuildChannelCandidateClassifier(ctx, "public-model")
	require.NotNil(t, classifier)
	assert.Equal(t, model.ChannelCandidateNative, classifier(channel))
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
	enableProtocolBridgePolicyForTest(t)

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
	unsupported.MarkProtocolUnsupported()
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

func TestAutomaticProtocolClassifierRequiresEvidenceBeforeNativeTier(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })
	enableProtocolBridgePolicyForTest(t)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"public-model"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	classifier := BuildChannelCandidateClassifier(ctx, "public-model")
	require.NotNil(t, classifier)

	unknown := &model.Channel{Id: 9910, Type: constant.ChannelTypeOpenAI}
	unknown.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})
	declaredNative := &model.Channel{Id: 9911, Type: constant.ChannelTypeOpenAI}
	declaredNative.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode:     dto.ProtocolSelectionModeAuto,
		UpstreamProtocols: []string{dto.ProtocolCapabilityResponses},
	}})

	assert.Equal(t, model.ChannelCandidateConvertible, classifier(unknown))
	assert.Equal(t, model.ChannelCandidateNative, classifier(declaredNative))

	channelcompat.RememberProtocolAffinity(unknown, "public-model", channelcompat.ProtocolResponses, channelcompat.ProtocolResponses)
	assert.Equal(t, model.ChannelCandidateNative, classifier(unknown))

	channelcompat.RememberProtocolAffinity(unknown, "public-model", channelcompat.ProtocolResponses, channelcompat.ProtocolChat)
	assert.Equal(t, model.ChannelCandidateConvertible, classifier(unknown))
}

func TestAutomaticProtocolAffinityDoesNotCrossResponsesAndMessagesEntries(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })
	enableProtocolBridgePolicyForTest(t)

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

func TestAutomaticProtocolSelectionKeepsBoundSessionProtocolAheadOfAffinityAndNative(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })
	enableProtocolBridgePolicyForTest(t)

	channel := &model.Channel{Id: 9905, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})
	channelcompat.RememberProtocolAffinity(channel, "public-model", channelcompat.ProtocolResponses, channelcompat.ProtocolMessages)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"public-model"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyProtocolStateBinding, &protocolstate.SelectionBinding{
		ChannelID:        channel.Id,
		UpstreamProtocol: channelcompat.ProtocolChat,
	})

	require.NoError(t, applySelectedChannelCompatibility(ctx, channel, "public-model"))
	plan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.ProtocolChat, plan.UpstreamProtocol)

	unsupported := types.NewErrorWithStatusCode(errors.New("unknown endpoint"), types.ErrorCodeBadResponseStatusCode, http.StatusNotFound)
	unsupported.MarkProtocolUnsupported()
	assert.True(t, AdvanceAutoProtocolAttempt(ctx, unsupported))
	require.NoError(t, applySelectedChannelCompatibility(ctx, channel, "public-model"))
	retriedPlan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.ProtocolMessages, retriedPlan.UpstreamProtocol)
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

	modelNotFoundContext := newContext()
	require.NoError(t, applySelectedChannelCompatibility(modelNotFoundContext, channel, "public-model"))
	modelNotFound := types.NewErrorWithStatusCode(errors.New("model not found"), types.ErrorCodeModelNotFound, http.StatusNotFound)
	assert.False(t, AdvanceAutoProtocolAttempt(modelNotFoundContext, modelNotFound))

	writtenContext := newContext()
	require.NoError(t, applySelectedChannelCompatibility(writtenContext, channel, "public-model"))
	writtenContext.String(http.StatusOK, "partial stream")
	unsupported := types.NewErrorWithStatusCode(errors.New("unknown endpoint"), types.ErrorCodeBadResponseStatusCode, http.StatusNotFound)
	unsupported.MarkProtocolUnsupported()
	assert.False(t, AdvanceAutoProtocolAttempt(writtenContext, unsupported))
}

func TestCommitAutomaticProtocolAffinityRemembersSuccessfulWireFormat(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })
	enableProtocolBridgePolicyForTest(t)

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

func TestCommitAutomaticProtocolAffinityRequiresVerifiedStreamCompletion(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })
	enableProtocolBridgePolicyForTest(t)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{"model":"public-model","stream":true}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	channel := &model.Channel{Id: 9912, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})

	require.NoError(t, applySelectedChannelCompatibility(ctx, channel, "public-model"))
	CommitAutoProtocolAffinity(ctx)
	_, found := channelcompat.LookupProtocolAffinity(channel, "public-model", channelcompat.ProtocolResponses)
	assert.False(t, found)

	protocolstate.MarkStreamCompleted(ctx)
	CommitAutoProtocolAffinity(ctx)
	protocol, found := channelcompat.LookupProtocolAffinity(channel, "public-model", channelcompat.ProtocolResponses)
	require.True(t, found)
	assert.Equal(t, channelcompat.ProtocolResponses, protocol)
}
