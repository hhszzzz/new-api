package channelcompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompatibilityMatrixProtectsImplementedProtocolBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		channel   *model.Channel
		protocol  Protocol
		modelName string
		status    Status
		upstream  Protocol
		converter string
	}{
		{
			name:      "messages converts through an OpenAI chat channel",
			channel:   &model.Channel{Type: constant.ChannelTypeOpenAI},
			protocol:  ProtocolMessages,
			modelName: "gpt-5.5",
			status:    StatusConvertible,
			upstream:  ProtocolChat,
			converter: relayconvert.ConverterClaudeMessagesToOpenAIChat,
		},
		{
			name:      "responses cannot use an Anthropic channel without an adaptor",
			channel:   &model.Channel{Type: constant.ChannelTypeAnthropic},
			protocol:  ProtocolResponses,
			modelName: "claude-sonnet-4",
			status:    StatusIncompatible,
		},
		{
			name:      "messages convert to Gemini",
			channel:   &model.Channel{Type: constant.ChannelTypeGemini},
			protocol:  ProtocolMessages,
			modelName: "gemini-2.5-pro",
			status:    StatusConvertible,
			upstream:  ProtocolGemini,
			converter: "anthropic_messages_to_gemini_generate_content",
		},
		{
			name:      "Codex only accepts Responses",
			channel:   &model.Channel{Type: constant.ChannelTypeCodex},
			protocol:  ProtocolChat,
			modelName: "gpt-5.5-codex",
			status:    StatusIncompatible,
		},
		{
			name:      "Vertex Claude accepts native Messages",
			channel:   &model.Channel{Type: constant.ChannelTypeVertexAi},
			protocol:  ProtocolMessages,
			modelName: "claude-sonnet-4",
			status:    StatusNative,
			upstream:  ProtocolMessages,
		},
		{
			name:      "Vertex Gemini rejects Messages",
			channel:   &model.Channel{Type: constant.ChannelTypeVertexAi},
			protocol:  ProtocolMessages,
			modelName: "gemini-2.5-pro",
			status:    StatusIncompatible,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compatibility := ForRequest(tt.channel, tt.protocol, tt.modelName, canonicalPath(tt.protocol, tt.modelName))
			assert.Equal(t, tt.status, compatibility.Status)
			assert.Equal(t, tt.upstream, compatibility.UpstreamProtocol)
			assert.Equal(t, tt.converter, compatibility.Converter)
		})
	}
}

func TestAdvancedCustomCompatibilityUsesMatchedRouteAndModel(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/v1/chat/completions",
				Converter:    relayconvert.ConverterClaudeMessagesToOpenAIChat,
				Models:       []string{"demo-gpt-5.5"},
			},
		}},
	}
	encoded, err := common.Marshal(settings)
	require.NoError(t, err)
	encodedString := string(encoded)
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, OtherSettings: encodedString}

	compatible := ForRequest(channel, ProtocolMessages, "demo-gpt-5.5", "/v1/messages")
	assert.Equal(t, StatusConvertible, compatible.Status)
	assert.Equal(t, ProtocolChat, compatible.UpstreamProtocol)

	incompatible := ForRequest(channel, ProtocolMessages, "different-model", "/v1/messages")
	assert.Equal(t, StatusIncompatible, incompatible.Status)
}

func TestDetectRequestProtocolLeavesNonTextEndpointsUnrestricted(t *testing.T) {
	assert.Equal(t, ProtocolMessages, DetectRequestProtocol("/v1/messages?beta=true"))
	assert.Equal(t, ProtocolResponses, DetectRequestProtocol("/v1/responses/compact"))
	assert.Equal(t, ProtocolGemini, DetectRequestProtocol("/v1beta/models/gemini-2.5:streamGenerateContent"))
	assert.Empty(t, DetectRequestProtocol("/v1/images/generations"))
}
