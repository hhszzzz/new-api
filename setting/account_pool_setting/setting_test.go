package account_pool_setting

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSetting() Setting {
	return Setting{
		Enabled:                   true,
		HideEmailFromNonAdmins:    true,
		ProviderGroups:            map[string][]string{},
		RegularRefreshSeconds:     300,
		NearResetThresholdSeconds: 600,
		NearResetRefreshSeconds:   60,
		PostResetDelaySeconds:     10,
		ManualRefreshCooldown:     60,
	}
}

func TestPrepareSettingNormalizesProviderGroups(t *testing.T) {
	setting := validSetting()
	setting.ProviderGroups = map[string][]string{
		"codex":       {" vip ", "codex-vip", "codex-vip"},
		"antigravity": {},
	}

	prepared, err := PrepareSetting(setting)

	require.NoError(t, err)
	assert.Equal(t, []string{"codex-vip", "vip"}, prepared.ProviderGroups["codex"])
	assert.NotContains(t, prepared.ProviderGroups, "antigravity")
}

func TestPrepareSettingNormalizesUnknownProviderGroupKeys(t *testing.T) {
	setting := validSetting()
	setting.ProviderGroups = map[string][]string{"gemini": {"vip"}}
	_, err := PrepareSetting(setting)
	require.Error(t, err)

	setting = validSetting()
	setting.ProviderGroups = map[string][]string{"claude": {"  "}}
	_, err = PrepareSetting(setting)
	require.Error(t, err)
}

func TestCanAccessProviderUsesOnlyItsOwnGroups(t *testing.T) {
	previous := GetSettingSnapshot()
	require.NotNil(t, previous)
	t.Cleanup(func() { previous.PublishConfig() })

	setting := validSetting()
	setting.ProviderGroups = map[string][]string{"claude": {"claude-team"}}
	setting.PublishConfig()

	// claude uses its own provider groups; other providers stay admin-only
	// until they get their own group configuration.
	assert.True(t, CanAccessProvider(common.RoleCommonUser, []string{"claude-team"}, ProviderClaude))
	assert.False(t, CanAccessProvider(common.RoleCommonUser, []string{"claude-team"}, ProviderCodex))
	assert.False(t, CanAccessProvider(common.RoleCommonUser, []string{"claude-team"}, ProviderAntigravity))
	assert.True(t, CanAccessProvider(common.RoleAdminUser, nil, ProviderClaude))

	// page-level access stays true when any provider matches.
	assert.True(t, CanAccess(common.RoleCommonUser, []string{"claude-team"}))
	assert.False(t, CanAccess(common.RoleCommonUser, []string{"vip"}))
}

func TestCanAccessProviderFailsClosedWhenDisabled(t *testing.T) {
	previous := GetSettingSnapshot()
	require.NotNil(t, previous)
	t.Cleanup(func() { previous.PublishConfig() })

	setting := validSetting()
	setting.Enabled = false
	setting.ProviderGroups = map[string][]string{"claude": {"claude-team"}}
	setting.PublishConfig()
	assert.False(t, CanAccessProvider(common.RoleAdminUser, []string{"claude-team"}, ProviderClaude))
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
	setting.ProviderGroups = map[string][]string{"codex": {"vip"}, "antigravity": {"team"}}
	setting.PublishConfig()

	assert.True(t, CanAccess(common.RoleCommonUser, []string{"default", "vip"}))
	assert.True(t, CanAccess(common.RoleCommonUser, []string{"default", "team"}))
	assert.False(t, CanAccess(common.RoleCommonUser, []string{"default"}))
	assert.True(t, CanAccess(common.RoleAdminUser, nil))
	assert.True(t, CanAccess(common.RoleRootUser, nil))
}

func TestCanAccessFailsClosedWhenDisabledOrProviderGroupsEmpty(t *testing.T) {
	previous := GetSettingSnapshot()
	require.NotNil(t, previous)
	t.Cleanup(func() { previous.PublishConfig() })

	setting := validSetting()
	setting.Enabled = false
	setting.PublishConfig()
	assert.False(t, CanAccess(common.RoleRootUser, []string{"vip"}))

	setting.Enabled = true
	setting.ProviderGroups = map[string][]string{}
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
		"provider_groups":            `{"codex":["vip","team"]}`,
		"regular_refresh_seconds":    "300",
		"near_reset_refresh_seconds": "60",
	})
	require.True(t, handled)
	require.NoError(t, err)
	assert.True(t, setting.Enabled)
	assert.True(t, setting.HideEmailFromNonAdmins)
	assert.Equal(t, map[string][]string{"codex": {"team", "vip"}}, setting.ProviderGroups)

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

