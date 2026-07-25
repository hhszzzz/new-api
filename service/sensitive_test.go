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

func TestSensitiveWordContainsUsesBoundariesForLatinKeywords(t *testing.T) {
	original := setting.SensitiveWordsToString()
	t.Cleanup(func() { setting.SensitiveWordsFromString(original) })
	setting.SensitiveWordsFromString("hi\n你好\nhello")

	tests := []struct {
		name     string
		text     string
		contains bool
		word     string
	}{
		{name: "latin keyword", text: "say hi please", contains: true, word: "hi"},
		{name: "latin keyword case insensitive", text: "HELLO!", contains: true, word: "hello"},
		{name: "chinese keyword in sentence", text: "你好世界", contains: true, word: "你好"},
		{name: "latin keyword next to chinese", text: "世界hi", contains: true, word: "hi"},
		{name: "inside this", text: "this request should pass"},
		{name: "inside thinking", text: "enable thinking"},
		{name: "inside which", text: "which option"},
		{name: "inside longer identifier", text: "hi_there"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contains, words := SensitiveWordContains(test.text)

			assert.Equal(t, test.contains, contains)
			if test.contains {
				assert.Equal(t, []string{test.word}, words)
			} else {
				assert.Empty(t, words)
			}
		})
	}
}

func TestSensitiveWordReplaceUsesTheSameBoundaries(t *testing.T) {
	original := setting.SensitiveWordsToString()
	t.Cleanup(func() { setting.SensitiveWordsFromString(original) })
	setting.SensitiveWordsFromString("hi\n你好")

	replaced, words, text := SensitiveWordReplace("this says hi and 你好", false)

	assert.True(t, replaced)
	assert.Equal(t, []string{"hi", "你好"}, words)
	assert.Equal(t, "this says **###** and **###**", text)
}
