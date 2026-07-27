package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopUpSettlementUsesSnapshotAndIsIdempotent(t *testing.T) {
	testCases := []struct {
		name             string
		provider         string
		paymentMethod    string
		settle           func(string) error
		assertUserFields func(*testing.T, User)
	}{
		{
			name:          "epay",
			provider:      PaymentProviderEpay,
			paymentMethod: "alipay",
			settle: func(tradeNo string) error {
				return RechargeEpay(tradeNo, "wxpay", "77.70", "127.0.0.1")
			},
		},
		{
			name:          "stripe",
			provider:      PaymentProviderStripe,
			paymentMethod: PaymentMethodStripe,
			settle: func(tradeNo string) error {
				return Recharge(tradeNo, "cus_snapshot", "127.0.0.1")
			},
			assertUserFields: func(t *testing.T, user User) {
				assert.Equal(t, "cus_snapshot", user.StripeCustomer)
			},
		},
		{
			name:          "creem",
			provider:      PaymentProviderCreem,
			paymentMethod: PaymentMethodCreem,
			settle: func(tradeNo string) error {
				return RechargeCreem(tradeNo, "paid@example.com", "Paid User", "127.0.0.1")
			},
			assertUserFields: func(t *testing.T, user User) {
				assert.Equal(t, "paid@example.com", user.Email)
			},
		},
		{
			name:          "waffo",
			provider:      PaymentProviderWaffo,
			paymentMethod: PaymentMethodWaffo,
			settle: func(tradeNo string) error {
				return RechargeWaffo(tradeNo, "127.0.0.1")
			},
		},
		{
			name:          "waffo pancake",
			provider:      PaymentProviderWaffoPancake,
			paymentMethod: PaymentMethodWaffoPancake,
			settle:        RechargeWaffoPancake,
		},
	}

	for index, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			userID := 700 + index
			tradeNo := "snapshot-" + tc.provider
			user := &User{
				Id:       userID,
				Username: "snapshot_user",
				Status:   common.UserStatusEnabled,
				Quota:    100,
			}
			require.NoError(t, DB.Create(user).Error)
			topUp := &TopUp{
				UserId:          userID,
				Amount:          999,
				Quota:           12345,
				Money:           77.7,
				TradeNo:         tradeNo,
				PaymentMethod:   tc.paymentMethod,
				PaymentProvider: tc.provider,
				Status:          common.TopUpStatusPending,
				CreateTime:      common.GetTimestamp(),
			}
			require.NoError(t, topUp.Insert())

			require.NoError(t, tc.settle(tradeNo))
			require.NoError(t, tc.settle(tradeNo))

			var storedUser User
			require.NoError(t, DB.First(&storedUser, userID).Error)
			assert.Equal(t, 12445, storedUser.Quota)
			if tc.assertUserFields != nil {
				tc.assertUserFields(t, storedUser)
			}
			storedTopUp := GetTopUpByTradeNo(tradeNo)
			require.NotNil(t, storedTopUp)
			assert.Equal(t, common.TopUpStatusSuccess, storedTopUp.Status)
			assert.NotZero(t, storedTopUp.CompleteTime)
			if tc.provider == PaymentProviderEpay {
				assert.Equal(t, "wxpay", storedTopUp.PaymentMethod)
			}
		})
	}
}

func TestTopUpSettlementRollsBackWhenUserQuotaWouldOverflow(t *testing.T) {
	truncateTables(t)
	user := &User{
		Id:       800,
		Username: "overflow_user",
		Status:   common.UserStatusEnabled,
		Quota:    common.MaxQuota - 5,
	}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId:          user.Id,
		Amount:          1,
		Quota:           10,
		Money:           1,
		TradeNo:         "overflow-epay",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	require.Error(t, RechargeEpay(topUp.TradeNo, "alipay", "1.00", "127.0.0.1"))

	var storedUser User
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.Equal(t, common.MaxQuota-5, storedUser.Quota)
	storedTopUp := GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, storedTopUp)
	assert.Equal(t, common.TopUpStatusPending, storedTopUp.Status)
}

func TestRechargeEpayRejectsPaidAmountMismatch(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 801, Username: "amount_user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId:          user.Id,
		Amount:          10,
		Quota:           1000,
		Money:           20,
		TradeNo:         "amount-mismatch-epay",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	require.Error(t, RechargeEpay(topUp.TradeNo, "alipay", "19.99", "127.0.0.1"))

	storedTopUp := GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, storedTopUp)
	assert.Equal(t, common.TopUpStatusPending, storedTopUp.Status)
	assert.Zero(t, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestResolveTopUpQuotaSupportsLegacyOrders(t *testing.T) {
	originalQuotaPerUnit := common.GetQuotaPerUnit()
	t.Cleanup(func() {
		require.NoError(t, common.SetQuotaPerUnit(originalQuotaPerUnit))
	})
	require.NoError(t, common.SetQuotaPerUnit(100))

	testCases := []struct {
		name     string
		topUp    TopUp
		expected int
	}{
		{name: "snapshot", topUp: TopUp{Quota: 321, Amount: 9, PaymentProvider: PaymentProviderEpay}, expected: 321},
		{name: "legacy epay", topUp: TopUp{Amount: 9, PaymentProvider: PaymentProviderEpay}, expected: 900},
		{name: "legacy stripe", topUp: TopUp{Money: 9.5, PaymentProvider: PaymentProviderStripe}, expected: 950},
		{name: "legacy creem", topUp: TopUp{Amount: 900, PaymentProvider: PaymentProviderCreem}, expected: 900},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			quota, err := resolveTopUpQuota(&tc.topUp)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, quota)
		})
	}
}
