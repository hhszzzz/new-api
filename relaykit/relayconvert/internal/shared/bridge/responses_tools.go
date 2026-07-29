package bridge

import (
	"fmt"
	"strings"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// EnsureResponsesFunctionTools keeps historical function calls valid when a
// Messages or Chat request is converted to a strict Responses upstream. Some
// clients omit declarations for tools that only appear in replayed history;
// Responses still validates those calls against the top-level tools array.
func EnsureResponsesFunctionTools(tools []map[string]any, input []map[string]any) ([]map[string]any, error) {
	declared := make(map[string]string, len(tools))
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		toolType := strings.TrimSpace(kitutil.Interface2String(tool["type"]))
		if toolType == "" {
			toolType = "function"
		}
		name := strings.TrimSpace(kitutil.Interface2String(tool["name"]))
		if name == "" || (toolType != "function" && toolType != "custom" && toolType != "freeform") {
			result = append(result, tool)
			continue
		}
		kind := toolType
		if kind == "freeform" {
			kind = "custom"
		}
		if existing, ok := declared[name]; ok {
			if existing != kind {
				return nil, fmt.Errorf("Responses tool %q is declared as both %s and %s", name, existing, kind)
			}
			continue
		}
		declared[name] = kind
		result = append(result, tool)
	}

	for _, item := range input {
		if strings.TrimSpace(kitutil.Interface2String(item["type"])) != "function_call" {
			continue
		}
		name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
		if name == "" {
			return nil, fmt.Errorf("Responses historical function_call is missing name")
		}
		if kind, ok := declared[name]; ok {
			if kind != "function" {
				return nil, fmt.Errorf("Responses historical function call %q conflicts with declared %s tool", name, kind)
			}
			continue
		}
		declared[name] = "function"
		result = append(result, map[string]any{
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
	return result, nil
}
