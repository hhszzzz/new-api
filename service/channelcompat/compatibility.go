package channelcompat

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
)

type Protocol string

const (
	ProtocolChat      Protocol = "chat"
	ProtocolMessages  Protocol = "messages"
	ProtocolResponses Protocol = "responses"
	ProtocolGemini    Protocol = "gemini"
)

type Status string

const (
	StatusNative       Status = "native"
	StatusConvertible  Status = "convertible"
	StatusIncompatible Status = "incompatible"
)

type Compatibility struct {
	Status           Status   `json:"status"`
	UpstreamProtocol Protocol `json:"upstream_protocol,omitempty"`
	Converter        string   `json:"converter,omitempty"`
}

var protocols = []Protocol{
	ProtocolChat,
	ProtocolMessages,
	ProtocolResponses,
	ProtocolGemini,
}

func Protocols() []Protocol {
	return append([]Protocol(nil), protocols...)
}

func DetectRequestProtocol(requestPath string) Protocol {
	requestPath = strings.Split(strings.TrimSpace(requestPath), "?")[0]
	switch {
	case strings.HasPrefix(requestPath, "/v1/chat/completions"),
		strings.HasPrefix(requestPath, "/pg/chat/completions"):
		return ProtocolChat
	case strings.HasPrefix(requestPath, "/v1/messages"):
		return ProtocolMessages
	case strings.HasPrefix(requestPath, "/v1/responses"):
		return ProtocolResponses
	case strings.Contains(requestPath, "/models/") &&
		(strings.Contains(requestPath, ":generateContent") || strings.Contains(requestPath, ":streamGenerateContent")):
		return ProtocolGemini
	default:
		return ""
	}
}

func Matrix(channel *model.Channel, modelName string) map[string]Compatibility {
	result := make(map[string]Compatibility, len(protocols))
	for _, protocol := range protocols {
		result[string(protocol)] = ForRequest(channel, protocol, modelName, canonicalPath(protocol, modelName))
	}
	return result
}

func ForRequest(channel *model.Channel, protocol Protocol, modelName, requestPath string) Compatibility {
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
			return converted(ProtocolGemini, "anthropic_messages_to_gemini_generate_content")
		case ProtocolResponses:
			return converted(ProtocolGemini, relayconvert.ConverterOpenAIResponsesToGemini)
		}
	case constant.ChannelTypeVertexAi:
		return vertexCompatibility(protocol, modelName)
	case constant.ChannelTypeAws:
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

func IsCompatible(channel *model.Channel, protocol Protocol, modelName, requestPath string) bool {
	return ForRequest(channel, protocol, modelName, requestPath).Status != StatusIncompatible
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

func advancedCustomCompatibility(channel *model.Channel, protocol Protocol, modelName, requestPath string) Compatibility {
	config := channel.GetOtherSettings().AdvancedCustom
	if config == nil {
		return incompatible()
	}
	if strings.TrimSpace(requestPath) == "" {
		requestPath = canonicalPath(protocol, modelName)
	}
	route, ok := config.MatchPathForModel(requestPath, modelName)
	if !ok {
		return incompatible()
	}
	converter := strings.TrimSpace(route.Converter)
	if converter == "" || converter == relayconvert.ConverterNone {
		return native(protocol)
	}
	switch converter {
	case relayconvert.ConverterClaudeMessagesToOpenAIChat:
		return converted(ProtocolChat, converter)
	case relayconvert.ConverterOpenAIChatToClaudeMessages:
		return converted(ProtocolMessages, converter)
	case relayconvert.ConverterOpenAIChatToOpenAIResponses:
		return converted(ProtocolResponses, converter)
	case relayconvert.ConverterOpenAIResponsesToOpenAIChat:
		return converted(ProtocolChat, converter)
	case relayconvert.ConverterOpenAIResponsesToGemini:
		return converted(ProtocolGemini, converter)
	case relayconvert.ConverterGeminiContentToOpenAIChat:
		return converted(ProtocolChat, converter)
	case relayconvert.ConverterOpenAIChatToGeminiContent:
		return converted(ProtocolGemini, converter)
	default:
		return incompatible()
	}
}

func canonicalPath(protocol Protocol, modelName string) string {
	switch protocol {
	case ProtocolChat:
		return "/v1/chat/completions"
	case ProtocolMessages:
		return "/v1/messages"
	case ProtocolResponses:
		return "/v1/responses"
	case ProtocolGemini:
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			modelName = "model"
		}
		return "/v1beta/models/" + modelName + ":generateContent"
	default:
		return ""
	}
}

func aliSupportsMessages(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if modelName == "" {
		return false
	}
	patterns := common.GetEnvOrDefaultString(
		"ALI_ANTHROPIC_MESSAGES_MODELS",
		"qwen,deepseek-v4,kimi,glm,minimax-m",
	)
	for _, pattern := range strings.Split(patterns, ",") {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern != "" && strings.Contains(modelName, pattern) {
			return true
		}
	}
	return false
}

func native(upstream Protocol) Compatibility {
	return Compatibility{Status: StatusNative, UpstreamProtocol: upstream}
}

func converted(upstream Protocol, converter string) Compatibility {
	return Compatibility{Status: StatusConvertible, UpstreamProtocol: upstream, Converter: converter}
}

func incompatible() Compatibility {
	return Compatibility{Status: StatusIncompatible}
}
