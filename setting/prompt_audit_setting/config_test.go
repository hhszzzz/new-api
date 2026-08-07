package prompt_audit_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSetting() PromptAuditSetting {
	return PromptAuditSetting{
		Mode:                ModeBlocking,
		EnabledCategories:   append([]string(nil), AllCategoryIDs...),
		AllGroups:           true,
		Endpoints:           []Endpoint{{ID: "primary", BaseURL: "https://guard.example.com", Token: "secret", Model: DefaultModel, TimeoutMS: DefaultEndpointTimeoutMS, InputLimit: DefaultEndpointInputLimit, Concurrency: DefaultEndpointConcurrency, Enabled: true}},
		TotalTimeoutMS:      DefaultTotalTimeoutMS,
		ChunkOverlap:        DefaultChunkOverlap,
		CacheTTLSeconds:     DefaultCacheTTLSeconds,
		WorkerCount:         DefaultWorkerCount,
		MaxAttempts:         DefaultMaxAttempts,
		RetentionDays:       DefaultRetentionDays,
		GlobalConcurrency:   DefaultGlobalConcurrency,
		EndpointConcurrency: DefaultEndpointConcurrency,
	}
}

func TestPromptAuditSettingValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*PromptAuditSetting)
		wantErr string
	}{
		{name: "valid", mutate: func(*PromptAuditSetting) {}},
		{name: "token optional for private nodes", mutate: func(setting *PromptAuditSetting) { setting.Endpoints[0].Token = "" }},
		{name: "invalid mode", mutate: func(setting *PromptAuditSetting) { setting.Mode = "audit" }, wantErr: "mode"},
		{name: "invalid URL", mutate: func(setting *PromptAuditSetting) { setting.Endpoints[0].BaseURL = "file:///tmp/reviewer" }, wantErr: "HTTP(S)"},
		{name: "URL credentials", mutate: func(setting *PromptAuditSetting) { setting.Endpoints[0].BaseURL = "https://user:pass@example.com" }, wantErr: "credentials"},
		{name: "URL query", mutate: func(setting *PromptAuditSetting) { setting.Endpoints[0].BaseURL = "https://example.com?q=secret" }, wantErr: "query"},
		{name: "invalid timeout", mutate: func(setting *PromptAuditSetting) { setting.TotalTimeoutMS = 0 }, wantErr: "total timeout"},
		{name: "more than three retries", mutate: func(setting *PromptAuditSetting) { setting.MaxAttempts = MaxAttemptsLimit + 1 }, wantErr: "max attempts"},
		{name: "invalid input limit", mutate: func(setting *PromptAuditSetting) { setting.Endpoints[0].InputLimit = 128 }, wantErr: "input limit"},
		{name: "overlap reaches limit", mutate: func(setting *PromptAuditSetting) { setting.ChunkOverlap = setting.Endpoints[0].InputLimit }, wantErr: "overlap"},
		{name: "unknown category", mutate: func(setting *PromptAuditSetting) { setting.EnabledCategories = []string{"future"} }, wantErr: "unknown"},
		{name: "duplicate category", mutate: func(setting *PromptAuditSetting) { setting.EnabledCategories = []string{"pii", "pii"} }, wantErr: "duplicate"},
		{name: "selected groups required", mutate: func(setting *PromptAuditSetting) { setting.AllGroups = false }, wantErr: "group"},
		{name: "enabled endpoint required", mutate: func(setting *PromptAuditSetting) { setting.Endpoints[0].Enabled = false }, wantErr: "enabled endpoint"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting := validSetting()
			test.mutate(&setting)
			err := setting.ValidateConfig()
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestOffModeAllowsNoEndpointAndSnapshotIsDeepCopied(t *testing.T) {
	setting := validSetting()
	setting.Mode = ModeOff
	setting.Endpoints = nil
	require.NoError(t, setting.ValidateConfig())

	setting.PublishConfig()
	first := GetSetting()
	require.NotEmpty(t, first.ConfigVersion)
	first.EnabledCategories[0] = "mutated"
	second := GetSetting()
	assert.Equal(t, AllCategoryIDs[0], second.EnabledCategories[0])
}

func TestGroupSelection(t *testing.T) {
	setting := validSetting()
	setting.AllGroups = false
	setting.Groups = []string{"paid", "vip"}
	require.NoError(t, setting.ValidateConfig())
	setting.PublishConfig()
	snapshot := GetSetting()
	assert.True(t, snapshot.AppliesToGroup("paid"))
	assert.True(t, snapshot.AppliesToGroup("auto"))
	assert.False(t, snapshot.AppliesToGroup("default"))
}
