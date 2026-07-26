package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
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
	assert.NotContains(t, recorder.Body.String(), "recommendations")
}
