package relayconvert

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnalyzeConversionFeaturesMessagesContextManagement(t *testing.T) {
	features := RequestFeatureSet{HasContextManagement: true}

	// The "safe" policy keeps best-effort directives required: only lossy
	// conversion may drop context_management.
	reason, losses := AnalyzeConversionFeatures(ProtocolMessages, ProtocolChat, features, true, false)
	assert.Equal(t, "context_management requires its native Messages upstream", reason)
	assert.Empty(t, losses)

	reason, losses = AnalyzeConversionFeatures(ProtocolMessages, ProtocolChat, features, true, true)
	assert.Empty(t, reason)
	assert.Equal(t, []string{"context_management"}, losses)

	// Messages edits are evaluated by the gateway, including Responses routes.
	for _, target := range []Protocol{ProtocolResponses, ProtocolGemini} {
		reason, losses = AnalyzeConversionFeatures(ProtocolMessages, target, features, true, true)
		assert.Empty(t, reason)
		assert.Equal(t, []string{"context_management"}, losses)
	}
}
