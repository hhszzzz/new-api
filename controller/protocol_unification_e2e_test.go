package controller

import (
	"bufio"
	"bytes"
	"context"
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
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const protocolTestModel = "protocol-public"
const protocolTestUpstreamModel = "protocol-upstream"
const protocolTestToken = "protocoltestclienttoken00000000000000000000000000"

func protocolHTTPFixture(t *testing.T) (*gorm.DB, *model.User, *httptest.Server, <-chan struct{}) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.Log{}, &model.UserModelRoute{}, &model.ChannelAggregate{}, &model.UserGroupMembership{}, &model.UserModelPermission{}, &model.UserModelBlock{}))
	require.NoError(t, i18n.Init())
	previousMemoryCache, previousBatchUpdate := common.MemoryCacheEnabled, common.BatchUpdateEnabled
	previousPreconsume, previousConsumeLog, previousRetries := common.PreConsumedQuota, common.LogConsumeEnabled, common.RetryTimes
	previousRateLimit := setting.ModelRequestRateLimitEnabled
	previousStreamTimeout := constant.StreamingTimeout
	previousSensitive, previousSensitivePrompt := setting.CheckSensitiveEnabled, setting.CheckSensitiveOnPromptEnabled
	global := model_setting.GetGlobalSettings()
	previousGlobal := *global
	previousModelRatio, previousCompletionRatio, previousGroupRatio := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString(), ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 5*time.Second, time.Millisecond, "request background accounting and metrics must finish before restoring fixture globals")
		common.MemoryCacheEnabled, common.BatchUpdateEnabled = previousMemoryCache, previousBatchUpdate
		common.PreConsumedQuota, common.LogConsumeEnabled, common.RetryTimes = previousPreconsume, previousConsumeLog, previousRetries
		setting.ModelRequestRateLimitEnabled = previousRateLimit
		constant.StreamingTimeout = previousStreamTimeout
		setting.SetCheckSensitiveEnabled(previousSensitive)
		setting.SetCheckSensitiveOnPromptEnabled(previousSensitivePrompt)
		*global = previousGlobal
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatio))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(previousCompletionRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatio))
		service.ResetProxyClientCache()
	})
	common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false
	common.PreConsumedQuota, common.LogConsumeEnabled, common.RetryTimes = 10, true, 0
	setting.ModelRequestRateLimitEnabled = false
	constant.StreamingTimeout = 30
	setting.SetCheckSensitiveEnabled(false)
	setting.SetCheckSensitiveOnPromptEnabled(false)
	policy := hostdto.DefaultProtocolPolicy()
	policy.StateScope = hostdto.ProtocolStateDisabled
	global.ProtocolPolicy = &policy
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"protocol-public":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"protocol-public":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	service.InitHttpClient()
	user := &model.User{Username: "protocol-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 10000, Group: "default", Setting: `{"billing_preference":"wallet_only"}`}
	require.NoError(t, db.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: protocolTestToken, Status: common.TokenStatusEnabled, Name: "protocol-test", ExpiredTime: -1, RemainQuota: 10000, Group: "default"}
	require.NoError(t, db.Create(token).Error)
	engine := gin.New()
	finished := make(chan struct{}, 1)
	engine.Use(func(c *gin.Context) { defer func() { finished <- struct{}{} }(); c.Next() })
	engine.Use(gin.Recovery(), middleware.BodyStorageCleanup(), middleware.TokenAuth(), middleware.Distribute())
	for _, protocol := range relayconvert.Protocols() {
		format, ok := protocol.RelayFormat()
		require.True(t, ok)
		if protocol == relayconvert.ProtocolGemini {
			engine.POST("/v1beta/models/*path", func(c *gin.Context) { Relay(c, format) })
		} else {
			engine.POST(relayconvert.CanonicalPath(protocol, protocolTestModel), func(c *gin.Context) { Relay(c, format) })
		}
	}
	engine.POST("/v1/responses/compact", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponsesCompaction) })
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return db, user, server, finished
}

