package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/stretchr/testify/assert"
)

// Callers that send generationConfig.thinkingConfig themselves skip the thinking
// adapter entirely, so the consume log can only show their effort if the native
// path records it. Gemini spells effort two ways and the log renders one shared
// badge, so both spellings must land on the same vocabulary.
func TestRecordReasoningEffortReadsBothGeminiSpellings(t *testing.T) {
	tests := []struct {
		name     string
		config   *dto.GeminiThinkingConfig
		expected string
	}{
		{
			name:     "no thinking config is not an effort choice",
			expected: "",
		},
		{
			name:     "native level is reported as sent",
			config:   &dto.GeminiThinkingConfig{ThinkingLevel: "high"},
			expected: reasoning.EffortHigh,
		},
		{
			name:     "native level is normalized",
			config:   &dto.GeminiThinkingConfig{ThinkingLevel: "  HIGH  "},
			expected: reasoning.EffortHigh,
		},
		{
			name: "native level wins over a budget",
			config: &dto.GeminiThinkingConfig{
				ThinkingLevel:  "low",
				ThinkingBudget: common.GetPointer(32768),
			},
			expected: reasoning.EffortLow,
		},
		{
			name:     "budget is described as a level",
			config:   &dto.GeminiThinkingConfig{ThinkingBudget: common.GetPointer(4096)},
			expected: reasoning.EffortMedium,
		},
		{
			name:     "a zero budget disables thinking and is not recorded",
			config:   &dto.GeminiThinkingConfig{ThinkingBudget: common.GetPointer(0)},
			expected: "",
		},
		{
			name:     "includeThoughts alone is not an effort choice",
			config:   &dto.GeminiThinkingConfig{IncludeThoughts: true},
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := &dto.GeminiChatRequest{}
			request.GenerationConfig.ThinkingConfig = test.config
			info := &relaycommon.RelayInfo{}

			RecordReasoningEffort(request, info)

			assert.Equal(t, test.expected, info.ReasoningEffort)
		})
	}
}

func TestRecordReasoningEffortToleratesMissingArguments(t *testing.T) {
	assert.NotPanics(t, func() {
		RecordReasoningEffort(nil, &relaycommon.RelayInfo{})
		RecordReasoningEffort(&dto.GeminiChatRequest{}, nil)
	})
}
