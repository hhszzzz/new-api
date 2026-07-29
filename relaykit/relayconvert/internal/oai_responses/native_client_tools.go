package oairesponses

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ResponsesClientToolBridge records the reversible lowering required by a
// strict Responses upstream that accepts ordinary function tools but not the
// Codex client extensions custom, namespace, tool_search, or additional_tools.
type ResponsesClientToolBridge struct {
	toolState     *sharedbridge.ToolState
	restoreNeeded bool
}

// ResponsesClientToolStreamRestorer restores one strict-upstream Responses
// stream. It is stateful because custom and tool_search calls arrive as a
// function-call item followed by separate argument delta events.
type ResponsesClientToolStreamRestorer struct {
	bridge          *ResponsesClientToolBridge
	nextSequence    int
	sequenceStarted bool
	callsByID       map[string]*responsesClientToolStreamCall
	callsByOutput   map[int]*responsesClientToolStreamCall
}

type responsesClientToolStreamCall struct {
	identity    sharedbridge.ToolIdentity
	itemID      string
	callID      string
	outputIndex int
	arguments   strings.Builder
}

// LowerResponsesClientTools mutates request into the public function-tool
// subset understood by strict Responses implementations. Hosted/server tools
// are retained unchanged. The returned bridge restores response identities.
func LowerResponsesClientTools(request *dto.OpenAIResponsesRequest) (*ResponsesClientToolBridge, bool, error) {
	bridge := &ResponsesClientToolBridge{toolState: sharedbridge.NewToolState()}
	if request == nil {
		return bridge, false, nil
	}

	tools, err := collectResponsesToolDeclarations(request)
	if err != nil {
		return nil, false, err
	}
	lowered := make([]map[string]any, 0, len(tools))
	declaredFunctions := make(map[string]struct{}, len(tools))
	hostedTools := make(map[string]struct{}, len(tools))
	changed := false
	for _, tool := range tools {
		toolType := strings.TrimSpace(kitutil.Interface2String(tool["type"]))
		if toolType == "" {
			toolType = "function"
		}
		switch toolType {
		case "function", "custom", "freeform", "tool_search", "namespace":
			converted, convertErr := responsesToolToChatFunctions(tool, "", bridge.toolState)
			if convertErr != nil {
				return nil, false, convertErr
			}
			if toolType != "function" {
				bridge.restoreNeeded = true
				changed = true
			}
			for _, function := range converted {
				name := strings.TrimSpace(function.Function.Name)
				if name == "" {
					return nil, false, fmt.Errorf("lowered Responses function tool is missing name")
				}
				if _, exists := declaredFunctions[name]; exists {
					continue
				}
				declaredFunctions[name] = struct{}{}
				loweredTool := map[string]any{
					"type":       "function",
					"name":       name,
					"parameters": function.Function.Parameters,
				}
				if description := strings.TrimSpace(function.Function.Description); description != "" {
					loweredTool["description"] = description
				}
				if function.Function.Strict != nil {
					loweredTool["strict"] = *function.Function.Strict
				}
				lowered = append(lowered, loweredTool)
			}
		default:
			key := nativeResponsesHostedToolKey(tool)
			if _, exists := hostedTools[key]; exists {
				changed = true
				continue
			}
			hostedTools[key] = struct{}{}
			lowered = append(lowered, cloneResponsesToolMap(tool))
		}
	}

	preparedInput, inputChanged, err := lowerResponsesClientToolInput(
		request.Input,
		bridge,
		&lowered,
		declaredFunctions,
	)
	if err != nil {
		return nil, false, err
	}
	if inputChanged {
		request.Input = preparedInput
		changed = true
	}

	preparedChoice, choiceChanged, err := lowerResponsesClientToolChoice(request.ToolChoice, bridge)
	if err != nil {
		return nil, false, err
	}
	if choiceChanged {
		request.ToolChoice = preparedChoice
		changed = true
	}

	if len(tools) > 0 || len(lowered) > 0 {
		encoded, encodeErr := kitutil.Marshal(lowered)
		if encodeErr != nil {
			return nil, false, encodeErr
		}
		if !bytes.Equal(bytes.TrimSpace(request.Tools), bytes.TrimSpace(encoded)) {
			request.Tools = encoded
			changed = true
		}
	}
	return bridge, changed, nil
}

