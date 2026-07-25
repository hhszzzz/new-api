package relay

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskModel2DtoSanitizesNestedModelRoutingForPublicAudience(t *testing.T) {
	const (
		originModel      = "requested-video-model"
		routeTargetModel = "routed-video-model"
		upstreamModel    = "provider-internal-video-model"
	)

	rawData, err := common.Marshal(map[string]any{
		"model": upstreamModel,
		"result": map[string]any{
			"model_name":     routeTargetModel,
			"fallback_model": upstreamModel,
			"clips": []any{
				map[string]any{
					"major_model_version": upstreamModel,
					"upstreamModelId":     upstreamModel,
				},
			},
		},
		"camera": map[string]any{
			"model": "Canon EOS R5",
		},
		"operation":              "projects/demo/models/" + upstreamModel + "/operations/123",
		"route/" + upstreamModel: "renamed",
		"large_integer":          uint64(18446744073709551615),
		"safe":                   "preserved",
	})
	require.NoError(t, err)

	task := &model.Task{
		ChannelId:  73,
		FailReason: "model " + upstreamModel + " is unavailable",
		PrivateData: model.TaskPrivateData{
			ResultURL:            "https://results.example/models/" + upstreamModel + "/" + routeTargetModel + "/video.mp4",
			RouteTargetModelName: routeTargetModel,
		},
		Properties: model.Properties{
			Input:             "requested through " + upstreamModel,
			OriginModelName:   originModel,
			UpstreamModelName: upstreamModel,
		},
		Data: rawData,
	}

	publicDto := TaskModel2Dto(task, TaskDtoAudiencePublic)
	publicJSON, err := common.Marshal(publicDto)
	require.NoError(t, err)
	assert.NotContains(t, string(publicJSON), upstreamModel)
	assert.NotContains(t, string(publicJSON), routeTargetModel)
	assert.Contains(t, string(publicJSON), originModel)
	assert.Contains(t, publicDto.FailReason, originModel)
	assert.Contains(t, publicDto.ResultURL, originModel)

	var publicData map[string]any
	require.NoError(t, common.Unmarshal(publicDto.Data, &publicData))
	assert.Equal(t, originModel, publicData["model"])
	assert.Equal(t, "preserved", publicData["safe"])
	assert.Equal(t, "projects/demo/models/"+originModel+"/operations/123", publicData["operation"])
	camera, ok := publicData["camera"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Canon EOS R5", camera["model"])
	assert.NotContains(t, publicData, "route/"+upstreamModel)
	assert.Equal(t, "renamed", publicData["route/"+originModel])
	assert.Contains(t, string(publicDto.Data), `"large_integer":18446744073709551615`)

	adminDto := TaskModel2Dto(task, TaskDtoAudienceAdmin)
	adminJSON, err := common.Marshal(adminDto)
	require.NoError(t, err)
	assert.Contains(t, string(adminJSON), upstreamModel)
	assert.Contains(t, adminDto.FailReason, upstreamModel)
	assert.Contains(t, adminDto.ResultURL, upstreamModel)
	assert.JSONEq(t, string(rawData), string(adminDto.Data))
	assert.JSONEq(t, string(rawData), string(task.Data))
}

func TestSanitizeTaskErrorForPublicKeepsInternalErrorAndHidesPublicRouting(t *testing.T) {
	const (
		originModel      = "requested-video-model"
		routeTargetModel = "routed-video-model"
		upstreamModel    = "provider-internal-video-model"
	)
	internalError := errors.New("model " + upstreamModel + " is unavailable")
	taskErr := &dto.TaskError{
		Code:       routeTargetModel + "/not_found",
		Message:    internalError.Error() + " via " + routeTargetModel,
		Data:       map[string]any{"model": upstreamModel, "route_model": routeTargetModel},
		StatusCode: http.StatusBadRequest,
		Error:      internalError,
	}
	info := &relaycommon.RelayInfo{
		OriginModelName:      originModel,
		RouteTargetModelName: routeTargetModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: upstreamModel,
		},
	}

	publicError := sanitizeTaskErrorForPublic(taskErr, info)
	publicJSON, err := common.Marshal(publicError)
	require.NoError(t, err)
	assert.NotContains(t, string(publicJSON), upstreamModel)
	assert.NotContains(t, string(publicJSON), routeTargetModel)
	assert.Contains(t, string(publicJSON), originModel)
	assert.Contains(t, publicError.Error.Error(), upstreamModel)
}

func TestSanitizeTaskErrorForPublicMasksSensitiveUpstreamURLAfterModelRedaction(t *testing.T) {
	const (
		originModel   = "requested-video-model"
		upstreamModel = "provider-internal-video-model"
	)
	rawMessage := "POST https://internal.example/v1/tasks?api_key=SECRET failed for Provider-Internal-Video-Model"
	taskErr := &dto.TaskError{
		Code:    "fail_to_fetch_task",
		Message: rawMessage,
		Data: map[string]any{
			"detail": []any{
				"GET https://metadata.internal/v1/models/Provider-Internal-Video-Model?token=DATA_SECRET",
			},
		},
		StatusCode: http.StatusBadGateway,
		Error:      errors.New(rawMessage),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: originModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: upstreamModel,
		},
	}

	publicError := sanitizeTaskErrorForPublic(taskErr, info)

	assert.NotContains(t, publicError.Message, "SECRET")
	assert.NotContains(t, publicError.Message, "internal.example")
	assert.NotContains(t, publicError.Message, "v1/tasks")
	assert.NotContains(t, publicError.Message, "Provider-Internal-Video-Model")
	assert.Contains(t, publicError.Message, originModel)
	assert.Contains(t, publicError.Message, "https://***.example/***")
	assert.Contains(t, publicError.Error.Error(), "SECRET")
	publicJSON, err := common.Marshal(publicError)
	require.NoError(t, err)
	assert.NotContains(t, string(publicJSON), "DATA_SECRET")
	assert.NotContains(t, string(publicJSON), "metadata.internal")
	assert.NotContains(t, string(publicJSON), "Provider-Internal-Video-Model")
}

func TestTaskModel2DtoRemovesRoutingFieldsWhenOriginModelIsUnavailable(t *testing.T) {
	rawData, err := common.Marshal(map[string]any{
		"model": "provider-internal-video-model",
		"nested": map[string]any{
			"major_model_version": "provider-internal-video-model",
			"status":              "complete",
		},
	})
	require.NoError(t, err)

	publicDto := TaskModel2Dto(&model.Task{Data: rawData}, TaskDtoAudiencePublic)
	var publicData map[string]any
	require.NoError(t, common.Unmarshal(publicDto.Data, &publicData))
	assert.NotContains(t, publicData, "model")
	nested, ok := publicData["nested"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, nested, "major_model_version")
	assert.Equal(t, "complete", nested["status"])
}

func TestTaskModel2DtoFailsClosedForInvalidPublicTaskData(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{UpstreamModelName: "provider-internal-video-model"},
		Data:       []byte(`{"model":"provider-internal-video-model"`),
	}

	publicDto := TaskModel2Dto(task, TaskDtoAudiencePublic)
	assert.Nil(t, publicDto.Data)

	adminDto := TaskModel2Dto(task, TaskDtoAudienceAdmin)
	assert.Equal(t, task.Data, adminDto.Data)
}
