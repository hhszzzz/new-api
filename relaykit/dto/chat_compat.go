package dto

import (
	"encoding/json"
	"strings"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const legacyFunctionCallID = "call_0"

// LegacyFunctionCall is the pre-tools Chat Completions function_call shape.
// Arguments stays raw so compatible providers returning an object instead of
// the specified JSON string can still be normalized without losing data.
type LegacyFunctionCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func (c *LegacyFunctionCall) ToolCallRequest() ToolCallRequest {
	if c == nil {
		return ToolCallRequest{}
	}
	return ToolCallRequest{
		ID:   c.normalizedID(),
		Type: "function",
		Function: FunctionRequest{
			Name:      c.Name,
			Arguments: legacyFunctionArguments(c.Arguments),
		},
	}
}

func (c *LegacyFunctionCall) ToolCallResponse(index *int) ToolCallResponse {
	if c == nil {
		return ToolCallResponse{}
	}
	return ToolCallResponse{
		Index: index,
		ID:    c.normalizedID(),
		Type:  "function",
		Function: FunctionResponse{
			Name:      c.Name,
			Arguments: legacyFunctionArguments(c.Arguments),
		},
	}
}

func (c *LegacyFunctionCall) normalizedID() string {
	if c != nil && strings.TrimSpace(c.ID) != "" {
		return strings.TrimSpace(c.ID)
	}
	return legacyFunctionCallID
}

func legacyFunctionArguments(arguments json.RawMessage) string {
	if len(arguments) == 0 || string(arguments) == "null" {
		return ""
	}
	return kitutil.JsonRawMessageToString(arguments)
}

func extractChatReasoningText(reasoningContent *string, reasoning json.RawMessage, reasoningDetails json.RawMessage) string {
	if reasoningContent != nil && *reasoningContent != "" {
		return *reasoningContent
	}
	if text := reasoningValueText(reasoning, false); text != "" {
		return text
	}
	return reasoningValueText(reasoningDetails, true)
}

func reasoningValueText(raw json.RawMessage, allowArray bool) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var value any
	if err := kitutil.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return reasoningAnyText(value, allowArray)
}

func reasoningAnyText(value any, allowArray bool) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		if !allowArray {
			return ""
		}
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := reasoningAnyText(item, true); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	case map[string]any:
		for _, key := range []string{"content", "text", "summary"} {
			if text, ok := typed[key].(string); ok && text != "" {
				return text
			}
		}
		if parts, ok := typed["parts"].([]any); ok {
			return reasoningAnyText(parts, true)
		}
	}
	return ""
}
