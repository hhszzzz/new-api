package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestInitChannelMetaResetsAttemptScopedProtocolState(t *testing.T) {
	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "public-model",
		Stream: &stream,
		Tools:  []byte(`[{"type":"web_search_preview","search_context_size":"high"}]`),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := GenRelayInfoResponses(ctx, request)
	info.InitRequestConversionChain()
	info.InitChannelMeta(ctx)
	info.AppendRequestConversion(types.RelayFormatClaude)
	info.FinalRequestRelayFormat = types.RelayFormatClaude
	info.StreamStatus = NewStreamStatus()
	info.ThinkingContentInfo = ThinkingContentInfo{HasSentThinkingContent: true}
	info.SendResponseCount = 4
	info.ReceivedResponseCount = 5
	info.ShouldIncludeUsage = true
	info.DisablePing = true
	info.RuntimeHeadersOverride = map[string]interface{}{"x-test": "stale"}
	info.UseRuntimeHeadersOverride = true
	info.ParamOverrideAudit = []string{"stale"}
	info.ReasoningEffort = "high"
	info.ClaudeConvertInfo = &ClaudeConvertInfo{Done: true, Index: 9}
	info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount = 2
	info.ResponsesUsageInfo.BuiltInTools["stale_function"] = &BuildInToolInfo{ToolName: "stale_function", CallCount: 1}

	info.InitChannelMeta(ctx)

	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponses}, info.RequestConversionChain)
	assert.Empty(t, info.FinalRequestRelayFormat)
	assert.Nil(t, info.StreamStatus)
	assert.True(t, info.ThinkingContentInfo.IsFirstThinkingContent)
	assert.False(t, info.ThinkingContentInfo.HasSentThinkingContent)
	assert.Zero(t, info.SendResponseCount)
	assert.Zero(t, info.ReceivedResponseCount)
	assert.False(t, info.ShouldIncludeUsage)
	assert.False(t, info.DisablePing)
	assert.Nil(t, info.RuntimeHeadersOverride)
	assert.False(t, info.UseRuntimeHeadersOverride)
	assert.Nil(t, info.ParamOverrideAudit)
	assert.Empty(t, info.ReasoningEffort)
	assert.True(t, info.IsStream)
	assert.Nil(t, info.ClaudeConvertInfo)
	require.NotNil(t, info.ResponsesUsageInfo)
	assert.Zero(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	assert.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
	assert.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "stale_function")
}

func TestInitChannelMetaRecreatesClaudeStreamConversionState(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := GenRelayInfoClaude(ctx, &dto.ClaudeRequest{Model: "claude-test"})
	stale := info.ClaudeConvertInfo
	stale.Done = true
	stale.Index = 7

	info.InitChannelMeta(ctx)

	require.NotNil(t, info.ClaudeConvertInfo)
	assert.NotSame(t, stale, info.ClaudeConvertInfo)
	assert.Equal(t, LastMessageTypeNone, info.ClaudeConvertInfo.LastMessagesType)
	assert.False(t, info.ClaudeConvertInfo.Done)
	assert.Zero(t, info.ClaudeConvertInfo.Index)
}

func TestRelayInfoMetaTypedNilReceiver(t *testing.T) {
	var info *RelayInfo
	var meta convmeta.Meta = info

	assert.Empty(t, meta.GetOriginModelName())
	assert.Empty(t, meta.GetUpstreamModelName())
	assert.False(t, meta.HasChannelMeta())
	assert.Zero(t, meta.GetChannelID())
	assert.Zero(t, meta.GetChannelType())
	assert.False(t, meta.GetIsStream())
	assert.Empty(t, meta.GetReasoningEffort())
	assert.Zero(t, meta.GetEstimatePromptTokens())
	assert.Zero(t, meta.GetSendResponseCount())

	assert.NotPanics(t, func() {
		meta.SetReasoningEffort("high")
		meta.IncrSendResponseCount()
		meta.AppendRequestConversion(types.RelayFormatClaude)
	})

	firstState := meta.EnsureClaudeConvertInfo()
	secondState := meta.EnsureClaudeConvertInfo()
	require.NotNil(t, firstState)
	require.NotNil(t, secondState)
	assert.Equal(t, convmeta.LastMessageTypeNone, firstState.LastMessagesType)
	assert.NotSame(t, firstState, secondState)

	firstOptions := meta.ConvOptions()
	secondOptions := meta.ConvOptions()
	require.NotNil(t, firstOptions)
	require.NotNil(t, secondOptions)
	assert.NotSame(t, firstOptions, secondOptions)
	assert.NotNil(t, firstOptions.Claude.DefaultMaxTokens)
	assert.NotNil(t, firstOptions.Gemini.SupportsImagine)
	assert.NotNil(t, firstOptions.Gemini.SafetySetting)
	assert.NotNil(t, firstOptions.PreserveThinkingSuffix)
}

