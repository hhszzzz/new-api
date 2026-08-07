package helper

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidAudioRequestCapturesMultipartPromptForAuditOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "whisper-1"))
	require.NoError(t, writer.WriteField("prompt", "proper nouns for this recording"))
	file, err := writer.CreateFormFile("file", "audio.wav")
	require.NoError(t, err)
	_, err = file.Write([]byte("audio bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	request, err := GetAndValidAudioRequest(c, relayconstant.RelayModeAudioTranscription)
	require.NoError(t, err)
	assert.Equal(t, "proper nouns for this recording", request.AuditPrompt)

	encoded, err := common.Marshal(request)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "proper nouns")

	form, err := common.ParseMultipartFormReusable(c)
	require.NoError(t, err)
	defer form.RemoveAll()
	assert.Equal(t, "proper nouns for this recording", form.Value["prompt"][0])
}
