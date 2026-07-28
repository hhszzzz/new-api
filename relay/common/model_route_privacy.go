package common

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// RedactUserModelRouteText replaces routed upstream model identifiers with the
// model name requested by the client. It is intentionally boundary-aware so a
// model such as "gpt-5" does not rewrite "gpt-5.1".
func RedactUserModelRouteText(text string, info *RelayInfo) string {
	if info == nil || !info.HasModelRouting() || text == "" {
		return text
	}
	publicName := info.PublicResponseModelName()
	privateNames := []string{info.RouteTargetModelName, info.UpstreamModelName}
	sort.SliceStable(privateNames, func(i, j int) bool {
		return len(strings.TrimSpace(privateNames[i])) > len(strings.TrimSpace(privateNames[j]))
	})
	for _, privateName := range privateNames {
		privateName = strings.TrimSpace(privateName)
		if privateName == "" || privateName == publicName {
			continue
		}
		text = replaceModelIdentifier(text, privateName, publicName)
	}
	return text
}

// RedactUserModelRouteJSON sanitizes protocol-owned model and error fields
// without decoding numbers into float64. Arbitrary metadata, tool arguments,
// and message content are left untouched.
func RedactUserModelRouteJSON(data []byte, info *RelayInfo) ([]byte, error) {
	if info == nil || !info.HasModelRouting() || len(data) == 0 {
		return data, nil
	}
	return redactUserModelRouteJSON(json.RawMessage(data), info)
}

// SanitizeUserModelRouteOpenAIError removes private routed model identifiers
// from every protocol-owned OpenAI error field before it is returned to the
// client. Error metadata is owned by the error envelope; unrelated response
// metadata is handled separately and remains opaque.
func SanitizeUserModelRouteOpenAIError(relayError types.OpenAIError, info *RelayInfo) types.OpenAIError {
	if info == nil || !info.HasModelRouting() {
		return relayError
	}
	relayError.Message = RedactUserModelRouteText(relayError.Message, info)
	relayError.Type = RedactUserModelRouteText(relayError.Type, info)
	relayError.Code = sanitizeStructuredErrorValue(relayError.Code, info)
	relayError.Param = sanitizeStructuredErrorValue(relayError.Param, info)
	if len(relayError.Metadata) > 0 {
		if redacted, err := sanitizeErrorOwnedJSON(relayError.Metadata, info); err == nil {
			relayError.Metadata = redacted
		}
	}
	return relayError
}

// SanitizeUserModelRouteClaudeError removes private routed model identifiers
// from the structured fields exposed by the Claude error protocol.
func SanitizeUserModelRouteClaudeError(relayError types.ClaudeError, info *RelayInfo) types.ClaudeError {
	if info == nil || !info.HasModelRouting() {
		return relayError
	}
	relayError.Message = RedactUserModelRouteText(relayError.Message, info)
	relayError.Type = RedactUserModelRouteText(relayError.Type, info)
	return relayError
}

// RewriteUserModelRouteRequestJSON changes only protocol-owned model fields
// before a client event is sent to the selected upstream channel. In
// particular, nested metadata and tool schemas are opaque client data.
func RewriteUserModelRouteRequestJSON(data []byte, modelName string) ([]byte, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" || len(data) == 0 {
		return data, nil
	}
	return rewriteUserModelRouteRequestJSON(json.RawMessage(data), modelName)
}

func rewriteUserModelRouteRequestJSON(data json.RawMessage, modelName string) (json.RawMessage, error) {
	if appcommon.GetJsonType(data) != "object" {
		return data, nil
	}
	var object map[string]json.RawMessage
	if err := appcommon.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if err := rewriteDirectModelFields(object, modelName); err != nil {
		return nil, err
	}
	for _, container := range []string{"session", "response"} {
		child, exists := object[container]
		if !exists || appcommon.GetJsonType(child) != "object" {
			continue
		}
		var childObject map[string]json.RawMessage
		if err := appcommon.Unmarshal(child, &childObject); err != nil {
			return nil, err
		}
		if err := rewriteDirectModelFields(childObject, modelName); err != nil {
			return nil, err
		}
		encoded, err := appcommon.Marshal(childObject)
		if err != nil {
			return nil, err
		}
		object[container] = encoded
	}
	encoded, err := appcommon.Marshal(object)
	return json.RawMessage(encoded), err
}

func redactUserModelRouteJSON(data json.RawMessage, info *RelayInfo) (json.RawMessage, error) {
	if appcommon.GetJsonType(data) != "object" {
		return data, nil
	}
	var object map[string]json.RawMessage
	if err := appcommon.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	publicName := info.PublicResponseModelName()
	if err := rewriteDirectModelFields(object, publicName); err != nil {
		return nil, err
	}
	if isRootErrorObject(object) {
		if err := sanitizeKnownErrorFields(object, info); err != nil {
			return nil, err
		}
	}
	if err := sanitizeNestedError(object, info); err != nil {
		return nil, err
	}
	for _, container := range []string{"session", "response", "message"} {
		child, exists := object[container]
		if !exists || appcommon.GetJsonType(child) != "object" {
			continue
		}
		var childObject map[string]json.RawMessage
		if err := appcommon.Unmarshal(child, &childObject); err != nil {
			return nil, err
		}
		if err := rewriteDirectModelFields(childObject, publicName); err != nil {
			return nil, err
		}
		if err := sanitizeNestedError(childObject, info); err != nil {
			return nil, err
		}
		encoded, err := appcommon.Marshal(childObject)
		if err != nil {
			return nil, err
		}
		object[container] = encoded
	}
	encoded, err := appcommon.Marshal(object)
	return json.RawMessage(encoded), err
}