func TestRelayInfoConvOptionsUsesCCSwitchReasoningVendorHints(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		model       string
		baseURL     string
		want        bool
	}{
		{name: "generic strict chat", channelType: constant.ChannelTypeOpenAI, model: "openpangu-2.0-flash", baseURL: "https://example.test/v1", want: false},
		{name: "deepseek channel", channelType: constant.ChannelTypeDeepSeek, model: "provider-alias", want: true},
		{name: "kimi model through openrouter", channelType: constant.ChannelTypeOpenRouter, model: "moonshotai/kimi-k2-thinking", baseURL: "https://openrouter.ai/api/v1", want: true},
		{name: "mimo endpoint", channelType: constant.ChannelTypeOpenAI, model: "provider-alias", baseURL: "https://api.xiaomimimo.com/v1", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &RelayInfo{ChannelMeta: &ChannelMeta{
				ChannelType:       test.channelType,
				ChannelBaseUrl:    test.baseURL,
				UpstreamModelName: test.model,
			}}

			assert.Equal(t, test.want, info.ConvOptions().PreserveChatReasoningContent)
		})
	}
}

func TestGenRelayInfoCapturesRequestReasoningEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		path        string
		relayFormat types.RelayFormat
		request     dto.Request
		expected    string
	}{
		{
			name:        "OpenAI chat top-level effort",
			path:        "/v1/chat/completions",
			relayFormat: types.RelayFormatOpenAI,
			request:     &dto.GeneralOpenAIRequest{Model: "gpt-5.6-sol", ReasoningEffort: " high "},
			expected:    "high",
		},
		{
			name:        "OpenRouter nested chat effort",
			path:        "/v1/chat/completions",
			relayFormat: types.RelayFormatOpenAI,
			request:     &dto.GeneralOpenAIRequest{Model: "anthropic/claude", Reasoning: json.RawMessage(`{"effort":"xhigh"}`)},
			expected:    "xhigh",
		},
		{
			name:        "OpenAI Responses effort",
			path:        "/v1/responses",
			relayFormat: types.RelayFormatOpenAIResponses,
			request:     &dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol", Reasoning: &dto.Reasoning{Effort: "max"}},
			expected:    "max",
		},
		{
			name:        "explicit none is preserved",
			path:        "/v1/responses",
			relayFormat: types.RelayFormatOpenAIResponses,
			request:     &dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol", Reasoning: &dto.Reasoning{Effort: "none"}},
			expected:    "none",
		},
		{
			name:        "non-string nested effort is ignored",
			path:        "/v1/chat/completions",
			relayFormat: types.RelayFormatOpenAI,
			request:     &dto.GeneralOpenAIRequest{Model: "anthropic/claude", Reasoning: json.RawMessage(`{"effort":42}`)},
			expected:    "",
		},
		{
			name:        "Claude output config effort",
			path:        "/v1/messages",
			relayFormat: types.RelayFormatClaude,
			request:     &dto.ClaudeRequest{Model: "claude-opus-4-7", OutputConfig: json.RawMessage(`{"effort":"medium"}`)},
			expected:    "medium",
		},
		{
			name:        "Gemini thinking level",
			path:        "/v1beta/models/gemini-3-pro:generateContent",
			relayFormat: types.RelayFormatGemini,
			request: &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{
				ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "low"},
			}},
			expected: "low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", tt.path, nil)

			info, err := GenRelayInfo(ctx, tt.relayFormat, tt.request, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, info.ReasoningEffort)
		})
	}
}

func TestInitChannelMetaRestoresRequestReasoningEffortForRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model:     "gpt-5.6-sol",
		Reasoning: &dto.Reasoning{Effort: "max"},
	}
	info, err := GenRelayInfo(ctx, types.RelayFormatOpenAIResponses, request, nil)
	require.NoError(t, err)

	info.SetReasoningEffort("high")
	info.InitChannelMeta(ctx)
	assert.Equal(t, "max", info.ReasoningEffort)

	info.SetReasoningEffort("low")
	info.InitChannelMeta(ctx)
	assert.Equal(t, "max", info.ReasoningEffort)
}
