package operation_setting

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ClientPolicyModeUnrestricted = "unrestricted"
	ClientPolicyModeAllow        = "allow"
	ClientPolicyModeDeny         = "deny"
	ClientPolicyUnknown          = "unknown"
	ClientPolicyRulesOptionKey   = "client_policy_setting.rules"
	ClientPolicyGroupsOptionKey  = "client_policy_setting.group_policies"
)

type ClientIdentificationMatch struct {
	Source string `json:"source"` // path, user_agent, header
	Header string `json:"header,omitempty"`
	Mode   string `json:"mode"` // exact, prefix
	Value  string `json:"value"`
}

type ClientIdentificationRule struct {
	Name    string                      `json:"name"`
	Matches []ClientIdentificationMatch `json:"matches"`
}

type ClientAccessPolicy = dto.ClientAccessPolicy

type ClientPolicySetting struct {
	Rules         []ClientIdentificationRule    `json:"rules"`
	GroupPolicies map[string]ClientAccessPolicy `json:"group_policies"`
}

var clientPolicySetting = ClientPolicySetting{
	Rules:         []ClientIdentificationRule{},
	GroupPolicies: map[string]ClientAccessPolicy{},
}

var clientPolicySnapshot atomic.Pointer[ClientPolicySetting]

func init() {
	config.GlobalConfig.Register("client_policy_setting", &clientPolicySetting)
	publishClientPolicySnapshot()
}

// GetClientPolicySetting returns the mutable configuration target registered
// with ConfigManager. Request paths must use GetClientPolicySettingSnapshot so
// hot updates never race with readers.
func GetClientPolicySetting() *ClientPolicySetting {
	return &clientPolicySetting
}

// GetClientPolicySettingSnapshot returns the immutable runtime snapshot. The
// pointed-to slices and maps must never be mutated by callers.
func GetClientPolicySettingSnapshot() *ClientPolicySetting {
	return clientPolicySnapshot.Load()
}

func NormalizeClientPolicyMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ClientPolicyModeAllow:
		return ClientPolicyModeAllow
	case ClientPolicyModeDeny:
		return ClientPolicyModeDeny
	default:
		return ClientPolicyModeUnrestricted
	}
}

// NormalizeClientPolicySetting sanitizes configuration loaded from the
// persistent option store. Invalid rules are dropped rather than making the
// request path fail open, and all names/match values are normalized for
// deterministic comparisons.
func NormalizeClientPolicySetting() {
	clientPolicySetting.PublishConfig()
}

func (setting *ClientPolicySetting) ValidateConfig() error {
	return ValidateClientPolicySetting(*setting)
}

func (setting *ClientPolicySetting) ValidateLoadedConfig() error {
	return nil
}

func (setting *ClientPolicySetting) PublishConfig() {
	*setting = normalizeClientPolicySettingValue(*setting)
	publishClientPolicySnapshot()
}

func PrepareClientPolicySetting(setting ClientPolicySetting) (ClientPolicySetting, error) {
	if err := ValidateClientPolicySetting(setting); err != nil {
		return ClientPolicySetting{}, err
	}
	return normalizeClientPolicySettingValue(setting), nil
}

// PublishClientPolicySetting replaces both policy fields under ConfigManager's
// update lock and publishes one immutable runtime snapshot.
func PublishClientPolicySetting(setting ClientPolicySetting) error {
	prepared, err := PrepareClientPolicySetting(setting)
	if err != nil {
		return err
	}
	rulesJSON, err := common.Marshal(prepared.Rules)
	if err != nil {
		return err
	}
	policiesJSON, err := common.Marshal(prepared.GroupPolicies)
	if err != nil {
		return err
	}
	handled, err := config.GlobalConfig.Update("client_policy_setting", map[string]string{
		"rules":          string(rulesJSON),
		"group_policies": string(policiesJSON),
	})
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("client policy config is not registered")
	}
	return nil
}

