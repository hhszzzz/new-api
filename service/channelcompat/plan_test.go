package channelcompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractRequestFeatureSetResponses(t *testing.T) {
	body, err := common.Marshal(map[string]any{
		"stream":               true,
		"previous_response_id": "resp_parent",
		"conversation":         map[string]any{"id": "conv_1"},
		"prompt":               map[string]any{"id": "pmpt_1"},
		"context_management":   map[string]any{"type": "compaction"},
		"reasoning":            map[string]any{"effort": "high"},
		"tools": []map[string]any{
			{"type": "function", "name": "lookup"},
			{"type": "custom", "name": "apply_patch"},
			{"type": "freeform", "name": "shell"},
			{
				"type":  "namespace",
				"name":  "crm",
				"tools": []map[string]any{{"type": "function", "name": "customer"}},
			},
			{"type": "tool_search", "execution": "client"},
			{"type": "web_search_preview"},
		},
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "inspect"},
					{"type": "input_image", "image_url": "https://example.test/image.png"},
					{"type": "input_text", "text": "again"},
				},
			},
			{
				"type":  "additional_tools",
				"tools": []map[string]any{{"type": "custom", "name": "render"}},
			},
		},
	})
	require.NoError(t, err)

	features, err := ExtractRequestFeatureSet(ProtocolResponses, body)

	require.NoError(t, err)
	assert.True(t, features.Stream)
	assert.True(t, features.HasPreviousResponse)
	assert.True(t, features.HasConversation)
	assert.True(t, features.HasPrompt)
	assert.True(t, features.HasContextManagement)
	assert.True(t, features.HasThinking)
	assert.True(t, features.HasCustomTools)
	assert.True(t, features.HasNamespaceTools)
	assert.True(t, features.HasToolSearch)
	assert.True(t, features.HasAdditionalTools)
	assert.Equal(t, []string{"input_text", "input_image"}, features.ContentTypes)
	assert.Equal(t, []string{"web_search_preview"}, features.HostedToolTypes)
}

func TestExtractRequestFeatureSetMessagesContentTypes(t *testing.T) {
	body := []byte(`{
		"system":[{"type":"text","text":"rules"}],
		"messages":[
			{"role":"assistant","content":[{"type":"thinking","thinking":"inspect"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"done"},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"abc"}}]},{"type":"server_tool_use","name":"web_search"}]}
		]
	}`)

	features, err := ExtractRequestFeatureSet(ProtocolMessages, body)

	require.NoError(t, err)
	assert.True(t, features.HasThinking)
	assert.Equal(t, []string{"text", "thinking", "tool_use", "tool_result", "document", "server_tool_use"}, features.ContentTypes)
	assert.Equal(t, []string{"server_tool_use"}, features.HostedToolTypes)
}

func TestExtractRequestFeatureSetDistinguishesClientAndServerToolSearch(t *testing.T) {
	clientBody := []byte(`{"tools":[{"type":"tool_search","execution":"client"}]}`)
	clientFeatures, err := ExtractRequestFeatureSet(ProtocolResponses, clientBody)
	require.NoError(t, err)
	assert.True(t, clientFeatures.HasToolSearch)
	assert.Empty(t, clientFeatures.HostedToolTypes)

	serverBody := []byte(`{"tools":[{"type":"tool_search","execution":"server"}]}`)
	serverFeatures, err := ExtractRequestFeatureSet(ProtocolResponses, serverBody)
	require.NoError(t, err)
	assert.True(t, serverFeatures.HasToolSearch)
	assert.Equal(t, []string{"tool_search"}, serverFeatures.HostedToolTypes)
}

func TestExtractRequestFeatureSetDetectsMessagesServerTools(t *testing.T) {
	body := []byte(`{"tools":[{"name":"lookup","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search"},{"type":"tool_search_tool_regex_20251119","name":"tool_search"}]}`)

	features, err := ExtractRequestFeatureSet(ProtocolMessages, body)

	require.NoError(t, err)
	assert.True(t, features.HasToolSearch)
	assert.Equal(t, []string{"web_search_20250305", "tool_search_tool_regex_20251119"}, features.HostedToolTypes)
}

