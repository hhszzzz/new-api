package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoUsesOriginModel(t *testing.T) {
	const upstreamModel = "veo-provider-internal-001"
	task := &model.Task{
		TaskID: "task_public-id",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			OriginModelName:   "requested-video-model",
			UpstreamModelName: upstreamModel,
		},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: taskcommon.EncodeLocalTaskID("projects/demo/locations/us/models/" + upstreamModel + "/operations/123"),
		},
	}

	responseBody, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.NotContains(t, string(responseBody), upstreamModel)

	var response map[string]any
	require.NoError(t, common.Unmarshal(responseBody, &response))
	assert.Equal(t, "requested-video-model", response["model"])
}
