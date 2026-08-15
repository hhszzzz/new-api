package channelcompat

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
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

type RequestFeatureSet struct {
	Stream                bool     `json:"stream"`
	HasPreviousResponse   bool     `json:"has_previous_response"`
	HasConversation       bool     `json:"has_conversation"`
	HasPrompt             bool     `json:"has_prompt"`
	HasContextManagement  bool     `json:"has_context_management"`
	HasThinking           bool     `json:"has_thinking"`
	HasCustomTools        bool     `json:"has_custom_tools"`
	HasNamespaceTools     bool     `json:"has_namespace_tools"`
	HasToolSearch         bool     `json:"has_tool_search"`
	HasAdditionalTools    bool     `json:"has_additional_tools"`
	HasStopSequences      bool     `json:"has_stop_sequences"`
	HasTopK               bool     `json:"has_top_k"`
	ContentTypes          []string `json:"content_types,omitempty"`
	DeclaredHostedTools   []string `json:"declared_hosted_tools,omitempty"`
	HistoricalHostedTools []string `json:"historical_hosted_tools,omitempty"`
	MessagesNativeFields  []string `json:"messages_native_fields,omitempty"`
}

func MergeRequestFeatureSets(featureSets ...RequestFeatureSet) RequestFeatureSet {
	merged := RequestFeatureSet{}
	contentTypes := make(map[string]struct{})
	declaredHostedTools := make(map[string]struct{})
	historicalHostedTools := make(map[string]struct{})
	messagesNativeFields := make(map[string]struct{})
	appendUnique := func(target *[]string, seen map[string]struct{}, values []string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			*target = append(*target, value)
		}
	}
	for _, features := range featureSets {
		merged.Stream = merged.Stream || features.Stream
		merged.HasPreviousResponse = merged.HasPreviousResponse || features.HasPreviousResponse
		merged.HasConversation = merged.HasConversation || features.HasConversation
		merged.HasPrompt = merged.HasPrompt || features.HasPrompt
		merged.HasContextManagement = merged.HasContextManagement || features.HasContextManagement
		merged.HasThinking = merged.HasThinking || features.HasThinking
		merged.HasCustomTools = merged.HasCustomTools || features.HasCustomTools
		merged.HasNamespaceTools = merged.HasNamespaceTools || features.HasNamespaceTools
		merged.HasToolSearch = merged.HasToolSearch || features.HasToolSearch
		merged.HasAdditionalTools = merged.HasAdditionalTools || features.HasAdditionalTools
		merged.HasStopSequences = merged.HasStopSequences || features.HasStopSequences
		merged.HasTopK = merged.HasTopK || features.HasTopK
		appendUnique(&merged.ContentTypes, contentTypes, features.ContentTypes)
		appendUnique(&merged.DeclaredHostedTools, declaredHostedTools, features.DeclaredHostedTools)
		appendUnique(&merged.HistoricalHostedTools, historicalHostedTools, features.HistoricalHostedTools)
		appendUnique(&merged.MessagesNativeFields, messagesNativeFields, features.MessagesNativeFields)
	}
	return merged
}

type ProtocolPlan struct {
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
		SelectionMode:   dto.ProtocolSelectionModeStrict,
		StateMode:       StateModeNone,
		Features:        features,
		Reason:          "no protocol plan is available",
	}
}

func PlansForRequest(channel *model.Channel, protocol Protocol, modelName, requestPath string, features RequestFeatureSet) []ProtocolPlan {
	if strings.HasPrefix(requestPath, "/v1/responses/compact") {
		modelName = strings.TrimSuffix(modelName, ratio_setting.CompactModelSuffix)
	}
	if channel != nil &&
		channel.Type != constant.ChannelTypeAdvancedCustom &&
		(protocol == ProtocolResponses || protocol == ProtocolMessages) &&
		model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled {
		capabilities := channel.GetOtherSettings().ProtocolCapabilities
		if capabilities != nil && capabilities.GetSelectionMode() == dto.ProtocolSelectionModeAuto {
			return autoPlansForRequest(channel, protocol, modelName, features, capabilities)
		}
	}
	return []ProtocolPlan{planForStrictRequest(channel, protocol, modelName, requestPath, features)}
}