func protocolHTTPChannel(t *testing.T, source, target relayconvert.Protocol, baseURL string) {
	t.Helper()
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, Key: "provider-test-key", Status: common.ChannelStatusEnabled, Name: "protocol-boundary", BaseURL: &baseURL, Models: protocolTestModel, Group: "default", AutoBan: common.GetPointer(0), ModelMapping: common.GetPointer(`{"protocol-public":"protocol-upstream"}`)}
	channel.SetOtherSettings(hostdto.ChannelOtherSettings{
		ProtocolPolicy: &hostdto.ProtocolPolicy{Version: 1, UpstreamProtocols: []string{string(target)}},
		AdvancedCustom: &hostdto.AdvancedCustomConfig{Routes: []hostdto.AdvancedCustomRoute{{IncomingPath: relayconvert.CanonicalPath(source, "{model}"), UpstreamPath: "/provider/" + string(target), TargetProtocol: string(target)}}},
	})
	require.NoError(t, channel.Insert())
}

func assertProtocolHTTPAccounting(t *testing.T, db *gorm.DB, userID, charge, records int) {
	t.Helper()
	// Refunds intentionally run asynchronously after the HTTP handler returns.
	// Wait for both ledgers before allowing the fixture database to be closed.
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		var user model.User
		var token model.Token
		if assert.NoError(check, db.First(&user, userID).Error) {
			assert.Equal(check, 10000-charge, user.Quota)
		}
		if assert.NoError(check, db.Where("user_id = ?", userID).First(&token).Error) {
			assert.Equal(check, 10000-charge, token.RemainQuota)
		}
	}, 5*time.Second, 10*time.Millisecond)
	var user model.User
	require.NoError(t, db.First(&user, userID).Error)
	assert.Equal(t, 10000-charge, user.Quota)
	var token model.Token
	require.NoError(t, db.Where("user_id = ?", userID).First(&token).Error)
	assert.Equal(t, 10000-charge, token.RemainQuota)
	var logs []model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", userID, model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, records)
	if records == 1 {
		assert.Equal(t, charge, logs[0].Quota)
	}
}

