package chat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

func TestApplyReasoningEffort(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		effort       string
		wantEffort   string
		wantThinking string
	}{
		{name: "openai style passthrough", model: "gpt-5.4", effort: "xhigh", wantEffort: "xhigh"},
		{name: "grok passthrough", model: "grok-4.5-fast", effort: "low", wantEffort: "low"},
		{name: "deepseek clamps low tiers to high", model: "deepseek-v4", effort: "medium", wantEffort: "high", wantThinking: `{"type":"enabled"}`},
		{name: "deepseek keeps max tier", model: "deepseek-v4", effort: "xhigh", wantEffort: "max", wantThinking: `{"type":"enabled"}`},
		{name: "deepseek explicit disable", model: "deepseek-v4", effort: "none", wantThinking: `{"type":"disabled"}`},
		{name: "kimi thinking only", model: "kimi-k2.5", effort: "high", wantThinking: `{"type":"enabled"}`},
		{name: "moonshot thinking only", model: "moonshot-v1-32k", effort: "medium", wantThinking: `{"type":"enabled"}`},
		{name: "glm explicit disable", model: "glm-5.2", effort: "off", wantThinking: `{"type":"disabled"}`},
		{name: "unknown model drops effort", model: "openpangu-2.0-flash", effort: "high"},
		{name: "empty effort is a no-op", model: "glm-5.2", effort: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := &dto.GeneralOpenAIRequest{Model: test.model}
			ApplyReasoningEffort(out, test.effort)

			assert.Equal(t, test.wantEffort, out.ReasoningEffort)
			assert.Equal(t, test.wantThinking, string(out.THINKING))
		})
	}
}
