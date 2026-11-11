package operation_setting

import (
	"fmt"
	"math"
	"strconv"
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

type QuotaSetting struct {
	EnableFreeModelPreConsume bool    `json:"enable_free_model_pre_consume"` // 是否对免费模型启用预消耗
	TrustQuotaUSD             float64 `json:"trust_quota_usd"`               // 钱包免预扣门槛，0 表示禁用
	PreConsumeMultiplier      float64 `json:"pre_consume_multiplier"`        // 预计输入费用的预扣倍率，仅影响预留
}

// 默认配置
var quotaSetting = QuotaSetting{
	EnableFreeModelPreConsume: true,
	TrustQuotaUSD:             10,
	PreConsumeMultiplier:      1,
}

var quotaSettingSnapshot atomic.Pointer[QuotaSetting]

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("quota_setting", &quotaSetting)
	quotaSetting.PublishConfig()
}

func (setting *QuotaSetting) PublishConfig() {
	snapshot := *setting
	quotaSettingSnapshot.Store(&snapshot)
}

func GetQuotaSetting() QuotaSetting {
	return *quotaSettingSnapshot.Load()
}

// ValidateConfig checks the complete candidate before persistence and publication.
func (setting *QuotaSetting) ValidateConfig() error {
	if setting.TrustQuotaUSD < 0 || math.IsNaN(setting.TrustQuotaUSD) || math.IsInf(setting.TrustQuotaUSD, 0) {
		return fmt.Errorf("quota_setting.trust_quota_usd must be a finite non-negative number")
	}
	if setting.PreConsumeMultiplier <= 0 || math.IsNaN(setting.PreConsumeMultiplier) || math.IsInf(setting.PreConsumeMultiplier, 0) {
		return fmt.Errorf("quota_setting.pre_consume_multiplier must be a finite number greater than zero")
	}
	return nil
}

func ValidateQuotaOption(key, value string) error {
	if key != "quota_setting.trust_quota_usd" && key != "quota_setting.pre_consume_multiplier" {
		return nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return fmt.Errorf("%s must be a finite non-negative number", key)
	}
	if key == "quota_setting.pre_consume_multiplier" && number == 0 {
		return fmt.Errorf("%s must be greater than zero", key)
	}
	return nil
}

// InputPreConsumeMultiplier rejects invalid runtime settings as well as invalid saves.
func InputPreConsumeMultiplier() (float64, error) {
	multiplier := GetQuotaSetting().PreConsumeMultiplier
	if multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return 0, fmt.Errorf("pre-consume multiplier must be a finite number greater than zero")
	}
	return multiplier, nil
}
