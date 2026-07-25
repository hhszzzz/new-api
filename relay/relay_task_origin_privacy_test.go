package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOriginTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	common.MemoryCacheEnabled = true
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}, &model.Ability{}))
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		if previousMemoryCacheEnabled && previousDB != nil {
			model.InitChannelCache()
		}
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestResolveOriginTaskRejectsLegacyUpstreamModelFallback(t *testing.T) {
	db := setupOriginTaskTestDB(t)

	const upstreamModel = "provider-internal-video-model"
	legacyTask := &model.Task{
		TaskID: "task_legacy",
		UserId: 42,
		Properties: model.Properties{
			UpstreamModelName: upstreamModel,
		},
	}
	require.NoError(t, db.Create(legacyTask).Error)

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task_legacy/remix", nil)
	context.Params = gin.Params{{Key: "video_id", Value: legacyTask.TaskID}}
	info := &relaycommon.RelayInfo{
		UserId:        legacyTask.UserId,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	taskErr := ResolveOriginTask(context, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "task_origin_model_unavailable", taskErr.Code)
	assert.NotContains(t, taskErr.Message, upstreamModel)
	assert.Empty(t, info.OriginModelName)
}

func TestResolveOriginTaskRestoresFrozenUserModelRoute(t *testing.T) {
	db := setupOriginTaskTestDB(t)
	channel := &model.Channel{
		Id:     12,
		Name:   "snapshot-channel",
		Key:    "snapshot-key",
		Status: common.ChannelStatusEnabled,
		Group:  "internal",
		Models: "provider-video-model",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "internal",
		Model:     "provider-video-model",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)
	model.InitChannelCache()
	originTask := &model.Task{
		TaskID:    "task_routed",
		UserId:    42,
		ChannelId: channel.Id,
		Properties: model.Properties{
			OriginModelName: "public-video-model",
		},
		PrivateData: model.TaskPrivateData{
			ModelRouteSnapshotVersion: 1,
			UserModelRouteId:          7,
			RouteTargetModelName:      "provider-video-model",
			RouteExecutionGroup:       "internal",
		},
	}
	require.NoError(t, db.Create(originTask).Error)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task_routed/remix", nil)
	context.Params = gin.Params{{Key: "video_id", Value: originTask.TaskID}}
	common.SetContextKey(context, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(context, constant.ContextKeyUserModelRouteId, 99)
	common.SetContextKey(context, constant.ContextKeyUserModelRouteTarget, "new-policy-model")
	info := &relaycommon.RelayInfo{
		UserId:               originTask.UserId,
		UserModelRouteId:     99,
		RouteTargetModelName: "new-policy-model",
		TaskRelayInfo:        &relaycommon.TaskRelayInfo{},
	}

	taskErr := ResolveOriginTask(context, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "public-video-model", info.OriginModelName)
	assert.Equal(t, 1, info.OriginRouteSnapshotVersion)
	assert.Equal(t, 7, info.UserModelRouteId)
	assert.Equal(t, "provider-video-model", info.RouteTargetModelName)
	assert.Equal(t, "internal", info.RouteExecutionGroup)
	channelIds, ok := common.GetContextKeyType[[]int](context, constant.ContextKeyUserModelRouteChannel)
	require.True(t, ok)
	assert.Equal(t, []int{channel.Id}, channelIds)
	lockedChannel, ok := info.LockedChannel.(*model.Channel)
	require.True(t, ok)
	assert.Equal(t, channel.Id, lockedChannel.Id)
	require.NoError(t, middleware.ValidateSelectedRouteChannel(context, lockedChannel, context.Request.URL.Path))
}

func TestResolveOriginTaskKeepsFrozenNoRouteDecision(t *testing.T) {
	db := setupOriginTaskTestDB(t)
	channel := &model.Channel{Id: 13, Name: "plain-channel", Key: "plain-key", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(channel).Error)
	originTask := &model.Task{
		TaskID:    "task_plain",
		UserId:    42,
		ChannelId: channel.Id,
		Properties: model.Properties{
			OriginModelName: "public-video-model",
		},
		PrivateData: model.TaskPrivateData{ModelRouteSnapshotVersion: 1},
	}
	require.NoError(t, db.Create(originTask).Error)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task_plain/remix", nil)
	context.Params = gin.Params{{Key: "video_id", Value: originTask.TaskID}}
	common.SetContextKey(context, constant.ContextKeyUserModelRouteId, 99)
	common.SetContextKey(context, constant.ContextKeyUserModelRouteTarget, "new-policy-model")
	common.SetContextKey(context, constant.ContextKeyAutoGroup, "new-policy-group")
	info := &relaycommon.RelayInfo{
		UserId:               originTask.UserId,
		UserModelRouteId:     99,
		RouteTargetModelName: "new-policy-model",
		RouteExecutionGroup:  "new-policy-group",
		TaskRelayInfo:        &relaycommon.TaskRelayInfo{},
	}

	taskErr := ResolveOriginTask(context, info)

	require.Nil(t, taskErr)
	assert.Equal(t, 1, info.OriginRouteSnapshotVersion)
	assert.Zero(t, info.UserModelRouteId)
	assert.Empty(t, info.RouteTargetModelName)
	assert.Empty(t, info.RouteExecutionGroup)
	_, hasRoute := common.GetContextKey(context, constant.ContextKeyUserModelRouteId)
	assert.False(t, hasRoute)
	_, hasAutoGroup := common.GetContextKey(context, constant.ContextKeyAutoGroup)
	assert.False(t, hasAutoGroup)
}
