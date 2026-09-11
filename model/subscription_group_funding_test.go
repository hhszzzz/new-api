package model

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupSubscriptionGroupFundingTestDB 建立只包含订阅计费所需表的独立库。
func setupSubscriptionGroupFundingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "subscription-group-funding.db")), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&User{},
		&UserGroupMembership{},
		&SubscriptionPlan{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
	))

	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled = previousRedisEnabled
		if sqlDB, sqlErr := db.DB(); sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func createGroupFundingPlan(t *testing.T, group string) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Title:         "group-funding-" + group,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   1000,
		UpgradeGroup:  group,
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	return plan
}

func createGroupFundingSubscription(t *testing.T, userId, planId int, group string, total int64, status string, endTime int64) *UserSubscription {
	t.Helper()
	sub := &UserSubscription{
		UserId:       userId,
		PlanId:       planId,
		Status:       status,
		StartTime:    time.Now().Unix() - 60,
		EndTime:      endTime,
		AmountTotal:  total,
		AmountUsed:   0,
		UpgradeGroup: group,
	}
	require.NoError(t, DB.Create(sub).Error)
	return sub
}

func createGroupFundingUser(t *testing.T, name string) *User {
	t.Helper()
	user := &User{
		Username: name,
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  name,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func TestHasActiveSubscriptionGrantingGroup(t *testing.T) {
	setupSubscriptionGroupFundingTestDB(t)
	user := createGroupFundingUser(t, "grant-check")
	now := time.Now().Unix()
	proPlan := createGroupFundingPlan(t, "pro")
	basicPlan := createGroupFundingPlan(t, "basic")

	// 活跃 pro 订阅：pro=true，basic=false。
	createGroupFundingSubscription(t, user.Id, proPlan.Id, "pro", 1000, "active", now+3600)
	granted, err := HasActiveSubscriptionGrantingGroup(user.Id, "pro")
	require.NoError(t, err)
	assert.True(t, granted)
	granted, err = HasActiveSubscriptionGrantingGroup(user.Id, "basic")
	require.NoError(t, err)
	assert.False(t, granted)

	// 大小写与空白不影响判定。
	granted, err = HasActiveSubscriptionGrantingGroup(user.Id, "  pro  ")
	require.NoError(t, err)
	assert.True(t, granted)

	// 空分组或非法用户直接返回 false。
	granted, err = HasActiveSubscriptionGrantingGroup(user.Id, "")
	require.NoError(t, err)
	assert.False(t, granted)
	granted, err = HasActiveSubscriptionGrantingGroup(0, "pro")
	require.NoError(t, err)
	assert.False(t, granted)

	// 非活跃状态不再授予。
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Update("status", "expired").Error)
	granted, err = HasActiveSubscriptionGrantingGroup(user.Id, "pro")
	require.NoError(t, err)
	assert.False(t, granted)

	// 到期时间已过不再授予。
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Updates(map[string]any{
		"status":   "active",
		"end_time": now - 1,
	}).Error)
	granted, err = HasActiveSubscriptionGrantingGroup(user.Id, "pro")
	require.NoError(t, err)
	assert.False(t, granted)

	// 另一分组订阅不影响 basic 判定，但 pro 请求不会命中它。
	createGroupFundingSubscription(t, user.Id, basicPlan.Id, "basic", 1000, "active", now+3600)
	granted, err = HasActiveSubscriptionGrantingGroup(user.Id, "basic")
	require.NoError(t, err)
	assert.True(t, granted)
}

