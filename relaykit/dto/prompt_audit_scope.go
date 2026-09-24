package dto

import "strings"

// appendScopeMessage keeps protocol parts independently selectable even when a
// provider embeds tool results inside a user message or calls inside a model turn.
func appendScopeMessage(segments []PromptAuditSegment, scope PromptAuditScope, role string, texts []string) []PromptAuditSegment {
	parts := appendRoleMessage(nil, role, scope == PromptScopeUser || scope == PromptScopeTask, texts)
	for _, part := range parts {
		if scope != PromptScopeUser && scope != PromptScopeSystem && scope != PromptScopeDeveloper || (part.Scope != PromptScopeAgentContext && part.Scope != PromptScopeSkill) {
			part.Scope = scope
		}
		switch scope {
		case PromptScopeToolCall:
			part.ToolPart = "call"
		case PromptScopeToolResult:
			part.ToolPart = "result"
		}
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
		case kind == "tool_result" || kind == "mcp_tool_result" || strings.HasSuffix(kind, "_call_output"):
			payload := content["content"]
			if payload == nil {
				payload = content["output"]
			}
			id, _ := content["tool_use_id"].(string)
			if id == "" {
				id, _ = content["call_id"].(string)
			}
			parts := promptAuditToolSegments(PromptScopeToolResult, "tool", "", id, structuredPromptAuditTexts(payload))
			if kind == "mcp_tool_result" {
				for index := range parts {
					parts[index].Scope = PromptScopeMCP
				}
			}
			return parts
		case kind == "tool_use" || kind == "mcp_tool_use" || kind == "function_call" || kind == "custom_tool_call":
			payload := content["input"]
			if payload == nil {
				payload = content["arguments"]
			}
			name, _ := content["name"].(string)
			id, _ := content["id"].(string)
			parts := promptAuditToolSegments(PromptScopeToolCall, "assistant", name, id, structuredPromptAuditTexts(payload))
			if kind == "mcp_tool_use" {
				for index := range parts {
					parts[index].Scope = PromptScopeMCP
				}
			}
			return parts
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
			result = append(result, promptAuditToolSegments(PromptScopeToolCall, "assistant", part.FunctionCall.FunctionName, part.FunctionCall.ID, orderedStringLeaves(part.FunctionCall.Arguments))...)
		}
		if part.FunctionResponse != nil {
			texts := orderedStringLeaves(part.FunctionResponse.Response)
			texts = append(texts, rawTextValues(part.FunctionResponse.Parts)...)
			result = append(result, promptAuditToolSegments(PromptScopeToolResult, "tool", part.FunctionResponse.Name, promptAuditJSONString(part.FunctionResponse.ID), texts)...)
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
			if last.SourceScope() == part.SourceScope() && last.Role == part.Role && last.ToolPart == part.ToolPart && last.ToolID == part.ToolID && last.ToolName == part.ToolName && !part.ToolRoundStart && last.ToolDefinition == part.ToolDefinition {
				last.Text = merged
				continue
			}
		}
		result = append(result, part)
	}
	return result
}

// markToolRoundStart flags the first tool call among the segments of one
// assistant response: every call a response issues belongs to one tool round.
// A response that issued no call is returned unchanged.
func markToolRoundStart(segments []PromptAuditSegment) []PromptAuditSegment {
	for index := range segments {
		if segments[index].SourceScope() == PromptScopeToolCall || segments[index].ToolPart == "call" {
			segments[index].ToolRoundStart = true
			break
		}
	}
	return segments
}
