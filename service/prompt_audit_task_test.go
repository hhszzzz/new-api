package service

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTaskPromptAuditSnapshotJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(`{
			"model":"video-model",
			"prompt":"main prompt",
			"prompt_en":"translated prompt",
			"negative_prompt":"negative prompt",
			"input":{"description":"nested description","image":"data:image/png;base64,secret","input":"https://example.test/image.png"},
		"metadata":{"prompt":"metadata must not be scanned"},
		"tools":[{"description":"tool definition must not be scanned"}],
		"encrypted_content":"ciphertext"
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	snapshot, modelName, err := ExtractTaskPromptAuditSnapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "video-model", modelName)
	text := snapshot.Text()
	assert.Equal(t, "main prompt\ntranslated prompt\nnegative prompt\nnested description", text)
	assert.Contains(t, text, "main prompt")
	assert.Contains(t, text, "translated prompt")
	assert.Contains(t, text, "negative prompt")
	assert.Contains(t, text, "nested description")
	assert.NotContains(t, text, "metadata must not be scanned")
	assert.NotContains(t, text, "tool definition must not be scanned")
	assert.NotContains(t, text, "data:image")
	assert.NotContains(t, text, "example.test")
	assert.NotContains(t, text, "ciphertext")
}

func TestExtractTaskPromptAuditSnapshotJSONWithoutContentTypePreservesWireOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(`{
			"model":"task-model",
			"prompt":"first prompt",
			"instruction":"second instruction",
			"metadata":{"prompt":"ignored metadata prompt"}
		}`))

	snapshot, modelName, err := ExtractTaskPromptAuditSnapshot(c)
	require.NoError(t, err)
	assert.Equal(t, "task-model", modelName)
	assert.Equal(t, "first prompt\nsecond instruction", snapshot.Text())
}

func TestExtractTaskPromptAuditSnapshotFormPreservesFieldOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader("model=video-model&prompt=first&metadata=hidden&negative_prompt=second"))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	snapshot, modelName, err := ExtractTaskPromptAuditSnapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "video-model", modelName)
	assert.Equal(t, "first\nsecond", snapshot.Text())
}

func TestExtractTaskPromptAuditSnapshotMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "sora-2"))
	require.NoError(t, writer.WriteField("prompt", "multipart prompt"))
	require.NoError(t, writer.WriteField("gpt_description_prompt", "song description"))
	require.NoError(t, writer.WriteField("metadata", `{"prompt":"hidden"}`))
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/video/generations", &body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())

	snapshot, modelName, err := ExtractTaskPromptAuditSnapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "sora-2", modelName)
	assert.Contains(t, snapshot.Text(), "multipart prompt")
	assert.Contains(t, snapshot.Text(), "song description")
	assert.NotContains(t, snapshot.Text(), "hidden")
}

func TestExtractTaskPromptAuditSnapshotRejectsInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(`{"prompt":`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	_, _, err := ExtractTaskPromptAuditSnapshot(ctx)
	require.Error(t, err)
}
