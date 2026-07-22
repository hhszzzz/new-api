package suno

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoResponseRedactsUpstreamModelFromSuccessMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"code":"success",
			"message":"model provider-internal-music-model is ready",
			"data":"upstream-task-id"
		}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "requested-music-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-internal-music-model",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID: "task_public-id",
		},
	}

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(context, response, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-task-id", taskID)
	assert.Nil(t, taskData)
	assert.NotContains(t, recorder.Body.String(), "provider-internal-music-model")

	var publicResponse map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &publicResponse))
	assert.Equal(t, "model requested-music-model is ready", publicResponse["message"])
	assert.Equal(t, "task_public-id", publicResponse["data"])
}
