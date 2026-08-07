package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	responsesWSE2EPublicModel   = "responses-ws-public"
	responsesWSE2EUpstreamModel = "responses-ws-upstream"
	responsesWSE2EClientKey     = "responses-ws-client-key"
	responsesWSE2EChannelKey    = "responses-ws-channel-key"
	responsesWSE2EInitialQuota  = 1000
)

type responsesWSE2EHandshake struct {
	path        string
	header      http.Header
	subprotocol string
}

type responsesWSE2EUpstream struct {
	server            *httptest.Server
	handshakes        chan responsesWSE2EHandshake
	requests          chan []byte
	closed            chan *websocket.CloseError
	errors            chan error
	allowFirstFailure chan struct{}
	allowFailureOnce  sync.Once
	connections       atomic.Int32
}

type responsesWSE2EBillingSnapshot struct {
	userQuota       int
	userUsedQuota   int
	requestCount    int
	tokenRemain     int
	tokenUsed       int
	channelUsed     int64
	consumeLogCount int64
}

func TestResponsesWebSocketEndToEndReuseBillingAndChannelDisable(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.Log{}, &model.PromptAudit{}))

	previousMemoryCache := common.MemoryCacheEnabled
	previousBatchUpdate := common.BatchUpdateEnabled
	previousPreConsumedQuota := common.PreConsumedQuota
	previousLogConsume := common.LogConsumeEnabled
	previousRetryTimes := common.RetryTimes
	previousRateLimit := setting.ModelRequestRateLimitEnabled
	previousSensitive := setting.CheckSensitiveEnabled
	previousSensitivePrompt := setting.CheckSensitiveOnPromptEnabled
	previousPromptAudit := prompt_audit_setting.GetSetting()
	previousModelRatios := ratio_setting.ModelRatio2JSONString()
	previousCompletionRatios := ratio_setting.CompletionRatio2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousProtocolPolicy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		common.BatchUpdateEnabled = previousBatchUpdate
		common.PreConsumedQuota = previousPreConsumedQuota
		common.LogConsumeEnabled = previousLogConsume
		common.RetryTimes = previousRetryTimes
		setting.ModelRequestRateLimitEnabled = previousRateLimit
		setting.SetCheckSensitiveEnabled(previousSensitive)
		setting.SetCheckSensitiveOnPromptEnabled(previousSensitivePrompt)
		previousPromptAudit.PublishConfig()
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(previousCompletionRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		model_setting.GetGlobalSettings().ProtocolBridgePolicy = previousProtocolPolicy
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
	var promptAuditCalls atomic.Int32
	promptAuditGuard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		promptAuditCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	t.Cleanup(promptAuditGuard.Close)
	promptAuditConfig := prompt_audit_setting.PromptAuditSetting{
		Mode:                prompt_audit_setting.ModeBlocking,
		EnabledCategories:   append([]string(nil), prompt_audit_setting.AllCategoryIDs...),
		AllGroups:           true,
		TotalTimeoutMS:      2000,
		ChunkOverlap:        64,
		CacheTTLSeconds:     0,
		WorkerCount:         4,
		MaxAttempts:         4,
		RetentionDays:       30,
		GlobalConcurrency:   4,
		EndpointConcurrency: 4,
		Endpoints: []prompt_audit_setting.Endpoint{{
			ID: "responses-ws-guard", Name: "Responses WS Guard",
			BaseURL: promptAuditGuard.URL, Model: "qwen3guard-test",
			TimeoutMS: 1000, InputLimit: 4000, Concurrency: 4, Enabled: true,
		}},
	}
	require.NoError(t, promptAuditConfig.ValidateConfig())
	promptAuditConfig.PublishConfig()
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(fmt.Sprintf(`{"%s":1}`, responsesWSE2EPublicModel)))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(fmt.Sprintf(`{"%s":1}`, responsesWSE2EPublicModel)))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	upstream := newResponsesWSE2EUpstream(t)
	baseURL := upstream.server.URL
	modelMapping := fmt.Sprintf(`{"%s":"%s"}`, responsesWSE2EPublicModel, responsesWSE2EUpstreamModel)
	headerOverride := `{"X-Responses-Override":"override-value","OpenAI-Beta":"existing=1"}`
	channel := &model.Channel{
		Type:           constant.ChannelTypeOpenAI,
		Key:            responsesWSE2EChannelKey,
		Status:         common.ChannelStatusEnabled,
		Name:           "responses websocket e2e",
		BaseURL:        &baseURL,
		Models:         responsesWSE2EPublicModel,
		Group:          "default",
		ModelMapping:   &modelMapping,
		HeaderOverride: &headerOverride,
		AutoBan:        common.GetPointer(0),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ProtocolCapabilities: &dto.ProtocolCapabilities{
			UpstreamProtocols: []string{dto.ProtocolCapabilityResponses},
			AllowConversion:   common.GetPointer(false),
			SelectionMode:     dto.ProtocolSelectionModeStrict,
		},
	})
	require.NoError(t, channel.Insert())

	userSetting, err := common.Marshal(dto.UserSetting{BillingPreference: "wallet_only"})
	require.NoError(t, err)
	user := &model.User{
		Username: "responses-ws-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    responsesWSE2EInitialQuota,
		Group:    "default",
		Setting:  string(userSetting),
	}
	require.NoError(t, db.Create(user).Error)
	token := &model.Token{
		UserId:       user.Id,
		Key:          responsesWSE2EClientKey,
		Status:       common.TokenStatusEnabled,
		Name:         "responses-ws-token",
		CreatedTime:  common.GetTimestamp(),
		AccessedTime: common.GetTimestamp(),
		ExpiredTime:  -1,
		RemainQuota:  responsesWSE2EInitialQuota,
		Group:        "default",
	}
	require.NoError(t, db.Create(token).Error)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
		common.SetContextKey(c, constant.ContextKeyUserQuota, responsesWSE2EInitialQuota)
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
		c.Set(common.RequestIdKey, "responses-ws-e2e")
		c.Next()
	})
	engine.GET("/v1/responses", ResponsesWebSocket)
	engine.POST("/api/channel/:id/status", UpdateChannelStatus)
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)

	dialer := websocket.Dialer{Subprotocols: []string{"responses"}}
	clientHeader := http.Header{}
	clientHeader.Set("Authorization", "Bearer "+responsesWSE2EClientKey)
	client, response, err := dialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/responses", clientHeader)
	if response != nil && response.Body != nil {
		t.Cleanup(func() { _ = response.Body.Close() })
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	assert.Equal(t, "responses", client.Subprotocol())

	firstCreate := fmt.Sprintf(`{
		"type":"response.create",
		"event_id":"evt-failed",
		"model":"%s",
		"input":"first",
		"store":false,
		"generate":false,
		"stream":true,
		"stream_options":{"include_usage":true}
	}`, responsesWSE2EPublicModel)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(firstCreate)))

	handshake := receiveResponsesWSE2EHandshake(t, upstream)
	assert.Equal(t, "/v1/responses", handshake.path)
	assert.Equal(t, "responses", handshake.subprotocol)
	assert.Equal(t, "Bearer "+responsesWSE2EChannelKey, handshake.header.Get("Authorization"))
	assert.Equal(t, "override-value", handshake.header.Get("X-Responses-Override"))
	assert.Equal(t, "existing=1, responses_websockets=2026-02-06", handshake.header.Get("OpenAI-Beta"))
	for key, values := range handshake.header {
		assert.NotContainsf(t, strings.Join(values, ","), responsesWSE2EClientKey, "client credential leaked through upstream header %s", key)
	}

	firstUpstreamRequest := receiveResponsesWSE2ERequest(t, upstream)
	assertResponsesWSE2ECreatePayload(t, firstUpstreamRequest, "first")
	preConsumed := readResponsesWSE2EBilling(t, db, user.Id, token.Id, channel.Id)
	assert.Less(t, preConsumed.userQuota, responsesWSE2EInitialQuota)
	assert.Less(t, preConsumed.tokenRemain, responsesWSE2EInitialQuota)
	assert.Equal(t, responsesWSE2EInitialQuota-preConsumed.userQuota, responsesWSE2EInitialQuota-preConsumed.tokenRemain)
	assert.Zero(t, preConsumed.consumeLogCount)

	upstream.releaseFirstFailure()
	failedEvent := readResponsesWSE2EMessage(t, client)
	var failed map[string]any
	require.NoError(t, common.Unmarshal(failedEvent, &failed))
	assert.Equal(t, "response.failed", failed["type"])
	assert.NotContains(t, string(failedEvent), responsesWSE2EUpstreamModel)

	require.Eventually(t, func() bool {
		snapshot, snapshotErr := tryReadResponsesWSE2EBilling(db, user.Id, token.Id, channel.Id)
		return snapshotErr == nil &&
			snapshot.userQuota == responsesWSE2EInitialQuota &&
			snapshot.tokenRemain == responsesWSE2EInitialQuota &&
			snapshot.tokenUsed == 0 &&
			snapshot.consumeLogCount == 0
	}, 2*time.Second, 10*time.Millisecond)

	secondCreate := fmt.Sprintf(`{
		"type":"response.create",
		"event_id":"evt-completed",
		"generate":false,
		"response":{
			"model":"%s",
			"input":"second",
			"store":false,
			"stream":true,
			"stream_options":{"include_usage":true}
		}
	}`, responsesWSE2EPublicModel)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(secondCreate)))
	secondUpstreamRequest := receiveResponsesWSE2ERequest(t, upstream)
	assertResponsesWSE2ECreatePayload(t, secondUpstreamRequest, "second")
	assert.Equal(t, int32(1), upstream.connections.Load())

	completedEvent := readResponsesWSE2EMessage(t, client)
	var completed struct {
		Type     string `json:"type"`
		Response struct {
			Model string `json:"model"`
		} `json:"response"`
	}
	require.NoError(t, common.Unmarshal(completedEvent, &completed))
	assert.Equal(t, "response.completed", completed.Type)
	assert.Equal(t, responsesWSE2EPublicModel, completed.Response.Model)
	assert.NotContains(t, string(completedEvent), responsesWSE2EUpstreamModel)

	settled := readResponsesWSE2EBilling(t, db, user.Id, token.Id, channel.Id)
	assert.Equal(t, responsesWSE2EInitialQuota-5, settled.userQuota)
	assert.Equal(t, 5, settled.userUsedQuota)
	assert.Equal(t, 1, settled.requestCount)
	assert.Equal(t, responsesWSE2EInitialQuota-5, settled.tokenRemain)
	assert.Equal(t, 5, settled.tokenUsed)
	assert.Equal(t, int64(5), settled.channelUsed)
	assert.Equal(t, int64(1), settled.consumeLogCount)
	var consumeLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
	assert.Equal(t, channel.Id, consumeLog.ChannelId)
	assert.Equal(t, token.Id, consumeLog.TokenId)
	assert.Equal(t, responsesWSE2EPublicModel, consumeLog.ModelName)
	assert.Equal(t, 2, consumeLog.PromptTokens)
	assert.Equal(t, 3, consumeLog.CompletionTokens)
	assert.Equal(t, 5, consumeLog.Quota)
	assert.EqualValues(t, 2, promptAuditCalls.Load(), "each response.create must be audited independently")
	var promptAudits []model.PromptAudit
	require.NoError(t, db.Order("id asc").Find(&promptAudits).Error)
	require.Len(t, promptAudits, 2)
	assert.Equal(t, model.PromptAuditStatusDone, promptAudits[0].Status)
	assert.Equal(t, model.PromptAuditStatusDone, promptAudits[1].Status)
	assert.NotEqual(t, promptAudits[0].PromptHash, promptAudits[1].PromptHash)

	statusRequest, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("%s/api/channel/%d/status", gateway.URL, channel.Id),
		strings.NewReader(fmt.Sprintf(`{"status":%d}`, common.ChannelStatusManuallyDisabled)),
	)
	require.NoError(t, err)
	statusRequest.Header.Set("Content-Type", "application/json")
	statusResponse, err := gateway.Client().Do(statusRequest)
	require.NoError(t, err)
	t.Cleanup(func() { _ = statusResponse.Body.Close() })
	assert.Equal(t, http.StatusOK, statusResponse.StatusCode)

	clientClose := readResponsesWSE2EClose(t, client)
	assert.Equal(t, websocket.ClosePolicyViolation, clientClose.Code)
	assert.Equal(t, service.ChannelDisabledCloseReason, clientClose.Text)
	upstreamClose := receiveResponsesWSE2EClose(t, upstream)
	assert.Equal(t, websocket.ClosePolicyViolation, upstreamClose.Code)
	assert.Equal(t, service.ChannelDisabledCloseReason, upstreamClose.Text)
	assert.Equal(t, int32(1), upstream.connections.Load())

	var storedChannel model.Channel
	require.NoError(t, db.First(&storedChannel, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, storedChannel.Status)
	upstream.assertNoError(t)
}