func lowerResponsesClientToolInput(
	raw []byte,
	bridge *ResponsesClientToolBridge,
	loweredTools *[]map[string]any,
	declaredFunctions map[string]struct{},
) ([]byte, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || kitutil.GetJsonType(trimmed) == "string" {
		return raw, false, nil
	}
	if kitutil.GetJsonType(trimmed) != "array" {
		return raw, false, nil
	}

	var items []any
	if err := kitutil.Unmarshal(trimmed, &items); err != nil {
		return nil, false, fmt.Errorf("invalid Responses input while lowering client tools: %w", err)
	}
	prepared := make([]any, 0, len(items))
	changed := false
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			prepared = append(prepared, value)
			continue
		}
		itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
		switch itemType {
		case "additional_tools":
			changed = true
			continue
		case "function_call":
			name, err := lowerResponsesFunctionCall(item, bridge, sharedbridge.ToolKindFunction)
			if err != nil {
				return nil, false, err
			}
			ensureNativeResponsesFunctionDeclaration(name, loweredTools, declaredFunctions)
			changed = changed || name != strings.TrimSpace(kitutil.Interface2String(item["name"])) || item["namespace"] != nil
		case "custom_tool_call":
			name, err := lowerResponsesFunctionCall(item, bridge, sharedbridge.ToolKindCustom)
			if err != nil {
				return nil, false, err
			}
			item["type"] = "function_call"
			item["arguments"] = customInputArguments(item["input"])
			delete(item, "input")
			ensureNativeResponsesFunctionDeclaration(name, loweredTools, declaredFunctions)
			bridge.restoreNeeded = true
			changed = true
		case "custom_tool_call_output":
			item["type"] = "function_call_output"
			normalizeNativeResponsesToolOutput(item, item["output"])
			bridge.restoreNeeded = true
			changed = true
		case "tool_search_call":
			name, err := lowerResponsesFunctionCall(item, bridge, sharedbridge.ToolKindToolSearch)
			if err != nil {
				return nil, false, err
			}
			item["type"] = "function_call"
			if args := toolSearchArguments(item["arguments"]); args != "" {
				item["arguments"] = args
			} else {
				item["arguments"] = "{}"
			}
			delete(item, "execution")
			ensureNativeResponsesFunctionDeclaration(name, loweredTools, declaredFunctions)
			bridge.restoreNeeded = true
			changed = true
		case "tool_search_output":
			item["type"] = "function_call_output"
			output := item["output"]
			if output == nil {
				output = item["tools"]
			}
			normalizeNativeResponsesToolOutput(item, output)
			delete(item, "tools")
			bridge.restoreNeeded = true
			changed = true
		}
		prepared = append(prepared, item)
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := kitutil.Marshal(prepared)
	return encoded, true, err
}

func lowerResponsesFunctionCall(item map[string]any, bridge *ResponsesClientToolBridge, kind sharedbridge.ToolKind) (string, error) {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	if kind == sharedbridge.ToolKindToolSearch {
		name = "tool_search"
	}
	if name == "" {
		return "", fmt.Errorf("Responses %s call is missing name", kind)
	}
	namespace := strings.TrimSpace(kitutil.Interface2String(item["namespace"]))
	upstreamName, err := upstreamToolName(bridge.toolState, kind, namespace, name)
	if err != nil {
		return "", err
	}
	item["name"] = upstreamName
	delete(item, "namespace")
	if kind != sharedbridge.ToolKindFunction || namespace != "" {
		bridge.restoreNeeded = true
	}
	return upstreamName, nil
}

func ensureNativeResponsesFunctionDeclaration(name string, tools *[]map[string]any, declared map[string]struct{}) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if _, exists := declared[name]; exists {
		return
	}
	declared[name] = struct{}{}
	*tools = append(*tools, map[string]any{
		"type":        "function",
		"name":        name,
		"description": "Historical tool retained for Responses replay.",
		"parameters": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": true,
		},
	})
}

