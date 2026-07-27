package setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateHeaderNavModulesAcceptsCustomIframeNavigation(t *testing.T) {
	raw := `{
		"home": true,
		"custom": [
			{"id":"docs-hub","title":"Docs Hub","url":"https://docs.example.com/app","enabled":true}
		],
		"order": ["home","custom:docs-hub","console"]
	}`

	require.NoError(t, ValidateHeaderNavModules(raw))
}

func TestValidateHeaderNavModulesRejectsUnsafeOrAmbiguousCustomNavigation(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "unsupported URL scheme",
			raw:  `{"custom":[{"id":"bad-url","title":"Bad","url":"javascript:alert(1)","enabled":true}]}`,
		},
		{
			name: "URL credentials exposed through public status",
			raw:  `{"custom":[{"id":"secret","title":"Secret","url":"https://user:pass@example.com","enabled":true}]}`,
		},
		{
			name: "duplicate custom id",
			raw:  `{"custom":[{"id":"same","title":"One","url":"https://one.example.com","enabled":true},{"id":"same","title":"Two","url":"https://two.example.com","enabled":true}]}`,
		},
		{
			name: "unknown order item",
			raw:  `{"custom":[],"order":["home","custom:missing"]}`,
		},
		{
			name: "duplicate order item",
			raw:  `{"custom":[],"order":["home","home"]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Error(t, ValidateHeaderNavModules(test.raw))
		})
	}
}
