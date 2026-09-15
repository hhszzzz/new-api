package channelcompat

import (
	"fmt"
	hostdto "github.com/QuantumNous/new-api/dto"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/modelmapping"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const (
	StateModeNone            = "none"
	StateModeNativeResponses = "native_responses"
	StateModeReplay          = "replay"
	StateModeStrictAppend    = "strict_append"
)

type RequestFeatureSet = relayconvert.RequestFeatureSet

func MergeRequestFeatureSets(featureSets ...RequestFeatureSet) RequestFeatureSet {
	return relayconvert.MergeRequestFeatureSets(featureSets...)
}

type ProtocolPlan struct {
	AdvancedCustomRoute    *hostdto.AdvancedCustomRoute      `json:"-"`
	Operation              relayconvert.Operation            `json:"operation"`
	Transport              relayconvert.Transport            `json:"transport"`
	Conversion             string                            `json:"conversion"`
	RequestMode            string                            `json:"request_mode"`
	StateScope             string                            `json:"state_scope"`
	StateTTLSeconds        int                               `json:"state_ttl_seconds,omitempty"`
	MaxStateTurns          int                               `json:"max_state_turns,omitempty"`
	MaxStateBytes          int                               `json:"max_state_bytes,omitempty"`
	RequestPath            []types.RelayFormat               `json:"request_path,omitempty"`
	ResponsePath           []types.RelayFormat               `json:"response_path,omitempty"`
	RequestProtocol        Protocol                          `json:"request_protocol"`
	UpstreamProtocol       Protocol                          `json:"upstream_protocol,omitempty"`
	RequestConverter       string                            `json:"request_converter,omitempty"`
	ResponseConverter      string                            `json:"response_converter,omitempty"`
	EffectiveUpstreamModel string                            `json:"effective_upstream_model,omitempty"`
	Quality                relayconvert.TextConverterQuality `json:"quality,omitempty"`
	Status                 Status                            `json:"status"`
	SelectionMode          string                            `json:"selection_mode,omitempty"`
	ExplicitCapabilities   bool                              `json:"explicit_capabilities,omitempty"`
	StateMode              string                            `json:"state_mode,omitempty"`
	StateEnabled           bool                              `json:"state_enabled,omitempty"`
	// LossyContentTypes lists request content types and request fields the
	// conversion will drop because the channel opted into lossy conversion.
	LossyContentTypes []string          `json:"lossy_content_types,omitempty"`
	Features          RequestFeatureSet `json:"features"`
	Reason            string            `json:"reason,omitempty"`
}

func PlanForRequest(channel *model.Channel, protocol Protocol, modelName, requestPath string, features RequestFeatureSet) ProtocolPlan {
	plans := PlansForRequest(channel, protocol, modelName, requestPath, features)
	if len(plans) > 0 {
		return plans[0]
	}
	return ProtocolPlan{
		RequestProtocol: protocol,
		Status:          StatusIncompatible,
		SelectionMode:   hostdto.ProtocolSelectionModeStrict,
		StateMode:       StateModeNone,
		Features:        features,
		Reason:          "no protocol plan is available",
	}
}