func lowerResponsesClientToolChoice(raw []byte, bridge *ResponsesClientToolBridge) ([]byte, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || kitutil.GetJsonType(trimmed) == "string" {
		return raw, false, nil
	}
	if kitutil.GetJsonType(trimmed) != "object" {
		return raw, false, nil
	}
	var choice map[string]any
	if err := kitutil.Unmarshal(trimmed, &choice); err != nil {
		return nil, false, fmt.Errorf("invalid Responses tool_choice: %w", err)
	}
	choiceType := strings.TrimSpace(kitutil.Interface2String(choice["type"]))
	if choiceType == "namespace" {
		encoded, err := kitutil.Marshal("auto")
		return encoded, true, err
	}
	if choiceType != "function" && choiceType != "custom" && choiceType != "freeform" && choiceType != "tool_search" {
		return raw, false, nil
	}
	name := strings.TrimSpace(kitutil.Interface2String(choice["name"]))
	namespace := strings.TrimSpace(kitutil.Interface2String(choice["namespace"]))
	kind := sharedbridge.ToolKindFunction
	if choiceType == "custom" || choiceType == "freeform" {
		kind = sharedbridge.ToolKindCustom
	} else if choiceType == "tool_search" {
		kind = sharedbridge.ToolKindToolSearch
		name = "tool_search"
	}
	if name == "" {
		return raw, false, nil
	}
	upstreamName, err := declaredUpstreamToolName(bridge.toolState, kind, namespace, name)
	if err != nil {
		return nil, false, err
	}
	encoded, err := kitutil.Marshal(map[string]any{"type": "function", "name": upstreamName})
	return encoded, true, err
}

func normalizeNativeResponsesToolOutput(item map[string]any, output any) {
	if text, ok := output.(string); ok {
		item["output"] = text
		return
	}
	if output == nil {
		item["output"] = ""
		return
	}
	encoded, err := kitutil.Marshal(output)
	if err != nil {
		item["output"] = ""
		return
	}
	item["output"] = string(encoded)
}

func nativeResponsesHostedToolKey(tool map[string]any) string {
	toolType := strings.TrimSpace(kitutil.Interface2String(tool["type"]))
	name := strings.TrimSpace(kitutil.Interface2String(tool["name"]))
	if name == "" {
		name = strings.TrimSpace(kitutil.Interface2String(tool["server_label"]))
	}
	if toolType != "" && name != "" {
		return toolType + "\x00" + name
	}
	encoded, _ := kitutil.Marshal(tool)
	return string(encoded)
}

func cloneResponsesToolMap(tool map[string]any) map[string]any {
	clone := make(map[string]any, len(tool))
	for key, value := range tool {
		clone[key] = value
	}
	return clone
}

// HasMappings reports whether any response-side restoration is required.
func (b *ResponsesClientToolBridge) HasMappings() bool {
	return b != nil && b.restoreNeeded
}

// RestoreResponseData restores function calls in a non-stream Responses JSON
// payload while preserving fields unknown to the DTO layer.
func (b *ResponsesClientToolBridge) RestoreResponseData(data []byte) ([]byte, error) {
	if b == nil || !b.restoreNeeded || len(bytes.TrimSpace(data)) == 0 {
		return data, nil
	}
	var value any
	if err := kitutil.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if !b.restoreValue(value) {
		return data, nil
	}
	return kitutil.Marshal(value)
}

func (b *ResponsesClientToolBridge) restoreValue(value any) bool {
	if b == nil || b.toolState == nil {
		return false
	}
	changed := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			changed = b.restoreValue(item) || changed
		}
	case map[string]any:
		if strings.TrimSpace(kitutil.Interface2String(typed["type"])) == "function_call" {
			name := strings.TrimSpace(kitutil.Interface2String(typed["name"]))
			if identity, ok := b.toolState.ResolveUpstream(name); ok {
				changed = restoreResponsesFunctionCallMap(typed, identity, "") || changed
			}
		}
		for _, child := range typed {
			changed = b.restoreValue(child) || changed
		}
	}
	return changed
}

