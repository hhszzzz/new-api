package account_pool_setting

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ConfigName = "account_pool"

	ProviderCodex       = "codex"
	ProviderClaude      = "claude"
	ProviderAntigravity = "antigravity"

	EnabledOptionKey                   = ConfigName + ".enabled"
	HideEmailFromNonAdminsOptionKey    = ConfigName + ".hide_email_from_non_admins"
	ProviderGroupsOptionKey            = ConfigName + ".provider_groups"
	RegularRefreshSecondsOptionKey     = ConfigName + ".regular_refresh_seconds"
	NearResetThresholdSecondsOptionKey = ConfigName + ".near_reset_threshold_seconds"
	NearResetRefreshSecondsOptionKey   = ConfigName + ".near_reset_refresh_seconds"
	PostResetDelaySecondsOptionKey     = ConfigName + ".post_reset_delay_seconds"
	ManualRefreshCooldownOptionKey     = ConfigName + ".manual_refresh_cooldown_seconds"
)

var KnownProviders = []string{ProviderCodex, ProviderClaude, ProviderAntigravity}

func IsKnownProvider(provider string) bool {
	for _, known := range KnownProviders {
		if known == provider {
			return true
		}
	}
	return false
}

type Setting struct {
	Enabled                   bool                `json:"enabled"`
	HideEmailFromNonAdmins    bool                `json:"hide_email_from_non_admins"`
	ProviderGroups            map[string][]string `json:"provider_groups"`
	RegularRefreshSeconds     int                 `json:"regular_refresh_seconds"`
	NearResetThresholdSeconds int                 `json:"near_reset_threshold_seconds"`
	NearResetRefreshSeconds   int                 `json:"near_reset_refresh_seconds"`
	PostResetDelaySeconds     int                 `json:"post_reset_delay_seconds"`
	ManualRefreshCooldown     int                 `json:"manual_refresh_cooldown_seconds"`
}

var accountPoolSetting = Setting{
	Enabled:                   false,
	HideEmailFromNonAdmins:    true,
	RegularRefreshSeconds:     300,
	NearResetThresholdSeconds: 600,
	NearResetRefreshSeconds:   60,
	PostResetDelaySeconds:     10,
	ManualRefreshCooldown:     60,
}

var accountPoolSnapshot atomic.Pointer[Setting]

func init() {
	config.GlobalConfig.Register(ConfigName, &accountPoolSetting)
	accountPoolSetting.PublishConfig()
}

func GetSettingSnapshot() *Setting {
	snapshot := accountPoolSnapshot.Load()
	if snapshot == nil {
		return nil
	}
	return copySetting(*snapshot)
}

func PrepareSetting(setting Setting) (Setting, error) {
	if err := setting.ValidateConfig(); err != nil {
		return Setting{}, err
	}
	return normalizeSetting(setting), nil
}

func (setting *Setting) ValidateConfig() error {
	if setting.RegularRefreshSeconds < 60 || setting.RegularRefreshSeconds > 3600 {
		return fmt.Errorf("regular_refresh_seconds must be between 60 and 3600")
	}
	if setting.NearResetThresholdSeconds < 60 || setting.NearResetThresholdSeconds > 3600 {
		return fmt.Errorf("near_reset_threshold_seconds must be between 60 and 3600")
	}
	if setting.NearResetRefreshSeconds < 30 || setting.NearResetRefreshSeconds > 600 {
		return fmt.Errorf("near_reset_refresh_seconds must be between 30 and 600")
	}
	if setting.NearResetRefreshSeconds > setting.RegularRefreshSeconds {
		return fmt.Errorf("near_reset_refresh_seconds must not exceed regular_refresh_seconds")
	}
	if setting.PostResetDelaySeconds < 0 || setting.PostResetDelaySeconds > 120 {
		return fmt.Errorf("post_reset_delay_seconds must be between 0 and 120")
	}
	if setting.ManualRefreshCooldown < 30 || setting.ManualRefreshCooldown > 600 {
		return fmt.Errorf("manual_refresh_cooldown_seconds must be between 30 and 600")
	}

	for provider, providerGroups := range setting.ProviderGroups {
		if !IsKnownProvider(provider) {
			return fmt.Errorf("provider_groups contains an unknown provider %q", provider)
		}
		seenProviderGroups := make(map[string]struct{}, len(providerGroups))
		for _, rawGroup := range providerGroups {
			group := strings.TrimSpace(rawGroup)
			if group == "" || len(group) > 64 {
				return fmt.Errorf("provider_groups contains an invalid group")
			}
			if _, exists := seenProviderGroups[group]; exists {
				continue
			}
			seenProviderGroups[group] = struct{}{}
		}
	}
	return nil
}

