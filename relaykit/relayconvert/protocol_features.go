package relayconvert

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

type RequestFeatureSet struct {
	Stream                    bool     `json:"stream"`
	HasPreviousResponse       bool     `json:"has_previous_response"`
	HasConversation           bool     `json:"has_conversation"`
	HasPrompt                 bool     `json:"has_prompt"`
	HasContextManagement      bool     `json:"has_context_management"`
	HasThinking               bool     `json:"has_thinking"`
	HasCustomTools            bool     `json:"has_custom_tools"`
	HasNamespaceTools         bool     `json:"has_namespace_tools"`
	HasToolSearch             bool     `json:"has_tool_search"`
	HasAdditionalTools        bool     `json:"has_additional_tools"`
	HasStopSequences          bool     `json:"has_stop_sequences"`
	HasTopK                   bool     `json:"has_top_k"`
	StopSequenceCount         int      `json:"stop_sequence_count,omitempty"`
	HasMultipleCandidates     bool     `json:"has_multiple_candidates"`
	HasOutputConstraint       bool     `json:"has_output_constraint"`
	HasProviderState          bool     `json:"has_provider_state"`
	HasMessagesState          bool     `json:"has_messages_state"`
	UnsupportedGeminiSchema   string   `json:"unsupported_gemini_schema,omitempty"`
	UnsupportedMessagesSchema string   `json:"unsupported_messages_schema,omitempty"`
	UnsupportedOpenAISchema   string   `json:"unsupported_openai_schema,omitempty"`
	HasSpeed                  bool     `json:"has_speed"`
	RequiredFields            []string `json:"required_fields,omitempty"`
	ResponsesIncludes         []string `json:"responses_includes,omitempty"`
	ContentTypes              []string `json:"content_types,omitempty"`
	DeclaredHostedTools       []string `json:"declared_hosted_tools,omitempty"`
	HistoricalHostedTools     []string `json:"historical_hosted_tools,omitempty"`
	MessagesNativeFields      []string `json:"messages_native_fields,omitempty"`
}

func MergeRequestFeatureSets(featureSets ...RequestFeatureSet) RequestFeatureSet {
	merged := RequestFeatureSet{}
	contentTypes := make(map[string]struct{})
	declaredHostedTools := make(map[string]struct{})
	historicalHostedTools := make(map[string]struct{})
	messagesNativeFields := make(map[string]struct{})
	requiredFields := make(map[string]struct{})
	responsesIncludes := make(map[string]struct{})
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
		merged.StopSequenceCount = max(merged.StopSequenceCount, features.StopSequenceCount)
		merged.HasMultipleCandidates = merged.HasMultipleCandidates || features.HasMultipleCandidates
		merged.HasOutputConstraint = merged.HasOutputConstraint || features.HasOutputConstraint
		merged.HasSpeed = merged.HasSpeed || features.HasSpeed
		if features.UnsupportedMessagesSchema != "" {
			merged.UnsupportedMessagesSchema = features.UnsupportedMessagesSchema
		}
		if features.UnsupportedOpenAISchema != "" {
			merged.UnsupportedOpenAISchema = features.UnsupportedOpenAISchema
		}
		merged.HasProviderState = merged.HasProviderState || features.HasProviderState
		merged.HasMessagesState = merged.HasMessagesState || features.HasMessagesState
		if features.UnsupportedGeminiSchema != "" {
			merged.UnsupportedGeminiSchema = features.UnsupportedGeminiSchema
		}
		appendUnique(&merged.RequiredFields, requiredFields, features.RequiredFields)
		appendUnique(&merged.ResponsesIncludes, responsesIncludes, features.ResponsesIncludes)
		appendUnique(&merged.ContentTypes, contentTypes, features.ContentTypes)
		appendUnique(&merged.DeclaredHostedTools, declaredHostedTools, features.DeclaredHostedTools)
		appendUnique(&merged.HistoricalHostedTools, historicalHostedTools, features.HistoricalHostedTools)
		appendUnique(&merged.MessagesNativeFields, messagesNativeFields, features.MessagesNativeFields)
	}
	return merged
}

