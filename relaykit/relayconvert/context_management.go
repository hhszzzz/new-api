package relayconvert

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// applyMessagesContextManagement edits a private copy. Token thresholds are
// estimates, so the caller must opt into lossy conversion. Upstream usage stays
// authoritative for billing; this estimate is never used as settled usage.
func applyMessagesContextManagement(request *dto.ClaudeRequest, info convmeta.Meta) (*dto.ClaudeRequest, string, error) {
	var config map[string]any
	if err := kitutil.Unmarshal(request.ContextManagement, &config); err != nil {
		return nil, "", err
	}
	if len(config) != 1 {
		return nil, "", fmt.Errorf("unsupported context_management fields")
	}
	edits, ok := config["edits"].([]any)
	if !ok {
		return nil, "", fmt.Errorf("context_management.edits must be an array")
	}
	copy := *request
	copy.Messages = slices.Clone(request.Messages)
	blocks := make([][]map[string]any, len(copy.Messages))
	for index, message := range copy.Messages {
		if message.IsStringContent() {
			continue
		}
		parsed, err := kitutil.Any2Type[[]map[string]any](message.Content)
		if err != nil {
			return nil, "", err
		}
		blocks[index] = parsed
	}
	encoded, err := kitutil.Marshal(request)
	if err != nil {
		return nil, "", err
	}
	estimatedTokens := (len(encoded) + 3) / 4
	if info != nil && info.GetEstimatePromptTokens() > 0 {
		estimatedTokens = info.GetEstimatePromptTokens()
	}
	clearedResults, clearedThinking := 0, 0
	seen := map[string]bool{}
	for _, rawEdit := range edits {
		edit, ok := rawEdit.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("context edit must be an object")
		}
		kind, _ := edit["type"].(string)
		if seen[kind] {
			return nil, "", fmt.Errorf("duplicate context edit %q", kind)
		}
		seen[kind] = true
		switch kind {
		case "clear_thinking_20251015":
			if seen["clear_tool_uses_20250919"] {
				return nil, "", fmt.Errorf("thinking edits must precede tool edits")
			}
			for key := range edit {
				if key != "type" && key != "keep" {
					return nil, "", fmt.Errorf("unsupported thinking edit field %q", key)
				}
			}
			keep := edit["keep"]
			if keep == nil {
				model := request.Model
				if info != nil && info.GetOriginModelName() != "" {
					model = info.GetOriginModelName()
				}
				keep, err = defaultThinkingKeep(model)
				if err != nil {
					return nil, "", err
				}
			}
			if keep == "all" {
				continue
			}
			_, count, err := contextEditCount(keep, "thinking_turns", 1)
			if err != nil {
				return nil, "", err
			}
			turns := make([]int, 0)
			for i, content := range blocks {
				if copy.Messages[i].Role != "assistant" {
					continue
				}
				if slices.ContainsFunc(content, func(block map[string]any) bool {
					return block["type"] == "thinking" || block["type"] == "redacted_thinking"
				}) {
					turns = append(turns, i)
				}
			}
			for _, i := range turns[:max(0, len(turns)-count)] {
				kept := make([]map[string]any, 0, len(blocks[i]))
				for _, block := range blocks[i] {
					if block["type"] == "thinking" || block["type"] == "redacted_thinking" {
						raw, _ := kitutil.Marshal(block)
						estimatedTokens -= (len(raw) + 3) / 4
						continue
					}
					kept = append(kept, block)
				}
				blocks[i] = kept
				clearedThinking++
			}
		case "clear_tool_uses_20250919":
			for key := range edit {
				if !slices.Contains([]string{"type", "trigger", "keep", "clear_at_least", "exclude_tools", "clear_tool_inputs"}, key) {
					return nil, "", fmt.Errorf("unsupported tool edit field %q", key)
				}
			}
			triggerKind, trigger := "input_tokens", 100000
			if value := edit["trigger"]; value != nil {
				triggerKind, trigger, err = contextEditCount(value, "", 1)
				if err != nil || triggerKind != "input_tokens" && triggerKind != "tool_uses" {
					return nil, "", fmt.Errorf("unsupported context edit trigger")
				}
			}
			keep := 3
			if value := edit["keep"]; value != nil {
				_, keep, err = contextEditCount(value, "tool_uses", 0)
				if err != nil {
					return nil, "", err
				}
			}
			minimum := 0
			if value := edit["clear_at_least"]; value != nil {
				_, minimum, err = contextEditCount(value, "input_tokens", 0)
				if err != nil {
					return nil, "", err
				}
			}
			excluded := []string{}
			if value := edit["exclude_tools"]; value != nil {
				var err error
				excluded, err = kitutil.Any2Type[[]string](value)
				if err != nil {
					return nil, "", err
				}
			}
			clearInputs := false
			if value := edit["clear_tool_inputs"]; value != nil {
				var ok bool
				clearInputs, ok = value.(bool)
				if !ok {
					return nil, "", fmt.Errorf("clear_tool_inputs must be boolean")
				}
			}
			calls := make([]map[string]any, 0)
			results := make(map[string]map[string]any)
			for _, content := range blocks {
				for _, block := range content {
					if block["type"] == "tool_use" {
						calls = append(calls, block)
					}
					if block["type"] == "tool_result" {
						if id, ok := block["tool_use_id"].(string); ok {
							results[id] = block
						}
					}
				}
			}
			if triggerKind == "input_tokens" && estimatedTokens <= trigger || triggerKind == "tool_uses" && len(calls) <= trigger {
				continue
			}
			candidates := make([]map[string]any, 0)
			savings := 0
			for _, call := range calls[:max(0, len(calls)-keep)] {
				name, _ := call["name"].(string)
				id, _ := call["id"].(string)
				result := results[id]
				if result == nil || slices.Contains(excluded, name) {
					continue
				}
				raw, _ := kitutil.Marshal(result["content"])
				savings += max(0, (len(raw)-len(contextClearedText))/4)
				if clearInputs {
					raw, _ := kitutil.Marshal(call["input"])
					savings += max(0, (len(raw)-2)/4)
				}
				candidates = append(candidates, call)
			}
			if savings < minimum {
				continue
			}
			for _, call := range candidates {
				id, _ := call["id"].(string)
				results[id]["content"] = contextClearedText
				if clearInputs {
					call["input"] = map[string]any{}
				}
				clearedResults++
			}
			estimatedTokens -= savings
		default:
			return nil, "", fmt.Errorf("unsupported context edit %q", kind)
		}
	}
	copy.Messages = make([]dto.ClaudeMessage, 0, len(request.Messages))
	for i, message := range request.Messages {
		if blocks[i] != nil {
			if len(blocks[i]) == 0 {
				continue
			}
			message.Content = blocks[i]
		}
		copy.Messages = append(copy.Messages, message)
	}
	copy.ContextManagement = nil
	return &copy, fmt.Sprintf("gateway cleared %d tool results and %d thinking turns; input-token thresholds and savings use estimates, upstream usage remains authoritative", clearedResults, clearedThinking), nil
}

