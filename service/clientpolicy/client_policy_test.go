package clientpolicy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func replaceClientPolicySettingForTest(
	t *testing.T,
	value operation_setting.ClientPolicySetting,
) {
	t.Helper()
	setting := operation_setting.GetClientPolicySetting()
	original := *operation_setting.GetClientPolicySettingSnapshot()
	*setting = value
	operation_setting.NormalizeClientPolicySetting()
	t.Cleanup(func() {
		*setting = original
		operation_setting.NormalizeClientPolicySetting()
	})
}

func TestDetectRecognizesBuiltInAndConfiguredClients(t *testing.T) {
	replaceClientPolicySettingForTest(t, operation_setting.ClientPolicySetting{
		Rules: []operation_setting.ClientIdentificationRule{
			{
				Name: "desktop_agent",
				Matches: []operation_setting.ClientIdentificationMatch{
					{Source: "path", Mode: "prefix", Value: "/v1/responses"},
					{Source: "header", Header: "X-Client-Request-Id", Mode: "prefix", Value: "desktop-"},
				},
			},
		},
	})

	configured := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	configured.Header.Set("X-Client-Request-Id", "desktop-123")
	assert.Equal(t, "desktop_agent", Detect(configured))

	codex := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	codex.Header.Set("User-Agent", "codex_cli_rs/1.2.3")
	assert.Equal(t, ClientCodex, Detect(codex))

	claude := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	claude.Header.Set("User-Agent", "claude-cli/2.0.0")
	assert.Equal(t, ClientClaudeCode, Detect(claude))

	unknown := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	unknown.Header.Set("User-Agent", "curl/8.0")
	assert.Equal(t, ClientUnknown, Detect(unknown))
}

func TestDetectRejectsUnsafeCustomHeaderRules(t *testing.T) {
	replaceClientPolicySettingForTest(t, operation_setting.ClientPolicySetting{
		Rules: []operation_setting.ClientIdentificationRule{
			{
				Name: "secret_match",
				Matches: []operation_setting.ClientIdentificationMatch{
					{Source: "header", Header: "Authorization", Mode: "prefix", Value: "Bearer "},
				},
			},
		},
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request.Header.Set("Authorization", "Bearer secret")
	assert.Equal(t, ClientUnknown, Detect(request))
}

func TestDetectAcceptsValidatedCustomHeaderRules(t *testing.T) {
	replaceClientPolicySettingForTest(t, operation_setting.ClientPolicySetting{
		Rules: []operation_setting.ClientIdentificationRule{
			{
				Name: "desktop_agent",
				Matches: []operation_setting.ClientIdentificationMatch{
					{Source: "header", Header: "X-Desktop-Agent", Mode: "prefix", Value: "demo/"},
				},
			},
		},
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("X-Desktop-Agent", "demo/1.2.3")
	assert.Equal(t, "desktop_agent", Detect(request))
}

func TestIsAllowedPreservesUnknownClientSemantics(t *testing.T) {
	allow := operation_setting.ClientAccessPolicy{
		Mode:    operation_setting.ClientPolicyModeAllow,
		Clients: []string{ClientCodex},
	}
	deny := operation_setting.ClientAccessPolicy{
		Mode:    operation_setting.ClientPolicyModeDeny,
		Clients: []string{ClientClaudeCode},
	}

	assert.True(t, IsAllowed(allow, ClientCodex))
	assert.False(t, IsAllowed(allow, ClientUnknown))
	assert.False(t, IsAllowed(deny, ClientClaudeCode))
	assert.True(t, IsAllowed(deny, ClientUnknown))
	assert.True(t, IsAllowed(operation_setting.ClientAccessPolicy{}, ClientUnknown))
	assert.False(t, IsAllowed(operation_setting.ClientAccessPolicy{Mode: "invalid"}, ClientUnknown))
}

func TestIsGroupAllowedUsesConfiguredAllowAndDenyPolicies(t *testing.T) {
	replaceClientPolicySettingForTest(t, operation_setting.ClientPolicySetting{
		GroupPolicies: map[string]operation_setting.ClientAccessPolicy{
			"codex-only": {
				Mode:    operation_setting.ClientPolicyModeAllow,
				Clients: []string{ClientCodex},
			},
			"no-claude": {
				Mode:    operation_setting.ClientPolicyModeDeny,
				Clients: []string{ClientClaudeCode},
			},
		},
	})

	assert.True(t, IsGroupAllowed("codex-only", ClientCodex))
	assert.False(t, IsGroupAllowed("codex-only", ClientUnknown))
	assert.False(t, IsGroupAllowed("no-claude", ClientClaudeCode))
	assert.True(t, IsGroupAllowed("no-claude", ClientUnknown))
	assert.True(t, IsGroupAllowed("unconfigured", ClientUnknown))
}

func TestChannelPolicyIsReadFromTypedChannelSettings(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		ClientPolicy: operation_setting.ClientAccessPolicy{
			Mode:    operation_setting.ClientPolicyModeAllow,
			Clients: []string{ClientCodex},
		},
	}
	encoded, err := common.Marshal(settings)
	require.NoError(t, err)
	channel := &model.Channel{OtherSettings: string(encoded)}

	assert.True(t, IsChannelAllowed(channel, ClientCodex))
	assert.False(t, IsChannelAllowed(channel, ClientClaudeCode))
}
