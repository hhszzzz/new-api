package gemini

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiResponsesHandlerReturnsOpenAIResponsesJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "gemini-responses-test")

	info := newGeminiResponsesRelayInfo(false)
	publicID := prepareGeminiResponsesProtocolState(t, c, info)
	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "hello"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     2,
			CandidatesTokenCount: 3,
			TotalTokenCount:      5,
		},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, newAPIError := GeminiResponsesHandler(c, info, &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	assert.Equal(t, 2, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)

	got := recorder.Body.String()
	assert.Contains(t, got, `"object":"response"`)
	assert.Contains(t, got, `"status":"completed"`)
	assert.Contains(t, got, `"type":"output_text"`)
	assert.Contains(t, got, `"text":"hello"`)
	assert.Contains(t, got, `"input_tokens":2`)
	assert.Contains(t, got, `"output_tokens":3`)
	assert.NotContains(t, got, `"choices"`)
	assert.NotContains(t, got, `"candidates"`)
	assert.Contains(t, got, `"id":"`+publicID+`"`)
	requireGeminiResponsesProtocolBinding(t, c, info, publicID)
}

func TestGeminiResponsesHandlerClosesBodyOnReadError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "gemini-responses-read-error-test")

	body := &failingReadCloser{}
	usage, newAPIError := GeminiResponsesHandler(c, newGeminiResponsesRelayInfo(false), &http.Response{Body: body})

	require.Nil(t, usage)
	require.NotNil(t, newAPIError)
	assert.True(t, body.closed)
}

func TestGeminiResponsesHandlerMapsBlockedPromptToIncompleteRefusal(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "gemini-responses-blocked-test")

	info := newGeminiResponsesRelayInfo(false)
	prepareGeminiResponsesProtocolState(t, c, info)
	blockReason := "SAFETY"
	payload := dto.GeminiChatResponse{
		PromptFeedback: &dto.GeminiChatPromptFeedback{BlockReason: &blockReason},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount: 4,
			TotalTokenCount:  4,
		},
		HasUsageMetadata: true,
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, apiError := GeminiResponsesHandler(c, info, &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	assert.Equal(t, 4, usage.PromptTokens)
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.JSONEq(t, `"incomplete"`, string(response.Status))
	require.NotNil(t, response.IncompleteDetails)
	assert.Equal(t, "content_filter", response.IncompleteDetails.Reason)
	require.Len(t, response.Output, 1)
	require.Len(t, response.Output[0].Content, 1)
	assert.Equal(t, "refusal", response.Output[0].Content[0].Type)
	assert.Equal(t, "Request blocked by Gemini safety filters: SAFETY", response.Output[0].Content[0].Refusal)
	assert.Equal(t, "gemini_block_reason=SAFETY", c.GetString(string(constant.ContextKeyAdminRejectReason)))
}

func TestGeminiResponsesStreamHandlerReturnsOpenAIResponsesSSE(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "gemini-responses-stream-test")

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	info := newGeminiResponsesRelayInfo(true)
	publicID := prepareGeminiResponsesProtocolState(t, c, info)
	first := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "hello"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     2,
			CandidatesTokenCount: 3,
			TotalTokenCount:      5,
		},
	}
	stop := "STOP"
	final := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				FinishReason: &stop,
				Content: dto.GeminiChatContent{
					Role:  "model",
					Parts: []dto.GeminiPart{{Text: ""}},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     2,
			CandidatesTokenCount: 3,
			TotalTokenCount:      5,
		},
	}
	firstData, err := common.Marshal(first)
	require.NoError(t, err)
	finalData, err := common.Marshal(final)
	require.NoError(t, err)
	streamBody := strings.Join([]string{
		"data: " + string(firstData),
		"",
		"data: " + string(finalData),
		"",
		"data: [DONE]",
		"",
	}, "\n")

	usage, newAPIError := GeminiResponsesStreamHandler(c, info, &http.Response{
		Body: io.NopCloser(strings.NewReader(streamBody)),
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	assert.Equal(t, 5, usage.TotalTokens)

	got := recorder.Body.String()
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Contains(t, got, `event: response.created`)
	assert.Contains(t, got, `event: response.output_text.delta`)
	assert.Contains(t, got, `"delta":"hello"`)
	assert.Contains(t, got, `event: response.completed`)
	assert.Contains(t, got, `"input_tokens":2`)
	assert.Contains(t, got, `"output_tokens":3`)
	assert.NotContains(t, got, `"choices"`)
	assert.NotContains(t, got, `"candidates"`)
	assert.Contains(t, got, `"id":"`+publicID+`"`)
	requireOrderedGeminiResponsesSubstrings(t, got,
		`event: response.created`,
		`event: response.output_item.added`,
		`event: response.output_text.delta`,
		`event: response.output_text.done`,
		`event: response.completed`,
	)
	requireGeminiResponsesProtocolBinding(t, c, info, publicID)
}

func TestGeminiResponsesStreamHandlerMapsBlockedPromptToIncompleteRefusal(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "gemini-responses-stream-blocked-test")

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	info := newGeminiResponsesRelayInfo(true)
	prepareGeminiResponsesProtocolState(t, c, info)
	blockReason := "PROHIBITED_CONTENT"
	payload := dto.GeminiChatResponse{
		PromptFeedback: &dto.GeminiChatPromptFeedback{BlockReason: &blockReason},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount: 3,
			TotalTokenCount:  3,
		},
		HasUsageMetadata: true,
	}
	data, err := common.Marshal(payload)
	require.NoError(t, err)
	streamBody := strings.Join([]string{"data: " + string(data), "", "data: [DONE]", ""}, "\n")

	usage, apiError := GeminiResponsesStreamHandler(c, info, &http.Response{
		Body: io.NopCloser(strings.NewReader(streamBody)),
	})

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	got := recorder.Body.String()
	assert.Contains(t, got, `event: response.refusal.delta`)
	assert.Contains(t, got, `Request blocked by Gemini safety filters: PROHIBITED_CONTENT`)
	assert.Contains(t, got, `event: response.incomplete`)
	assert.Contains(t, got, `"reason":"content_filter"`)
	assert.NotContains(t, got, `event: response.completed`)
}

