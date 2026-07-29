package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeUpstreamServesGeminiClientJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/public-model:generateContent", nil)
	info := claudeGeminiBridgeInfo(false)

	usage, apiErr := (&Adaptor{}).DoResponse(ctx, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"msg_1","type":"message","role":"assistant","model":"claude-upstream",
			"content":[{"type":"text","text":"hello"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":5,"output_tokens":2}
		}`)),
	}, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	usageDto, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 5, usageDto.PromptTokens)
	assert.Equal(t, 2, usageDto.CompletionTokens)

	var response dto.GeminiChatResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotEmpty(t, response.Candidates)
	require.NotEmpty(t, response.Candidates[0].Content.Parts)
	assert.Equal(t, "hello", response.Candidates[0].Content.Parts[0].Text)
	require.NotNil(t, response.Candidates[0].FinishReason)
	assert.Equal(t, "STOP", *response.Candidates[0].FinishReason)
}

func TestClaudeUpstreamServesGeminiClientSSE(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/public-model:streamGenerateContent?alt=sse", nil)
	info := claudeGeminiBridgeInfo(true)
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-upstream","usage":{"input_tokens":5,"output_tokens":1}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	usage, apiErr := (&Adaptor{}).DoResponse(ctx, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	usageDto, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 5, usageDto.PromptTokens)
	assert.Equal(t, 2, usageDto.CompletionTokens)

	got := recorder.Body.String()
	assert.Contains(t, got, `"text":"hello"`)
	assert.Contains(t, got, `"finishReason":"STOP"`)
	assert.Contains(t, got, `"usageMetadata"`)
	assert.NotContains(t, got, `"type":"message_start"`)
}

func TestClaudeAdaptorConvertsGeminiRequest(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/public-model:generateContent", nil)
	info := claudeGeminiBridgeInfo(false)

	converted, err := (&Adaptor{}).ConvertGeminiRequest(ctx, info, &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "user", Parts: []dto.GeminiPart{{Text: "hi"}}},
		},
	})

	require.NoError(t, err)
	claudeRequest, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.NotEmpty(t, claudeRequest.Messages)
	assert.Equal(t, "user", claudeRequest.Messages[0].Role)
}

func claudeGeminiBridgeInfo(stream bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		IsStream:        stream,
		DisablePing:     true,
		OriginModelName: "public-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeAnthropic,
			UpstreamModelName: "claude-upstream",
			IsModelMapped:     true,
		},
	}
}
