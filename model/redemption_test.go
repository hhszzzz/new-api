package model

import (
	"bytes"
	"errors"
	"log"
	"strconv"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/group_rate_limit_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type legacyRedemptionSchema struct {
	Id           int `gorm:"primaryKey"`
	UserId       int
	Key          string `gorm:"type:char(32);uniqueIndex"`
	Status       int
	Name         string
	Quota        int
	CreatedTime  int64
	RedeemedTime int64
	UsedUserId   int
	ExpiredTime  int64
}

func (legacyRedemptionSchema) TableName() string { return "redemptions" }

func TestRedemptionCodeMigrationKeepsLegacyCodesUsableAndIsIdempotent(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	t.Cleanup(func() { DB = previousDB })
	require.NoError(t, db.AutoMigrate(&legacyRedemptionSchema{}))
	legacy := legacyRedemptionSchema{
		UserId: 1, Key: "00000000000000000000000000000031",
		Status: common.RedemptionCodeStatusEnabled, Name: "legacy", Quota: 100,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, db.Create(&legacy).Error)
	require.NoError(t, db.AutoMigrate(&Redemption{}, &RedemptionBatch{}))
	require.NoError(t, MigrateRedemptionCodeMetadata())
	require.NoError(t, MigrateRedemptionCodeMetadata())

	var migrated Redemption
	require.NoError(t, db.First(&migrated, legacy.Id).Error)
	assert.Equal(t, RedemptionTypeBalance, migrated.Type)
	assert.Equal(t, RedemptionCodeDigest(legacy.Key), migrated.KeyHash)
}

func TestSearchRedemptionsFiltersAndPaginates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	})

	now := common.GetTimestamp()
	redemptions := []Redemption{
		{Id: 1, Name: "alpha-active", Key: "00000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: 0},
		{Id: 2, Name: "alpha-future", Key: "00000000000000000000000000000002", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now + 3600},
		{Id: 3, Name: "alpha-expired", Key: "00000000000000000000000000000003", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now - 10},
		{Id: 4, Name: "beta-disabled", Key: "00000000000000000000000000000004", Status: common.RedemptionCodeStatusDisabled, ExpiredTime: 0},
		{Id: 5, Name: "beta-used", Key: "00000000000000000000000000000005", Status: common.RedemptionCodeStatusUsed, ExpiredTime: 0},
	}
	require.NoError(t, DB.Create(&redemptions).Error)

	tests := []struct {
		name      string
		keyword   string
		status    string
		startIdx  int
		num       int
		wantTotal int64
		wantIds   []int
	}{
		{
			name:      "no filters returns all rows",
			num:       10,
			wantTotal: 5,
			wantIds:   []int{5, 4, 3, 2, 1},
		},
		{
			name:      "keyword filters by name prefix",
			keyword:   "alpha",
			num:       10,
			wantTotal: 3,
			wantIds:   []int{3, 2, 1},
		},
		{
			name:      "enabled status excludes expired rows",
			status:    "1",
			num:       10,
			wantTotal: 2,
			wantIds:   []int{2, 1},
		},
		{
			name:      "expired status returns enabled expired rows",
			status:    "expired",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{3},
		},
		{
			name:      "disabled status",
			status:    "2",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{4},
		},
		{
			name:      "used status",
			status:    "3",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{5},
		},
		{
			name:      "pagination keeps unpaged total",
			startIdx:  1,
			num:       2,
			wantTotal: 5,
			wantIds:   []int{4, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, total, err := SearchRedemptions(tt.keyword, tt.status, tt.startIdx, tt.num)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			gotIds := make([]int, 0, len(rows))
			for _, row := range rows {
				gotIds = append(gotIds, row.Id)
			}
			assert.Equal(t, tt.wantIds, gotIds)
		})
	}
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	user := &User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "redeem-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)

	// Redeeming the same code again must fail and must not credit quota.
	_, err = Redeem(key, userId)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)
}

func TestRedeemRejectsWalletOverflow(t *testing.T) {
	userId, key := setupRedeemFixture(t, 11)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", common.MaxWalletQuota-10).Error)

	_, err := Redeem(key, userId)
	require.ErrorIs(t, err, ErrRedeemFailed)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, common.MaxWalletQuota-10, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
}

