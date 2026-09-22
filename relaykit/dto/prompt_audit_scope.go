package dto

import "strings"

// appendScopeMessage keeps protocol parts independently selectable even when a
// provider embeds tool results inside a user message or calls inside a model turn.
func appendScopeMessage(segments []PromptAuditSegment, scope PromptAuditScope, role string, texts []string) []PromptAuditSegment {
	parts := appendRoleMessage(nil, role, scope == PromptScopeUser || scope == PromptScopeTask, texts)
	for _, part := range parts {
		part.Scope = scope
		segments = append(segments, part)
	}
	return segments
}

func scopedContentSegments(role string, value any) []PromptAuditSegment {
	scope := (PromptAuditSegment{Role: role}).SourceScope()
	var result []PromptAuditSegment
	switch content := value.(type) {
	case []any:
		for _, part := range content {
			result = append(result, scopedContentSegments(role, part)...)
		}
	case map[string]any:
		kind, _ := content["type"].(string)
		kind = strings.ToLower(strings.TrimSpace(kind))
		switch {
		case kind == "tool_result" || strings.HasSuffix(kind, "_call_output"):
			payload := content["content"]
			if payload == nil {
				payload = content["output"]
			}
			return appendScopeMessage(nil, PromptScopeToolResult, "tool", structuredPromptAuditTexts(payload))
		case kind == "tool_use" || kind == "function_call" || kind == "custom_tool_call":
			payload := content["input"]
			if payload == nil {
				payload = content["arguments"]
			}
			return appendScopeMessage(nil, PromptScopeToolCall, "assistant", structuredPromptAuditTexts(payload))
		case kind == "thinking":
			return appendScopeMessage(nil, PromptScopeAssistant, "assistant", anyTextValues(content, false))
		default:
			return appendScopeMessage(nil, scope, role, anyTextValues(content, false))
		}
	default:
		texts := anyTextValues(value, false)
		if scope == PromptScopeToolResult {
			texts = structuredPromptAuditTexts(value)
		}
		return appendScopeMessage(nil, scope, role, texts)
	}
	return mergePromptAuditParts(result)
}

func scopedGeminiSegments(role string, parts []GeminiPart) []PromptAuditSegment {
	var result []PromptAuditSegment
	scope := (PromptAuditSegment{Role: role}).SourceScope()
	for _, part := range parts {
		if part.Thought {
			result = append(result, PromptAuditSegment{
				Role:  role,
				Scope: PromptScopeAssistant,
				Text:  part.Text,
				User:  false,
			})
		} else {
			result = appendScopeMessage(result, scope, role, []string{part.Text})
		}
		if part.FunctionCall != nil {
			result = appendScopeMessage(result, PromptScopeToolCall, "assistant", orderedStringLeaves(part.FunctionCall.Arguments))
		}
		if part.FunctionResponse != nil {
			texts := orderedStringLeaves(part.FunctionResponse.Response)
			texts = append(texts, rawTextValues(part.FunctionResponse.Parts)...)
			result = appendScopeMessage(result, PromptScopeToolResult, "tool", texts)
		}
		if part.ExecutableCode != nil {
			result = appendScopeMessage(result, PromptScopeToolCall, "assistant", []string{part.ExecutableCode.Code})
		}
		if part.CodeExecutionResult != nil {
			result = appendScopeMessage(result, PromptScopeToolResult, "tool", []string{part.CodeExecutionResult.Output})
		}
	}
	return mergePromptAuditParts(result)
}

// Merge only neighboring parts from the same message and source. Callers append
// each message separately so text cannot be matched across message boundaries.
func mergePromptAuditParts(parts []PromptAuditSegment) []PromptAuditSegment {
	result := make([]PromptAuditSegment, 0, len(parts))
	for _, part := range parts {
		if len(result) > 0 {
			last := &result[len(result)-1]
			merged := last.Text + "\n" + part.Text
			if last.SourceScope() == part.SourceScope() && last.Role == part.Role {
				last.Text = merged
				continue
			}
		}
		result = append(result, part)
	}
	return result
}
