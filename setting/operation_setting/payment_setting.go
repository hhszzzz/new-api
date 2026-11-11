package operation_setting

import (
	"strings"
	"sync/atomic"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
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

	// 易支付签名方式：MD5 使用 EpayKey；RSA 使用商户私钥签名请求、平台公钥验证回调。
	EpaySignType           string `json:"epay_sign_type"`
	EpayMerchantPrivateKey string `json:"epay_merchant_private_key"`
	EpayPlatformPublicKey  string `json:"epay_platform_public_key"`
}

const CurrentComplianceTermsVersion = "v1"

const (
	EpaySignTypeMD5 = "MD5"
	EpaySignTypeRSA = "RSA"

	EpaySignTypeOptionKey           = "payment_setting.epay_sign_type"
	EpayMerchantPrivateKeyOptionKey = "payment_setting.epay_merchant_private_key"
	EpayPlatformPublicKeyOptionKey  = "payment_setting.epay_platform_public_key"
)

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
	// 没有保存过签名方式的站点按 MD5 运行，升级后行为不变。
	EpaySignType: EpaySignTypeMD5,
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

// ValidateEpaySignOption checks a change to the Epay signing settings against
// the saved ones. While RSA is active, both keys must parse; under MD5 only a
// newly entered key is checked.
func ValidateEpaySignOption(key, value string) error {
	if key != EpaySignTypeOptionKey && key != EpayMerchantPrivateKeyOptionKey && key != EpayPlatformPublicKeyOptionKey {
		return nil
	}
	return ValidateEpaySignOptionsUpdate(map[string]string{strings.TrimPrefix(key, "payment_setting."): value})
}

func ValidateEpaySignOptionsUpdate(fields map[string]string) error {
	setting := GetPaymentSetting()
	privateKeyUpdated := false
	publicKeyUpdated := false
	for key, value := range fields {
		switch key {
		case "epay_sign_type":
			setting.EpaySignType = value
		case "epay_merchant_private_key":
			setting.EpayMerchantPrivateKey = value
			privateKeyUpdated = true
		case "epay_platform_public_key":
			setting.EpayPlatformPublicKey = value
			publicKeyUpdated = true
		}
	}
	return validateEpaySignSettings(setting.EpaySignType, setting.EpayMerchantPrivateKey, setting.EpayPlatformPublicKey, privateKeyUpdated, publicKeyUpdated)
}

func validateEpaySignSettings(signType, privateKey, publicKey string, privateKeyUpdated, publicKeyUpdated bool) error {
	if signType != EpaySignTypeMD5 && signType != EpaySignTypeRSA {
		return common.NewMessage("Epay signature type must be MD5 or RSA")
	}
	rsaActive := signType == EpaySignTypeRSA
	if rsaActive || (privateKeyUpdated && strings.TrimSpace(privateKey) != "") {
		if _, err := epay.ParseRSAPrivateKey(privateKey); err != nil {
			return common.NewMessage("The Epay merchant private key is missing or invalid")
		}
	}
	if rsaActive || (publicKeyUpdated && strings.TrimSpace(publicKey) != "") {
		if _, err := epay.ParseRSAPublicKey(publicKey); err != nil {
			return common.NewMessage("The Epay platform public key is missing or invalid")
		}
	}
	return nil
}
