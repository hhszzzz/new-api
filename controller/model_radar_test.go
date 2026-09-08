package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModelRadarReturnsServiceUnavailableBeforeFirstSnapshot(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ModelRadarSnapshot{}))
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/model-radar", nil)

	GetModelRadar(context)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"code":"data_unavailable"`)
}

func TestGetModelRadarReturnsStoredSnapshot(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ModelRadarSnapshot{}))
	payload, err := common.Marshal(service.ModelRadarData{
		SchemaVersion: 1,
		Source: service.ModelRadarSource{
			Name: "Codex Radar", URL: "https://codexradar.com", Attribution: "数据来自 Codex 雷达 codexradar.com",
		},
		ModelCount: 1, ConfigurationCount: 1,
		Configurations:    []service.ModelRadarConfiguration{},
		History:           []service.ModelRadarHistoryFrame{},
		DegradationAlerts: []service.ModelRadarDegradationAlert{},
	})
	require.NoError(t, err)
	require.NoError(t, model.SaveModelRadarSnapshot(t.Context(), &model.ModelRadarSnapshot{
		SchemaVersion: 1, Payload: payload, SourceUpdatedAt: 2_000_000_000,
		AlertsUpdatedAt: 2_000_000_000, FetchedAt: 2_000_000_000,
	}))
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/model-radar", nil)

	GetModelRadar(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Contains(t, recorder.Body.String(), `"model_count":1`)
	assert.Contains(t, recorder.Body.String(), `"settings":`)
	assert.Contains(t, recorder.Body.String(), `"default_vendor":"openai"`)
	assert.NotContains(t, recorder.Body.String(), "recommendations")
}

func TestModelRadarManagementDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			require.NoError(t, db.AutoMigrate(&model.ModelRadarSnapshot{}, &model.SystemTask{}, &model.SystemTaskLock{}))
			t.Setenv("MODEL_RADAR_SYNC_ENABLED", "false")
			var management struct {
				Success bool `json:"success"`
				Data    struct {
					Settings setting.ModelRadarSettings `json:"settings"`
					Snapshot *struct {
						ModelCount         int `json:"model_count"`
						ConfigurationCount int `json:"configuration_count"`
						Models             []struct {
							Model   string   `json:"model"`
							Efforts []string `json:"efforts"`
						} `json:"models"`
					} `json:"snapshot"`
					CurrentTask *model.SystemTaskResponse `json:"current_task"`
					Sync        struct {
						Enabled bool `json:"enabled"`
					} `json:"sync"`
				} `json:"data"`
			}
			modelManagementRequest(t, GetModelRadarManagement, "GET", "/api/model-radar/manage", nil, &management)
			require.True(t, management.Success)
			assert.Nil(t, management.Data.Snapshot)
			assert.Nil(t, management.Data.CurrentTask)
			assert.False(t, management.Data.Sync.Enabled)
			assert.Equal(t, "openai", management.Data.Settings.DefaultVendor)

			settingsJSON := `{"default_vendor":"anthropic","show_degradation_alerts":false,"models":{"k3":{"hidden":true}}}`
			require.NoError(t, model.UpdateOption("ModelRadarSettings", settingsJSON))
			var option model.Option
			require.NoError(t, db.Where(&model.Option{Key: "ModelRadarSettings"}).First(&option).Error)
			assert.Equal(t, settingsJSON, option.Value)
			require.Error(t, model.UpdateOption("ModelRadarSettings", `{"default_vendor":"INVALID"}`))
			assert.Equal(t, "anthropic", setting.GetModelRadarSettings().DefaultVendor)

			payload, err := common.Marshal(service.ModelRadarData{Configurations: []service.ModelRadarConfiguration{
				{Model: "k3", Effort: "high"}, {Model: "gpt-a", Effort: "low"}, {Model: "gpt-a", Effort: "high"},
			}})
			require.NoError(t, err)
			require.NoError(t, model.SaveModelRadarSnapshot(t.Context(), &model.ModelRadarSnapshot{SchemaVersion: 1, Payload: payload, FetchedAt: common.GetTimestamp()}))
			data, err := service.GetModelRadar(t.Context())
			require.NoError(t, err)
			require.NotNil(t, data.Settings)
			assert.Equal(t, "anthropic", data.Settings.DefaultVendor)
			assert.True(t, data.Settings.Models["k3"].Hidden)
			snapshot, err := model.GetModelRadarSnapshot(t.Context())
			require.NoError(t, err)
			assert.NotContains(t, string(snapshot.Payload), `"settings"`, "display settings must not be persisted in the source snapshot")

			var task struct {
				Data model.SystemTaskResponse `json:"data"`
			}
			response := modelManagementRequest(t, TriggerModelRadarSync, "POST", "/api/model-radar/sync", nil, &task)
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, model.SystemTaskTypeModelRadarSync, task.Data.Type)
			assert.Equal(t, model.SystemTaskStatusPending, task.Data.Status)
			response = modelManagementRequest(t, TriggerModelRadarSync, "POST", "/api/model-radar/sync", nil, nil)
			assert.Equal(t, http.StatusConflict, response.Code)
			assert.Contains(t, response.Body.String(), task.Data.TaskID)
			var count int64
			require.NoError(t, db.Model(&model.SystemTask{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)

			modelManagementRequest(t, GetModelRadarManagement, "GET", "/api/model-radar/manage", nil, &management)
			require.True(t, management.Success)
			require.NotNil(t, management.Data.Snapshot)
			assert.Equal(t, 2, management.Data.Snapshot.ModelCount)
			assert.Equal(t, 3, management.Data.Snapshot.ConfigurationCount)
			assert.Equal(t, "gpt-a", management.Data.Snapshot.Models[0].Model)
			assert.Equal(t, []string{"high", "low"}, management.Data.Snapshot.Models[0].Efforts)
			require.NotNil(t, management.Data.CurrentTask)
			assert.Equal(t, task.Data.TaskID, management.Data.CurrentTask.TaskID)
		})
	}
}

func TestModelRadarManagementRequiresRootWhenPublicRadarIsDisabled(t *testing.T) {
	user, token := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ModelRadarSnapshot{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{"HeaderNavModules": `{"modelRadar":{"enabled":false}}`}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
	router := gin.New()
	router.GET("/api/model-radar", middleware.HeaderNavModuleAuth(middleware.HeaderNavModuleModelRadar), GetModelRadar)
	router.GET("/api/model-radar/manage", middleware.RootAuth(), GetModelRadarManagement)
	router.POST("/api/model-radar/sync", middleware.RootAuth(), TriggerModelRadarSync)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/model-radar/manage"}, {"POST", "/api/model-radar/sync"}} {
		assert.Equal(t, http.StatusUnauthorized, auditRequest(router, endpoint.method, endpoint.path, "").Code)
		assert.Equal(t, http.StatusForbidden, auditRequest(router, endpoint.method, endpoint.path, token).Code)
	}
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	assert.Equal(t, http.StatusForbidden, auditRequest(router, "GET", "/api/model-radar", token).Code)
	assert.Equal(t, http.StatusOK, auditRequest(router, "GET", "/api/model-radar/manage", token).Code)
	assert.Equal(t, http.StatusOK, auditRequest(router, "POST", "/api/model-radar/sync", token).Code)
}
