package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

	regular := invokeRankingsUsageRequest(t, requestURL, 7, common.RoleCommonUser, "team")
	require.Equal(t, http.StatusOK, regular.Code)
	assert.Contains(t, regular.Body.String(), "user_usage")
	assert.NotContains(t, regular.Body.String(), "alice")
	assert.NotContains(t, regular.Body.String(), "bob")
	assert.Contains(t, regular.Body.String(), "a***e")
	assert.NotContains(t, regular.Body.String(), "b***b")
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
	// Group scoping: the regular viewer belongs to "team" only, so bob's
	// rows (default/secret) stay invisible while alice's team row is shown
	// with the scoping-adjusted share.
	require.Len(t, regularPayload.Data.UserUsage.Users, 1)
	require.Len(t, adminPayload.Data.UserUsage.Users, 2)
	assert.Equal(t, "a***e", regularPayload.Data.UserUsage.Users[0].Username)
	assert.Len(t, regularPayload.Data.UserUsage.Users[0].Groups, 1)
	assert.Equal(t, "team", regularPayload.Data.UserUsage.Users[0].Groups[0].UseGroup)
	require.Len(t, adminPayload.Data.UserUsage.Users[0].Groups, 2)
	// The section header totals stay global for both viewers.
	assert.Equal(t, adminPayload.Data.UserUsage.TotalTokens, regularPayload.Data.UserUsage.TotalTokens)
	assert.Equal(t, adminPayload.Data.UserUsage.TotalQuota, regularPayload.Data.UserUsage.TotalQuota)
	assert.Equal(t, adminPayload.Data.UserUsage.TotalUSD, regularPayload.Data.UserUsage.TotalUSD)
	// Per-model usage follows the same viewer visibility as the main
	// leaderboard: the session admin sees the admin-only model name while
	// regular viewers get it folded into "Others".
	adminModelsByUsername := make(map[string][]service.RankingUserModel, len(adminPayload.Data.UserUsage.Users))
	for _, user := range adminPayload.Data.UserUsage.Users {
		adminModelsByUsername[user.Username] = user.Models
	}
	require.Len(t, adminModelsByUsername["alice"], 2)
	assert.Equal(t, "ranking-visible-model", adminModelsByUsername["alice"][0].ModelName)
	assert.Equal(t, "admin-secret-model", adminModelsByUsername["alice"][1].ModelName)
	regularModelsByUsername := make(map[string][]service.RankingUserModel, len(regularPayload.Data.UserUsage.Users))
	for _, user := range regularPayload.Data.UserUsage.Users {
		regularModelsByUsername[user.Username] = user.Models
	}
	// Group scoping drops alice's secret-group row, so the redacted viewer
	// only sees the model usage recorded against the "team" group.
	require.Len(t, regularModelsByUsername["a***e"], 1)
	assert.Equal(t, "ranking-visible-model", regularModelsByUsername["a***e"][0].ModelName)
	assert.Equal(t, adminModelsByUsername["alice"][0].TotalQuota, regularModelsByUsername["a***e"][0].TotalQuota)
	assert.Equal(t, adminModelsByUsername["alice"][0].TotalTokens, regularModelsByUsername["a***e"][0].TotalTokens)

	// A scoped regular viewer keeps the visible row's absolute numbers: the
	// team row matches the admin's top group row exactly.
	adminUser := adminPayload.Data.UserUsage.Users[0]
	regularUser := regularPayload.Data.UserUsage.Users[0]
	assert.Equal(t, adminUser.Groups[0].TotalTokens, regularUser.TotalTokens)
	assert.Equal(t, adminUser.Groups[0].TotalQuota, regularUser.TotalQuota)
	assert.Equal(t, "team", adminUser.Groups[0].UseGroup)
	assert.Equal(t, adminUser.Groups[0].TotalQuota, regularUser.Groups[0].TotalQuota)

	patAdminRecorder := httptest.NewRecorder()
	patAdminContext, _ := gin.CreateTestContext(patAdminRecorder)
	patAdminContext.Request = httptest.NewRequest(http.MethodGet, requestURL, nil)
	patAdminContext.Set("id", 9)
	patAdminContext.Set("role", common.RoleAdminUser)
	patAdminContext.Set("use_access_token", true)
	// PAT-authenticated admins fail the dashboard-session check, so they get
	// the masked, group-scoped view like any regular user. Their memberships
	// come from the user cache in production; emulate "team" here.
	common.SetContextKey(patAdminContext, constant.ContextKeyUserGroups, []string{"team"})
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

func invokeRankingsUsageRequest(t *testing.T, requestURL string, userID int, role int, groups ...string) *httptest.ResponseRecorder {
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
		common.SetContextKey(ctx, constant.ContextKeyUserGroups, groups)
	}
	GetRankings(ctx)
	return recorder
}
