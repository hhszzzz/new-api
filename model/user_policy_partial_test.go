package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserPolicyPartialTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "policy-partial.db")), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(
		&User{},
		&UserGroupMembership{},
		&UserModelPermission{},
		&UserModelBlock{},
		&Token{},
	))
	t.Cleanup(func() {
		DB = previousDB
		common.RedisEnabled = previousRedis
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestUpdateUserPolicyPartialOnlyTouchesRequestedSections(t *testing.T) {
	setupUserPolicyPartialTestDB(t)

	require.NoError(t, DB.Create(&User{
		Id:       1,
		Username: "partial-user",
		Group:    "vip",
		AffCode:  "pp1",
	}).Error)
	require.NoError(t, DB.Create(&UserGroupMembership{UserId: 1, GroupName: "vip"}).Error)
	require.NoError(t, DB.Create(&UserModelPermission{UserId: 1, ModelName: "gpt-5.4"}).Error)
	require.NoError(t, DB.Create(&UserModelBlock{UserId: 1, ModelName: "blocked-old"}).Error)

	var before User
	require.NoError(t, DB.First(&before, 1).Error)

	limits := []string{"gpt-5.5", "claude-4.5"}
	require.NoError(t, UpdateUserPolicyPartial(1, UserPolicyPartialUpdate{
		ModelLimits:        &limits,
		ModelLimitsEnabled: common.GetPointer(true),
		SetCheckinEnabled:  true,
		CheckinEnabled:     common.GetPointer(false),
	}))

	var after User
	require.NoError(t, DB.First(&after, 1).Error)
	assert.True(t, after.ModelLimitsEnabled)
	require.NotNil(t, after.CheckinEnabled)
	assert.False(t, *after.CheckinEnabled)
	assert.Nil(t, after.CheckinMinQuota)
	assert.Greater(t, after.PolicyVersion, before.PolicyVersion)
	// Untouched sections keep their state.
	assert.False(t, after.ModelBlocklistEnabled)
	assert.Equal(t, "vip", after.Group)

	var permissions []UserModelPermission
	require.NoError(t, DB.Where("user_id = ?", 1).Order("model_name asc").Find(&permissions).Error)
	require.Len(t, permissions, 2)
	assert.Equal(t, "claude-4.5", permissions[0].ModelName)
	assert.Equal(t, "gpt-5.5", permissions[1].ModelName)

	var blocks []UserModelBlock
	require.NoError(t, DB.Where("user_id = ?", 1).Find(&blocks).Error)
	require.Len(t, blocks, 1)
	assert.Equal(t, "blocked-old", blocks[0].ModelName)

	var memberships []UserGroupMembership
	require.NoError(t, DB.Where("user_id = ?", 1).Find(&memberships).Error)
	require.Len(t, memberships, 1)
	assert.Equal(t, "vip", memberships[0].GroupName)
}

func TestUpdateUserPolicyPartialWritesCheckinFollowGlobal(t *testing.T) {
	setupUserPolicyPartialTestDB(t)

	require.NoError(t, DB.Create(&User{
		Id:              2,
		Username:        "partial-checkin",
		Group:           "default",
		AffCode:         "pp2",
		CheckinEnabled:  common.GetPointer(false),
		CheckinMinQuota: common.GetPointer(5),
		CheckinMaxQuota: common.GetPointer(9),
	}).Error)
	require.NoError(t, DB.Create(&UserGroupMembership{UserId: 2, GroupName: "default"}).Error)

	require.NoError(t, UpdateUserPolicyPartial(2, UserPolicyPartialUpdate{
		SetCheckinEnabled: true,
		SetCheckinQuota:   true,
	}))

	var after User
	require.NoError(t, DB.First(&after, 2).Error)
	assert.Nil(t, after.CheckinEnabled)
	assert.Nil(t, after.CheckinMinQuota)
	assert.Nil(t, after.CheckinMaxQuota)
}

func TestUpdateUserPolicyPartialEmptyIsNoOp(t *testing.T) {
	setupUserPolicyPartialTestDB(t)

	require.NoError(t, DB.Create(&User{Id: 3, Username: "partial-noop", Group: "default", AffCode: "pp3"}).Error)
	var before User
	require.NoError(t, DB.First(&before, 3).Error)

	require.NoError(t, UpdateUserPolicyPartial(3, UserPolicyPartialUpdate{}))

	var after User
	require.NoError(t, DB.First(&after, 3).Error)
	assert.Equal(t, before.PolicyVersion, after.PolicyVersion)
}

func TestUserRateLimitColumnsAreNullableAndPartialUpdatesPreserveUntouchedValues(t *testing.T) {
	setupUserPolicyPartialTestDB(t)
	for _, column := range []string{"rpm_limit", "concurrency_limit", "stream_tps_limit", "first_token_delay_ms"} {
		assert.True(t, DB.Migrator().HasColumn(&User{}, column))
	}

	user := User{
		Id:                4,
		Username:          "rate-limit-policy",
		Group:             "default",
		AffCode:           "pp4",
		RpmLimit:          common.GetPointer(60),
		ConcurrencyLimit:  common.GetPointer(2),
		StreamTpsLimit:    common.GetPointer(12),
		FirstTokenDelayMs: common.GetPointer(1000),
	}
	require.NoError(t, DB.Create(&user).Error)
	beforeVersion := user.PolicyVersion

	require.NoError(t, UpdateUserPolicyPartial(user.Id, UserPolicyPartialUpdate{
		SetRpmLimit:          true,
		RpmLimit:             common.GetPointer(90),
		SetConcurrencyLimit:  true,
		ConcurrencyLimit:     nil,
		SetFirstTokenDelayMs: true,
		FirstTokenDelayMs:    common.GetPointer(1500),
	}))

	var updated User
	require.NoError(t, DB.First(&updated, user.Id).Error)
	require.NotNil(t, updated.RpmLimit)
	assert.Equal(t, 90, *updated.RpmLimit)
	assert.Nil(t, updated.ConcurrencyLimit)
	require.NotNil(t, updated.StreamTpsLimit)
	assert.Equal(t, 12, *updated.StreamTpsLimit)
	require.NotNil(t, updated.FirstTokenDelayMs)
	assert.Equal(t, 1500, *updated.FirstTokenDelayMs)
	assert.Greater(t, updated.PolicyVersion, beforeVersion)

	encoded, err := common.Marshal(updated)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "rpm_limit")
	assert.NotContains(t, string(encoded), "concurrency_limit")
	assert.NotContains(t, string(encoded), "stream_tps_limit")
	assert.NotContains(t, string(encoded), "first_token_delay_ms")
}
