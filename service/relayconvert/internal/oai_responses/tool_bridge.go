package oairesponses

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	sharedbridge "github.com/QuantumNous/new-api/service/relayconvert/internal/shared/bridge"
	"github.com/gin-gonic/gin"
)

const chatToolNameLimit = 64

func prepareResponsesToolsForChat(c *gin.Context, req *dto.OpenAIResponsesRequest) ([]dto.ToolCallRequest, *sharedbridge.ToolState, error) {
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
		if err := common.Unmarshal(req.Tools, &declared); err != nil {
			return nil, fmt.Errorf("invalid tools: %w", err)
		}
		tools = append(tools, declared...)
	}
	if !rawJSONPresent(req.Input) || common.GetJsonType(req.Input) != "array" {
		return tools, nil
	}
	var inputItems []map[string]any
	if err := common.Unmarshal(req.Input, &inputItems); err != nil {
		return nil, fmt.Errorf("invalid input array: %w", err)
	}
	for _, item := range inputItems {
		if strings.TrimSpace(common.Interface2String(item["type"])) != "additional_tools" {
			continue
		}
		additional, err := toolMapsFromAny(item["tools"])
		if err != nil {
			return nil, fmt.Errorf("invalid additional_tools: %w", err)
		}
		tools = append(tools, additional...)
	}
	return tools, nil
}

func toolMapsFromAny(value any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var tools []map[string]any
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func responsesToolToChatFunctions(tool map[string]any, namespace string, state *sharedbridge.ToolState) ([]dto.ToolCallRequest, error) {
	toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
	if toolType == "" {
		toolType = "function"
	}
	name := strings.TrimSpace(common.Interface2String(tool["name"]))

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
		return nil, fmt.Errorf("Responses tool type %q requires a native Responses upstream", toolType)
	}

	if toolType == "tool_search" {
		if namespace != "" {
			return nil, fmt.Errorf("tool_search cannot be nested in namespace %q", namespace)
		}
		if execution := strings.TrimSpace(common.Interface2String(tool["execution"])); execution != "client" {
			return nil, fmt.Errorf("server-executed tool_search requires a native Responses upstream")
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

	description := common.Interface2String(tool["description"])
	parameters := tool["parameters"]
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
	} else if parameters == nil {
		parameters = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
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
			},
		},
	}, nil
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
		raw, err := common.Marshal(value)
		if err == nil {
			value = string(raw)
		}
	}
	raw, err := common.Marshal(map[string]any{"input": value})
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
		if common.Unmarshal([]byte(text), &parsed) == nil {
			return text
		}
	}
	raw, err := common.Marshal(arguments)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeCustomToolInput(arguments string) string {
	var wrapper map[string]any
	if common.Unmarshal([]byte(arguments), &wrapper) == nil {
		if input, ok := wrapper["input"].(string); ok {
			return input
		}
		if input, exists := wrapper["input"]; exists {
			raw, err := common.Marshal(input)
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
	if common.Unmarshal([]byte(trimmed), &value) == nil {
		return json.RawMessage(trimmed)
	}
	raw, _ := common.Marshal(map[string]any{"input": arguments})
	return raw
}
