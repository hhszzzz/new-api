package claude

import "strings"

func UsesAdaptiveThinking(model string) bool {
	normalized := normalizeThinkingModelName(model)
	for _, marker := range []string{
		"fable-5",
		"mythos-5",
		"mythos-preview",
		"sonnet-5",
		"opus-4-8",
		"opus-4-7",
		"opus-4-6",
		"sonnet-4-6",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func AdaptiveThinkingIsDefault(model string) bool {
	normalized := normalizeThinkingModelName(model)
	for _, marker := range []string{"fable-5", "mythos-5", "mythos-preview", "sonnet-5"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func ThinkingCannotBeDisabled(model string) bool {
	normalized := normalizeThinkingModelName(model)
	return strings.Contains(normalized, "fable-5") || strings.Contains(normalized, "mythos-5")
}

func AdaptiveEffort(effort string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal", "low":
		return "low", true
	case "medium":
		return "medium", true
	case "high":
		return "high", true
	case "xhigh", "max":
		return "max", true
	default:
		return "", false
	}
}

func ReasoningExplicitlyDisabled(effort string) bool {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none", "off", "disabled":
		return true
	default:
		return false
	}
}

func normalizeThinkingModelName(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return strings.NewReplacer(".", "-", "_", "-").Replace(normalized)
}