func ExtractRequestFeatureSet(protocol Protocol, body []byte) (RequestFeatureSet, error) {
	features := RequestFeatureSet{}
	if len(body) == 0 {
		return features, nil
	}
	var request map[string]any
	if err := kitutil.Unmarshal(body, &request); err != nil {
		return features, err
	}
	features.Stream, _ = request["stream"].(bool)
	features.HasPreviousResponse = nonEmptyString(request["previous_response_id"])
	features.HasConversation = nonNullValue(request, "conversation")
	features.HasPrompt = nonNullValue(request, "prompt")
	features.HasContextManagement = nonNullValue(request, "context_management")
	features.HasThinking = nonNullValue(request, "thinking") || nonNullValue(request, "reasoning")
	inspectRequiredRequestFields(protocol, request, &features)
	if protocol == ProtocolChat {
		features.StopSequenceCount = requestSequenceCount(request["stop"])
		features.HasStopSequences = features.StopSequenceCount > 0
		features.HasTopK = nonNullValue(request, "top_k")
		features.HasMultipleCandidates = requestNumberGreaterThanOne(request["n"])
		features.HasOutputConstraint = hasOutputConstraint(request["response_format"])
		if format, ok := request["response_format"].(map[string]any); ok {
			if schema, ok := format["json_schema"].(map[string]any); ok {
				features.UnsupportedGeminiSchema = sharedgemini.UnsupportedOutputSchemaField(schema["schema"], 0)
			}
		}
	}
	if protocol == ProtocolResponses {
		if includes, ok := request["include"].([]any); ok {
			for _, include := range includes {
				if value, ok := include.(string); ok {
					features.ResponsesIncludes = append(features.ResponsesIncludes, value)
				}
			}
		}
		if config, ok := request["text"].(map[string]any); ok {
			features.HasOutputConstraint = hasOutputConstraint(config["format"])
			if format, ok := config["format"].(map[string]any); ok {
				features.UnsupportedGeminiSchema = sharedgemini.UnsupportedOutputSchemaField(format["schema"], 0)
			}
			for key, value := range config {
				if key != "format" && value != nil {
					features.RequiredFields = append(features.RequiredFields, "text."+key)
				}
			}
		}
	}
	if protocol == ProtocolMessages {
		features.StopSequenceCount = requestSequenceCount(request["stop_sequences"])
		features.HasStopSequences = features.StopSequenceCount > 0
		features.HasTopK = nonNullValue(request, "top_k")
		inspectMessagesNativeFields(request, &features)
		features.HasSpeed = meaningfulRequestValue(request["speed"])
	}
	if protocol == ProtocolGemini {
		inspectGeminiRequestFeatures(request, &features)
	}
	var outputFormat *dto.ResponseFormat
	switch protocol {
	case ProtocolChat:
		if raw := request["response_format"]; raw != nil {
			outputFormat, _ = kitutil.Any2Type[*dto.ResponseFormat](raw)
		}
	case ProtocolResponses:
		if config, ok := request["text"].(map[string]any); ok {
			if format, ok := config["format"].(map[string]any); ok {
				raw, _ := kitutil.Marshal(format)
				outputFormat = &dto.ResponseFormat{Type: kitutil.Interface2String(format["type"]), JsonSchema: raw}
			}
		}
	case ProtocolMessages:
		var messages dto.ClaudeRequest
		if err := kitutil.Unmarshal(body, &messages); err != nil {
			return features, err
		}
		format, err := sharedclaude.OutputFormat(&messages)
		if err != nil {
			features.MessagesNativeFields = append(features.MessagesNativeFields, "output_config: "+err.Error())
		} else {
			outputFormat = format
		}
	case ProtocolGemini:
		var gemini dto.GeminiChatRequest
		if err := kitutil.Unmarshal(body, &gemini); err != nil {
			return features, err
		}
		format, err := sharedgemini.OutputFormat(&gemini.GenerationConfig)
		if err != nil {
			features.RequiredFields = append(features.RequiredFields, "generationConfig.response: "+err.Error())
		} else {
			outputFormat = format
		}
	}
	if outputFormat != nil && outputFormat.Type != "" && outputFormat.Type != "text" {
		features.HasOutputConstraint = true
		if outputFormat.Type != "json_schema" {
			features.UnsupportedMessagesSchema = "output format " + outputFormat.Type + " has no verified Messages mapping"
		} else {
			var schema dto.FormatJsonSchema
			if err := kitutil.Unmarshal(outputFormat.JsonSchema, &schema); err != nil {
				return features, err
			}
			features.UnsupportedGeminiSchema = sharedgemini.UnsupportedOutputSchemaField(schema.Schema, 0)
			if err := sharedclaude.ValidateOutputSchema(schema.Schema, false); err != nil {
				features.UnsupportedMessagesSchema = err.Error()
			}
			if protocol == ProtocolMessages || protocol == ProtocolGemini {
				if err := sharedclaude.ValidateOutputSchema(schema.Schema, true); err != nil {
					features.UnsupportedOpenAISchema = err.Error()
				}
			}
		}
	}
	slices.Sort(features.RequiredFields)
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
	case ProtocolChat:
		messages, _ := request["messages"].([]any)
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				continue
			}
			inspectContentBlocks(protocol, message["content"], features, contentSeen, historicalHostedSeen)
			role, _ := message["role"].(string)
			if role == "system" || role == "developer" || role == "tool" || role == "function" {
				blocks, _ := message["content"].([]any)
				for _, raw := range blocks {
					block, _ := raw.(map[string]any)
					if kind, _ := block["type"].(string); kind != "text" {
						addUniqueToolType(role+":"+kind, &features.ContentTypes, contentSeen)
					}
				}
			}
		}
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
			if state, ok := item["encrypted_content"].(string); ok && state != "" {
				if sharedclaude.IsThinkingBlockState(state) {
					features.HasMessagesState = true
				} else {
					features.HasProviderState = true
				}
			}
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

