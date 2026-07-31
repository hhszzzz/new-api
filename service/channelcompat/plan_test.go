package channelcompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeRequestFeatureSetsPreservesUniqueCapabilities(t *testing.T) {
	merged := MergeRequestFeatureSets(
		RequestFeatureSet{
			Stream:               true,
			HasCustomTools:       true,
			ContentTypes:         []string{"input_text", "input_image"},
			DeclaredHostedTools:  []string{"web_search_preview"},
			MessagesNativeFields: []string{"container"},
		},
		RequestFeatureSet{
			HasPreviousResponse:   true,
			HasNamespaceTools:     true,
			ContentTypes:          []string{"input_image", "output_text"},
			DeclaredHostedTools:   []string{"web_search_preview", "file_search"},
			HistoricalHostedTools: []string{"web_search_call"},
			MessagesNativeFields:  []string{"container", "mcp_servers"},
		},
	)

	assert.True(t, merged.Stream)
	assert.True(t, merged.HasPreviousResponse)
	assert.True(t, merged.HasCustomTools)
	assert.True(t, merged.HasNamespaceTools)
	assert.Equal(t, []string{"input_text", "input_image", "output_text"}, merged.ContentTypes)
	assert.Equal(t, []string{"web_search_preview", "file_search"}, merged.DeclaredHostedTools)
	assert.Equal(t, []string{"web_search_call"}, merged.HistoricalHostedTools)
	assert.Equal(t, []string{"container", "mcp_servers"}, merged.MessagesNativeFields)
}

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
	assert.Equal(t, []string{"web_search_preview"}, features.DeclaredHostedTools)
	assert.Empty(t, features.HistoricalHostedTools)
}

func TestExtractRequestFeatureSetRecognizesStringToolsAndNamespaceChildren(t *testing.T) {
	body := []byte(`{
		"tools":[
			"apply_patch",
			{"type":"namespace","name":"workspace","children":["shell"]}
		]
	}`)

	features, err := ExtractRequestFeatureSet(ProtocolResponses, body)

	require.NoError(t, err)
	assert.True(t, features.HasCustomTools)
	assert.True(t, features.HasNamespaceTools)
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
	assert.Empty(t, features.DeclaredHostedTools)
	assert.Equal(t, []string{"server_tool_use"}, features.HistoricalHostedTools)
}

func TestExtractRequestFeatureSetMessagesDetectsLossyTopLevelFields(t *testing.T) {
	body := []byte(`{
		"stop_sequences":["END"],
		"top_k":0,
		"output_format":{"type":"json_schema"},
		"output_config":{"effort":"high","format":{"type":"json"}},
		"container":{"id":"container_1"},
		"mcp_servers":[{"name":"tools"}],
		"inference_geo":"us",
		"speed":"fast",
		"service_tier":"priority"
	}`)

	features, err := ExtractRequestFeatureSet(ProtocolMessages, body)

	require.NoError(t, err)
	assert.True(t, features.HasStopSequences)
	assert.True(t, features.HasTopK)
	assert.Equal(t, []string{
		"output_format",
		"container",
		"mcp_servers",
		"inference_geo",
		"speed",
		"service_tier",
		"output_config",
	}, features.MessagesNativeFields)

	effortOnly, err := ExtractRequestFeatureSet(ProtocolMessages, []byte(`{"output_config":{"effort":"max"}}`))
	require.NoError(t, err)
	assert.Empty(t, effortOnly.MessagesNativeFields)
}

