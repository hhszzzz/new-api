package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type subscriptionAuthorizationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type subscriptionPlanListResponse struct {
	Success bool                  `json:"success"`
	Data    []SubscriptionPlanDTO `json:"data"`
}

func setupSubscriptionAuthorizationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSubscription{}))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled = previousRedisEnabled
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func callSubscriptionAdminHandler(t *testing.T, handler gin.HandlerFunc, method string, body string, params gin.Params) subscriptionAuthorizationResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = params
	ctx.Set("role", common.RoleAdminUser)

	handler(ctx)

	var response subscriptionAuthorizationResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestSubscriptionAdminHandlersRejectSameRoleTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupSubscriptionAuthorizationTestDB(t)
	target := &model.User{
		Username: "peer-admin",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(target).Error)
	subscription := &model.UserSubscription{
		UserId:    target.Id,
		PlanId:    1,
		Status:    "active",
		StartTime: 1,
		EndTime:   common.GetTimestamp() + 3600,
	}
	require.NoError(t, db.Create(subscription).Error)

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		method  string
		body    string
		params  gin.Params
	}{
		{name: "bind", handler: AdminBindSubscription, method: http.MethodPost, body: fmt.Sprintf(`{"user_id":%d,"plan_id":1}`, target.Id)},
		{name: "list", handler: AdminListUserSubscriptions, method: http.MethodGet, params: gin.Params{{Key: "id", Value: fmt.Sprint(target.Id)}}},
		{name: "create", handler: AdminCreateUserSubscription, method: http.MethodPost, body: `{"plan_id":1}`, params: gin.Params{{Key: "id", Value: fmt.Sprint(target.Id)}}},
		{name: "reset", handler: AdminResetUserSubscriptionsByPlan, method: http.MethodPost, body: `{"plan_id":1}`, params: gin.Params{{Key: "id", Value: fmt.Sprint(target.Id)}}},
		{name: "invalidate", handler: AdminInvalidateUserSubscription, method: http.MethodPost, params: gin.Params{{Key: "id", Value: fmt.Sprint(subscription.Id)}}},
		{name: "delete", handler: AdminDeleteUserSubscription, method: http.MethodDelete, params: gin.Params{{Key: "id", Value: fmt.Sprint(subscription.Id)}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := callSubscriptionAdminHandler(t, test.handler, test.method, test.body, test.params)

			assert.False(t, response.Success)
			assert.Contains(t, response.Message, "无权管理")
		})
	}

	var persisted model.UserSubscription
	require.NoError(t, db.First(&persisted, subscription.Id).Error)
	assert.Equal(t, "active", persisted.Status)
}

func TestAdminPlanManagementDoesNotRequirePaymentCompliance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupSubscriptionAuthorizationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))

	setPaymentComplianceForTest(t, false)

	response := callSubscriptionAdminHandler(
		t,
		AdminCreateSubscriptionPlan,
		http.MethodPost,
		`{"plan":{"title":"Manually assigned plan","enabled":true,"duration_unit":"month","duration_value":1}}`,
		nil,
	)

	assert.True(t, response.Success, response.Message)
	var count int64
	require.NoError(t, db.Model(&model.SubscriptionPlan{}).Where("title = ?", "Manually assigned plan").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestUserSubscriptionPlanListHidesInternalOnlyPlans(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupSubscriptionAuthorizationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))
	confirmPaymentComplianceForTest(t)

	publicPurchase := true
	adminOnly := false
	plans := []model.SubscriptionPlan{
		{Title: "Public plan", Enabled: true, Purchasable: &publicPurchase},
		{Title: "Internal plan", Enabled: true, Purchasable: &adminOnly},
		{Title: "Disabled plan", Enabled: true, Purchasable: &publicPurchase},
	}
	require.NoError(t, db.Create(&plans).Error)
	require.NoError(t, db.Model(&model.SubscriptionPlan{}).Where("id = ?", plans[2].Id).Update("enabled", false).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	GetSubscriptionPlans(ctx)

	var response subscriptionPlanListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data, 1)
	assert.Equal(t, "Public plan", response.Data[0].Plan.Title)
	assert.True(t, response.Data[0].Plan.IsPurchasable())
}