// Only fields whose execution semantics are accounted for may cross protocols.
// Other fields stay available to native forwarding, including future extensions.
func inspectRequiredRequestFields(protocol Protocol, request map[string]any, features *RequestFeatureSet) {
	var portable string
	switch protocol {
	case ProtocolChat:
		portable = "model messages stream stream_options max_tokens max_completion_tokens temperature top_p top_k stop n response_format tools tool_choice parallel_tool_calls functions function_call reasoning_effort reasoning thinking enable_thinking thinking_budget web_search_options"
	case ProtocolResponses:
		portable = "model input stream stream_options instructions max_output_tokens temperature top_p reasoning text tools tool_choice parallel_tool_calls previous_response_id store conversation prompt context_management include"
	case ProtocolMessages:
		portable = "model messages system stream max_tokens temperature top_p top_k stop_sequences tools tool_choice thinking context_management output_config output_format container mcp_servers inference_geo speed service_tier"
	case ProtocolGemini:
		portable = "contents generationConfig systemInstruction system_instruction tools toolConfig"
	}
	known := strings.Fields(portable)
	for field, value := range request {
		if value != nil && !slices.Contains(known, field) {
			features.RequiredFields = append(features.RequiredFields, field)
		}
	}
}

func requestSequenceCount(value any) int {
	switch value := value.(type) {
	case string:
		if value != "" {
			return 1
		}
	case []any:
		return len(value)
	}
	return 0
}

func requestNumberGreaterThanOne(value any) bool {
	number, ok := value.(float64)
	return ok && number > 1
}

func hasOutputConstraint(value any) bool {
	format, ok := value.(map[string]any)
	if !ok {
		return meaningfulRequestValue(value)
	}
	kind, _ := format["type"].(string)
	return kind != "" && kind != "text"
}

