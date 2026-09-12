package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rebuildRadarAutoEffortIndex installs a radar snapshot for the resolver to
// read, replacing whatever the previous test left in the process-wide index.
// Radar auto-effort is opt-in per model, so the snapshot's models are also
// allowed by the administrator unless a test overrides the allowlist.
func rebuildRadarAutoEffortIndex(t *testing.T, configurations ...service.ModelRadarConfiguration) {
	t.Helper()
	models := make([]string, 0, len(configurations))
	for _, configuration := range configurations {
		models = append(models, configuration.Model)
	}
	// The allowlist must be in place before the index snapshots the raw option,
	// otherwise the cached index looks stale and the lookup falls back to the DB.
	allowRadarModels(t, models...)
	service.RebuildModelRadarAutoEffortIndex(&service.ModelRadarData{
		FetchedAt:      common.GetTimestamp(),
		Configurations: configurations,
	})
}

// allowRadarModels replaces the administrator's per-model opt-in list for the
// rest of the test.
func allowRadarModels(t *testing.T, models ...string) {
	t.Helper()
	overrides := make(map[string]any, len(models))
	for _, model := range models {
		overrides[model] = map[string]any{"auto_effort": true}
	}
	payload, err := common.Marshal(map[string]any{
		"auto_effort_enabled": true,
		"models":              overrides,
	})
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	previous, had := common.OptionMap["ModelRadarSettings"]
	common.OptionMap["ModelRadarSettings"] = string(payload)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if had {
			common.OptionMap["ModelRadarSettings"] = previous
		} else {
			delete(common.OptionMap, "ModelRadarSettings")
		}
		common.OptionMapRWMutex.Unlock()
	})
}

func radarAutoEffortSetting(policy string, minIQDelta float64, models ...string) *dto.RadarAutoEffortSetting {
	enabled := make(map[string]dto.RadarAutoEffortModel, len(models))
	for _, model := range models {
		enabled[model] = dto.RadarAutoEffortModel{Enabled: true}
	}
	return &dto.RadarAutoEffortSetting{Policy: policy, MinIQDelta: minIQDelta, Models: enabled}
}

func newRadarAutoEffortRelayInfo(model string, request dto.Request, setting *dto.RadarAutoEffortSetting) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		RelayFormat:     types.RelayFormatOpenAI,
		Request:         request,
		UserSetting:     dto.UserSetting{RadarAutoEffort: setting},
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: model},
	}
}

func radarAutoEffortRequest(model string, effort string) *dto.GeneralOpenAIRequest {
	request := &dto.GeneralOpenAIRequest{Model: model}
	if effort != "" {
		request.ReasoningEffort = effort
	}
	return request
}

func TestResolveRadarAutoEffortReplacesExplicitTier(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "medium", IQ: 80, ValidTasks: 10},
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "low", IQ: 60, ValidTasks: 10},
	)
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "low"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	require.NotNil(t, info.RadarAutoEffort)
	assert.True(t, info.RadarAutoEffort.Applied)
	assert.Equal(t, "", info.RadarAutoEffort.SkipReason)
	assert.Equal(t, reasoning.EffortLow, info.RadarAutoEffort.From)
	assert.Equal(t, reasoning.EffortHigh, info.RadarAutoEffort.To)
	assert.Equal(t, 90.0, info.RadarAutoEffort.IQ)
	assert.Equal(t, "gpt-test", info.BillingModelName)
}

func TestResolveRadarAutoEffortSkipsWhenClientKeptTheDefaultTier(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
	)
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", ""),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	require.NotNil(t, info.RadarAutoEffort)
	assert.False(t, info.RadarAutoEffort.Applied)
	assert.Equal(t, relaycommon.RadarAutoEffortSkipClientDefault, info.RadarAutoEffort.SkipReason)
	assert.Empty(t, info.BillingModelName)
}

