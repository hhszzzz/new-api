package prompt_audit_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptAuditProbeDefaultsAndScopeInheritance(t *testing.T) {
	old := GetSetting()
	t.Cleanup(func() { old.PublishConfig() })
	configured := validSetting()
	require.NoError(t, configured.ValidateConfig())
	assert.Contains(t, configured.ProbePhrases, "你好")
	configured.ProbePhrases = []string{}
	configured.PublishConfig()
	assert.NotNil(t, GetSetting().ProbePhrases)
	assert.Empty(t, GetSetting().ProbePhrases)
	configured.ProbePhrases = []string{"custom"}
	configured.PublishConfig()
	configured = validSetting()
	require.NoError(t, configured.ValidateConfig())
	assert.Contains(t, configured.ProbePhrases, "hello")
	configured.ScopePolicies = map[dto.PromptAuditScope]ScopePolicy{
		dto.PromptScopeUser:       {LibraryIDs: []string{"one"}, ModelAudit: true},
		dto.PromptScopeToolResult: {LibraryIDs: []string{"two"}, ModelAudit: false},
	}
	assert.Equal(t, configured.PolicyFor(dto.PromptScopeUser), configured.PolicyFor(dto.PromptScopeAgentContext))
	assert.Equal(t, configured.PolicyFor(dto.PromptScopeUser), configured.PolicyFor(dto.PromptScopeSkill))
	assert.Equal(t, configured.PolicyFor(dto.PromptScopeToolResult), configured.PolicyFor(dto.PromptScopeMCP))
	configured.ScopePolicies[dto.PromptScopeSkill] = ScopePolicy{}
	assert.False(t, configured.PolicyFor(dto.PromptScopeSkill).ModelAudit)
	assert.Empty(t, configured.PolicyFor(dto.PromptScopeSkill).LibraryIDs)
}

func validSetting() PromptAuditSetting {
	return PromptAuditSetting{
		Mode:                   ModeBlocking,
		OutputMode:             ModeOff,
		BlockingLatestTurnOnly: true,
		ManualWordlistAction:   WordlistActionBlock,
		EnabledCategories:      append([]string(nil), AllCategoryIDs...),
		ControversialBlocks:    []string{"jailbreak", "pii", "suicide_and_self_harm"},
		AllGroups:              true,
		Endpoints:              []Endpoint{{ID: "primary", BaseURL: "https://guard.example.com", Token: "secret", Model: DefaultModel, TimeoutMS: DefaultEndpointTimeoutMS, InputLimit: DefaultEndpointInputLimit, Concurrency: DefaultEndpointConcurrency, Enabled: true}},
		TotalTimeoutMS:         DefaultTotalTimeoutMS,
		ChunkOverlap:           DefaultChunkOverlap,
		ChunkConcurrency:       DefaultChunkConcurrency,
		CacheTTLSeconds:        DefaultCacheTTLSeconds,
		WorkerCount:            DefaultWorkerCount,
		MaxAttempts:            DefaultMaxAttempts,
		RetentionDays:          DefaultRetentionDays,
		GlobalConcurrency:      DefaultGlobalConcurrency,
		EndpointConcurrency:    DefaultEndpointConcurrency,
		OutputMaxBytes:         DefaultOutputMaxBytes,
		OutputMemoryBytes:      DefaultOutputMemoryBytes,
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
		{name: "chunk concurrency out of range", mutate: func(setting *PromptAuditSetting) { setting.ChunkConcurrency = 17 }, wantErr: "chunk concurrency"},
		{name: "unknown category", mutate: func(setting *PromptAuditSetting) { setting.EnabledCategories = []string{"future"} }, wantErr: "unknown"},
		{name: "duplicate category", mutate: func(setting *PromptAuditSetting) { setting.EnabledCategories = []string{"pii", "pii"} }, wantErr: "duplicate"},
		{name: "selected groups required", mutate: func(setting *PromptAuditSetting) { setting.AllGroups = false }, wantErr: "group"},
		{name: "enabled endpoint required", mutate: func(setting *PromptAuditSetting) { setting.Endpoints[0].Enabled = false }, wantErr: "enabled endpoint"},
		{name: "output direction required", mutate: func(setting *PromptAuditSetting) {
			setting.OutputMode = ModeBlocking
			setting.Endpoints[0].Directions = []string{"input"}
		}, wantErr: "output classification"},
		{name: "invalid direction", mutate: func(setting *PromptAuditSetting) { setting.Endpoints[0].Directions = []string{"future"} }, wantErr: "invalid direction"},
		{name: "review endpoint required", mutate: func(setting *PromptAuditSetting) { setting.ReviewEnabled = true }, wantErr: "review endpoint"},
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

func TestSettingFingerprintTracksBlockingLatestTurnOnly(t *testing.T) {
	// promptAuditCacheKey mixes ConfigVersion into its digest, so a fingerprint
	// that ignored this field would keep serving a verdict computed under the
	// other scan scope after an operator flips the switch. The stale verdict is
	// indistinguishable from a correct one, which is why this needs a test.
	setting := validSetting()
	original := GetSetting()
	t.Cleanup(func() { original.PublishConfig() })
	narrow := settingFingerprint(setting)

	setting.BlockingLatestTurnOnly = false
	wide := settingFingerprint(setting)

	assert.NotEqual(t, narrow, wide)

	setting.BlockingLatestTurnOnly = true
	setting.PublishConfig()
	published := GetSetting().ConfigVersion
	setting.BlockingLatestTurnOnly = false
	setting.PublishConfig()
	assert.NotEqual(t, published, GetSetting().ConfigVersion)
}

func TestSettingFingerprintTracksChunkConcurrency(t *testing.T) {
	// The batch boundary decides where a blocked chunk stops the scan, so two
	// values can record different category evidence for identical text. Without
	// the fingerprint entry, promptAuditCacheKey would keep serving a verdict
	// computed under the other batch size, and the difference would be invisible.
	setting := validSetting()
	serial := settingFingerprint(setting)
	setting.ChunkConcurrency = DefaultChunkConcurrency + 1
	assert.NotEqual(t, serial, settingFingerprint(setting))
}

func TestChunkConcurrencyAboveEndpointBudgetIsAccepted(t *testing.T) {
	// promptAuditBatchSize clamps the batch to the smaller budget, so the pair is
	// valid. Rejecting it would instead block every settings save after an
	// operator lowers endpoint concurrency while leaving chunk concurrency at its
	// default, which is an upgrade hazard rather than a safety gain.
	setting := validSetting()
	setting.EndpointConcurrency = 2
	setting.ChunkConcurrency = DefaultChunkConcurrency
	require.NoError(t, setting.ValidateConfig())
}

func TestBlockingLatestTurnOnlyOptionKeyReachesTheField(t *testing.T) {
	// Guards the json tag end to end. A typo would make the controller persist a
	// key that never lands on the field, silently disabling the switch while the
	// UI kept reporting it. UpdateFromDB is the exact path
	// model.SyncPromptInspectionOptions uses to apply persisted values.
	original := GetSetting()
	t.Cleanup(func() { original.PublishConfig() })

	setting := validSetting()
	setting.BlockingLatestTurnOnly = true
	setting.PublishConfig()
	require.True(t, GetSetting().BlockingLatestTurnOnly)

	changed, err := config.GlobalConfig.UpdateFromDB("prompt_audit", map[string]string{"blocking_latest_turn_only": "false"})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.False(t, GetSetting().BlockingLatestTurnOnly)
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
