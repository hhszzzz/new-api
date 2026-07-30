package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	responsesWSBridgePublicModel   = "responses-bridge-public"
	responsesWSBridgeUpstreamModel = "responses-bridge-upstream"
	responsesWSBridgeClientKey     = "responses-bridge-client-key"
	responsesWSBridgeChannelKey    = "responses-bridge-channel-key"
	responsesWSBridgeInitialQuota  = 1000
)

// TestResponsesWebSocketBridgesChatOnlyChannelOverHTTP covers the WS→HTTP
// transport bridge: a channel that only speaks Chat Completions cannot carry a
// native Responses WebSocket, so each response.create must be executed through
// the HTTP relay pipeline (with protocol conversion) and its SSE events
// forwarded to the WebSocket client, with billing settled per logical request.
func TestResponsesWebSocketBridgesChatOnlyChannelOverHTTP(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.Log{}))

	previousMemoryCache := common.MemoryCacheEnabled
	previousBatchUpdate := common.BatchUpdateEnabled
	previousPreConsumedQuota := common.PreConsumedQuota
	previousLogConsume := common.LogConsumeEnabled
	previousRetryTimes := common.RetryTimes
	previousRateLimit := setting.ModelRequestRateLimitEnabled
	previousSensitive := setting.CheckSensitiveEnabled
	previousSensitivePrompt := setting.CheckSensitiveOnPromptEnabled
	previousModelRatios := ratio_setting.ModelRatio2JSONString()
	previousCompletionRatios := ratio_setting.CompletionRatio2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousStreamingTimeout := constant.StreamingTimeout
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		common.BatchUpdateEnabled = previousBatchUpdate
		common.PreConsumedQuota = previousPreConsumedQuota
		common.LogConsumeEnabled = previousLogConsume
		common.RetryTimes = previousRetryTimes
		setting.ModelRequestRateLimitEnabled = previousRateLimit
		setting.SetCheckSensitiveEnabled(previousSensitive)
		setting.SetCheckSensitiveOnPromptEnabled(previousSensitivePrompt)
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(previousCompletionRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		constant.StreamingTimeout = previousStreamingTimeout
		service.ResetProxyClientCache()
	})
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.PreConsumedQuota = 10
	common.LogConsumeEnabled = true
	common.RetryTimes = 0
	setting.ModelRequestRateLimitEnabled = false
	setting.SetCheckSensitiveEnabled(false)
	setting.SetCheckSensitiveOnPromptEnabled(false)
	constant.StreamingTimeout = 300
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(fmt.Sprintf(`{"%s":1}`, responsesWSBridgePublicModel)))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(fmt.Sprintf(`{"%s":1}`, responsesWSBridgePublicModel)))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	var chatRequests atomic.Int32
	var lastChatBody atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			http.Error(w, fmt.Sprintf("unexpected upstream request %s %s", r.Method, r.URL.Path), http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		lastChatBody.Store(body)
		chatRequests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "flusher unavailable", http.StatusInternalServerError)
			return
		}
		for _, chunk := range []string{
			fmt.Sprintf(`{"id":"chatcmpl-bridge","object":"chat.completion.chunk","created":1720000000,"model":"%s","choices":[{"index":0,"delta":{"role":"assistant","content":"he"}}]}`, responsesWSBridgeUpstreamModel),
			fmt.Sprintf(`{"id":"chatcmpl-bridge","object":"chat.completion.chunk","created":1720000000,"model":"%s","choices":[{"index":0,"delta":{"content":"llo"}}]}`, responsesWSBridgeUpstreamModel),
			fmt.Sprintf(`{"id":"chatcmpl-bridge","object":"chat.completion.chunk","created":1720000000,"model":"%s","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`, responsesWSBridgeUpstreamModel),
			`[DONE]`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
	}))
	t.Cleanup(upstream.Close)

	baseURL := upstream.URL
	modelMapping := fmt.Sprintf(`{"%s":"%s"}`, responsesWSBridgePublicModel, responsesWSBridgeUpstreamModel)
	channel := &model.Channel{
		Type:         constant.ChannelTypeOpenAI,
		Key:          responsesWSBridgeChannelKey,
		Status:       common.ChannelStatusEnabled,
		Name:         "responses websocket bridge e2e",
		BaseURL:      &baseURL,
		Models:       responsesWSBridgePublicModel,
		Group:        "default",
		ModelMapping: &modelMapping,
		AutoBan:      common.GetPointer(0),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ProtocolCapabilities: &dto.ProtocolCapabilities{
			UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
			AllowConversion:   common.GetPointer(true),
			SelectionMode:     dto.ProtocolSelectionModeStrict,
		},
	})
	require.NoError(t, channel.Insert())

	userSetting, err := common.Marshal(dto.UserSetting{BillingPreference: "wallet_only"})
	require.NoError(t, err)
	user := &model.User{
		Username: "responses-bridge-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    responsesWSBridgeInitialQuota,
		Group:    "default",
		Setting:  string(userSetting),
	}
	require.NoError(t, db.Create(user).Error)
	token := &model.Token{
		UserId:       user.Id,
		Key:          responsesWSBridgeClientKey,
		Status:       common.TokenStatusEnabled,
		Name:         "responses-bridge-token",
		CreatedTime:  common.GetTimestamp(),
		AccessedTime: common.GetTimestamp(),
		ExpiredTime:  -1,
		RemainQuota:  responsesWSBridgeInitialQuota,
		Group:        "default",
	}
	require.NoError(t, db.Create(token).Error)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
		common.SetContextKey(c, constant.ContextKeyUserQuota, responsesWSBridgeInitialQuota)
		common.SetContextKey(c, constant.ContextKeyUserStatus, common.UserStatusEnabled)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroups, []string{"default"})
		common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
		common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
		common.SetContextKey(c, constant.ContextKeyTokenKey, token.Key)
		common.SetContextKey(c, constant.ContextKeyTokenGroup, token.Group)
		common.SetContextKey(c, constant.ContextKeyTokenUnlimited, false)
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, fmt.Sprintf("%d", channel.Id))
		c.Set("token_name", token.Name)
		c.Set("token_quota", token.RemainQuota)
		c.Set(common.RequestIdKey, "responses-bridge-e2e")
		c.Next()
	})
	engine.GET("/v1/responses", ResponsesWebSocket)
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)

	dialer := websocket.Dialer{Subprotocols: []string{"responses"}}
	clientHeader := http.Header{}
	clientHeader.Set("Authorization", "Bearer "+responsesWSBridgeClientKey)
	client, response, err := dialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/responses", clientHeader)
	if response != nil && response.Body != nil {
		t.Cleanup(func() { _ = response.Body.Close() })
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	firstCreate := fmt.Sprintf(`{
		"type":"response.create",
		"event_id":"evt-bridge-1",
		"model":"%s",
		"input":"hi",
		"store":false
	}`, responsesWSBridgePublicModel)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(firstCreate)))

	events := readResponsesBridgeEventsUntilCompleted(t, client)
	require.NotEmpty(t, events)
	assert.Equal(t, "response.created", events[0]["type"])
	assert.Equal(t, "hello", responsesBridgeOutputText(events))
	completed := events[len(events)-1]
	usage, ok := completed["response"].(map[string]any)["usage"].(map[string]any)
	require.True(t, ok, "completed event must carry usage")
	assert.EqualValues(t, 2, usage["input_tokens"])
	assert.EqualValues(t, 3, usage["output_tokens"])
	assert.EqualValues(t, 5, usage["total_tokens"])

	assert.Equal(t, int32(1), chatRequests.Load())
	chatBody, _ := lastChatBody.Load().([]byte)
	require.NotEmpty(t, chatBody)
	var chatRequest map[string]any
	require.NoError(t, common.Unmarshal(chatBody, &chatRequest))
	assert.Equal(t, responsesWSBridgeUpstreamModel, chatRequest["model"])
	assert.Equal(t, true, chatRequest["stream"])
	assert.Contains(t, chatRequest, "messages")

	// Terminal events are flushed only after billing settled and the session
	// slot was released, so plain assertions are safe here and an immediate
	// follow-up response.create must not hit the in-progress conflict.
	settled := readResponsesWSE2EBilling(t, db, user.Id, token.Id, channel.Id)
	assert.Equal(t, responsesWSBridgeInitialQuota-5, settled.userQuota)
	assert.Equal(t, 5, settled.userUsedQuota)
	assert.Equal(t, 1, settled.requestCount)
	assert.Equal(t, responsesWSBridgeInitialQuota-5, settled.tokenRemain)
	assert.Equal(t, 5, settled.tokenUsed)
	assert.Equal(t, int64(5), settled.channelUsed)
	assert.Equal(t, int64(1), settled.consumeLogCount)
	var consumeLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
	assert.Equal(t, channel.Id, consumeLog.ChannelId)
	assert.Equal(t, responsesWSBridgePublicModel, consumeLog.ModelName)
	assert.Equal(t, 2, consumeLog.PromptTokens)
	assert.Equal(t, 3, consumeLog.CompletionTokens)
	assert.Equal(t, 5, consumeLog.Quota)

	secondCreate := fmt.Sprintf(`{
		"type":"response.create",
		"event_id":"evt-bridge-2",
		"response":{"model":"%s","input":"hi again","store":false}
	}`, responsesWSBridgePublicModel)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(secondCreate)))

	events = readResponsesBridgeEventsUntilCompleted(t, client)
	assert.Equal(t, "hello", responsesBridgeOutputText(events))
	assert.Equal(t, int32(2), chatRequests.Load())

	settled = readResponsesWSE2EBilling(t, db, user.Id, token.Id, channel.Id)
	assert.Equal(t, responsesWSBridgeInitialQuota-10, settled.userQuota)
	assert.Equal(t, 2, settled.requestCount)
	assert.Equal(t, int64(10), settled.channelUsed)
	assert.Equal(t, int64(2), settled.consumeLogCount)
}

func readResponsesBridgeEventsUntilCompleted(t *testing.T, conn *websocket.Conn) []map[string]any {
	t.Helper()
	var events []map[string]any
	for {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		messageType, message, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, websocket.TextMessage, messageType)
		var event map[string]any
		require.NoError(t, common.Unmarshal(message, &event))
		eventType, _ := event["type"].(string)
		require.NotEqualf(t, "error", eventType, "unexpected error event: %s", message)
		events = append(events, event)
		if eventType == "response.completed" {
			return events
		}
		require.Less(t, len(events), 200, "no response.completed event received")
	}
}

func responsesBridgeOutputText(events []map[string]any) string {
	var text strings.Builder
	for _, event := range events {
		if event["type"] == "response.output_text.delta" {
			if delta, ok := event["delta"].(string); ok {
				text.WriteString(delta)
			}
		}
	}
	return text.String()
}