func TestResolveRadarAutoEffortIgnoresDisabledModels(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
	)
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "low"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "other-model"))

	ResolveRadarAutoEffort(nil, info)

	assert.Nil(t, info.RadarAutoEffort)
	assert.Empty(t, info.BillingModelName)
}

func TestResolveRadarAutoEffortIgnoresModelsTheAdministratorDidNotAllow(t *testing.T) {
	// The user switched the model on, but the administrator never opted it in.
	allowRadarModels(t, "other-model")
	service.RebuildModelRadarAutoEffortIndex(&service.ModelRadarData{
		FetchedAt: common.GetTimestamp(),
		Configurations: []service.ModelRadarConfiguration{
			{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
		},
	})
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "low"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	assert.Nil(t, info.RadarAutoEffort)
	assert.Empty(t, info.BillingModelName)
}

func TestResolveRadarAutoEffortIgnoresAStoredStrategyWithoutEnabledModels(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
	)
	// Disabling the last model keeps a non-default strategy stored, so the
	// resolver must still treat the request as opted out.
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "low"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyIQPerCost, 30))

	ResolveRadarAutoEffort(nil, info)

	assert.Nil(t, info.RadarAutoEffort)
	assert.Empty(t, info.BillingModelName)
}

func TestResolveRadarAutoEffortSkipsStaleSnapshot(t *testing.T) {
	allowRadarModels(t, "gpt-test")
	service.RebuildModelRadarAutoEffortIndex(&service.ModelRadarData{
		FetchedAt: common.GetTimestamp() - int64(service.ModelRadarStaleAfter().Seconds()) - 1,
		Configurations: []service.ModelRadarConfiguration{
			{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
		},
	})
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "low"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	require.NotNil(t, info.RadarAutoEffort)
	assert.False(t, info.RadarAutoEffort.Applied)
	assert.Equal(t, relaycommon.RadarAutoEffortSkipStale, info.RadarAutoEffort.SkipReason)
	assert.Empty(t, info.BillingModelName)
}

func TestResolveRadarAutoEffortKeepsTierWithinMinimumIQDelta(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "medium", IQ: 88, ValidTasks: 10},
	)
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "medium"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyMinIQDelta, 5, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	require.NotNil(t, info.RadarAutoEffort)
	assert.False(t, info.RadarAutoEffort.Applied)
	assert.Equal(t, relaycommon.RadarAutoEffortSkipAlreadyBest, info.RadarAutoEffort.SkipReason)
	assert.Equal(t, reasoning.EffortMedium, info.RadarAutoEffort.To)
	assert.Empty(t, info.BillingModelName)
}

func TestResolveRadarAutoEffortReplacesTierBeyondMinimumIQDelta(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "medium", IQ: 80, ValidTasks: 10},
	)
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "medium"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyMinIQDelta, 5, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	require.NotNil(t, info.RadarAutoEffort)
	assert.True(t, info.RadarAutoEffort.Applied)
	assert.Equal(t, reasoning.EffortHigh, info.RadarAutoEffort.To)
}

func TestResolveRadarAutoEffortIQPerCostPrefersCheaperTier(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "xhigh", IQ: 95, ValidTasks: 10, AveragePriceUSD: common.GetPointer(50.0)},
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "medium", IQ: 85, ValidTasks: 10, AveragePriceUSD: common.GetPointer(1.0)},
	)
	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "low"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyIQPerCost, 0, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	require.NotNil(t, info.RadarAutoEffort)
	assert.True(t, info.RadarAutoEffort.Applied)
	assert.Equal(t, reasoning.EffortMedium, info.RadarAutoEffort.To)
}

func TestResolveRadarAutoEffortReplacesModifierSelectedTier(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
	)
	info := newRadarAutoEffortRelayInfo("gpt-test@effort:low", radarAutoEffortRequest("gpt-test@effort:low", ""),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	require.NotNil(t, info.RadarAutoEffort)
	assert.True(t, info.RadarAutoEffort.Applied)
	assert.Equal(t, reasoning.EffortLow, info.RadarAutoEffort.From)
	assert.Equal(t, reasoning.EffortHigh, info.RadarAutoEffort.To)
}