func quotaOverrideTestID(char string) string {
	return strings.Repeat(char, 24)
}

func publishQuotaOverridesForTest(t *testing.T, overrides map[string]QuotaOverride) {
	t.Helper()
	setting := quotaOverridesSetting{Overrides: cloneQuotaOverrides(overrides)}
	setting.PublishConfig()
}

func quotaWindowOverride(offset float64, pin *QuotaPin) QuotaWindowOverride {
	return QuotaWindowOverride{OffsetPercent: offset, Pin: pin}
}

func TestPrepareQuotaOverridesValidatesAndDropsEmptyEntries(t *testing.T) {
	releaseAt := time.Now().Add(time.Hour)
	prepared, err := PrepareQuotaOverrides(map[string]QuotaOverride{
		quotaOverrideTestID("a"): {
			Primary:   quotaWindowOverride(12.5, &QuotaPin{ReleaseMode: QuotaPinReleaseAtTime, ReleaseAt: &releaseAt}),
			Secondary: quotaWindowOverride(-5, nil),
		},
		quotaOverrideTestID("b"): {},
	})

	require.NoError(t, err)
	require.Len(t, prepared, 1)
	override := prepared[quotaOverrideTestID("a")]
	require.NotNil(t, override.Primary.Pin)
	assert.Equal(t, 12.5, override.Primary.OffsetPercent)
	assert.Equal(t, QuotaPinReleaseAtTime, override.Primary.Pin.ReleaseMode)
	assert.Equal(t, -5.0, override.Secondary.OffsetPercent)
}

func TestPrepareQuotaOverridesRejectsInvalidEntries(t *testing.T) {
	releaseAt := time.Now().Add(time.Hour)
	tests := []struct {
		name     string
		publicID string
		override QuotaOverride
	}{
		{name: "invalid id", publicID: "bad-id", override: QuotaOverride{Primary: quotaWindowOverride(1, nil)}},
		{name: "primary offset too large", publicID: quotaOverrideTestID("a"), override: QuotaOverride{Primary: quotaWindowOverride(101, nil)}},
		{name: "secondary offset too small", publicID: quotaOverrideTestID("a"), override: QuotaOverride{Secondary: quotaWindowOverride(-101, nil)}},
		{name: "unknown release mode", publicID: quotaOverrideTestID("a"), override: QuotaOverride{Primary: quotaWindowOverride(0, &QuotaPin{ReleaseMode: "later"})}},
		{name: "at_time without release", publicID: quotaOverrideTestID("a"), override: QuotaOverride{Secondary: quotaWindowOverride(0, &QuotaPin{ReleaseMode: QuotaPinReleaseAtTime})}},
		{name: "refresh mode with release", publicID: quotaOverrideTestID("a"), override: QuotaOverride{Primary: quotaWindowOverride(0, &QuotaPin{ReleaseMode: QuotaPinReleaseAnyRefresh, ReleaseAt: &releaseAt})}},
		{name: "legacy fields", publicID: quotaOverrideTestID("a"), override: QuotaOverride{LegacyOffsetPercent: &releaseAt1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := PrepareQuotaOverrides(map[string]QuotaOverride{test.publicID: test.override})
			require.Error(t, err)
		})
	}
}

var releaseAt1 = 1.0

