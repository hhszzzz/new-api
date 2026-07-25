package model

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserPolicySubscriptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "policy-subscriptions.db")), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&User{},
		&UserGroupMembership{},
		&UserModelPermission{},
		&UserModelRoute{},
		&UserModelRouteGroup{},
		&UserModelRouteChannel{},
		&SubscriptionPlan{},
		&UserSubscription{},
	))

	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled = previousRedisEnabled
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func createPolicySubscriptionTestUser(t *testing.T, group string) *User {
	t.Helper()
	user := &User{
		Username: "policy-user-" + group,
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    group,
		AffCode:  "aff-" + group,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func createPolicySubscriptionTestPlan(t *testing.T, group string) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Title:         "Policy plan " + group,
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

func policySubscriptionTestGroups(t *testing.T, userId int) []UserGroupMembership {
	t.Helper()
	var memberships []UserGroupMembership
	require.NoError(t, DB.Where("user_id = ?", userId).Order("sort_order asc, id asc").Find(&memberships).Error)
	return memberships
}

func requireUserQueryBeforeMembershipQuery(t *testing.T, db *gorm.DB, mutate func() error) {
	t.Helper()
	queriedTables := make([]string, 0, 4)
	const callbackName = "test:record_user_membership_lock_order"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil {
			return
		}
		if tx.Statement.Table == "users" || tx.Statement.Table == "user_group_memberships" {
			queriedTables = append(queriedTables, tx.Statement.Table)
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
	})

	require.NoError(t, mutate())
	userIndex, membershipIndex := -1, -1
	for index, table := range queriedTables {
		if table == "users" && userIndex == -1 {
			userIndex = index
		}
		if table == "user_group_memberships" && membershipIndex == -1 {
			membershipIndex = index
		}
	}
	require.NotEqual(t, -1, userIndex, "mutation did not query the user row")
	require.NotEqual(t, -1, membershipIndex, "mutation did not query group memberships")
	assert.Less(t, userIndex, membershipIndex, "user row must be locked before group memberships")
}

func TestUserPolicyReadsLegacyGroupUntilMembershipsAreBackfilled(t *testing.T) {
	db := setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")

	groups, err := GetUserGroups(user.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"legacy"}, groups)

	fetched, err := GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"legacy"}, fetched.Groups)

	users, total, err := SearchUsers("", "legacy", nil, nil, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, users, 1)
	assert.Equal(t, user.Id, users[0].Id)

	manual := true
	require.NoError(t, db.Create(&UserGroupMembership{
		UserId:    user.Id,
		GroupName: "other",
		SortOrder: 0,
		Manual:    &manual,
	}).Error)
	users, total, err = SearchUsers("", "legacy", nil, nil, 0, 20)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, users)
	users, total, err = SearchUsers("", "other", nil, nil, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Len(t, users, 1)
}