func TestResolveRadarAutoEffortKeepsGeminiBillingIdentity(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gemini-test", Effort: "high", IQ: 90, ValidTasks: 10},
	)
	info := newRadarAutoEffortRelayInfo("gemini-test", radarAutoEffortRequest("gemini-test", "low"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gemini-test"))
	info.RelayFormat = types.RelayFormatGemini
	info.BillingModelName = "gemini-test-nothinking"

	ResolveRadarAutoEffort(nil, info)

	require.NotNil(t, info.RadarAutoEffort)
	assert.True(t, info.RadarAutoEffort.Applied)
	assert.Equal(t, "gemini-test-nothinking", info.BillingModelName)
}

func TestResolveRadarAutoEffortPassThroughIsUntouched(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = original })
	settings.PassThroughRequestEnabled = true

	info := newRadarAutoEffortRelayInfo("gpt-test", radarAutoEffortRequest("gpt-test", "low"),
		radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)

	assert.Nil(t, info.RadarAutoEffort)
	assert.Empty(t, info.BillingModelName)
}

func TestResolveBillingModelNameForEffortFollowsTheReplacement(t *testing.T) {
	t.Cleanup(ratio_setting.InitRatioSettings)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{
		"gpt-test@effort:high@thinking:on": 1.5,
		"gpt-test@effort:low@thinking:on": 0.5
	}`))

	assert.Equal(t, "gpt-test@effort:high@thinking:on", resolveBillingModelNameForEffort("gpt-test", reasoning.EffortHigh))
	assert.Equal(t, "gpt-test@effort:low@thinking:on", resolveBillingModelNameForEffort("gpt-test", reasoning.EffortLow))
	// Nothing priced for this tier or the bare base, so the base is the identity.
	assert.Equal(t, "gpt-test", resolveBillingModelNameForEffort("gpt-test", reasoning.EffortMax))
	assert.Equal(t, "", resolveBillingModelNameForEffort("", reasoning.EffortHigh))
}

func TestApplyReasoningModelSuffixAppliesRadarDecision(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
	)
	request := radarAutoEffortRequest("gpt-test", "low")
	info := newRadarAutoEffortRelayInfo("gpt-test", request, radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gpt-test"))

	ResolveRadarAutoEffort(nil, info)
	require.NoError(t, ApplyReasoningModelSuffix(nil, info, request))

	assert.Equal(t, "gpt-test", info.UpstreamModelName)
	assert.Equal(t, "high", request.ReasoningEffort)
	assert.Equal(t, "high", info.GetReasoningEffort())
	require.NotNil(t, info.ReasoningConversion)
	assert.Equal(t, "high", info.ReasoningConversion.Effort)
	require.NotEmpty(t, info.ConversionDiagnostics())
	assert.Equal(t, "radar_auto_effort_applied", info.ConversionDiagnostics()[0].Code)
}

func TestApplyReasoningModelSuffixKeepsMappedModelModifierOverRadarDecision(t *testing.T) {
	rebuildRadarAutoEffortIndex(t,
		service.ModelRadarConfiguration{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10},
	)
	request := radarAutoEffortRequest("gpt-test", "low")
	info := newRadarAutoEffortRelayInfo("gpt-test", request, radarAutoEffortSetting(dto.RadarAutoEffortPolicyHighestIQ, 0, "gpt-test"))
	// The channel maps the request onto a tier of its own.
	info.ChannelMeta.UpstreamModelName = "gpt-test-xhigh"
	info.ChannelMeta.IsModelMapped = true

	ResolveRadarAutoEffort(nil, info)
	require.NoError(t, ApplyReasoningModelSuffix(nil, info, request))

	assert.Equal(t, "gpt-test", info.UpstreamModelName)
	assert.Equal(t, "xhigh", request.ReasoningEffort)
}
