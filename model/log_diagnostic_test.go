package model

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendLogDiagnosticsKeepsOnlySafeRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalNodeName := common.NodeName
	common.NodeName = "demo-node"
	t.Cleanup(func() { common.NodeName = originalNodeName })
	setting := operation_setting.GetLogDiagnosticSetting()
	original := *operation_setting.GetLogDiagnosticSettingSnapshot()
	t.Cleanup(func() {
		*setting = original
		operation_setting.NormalizeLogDiagnosticSetting()
	})
	setting.RecordIP = true
	setting.RecordHeaders = true
	setting.ExtraHeaders = []string{"x-safe-extra"}
	operation_setting.NormalizeLogDiagnosticSetting()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/messages?secret=query-value",
		http.NoBody,
	)
	context.Request.RemoteAddr = "203.0.113.27:54321"
	context.Request.Header.Set("User-Agent", "claude-cli/2.0")
	context.Request.Header.Set("Authorization", "Bearer secret")
	context.Request.Header.Set("Cookie", "session=secret")
	context.Request.Header.Set("X-Codex-Parent-Thread-Id", "thread-secret")
	context.Request.Header.Set("X-Safe-Extra", "visible")
	common.SetContextKey(context, constant.ContextKeyClientName, "claude_code")
	common.SetContextKey(context, constant.ContextKeyRequestProtocol, "messages")
	common.SetContextKey(context, constant.ContextKeyUpstreamProtocol, "chat")
	common.SetContextKey(context, constant.ContextKeyProtocolConverter, "anthropic_messages_to_openai_chat_completions")
	common.SetContextKey(context, constant.ContextKeyUpstreamRequestSize, int64(321))
	context.Set("use_channel", []string{"2", "3"})

	other := appendLogDiagnostics(context, 0, map[string]interface{}{})

	diagnostics, ok := other["diagnostics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, http.MethodPost, diagnostics["method"])
	assert.Equal(t, "/v1/messages", diagnostics["path"])
	assert.NotContains(t, diagnostics["path"], "query-value")
	assert.Equal(t, "203.0.113.27", diagnostics["ip"])
	assert.Equal(t, "claude_code", diagnostics["client"])
	assert.Equal(t, "messages", diagnostics["request_protocol"])
	assert.Equal(t, "chat", diagnostics["upstream_protocol"])
	assert.Equal(t, int64(321), diagnostics["upstream_request_size"])
	assert.Equal(t, "demo-node", diagnostics["node"])

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, []string{"2", "3"}, adminInfo["retry_chain"])
	headers, ok := adminInfo["request_headers"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "claude-cli/2.0", headers["user-agent"])
	assert.Equal(t, "visible", headers["x-safe-extra"])
	assert.NotContains(t, headers, "authorization")
	assert.NotContains(t, headers, "cookie")
	assert.Regexp(t, `^sha256:[0-9a-f]{16}$`, headers["x-codex-parent-thread-id"])
}

func TestAppendLogDiagnosticsUsesAggregateOnlyForSingleAggregateRoutePool(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	firstAggregate := &ChannelAggregate{Name: "Aggregate A"}
	secondAggregate := &ChannelAggregate{Name: "Aggregate B"}
	require.NoError(t, SaveChannelAggregate(firstAggregate))
	require.NoError(t, SaveChannelAggregate(secondAggregate))
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9101, Name: "A-1", Key: "key-a1", AggregateId: &firstAggregate.Id},
		{Id: 9102, Name: "A-2", Key: "key-a2", AggregateId: &firstAggregate.Id},
		{Id: 9201, Name: "B-1", Key: "key-b1", AggregateId: &secondAggregate.Id},
	}).Error)

	newRouteContext := func(channelIds []int) *gin.Context {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
		common.SetContextKey(context, constant.ContextKeyUserModelRouteId, 7)
		common.SetContextKey(context, constant.ContextKeyUserModelRoutePool, "Route Pool")
		common.SetContextKey(context, constant.ContextKeyUserModelRouteChannel, channelIds)
		return context
	}

	sameAggregate := appendLogDiagnostics(newRouteContext([]int{9101, 9102}), 9101, map[string]interface{}{})
	sameAdminInfo, ok := sameAggregate["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Aggregate A", sameAdminInfo["surface_channel_name"])

	crossAggregate := appendLogDiagnostics(newRouteContext([]int{9101, 9201}), 9101, map[string]interface{}{})
	crossAdminInfo, ok := crossAggregate["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Route Pool", crossAdminInfo["surface_channel_name"])
}

