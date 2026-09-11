package toolconv

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ExtractRequestForConversion leaves Responses client tools with the protocol
// converter, which owns reversible namespace, custom-tool and tool-search
// lowering. Hosted tools still use the shared provider capability conversion.
func ExtractRequestForConversion(format types.RelayFormat, request any) (any, Set, error) {
	if format != types.RelayFormatOpenAIResponses {
		return ExtractRequest(format, request)
	}
	source, ok := request.(*dto.OpenAIResponsesRequest)
	if !ok {
		value, valid := request.(dto.OpenAIResponsesRequest)
		if !valid {
			return nil, Set{}, fmt.Errorf("expected Responses request, got %T", request)
		}
		source = &value
	}
	var tools []json.RawMessage
	if len(source.Tools) > 0 && kitutil.GetJsonType(source.Tools) != "null" {
		if err := kitutil.Unmarshal(source.Tools, &tools); err != nil {
			return nil, Set{}, fmt.Errorf("invalid Responses tools: %w", err)
		}
	}
	var clientTools, hostedTools []json.RawMessage
	for _, raw := range tools {
		if kitutil.GetJsonType(raw) == "string" {
			clientTools = append(clientTools, raw)
			continue
		}
		var tool struct {
			Type string `json:"type"`
		}
		if err := kitutil.Unmarshal(raw, &tool); err != nil {
			return nil, Set{}, err
		}
		switch strings.TrimSpace(tool.Type) {
		case "", "function", "custom", "freeform", "namespace", "tool_search", "local_shell":
			clientTools = append(clientTools, raw)
		default:
			hostedTools = append(hostedTools, raw)
		}
	}
	hosted := *source
	hosted.Tools = nil
	hosted.ToolChoice = nil
	hosted.ParallelToolCalls = nil
	if len(hostedTools) > 0 {
		hosted.Tools, _ = kitutil.Marshal(hostedTools)
	}
	clientChoice := source.ToolChoice
	if len(source.ToolChoice) > 0 && kitutil.GetJsonType(source.ToolChoice) == "object" {
		var choice struct {
			Type string `json:"type"`
		}
		if err := kitutil.Unmarshal(source.ToolChoice, &choice); err != nil {
			return nil, Set{}, err
		}
		switch choice.Type {
		case "function", "custom", "tool_search", "local_shell", "allowed_tools", "":
		default:
			hosted.ToolChoice = source.ToolChoice
			clientChoice = nil
		}
	} else if len(clientTools) == 0 {
		hosted.ToolChoice = source.ToolChoice
	}
	value, set, err := extractOpenAIResponsesRequest(&hosted)
	if err != nil {
		return nil, set, err
	}
	prepared := value.(*dto.OpenAIResponsesRequest)
	if len(clientTools) > 0 {
		prepared.Tools, _ = kitutil.Marshal(clientTools)
	}
	prepared.ToolChoice = clientChoice
	prepared.ParallelToolCalls = source.ParallelToolCalls
	return prepared, set, nil
}
