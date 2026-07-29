package oairesponses

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func openAIResponsesRequestFromAny(request any) (*dto.OpenAIResponsesRequest, error) {
	responsesRequest, ok := request.(*dto.OpenAIResponsesRequest)
	if !ok {
		if value, ok := request.(dto.OpenAIResponsesRequest); ok {
			responsesRequest = &value
		}
	}
	if responsesRequest == nil {
		return nil, fmt.Errorf("expected OpenAI responses request, got %T", request)
	}
	return responsesRequest, nil
}

func OpenAIResponsesRequestFromAny(request any) (*dto.OpenAIResponsesRequest, error) {
	return openAIResponsesRequestFromAny(request)
}

func responsesInputItems(raw []byte) ([]map[string]any, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}

	switch kitutil.GetJsonType(raw) {
	case "string":
		input, err := responsesJSONString(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid input string: %w", err)
		}
		return []map[string]any{
			{
				"role":    "user",
				"content": input,
			},
		}, nil
	case "array":
		var items []map[string]any
		if err := kitutil.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("invalid input array: %w", err)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported responses input type %q", kitutil.GetJsonType(raw))
	}
}

func InputItems(raw []byte) ([]map[string]any, error) {
	return responsesInputItems(raw)
}

func responsesContentParts(content any) ([]map[string]any, error) {
	switch typed := content.(type) {
	case nil:
		return nil, nil
	case string:
		return []map[string]any{{"type": "input_text", "text": typed}}, nil
	case []map[string]any:
		return typed, nil
	case []any:
		parts := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			switch part := item.(type) {
			case string:
				parts = append(parts, map[string]any{"type": "input_text", "text": part})
			case map[string]any:
				parts = append(parts, part)
			default:
				raw, err := kitutil.Marshal(part)
				if err != nil {
					return nil, err
				}
				parts = append(parts, map[string]any{"type": "input_text", "text": string(raw)})
			}
		}
		return parts, nil
	default:
		raw, err := kitutil.Marshal(typed)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{"type": "input_text", "text": string(raw)}}, nil
	}
}

func ContentParts(content any) ([]map[string]any, error) {
	return responsesContentParts(content)
}

func responsesReasoningEffort(req *dto.OpenAIResponsesRequest) string {
	if req == nil || req.Reasoning == nil {
		return ""
	}
	return req.Reasoning.Effort
}

func ReasoningEffort(req *dto.OpenAIResponsesRequest) string {
	return responsesReasoningEffort(req)
}

func responsesObjectValue(value any, fallbackKey string) map[string]any {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return typed
	case string:
		var object map[string]any
		if err := kitutil.Unmarshal([]byte(typed), &object); err == nil {
			return object
		}
		var array []any
		if err := kitutil.Unmarshal([]byte(typed), &array); err == nil {
			return map[string]any{fallbackKey: array}
		}
		return map[string]any{fallbackKey: typed}
	case []any:
		return map[string]any{fallbackKey: typed}
	default:
		return map[string]any{fallbackKey: typed}
	}
}

func ObjectValue(value any, fallbackKey string) map[string]any {
	return responsesObjectValue(value, fallbackKey)
}

func responsesFunctionArgumentsObject(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return typed, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return map[string]any{}, nil
		}
		var object map[string]any
		if err := kitutil.Unmarshal([]byte(typed), &object); err != nil || object == nil {
			return nil, fmt.Errorf("function_call arguments must be a JSON object")
		}
		return object, nil
	default:
		encoded, err := kitutil.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("marshal function_call arguments: %w", err)
		}
		var object map[string]any
		if err := kitutil.Unmarshal(encoded, &object); err != nil || object == nil {
			return nil, fmt.Errorf("function_call arguments must be a JSON object")
		}
		return object, nil
	}
}

func responsesFunctionArgumentsString(value any) (string, error) {
	object, err := responsesFunctionArgumentsObject(value)
	if err != nil {
		return "", err
	}
	encoded, err := kitutil.Marshal(object)
	if err != nil {
		return "", fmt.Errorf("marshal function_call arguments: %w", err)
	}
	return string(encoded), nil
}

