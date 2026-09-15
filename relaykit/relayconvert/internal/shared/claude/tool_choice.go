package claude

import (
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ResolveThinkingToolChoice runs after both reasoning and tools are rendered.
// Forced calls cannot be combined with Anthropic thinking; models which require
// thinking must reject the route rather than silently changing tool execution.
func ResolveThinkingToolChoice(request *dto.ClaudeRequest) error {
	if request == nil || request.ToolChoice == nil {
		return nil
	}
	choice, err := kitutil.Any2Type[dto.ClaudeToolChoice](request.ToolChoice)
	if err != nil {
		return fmt.Errorf("invalid Claude tool_choice: %w", err)
	}
	if choice.Type != "any" && choice.Type != "tool" {
		return nil
	}
	if request.Thinking != nil && request.Thinking.Type == "disabled" {
		return nil
	}
	if request.Thinking == nil && !AdaptiveThinkingIsDefault(request.Model) {
		return nil
	}
	if ThinkingCannotBeDisabled(request.Model) {
		return fmt.Errorf("model %q cannot honor a forced tool_choice because thinking cannot be disabled", request.Model)
	}
	request.Thinking = &dto.Thinking{Type: "disabled"}
	return nil
}
