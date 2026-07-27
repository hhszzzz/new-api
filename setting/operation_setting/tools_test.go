package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolPriceSettingRejectsNegativePrices(t *testing.T) {
	original := config.GlobalConfig.ExportAllConfigs()["tool_price_setting.prices"]
	t.Cleanup(func() {
		handled, err := config.GlobalConfig.Update("tool_price_setting", map[string]string{"prices": original})
		require.True(t, handled)
		require.NoError(t, err)
	})

	before := GetToolPriceSnapshot()
	handled, err := config.GlobalConfig.Update("tool_price_setting", map[string]string{
		"prices": `{"web_search":-1}`,
	})
	require.True(t, handled)
	require.Error(t, err)
	assert.Same(t, before, GetToolPriceSnapshot())
}

func TestToolPriceSnapshotRemainsStableAfterUpdate(t *testing.T) {
	original := config.GlobalConfig.ExportAllConfigs()["tool_price_setting.prices"]
	t.Cleanup(func() {
		handled, err := config.GlobalConfig.Update("tool_price_setting", map[string]string{"prices": original})
		require.True(t, handled)
		require.NoError(t, err)
	})

	handled, err := config.GlobalConfig.Update("tool_price_setting", map[string]string{
		"prices": `{"web_search":11}`,
	})
	require.True(t, handled)
	require.NoError(t, err)
	captured := GetToolPriceSnapshot()

	handled, err = config.GlobalConfig.Update("tool_price_setting", map[string]string{
		"prices": `{"web_search":22}`,
	})
	require.True(t, handled)
	require.NoError(t, err)

	assert.Equal(t, 11.0, captured.GetToolPrice("web_search"))
	assert.Equal(t, 22.0, GetToolPrice("web_search"))
}
