package oairesponses

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const chatToolNameLimit = 64

func prepareResponsesToolsForChat(c context.Context, req *dto.OpenAIResponsesRequest) ([]dto.ToolCallRequest, *sharedbridge.ToolState, error) {
	tools, err := collectResponsesToolDeclarations(req)
	if err != nil {
		return nil, nil, err
	}
	if len(tools) == 0 {
		return nil, nil, nil
	}

	state := sharedbridge.NewToolState()
	chatTools := make([]dto.ToolCallRequest, 0, len(tools))
	for _, tool := range tools {
		converted, err := responsesToolToChatFunctions(tool, "", state)
		if err != nil {
			return nil, nil, err
		}
		chatTools = append(chatTools, converted...)
	}
	if len(chatTools) == 0 {
		return nil, nil, nil
	}
	sharedbridge.SetToolState(c, state)
	return chatTools, state, nil
}

func collectResponsesToolDeclarations(req *dto.OpenAIResponsesRequest) ([]map[string]any, error) {
	if req == nil {
		return nil, nil
	}
	tools := make([]map[string]any, 0)
	if rawJSONPresent(req.Tools) {
		var declared []map[string]any
		if err := kitutil.Unmarshal(req.Tools, &declared); err != nil {
			return nil, fmt.Errorf("invalid tools: %w", err)
		}
		tools = append(tools, declared...)
	}
	if !rawJSONPresent(req.Input) || kitutil.GetJsonType(req.Input) != "array" {
		return tools, nil
	}
	var inputItems []map[string]any
	if err := kitutil.Unmarshal(req.Input, &inputItems); err != nil {
		return nil, fmt.Errorf("invalid input array: %w", err)
	}
	for _, item := range inputItems {
		itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
		if itemType != "additional_tools" && itemType != "tool_search_output" {
			continue
		}
		additional, err := toolMapsFromAny(item["tools"])
		if err != nil {
			return nil, fmt.Errorf("invalid %s tools: %w", itemType, err)
		}
		tools = append(tools, additional...)
	}
	return tools, nil
}

func toolMapsFromAny(value any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := kitutil.Marshal(value)
	if err != nil {
		return nil, err
	}
	var tools []map[string]any
	if err := kitutil.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func responsesToolToChatFunctions(tool map[string]any, namespace string, state *sharedbridge.ToolState) ([]dto.ToolCallRequest, error) {
	toolType := strings.TrimSpace(kitutil.Interface2String(tool["type"]))
	if toolType == "" {
		toolType = "function"
	}
	name := strings.TrimSpace(kitutil.Interface2String(tool["name"]))

	switch toolType {
	case "namespace":
		if namespace != "" {
			return nil, fmt.Errorf("nested namespace tool %q is not supported by Chat conversion", name)
		}
		if name == "" {
			return nil, fmt.Errorf("namespace tool is missing name")
		}
		children, err := toolMapsFromAny(tool["tools"])
		if err != nil {
			return nil, fmt.Errorf("invalid namespace tool %q: %w", name, err)
		}
		if len(children) == 0 {
			return nil, fmt.Errorf("namespace tool %q has no child tools", name)
		}
		out := make([]dto.ToolCallRequest, 0, len(children))
		for _, child := range children {
			converted, err := responsesToolToChatFunctions(child, name, state)
			if err != nil {
				return nil, err
			}
			out = append(out, converted...)
		}
		return out, nil
	case "function", "custom", "freeform", "tool_search":
	default:
		if isDroppableResponsesHostedToolType(toolType) {
			return nil, nil
		}
		return nil, fmt.Errorf("Responses tool type %q requires a native Responses upstream", toolType)
	}

	if toolType == "tool_search" {
		if namespace != "" {
			return nil, fmt.Errorf("tool_search cannot be nested in namespace %q", namespace)
		}
		if execution := strings.TrimSpace(kitutil.Interface2String(tool["execution"])); execution != "" && execution != "client" {
			return nil, nil
		}
		name = "tool_search"
	}
	if name == "" {
		return nil, fmt.Errorf("Responses %s tool is missing name", toolType)
	}

	kind := sharedbridge.ToolKindFunction
	if toolType == "custom" || toolType == "freeform" {
		kind = sharedbridge.ToolKindCustom
	} else if toolType == "tool_search" {
		kind = sharedbridge.ToolKindToolSearch
	}
	upstreamName := encodedChatToolName(kind, namespace, name)
	identity := sharedbridge.ToolIdentity{
		Kind:         kind,
		Name:         name,
		Namespace:    namespace,
		UpstreamName: upstreamName,
	}
	if !state.Register(identity) {
		return nil, fmt.Errorf("Responses tool %q conflicts after Chat name encoding as %q", qualifiedToolName(namespace, name), upstreamName)
	}

	description := kitutil.Interface2String(tool["description"])
	parameters := normalizeChatFunctionParameters(tool["parameters"])
	var strict *bool
	if value, ok := tool["strict"].(bool); ok {
		strict = kitutil.GetPointer(value)
	}
	if kind == sharedbridge.ToolKindCustom {
		parameters = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Raw input for the custom tool.",
				},
			},
			"required":             []string{"input"},
			"additionalProperties": false,
		}
	}
	if kind == sharedbridge.ToolKindToolSearch && strings.TrimSpace(description) == "" {
		description = "Search and load tools available to the client for the current task."
	}

	return []dto.ToolCallRequest{
		{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        upstreamName,
				Description: description,
				Parameters:  parameters,
				Strict:      strict,
			},
		},
	}, nil
}

