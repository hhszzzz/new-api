package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func routedAudioInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName:      "gpt-5.4",
		UserModelRouteId:     7,
		RouteTargetModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider/gpt-5.5",
		},
	}
}

func TestOpenaiTTSStreamHidesUserModelRouteOnlyInProtocolFields(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"audio.delta","model":"provider/gpt-5.5","error":{"message":"gpt-5.5 unavailable"},"delta":"provider/gpt-5.5","metadata":{"model":"provider/gpt-5.5"}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := routedAudioInfo()
	info.IsStream = true

	usage := OpenaiTTSHandler(context, response, info)

	require.NotNil(t, usage)
	output := recorder.Body.String()
	assert.Contains(t, output, `"model":"gpt-5.4"`)
	assert.Contains(t, output, `"message":"gpt-5.4 unavailable"`)
	assert.Contains(t, output, `"delta":"provider/gpt-5.5"`)
	assert.Contains(t, output, `"metadata":{"model":"provider/gpt-5.5"}`)
}

func TestOpenaiTTSNonStreamLeavesBinaryAudioUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	binaryAudio := []byte{0x00, 0xff, 0x10, 0x7f, 0x01}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/pcm"}},
		Body:       io.NopCloser(bytes.NewReader(binaryAudio)),
	}
	info := routedAudioInfo()
	info.Request = &dto.AudioRequest{ResponseFormat: "pcm"}

	usage := OpenaiTTSHandler(context, response, info)

	require.NotNil(t, usage)
	assert.Equal(t, binaryAudio, recorder.Body.Bytes())
}

func TestOpenaiSTTHidesUserModelRouteWithoutChangingTranscript(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("JSON response", func(t *testing.T) {
		body := `{"model":"provider/gpt-5.5","text":"provider/gpt-5.5 and gpt-5.5 are transcript content","metadata":{"model":"provider/gpt-5.5"},"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
		response := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}

		apiErr, usage := OpenaiSTTHandler(context, response, routedAudioInfo(), "json")

		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		assert.Equal(t, 5, usage.TotalTokens)
		output := recorder.Body.String()
		assert.Equal(t, "gpt-5.4", gjson.Get(output, "model").String())
		assert.Equal(t, "provider/gpt-5.5 and gpt-5.5 are transcript content", gjson.Get(output, "text").String())
		assert.Equal(t, "provider/gpt-5.5", gjson.Get(output, "metadata.model").String())
	})

	t.Run("plain text response", func(t *testing.T) {
		body := "provider/gpt-5.5 and gpt-5.5 are transcript content"
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
		response := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}

		apiErr, usage := OpenaiSTTHandler(context, response, routedAudioInfo(), "text")

		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		assert.Equal(t, body, recorder.Body.String())
	})
}