func TestExtractRequestFeatureSetDetectsResponsesHostedToolHistory(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"web_search_call","id":"ws_1"},
			{"type":"file_search_call","id":"fs_1"},
			{"type":"computer_call","call_id":"computer_1"},
			{"type":"computer_call_output","call_id":"computer_1"},
			{"type":"code_interpreter_call","id":"ci_1"},
			{"type":"image_generation_call","id":"ig_1"},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"type":"custom_tool_call","call_id":"call_2","name":"shell","input":"pwd"},
			{"type":"tool_search_call","call_id":"call_3","execution":"client","arguments":{}}
		]
	}`)

	features, err := ExtractRequestFeatureSet(ProtocolResponses, body)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"web_search_call",
		"file_search_call",
		"computer_call",
		"computer_call_output",
		"code_interpreter_call",
		"image_generation_call",
	}, features.HostedToolTypes)
}

func TestPlanForRequestHonorsBridgePolicyCapabilitiesAndMappedModelOverrides(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	allow := true
	disallow := false
	mapping := `{"public-model":"provider-chat-model"}`
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, ModelMapping: &mapping}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityResponses},
		AllowConversion:   &disallow,
		ModelOverrides: []dto.ProtocolCapabilityModelOverride{
			{
				ModelPattern:      `^provider-chat-model$`,
				UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
				AllowConversion:   &allow,
			},
			{
				ModelPattern:      `provider-.*`,
				UpstreamProtocols: []string{dto.ProtocolCapabilityMessages},
			},
		},
	}})

	plan := PlanForRequest(channel, ProtocolResponses, "public-model", "/v1/responses", RequestFeatureSet{Stream: true})

	assert.Equal(t, StatusConvertible, plan.Status)
	assert.Equal(t, ProtocolChat, plan.UpstreamProtocol)
	assert.Equal(t, relayconvert.ConverterOpenAIResponsesToOpenAIChat, plan.RequestConverter)
	assert.Equal(t, "provider-chat-model", plan.EffectiveUpstreamModel)
	assert.Equal(t, StateModeReplay, plan.StateMode)
}

func TestPlanForRequestAdvancedCustomRouteOverridesDeclaredProtocols(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	allow := true
	mapping := `{"public-model":"provider-chat-model"}`
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, ModelMapping: &mapping}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    relayconvert.ConverterOpenAIResponsesToOpenAIChat,
				Models:       []string{"provider-chat-model"},
			},
		}},
		ProtocolCapabilities: &dto.ProtocolCapabilities{
			UpstreamProtocols: []string{dto.ProtocolCapabilityResponses},
			AllowConversion:   &allow,
		},
	})

	plan := PlanForRequest(channel, ProtocolResponses, "public-model", "/v1/responses", RequestFeatureSet{})

	assert.Equal(t, StatusConvertible, plan.Status)
	assert.Equal(t, ProtocolChat, plan.UpstreamProtocol)
	assert.Equal(t, relayconvert.ConverterOpenAIResponsesToOpenAIChat, plan.RequestConverter)
	assert.Equal(t, "provider-chat-model", plan.EffectiveUpstreamModel)
}

func TestPlanForRequestAdvancedCustomConversionUsesCapabilitySwitch(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	disallow := false
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/v1/chat/completions",
				Converter:    relayconvert.ConverterClaudeMessagesToOpenAIChat,
			},
		}},
		ProtocolCapabilities: &dto.ProtocolCapabilities{
			UpstreamProtocols: []string{dto.ProtocolCapabilityMessages},
			AllowConversion:   &disallow,
		},
	})

	plan := PlanForRequest(channel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{})

	assert.Equal(t, StatusIncompatible, plan.Status)
	assert.Contains(t, plan.Reason, "disabled")
}

func TestPlanForRequestAdvancedCustomCountTokensUsesMessagesRouteWhenAuxiliaryRouteIsAbsent(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	tests := []struct {
		name              string
		converter         string
		wantProtocol      Protocol
		wantRequestBridge string
	}{
		{
			name:              "Chat upstream",
			converter:         relayconvert.ConverterClaudeMessagesToOpenAIChat,
			wantProtocol:      ProtocolChat,
			wantRequestBridge: relayconvert.ConverterClaudeMessagesToOpenAIChat,
		},
		{
			name:              "Responses upstream",
			converter:         relayconvert.ConverterClaudeMessagesToOpenAIResponses,
			wantProtocol:      ProtocolResponses,
			wantRequestBridge: relayconvert.ConverterClaudeMessagesToOpenAIResponses,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
					{
						IncomingPath: "/v1/messages",
						UpstreamPath: "/provider/text",
						Converter:    test.converter,
					},
				}},
			})

			plan := PlanForRequest(channel, ProtocolMessages, "claude-public", "/v1/messages/count_tokens", RequestFeatureSet{})

			assert.Equal(t, StatusConvertible, plan.Status)
			assert.Equal(t, test.wantProtocol, plan.UpstreamProtocol)
			assert.Equal(t, test.wantRequestBridge, plan.RequestConverter)
		})
	}
}

func TestPlanForRequestAdvancedCustomCountTokensPrefersExplicitAuxiliaryRoute(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages/count_tokens",
				UpstreamPath: "/provider/messages/count_tokens",
				Converter:    relayconvert.ConverterNone,
			},
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/provider/chat",
				Converter:    relayconvert.ConverterClaudeMessagesToOpenAIChat,
			},
		}},
	})

	plan := PlanForRequest(channel, ProtocolMessages, "claude-public", "/v1/messages/count_tokens", RequestFeatureSet{})

	assert.Equal(t, StatusNative, plan.Status)
	assert.Equal(t, ProtocolMessages, plan.UpstreamProtocol)
	assert.Empty(t, plan.RequestConverter)
}

func TestPlanForRequestGlobalDisablePreservesLegacyCompatibility(t *testing.T) {
	withProtocolBridgePolicy(t, false, false)
	allow := true
	channel := &model.Channel{Type: constant.ChannelTypeAnthropic}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityMessages},
		AllowConversion:   &allow,
	}})

	plan := PlanForRequest(channel, ProtocolResponses, "claude-sonnet-4", "/v1/responses", RequestFeatureSet{})

	assert.Equal(t, StatusIncompatible, plan.Status)
	assert.Empty(t, plan.UpstreamProtocol)
}

func TestPlanForRequestCompactRequiresNativeResponses(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	allow := true
	chatChannel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	chatChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
		AllowConversion:   &allow,
	}})

	converted := PlanForRequest(chatChannel, ProtocolResponses, "gpt-test", "/v1/responses/compact", RequestFeatureSet{})
	assert.Equal(t, StatusIncompatible, converted.Status)
	assert.Contains(t, converted.Reason, "native Responses")

	responsesChannel := &model.Channel{Type: constant.ChannelTypeCodex}
	native := PlanForRequest(responsesChannel, ProtocolResponses, "gpt-test", "/v1/responses/compact", RequestFeatureSet{})
	assert.Equal(t, StatusNative, native.Status)
	assert.Equal(t, ProtocolResponses, native.UpstreamProtocol)
}

func TestPlanForRequestRejectsNativeOnlyResponsesFeaturesBeforeConversion(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})

	tests := []struct {
		name     string
		features RequestFeatureSet
		want     string
	}{
		{name: "conversation", features: RequestFeatureSet{HasConversation: true}, want: "conversation"},
		{name: "prompt", features: RequestFeatureSet{HasPrompt: true}, want: "prompt"},
		{name: "context management", features: RequestFeatureSet{HasContextManagement: true}, want: "context_management"},
		{name: "hosted tool", features: RequestFeatureSet{HostedToolTypes: []string{"web_search"}}, want: "native Responses"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanForRequest(channel, ProtocolResponses, "gpt-test", "/v1/responses", test.features)
			assert.Equal(t, StatusIncompatible, plan.Status)
			assert.Contains(t, plan.Reason, test.want)
		})
	}
}

func TestPlanForRequestRejectsMessagesServerToolsBeforeConversion(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})

	plan := PlanForRequest(channel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{
		HostedToolTypes: []string{"web_search_20250305"},
	})

	assert.Equal(t, StatusIncompatible, plan.Status)
	assert.Contains(t, plan.Reason, "native Messages")
}

func TestPlanForRequestRejectsLossyContentTypesOnlyForConversion(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	chatChannel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	chatChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})

	messagesPlan := PlanForRequest(chatChannel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{
		ContentTypes: []string{"text", "document", "redacted_thinking"},
	})
	assert.Equal(t, StatusIncompatible, messagesPlan.Status)
	assert.Contains(t, messagesPlan.Reason, "document, redacted_thinking")

	messagesChannel := &model.Channel{Type: constant.ChannelTypeAnthropic}
	messagesChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityMessages},
	}})
	responsesPlan := PlanForRequest(messagesChannel, ProtocolResponses, "gpt-public", "/v1/responses", RequestFeatureSet{
		ContentTypes: []string{"input_text", "input_audio"},
	})
	assert.Equal(t, StatusIncompatible, responsesPlan.Status)
	assert.Contains(t, responsesPlan.Reason, "input_audio")

	nativePlan := PlanForRequest(messagesChannel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{
		ContentTypes: []string{"document", "redacted_thinking"},
	})
	assert.Equal(t, StatusNative, nativePlan.Status)
}

func TestPlanForRequestTriesNextProtocolWhenContentIsLossyForFirst(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityMessages, dto.ProtocolCapabilityChat},
	}})

	plan := PlanForRequest(channel, ProtocolResponses, "gpt-public", "/v1/responses", RequestFeatureSet{
		ContentTypes: []string{"input_audio"},
	})

	assert.Equal(t, StatusConvertible, plan.Status)
	assert.Equal(t, ProtocolChat, plan.UpstreamProtocol)
}

func TestPlanForRequestDefaultUpstreamProtocols(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	officialURL := "https://api.openai.com/v1"
	emptyURL := ""
	compatibleURL := "https://gateway.example/v1"
	tests := []struct {
		name     string
		channel  *model.Channel
		protocol Protocol
		status   Status
	}{
		{name: "Codex Responses", channel: &model.Channel{Type: constant.ChannelTypeCodex}, protocol: ProtocolResponses, status: StatusNative},
		{name: "Anthropic Messages", channel: &model.Channel{Type: constant.ChannelTypeAnthropic}, protocol: ProtocolMessages, status: StatusNative},
		{name: "AWS Messages", channel: &model.Channel{Type: constant.ChannelTypeAws}, protocol: ProtocolMessages, status: StatusNative},
		{name: "Azure Responses", channel: &model.Channel{Type: constant.ChannelTypeAzure}, protocol: ProtocolResponses, status: StatusNative},
		{name: "default OpenAI Responses", channel: &model.Channel{Type: constant.ChannelTypeOpenAI}, protocol: ProtocolResponses, status: StatusNative},
		{name: "empty OpenAI Responses", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &emptyURL}, protocol: ProtocolResponses, status: StatusNative},
		{name: "official OpenAI Responses", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &officialURL}, protocol: ProtocolResponses, status: StatusNative},
		{name: "compatible OpenAI Chat", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &compatibleURL}, protocol: ProtocolChat, status: StatusNative},
		{name: "compatible OpenAI does not assume Responses", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &compatibleURL}, protocol: ProtocolResponses, status: StatusIncompatible},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanForRequest(test.channel, test.protocol, "model", canonicalPath(test.protocol, "model"), RequestFeatureSet{})
			assert.Equal(t, test.status, plan.Status)
		})
	}
}

func withProtocolBridgePolicy(t *testing.T, enabled, defaultAllowConversion bool) {
	t.Helper()
	settings := model_setting.GetGlobalSettings()
	original := settings.ProtocolBridgePolicy
	settings.ProtocolBridgePolicy.Enabled = enabled
	settings.ProtocolBridgePolicy.DefaultAllowConversion = defaultAllowConversion
	t.Cleanup(func() {
		settings.ProtocolBridgePolicy = original
	})
}
