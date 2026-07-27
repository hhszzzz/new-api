package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingSnapshotBulkUpdatePublishesAllMapsTogether(t *testing.T) {
	previous := GetPricingSnapshot()
	t.Cleanup(func() {
		pricingSnapshot.Store(previous)
		InvalidateExposedDataCache()
	})

	require.NoError(t, UpdatePricingOptionsByJSONString(map[string]string{
		ModelPriceOptionKey: `{"atomic-model":1}`,
		ModelRatioOptionKey: `{"atomic-model":2}`,
	}))
	before := GetPricingSnapshot()
	require.NoError(t, UpdatePricingOptionsByJSONString(map[string]string{
		ModelPriceOptionKey: `{"atomic-model":3}`,
		ModelRatioOptionKey: `{"atomic-model":4}`,
	}))
	after := GetPricingSnapshot()

	beforePrice, beforePriceOK := before.GetModelPrice("atomic-model", false)
	beforeRatio, beforeRatioOK, _ := before.GetModelRatio("atomic-model")
	afterPrice, afterPriceOK := after.GetModelPrice("atomic-model", false)
	afterRatio, afterRatioOK, _ := after.GetModelRatio("atomic-model")
	assert.True(t, beforePriceOK)
	assert.True(t, beforeRatioOK)
	assert.Equal(t, 1.0, beforePrice)
	assert.Equal(t, 2.0, beforeRatio)
	assert.True(t, afterPriceOK)
	assert.True(t, afterRatioOK)
	assert.Equal(t, 3.0, afterPrice)
	assert.Equal(t, 4.0, afterRatio)
}

func TestPricingSnapshotRejectsInvalidValuesWithoutPublishing(t *testing.T) {
	previous := GetPricingSnapshot()

	err := UpdatePricingOptionsByJSONString(map[string]string{
		ModelPriceOptionKey: `{"invalid-model":-1}`,
	})

	require.Error(t, err)
	assert.Same(t, previous, GetPricingSnapshot())
}
