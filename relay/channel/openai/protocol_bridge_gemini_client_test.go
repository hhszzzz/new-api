package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatUpstreamReturnsGeminiJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-public:generateContent", nil)
	body := `{"id":"chatcmpl_upstream","object":"chat.completion","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-public",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-chat-model",
		},
	}

	usage, apiErr := OpenaiHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)

	var response dto.GeminiChatResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotEmpty(t, response.Candidates)
	require.NotEmpty(t, response.Candidates[0].Content.Parts)
	assert.Equal(t, "hello", response.Candidates[0].Content.Parts[0].Text)
	require.NotNil(t, response.UsageMetadata)
	assert.Equal(t, 11, response.UsageMetadata.TotalTokenCount)
}

func TestChatUpstreamReturnsGeminiSSE(t *testing.T) {
	withOpenAIProtocolStreamTestMode(t)
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_upstream","object":"chat.completion.chunk","created":1710000000,"model":"provider-chat-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-public:streamGenerateContent?alt=sse", nil)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.RelayFormat = types.RelayFormatGemini
	info.OriginModelName = "gemini-public"
	info.ChannelMeta.UpstreamModelName = "provider-chat-model"

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)

	got := recorder.Body.String()
	assert.Contains(t, got, `"text":"hello"`)
	assert.Contains(t, got, `"finishReason":"STOP"`)
	assert.Contains(t, got, `"usageMetadata"`)
	assert.NotContains(t, got, `chat.completion.chunk`)
}
