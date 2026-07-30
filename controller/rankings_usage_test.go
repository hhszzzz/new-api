package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRankingsUserUsageMatchesAdminTotalsAndMasksPrivateUsernames(t *testing.T) {
	service.InvalidateRankingsCache()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.QuotaData{}, &model.ScopedQuotaData{}))
	require.NoError(t, db.Create(&model.Channel{Id: 981, Name: "rankings-usage", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "ranking-visible-model", ChannelId: 981, Enabled: true,
	}).Error)
	now := time.Now().Unix()
	start := now - 3600
	end := now
	require.NoError(t, db.Create(&[]model.ScopedQuotaData{
		{UserID: 1, Username: "alice", ModelName: "ranking-visible-model", ModelScope: model.QuotaModelScopeRequested, CreatedAt: now, UseGroup: "team", TokenUsed: 100, Quota: 500000, Count: 1},
		{UserID: 1, Username: "alice", ModelName: "admin-secret-model", ModelScope: model.QuotaModelScopeAdminOnly, CreatedAt: now, UseGroup: "secret", TokenUsed: 0, Quota: 200000, Count: 1},
		{UserID: 2, Username: "bob", ModelName: "ranking-visible-model", ModelScope: model.QuotaModelScopeRequested, CreatedAt: now, UseGroup: "default", TokenUsed: 50, Quota: 300000, Count: 1},
	}).Error)
	model.InvalidatePricingCache()

	requestURL := fmt.Sprintf("/api/rankings?period=custom&start_timestamp=%d&end_timestamp=%d", start, end)
	anonymous := invokeRankingsUsageRequest(t, requestURL, 0, common.RoleGuestUser)
	require.Equal(t, http.StatusOK, anonymous.Code)
	assert.NotContains(t, anonymous.Body.String(), "user_usage")
	assert.NotContains(t, anonymous.Body.String(), "alice")

	regular := invokeRankingsUsageRequest(t, requestURL, 7, common.RoleCommonUser)
	require.Equal(t, http.StatusOK, regular.Code)
	assert.Contains(t, regular.Body.String(), "user_usage")
	assert.NotContains(t, regular.Body.String(), "alice")
	assert.NotContains(t, regular.Body.String(), "bob")
	assert.Contains(t, regular.Body.String(), "a***e")
	assert.Contains(t, regular.Body.String(), "b***b")
	assert.NotContains(t, regular.Body.String(), "Other users")
	assert.NotContains(t, regular.Body.String(), "Unknown user")
	assert.NotContains(t, regular.Body.String(), "admin-secret-model")
	assert.NotContains(t, regular.Body.String(), "user_id")

	admin := invokeRankingsUsageRequest(t, requestURL, 9, common.RoleAdminUser)
	require.Equal(t, http.StatusOK, admin.Code)
	assert.Contains(t, admin.Body.String(), "alice")
	assert.Contains(t, admin.Body.String(), "bob")
	assert.NotContains(t, admin.Body.String(), "a***e")
	var regularPayload struct {
		Data service.RankingsResponse `json:"data"`
	}
	var adminPayload struct {
		Data service.RankingsResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(regular.Body.Bytes(), &regularPayload))
	require.NoError(t, common.Unmarshal(admin.Body.Bytes(), &adminPayload))
	require.NotNil(t, regularPayload.Data.UserUsage)
	require.NotNil(t, adminPayload.Data.UserUsage)
	require.Len(t, regularPayload.Data.UserUsage.Users, 2)
	require.Len(t, adminPayload.Data.UserUsage.Users, 2)
	assert.Equal(t, adminPayload.Data.UserUsage.TotalTokens, regularPayload.Data.UserUsage.TotalTokens)
	assert.Equal(t, adminPayload.Data.UserUsage.TotalQuota, regularPayload.Data.UserUsage.TotalQuota)
	assert.Equal(t, adminPayload.Data.UserUsage.TotalUSD, regularPayload.Data.UserUsage.TotalUSD)
	for index := range adminPayload.Data.UserUsage.Users {
		regularUser := regularPayload.Data.UserUsage.Users[index]
		adminUser := adminPayload.Data.UserUsage.Users[index]
		regularUser.Username = adminUser.Username
		assert.Equal(t, adminUser, regularUser)
	}

	patAdminRecorder := httptest.NewRecorder()
	patAdminContext, _ := gin.CreateTestContext(patAdminRecorder)
	patAdminContext.Request = httptest.NewRequest(http.MethodGet, requestURL, nil)
	patAdminContext.Set("id", 9)
	patAdminContext.Set("role", common.RoleAdminUser)
	patAdminContext.Set("use_access_token", true)
	GetRankings(patAdminContext)
	require.Equal(t, http.StatusOK, patAdminRecorder.Code)
	assert.NotContains(t, patAdminRecorder.Body.String(), "\"username\":\"alice\"")
	assert.Contains(t, patAdminRecorder.Body.String(), "a***e")
	assert.NotContains(t, patAdminRecorder.Body.String(), "admin-secret-model")

	// The same resolved range has separate cache entries for each viewer level.
	assert.NotEqual(t, regular.Body.String(), admin.Body.String())
}

func TestRankingsCustomPeriodRejectsMissingAndReversedEndpoints(t *testing.T) {
	service.InvalidateRankingsCache()
	setupModelListControllerTestDB(t)

	missing := invokeRankingsUsageRequest(t, "/api/rankings?period=custom", 0, common.RoleGuestUser)
	assert.Equal(t, http.StatusBadRequest, missing.Code)

	reversed := invokeRankingsUsageRequest(t, "/api/rankings?period=custom&start_timestamp=200&end_timestamp=100", 0, common.RoleGuestUser)
	assert.Equal(t, http.StatusBadRequest, reversed.Code)
}

func TestRankingsCustomPeriodAcceptsExactly366Days(t *testing.T) {
	service.InvalidateRankingsCache()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.QuotaData{}, &model.ScopedQuotaData{}))

	end := time.Now().Unix()
	start := end - int64(366*24*time.Hour/time.Second) + 1
	boundaryURL := fmt.Sprintf("/api/rankings?period=custom&start_timestamp=%d&end_timestamp=%d", start, end)
	boundary := invokeRankingsUsageRequest(t, boundaryURL, 0, common.RoleGuestUser)
	assert.Equal(t, http.StatusOK, boundary.Code)

	tooLongURL := fmt.Sprintf("/api/rankings?period=custom&start_timestamp=%d&end_timestamp=%d", start-1, end)
	tooLong := invokeRankingsUsageRequest(t, tooLongURL, 0, common.RoleGuestUser)
	assert.Equal(t, http.StatusBadRequest, tooLong.Code)
}

func invokeRankingsUsageRequest(t *testing.T, requestURL string, userID int, role int) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, requestURL, nil)
	ctx.Set("id", userID)
	ctx.Set("role", role)
	if userID > 0 {
		ctx.Set("session_id", fmt.Sprintf("ranking-session-%d", userID))
		ctx.Set("auth_version", int64(1))
		ctx.Set("session_version", int64(1))
	}
	GetRankings(ctx)
	return recorder
}
