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
)

const (
	StateModeNone            = "none"
	StateModeNativeResponses = "native_responses"
	StateModeReplay          = "replay"
	StateModeStrictAppend    = "strict_append"
)

type RequestFeatureSet struct {
	Stream               bool     `json:"stream"`
	HasPreviousResponse  bool     `json:"has_previous_response"`
	HasConversation      bool     `json:"has_conversation"`
	HasPrompt            bool     `json:"has_prompt"`
	HasContextManagement bool     `json:"has_context_management"`
	HasThinking          bool     `json:"has_thinking"`
	HasCustomTools       bool     `json:"has_custom_tools"`
	HasNamespaceTools    bool     `json:"has_namespace_tools"`
	HasToolSearch        bool     `json:"has_tool_search"`
	HasAdditionalTools   bool     `json:"has_additional_tools"`
	ContentTypes         []string `json:"content_types,omitempty"`
	HostedToolTypes      []string `json:"hosted_tool_types,omitempty"`
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
	Features               RequestFeatureSet                 `json:"features"`
	Reason                 string                            `json:"reason,omitempty"`
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
	if channel != nil &&
		channel.Type != constant.ChannelTypeAdvancedCustom &&
		(protocol == ProtocolResponses || protocol == ProtocolMessages) {
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
	if !policy.Enabled && capabilities == nil {
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
			plan.StateEnabled = policy.Enabled
			return plan
		}
		if !allowConversion {
			plan.Reason = "protocol conversion is disabled for this channel and model"
			return plan
		}
		if reason := conversionFeatureIncompatibility(protocol, compatibility.UpstreamProtocol, features); reason != "" {
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
		return plan
	}

	if len(upstreamProtocols) == 0 {
		upstreamProtocols = defaultUpstreamProtocols(channel)
	}

	for _, configured := range upstreamProtocols {
		upstream := Protocol(strings.TrimSpace(configured))
		if upstream != protocol {
			continue
		}
		plan.Status = StatusNative
		plan.UpstreamProtocol = upstream
		plan.StateMode = stateModeFor(protocol, upstream)
		plan.StateEnabled = policy.Enabled
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
		if reason := conversionFeatureIncompatibility(protocol, upstream, features); reason != "" {
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
		for _, candidate := range []Protocol{ProtocolChat, ProtocolMessages, ProtocolResponses} {
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
		plan.StateEnabled = true
		if upstream == protocol {
			plan.Status = StatusNative
			plans = append(plans, plan)
			continue
		}
		if !allowConversion {
			continue
		}
		if reason := conversionFeatureIncompatibility(protocol, upstream, features); reason != "" {
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
		plan.RequestConverter = converter.ID
		plan.ResponseConverter = converter.ID
		plan.Quality = converter.Quality
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

func automaticProtocolOrder(protocol Protocol) []Protocol {
	switch protocol {
	case ProtocolResponses:
		return []Protocol{ProtocolResponses, ProtocolChat, ProtocolMessages}
	case ProtocolMessages:
		return []Protocol{ProtocolMessages, ProtocolChat, ProtocolResponses}
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
	hostedSeen := make(map[string]struct{})
	contentSeen := make(map[string]struct{})
	inspectRequestContentTypes(protocol, request, &features, contentSeen, hostedSeen)
	features.HasAdditionalTools = inspectRequestInput(protocol, request["input"], &features, hostedSeen)

	toolValue, exists := request["tools"]
	if !exists {
		return features, nil
	}
	tools, ok := toolValue.([]any)
	if !ok {
		return features, nil
	}
	inspectTools(protocol, tools, &features, hostedSeen)
	return features, nil
}

func inspectRequestContentTypes(protocol Protocol, request map[string]any, features *RequestFeatureSet, contentSeen, hostedSeen map[string]struct{}) {
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
			inspectContentBlocks(protocol, item["content"], features, contentSeen, hostedSeen)
		}
	case ProtocolMessages:
		inspectContentBlocks(protocol, request["system"], features, contentSeen, hostedSeen)
		messages, ok := request["messages"].([]any)
		if !ok {
			return
		}
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				continue
			}
			inspectContentBlocks(protocol, message["content"], features, contentSeen, hostedSeen)
		}
	}
}

func inspectContentBlocks(protocol Protocol, value any, features *RequestFeatureSet, contentSeen, hostedSeen map[string]struct{}) {
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
				addHostedToolType(contentType, features, hostedSeen)
			}
		}
		if nested, exists := block["content"]; exists {
			inspectContentBlocks(protocol, nested, features, contentSeen, hostedSeen)
		}
	}
}