func planForStrictRequest(channel *model.Channel, protocol Protocol, modelName, requestPath string, features RequestFeatureSet) ProtocolPlan {
	plan := ProtocolPlan{
		RequestProtocol:        protocol,
		EffectiveUpstreamModel: strings.TrimSpace(modelName),
		Status:                 StatusIncompatible,
		SelectionMode:          dto.ProtocolSelectionModeStrict,
		StateMode:              StateModeNone,
		Features:               features,
	}
	if channel == nil || protocol == "" {
		plan.Reason = "missing channel or request protocol"
		return plan
	}

	policy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	if protocol != ProtocolResponses && protocol != ProtocolMessages {
		legacy := ForRequest(channel, protocol, modelName, requestPath)
		plan.Status = legacy.Status
		plan.UpstreamProtocol = legacy.UpstreamProtocol
		plan.RequestConverter = legacy.Converter
		plan.ResponseConverter = legacy.Converter
		plan.StateMode = stateModeFor(protocol, legacy.UpstreamProtocol)
		if legacy.Status == StatusIncompatible {
			plan.Reason = fmt.Sprintf("channel does not support %s requests", protocol)
		}
		return plan
	}

	capabilities := channel.GetOtherSettings().ProtocolCapabilities
	plan.ExplicitCapabilities = capabilities != nil
	// The global bridge switch is a hard gate: with it off, every channel serves
	// the request protocol through the legacy adaptor layer even when it has
	// explicit protocol capabilities configured.
	if !policy.Enabled || (capabilities == nil && !policy.DefaultAllowConversion) {
		legacy := ForRequest(channel, protocol, modelName, requestPath)
		plan.Status = legacy.Status
		plan.UpstreamProtocol = legacy.UpstreamProtocol
		plan.RequestConverter = legacy.Converter
		plan.ResponseConverter = legacy.Converter
		plan.StateMode = stateModeFor(protocol, legacy.UpstreamProtocol)
		if legacy.Status == StatusIncompatible {
			plan.Reason = fmt.Sprintf("channel does not support %s requests", protocol)
		}
		return plan
	}

	resolved, err := modelmapping.Resolve(channel.GetModelMapping(), modelName)
	if err != nil {
		plan.Reason = err.Error()
		return plan
	}
	plan.EffectiveUpstreamModel = resolved.Model

	upstreamProtocols, allowConversionOverride := capabilities.Resolve(resolved.Model)
	allowConversion := policy.DefaultAllowConversion
	if allowConversionOverride != nil {
		allowConversion = *allowConversionOverride
	} else if capabilities != nil {
		// Declaring the authoritative upstream protocol is itself an explicit
		// request to bridge incompatible client protocols. Administrators can
		// still opt out with allow_conversion=false.
		allowConversion = true
	}

	if channel.Type == constant.ChannelTypeAdvancedCustom {
		compatibility := advancedCustomCompatibility(channel, protocol, resolved.Model, requestPath)
		if compatibility.Status == StatusIncompatible {
			if mainPath, ok := MainProtocolPathForAuxiliaryRequest(requestPath); ok {
				compatibility = advancedCustomCompatibility(channel, protocol, resolved.Model, mainPath)
			}
		}
		if compatibility.Status == StatusIncompatible {
			plan.Reason = "advanced custom route does not support this protocol and model"
			return plan
		}
		if compatibility.Status == StatusNative {
			plan.Status = StatusNative
			plan.UpstreamProtocol = compatibility.UpstreamProtocol
			plan.StateMode = stateModeFor(protocol, compatibility.UpstreamProtocol)
			return plan
		}
		if !allowConversion {
			plan.Reason = "protocol conversion is disabled for this channel and model"
			return plan
		}
		reason, lossyContentTypes := conversionFeatureIncompatibility(protocol, compatibility.UpstreamProtocol, features, capabilities.LossyConversionAllowed())
		if reason != "" {
			plan.Reason = reason
			return plan
		}
		converter, ok := relayconvert.LookupTextConverter(compatibility.Converter)
		if !ok {
			plan.Reason = fmt.Sprintf("advanced custom converter %q is not registered", compatibility.Converter)
			return plan
		}
		plan.Status = StatusConvertible
		plan.UpstreamProtocol = compatibility.UpstreamProtocol
		plan.RequestConverter = converter.ID
		plan.ResponseConverter = converter.ID
		plan.Quality = converter.Quality
		plan.StateMode = stateModeFor(protocol, compatibility.UpstreamProtocol)
		plan.StateEnabled = true
		plan.LossyContentTypes = lossyContentTypes
		return plan
	}

	if len(upstreamProtocols) == 0 {
		upstreamProtocols = defaultUpstreamProtocols(channel, resolved.Model)
	}

	for _, configured := range upstreamProtocols {
		upstream := Protocol(strings.TrimSpace(configured))
		if upstream != protocol {
			continue
		}
		plan.Status = StatusNative
		plan.UpstreamProtocol = upstream
		plan.StateMode = stateModeFor(protocol, upstream)
		return plan
	}

	if !allowConversion {
		plan.Reason = "protocol conversion is disabled for this channel and model"
		return plan
	}

	fromFormat, ok := protocolRelayFormat(protocol)
	if !ok {
		plan.Reason = fmt.Sprintf("request protocol %s cannot be converted", protocol)
		return plan
	}
	featureReason := ""
	for _, configured := range upstreamProtocols {
		upstream := Protocol(strings.TrimSpace(configured))
		toFormat, ok := protocolRelayFormat(upstream)
		if !ok || upstream == protocol {
			continue
		}
		reason, lossyContentTypes := conversionFeatureIncompatibility(protocol, upstream, features, capabilities.LossyConversionAllowed())
		if reason != "" {
			if featureReason == "" {
				featureReason = reason
			}
			continue
		}
		converter, ok := relayconvert.LookupTextConverterRoute(fromFormat, toFormat)
		if !ok {
			continue
		}
		plan.Status = StatusConvertible
		plan.UpstreamProtocol = upstream
		plan.RequestConverter = converter.ID
		plan.ResponseConverter = converter.ID
		plan.Quality = converter.Quality
		plan.StateMode = stateModeFor(protocol, upstream)
		plan.StateEnabled = true
		plan.LossyContentTypes = lossyContentTypes
		return plan
	}

	if featureReason != "" {
		plan.Reason = featureReason
		return plan
	}
	plan.Reason = fmt.Sprintf("no conversion route from %s to declared upstream protocols", protocol)
	return plan
}