func inspectGeminiRequestFeatures(request map[string]any, features *RequestFeatureSet) {
	config, _ := request["generationConfig"].(map[string]any)
	portable := strings.Fields("temperature topP top_p topK top_k maxOutputTokens max_output_tokens stopSequences stop_sequences candidateCount candidate_count thinkingConfig thinking_config responseSchema response_schema responseJsonSchema response_json_schema responseMimeType response_mime_type")
	for field, value := range config {
		if value != nil && !slices.Contains(portable, field) {
			features.RequiredFields = append(features.RequiredFields, "generationConfig."+field)
		}
	}
	features.HasTopK = nonNullValue(config, "topK") || nonNullValue(config, "top_k")
	features.StopSequenceCount = max(requestSequenceCount(config["stopSequences"]), requestSequenceCount(config["stop_sequences"]))
	features.HasStopSequences = features.StopSequenceCount > 0
	features.HasMultipleCandidates = requestNumberGreaterThanOne(config["candidateCount"]) || requestNumberGreaterThanOne(config["candidate_count"])
	features.HasThinking = nonNullValue(config, "thinkingConfig") || nonNullValue(config, "thinking_config")
	contents, _ := request["contents"].([]any)
	conversationCount := len(contents)
	contents = append(slices.Clone(contents), request["systemInstruction"], request["system_instruction"])
	seen := make(map[string]struct{})
	for contentIndex, rawContent := range contents {
		content, _ := rawContent.(map[string]any)
		parts, _ := content["parts"].([]any)
		seenCall, seenContent, seenResponse := false, false, false
		for _, raw := range parts {
			part, _ := raw.(map[string]any)
			payloads := 0
			for _, field := range []string{"text", "inlineData", "inline_data", "fileData", "functionCall", "functionResponse"} {
				if nonNullValue(part, field) {
					payloads++
				}
			}
			if payloads > 1 {
				addUniqueToolType("multiple_part_payloads", &features.ContentTypes, seen)
			}
			thought, _ := part["thought"].(bool)
			text, _ := part["text"].(string)
			hasContent := !thought && text != "" || nonNullValue(part, "inlineData") || nonNullValue(part, "inline_data") || nonNullValue(part, "fileData")
			if hasContent && seenCall {
				addUniqueToolType("content_after_function_call", &features.ContentTypes, seen)
			}
			if nonNullValue(part, "functionResponse") {
				if seenContent {
					addUniqueToolType("content_before_function_response", &features.ContentTypes, seen)
				}
				seenResponse = true
			}
			seenCall = seenCall || nonNullValue(part, "functionCall")
			seenContent = seenContent || hasContent
			if seenCall && seenResponse {
				addUniqueToolType("mixed_function_call_and_response", &features.ContentTypes, seen)
			}
			for field, value := range part {
				if value == nil {
					continue
				}
				if contentIndex >= conversationCount && field != "text" {
					addUniqueToolType("system:"+field, &features.ContentTypes, seen)
				}
				switch field {
				case "thoughtSignature", "thought_signature":
					features.HasProviderState = features.HasProviderState || meaningfulRequestValue(value)
				case "text", "functionCall", "functionResponse", "thought":
				case "inlineData", "inline_data", "fileData":
					media, _ := value.(map[string]any)
					mimeType, _ := media["mimeType"].(string)
					if mimeType == "" {
						mimeType, _ = media["mime_type"].(string)
					}
					kind := "image"
					if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
						kind = "media:" + mimeType
					}
					addUniqueToolType(kind, &features.ContentTypes, seen)
				default:
					addUniqueToolType(field, &features.ContentTypes, seen)
				}
			}
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
		contentType := strings.TrimSpace(kitutil.Interface2String(block["type"]))
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
			execution := strings.TrimSpace(kitutil.Interface2String(tool["execution"]))
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
			if protocol == ProtocolResponses && toolType != "local_shell" || protocol == ProtocolMessages && isMessagesServerTool(toolType) {
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
		typedType := strings.TrimSpace(kitutil.Interface2String(typed["type"]))
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
	for _, field := range []string{"container", "mcp_servers", "inference_geo"} {
		if meaningfulRequestValue(request[field]) {
			features.MessagesNativeFields = append(features.MessagesNativeFields, field)
		}
	}
	if tier := request["service_tier"]; meaningfulRequestValue(tier) {
		if tier != "auto" && tier != "standard_only" {
			features.MessagesNativeFields = append(features.MessagesNativeFields, "service_tier")
		} else {
			features.RequiredFields = append(features.RequiredFields, "service_tier")
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
		if field != "effort" && field != "format" && fieldValue != nil {
			return true
		}
	}
	return false
}

func AnalyzeConversionFeatures(protocol, upstream Protocol, features RequestFeatureSet, allowLossy bool, allowDirectiveDrop bool) (string, []string) {
	if features.HasMessagesState && upstream != ProtocolMessages {
		return "Anthropic thinking state requires its originating Messages upstream", nil
	}
	if protocol == upstream {
		return "", nil
	}
	if features.HasProviderState {
		return "provider-bound state requires its native protocol and provider", nil
	}
	if features.HasMultipleCandidates {
		return "multiple output candidates require a native protocol round trip", nil
	}
	var lossyFields []string
	if features.HasStopSequences && upstream == ProtocolResponses {
		if !allowDirectiveDrop {
			return "stop sequences (stop_sequences) require lossy gateway emulation for Responses", nil
		}
		lossyFields = append(lossyFields, "stop")
	}
	if features.HasTopK && (upstream == ProtocolResponses || upstream == ProtocolChat && protocol != ProtocolChat) {
		if !allowDirectiveDrop {
			return "top_k has no verified mapping on this conversion route", nil
		}
		lossyFields = append(lossyFields, "top_k")
	}
	if features.HasSpeed {
		if !allowDirectiveDrop {
			return "speed requires a native Messages upstream", nil
		}
		lossyFields = append(lossyFields, "speed")
	}
	if features.StopSequenceCount > 5 && upstream == ProtocolGemini || features.StopSequenceCount > 4 && protocol == ProtocolGemini {
		return "stop sequence count exceeds the conversion route's supported limit", nil
	}
	if features.HasOutputConstraint && upstream == ProtocolMessages && features.UnsupportedMessagesSchema != "" {
		return "required output format: " + features.UnsupportedMessagesSchema, nil
	}
	if (upstream == ProtocolChat || upstream == ProtocolResponses) && features.UnsupportedOpenAISchema != "" {
		return features.UnsupportedOpenAISchema, nil
	}
	if upstream == ProtocolGemini && features.UnsupportedGeminiSchema != "" {
		return fmt.Sprintf("Gemini output schema does not preserve constraint %q", features.UnsupportedGeminiSchema), nil
	}
	for _, include := range features.ResponsesIncludes {
		if include == "message.output_text.logprobs" && upstream == ProtocolChat {
			continue
		}
		if include != "reasoning.encrypted_content" || upstream != ProtocolMessages {
			return fmt.Sprintf("Responses include %q has no verified mapping to %s", include, upstream), nil
		}
	}
	for _, field := range features.RequiredFields {
		if conversionFieldSupported(protocol, upstream, field) {
			continue
		}
		if allowLossy && slices.Contains([]string{"metadata", "client_metadata", "user"}, field) {
			lossyFields = append(lossyFields, field)
			continue
		}
		return fmt.Sprintf("%s field %q has no verified mapping to %s", protocol, field, upstream), nil
	}
	if protocol == ProtocolResponses {
		switch {
		case features.HasConversation:
			return "conversation is only supported by a native Responses upstream", nil
		case features.HasPrompt:
			return "hosted prompt is only supported by a native Responses upstream", nil
		case features.HasContextManagement:
			return "context_management is only supported by a native Responses upstream", nil
		}
		if len(features.HistoricalHostedTools) > 0 {
			return fmt.Sprintf(
				"Responses server tool history cannot be replayed to %s: %s",
				upstream,
				strings.Join(features.HistoricalHostedTools, ", "),
			), nil
		}
	}
	for _, tool := range features.DeclaredHostedTools {
		if strings.HasPrefix(tool, "web_search") || tool == "web_search_options" || tool == "googleSearch" {
			continue
		}
		return fmt.Sprintf("server tool %q has no verified mapping to %s", tool, upstream), nil
	}
	if protocol == ProtocolMessages {
		if features.HasContextManagement {
			// Gateway context edits use estimated token thresholds. Lossy
			// conversion must be enabled both for those estimates and for edits
			// that cannot be applied; the request executor records the outcome.
			if allowDirectiveDrop {
				lossyFields = append(lossyFields, "context_management")
			} else {
				return "context_management requires its native Messages upstream", nil
			}
		}
		if len(features.MessagesNativeFields) > 0 {
			return fmt.Sprintf("Messages fields require a native Messages upstream: %s", strings.Join(features.MessagesNativeFields, ", ")), nil
		}
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

func conversionFieldSupported(protocol, upstream Protocol, field string) bool {
	if field == "service_tier" && (protocol == ProtocolMessages && (upstream == ProtocolChat || upstream == ProtocolResponses) || upstream == ProtocolMessages && (protocol == ProtocolChat || protocol == ProtocolResponses)) {
		return true
	}
	if protocol == ProtocolMessages && upstream == ProtocolResponses && field == "cache_control" {
		return true
	}
	if protocol == ProtocolChat && upstream == ProtocolResponses {
		return slices.Contains([]string{"frequency_penalty", "presence_penalty", "user", "store", "metadata", "prompt_cache_key", "service_tier", "verbosity", "logprobs", "top_logprobs", "safety_identifier", "prompt_cache_retention"}, field)
	}
	if protocol == ProtocolChat && upstream == ProtocolGemini {
		return field == "seed" || field == "extra_body"
	}
	if protocol == ProtocolResponses && upstream == ProtocolChat {
		return slices.Contains([]string{"frequency_penalty", "presence_penalty", "user", "metadata", "prompt_cache_key", "top_logprobs", "service_tier", "prompt_cache_retention", "safety_identifier", "enable_thinking", "thinking_budget", "text.verbosity"}, field)
	}
	return false
}

func convertedContentTypeSupported(protocol, upstream Protocol, contentType string) bool {
	switch protocol {
	case ProtocolChat:
		switch contentType {
		case "text", "image_url", "file":
			return true
		case "input_audio", "video_url":
			return upstream == ProtocolResponses || upstream == ProtocolGemini
		}
	case ProtocolGemini:
		return contentType == "image"
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
