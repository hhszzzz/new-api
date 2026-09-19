package prompt_audit_setting

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

const ManualWordlistID = "manual"

func (setting PromptAuditSetting) ManualWordlistActive() bool {
	return setting.ManualWordlistEnabled == nil || *setting.ManualWordlistEnabled
}

type ScopePolicy struct {
	LibraryIDs []string `json:"library_ids"`
	ModelAudit bool     `json:"model_audit"`
}

// A missing policy is the pre-wordlist configuration, not an empty policy.
// Explicitly saving an empty map disables inspection for every source.
func (setting PromptAuditSetting) PolicyFor(scope dto.PromptAuditScope) ScopePolicy {
	if setting.ScopePolicies == nil {
		ids := []string{}
		if scope == dto.PromptScopeUser || scope == dto.PromptScopeTask {
			ids = append(ids, ManualWordlistID)
		}
		return ScopePolicy{LibraryIDs: ids, ModelAudit: true}
	}
	policy := setting.ScopePolicies[scope]
	policy.LibraryIDs = append([]string{}, policy.LibraryIDs...)
	return policy
}

func (setting PromptAuditSetting) EffectiveScopePolicies() map[dto.PromptAuditScope]ScopePolicy {
	result := make(map[dto.PromptAuditScope]ScopePolicy)
	for _, scope := range dto.PromptAuditScopes() {
		result[scope] = setting.PolicyFor(scope)
	}
	return result
}

func validateScopePolicies(policies map[dto.PromptAuditScope]ScopePolicy) error {
	for scope, policy := range policies {
		if !slices.Contains(dto.PromptAuditScopes(), scope) {
			return fmt.Errorf("unknown prompt audit source %q", scope)
		}
		if len(policy.LibraryIDs) > 51 {
			return fmt.Errorf("too many wordlists for %s", scope)
		}
		seen := make(map[string]bool)
		for _, id := range policy.LibraryIDs {
			if id != ManualWordlistID {
				parsed, err := strconv.ParseInt(id, 10, 64)
				if err != nil || parsed <= 0 {
					return fmt.Errorf("invalid wordlist id %q", id)
				}
			}
			if seen[id] {
				return fmt.Errorf("duplicate wordlist id %q", id)
			}
			seen[id] = true
		}
	}
	return nil
}