func TestFormatUserLogsClearsChannelsAndLimitsDiagnostics(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"admin_info": map[string]interface{}{
			"actual_channel_id":   9,
			"actual_channel_name": "private-child",
			"request_headers": map[string]string{
				"user-agent": "codex-cli/1.0",
			},
		},
		"diagnostics": map[string]interface{}{
			"client":                "codex",
			"request_protocol":      "responses",
			"upstream_protocol":     "chat",
			"protocol_converter":    "responses_to_chat",
			"route_rule_id":         4,
			"method":                http.MethodPost,
			"path":                  "/v1/responses",
			"status_code":           200,
			"upstream_request_size": int64(321),
			"node":                  "demo-node",
		},
	})
	logs := []*Log{{
		ChannelId:         9,
		ChannelName:       "private-child",
		UpstreamRequestId: "upstream-secret",
		Ip:                "203.0.113.27",
		Other:             other,
	}}

	formatUserLogs(logs, 0, false)

	assert.Zero(t, logs[0].ChannelId)
	assert.Empty(t, logs[0].ChannelName)
	assert.Empty(t, logs[0].UpstreamRequestId)
	assert.Equal(t, "203.0.113.27", logs[0].Ip)
	publicOther, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, publicOther, "admin_info")
	publicDiagnostics, ok := publicOther["diagnostics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "codex", publicDiagnostics["client"])
	assert.Equal(t, "responses", publicDiagnostics["request_protocol"])
	assert.NotContains(t, publicDiagnostics, "upstream_protocol")
	assert.NotContains(t, publicDiagnostics, "protocol_converter")
	assert.NotContains(t, publicDiagnostics, "route_rule_id")
	assert.NotContains(t, publicDiagnostics, "method")
	assert.NotContains(t, publicDiagnostics, "path")
	assert.NotContains(t, publicDiagnostics, "status_code")
	assert.NotContains(t, publicDiagnostics, "upstream_request_size")
	assert.NotContains(t, publicDiagnostics, "node")
}

func TestFormatAdminSelfLogsKeepsRoutingAndPublicDiagnostics(t *testing.T) {
	logs := []*Log{{
		ChannelId:   9,
		ChannelName: "private-child",
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"is_model_mapped":     true,
				"upstream_model_name": "private-model",
				"actual_channel_name": "private-child",
				"request_headers": map[string]string{
					"user-agent": "codex-cli/1.0",
				},
			},
			"diagnostics": map[string]interface{}{
				"client": "codex",
				"ip":     "203.0.113.27",
			},
			"request_conversion": []string{"responses_to_chat"},
			"request_path":       "/v1/responses",
			"audit_info":         map[string]interface{}{"route": "private"},
			"stream_status":      map[string]interface{}{"upstream": "private"},
		}),
	}}

	formatUserLogs(logs, 0, true)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	diagnostics, ok := other["diagnostics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "codex", diagnostics["client"])
	assert.NotContains(t, diagnostics, "ip")
	assert.NotContains(t, other, "request_conversion")
	assert.NotContains(t, other, "request_path")
	assert.NotContains(t, other, "audit_info")
	assert.NotContains(t, other, "stream_status")
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, adminInfo["is_model_mapped"])
	assert.Equal(t, "private-model", adminInfo["upstream_model_name"])
	assert.NotContains(t, adminInfo, "actual_channel_name")
	assert.NotContains(t, adminInfo, "request_headers")
}

func TestCollectDiagnosticHeadersEnforcesConfiguredSafetyAndSizeBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setting := operation_setting.GetLogDiagnosticSetting()
	original := *operation_setting.GetLogDiagnosticSettingSnapshot()
	t.Cleanup(func() {
		*setting = original
		operation_setting.NormalizeLogDiagnosticSetting()
	})
	setting.RecordHeaders = true
	setting.ExtraHeaders = make([]string, 0, 16)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)
	for index := 0; index < 16; index++ {
		name := fmt.Sprintf("x-safe-%02d", index)
		setting.ExtraHeaders = append(setting.ExtraHeaders, name)
		context.Request.Header.Set(name, strings.Repeat(string(rune('a'+index)), 700))
	}
	operation_setting.NormalizeLogDiagnosticSetting()
	context.Request.Header.Set("X-Service-Token", "must-not-be-recorded")
	context.Request.Header.Set("X-Client-Signature", "must-not-be-recorded")

	headers := collectDiagnosticHeaders(context)
	assert.NotContains(t, headers, "x-service-token")
	assert.NotContains(t, headers, "x-client-signature")
	require.NotEmpty(t, headers)

	total := 0
	for name, value := range headers {
		assert.LessOrEqual(t, len(value), maxDiagnosticHeaderValueBytes)
		total += len(name) + len(value) + 2
	}
	assert.LessOrEqual(t, total, maxDiagnosticHeaderBytes)
	assert.Less(t, len(headers), len(setting.ExtraHeaders))
}

func TestFormatAdminLogsUsesStoredSurfaceChannelSnapshot(t *testing.T) {
	logs := []*Log{{
		ChannelId:   7,
		ChannelName: "current-child-name",
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"surface_channel_name": "Demo route pool",
				"actual_channel_name":  "Snapshot child",
				"request_headers": map[string]string{
					"user-agent": "codex-cli/1.0",
				},
			},
		}),
	}}

	formatAdminLogs(logs)

	assert.Equal(t, "Demo route pool", logs[0].ChannelName)
	adminOther, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := adminOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Snapshot child", adminInfo["actual_channel_name"])
	assert.Contains(t, adminInfo, "request_headers")
}