func TestInitializeUserPoliciesBackfillsMissingRowsIdempotently(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	legacyUser := createPolicySubscriptionTestUser(t, "legacy")
	autoUser := createPolicySubscriptionTestUser(t, "auto")
	existingUser := createPolicySubscriptionTestUser(t, "existing")

	manual := true
	require.NoError(t, DB.Create(&UserGroupMembership{
		UserId:    existingUser.Id,
		GroupName: "existing-membership",
		SortOrder: 7,
		Manual:    &manual,
	}).Error)
	require.NoError(t, DB.Model(&User{}).Where("id IN ?", []int{legacyUser.Id, autoUser.Id, existingUser.Id}).
		Updates(map[string]interface{}{"policy_version": 0, "topup_group": ""}).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", existingUser.Id).
		Update("topup_group", "existing-membership").Error)

	require.NoError(t, InitializeUserPolicies())
	require.NoError(t, InitializeUserPolicies())

	assert.Equal(t, []string{"legacy"}, groupNames(policySubscriptionTestGroups(t, legacyUser.Id)))
	assert.Equal(t, []string{"default"}, groupNames(policySubscriptionTestGroups(t, autoUser.Id)))
	existingMemberships := policySubscriptionTestGroups(t, existingUser.Id)
	require.Len(t, existingMemberships, 1)
	assert.Equal(t, "existing-membership", existingMemberships[0].GroupName)
	assert.Equal(t, 7, existingMemberships[0].SortOrder)
	require.NotNil(t, existingMemberships[0].Manual)
	assert.True(t, *existingMemberships[0].Manual)

	assert.Equal(t, "legacy", mustLoadPolicySubscriptionUser(t, legacyUser.Id).TopupGroup)
	assert.Equal(t, "default", mustLoadPolicySubscriptionUser(t, autoUser.Id).TopupGroup)
	assert.Equal(t, "existing-membership", mustLoadPolicySubscriptionUser(t, existingUser.Id).TopupGroup)
	assert.Equal(t, int64(1), mustLoadPolicySubscriptionUser(t, legacyUser.Id).PolicyVersion)
	assert.Equal(t, int64(1), mustLoadPolicySubscriptionUser(t, autoUser.Id).PolicyVersion)
	assert.Equal(t, int64(1), mustLoadPolicySubscriptionUser(t, existingUser.Id).PolicyVersion)

	var membershipCount int64
	require.NoError(t, DB.Model(&UserGroupMembership{}).Count(&membershipCount).Error)
	assert.EqualValues(t, 3, membershipCount)
}

func TestSubscriptionMembershipLifecyclePreservesManualGroupsAndSharedGrants(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")
	plan := createPolicySubscriptionTestPlan(t, "premium")

	first, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "admin")
	require.NoError(t, err)
	assert.Equal(t, []string{"legacy", "premium"}, groupNames(policySubscriptionTestGroups(t, user.Id)))
	assert.Equal(t, "legacy", mustLoadPolicySubscriptionUser(t, user.Id).Group)

	second, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "admin")
	require.NoError(t, err)
	assert.Equal(t, []string{"legacy", "premium"}, groupNames(policySubscriptionTestGroups(t, user.Id)))
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("topup_group", "premium").Error)

	_, err = AdminInvalidateUserSubscription(first.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"legacy", "premium"}, groupNames(policySubscriptionTestGroups(t, user.Id)))
	assert.Equal(t, "premium", mustLoadPolicySubscriptionUser(t, user.Id).TopupGroup)

	_, err = AdminDeleteUserSubscription(second.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"legacy"}, groupNames(policySubscriptionTestGroups(t, user.Id)))
	assert.Equal(t, "legacy", mustLoadPolicySubscriptionUser(t, user.Id).Group)
	assert.Equal(t, "legacy", mustLoadPolicySubscriptionUser(t, user.Id).TopupGroup)
}

func TestSubscriptionExplicitDowngradeBecomesPrimaryAfterLastGrantEnds(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")
	plan := createPolicySubscriptionTestPlan(t, "premium")
	plan.DowngradeGroup = "default"

	first, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "admin")
	require.NoError(t, err)
	second, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "admin")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("topup_group", "premium").Error)

	message, err := AdminInvalidateUserSubscription(first.Id)
	require.NoError(t, err)
	assert.Empty(t, message)
	assert.Equal(t, []string{"legacy", "premium"}, groupNames(policySubscriptionTestGroups(t, user.Id)))

	message, err = AdminInvalidateUserSubscription(second.Id)
	require.NoError(t, err)
	assert.Equal(t, "用户主分组将调整为 default", message)
	assert.Equal(t, []string{"default", "legacy"}, groupNames(policySubscriptionTestGroups(t, user.Id)))
	assert.Equal(t, "default", mustLoadPolicySubscriptionUser(t, user.Id).Group)
	assert.Equal(t, "default", mustLoadPolicySubscriptionUser(t, user.Id).TopupGroup)
	defaultMembership := policySubscriptionTestGroups(t, user.Id)[0]
	require.NotNil(t, defaultMembership.Manual)
	assert.True(t, *defaultMembership.Manual)
}