func TestRedemptionQuotaRejectsWalletOverflow(t *testing.T) {
	setupRedeemFixture(t, 500)

	redemption := &Redemption{
		Name:        "overflow-redemption",
		Key:         "10000000000000000000000000000002",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       common.MaxWalletQuota + 1,
		CreatedTime: common.GetTimestamp(),
	}
	require.Error(t, redemption.Insert())
}

func TestRedemptionBatchCreatesAtomicCodesAndTracksStatus(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}, &RedemptionBatch{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&RedemptionBatch{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&RedemptionBatch{}).Error)
	})
	var createAttempts int
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("redemption_batch_atomic_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "redemptions" {
			createAttempts++
			if createAttempts == 2 {
				tx.AddError(errors.New("injected redemption insert failure"))
			}
		}
	}))
	_, _, err := CreateRedemptionBatch(RedemptionBatchCreate{
		Name: "atomic-failure", Type: RedemptionTypeBalance, CreatedBy: 1,
		CreatedAt: common.GetTimestamp(), Count: 3, Quota: 500,
	})
	require.Error(t, err)
	require.NoError(t, DB.Callback().Create().Remove("redemption_batch_atomic_failure"))
	var count int64
	require.NoError(t, DB.Model(&Redemption{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&RedemptionBatch{}).Count(&count).Error)
	assert.Zero(t, count)

	keys, batchID, err := CreateRedemptionBatch(RedemptionBatchCreate{
		Name: "batch-test", Type: RedemptionTypeBalance, CreatedBy: 1,
		CreatedAt: common.GetTimestamp(), Count: 3, Quota: 500,
	})
	require.NoError(t, err)
	assert.Len(t, keys, 3)
	assert.NotZero(t, batchID)
	count = 0
	require.NoError(t, DB.Model(&Redemption{}).Where("batch_id = ?", batchID).Count(&count).Error)
	assert.EqualValues(t, 3, count)

	var used Redemption
	require.NoError(t, DB.First(&used, "batch_id = ?", batchID).Error)
	require.NoError(t, DB.Model(&Redemption{}).Where("id = ?", used.Id).Updates(map[string]any{
		"status": common.RedemptionCodeStatusUsed, "used_user_id": 12,
	}).Error)
	var expired Redemption
	require.NoError(t, DB.Where("batch_id = ? AND status = ? AND id <> ?", batchID, common.RedemptionCodeStatusEnabled, used.Id).First(&expired).Error)
	require.NoError(t, DB.Model(&Redemption{}).Where("id = ?", expired.Id).Update("expired_time", common.GetTimestamp()-1).Error)
	result, total, err := GetRedemptionBatches(0, 10, RedemptionTypeBalance)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, result, 1)
	assert.Equal(t, 1, result[0].UsedCount)
	assert.Equal(t, 1, result[0].AvailableCount)
	assert.Equal(t, 1, result[0].ExpiredCount)
	detail, err := GetRedemptionBatch(batchID)
	require.NoError(t, err)
	assert.Equal(t, batchID, detail.Id)
	assert.Len(t, detail.Redemptions, 3)
	exported, err := ExportRedemptionBatch(batchID)
	require.NoError(t, err)
	assert.Len(t, exported, 3)

	disabled, err := DisableUnusedRedemptionBatchCodes(batchID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, disabled)
	_, err = DisableUnusedRedemptionBatchCodes(batchID + 1_000_000)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	result, _, err = GetRedemptionBatches(0, 10, "")
	require.NoError(t, err)
	assert.Equal(t, 1, result[0].UsedCount)
	assert.Equal(t, 2, result[0].DisabledCount)
	assert.Zero(t, result[0].ExpiredCount)
}

func TestRedemptionBatchDoesNotLogGeneratedCodes(t *testing.T) {
	var output bytes.Buffer
	queryLogger := gormlogger.New(log.New(&output, "", 0), gormlogger.Config{
		LogLevel: gormlogger.Info, ParameterizedQueries: false,
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: queryLogger})
	require.NoError(t, err)
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&Redemption{}, &RedemptionBatch{}))

	keys, _, err := CreateRedemptionBatch(RedemptionBatchCreate{
		Name: "sensitive-codes", Type: RedemptionTypeBalance, CreatedBy: 1,
		CreatedAt: common.GetTimestamp(), Count: 2, Quota: 100,
	})
	require.NoError(t, err)
	for _, key := range keys {
		assert.NotContains(t, output.String(), key)
	}
}

