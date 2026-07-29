package bridge

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

func TestOpenAIReasoningTextKeepsDistinctSummaryAndVisibleContent(t *testing.T) {
	text := OpenAIReasoningText(dto.ResponsesOutput{
		Type:    "reasoning",
		Summary: []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "summary"}},
		Content: []dto.ResponsesOutputContent{{Type: "reasoning_text", Text: "details"}},
	})
	assert.Equal(t, "summary\n\ndetails", text)
}