func TestQuotaOverridesLoadedConfigDropsInvalidEntries(t *testing.T) {
	releaseAt := time.Now().Add(time.Hour)
	legacyOffset := 15.0
	setting := quotaOverridesSetting{Overrides: map[string]QuotaOverride{
		quotaOverrideTestID("a"): {Primary: quotaWindowOverride(10, nil)},
		"not-a-public-id":        {Primary: quotaWindowOverride(10, nil)},
		quotaOverrideTestID("b"): {Primary: quotaWindowOverride(200, nil)},
		quotaOverrideTestID("c"): {Secondary: quotaWindowOverride(5, &QuotaPin{ReleaseMode: "invalid"})},
		quotaOverrideTestID("d"): {Primary: quotaWindowOverride(5, &QuotaPin{ReleaseMode: QuotaPinReleaseAtTime})},
		quotaOverrideTestID("e"): {},
		quotaOverrideTestID("f"): {Primary: quotaWindowOverride(0, &QuotaPin{ReleaseMode: QuotaPinReleaseAnyRefresh, ReleaseAt: &releaseAt})},
		quotaOverrideTestID("9"): {LegacyOffsetPercent: &legacyOffset, LegacyPin: &QuotaPin{ReleaseMode: QuotaPinReleaseManual}},
	}}

	require.NoError(t, setting.ValidateLoadedConfig())

	assert.Contains(t, setting.Overrides, quotaOverrideTestID("a"))
	assert.NotContains(t, setting.Overrides, "not-a-public-id")
	assert.NotContains(t, setting.Overrides, quotaOverrideTestID("b"))
	assert.Contains(t, setting.Overrides, quotaOverrideTestID("c"))
	assert.Nil(t, setting.Overrides[quotaOverrideTestID("c")].Secondary.Pin)
	assert.Equal(t, 5.0, setting.Overrides[quotaOverrideTestID("c")].Secondary.OffsetPercent)
	assert.Contains(t, setting.Overrides, quotaOverrideTestID("d"))
	assert.Nil(t, setting.Overrides[quotaOverrideTestID("d")].Primary.Pin)
	assert.Equal(t, 5.0, setting.Overrides[quotaOverrideTestID("d")].Primary.OffsetPercent)
	assert.NotContains(t, setting.Overrides, quotaOverrideTestID("e"))
	assert.Contains(t, setting.Overrides, quotaOverrideTestID("f"))
	assert.Nil(t, setting.Overrides[quotaOverrideTestID("f")].Primary.Pin.ReleaseAt)

	legacy, ok := setting.Overrides[quotaOverrideTestID("9")]
	require.True(t, ok)
	assert.Equal(t, 15.0, legacy.Primary.OffsetPercent)
	assert.Equal(t, 15.0, legacy.Secondary.OffsetPercent)
	assert.Equal(t, QuotaPinReleaseManual, legacy.Primary.Pin.ReleaseMode)
	assert.NotNil(t, legacy.Secondary.Pin)
	assert.Nil(t, legacy.LegacyOffsetPercent)
	assert.Nil(t, legacy.LegacyPin)
}

func TestGetQuotaOverridesReturnsDeepCopy(t *testing.T) {
	previous := GetQuotaOverrides()
	t.Cleanup(func() { publishQuotaOverridesForTest(t, previous) })

	releaseAt := time.Now().Add(time.Hour)
	publicID := quotaOverrideTestID("a")
	publishQuotaOverridesForTest(t, map[string]QuotaOverride{
		publicID: {
			Primary:   quotaWindowOverride(20, &QuotaPin{ReleaseMode: QuotaPinReleaseAtTime, ReleaseAt: &releaseAt}),
			Secondary: quotaWindowOverride(5, nil),
		},
	})

	overrides := GetQuotaOverrides()
	override := overrides[publicID]
	*override.Primary.Pin.ReleaseAt = releaseAt.Add(time.Hour)
	override.Primary.OffsetPercent = 80
	override.Secondary.OffsetPercent = 90

	stored, ok := GetQuotaOverride(publicID)
	require.True(t, ok)
	assert.Equal(t, 20.0, stored.Primary.OffsetPercent)
	assert.Equal(t, releaseAt, *stored.Primary.Pin.ReleaseAt)
	assert.Equal(t, 5.0, stored.Secondary.OffsetPercent)
}

func TestQuotaPinReleaseSemantics(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	assert.False(t, (*QuotaPin)(nil).ActiveAt(now))
	assert.False(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseAtTime, ReleaseAt: &past}).ActiveAt(now))
	assert.True(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseAtTime, ReleaseAt: &future}).ActiveAt(now))
	assert.True(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseAuto}).ActiveAt(now))

	assert.True(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseAuto}).ReleasedByRefresh(QuotaRefreshAuto))
	assert.False(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseAuto}).ReleasedByRefresh(QuotaRefreshManual))
	assert.True(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseManual}).ReleasedByRefresh(QuotaRefreshManual))
	assert.True(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseAnyRefresh}).ReleasedByRefresh(QuotaRefreshAuto))
	assert.True(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseAnyRefresh}).ReleasedByRefresh(QuotaRefreshManual))
	assert.False(t, (&QuotaPin{ReleaseMode: QuotaPinReleaseAtTime, ReleaseAt: &future}).ReleasedByRefresh(QuotaRefreshAuto))
}
