package reasonmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaudeContextWindowStopReasonMapsToLength(t *testing.T) {
	assert.Equal(t, "length", ClaudeStopReasonToOpenAIFinishReason("model_context_window_exceeded"))
}

func TestOpenAILegacyFunctionCallFinishReasonMapsToToolUse(t *testing.T) {
	assert.Equal(t, "tool_use", OpenAIFinishReasonToClaudeStopReason("function_call"))
}
