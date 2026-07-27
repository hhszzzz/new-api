package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromJsonStringPreservesExistingDataOnInvalidJSON(t *testing.T) {
	values := NewRWMap[string, float64]()
	values.Set("existing", 1.5)
	callbackCalled := false

	err := LoadFromJsonStringWithCallback(values, `{"replacement":`, func() {
		callbackCalled = true
	})

	require.Error(t, err)
	assert.Equal(t, map[string]float64{"existing": 1.5}, values.ReadAll())
	assert.False(t, callbackCalled)
}
