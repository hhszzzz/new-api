package reasoning

import "strings"

// Effort levels recorded on the consume log. They match the OpenAI
// reasoning_effort vocabulary so the log UI renders one shared badge for every
// provider instead of a per-provider dialect.
const (
	EffortMinimal = "minimal"
	EffortLow     = "low"
	EffortMedium  = "medium"
	EffortHigh    = "high"
	EffortXHigh   = "xhigh"
	EffortMax     = "max"
	EffortNone    = "none"
)

// Thinking-budget boundaries used to describe a token budget as an effort
// level. Anthropic requires at least 1024 budget tokens and Gemini accepts up
// to 32768, so the buckets cover both scales: the value is a request-shaping
// hint, and the log only needs the magnitude to be recognizable.
const (
	budgetLowMax    = 2048
	budgetMediumMax = 8192
	budgetHighMax   = 24576
)

// NormalizeEffort maps a provider-specific effort string onto the shared
// vocabulary. Unknown values are returned lowercased so a new upstream level
// still reaches the log instead of being silently dropped.
func NormalizeEffort(effort string) string {
	return strings.ToLower(strings.TrimSpace(effort))
}

// ClaudeEffort resolves the effort level of an Anthropic request. Anthropic
// expresses effort either natively (output_config.effort, used with adaptive
// thinking) or as a token budget on enabled thinking, so both spellings are
// folded into the shared vocabulary. An empty result means thinking is off.
func ClaudeEffort(nativeEffort string, thinkingEnabled bool, budgetTokens int) string {
	if effort := NormalizeEffort(nativeEffort); effort != "" {
		return effort
	}
	if thinkingEnabled {
		return EffortFromBudgetTokens(budgetTokens)
	}
	return ""
}

// EffortFromBudgetTokens describes a thinking-token budget as an effort level.
// Providers that only accept a budget (Anthropic thinking.budget_tokens, Gemini
// thinkingBudget) can then report the same levels as providers with a native
// effort field. A non-positive budget means thinking is disabled.
func EffortFromBudgetTokens(budgetTokens int) string {
	switch {
	case budgetTokens <= 0:
		return EffortNone
	case budgetTokens <= budgetLowMax:
		return EffortLow
	case budgetTokens <= budgetMediumMax:
		return EffortMedium
	case budgetTokens <= budgetHighMax:
		return EffortHigh
	default:
		return EffortXHigh
	}
}