func TestPreConsumeUserSubscriptionScopesToGroup(t *testing.T) {
	setupSubscriptionGroupFundingTestDB(t)
	user := createGroupFundingUser(t, "group-scope")
	now := time.Now().Unix()
	proPlan := createGroupFundingPlan(t, "pro")
	basicPlan := createGroupFundingPlan(t, "basic")
	proSub := createGroupFundingSubscription(t, user.Id, proPlan.Id, "pro", 1000, "active", now+3600)
	// basic 更早到期，用于验证空分组时按 end_time 顺序选取。
	basicSub := createGroupFundingSubscription(t, user.Id, basicPlan.Id, "basic", 1000, "active", now+1800)

	// pro 分组的请求只能扣 pro 订阅。
	res, err := PreConsumeUserSubscription("req-pro-1", user.Id, "gpt-4o", 0, 300, "pro")
	require.NoError(t, err)
	assert.Equal(t, proSub.Id, res.UserSubscriptionId)
	assert.EqualValues(t, 300, res.PreConsumed)

	var reloadedPro, reloadedBasic UserSubscription
	require.NoError(t, DB.First(&reloadedPro, proSub.Id).Error)
	require.NoError(t, DB.First(&reloadedBasic, basicSub.Id).Error)
	assert.EqualValues(t, 300, reloadedPro.AmountUsed)
	assert.EqualValues(t, 0, reloadedBasic.AmountUsed, "无关分组的订阅不应被扣费")

	// pro 订阅余额不足以覆盖该请求时，不得回退到 basic 订阅。
	_, err = PreConsumeUserSubscription("req-basic-1", user.Id, "gpt-4o", 0, 800, "pro")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient")
	require.NoError(t, DB.First(&reloadedPro, proSub.Id).Error)
	assert.EqualValues(t, 300, reloadedPro.AmountUsed, "失败请求不得产生扣费")
	require.NoError(t, DB.First(&reloadedBasic, basicSub.Id).Error)
	assert.EqualValues(t, 0, reloadedBasic.AmountUsed, "不得借用其他分组的订阅额度")

	// 空分组保持原有语义：任意活跃订阅均可付费。
	res, err = PreConsumeUserSubscription("req-any-1", user.Id, "gpt-4o", 0, 50, "")
	require.NoError(t, err)
	assert.Equal(t, basicSub.Id, res.UserSubscriptionId, "按 end_time 顺序应优先命中较早到期的订阅")
}

func TestPreConsumeUserSubscriptionGroupQuotaExhausted(t *testing.T) {
	setupSubscriptionGroupFundingTestDB(t)
	user := createGroupFundingUser(t, "group-exhaust")
	now := time.Now().Unix()
	proPlan := createGroupFundingPlan(t, "pro")
	basicPlan := createGroupFundingPlan(t, "basic")
	proSub := createGroupFundingSubscription(t, user.Id, proPlan.Id, "pro", 100, "active", now+3600)
	basicSub := createGroupFundingSubscription(t, user.Id, basicPlan.Id, "basic", 1000, "active", now+3600)

	// 扣满 pro 订阅额度。
	_, err := PreConsumeUserSubscription("req-fill", user.Id, "gpt-4o", 0, 100, "pro")
	require.NoError(t, err)

	// pro 额度耗尽后，不得回退到 basic 订阅（更不得回退钱包）。
	_, err = PreConsumeUserSubscription("req-over", user.Id, "gpt-4o", 0, 1, "pro")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient")

	require.NoError(t, DB.First(&basicSub).Error)
	assert.EqualValues(t, 0, basicSub.AmountUsed, "耗尽后不得借用其他订阅额度")
	require.NoError(t, DB.First(&proSub).Error)
	assert.EqualValues(t, 100, proSub.AmountUsed)
}

