package dto

import (
	"encoding/json"
	"sort"
	"strings"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// PromptAuditSegment is one client-supplied textual content block. Binary data,
// credentials and gateway-owned state are excluded. MCP definitions are marked
// separately so their verdicts can be cached independently of conversation text.
type PromptAuditSegment struct {
	Role  string           `json:"role"`
	Text  string           `json:"text"`
	User  bool             `json:"user"`
	Scope PromptAuditScope `json:"scope,omitempty"`
	// ToolRoundStart marks the first tool call one assistant response issued:
	// every call and result after it, up to the next marked call, belongs to the
	// same round. It is structure read by BlockingSnapshot, not inspected text,
	// so it never leaves the gateway — payloads sent to audit nodes omit it.
	ToolRoundStart bool   `json:"-"`
	ToolPart       string `json:"-"`
	ToolDefinition bool   `json:"tool_definition,omitempty"`
	ToolName       string `json:"-"`
	ToolID         string `json:"-"`
}

type PromptAuditScope string

const (
	PromptScopeSystem       PromptAuditScope = "system"
	PromptScopeDeveloper    PromptAuditScope = "developer"
	PromptScopeUser         PromptAuditScope = "user"
	PromptScopeAssistant    PromptAuditScope = "assistant"
	PromptScopeToolCall     PromptAuditScope = "tool_call"
	PromptScopeToolResult   PromptAuditScope = "tool_result"
	PromptScopeTask         PromptAuditScope = "task"
	PromptScopeAgentContext PromptAuditScope = "agent_context"
	PromptScopeSkill        PromptAuditScope = "skill"
	PromptScopeMCP          PromptAuditScope = "mcp"
)

func PromptAuditScopes() []PromptAuditScope {
	return []PromptAuditScope{PromptScopeSystem, PromptScopeDeveloper, PromptScopeUser, PromptScopeAssistant, PromptScopeToolCall, PromptScopeToolResult, PromptScopeTask, PromptScopeAgentContext, PromptScopeSkill, PromptScopeMCP}
}

func (segment PromptAuditSegment) SourceScope() PromptAuditScope {
	if segment.Scope != "" {
		return segment.Scope
	}
	if segment.ToolPart == "call" {
		return PromptScopeToolCall
	}
	if segment.ToolPart == "result" {
		return PromptScopeToolResult
	}
	switch strings.ToLower(strings.TrimSpace(segment.Role)) {
	case "system":
		return PromptScopeSystem
	case "developer":
		return PromptScopeDeveloper
	case "assistant", "model":
		return PromptScopeAssistant
	case "tool", "function":
		return PromptScopeToolResult
	case "task":
		return PromptScopeTask
	default:
		return PromptScopeUser
	}
}

type PromptAuditSnapshot struct {
	Segments    []PromptAuditSegment `json:"segments"`
	HumanPrompt string               `json:"-"`
	RequestKind string               `json:"-"`
	SessionKey  string               `json:"-"`
	HasHistory  bool                 `json:"-"`
	HasMedia    bool                 `json:"-"`
}

// OrderedSegments normalizes inspectable text while preserving the original
// message order. It is the canonical representation sent to moderation nodes.
//
// Do NOT "align with sub2api" by adding whitespace collapsing or a rune cap
// here. sub2api ships two independent moderation pipelines, and this function
// mirrors the newer securityaudit one (prompt_snapshot.go's
// normalizedPromptSegments), which — like this function — only trims each
// segment. The two features commonly mistaken for a gap live only in sub2api's
// legacy content_moderation pipeline:
//
//   - strings.Fields whitespace collapsing (normalizeContentModerationText)
//   - the maxModerationInputRunes = 12000 hard truncation (trimRunes)
//
// The 12000 cap is not merely a different choice, it is a defect: it silently
// exempts the tail of any long prompt from inspection. Long inputs are covered
// here by splitPromptAuditRunes, which chunks with overlap so the tail is
// scanned. Adding a hard cap back would reopen that hole.
func (snapshot PromptAuditSnapshot) OrderedSegments() []PromptAuditSegment {
	normalized := make([]PromptAuditSegment, 0, len(snapshot.Segments))
	for _, segment := range snapshot.Segments {
		segment.Role = strings.ToLower(strings.TrimSpace(segment.Role))
		segment.Text = strings.TrimSpace(segment.Text)
		if segment.Text != "" || segment.ToolPart != "" {
			normalized = append(normalized, segment)
		}
	}
	return normalized
}

func (snapshot PromptAuditSnapshot) PrioritizedSegments() []PromptAuditSegment {
	normalized := snapshot.OrderedSegments()
	if len(normalized) == 0 {
		return nil
	}
	latestUser := -1
	for index := len(normalized) - 1; index >= 0; index-- {
		if isPromptAuditUserSegment(normalized[index]) || (normalized[index].SourceScope() == PromptScopeTask && normalized[index].User) {
			latestUser = index
			break
		}
	}
	if latestUser < 0 {
		return normalized
	}
	result := make([]PromptAuditSegment, 0, len(normalized))
	result = append(result, normalized[latestUser])
	for index, segment := range normalized {
		if index != latestUser {
			result = append(result, segment)
		}
	}
	return result
}

// SemanticSegments preserves all client-supplied text for model classification.
// Reminder and context tags are untrusted text, not proof of gateway ownership.
func (snapshot PromptAuditSnapshot) SemanticSegments() PromptAuditSnapshot {
	snapshot.Segments = snapshot.OrderedSegments()
	return snapshot
}

// BlockingSnapshot drops older conversation turns and superseded tool rounds,
// retaining the latest user turn, the assistant block that precedes it, the last
// tool round, and every remaining source in their original order.
//
// Tool calls and results are the only accumulating source: an agent resends its
// whole transcript on every step, so keeping their history would re-inspect the
// same tool output on every request. A round that an earlier request already
// carried as its newest was inspected then, so only the last round is carried
// forward: every tool call and result from the call that opened the newest
// round (see latestToolRound). That round is kept even when it precedes the
// latest user turn: it is the tool work the request continues.
//
// Sources an administrator selects independently (system, developer, task) are
// never narrowed: they are the request's own authority blocks rather than
// conversation history, and dropping them would silently disable a policy.
func (snapshot PromptAuditSnapshot) BlockingSnapshot() PromptAuditSnapshot {
	normalized := snapshot.OrderedSegments()
	userStart := latestUserSegmentStart(normalized)
	if userStart < 0 {
		// A request without user content cannot be narrowed safely. Preserve the
		// established full-snapshot behavior for unusual protocol payloads.
		snapshot.Segments = normalized
		return snapshot
	}
	assistantStart, assistantEnd := -1, -1
	for index := userStart - 1; index >= 0; index-- {
		if !isAssistantOutputSegment(normalized[index]) {
			continue
		}
		start := index
		for start > 0 && isAssistantOutputSegment(normalized[start-1]) {
			start--
		}
		assistantStart, assistantEnd = start, index
		break
	}
	toolStart, toolEnd := latestToolRound(normalized)
	selected := make([]PromptAuditSegment, 0, len(normalized))
	for index, segment := range normalized {
		scope := segment.SourceScope()
		switch {
		case isPromptAuditToolSegment(segment):
			if index >= toolStart && index <= toolEnd {
				selected = append(selected, segment)
			}
		case index >= userStart, index >= assistantStart && index <= assistantEnd:
			selected = append(selected, segment)
		default:
			// Every remaining source (system, developer, task) is the request's own
			// authority block; conversation text outside the retained turns is not.
			if scope != PromptScopeUser && scope != PromptScopeAssistant && !isPromptAuditUserContext(segment) {
				selected = append(selected, segment)
			}
		}
	}
	snapshot.Segments = selected
	return snapshot
}

// latestToolRound returns the bounds of the last tool round: the newest tool
// call or result, extended back over the contiguous tool calls and results
// before it up to the call that opened the round. A round opens at a call
// marked ToolRoundStart. A run of tool segments carrying no mark is kept whole:
// rounds that cannot be told apart are never split, because a parallel round is
// often recorded as call/result pairs, and splitting it would drop results that
// no earlier request carried. -1, -1 means the snapshot carries no tool content.
func latestToolRound(segments []PromptAuditSegment) (int, int) {
	end := -1
	for index := len(segments) - 1; index >= 0; index-- {
		if isPromptAuditToolSegment(segments[index]) {
			end = index
			break
		}
	}
	if end < 0 {
		return -1, -1
	}
	start := end
	for start > 0 && !segments[start].ToolRoundStart && isPromptAuditToolSegment(segments[start-1]) {
		start--
	}
	return start, end
}

func isPromptAuditToolSegment(segment PromptAuditSegment) bool {
	scope := segment.SourceScope()
	return scope == PromptScopeToolCall || scope == PromptScopeToolResult || segment.ToolPart == "call" || segment.ToolPart == "result"
}

func latestUserSegmentStart(segments []PromptAuditSegment) int {
	latest := -1
	for index := len(segments) - 1; index >= 0; index-- {
		if isPromptAuditHumanTurn(segments[index]) {
			latest = index
			break
		}
	}
	for latest > 0 && (isPromptAuditHumanTurn(segments[latest-1]) || isPromptAuditUserContext(segments[latest-1])) {
		latest--
	}
	return latest
}

func isPromptAuditUserSegment(segment PromptAuditSegment) bool {
	return segment.SourceScope() == PromptScopeUser && (segment.User || strings.EqualFold(strings.TrimSpace(segment.Role), "user"))
}

// A human turn is a user message, or the slash-command wrapper a client turns
// one into. The wrapper is context for the sake of policies, but it is still the
// turn the user issued: without it the newest command would read as the previous
// question, which is what grouping and the redacted preview take their text from.
func isPromptAuditHumanTurn(segment PromptAuditSegment) bool {
	if isPromptAuditUserSegment(segment) {
		return true
	}
	return isPromptAuditUserContext(segment) && promptAuditCommandWrapper(segment.Text)
}

func isPromptAuditUserContext(segment PromptAuditSegment) bool {
	return segment.ToolPart == "" && segment.Role == "user" && (segment.SourceScope() == PromptScopeAgentContext || segment.SourceScope() == PromptScopeSkill)
}

func isAssistantOutputSegment(segment PromptAuditSegment) bool {
	return segment.SourceScope() == PromptScopeAssistant
}

func (snapshot PromptAuditSnapshot) Text() string {
	segments := snapshot.PrioritizedSegments()
	texts := make([]string, 0, len(segments))
	for _, segment := range segments {
		texts = append(texts, segment.Text)
	}
	return strings.Join(texts, "\n\n")
}

type PromptAuditSnapshotProvider interface {
	GetPromptAuditSnapshot() PromptAuditSnapshot
}

func PromptAuditSnapshotOf(request Request) PromptAuditSnapshot {
	if request == nil {
		return PromptAuditSnapshot{}
	}
	if provider, ok := request.(PromptAuditSnapshotProvider); ok {
		return provider.GetPromptAuditSnapshot()
	}
	return PromptAuditSnapshot{Segments: userSegments(request.GetSensitiveText())}
}

// PromptAuditText remains as a compatibility helper for call sites that only
// need the normalized full text. New audit code should keep the snapshot so it
// can chunk the prioritized user segment independently from the remaining
// context.
func PromptAuditText(request Request) string {
	return PromptAuditSnapshotOf(request).Text()
}

func (r *GeneralOpenAIRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	segments := make([]PromptAuditSegment, 0, len(r.Messages)+4)
	hasHistory, hasMedia, userMessages := false, false, 0
	segments = appendRoleMessage(segments, "system", false, anyTextValues(r.Instruction, false))
	for index := range r.Messages {
		message := &r.Messages[index]
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "assistant" || role == "tool" || role == "function" {
			hasHistory = true
		}
		if !isPromptAuditRole(role) {
			continue
		}
		texts := make([]string, 0)
		messageHasMedia := false
		for _, content := range message.ParseContent() {
			if role == "user" && content.Type != ContentTypeText {
				messageHasMedia = true
				hasMedia = true
			}
			if content.Type == ContentTypeText && content.Text != "" {
				if role == "tool" || role == "function" {
					texts = append(texts, structuredPromptAuditTexts(content.Text)...)
				} else {
					texts = append(texts, content.Text)
				}
			}
		}
		messageSegments := appendRoleMessage(nil, role, role == "user", texts)
		if messageHasMedia {
			userMessages++
		}
		for part := range messageSegments {
			if !messageHasMedia && isPromptAuditUserSegment(messageSegments[part]) {
				userMessages++
				break
			}
		}
		if role == "tool" || role == "function" {
			for part := range messageSegments {
				messageSegments[part].ToolPart = "result"
				messageSegments[part].ToolID = message.ToolCallId
				if message.Name != nil {
					messageSegments[part].ToolName = *message.Name
				}
			}
		}
		messageSegments = appendScopeMessage(messageSegments, PromptScopeAssistant, "assistant", []string{message.GetReasoningContent(), message.GetRefusalContent()})
		for _, toolCall := range message.ParseToolCalls() {
			toolTexts := structuredPromptAuditTexts(toolCall.Function.Arguments)
			toolTexts = append(toolTexts, rawStructuredPromptAuditTexts(toolCall.Custom)...)
			name := toolCall.Function.Name
			if name == "" && len(toolCall.Custom) > 0 {
				var custom struct {
					Name string `json:"name"`
				}
				_ = kitutil.Unmarshal(toolCall.Custom, &custom)
				name = custom.Name
			}
			messageSegments = append(messageSegments, promptAuditToolSegments(PromptScopeToolCall, "assistant", name, toolCall.ID, toolTexts)...)
		}
		if message.FunctionCall != nil {
			messageSegments = append(messageSegments, promptAuditToolSegments(PromptScopeToolCall, "assistant", message.FunctionCall.Name, "", structuredPromptAuditTexts(message.FunctionCall.Arguments))...)
		}
		segments = append(segments, markToolRoundStart(mergePromptAuditParts(messageSegments))...)
	}
	if len(r.Messages) == 0 {
		for _, value := range []any{r.Prompt, r.Prefix, r.Suffix, r.Input} {
			segments = appendRoleTexts(segments, "task", true, anyTextValues(value, false))
		}
	}
	session := r.PromptCacheKey
	if session == "" {
		session = promptAuditJSONString(r.User)
	}
	return completePromptAuditSnapshot(PromptAuditSnapshot{Segments: segments, HasHistory: hasHistory || userMessages > 1, HasMedia: hasMedia}, r.Tools, session)
}

func (c *ClaudeRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if c == nil {
		return PromptAuditSnapshot{}
	}
	segments := scopedContentSegments("system", c.System)
	hasHistory, hasMedia, userMessages := false, false, 0
	if c.Prompt != "" {
		segments = appendRoleTexts(segments, "task", true, []string{c.Prompt})
	}
	for index := range c.Messages {
		message := &c.Messages[index]
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "assistant" {
			hasHistory = true
		}
		if !isPromptAuditRole(role) {
			continue
		}
		parts := markToolRoundStart(scopedContentSegments(role, message.Content))
		messageHasMedia := role == "user" && promptAuditHasMedia(message.Content)
		if messageHasMedia {
			hasMedia = true
			userMessages++
		}
		for _, part := range parts {
			if !messageHasMedia && isPromptAuditUserSegment(part) {
				userMessages++
				break
			}
		}
		for _, part := range parts {
			if isPromptAuditToolSegment(part) {
				hasHistory = true
			}
		}
		segments = append(segments, parts...)
	}
	return completePromptAuditSnapshot(PromptAuditSnapshot{Segments: segments, HasHistory: hasHistory || userMessages > 1, HasMedia: hasMedia}, c.Tools, promptAuditClaudeSession(c.Metadata))
}

func (r *GeminiChatRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	segments := make([]PromptAuditSegment, 0)
	hasHistory, hasMedia, userMessages := false, false, 0
	if r.SystemInstructions != nil {
		segments = append(segments, scopedGeminiSegments("system", r.SystemInstructions.Parts)...)
	}
	for index := range r.Contents {
		content := &r.Contents[index]
		role := strings.ToLower(strings.TrimSpace(content.Role))
		if role == "" {
			role = "user"
		}
		if role == "model" || role == "assistant" {
			hasHistory = true
		}
		if !isPromptAuditRole(role) {
			continue
		}
		parts := markToolRoundStart(scopedGeminiSegments(role, content.Parts))
		messageHasMedia := false
		if role == "user" {
			for _, part := range content.Parts {
				if part.InlineData != nil || part.FileData != nil {
					messageHasMedia = true
					hasMedia = true
					break
				}
			}
		}
		if messageHasMedia {
			userMessages++
		}
		for _, part := range parts {
			if !messageHasMedia && isPromptAuditUserSegment(part) {
				userMessages++
				break
			}
		}
		for _, part := range parts {
			if isPromptAuditToolSegment(part) {
				hasHistory = true
			}
		}
		segments = append(segments, parts...)
	}
	for index := range r.Requests {
		child := r.Requests[index].GetPromptAuditSnapshot()
		segments = append(segments, child.Segments...)
		hasMedia = hasMedia || child.HasMedia
		hasHistory = hasHistory || child.HasHistory
	}
	return completePromptAuditSnapshot(PromptAuditSnapshot{Segments: segments, HasHistory: hasHistory || userMessages > 1, HasMedia: hasMedia}, r.Tools, "")
}

func (r *GeminiEmbeddingRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	return PromptAuditSnapshot{Segments: scopedGeminiSegments("task", r.Content.Parts)}
}

func (r *GeminiBatchEmbeddingRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	segments := make([]PromptAuditSegment, 0)
	for _, request := range r.Requests {
		if request != nil {
			segments = append(segments, request.GetPromptAuditSnapshot().Segments...)
		}
	}
	return PromptAuditSnapshot{Segments: segments}
}

func (r *OpenAIResponsesRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	segments := appendRoleMessage(nil, "system", false, rawTextValues(r.Instructions))
	segments = append(segments, responsesInputSegments(r.Input)...)
	return completePromptAuditSnapshot(PromptAuditSnapshot{Segments: segments, HasHistory: r.PreviousResponseID != "" || promptAuditResponsesHistory(r.Input), HasMedia: promptAuditHasMedia(r.Input)}, r.Tools, promptAuditJSONString(r.PromptCacheKey))
}

func (r *OpenAIResponsesCompactionRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	segments := appendRoleMessage(nil, "system", false, rawTextValues(r.Instructions))
	segments = append(segments, responsesInputSegments(r.Input)...)
	return completePromptAuditSnapshot(PromptAuditSnapshot{Segments: segments, HasHistory: true}, r.Tools, promptAuditJSONString(r.PromptCacheKey))
}

func (r *EmbeddingRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	return PromptAuditSnapshot{Segments: appendRoleTexts(nil, "task", true, r.ParseInput())}
}

func (r *RerankRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	segments := appendRoleTexts(nil, "task", true, []string{r.Query})
	for _, document := range r.Documents {
		segments = appendRoleTexts(segments, "task", true, rerankDocumentTexts(document))
	}
	return PromptAuditSnapshot{Segments: segments}
}

func (r *ImageRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	return PromptAuditSnapshot{Segments: appendRoleTexts(nil, "task", true, []string{r.Prompt})}
}

func (r *AudioRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil {
		return PromptAuditSnapshot{}
	}
	segments := appendRoleMessage(nil, "system", false, []string{r.Instructions})
	segments = appendRoleTexts(segments, "task", true, []string{r.Input, r.AuditPrompt})
	segments = appendRoleTexts(segments, "task", true, rawTextValues(r.RefText))
	return PromptAuditSnapshot{Segments: segments}
}

func (r *AlphaSearchRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil || len(r.RawBody) == 0 {
		return PromptAuditSnapshot{}
	}
	var root map[string]any
	if kitutil.Unmarshal(r.RawBody, &root) != nil {
		return PromptAuditSnapshot{}
	}
	segments := appendRoleMessage(nil, "system", false, anyTextValues(root["instructions"], false))
	segments = append(segments, responsesValueSegments(root["input"])...)
	if commands, ok := root["commands"].(map[string]any); ok {
		if queries, ok := commands["search_query"].([]any); ok {
			for _, value := range queries {
				query, _ := value.(map[string]any)
				text, _ := query["q"].(string)
				segments = appendRoleTexts(segments, "task", true, []string{text})
			}
		}
	}
	return PromptAuditSnapshot{Segments: segments}
}

func appendRoleTexts(segments []PromptAuditSegment, role string, user bool, texts []string) []PromptAuditSegment {
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		segments = append(segments, PromptAuditSegment{Role: role, Text: text, User: user})
	}
	return segments
}

