package group_rate_limit_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPointer(value int) *int {
	return &value
}

func TestGroupRateLimitSettingDefaultsAndPublishesImmutableSnapshot(t *testing.T) {
	snapshot := GetSettingSnapshot()
	require.NotNil(t, snapshot)
	assert.False(t, snapshot.MemberEnabled)
	assert.False(t, snapshot.SharedPoolEnabled)
	assert.Empty(t, snapshot.Policies)

	setting := Setting{
		MemberEnabled:     true,
		SharedPoolEnabled: true,
		Policies: map[string]GroupPolicy{
			" vip ": {
				MemberLimits: Limits{RPMLimit: intPointer(60)},
				SharedPool:   Limits{ConcurrencyLimit: intPointer(100)},
			},
		},
	}
	prepared, err := PrepareSetting(setting)
	require.NoError(t, err)
	assert.Contains(t, prepared.Policies, "vip")

	previous := groupRateLimitSetting
	t.Cleanup(func() {
		groupRateLimitSetting = previous
		groupRateLimitSetting.PublishConfig()
	})
	groupRateLimitSetting = prepared
	groupRateLimitSetting.PublishConfig()

	snapshot = GetSettingSnapshot()
	require.NotNil(t, snapshot.Policies["vip"].MemberLimits.RPMLimit)
	assert.Equal(t, 60, *snapshot.Policies["vip"].MemberLimits.RPMLimit)
	*groupRateLimitSetting.Policies["vip"].MemberLimits.RPMLimit = 1
	assert.Equal(t, 60, *snapshot.Policies["vip"].MemberLimits.RPMLimit)
}

func TestGroupRateLimitSettingValidation(t *testing.T) {
	tests := []struct {
		name    string
		setting Setting
	}{
		{
			name: "empty group",
			setting: Setting{Policies: map[string]GroupPolicy{
				" ": {},
			}},
		},
		{
			name: "duplicate normalized group",
			setting: Setting{Policies: map[string]GroupPolicy{
				"vip":   {},
				" vip ": {},
			}},
		},
		{
			name: "zero member limit",
			setting: Setting{Policies: map[string]GroupPolicy{
				"default": {MemberLimits: Limits{RPMLimit: intPointer(0)}},
			}},
		},
		{
			name: "negative shared limit",
			setting: Setting{Policies: map[string]GroupPolicy{
				"default": {SharedPool: Limits{StreamTPSLimit: intPointer(-1)}},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Error(t, test.setting.ValidateConfig())
		})
	}
}

func TestGroupRateLimitSettingConfigManagerUpdateIsAtomic(t *testing.T) {
	manager := config.NewConfigManager()
	setting := Setting{Policies: map[string]GroupPolicy{}}
	manager.Register(ConfigName, &setting)

	handled, err := manager.Update(ConfigName, map[string]string{
		"member_enabled":      "true",
		"shared_pool_enabled": "true",
		"policies":            `{"default":{"member_limits":{"rpm_limit":60},"shared_pool":{"concurrency_limit":100}}}`,
	})
	require.True(t, handled)
	require.NoError(t, err)
	assert.True(t, setting.MemberEnabled)
	assert.True(t, setting.SharedPoolEnabled)
	require.NotNil(t, setting.Policies["default"].MemberLimits.RPMLimit)

	handled, err = manager.Update(ConfigName, map[string]string{
		"member_enabled": "false",
		"policies":       `{"default":{"member_limits":{"rpm_limit":0},"shared_pool":{}}}`,
	})
	require.True(t, handled)
	require.Error(t, err)
	assert.True(t, setting.MemberEnabled, "a rejected candidate must not partially publish")
	require.NotNil(t, setting.Policies["default"].MemberLimits.RPMLimit)
	assert.Equal(t, 60, *setting.Policies["default"].MemberLimits.RPMLimit)
}
