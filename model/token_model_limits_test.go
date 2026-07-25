package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenModelLimitsMapIncludesExactAndMatchingNames(t *testing.T) {
	token := &Token{ModelLimits: " gpt-4-gizmo-customer-a , plain-model, ,plain-model "}

	limits := token.GetModelLimitsMap()

	assert.True(t, limits["gpt-4-gizmo-customer-a"])
	assert.True(t, limits["gpt-4-gizmo-*"])
	assert.True(t, limits["plain-model"])
	assert.NotContains(t, limits, "")
}