func normalizeClientPolicySettingValue(setting ClientPolicySetting) ClientPolicySetting {
	normalizedRules := make([]ClientIdentificationRule, 0, minPolicyInt(len(setting.Rules), 64))
	seenRuleNames := make(map[string]struct{}, minPolicyInt(len(setting.Rules), 64))
	for _, rule := range setting.Rules {
		if len(normalizedRules) >= 64 {
			break
		}
		rule.Name = normalizePolicyToken(rule.Name)
		if rule.Name == "" || rule.Name == ClientPolicyUnknown || len(rule.Matches) == 0 || len(rule.Matches) > 8 {
			continue
		}
		if _, exists := seenRuleNames[rule.Name]; exists {
			continue
		}
		matches := make([]ClientIdentificationMatch, 0, len(rule.Matches))
		for _, match := range rule.Matches {
			match.Source = strings.ToLower(strings.TrimSpace(match.Source))
			match.Header = strings.ToLower(strings.TrimSpace(match.Header))
			match.Mode = strings.ToLower(strings.TrimSpace(match.Mode))
			match.Value = strings.TrimSpace(match.Value)
			if !validClientPolicyMatch(match) {
				continue
			}
			matches = append(matches, match)
		}
		if len(matches) > 0 {
			rule.Matches = matches
			normalizedRules = append(normalizedRules, rule)
			seenRuleNames[rule.Name] = struct{}{}
		}
	}
	setting.Rules = normalizedRules
	if setting.GroupPolicies == nil {
		setting.GroupPolicies = map[string]ClientAccessPolicy{}
	}
	normalizedPolicies := make(map[string]ClientAccessPolicy, minPolicyInt(len(setting.GroupPolicies), 256))
	groups := make([]string, 0, len(setting.GroupPolicies))
	for group := range setting.GroupPolicies {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	for _, group := range groups {
		if len(normalizedPolicies) >= 256 {
			break
		}
		policy := setting.GroupPolicies[group]
		normalizedGroup := strings.TrimSpace(group)
		if normalizedGroup == "" {
			continue
		}
		if _, exists := normalizedPolicies[normalizedGroup]; exists {
			// Ambiguous keys such as "premium" and " premium " must not
			// randomly alternate policies across process reloads.
			normalizedPolicies[normalizedGroup] = ClientAccessPolicy{
				Mode:    ClientPolicyModeAllow,
				Clients: []string{},
			}
			continue
		}
		mode, validMode := normalizeClientPolicyModeChecked(policy.Mode)
		if !validMode {
			// Persisted configuration can bypass the API validator. Fail closed
			// instead of silently turning an invalid policy into unrestricted.
			mode = ClientPolicyModeAllow
			policy.Clients = nil
		}
		policy.Mode = mode
		policy.Clients = normalizePolicyTokens(policy.Clients, 32)
		normalizedPolicies[normalizedGroup] = policy
	}
	setting.GroupPolicies = normalizedPolicies
	return setting
}

func ValidateClientPolicySetting(setting ClientPolicySetting) error {
	if len(setting.Rules) > 64 {
		return fmt.Errorf("at most 64 client identification rules are allowed")
	}
	seenRuleNames := make(map[string]struct{}, len(setting.Rules))
	for _, rule := range setting.Rules {
		normalizedName := normalizePolicyToken(rule.Name)
		if normalizedName == "" {
			return fmt.Errorf("client rule name is required")
		}
		if normalizedName == ClientPolicyUnknown {
			return fmt.Errorf("client rule name is reserved")
		}
		if _, exists := seenRuleNames[normalizedName]; exists {
			return fmt.Errorf("duplicate client rule name")
		}
		seenRuleNames[normalizedName] = struct{}{}
		if len(rule.Matches) == 0 || len(rule.Matches) > 8 {
			return fmt.Errorf("each client rule must contain 1 to 8 matches")
		}
		for _, match := range rule.Matches {
			if !validClientPolicyMatch(match) {
				return fmt.Errorf("invalid client identification match")
			}
		}
	}
	if len(setting.GroupPolicies) > 256 {
		return fmt.Errorf("at most 256 group client policies are allowed")
	}
	seenGroups := make(map[string]struct{}, len(setting.GroupPolicies))
	for group, policy := range setting.GroupPolicies {
		if strings.TrimSpace(group) == "" || len(group) > 64 {
			return fmt.Errorf("invalid group client policy name")
		}
		normalizedGroup := strings.TrimSpace(group)
		if _, exists := seenGroups[normalizedGroup]; exists {
			return fmt.Errorf("duplicate normalized group client policy name")
		}
		seenGroups[normalizedGroup] = struct{}{}
		if err := ValidateClientAccessPolicy(policy); err != nil {
			return fmt.Errorf("invalid group client policy: %w", err)
		}
	}
	return nil
}

// ValidateClientAccessPolicy validates the reusable allow/deny policy stored
// on groups and individual channels. Runtime selection must never silently
// turn a malformed policy into unrestricted access.
func ValidateClientAccessPolicy(policy ClientAccessPolicy) error {
	mode := strings.ToLower(strings.TrimSpace(policy.Mode))
	if mode != "" && NormalizeClientPolicyMode(mode) != mode {
		return fmt.Errorf("invalid client policy mode")
	}
	if len(policy.Clients) > 32 {
		return fmt.Errorf("at most 32 clients may be listed for a client policy")
	}
	for _, client := range policy.Clients {
		if normalizePolicyToken(client) == "" || len(client) > 128 {
			return fmt.Errorf("invalid client in client policy")
		}
	}
	return nil
}

func validClientPolicyMatch(match ClientIdentificationMatch) bool {
	source := strings.ToLower(strings.TrimSpace(match.Source))
	mode := strings.ToLower(strings.TrimSpace(match.Mode))
	value := strings.TrimSpace(match.Value)
	if value == "" || len(value) > 512 || (mode != "exact" && mode != "prefix") {
		return false
	}
	switch source {
	case "path", "user_agent":
		return true
	case "header":
		header := strings.ToLower(strings.TrimSpace(match.Header))
		if header == "" || !IsClientPolicyHeaderAllowed(header) {
			return false
		}
		return true
	default:
		return false
	}
}

func normalizeClientPolicyModeChecked(mode string) (string, bool) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", ClientPolicyModeUnrestricted:
		return ClientPolicyModeUnrestricted, true
	case ClientPolicyModeAllow:
		return ClientPolicyModeAllow, true
	case ClientPolicyModeDeny:
		return ClientPolicyModeDeny, true
	default:
		return "", false
	}
}