func autoPlansForRequest(channel *model.Channel, protocol Protocol, modelName string, features RequestFeatureSet, capabilities *dto.ProtocolCapabilities) []ProtocolPlan {
	basePlan := ProtocolPlan{
		RequestProtocol:        protocol,
		EffectiveUpstreamModel: strings.TrimSpace(modelName),
		Status:                 StatusIncompatible,
		SelectionMode:          dto.ProtocolSelectionModeAuto,
		ExplicitCapabilities:   true,
		StateMode:              StateModeNone,
		Features:               features,
	}
	if channel == nil || capabilities == nil {
		basePlan.Reason = "missing channel or automatic protocol capabilities"
		return []ProtocolPlan{basePlan}
	}

	resolved, err := modelmapping.Resolve(channel.GetModelMapping(), modelName)
	if err != nil {
		basePlan.Reason = err.Error()
		return []ProtocolPlan{basePlan}
	}
	basePlan.EffectiveUpstreamModel = resolved.Model

	configuredProtocols, allowConversionOverride := capabilities.Resolve(resolved.Model)
	allowedProtocols := make(map[Protocol]struct{}, len(configuredProtocols))
	if len(configuredProtocols) == 0 {
		for _, candidate := range automaticProbeProtocols(channel, resolved.Model) {
			allowedProtocols[candidate] = struct{}{}
		}
	} else {
		for _, configured := range configuredProtocols {
			allowedProtocols[Protocol(strings.TrimSpace(configured))] = struct{}{}
		}
	}

	policy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	allowConversion := policy.DefaultAllowConversion
	if allowConversionOverride != nil {
		allowConversion = *allowConversionOverride
	} else {
		// Automatic selection is an explicit request to try compatible wire
		// protocols. Administrators can still restrict it to the entry protocol
		// with allow_conversion=false.
		allowConversion = true
	}

	fromFormat, ok := protocolRelayFormat(protocol)
	if !ok {
		basePlan.Reason = fmt.Sprintf("request protocol %s cannot be converted", protocol)
		return []ProtocolPlan{basePlan}
	}

	plans := make([]ProtocolPlan, 0, len(allowedProtocols))
	firstFeatureReason := ""
	for _, upstream := range automaticProtocolOrder(protocol) {
		if _, allowed := allowedProtocols[upstream]; !allowed {
			continue
		}

		plan := basePlan
		plan.UpstreamProtocol = upstream
		plan.StateMode = stateModeFor(protocol, upstream)
		if upstream == protocol {
			plan.Status = StatusNative
			plans = append(plans, plan)
			continue
		}
		if !allowConversion {
			continue
		}
		reason, lossyContentTypes := conversionFeatureIncompatibility(protocol, upstream, features, capabilities.LossyConversionAllowed())
		if reason != "" {
			if firstFeatureReason == "" {
				firstFeatureReason = reason
			}
			continue
		}
		toFormat, ok := protocolRelayFormat(upstream)
		if !ok {
			continue
		}
		converter, ok := relayconvert.LookupTextConverterRoute(fromFormat, toFormat)
		if !ok {
			continue
		}
		plan.Status = StatusConvertible
		plan.StateEnabled = true
		plan.RequestConverter = converter.ID
		plan.ResponseConverter = converter.ID
		plan.Quality = converter.Quality
		plan.LossyContentTypes = lossyContentTypes
		plans = append(plans, plan)
	}
	if len(plans) > 0 {
		return plans
	}
	if firstFeatureReason != "" {
		basePlan.Reason = firstFeatureReason
	} else if !allowConversion {
		basePlan.Reason = "protocol conversion is disabled for this channel and model"
	} else {
		basePlan.Reason = fmt.Sprintf("no automatic protocol route is available for %s", protocol)
	}
	return []ProtocolPlan{basePlan}
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
	switch protocol {
	case ProtocolResponses:
		return []Protocol{ProtocolResponses, ProtocolChat, ProtocolMessages, ProtocolGemini}
	case ProtocolMessages:
		return []Protocol{ProtocolMessages, ProtocolChat, ProtocolResponses, ProtocolGemini}
	default:
		return []Protocol{protocol}
	}
}