func TestExtractRequestFeatureSetTreatsUnspecifiedToolSearchAsClientExecuted(t *testing.T) {
	defaultBody := []byte(`{"tools":[{"type":"tool_search"}]}`)
	defaultFeatures, err := ExtractRequestFeatureSet(ProtocolResponses, defaultBody)
	require.NoError(t, err)
	assert.True(t, defaultFeatures.HasToolSearch)
	assert.Empty(t, defaultFeatures.DeclaredHostedTools)

	clientBody := []byte(`{"tools":[{"type":"tool_search","execution":"client"}]}`)
	clientFeatures, err := ExtractRequestFeatureSet(ProtocolResponses, clientBody)
	require.NoError(t, err)
	assert.True(t, clientFeatures.HasToolSearch)
	assert.Empty(t, clientFeatures.DeclaredHostedTools)

	serverBody := []byte(`{"tools":[{"type":"tool_search","execution":"server"}]}`)
	serverFeatures, err := ExtractRequestFeatureSet(ProtocolResponses, serverBody)
	require.NoError(t, err)
	assert.True(t, serverFeatures.HasToolSearch)
	assert.Equal(t, []string{"tool_search"}, serverFeatures.DeclaredHostedTools)
}

func TestExtractRequestFeatureSetDetectsMessagesServerTools(t *testing.T) {
	body := []byte(`{"tools":[{"name":"lookup","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search"},{"type":"tool_search_tool_regex_20251119","name":"tool_search"}]}`)

	features, err := ExtractRequestFeatureSet(ProtocolMessages, body)

	require.NoError(t, err)
	assert.True(t, features.HasToolSearch)
	// Typed Messages tool declarations are dropped or lowered by the request
	// converters, so they no longer register as hosted-tool features.
	assert.Empty(t, features.DeclaredHostedTools)
	assert.Empty(t, features.HistoricalHostedTools)
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
	}, features.HistoricalHostedTools)
	assert.Empty(t, features.DeclaredHostedTools)
}

func TestExtractRequestFeatureSetDetectsHostedToolsLoadedFromToolSearchOutput(t *testing.T) {
	body := []byte(`{
		"input":[{
			"type":"tool_search_output",
			"call_id":"call_search",
			"tools":[
				{"type":"function","name":"lookup","parameters":{"type":"object"}},
				{"type":"web_search_preview"}
			]
		}]
	}`)

	features, err := ExtractRequestFeatureSet(ProtocolResponses, body)

	require.NoError(t, err)
	assert.Equal(t, []string{"web_search_preview"}, features.DeclaredHostedTools)
	assert.Empty(t, features.HistoricalHostedTools)
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
	assert.True(t, plan.StateEnabled)
	assert.Equal(t, "provider-chat-model", plan.EffectiveUpstreamModel)
	assert.Equal(t, StateModeReplay, plan.StateMode)
}

func TestPlanForCompactUsesActualMappedUpstreamModelForOverrides(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	allow := true
	disallow := false
	mapping := `{"public-model":"provider-chat-model"}`
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, ModelMapping: &mapping}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityResponses},
		AllowConversion:   &disallow,
		ModelOverrides: []dto.ProtocolCapabilityModelOverride{{
			ModelPattern:      `^provider-chat-model$`,
			UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
			AllowConversion:   &allow,
		}},
	}})

	plan := PlanForRequest(
		channel,
		ProtocolResponses,
		ratio_setting.WithCompactModelSuffix("public-model"),
		"/v1/responses/compact",
		RequestFeatureSet{},
	)

	assert.Equal(t, StatusConvertible, plan.Status)
	assert.Equal(t, ProtocolChat, plan.UpstreamProtocol)
	assert.Equal(t, "provider-chat-model", plan.EffectiveUpstreamModel)
	assert.Equal(t, relayconvert.ConverterOpenAIResponsesToOpenAIChat, plan.RequestConverter)
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

func TestPlanForRequestGlobalDisableHardGatesExplicitCapabilities(t *testing.T) {
	withProtocolBridgePolicy(t, false, false)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})

	// With the global bridge switch off, configured capabilities are ignored:
	// Responses passes through natively on OpenAI-type channels.
	plan := PlanForRequest(channel, ProtocolResponses, "gpt-test", "/v1/responses", RequestFeatureSet{})
	assert.Equal(t, StatusNative, plan.Status)
	assert.Equal(t, ProtocolResponses, plan.UpstreamProtocol)
	assert.Empty(t, plan.RequestConverter)

	// The legacy adaptor layer keeps working: Messages still converts to chat.
	messagesPlan := PlanForRequest(channel, ProtocolMessages, "gpt-test", "/v1/messages", RequestFeatureSet{})
	assert.Equal(t, StatusConvertible, messagesPlan.Status)
	assert.Equal(t, ProtocolChat, messagesPlan.UpstreamProtocol)
	assert.Equal(t, relayconvert.ConverterClaudeMessagesToOpenAIChat, messagesPlan.RequestConverter)
}