func appendRoleMessage(segments []PromptAuditSegment, role string, user bool, texts []string) []PromptAuditSegment {
	normalized := make([]PromptAuditSegment, 0, len(texts))
	for _, text := range texts {
		if strings.TrimSpace(text) != "" {
			scope := classifyPromptAuditText(text, role)
			normalized = append(normalized, PromptAuditSegment{Role: role, Text: text, User: user && (scope == PromptScopeUser || scope == PromptScopeTask), Scope: scope})
		}
	}
	return append(segments, mergePromptAuditParts(normalized)...)
}

func userSegments(text string) []PromptAuditSegment {
	return appendRoleTexts(nil, "user", true, []string{text})
}

func isPromptAuditRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "system", "developer", "assistant", "model", "tool", "function":
		return true
	default:
		return false
	}
}

func anyTextValues(value any, nestedToolResult bool) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, anyTextValues(item, nestedToolResult)...)
		}
		return result
	case map[string]any:
		typeName, _ := typed["type"].(string)
		typeName = strings.ToLower(strings.TrimSpace(typeName))
		if typeName == "" || typeName == "text" || typeName == "input_text" || typeName == "output_text" {
			if text, ok := typed["text"].(string); ok {
				return []string{text}
			}
		}
		if typeName == "thinking" {
			for _, key := range []string{"thinking", "text", "content"} {
				if content, exists := typed[key]; exists {
					return promptAuditReasoningTexts(content)
				}
			}
		}
		if nestedToolResult && (typeName == "tool_result" || strings.HasSuffix(typeName, "_call_output")) {
			if content, exists := typed["content"]; exists {
				return structuredPromptAuditTexts(content)
			}
			if output, exists := typed["output"]; exists {
				return structuredPromptAuditTexts(output)
			}
		}
		if nestedToolResult && (typeName == "tool_use" || typeName == "function_call" || typeName == "custom_tool_call") {
			for _, key := range []string{"input", "arguments"} {
				if payload, exists := typed[key]; exists {
					return structuredPromptAuditTexts(payload)
				}
			}
		}
	}
	return nil
}

