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

	EnabledOptionKey                   = ConfigName + ".enabled"
	AllowedGroupsOptionKey             = ConfigName + ".allowed_groups"
	RegularRefreshSecondsOptionKey     = ConfigName + ".regular_refresh_seconds"
	NearResetThresholdSecondsOptionKey = ConfigName + ".near_reset_threshold_seconds"
	NearResetRefreshSecondsOptionKey   = ConfigName + ".near_reset_refresh_seconds"
	PostResetDelaySecondsOptionKey     = ConfigName + ".post_reset_delay_seconds"
	ManualRefreshCooldownOptionKey     = ConfigName + ".manual_refresh_cooldown_seconds"
)

type Setting struct {
	Enabled                   bool     `json:"enabled"`
	AllowedGroups             []string `json:"allowed_groups"`
	RegularRefreshSeconds     int      `json:"regular_refresh_seconds"`
	NearResetThresholdSeconds int      `json:"near_reset_threshold_seconds"`
	NearResetRefreshSeconds   int      `json:"near_reset_refresh_seconds"`
	PostResetDelaySeconds     int      `json:"post_reset_delay_seconds"`
	ManualRefreshCooldown     int      `json:"manual_refresh_cooldown_seconds"`
}

var accountPoolSetting = Setting{
	Enabled:                   false,
	AllowedGroups:             []string{},
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

	seen := make(map[string]struct{}, len(setting.AllowedGroups))
	for _, rawGroup := range setting.AllowedGroups {
		group := strings.TrimSpace(rawGroup)
		if group == "" || len(group) > 64 {
			return fmt.Errorf("allowed_groups contains an invalid group")
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
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
	if snapshot == nil || !snapshot.Enabled {
		return false
	}
	if role >= common.RoleAdminUser {
		return true
	}
	if len(snapshot.AllowedGroups) == 0 || len(userGroups) == 0 {
		return false
	}

	allowed := make(map[string]struct{}, len(snapshot.AllowedGroups))
	for _, group := range snapshot.AllowedGroups {
		allowed[group] = struct{}{}
	}
	for _, rawGroup := range userGroups {
		if _, ok := allowed[strings.TrimSpace(rawGroup)]; ok {
			return true
		}
	}
	return false
}

func normalizeSetting(setting Setting) Setting {
	seen := make(map[string]struct{}, len(setting.AllowedGroups))
	groups := make([]string, 0, len(setting.AllowedGroups))
	for _, rawGroup := range setting.AllowedGroups {
		group := strings.TrimSpace(rawGroup)
		if group == "" {
			continue
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}
	sort.Strings(groups)
	setting.AllowedGroups = groups
	return setting
}

func copySetting(setting Setting) *Setting {
	copy := setting
	copy.AllowedGroups = append([]string(nil), setting.AllowedGroups...)
	return &copy
}
