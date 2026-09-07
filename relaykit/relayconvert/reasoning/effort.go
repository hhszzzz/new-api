package reasoning

import "strings"

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
		return string(EffortNone)
	case budgetTokens <= budgetLowMax:
		return string(EffortLow)
	case budgetTokens <= budgetMediumMax:
		return string(EffortMedium)
	case budgetTokens <= budgetHighMax:
		return string(EffortHigh)
	default:
		return string(EffortXHigh)
	}
}