func TestRegistrationBatchRequiresRegisteredTargetGroup(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}, &RedemptionBatch{}, &Group{}))
	group := Group{
		Name: "redemption-batch-target", Description: "Redemption batch target",
		Ratio: 1, CreatedAt: common.GetTimestamp(), UpdatedAt: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(&group).Error)
	var batchID int
	t.Cleanup(func() {
		if batchID > 0 {
			require.NoError(t, DB.Unscoped().Where("batch_id = ?", batchID).Delete(&Redemption{}).Error)
			require.NoError(t, DB.Unscoped().Delete(&RedemptionBatch{}, batchID).Error)
		}
		require.NoError(t, DB.Unscoped().Delete(&Group{}, "name = ?", group.Name).Error)
	})

	input := RedemptionBatchCreate{
		Name: "registration-batch", Type: RedemptionTypeRegistration,
		CreatedBy: 1, CreatedAt: common.GetTimestamp(), Count: 1,
		TargetGroup: "missing-registration-batch-target",
	}
	_, _, err := CreateRedemptionBatch(input)
	require.EqualError(t, err, "redemption target group does not exist")

	input.TargetGroup = group.Name
	var keys []string
	keys, batchID, err = CreateRedemptionBatch(input)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.NotZero(t, batchID)
}

func TestRedemptionBatchRateBonusesStackAndExpireIndependently(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	})
	now := common.GetTimestamp()
	userID := 9011
	grants := []Redemption{
		{Key: "00000000000000000000000000000011", Type: RedemptionTypeRateLimit, Status: common.RedemptionCodeStatusUsed, UsedUserId: userID, RPMDelta: 5, ConcurrencyDelta: 1, RedeemedTime: now - 30, ValiditySeconds: 3600},
		{Key: "00000000000000000000000000000012", Type: RedemptionTypeRateLimit, Status: common.RedemptionCodeStatusUsed, UsedUserId: userID, RPMDelta: 7, ConcurrencyDelta: 2, RedeemedTime: now - 7200, ValiditySeconds: 3600},
	}
	require.NoError(t, DB.Create(&grants).Error)
	rpm, concurrency, nextExpiry, err := GetActiveRedemptionRateBonuses(userID)
	require.NoError(t, err)
	assert.Equal(t, 5, rpm)
	assert.Equal(t, 1, concurrency)
	assert.Equal(t, grants[0].RedeemedTime+grants[0].ValiditySeconds, nextExpiry)
}

func TestRateLimitRedemptionRequiresFiniteBaseAndDoesNotConsumeOnFailure(t *testing.T) {
	userID, _ := setupRedeemFixture(t, 100)
	code := Redemption{
		Name: "rate-test", Key: "rate-limit-code-000000000000001", Type: RedemptionTypeRateLimit,
		Status: common.RedemptionCodeStatusEnabled, RPMDelta: 10, ValiditySeconds: 3600,
	}
	require.NoError(t, DB.Create(&code).Error)

	_, err := RedeemRedemptionCode(code.Key, userID)
	require.ErrorIs(t, err, ErrRedeemFailed)
	var stored Redemption
	require.NoError(t, DB.First(&stored, "key = ?", code.Key).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, stored.Status)

	rpm := 60
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("rpm_limit", rpm).Error)
	result, err := RedeemRedemptionCode(code.Key, userID)
	require.NoError(t, err)
	assert.Equal(t, RedemptionTypeRateLimit, result.Type)
	assert.Equal(t, 10, result.RPMDelta)
	require.NoError(t, DB.First(&stored, "key = ?", code.Key).Error)
	assert.Equal(t, stored.RedeemedTime+stored.ValiditySeconds, result.ExpiresAt)

	require.NoError(t, DB.Delete(&stored).Error)
	activeRPM, _, _, err := GetActiveRedemptionRateBonuses(userID)
	require.NoError(t, err)
	assert.Equal(t, 10, activeRPM, "deleting an already redeemed code must not revoke its timed grant")
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, rpm, *user.RpmLimit, "the stored baseline remains unchanged")
	assert.Greater(t, user.PolicyVersion, int64(0), "the cache policy fence advances with a new bonus")
}

