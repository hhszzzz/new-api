package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type quotaDataResponse struct {
	Success bool              `json:"success"`
	Data    []model.QuotaData `json:"data"`
}

func setupQuotaVisibilityControllerTest(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.QuotaData{}, &model.ScopedQuotaData{}))
	require.NoError(t, db.Create(&model.QuotaData{
		UserID: 1, Username: "alice", ModelName: "legacy-private-upstream", ModelScope: model.QuotaModelScopeLegacy,
		CreatedAt: 1000, UseGroup: "default", Count: 1, Quota: 20, TokenUsed: 20,
	}).Error)
	require.NoError(t, db.Create(&model.ScopedQuotaData{
		UserID: 1, Username: "alice", ModelName: "requested-model", ModelScope: model.QuotaModelScopeRequested,
		CreatedAt: 1000, UseGroup: "default", Count: 1, Quota: 10, TokenUsed: 10,
	}).Error)
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)
}

func TestGetUserQuotaDatesRedactsLegacyUnknownModelButPreservesUsage(t *testing.T) {
	setupQuotaVisibilityControllerTest(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleCommonUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self?start_timestamp=900&end_timestamp=1100", nil)

	GetUserQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "legacy-private-upstream")
	var payload quotaDataResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data, 2)
	assert.Equal(t, "", payload.Data[0].ModelName)
	assert.Equal(t, 20, payload.Data[0].Quota)
	assert.Equal(t, "requested-model", payload.Data[1].ModelName)
	assert.Equal(t, 10, payload.Data[1].Quota)
}

func TestGetUserQuotaDatesKeepsRawModelForAdminSelfView(t *testing.T) {
	setupQuotaVisibilityControllerTest(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self?start_timestamp=900&end_timestamp=1100", nil)

	GetUserQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "legacy-private-upstream")
}

func TestGetUserFlowQuotaDatesDoesNotExposeLegacyUnknownModel(t *testing.T) {
	setupQuotaVisibilityControllerTest(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleCommonUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=900&end_timestamp=1100", nil)

	GetUserFlowQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "legacy-private-upstream")
	assert.Contains(t, recorder.Body.String(), "requested-model")
}
