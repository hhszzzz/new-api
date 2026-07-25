package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTaskPersistsModelRouteSnapshot(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UserId:               42,
		UsingGroup:           "default",
		OriginModelName:      "gpt-5.4",
		UserModelRouteId:     7,
		RouteTargetModelName: "gpt-5.5",
		RouteExecutionGroup:  "internal",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         12,
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gpt-5.5",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	task := InitTask(constant.TaskPlatform("sora"), info)

	require.NotNil(t, task)
	assert.Equal(t, "task_public", task.TaskID)
	assert.Equal(t, "gpt-5.4", task.Properties.OriginModelName)
	assert.Equal(t, "gpt-5.5", task.Properties.UpstreamModelName)
	assert.Equal(t, 1, task.PrivateData.ModelRouteSnapshotVersion)
	assert.Equal(t, 7, task.PrivateData.UserModelRouteId)
	assert.Equal(t, "gpt-5.5", task.PrivateData.RouteTargetModelName)
	assert.Equal(t, "internal", task.PrivateData.RouteExecutionGroup)
}

func TestInitTaskVersionsSnapshotWhenNoRouteMatched(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UserId:          42,
		OriginModelName: "sora-2",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 12},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	task := InitTask(constant.TaskPlatform("sora"), info)

	assert.Equal(t, 1, task.PrivateData.ModelRouteSnapshotVersion)
	assert.Zero(t, task.PrivateData.UserModelRouteId)
	assert.Empty(t, task.PrivateData.RouteTargetModelName)
	assert.Empty(t, task.PrivateData.RouteExecutionGroup)
}
