package chat

import "strings"

func UsesMaxCompletionTokens(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return len(model) > 1 && model[0] == 'o' && model[1] >= '0' && model[1] <= '9'
}

func SupportsReasoningEffort(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if UsesMaxCompletionTokens(model) {
		return true
	}
	if strings.HasPrefix(model, "gpt-") && len(model) > len("gpt-") {
		first := model[len("gpt-")]
		if first >= '5' && first <= '9' {
			return true
		}
	}
	return model == "grok-4.5" || strings.HasPrefix(model, "grok-4.5-") || strings.HasPrefix(model, "grok-build-")
}