const contextClearedText = "[new-api: tool result cleared by context_management]"

func defaultThinkingKeep(model string) (any, error) {
	parts := strings.FieldsFunc(strings.ToLower(model), func(r rune) bool {
		return r == '-' || r == '.' || r == '/' || r == ':' || r == '_'
	})
	for i, part := range parts {
		if part == "fable" || part == "mythos" {
			return "all", nil
		}
		if part == "haiku" {
			return map[string]any{"type": "thinking_turns", "value": float64(1)}, nil
		}
		if (part != "opus" && part != "sonnet") || i+1 >= len(parts) {
			continue
		}
		major, err := strconv.Atoi(parts[i+1])
		if err != nil || major < 1 || major > 100 {
			continue
		}
		minor := 0
		if i+2 < len(parts) {
			if parsed, err := strconv.Atoi(parts[i+2]); err == nil && parsed < 100 {
				minor = parsed
			}
		}
		if major > 4 || major == 4 && (part == "opus" && minor >= 5 || part == "sonnet" && minor >= 6) {
			return "all", nil
		}
		return map[string]any{"type": "thinking_turns", "value": float64(1)}, nil
	}
	if strings.Contains(strings.ToLower(model), "claude-3-") {
		return map[string]any{"type": "thinking_turns", "value": float64(1)}, nil
	}
	return nil, fmt.Errorf("thinking keep default is unknown for model %q", model)
}

func contextEditCount(value any, expected string, minimum int) (string, int, error) {
	object, ok := value.(map[string]any)
	if !ok || len(object) != 2 {
		return "", 0, fmt.Errorf("context edit count requires type and value")
	}
	kind, _ := object["type"].(string)
	count, ok := object["value"].(float64)
	if !ok || count < float64(minimum) || count > 1e9 || count != float64(int(count)) || expected != "" && kind != expected {
		return "", 0, fmt.Errorf("invalid context edit %s count", expected)
	}
	return kind, int(count), nil
}
