package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeBufferedStreamHandlerReturnsMessagesJSON(t *testing.T) {
	body := strings.Join([]string{
		"event: message_start",
		"data: {",
		`data: "type":"message_start",`,
		`data: "message":{"id":"msg_upstream","type":"message","role":"assistant","model":"provider-claude-model","content":[],"usage":{"input_tokens":8,"cache_read_input_tokens":2,"output_tokens":0}}`,
		"data: }",
		"",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"inspect"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signature"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}`,
		`data: {"type":"content_block_stop","index":2}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
		`data: {"type":"message_stop"}`,
	}, "\n")
	c, recorder, resp, info := newBufferedClaudeTestContext(body, types.RelayFormatClaude)
	info.OriginModelName = "claude-public"

	usage, apiError := ClaudeBufferedStreamHandler(c, info, resp)

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	assert.Equal(t, 8, usage.PromptTokens)
	assert.Equal(t, 4, usage.CompletionTokens)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.NotContains(t, recorder.Body.String(), "data:")
	var response dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "msg_upstream", response.Id)
	assert.Equal(t, "claude-public", response.Model)
	assert.Equal(t, "tool_use", response.StopReason)
	require.Len(t, response.Content, 3)
	require.NotNil(t, response.Content[0].Thinking)
	assert.Equal(t, "inspect", *response.Content[0].Thinking)
	assert.Equal(t, "signature", response.Content[0].Signature)
	assert.Equal(t, "hello", response.Content[1].GetText())
	assert.Equal(t, "tool_use", response.Content[2].Type)
	assert.Equal(t, "toolu_1", response.Content[2].Id)
	toolInput, ok := response.Content[2].Input.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "x", toolInput["q"])
}

func TestClaudeBufferedStreamHandlerReturnsResponsesJSON(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_upstream","type":"message","role":"assistant","model":"provider-claude-model","content":[],"usage":{"input_tokens":8,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
		`data: {"type":"message_stop"}`,
	}, "\n")
	c, recorder, resp, info := newBufferedClaudeTestContext(body, types.RelayFormatOpenAIResponses)
	info.OriginModelName = "gpt-public"

	usage, apiError := ClaudeBufferedStreamHandler(c, info, resp)

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	assert.Equal(t, 11, usage.TotalTokens)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "gpt-public", response.Model)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "hello", response.Output[0].Content[0].Text)
}

func TestClaudeBufferedStreamHandlerMarksFirstProtocolError(t *testing.T) {
	body := `data: {"type":"error","error":{"type":"not_found_error","message":"unsupported endpoint /v1/messages"}}`
	c, recorder, resp, info := newBufferedClaudeTestContext(body, types.RelayFormatClaude)

	usage, apiError := ClaudeBufferedStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiError)
	assert.True(t, apiError.HasProtocolUnsupportedEvidence())
	assert.Empty(t, recorder.Body.String())
}

func TestClaudeBufferedStreamHandlerRejectsTruncatedStream(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_upstream","model":"provider-claude-model","content":[],"usage":{"input_tokens":2,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
	}, "\n")
	c, recorder, resp, info := newBufferedClaudeTestContext(body, types.RelayFormatClaude)

	usage, apiError := ClaudeBufferedStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiError)
	assert.Contains(t, apiError.Error(), "without message_stop")
	assert.Empty(t, recorder.Body.String())
}

func TestClaudeBufferedStreamHandlerRejectsMessageStopWithoutStopReason(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_upstream","model":"provider-claude-model","content":[],"usage":{"input_tokens":2,"output_tokens":0}}}`,
		`data: {"type":"message_delta","usage":{"output_tokens":1}}`,
		`data: {"type":"message_stop"}`,
	}, "\n")
	c, recorder, resp, info := newBufferedClaudeTestContext(body, types.RelayFormatClaude)

	usage, apiError := ClaudeBufferedStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiError)
	assert.Contains(t, apiError.Error(), "terminal stop_reason")
	assert.Empty(t, recorder.Body.String())
}

func newBufferedClaudeTestContext(body string, relayFormat types.RelayFormat) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		RelayFormat: relayFormat,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-claude-model",
			IsModelMapped:     true,
		},
	}
	return c, recorder, resp, info
}
