package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func replaceClientPolicySettingForTest(t *testing.T, value ClientPolicySetting) {
	t.Helper()
	setting := GetClientPolicySetting()
	original := *GetClientPolicySettingSnapshot()
	*setting = value
	NormalizeClientPolicySetting()
	t.Cleanup(func() {
		*setting = original
		NormalizeClientPolicySetting()
	})
}

func TestNormalizeClientPolicySettingNormalizesGroupKeysAndValues(t *testing.T) {
	replaceClientPolicySettingForTest(t, ClientPolicySetting{
		GroupPolicies: map[string]ClientAccessPolicy{
			" Premium ": {
				Mode:    "ALLOW",
				Clients: []string{" Codex ", "codex", "CLAUDE_CODE"},
			},
		},
	})
	setting := GetClientPolicySettingSnapshot()

	require.Len(t, setting.GroupPolicies, 1)
	policy, ok := setting.GroupPolicies["Premium"]
	require.True(t, ok)
	assert.Equal(t, ClientPolicyModeAllow, policy.Mode)
	assert.Equal(t, []string{"codex", "claude_code"}, policy.Clients)
}

func TestNormalizeClientPolicySettingPublishesAnImmutableSnapshot(t *testing.T) {
	replaceClientPolicySettingForTest(t, ClientPolicySetting{
		Rules: []ClientIdentificationRule{
			{
				Name: "desktop",
				Matches: []ClientIdentificationMatch{
					{Source: "path", Mode: "prefix", Value: "/v1/responses"},
				},
			},
		},
		GroupPolicies: map[string]ClientAccessPolicy{
			"default": {Mode: ClientPolicyModeAllow, Clients: []string{"desktop"}},
		},
	})

	previous := GetClientPolicySettingSnapshot()
	setting := GetClientPolicySetting()
	setting.Rules = []ClientIdentificationRule{
		{
			Name: "cli",
			Matches: []ClientIdentificationMatch{
				{Source: "user_agent", Mode: "prefix", Value: "cli/"},
			},
		},
	}
	setting.GroupPolicies = map[string]ClientAccessPolicy{
		"default": {Mode: ClientPolicyModeDeny, Clients: []string{"cli"}},
	}
	NormalizeClientPolicySetting()

	current := GetClientPolicySettingSnapshot()
	require.NotSame(t, previous, current)
	assert.Equal(t, "desktop", previous.Rules[0].Name)
	assert.Equal(t, []string{"desktop"}, previous.GroupPolicies["default"].Clients)
	assert.Equal(t, "cli", current.Rules[0].Name)
	assert.Equal(t, []string{"cli"}, current.GroupPolicies["default"].Clients)
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
		"",
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

func TestPersistedInvalidClientPolicyFailsClosed(t *testing.T) {
	original := config.GlobalConfig.ExportAllConfigs()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(original))
	})

	handled, err := config.GlobalConfig.UpdateFromDB("client_policy_setting", map[string]string{
		"group_policies": `{"default":{"mode":"sometimes","clients":["desktop"]}}`,
	})
	require.True(t, handled)
	require.NoError(t, err)

	policy := GetClientPolicySettingSnapshot().GroupPolicies["default"]
	assert.Equal(t, ClientPolicyModeAllow, policy.Mode)
	assert.Empty(t, policy.Clients)
}

func TestValidateClientPolicySettingRejectsAmbiguousRuleNames(t *testing.T) {
	match := []ClientIdentificationMatch{
		{Source: "path", Mode: "prefix", Value: "/v1/responses"},
	}

	for _, rules := range [][]ClientIdentificationRule{
		{
			{Name: "unknown", Matches: match},
		},
		{
			{Name: " Desktop ", Matches: match},
			{Name: "desktop", Matches: match},
		},
	} {
		require.Error(t, ValidateClientPolicySetting(ClientPolicySetting{Rules: rules}))
	}
}
