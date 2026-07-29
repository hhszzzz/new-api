package protocolstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service/channelcompat"
)

func normalizeResponsesRequest(request *dto.OpenAIResponsesRequest, upstreamProtocol channelcompat.Protocol, historicalTools []json.RawMessage) (bool, error) {
	normalized := false
	if upstreamProtocol == channelcompat.ProtocolResponses {
		preparedInput, inputChanged, err := prepareResponsesInputForNativeUpstream(request.Input)
		if err != nil {
			return false, err
		}
		request.Input = preparedInput
		normalized = inputChanged
	}
	toolsChanged, err := mergeResponsesToolDeclarations(request, historicalTools)
	if err != nil {
		return false, err
	}
	return normalized || toolsChanged, nil
}

type responsesToolSet struct {
	tools               []map[string]any
	identities          map[string]string
	namespaces          map[string]int
	hostedTools         map[string]struct{}
	toolSearchExecution string
}

func mergeResponsesToolDeclarations(request *dto.OpenAIResponsesRequest, historicalTools []json.RawMessage) (bool, error) {
	if request == nil {
		return false, nil
	}
	originalTools := append(json.RawMessage(nil), request.Tools...)
	set := &responsesToolSet{
		identities:  make(map[string]string),
		namespaces:  make(map[string]int),
		hostedTools: make(map[string]struct{}),
	}
	if err := set.addRawTools(request.Tools); err != nil {
		return false, fmt.Errorf("invalid Responses tools: %w", err)
	}
	if err := set.addInputToolCarriers(request.Input); err != nil {
		return false, err
	}
	for _, raw := range historicalTools {
		if err := set.addRawTools(raw); err != nil {
			return false, fmt.Errorf("invalid stored Responses tools: %w", err)
		}
	}
	if err := set.ensureInputCallsDeclared(request.Input); err != nil {
		return false, err
	}
	if len(set.tools) == 0 {
		return false, nil
	}
	encoded, err := common.Marshal(set.tools)
	if err != nil {
		return false, err
	}
	request.Tools = encoded
	return !responsesJSONEqual(originalTools, encoded), nil
}

func (s *responsesToolSet) addRawTools(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var value any
	if err := common.Unmarshal(trimmed, &value); err != nil {
		return err
	}
	tools, err := responsesToolMaps(value)
	if err != nil {
		return err
	}
	for _, tool := range tools {
		if err := s.addTopLevelTool(tool); err != nil {
			return err
		}
	}
	return nil
}

func (s *responsesToolSet) addInputToolCarriers(raw json.RawMessage) error {
	items, err := responsesInputItems(raw)
	if err != nil {
		return fmt.Errorf("invalid Responses input while collecting tools: %w", err)
	}
	for _, item := range items {
		itemType := strings.TrimSpace(common.Interface2String(item["type"]))
		if itemType != "additional_tools" && itemType != "tool_search_output" {
			continue
		}
		encoded, err := common.Marshal(item["tools"])
		if err != nil {
			return err
		}
		if err := s.addRawTools(encoded); err != nil {
			return fmt.Errorf("invalid %s declarations: %w", itemType, err)
		}
	}
	return nil
}