func rawTextValues(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if kitutil.Unmarshal(raw, &value) != nil {
		return nil
	}
	return anyTextValues(value, true)
}

func rawStructuredPromptAuditTexts(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if kitutil.Unmarshal(raw, &value) != nil {
		return nil
	}
	return structuredPromptAuditTexts(value)
}

func claudeContentTexts(value any) []string {
	return anyTextValues(value, true)
}

func geminiPartTexts(parts []GeminiPart) []string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			result = append(result, part.Text)
		}
		if part.FunctionCall != nil {
			result = append(result, orderedStringLeaves(part.FunctionCall.Arguments)...)
		}
		if part.FunctionResponse != nil {
			result = append(result, orderedStringLeaves(part.FunctionResponse.Response)...)
			result = append(result, rawTextValues(part.FunctionResponse.Parts)...)
		}
		if part.ExecutableCode != nil && part.ExecutableCode.Code != "" {
			result = append(result, part.ExecutableCode.Code)
		}
		if part.CodeExecutionResult != nil && part.CodeExecutionResult.Output != "" {
			result = append(result, part.CodeExecutionResult.Output)
		}
	}
	return result
}

func orderedStringLeaves(value any) []string {
	return orderedStringLeavesForKey(value, "")
}

func structuredPromptAuditTexts(value any) []string {
	text, isString := value.(string)
	if !isString {
		return orderedStringLeaves(value)
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || looksLikePromptAuditBinary(trimmed) {
		return nil
	}
	var decoded any
	if kitutil.Unmarshal([]byte(trimmed), &decoded) == nil {
		return orderedStringLeaves(decoded)
	}
	return []string{text}
}

func promptAuditReasoningTexts(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			return []string{typed}
		}
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, promptAuditReasoningTexts(item)...)
		}
		return result
	case map[string]any:
		result := make([]string, 0)
		for _, key := range []string{"summary", "content", "text", "parts", "thinking"} {
			if child, exists := typed[key]; exists {
				result = append(result, promptAuditReasoningTexts(child)...)
			}
		}
		return result
	}
	return nil
}