func TestPlanForRequestExplicitConversionDisableStillWins(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	disallow := false
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
		AllowConversion:   &disallow,
	}})

	plan := PlanForRequest(channel, ProtocolResponses, "gpt-test", "/v1/responses", RequestFeatureSet{})

	assert.Equal(t, StatusIncompatible, plan.Status)
	assert.Contains(t, plan.Reason, "disabled")
}

func TestPlanForRequestCompactCanUseChatBridge(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	chatChannel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	chatChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})

	converted := PlanForRequest(chatChannel, ProtocolResponses, "gpt-test", "/v1/responses/compact", RequestFeatureSet{})
	assert.Equal(t, StatusConvertible, converted.Status)
	assert.Equal(t, ProtocolChat, converted.UpstreamProtocol)
	assert.Equal(t, relayconvert.ConverterOpenAIResponsesToOpenAIChat, converted.RequestConverter)

	responsesChannel := &model.Channel{Type: constant.ChannelTypeCodex}
	native := PlanForRequest(responsesChannel, ProtocolResponses, "gpt-test", "/v1/responses/compact", RequestFeatureSet{})
	assert.Equal(t, StatusNative, native.Status)
	assert.Equal(t, ProtocolResponses, native.UpstreamProtocol)
}

func TestPlanForRequestRejectsStatefulFieldsAndHostedToolHistory(t *testing.T) {
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanForRequest(channel, ProtocolResponses, "gpt-test", "/v1/responses", test.features)
			assert.Equal(t, StatusIncompatible, plan.Status)
			assert.Contains(t, plan.Reason, test.want)
		})
	}

	// Declared hosted tools no longer block selection: the request converters
	// drop them (CC Switch semantics), so the plan stays convertible.
	declaredHostedPlan := PlanForRequest(channel, ProtocolResponses, "gpt-test", "/v1/responses", RequestFeatureSet{
		DeclaredHostedTools: []string{"web_search_preview", "file_search"},
	})
	assert.Equal(t, StatusConvertible, declaredHostedPlan.Status)

	historicalHostedPlan := PlanForRequest(channel, ProtocolResponses, "gpt-test", "/v1/responses", RequestFeatureSet{
		HistoricalHostedTools: []string{"web_search_call", "file_search_call"},
	})
	assert.Equal(t, StatusIncompatible, historicalHostedPlan.Status)
	assert.Contains(t, historicalHostedPlan.Reason, "web_search_call, file_search_call")

	unknownPlan := PlanForRequest(channel, ProtocolResponses, "gpt-test", "/v1/responses", RequestFeatureSet{
		HistoricalHostedTools: []string{"future_server_tool"},
	})
	assert.Equal(t, StatusIncompatible, unknownPlan.Status)
	assert.Contains(t, unknownPlan.Reason, "future_server_tool")
}

func TestPlanForRequestRejectsMessagesServerToolsBeforeConversion(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})

	// Declared server tools are dropped by the converter, so selection stays
	// convertible; executed server tool history still requires a native upstream.
	declaredPlan := PlanForRequest(channel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{
		DeclaredHostedTools: []string{"web_search_20250305"},
	})
	assert.Equal(t, StatusConvertible, declaredPlan.Status)

	historyPlan := PlanForRequest(channel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{
		HistoricalHostedTools: []string{"server_tool_use"},
	})
	assert.Equal(t, StatusIncompatible, historyPlan.Status)
	assert.Contains(t, historyPlan.Reason, "native Messages")
}

