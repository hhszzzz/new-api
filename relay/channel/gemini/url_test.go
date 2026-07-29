package gemini

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiRequestURLNormalizesCommonCCSwitchBaseForms(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		model   string
		stream  bool
		want    string
	}{
		{
			name:    "origin",
			baseURL: "https://generativelanguage.googleapis.com",
			model:   "gemini-2.5-pro",
			want:    "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:generateContent",
		},
		{
			name:    "v1beta resource root",
			baseURL: "https://generativelanguage.googleapis.com/v1beta",
			model:   "gemini-2.5-pro",
			want:    "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:generateContent",
		},
		{
			name:    "models resource root and resource model id",
			baseURL: "https://generativelanguage.googleapis.com/v1beta/models",
			model:   "models/gemini-2.5-pro",
			want:    "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:generateContent",
		},
		{
			name:    "gateway prefix",
			baseURL: "https://relay.example/gemini/v1beta",
			model:   "gemini-2.5-pro",
			want:    "https://relay.example/gemini/v1beta/models/gemini-2.5-pro:generateContent",
		},
		{
			name:    "complete streaming endpoint",
			baseURL: "https://relay.example/custom/models/gemini-fixed:streamGenerateContent?gateway=1",
			model:   "gemini-2.5-flash",
			stream:  true,
			want:    "https://relay.example/custom/models/gemini-fixed:streamGenerateContent?gateway=1&alt=sse",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				IsStream: test.stream,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelBaseUrl:    test.baseURL,
					UpstreamModelName: test.model,
				},
			}

			got, err := (&Adaptor{}).GetRequestURL(info)

			require.NoError(t, err)
			assert.Equal(t, test.want, got)
			assert.NotContains(t, got, "/models/models/")
		})
	}
}