func TestRegistrationCodeConsumptionAndAccountPolicyAreAtomic(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	previous := group_rate_limit_setting.GetSettingSnapshot()
	_, err := config.GlobalConfig.Update(group_rate_limit_setting.ConfigName, map[string]string{
		"default_new_user_rpm":         "30",
		"default_new_user_concurrency": "7",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, restoreErr := config.GlobalConfig.Update(group_rate_limit_setting.ConfigName, map[string]string{
			"default_new_user_rpm":         strconv.Itoa(previous.DefaultNewUserRPMLimit),
			"default_new_user_concurrency": strconv.Itoa(previous.DefaultNewUserConcurrencyLimit),
		})
		assert.NoError(t, restoreErr)
	})
	require.NoError(t, DB.AutoMigrate(&User{}, &UserGroupMembership{}, &UserModelPermission{}, &UserModelBlock{}, &UserModelRoute{}, &Redemption{}, &Group{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&UserGroupMembership{}).Error)
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&User{}).Error)
	})

	rpmLimit := 45
	code := Redemption{
		Name: "registration", Key: "00000000000000000000000000000021",
		Type: RedemptionTypeRegistration, Status: common.RedemptionCodeStatusEnabled,
		TargetGroup: "registration-group", InitialRPMLimit: &rpmLimit,
		ExpiredTime: common.GetTimestamp() + 3600,
	}
	require.NoError(t, DB.Create(&Group{
		Name: "registration-group", Description: "Registration group",
		Ratio: 1, CreatedAt: common.GetTimestamp(), UpdatedAt: common.GetTimestamp(),
	}).Error)
	require.NoError(t, DB.Create(&code).Error)
	usable, err := IsRegistrationCodeUsable(RedemptionCodeDigest(code.Key))
	require.NoError(t, err)
	assert.True(t, usable)
	usable, err = IsRegistrationCodeUsable(RedemptionCodeDigest(""))
	require.NoError(t, err)
	assert.False(t, usable)
	emptyCode := Redemption{
		Name: "empty-registration", Key: "   ", Type: RedemptionTypeRegistration,
		Status: common.RedemptionCodeStatusEnabled,
	}
	require.Error(t, DB.Create(&emptyCode).Error, "a blank code cannot be created as a usable registration credential")
	usable, err = IsRegistrationCodeUsable(RedemptionCodeDigest("missing-registration-code"))
	require.NoError(t, err)
	assert.False(t, usable)
	require.NoError(t, UpdateRedemptionStatus(code.Id, common.RedemptionCodeStatusDisabled))
	require.NoError(t, UpdateRedemptionStatus(code.Id, common.RedemptionCodeStatusDisabled), "repeating the requested state is idempotent")
	require.NoError(t, UpdateRedemptionStatus(code.Id, common.RedemptionCodeStatusEnabled))
	require.NoError(t, UpdateRedemptionStatus(code.Id, common.RedemptionCodeStatusEnabled), "repeating the requested state is idempotent")

	user := User{
		Username: "registration-code-user", Password: "password-12345",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
	}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return user.InsertWithRegistrationCodeTx(tx, RedemptionCodeDigest(code.Key), 0)
	}))
	assert.Equal(t, "registration-group", user.Group)
	assert.Equal(t, &rpmLimit, user.RpmLimit)
	assert.Equal(t, 7, *user.ConcurrencyLimit, "an omitted code override inherits the global default")

	var memberships []UserGroupMembership
	require.NoError(t, DB.Where("user_id = ?", user.Id).Order("sort_order asc").Find(&memberships).Error)
	require.Len(t, memberships, 1)
	assert.Equal(t, "registration-group", memberships[0].GroupName)
	var consumed Redemption
	require.NoError(t, DB.First(&consumed, "id = ?", code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, consumed.Status)
	assert.Equal(t, user.Id, consumed.UsedUserId)
	usable, err = IsRegistrationCodeUsable(RedemptionCodeDigest(code.Key))
	require.NoError(t, err)
	assert.False(t, usable)
	require.ErrorIs(t, UpdateRedemptionStatus(consumed.Id, common.RedemptionCodeStatusEnabled), ErrRedemptionAlreadyUsed)
	require.NoError(t, DB.First(&consumed, "id = ?", code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, consumed.Status)

	zero := 0
	defaultCode := Redemption{
		Name: "registration-default", Key: "00000000000000000000000000000022",
		Type: RedemptionTypeRegistration, Status: common.RedemptionCodeStatusEnabled,
		InitialConcurrencyLimit: &zero, ExpiredTime: common.GetTimestamp() + 3600,
	}
	require.NoError(t, DB.Create(&defaultCode).Error)
	defaultUser := User{
		Username: "registration-code-default-user", Password: "password-12345",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
	}
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return defaultUser.InsertWithRegistrationCodeTx(tx, RedemptionCodeDigest(defaultCode.Key), 0)
	}))
	assert.Equal(t, defaultUserGroup, defaultUser.Group)
	assert.Equal(t, 30, *defaultUser.RpmLimit, "an omitted RPM override inherits the global default")
	assert.Zero(t, *defaultUser.ConcurrencyLimit, "an explicit zero suppresses the global default")
	var defaultMembership UserGroupMembership
	require.NoError(t, DB.Where("user_id = ?", defaultUser.Id).Take(&defaultMembership).Error)
	assert.Equal(t, defaultUserGroup, defaultMembership.GroupName)

	duplicate := User{Username: "registration-code-reuse", Password: "password-12345", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	err = DB.Transaction(func(tx *gorm.DB) error {
		return duplicate.InsertWithRegistrationCodeTx(tx, RedemptionCodeDigest(code.Key), 0)
	})
	require.ErrorIs(t, err, ErrRegistrationCodeInvalid)
	assert.Zero(t, duplicate.Id)

	expired := Redemption{
		Name: "registration-expired", Key: "00000000000000000000000000000023",
		Type: RedemptionTypeRegistration, Status: common.RedemptionCodeStatusEnabled,
		ExpiredTime: common.GetTimestamp() - 1,
	}
	require.NoError(t, DB.Create(&expired).Error)
	expiredUser := User{Username: "registration-code-expired-user", Password: "password-12345", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	err = DB.Transaction(func(tx *gorm.DB) error {
		return expiredUser.InsertWithRegistrationCodeTx(tx, RedemptionCodeDigest(expired.Key), 0)
	})
	require.ErrorIs(t, err, ErrRegistrationCodeInvalid)
	assert.Zero(t, expiredUser.Id)
	var untouched Redemption
	require.NoError(t, DB.First(&untouched, "id = ?", expired.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, untouched.Status)
}

func TestSubscriptionRedemptionUsesIssuedPlanSnapshot(t *testing.T) {
	setupUserPolicySubscriptionTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Group{}, &User{}, &UserGroupMembership{}, &UserModelPermission{}, &UserModelBlock{}, &UserModelRoute{}, &SubscriptionPlan{}, &UserSubscription{}, &Redemption{}, &RedemptionBatch{}))
	for _, value := range []any{&Redemption{}, &RedemptionBatch{}, &UserSubscription{}, &UserGroupMembership{}, &UserModelPermission{}, &UserModelBlock{}, &UserModelRoute{}, &User{}, &SubscriptionPlan{}, &Group{}} {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(value).Error)
	}
	t.Cleanup(func() {
		for _, value := range []any{&Redemption{}, &RedemptionBatch{}, &UserSubscription{}, &UserGroupMembership{}, &UserModelPermission{}, &UserModelBlock{}, &UserModelRoute{}, &User{}, &SubscriptionPlan{}, &Group{}} {
			require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(value).Error)
		}
	})

	user := User{
		Username: "snapshot-redemption-user", Password: "password-12345",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: defaultUserGroup,
	}
	require.NoError(t, DB.Create(&user).Error)
	for _, groupName := range []string{defaultUserGroup, "snapshot-group"} {
		require.NoError(t, DB.Create(&Group{
			Name: groupName, Description: groupName, Ratio: 1,
			CreatedAt: common.GetTimestamp(), UpdatedAt: common.GetTimestamp(),
		}).Error)
	}
	plan := SubscriptionPlan{
		Title: "Issued plan", DurationUnit: SubscriptionDurationDay, DurationValue: 2,
		TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetDaily,
		UpgradeGroup: "snapshot-group", DowngradeGroup: defaultUserGroup,
		Quota5hAmount: 100, QuotaWeeklyAmount: 200, MaxPurchasePerUser: 1,
		Enabled: true,
	}
	plan.NormalizeDefaults()
	require.NoError(t, DB.Create(&plan).Error)
	snapshot, err := common.Marshal(plan)
	require.NoError(t, err)
	keys, _, err := CreateRedemptionBatch(RedemptionBatchCreate{
		Name: "snapshot-plan", Type: RedemptionTypeSubscription, CreatedBy: 1,
		CreatedAt: common.GetTimestamp(), Count: 1, PlanSnapshot: string(snapshot),
	})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]any{
		"title": "Edited plan", "duration_value": 30, "total_amount": 9999,
		"quota_reset_period": SubscriptionResetNever, "upgrade_group": "edited-group",
	}).Error)

	result, err := RedeemRedemptionCode(keys[0], user.Id)
	require.NoError(t, err)
	assert.Equal(t, RedemptionTypeSubscription, result.Type)
	assert.Equal(t, "Issued plan", result.PlanTitle)
	var subscription UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).Take(&subscription).Error)
	assert.Equal(t, "redemption", subscription.Source)
	assert.EqualValues(t, 1000, subscription.AmountTotal)
	assert.EqualValues(t, 2*24*60*60, subscription.EndTime-subscription.StartTime)
	assert.Greater(t, subscription.NextResetTime, int64(0), "the issued reset period remains in effect")
	assert.EqualValues(t, 100, subscription.Window5hAmount)
	assert.EqualValues(t, 200, subscription.WeeklyAmount)
	assert.Equal(t, "snapshot-group", subscription.UpgradeGroup)
	assert.Equal(t, defaultUserGroup, subscription.DowngradeGroup)
	var memberships []UserGroupMembership
	require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&memberships).Error)
	require.NotEmpty(t, memberships)
	membershipGroups := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		membershipGroups = append(membershipGroups, membership.GroupName)
	}
	assert.Contains(t, membershipGroups, "snapshot-group")
}

