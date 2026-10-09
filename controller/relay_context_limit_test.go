package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const claudeTestCompactionPrompt = "CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.\n- Your entire response must be plain text."

func TestIsClaudeCompactionRequest(t *testing.T) {
	cases := []struct {
		name     string
		request  *dto.ClaudeRequest
		expected bool
	}{
		{"nil request", nil, false},
		{"empty messages", &dto.ClaudeRequest{}, false},
		{
			"compact prompt as last user message",
			&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
				{Role: "user", Content: claudeTestCompactionPrompt},
			}},
			true,
		},
		{
			"compact prompt in text blocks with padding",
			&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "hello"},
				{Role: "user", Content: []any{
					map[string]any{"type": "text", "text": "\n  " + claudeTestCompactionPrompt},
				}},
			}},
			true,
		},
		{
			"marker only in an earlier turn",
			&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: claudeTestCompactionPrompt},
				{Role: "assistant", Content: "the summary"},
				{Role: "user", Content: "continue"},
			}},
			false,
		},
		{
			"normal conversation",
			&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "hi"},
			}},
			false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, isClaudeCompactionRequest(testCase.request))
		})
	}
}

func TestEffectiveContextLimit(t *testing.T) {
	assert.Equal(t, 0, effectiveContextLimit(0, 0))
	assert.Equal(t, 400000, effectiveContextLimit(400000, 0))
	assert.Equal(t, 128000, effectiveContextLimit(0, 128000))
	assert.Equal(t, 128000, effectiveContextLimit(400000, 128000))
	assert.Equal(t, 400000, effectiveContextLimit(400000, 600000))
}

func TestClaudeContextLimitRejection(t *testing.T) {
	assert.Nil(t, claudeContextLimitRejection(0, 400000))
	assert.Nil(t, claudeContextLimitRejection(985000, 0))
	assert.Nil(t, claudeContextLimitRejection(400000, 400000))
	assert.Nil(t, claudeContextLimitRejection(399999, 400000))

	rejection := claudeContextLimitRejection(985000, 400000)
	require.NotNil(t, rejection)
	assert.Equal(t, http.StatusBadRequest, rejection.StatusCode)
	assert.Equal(t, hosttypes.ErrorCodeContextLimitExceeded, rejection.GetErrorCode())
	assert.Equal(t, "prompt is too long: 985000 tokens > 400000 maximum", rejection.Error())
	assert.True(t, hosttypes.IsSkipRetryError(rejection))

	toClaude := rejection.ToClaudeError()
	assert.Equal(t, "invalid_request_error", toClaude.Type)
	assert.Equal(t, "prompt is too long: 985000 tokens > 400000 maximum", toClaude.Message)
}

func TestEnforceClaudeContextLimit(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	require.NoError(t, db.Create(&model.Channel{
		Id: 31, Type: 1, Key: "key", Name: "ctx-guard-channel", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "ctx-guard-model", ChannelId: 31, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "ctx-guard-model", ContextLimit: 400000, Status: 1}).Error)
	model.RefreshPricing()

	plainChannel := &model.Channel{Id: 31, Type: 1, Key: "key", Name: "ctx-guard-channel", Status: common.ChannelStatusEnabled}
	strictChannel := &model.Channel{Id: 32, Type: 1, Key: "key", Name: "ctx-guard-strict", Status: common.ChannelStatusEnabled}
	strictChannel.SetOtherSettings(hostdto.ChannelOtherSettings{ContextLimits: []hostdto.ChannelContextLimitRule{
		{ModelPattern: "^ctx-guard-model$", ContextLimit: 128000},
	}})

	normalRequest := &dto.ClaudeRequest{Model: "ctx-guard-model", Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}}
	compactRequest := &dto.ClaudeRequest{Model: "ctx-guard-model", Messages: []dto.ClaudeMessage{
		{Role: "user", Content: "hello"},
		{Role: "user", Content: claudeTestCompactionPrompt},
	}}
	newInfo := func(modelName string, estimate int) *relaycommon.RelayInfo {
		info := &relaycommon.RelayInfo{OriginModelName: modelName}
		info.SetEstimatePromptTokens(estimate)
		return info
	}

	// 全局声明生效：超限的普通请求被拒，错误文案与 Anthropic 原生一致。
	rejection := enforceClaudeContextLimit(types.RelayFormatClaude, normalRequest, newInfo("ctx-guard-model", 985000), plainChannel)
	require.NotNil(t, rejection)
	assert.Equal(t, "prompt is too long: 985000 tokens > 400000 maximum", rejection.Error())

	// 压缩请求本身放行（压缩需要完整历史）。
	assert.Nil(t, enforceClaudeContextLimit(types.RelayFormatClaude, compactRequest, newInfo("ctx-guard-model", 985000), plainChannel))

	// 渠道声明更严时取渠道值。
	rejection = enforceClaudeContextLimit(types.RelayFormatClaude, normalRequest, newInfo("ctx-guard-model", 985000), strictChannel)
	require.NotNil(t, rejection)
	assert.Equal(t, "prompt is too long: 985000 tokens > 128000 maximum", rejection.Error())

	// 未超限、无法估算、其他协议入口、未声明上限的模型都不拦截。
	assert.Nil(t, enforceClaudeContextLimit(types.RelayFormatClaude, normalRequest, newInfo("ctx-guard-model", 1000), plainChannel))
	assert.Nil(t, enforceClaudeContextLimit(types.RelayFormatClaude, normalRequest, newInfo("ctx-guard-model", 0), plainChannel))
	assert.Nil(t, enforceClaudeContextLimit(types.RelayFormatOpenAI, normalRequest, newInfo("ctx-guard-model", 985000), plainChannel))
	unlimitedRequest := &dto.ClaudeRequest{Model: "ctx-guard-unlimited", Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}}
	assert.Nil(t, enforceClaudeContextLimit(types.RelayFormatClaude, unlimitedRequest, newInfo("ctx-guard-unlimited", 985000), plainChannel))
}

func TestWriteRelayErrorResponseContextLimitAlwaysJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 上下文上限拦截模拟真实 API 的流前校验：stream 请求也返回 400 JSON。
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	rejection := claudeContextLimitRejection(985000, 400000)
	require.NotNil(t, rejection)
	writeRelayErrorResponse(context, types.RelayFormatClaude, rejection, &relaycommon.RelayInfo{IsStream: true}, nil)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	var parsed struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &parsed))
	assert.Equal(t, "error", parsed.Type)
	assert.Equal(t, "invalid_request_error", parsed.Error.Type)
	assert.Equal(t, "prompt is too long: 985000 tokens > 400000 maximum", parsed.Error.Message)

	// 回归：其余错误的流式路径保持 SSE 事件通道不变。
	otherRecorder := httptest.NewRecorder()
	otherContext, _ := gin.CreateTestContext(otherRecorder)
	otherError := hosttypes.NewErrorWithStatusCode(errors.New("boom"), hosttypes.ErrorCodeBadRequestBody, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	writeRelayErrorResponse(otherContext, types.RelayFormatClaude, otherError, &relaycommon.RelayInfo{IsStream: true}, nil)
	assert.Contains(t, otherRecorder.Header().Get("Content-Type"), "text/event-stream")
}
