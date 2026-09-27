package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var templateSlot = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

func loadLocale(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := localeFS.ReadFile("locales/" + lang + ".yaml")
	require.NoError(t, err)
	messages := make(map[string]string)
	require.NoError(t, yaml.Unmarshal(raw, &messages))
	return messages
}

func templateSlots(message string) []string {
	var slots []string
	for _, match := range templateSlot.FindAllStringSubmatch(message, -1) {
		slots = append(slots, match[1])
	}
	sort.Strings(slots)
	return slots
}

func TestLocalesDefineEveryKeyWithTheSameSlots(t *testing.T) {
	require.NoError(t, Init())
	english := loadLocale(t, LangEn)

	file, err := parser.ParseFile(token.NewFileSet(), "keys.go", nil, 0)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			for _, value := range spec.(*ast.ValueSpec).Values {
				literal, ok := value.(*ast.BasicLit)
				require.True(t, ok)
				key, err := strconv.Unquote(literal.Value)
				require.NoError(t, err)
				assert.Contains(t, english, key, "keys.go declares a key without an English message")
			}
		}
	}

	for _, lang := range []string{LangZhCN, LangZhTW} {
		messages := loadLocale(t, lang)
		for key, message := range english {
			translated, ok := messages[key]
			if !assert.True(t, ok, "%s is missing %s", lang, key) {
				continue
			}
			assert.Equal(t, templateSlots(message), templateSlots(translated), "%s %s uses different slots", lang, key)
		}
		for key := range messages {
			assert.Contains(t, english, key, "%s defines %s, which has no English message", lang, key)
		}
	}
}

func TestTranslateRendersNestedKeysInTheMessageLanguage(t *testing.T) {
	params := map[string]any{"Name": Key(MsgUserLimitNameConcurrency), "Max": 10}

	assert.Equal(t, "“并发”限制必须在 1 到 10 之间", Translate(LangZhCN, MsgUserRateLimitRange, params))
	assert.Equal(t, "Concurrency limit must be between 1 and 10", Translate(LangEn, MsgUserRateLimitRange, params))
	assert.Equal(t, Key(MsgUserLimitNameConcurrency), params["Name"], "the caller's params must stay reusable")
}
