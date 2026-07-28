package common

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

// RecordClaudeReasoningEffort publishes the effort level of an outbound
// Anthropic request onto the relay info so the consume log can show it.
//
// It must be called once the thinking config is final, because the effort that
// matters is what actually goes upstream: a request can arrive without any
// reasoning hint and still be given thinking by a model suffix, the thinking
// adapter, or an OpenAI-style reasoning_effort translated during conversion.
//
// A request without any reasoning configuration is left alone rather than
// recorded as "none", so the log only gains a badge when reasoning really
// happened. output_config.effort is read even when thinking is absent, because
// callers may send the effort field on its own.
func RecordClaudeReasoningEffort(info *RelayInfo, request *dto.ClaudeRequest) {
	if info == nil || request == nil {
		return
	}
	thinkingEnabled := false
	budgetTokens := 0
	if request.Thinking != nil {
		thinkingEnabled = request.Thinking.Type == "enabled"
		budgetTokens = request.Thinking.GetBudgetTokens()
	}
	effort := reasoning.ClaudeEffort(request.GetEfforts(), thinkingEnabled, budgetTokens)
	if effort != "" && effort != reasoning.EffortNone {
		info.ReasoningEffort = effort
	}
}
