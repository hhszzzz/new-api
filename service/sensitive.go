package service

import (
	"errors"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting"
)

func CheckSensitiveMessages(messages []dto.Message) ([]string, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	for _, message := range messages {
		if !strings.EqualFold(message.Role, "user") {
			continue
		}
		arrayContent := message.ParseContent()
		for _, m := range arrayContent {
			if m.Type == "image_url" {
				// TODO: check image url
				continue
			}
			// 检查 text 是否为空
			if m.Text == "" {
				continue
			}
			if ok, words := SensitiveWordContains(m.Text); ok {
				return words, errors.New("sensitive words detected")
			}
		}
	}
	return nil, nil
}

func CheckSensitiveText(text string) (bool, []string) {
	return SensitiveWordContains(text)
}

// SensitiveWordContains 是否包含敏感词，返回是否包含敏感词和敏感词列表
func SensitiveWordContains(text string) (bool, []string) {
	words := setting.SensitiveWordsSnapshot()
	hits := sensitiveWordMatches(text, words, true)
	if len(hits) == 0 {
		return false, nil
	}
	return true, []string{string(hits[0].word)}
}

type sensitiveWordMatch struct {
	pos  int
	word []rune
}

func sensitiveWordMatches(text string, words []string, returnImmediately bool) []sensitiveWordMatch {
	if len(text) == 0 || len(words) == 0 {
		return nil
	}
	m := getOrBuildAC(words)
	if m == nil {
		return nil
	}
	textRunes := []rune(strings.ToLower(text))
	matches := make([]sensitiveWordMatch, 0)
	for _, hit := range m.MultiPatternSearch(textRunes, false) {
		word := string(hit.Word)
		if sensitiveWordHasLatinOrDigit(word) && !sensitiveWordAtBoundary(textRunes, hit.Pos, len(hit.Word)) {
			continue
		}
		matches = append(matches, sensitiveWordMatch{pos: hit.Pos, word: hit.Word})
		if returnImmediately {
			break
		}
	}
	return matches
}

func sensitiveWordHasLatinOrDigit(word string) bool {
	for _, r := range word {
		if unicode.In(r, unicode.Latin) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func sensitiveWordAtBoundary(text []rune, start int, length int) bool {
	if start > 0 && isLatinIdentifierRune(text[start-1]) {
		return false
	}
	end := start + length
	return end >= len(text) || !isLatinIdentifierRune(text[end])
}

func isLatinIdentifierRune(r rune) bool {
	return unicode.In(r, unicode.Latin) || unicode.IsDigit(r) || r == '_'
}

// SensitiveWordReplace 敏感词替换，返回是否包含敏感词和替换后的文本
func SensitiveWordReplace(text string, returnImmediately bool) (bool, []string, string) {
	words := setting.SensitiveWordsSnapshot()
	hits := sensitiveWordMatches(text, words, returnImmediately)
	if len(hits) == 0 {
		return false, nil, text
	}
	matchedWords := make([]string, 0, len(hits))
	var builder strings.Builder
	textRunes := []rune(text)
	lastPos := 0
	for _, hit := range hits {
		pos := hit.pos
		end := pos + len(hit.word)
		if pos < lastPos || end > len(textRunes) {
			continue
		}
		builder.WriteString(string(textRunes[lastPos:pos]))
		builder.WriteString("**###**")
		lastPos = end
		matchedWords = append(matchedWords, string(hit.word))
	}
	if len(matchedWords) == 0 {
		return false, nil, text
	}
	builder.WriteString(string(textRunes[lastPos:]))
	return true, matchedWords, builder.String()
}
