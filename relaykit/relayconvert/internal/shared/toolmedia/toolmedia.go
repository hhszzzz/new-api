package toolmedia

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const (
	ToolResultMediaMovedMarker    = "[new-api: tool result media moved to the following user message]"
	ToolResultMediaAttachedMarker = "[new-api: tool result media attached as native media]"
	wholeDataURLMinBytes          = 8 * 1024
	base64ishMinBytes             = 16 * 1024
	maxTraversalDepth             = 32
)

type Scope int

const (
	ImagesOnly Scope = iota
	InlineImagesOnly
	AllSupported
)

type ChatPlan struct {
	Content string
	Media   []dto.MediaContent
}

func PlanChatToolOutput(value any) (*ChatPlan, error) {
	return PlanChatToolOutputWithScope(value, AllSupported)
}

func PlanChatToolOutputWithScope(value any, scope Scope) (*ChatPlan, error) {
	cleaned, media, changed, err := StripAndClamp(
		value,
		scope,
		map[string]any{"type": "text", "text": ToolResultMediaMovedMarker},
		ToolResultMediaMovedMarker,
	)
	if err != nil || !changed {
		return nil, err
	}

	content := ""
	if _, ok := value.(string); ok {
		content = kitutil.Interface2String(cleaned)
	} else {
		raw, err := kitutil.Marshal(cleaned)
		if err != nil {
			return nil, err
		}
		content = string(raw)
	}
	return &ChatPlan{Content: content, Media: media}, nil
}

func StripAndClamp(value any, scope Scope, replacementBlock map[string]any, replacementText string) (any, []dto.MediaContent, bool, error) {
	normalized, err := normalizeValue(value)
	if err != nil {
		return nil, nil, false, err
	}
	cleaned, media, changed, err := stripMedia(normalized, scope, replacementBlock, replacementText, 0)
	if err != nil || !changed {
		return cleaned, media, changed, err
	}
	clampBase64ishStrings(cleaned)
	return cleaned, media, true, nil
}

func ImageURL(media dto.MediaContent) string {
	if media.Type != dto.ContentTypeImageURL {
		return ""
	}
	switch value := media.ImageUrl.(type) {
	case string:
		return strings.TrimSpace(value)
	case *dto.MessageImageUrl:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(value.Url)
	case map[string]any:
		return strings.TrimSpace(kitutil.Interface2String(value["url"]))
	default:
		return ""
	}
}

func ImageDetail(media dto.MediaContent) any {
	switch value := media.ImageUrl.(type) {
	case *dto.MessageImageUrl:
		if value != nil && value.Detail != "" {
			return value.Detail
		}
	case map[string]any:
		return value["detail"]
	}
	return nil
}

func stripMedia(value any, scope Scope, replacementBlock map[string]any, replacementText string, depth int) (any, []dto.MediaContent, bool, error) {
	if depth > maxTraversalDepth {
		return value, nil, false, nil
	}

	switch typed := value.(type) {
	case string:
		if media, ok := wholeStringImageDataURL(typed); ok && scopeAllows(scope, media) {
			return replacementText, []dto.MediaContent{media}, true, nil
		}
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
			return value, nil, false, nil
		}
		var parsed any
		if err := kitutil.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return value, nil, false, nil
		}
		cleaned, media, changed, err := stripMedia(parsed, scope, replacementBlock, replacementText, depth+1)
		if err != nil || !changed {
			return value, media, changed, err
		}
		clampBase64ishStrings(cleaned)
		raw, err := kitutil.Marshal(cleaned)
		if err != nil {
			return nil, nil, false, err
		}
		return string(raw), media, true, nil
	case []any:
		result := make([]any, len(typed))
		media := make([]dto.MediaContent, 0)
		changed := false
		for i, item := range typed {
			cleaned, itemMedia, itemChanged, err := stripMedia(item, scope, replacementBlock, replacementText, depth+1)
			if err != nil {
				return nil, nil, false, err
			}
			result[i] = cleaned
			media = append(media, itemMedia...)
			changed = changed || itemChanged
		}
		return result, media, changed, nil
	case map[string]any:
		if media, ok := chatMediaPart(typed, scope); ok {
			return cloneMap(replacementBlock), []dto.MediaContent{media}, true, nil
		}
		content, ok := typed["content"]
		if !ok {
			return value, nil, false, nil
		}
		cleaned, media, changed, err := stripMedia(content, scope, replacementBlock, replacementText, depth+1)
		if err != nil || !changed {
			return value, media, changed, err
		}
		result := cloneMap(typed)
		result["content"] = cleaned
		return result, media, true, nil
	default:
		return value, nil, false, nil
	}
}

