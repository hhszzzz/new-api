package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
)

func TestSensitiveWordContainsDoesNotMatchBlankConfiguration(t *testing.T) {
	original := setting.SensitiveWordsToString()
	t.Cleanup(func() { setting.SensitiveWordsFromString(original) })
	setting.SensitiveWordsFromString(" \n\t\n")

	contains, words := SensitiveWordContains("ordinary request")

	assert.False(t, contains)
	assert.Empty(t, words)
}

func TestSensitiveWordContainsUsesLatestConfiguration(t *testing.T) {
	original := setting.SensitiveWordsToString()
	t.Cleanup(func() { setting.SensitiveWordsFromString(original) })
	setting.SensitiveWordsFromString("blocked")

	contains, words := SensitiveWordContains("This is BLOCKED content")
	assert.True(t, contains)
	assert.Equal(t, []string{"blocked"}, words)

	setting.SensitiveWordsFromString("replacement")
	contains, words = SensitiveWordContains("This is blocked content")
	assert.False(t, contains)
	assert.Empty(t, words)
}
