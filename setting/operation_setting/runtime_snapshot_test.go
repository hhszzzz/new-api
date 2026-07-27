package operation_setting

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentSettingPublishesImmutableRuntimeSnapshot(t *testing.T) {
	original := GetPaymentSetting()
	originalOptions, err := common.Marshal(original.AmountOptions)
	require.NoError(t, err)
	originalDiscounts, err := common.Marshal(original.AmountDiscount)
	require.NoError(t, err)
	t.Cleanup(func() {
		handled, updateErr := config.GlobalConfig.Update("payment_setting", map[string]string{
			"amount_options":           string(originalOptions),
			"amount_discount":          string(originalDiscounts),
			"compliance_confirmed":     strconv.FormatBool(original.ComplianceConfirmed),
			"compliance_terms_version": original.ComplianceTermsVersion,
			"compliance_confirmed_at":  strconv.FormatInt(original.ComplianceConfirmedAt, 10),
			"compliance_confirmed_by":  strconv.Itoa(original.ComplianceConfirmedBy),
			"compliance_confirmed_ip":  original.ComplianceConfirmedIP,
		})
		require.NoError(t, updateErr)
		require.True(t, handled)
	})

	handled, err := config.GlobalConfig.Update("payment_setting", map[string]string{
		"amount_options":           `[25,50]`,
		"amount_discount":          `{"25":0.75}`,
		"compliance_confirmed":     "true",
		"compliance_terms_version": CurrentComplianceTermsVersion,
	})
	require.NoError(t, err)
	require.True(t, handled)

	callerCopy := GetPaymentSetting()
	callerCopy.AmountOptions[0] = 999
	callerCopy.AmountDiscount[25] = 0.1

	published := GetPaymentSetting()
	assert.Equal(t, []int{25, 50}, published.AmountOptions)
	assert.Equal(t, map[int]float64{25: 0.75}, published.AmountDiscount)
	assert.True(t, IsPaymentComplianceConfirmed())
	discount, ok := GetTopUpDiscount(25)
	assert.True(t, ok)
	assert.Equal(t, 0.75, discount)
}

func TestGeneralSettingPublishesRuntimeSnapshot(t *testing.T) {
	original := GetGeneralSetting()
	t.Cleanup(func() {
		handled, updateErr := config.GlobalConfig.Update("general_setting", map[string]string{
			"ping_interval_enabled":         strconv.FormatBool(original.PingIntervalEnabled),
			"ping_interval_seconds":         strconv.Itoa(original.PingIntervalSeconds),
			"quota_display_type":            original.QuotaDisplayType,
			"custom_currency_symbol":        original.CustomCurrencySymbol,
			"custom_currency_exchange_rate": strconv.FormatFloat(original.CustomCurrencyExchangeRate, 'f', -1, 64),
		})
		require.NoError(t, updateErr)
		require.True(t, handled)
	})

	handled, err := config.GlobalConfig.Update("general_setting", map[string]string{
		"ping_interval_enabled":         "true",
		"ping_interval_seconds":         "3",
		"quota_display_type":            QuotaDisplayTypeCustom,
		"custom_currency_symbol":        "C",
		"custom_currency_exchange_rate": "2.5",
	})
	require.NoError(t, err)
	require.True(t, handled)

	callerCopy := GetGeneralSetting()
	callerCopy.QuotaDisplayType = QuotaDisplayTypeTokens

	published := GetGeneralSetting()
	assert.True(t, published.PingIntervalEnabled)
	assert.Equal(t, 3, published.PingIntervalSeconds)
	assert.Equal(t, QuotaDisplayTypeCustom, published.QuotaDisplayType)
	assert.Equal(t, "C", GetCurrencySymbol())
	assert.Equal(t, 2.5, GetUsdToCurrencyRate(7.3))
}