func normalizeValue(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	raw, err := kitutil.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := kitutil.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func chatMediaPart(part map[string]any, scope Scope) (dto.MediaContent, bool) {
	partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
	var media dto.MediaContent
	var ok bool
	switch partType {
	case "input_image", "image_url":
		media, ok = normalizedImageURL(part)
	case "image":
		media, ok = typedImage(part)
	case "input_file":
		if scope != AllSupported {
			return dto.MediaContent{}, false
		}
		file := make(map[string]any)
		for _, key := range []string{"file_id", "file_data", "filename"} {
			if item, exists := part[key]; exists {
				file[key] = item
			}
		}
		if file["file_id"] == nil && file["file_data"] == nil {
			return dto.MediaContent{}, false
		}
		return dto.MediaContent{Type: dto.ContentTypeFile, File: file}, true
	case "input_audio":
		if scope != AllSupported || part["input_audio"] == nil {
			return dto.MediaContent{}, false
		}
		return dto.MediaContent{Type: dto.ContentTypeInputAudio, InputAudio: part["input_audio"]}, true
	case "":
		media, ok = looseDataImageURL(part)
	default:
		return dto.MediaContent{}, false
	}
	if !ok || !scopeAllows(scope, media) {
		return dto.MediaContent{}, false
	}
	return media, true
}

func scopeAllows(scope Scope, media dto.MediaContent) bool {
	if media.Type != dto.ContentTypeImageURL {
		return scope == AllSupported
	}
	if scope != InlineImagesOnly {
		return true
	}
	url := ImageURL(media)
	comma := strings.IndexByte(url, ',')
	return comma >= 0 && comma+1 < len(url) && isImageBase64DataURL(url)
}

func normalizedImageURL(part map[string]any) (dto.MediaContent, bool) {
	imageURL, ok := imageURLMap(part["image_url"])
	if !ok {
		return dto.MediaContent{}, false
	}
	if _, exists := imageURL["detail"]; !exists {
		if detail, exists := part["detail"]; exists {
			imageURL["detail"] = detail
		}
	}
	return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: imageURL}, true
}

func looseDataImageURL(part map[string]any) (dto.MediaContent, bool) {
	media, ok := normalizedImageURL(part)
	if !ok || !strings.HasPrefix(strings.ToLower(ImageURL(media)), "data:") {
		return dto.MediaContent{}, false
	}
	return media, true
}

func typedImage(part map[string]any) (dto.MediaContent, bool) {
	if source, ok := part["source"].(map[string]any); ok {
		mediaType := firstString(source, "media_type", "mime_type", "mimeType")
		if mediaType != "" && !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
			return dto.MediaContent{}, false
		}
		if url := strings.TrimSpace(kitutil.Interface2String(source["url"])); url != "" {
			imageURL := map[string]any{"url": url}
			if detail, exists := part["detail"]; exists {
				imageURL["detail"] = detail
			}
			return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: imageURL}, true
		}
		if data := kitutil.Interface2String(source["data"]); data != "" {
			if mediaType == "" {
				mediaType = "image/png"
			}
			url := data
			if !strings.HasPrefix(strings.ToLower(data), "data:image/") {
				url = fmt.Sprintf("data:%s;base64,%s", mediaType, data)
			}
			imageURL := map[string]any{"url": url}
			if detail, exists := part["detail"]; exists {
				imageURL["detail"] = detail
			}
			return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: imageURL}, true
		}
	}

	data := kitutil.Interface2String(part["data"])
	mediaType := firstString(part, "mimeType", "mime_type")
	if data == "" || !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return dto.MediaContent{}, false
	}
	return dto.MediaContent{
		Type:     dto.ContentTypeImageURL,
		ImageUrl: map[string]any{"url": fmt.Sprintf("data:%s;base64,%s", mediaType, data)},
	}, true
}

func wholeStringImageDataURL(value string) (dto.MediaContent, bool) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < wholeDataURLMinBytes || !isImageBase64DataURL(trimmed) {
		return dto.MediaContent{}, false
	}
	return dto.MediaContent{
		Type:     dto.ContentTypeImageURL,
		ImageUrl: map[string]any{"url": trimmed},
	}, true
}

func imageURLMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, false
		}
		return map[string]any{"url": typed}, true
	case map[string]any:
		if strings.TrimSpace(kitutil.Interface2String(typed["url"])) == "" {
			return nil, false
		}
		return cloneMap(typed), true
	default:
		return nil, false
	}
}

func clampBase64ishStrings(value any) {
	switch typed := value.(type) {
	case string:
		return
	case []any:
		for i, item := range typed {
			if text, ok := item.(string); ok && shouldOmit(text) {
				typed[i] = fmt.Sprintf("[new-api: omitted %d bytes]", len(text))
				continue
			}
			clampBase64ishStrings(item)
		}
	case map[string]any:
		for key, item := range typed {
			if text, ok := item.(string); ok && shouldOmit(text) {
				typed[key] = fmt.Sprintf("[new-api: omitted %d bytes]", len(text))
				continue
			}
			clampBase64ishStrings(item)
		}
	}
}

func shouldOmit(value string) bool {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= wholeDataURLMinBytes && strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		return true
	}
	if len(trimmed) < base64ishMinBytes {
		return false
	}
	for _, char := range []byte(trimmed) {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '+' || char == '/' || char == '=' {
			continue
		}
		return false
	}
	return true
}

func isImageBase64DataURL(value string) bool {
	comma := strings.IndexByte(value, ',')
	if comma < 0 {
		return false
	}
	header := strings.ToLower(value[:comma])
	return strings.HasPrefix(header, "data:image/") && strings.HasSuffix(header, ";base64")
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := strings.TrimSpace(kitutil.Interface2String(value[key])); text != "" {
			return text
		}
	}
	return ""
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