func TestPlanForRequestRejectsMessagesFieldsOnlyOnLossyRoutes(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	responsesChannel := &model.Channel{Type: constant.ChannelTypeCodex}
	responsesChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityResponses},
	}})
	chatChannel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	chatChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
	}})
	messagesChannel := &model.Channel{Type: constant.ChannelTypeAnthropic}
	messagesChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols: []string{dto.ProtocolCapabilityMessages},
	}})

	stopPlan := PlanForRequest(responsesChannel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{HasStopSequences: true})
	assert.Equal(t, StatusIncompatible, stopPlan.Status)
	assert.Contains(t, stopPlan.Reason, "stop_sequences")

	topKPlan := PlanForRequest(responsesChannel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{HasTopK: true})
	assert.Equal(t, StatusIncompatible, topKPlan.Status)
	assert.Contains(t, topKPlan.Reason, "top_k")

	chatPlan := PlanForRequest(chatChannel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{
		HasStopSequences: true,
		HasTopK:          true,
	})
	assert.Equal(t, StatusConvertible, chatPlan.Status)
	assert.Equal(t, ProtocolChat, chatPlan.UpstreamProtocol)

	nativeOnlyPlan := PlanForRequest(chatChannel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{
		MessagesNativeFields: []string{"output_format", "container"},
	})
	assert.Equal(t, StatusIncompatible, nativeOnlyPlan.Status)
	assert.Contains(t, nativeOnlyPlan.Reason, "output_format, container")

	nativePlan := PlanForRequest(messagesChannel, ProtocolMessages, "claude-public", "/v1/messages", RequestFeatureSet{
		HasStopSequences:     true,
		HasTopK:              true,
		MessagesNativeFields: []string{"output_format"},
	})
	assert.Equal(t, StatusNative, nativePlan.Status)
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
	assert.Contains(t, messagesPlan.Reason, "redacted_thinking")
	assert.NotContains(t, messagesPlan.Reason, "document")

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
	withProtocolBridgePolicy(t, true, true)
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
		{name: "AWS Nova bridges Responses through Chat", channel: &model.Channel{Type: constant.ChannelTypeAws}, protocol: ProtocolResponses, status: StatusConvertible},
		{name: "AWS Nova bridges Messages through Chat", channel: &model.Channel{Type: constant.ChannelTypeAws}, protocol: ProtocolMessages, status: StatusConvertible},
		{name: "Azure Responses", channel: &model.Channel{Type: constant.ChannelTypeAzure}, protocol: ProtocolResponses, status: StatusNative},
		{name: "default OpenAI Responses", channel: &model.Channel{Type: constant.ChannelTypeOpenAI}, protocol: ProtocolResponses, status: StatusNative},
		{name: "empty OpenAI Responses", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &emptyURL}, protocol: ProtocolResponses, status: StatusNative},
		{name: "official OpenAI Responses", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &officialURL}, protocol: ProtocolResponses, status: StatusNative},
		{name: "compatible OpenAI Chat", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &compatibleURL}, protocol: ProtocolChat, status: StatusNative},
		{name: "compatible OpenAI bridges Responses through Chat", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &compatibleURL}, protocol: ProtocolResponses, status: StatusConvertible},
		{name: "Gemini bridges Responses through generateContent", channel: &model.Channel{Type: constant.ChannelTypeGemini}, protocol: ProtocolResponses, status: StatusConvertible},
		{name: "Vertex Gemini bridges Responses through generateContent", channel: &model.Channel{Type: constant.ChannelTypeVertexAi}, protocol: ProtocolResponses, status: StatusConvertible},
		{name: "Vertex Claude Messages", channel: &model.Channel{Type: constant.ChannelTypeVertexAi}, protocol: ProtocolMessages, status: StatusNative},
		{name: "Vertex open source bridges Responses through Chat", channel: &model.Channel{Type: constant.ChannelTypeVertexAi}, protocol: ProtocolResponses, status: StatusConvertible},
		{name: "Ali Responses", channel: &model.Channel{Type: constant.ChannelTypeAli}, protocol: ProtocolResponses, status: StatusNative},
		{name: "VolcEngine Responses", channel: &model.Channel{Type: constant.ChannelTypeVolcEngine}, protocol: ProtocolResponses, status: StatusNative},
		{name: "XAI Responses", channel: &model.Channel{Type: constant.ChannelTypeXai}, protocol: ProtocolResponses, status: StatusNative},
		{name: "Sub2API Messages", channel: &model.Channel{Type: constant.ChannelTypeSub2API}, protocol: ProtocolMessages, status: StatusNative},
		{name: "New API Responses", channel: &model.Channel{Type: constant.ChannelTypeNewAPI}, protocol: ProtocolResponses, status: StatusNative},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			modelName := "model"
			switch test.name {
			case "AWS Nova bridges Responses through Chat", "AWS Nova bridges Messages through Chat":
				modelName = "nova-pro-v1:0"
			case "Vertex Claude Messages":
				modelName = "claude-sonnet-4"
			case "Vertex open source bridges Responses through Chat":
				modelName = "meta/llama3-405b-instruct-maas"
			}
			plan := PlanForRequest(test.channel, test.protocol, modelName, canonicalPath(test.protocol, modelName), RequestFeatureSet{})
			assert.Equal(t, test.status, plan.Status)
		})
	}
}

