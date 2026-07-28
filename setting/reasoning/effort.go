package reasoning

import kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"

// Effort levels recorded on the consume log. They match the OpenAI
// reasoning_effort vocabulary so the log UI renders one shared badge for every
// provider instead of a per-provider dialect.
const (
	EffortMinimal = kitreasoning.EffortMinimal
	EffortLow     = kitreasoning.EffortLow
	EffortMedium  = kitreasoning.EffortMedium
	EffortHigh    = kitreasoning.EffortHigh
	EffortXHigh   = kitreasoning.EffortXHigh
	EffortMax     = kitreasoning.EffortMax
	EffortNone    = kitreasoning.EffortNone
)

// NormalizeEffort maps a provider-specific effort string onto the shared
// vocabulary. Unknown values are returned lowercased so a new upstream level
// still reaches the log instead of being silently dropped.
func NormalizeEffort(effort string) string {
	return kitreasoning.NormalizeEffort(effort)
}

// ClaudeEffort resolves the effort level of an Anthropic request. Anthropic
// expresses effort either natively (output_config.effort, used with adaptive
// thinking) or as a token budget on enabled thinking, so both spellings are
// folded into the shared vocabulary. An empty result means thinking is off.
func ClaudeEffort(nativeEffort string, thinkingEnabled bool, budgetTokens int) string {
	return kitreasoning.ClaudeEffort(nativeEffort, thinkingEnabled, budgetTokens)
}

// EffortFromBudgetTokens describes a thinking-token budget as an effort level.
// Providers that only accept a budget (Anthropic thinking.budget_tokens, Gemini
// thinkingBudget) can then report the same levels as providers with a native
// effort field. A non-positive budget means thinking is disabled.
func EffortFromBudgetTokens(budgetTokens int) string {
	return kitreasoning.EffortFromBudgetTokens(budgetTokens)
}