func PlansForRequest(channel *model.Channel, protocol Protocol, modelName, requestPath string, features RequestFeatureSet) []ProtocolPlan {
	base := ProtocolPlan{RequestProtocol: protocol, Status: StatusIncompatible, SelectionMode: hostdto.ProtocolSelectionModeStrict, StateMode: StateModeNone, Features: features}
	reject := func(err error) []ProtocolPlan { base.Reason = err.Error(); return []ProtocolPlan{base} }
	if channel == nil || protocol == "" {
		return reject(fmt.Errorf("missing channel or request protocol"))
	}
	if strings.HasPrefix(requestPath, "/v1/responses/compact") {
		modelName = strings.TrimSuffix(modelName, ratio_setting.CompactModelSuffix)
	}
	resolved, err := modelmapping.Resolve(channel.GetModelMapping(), modelName)
	if err != nil {
		return reject(err)
	}
	base.EffectiveUpstreamModel = resolved.Model
	var settings hostdto.ChannelOtherSettings
	if channel.OtherSettings != "" {
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
			return reject(fmt.Errorf("invalid channel protocol settings"))
		}
	}
	global := model_setting.GetGlobalSettings()
	var policy hostdto.ProtocolPolicy
	var candidates []Protocol
	var forced Protocol
	if global.ProtocolPolicy == nil && settings.ProtocolPolicy == nil {
		policy, candidates, err = LegacyPolicyForRequest(channel, protocol, resolved.Model, requestPath, settings, *global)
		if err != nil {
			return reject(err)
		}
		base.ExplicitCapabilities = settings.ProtocolCapabilities != nil
	} else {
		if err := settings.ProtocolPolicy.Validate(); err != nil {
			return reject(err)
		}
		var denied bool
		policy, forced, denied = hostdto.ResolveProtocolPolicy(global.EffectiveProtocolPolicy(), settings.ProtocolPolicy, resolved.Model, protocol, channel.Id, channel.Type)
		if denied {
			return reject(fmt.Errorf("protocol policy denies %s for this channel and model", protocol))
		}
		base.ExplicitCapabilities = settings.ProtocolPolicy != nil && len(policy.UpstreamProtocols) > 0
		configured := policy.UpstreamProtocols
		if len(configured) == 0 {
			if policy.Selection == hostdto.ProtocolSelectionAutomatic {
				for _, p := range automaticProbeProtocols(channel, resolved.Model) {
					configured = append(configured, string(p))
				}
			} else {
				configured = defaultUpstreamProtocols(channel, resolved.Model)
			}
		}
		for _, p := range configured {
			candidates = append(candidates, Protocol(p))
		}
		if forced != "" {
			candidates = []Protocol{forced}
		}
	}
	base.Conversion = policy.Conversion
	base.RequestMode = policy.RequestMode
	base.StateScope = policy.StateScope
	base.StateTTLSeconds = policy.StateTTLSeconds
	base.MaxStateTurns = policy.MaxStateTurns
	base.MaxStateBytes = policy.MaxStateBytes
	if policy.Selection == hostdto.ProtocolSelectionAutomatic {
		base.SelectionMode = hostdto.ProtocolSelectionModeAuto
	}
	operation, known := relayconvert.DetectOperation(requestPath)
	if !known {
		if requestPath != "" {
			return reject(fmt.Errorf("unknown protocol operation"))
		}
		operation, _ = relayconvert.DetectOperation(relayconvert.CanonicalPath(protocol, modelName))
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if settings.AdvancedCustom == nil {
			return reject(fmt.Errorf("advanced custom configuration is missing"))
		}
		route, ok := channel.MatchAdvancedCustomRoute(requestPath, modelName, settings.AdvancedCustom)
		if !ok {
			return reject(fmt.Errorf("advanced custom route does not support this protocol and model"))
		}
		target, err := route.ResolveTarget()
		if err != nil {
			return reject(err)
		}
		if target == "" {
			target = protocol
		}
		candidates = []Protocol{target}
		base.AdvancedCustomRoute = &route
		base.SelectionMode = hostdto.ProtocolSelectionModeStrict
	}
	transport := relayconvert.TransportHTTP
	if features.Stream {
		transport = relayconvert.TransportSSE
	}
	plannedOperation := operation.ID
	if plannedOperation == relayconvert.OperationCountTokens {
		plannedOperation = relayconvert.OperationGenerate
	}
	routes, err := relayconvert.PlanConversions(protocol, plannedOperation, transport, candidates, features, policy.Conversion)
	if err != nil {
		return reject(err)
	}
	plans := make([]ProtocolPlan, 0, len(routes))
	for _, route := range routes {
		plan := base
		plan.Operation = operation.ID
		plan.Transport = transport
		plan.UpstreamProtocol = route.UpstreamProtocol
		plan.RequestConverter = route.RequestConverter
		plan.ResponseConverter = route.ResponseConverter
		plan.RequestPath = route.RequestPath
		plan.ResponsePath = route.ResponsePath
		plan.LossyContentTypes = route.Losses
		plan.Status = StatusNative
		if route.UpstreamProtocol != protocol {
			plan.Status = StatusConvertible
			if spec, ok := relayconvert.LookupTextConverter(route.RequestConverter); ok {
				plan.Quality = spec.Quality
			}
		}
		plan.StateMode = stateModeFor(protocol, route.UpstreamProtocol)
		plan.StateEnabled = policy.StateScope == hostdto.ProtocolStateAll || policy.StateScope == hostdto.ProtocolStateBridge && plan.Status == StatusConvertible
		plans = append(plans, plan)
	}
	return plans
}