func TestPlanForRequestExplicitMessagesOverridesProviderHeuristics(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	volcBaseURL := "https://ark.example"
	tests := []struct {
		name    string
		channel *model.Channel
		model   string
	}{
		{name: "Ali", channel: &model.Channel{Type: constant.ChannelTypeAli}, model: "wan2.6"},
		{name: "VolcEngine", channel: &model.Channel{Type: constant.ChannelTypeVolcEngine, BaseURL: &volcBaseURL}, model: "doubao-seed-code"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
				UpstreamProtocols: []string{dto.ProtocolCapabilityMessages},
			}})

			plan := PlanForRequest(test.channel, ProtocolMessages, test.model, "/v1/messages", RequestFeatureSet{})

			assert.Equal(t, StatusNative, plan.Status)
			assert.Equal(t, ProtocolMessages, plan.UpstreamProtocol)
			assert.True(t, plan.ExplicitCapabilities)
		})
	}
}

func TestPlanForRequestUnconfiguredProviderHeuristicsKeepLegacyChatFallback(t *testing.T) {
	withProtocolBridgePolicy(t, false, false)
	volcBaseURL := "https://ark.example"
	tests := []struct {
		name    string
		channel *model.Channel
		model   string
	}{
		{name: "Ali", channel: &model.Channel{Type: constant.ChannelTypeAli}, model: "wan2.6"},
		{name: "VolcEngine", channel: &model.Channel{Type: constant.ChannelTypeVolcEngine, BaseURL: &volcBaseURL}, model: "doubao-seed-code"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanForRequest(test.channel, ProtocolMessages, test.model, "/v1/messages", RequestFeatureSet{})

			assert.Equal(t, StatusConvertible, plan.Status)
			assert.Equal(t, ProtocolChat, plan.UpstreamProtocol)
			assert.False(t, plan.ExplicitCapabilities)
		})
	}
}

