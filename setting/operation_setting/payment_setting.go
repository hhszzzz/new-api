package operation_setting

import (
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

type PaymentSetting struct {
	AmountOptions  []int           `json:"amount_options"`
	AmountDiscount map[int]float64 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`
}

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
}

var paymentSettingSnapshot atomic.Pointer[PaymentSetting]

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
	paymentSetting.PublishConfig()
}

func (setting *PaymentSetting) PublishConfig() {
	snapshot := clonePaymentSetting(*setting)
	paymentSettingSnapshot.Store(&snapshot)
}

func clonePaymentSetting(setting PaymentSetting) PaymentSetting {
	amountDiscount := setting.AmountDiscount
	setting.AmountOptions = append([]int(nil), setting.AmountOptions...)
	setting.AmountDiscount = make(map[int]float64, len(amountDiscount))
	for amount, discount := range amountDiscount {
		setting.AmountDiscount[amount] = discount
	}
	return setting
}

func getPaymentSettingSnapshot() *PaymentSetting {
	return paymentSettingSnapshot.Load()
}

// GetPaymentSetting returns a mutable copy so callers cannot alter the
// published runtime snapshot.
func GetPaymentSetting() PaymentSetting {
	return clonePaymentSetting(*getPaymentSettingSnapshot())
}

func GetTopUpDiscount(amount int) (float64, bool) {
	discount, ok := getPaymentSettingSnapshot().AmountDiscount[amount]
	return discount, ok
}

func IsPaymentComplianceConfirmed() bool {
	snapshot := getPaymentSettingSnapshot()
	return snapshot.ComplianceConfirmed &&
		snapshot.ComplianceTermsVersion == CurrentComplianceTermsVersion
}
