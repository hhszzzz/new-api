package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoStoresModelRoutingInAdminInfo(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		OriginModelName:   "requested-model",
		ParamOverrideAudit: []string{
			"set model = upstream-model",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			IsModelMapped:     true,
			UpstreamModelName: "upstream-model",
		},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	assert.NotContains(t, other, "is_model_mapped")
	assert.NotContains(t, other, "upstream_model_name")
	assert.NotContains(t, other, "po")
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, adminInfo["is_model_mapped"])
	assert.Equal(t, "upstream-model", adminInfo["upstream_model_name"])
	assert.Equal(t, true, adminInfo["model_routing_checked"])
	assert.Equal(t, []string{"set model = upstream-model"}, adminInfo["po"])

	serialized := common.MapToJsonStr(other)
	require.NotEmpty(t, serialized)
	parsed, err := common.StrToMap(serialized)
	require.NoError(t, err)
	assert.NotContains(t, parsed, "po")
	parsedAdminInfo, ok := parsed["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"set model = upstream-model"}, parsedAdminInfo["po"])
}

func TestGenerateTextOtherInfoOmitsClaudeReportedVersionAlias(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	now := time.Now()
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		OriginModelName:   "claude-opus-4-8",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-opus-4.8",
		},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, adminInfo["model_routing_checked"])
	assert.NotContains(t, adminInfo, "is_model_mapped")
	assert.NotContains(t, adminInfo, "upstream_model_name")
}

func TestGenerateTextOtherInfoRecordsMillisecondDuration(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	start := time.Now().Add(-1250 * time.Millisecond)
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         start,
		FirstResponseTime: start.Add(250 * time.Millisecond),
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, 0, 1)

	durationMs, ok := other["duration_ms"].(int64)
	require.True(t, ok)
	assert.GreaterOrEqual(t, durationMs, int64(1250))
}

func TestAppendModelRoutingAdminInfoPreservesExistingAdminFields(t *testing.T) {
	other := map[string]interface{}{
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{"kind": "overflow"},
		},
	}

	AppendModelRoutingAdminInfo(other, true, "upstream-model")

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, adminInfo, "quota_saturation")
	assert.Equal(t, true, adminInfo["model_routing_checked"])
	assert.Equal(t, true, adminInfo["is_model_mapped"])
	assert.Equal(t, "upstream-model", adminInfo["upstream_model_name"])
}

func TestAppendModelRoutingAdminInfoRecordsCompletedUnroutedCheck(t *testing.T) {
	other := map[string]interface{}{}

	AppendModelRoutingAdminInfo(other, false, "requested-model")

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, adminInfo["model_routing_checked"])
	assert.NotContains(t, adminInfo, "is_model_mapped")
	assert.NotContains(t, adminInfo, "upstream_model_name")
}
