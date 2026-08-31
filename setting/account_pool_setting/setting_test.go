package account_pool_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSetting() Setting {
	return Setting{
		Enabled:                   true,
		HideEmailFromNonAdmins:    true,
		AllowedGroups:             []string{"vip", "default"},
		RegularRefreshSeconds:     300,
		NearResetThresholdSeconds: 600,
		NearResetRefreshSeconds:   60,
		PostResetDelaySeconds:     10,
		ManualRefreshCooldown:     60,
	}
}

func TestPrepareSettingNormalizesAllowedGroups(t *testing.T) {
	setting := validSetting()
	setting.AllowedGroups = []string{" vip ", "default", "vip"}

	prepared, err := PrepareSetting(setting)

	require.NoError(t, err)
	assert.Equal(t, []string{"default", "vip"}, prepared.AllowedGroups)
}

func TestPrepareSettingRejectsInvalidRefreshRanges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Setting)
	}{
		{name: "regular too small", mutate: func(setting *Setting) { setting.RegularRefreshSeconds = 59 }},
		{name: "near threshold too large", mutate: func(setting *Setting) { setting.NearResetThresholdSeconds = 3601 }},
		{name: "near interval too small", mutate: func(setting *Setting) { setting.NearResetRefreshSeconds = 29 }},
		{name: "near exceeds regular", mutate: func(setting *Setting) { setting.RegularRefreshSeconds = 60; setting.NearResetRefreshSeconds = 61 }},
		{name: "post reset delay too large", mutate: func(setting *Setting) { setting.PostResetDelaySeconds = 121 }},
		{name: "manual cooldown too small", mutate: func(setting *Setting) { setting.ManualRefreshCooldown = 29 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting := validSetting()
			test.mutate(&setting)
			_, err := PrepareSetting(setting)
			require.Error(t, err)
		})
	}
}

func TestCanAccessUsesAnyAssignedGroupAndAdminBypass(t *testing.T) {
	previous := GetSettingSnapshot()
	require.NotNil(t, previous)
	t.Cleanup(func() { previous.PublishConfig() })

	setting := validSetting()
	setting.AllowedGroups = []string{"vip", "team"}
	setting.PublishConfig()

	assert.True(t, CanAccess(common.RoleCommonUser, []string{"default", "vip"}))
	assert.False(t, CanAccess(common.RoleCommonUser, []string{"default"}))
	assert.True(t, CanAccess(common.RoleAdminUser, nil))
	assert.True(t, CanAccess(common.RoleRootUser, nil))
}

func TestCanAccessFailsClosedWhenDisabledOrAllowedGroupsEmpty(t *testing.T) {
	previous := GetSettingSnapshot()
	require.NotNil(t, previous)
	t.Cleanup(func() { previous.PublishConfig() })

	setting := validSetting()
	setting.Enabled = false
	setting.PublishConfig()
	assert.False(t, CanAccess(common.RoleRootUser, []string{"vip"}))

	setting.Enabled = true
	setting.AllowedGroups = []string{}
	setting.PublishConfig()
	assert.False(t, CanAccess(common.RoleCommonUser, []string{"vip"}))
	assert.True(t, CanAccess(common.RoleAdminUser, nil))
}

func TestShouldIncludeEmailUsesPrivacySettingAndAdminBypass(t *testing.T) {
	previous := GetSettingSnapshot()
	require.NotNil(t, previous)
	t.Cleanup(func() { previous.PublishConfig() })

	setting := validSetting()
	setting.HideEmailFromNonAdmins = true
	setting.PublishConfig()
	assert.False(t, ShouldIncludeEmail(common.RoleCommonUser))
	assert.True(t, ShouldIncludeEmail(common.RoleAdminUser))

	setting.HideEmailFromNonAdmins = false
	setting.PublishConfig()
	assert.True(t, ShouldIncludeEmail(common.RoleCommonUser))
}

func TestAccountPoolConfigManagerRejectsPartialInvalidPublication(t *testing.T) {
	manager := config.NewConfigManager()
	setting := validSetting()
	setting.Enabled = false
	manager.Register(ConfigName, &setting)

	handled, err := manager.Update(ConfigName, map[string]string{
		"enabled":                    "true",
		"allowed_groups":             `["vip","team"]`,
		"regular_refresh_seconds":    "300",
		"near_reset_refresh_seconds": "60",
	})
	require.True(t, handled)
	require.NoError(t, err)
	assert.True(t, setting.Enabled)
	assert.True(t, setting.HideEmailFromNonAdmins)
	assert.Equal(t, []string{"team", "vip"}, setting.AllowedGroups)

	handled, err = manager.Update(ConfigName, map[string]string{
		"enabled":                    "false",
		"regular_refresh_seconds":    "60",
		"near_reset_refresh_seconds": "61",
	})
	require.True(t, handled)
	require.Error(t, err)
	assert.True(t, setting.Enabled)
	assert.Equal(t, 300, setting.RegularRefreshSeconds)
	assert.Equal(t, 60, setting.NearResetRefreshSeconds)
}