func ExtractRequestFeatureSet(protocol Protocol, body []byte) (RequestFeatureSet, error) {
	features := RequestFeatureSet{}
	if len(body) == 0 {
		return features, nil
	}
	var request map[string]any
	if err := common.Unmarshal(body, &request); err != nil {
		return features, err
	}
	features.Stream, _ = request["stream"].(bool)
	features.HasPreviousResponse = nonEmptyString(request["previous_response_id"])
	features.HasConversation = nonNullValue(request, "conversation")
	features.HasPrompt = nonNullValue(request, "prompt")
	features.HasContextManagement = nonNullValue(request, "context_management")
	features.HasThinking = nonNullValue(request, "thinking") || nonNullValue(request, "reasoning")
	if protocol == ProtocolMessages {
		if stopSequences, ok := request["stop_sequences"].([]any); ok {
			features.HasStopSequences = len(stopSequences) > 0
		}
		features.HasTopK = nonNullValue(request, "top_k")
		inspectMessagesNativeFields(request, &features)
	}
	declaredHostedSeen := make(map[string]struct{})
	historicalHostedSeen := make(map[string]struct{})
	contentSeen := make(map[string]struct{})
	inspectRequestContentTypes(protocol, request, &features, contentSeen, historicalHostedSeen)
	features.HasAdditionalTools = inspectRequestInput(
		protocol,
		request["input"],
		&features,
		declaredHostedSeen,
		historicalHostedSeen,
	)

	toolValue, exists := request["tools"]
	if !exists {
		return features, nil
	}
	tools, ok := toolValue.([]any)
	if !ok {
		return features, nil
	}
	inspectTools(protocol, tools, &features, declaredHostedSeen)
	return features, nil
}

