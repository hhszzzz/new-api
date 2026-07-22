package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rankingsVisibilityResponse struct {
	Success bool                     `json:"success"`
	Data    service.RankingsResponse `json:"data"`
}

func TestGetRankingsHidesLegacyAndNonVisibleModelNamesWithoutDroppingUsage(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.QuotaData{}, &model.ScopedQuotaData{}))
	require.NoError(t, db.Create(&model.Channel{Id: 971, Name: "ranking-visibility", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "public-ranking-model", ChannelId: 971, Enabled: true,
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.QuotaData{
		ModelName: "public-ranking-model", ModelScope: model.QuotaModelScopeLegacy,
		CreatedAt: now, TokenUsed: 100, Count: 1,
	}).Error)
	require.NoError(t, db.Create(&[]model.ScopedQuotaData{
		{
			ModelName: "public-ranking-model", ModelScope: model.QuotaModelScopeRequested,
			CreatedAt: now, TokenUsed: 10, Count: 1,
		},
		{
			ModelName: "private-ranking-upstream", ModelScope: model.QuotaModelScopeRequested,
			CreatedAt: now, TokenUsed: 20, Count: 1,
		},
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/rankings?period=today", nil)

	GetRankings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "private-ranking-upstream")
	var payload rankingsVisibilityResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Models, 2)
	assert.Equal(t, "Others", payload.Data.Models[0].ModelName)
	assert.Equal(t, int64(120), payload.Data.Models[0].TotalTokens)
	assert.Equal(t, "public-ranking-model", payload.Data.Models[1].ModelName)
	assert.Equal(t, int64(10), payload.Data.Models[1].TotalTokens)
}