func (s *responsesToolSet) ensureInputCallsDeclared(raw json.RawMessage) error {
	items, err := responsesInputItems(raw)
	if err != nil {
		return fmt.Errorf("invalid Responses input while checking tool calls: %w", err)
	}
	for _, item := range items {
		itemType := strings.TrimSpace(common.Interface2String(item["type"]))
		name := strings.TrimSpace(common.Interface2String(item["name"]))
		namespace := strings.TrimSpace(common.Interface2String(item["namespace"]))
		var kind string
		switch itemType {
		case "function_call":
			kind = "function"
		case "custom_tool_call":
			kind = "custom"
		case "tool_search_call":
			execution := strings.ToLower(strings.TrimSpace(common.Interface2String(item["execution"])))
			if _, exists := s.identities[responsesToolIdentity("", "tool_search")]; exists {
				if s.toolSearchExecution != "" && execution != "" && s.toolSearchExecution != execution {
					return fmt.Errorf(
						"Responses tool_search call execution %q conflicts with declared execution %q",
						execution,
						s.toolSearchExecution,
					)
				}
				if s.toolSearchExecution == "" {
					s.toolSearchExecution = execution
				}
				continue
			}
			if execution != "" && execution != "client" {
				continue
			}
			if err := s.addTopLevelTool(map[string]any{
				"type":        "tool_search",
				"execution":   "client",
				"description": "Historical client tool search retained for replay.",
			}); err != nil {
				return err
			}
			continue
		default:
			continue
		}
		if name == "" {
			return fmt.Errorf("Responses %s item is missing a tool name", itemType)
		}
		identity := responsesToolIdentity(namespace, name)
		if declaredKind, exists := s.identities[identity]; exists {
			if declaredKind != kind {
				return fmt.Errorf("Responses tool call %q conflicts with declared %s tool", responsesQualifiedToolName(namespace, name), declaredKind)
			}
			continue
		}
		placeholder := map[string]any{
			"type":        kind,
			"name":        name,
			"description": "Historical tool retained for Responses replay.",
		}
		if kind == "function" {
			placeholder["parameters"] = map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": true,
			}
		} else {
			placeholder["format"] = map[string]any{"type": "text"}
		}
		if namespace == "" {
			if err := s.addTopLevelTool(placeholder); err != nil {
				return err
			}
			continue
		}
		if err := s.addNamespacedTool(namespace, placeholder); err != nil {
			return err
		}
	}
	return nil
}

func (s *responsesToolSet) addTopLevelTool(tool map[string]any) error {
	tool = normalizeResponsesFunctionDeclaration(tool)
	toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
	if toolType == "" {
		toolType = "function"
		tool["type"] = toolType
	}
	if toolType == "namespace" {
		return s.addNamespace(tool)
	}
	if toolType == "tool_search" {
		identity := responsesToolIdentity("", "tool_search")
		execution := strings.ToLower(strings.TrimSpace(common.Interface2String(tool["execution"])))
		if existing, exists := s.identities[identity]; exists {
			if existing != toolType {
				return fmt.Errorf("Responses tool_search conflicts with declared %s tool", existing)
			}
			if s.toolSearchExecution != "" && execution != "" && s.toolSearchExecution != execution {
				return fmt.Errorf(
					"Responses tool_search is declared with conflicting execution modes %q and %q",
					s.toolSearchExecution,
					execution,
				)
			}
			if s.toolSearchExecution == "" {
				s.toolSearchExecution = execution
			}
			return nil
		}
		s.identities[identity] = toolType
		s.toolSearchExecution = execution
		s.tools = append(s.tools, tool)
		return nil
	}
	if toolType != "function" && toolType != "custom" && toolType != "freeform" {
		key := responsesHostedToolKey(tool)
		if _, exists := s.hostedTools[key]; exists {
			return nil
		}
		s.hostedTools[key] = struct{}{}
		s.tools = append(s.tools, tool)
		return nil
	}
	name := strings.TrimSpace(common.Interface2String(tool["name"]))
	if name == "" {
		return fmt.Errorf("Responses %s tool is missing name", toolType)
	}
	kind := toolType
	if kind == "freeform" {
		kind = "custom"
	}
	identity := responsesToolIdentity("", name)
	if existing, exists := s.identities[identity]; exists {
		if existing != kind {
			return fmt.Errorf("Responses tool %q is declared as both %s and %s", name, existing, kind)
		}
		return nil
	}
	s.identities[identity] = kind
	s.tools = append(s.tools, tool)
	return nil
}