func inspectRequestContentTypes(protocol Protocol, request map[string]any, features *RequestFeatureSet, contentSeen, historicalHostedSeen map[string]struct{}) {
	switch protocol {
	case ProtocolResponses:
		items, ok := request["input"].([]any)
		if !ok {
			return
		}
		for _, rawItem := range items {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			inspectContentBlocks(protocol, item["content"], features, contentSeen, historicalHostedSeen)
		}
	case ProtocolMessages:
		inspectContentBlocks(protocol, request["system"], features, contentSeen, historicalHostedSeen)
		messages, ok := request["messages"].([]any)
		if !ok {
			return
		}
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				continue
			}
			inspectContentBlocks(protocol, message["content"], features, contentSeen, historicalHostedSeen)
		}
	}
}

func inspectContentBlocks(protocol Protocol, value any, features *RequestFeatureSet, contentSeen, historicalHostedSeen map[string]struct{}) {
	blocks, ok := value.([]any)
	if !ok {
		if block, blockOK := value.(map[string]any); blockOK {
			blocks = []any{block}
		} else {
			return
		}
	}
	for _, rawBlock := range blocks {
		block, ok := rawBlock.(map[string]any)
		if !ok {
			continue
		}
		contentType := strings.TrimSpace(common.Interface2String(block["type"]))
		if contentType != "" {
			if _, exists := contentSeen[contentType]; !exists {
				contentSeen[contentType] = struct{}{}
				features.ContentTypes = append(features.ContentTypes, contentType)
			}
			if contentType == "thinking" || contentType == "redacted_thinking" {
				features.HasThinking = true
			}
			if protocol == ProtocolMessages && (contentType == "server_tool_use" || isMessagesServerTool(contentType)) {
				addUniqueToolType(contentType, &features.HistoricalHostedTools, historicalHostedSeen)
			}
		}
		if nested, exists := block["content"]; exists {
			inspectContentBlocks(protocol, nested, features, contentSeen, historicalHostedSeen)
		}
	}
}

func inspectTools(protocol Protocol, tools []any, features *RequestFeatureSet, declaredHostedSeen map[string]struct{}) {
	for _, item := range tools {
		if name, ok := item.(string); ok {
			if strings.TrimSpace(name) != "" {
				features.HasCustomTools = true
			}
			continue
		}
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		toolType, _ := tool["type"].(string)
		toolType = strings.TrimSpace(toolType)
		switch toolType {
		case "", "function":
		case "custom", "freeform":
			features.HasCustomTools = true
		case "namespace":
			features.HasNamespaceTools = true
			nestedValue, exists := tool["tools"]
			if !exists {
				nestedValue = tool["children"]
			}
			if nested, ok := nestedValue.([]any); ok {
				inspectTools(protocol, nested, features, declaredHostedSeen)
			}
		case "tool_search":
			features.HasToolSearch = true
			execution := strings.TrimSpace(common.Interface2String(tool["execution"]))
			if protocol == ProtocolResponses && execution != "" && execution != "client" {
				addUniqueToolType("tool_search", &features.DeclaredHostedTools, declaredHostedSeen)
			}
		default:
			if strings.Contains(toolType, "tool_search") {
				features.HasToolSearch = true
			}
			// Responses local_shell is client-executed and lowered to a
			// function tool; Messages typed tools are dropped or lowered by
			// the converter, so declarations never force a native upstream.
			if protocol == ProtocolResponses && toolType != "local_shell" {
				addUniqueToolType(toolType, &features.DeclaredHostedTools, declaredHostedSeen)
			}
		}
	}
}