func TestUnifiedProtocolHTTPRejectsSemanticLossBeforeCallingUpstream(t *testing.T) {
	for _, tc := range []struct {
		name           string
		source, target relayconvert.Protocol
		body           string
	}{
		{"chat structured output", relayconvert.ProtocolChat, relayconvert.ProtocolMessages, `{"model":"protocol-public","messages":[{"role":"user","content":"hello"}],"response_format":{"type":"json_object"}}`},
		{"gemini audio", relayconvert.ProtocolGemini, relayconvert.ProtocolChat, `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"audio/wav","data":"AA=="}}]}]}`},
		{"messages native output", relayconvert.ProtocolMessages, relayconvert.ProtocolChat, `{"model":"protocol-public","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}}`},
		{"responses unknown state", relayconvert.ProtocolResponses, relayconvert.ProtocolChat, `{"model":"protocol-public","input":[{"type":"reasoning","encrypted_content":"never-log-secret"},{"role":"user","content":"hello"}]}`},
		{"unknown extension", relayconvert.ProtocolChat, relayconvert.ProtocolResponses, `{"model":"protocol-public","messages":[{"role":"user","content":"hello"}],"future_constraint":"never-log-secret"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, user, gateway, finished := protocolHTTPFixture(t)
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, protocolUpstreamReply(tc.target, false))
			}))
			t.Cleanup(upstream.Close)
			protocolHTTPChannel(t, tc.source, tc.target, upstream.URL)
			request, err := http.NewRequest(http.MethodPost, gateway.URL+relayconvert.CanonicalPath(tc.source, protocolTestModel), strings.NewReader(tc.body))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+protocolTestToken)
			request.Header.Set("Content-Type", "application/json")
			response, err := gateway.Client().Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			select {
			case <-finished:
			case <-time.After(10 * time.Second):
				t.Fatal("request did not finish")
			}
			assert.Contains(t, []int{http.StatusBadRequest, http.StatusServiceUnavailable}, response.StatusCode, string(body))
			assert.NotContains(t, string(body), "never-log-secret")
			assert.Zero(t, calls.Load())
			assertProtocolHTTPAccounting(t, db, user.Id, 0, 0)
		})
	}
}

func TestUnifiedProtocolHTTPStreamFailureAndLateUsage(t *testing.T) {
	const first = "data: {\"id\":\"chatcmpl-boundary\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"visible\"}}]}\n\n"
	const terminal = "data: {\"id\":\"chatcmpl-boundary\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
	const lateUsage = "data: {\"id\":\"chatcmpl-boundary\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"
	for _, source := range []relayconvert.Protocol{relayconvert.ProtocolChat, relayconvert.ProtocolResponses} {
		for _, tc := range []struct {
			name, reply string
			success     bool
		}{
			{"missing terminal", first + first, false},
			{"output after error", first + first + "data: {\"error\":{\"message\":\"fixture error\",\"type\":\"server_error\"}}\n\n" + strings.ReplaceAll(first, "visible", "forbidden-after-error") + terminal + lateUsage, false},
			{"late usage", first + terminal + lateUsage, true},
		} {
			t.Run(string(source)+"/"+tc.name, func(t *testing.T) {
				db, user, gateway, finished := protocolHTTPFixture(t)
				common.RetryTimes = 2
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, tc.reply)
				}))
				t.Cleanup(upstream.Close)
				protocolHTTPChannel(t, source, relayconvert.ProtocolChat, upstream.URL)
				path, raw := protocolClientRequest(source, true)
				var value map[string]any
				require.NoError(t, common.Unmarshal([]byte(raw), &value))
				delete(value, "vendor_extension")
				encoded, err := common.Marshal(value)
				require.NoError(t, err)
				request, err := http.NewRequest(http.MethodPost, gateway.URL+path, bytes.NewReader(encoded))
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer "+protocolTestToken)
				request.Header.Set("Content-Type", "application/json")
				response, err := gateway.Client().Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Fatal("request did not finish")
				}
				assert.Contains(t, string(body), "visible")
				assert.NotContains(t, string(body), "forbidden-after-error")
				assert.EqualValues(t, 1, calls.Load(), "committed output must never be retried")
				if tc.success {
					assertProtocolHTTPAccounting(t, db, user.Id, 5, 1)
				} else {
					assert.NotContains(t, string(body), `"finish_reason":"stop"`)
					assert.NotContains(t, string(body), "response.completed")
					assertProtocolHTTPAccounting(t, db, user.Id, 0, 0)
				}
			})
		}
	}
}

func TestUnifiedProtocolHTTPClientCancellationReleasesAttempt(t *testing.T) {
	db, user, gateway, finished := protocolHTTPFixture(t)
	upstreamDone := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Repeat("data: {\"id\":\"chatcmpl-cancel\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"visible\"}}]}\n\n", 2))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); upstream.Close() })
	protocolHTTPChannel(t, relayconvert.ProtocolChat, relayconvert.ProtocolChat, upstream.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path, body := protocolClientRequest(relayconvert.ProtocolChat, true)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+protocolTestToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := gateway.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, line, "visible")
	cancel()
	select {
	case <-upstreamDone:
	case <-time.After(helper.DefaultStreamDrainTimeout + 3*time.Second):
		t.Fatal("upstream request exceeded the bounded cancellation drain")
	}
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("canceled attempt did not release its resources")
	}
	assertProtocolHTTPAccounting(t, db, user.Id, 0, 0)
}

func TestUnifiedProtocolHTTPCompactPreservesNativeFields(t *testing.T) {
	db, user, gateway, finished := protocolHTTPFixture(t)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"protocol-public":1,"protocol-public-openai-compact":1,"protocol-upstream-openai-compact":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"protocol-public":1,"protocol-public-openai-compact":1,"protocol-upstream-openai-compact":1}`))
	captured := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/responses/compact", r.URL.Path)
		var request map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &request))
		captured <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cmp_test","object":"response.compaction","created_at":1720000000,"output":[{"type":"compaction","encrypted_content":"opaque-compact"}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`)
	}))
	t.Cleanup(upstream.Close)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "provider-test-key", Status: common.ChannelStatusEnabled, Name: "compact-native", BaseURL: &upstream.URL, Models: protocolTestModel, Group: "default", ModelMapping: common.GetPointer(`{"protocol-public":"protocol-upstream"}`)}
	channel.Models += "," + ratio_setting.WithCompactModelSuffix(protocolTestModel)
	channel.SetOtherSettings(hostdto.ChannelOtherSettings{ProtocolPolicy: &hostdto.ProtocolPolicy{Version: 1, UpstreamProtocols: []string{"responses"}}})
	require.NoError(t, channel.Insert())
	request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/responses/compact", strings.NewReader(`{"model":"protocol-public","input":[{"role":"user","content":"hello"}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"parallel_tool_calls":false,"reasoning":{"effort":"low"},"text":{"format":{"type":"json_object"}},"vendor_compact":{"keep":false}}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+protocolTestToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := gateway.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("compact request did not finish")
	}
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.Contains(t, string(body), "response.compaction")
	assert.Contains(t, string(body), "opaque-compact")
	upstreamBody := <-captured
	assert.Equal(t, protocolTestUpstreamModel, upstreamBody["model"])
	assert.Equal(t, false, upstreamBody["parallel_tool_calls"])
	assert.Equal(t, map[string]any{"effort": "low"}, upstreamBody["reasoning"])
	assert.Contains(t, upstreamBody, "tools")
	assert.Contains(t, upstreamBody, "text")
	assert.Equal(t, map[string]any{"keep": false}, upstreamBody["vendor_compact"])
	assertProtocolHTTPAccounting(t, db, user.Id, 5, 1)
}

