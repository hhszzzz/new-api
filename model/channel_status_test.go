package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

func TestUpdateChannelStatusPersistsMultiKeyState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "multi-key-status",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	changed := UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key")
	require.True(t, changed)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.NotZero(t, stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveStatusStateFromSingleKeySnapshotPreservesUnownedColumns(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:        "single-key-status",
		Key:         "original-key",
		Status:      common.ChannelStatusEnabled,
		Models:      "original-model",
		Group:       "default",
		UsedQuota:   100,
		ChannelInfo: ChannelInfo{},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)

	concurrentChannelInfo := ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 1,
	}
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"key":          "rotated-key",
		"used_quota":   gorm.Expr("used_quota + ?", 250),
		"models":       "concurrent-model",
		"channel_info": concurrentChannelInfo,
	}).Error)

	stale.Status = common.ChannelStatusManuallyDisabled
	stale.SetOtherInfo(map[string]any{
		"status_reason": "manual operation",
		"status_time":   int64(1234),
	})
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, "rotated-key", stored.Key)
	assert.Equal(t, int64(350), stored.UsedQuota)
	assert.Equal(t, "concurrent-model", stored.Models)
	assert.Equal(t, concurrentChannelInfo, stored.ChannelInfo)

	otherInfo := stored.GetOtherInfo()
	assert.Equal(t, "manual operation", otherInfo["status_reason"])
	assert.Equal(t, float64(1234), otherInfo["status_time"])
}

func TestSetChannelModelDisabledScopesToOneGroup(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "model-scope",
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Models: "m1,m2",
		Group:  "g1,g2",
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))

	abilityEnabled := func(group string, model string) bool {
		var row Ability
		require.NoError(t, DB.Where(
			"channel_id = ? AND model = ? AND "+commonGroupCol+" = ?",
			channel.Id, model, group,
		).First(&row).Error)
		return row.Enabled
	}

	require.NoError(t, SetChannelModelDisabled(channel.Id, dto.DisabledModelEntry{
		Group:  "g1",
		Model:  "m1",
		Source: dto.DisabledModelSourceManual,
		Reason: "manual operation",
	}, true))

	assert.False(t, abilityEnabled("g1", "m1"))
	assert.True(t, abilityEnabled("g1", "m2"))
	assert.True(t, abilityEnabled("g2", "m1"))
	assert.True(t, abilityEnabled("g2", "m2"))

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Len(t, reloaded.GetDisabledModels(), 1)
	assert.Equal(t, "g1", reloaded.GetDisabledModels()[0].Group)

	require.NoError(t, SetChannelModelDisabled(channel.Id, dto.DisabledModelEntry{
		Group: "g1",
		Model: "m1",
	}, false))

	assert.True(t, abilityEnabled("g1", "m1"))
	assert.True(t, abilityEnabled("g1", "m2"))
	assert.True(t, abilityEnabled("g2", "m1"))
	assert.True(t, abilityEnabled("g2", "m2"))

	reloaded, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Empty(t, reloaded.GetDisabledModels())
}

func TestUpdateAbilityStatusKeepsManuallyDisabledModel(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "model-reapply",
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Models: "m1,m2",
		Group:  "g1",
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))
	require.NoError(t, SetChannelModelDisabled(channel.Id, dto.DisabledModelEntry{
		Group:  "g1",
		Model:  "m1",
		Source: dto.DisabledModelSourceManual,
	}, true))

	abilityEnabled := func(model string) bool {
		var row Ability
		require.NoError(t, DB.Where(
			"channel_id = ? AND model = ? AND "+commonGroupCol+" = ?",
			channel.Id, model, "g1",
		).First(&row).Error)
		return row.Enabled
	}

	require.NoError(t, UpdateAbilityStatus(channel.Id, false))
	assert.False(t, abilityEnabled("m1"))
	assert.False(t, abilityEnabled("m2"))

	require.NoError(t, UpdateAbilityStatus(channel.Id, true))
	assert.False(t, abilityEnabled("m1"))
	assert.True(t, abilityEnabled("m2"))
}

func TestDisabledModelWithoutGroupSurvivesAbilityRebuild(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "model-wildcard",
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Models: "m1",
		Group:  "g1,g2",
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))
	require.NoError(t, SetChannelModelDisabled(channel.Id, dto.DisabledModelEntry{
		Model:  "m1",
		Source: dto.DisabledModelSourceAuto,
	}, true))

	abilityEnabled := func(group string) bool {
		var row Ability
		require.NoError(t, DB.Where(
			"channel_id = ? AND model = ? AND "+commonGroupCol+" = ?",
			channel.Id, "m1", group,
		).First(&row).Error)
		return row.Enabled
	}

	assert.False(t, abilityEnabled("g1"))
	assert.False(t, abilityEnabled("g2"))

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.NoError(t, reloaded.UpdateAbilities(nil))

	assert.False(t, abilityEnabled("g1"))
	assert.False(t, abilityEnabled("g2"))
}
