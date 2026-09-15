package channelcompat

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"regexp"
	"strings"
)

// Legacy compatibility is used only to decode pre-v1 policy intent. It never
// converts or executes a request; old and new data use the same planner.
func legacyCompatibility(channel *model.Channel, protocol Protocol, modelName, requestPath string) Compatibility {
	if channel == nil || protocol == "" {
		return incompatible()
	}
	modelName = strings.TrimSpace(modelName)

	if channel.Type == constant.ChannelTypeAdvancedCustom {
		return advancedCustomCompatibility(channel, protocol, modelName, requestPath)
	}

	switch channel.Type {
	case constant.ChannelTypeCodex:
		if protocol == ProtocolResponses {
			return native(ProtocolResponses)
		}
		return incompatible()
	case constant.ChannelTypeAnthropic:
		switch protocol {
		case ProtocolMessages:
			return native(ProtocolMessages)
		case ProtocolChat:
			return converted(ProtocolMessages, relayconvert.ConverterOpenAIChatToClaudeMessages)
		case ProtocolGemini:
			return converted(ProtocolMessages, "gemini_generate_content_to_claude_messages")
		default:
			return incompatible()
		}
	case constant.ChannelTypeGemini:
		switch protocol {
		case ProtocolGemini:
			return native(ProtocolGemini)
		case ProtocolChat:
			return converted(ProtocolGemini, relayconvert.ConverterOpenAIChatToGeminiContent)
		case ProtocolMessages:
			return converted(ProtocolGemini, "claude_messages_to_gemini_generate_content")
		case ProtocolResponses:
			return converted(ProtocolGemini, relayconvert.ConverterOpenAIResponsesToGemini)
		}
	case constant.ChannelTypeVertexAi:
		return vertexCompatibility(protocol, modelName)
	case constant.ChannelTypeAws:
		if strings.Contains(strings.ToLower(modelName), "nova-") && protocol == ProtocolChat {
			return native(ProtocolChat)
		}
		switch protocol {
		case ProtocolMessages:
			return native(ProtocolMessages)
		case ProtocolChat:
			return converted(ProtocolMessages, relayconvert.ConverterOpenAIChatToClaudeMessages)
		default:
			return incompatible()
		}
	case constant.ChannelTypeAli:
		switch protocol {
		case ProtocolChat, ProtocolResponses:
			return native(protocol)
		case ProtocolMessages:
			if aliSupportsMessages(modelName) {
				return native(ProtocolMessages)
			}
			return converted(ProtocolChat, relayconvert.ConverterClaudeMessagesToOpenAIChat)
		default:
			return incompatible()
		}
	case constant.ChannelTypeDeepSeek, constant.ChannelTypeMoonshot, constant.ChannelTypeMiniMax:
		if protocol == ProtocolChat || protocol == ProtocolMessages {
			return native(protocol)
		}
		return incompatible()
	case constant.ChannelTypeVolcEngine:
		switch protocol {
		case ProtocolChat, ProtocolResponses:
			return native(protocol)
		case ProtocolMessages:
			if _, ok := constant.ChannelSpecialBases[channel.GetBaseURL()]; ok {
				return native(ProtocolMessages)
			}
			return converted(ProtocolChat, relayconvert.ConverterClaudeMessagesToOpenAIChat)
		default:
			return incompatible()
		}
	}

	apiType, _ := common.ChannelType2APIType(channel.Type)
	switch apiType {
	case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference:
		switch protocol {
		case ProtocolChat, ProtocolResponses:
			return native(protocol)
		case ProtocolMessages:
			return converted(ProtocolChat, relayconvert.ConverterClaudeMessagesToOpenAIChat)
		case ProtocolGemini:
			return converted(ProtocolChat, relayconvert.ConverterGeminiContentToOpenAIChat)
		}
	case constant.APITypeZhipuV4:
		if protocol == ProtocolChat || protocol == ProtocolMessages {
			return native(protocol)
		}
	}

	// Existing text adaptors are all exposed through the OpenAI-compatible chat
	// entry unless a more specific capability is declared above.
	if protocol == ProtocolChat {
		return native(ProtocolChat)
	}
	return incompatible()
}