func TestUnifiedProtocolHTTPAdaptiveThinkingUsesMappedModel(t *testing.T) {
	db, user, gateway, finished := protocolHTTPFixture(t)
	captured := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &request))
		captured <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, protocolUpstreamReply(relayconvert.ProtocolMessages, false))
	}))
	t.Cleanup(upstream.Close)
	protocolHTTPChannel(t, relayconvert.ProtocolChat, relayconvert.ProtocolMessages, upstream.URL)
	require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "protocol-boundary").Update("model_mapping", `{"protocol-public":"claude-sonnet-4-6"}`).Error)
	request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(`{"model":"protocol-public","messages":[{"role":"user","content":"hello"}],"max_tokens":4096,"reasoning_effort":"high"}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+protocolTestToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := gateway.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("adaptive request did not finish")
	}
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	upstreamBody := <-captured
	assert.Equal(t, "claude-sonnet-4-6", upstreamBody["model"])
	assert.Equal(t, map[string]any{"type": "adaptive"}, upstreamBody["thinking"])
	assert.Equal(t, map[string]any{"effort": "high"}, upstreamBody["output_config"])
	assertProtocolHTTPAccounting(t, db, user.Id, 5, 1)
}

func TestProtocolManagementAPIPermissionsAndSanitizedPreview(t *testing.T) {
	admin, token := setupAccessTokenAudit(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Option{}, &model.UserGroupMembership{}, &model.UserModelPermission{}, &model.UserModelBlock{}))
	global := model_setting.GetGlobalSettings()
	previous := *global
	t.Cleanup(func() { *global = previous })
	policy := hostdto.DefaultProtocolPolicy()
	global.ProtocolPolicy = &policy
	channel := model.Channel{Type: constant.ChannelTypeAdvancedCustom, Name: "preview-channel", Key: "never-expose-channel-key", Models: protocolTestModel, Group: "default"}
	channel.SetOtherSettings(hostdto.ChannelOtherSettings{AdvancedCustom: &hostdto.AdvancedCustomConfig{Routes: []hostdto.AdvancedCustomRoute{{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/messages", Converter: relayconvert.ConverterOpenAIChatToClaudeMessages, Auth: &hostdto.AdvancedCustomRouteAuth{Type: "header", Name: "x-api-key", Value: "never-expose-route-key"}}}}})
	require.NoError(t, db.Create(&channel).Error)
	engine := gin.New()
	engine.Use(middleware.AdminAuth())
	engine.GET("/catalog", middleware.RequirePermission(authz.ChannelRead), GetProtocolCatalog)
	engine.POST("/plan", middleware.RequirePermission(authz.ChannelRead), PreviewProtocolPlan)
	engine.POST("/normalize", middleware.RequirePermission(authz.ChannelSensitiveWrite), NormalizeProtocolConfiguration)
	engine.GET("/migration", middleware.RequirePermission(authz.ChannelRead), PreviewProtocolMigration)
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{authz.ResourceChannel: {authz.ActionRead: false}}))
	for _, path := range []string{"/catalog", "/migration"} {
		assert.Equal(t, http.StatusForbidden, auditRequest(engine, http.MethodGet, path, token).Code)
	}
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{authz.ResourceChannel: {authz.ActionRead: true}}))
	for _, path := range []string{"/catalog", "/migration"} {
		response := auditRequest(engine, http.MethodGet, path, token)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), `"success":true`)
		assert.NotContains(t, response.Body.String(), "never-expose")
	}
	preview, err := common.Marshal(map[string]any{"channel": channel, "protocol": "chat", "model": protocolTestModel, "path": "/v1/chat/completions", "request": map[string]any{"model": protocolTestModel, "messages": []map[string]any{{"role": "user", "content": "hello"}}}})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/plan", bytes.NewReader(preview))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"upstream_protocol":"messages"`)
	assert.NotContains(t, response.Body.String(), "never-expose")
	assert.Equal(t, http.StatusForbidden, auditRequest(engine, http.MethodPost, "/normalize", token).Code)
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{authz.ResourceChannel: {authz.ActionRead: true, authz.ActionSensitiveWrite: true}}))
	body, err := common.Marshal(channel)
	require.NoError(t, err)
	request = httptest.NewRequest(http.MethodPost, "/normalize", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var normalized struct {
		Success bool `json:"success"`
		Data    struct {
			Settings string `json:"settings"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &normalized))
	require.True(t, normalized.Success, response.Body.String())
	var normalizedSettings hostdto.ChannelOtherSettings
	require.NoError(t, common.UnmarshalJsonStr(normalized.Data.Settings, &normalizedSettings))
	require.NotNil(t, normalizedSettings.AdvancedCustom)
	require.Len(t, normalizedSettings.AdvancedCustom.Routes, 1)
	assert.Equal(t, "messages", normalizedSettings.AdvancedCustom.Routes[0].TargetProtocol)
	assert.Empty(t, normalizedSettings.AdvancedCustom.Routes[0].Converter)
	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, channel.OtherSettings, stored.OtherSettings, "previews and import normalization must not mutate stored configuration")
}

func TestUnifiedProtocolHTTPMessagesAdaptiveThinkingToResponses(t *testing.T) {
	db, user, gateway, finished := protocolHTTPFixture(t)
	captured := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &request))
		captured <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, protocolUpstreamReply(relayconvert.ProtocolResponses, false))
	}))
	t.Cleanup(upstream.Close)
	protocolHTTPChannel(t, relayconvert.ProtocolMessages, relayconvert.ProtocolResponses, upstream.URL)
	require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "protocol-boundary").Update("model_mapping", `{"protocol-public":"gpt-5.6-sol"}`).Error)
	request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(`{"model":"protocol-public","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"thinking":{"type":"adaptive","display":"summarized"}}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+protocolTestToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := gateway.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("adaptive Messages request did not finish")
	}
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	upstreamBody := <-captured
	assert.Equal(t, "gpt-5.6-sol", upstreamBody["model"])
	assert.Equal(t, map[string]any{"effort": "high", "summary": "detailed"}, upstreamBody["reasoning"])
	assert.NotContains(t, upstreamBody, "messages")
	assertProtocolHTTPAccounting(t, db, user.Id, 5, 1)
}

func protocolClientRequest(protocol relayconvert.Protocol, stream bool) (string, string) {
	path := relayconvert.CanonicalPath(protocol, protocolTestModel)
	var body string
	switch protocol {
	case relayconvert.ProtocolChat:
		body = `{"model":"protocol-public","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"temperature":0,"vendor_extension":{"enabled":false},"stream":%t}`
	case relayconvert.ProtocolMessages:
		body = `{"model":"protocol-public","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"temperature":0,"vendor_extension":{"enabled":false},"stream":%t}`
	case relayconvert.ProtocolResponses:
		body = `{"model":"protocol-public","input":"hello","max_output_tokens":64,"temperature":0,"store":false,"vendor_extension":{"enabled":false},"stream":%t}`
	case relayconvert.ProtocolGemini:
		body = `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":64,"temperature":0},"vendor_extension":{"enabled":false}}`
		if stream {
			path = strings.ReplaceAll(path, ":generateContent", ":streamGenerateContent") + "?alt=sse"
		}
		return path, body
	}
	return path, fmt.Sprintf(body, stream)
}

func protocolUpstreamReply(protocol relayconvert.Protocol, stream bool) string {
	var message string
	switch protocol {
	case relayconvert.ProtocolChat:
		message = `{"id":"chatcmpl-test","object":"chat.completion","created":1720000000,"model":"protocol-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5},"vendor_response":"native"}`
	case relayconvert.ProtocolMessages:
		message = `{"id":"msg-test","type":"message","role":"assistant","model":"protocol-upstream","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2},"vendor_response":"native"}`
	case relayconvert.ProtocolResponses:
		message = `{"id":"resp-test","object":"response","status":"completed","model":"protocol-upstream","output":[{"id":"msg-test","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5},"vendor_response":"native"}`
	case relayconvert.ProtocolGemini:
		message = `{"responseId":"gemini-test","modelVersion":"protocol-upstream","candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5},"vendor_response":"native"}`
	}
	if !stream {
		return message
	}
	switch protocol {
	case relayconvert.ProtocolChat:
		return "data: " + `{"id":"chatcmpl-test","object":"chat.completion.chunk","created":1720000000,"model":"protocol-upstream","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}` + "\n\ndata: " + `{"id":"chatcmpl-test","object":"chat.completion.chunk","created":1720000000,"model":"protocol-upstream","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}` + "\n\ndata: [DONE]\n\n"
	case relayconvert.ProtocolMessages:
		return "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg-test","type":"message","role":"assistant","model":"protocol-upstream","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\nevent: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}` + "\n\nevent: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\nevent: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\nevent: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
	case relayconvert.ProtocolResponses:
		return "event: response.created\ndata: " + `{"type":"response.created","sequence_number":0,"response":{"id":"resp-test","object":"response","model":"protocol-upstream","status":"in_progress","output":[]}}` + "\n\nevent: response.output_item.added\ndata: " + `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"msg-test","type":"message","role":"assistant","status":"in_progress","content":[]}}` + "\n\nevent: response.content_part.added\ndata: " + `{"type":"response.content_part.added","sequence_number":2,"item_id":"msg-test","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}` + "\n\nevent: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","sequence_number":3,"item_id":"msg-test","output_index":0,"content_index":0,"delta":"hello"}` + "\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":4,\"response\":" + message + "}\n\n"
	case relayconvert.ProtocolGemini:
		return "data: " + message + "\n\n"
	}
	return ""
}

func TestUnifiedProtocolHTTPMatrix(t *testing.T) {
	for _, source := range relayconvert.Protocols() {
		for _, target := range relayconvert.Protocols() {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("advanced_%s_to_%s_stream_%t", source, target, stream), func(t *testing.T) {
					runProtocolHTTPRoundTrip(t, source, target, stream, constant.ChannelTypeAdvancedCustom)
				})
			}
		}
	}
	for _, tc := range []struct {
		protocol    relayconvert.Protocol
		channelType int
	}{
		{relayconvert.ProtocolChat, constant.ChannelTypeOpenAI},
		{relayconvert.ProtocolResponses, constant.ChannelTypeOpenAI},
		{relayconvert.ProtocolMessages, constant.ChannelTypeAnthropic},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("provider_%s_stream_%t", tc.protocol, stream), func(t *testing.T) {
				runProtocolHTTPRoundTrip(t, tc.protocol, tc.protocol, stream, tc.channelType)
			})
		}
	}
}

func TestUnifiedProtocolHTTPOutputPolicy(t *testing.T) {
	for _, target := range []relayconvert.Protocol{relayconvert.ProtocolChat, relayconvert.ProtocolMessages} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_stream_%t", target, stream), func(t *testing.T) {
				db, user, gateway, finished := protocolHTTPFixture(t)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					reply := protocolUpstreamReply(target, false)
					if target == relayconvert.ProtocolChat {
						reply = strings.Replace(reply, `"content":"hello"`, `"reasoning_content":"why","content":"hello"`, 1)
					} else {
						reply = strings.Replace(reply, `{"type":"text","text":"hello"}`, `{"type":"thinking","thinking":"why"},{"type":"text","text":"hello"}`, 1)
					}
					_, _ = io.WriteString(w, reply)
				}))
				t.Cleanup(upstream.Close)
				protocolHTTPChannel(t, relayconvert.ProtocolChat, target, upstream.URL)
				require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "protocol-boundary").Update("setting", `{"force_format":true,"thinking_to_content":true}`).Error)
				body := fmt.Sprintf(`{"model":"protocol-public","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"stream":%t}`, stream)
				request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer "+protocolTestToken)
				request.Header.Set("Content-Type", "application/json")
				response, err := gateway.Client().Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				payload, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, response.StatusCode, string(payload))
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Fatal("request did not finish")
				}
				text := gjson.GetBytes(payload, "choices.0.message.content").String()
				if stream {
					var joined strings.Builder
					for line := range strings.SplitSeq(string(payload), "\n") {
						if data, ok := strings.CutPrefix(line, "data: "); ok {
							joined.WriteString(gjson.Get(data, "choices.0.delta.content").String())
						}
					}
					text = joined.String()
				}
				assert.Equal(t, "<think>\nwhy\n</think>\nhello", text)
				assert.NotContains(t, string(payload), "vendor_response")
				assertProtocolHTTPAccounting(t, db, user.Id, 5, 1)
			})
		}
	}
}