func (setting *Setting) PublishConfig() {
	prepared := normalizeSetting(*setting)
	*setting = prepared
	accountPoolSnapshot.Store(copySetting(prepared))
}

func CanAccess(role int, userGroups []string) bool {
	snapshot := accountPoolSnapshot.Load()
	if snapshot == nil {
		return false
	}
	return snapshot.CanAccess(role, userGroups)
}

func CanAccessProvider(role int, userGroups []string, provider string) bool {
	snapshot := accountPoolSnapshot.Load()
	if snapshot == nil {
		return false
	}
	return snapshot.CanAccessProvider(role, userGroups, provider)
}

func (setting *Setting) CanAccess(role int, userGroups []string) bool {
	if !setting.Enabled {
		return false
	}
	if role >= common.RoleAdminUser {
		return true
	}
	for _, provider := range KnownProviders {
		if setting.CanAccessProvider(role, userGroups, provider) {
			return true
		}
	}
	return false
}

func (setting *Setting) CanAccessProvider(role int, userGroups []string, provider string) bool {
	if !setting.Enabled {
		return false
	}
	if role >= common.RoleAdminUser {
		return true
	}

	var providerGroups []string
	if setting.ProviderGroups != nil {
		providerGroups = setting.ProviderGroups[provider]
	}
	if len(providerGroups) == 0 || len(userGroups) == 0 {
		return false
	}

	allowed := make(map[string]struct{}, len(providerGroups))
	for _, group := range providerGroups {
		allowed[group] = struct{}{}
	}
	for _, rawGroup := range userGroups {
		if _, ok := allowed[strings.TrimSpace(rawGroup)]; ok {
			return true
		}
	}
	return false
}

func ShouldIncludeEmail(role int) bool {
	if role >= common.RoleAdminUser {
		return true
	}
	snapshot := accountPoolSnapshot.Load()
	return snapshot != nil && !snapshot.HideEmailFromNonAdmins
}

func normalizeSetting(setting Setting) Setting {
	if setting.ProviderGroups == nil {
		setting.ProviderGroups = map[string][]string{}
	} else {
		normalizedProviderGroups := make(map[string][]string, len(setting.ProviderGroups))
		for provider, groups := range setting.ProviderGroups {
			normalizedGroups := normalizeGroups(groups)
			if len(normalizedGroups) == 0 {
				continue
			}
			normalizedProviderGroups[provider] = normalizedGroups
		}
		setting.ProviderGroups = normalizedProviderGroups
	}
	return setting
}

func normalizeGroups(groups []string) []string {
	seen := make(map[string]struct{}, len(groups))
	normalized := make([]string, 0, len(groups))
	for _, rawGroup := range groups {
		group := strings.TrimSpace(rawGroup)
		if group == "" {
			continue
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		normalized = append(normalized, group)
	}
	sort.Strings(normalized)
	return normalized
}

func copySetting(setting Setting) *Setting {
	copy := setting
	if setting.ProviderGroups == nil {
		copy.ProviderGroups = map[string][]string{}
	} else {
		providerGroups := make(map[string][]string, len(setting.ProviderGroups))
		for provider, groups := range setting.ProviderGroups {
			providerGroups[provider] = append([]string{}, groups...)
		}
		copy.ProviderGroups = providerGroups
	}
	return &copy
}
