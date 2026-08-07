package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptAuditEndpointTokensAreWriteOnlyAndSurviveIDRename(t *testing.T) {
	current := []prompt_audit_setting.Endpoint{{
		ID: "old-id", BaseURL: "https://guard.example.com", Token: "top-secret-token",
	}}
	updates := []promptAuditEndpointUpdate{{
		ID: "new-id", OriginalID: "old-id", Name: "renamed", BaseURL: "https://guard.example.com",
		Model: "guard", TimeoutMS: 1000, InputLimit: 4000, Concurrency: 2, Enabled: true,
	}}
	merged := mergePromptAuditEndpointUpdates(current, updates)
	require.Len(t, merged, 1)
	assert.Equal(t, "new-id", merged[0].ID)
	assert.Equal(t, "top-secret-token", merged[0].Token)

	response := promptAuditConfigResponse(prompt_audit_setting.PromptAuditSetting{Endpoints: merged})
	encoded, err := common.Marshal(response)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "top-secret-token")
	assert.Contains(t, string(encoded), `"has_token":true`)

	clear := ""
	updates[0].Token = &clear
	cleared := mergePromptAuditEndpointUpdates(current, updates)
	require.Len(t, cleared, 1)
	assert.Empty(t, cleared[0].Token)
}

func TestPromptAuditEndpointURLChangeCannotForwardStoredToken(t *testing.T) {
	current := []prompt_audit_setting.Endpoint{{
		ID: "primary", BaseURL: "https://guard.example.com/v1", Token: "top-secret-token",
	}}
	updates := []promptAuditEndpointUpdate{{
		ID: "primary", Name: "redirected", BaseURL: "https://attacker.example.com/v1",
		Model: "guard", TimeoutMS: 1000, InputLimit: 4000, Concurrency: 2, Enabled: true,
	}}

	merged := mergePromptAuditEndpointUpdates(current, updates)
	require.Len(t, merged, 1)
	assert.Empty(t, merged[0].Token)

	replacement := "new-explicit-token"
	updates[0].Token = &replacement
	merged = mergePromptAuditEndpointUpdates(current, updates)
	require.Len(t, merged, 1)
	assert.Equal(t, replacement, merged[0].Token)
}