func runProtocolHTTPRoundTrip(t *testing.T, source, target relayconvert.Protocol, stream bool, channelType int) {
	t.Helper()
	db, user, gateway, finished := protocolHTTPFixture(t)
	var calls atomic.Int32
	captured := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		captured <- body
		assert.Equal(t, "configured", r.Header.Get("X-Protocol-Test"))
		if channelType == constant.ChannelTypeAdvancedCustom {
			assert.Equal(t, "/provider/"+string(target), r.URL.Path)
			assert.Equal(t, "provider-test-key", r.Header.Get("X-Provider-Key"))
		}
		contentType := "application/json"
		if stream {
			contentType = "text/event-stream"
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, protocolUpstreamReply(target, stream))
	}))
	t.Cleanup(upstream.Close)
	settings := hostdto.ChannelOtherSettings{ProtocolPolicy: &hostdto.ProtocolPolicy{Version: 1, UpstreamProtocols: []string{string(target)}}}
	if channelType == constant.ChannelTypeAdvancedCustom {
		settings.AdvancedCustom = &hostdto.AdvancedCustomConfig{Routes: []hostdto.AdvancedCustomRoute{{
			IncomingPath: relayconvert.CanonicalPath(source, "{model}"), UpstreamPath: "/provider/" + string(target), TargetProtocol: string(target),
			Models: []string{"re:^protocol-upstream$"}, Auth: &hostdto.AdvancedCustomRouteAuth{Type: "header", Name: "X-Provider-Key", Value: "{api_key}"},
		}}}
	}
	channel := &model.Channel{Type: channelType, Key: "provider-test-key", Status: common.ChannelStatusEnabled, Name: "protocol-route", BaseURL: &upstream.URL, Models: protocolTestModel, Group: "default", AutoBan: common.GetPointer(0), ModelMapping: common.GetPointer(`{"protocol-public":"protocol-upstream"}`), HeaderOverride: common.GetPointer(`{"X-Protocol-Test":"configured"}`), ParamOverride: common.GetPointer(`{"route_override":"configured"}`)}
	channel.SetOtherSettings(settings)
	channel.SetSetting(hostdto.ChannelSettings{SystemPrompt: "gateway system", SystemPromptOverride: true})
	require.NoError(t, channel.Insert())
	path, body := protocolClientRequest(source, stream)
	if source != target {
		var value map[string]any
		require.NoError(t, common.Unmarshal([]byte(body), &value))
		delete(value, "vendor_extension")
		encoded, err := common.Marshal(value)
		require.NoError(t, err)
		body = string(encoded)
	}
	request, err := http.NewRequest(http.MethodPost, gateway.URL+path, bytes.NewBufferString(body))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+protocolTestToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	clientBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("gateway did not finish the logical request")
	}
	require.Equal(t, http.StatusOK, response.StatusCode, string(clientBody))
	require.NotContains(t, string(clientBody), `"error"`, string(clientBody))
	assert.Contains(t, string(clientBody), "hello")
	require.EqualValues(t, 1, calls.Load())
	var upstreamBody map[string]any
	require.NoError(t, common.Unmarshal(<-captured, &upstreamBody))
	assert.Equal(t, "configured", upstreamBody["route_override"])
	if target != relayconvert.ProtocolGemini {
		assert.Equal(t, protocolTestUpstreamModel, upstreamBody["model"])
		assert.Equal(t, float64(0), upstreamBody["temperature"])
	} else {
		config, ok := upstreamBody["generationConfig"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, float64(0), config["temperature"])
	}
	if source == target {
		assert.Contains(t, upstreamBody, "vendor_extension")
	}
	encodedUpstream, err := common.Marshal(upstreamBody)
	require.NoError(t, err)
	assert.Contains(t, string(encodedUpstream), "gateway system")
	if source == target && !stream {
		assert.Contains(t, string(clientBody), "vendor_response")
	}
	if stream {
		assert.Contains(t, response.Header.Get("Content-Type"), "text/event-stream")
		switch source {
		case relayconvert.ProtocolChat:
			assert.Contains(t, string(clientBody), "[DONE]")
		case relayconvert.ProtocolMessages:
			assert.Contains(t, string(clientBody), "message_stop")
		case relayconvert.ProtocolResponses:
			assert.Contains(t, string(clientBody), "response.completed")
		case relayconvert.ProtocolGemini:
			assert.Contains(t, string(clientBody), "STOP")
		}
	}
	var logs []model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", user.Id, model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 3, logs[0].PromptTokens)
	assert.Equal(t, 2, logs[0].CompletionTokens)
	assert.Equal(t, 5, logs[0].Quota)
	var after model.User
	require.NoError(t, db.First(&after, user.Id).Error)
	assert.Equal(t, 9995, after.Quota)
}
