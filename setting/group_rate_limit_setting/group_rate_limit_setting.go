package group_rate_limit_setting

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ConfigName                 = "group_rate_limit_setting"
	MemberEnabledOptionKey     = ConfigName + ".member_enabled"
	SharedPoolEnabledOptionKey = ConfigName + ".shared_pool_enabled"
	PoliciesOptionKey          = ConfigName + ".policies"
)

type Limits struct {
	RPMLimit         *int `json:"rpm_limit,omitempty"`
	ConcurrencyLimit *int `json:"concurrency_limit,omitempty"`
	StreamTPSLimit   *int `json:"stream_tps_limit,omitempty"`
}

type GroupPolicy struct {
	MemberLimits Limits `json:"member_limits"`
	SharedPool   Limits `json:"shared_pool"`
}

type Setting struct {
	MemberEnabled     bool                   `json:"member_enabled"`
	SharedPoolEnabled bool                   `json:"shared_pool_enabled"`
	Policies          map[string]GroupPolicy `json:"policies"`
}

var groupRateLimitSetting = Setting{
	Policies: map[string]GroupPolicy{},
}

var groupRateLimitSnapshot atomic.Pointer[Setting]

func init() {
	config.GlobalConfig.Register(ConfigName, &groupRateLimitSetting)
	groupRateLimitSetting.PublishConfig()
}

func GetSettingSnapshot() *Setting {
	return groupRateLimitSnapshot.Load()
}

func GetGroupPolicy(group string) (GroupPolicy, bool) {
	snapshot := groupRateLimitSnapshot.Load()
	if snapshot == nil {
		return GroupPolicy{}, false
	}
	policy, ok := snapshot.Policies[strings.TrimSpace(group)]
	return policy, ok
}

func (setting *Setting) ValidateConfig() error {
	seenGroups := make(map[string]struct{}, len(setting.Policies))
	for group, policy := range setting.Policies {
		normalizedGroup := strings.TrimSpace(group)
		if normalizedGroup == "" || len(normalizedGroup) > 64 {
			return fmt.Errorf("invalid group rate limit policy name")
		}
		if _, exists := seenGroups[normalizedGroup]; exists {
			return fmt.Errorf("duplicate normalized group rate limit policy name")
		}
		seenGroups[normalizedGroup] = struct{}{}
		if err := validateLimits(policy.MemberLimits); err != nil {
			return fmt.Errorf("invalid member limits for group %s: %w", normalizedGroup, err)
		}
		if err := validateLimits(policy.SharedPool); err != nil {
			return fmt.Errorf("invalid shared pool for group %s: %w", normalizedGroup, err)
		}
	}
	return nil
}

func (setting *Setting) PublishConfig() {
	prepared := normalizeSetting(*setting)
	*setting = prepared
	groupRateLimitSnapshot.Store(copySetting(prepared))
}

func PrepareSetting(setting Setting) (Setting, error) {
	if err := setting.ValidateConfig(); err != nil {
		return Setting{}, err
	}
	return normalizeSetting(setting), nil
}

func validateLimits(limits Limits) error {
	for name, value := range map[string]*int{
		"rpm_limit":         limits.RPMLimit,
		"concurrency_limit": limits.ConcurrencyLimit,
		"stream_tps_limit":  limits.StreamTPSLimit,
	} {
		if value != nil && (*value < 1 || int64(*value) > math.MaxInt32) {
			return fmt.Errorf("%s must be between 1 and 2147483647", name)
		}
	}
	return nil
}

func normalizeSetting(setting Setting) Setting {
	groups := make([]string, 0, len(setting.Policies))
	for group := range setting.Policies {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	policies := make(map[string]GroupPolicy, len(setting.Policies))
	for _, group := range groups {
		normalizedGroup := strings.TrimSpace(group)
		policies[normalizedGroup] = copyPolicy(setting.Policies[group])
	}
	setting.Policies = policies
	return setting
}

func copySetting(setting Setting) *Setting {
	copy := setting
	copy.Policies = make(map[string]GroupPolicy, len(setting.Policies))
	for group, policy := range setting.Policies {
		copy.Policies[group] = copyPolicy(policy)
	}
	return &copy
}

func copyPolicy(policy GroupPolicy) GroupPolicy {
	return GroupPolicy{
		MemberLimits: copyLimits(policy.MemberLimits),
		SharedPool:   copyLimits(policy.SharedPool),
	}
}

func copyLimits(limits Limits) Limits {
	return Limits{
		RPMLimit:         copyInt(limits.RPMLimit),
		ConcurrencyLimit: copyInt(limits.ConcurrencyLimit),
		StreamTPSLimit:   copyInt(limits.StreamTPSLimit),
	}
}

func copyInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
