package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
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
	info.UpstreamRequestBodySize = 128
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
	assert.Zero(t, info.UpstreamRequestBodySize)
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