func TestPlanForRequestPreservesLegacyCapabilitiesWhenConversionRequiresExplicitOptIn(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	compatibleURL := "https://gateway.example/v1"
	tests := []struct {
		name             string
		channel          *model.Channel
		protocol         Protocol
		wantStatus       Status
		wantUpstream     Protocol
		wantConverter    string
		wantStateEnabled bool
	}{
		{
			name:         "OpenAI-compatible Responses remains native",
			channel:      &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &compatibleURL},
			protocol:     ProtocolResponses,
			wantStatus:   StatusNative,
			wantUpstream: ProtocolResponses,
		},
		{
			name:          "Gemini Responses keeps existing converter",
			channel:       &model.Channel{Type: constant.ChannelTypeGemini},
			protocol:      ProtocolResponses,
			wantStatus:    StatusConvertible,
			wantUpstream:  ProtocolGemini,
			wantConverter: relayconvert.ConverterOpenAIResponsesToGemini,
		},
		{
			name:         "Ali Responses remains native",
			channel:      &model.Channel{Type: constant.ChannelTypeAli},
			protocol:     ProtocolResponses,
			wantStatus:   StatusNative,
			wantUpstream: ProtocolResponses,
		},
		{
			name:         "VolcEngine Responses remains native",
			channel:      &model.Channel{Type: constant.ChannelTypeVolcEngine},
			protocol:     ProtocolResponses,
			wantStatus:   StatusNative,
			wantUpstream: ProtocolResponses,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanForRequest(test.channel, test.protocol, "model", canonicalPath(test.protocol, "model"), RequestFeatureSet{})

			assert.Equal(t, test.wantStatus, plan.Status)
			assert.Equal(t, test.wantUpstream, plan.UpstreamProtocol)
			assert.Equal(t, test.wantConverter, plan.RequestConverter)
			assert.Equal(t, test.wantStateEnabled, plan.StateEnabled)
		})
	}
}

func TestPlansForRequestAutomaticOrderFollowsEntryProtocol(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	tests := []struct {
		name        string
		protocol    Protocol
		requestPath string
		want        []Protocol
	}{
		{
			name:        "Codex Responses",
			protocol:    ProtocolResponses,
			requestPath: "/v1/responses",
			want:        []Protocol{ProtocolResponses, ProtocolChat, ProtocolMessages, ProtocolGemini},
		},
		{
			name:        "Claude Code Messages",
			protocol:    ProtocolMessages,
			requestPath: "/v1/messages",
			want:        []Protocol{ProtocolMessages, ProtocolChat, ProtocolResponses, ProtocolGemini},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{Id: 901, Type: constant.ChannelTypeOpenAI}
			channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
				SelectionMode: dto.ProtocolSelectionModeAuto,
			}})

			plans := PlansForRequest(channel, test.protocol, "public-model", test.requestPath, RequestFeatureSet{})

			require.Len(t, plans, len(test.want))
			actual := make([]Protocol, 0, len(plans))
			for _, plan := range plans {
				actual = append(actual, plan.UpstreamProtocol)
				assert.Equal(t, dto.ProtocolSelectionModeAuto, plan.SelectionMode)
				assert.True(t, plan.StateEnabled)
			}
			assert.Equal(t, test.want, actual)
			assert.Equal(t, StatusNative, plans[0].Status)
			assert.Equal(t, StatusConvertible, plans[1].Status)
			assert.Equal(t, StatusConvertible, plans[2].Status)
		})
	}
}