func automaticProbeProtocols(channel *model.Channel, modelName string) []Protocol {
	if channel == nil {
		return nil
	}
	if protocol, ok := protocolFromConfiguredEndpoint(channel.GetBaseURL()); ok {
		return []Protocol{protocol}
	}
	apiType, _ := common.ChannelType2APIType(channel.Type)
	switch apiType {
	case constant.APITypeOpenAI,
		constant.APITypeOpenRouter,
		constant.APITypeXinference,
		constant.APITypeAnthropic,
		constant.APITypeGemini,
		constant.APITypeNewAPI,
		constant.APITypeSub2API:
		return []Protocol{ProtocolChat, ProtocolMessages, ProtocolResponses, ProtocolGemini}
	}

	configured := defaultUpstreamProtocols(channel, modelName)
	protocols := make([]Protocol, 0, len(configured))
	seen := make(map[Protocol]struct{}, len(configured))
	for _, value := range configured {
		protocol := Protocol(strings.TrimSpace(value))
		if _, ok := protocolRelayFormat(protocol); !ok {
			continue
		}
		if _, exists := seen[protocol]; exists {
			continue
		}
		seen[protocol] = struct{}{}
		protocols = append(protocols, protocol)
	}
	return protocols
}

func protocolFromConfiguredEndpoint(baseURL string) (Protocol, bool) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", false
	}
	path := strings.ToLower(strings.TrimRight(parsed.Path, "/"))
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return ProtocolChat, true
	case strings.HasSuffix(path, "/responses"):
		return ProtocolResponses, true
	case strings.HasSuffix(path, "/messages"):
		return ProtocolMessages, true
	case strings.HasSuffix(path, ":generatecontent"), strings.HasSuffix(path, ":streamgeneratecontent"):
		return ProtocolGemini, true
	default:
		return "", false
	}
}

func automaticProtocolOrder(protocol Protocol) []Protocol {
	result := []Protocol{protocol}
	for _, candidate := range relayconvert.Protocols() {
		if candidate != protocol {
			result = append(result, candidate)
		}
	}
	return result
}

func ExtractRequestFeatureSet(protocol Protocol, body []byte) (RequestFeatureSet, error) {
	return relayconvert.ExtractRequestFeatureSet(protocol, body)
}

func conversionFeatureIncompatibility(protocol, upstream Protocol, features RequestFeatureSet, allowLossy bool) (string, []string) {
	return relayconvert.AnalyzeConversionFeatures(protocol, upstream, features, allowLossy)
}