func inspectTools(protocol Protocol, tools []any, features *RequestFeatureSet, hostedSeen map[string]struct{}) {
	for _, item := range tools {
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
			if nested, ok := tool["tools"].([]any); ok {
				inspectTools(protocol, nested, features, hostedSeen)
			}
		case "tool_search":
			features.HasToolSearch = true
			execution := strings.TrimSpace(common.Interface2String(tool["execution"]))
			if protocol == ProtocolResponses && execution != "" && execution != "client" {
				addHostedToolType("tool_search", features, hostedSeen)
			} else if protocol == ProtocolMessages {
				addHostedToolType("tool_search", features, hostedSeen)
			}
		default:
			if strings.Contains(toolType, "tool_search") {
				features.HasToolSearch = true
			}
			if protocol == ProtocolResponses || protocol == ProtocolMessages && isMessagesServerTool(toolType) {
				addHostedToolType(toolType, features, hostedSeen)
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

func inspectRequestInput(protocol Protocol, value any, features *RequestFeatureSet, hostedSeen map[string]struct{}) bool {
	found := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if inspectRequestInput(protocol, item, features, hostedSeen) {
				found = true
			}
		}
	case map[string]any:
		typedType := strings.TrimSpace(common.Interface2String(typed["type"]))
		if typedType == "reasoning" {
			features.HasThinking = true
		}
		if protocol == ProtocolResponses && isResponsesHostedHistoryItem(typedType) {
			addHostedToolType(typedType, features, hostedSeen)
		}
		if typedType == "additional_tools" {
			found = true
			if tools, ok := typed["tools"].([]any); ok {
				inspectTools(protocol, tools, features, hostedSeen)
			}
		}
		for _, nested := range typed {
			if inspectRequestInput(protocol, nested, features, hostedSeen) {
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
		"local_shell_call",
		"local_shell_call_output",
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

func addHostedToolType(toolType string, features *RequestFeatureSet, hostedSeen map[string]struct{}) {
	if _, exists := hostedSeen[toolType]; exists {
		return
	}
	hostedSeen[toolType] = struct{}{}
	features.HostedToolTypes = append(features.HostedToolTypes, toolType)
}

func nonNullValue(values map[string]any, key string) bool {
	value, exists := values[key]
	return exists && value != nil
}

func nonEmptyString(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

func conversionFeatureIncompatibility(protocol, upstream Protocol, features RequestFeatureSet) string {
	if protocol == ProtocolResponses {
		switch {
		case features.HasConversation:
			return "conversation is only supported by a native Responses upstream"
		case features.HasPrompt:
			return "hosted prompt is only supported by a native Responses upstream"
		case features.HasContextManagement:
			return "context_management is only supported by a native Responses upstream"
		}
		unsupportedHostedTools := make([]string, 0, len(features.HostedToolTypes))
		for _, toolType := range features.HostedToolTypes {
			if isDroppableResponsesHostedToolType(toolType) {
				continue
			}
			unsupportedHostedTools = append(unsupportedHostedTools, toolType)
		}
		if len(unsupportedHostedTools) > 0 {
			return fmt.Sprintf("Responses server tools cannot be converted to %s: %s", upstream, strings.Join(unsupportedHostedTools, ", "))
		}
	}
	if protocol == ProtocolMessages {
		if features.HasContextManagement {
			return "context_management is only supported by a native Messages upstream"
		}
		if len(features.HostedToolTypes) > 0 {
			return fmt.Sprintf("server-side Messages tools require a native Messages upstream: %s", strings.Join(features.HostedToolTypes, ", "))
		}
	}
	unsupportedContentTypes := make([]string, 0)
	for _, contentType := range features.ContentTypes {
		if convertedContentTypeSupported(protocol, upstream, contentType) {
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
		)
	}
	return ""
}

func isDroppableResponsesHostedToolType(toolType string) bool {
	toolType = strings.ToLower(strings.TrimSpace(toolType))
	for _, prefix := range []string{
		"web_search",
		"web_fetch",
		"file_search",
		"computer",
		"code_interpreter",
		"code_execution",
		"image_generation",
		"local_shell",
		"shell",
		"apply_patch",
		"mcp",
		"program",
		"browser",
		"tool_search",
	} {
		if strings.HasPrefix(toolType, prefix) {
			return true
		}
	}
	return false
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
			case "input_text", "output_text", "text", "input_image":
				return true
			}
		}
	case ProtocolMessages:
		switch upstream {
		case ProtocolChat, ProtocolResponses, ProtocolGemini:
			switch contentType {
			case "text", "input_text", "image", "thinking", "tool_use", "tool_result":
				return true
			}
		}
	}
	return false
}

func defaultUpstreamProtocols(channel *model.Channel) []string {
	if channel == nil {
		return nil
	}
	switch channel.Type {
	case constant.ChannelTypeCodex:
		return []string{dto.ProtocolCapabilityResponses}
	case constant.ChannelTypeAnthropic, constant.ChannelTypeAws:
		return []string{dto.ProtocolCapabilityMessages}
	case constant.ChannelTypeAzure:
		return []string{dto.ProtocolCapabilityChat, dto.ProtocolCapabilityResponses}
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
