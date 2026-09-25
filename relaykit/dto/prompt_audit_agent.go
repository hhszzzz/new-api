package dto

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// The wrapper markup a client injects around a slash command. Like the other
// context tags it is client-controlled text, but unlike them it carries the turn
// the user actually issued: it is classified as context and still reads as the
// latest human turn, see isPromptAuditHumanTurn.
var promptAuditCommandTags = []string{"<command-name>", "<command-message>", "<command-args>"}

func promptAuditCommandWrapper(text string) bool {
	for _, tag := range promptAuditCommandTags {
		if strings.HasPrefix(text, tag) {
			return true
		}
	}
	return false
}

// classifyPromptAuditText classifies a whole text block. These client-controlled
// wrappers are hints, never trusted evidence that the content is safe.
func classifyPromptAuditText(text, role string) PromptAuditScope {
	base := (PromptAuditSegment{Role: role}).SourceScope()
	if base != PromptScopeUser && base != PromptScopeSystem && base != PromptScopeDeveloper {
		return base
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "Base directory for this skill:") || strings.HasPrefix(text, "The following skills are available") || strings.HasPrefix(text, "<skills_instructions>") {
		return PromptScopeSkill
	}
	if base != PromptScopeUser {
		return base
	}
	if promptAuditCommandWrapper(text) {
		return PromptScopeAgentContext
	}
	for _, prefix := range []string{"<system-reminder>", "<ide_opened_file>", "<ide_selection>", "<local-command-", "Caveat: The messages below were generated", "# AGENTS.md instructions for", "<environment_context>", "<user_instructions>", "<turn_aborted>"} {
		if strings.HasPrefix(text, prefix) {
			return PromptScopeAgentContext
		}
	}
	return base
}