func isMessagesServerTool(toolType string) bool {
	for _, marker := range []string{
		"web_search",
		"web_fetch",
		"computer",
		"code_execution",
		"bash",
		"text_editor",
		"memory",
		"tool_search",
	} {
		if strings.Contains(toolType, marker) {
			return true
		}
	}
	return false
}

func inspectRequestInput(
	protocol Protocol,
	value any,
	features *RequestFeatureSet,
	declaredHostedSeen map[string]struct{},
	historicalHostedSeen map[string]struct{},
) bool {
	found := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if inspectRequestInput(protocol, item, features, declaredHostedSeen, historicalHostedSeen) {
				found = true
			}
		}
	case map[string]any:
		typedType := strings.TrimSpace(common.Interface2String(typed["type"]))
		if typedType == "reasoning" {
			features.HasThinking = true
		}
		if protocol == ProtocolResponses && isResponsesHostedHistoryItem(typedType) {
			addUniqueToolType(typedType, &features.HistoricalHostedTools, historicalHostedSeen)
		}
		if typedType == "additional_tools" || typedType == "tool_search_output" {
			found = found || typedType == "additional_tools"
			if tools, ok := typed["tools"].([]any); ok {
				inspectTools(protocol, tools, features, declaredHostedSeen)
			}
		}
		for _, nested := range typed {
			if inspectRequestInput(protocol, nested, features, declaredHostedSeen, historicalHostedSeen) {
				found = true
			}
		}
	}
	return found
}

func isResponsesHostedHistoryItem(itemType string) bool {
	switch itemType {
	case "web_search_call",
		"file_search_call",
		"computer_call",
		"computer_call_output",
		"code_interpreter_call",
		"image_generation_call",
		"shell_call",
		"shell_call_output",
		"apply_patch_call",
		"apply_patch_call_output",
		"mcp_list_tools",
		"mcp_approval_request",
		"mcp_approval_response",
		"mcp_call",
		"program",
		"program_output":
		return true
	default:
		return false
	}
}

func addUniqueToolType(toolType string, target *[]string, seen map[string]struct{}) {
	if _, exists := seen[toolType]; exists {
		return
	}
	seen[toolType] = struct{}{}
	*target = append(*target, toolType)
}

func nonNullValue(values map[string]any, key string) bool {
	value, exists := values[key]
	return exists && value != nil
}

