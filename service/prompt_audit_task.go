package service

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

var promptAuditTaskTextFields = map[string]struct{}{
	"actual_prompt":          {},
	"caption":                {},
	"content":                {},
	"description":            {},
	"final_prompt":           {},
	"final_zh_prompt":        {},
	"gpt_description_prompt": {},
	"image_prompt":           {},
	"input":                  {},
	"input_prompt":           {},
	"instruction":            {},
	"instructions":           {},
	"lyrics":                 {},
	"negative_prompt":        {},
	"orig_prompt":            {},
	"positive_prompt":        {},
	"prefix":                 {},
	"prompt":                 {},
	"prompt_en":              {},
	"query":                  {},
	"script":                 {},
	"suffix":                 {},
	"tags":                   {},
	"text":                   {},
	"text_prompt":            {},
	"title":                  {},
}

var promptAuditTaskExcludedFields = map[string]struct{}{
	"audio":             {},
	"audio_url":         {},
	"base64":            {},
	"data":              {},
	"encrypted_content": {},
	"file":              {},
	"files":             {},
	"function":          {},
	"functions":         {},
	"image":             {},
	"image_url":         {},
	"images":            {},
	"input_audio":       {},
	"input_image":       {},
	"input_reference":   {},
	"mask":              {},
	"metadata":          {},
	"reasoning":         {},
	"tool":              {},
	"tool_choice":       {},
	"tools":             {},
	"video":             {},
	"video_url":         {},
}

// ExtractTaskPromptAuditSnapshot extracts only client-supplied textual prompt
// fields from task/media requests. It deliberately ignores metadata, tool
// definitions, URLs, and binary/file fields.
func ExtractTaskPromptAuditSnapshot(c *gin.Context) (dto.PromptAuditSnapshot, string, error) {
	mediaType, parameters, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	mediaType = strings.ToLower(mediaType)
	switch {
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		return extractTaskPromptAuditJSON(c)
	case mediaType == "multipart/form-data":
		return extractTaskPromptAuditMultipart(c, parameters["boundary"])
	case mediaType == "application/x-www-form-urlencoded":
		return extractTaskPromptAuditForm(c)
	}

	// Existing task clients sometimes omit Content-Type while still sending JSON.
	// Use the streaming JSON walker here too so the audit order remains the exact
	// client wire order instead of being reconstructed from an unordered map.
	return extractTaskPromptAuditJSON(c)
}

func extractTaskPromptAuditJSON(c *gin.Context) (dto.PromptAuditSnapshot, string, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return dto.PromptAuditSnapshot{}, "", err
	}
	reader, err := storage.NewReader()
	if err != nil {
		return dto.PromptAuditSnapshot{}, "", err
	}
	defer reader.Close()

	modelName := ""
	texts := make([]string, 0, 4)
	err = common.WalkJsonStrings(reader, func(path []string, value string) error {
		if len(path) == 0 || taskPromptAuditPathExcluded(path) {
			return nil
		}
		field := strings.ToLower(strings.TrimSpace(path[len(path)-1]))
		if len(path) == 1 && field == "model" {
			modelName = strings.TrimSpace(value)
			return nil
		}
		if _, ok := promptAuditTaskTextFields[field]; ok {
			appendTaskPromptAuditText(&texts, value)
		}
		return nil
	})
	if err != nil {
		return dto.PromptAuditSnapshot{}, "", err
	}
	return taskPromptAuditSnapshot(texts), modelName, nil
}

func extractTaskPromptAuditMultipart(c *gin.Context, boundary string) (dto.PromptAuditSnapshot, string, error) {
	if strings.TrimSpace(boundary) == "" {
		return dto.PromptAuditSnapshot{}, "", errors.New("multipart boundary is required")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return dto.PromptAuditSnapshot{}, "", err
	}
	reader, err := storage.NewReader()
	if err != nil {
		return dto.PromptAuditSnapshot{}, "", err
	}
	defer reader.Close()

	modelName := ""
	texts := make([]string, 0, 4)
	parts := multipart.NewReader(reader, boundary)
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return dto.PromptAuditSnapshot{}, "", err
		}
		field := strings.ToLower(strings.TrimSpace(part.FormName()))
		if field == "" || part.FileName() != "" {
			_ = part.Close()
			continue
		}
		if _, excluded := promptAuditTaskExcludedFields[field]; excluded {
			_ = part.Close()
			continue
		}
		value, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return dto.PromptAuditSnapshot{}, "", err
		}
		if field == "model" {
			modelName = strings.TrimSpace(string(value))
			continue
		}
		if _, ok := promptAuditTaskTextFields[field]; ok {
			appendTaskPromptAuditText(&texts, string(value))
		}
	}
	return taskPromptAuditSnapshot(texts), modelName, nil
}

func extractTaskPromptAuditForm(c *gin.Context) (dto.PromptAuditSnapshot, string, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return dto.PromptAuditSnapshot{}, "", err
	}
	reader, err := storage.NewReader()
	if err != nil {
		return dto.PromptAuditSnapshot{}, "", err
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return dto.PromptAuditSnapshot{}, "", err
	}

	modelName := ""
	texts := make([]string, 0, 4)
	for _, pair := range strings.Split(string(body), "&") {
		if pair == "" {
			continue
		}
		name, rawValue, _ := strings.Cut(pair, "=")
		name, err = url.QueryUnescape(name)
		if err != nil {
			return dto.PromptAuditSnapshot{}, "", err
		}
		value, err := url.QueryUnescape(rawValue)
		if err != nil {
			return dto.PromptAuditSnapshot{}, "", err
		}
		field := strings.ToLower(strings.TrimSpace(name))
		if field == "model" {
			modelName = strings.TrimSpace(value)
			continue
		}
		if _, excluded := promptAuditTaskExcludedFields[field]; excluded {
			continue
		}
		if _, ok := promptAuditTaskTextFields[field]; ok {
			appendTaskPromptAuditText(&texts, value)
		}
	}
	return taskPromptAuditSnapshot(texts), modelName, nil
}

func taskPromptAuditPathExcluded(path []string) bool {
	for _, field := range path {
		if _, excluded := promptAuditTaskExcludedFields[strings.ToLower(strings.TrimSpace(field))]; excluded {
			return true
		}
	}
	return false
}

func appendTaskPromptAuditText(texts *[]string, value string) {
	if text := strings.TrimSpace(value); text != "" && !looksLikePromptAuditTaskPayload(text) {
		*texts = append(*texts, text)
	}
}

func taskPromptAuditSnapshot(texts []string) dto.PromptAuditSnapshot {
	if len(texts) == 0 {
		return dto.PromptAuditSnapshot{}
	}
	return dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{
		Role: "user", Text: strings.Join(texts, "\n"), User: true,
	}}}
}

func looksLikePromptAuditTaskPayload(value string) bool {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:image/") || strings.HasPrefix(lower, "data:audio/") || strings.HasPrefix(lower, "data:video/") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	if len(value) < 256 {
		return false
	}
	for _, character := range value {
		alphaNumeric := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if !alphaNumeric && character != '+' && character != '/' && character != '=' {
			return false
		}
	}
	return true
}