func TestAdminAssignmentAllowsEmptySourceNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupSubscriptionAuthorizationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))
	plan := &model.SubscriptionPlan{
		Title:         "note-optional",
		Enabled:       true,
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
	}
	require.NoError(t, db.Create(plan).Error)

	target := &model.User{
		Username: "subscription-note-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(target).Error)

	response := callSubscriptionAdminHandler(
		t,
		AdminCreateUserSubscription,
		http.MethodPost,
		fmt.Sprintf(`{"plan_id":%d,"source_note":"  "}`, plan.Id),
		gin.Params{{Key: "id", Value: fmt.Sprint(target.Id)}},
	)

	assert.True(t, response.Success)
}

func TestAdminBatchSubscriptionAssignInvalidateAndDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupSubscriptionAuthorizationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))
	plan := &model.SubscriptionPlan{
		Title:         "batch-plan",
		Enabled:       true,
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
	}
	require.NoError(t, db.Create(plan).Error)

	ids := make([]int, 2)
	for i := range ids {
		username := fmt.Sprintf("batch-subscription-user-%d", i)
		user := &model.User{
			Username: username,
			Role:     common.RoleCommonUser,
			Status:   common.UserStatusEnabled,
			Group:    "default",
			AffCode:  username,
		}
		require.NoError(t, db.Create(user).Error)
		ids[i] = user.Id
	}

	countSubscriptions := func(where string, args ...any) int64 {
		t.Helper()
		var count int64
		require.NoError(t, db.Model(&model.UserSubscription{}).Where(where, args...).Count(&count).Error)
		return count
	}

	assignBody := fmt.Sprintf(`{"user_ids":[%d,%d],"plan_id":%d}`, ids[0], ids[1], plan.Id)
	response := callSubscriptionAdminHandler(t, AdminBatchAssignUserSubscriptions, http.MethodPost, assignBody, nil)
	require.True(t, response.Success)
	require.EqualValues(t, 2, countSubscriptions("plan_id = ? AND status = ?", plan.Id, "active"))

	// Invalidating cancels the subscriptions but keeps their records.
	response = callSubscriptionAdminHandler(t, AdminBatchRevokeUserSubscriptions, http.MethodPost,
		fmt.Sprintf(`{"user_ids":[%d,%d],"plan_id":%d,"action":"invalidate"}`, ids[0], ids[1], plan.Id), nil)
	require.True(t, response.Success)
	require.EqualValues(t, 2, countSubscriptions("plan_id = ?", plan.Id))
	require.EqualValues(t, 2, countSubscriptions("plan_id = ? AND status = ?", plan.Id, "cancelled"))

	// Assign again, then delete: only the active records are removed, the
	// cancelled history stays behind.
	response = callSubscriptionAdminHandler(t, AdminBatchAssignUserSubscriptions, http.MethodPost, assignBody, nil)
	require.True(t, response.Success)
	require.EqualValues(t, 2, countSubscriptions("plan_id = ? AND status = ?", plan.Id, "active"))

	response = callSubscriptionAdminHandler(t, AdminBatchRevokeUserSubscriptions, http.MethodPost,
		fmt.Sprintf(`{"user_ids":[%d,%d],"plan_id":%d,"action":"delete"}`, ids[0], ids[1], plan.Id), nil)
	require.True(t, response.Success)
	require.EqualValues(t, 0, countSubscriptions("plan_id = ? AND status = ?", plan.Id, "active"))
	require.EqualValues(t, 2, countSubscriptions("plan_id = ? AND status = ?", plan.Id, "cancelled"))
}