func newResponsesWSE2EUpstream(t *testing.T) *responsesWSE2EUpstream {
	t.Helper()
	fixture := &responsesWSE2EUpstream{
		handshakes:        make(chan responsesWSE2EHandshake, 1),
		requests:          make(chan []byte, 2),
		closed:            make(chan *websocket.CloseError, 1),
		errors:            make(chan error, 1),
		allowFirstFailure: make(chan struct{}),
	}
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"responses"},
		CheckOrigin:  func(*http.Request) bool { return true },
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.connections.Add(1)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			fixture.reportError(err)
			return
		}
		defer conn.Close()
		fixture.handshakes <- responsesWSE2EHandshake{
			path:        r.URL.Path,
			header:      r.Header.Clone(),
			subprotocol: conn.Subprotocol(),
		}

		for index := 0; index < 2; index++ {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				fixture.reportError(err)
				return
			}
			if messageType != websocket.TextMessage {
				fixture.reportError(fmt.Errorf("unexpected upstream message type %d", messageType))
				return
			}
			fixture.requests <- message
			if index == 0 {
				select {
				case <-fixture.allowFirstFailure:
				case <-time.After(5 * time.Second):
					fixture.reportError(errors.New("timed out waiting to release first failure"))
					return
				}
				err = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
					`{"type":"response.failed","response":{"id":"resp_failed","status":"failed","model":"%s","error":{"message":"fixture failure","type":"server_error","code":"fixture_failed"}}}`,
					responsesWSE2EUpstreamModel,
				)))
			} else {
				err = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
					`{"type":"response.completed","response":{"id":"resp_completed","status":"completed","model":"%s","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
					responsesWSE2EUpstreamModel,
				)))
			}
			if err != nil {
				fixture.reportError(err)
				return
			}
		}

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _, err = conn.ReadMessage()
		var closeErr *websocket.CloseError
		if !errors.As(err, &closeErr) {
			fixture.reportError(fmt.Errorf("expected upstream close frame, got %v", err))
			return
		}
		fixture.closed <- closeErr
	}))
	t.Cleanup(func() {
		fixture.releaseFirstFailure()
		fixture.server.CloseClientConnections()
		fixture.server.Close()
	})
	return fixture
}

func (fixture *responsesWSE2EUpstream) releaseFirstFailure() {
	fixture.allowFailureOnce.Do(func() { close(fixture.allowFirstFailure) })
}

func (fixture *responsesWSE2EUpstream) reportError(err error) {
	select {
	case fixture.errors <- err:
	default:
	}
}

func (fixture *responsesWSE2EUpstream) assertNoError(t *testing.T) {
	t.Helper()
	select {
	case err := <-fixture.errors:
		require.NoError(t, err)
	default:
	}
}

func receiveResponsesWSE2EHandshake(t *testing.T, fixture *responsesWSE2EUpstream) responsesWSE2EHandshake {
	t.Helper()
	select {
	case handshake := <-fixture.handshakes:
		return handshake
	case err := <-fixture.errors:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream websocket handshake")
	}
	return responsesWSE2EHandshake{}
}

func receiveResponsesWSE2ERequest(t *testing.T, fixture *responsesWSE2EUpstream) []byte {
	t.Helper()
	select {
	case request := <-fixture.requests:
		return request
	case err := <-fixture.errors:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream response.create")
	}
	return nil
}

func receiveResponsesWSE2EClose(t *testing.T, fixture *responsesWSE2EUpstream) *websocket.CloseError {
	t.Helper()
	select {
	case closeErr := <-fixture.closed:
		return closeErr
	case err := <-fixture.errors:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream close frame")
	}
	return nil
}

func readResponsesWSE2EMessage(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, message, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, messageType)
	return message
}

func readResponsesWSE2EClose(t *testing.T, conn *websocket.Conn) *websocket.CloseError {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err := conn.ReadMessage()
	var closeErr *websocket.CloseError
	require.ErrorAs(t, err, &closeErr)
	return closeErr
}

func assertResponsesWSE2ECreatePayload(t *testing.T, payload []byte, input string) {
	t.Helper()
	var event map[string]any
	require.NoError(t, common.Unmarshal(payload, &event))
	assert.Equal(t, "response.create", event["type"])
	assert.Equal(t, responsesWSE2EUpstreamModel, event["model"])
	assert.Equal(t, input, event["input"])
	assert.Equal(t, false, event["store"])
	assert.Equal(t, false, event["generate"])
	for _, key := range []string{"response", "event_id", "background", "stream", "stream_options"} {
		assert.NotContains(t, event, key)
	}
}

func readResponsesWSE2EBilling(t *testing.T, db *gorm.DB, userID, tokenID, channelID int) responsesWSE2EBillingSnapshot {
	t.Helper()
	snapshot, err := tryReadResponsesWSE2EBilling(db, userID, tokenID, channelID)
	require.NoError(t, err)
	return snapshot
}

func tryReadResponsesWSE2EBilling(db *gorm.DB, userID, tokenID, channelID int) (responsesWSE2EBillingSnapshot, error) {
	var user model.User
	if err := db.First(&user, userID).Error; err != nil {
		return responsesWSE2EBillingSnapshot{}, err
	}
	var token model.Token
	if err := db.First(&token, tokenID).Error; err != nil {
		return responsesWSE2EBillingSnapshot{}, err
	}
	var channel model.Channel
	if err := db.First(&channel, channelID).Error; err != nil {
		return responsesWSE2EBillingSnapshot{}, err
	}
	var consumeLogCount int64
	if err := db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error; err != nil {
		return responsesWSE2EBillingSnapshot{}, err
	}
	return responsesWSE2EBillingSnapshot{
		userQuota:       user.Quota,
		userUsedQuota:   user.UsedQuota,
		requestCount:    user.RequestCount,
		tokenRemain:     token.RemainQuota,
		tokenUsed:       token.UsedQuota,
		channelUsed:     channel.UsedQuota,
		consumeLogCount: consumeLogCount,
	}, nil
}