func TestSubscriptionDowngradeWithoutUpgradePreservesLegacyMembership(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")
	plan := createPolicySubscriptionTestPlan(t, "")
	plan.DowngradeGroup = "default"

	subscription, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "admin")
	require.NoError(t, err)
	message, err := AdminInvalidateUserSubscription(subscription.Id)
	require.NoError(t, err)

	assert.Equal(t, "用户主分组将调整为 default", message)
	assert.Equal(t, []string{"default", "legacy"}, groupNames(policySubscriptionTestGroups(t, user.Id)))
	assert.Equal(t, "default", mustLoadPolicySubscriptionUser(t, user.Id).Group)
}

func TestSubscriptionDoesNotRemoveGroupMadeManualOrReportFalseUpgrade(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")
	plan := createPolicySubscriptionTestPlan(t, "premium")

	manual := true
	require.NoError(t, DB.Create([]UserGroupMembership{
		{UserId: user.Id, GroupName: "legacy", SortOrder: 0, Manual: &manual},
		{UserId: user.Id, GroupName: "premium", SortOrder: 1, Manual: &manual},
	}).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("policy_version", 2).Error)

	message, err := AdminBindSubscription(user.Id, plan.Id, "assigned by admin")
	require.NoError(t, err)
	assert.Empty(t, message)
	assert.Equal(t, []string{"legacy", "premium"}, groupNames(policySubscriptionTestGroups(t, user.Id)))

	var subscription UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
	_, err = AdminInvalidateUserSubscription(subscription.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"legacy", "premium"}, groupNames(policySubscriptionTestGroups(t, user.Id)))
}

func TestPrimaryGroupIsExplicitAndKeepsLegacyColumnInSync(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")

	update := UserPolicyUpdate{
		Groups:       []string{"legacy", "premium"},
		PrimaryGroup: "premium",
		TopupGroup:   "legacy",
	}
	require.NoError(t, ReplaceUserPolicy(user.Id, update))

	groups, err := GetUserGroups(user.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"premium", "legacy"}, groups)

	var persisted User
	require.NoError(t, DB.First(&persisted, user.Id).Error)
	assert.Equal(t, "premium", persisted.Group)

	loaded, err := GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, "premium", loaded.Group)
	assert.Equal(t, []string{"premium", "legacy"}, loaded.Groups)
}

func TestPolicyEditPreservesRoutesScopedToAuthorizedRequestGroups(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")
	require.NoError(t, ReplaceUserPolicy(user.Id, UserPolicyUpdate{
		Groups:       []string{"legacy", "premium"},
		PrimaryGroup: "legacy",
		TopupGroup:   "legacy",
	}))
	route := &UserModelRoute{
		UserId:         user.Id,
		SourceModel:    "gpt-5.4",
		TargetModel:    "gpt-5.5",
		Groups:         []string{"vip"},
		ExecutionGroup: "default",
		Enabled:        true,
		ChannelIds:     []int{10},
	}
	require.NoError(t, SaveUserModelRoute(route))

	require.NoError(t, ReplaceUserPolicy(user.Id, UserPolicyUpdate{
		Groups:       []string{"legacy"},
		PrimaryGroup: "legacy",
		TopupGroup:   "legacy",
	}))

	routes, err := GetUserModelRoutes(user.Id)
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.Equal(t, []string{"vip"}, routes[0].Groups)
}

func TestPolicyAndSubscriptionMutationsLockUserBeforeMemberships(t *testing.T) {
	t.Run("policy replacement", func(t *testing.T) {
		db := setupUserPolicySubscriptionTestDB(t)
		user := createPolicySubscriptionTestUser(t, "legacy")

		requireUserQueryBeforeMembershipQuery(t, db, func() error {
			return ReplaceUserPolicy(user.Id, UserPolicyUpdate{
				Groups:       []string{"legacy", "premium"},
				PrimaryGroup: "legacy",
				TopupGroup:   "legacy",
			})
		})
	})

	t.Run("subscription revocation", func(t *testing.T) {
		db := setupUserPolicySubscriptionTestDB(t)
		user := createPolicySubscriptionTestUser(t, "legacy")
		plan := createPolicySubscriptionTestPlan(t, "premium")
		subscription, err := CreateUserSubscriptionFromPlanTx(db, user.Id, plan, "admin")
		require.NoError(t, err)

		requireUserQueryBeforeMembershipQuery(t, db, func() error {
			_, err := AdminInvalidateUserSubscription(subscription.Id)
			return err
		})
	})
}

