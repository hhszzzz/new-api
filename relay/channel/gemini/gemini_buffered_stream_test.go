package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiBufferedStreamHandlerReturnsResponsesJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	c := newGeminiResponsesBufferedContext(t, recorder)
	info := newGeminiResponsesRelayInfo(false)
	prepareGeminiResponsesProtocolState(t, c, info)
	stop := "STOP"
	body := geminiBufferedSSE(t,
		dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
				{Thought: true, Text: "inspect"},
				{Text: "answer"},
			}},
		}}},
		dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{{FinishReason: &stop}},
			UsageMetadata: dto.GeminiUsageMetadata{
				PromptTokenCount:     4,
				CandidatesTokenCount: 2,
				ThoughtsTokenCount:   1,
				TotalTokenCount:      7,
			},
			HasUsageMetadata: true,
		},
	)

	usage, apiError := GeminiBufferedStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	assert.Equal(t, 7, usage.TotalTokens)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.NotContains(t, recorder.Body.String(), "data:")
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Output, 2)
	assert.Equal(t, "reasoning", response.Output[0].Type)
	assert.Equal(t, "inspect", response.Output[0].Summary[0].Text)
	assert.Equal(t, "message", response.Output[1].Type)
	assert.Equal(t, "answer", response.Output[1].Content[0].Text)
}

func TestGeminiBufferedStreamHandlerPreservesToolIDAndSuppressesCumulativeDuplicate(t *testing.T) {
	recorder := httptest.NewRecorder()
	c := newGeminiResponsesBufferedContext(t, recorder)
	info := newGeminiResponsesRelayInfo(false)
	prepareGeminiResponsesProtocolState(t, c, info)
	stop := "STOP"
	toolPart := dto.GeminiPart{FunctionCall: &dto.FunctionCall{
		ID:           "provider_call_1",
		FunctionName: "lookup",
		Arguments:    map[string]any{"q": "x"},
	}}
	body := geminiBufferedSSE(t,
		dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{toolPart}},
		}}},
		dto.GeminiChatResponse{
			Candidates: []dto.GeminiChatCandidate{{
				FinishReason: &stop,
				Content:      dto.GeminiChatContent{Parts: []dto.GeminiPart{toolPart}},
			}},
			UsageMetadata:    dto.GeminiUsageMetadata{PromptTokenCount: 4, CandidatesTokenCount: 2, TotalTokenCount: 6},
			HasUsageMetadata: true,
		},
	)

	usage, apiError := GeminiBufferedStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})

	require.Nil(t, apiError)
	require.NotNil(t, usage)
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Output, 1)
	assert.Equal(t, "function_call", response.Output[0].Type)
	assert.Equal(t, "provider_call_1", response.Output[0].CallId)
	assert.Equal(t, "lookup", response.Output[0].Name)
	assert.JSONEq(t, `{"q":"x"}`, dto.ResponsesArgumentsString(response.Output[0].Arguments))
}

func TestGeminiBufferedStreamHandlerMapsSafetyToResponsesRefusal(t *testing.T) {
	recorder := httptest.NewRecorder()
	c := newGeminiResponsesBufferedContext(t, recorder)
	info := newGeminiResponsesRelayInfo(false)
	prepareGeminiResponsesProtocolState(t, c, info)
	safety := "SAFETY"
	body := geminiBufferedSSE(t, dto.GeminiChatResponse{
		PromptFeedback:   &dto.GeminiChatPromptFeedback{BlockReason: &safety},
		UsageMetadata:    dto.GeminiUsageMetadata{PromptTokenCount: 4, TotalTokenCount: 4},
		HasUsageMetadata: true,
	})

	_, apiError := GeminiBufferedStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})

	require.Nil(t, apiError)
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Output, 1)
	require.Len(t, response.Output[0].Content, 1)
	assert.Equal(t, "refusal", response.Output[0].Content[0].Type)
	assert.Equal(t, "Request blocked by Gemini safety filters: SAFETY", response.Output[0].Content[0].Refusal)
	assert.Equal(t, "gemini_block_reason=SAFETY", c.GetString(string(constant.ContextKeyAdminRejectReason)))
}

func TestGeminiBufferedStreamHandlerRejectsTruncatedStream(t *testing.T) {
	recorder := httptest.NewRecorder()
	c := newGeminiResponsesBufferedContext(t, recorder)
	info := newGeminiResponsesRelayInfo(false)
	body := geminiBufferedSSE(t, dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "partial"}}}}},
	})

	usage, apiError := GeminiBufferedStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})

	require.Nil(t, usage)
	require.NotNil(t, apiError)
	assert.Contains(t, apiError.Error(), "terminal finish reason")
	assert.Empty(t, recorder.Body.String())
}

func newGeminiResponsesBufferedContext(t *testing.T, recorder *httptest.ResponseRecorder) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "gemini-buffered-test")
	return c
}

func geminiBufferedSSE(t *testing.T, responses ...dto.GeminiChatResponse) string {
	t.Helper()
	lines := make([]string, 0, len(responses)+2)
	for _, response := range responses {
		data, err := common.Marshal(response)
		require.NoError(t, err)
		lines = append(lines, "data: "+string(data))
	}
	lines = append(lines, "data: [DONE]", "")
	return strings.Join(lines, "\n")
}
