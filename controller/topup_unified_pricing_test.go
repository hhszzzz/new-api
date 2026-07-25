package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedTopupPricingIsSharedByEveryPaymentProvider(t *testing.T) {
	originalPrice := operation_setting.Price
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		operation_setting.Price = originalPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	operation_setting.Price = 2.5
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{10: 0.8}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":9.9}`))

	expected := 20.0
	assert.InDelta(t, expected, getPayMoney(10, "default"), 0.000001)
	assert.InDelta(t, expected, getPayMoney(10, "vip"), 0.000001)
	assert.InDelta(t, expected, getStripePayMoney(10, "vip"), 0.000001)
	assert.InDelta(t, expected, getWaffoPayMoney(10, "vip"), 0.000001)
	assert.InDelta(t, expected, getWaffoPancakePayMoney(10, "vip"), 0.000001)
	require.NoError(t, validateCreemProductPrice(CreemProduct{
		Quota: int64(common.QuotaPerUnit * 10),
		Price: expected,
	}))
	require.Error(t, validateCreemProductPrice(CreemProduct{
		Quota: int64(common.QuotaPerUnit * 10),
		Price: expected + 1,
	}))
}

func TestUnifiedTopupPricingPreservesTokenDisplayDiscounts(t *testing.T) {
	originalPrice := operation_setting.Price
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	t.Cleanup(func() {
		operation_setting.Price = originalPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
	})

	requestedTokens := int64(common.QuotaPerUnit * 3)
	operation_setting.Price = 2.5
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{
		int(requestedTokens): 0.5,
	}

	expected := 3.75
	assert.InDelta(t, expected, getUnifiedTopupPayMoney(float64(requestedTokens)), 0.000001)
	assert.InDelta(t, expected, getStripePayMoney(float64(requestedTokens), "ignored"), 0.000001)
	assert.InDelta(t, expected, getWaffoPayMoney(float64(requestedTokens), "ignored"), 0.000001)
	assert.InDelta(t, expected, getWaffoPancakePayMoney(requestedTokens, "ignored"), 0.000001)
	require.NoError(t, validateCreemProductPrice(CreemProduct{
		Quota: requestedTokens,
		Price: expected,
	}))
}