func defaultUpstreamProtocols(channel *model.Channel, modelName string) []string {
	if channel == nil {
		return nil
	}
	switch channel.Type {
	case constant.ChannelTypeCodex:
		return []string{hostdto.ProtocolCapabilityResponses}
	case constant.ChannelTypeAnthropic:
		return []string{hostdto.ProtocolCapabilityMessages}
	case constant.ChannelTypeAws:
		if strings.Contains(strings.ToLower(strings.TrimSpace(modelName)), "nova-") {
			return []string{hostdto.ProtocolCapabilityChat}
		}
		return []string{hostdto.ProtocolCapabilityMessages}
	case constant.ChannelTypeAzure:
		return []string{hostdto.ProtocolCapabilityChat, hostdto.ProtocolCapabilityResponses}
	case constant.ChannelTypeGemini:
		return []string{hostdto.ProtocolCapabilityGemini}
	case constant.ChannelTypeVertexAi:
		modelName = strings.ToLower(strings.TrimSpace(modelName))
		switch {
		case strings.HasPrefix(modelName, "claude"):
			return []string{hostdto.ProtocolCapabilityMessages}
		case strings.Contains(modelName, "llama"), strings.Contains(modelName, "-maas"):
			return []string{hostdto.ProtocolCapabilityChat}
		default:
			return []string{hostdto.ProtocolCapabilityGemini}
		}
	case constant.ChannelTypeAli:
		protocols := []string{hostdto.ProtocolCapabilityChat, hostdto.ProtocolCapabilityResponses}
		if aliSupportsMessages(modelName) {
			protocols = append(protocols, hostdto.ProtocolCapabilityMessages)
		}
		return protocols
	case constant.ChannelTypeVolcEngine:
		protocols := []string{hostdto.ProtocolCapabilityChat, hostdto.ProtocolCapabilityResponses}
		if _, ok := constant.ChannelSpecialBases[channel.GetBaseURL()]; ok {
			protocols = append(protocols, hostdto.ProtocolCapabilityMessages)
		}
		return protocols
	case constant.ChannelTypeDeepSeek, constant.ChannelTypeMoonshot, constant.ChannelTypeMiniMax, constant.ChannelTypeZhipu_v4:
		return []string{hostdto.ProtocolCapabilityChat, hostdto.ProtocolCapabilityMessages}
	case constant.ChannelTypeXai:
		return []string{hostdto.ProtocolCapabilityChat, hostdto.ProtocolCapabilityResponses}
	case constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI:
		return []string{
			hostdto.ProtocolCapabilityChat,
			hostdto.ProtocolCapabilityMessages,
			hostdto.ProtocolCapabilityResponses,
			hostdto.ProtocolCapabilityGemini,
		}
	case constant.ChannelTypeOpenAI:
		baseURL := strings.TrimSpace(channel.GetBaseURL())
		if baseURL == "" && channel.Type >= 0 && channel.Type < len(constant.ChannelBaseURLs) {
			baseURL = constant.ChannelBaseURLs[channel.Type]
		}
		if isOfficialOpenAIBaseURL(baseURL) {
			return []string{hostdto.ProtocolCapabilityChat, hostdto.ProtocolCapabilityResponses}
		}
		return []string{hostdto.ProtocolCapabilityChat}
	default:
		return []string{hostdto.ProtocolCapabilityChat}
	}
}

func isOfficialOpenAIBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "api.openai.com")
}

func protocolRelayFormat(protocol Protocol) (types.RelayFormat, bool) {
	switch protocol {
	case ProtocolChat:
		return types.RelayFormatOpenAI, true
	case ProtocolMessages:
		return types.RelayFormatClaude, true
	case ProtocolResponses:
		return types.RelayFormatOpenAIResponses, true
	case ProtocolGemini:
		return types.RelayFormatGemini, true
	default:
		return "", false
	}
}

func stateModeFor(request, upstream Protocol) string {
	if request == ProtocolResponses && upstream == ProtocolResponses {
		return StateModeNativeResponses
	}
	if request == ProtocolMessages && upstream == ProtocolResponses {
		return StateModeStrictAppend
	}
	if request != upstream && (request == ProtocolResponses || upstream == ProtocolResponses) {
		return StateModeReplay
	}
	return StateModeNone
}