func publishClientPolicySnapshot() {
	rules := make([]ClientIdentificationRule, len(clientPolicySetting.Rules))
	for index, rule := range clientPolicySetting.Rules {
		rules[index] = rule
		rules[index].Matches = append([]ClientIdentificationMatch(nil), rule.Matches...)
	}
	policies := make(map[string]ClientAccessPolicy, len(clientPolicySetting.GroupPolicies))
	for group, policy := range clientPolicySetting.GroupPolicies {
		policy.Clients = append([]string(nil), policy.Clients...)
		policies[group] = policy
	}
	clientPolicySnapshot.Store(&ClientPolicySetting{
		Rules:         rules,
		GroupPolicies: policies,
	})
}

// IsClientPolicyHeaderAllowed permits administrator-defined request-header
// fingerprints while rejecting names that can carry credentials or secrets.
// The same classifier is shared with diagnostic header recording so a header
// cannot be considered safe in one configuration surface and unsafe in the
// other.
func IsClientPolicyHeaderAllowed(header string) bool {
	header = strings.ToLower(strings.TrimSpace(header))
	return isValidHeaderName(header) && !isSensitiveRequestHeaderName(header)
}

func isSensitiveRequestHeaderName(name string) bool {
	for _, sensitive := range []string{
		"authorization",
		"cookie",
		"credential",
		"password",
		"passwd",
		"secret",
		"signature",
		"token",
		"api-key",
		"apikey",
		"access-key",
		"private-key",
	} {
		if strings.Contains(name, sensitive) {
			return true
		}
	}
	return false
}

func normalizePolicyToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizePolicyTokens(values []string, limit int) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, minPolicyInt(len(values), limit))
	for _, value := range values {
		value = normalizePolicyToken(value)
		if value == "" || len(result) >= limit {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func minPolicyInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
