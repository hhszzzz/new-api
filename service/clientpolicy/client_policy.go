package clientpolicy

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	ClientUnknown         = "unknown"
	ClientCodex           = "codex"
	ClientClaudeCode      = "claude_code"
	ClientPi              = "pi"
	ClientDeepSeekHarness = "deepseek_harness"
	ClientAntigravity     = "antigravity"
	ClientCline           = "cline"
	ClientOpenCode        = "opencode"
	ClientKimiCode        = "kimi_code"
	ClientZCode           = "zcode"
)

type Identity struct {
	Client string
	Rule   string
}

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
	case ClientAntigravity:
		return "Antigravity"
	case ClientCline:
		return "Cline"
	case ClientOpenCode:
		return "OpenCode"
	case ClientKimiCode:
		return "Kimi Code"
	case ClientZCode:
		return "ZCode"
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
	return Identify(request).Client
}

func Identify(request *http.Request) Identity {
	if request == nil {
		return Identity{Client: ClientUnknown}
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
			return Identity{Client: name, Rule: "custom:" + name}
		}
	}

	path := ""
	if request.URL != nil {
		path = strings.ToLower(request.URL.Path)
	}
	userAgent := strings.ToLower(strings.TrimSpace(request.UserAgent()))
	originator := strings.ToLower(strings.TrimSpace(request.Header.Get("Originator")))
	if isCodexOriginator(originator) || strings.HasPrefix(userAgent, "codex_cli_rs/") ||
		strings.HasPrefix(userAgent, "codex-cli/") || strings.HasPrefix(userAgent, "codex_vscode/") ||
		strings.HasPrefix(userAgent, "codex-desktop/") || strings.HasPrefix(userAgent, "codex/") ||
		strings.HasPrefix(userAgent, "codex ") {
		return Identity{Client: ClientCodex, Rule: "builtin:codex"}
	}
	if strings.Contains(userAgent, "antigravity/") || hasAntigravityClientMetadata(request.Header.Get("Client-Metadata")) {
		return Identity{Client: ClientAntigravity, Rule: "builtin:antigravity"}
	}
	if strings.HasPrefix(userAgent, "cline-vscode-extension") || strings.HasPrefix(userAgent, "cline/") ||
		strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Title")), "Cline") {
		return Identity{Client: ClientCline, Rule: "builtin:cline"}
	}
	if strings.HasPrefix(userAgent, "opencode/") || strings.HasPrefix(userAgent, "open-code/") {
		return Identity{Client: ClientOpenCode, Rule: "builtin:opencode"}
	}
	// Kimi Code lets users customize its identity slug; match the common CLI forms.
	if strings.HasPrefix(userAgent, "kimi-code-cli/") || strings.HasPrefix(userAgent, "kimi-code/") {
		return Identity{Client: ClientKimiCode, Rule: "builtin:kimi_code"}
	}
	// ZCode does not publish a stable request fingerprint, so these are best-effort hints.
	if strings.HasPrefix(userAgent, "zcode/") ||
		strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Title")), "ZCode") {
		return Identity{Client: ClientZCode, Rule: "builtin:zcode"}
	}
	// Pi and DeepSeek Harness identify themselves on every relay protocol, so
	// their signatures are checked before the path-scoped built-ins. Pi sends
	// "pi (darwin ...)" or "pi/<version>" on provider calls and "pi-coding-agent"
	// on its Cloudflare provider; it also sets the pi originator on some routes.
	if strings.HasPrefix(userAgent, "deepseek-harness/") {
		return Identity{Client: ClientDeepSeekHarness, Rule: "builtin:deepseek_harness"}
	}
	if strings.HasPrefix(userAgent, "pi (") || strings.HasPrefix(userAgent, "pi/") ||
		strings.HasPrefix(userAgent, "pi-coding-agent") || originator == ClientPi ||
		strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Msh-Platform")), ClientPi) {
		return Identity{Client: ClientPi, Rule: "builtin:pi"}
	}
	if strings.HasPrefix(path, "/v1/responses") && (request.Header.Get("X-Codex-Beta-Features") != "" ||
		request.Header.Get("X-Codex-Turn-State") != "") {
		return Identity{Client: ClientCodex, Rule: "builtin:codex"}
	}
	if strings.HasPrefix(path, "/v1/messages") && (strings.HasPrefix(userAgent, "claude-cli/") ||
		strings.HasPrefix(userAgent, "claude-code/") ||
		strings.EqualFold(strings.TrimSpace(request.Header.Get("X-App")), "cli")) {
		return Identity{Client: ClientClaudeCode, Rule: "builtin:claude_code"}
	}
	return Identity{Client: ClientUnknown}
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
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "codex_cli_rs", "codex-cli", "codex-tui", "codex_vscode", "codex_desktop", "codex-chatgpt-desktop", "codex_chatgpt_desktop", "codex_atlas":
		return ClientCodex
	default:
		if strings.HasPrefix(name, "codex ") {
			return ClientCodex
		}
		return name
	}
}

func isCodexOriginator(originator string) bool {
	switch originator {
	case "codex_cli_rs", "codex-cli", "codex-tui", "codex_vscode", "codex_desktop", "codex-chatgpt-desktop", "codex_chatgpt_desktop", "codex_atlas":
		return true
	default:
		return strings.HasPrefix(originator, "codex ")
	}
}

func hasAntigravityClientMetadata(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	var metadata map[string]any
	if err := common.Unmarshal([]byte(value), &metadata); err != nil {
		return false
	}
	ideType, ok := metadata["ideType"].(string)
	return ok && strings.EqualFold(strings.TrimSpace(ideType), "ANTIGRAVITY")
}
