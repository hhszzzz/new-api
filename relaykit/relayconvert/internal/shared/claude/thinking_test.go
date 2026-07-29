package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsesAdaptiveThinkingNormalizesProviderModelNames(t *testing.T) {
	for _, model := range []string{
		"claude-sonnet-4-6",
		"anthropic/claude-opus-4.8",
		"anthropic.claude_opus_4_7_20260701-v1:0",
		"CLAUDE-SONNET-5",
		"claude-fable-5",
		"claude-mythos-preview",
	} {
		assert.True(t, UsesAdaptiveThinking(model), "model=%s", model)
	}
	assert.False(t, UsesAdaptiveThinking("claude-sonnet-4-5"))
}

func TestAdaptiveThinkingModelPolicies(t *testing.T) {
	assert.True(t, AdaptiveThinkingIsDefault("claude-sonnet.5"))
	assert.True(t, AdaptiveThinkingIsDefault("anthropic_claude-mythos-preview"))
	assert.False(t, AdaptiveThinkingIsDefault("claude-opus-4-8"))
	assert.True(t, ThinkingCannotBeDisabled("anthropic.claude_fable_5-v1:0"))
	assert.True(t, ThinkingCannotBeDisabled("claude-mythos-5"))
	assert.False(t, ThinkingCannotBeDisabled("claude-sonnet-5"))
}

func TestAdaptiveEffort(t *testing.T) {
	tests := map[string]string{
		"minimal": "low",
		"low":     "low",
		"medium":  "medium",
		"high":    "high",
		"xhigh":   "max",
		"max":     "max",
	}
	for input, expected := range tests {
		actual, ok := AdaptiveEffort(input)
		assert.True(t, ok, "effort=%s", input)
		assert.Equal(t, expected, actual, "effort=%s", input)
	}
	_, ok := AdaptiveEffort("unknown")
	assert.False(t, ok)
	assert.True(t, ReasoningExplicitlyDisabled("OFF"))
	assert.False(t, ReasoningExplicitlyDisabled("minimal"))
}
