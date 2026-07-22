package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResolveOriginTaskRejectsLegacyUpstreamModelFallback(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Task{}))

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
