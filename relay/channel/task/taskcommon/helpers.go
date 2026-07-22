package taskcommon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// UnmarshalMetadata converts a map[string]any metadata to a typed struct via JSON round-trip.
// This replaces the repeated pattern: json.Marshal(metadata) → json.Unmarshal(bytes, &target).
func UnmarshalMetadata(metadata map[string]any, target any) error {
	if metadata == nil {
		return nil
	}
	// Prevent metadata from overriding model fields to avoid billing bypass.
	delete(metadata, "model")
	metaBytes, err := common.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata failed: %w", err)
	}
	if err := common.Unmarshal(metaBytes, target); err != nil {
		return fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	return nil
}

// DefaultString returns val if non-empty, otherwise fallback.
func DefaultString(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// DefaultInt returns val if non-zero, otherwise fallback.
func DefaultInt(val, fallback int) int {
	if val == 0 {
		return fallback
	}
	return val
}

// EncodeLocalTaskID encodes an upstream operation name to a URL-safe base64 string.
// Used by Gemini/Vertex to store upstream names as task IDs.
func EncodeLocalTaskID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// DecodeLocalTaskID decodes a base64-encoded upstream operation name.
func DecodeLocalTaskID(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SanitizePublicTaskData removes provider model-routing details from arbitrary
// task payloads while preserving JSON number precision and unknown fields.
func SanitizePublicTaskData(data []byte, originModelName, upstreamModelName string) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	return sanitizeTaskModelRouting(json.RawMessage(data), originModelName, upstreamModelName, 0, false)
}

// SanitizePublicTaskErrorData applies model-routing redaction and masks
// sensitive network details in arbitrary upstream error payloads.
func SanitizePublicTaskErrorData(data []byte, originModelName, upstreamModelName string) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	return sanitizeTaskModelRouting(json.RawMessage(data), originModelName, upstreamModelName, 0, true)
}

func sanitizeTaskModelRouting(data json.RawMessage, originModelName, upstreamModelName string, depth int, maskSensitive bool) (json.RawMessage, error) {
	switch common.GetJsonType(data) {
	case "object":
		var object map[string]json.RawMessage
		if err := common.Unmarshal(data, &object); err != nil {
			return nil, err
		}
		sanitizedObject := make(map[string]json.RawMessage, len(object))
		originalPublicKeys := make(map[string]bool, len(object))
		for key, child := range object {
			sanitizedKey := redactModelRoutingKey(key, originModelName, upstreamModelName)
			if maskSensitive {
				sanitizedKey = maskSensitiveTaskErrorText(sanitizedKey)
			}
			if sanitizedKey == "" {
				continue
			}
			isOriginalPublicKey := sanitizedKey == key
			if _, exists := sanitizedObject[sanitizedKey]; exists {
				if originalPublicKeys[sanitizedKey] || !isOriginalPublicKey {
					continue
				}
			}
			if sanitizedKey == key && shouldReplaceTaskModelRoutingField(key, child, originModelName, upstreamModelName, depth) {
				if originModelName == "" {
					delete(sanitizedObject, sanitizedKey)
				} else {
					encodedModel, err := common.Marshal(originModelName)
					if err != nil {
						return nil, err
					}
					sanitizedObject[sanitizedKey] = encodedModel
					originalPublicKeys[sanitizedKey] = isOriginalPublicKey
				}
				continue
			}
			sanitizedChild, err := sanitizeTaskModelRouting(child, originModelName, upstreamModelName, depth+1, maskSensitive)
			if err != nil {
				return nil, err
			}
			sanitizedObject[sanitizedKey] = sanitizedChild
			originalPublicKeys[sanitizedKey] = isOriginalPublicKey
		}
		encodedObject, err := common.Marshal(sanitizedObject)
		return json.RawMessage(encodedObject), err
	case "array":
		var items []json.RawMessage
		if err := common.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		for i, child := range items {
			sanitizedChild, err := sanitizeTaskModelRouting(child, originModelName, upstreamModelName, depth, maskSensitive)
			if err != nil {
				return nil, err
			}
			items[i] = sanitizedChild
		}
		encodedItems, err := common.Marshal(items)
		return json.RawMessage(encodedItems), err
	case "string":
		var text string
		if err := common.Unmarshal(data, &text); err != nil {
			return nil, err
		}
		text = RedactModelRoutingText(text, originModelName, upstreamModelName)
		if maskSensitive {
			text = maskSensitiveTaskErrorText(text)
		}
		encodedText, err := common.Marshal(text)
		return json.RawMessage(encodedText), err
	default:
		var scalar json.RawMessage
		if err := common.Unmarshal(data, &scalar); err != nil {
			return nil, err
		}
		return scalar, nil
	}
}

func maskSensitiveTaskErrorText(text string) string {
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
		return common.MaskSensitiveInfo(text)
	}
	return text
}

// RedactModelRoutingText replaces upstream model identifiers at non-alphanumeric
// boundaries while leaving longer alphanumeric model names unchanged.
func RedactModelRoutingText(text, originModelName, upstreamModelName string) string {
	return redactModelRoutingIdentifier(text, originModelName, upstreamModelName)
}

func redactModelRoutingKey(key, originModelName, upstreamModelName string) string {
	return redactModelRoutingIdentifier(key, originModelName, upstreamModelName)
}

