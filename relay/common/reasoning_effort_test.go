package common

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// The consume log shows a reasoning badge only when a request really asked for
// reasoning, and Anthropic spells that request two different ways: adaptive
// thinking carries output_config.effort, while enabled thinking carries only a
// token budget. Both spellings must reach the log, and a request with a native
// effort field but no thinking block must not be skipped: callers may send
// output_config on its own.
func TestRecordClaudeReasoningEffortReadsBothAnthropicSpellings(t *testing.T) {
	tests := []struct {
		name     string
		request  *dto.ClaudeRequest
		expected string
	}{
		{
			name: "native effort without a thinking block",
			request: &dto.ClaudeRequest{
				OutputConfig: []byte(`{"effort":"high"}`),
			},
			expected: "high",
		},
		{
			name: "native effort alongside adaptive thinking",
			request: &dto.ClaudeRequest{
				Thinking:     &dto.Thinking{Type: "adaptive"},
				OutputConfig: []byte(`{"effort":"low"}`),
			},
			expected: "low",
		},
		{
			name: "enabled thinking reports its budget as a level",
			request: &dto.ClaudeRequest{
				Thinking: &dto.Thinking{
					Type:         "enabled",
					BudgetTokens: common.GetPointer[int](4096),
				},
			},
			expected: "medium",
		},
		{
			name:     "no reasoning configuration leaves the log untouched",
			request:  &dto.ClaudeRequest{},
			expected: "",
		},
		{
			name: "adaptive thinking without an effort field",
			request: &dto.ClaudeRequest{
				Thinking: &dto.Thinking{Type: "adaptive"},
			},
			expected: "",
		},
		{
			name: "enabled thinking without a budget is not reasoning",
			request: &dto.ClaudeRequest{
				Thinking: &dto.Thinking{Type: "enabled"},
			},
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &RelayInfo{}
			RecordClaudeReasoningEffort(info, test.request)
			assert.Equal(t, test.expected, info.ReasoningEffort)
		})
	}
}

// Recording must never panic on a partially built relay: the Claude handler and
// the OpenAI->Claude adaptor both call this before the channel meta exists.
func TestRecordClaudeReasoningEffortToleratesMissingArguments(t *testing.T) {
	assert.NotPanics(t, func() {
		RecordClaudeReasoningEffort(nil, &dto.ClaudeRequest{})
		RecordClaudeReasoningEffort(&RelayInfo{}, nil)
	})
}

func TestInitChannelMetaClearsReasoningEffortForRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &RelayInfo{ReasoningEffort: "high"}

	info.InitChannelMeta(ctx)

	assert.Empty(t, info.ReasoningEffort)
}