func restoreResponsesFunctionCallMap(item map[string]any, identity sharedbridge.ToolIdentity, arguments string) bool {
	if item == nil {
		return false
	}
	if arguments == "" {
		arguments = nativeResponsesArgumentsString(item["arguments"])
	}
	switch identity.Kind {
	case sharedbridge.ToolKindCustom:
		item["type"] = "custom_tool_call"
		item["name"] = identity.Name
		item["input"] = sharedbridge.DecodeCustomInput(arguments)
		delete(item, "arguments")
		if identity.Namespace != "" {
			item["namespace"] = identity.Namespace
		} else {
			delete(item, "namespace")
		}
		return true
	case sharedbridge.ToolKindToolSearch:
		item["type"] = "tool_search_call"
		item["execution"] = "client"
		delete(item, "name")
		delete(item, "namespace")
		return true
	case sharedbridge.ToolKindFunction:
		if identity.Namespace == "" || identity.Name == strings.TrimSpace(kitutil.Interface2String(item["name"])) {
			return false
		}
		item["name"] = identity.Name
		item["namespace"] = identity.Namespace
		return true
	default:
		return false
	}
}

func nativeResponsesArgumentsString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	if value == nil {
		return ""
	}
	encoded, err := kitutil.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// NewStreamRestorer creates isolated state for one upstream SSE response.
func (b *ResponsesClientToolBridge) NewStreamRestorer() *ResponsesClientToolStreamRestorer {
	return &ResponsesClientToolStreamRestorer{
		bridge:        b,
		callsByID:     make(map[string]*responsesClientToolStreamCall),
		callsByOutput: make(map[int]*responsesClientToolStreamCall),
	}
}

// RestoreData transforms one Responses SSE data JSON payload into zero or more
// downstream payloads. Function argument events for custom/tool_search proxies
// are buffered and replaced with the matching Codex lifecycle.
func (r *ResponsesClientToolStreamRestorer) RestoreData(data []byte) ([][]byte, error) {
	if r == nil || r.bridge == nil || !r.bridge.restoreNeeded {
		return [][]byte{data}, nil
	}
	var event map[string]any
	if err := kitutil.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	r.beginSequence(event)
	eventType := strings.TrimSpace(kitutil.Interface2String(event["type"]))
	switch eventType {
	case "response.output_item.added", "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		call := r.recordCall(event, item)
		if call != nil {
			arguments := call.arguments.String()
			if eventType == "response.output_item.done" {
				if itemArguments := nativeResponsesArgumentsString(item["arguments"]); itemArguments != "" {
					arguments = itemArguments
				}
			}
			restoreResponsesFunctionCallMap(item, call.identity, arguments)
			if eventType == "response.output_item.added" && call.identity.Kind == sharedbridge.ToolKindCustom {
				item["input"] = ""
			}
			if eventType == "response.output_item.done" {
				r.removeCall(call)
			}
		} else if item != nil {
			r.bridge.restoreValue(item)
		}
		return r.encodeEvents(event)
	case "response.function_call_arguments.delta":
		if call := r.callForEvent(event); call != nil && call.identity.Kind != sharedbridge.ToolKindFunction {
			call.arguments.WriteString(kitutil.Interface2String(event["delta"]))
			return nil, nil
		}
		r.restoreArgumentEventName(event)
		return r.encodeEvents(event)
	case "response.function_call_arguments.done":
		if call := r.callForEvent(event); call != nil && call.identity.Kind != sharedbridge.ToolKindFunction {
			if arguments := nativeResponsesArgumentsString(event["arguments"]); arguments != "" {
				call.arguments.Reset()
				call.arguments.WriteString(arguments)
			}
			if call.identity.Kind == sharedbridge.ToolKindToolSearch {
				return nil, nil
			}
			input := sharedbridge.DecodeCustomInput(call.arguments.String())
			events := make([]map[string]any, 0, 2)
			if input != "" {
				events = append(events, map[string]any{
					"type":         "response.custom_tool_call_input.delta",
					"output_index": call.outputIndex,
					"item_id":      call.itemID,
					"delta":        input,
				})
			}
			events = append(events, map[string]any{
				"type":         "response.custom_tool_call_input.done",
				"output_index": call.outputIndex,
				"item_id":      call.itemID,
				"input":        input,
			})
			return r.encodeEvents(events...)
		}
		r.restoreArgumentEventName(event)
		return r.encodeEvents(event)
	default:
		r.bridge.restoreValue(event)
		return r.encodeEvents(event)
	}
}

