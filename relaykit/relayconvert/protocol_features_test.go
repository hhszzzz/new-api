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

	// A Responses upstream supports the field natively; lossy conversion must
	// not silently drop it there.
	reason, _ = AnalyzeConversionFeatures(ProtocolMessages, ProtocolResponses, features, true, true)
	assert.Equal(t, "context_management requires its native Messages upstream", reason)
}
