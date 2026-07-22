package common

import (
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoHasModelRoutingIncludesAdaptorRewrites(t *testing.T) {
	tests := []struct {
		name     string
		info     *RelayInfo
		expected bool
	}{
		{name: "nil relay info", info: nil, expected: false},
		{
			name: "explicit model mapping",
			info: &RelayInfo{
				OriginModelName: "public-model",
				ChannelMeta: &ChannelMeta{
					IsModelMapped:     true,
					UpstreamModelName: "private-model",
				},
			},
			expected: true,
		},
		{
			name: "adaptor rewrites upstream without mapping flag",
			info: &RelayInfo{
				OriginModelName: "public-model",
				ChannelMeta: &ChannelMeta{
					UpstreamModelName: "provider-internal-model",
				},
			},
			expected: true,
		},
		{
			name: "unchanged upstream model",
			info: &RelayInfo{
				OriginModelName: "public-model",
				ChannelMeta: &ChannelMeta{
					UpstreamModelName: "public-model",
				},
			},
			expected: false,
		},
		{
			name: "compact suffix is a public transport variant",
			info: &RelayInfo{
				OriginModelName: ratio_setting.WithCompactModelSuffix("public-model"),
				RelayMode:       relayconstant.RelayModeResponsesCompact,
				ChannelMeta: &ChannelMeta{
					UpstreamModelName: "public-model",
				},
			},
			expected: false,
		},
		{
			name: "compact adaptor rewrite remains routing",
			info: &RelayInfo{
				OriginModelName: ratio_setting.WithCompactModelSuffix("public-model"),
				RelayMode:       relayconstant.RelayModeResponsesCompact,
				ChannelMeta: &ChannelMeta{
					UpstreamModelName: "provider-internal-model",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.info.HasModelRouting())
		})
	}
}
