package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRadarAutoEffortSetting(t *testing.T) {
	t.Run("empty setting stays absent", func(t *testing.T) {
		setting, err := NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{})
		require.NoError(t, err)
		assert.Nil(t, setting)
	})

	t.Run("invalid policy is rejected", func(t *testing.T) {
		_, err := NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{
			Policy: "cheapest",
			Models: map[string]RadarAutoEffortModel{"gpt-test": {Enabled: true}},
		})
		require.Error(t, err)

		_, err = NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{
			Models: map[string]RadarAutoEffortModel{"gpt-test": {Enabled: true, Policy: "cheapest"}},
		})
		require.Error(t, err)
	})

	t.Run("minimum iq delta is bounded", func(t *testing.T) {
		_, err := NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{
			MinIQDelta: -1,
			Models:     map[string]RadarAutoEffortModel{"gpt-test": {Enabled: true}},
		})
		require.Error(t, err)

		_, err = NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{
			MinIQDelta: RadarAutoEffortMaxMinIQDelta + 1,
			Models:     map[string]RadarAutoEffortModel{"gpt-test": {Enabled: true}},
		})
		require.Error(t, err)
	})

	t.Run("models are normalized and disabled entries dropped", func(t *testing.T) {
		setting, err := NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{
			Policy: "IQ_PER_COST",
			Models: map[string]RadarAutoEffortModel{
				" GPT-Test ": {Enabled: true, Policy: "Min_IQ_Delta"},
				"gpt-idle":   {},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, setting)
		assert.Equal(t, RadarAutoEffortPolicyIQPerCost, setting.Policy)
		assert.Equal(t, RadarAutoEffortDefaultMinIQDelta, setting.MinIQDelta)
		assert.Equal(t, map[string]RadarAutoEffortModel{
			"gpt-test": {Enabled: true, Policy: RadarAutoEffortPolicyMinIQDelta},
		}, setting.Models)
		assert.True(t, setting.EnabledModel("GPT-Test"))
		assert.False(t, setting.EnabledModel("gpt-idle"))
		assert.Equal(t, RadarAutoEffortPolicyMinIQDelta, setting.EffectivePolicy("gpt-test"))
		assert.Equal(t, RadarAutoEffortPolicyIQPerCost, setting.EffectivePolicy("gpt-other"))
	})

	t.Run("case-insensitive duplicate models are rejected", func(t *testing.T) {
		_, err := NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{
			Models: map[string]RadarAutoEffortModel{
				"gpt-test": {Enabled: true},
				"GPT-Test": {Enabled: true},
			},
		})
		require.Error(t, err)
	})

	t.Run("a chosen strategy survives with every model switched off", func(t *testing.T) {
		setting, err := NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{
			Policy:     RadarAutoEffortPolicyIQPerCost,
			MinIQDelta: 30,
			Models:     map[string]RadarAutoEffortModel{"gpt-test": {Enabled: false}},
		})
		require.NoError(t, err)
		require.NotNil(t, setting)
		assert.Equal(t, RadarAutoEffortPolicyIQPerCost, setting.Policy)
		assert.Equal(t, 30.0, setting.MinIQDelta)
		assert.Empty(t, setting.Models)

		setting, err = NormalizeRadarAutoEffortSetting(RadarAutoEffortSetting{
			MinIQDelta: 30,
			Models:     map[string]RadarAutoEffortModel{"gpt-test": {Enabled: false}},
		})
		require.NoError(t, err)
		assert.Nil(t, setting)
	})
}