func sanitizeNestedError(object map[string]json.RawMessage, info *RelayInfo) error {
	errorObject, exists := object["error"]
	if !exists || appcommon.GetJsonType(errorObject) != "object" {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := appcommon.Unmarshal(errorObject, &fields); err != nil {
		return err
	}
	if err := sanitizeKnownErrorFields(fields, info); err != nil {
		return err
	}
	if err := sanitizeNestedError(fields, info); err != nil {
		return err
	}
	encoded, err := appcommon.Marshal(fields)
	if err != nil {
		return err
	}
	object["error"] = encoded
	return nil
}

func sanitizeKnownErrorFields(fields map[string]json.RawMessage, info *RelayInfo) error {
	for _, key := range []string{"message", "type", "code", "param", "metadata"} {
		value, exists := fields[key]
		if !exists {
			continue
		}
		redacted, err := sanitizeErrorOwnedJSON(value, info)
		if err != nil {
			return err
		}
		fields[key] = redacted
	}
	return nil
}

func sanitizeErrorOwnedJSON(data json.RawMessage, info *RelayInfo) (json.RawMessage, error) {
	switch appcommon.GetJsonType(data) {
	case "string":
		var value string
		if err := appcommon.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		encoded, err := appcommon.Marshal(RedactUserModelRouteText(value, info))
		return json.RawMessage(encoded), err
	case "object":
		var object map[string]json.RawMessage
		if err := appcommon.Unmarshal(data, &object); err != nil {
			return nil, err
		}
		for key, value := range object {
			if isOpaqueProtocolPayloadField(key) {
				continue
			}
			if isModelRouteField(key) && appcommon.GetJsonType(value) == "string" {
				encoded, err := appcommon.Marshal(info.PublicResponseModelName())
				if err != nil {
					return nil, err
				}
				object[key] = encoded
				continue
			}
			redacted, err := sanitizeErrorOwnedJSON(value, info)
			if err != nil {
				return nil, err
			}
			object[key] = redacted
		}
		encoded, err := appcommon.Marshal(object)
		return json.RawMessage(encoded), err
	case "array":
		var values []json.RawMessage
		if err := appcommon.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		for index, value := range values {
			redacted, err := sanitizeErrorOwnedJSON(value, info)
			if err != nil {
				return nil, err
			}
			values[index] = redacted
		}
		encoded, err := appcommon.Marshal(values)
		return json.RawMessage(encoded), err
	default:
		return data, nil
	}
}

func sanitizeStructuredErrorValue(value any, info *RelayInfo) any {
	switch typed := value.(type) {
	case string:
		return RedactUserModelRouteText(typed, info)
	case json.RawMessage:
		redacted, err := sanitizeErrorOwnedJSON(typed, info)
		if err != nil {
			return typed
		}
		return redacted
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, child := range typed {
			if isOpaqueProtocolPayloadField(key) {
				redacted[key] = child
				continue
			}
			if isModelRouteField(key) {
				if _, ok := child.(string); ok {
					redacted[key] = info.PublicResponseModelName()
					continue
				}
			}
			redacted[key] = sanitizeStructuredErrorValue(child, info)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for index, child := range typed {
			redacted[index] = sanitizeStructuredErrorValue(child, info)
		}
		return redacted
	default:
		return value
	}
}

func isRootErrorObject(object map[string]json.RawMessage) bool {
	typeValue, exists := object["type"]
	if !exists || appcommon.GetJsonType(typeValue) != "string" {
		return false
	}
	var value string
	if err := appcommon.Unmarshal(typeValue, &value); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(value)), "error")
}

func rewriteDirectModelFields(object map[string]json.RawMessage, modelName string) error {
	for key, child := range object {
		if !isModelRouteField(key) || appcommon.GetJsonType(child) != "string" {
			continue
		}
		encoded, err := appcommon.Marshal(modelName)
		if err != nil {
			return err
		}
		object[key] = encoded
	}
	return nil
}

func isModelRouteField(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return normalized == "model" ||
		normalized == "model_name" ||
		normalized == "modelversion" ||
		normalized == "model_version" ||
		normalized == "upstream_model" ||
		normalized == "upstream_model_name" ||
		normalized == "target_model"
}

func isOpaqueProtocolPayloadField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "arguments", "content", "input":
		return true
	default:
		return false
	}
}

func replaceModelIdentifier(text, privateName, publicName string) string {
	if privateName == "" || privateName == publicName {
		return text
	}
	var result strings.Builder
	result.Grow(len(text))
	searchFrom := 0
	last := 0
	for searchFrom < len(text) {
		relative := strings.Index(strings.ToLower(text[searchFrom:]), strings.ToLower(privateName))
		if relative < 0 {
			break
		}
		start := searchFrom + relative
		end := start + len(privateName)
		if !modelIdentifierBoundary(text, start, end) {
			searchFrom = end
			continue
		}
		result.WriteString(text[last:start])
		result.WriteString(publicName)
		last = end
		searchFrom = end
	}
	if last == 0 {
		return text
	}
	result.WriteString(text[last:])
	return result.String()
}

func modelIdentifierBoundary(text string, start, end int) bool {
	if start > 0 {
		previous, _ := utf8.DecodeLastRuneInString(text[:start])
		if isModelIdentifierRune(previous) {
			return false
		}
	}
	if end < len(text) {
		next, _ := utf8.DecodeRuneInString(text[end:])
		if isModelIdentifierRune(next) {
			return false
		}
	}
	return true
}

func isModelIdentifierRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_' || value == '-' || value == '.'
}
