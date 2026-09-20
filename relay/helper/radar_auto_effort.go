package helper

import (
	"fmt"
	hostdto "github.com/QuantumNous/new-api/dto"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

// ResolveRadarAutoEffort decides, before pricing runs, whether the reasoning
// tier the client explicitly selected should be replaced by the tier the model
// radar scores best for this user's strategy. ApplyReasoningModelSuffix owns
// every outbound request write and performs the replacement; this step only
// records the decision and, because the pricing identity is fixed before the
// handler runs, the billing name that goes with it. Anything unexpected leaves
// the client's own tier in place.
func ResolveRadarAutoEffort(c *gin.Context, info *relaycommon.RelayInfo) {
	if info == nil || info.UserSetting.RadarAutoEffort == nil {
		return
	}
	// A user who configured a strategy but switched every model off keeps the
	// setting stored, so the hot path stops here before touching the radar.
	if len(info.UserSetting.RadarAutoEffort.Models) == 0 {
		return
	}
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return
	}
	radarSettings := setting.GetModelRadarSettings()
	if !radarSettings.AutoEffortEnabled {
		return
	}
	if !isRadarAutoEffortRelayFormat(info.RelayFormat) {
		return
	}

	autoSetting := info.UserSetting.RadarAutoEffort
	parsed, err := parseRequestModelName(info.GetOriginModelName(), info.ConvOptions())
	if err != nil || parsed.base == "" {
		return
	}
	candidates, ok := service.LookupModelRadarCandidates(parsed.base)
	if !ok || len(candidates.Candidates) == 0 {
		return
	}
	if !autoSetting.EnabledModel(candidates.Model) {
		return
	}
	// The administrator opts each model in; an unlisted model must never have
	// its tier replaced even when the user switched it on earlier.
	if !radarSettings.Models[candidates.Model].AutoEffort {
		return
	}

	decision := &relaycommon.RadarAutoEffortDecision{
		Model:     candidates.Model,
		Base:      parsed.base,
		Policy:    autoSetting.EffectivePolicy(candidates.Model),
		FetchedAt: candidates.FetchedAt,
	}
	// Record the decision before any skip so the request log can explain it.
	info.RadarAutoEffort = decision

	if service.IsModelRadarStale(common.GetTimestamp(), candidates.FetchedAt, service.ModelRadarStaleAfter()) {
		decision.SkipReason = relaycommon.RadarAutoEffortSkipStale
		return
	}

	explicit, _, err := explicitIntentFromRequest(info.Request)
	if err != nil {
		decision.SkipReason = relaycommon.RadarAutoEffortSkipInvalidRequest
		return
	}
	clientIntent := parsed.intent
	if !parsed.hasThinking {
		clientIntent = explicit
	}
	if !parsed.hasThinking && !explicit.HasStrength() {
		decision.SkipReason = relaycommon.RadarAutoEffortSkipClientDefault
		return
	}

	from := reasoning.EffectiveEffort(clientIntent)
	to, iq, replace := pickRadarAutoEffort(candidates.Candidates, decision.Policy, autoSetting.MinIQDeltaOrDefault(), from)
	decision.From = from
	decision.To = to
	decision.IQ = iq
	if !replace {
		decision.SkipReason = relaycommon.RadarAutoEffortSkipAlreadyBest
		return
	}

	// Pricing and pre-consume already ran with the client's tier, so the
	// billing identity must move with the replacement.
	decision.Applied = true
	if info.BillingModelName == "" {
		info.BillingModelName = resolveBillingModelNameForEffort(parsed.base, to)
	}
	if c != nil {
		logger.LogDebug(c, fmt.Sprintf("radar auto-effort: model %s reasoning effort %s -> %s (policy %s, IQ %.1f)",
			parsed.base, from, to, decision.Policy, iq))
	}
}

func isRadarAutoEffortRelayFormat(format types.RelayFormat) bool {
	switch format {
	case types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini, types.RelayFormatOpenAIResponses:
		return true
	default:
		return false
	}
}

// pickRadarAutoEffort returns the tier a request should use under the configured
// strategy. replace is false when the client's own tier is already the choice,
// either because it is the best candidate or because it is within the minimum
// IQ gap.
func pickRadarAutoEffort(candidates []service.ModelRadarEffortCandidate, policy string, minIQDelta float64, from reasoning.Effort) (reasoning.Effort, float64, bool) {
	if len(candidates) == 0 {
		return from, 0, false
	}
	// The index orders candidates by descending IQ and lower effort first on
	// ties, so the head is the highest-IQ tier.
	best := candidates[0]
	switch policy {
	case hostdto.RadarAutoEffortPolicyIQPerCost:
		if affordable, ok := bestRadarIQPerCost(candidates); ok {
			best = affordable
		}
	case hostdto.RadarAutoEffortPolicyMinIQDelta:
		client, ok := findRadarCandidate(candidates, from)
		if ok && best.IQ-client.IQ < minIQDelta {
			return from, client.IQ, false
		}
	}
	if best.Effort == from {
		return from, best.IQ, false
	}
	return best.Effort, best.IQ, true
}

func bestRadarIQPerCost(candidates []service.ModelRadarEffortCandidate) (service.ModelRadarEffortCandidate, bool) {
	var (
		best      service.ModelRadarEffortCandidate
		bestScore float64
		found     bool
	)
	for _, candidate := range candidates {
		if candidate.PriceUSD == nil || *candidate.PriceUSD <= 0 {
			continue
		}
		score := candidate.IQ / *candidate.PriceUSD
		if !found || score > bestScore {
			best, bestScore, found = candidate, score, true
		}
	}
	return best, found
}

func findRadarCandidate(candidates []service.ModelRadarEffortCandidate, effort reasoning.Effort) (service.ModelRadarEffortCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.Effort == effort {
			return candidate, true
		}
	}
	return service.ModelRadarEffortCandidate{}, false
}

// radarEffortIntent converts a radar-chosen tier into the intent that drives
// both the outbound request fields and the billing identity.
func radarEffortIntent(effort reasoning.Effort) reasoning.Intent {
	if effort == reasoning.EffortNone {
		return reasoning.Intent{Mode: reasoning.ModeDisabled, Effort: reasoning.EffortNone, Source: reasoning.SourceSuffix}
	}
	return reasoning.Intent{Mode: reasoning.ModeEnabled, Effort: effort, Source: reasoning.SourceSuffix}
}