func TestPlansForRequestAutomaticModeOnlyProbesExecutableProviderProtocols(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	tests := []struct {
		name    string
		channel *model.Channel
		model   string
		want    []Protocol
	}{
		{name: "Codex", channel: &model.Channel{Type: constant.ChannelTypeCodex}, model: "gpt-5", want: []Protocol{ProtocolResponses}},
		{name: "AWS Claude", channel: &model.Channel{Type: constant.ChannelTypeAws}, model: "claude-sonnet", want: []Protocol{ProtocolMessages}},
		{name: "AWS Nova", channel: &model.Channel{Type: constant.ChannelTypeAws}, model: "amazon.nova-pro-v1:0", want: []Protocol{ProtocolChat}},
		{name: "DeepSeek", channel: &model.Channel{Type: constant.ChannelTypeDeepSeek}, model: "deepseek-v4", want: []Protocol{ProtocolChat, ProtocolMessages}},
		{name: "xAI", channel: &model.Channel{Type: constant.ChannelTypeXai}, model: "grok-4", want: []Protocol{ProtocolResponses, ProtocolChat}},
		{name: "Ali without Messages model support", channel: &model.Channel{Type: constant.ChannelTypeAli}, model: "wan2.6", want: []Protocol{ProtocolResponses, ProtocolChat}},
		{name: "Ali with Messages model support", channel: &model.Channel{Type: constant.ChannelTypeAli}, model: "qwen-max", want: []Protocol{ProtocolResponses, ProtocolChat, ProtocolMessages}},
		{name: "Vertex Claude", channel: &model.Channel{Type: constant.ChannelTypeVertexAi}, model: "claude-sonnet-4-6", want: []Protocol{ProtocolMessages}},
		{name: "Vertex Gemini", channel: &model.Channel{Type: constant.ChannelTypeVertexAi}, model: "gemini-2.5-pro", want: []Protocol{ProtocolGemini}},
		{name: "ordinary Chat adaptor", channel: &model.Channel{Type: constant.ChannelTypeOllama}, model: "llama3", want: []Protocol{ProtocolChat}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
				SelectionMode: dto.ProtocolSelectionModeAuto,
			}})

			plans := PlansForRequest(test.channel, ProtocolResponses, test.model, "/v1/responses", RequestFeatureSet{})

			actual := make([]Protocol, 0, len(plans))
			for _, plan := range plans {
				actual = append(actual, plan.UpstreamProtocol)
			}
			assert.Equal(t, test.want, actual)
		})
	}
}

func TestPlansForRequestAutomaticModeUsesCCSwitchFullEndpointSignal(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	tests := []struct {
		name    string
		baseURL string
		want    Protocol
	}{
		{name: "Chat endpoint", baseURL: "https://gateway.example/proxy/v1/chat/completions?region=cn", want: ProtocolChat},
		{name: "Responses endpoint", baseURL: "https://gateway.example/backend-api/codex/responses", want: ProtocolResponses},
		{name: "Messages endpoint", baseURL: "https://gateway.example/anthropic/v1/messages", want: ProtocolMessages},
		{name: "Gemini endpoint", baseURL: "https://gateway.example/v1beta/models/gemini-fixed:streamGenerateContent?alt=sse", want: ProtocolGemini},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{Id: 904, Type: constant.ChannelTypeOpenAI, BaseURL: &test.baseURL}
			channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
				SelectionMode: dto.ProtocolSelectionModeAuto,
			}})

			plans := PlansForRequest(channel, ProtocolResponses, "public-model", "/v1/responses", RequestFeatureSet{})

			require.Len(t, plans, 1)
			assert.Equal(t, test.want, plans[0].UpstreamProtocol)
			if test.want == ProtocolResponses {
				assert.Equal(t, StatusNative, plans[0].Status)
			} else {
				assert.Equal(t, StatusConvertible, plans[0].Status)
			}
		})
	}
}

func TestPlansForRequestAutomaticModeRespectsAllowedProtocolsAndConversionSwitch(t *testing.T) {
	withProtocolBridgePolicy(t, true, true)
	disallow := false
	channel := &model.Channel{Id: 902, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode:     dto.ProtocolSelectionModeAuto,
		UpstreamProtocols: []string{dto.ProtocolCapabilityResponses, dto.ProtocolCapabilityChat},
		AllowConversion:   &disallow,
	}})

	plans := PlansForRequest(channel, ProtocolResponses, "public-model", "/v1/responses", RequestFeatureSet{})

	require.Len(t, plans, 1)
	assert.Equal(t, ProtocolResponses, plans[0].UpstreamProtocol)
	assert.Equal(t, StatusNative, plans[0].Status)
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