func (s *responsesToolSet) addNamespace(namespace map[string]any) error {
	name := strings.TrimSpace(common.Interface2String(namespace["name"]))
	if name == "" {
		return fmt.Errorf("Responses namespace tool is missing name")
	}
	childrenValue, exists := namespace["tools"]
	if !exists {
		childrenValue = namespace["children"]
	}
	children, err := responsesToolMaps(childrenValue)
	if err != nil {
		return fmt.Errorf("Responses namespace %q tools are invalid: %w", name, err)
	}
	for index := range children {
		children[index] = normalizeResponsesFunctionDeclaration(children[index])
	}
	if index, exists := s.namespaces[name]; exists {
		existingChildren, err := responsesToolMaps(s.tools[index]["tools"])
		if err != nil {
			return err
		}
		for _, child := range children {
			added, err := s.registerNamespacedTool(name, child)
			if err != nil {
				return err
			}
			if added {
				existingChildren = append(existingChildren, child)
			}
		}
		s.tools[index]["tools"] = existingChildren
		return nil
	}

	normalizedChildren := make([]map[string]any, 0, len(children))
	for _, child := range children {
		added, err := s.registerNamespacedTool(name, child)
		if err != nil {
			return err
		}
		if added {
			normalizedChildren = append(normalizedChildren, child)
		}
	}
	copy := make(map[string]any, len(namespace))
	for key, value := range namespace {
		copy[key] = value
	}
	delete(copy, "children")
	copy["tools"] = normalizedChildren
	s.namespaces[name] = len(s.tools)
	s.tools = append(s.tools, copy)
	return nil
}

func (s *responsesToolSet) addNamespacedTool(namespace string, tool map[string]any) error {
	if index, exists := s.namespaces[namespace]; exists {
		added, err := s.registerNamespacedTool(namespace, tool)
		if err != nil || !added {
			return err
		}
		children, err := responsesToolMaps(s.tools[index]["tools"])
		if err != nil {
			return err
		}
		s.tools[index]["tools"] = append(children, tool)
		return nil
	}
	return s.addNamespace(map[string]any{
		"type":        "namespace",
		"name":        namespace,
		"description": "Historical tool namespace retained for Responses replay.",
		"tools":       []map[string]any{tool},
	})
}

func (s *responsesToolSet) registerNamespacedTool(namespace string, tool map[string]any) (bool, error) {
	toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
	if toolType == "" {
		toolType = "function"
		tool["type"] = toolType
	}
	kind := toolType
	if kind == "freeform" {
		kind = "custom"
	}
	if kind != "function" && kind != "custom" {
		return false, fmt.Errorf("Responses namespace %q contains unsupported %s tool", namespace, toolType)
	}
	name := strings.TrimSpace(common.Interface2String(tool["name"]))
	if name == "" {
		return false, fmt.Errorf("Responses namespace %q contains a tool without a name", namespace)
	}
	identity := responsesToolIdentity(namespace, name)
	if existing, exists := s.identities[identity]; exists {
		if existing != kind {
			return false, fmt.Errorf("Responses tool %q is declared as both %s and %s", responsesQualifiedToolName(namespace, name), existing, kind)
		}
		return false, nil
	}
	s.identities[identity] = kind
	return true, nil
}

func prepareResponsesInputForNativeUpstream(raw json.RawMessage) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || common.GetJsonType(trimmed) == "string" {
		return raw, false, nil
	}
	switch common.GetJsonType(trimmed) {
	case "object":
		return prepareNativeResponsesItem(trimmed)
	case "array":
		var items []json.RawMessage
		if err := common.Unmarshal(trimmed, &items); err != nil {
			return nil, false, err
		}
		changed := false
		for index, item := range items {
			prepared, itemChanged, err := prepareNativeResponsesItem(item)
			if err != nil {
				return nil, false, err
			}
			items[index] = prepared
			changed = changed || itemChanged
		}
		if !changed {
			return raw, false, nil
		}
		prepared, err := common.Marshal(items)
		return prepared, true, err
	default:
		return raw, false, nil
	}
}

func responsesJSONEqual(left, right json.RawMessage) bool {
	left = bytes.TrimSpace(left)
	right = bytes.TrimSpace(right)
	leftEmpty := len(left) == 0 || bytes.Equal(left, []byte("null"))
	rightEmpty := len(right) == 0 || bytes.Equal(right, []byte("null"))
	if leftEmpty || rightEmpty {
		return leftEmpty && rightEmpty
	}
	var leftValue any
	if err := common.Unmarshal(left, &leftValue); err != nil {
		return bytes.Equal(left, right)
	}
	var rightValue any
	if err := common.Unmarshal(right, &rightValue); err != nil {
		return bytes.Equal(left, right)
	}
	leftCanonical, err := common.Marshal(leftValue)
	if err != nil {
		return bytes.Equal(left, right)
	}
	rightCanonical, err := common.Marshal(rightValue)
	if err != nil {
		return bytes.Equal(left, right)
	}
	return bytes.Equal(leftCanonical, rightCanonical)
}