func nonEmptyString(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

func inspectMessagesNativeFields(request map[string]any, features *RequestFeatureSet) {
	if features == nil {
		return
	}
	for _, field := range []string{"output_format", "container", "mcp_servers", "inference_geo", "speed", "service_tier"} {
		if meaningfulRequestValue(request[field]) {
			features.MessagesNativeFields = append(features.MessagesNativeFields, field)
		}
	}
	if outputConfig, exists := request["output_config"]; exists && messagesOutputConfigHasUnsupportedFields(outputConfig) {
		features.MessagesNativeFields = append(features.MessagesNativeFields, "output_config")
	}
}

func meaningfulRequestValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func messagesOutputConfigHasUnsupportedFields(value any) bool {
	config, ok := value.(map[string]any)
	if !ok {
		return value != nil
	}
	for field, fieldValue := range config {
		if field != "effort" && fieldValue != nil {
			return true
		}
	}
	return false
}

func conversionFeatureIncompatibility(protocol, upstream Protocol, features RequestFeatureSet, allowLossy bool) (string, []string) {
	if protocol == ProtocolResponses {
		switch {
		case features.HasConversation:
			return "conversation is only supported by a native Responses upstream", nil
		case features.HasPrompt:
			return "hosted prompt is only supported by a native Responses upstream", nil
		case features.HasContextManagement:
			return "context_management is only supported by a native Responses upstream", nil
		}
		// Declared hosted tools (web_search, local_shell, ...) are dropped by
		// the request converters, matching CC Switch: a Codex client always
		// declares them, so rejecting the request would make every non-native
		// upstream unusable. Historical hosted calls are different — dropping
		// them would corrupt conversation context, so those still require a
		// native Responses upstream even when lossy conversion is allowed.
		if len(features.HistoricalHostedTools) > 0 {
			return fmt.Sprintf(
				"Responses server tool history cannot be replayed to %s: %s",
				upstream,
				strings.Join(features.HistoricalHostedTools, ", "),
			), nil
		}
	}
	var lossyFields []string
	if protocol == ProtocolMessages {
		if features.HasContextManagement {
			// context_management is a best-effort server-side trimming directive:
			// dropping it only means the upstream sees the untrimmed history, so a
			// channel that opted into lossy conversion may discard it instead of
			// requiring a native Messages upstream.
			if !allowLossy {
				return "context_management is only supported by a native Messages upstream", nil
			}
			lossyFields = append(lossyFields, "context_management")
		}
		if upstream == ProtocolResponses && features.HasStopSequences {
			return "stop_sequences cannot be represented by a Responses upstream", nil
		}
		if upstream == ProtocolResponses && features.HasTopK {
			return "top_k cannot be represented by a Responses upstream", nil
		}
		if len(features.MessagesNativeFields) > 0 {
			return fmt.Sprintf("Messages fields require a native Messages upstream: %s", strings.Join(features.MessagesNativeFields, ", ")), nil
		}
		// Declared server tools are dropped by the request converters — CC
		// Switch semantics — and client-executed typed tools are lowered to
		// plain functions, so declarations no longer force a native upstream.
		// Executed server tool history still does: dropping server_tool_use
		// context would corrupt the conversation.
		if len(features.HistoricalHostedTools) > 0 {
			return fmt.Sprintf("server-side Messages tool history requires a native Messages upstream: %s", strings.Join(features.HistoricalHostedTools, ", ")), nil
		}
	}
	unsupportedContentTypes := make([]string, 0)
	var lossyContentTypes []string
	for _, contentType := range features.ContentTypes {
		if convertedContentTypeSupported(protocol, upstream, contentType) {
			continue
		}
		if allowLossy && lossyDroppableContentType(protocol, upstream, contentType) {
			lossyContentTypes = append(lossyContentTypes, contentType)
			continue
		}
		unsupportedContentTypes = append(unsupportedContentTypes, contentType)
	}
	if len(unsupportedContentTypes) > 0 {
		return fmt.Sprintf(
			"%s content types cannot be converted losslessly to %s: %s",
			protocol,
			upstream,
			strings.Join(unsupportedContentTypes, ", "),
		), nil
	}
	return "", append(lossyFields, lossyContentTypes...)
}

// lossyDroppableContentType reports whether the content type carries only
// opaque provider-bound state that a conversion may discard when the channel
// opted into lossy conversion. Encrypted reasoning state is unreadable by any
// other provider, so dropping it loses nothing the target model could use.
// Gemini upstream conversion errors on unknown content parts, so lossy drops
// stay limited to Chat and Messages upstreams.
func lossyDroppableContentType(protocol, upstream Protocol, contentType string) bool {
	if protocol != ProtocolResponses {
		return false
	}
	if upstream != ProtocolChat && upstream != ProtocolMessages {
		return false
	}
	return contentType == "encrypted_content"
}

func convertedContentTypeSupported(protocol, upstream Protocol, contentType string) bool {
	switch protocol {
	case ProtocolResponses:
		switch upstream {
		case ProtocolChat, ProtocolGemini:
			switch contentType {
			case "input_text", "output_text", "text", "input_image", "input_file", "input_audio", "input_video":
				return true
			}
		case ProtocolMessages:
			switch contentType {
			case "input_text", "output_text", "text", "input_image", "input_file":
				return true
			}
		}
	case ProtocolMessages:
		switch upstream {
		case ProtocolChat, ProtocolResponses:
			switch contentType {
			case "text", "input_text", "image", "document", "thinking", "tool_use", "tool_result":
				return true
			}
		case ProtocolGemini:
			switch contentType {
			case "text", "input_text", "image", "thinking", "tool_use", "tool_result":
				return true
			}
		}
	}
	return false
}

func defaultUpstreamProtocols(channel *model.Channel, modelName string) []string {
	if channel == nil {
		return nil
	}
	switch channel.Type {
	case constant.ChannelTypeCodex:
		return []string{dto.ProtocolCapabilityResponses}
	case constant.ChannelTypeAnthropic:
		return []string{dto.ProtocolCapabilityMessages}
	case constant.ChannelTypeAws:
		if strings.Contains(strings.ToLower(strings.TrimSpace(modelName)), "nova-") {
			return []string{dto.ProtocolCapabilityChat}
		}
		return []string{dto.ProtocolCapabilityMessages}
	case constant.ChannelTypeAzure:
		return []string{dto.ProtocolCapabilityChat, dto.ProtocolCapabilityResponses}
	case constant.ChannelTypeGemini:
		return []string{dto.ProtocolCapabilityGemini}
	case constant.ChannelTypeVertexAi:
		modelName = strings.ToLower(strings.TrimSpace(modelName))
		switch {
		case strings.HasPrefix(modelName, "claude"):
			return []string{dto.ProtocolCapabilityMessages}
		case strings.Contains(modelName, "llama"), strings.Contains(modelName, "-maas"):
			return []string{dto.ProtocolCapabilityChat}
		default:
			return []string{dto.ProtocolCapabilityGemini}
		}
	case constant.ChannelTypeAli:
		protocols := []string{dto.ProtocolCapabilityChat, dto.ProtocolCapabilityResponses}
		if aliSupportsMessages(modelName) {
			protocols = append(protocols, dto.ProtocolCapabilityMessages)
		}
		return protocols
	case constant.ChannelTypeVolcEngine:
		protocols := []string{dto.ProtocolCapabilityChat, dto.ProtocolCapabilityResponses}
		if _, ok := constant.ChannelSpecialBases[channel.GetBaseURL()]; ok {
			protocols = append(protocols, dto.ProtocolCapabilityMessages)
		}
		return protocols
	case constant.ChannelTypeDeepSeek, constant.ChannelTypeMoonshot, constant.ChannelTypeMiniMax, constant.ChannelTypeZhipu_v4:
		return []string{dto.ProtocolCapabilityChat, dto.ProtocolCapabilityMessages}
	case constant.ChannelTypeXai:
		return []string{dto.ProtocolCapabilityChat, dto.ProtocolCapabilityResponses}
	case constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI:
		return []string{
			dto.ProtocolCapabilityChat,
			dto.ProtocolCapabilityMessages,
			dto.ProtocolCapabilityResponses,
			dto.ProtocolCapabilityGemini,
		}
	case constant.ChannelTypeOpenAI:
		baseURL := strings.TrimSpace(channel.GetBaseURL())
		if baseURL == "" && channel.Type >= 0 && channel.Type < len(constant.ChannelBaseURLs) {
			baseURL = constant.ChannelBaseURLs[channel.Type]
		}
		if isOfficialOpenAIBaseURL(baseURL) {
			return []string{dto.ProtocolCapabilityChat, dto.ProtocolCapabilityResponses}
		}
		return []string{dto.ProtocolCapabilityChat}
	default:
		return []string{dto.ProtocolCapabilityChat}
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