// HumanPrompt returns the text of the most recent user turn without its
// separately classified context and skill blocks.
func HumanPrompt(snapshot PromptAuditSnapshot) string {
	segments := snapshot.OrderedSegments()
	start := latestUserSegmentStart(segments)
	if start < 0 {
		return ""
	}
	var texts []string
	for _, segment := range segments[start:] {
		if isPromptAuditHumanTurn(segment) {
			texts = append(texts, segment.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func completePromptAuditSnapshot(snapshot PromptAuditSnapshot, tools any, session string) PromptAuditSnapshot {
	type callInfo struct {
		name  string
		skill bool
	}
	calls := make(map[string]callInfo)
	for index := range snapshot.Segments {
		segment := &snapshot.Segments[index]
		if segment.ToolPart != "" {
			snapshot.HasHistory = true
		}
		if segment.ToolPart == "call" {
			info := callInfo{name: segment.ToolName, skill: strings.Contains(strings.ToLower(segment.Text), "skill.md")}
			if segment.ToolID != "" {
				calls["id:"+segment.ToolID] = info
			}
			if segment.ToolName != "" {
				calls["name:"+segment.ToolName] = info
			}
		}
		if segment.ToolPart == "result" {
			key := "id:" + segment.ToolID
			if segment.ToolID == "" {
				key = "name:" + segment.ToolName
			}
			if info, ok := calls[key]; ok {
				segment.ToolName = info.name
				if info.skill {
					segment.Scope = PromptScopeSkill
				}
			}
		}
		if segment.Scope != PromptScopeSkill && strings.HasPrefix(segment.ToolName, "mcp__") {
			segment.Scope = PromptScopeMCP
		}
	}
	snapshot.HumanPrompt = HumanPrompt(snapshot)
	snapshot.RequestKind = "prompt"
	for index := len(snapshot.Segments) - 1; index >= 0; index-- {
		segment := snapshot.Segments[index]
		if isPromptAuditUserSegment(segment) {
			break
		}
		if isPromptAuditToolSegment(segment) || isAssistantOutputSegment(segment) {
			snapshot.RequestKind = "step"
			break
		}
	}
	for _, marker := range []struct{ prefix, kind string }{
		{"<transcript>", "safety"}, {"Perform a web search for", "web_search"},
		{"Web page content:", "web_summary"}, {"Describe your most recent action", "status"},
		{"The user stepped away", "recap"}, {"CRITICAL: Respond with TEXT ONLY", "summary"},
		{"Please write a 5-10 word title for the following conversation", "title"},
		{"Generate a short title for this conversation", "title"},
	} {
		if strings.HasPrefix(snapshot.HumanPrompt, marker.prefix) {
			snapshot.RequestKind = "side:" + marker.kind
			snapshot.HumanPrompt = ""
			break
		}
	}
	for _, segment := range snapshot.Segments {
		if segment.SourceScope() == PromptScopeSystem && (strings.Contains(segment.Text, "You are a subagent") || strings.Contains(segment.Text, "You are an agent for Claude Code")) {
			snapshot.RequestKind = "subagent"
			snapshot.HumanPrompt = ""
			break
		}
	}
	if session = strings.TrimSpace(session); session != "" {
		digest := sha256.Sum256([]byte(session))
		snapshot.SessionKey = hex.EncodeToString(digest[:])
	}
	if tools != nil {
		data, err := kitutil.Marshal(tools)
		var decoded any
		if err == nil && kitutil.Unmarshal(data, &decoded) == nil {
			snapshot.Segments = append(snapshot.Segments, promptAuditMCPDefinitions(decoded, false)...)
		}
	}
	return snapshot
}

// Only tool descriptions and schemas are inspectable. Connector credentials,
// headers and URLs must never be copied into moderation payloads or records.
func promptAuditMCPDefinitions(value any, mcp bool) []PromptAuditSegment {
	switch typed := value.(type) {
	case []any:
		var result []PromptAuditSegment
		for _, item := range typed {
			result = append(result, promptAuditMCPDefinitions(item, mcp)...)
		}
		return result
	case map[string]any:
		if typed["type"] == "namespace" {
			children, exists := typed["tools"]
			if !exists {
				children = typed["children"]
			}
			name, _ := typed["name"].(string)
			return promptAuditMCPDefinitions(children, mcp || strings.HasPrefix(name, "mcp__"))
		}
		if functions, ok := typed["functionDeclarations"]; ok {
			return promptAuditMCPDefinitions(functions, mcp)
		}
		if functions, ok := typed["function_declarations"]; ok {
			return promptAuditMCPDefinitions(functions, mcp)
		}
		if function, ok := typed["function"]; ok {
			return promptAuditMCPDefinitions(function, mcp)
		}
		name, _ := typed["name"].(string)
		if !mcp && !strings.HasPrefix(name, "mcp__") {
			return nil
		}
		texts := []string{name}
		if description, ok := typed["description"].(string); ok {
			texts = append(texts, description)
		}
		for _, key := range []string{"input_schema", "inputSchema", "parameters", "parametersJsonSchema"} {
			if schema, ok := typed[key]; ok {
				// Keep property names as well as descriptions, but never arbitrary tool metadata.
				if data, err := kitutil.Marshal(schema); err == nil {
					texts = append(texts, string(data))
				}
			}
		}
		parts := appendScopeMessage(nil, PromptScopeMCP, "tool", texts)
		for index := range parts {
			parts[index].ToolDefinition = true
		}
		return parts
	}
	return nil
}

func promptAuditJSONString(raw json.RawMessage) string {
	var value string
	_ = kitutil.Unmarshal(raw, &value)
	return value
}

func promptAuditResponsesHistory(raw json.RawMessage) bool {
	var items []map[string]any
	if kitutil.Unmarshal(raw, &items) != nil {
		return false
	}
	users := 0
	for _, item := range items {
		kind, _ := item["type"].(string)
		role, _ := item["role"].(string)
		if role == "assistant" || role == "tool" || kind == "reasoning" || strings.HasSuffix(kind, "_call") || strings.HasSuffix(kind, "_call_output") {
			return true
		}
		if role == "user" {
			if promptAuditHasMedia(item["content"]) {
				users++
				continue
			}
			for _, segment := range scopedContentSegments(role, item["content"]) {
				if isPromptAuditUserSegment(segment) {
					users++
					break
				}
			}
		}
	}
	return users > 1
}

func promptAuditClaudeSession(raw json.RawMessage) string {
	var metadata ClaudeMetadata
	if kitutil.Unmarshal(raw, &metadata) != nil {
		return ""
	}
	// Newer Claude Code versions put device/account/session fields into user_id.
	var identifier struct {
		SessionID string `json:"session_id"`
	}
	if kitutil.Unmarshal([]byte(metadata.UserId), &identifier) == nil && identifier.SessionID != "" {
		return identifier.SessionID
	}
	return metadata.UserId
}

func promptAuditToolSegments(scope PromptAuditScope, role, name, id string, texts []string) []PromptAuditSegment {
	segments := appendScopeMessage(nil, scope, role, texts)
	if len(segments) == 0 {
		part := "call"
		if scope == PromptScopeToolResult {
			part = "result"
		}
		segments = append(segments, PromptAuditSegment{Role: role, Scope: scope, ToolPart: part})
	}
	for index := range segments {
		segments[index].ToolName = name
		segments[index].ToolID = id
	}
	return segments
}

// Media makes a request more than a standalone greeting even though binary
// data is deliberately absent from the text inspection snapshot.
func promptAuditHasMedia(value any) bool {
	switch typed := value.(type) {
	case json.RawMessage:
		var decoded any
		if kitutil.Unmarshal(typed, &decoded) == nil {
			return promptAuditHasMedia(decoded)
		}
	case []any:
		for _, part := range typed {
			if promptAuditHasMedia(part) {
				return true
			}
		}
	case map[string]any:
		kind, _ := typed["type"].(string)
		switch kind {
		case "image", "image_url", "input_image", "input_audio", "audio", "video", "video_url", "input_video", "document", "file", "input_file":
			return true
		}
		return promptAuditHasMedia(typed["content"])
	}
	return false
}