func (r *ResponsesClientToolStreamRestorer) recordCall(event map[string]any, item map[string]any) *responsesClientToolStreamCall {
	if item == nil || strings.TrimSpace(kitutil.Interface2String(item["type"])) != "function_call" {
		return nil
	}
	identity, ok := r.bridge.toolState.ResolveUpstream(strings.TrimSpace(kitutil.Interface2String(item["name"])))
	if !ok {
		return nil
	}
	itemID := strings.TrimSpace(kitutil.Interface2String(item["id"]))
	callID := strings.TrimSpace(kitutil.Interface2String(item["call_id"]))
	outputIndex := nativeResponsesInt(event["output_index"])
	call := r.callsByID[itemID]
	if call == nil && callID != "" {
		call = r.callsByID[callID]
	}
	if call == nil {
		call = &responsesClientToolStreamCall{
			identity:    identity,
			itemID:      itemID,
			callID:      callID,
			outputIndex: outputIndex,
		}
		if itemID != "" {
			r.callsByID[itemID] = call
		}
		if callID != "" {
			r.callsByID[callID] = call
		}
		r.callsByOutput[outputIndex] = call
	}
	if arguments := nativeResponsesArgumentsString(item["arguments"]); arguments != "" {
		call.arguments.Reset()
		call.arguments.WriteString(arguments)
	}
	return call
}

func (r *ResponsesClientToolStreamRestorer) callForEvent(event map[string]any) *responsesClientToolStreamCall {
	itemID := strings.TrimSpace(kitutil.Interface2String(event["item_id"]))
	if call := r.callsByID[itemID]; call != nil {
		return call
	}
	callID := strings.TrimSpace(kitutil.Interface2String(event["call_id"]))
	if call := r.callsByID[callID]; call != nil {
		return call
	}
	return r.callsByOutput[nativeResponsesInt(event["output_index"])]
}

func (r *ResponsesClientToolStreamRestorer) removeCall(call *responsesClientToolStreamCall) {
	if call == nil {
		return
	}
	delete(r.callsByID, call.itemID)
	delete(r.callsByID, call.callID)
	delete(r.callsByOutput, call.outputIndex)
}

func (r *ResponsesClientToolStreamRestorer) restoreArgumentEventName(event map[string]any) {
	name := strings.TrimSpace(kitutil.Interface2String(event["name"]))
	identity, ok := r.bridge.toolState.ResolveUpstream(name)
	if !ok || identity.Kind != sharedbridge.ToolKindFunction || identity.Namespace == "" {
		return
	}
	event["name"] = identity.Name
}

func (r *ResponsesClientToolStreamRestorer) beginSequence(event map[string]any) {
	if r.sequenceStarted {
		return
	}
	value, exists := event["sequence_number"]
	if !exists {
		return
	}
	r.nextSequence = nativeResponsesInt(value)
	r.sequenceStarted = true
}

func (r *ResponsesClientToolStreamRestorer) encodeEvents(events ...map[string]any) ([][]byte, error) {
	encoded := make([][]byte, 0, len(events))
	for _, event := range events {
		if r.sequenceStarted {
			event["sequence_number"] = r.nextSequence
			r.nextSequence++
		}
		payload, err := kitutil.Marshal(event)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, payload)
	}
	return encoded, nil
}

func nativeResponsesInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}
