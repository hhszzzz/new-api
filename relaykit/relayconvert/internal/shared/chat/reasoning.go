package chat

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// ApplyReasoningEffort maps a client-requested reasoning effort onto the chat
// request using the control convention inferred from the upstream model name,
// mirroring CC Switch's per-provider reasoning presets:
//   - OpenAI-style models (o-series, gpt-5+, grok): top-level reasoning_effort.
//   - DeepSeek: thinking {"type"} plus reasoning_effort clamped to high/max
//     (DeepSeek's effort enum has no low/medium/none tiers).
//   - Kimi/Moonshot and GLM: thinking {"type"} only, no effort tiers.
//
// "none"/"off"/"disabled" efforts emit an explicit thinking disable for the
// families above — several of these models think by default, so dropping the
// field would silently keep reasoning on. An empty effort is a no-op, and any
// other model family drops the effort entirely: a strict OpenAI-compatible
// gateway may reject non-standard fields with a 400.
func ApplyReasoningEffort(out *dto.GeneralOpenAIRequest, effort string) {
	if out == nil {
		return
	}
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" {
		return
	}
	if SupportsReasoningEffort(out.Model) {
		out.ReasoningEffort = effort
		return
	}

	family := thinkingModelFamily(out.Model)
	if family == "" {
		return
	}
	enabled := effort != "none" && effort != "off" && effort != "disabled"
	if enabled {
		out.THINKING = json.RawMessage(`{"type":"enabled"}`)
	} else {
		out.THINKING = json.RawMessage(`{"type":"disabled"}`)
	}
	if family == thinkingFamilyDeepSeek && enabled {
		if effort == "max" || effort == "xhigh" {
			out.ReasoningEffort = "max"
		} else {
			out.ReasoningEffort = "high"
		}
	}
}

const (
	thinkingFamilyDeepSeek = "deepseek"
	thinkingFamilyThinking = "thinking"
)

func thinkingModelFamily(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(model, "deepseek"):
		return thinkingFamilyDeepSeek
	case strings.HasPrefix(model, "kimi"), strings.HasPrefix(model, "moonshot"),
		strings.HasPrefix(model, "glm"):
		return thinkingFamilyThinking
	}
	return ""
}
