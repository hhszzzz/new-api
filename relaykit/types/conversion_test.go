package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFillDiagnosticMessageFillsEachSlotOnce(t *testing.T) {
	template := `model "{{model}}" uses budget {{budget}}`
	params := map[string]string{"model": "{{budget}}", "budget": "512"}

	message, key, values := FillDiagnosticMessage(template, params)

	assert.Equal(t, `model "{{budget}}" uses budget 512`, message)
	assert.Equal(t, template, key)
	assert.Equal(t, params, values)
}

func TestFillDiagnosticMessageWithoutValuesIsItsOwnKey(t *testing.T) {
	message, key, values := FillDiagnosticMessage("target protocol does not carry this display metadata")

	assert.Equal(t, "target protocol does not carry this display metadata", message)
	assert.Empty(t, key)
	assert.Nil(t, values)
}
