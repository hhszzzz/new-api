package operation_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ClientPolicyModeUnrestricted = "unrestricted"
	ClientPolicyModeAllow        = "allow"
	ClientPolicyModeDeny         = "deny"
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

type ClientAccessPolicy struct {
	Mode    string   `json:"mode"`
	Clients []string `json:"clients"`
}

type ClientPolicySetting struct {
	Rules         []ClientIdentificationRule    `json:"rules"`
	GroupPolicies map[string]ClientAccessPolicy `json:"group_policies"`
}

var clientPolicySetting = ClientPolicySetting{
	Rules:         []ClientIdentificationRule{},
	GroupPolicies: map[string]ClientAccessPolicy{},
}

func init() {
	config.GlobalConfig.Register("client_policy_setting", &clientPolicySetting)
}

func GetClientPolicySetting() *ClientPolicySetting {
	if clientPolicySetting.Rules == nil {
		clientPolicySetting.Rules = []ClientIdentificationRule{}
	}
	if clientPolicySetting.GroupPolicies == nil {
		clientPolicySetting.GroupPolicies = map[string]ClientAccessPolicy{}
	}
	return &clientPolicySetting
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
	setting := GetClientPolicySetting()
	normalizedRules := make([]ClientIdentificationRule, 0, minPolicyInt(len(setting.Rules), 64))
	for _, rule := range setting.Rules {
		rule.Name = normalizePolicyToken(rule.Name)
		if rule.Name == "" || len(rule.Matches) == 0 || len(rule.Matches) > 8 {
			continue
		}
		matches := make([]ClientIdentificationMatch, 0, len(rule.Matches))
		for _, match := range rule.Matches {
			match.Source = strings.ToLower(strings.TrimSpace(match.Source))
			match.Header = strings.ToLower(strings.TrimSpace(match.Header))
			match.Mode = strings.ToLower(strings.TrimSpace(match.Mode))
			match.Value = strings.TrimSpace(match.Value)
			if match.Mode != "exact" && match.Mode != "prefix" {
				match.Mode = "prefix"
			}
			if !validClientPolicyMatch(match) {
				continue
			}
			matches = append(matches, match)
		}
		if len(matches) > 0 {
			rule.Matches = matches
			normalizedRules = append(normalizedRules, rule)
		}
	}
	setting.Rules = normalizedRules
	if setting.GroupPolicies == nil {
		setting.GroupPolicies = map[string]ClientAccessPolicy{}
	}
	normalizedPolicies := make(map[string]ClientAccessPolicy, len(setting.GroupPolicies))
	for group, policy := range setting.GroupPolicies {
		normalizedGroup := strings.TrimSpace(group)
		if normalizedGroup == "" {
			continue
		}
		policy.Mode = NormalizeClientPolicyMode(policy.Mode)
		policy.Clients = normalizePolicyTokens(policy.Clients, 32)
		normalizedPolicies[normalizedGroup] = policy
	}
	setting.GroupPolicies = normalizedPolicies
}

func ValidateClientPolicySetting(setting ClientPolicySetting) error {
	if len(setting.Rules) > 64 {
		return fmt.Errorf("at most 64 client identification rules are allowed")
	}
	for _, rule := range setting.Rules {
		if normalizePolicyToken(rule.Name) == "" {
			return fmt.Errorf("client rule name is required")
		}
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
	for group, policy := range setting.GroupPolicies {
		if strings.TrimSpace(group) == "" || len(group) > 64 {
			return fmt.Errorf("invalid group client policy name")
		}
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
		if !IsClientPolicyHeaderAllowed(header) {
			return false
		}
		return true
	default:
		return false
	}
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