func orderedStringLeavesForKey(value any, key string) []string {
	switch typed := value.(type) {
	case string:
		if promptAuditBinaryField(key) || looksLikePromptAuditBinary(typed) {
			return nil
		}
		return []string{typed}
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, orderedStringLeavesForKey(item, key)...)
		}
		return result
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make([]string, 0, len(keys))
		for _, key := range keys {
			if promptAuditExcludedStructuredField(key) {
				continue
			}
			result = append(result, orderedStringLeavesForKey(typed[key], key)...)
		}
		return result
	}
	return nil
}

func promptAuditExcludedStructuredField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "audio", "audio_url", "encrypted_content", "file", "file_data", "image", "image_url", "inline_data", "input_audio", "input_image", "metadata", "thought_signature", "video", "video_url":
		return true
	default:
		return false
	}
}

func promptAuditBinaryField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "base64", "bytes", "blob":
		return true
	default:
		return false
	}
}

func looksLikePromptAuditBinary(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "data:image/") || strings.HasPrefix(lower, "data:audio/") || strings.HasPrefix(lower, "data:video/") || strings.HasPrefix(lower, "data:application/octet-stream") {
		return true
	}
	if len(trimmed) < 256 {
		return false
	}
	for _, character := range trimmed {
		alphaNumeric := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if !alphaNumeric && character != '+' && character != '/' && character != '=' {
			return false
		}
	}
	return true
}

