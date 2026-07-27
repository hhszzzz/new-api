package operation_setting

import (
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

type QuotaSetting struct {
	EnableFreeModelPreConsume bool `json:"enable_free_model_pre_consume"` // 是否对免费模型启用预消耗
}

// 默认配置
var quotaSetting = QuotaSetting{
	EnableFreeModelPreConsume: true,
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