func filterIncompleteResponsesToolHistory(items []map[string]any, dropUnanswered bool) ([]map[string]any, error) {
	type callBatch struct {
		indexes []int
		ids     []string
	}

	batches := make([]callBatch, 0)
	currentBatch := -1
	knownCallIDs := make(map[string]struct{})
	outputCounts := make(map[string]int)
	for index, item := range items {
		itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
		switch itemType {
		case ResponsesInputTypeFunctionCall, ResponsesInputTypeCustomToolCall, "tool_search_call":
			if currentBatch < 0 {
				batches = append(batches, callBatch{})
				currentBatch = len(batches) - 1
			}
			callID := CallID(item)
			batches[currentBatch].indexes = append(batches[currentBatch].indexes, index)
			batches[currentBatch].ids = append(batches[currentBatch].ids, callID)
			if callID != "" {
				knownCallIDs[callID] = struct{}{}
			}
		case "reasoning":
			// Reasoning belongs to the surrounding assistant turn and does not
			// split a batch of parallel tool calls.
		case ResponsesInputTypeFunctionCallOutput, ResponsesInputTypeCustomToolOutput, "tool_search_output":
			currentBatch = -1
			if callID := CallID(item); callID != "" {
				outputCounts[callID]++
			}
		default:
			currentBatch = -1
		}
	}

	droppedIndexes := make(map[int]struct{})
	droppedCallIDs := make(map[string]struct{})
	for _, batch := range batches {
		complete := len(batch.ids) > 0
		seen := make(map[string]struct{}, len(batch.ids))
		batchOutputCount := 0
		for _, callID := range batch.ids {
			batchOutputCount += outputCounts[callID]
		}
		for offset, callID := range batch.ids {
			item := items[batch.indexes[offset]]
			if callID == "" || strings.TrimSpace(kitutil.Interface2String(item["status"])) == "incomplete" ||
				(batchOutputCount > 0 || dropUnanswered) && outputCounts[callID] != 1 {
				complete = false
				continue
			}
			if _, exists := seen[callID]; exists {
				complete = false
				continue
			}
			seen[callID] = struct{}{}
		}
		if complete {
			continue
		}
		for offset, index := range batch.indexes {
			droppedIndexes[index] = struct{}{}
			if callID := batch.ids[offset]; callID != "" {
				droppedCallIDs[callID] = struct{}{}
			}
		}
	}

	filtered := make([]map[string]any, 0, len(items))
	for index, item := range items {
		itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
		switch itemType {
		case ResponsesInputTypeFunctionCall, ResponsesInputTypeCustomToolCall, "tool_search_call":
			if _, drop := droppedIndexes[index]; drop {
				continue
			}
		case ResponsesInputTypeFunctionCallOutput, ResponsesInputTypeCustomToolOutput, "tool_search_output":
			callID := CallID(item)
			if _, drop := droppedCallIDs[callID]; drop {
				continue
			}
			if _, declared := knownCallIDs[callID]; !declared {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func responsesGeminiResponseMap(value any) map[string]interface{} {
	switch typed := value.(type) {
	case nil:
		return map[string]interface{}{}
	case map[string]any:
		return typed
	case string:
		var object map[string]interface{}
		if err := kitutil.Unmarshal([]byte(typed), &object); err == nil {
			return object
		}
		var array []interface{}
		if err := kitutil.Unmarshal([]byte(typed), &array); err == nil {
			return map[string]interface{}{"result": array}
		}
		return map[string]interface{}{"content": typed}
	case []any:
		return map[string]interface{}{"result": typed}
	default:
		return map[string]interface{}{"content": typed}
	}
}

func GeminiResponseMap(value any) map[string]interface{} {
	return responsesGeminiResponseMap(value)
}

func responsesParallelToolCalls(raw []byte) *bool {
	if !rawJSONPresent(raw) || kitutil.GetJsonType(raw) != "boolean" {
		return nil
	}
	var parallelToolCalls bool
	if err := kitutil.Unmarshal(raw, &parallelToolCalls); err != nil {
		return nil
	}
	return &parallelToolCalls
}

func ParallelToolCalls(raw []byte) *bool {
	return responsesParallelToolCalls(raw)
}

func ContentPartToFileSource(part map[string]any) types.FileSource {
	partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
	var data string
	var mimeType string

	switch partType {
	case "input_image":
		data, mimeType = responsesPartDataAndMime(part, "image_url", "url")
	case "input_file":
		data, mimeType = responsesPartDataAndMime(part, "file", "file_data", "file_url", "url")
	case "input_audio":
		data, mimeType = responsesPartDataAndMime(part, "input_audio", "data", "url")
		if mimeType == "" {
			if payload, ok := part["input_audio"].(map[string]any); ok {
				if format := strings.TrimSpace(kitutil.Interface2String(payload["format"])); format != "" {
					mimeType = "audio/" + format
				}
			}
		}
	case "input_video":
		data, mimeType = responsesPartDataAndMime(part, "video_url", "url")
	}
	if data == "" {
		return nil
	}
	return types.NewFileSourceFromData(data, mimeType)
}

func responsesPartDataAndMime(part map[string]any, keys ...string) (string, string) {
	mimeType := strings.TrimSpace(kitutil.Interface2String(part["mime_type"]))
	for _, key := range keys {
		value, ok := part[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed, mimeType
			}
		case map[string]any:
			if mimeType == "" {
				mimeType = strings.TrimSpace(kitutil.Interface2String(typed["mime_type"]))
			}
			for _, nestedKey := range []string{"url", "file_data", "file_url", "data"} {
				if data := strings.TrimSpace(kitutil.Interface2String(typed[nestedKey])); data != "" {
					return data, mimeType
				}
			}
		}
	}
	return "", mimeType
}