func responsesInputSegments(raw json.RawMessage) []PromptAuditSegment {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if kitutil.Unmarshal(raw, &value) != nil {
		return nil
	}
	return responsesValueSegments(value)
}

func responsesValueSegments(value any) []PromptAuditSegment {
	switch typed := value.(type) {
	case string:
		return userSegments(typed)
	case []any:
		result := make([]PromptAuditSegment, 0, len(typed))
		// A response's items arrive in order: a reasoning or message item, then
		// the calls it issued, each possibly followed straight away by its output.
		// So the first call after any other item opens a new tool round, while a
		// call that follows a call or an output joins the round already open —
		// clients record a parallel round as call/output pairs as often as calls
		// followed by outputs.
		roundOpen := false
		for _, item := range typed {
			segments := responsesValueSegments(item)
			typeName := ""
			if entry, ok := item.(map[string]any); ok {
				typeName, _ = entry["type"].(string)
				typeName = strings.ToLower(strings.TrimSpace(typeName))
			}
			switch {
			case strings.HasSuffix(typeName, "_call_output") || typeName == "tool_result":
				// An output stays inside the round its call opened.
			case strings.HasSuffix(typeName, "_call"):
				if !roundOpen {
					markToolRoundStart(segments)
					roundOpen = true
				}
			default:
				roundOpen = false
			}
			result = append(result, segments...)
		}
		return result
	case map[string]any:
		role, _ := typed["role"].(string)
		role = strings.ToLower(strings.TrimSpace(role))
		typeName, _ := typed["type"].(string)
		typeName = strings.ToLower(strings.TrimSpace(typeName))
		if role == "" && (typeName == "input_text" || typeName == "text") {
			text, _ := typed["text"].(string)
			return userSegments(text)
		}
		if role != "" && isPromptAuditRole(role) {
			segments := scopedContentSegments(role, typed["content"])
			if len(segments) == 0 {
				segments = scopedContentSegments(role, typed["text"])
			}
			return segments
		}
		if typeName == "reasoning" {
			return appendRoleMessage(nil, "assistant", false, promptAuditReasoningTexts(typed))
		}
		if strings.HasSuffix(typeName, "_call_output") || typeName == "tool_result" {
			id, _ := typed["call_id"].(string)
			return promptAuditToolSegments(PromptScopeToolResult, "tool", "", id, structuredPromptAuditTexts(typed["output"]))
		}
		if typeName == "function_call" || typeName == "custom_tool_call" {
			payload := typed["arguments"]
			if payload == nil {
				payload = typed["input"]
			}
			name, _ := typed["name"].(string)
			id, _ := typed["call_id"].(string)
			return promptAuditToolSegments(PromptScopeToolCall, "assistant", name, id, structuredPromptAuditTexts(payload))
		}
		if typeName == "mcp_list_tools" {
			return promptAuditMCPDefinitions(typed["tools"], true)
		}
		if typeName == "mcp_call" {
			name, _ := typed["name"].(string)
			id, _ := typed["id"].(string)
			parts := promptAuditToolSegments(PromptScopeToolCall, "assistant", name, id, structuredPromptAuditTexts(typed["arguments"]))
			parts = append(parts, promptAuditToolSegments(PromptScopeToolResult, "tool", name, id, structuredPromptAuditTexts(typed["output"]))...)
			for index := range parts {
				parts[index].Scope = PromptScopeMCP
			}
			return parts
		}
	}
	return nil
}

func rerankDocumentTexts(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case map[string]any:
		return anyTextValues(typed["text"], false)
	case RerankDocument:
		return anyTextValues(typed.Text, false)
	case *RerankDocument:
		if typed != nil {
			return anyTextValues(typed.Text, false)
		}
	}
	return nil
}
