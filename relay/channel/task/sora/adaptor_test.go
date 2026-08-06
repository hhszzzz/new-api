package sora

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoResponseUsesOriginModelInPublicSubmitResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"upstream-task-id",
			"object":"video",
			"model":"provider-internal-video-model",
			"status":"queued",
			"progress":0,
			"created_at":123,
			"error":{
				"message":"model provider-internal-video-model is unavailable",
				"code":"provider-internal-video-model/error"
			}
		}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "requested-video-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-internal-video-model",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID: "task_public-id",
		},
	}

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(context, response, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-task-id", taskID)
	assert.Contains(t, string(taskData), "provider-internal-video-model")

	var publicResponse map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &publicResponse))
	assert.Equal(t, "task_public-id", publicResponse["id"])
	assert.Equal(t, "task_public-id", publicResponse["task_id"])
	assert.Equal(t, "requested-video-model", publicResponse["model"])
	assert.NotContains(t, recorder.Body.String(), "provider-internal-video-model")
}

func TestConvertToOpenAIVideoHidesUpstreamModelAndTaskID(t *testing.T) {
	taskData := []byte(`{
		"id":"upstream-task-id",
		"model":"provider-internal-video-model",
		"status":"completed",
		"vendor_extension":"preserved",
		"large_integer":18446744073709551615,
		"nested":{
			"model_name":"provider-internal-video-model",
			"error":"model provider-internal-video-model is unavailable"
		}
	}`)
	task := &model.Task{
		TaskID: "task_public-id",
		Properties: model.Properties{
			OriginModelName:   "requested-video-model",
			UpstreamModelName: "provider-internal-video-model",
		},
		Data: taskData,
	}

	responseBody, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.NotContains(t, string(responseBody), "provider-internal-video-model")
	assert.NotContains(t, string(responseBody), "upstream-task-id")

	var response map[string]any
	require.NoError(t, common.Unmarshal(responseBody, &response))
	assert.Equal(t, "task_public-id", response["id"])
	assert.Equal(t, "requested-video-model", response["model"])
	assert.Equal(t, "preserved", response["vendor_extension"])
	assert.Contains(t, string(responseBody), `"large_integer":18446744073709551615`)
}

func TestSoraBuildRequestBodyReturnsReplayablePassThroughBody(t *testing.T) {
	payload := []byte("opaque-sora-request-body")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/octet-stream")
	defer common.CleanupBodyStorage(c)

	info := &relaycommon.RelayInfo{}
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	replayable, ok := body.(common.ReplayableBody)
	require.True(t, ok)

	sent, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, payload, sent)
	assert.EqualValues(t, len(payload), replayable.Size())

	replayBody, err := replayable.NewReader()
	require.NoError(t, err)
	replay, err := io.ReadAll(replayBody)
	require.NoError(t, err)
	require.NoError(t, replayBody.Close())
	assert.Equal(t, payload, replay)
}
