package ratio_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigManagerGroupRatioUpdatePublishesSnapshot(t *testing.T) {
	original := config.GlobalConfig.ExportAllConfigs()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(original))
	})

	previous := GetGroupRatioSetting()
	handled, err := config.GlobalConfig.Update("group_ratio_setting", map[string]string{
		"group_ratio": `{"snapshot-group":2.5}`,
	})
	require.True(t, handled)
	require.NoError(t, err)

	current := GetGroupRatioSetting()
	require.NotSame(t, previous, current)
	assert.Equal(t, 2.5, GetGroupRatio("snapshot-group"))
	assert.NotContains(t, previous.GroupRatio.ReadAll(), "snapshot-group")
}

func TestLegacyGroupRatioUpdatePublishesSnapshot(t *testing.T) {
	originalGroupRatios := GroupRatio2JSONString()
	originalGroupGroupRatios := GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, UpdateGroupGroupRatioByJSONString(originalGroupGroupRatios))
	})

	previous := GetGroupRatioSetting()
	require.NoError(t, UpdateGroupRatioByJSONString(`{"legacy-snapshot-group":3}`))

	current := GetGroupRatioSetting()
	require.NotSame(t, previous, current)
	assert.Equal(t, 3.0, GetGroupRatio("legacy-snapshot-group"))
	assert.NotContains(t, previous.GroupRatio.ReadAll(), "legacy-snapshot-group")

	previous = current
	require.NoError(t, UpdateGroupGroupRatioByJSONString(`{"vip":{"legacy-snapshot-group":0.5}}`))

	current = GetGroupRatioSetting()
	require.NotSame(t, previous, current)
	previousVipRatios, exists := previous.GroupGroupRatio.Get("vip")
	require.True(t, exists)
	assert.NotContains(t, previousVipRatios, "legacy-snapshot-group")
	ratio, exists := GetGroupGroupRatio("vip", "legacy-snapshot-group")
	assert.True(t, exists)
	assert.Equal(t, 0.5, ratio)
}
