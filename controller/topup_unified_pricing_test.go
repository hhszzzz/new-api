package controller

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTopUpPricingSettingsForTest(t *testing.T, price float64, displayType string, discounts map[int]float64) {
	t.Helper()
	operation_setting.SetPrice(price)
	discountsJSON, err := common.Marshal(discounts)
	require.NoError(t, err)
	handled, err := config.GlobalConfig.Update("general_setting", map[string]string{
		"quota_display_type": displayType,
	})
	require.NoError(t, err)
	require.True(t, handled)
	handled, err = config.GlobalConfig.Update("payment_setting", map[string]string{
		"amount_discount": string(discountsJSON),
	})
	require.NoError(t, err)
	require.True(t, handled)
}

func TestUnifiedTopupPricingIsSharedByEveryPaymentProvider(t *testing.T) {
	originalPrice := operation_setting.GetPrice()
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		setTopUpPricingSettingsForTest(t, originalPrice, originalDisplayType, originalDiscounts)
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setTopUpPricingSettingsForTest(t, 2.5, operation_setting.QuotaDisplayTypeUSD, map[int]float64{10: 0.8})
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":9.9}`))

	expected := 20.0
	assert.InDelta(t, expected, getPayMoney(10, "default"), 0.000001)
	assert.InDelta(t, expected, getPayMoney(10, "vip"), 0.000001)
	assert.InDelta(t, expected, getStripePayMoney(10, "vip"), 0.000001)
	assert.InDelta(t, expected, getWaffoPayMoney(10, "vip"), 0.000001)
	assert.InDelta(t, expected, getWaffoPancakePayMoney(10, "vip"), 0.000001)
	require.NoError(t, validateCreemProductPrice(CreemProduct{
		Quota: int64(common.GetQuotaPerUnit() * 10),
		Price: expected,
	}))
	require.Error(t, validateCreemProductPrice(CreemProduct{
		Quota: int64(common.GetQuotaPerUnit() * 10),
		Price: expected + 1,
	}))
}

func TestUnifiedTopupPricingPreservesTokenDisplayDiscounts(t *testing.T) {
	originalPrice := operation_setting.GetPrice()
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	t.Cleanup(func() {
		setTopUpPricingSettingsForTest(t, originalPrice, originalDisplayType, originalDiscounts)
	})

	requestedTokens := int64(common.GetQuotaPerUnit() * 3)
	setTopUpPricingSettingsForTest(t, 2.5, operation_setting.QuotaDisplayTypeTokens, map[int]float64{
		int(requestedTokens): 0.5,
	})

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

func TestQuoteTopUpSnapshotsCurrencyAndTokenAmounts(t *testing.T) {
	originalPrice := operation_setting.GetPrice()
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	t.Cleanup(func() {
		setTopUpPricingSettingsForTest(t, originalPrice, originalDisplayType, originalDiscounts)
	})

	setTopUpPricingSettingsForTest(t, 2.5, operation_setting.QuotaDisplayTypeUSD, map[int]float64{10: 0.8})

	currencyQuote, err := quoteTopUp(10)
	require.NoError(t, err)
	assert.Equal(t, common.QuotaFromFloat(10*common.GetQuotaPerUnit()), currencyQuote.Quota)
	assert.Equal(t, int64(10), currencyQuote.LegacyAmount)
	assert.InDelta(t, 20, currencyQuote.PayMoney, 0.000001)

	requestedTokens := int64(common.GetQuotaPerUnit() * 3)
	setTopUpPricingSettingsForTest(t, 2.5, operation_setting.QuotaDisplayTypeTokens, map[int]float64{int(requestedTokens): 0.5})

	tokenQuote, err := quoteTopUp(requestedTokens)
	require.NoError(t, err)
	assert.Equal(t, int(requestedTokens), tokenQuote.Quota)
	assert.Equal(t, int64(3), tokenQuote.LegacyAmount)
	assert.InDelta(t, 3.75, tokenQuote.PayMoney, 0.000001)
	assert.Equal(t, int64(common.GetQuotaPerUnit()), getTopUpMinimum(1))
}

func TestQuoteTopUpRejectsInvalidOrUnrepresentableQuota(t *testing.T) {
	originalPrice := operation_setting.GetPrice()
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	t.Cleanup(func() {
		setTopUpPricingSettingsForTest(t, originalPrice, originalDisplayType, originalDiscounts)
	})

	setTopUpPricingSettingsForTest(t, 1, operation_setting.QuotaDisplayTypeTokens, originalDiscounts)

	_, err := quoteTopUp(0)
	require.Error(t, err)
	_, err = quoteTopUp(int64(common.MaxQuota))
	require.Error(t, err)

	operation_setting.SetPrice(0)
	_, err = quoteTopUp(int64(common.GetQuotaPerUnit()))
	require.ErrorContains(t, err, "价格配置无效")

	operation_setting.SetPrice(math.MaxFloat64)
	_, err = quoteTopUp(int64(common.GetQuotaPerUnit()))
	require.ErrorContains(t, err, "金额配置无效")
}
