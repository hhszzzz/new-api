package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTaskRoutingControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()

	gin.SetMode(gin.TestMode)
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Task{}))

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		require.NoError(t, sqlDB.Close())
	})

	return db
}

func TestTasksToDtoRestrictsModelRoutingByRole(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{
			Input:             "prompt",
			OriginModelName:   "requested-model",
			UpstreamModelName: "upstream-model",
		},
	}

	userItems := tasksToDto([]*model.Task{task}, false, relay.TaskDtoAudiencePublic)
	require.Len(t, userItems, 1)
	userProperties, ok := userItems[0].Properties.(model.Properties)
	require.True(t, ok)
	assert.Equal(t, "requested-model", userProperties.OriginModelName)
	assert.Empty(t, userProperties.UpstreamModelName)

	adminItems := tasksToDto([]*model.Task{task}, false, relay.TaskDtoAudienceAdmin)
	require.Len(t, adminItems, 1)
	adminProperties, ok := adminItems[0].Properties.(model.Properties)
	require.True(t, ok)
	assert.Equal(t, "upstream-model", adminProperties.UpstreamModelName)

	unknownAudienceItems := tasksToDto([]*model.Task{task}, false, relay.TaskDtoAudience(255))
	require.Len(t, unknownAudienceItems, 1)
	unknownAudienceProperties, ok := unknownAudienceItems[0].Properties.(model.Properties)
	require.True(t, ok)
	assert.Empty(t, unknownAudienceProperties.UpstreamModelName)
	assert.Equal(t, "upstream-model", task.Properties.UpstreamModelName)
}

func TestTaskDashboardRoutesApplyModelRoutingAudience(t *testing.T) {
	db := setupTaskRoutingControllerTestDB(t)
	task := &model.Task{
		TaskID:     "task_dashboard_route",
		UserId:     42,
		Platform:   constant.TaskPlatformSuno,
		SubmitTime: 100,
		Properties: model.Properties{
			Input:             "prompt",
			OriginModelName:   "requested-model",
			UpstreamModelName: "upstream-model",
		},
	}
	require.NoError(t, db.Create(task).Error)

	tests := []struct {
		name         string
		path         string
		role         int
		handler      gin.HandlerFunc
		wantUpstream bool
	}{
		{
			name:    "common user self route",
			path:    "/api/task/self",
			role:    common.RoleCommonUser,
			handler: GetUserTask,
		},
		{
			name:         "administrator self route",
			path:         "/api/task/self",
			role:         common.RoleAdminUser,
			handler:      GetUserTask,
			wantUpstream: true,
		},
		{
			name:         "root self route",
			path:         "/api/task/self",
			role:         common.RoleRootUser,
			handler:      GetUserTask,
			wantUpstream: true,
		},
		{
			name:         "administrator management route",
			path:         "/api/task/",
			role:         common.RoleAdminUser,
			handler:      GetAllTask,
			wantUpstream: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.GET(test.path, func(c *gin.Context) {
				c.Set("id", task.UserId)
				c.Set("role", test.role)
				test.handler(c)
			})

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					Items []struct {
						Properties map[string]any `json:"properties"`
					} `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			require.Len(t, response.Data.Items, 1)
			properties := response.Data.Items[0].Properties
			assert.Equal(t, "requested-model", properties["origin_model_name"])
			if test.wantUpstream {
				assert.Equal(t, "upstream-model", properties["upstream_model_name"])
			} else {
				assert.NotContains(t, properties, "upstream_model_name")
			}
		})
	}
}

func TestTaskTokenFetchRoutesOmitModelRouting(t *testing.T) {
	db := setupTaskRoutingControllerTestDB(t)
	task := &model.Task{
		TaskID:     "task_token_route",
		UserId:     77,
		Platform:   constant.TaskPlatformSuno,
		SubmitTime: 100,
		Properties: model.Properties{
			Input:             "prompt",
			OriginModelName:   "requested-model",
			UpstreamModelName: "upstream-model",
		},
	}
	require.NoError(t, db.Create(task).Error)

	batchBody, err := common.Marshal(map[string]any{"ids": []string{task.TaskID}})
	require.NoError(t, err)
	tests := []struct {
		name      string
		method    string
		route     string
		path      string
		relayMode int
		body      []byte
		batch     bool
	}{
		{
			name:      "suno batch fetch",
			method:    http.MethodPost,
			route:     "/suno/fetch",
			path:      "/suno/fetch",
			relayMode: relayconstant.RelayModeSunoFetch,
			body:      batchBody,
			batch:     true,
		},
		{
			name:      "suno fetch by id",
			method:    http.MethodGet,
			route:     "/suno/fetch/:id",
			path:      "/suno/fetch/" + task.TaskID,
			relayMode: relayconstant.RelayModeSunoFetchByID,
		},
		{
			name:      "legacy video fetch",
			method:    http.MethodGet,
			route:     "/v1/video/generations/:task_id",
			path:      "/v1/video/generations/" + task.TaskID,
			relayMode: relayconstant.RelayModeVideoFetchByID,
		},
		{
			name:      "kling text video fetch",
			method:    http.MethodGet,
			route:     "/kling/v1/videos/text2video/:task_id",
			path:      "/kling/v1/videos/text2video/" + task.TaskID,
			relayMode: relayconstant.RelayModeVideoFetchByID,
		},
		{
			name:      "kling image video fetch",
			method:    http.MethodGet,
			route:     "/kling/v1/videos/image2video/:task_id",
			path:      "/kling/v1/videos/image2video/" + task.TaskID,
			relayMode: relayconstant.RelayModeVideoFetchByID,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.Handle(test.method, test.route, func(c *gin.Context) {
				c.Set("id", task.UserId)
				// A relay API token never becomes a management interface, even if
				// another middleware happens to attach an elevated owner role.
				c.Set("role", common.RoleRootUser)
				c.Set("relay_mode", test.relayMode)
				RelayTaskFetch(c)
			})

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, bytes.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			data := response["data"]
			if test.batch {
				items, ok := data.([]any)
				require.True(t, ok)
				require.Len(t, items, 1)
				data = items[0]
			}
			item, ok := data.(map[string]any)
			require.True(t, ok)
			properties, ok := item["properties"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "requested-model", properties["origin_model_name"])
			assert.NotContains(t, properties, "upstream_model_name")
		})
	}
}
