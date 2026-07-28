package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayReturnsBadRequestWhenSensitiveWordsAreDetected(t *testing.T) {
	originalWords := setting.SensitiveWordsToString()
	originalEnabled := setting.CheckSensitiveEnabled
	originalPromptEnabled := setting.CheckSensitiveOnPromptEnabled
	t.Cleanup(func() {
		setting.SensitiveWordsFromString(originalWords)
		setting.SetCheckSensitiveEnabled(originalEnabled)
		setting.SetCheckSensitiveOnPromptEnabled(originalPromptEnabled)
	})
	setting.SensitiveWordsFromString("blocked")
	setting.SetCheckSensitiveEnabled(true)
	setting.SetCheckSensitiveOnPromptEnabled(true)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"blocked content"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	Relay(ctx, types.RelayFormatEmbedding)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var response struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, string(types.ErrorCodeSensitiveWordsDetected), response.Error.Code)
	assert.Contains(t, response.Error.Message, "sensitive words detected")
}

func TestRelayChecksOnlyOpenAIUserTextForSensitiveWords(t *testing.T) {
	originalWords := setting.SensitiveWordsToString()
	originalEnabled := setting.CheckSensitiveEnabled
	originalPromptEnabled := setting.CheckSensitiveOnPromptEnabled
	t.Cleanup(func() {
		setting.SensitiveWordsFromString(originalWords)
		setting.SetCheckSensitiveEnabled(originalEnabled)
		setting.SetCheckSensitiveOnPromptEnabled(originalPromptEnabled)
	})
	setting.SensitiveWordsFromString("hello")
	setting.SetCheckSensitiveEnabled(true)
	setting.SetCheckSensitiveOnPromptEnabled(true)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-4o",
		"messages":[
			{"role":"system","content":"hello from hidden instructions"},
			{"role":"assistant","content":"hello from chat history"},
			{"role":"user","content":"请介绍一下人工智能"}
		],
		"tools":[{"type":"function","function":{"name":"hello_tool","description":"hello from tool description"}}],
		"metadata":{"note":"hello from metadata"}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	Relay(ctx, types.RelayFormatOpenAI)

	assert.NotContains(t, recorder.Body.String(), string(types.ErrorCodeSensitiveWordsDetected))
}
