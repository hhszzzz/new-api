package clientpolicy

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	ClientUnknown         = "unknown"
	ClientCodex           = "codex"
	ClientClaudeCode      = "claude_code"
	ClientPi              = "pi"
	ClientDeepSeekHarness = "deepseek_harness"
)

// DisplayName is the product name shown to people, while the constants above
// remain the stable identifiers stored in channel and group policies.
func DisplayName(client string) string {
	switch normalizeClientName(client) {
	case ClientCodex:
		return "Codex"
	case ClientClaudeCode:
		return "Claude Code"
	case ClientPi:
		return "Pi"
	case ClientDeepSeekHarness:
		return "DeepSeek Harness"
	case ClientUnknown:
		return "Unknown"
	default:
		return strings.TrimSpace(client)
	}
}

// AllowedClients returns the display names an allow-list policy accepts. A deny
// policy returns nil because its list names rejected clients, not accepted ones.
func AllowedClients(policy operation_setting.ClientAccessPolicy) []string {
	if operation_setting.NormalizeClientPolicyMode(policy.Mode) != operation_setting.ClientPolicyModeAllow {
		return nil
	}
	clients := make([]string, 0, len(policy.Clients))
	for _, client := range policy.Clients {
		if name := DisplayName(client); name != "" {
			clients = append(clients, name)
		}
	}
	return clients
}

func Detect(request *http.Request) string {
	if request == nil {
		return ClientUnknown
	}
	for _, rule := range operation_setting.GetClientPolicySettingSnapshot().Rules {
		name := normalizeClientName(rule.Name)
		if name == "" || len(rule.Matches) == 0 {
			continue
		}
		matched := true
		for _, match := range rule.Matches {
			if !matchesRequest(request, match) {
				matched = false
				break
			}
		}
		if matched {
			return name
		}
	}

	path := ""
	if request.URL != nil {
		path = strings.ToLower(request.URL.Path)
	}
	userAgent := strings.ToLower(strings.TrimSpace(request.UserAgent()))
	originator := strings.ToLower(strings.TrimSpace(request.Header.Get("Originator")))
	// Pi and DeepSeek Harness identify themselves on every relay protocol, so
	// their signatures are checked before the path-scoped built-ins. Pi sends
	// "pi (darwin ...)" or "pi/<version>" on provider calls and "pi-coding-agent"
	// on its Cloudflare provider; it also sets the pi originator on some routes.
	if strings.HasPrefix(userAgent, "deepseek-harness/") {
		return ClientDeepSeekHarness
	}
	if strings.HasPrefix(userAgent, "pi (") || strings.HasPrefix(userAgent, "pi/") ||
		strings.HasPrefix(userAgent, "pi-coding-agent") || originator == ClientPi {
		return ClientPi
	}
	if strings.HasPrefix(path, "/v1/responses") && (strings.HasPrefix(userAgent, "codex_cli_rs/") ||
		strings.HasPrefix(userAgent, "codex-cli/") ||
		strings.HasPrefix(originator, "codex") ||
		request.Header.Get("X-Codex-Beta-Features") != "" ||
		request.Header.Get("X-Codex-Turn-State") != "") {
		return ClientCodex
	}
	if strings.HasPrefix(path, "/v1/messages") && (strings.HasPrefix(userAgent, "claude-cli/") ||
		strings.HasPrefix(userAgent, "claude-code/") ||
		strings.EqualFold(strings.TrimSpace(request.Header.Get("X-App")), "cli")) {
		return ClientClaudeCode
	}
	return ClientUnknown
}

func IsChannelAllowed(channel *model.Channel, client string) bool {
	if channel == nil {
		return false
	}
	return IsAllowed(channel.GetOtherSettings().ClientPolicy, client)
}

func IsGroupAllowed(group string, client string) bool {
	group = strings.TrimSpace(group)
	if group == "" {
		return true
	}
	policy := operation_setting.GetClientPolicySettingSnapshot().GroupPolicies[group]
	return IsAllowed(policy, client)
}

func IsAllowed(policy operation_setting.ClientAccessPolicy, client string) bool {
	mode := operation_setting.NormalizeClientPolicyMode(policy.Mode)
	if strings.TrimSpace(policy.Mode) != "" && mode == operation_setting.ClientPolicyModeUnrestricted &&
		!strings.EqualFold(strings.TrimSpace(policy.Mode), operation_setting.ClientPolicyModeUnrestricted) {
		return false
	}
	if mode == operation_setting.ClientPolicyModeUnrestricted {
		return true
	}
	client = normalizeClientName(client)
	if client == "" {
		client = ClientUnknown
	}
	listed := false
	for _, allowed := range policy.Clients {
		if normalizeClientName(allowed) == client {
			listed = true
			break
		}
	}
	if mode == operation_setting.ClientPolicyModeAllow {
		return listed
	}
	return !listed
}

func IsSafeMatchHeader(name string) bool {
	return operation_setting.IsClientPolicyHeaderAllowed(name)
}

func matchesRequest(request *http.Request, match operation_setting.ClientIdentificationMatch) bool {
	value := ""
	switch strings.ToLower(strings.TrimSpace(match.Source)) {
	case "path":
		if request.URL != nil {
			value = request.URL.Path
		}
	case "user_agent":
		value = request.UserAgent()
	case "header":
		if !IsSafeMatchHeader(match.Header) {
			return false
		}
		value = request.Header.Get(match.Header)
	default:
		return false
	}
	expected := strings.TrimSpace(match.Value)
	if expected == "" {
		return false
	}
	value = strings.ToLower(strings.TrimSpace(value))
	expected = strings.ToLower(expected)
	if strings.EqualFold(strings.TrimSpace(match.Mode), "exact") {
		return value == expected
	}
	return strings.HasPrefix(value, expected)
}

func normalizeClientName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
