package controller

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestShouldApplyUserRateLimitsOnlyToStandardTextRoutes(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		mode   int
		format types.RelayFormat
		stream bool
		want   bool
	}{
		{name: "chat completions", path: "/v1/chat/completions", mode: relayconstant.RelayModeChatCompletions, format: types.RelayFormatOpenAI, want: true},
		{name: "legacy completions", path: "/v1/completions", mode: relayconstant.RelayModeCompletions, format: types.RelayFormatOpenAI, want: true},
		{name: "messages", path: "/v1/messages", mode: relayconstant.RelayModeUnknown, format: types.RelayFormatClaude, want: true},
		{name: "responses", path: "/v1/responses", mode: relayconstant.RelayModeResponses, format: types.RelayFormatOpenAIResponses, want: true},
		{name: "responses compact", path: "/v1/responses/compact", mode: relayconstant.RelayModeResponsesCompact, format: types.RelayFormatOpenAIResponsesCompaction, want: true},
		{name: "realtime", path: "/v1/realtime", mode: relayconstant.RelayModeRealtime, format: types.RelayFormatOpenAIRealtime, want: true},
		{name: "gemini generate", path: "/v1beta/models/gemini:generateContent", mode: relayconstant.RelayModeGemini, format: types.RelayFormatGemini, want: true},
		{name: "gemini stream", path: "/v1beta/models/gemini:streamGenerateContent", mode: relayconstant.RelayModeGemini, format: types.RelayFormatGemini, stream: true, want: true},
		{name: "gemini embeddings", path: "/v1beta/models/gemini:embedContent", mode: relayconstant.RelayModeGemini, format: types.RelayFormatGemini},
		{name: "embeddings", path: "/v1/embeddings", mode: relayconstant.RelayModeEmbeddings, format: types.RelayFormatEmbedding},
		{name: "rerank", path: "/v1/rerank", mode: relayconstant.RelayModeRerank, format: types.RelayFormatRerank},
		{name: "moderation", path: "/v1/moderations", mode: relayconstant.RelayModeModerations, format: types.RelayFormatOpenAI},
		{name: "image", path: "/v1/images/generations", mode: relayconstant.RelayModeImagesGenerations, format: types.RelayFormatOpenAIImage},
		{name: "audio", path: "/v1/audio/speech", mode: relayconstant.RelayModeAudioSpeech, format: types.RelayFormatOpenAIAudio},
		{name: "search", path: "/v1/alpha/search", mode: relayconstant.RelayModeAlphaSearch, format: types.RelayFormatOpenAIAlphaSearch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", test.path, nil)
			info := &relaycommon.RelayInfo{RelayMode: test.mode, RelayFormat: test.format, IsStream: test.stream}
			assert.Equal(t, test.want, shouldApplyUserRateLimits(c, info))
		})
	}
}

func TestShouldApplyUserRateLimitsExcludesPlaygroundAndChannelTests(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI, IsPlayground: true}
	assert.False(t, shouldApplyUserRateLimits(c, info))
	info.IsPlayground = false
	info.IsChannelTest = true
	assert.False(t, shouldApplyUserRateLimits(c, info))
}
