package channelcompat

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
)

type Protocol = relayconvert.Protocol

const (
	ProtocolChat      = relayconvert.ProtocolChat
	ProtocolMessages  = relayconvert.ProtocolMessages
	ProtocolResponses = relayconvert.ProtocolResponses
	ProtocolGemini    = relayconvert.ProtocolGemini
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

func Protocols() []Protocol {
	return relayconvert.Protocols()
}

func DetectRequestProtocol(requestPath string) Protocol {
	operation, ok := relayconvert.DetectOperation(requestPath)
	if ok && (operation.ID == relayconvert.OperationGenerate || operation.ID == relayconvert.OperationCompact || operation.ID == relayconvert.OperationCountTokens) {
		return operation.Protocol
	}
	return ""
}

// MainProtocolPathForAuxiliaryRequest returns the primary text endpoint whose
// configured route determines whether an auxiliary endpoint can be served
// locally when no dedicated upstream route exists.
func MainProtocolPathForAuxiliaryRequest(requestPath string) (string, bool) {
	requestPath = strings.Split(strings.TrimSpace(requestPath), "?")[0]
	if requestPath == "/v1/messages/count_tokens" {
		return "/v1/messages", true
	}
	return "", false
}

func Matrix(channel *model.Channel, modelName string) map[string]Compatibility {
	protocols := Protocols()
	result := make(map[string]Compatibility, len(protocols))
	for _, protocol := range protocols {
		result[string(protocol)] = ForRequest(channel, protocol, modelName, canonicalPath(protocol, modelName))
	}
	return result
}

func ForRequest(channel *model.Channel, protocol Protocol, modelName, requestPath string) Compatibility {
	plan := PlanForRequest(channel, protocol, modelName, requestPath, RequestFeatureSet{})
	return Compatibility{Status: plan.Status, UpstreamProtocol: plan.UpstreamProtocol, Converter: plan.RequestConverter}
}

func IsCompatible(channel *model.Channel, protocol Protocol, modelName, requestPath string) bool {
	return ForRequest(channel, protocol, modelName, requestPath).Status != StatusIncompatible
}

func advancedCustomCompatibility(channel *model.Channel, protocol Protocol, modelName, requestPath string) Compatibility {
	var settings hostdto.ChannelOtherSettings
	if common.UnmarshalJsonStr(channel.OtherSettings, &settings) != nil || settings.AdvancedCustom == nil {
		return incompatible()
	}
	if requestPath == "" {
		requestPath = canonicalPath(protocol, modelName)
	}
	route, ok := channel.MatchAdvancedCustomRoute(requestPath, modelName, settings.AdvancedCustom)
	if !ok {
		return incompatible()
	}
	target, err := route.ResolveTarget()
	if err != nil {
		return incompatible()
	}
	if target == protocol || target == "" {
		return native(protocol)
	}
	from, _ := protocol.RelayFormat()
	to, _ := target.RelayFormat()
	converter, ok := relayconvert.LookupTextConverterRoute(from, to)
	if !ok {
		return incompatible()
	}
	return converted(target, converter.ID)
}

func canonicalPath(protocol Protocol, modelName string) string {
	return relayconvert.CanonicalPath(protocol, modelName)
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