func TestGeminiTruncatedStreamDoesNotSynthesizeResponsesCompletion(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "gemini-responses-truncated-test")

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	info := newGeminiResponsesRelayInfo(true)
	first := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role:  "model",
				Parts: []dto.GeminiPart{{Text: "partial"}},
			},
		}},
	}
	firstData, err := common.Marshal(first)
	require.NoError(t, err)
	streamBody := strings.Join([]string{
		"data: " + string(firstData),
		"",
		"data: [DONE]",
		"",
	}, "\n")

	usage, apiErr := GeminiResponsesStreamHandler(c, info, &http.Response{
		Body: io.NopCloser(strings.NewReader(streamBody)),
	})

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "terminal finish reason")
	assert.Contains(t, recorder.Body.String(), `"delta":"partial"`)
	assert.NotContains(t, recorder.Body.String(), `event: response.completed`)
}

func newGeminiResponsesRelayInfo(isStream bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:        isStream,
		RelayMode:       relayconstant.RelayModeResponses,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RequestURLPath:  "/v1/responses",
		DisablePing:     true,
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
	}
}

func prepareGeminiResponsesProtocolState(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo) string {
	t.Helper()
	common.SetContextKey(c, constant.ContextKeyUserId, 701)
	common.SetContextKey(c, constant.ContextKeyTokenId, 702)
	info.ChannelMeta.ChannelId = 703
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		Status:           channelcompat.StatusConvertible,
		StateEnabled:     true,
	}
	request := &dto.OpenAIResponsesRequest{
		Model: info.OriginModelName,
		Input: []byte(`[{"role":"user","content":"hello"}]`),
	}
	require.NoError(t, protocolstate.PrepareResponsesRequest(c, info, plan, request))
	publicID := protocolstate.PublicResponseID(c, "")
	require.NotEmpty(t, publicID)
	return publicID
}

func requireGeminiResponsesProtocolBinding(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo, publicID string) {
	t.Helper()
	require.NoError(t, protocolstate.Commit(c))
	next, _ := gin.CreateTestContext(httptest.NewRecorder())
	next.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	next.Set(common.RequestIdKey, publicID+"-next")
	common.SetContextKey(next, constant.ContextKeyUserId, 701)
	common.SetContextKey(next, constant.ContextKeyTokenId, 702)
	body, err := common.Marshal(map[string]any{
		"model":                info.OriginModelName,
		"previous_response_id": publicID,
	})
	require.NoError(t, err)
	binding, err := protocolstate.ResolveSelectionBinding(next, "/v1/responses", info.OriginModelName, body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, info.ChannelId, binding.ChannelID)
	assert.Equal(t, channelcompat.ProtocolChat, binding.UpstreamProtocol)
}

type failingReadCloser struct {
	closed bool
}

func (r *failingReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (r *failingReadCloser) Close() error {
	r.closed = true
	return nil
}

func requireOrderedGeminiResponsesSubstrings(t *testing.T, s string, parts ...string) {
	t.Helper()
	offset := 0
	for _, part := range parts {
		idx := strings.Index(s[offset:], part)
		require.NotEqualf(t, -1, idx, "missing %q after byte offset %d", part, offset)
		offset += idx + len(part)
	}
}
