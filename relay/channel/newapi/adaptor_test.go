package newapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSetupRequestHeaderUsesSelectedUpstreamProtocol(t *testing.T) {
	tests := []struct {
		name             string
		upstreamProtocol channelcompat.Protocol
		assertHeader     func(*testing.T, http.Header)
	}{
		{
			name:             "Messages",
			upstreamProtocol: channelcompat.ProtocolMessages,
			assertHeader: func(t *testing.T, header http.Header) {
				assert.Equal(t, "sk-test", header.Get("x-api-key"))
				assert.Equal(t, "2023-06-01", header.Get("anthropic-version"))
				assert.Empty(t, header.Get("anthropic-beta"))
			},
		},
		{
			name:             "Gemini",
			upstreamProtocol: channelcompat.ProtocolGemini,
			assertHeader: func(t *testing.T, header http.Header) {
				assert.Equal(t, "sk-test", header.Get("x-goog-api-key"))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
				RequestProtocol:  channelcompat.ProtocolResponses,
				UpstreamProtocol: test.upstreamProtocol,
				Status:           channelcompat.StatusConvertible,
			})
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatOpenAIResponses,
				ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "sk-test"},
			}
			header := http.Header{}

			err := (&Adaptor{}).SetupRequestHeader(c, &header, info)

			assert.NoError(t, err)
			assert.Equal(t, "Bearer sk-test", header.Get("Authorization"))
			test.assertHeader(t, header)
		})
	}
}

func TestSetupRequestHeaderPassesAnthropicBetaOnlyForNativeMessagesEntry(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("anthropic-beta", "tools-2026-01-01")
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolMessages,
		Status:           channelcompat.StatusNative,
	})
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "sk-test"},
	}
	header := http.Header{}

	err := (&Adaptor{}).SetupRequestHeader(c, &header, info)

	assert.NoError(t, err)
	assert.Equal(t, "tools-2026-01-01", header.Get("anthropic-beta"))
}