func TestHasActiveSubscriptionForGroup(t *testing.T) {
	setupSubscriptionGroupFundingTestDB(t)
	user := createGroupFundingUser(t, "eligible-check")
	now := time.Now().Unix()
	proPlan := createGroupFundingPlan(t, "pro")
	createGroupFundingSubscription(t, user.Id, proPlan.Id, "pro", 1000, "active", now+3600)

	// 绑定订阅只让自己的分组可选。
	eligible, err := HasActiveSubscriptionForGroup(user.Id, "pro")
	require.NoError(t, err)
	assert.True(t, eligible)
	eligible, err = HasActiveSubscriptionForGroup(user.Id, "basic")
	require.NoError(t, err)
	assert.False(t, eligible, "绑定分组的订阅不得为其他分组提供额度")

	// 无分组的通用订阅可为任意分组付费（历史行为）。
	genericPlan := createGroupFundingPlan(t, "")
	createGroupFundingSubscription(t, user.Id, genericPlan.Id, "", 1000, "active", now+3600)
	eligible, err = HasActiveSubscriptionForGroup(user.Id, "basic")
	require.NoError(t, err)
	assert.True(t, eligible, "通用订阅应可为任意分组提供额度")
	eligible, err = HasActiveSubscriptionForGroup(user.Id, "")
	require.NoError(t, err)
	assert.True(t, eligible)
}

func TestPreConsumeUserSubscriptionAllowsGenericSubscriptionForAnyGroup(t *testing.T) {
	setupSubscriptionGroupFundingTestDB(t)
	user := createGroupFundingUser(t, "generic-funding")
	now := time.Now().Unix()
	genericPlan := createGroupFundingPlan(t, "")
	genericSub := createGroupFundingSubscription(t, user.Id, genericPlan.Id, "", 1000, "active", now+3600)

	res, err := PreConsumeUserSubscription("req-generic", user.Id, "gpt-4o", 0, 100, "pro")
	require.NoError(t, err)
	assert.Equal(t, genericSub.Id, res.UserSubscriptionId, "通用订阅应能为任意分组付费")

	var reloaded UserSubscription
	require.NoError(t, DB.First(&reloaded, genericSub.Id).Error)
	assert.EqualValues(t, 100, reloaded.AmountUsed)
}

func TestPreConsumeUserSubscriptionGroupFilterIgnoresOtherGroups(t *testing.T) {
	setupSubscriptionGroupFundingTestDB(t)
	user := createGroupFundingUser(t, "group-only-other")
	now := time.Now().Unix()
	basicPlan := createGroupFundingPlan(t, "basic")
	createGroupFundingSubscription(t, user.Id, basicPlan.Id, "basic", 1000, "active", now+3600)

	// 用户仅有 basic 订阅，却走 pro 分组：不命中任何订阅。
	_, err := PreConsumeUserSubscription("req-pro", user.Id, "gpt-4o", 0, 10, "pro")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient")
}

func TestEnsureNoActiveSubscriptionsForGroups(t *testing.T) {
	setupSubscriptionGroupFundingTestDB(t)
	user := createGroupFundingUser(t, "group-removal-guard")
	plan := createGroupFundingPlan(t, "pro")
	now := time.Now().Unix()
	createGroupFundingSubscription(t, user.Id, plan.Id, "pro", 1000, "active", now+3600)

	err := EnsureNoActiveSubscriptionsForGroups([]string{"pro"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pro")

	// 移除无关分组或空集合不阻止。
	require.NoError(t, EnsureNoActiveSubscriptionsForGroups([]string{"basic"}))
	require.NoError(t, EnsureNoActiveSubscriptionsForGroups(nil))

	// 订阅过期后不再保护该分组。
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).
		Update("end_time", now-10).Error)
	require.NoError(t, EnsureNoActiveSubscriptionsForGroups([]string{"pro"}))
}

func TestUpdateOptionsBulkBlocksRemovingGroupUsedByActiveSubscription(t *testing.T) {
	db := setupSubscriptionGroupFundingTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	user := createGroupFundingUser(t, "group-removal-option")
	plan := createGroupFundingPlan(t, "pro")
	createGroupFundingSubscription(t, user.Id, plan.Id, "pro", 1000, "active", time.Now().Unix()+3600)

	original := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"pro":1.5}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(original)) })

	err := UpdateOptionsBulk(map[string]string{"GroupRatio": `{"default":1}`})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pro")
}
