package reasoning

import "strings"

const (
	EffortMinimal = "minimal"
	EffortLow     = "low"
	EffortMedium  = "medium"
	EffortHigh    = "high"
	EffortXHigh   = "xhigh"
	EffortMax     = "max"
	EffortNone    = "none"
)

const (
	budgetLowMax    = 2048
	budgetMediumMax = 8192
	budgetHighMax   = 24576
)

func NormalizeEffort(effort string) string {
	return strings.ToLower(strings.TrimSpace(effort))
}

func ClaudeEffort(nativeEffort string, thinkingEnabled bool, budgetTokens int) string {
	if effort := NormalizeEffort(nativeEffort); effort != "" {
		return effort
	}
	if thinkingEnabled {
		return EffortFromBudgetTokens(budgetTokens)
	}
	return ""
}

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