func redactModelRoutingIdentifier(text, originModelName, upstreamModelName string) string {
	if upstreamModelName == "" || upstreamModelName == originModelName || indexASCIIInsensitive(text, upstreamModelName) < 0 {
		return text
	}

	var redacted strings.Builder
	cursor := 0
	searchFrom := 0
	replaced := false
	for searchFrom < len(text) {
		relativeIndex := indexASCIIInsensitive(text[searchFrom:], upstreamModelName)
		if relativeIndex < 0 {
			break
		}
		start := searchFrom + relativeIndex
		end := start + len(upstreamModelName)
		if identifierRangeWithin(text, start, end, originModelName) {
			searchFrom = end
			continue
		}
		previousRune, _ := utf8.DecodeLastRuneInString(text[:start])
		nextRune, _ := utf8.DecodeRuneInString(text[end:])
		startsAtBoundary := start == 0 || !isASCIIModelAlphaNumeric(previousRune)
		endsAtBoundary := end == len(text) || !isASCIIModelAlphaNumeric(nextRune)
		if startsAtBoundary && endsAtBoundary {
			redacted.WriteString(text[cursor:start])
			redacted.WriteString(originModelName)
			cursor = end
			replaced = true
		}
		searchFrom = end
	}
	if !replaced {
		return text
	}
	redacted.WriteString(text[cursor:])
	return redacted.String()
}

func identifierRangeWithin(text string, start, end int, identifier string) bool {
	if identifier == "" || len(identifier) > len(text) {
		return false
	}
	searchFrom := 0
	for searchFrom <= len(text)-len(identifier) {
		relativeIndex := indexASCIIInsensitive(text[searchFrom:], identifier)
		if relativeIndex < 0 {
			return false
		}
		identifierStart := searchFrom + relativeIndex
		identifierEnd := identifierStart + len(identifier)
		if identifierStart <= start && identifierEnd >= end {
			return true
		}
		searchFrom = identifierStart + 1
	}
	return false
}

func indexASCIIInsensitive(text, identifier string) int {
	if identifier == "" || len(identifier) > len(text) {
		return -1
	}
	for start := 0; start <= len(text)-len(identifier); start++ {
		matched := true
		for offset := range len(identifier) {
			if foldASCII(text[start+offset]) != foldASCII(identifier[offset]) {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}

func foldASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}

func isASCIIModelAlphaNumeric(value rune) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9'
}

func isTaskModelRoutingField(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(key))
	switch normalized {
	case "model",
		"modelid",
		"modelname",
		"modelversion",
		"modeltype",
		"majormodelversion",
		"upstreammodel",
		"upstreammodelid",
		"upstreammodelname",
		"upstreammodelversion",
		"providermodel",
		"providermodelid",
		"providermodelname",
		"providermodelversion",
		"selectedmodel",
		"actualmodel",
		"resolvedmodel",
		"routedmodel",
		"routingmodel",
		"mappedmodel",
		"effectivemodel",
		"fallbackmodel",
		"fallbackmodelname",
		"deploymentmodel",
		"mv",
		"reqkey",
		"engine",
		"deployment",
		"deploymentid",
		"deploymentname":
		return true
	default:
		return false
	}
}

func shouldReplaceTaskModelRoutingField(
	key string,
	value json.RawMessage,
	originModelName, upstreamModelName string,
	depth int,
) bool {
	if !isTaskModelRoutingField(key) {
		return false
	}
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(key))
	switch normalized {
	case "engine", "modeltype", "deployment", "deploymentid", "deploymentname":
		if upstreamModelName == "" {
			return false
		}
		var modelName string
		if err := common.Unmarshal(value, &modelName); err != nil {
			return false
		}
		return RedactModelRoutingText(modelName, originModelName, upstreamModelName) != modelName
	}
	if normalized != "model" || depth == 0 || upstreamModelName == "" {
		return true
	}

	var modelName string
	if err := common.Unmarshal(value, &modelName); err != nil {
		return false
	}
	return RedactModelRoutingText(modelName, originModelName, upstreamModelName) != modelName
}

// BuildProxyURL constructs the video proxy URL using the public task ID.
// e.g., "https://your-server.com/v1/videos/task_xxxx/content"
func BuildProxyURL(taskID string) string {
	return fmt.Sprintf("%s/v1/videos/%s/content", system_setting.ServerAddress, taskID)
}

// Status-to-progress mapping constants for polling updates.
const (
	ProgressSubmitted  = "10%"
	ProgressQueued     = "20%"
	ProgressInProgress = "30%"
	ProgressComplete   = "100%"
)

// ---------------------------------------------------------------------------
// BaseBilling — embeddable no-op implementations for TaskAdaptor billing methods.
// Adaptors that do not need custom billing can embed this struct directly.
// ---------------------------------------------------------------------------

type BaseBilling struct{}

// EstimateBilling returns nil (no extra ratios; use base model price).
func (BaseBilling) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

// AdjustBillingOnSubmit returns nil (no submit-time adjustment).
func (BaseBilling) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, _ []byte) map[string]float64 {
	return nil
}

// AdjustBillingOnComplete returns 0 (keep pre-charged amount).
func (BaseBilling) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}
