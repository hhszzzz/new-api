package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripLeadingBillingHeader(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "LF separator",
			in:   "x-anthropic-billing-header: cc_version=2.1; cch=rotating\n\nStable prompt",
			want: "Stable prompt",
		},
		{
			name: "CRLF separator",
			in:   "x-anthropic-billing-header: cc_version=2.1; cch=rotating\r\n\r\nStable prompt",
			want: "Stable prompt",
		},
		{
			name: "header only",
			in:   "x-anthropic-billing-header: cc_version=2.1; cch=rotating",
			want: "",
		},
		{
			name: "later literal is preserved",
			in:   "Keep this literal:\nx-anthropic-billing-header: example",
			want: "Keep this literal:\nx-anthropic-billing-header: example",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, StripLeadingBillingHeader(test.in))
		})
	}
}
