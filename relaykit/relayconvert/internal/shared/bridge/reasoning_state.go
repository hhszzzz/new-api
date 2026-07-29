package bridge

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

func OpenAIReasoningText(item dto.ResponsesOutput) string {
	if strings.TrimSpace(item.Type) != "reasoning" {
		return ""
	}
	var summary strings.Builder
	for _, part := range item.Summary {
		if (part.Type == "summary_text" || part.Type == "reasoning_text") && part.Text != "" {
			summary.WriteString(part.Text)
		}
	}
	var visible strings.Builder
	for _, part := range item.Content {
		if (part.Type == "summary_text" || part.Type == "reasoning_text") && part.Text != "" {
			visible.WriteString(part.Text)
		}
	}
	if visible.Len() == 0 || visible.String() == summary.String() {
		return summary.String()
	}
	if summary.Len() == 0 {
		return visible.String()
	}
	return summary.String() + "\n\n" + visible.String()
}
