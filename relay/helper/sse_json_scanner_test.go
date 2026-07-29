package helper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanJSONSSESupportsMultilineAndMissingBlankSeparators(t *testing.T) {
	body := "\uFEFF: keep-alive\n" + strings.Join([]string{
		"event: first",
		"data: {",
		`data: "type":"first",`,
		`data: "value":1`,
		"data: }",
		"",
		`data: {"type":"second"}`,
		`data: {"type":"third"}`,
		"data: [DONE]",
		`data: {"type":"ignored"}`,
	}, "\n")

	var got []string
	err := ScanJSONSSE(strings.NewReader(body), func(data string) (bool, error) {
		got = append(got, data)
		return data == "[DONE]", nil
	})

	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.JSONEq(t, `{"type":"first","value":1}`, got[0])
	assert.JSONEq(t, `{"type":"second"}`, got[1])
	assert.JSONEq(t, `{"type":"third"}`, got[2])
	assert.Equal(t, "[DONE]", got[3])
}