func vertexCompatibility(protocol Protocol, modelName string) Compatibility {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case strings.HasPrefix(modelName, "claude"):
		if protocol == ProtocolMessages {
			return native(ProtocolMessages)
		}
		if protocol == ProtocolChat {
			return converted(ProtocolMessages, relayconvert.ConverterOpenAIChatToClaudeMessages)
		}
	case strings.Contains(modelName, "llama"), strings.Contains(modelName, "-maas"):
		if protocol == ProtocolChat {
			return native(ProtocolChat)
		}
	default:
		switch protocol {
		case ProtocolGemini:
			return native(ProtocolGemini)
		case ProtocolChat:
			return converted(ProtocolGemini, relayconvert.ConverterOpenAIChatToGeminiContent)
		}
	}
	return incompatible()
}

func LegacyPolicyForRequest(channel *model.Channel, protocol Protocol, modelName, requestPath string, settings hostdto.ChannelOtherSettings, global model_setting.GlobalSettings) (hostdto.ProtocolPolicy, []Protocol, error) {
	policy := global.EffectiveProtocolPolicy()
	policy.Conversion = hostdto.ProtocolConversionSafe
	if settings.ToolLossPolicy == "strict" {
		policy.Conversion = hostdto.ProtocolConversionLossless
	}
	if channel.Setting != nil {
		var setting hostdto.ChannelSettings
		if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
			return policy, nil, fmt.Errorf("invalid channel request settings")
		}
		if setting.PassThroughBodyEnabled {
			policy.RequestMode = hostdto.ProtocolRequestPassthrough
		}
	}
	caps := settings.ProtocolCapabilities
	if global.ProtocolBridgePolicy.Enabled && caps != nil && (protocol == ProtocolResponses || protocol == ProtocolMessages) {
		_, allowed := caps.Resolve(modelName)
		if allowed != nil && !*allowed {
			policy.Conversion = hostdto.ProtocolConversionNative
		}
	}
	if channel.Type != constant.ChannelTypeAdvancedCustom && (protocol == ProtocolResponses || protocol == ProtocolMessages) && global.ProtocolBridgePolicy.Enabled && (caps != nil || global.ProtocolBridgePolicy.DefaultAllowConversion) {
		configured, allowed := caps.Resolve(modelName)
		if allowed != nil && !*allowed {
			policy.Conversion = hostdto.ProtocolConversionNative
		}
		if caps != nil && caps.GetSelectionMode() == hostdto.ProtocolSelectionModeAuto {
			policy.Selection = hostdto.ProtocolSelectionAutomatic
			if len(configured) == 0 {
				return policy, automaticProbeProtocols(channel, modelName), nil
			}
		}
		if len(configured) == 0 {
			configured = defaultUpstreamProtocols(channel, modelName)
		}
		candidates := make([]Protocol, len(configured))
		for i, p := range configured {
			candidates[i] = Protocol(p)
		}
		return policy, candidates, nil
	}
	legacy := legacyCompatibility(channel, protocol, modelName, requestPath)
	if legacy.Status == StatusIncompatible && channel.Type == constant.ChannelTypeAdvancedCustom {
		if main, ok := MainProtocolPathForAuxiliaryRequest(requestPath); ok {
			legacy = legacyCompatibility(channel, protocol, modelName, main)
		}
	}
	if legacy.Status == StatusIncompatible {
		return policy, nil, fmt.Errorf("channel does not support %s requests", protocol)
	}
	if (protocol == ProtocolChat || protocol == ProtocolMessages && !global.ProtocolBridgePolicy.Enabled) && policy.RequestMode != hostdto.ProtocolRequestPassthrough && global.ChatCompletionsToResponsesPolicy.IsChannelEnabled(channel.Id, channel.Type) {
		for _, pattern := range global.ChatCompletionsToResponsesPolicy.ModelPatterns {
			matched, err := regexp.MatchString(pattern, modelName)
			if err == nil && pattern != "" && matched {
				return policy, []Protocol{ProtocolResponses}, nil
			}
		}
	}
	return policy, []Protocol{legacy.UpstreamProtocol}, nil
}