func TestConversionFeatureGateDropsDeclaredHostedToolsButKeepsHistoryGuard(t *testing.T) {
	// Codex always declares hosted tools (local_shell, web_search); the request
	// converters drop them, so declarations must not block channel selection.
	codexBody := `{"model":"gpt-5.1","stream":true,"tools":[{"type":"local_shell"},{"type":"web_search"},{"type":"function","name":"update_plan","parameters":{"type":"object"}}],"input":"hi"}`
	features, err := ExtractRequestFeatureSet(ProtocolResponses, []byte(codexBody))
	require.NoError(t, err)
	require.NotEmpty(t, features.DeclaredHostedTools)
	reason, _ := conversionFeatureIncompatibility(ProtocolResponses, ProtocolChat, features, false)
	assert.Empty(t, reason)

	// Replaying an executed hosted call to a non-native upstream would corrupt
	// conversation context, so historical hosted items still gate. local_shell
	// is the exception: it is client-executed and lowered/restored by the
	// bridge, so its history converts.
	historyBody := `{"model":"gpt-5.1","stream":true,"input":[{"type":"web_search_call","id":"ws1","status":"completed"}]}`
	features, err = ExtractRequestFeatureSet(ProtocolResponses, []byte(historyBody))
	require.NoError(t, err)
	reason, _ = conversionFeatureIncompatibility(ProtocolResponses, ProtocolChat, features, false)
	assert.Contains(t, reason, "server tool history cannot be replayed")

	shellHistoryBody := `{"model":"gpt-5.1","stream":true,"input":[{"type":"local_shell_call","call_id":"c1","action":{"command":["ls"]}}]}`
	features, err = ExtractRequestFeatureSet(ProtocolResponses, []byte(shellHistoryBody))
	require.NoError(t, err)
	reason, _ = conversionFeatureIncompatibility(ProtocolResponses, ProtocolChat, features, false)
	assert.Empty(t, reason)
}

func TestPlanForRequestLossyConversionAdmitsEncryptedContent(t *testing.T) {
	withProtocolBridgePolicy(t, true, false)
	encryptedSeedBody := `{"model":"public-model","stream":true,"input":[{"type":"reasoning","id":"rs1","content":[{"type":"encrypted_content","data":"opaque"}]},{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	features, err := ExtractRequestFeatureSet(ProtocolResponses, []byte(encryptedSeedBody))
	require.NoError(t, err)
	require.Contains(t, features.ContentTypes, "encrypted_content")

	strictChat := func(lossy bool) *model.Channel {
		channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
		channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
			UpstreamProtocols:    []string{dto.ProtocolCapabilityChat},
			AllowLossyConversion: lossy,
		}})
		return channel
	}

	plan := PlanForRequest(strictChat(false), ProtocolResponses, "public-model", "/v1/responses", features)
	assert.Equal(t, StatusIncompatible, plan.Status)
	assert.Contains(t, plan.Reason, "encrypted_content")

	plan = PlanForRequest(strictChat(true), ProtocolResponses, "public-model", "/v1/responses", features)
	assert.Equal(t, StatusConvertible, plan.Status)
	assert.Equal(t, ProtocolChat, plan.UpstreamProtocol)
	assert.Equal(t, []string{"encrypted_content"}, plan.LossyContentTypes)

	// Hosted tool history still gates even with lossy conversion enabled:
	// dropping executed server tool context would corrupt the conversation.
	hostedHistoryBody := `{"model":"public-model","stream":true,"input":[{"type":"web_search_call","id":"ws1","status":"completed"}]}`
	hostedFeatures, err := ExtractRequestFeatureSet(ProtocolResponses, []byte(hostedHistoryBody))
	require.NoError(t, err)
	plan = PlanForRequest(strictChat(true), ProtocolResponses, "public-model", "/v1/responses", hostedFeatures)
	assert.Equal(t, StatusIncompatible, plan.Status)
	assert.Contains(t, plan.Reason, "server tool history cannot be replayed")

	// Gemini upstream conversion errors on unknown content parts, so lossy
	// drops stay limited to Chat and Messages upstreams.
	geminiChannel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	geminiChannel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		UpstreamProtocols:    []string{dto.ProtocolCapabilityGemini},
		AllowLossyConversion: true,
	}})
	plan = PlanForRequest(geminiChannel, ProtocolResponses, "public-model", "/v1/responses", features)
	assert.Equal(t, StatusIncompatible, plan.Status)
	assert.Contains(t, plan.Reason, "encrypted_content")
}
