package dto

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Radar auto-effort selection strategies.
const (
	RadarAutoEffortPolicyHighestIQ  = "highest_iq"
	RadarAutoEffortPolicyIQPerCost  = "iq_per_cost"
	RadarAutoEffortPolicyMinIQDelta = "min_iq_delta"

	RadarAutoEffortDefaultMinIQDelta = float64(5)
	RadarAutoEffortMaxMinIQDelta     = float64(150)

	radarAutoEffortMaxModels         = 256
	radarAutoEffortMaxModelNameRunes = 128
)

// RadarAutoEffortModel is the per-model switch of the radar auto-effort setting.
type RadarAutoEffortModel struct {
	Enabled bool   `json:"enabled"`
	Policy  string `json:"policy,omitempty"` // optional per-model strategy override
}

// RadarAutoEffortSetting holds a user's radar-driven reasoning-tier
// preferences. Models is keyed by the radar model name in lowercase.
type RadarAutoEffortSetting struct {
	Policy     string                          `json:"policy,omitempty"`       // empty means highest_iq
	MinIQDelta float64                         `json:"min_iq_delta,omitempty"` // empty means the default threshold
	Models     map[string]RadarAutoEffortModel `json:"models,omitempty"`
}

// IsRadarAutoEffortPolicy reports whether policy names a supported strategy.
func IsRadarAutoEffortPolicy(policy string) bool {
	switch policy {
	case RadarAutoEffortPolicyHighestIQ, RadarAutoEffortPolicyIQPerCost, RadarAutoEffortPolicyMinIQDelta:
		return true
	default:
		return false
	}
}

// NormalizeRadarAutoEffortSetting validates and canonicalizes a user-supplied
// setting. It returns nil for a setting without any enabled model so the stored
// payload stays compact.
func NormalizeRadarAutoEffortSetting(in RadarAutoEffortSetting) (*RadarAutoEffortSetting, error) {
	policy := strings.ToLower(strings.TrimSpace(in.Policy))
	if policy == "" {
		policy = RadarAutoEffortPolicyHighestIQ
	}
	if !IsRadarAutoEffortPolicy(policy) {
		return nil, fmt.Errorf("unsupported radar auto-effort policy %q", in.Policy)
	}
	minIQDelta := in.MinIQDelta
	if math.IsNaN(minIQDelta) || math.IsInf(minIQDelta, 0) || minIQDelta < 0 || minIQDelta > RadarAutoEffortMaxMinIQDelta {
		return nil, fmt.Errorf("radar auto-effort minimum IQ delta must be between 0 and %v", RadarAutoEffortMaxMinIQDelta)
	}
	if minIQDelta == 0 {
		minIQDelta = RadarAutoEffortDefaultMinIQDelta
	}

	models := make(map[string]RadarAutoEffortModel, len(in.Models))
	for key, entry := range in.Models {
		name := strings.ToLower(strings.TrimSpace(key))
		if name == "" || utf8.RuneCountInString(name) > radarAutoEffortMaxModelNameRunes {
			return nil, fmt.Errorf("radar auto-effort model names must contain 1 to %d characters", radarAutoEffortMaxModelNameRunes)
		}
		modelPolicy := strings.ToLower(strings.TrimSpace(entry.Policy))
		if modelPolicy != "" && !IsRadarAutoEffortPolicy(modelPolicy) {
			return nil, fmt.Errorf("unsupported radar auto-effort policy %q for model %q", entry.Policy, key)
		}
		if !entry.Enabled && modelPolicy == "" {
			continue
		}
		if _, duplicate := models[name]; duplicate {
			return nil, fmt.Errorf("radar auto-effort setting contains duplicate model %q", name)
		}
		models[name] = RadarAutoEffortModel{Enabled: entry.Enabled, Policy: modelPolicy}
	}
	if len(models) > radarAutoEffortMaxModels {
		return nil, fmt.Errorf("radar auto-effort setting cannot contain more than %d models", radarAutoEffortMaxModels)
	}
	if len(models) == 0 && policy == RadarAutoEffortPolicyHighestIQ {
		// Nothing to act on and nothing but defaults to remember, so keep the
		// user's setting compact. A non-default strategy is kept even with every
		// model switched off, otherwise toggling the last model off would
		// silently reset the strategy the user picked.
		return nil, nil
	}
	return &RadarAutoEffortSetting{Policy: policy, MinIQDelta: minIQDelta, Models: models}, nil
}

