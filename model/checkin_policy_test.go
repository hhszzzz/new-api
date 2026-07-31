package model

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCheckinPolicyTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "checkin.db")), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&User{}, &Checkin{}))
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	setting := operation_setting.GetCheckinSetting()
	previous := *setting
	setting.Enabled = true
	setting.MinQuota = 300
	setting.MaxQuota = 300
	t.Cleanup(func() { *setting = previous })
}

func TestUserCheckinHonorsPerUserOverrides(t *testing.T) {
	setupCheckinPolicyTestDB(t)

	blocked := false
	fixed := 7
	tests := []struct {
		name       string
		user       User
		wantErr    string
		wantAmount int
	}{
		{
			name:       "nil override follows global setting",
			user:       User{Id: 1, Username: "checkin-global", Group: "default", DisplayName: "u1", AffCode: "ck1"},
			wantAmount: 300,
		},
		{
			name:    "blocked user cannot check in",
			user:    User{Id: 2, Username: "checkin-blocked", Group: "default", AffCode: "ck2", CheckinEnabled: &blocked},
			wantErr: "该账户已被限制签到",
		},
		{
			name:       "quota override wins over global range",
			user:       User{Id: 3, Username: "checkin-custom", Group: "default", AffCode: "ck3", CheckinMinQuota: &fixed, CheckinMaxQuota: &fixed},
			wantAmount: 7,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, DB.Create(&test.user).Error)

			checkin, err := UserCheckin(test.user.Id)
			if test.wantErr != "" {
				require.EqualError(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantAmount, checkin.QuotaAwarded)
			assert.Equal(t, time.Now().Format("2006-01-02"), checkin.CheckinDate)

			var quota int
			require.NoError(t, DB.Model(&User{}).Select("quota").Where("id = ?", test.user.Id).Take(&quota).Error)
			assert.Equal(t, test.wantAmount, quota)

			_, err = UserCheckin(test.user.Id)
			require.EqualError(t, err, "今日已签到")
		})
	}
}

func TestGiftQuotaCapLimitsCheckinRedemptionAndAffTransfer(t *testing.T) {
	setupCheckinPolicyTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Redemption{}, &Log{}))

	quotaCap := 400
	require.NoError(t, DB.Create(&User{
		Id:       11,
		Username: "capped-user",
		AffCode:  "cap1",
		Group:    "default",
		Quota:    350,
		AffQuota: 600000,
		QuotaCap: &quotaCap,
	}).Error)

	// Check-in clamps the award (global fixed 300) to the remaining headroom.
	checkin, err := UserCheckin(11)
	require.NoError(t, err)
	assert.Equal(t, 50, checkin.QuotaAwarded)
	var quota int
	require.NoError(t, DB.Model(&User{}).Select("quota").Where("id = ?", 11).Take(&quota).Error)
	assert.Equal(t, 400, quota)

	// At the cap, further check-ins are rejected outright.
	require.NoError(t, DB.Delete(&Checkin{}, "user_id = ?", 11).Error)
	_, err = UserCheckin(11)
	require.EqualError(t, err, "账户额度已达上限，无法签到")

	// Redemption codes that would exceed the cap are rejected and stay unused.
	require.NoError(t, DB.Create(&Redemption{Id: 1, UserId: 1, Key: "cap-code-1", Quota: 100, Status: common.RedemptionCodeStatusEnabled, Name: "cap"}).Error)
	_, err = Redeem("cap-code-1", 11)
	require.ErrorIs(t, err, ErrQuotaCapExceeded)
	var status int
	require.NoError(t, DB.Model(&Redemption{}).Select("status").Where("id = ?", 1).Take(&status).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, status)

	// Aff transfers past the cap are rejected too.
	cappedUser := &User{Id: 11}
	err = cappedUser.TransferAffQuotaToQuota(500000)
	require.EqualError(t, err, "转移后将超过账户额度上限")
}

func TestEffectiveCheckinPolicyClampsInvertedRange(t *testing.T) {
	setupCheckinPolicyTestDB(t)

	minQuota := 50
	maxQuota := 10
	require.NoError(t, DB.Create(&User{
		Id:              9,
		Username:        "checkin-inverted",
		AffCode:         "ck9",
		Group:           "default",
		CheckinMinQuota: &minQuota,
		CheckinMaxQuota: &maxQuota,
	}).Error)

	allowed, gotMin, gotMax, err := EffectiveCheckinPolicy(9)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 50, gotMin)
	assert.Equal(t, 50, gotMax)
}
