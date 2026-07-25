package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeClientPolicySettingNormalizesGroupKeysAndValues(t *testing.T) {
	setting := GetClientPolicySetting()
	originalRules := setting.Rules
	originalPolicies := setting.GroupPolicies
	t.Cleanup(func() {
		setting.Rules = originalRules
		setting.GroupPolicies = originalPolicies
	})

	setting.Rules = nil
	setting.GroupPolicies = map[string]ClientAccessPolicy{
		" Premium ": {
			Mode:    "ALLOW",
			Clients: []string{" Codex ", "codex", "CLAUDE_CODE"},
		},
	}

	NormalizeClientPolicySetting()

	require.Len(t, setting.GroupPolicies, 1)
	policy, ok := setting.GroupPolicies["Premium"]
	require.True(t, ok)
	assert.Equal(t, ClientPolicyModeAllow, policy.Mode)
	assert.Equal(t, []string{"codex", "claude_code"}, policy.Clients)
}

func TestValidateClientPolicySettingRejectsInvalidGroupPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy ClientPolicySetting
	}{
		{
			name: "empty group",
			policy: ClientPolicySetting{GroupPolicies: map[string]ClientAccessPolicy{
				" ": {Mode: ClientPolicyModeAllow, Clients: []string{"codex"}},
			}},
		},
		{
			name: "invalid mode",
			policy: ClientPolicySetting{GroupPolicies: map[string]ClientAccessPolicy{
				"default": {Mode: "sometimes", Clients: []string{"codex"}},
			}},
		},
		{
			name: "empty client",
			policy: ClientPolicySetting{GroupPolicies: map[string]ClientAccessPolicy{
				"default": {Mode: ClientPolicyModeDeny, Clients: []string{""}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, ValidateClientPolicySetting(tt.policy))
		})
	}
}

func TestValidateClientPolicySettingUsesSameSafeHeaderRulesAsRuntime(t *testing.T) {
	valid := ClientPolicySetting{Rules: []ClientIdentificationRule{
		{
			Name: "desktop_agent",
			Matches: []ClientIdentificationMatch{
				{Source: "header", Header: "X-Desktop-Agent", Mode: "prefix", Value: "demo/"},
			},
		},
	}}
	require.NoError(t, ValidateClientPolicySetting(valid))

	for _, header := range []string{
		"X-Service-Token",
		"X-Client-Secret",
		"X-Signature",
		"X-Password-Hint",
		"X-Api-Key",
	} {
		t.Run(header, func(t *testing.T) {
			invalid := valid
			invalid.Rules = []ClientIdentificationRule{
				{
					Name: "unsafe",
					Matches: []ClientIdentificationMatch{
						{Source: "header", Header: header, Mode: "prefix", Value: "secret"},
					},
				},
			}
			require.Error(t, ValidateClientPolicySetting(invalid))
		})
	}
}