func prepareNativeResponsesItem(raw json.RawMessage) (json.RawMessage, bool, error) {
	if common.GetJsonType(raw) != "object" {
		return raw, false, nil
	}
	var item map[string]any
	if err := common.Unmarshal(raw, &item); err != nil {
		return nil, false, err
	}
	itemType := strings.TrimSpace(common.Interface2String(item["type"]))
	if itemType == "" && strings.TrimSpace(common.Interface2String(item["role"])) != "" {
		itemType = "message"
	}
	id := strings.TrimSpace(common.Interface2String(item["id"]))
	if id == "" {
		return raw, false, nil
	}
	prefix := ""
	switch itemType {
	case "message":
		prefix = "msg_"
	case "function_call":
		prefix = "fc_"
	case "custom_tool_call":
		prefix = "ctc_"
	case "tool_search_call":
		prefix = "tsc_"
	case "reasoning":
		prefix = "rs_"
	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		delete(item, "id")
	default:
		return raw, false, nil
	}
	if prefix != "" && strings.HasPrefix(id, prefix) {
		return raw, false, nil
	}
	delete(item, "id")
	encoded, err := common.Marshal(item)
	return encoded, true, err
}

func responsesInputItems(raw json.RawMessage) ([]map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || common.GetJsonType(trimmed) == "string" {
		return nil, nil
	}
	if common.GetJsonType(trimmed) == "object" {
		var item map[string]any
		if err := common.Unmarshal(trimmed, &item); err != nil {
			return nil, err
		}
		return []map[string]any{item}, nil
	}
	if common.GetJsonType(trimmed) != "array" {
		return nil, fmt.Errorf("unsupported input JSON type %s", common.GetJsonType(trimmed))
	}
	var values []json.RawMessage
	if err := common.Unmarshal(trimmed, &values); err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if common.GetJsonType(value) != "object" {
			continue
		}
		var item map[string]any
		if err := common.Unmarshal(value, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func responsesToolMaps(value any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var entries []any
	if err := common.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	tools := make([]map[string]any, 0, len(entries))
	for index, entry := range entries {
		switch typed := entry.(type) {
		case map[string]any:
			tools = append(tools, typed)
		case string:
			name := strings.TrimSpace(typed)
			if name == "" {
				return nil, fmt.Errorf("tool %d has an empty string name", index)
			}
			tools = append(tools, map[string]any{"type": "custom", "name": name})
		default:
			return nil, fmt.Errorf("tool %d must be an object or string", index)
		}
	}
	return tools, nil
}

func normalizeResponsesFunctionDeclaration(tool map[string]any) map[string]any {
	if tool == nil || strings.TrimSpace(common.Interface2String(tool["type"])) != "function" {
		return tool
	}
	nested, ok := tool["function"].(map[string]any)
	if !ok {
		return tool
	}
	normalized := make(map[string]any, len(nested)+1)
	for key, value := range nested {
		normalized[key] = value
	}
	normalized["type"] = "function"
	for _, key := range []string{"name", "description", "parameters", "strict"} {
		if _, exists := normalized[key]; exists {
			continue
		}
		if value, exists := tool[key]; exists {
			normalized[key] = value
		}
	}
	return normalized
}

func responsesToolIdentity(namespace, name string) string {
	return strings.TrimSpace(namespace) + "\x00" + strings.TrimSpace(name)
}

func responsesQualifiedToolName(namespace, name string) string {
	if strings.TrimSpace(namespace) == "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(namespace) + "/" + strings.TrimSpace(name)
}

func responsesHostedToolKey(tool map[string]any) string {
	toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
	name := strings.TrimSpace(common.Interface2String(tool["name"]))
	if name == "" {
		name = strings.TrimSpace(common.Interface2String(tool["server_label"]))
	}
	if name != "" {
		return toolType + "\x00" + name
	}
	raw, _ := common.Marshal(tool)
	return string(raw)
}
