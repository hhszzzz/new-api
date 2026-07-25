package reasoning

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Anthropic and Gemini never send an OpenAI-style reasoning_effort field, so the
// consume log can only show a reasoning badge for them if a token budget is
// described as an effort level. These boundaries are the contract the log UI
// renders, so they are pinned here rather than left to the call sites.
func TestEffortFromBudgetTokensBucketsBudgetsByMagnitude(t *testing.T) {
	tests := []struct {
		name     string
		budget   int
		expected string
	}{
		{name: "thinking disabled", budget: 0, expected: EffortNone},
		{name: "negative budget is disabled", budget: -1, expected: EffortNone},
		{name: "anthropic minimum budget", budget: 1024, expected: EffortLow},
		{name: "at low boundary", budget: 2048, expected: EffortLow},
		{name: "just above low boundary", budget: 2049, expected: EffortMedium},
		{name: "at medium boundary", budget: 8192, expected: EffortMedium},
		{name: "just above medium boundary", budget: 8193, expected: EffortHigh},
		{name: "at high boundary", budget: 24576, expected: EffortHigh},
		{name: "gemini maximum budget", budget: 32768, expected: EffortXHigh},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, EffortFromBudgetTokens(test.budget))
		})
	}
}

// A native effort field must win over the budget heuristic, because Anthropic
// adaptive thinking carries both: output_config.effort is the level the caller
// actually selected, while the budget is only a request-shaping detail.
func TestClaudeEffortPrefersTheNativeEffortField(t *testing.T) {
	tests := []struct {
		name            string
		nativeEffort    string
		thinkingEnabled bool
		budgetTokens    int
		expected        string
	}{
		{
			name:         "native effort wins over budget",
			nativeEffort: "high",
			budgetTokens: 1024,
			expected:     EffortHigh,
		},
		{
			name:         "native effort is normalized",
			nativeEffort: "  HIGH  ",
			expected:     EffortHigh,
		},
		{
			name:         "unknown native effort still reaches the log",
			nativeEffort: "ultra",
			expected:     "ultra",
		},
		{
			name:            "enabled thinking falls back to the budget",
			thinkingEnabled: true,
			budgetTokens:    4096,
			expected:        EffortMedium,
		},
		{
			name:            "enabled thinking without a budget is disabled",
			thinkingEnabled: true,
			expected:        EffortNone,
		},
		{
			name:         "a budget alone is not an effort choice",
			budgetTokens: 4096,
			expected:     "",
		},
		{
			name:     "no reasoning configuration at all",
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(
				t,
				test.expected,
				ClaudeEffort(test.nativeEffort, test.thinkingEnabled, test.budgetTokens),
			)
		})
	}
}