func normalizeChatFunctionParameters(parameters any) map[string]any {
	if source, ok := parameters.(map[string]any); ok {
		normalized := make(map[string]any, len(source)+1)
		for key, value := range source {
			normalized[key] = value
		}
		normalized["type"] = "object"
		return normalized
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
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
	} {
		if strings.HasPrefix(toolType, prefix) {
			return true
		}
	}
	return false
}

func isDroppableResponsesHostedHistoryItem(itemType string) bool {
	itemType = strings.ToLower(strings.TrimSpace(itemType))
	for _, suffix := range []string{"_call", "_call_output", "_approval_request", "_approval_response", "_list_tools"} {
		if strings.HasSuffix(itemType, suffix) && isDroppableResponsesHostedToolType(strings.TrimSuffix(itemType, suffix)) {
			return true
		}
	}
	return itemType == "program" || itemType == "program_output"
}

func encodedChatToolName(kind sharedbridge.ToolKind, namespace, name string) string {
	candidate := name
	if namespace != "" {
		candidate = namespace + "__" + name
	}
	if validChatToolName(candidate) && len(candidate) <= chatToolNameLimit {
		return candidate
	}
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + namespace + "\x00" + name))
	prefix := "newapi_tool_"
	if namespace != "" {
		prefix = "newapi_ns_"
	}
	return prefix + hex.EncodeToString(sum[:12])
}

func validChatToolName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func qualifiedToolName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

func upstreamToolName(state *sharedbridge.ToolState, kind sharedbridge.ToolKind, namespace, name string) (string, error) {
	if state == nil {
		return name, nil
	}
	upstream, ok := state.UpstreamName(kind, namespace, name)
	if !ok {
		return "", fmt.Errorf("Responses tool call references undeclared tool %q", qualifiedToolName(namespace, name))
	}
	return upstream, nil
}

func customInputArguments(input any) string {
	value := input
	if value == nil {
		value = ""
	}
	if _, ok := value.(string); !ok {
		raw, err := kitutil.Marshal(value)
		if err == nil {
			value = string(raw)
		}
	}
	raw, err := kitutil.Marshal(map[string]any{"input": value})
	if err != nil {
		return `{"input":""}`
	}
	return string(raw)
}

func toolSearchArguments(arguments any) string {
	if arguments == nil {
		return "{}"
	}
	if text, ok := arguments.(string); ok {
		var parsed any
		if kitutil.Unmarshal([]byte(text), &parsed) == nil {
			return text
		}
	}
	raw, err := kitutil.Marshal(arguments)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeCustomToolInput(arguments string) string {
	var wrapper map[string]any
	if kitutil.Unmarshal([]byte(arguments), &wrapper) == nil {
		if input, ok := wrapper["input"].(string); ok {
			return input
		}
		if input, exists := wrapper["input"]; exists {
			raw, err := kitutil.Marshal(input)
			if err == nil {
				return string(raw)
			}
		}
	}
	return arguments
}

func toolSearchArgumentsRaw(arguments string) json.RawMessage {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	var value any
	if kitutil.Unmarshal([]byte(trimmed), &value) == nil {
		return json.RawMessage(trimmed)
	}
	raw, _ := kitutil.Marshal(map[string]any{"input": arguments})
	return raw
}