func TestAdminSubscriptionAssignmentRequiresEnabledPlanAndAuditNote(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")
	plan := createPolicySubscriptionTestPlan(t, "premium")

	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Update("enabled", false).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	_, err := AdminBindSubscription(user.Id, plan.Id, "manual grant")
	assert.EqualError(t, err, "套餐未启用，不能手动分配")

	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]interface{}{
		"enabled":     true,
		"purchasable": false,
	}).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	_, err = AdminBindSubscription(user.Id, plan.Id, "")
	assert.EqualError(t, err, "管理员分配备注不能为空")
	internalPlan, err := GetSubscriptionPlanById(plan.Id)
	require.NoError(t, err)
	assert.EqualError(t, ValidateSubscriptionPlanPurchase(internalPlan), "该套餐仅支持管理员分配")

	_, err = AdminBindSubscription(user.Id, plan.Id, "manual grant")
	require.NoError(t, err)
	var subscription UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
	assert.Equal(t, "admin", subscription.Source)
	assert.Equal(t, "manual grant", subscription.SourceNote)
}

func TestInternalOnlySubscriptionPlanCannotBePurchasedWithBalance(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	user := createPolicySubscriptionTestUser(t, "legacy")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", 100000).Error)
	plan := createPolicySubscriptionTestPlan(t, "")
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]interface{}{
		"price_amount": 1,
		"purchasable":  false,
	}).Error)
	InvalidateSubscriptionPlanCache(plan.Id)

	err := PurchaseSubscriptionWithBalance(user.Id, plan.Id)
	assert.EqualError(t, err, "该套餐仅支持管理员分配")

	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	assert.Zero(t, subscriptionCount)
	var persisted User
	require.NoError(t, DB.First(&persisted, user.Id).Error)
	assert.Equal(t, 100000, persisted.Quota)
}

func TestSubscriptionPolicyChangesRefreshCachedGroups(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, subscription *UserSubscription)
	}{
		{
			name: "invalidate",
			mutate: func(t *testing.T, subscription *UserSubscription) {
				_, err := AdminInvalidateUserSubscription(subscription.Id)
				require.NoError(t, err)
			},
		},
		{
			name: "delete",
			mutate: func(t *testing.T, subscription *UserSubscription) {
				_, err := AdminDeleteUserSubscription(subscription.Id)
				require.NoError(t, err)
			},
		},
		{
			name: "expire",
			mutate: func(t *testing.T, subscription *UserSubscription) {
				require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subscription.Id).
					Update("end_time", time.Now().Add(-time.Minute).Unix()).Error)
				expired, err := ExpireDueSubscriptions(10)
				require.NoError(t, err)
				assert.Equal(t, 1, expired)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupUserPolicySubscriptionTestDB(t)
			useUserCacheMiniRedis(t)
			user := createPolicySubscriptionTestUser(t, "legacy")
			plan := createPolicySubscriptionTestPlan(t, "premium")
			require.NoError(t, populateUserCache(*user))

			_, err := AdminBindSubscription(user.Id, plan.Id, "cache lifecycle test")
			require.NoError(t, err)
			cached, err := GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, "legacy", cached.Group)
			assert.Equal(t, []string{"legacy", "premium"}, cached.Groups)

			var subscription UserSubscription
			require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
			test.mutate(t, &subscription)

			cached, err = GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, "legacy", cached.Group)
			assert.Equal(t, []string{"legacy"}, cached.Groups)
		})
	}
}

func groupNames(memberships []UserGroupMembership) []string {
	groups := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		groups = append(groups, membership.GroupName)
	}
	return groups
}

func mustLoadPolicySubscriptionUser(t *testing.T, userId int) User {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	return user
}