// EnabledModel reports whether auto-effort is switched on for a radar model.
func (s *RadarAutoEffortSetting) EnabledModel(radarModel string) bool {
	if s == nil {
		return false
	}
	entry, ok := s.Models[strings.ToLower(strings.TrimSpace(radarModel))]
	return ok && entry.Enabled
}

// EffectivePolicy resolves the strategy for a radar model, falling back to the
// user-level default.
func (s *RadarAutoEffortSetting) EffectivePolicy(radarModel string) string {
	if s == nil {
		return RadarAutoEffortPolicyHighestIQ
	}
	if entry, ok := s.Models[strings.ToLower(strings.TrimSpace(radarModel))]; ok && entry.Policy != "" {
		return entry.Policy
	}
	if s.Policy != "" {
		return s.Policy
	}
	return RadarAutoEffortPolicyHighestIQ
}

// MinIQDeltaOrDefault returns the configured minimum IQ gap.
func (s *RadarAutoEffortSetting) MinIQDeltaOrDefault() float64 {
	if s == nil || s.MinIQDelta == 0 {
		return RadarAutoEffortDefaultMinIQDelta
	}
	return s.MinIQDelta
}

type UserSetting struct {
	NotifyType                       string  `json:"notify_type,omitempty"`                          // QuotaWarningType 额度预警类型
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold,omitempty"`              // QuotaWarningThreshold 额度预警阈值
	WebhookUrl                       string  `json:"webhook_url,omitempty"`                          // WebhookUrl webhook地址
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`                       // WebhookSecret webhook密钥
	NotificationEmail                string  `json:"notification_email,omitempty"`                   // NotificationEmail 通知邮箱地址
	BarkUrl                          string  `json:"bark_url,omitempty"`                             // BarkUrl Bark推送URL
	GotifyUrl                        string  `json:"gotify_url,omitempty"`                           // GotifyUrl Gotify服务器地址
	GotifyToken                      string  `json:"gotify_token,omitempty"`                         // GotifyToken Gotify应用令牌
	GotifyPriority                   int     `json:"gotify_priority"`                                // GotifyPriority Gotify消息优先级
	UpstreamModelUpdateNotifyEnabled bool    `json:"upstream_model_update_notify_enabled,omitempty"` // 是否接收上游模型更新定时检测通知（仅管理员）
	AcceptUnsetRatioModel            bool    `json:"accept_unset_model_ratio_model,omitempty"`       // AcceptUnsetRatioModel 是否接受未设置价格的模型
	RecordIpLog                      bool    `json:"record_ip_log,omitempty"`                        // 是否记录请求和错误日志IP
	SidebarModules                   string  `json:"sidebar_modules,omitempty"`                      // SidebarModules 左侧边栏模块配置
	BillingPreference                string  `json:"billing_preference,omitempty"`                   // BillingPreference 扣费策略（订阅/钱包）
	Language                         string  `json:"language,omitempty"`                             // Language 用户语言偏好 (zh, en)
	// RadarAutoEffort is the user's model-radar driven reasoning tier override.
	RadarAutoEffort *RadarAutoEffortSetting `json:"radar_auto_effort,omitempty"`
}

var (
	NotifyTypeEmail   = "email"   // Email 邮件
	NotifyTypeWebhook = "webhook" // Webhook
	NotifyTypeBark    = "bark"    // Bark 推送
	NotifyTypeGotify  = "gotify"  // Gotify 推送
)
