package xai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/gin-gonic/gin"
)

const xaiResponsesClientToolBridgeContextKey = "xai_responses_client_tool_bridge"

var xaiResponsesSupportedToolTypes = map[string]struct{}{
	"code_execution":     {},
	"code_interpreter":   {},
	"collections_search": {},
	"file_search":        {},
	"function":           {},
	"image_generation":   {},
	"mcp":                {},
	"shell":              {},
	"web_search":         {},
	"x_search":           {},
}

func prepareXAIResponsesRequest(c *gin.Context, request *dto.OpenAIResponsesRequest) error {
	if request == nil {
		return nil
	}
	clearXAIResponsesClientToolBridge(c)

	// xAI's public Responses schema rejects these OpenAI/Codex-specific fields.
	request.PromptCacheRetention = nil
	request.SafetyIdentifier = nil
	if xaiModelRejectsReasoning(request.Model) {
		request.Reasoning = nil
	}

	input, changed, err := sanitizeXAIResponsesJSON(request.Input, true)
	if err != nil {
		return fmt.Errorf("sanitize xAI Responses input: %w", err)
	}
	if changed {
		request.Input = input
	}
	tools, changed, err := sanitizeXAIResponsesJSON(request.Tools, false)
	if err != nil {
		return fmt.Errorf("sanitize xAI Responses tools: %w", err)
	}
	if changed {
		request.Tools = tools
	}

	bridge, _, err := relayconvert.LowerResponsesClientTools(request)
	if err != nil {
		return fmt.Errorf("lower xAI Responses client tools: %w", err)
	}
	if err := normalizeAndValidateXAIResponsesTools(request); err != nil {
		return err
	}
	if bridge != nil && bridge.HasMappings() && c != nil {
		c.Set(xaiResponsesClientToolBridgeContextKey, bridge)
	}
	return nil
}

func sanitizeXAIResponsesJSON(raw json.RawMessage, input bool) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw, false, nil
	}
	var value any
	if err := common.Unmarshal(trimmed, &value); err != nil {
		return nil, false, err
	}
	changed := removeXAIResponsesField(value, "external_web_access")
	if input {
		changed = removeXAINullReasoningContent(value) || changed
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := common.Marshal(value)
	return encoded, true, err
}

func removeXAIResponsesField(value any, field string) bool {
	changed := false
	switch typed := value.(type) {
	case map[string]any:
		if _, exists := typed[field]; exists {
			delete(typed, field)
			changed = true
		}
		for _, child := range typed {
			changed = removeXAIResponsesField(child, field) || changed
		}
	case []any:
		for _, child := range typed {
			changed = removeXAIResponsesField(child, field) || changed
		}
	}
	return changed
}

func removeXAINullReasoningContent(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	changed := false
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok || strings.TrimSpace(common.Interface2String(item["type"])) != "reasoning" {
			continue
		}
		if content, exists := item["content"]; exists && content == nil {
			delete(item, "content")
			changed = true
		}
	}
	return changed
}

func normalizeAndValidateXAIResponsesTools(request *dto.OpenAIResponsesRequest) error {
	if request == nil || len(bytes.TrimSpace(request.Tools)) == 0 || bytes.Equal(bytes.TrimSpace(request.Tools), []byte("null")) {
		return nil
	}
	var tools []map[string]any
	if err := common.Unmarshal(request.Tools, &tools); err != nil {
		return fmt.Errorf("invalid xAI Responses tools: %w", err)
	}
	changed := false
	for _, tool := range tools {
		toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
		if toolType == "web_search_preview" {
			toolType = "web_search"
			tool["type"] = toolType
			changed = true
		}
		if _, supported := xaiResponsesSupportedToolTypes[toolType]; supported {
			continue
		}
		if toolType == "" {
			return fmt.Errorf("xAI Responses tool is missing type")
		}
		return fmt.Errorf("xAI Responses does not support tool type %q; use a Chat upstream or remove that tool", toolType)
	}
	if changed {
		encoded, err := common.Marshal(tools)
		if err != nil {
			return err
		}
		request.Tools = encoded
	}
	return nil
}

func xaiModelRejectsReasoning(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = strings.TrimSpace(model[slash+1:])
	}
	switch model {
	case "grok-composer", "grok-composer-2.5-fast", "composer-2.5":
		return true
	default:
		return false
	}
}

func clearXAIResponsesClientToolBridge(c *gin.Context) {
	if c != nil {
		c.Set(xaiResponsesClientToolBridgeContextKey, (*relayconvert.ResponsesClientToolBridge)(nil))
	}
}

func xaiResponsesClientToolBridge(c *gin.Context) *relayconvert.ResponsesClientToolBridge {
	if c == nil {
		return nil
	}
	value, exists := c.Get(xaiResponsesClientToolBridgeContextKey)
	if !exists {
		return nil
	}
	bridge, _ := value.(*relayconvert.ResponsesClientToolBridge)
	return bridge
}

func restoreXAIResponsesBody(c *gin.Context, response *http.Response, stream bool) error {
	bridge := xaiResponsesClientToolBridge(c)
	if bridge == nil || !bridge.HasMappings() || response == nil || response.Body == nil {
		return nil
	}
	if stream {
		response.Body = newXAIResponsesBridgeStreamBody(response.Body, bridge.NewStreamRestorer())
		response.ContentLength = -1
		return nil
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		return closeErr
	}
	restored, err := bridge.RestoreResponseData(body)
	if err != nil {
		return err
	}
	response.Body = io.NopCloser(bytes.NewReader(restored))
	response.ContentLength = int64(len(restored))
	response.Header.Set("Content-Length", fmt.Sprintf("%d", len(restored)))
	return nil
}

type xaiResponsesBridgeStreamBody struct {
	*io.PipeReader
	source io.Closer
}

func (b *xaiResponsesBridgeStreamBody) Close() error {
	readerErr := b.PipeReader.Close()
	sourceErr := b.source.Close()
	if readerErr != nil {
		return readerErr
	}
	return sourceErr
}

func newXAIResponsesBridgeStreamBody(source io.ReadCloser, restorer *relayconvert.ResponsesClientToolStreamRestorer) io.ReadCloser {
	reader, writer := io.Pipe()
	body := &xaiResponsesBridgeStreamBody{PipeReader: reader, source: source}
	go func() {
		defer func() { _ = source.Close() }()
		buffered := bufio.NewWriterSize(writer, 4*1024)
		writePayload := func(payload []byte) error {
			if _, err := buffered.WriteString("data: "); err != nil {
				return err
			}
			if _, err := buffered.Write(payload); err != nil {
				return err
			}
			if _, err := buffered.WriteString("\n\n"); err != nil {
				return err
			}
			return buffered.Flush()
		}

		err := helper.ScanJSONSSE(source, func(data string) (bool, error) {
			if data == "[DONE]" {
				return true, writePayload([]byte(data))
			}
			payloads, restoreErr := restorer.RestoreData([]byte(data))
			if restoreErr != nil {
				return false, restoreErr
			}
			for _, payload := range payloads {
				if err := writePayload(payload); err != nil {
					return false, err
				}
			}
			return false, nil
		})
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		_ = buffered.Flush()
		_ = writer.Close()
	}()
	return body
}
