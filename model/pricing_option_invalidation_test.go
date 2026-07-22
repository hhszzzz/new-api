package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyPricingOptionsInvalidatePricingImmediately(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	resetPricingEndpointTestTables(t)
	const modelName = "legacy-pricing-cache-model"
	insertPricingCacheChannel(t, 541, modelName)

	tests := []struct {
		key      string
		original string
		value    func(Pricing) (float64, bool)
	}{
		{
			key:      "ModelRatio",
			original: ratio_setting.ModelRatio2JSONString(),
			value: func(pricing Pricing) (float64, bool) {
				return pricing.ModelRatio, true
			},
		},
		{
			key:      "CompletionRatio",
			original: ratio_setting.CompletionRatio2JSONString(),
			value: func(pricing Pricing) (float64, bool) {
				return pricing.CompletionRatio, true
			},
		},
		{
			key:      "ModelPrice",
			original: ratio_setting.ModelPrice2JSONString(),
			value: func(pricing Pricing) (float64, bool) {
				return pricing.ModelPrice, pricing.QuotaType == 1
			},
		},
		{
			key:      "CacheRatio",
			original: ratio_setting.CacheRatio2JSONString(),
			value: func(pricing Pricing) (float64, bool) {
				if pricing.CacheRatio == nil {
					return 0, false
				}
				return *pricing.CacheRatio, true
			},
		},
		{
			key:      "CreateCacheRatio",
			original: ratio_setting.CreateCacheRatio2JSONString(),
			value: func(pricing Pricing) (float64, bool) {
				if pricing.CreateCacheRatio == nil {
					return 0, false
				}
				return *pricing.CreateCacheRatio, true
			},
		},
		{
			key:      "ImageRatio",
			original: ratio_setting.ImageRatio2JSONString(),
			value: func(pricing Pricing) (float64, bool) {
				if pricing.ImageRatio == nil {
					return 0, false
				}
				return *pricing.ImageRatio, true
			},
		},
		{
			key:      "AudioRatio",
			original: ratio_setting.AudioRatio2JSONString(),
			value: func(pricing Pricing) (float64, bool) {
				if pricing.AudioRatio == nil {
					return 0, false
				}
				return *pricing.AudioRatio, true
			},
		},
		{
			key:      "AudioCompletionRatio",
			original: ratio_setting.AudioCompletionRatio2JSONString(),
			value: func(pricing Pricing) (float64, bool) {
				if pricing.AudioCompletionRatio == nil {
					return 0, false
				}
				return *pricing.AudioCompletionRatio, true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			t.Cleanup(func() {
				require.NoError(t, updateOptionMap(test.key, test.original))
			})

			require.NoError(t, updateOptionMap(test.key, `{"`+modelName+`":1.25}`))
			initial := GetPricing()
			require.Len(t, initial, 1)
			initialValue, ok := test.value(initial[0])
			require.True(t, ok)
			assert.Equal(t, 1.25, initialValue)

			require.NoError(t, updateOptionMap(test.key, `{"`+modelName+`":2.5}`))
			updated := GetPricing()
			require.Len(t, updated, 1)
			updatedValue, ok := test.value(updated[0])
			require.True(t, ok)
			assert.Equal(t, 2.5, updatedValue)
		})
	}
}