func TestRegistrationCodeConcurrentClaimsCreateOneAccount(t *testing.T) {
	db := setupUserPolicySubscriptionTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, DB.AutoMigrate(&User{}, &UserGroupMembership{}, &UserModelPermission{}, &UserModelBlock{}, &UserModelRoute{}, &Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		subquery := DB.Model(&User{}).Select("id").Where("username LIKE ?", "registration-race-%")
		require.NoError(t, DB.Unscoped().Where("user_id IN (?)", subquery).Delete(&UserGroupMembership{}).Error)
		require.NoError(t, DB.Unscoped().Where("username LIKE ?", "registration-race-%").Delete(&User{}).Error)
	})
	code := Redemption{
		Name: "registration-race", Key: "00000000000000000000000000000024",
		Type: RedemptionTypeRegistration, Status: common.RedemptionCodeStatusEnabled,
	}
	require.NoError(t, DB.Create(&code).Error)
	const workers = 5
	start := make(chan struct{})
	results := make(chan error, workers)
	var wait sync.WaitGroup
	for index := range workers {
		wait.Go(func() {
			<-start
			user := User{
				Username: "registration-race-" + strconv.Itoa(index), Password: "password-12345",
				Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
			}
			results <- DB.Transaction(func(tx *gorm.DB) error {
				return user.InsertWithRegistrationCodeTx(tx, RedemptionCodeDigest(code.Key), 0)
			})
		})
	}
	close(start)
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			assert.ErrorIs(t, err, ErrRegistrationCodeInvalid)
		}
	}
	assert.Equal(t, 1, successes)
	var accounts int64
	require.NoError(t, DB.Model(&User{}).Where("username LIKE ?", "registration-race-%").Count(&accounts).Error)
	assert.EqualValues(t, 1, accounts)
	var consumed Redemption
	require.NoError(t, DB.First(&consumed, "id = ?", code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, consumed.Status)
}

// Exactly one of several concurrent redeems of the same code may win, and
// quota must be credited exactly once.
func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	userId, key := setupRedeemFixture(t, 300)

	const goroutines = 5
	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userId); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 300, user.Quota, "quota must be credited exactly once")
}
