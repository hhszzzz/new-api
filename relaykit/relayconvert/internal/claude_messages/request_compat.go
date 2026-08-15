package claudemessages

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func validateClaudeRequestConversion(request *dto.ClaudeRequest, target string) error {
	if request == nil {
		return nil
	}
	if target == "Responses" {
		if len(request.StopSequences) > 0 {
			return fmt.Errorf("stop_sequences cannot be represented by a Responses request")
		}
		if request.TopK != nil {
			return fmt.Errorf("top_k cannot be represented by a Responses request")
		}
	}
	// context_management is deliberately absent from the list below: it is a
	// best-effort server-side trimming directive, so conversion drops it and the
	// upstream simply sees the untrimmed history. Whether a lossy route may be
	// chosen at all is gated by the host's protocol plan (AllowLossyConversion).
	for _, field := range []struct {
		name    string
		present bool
	}{
		{name: "output_format", present: meaningfulClaudeRawField(request.OutputFormat)},
		{name: "container", present: meaningfulClaudeRawField(request.Container)},
		{name: "mcp_servers", present: meaningfulClaudeRawField(request.McpServers)},
		{name: "inference_geo", present: strings.TrimSpace(request.InferenceGeo) != ""},
		{name: "speed", present: meaningfulClaudeRawField(request.Speed)},
		{name: "service_tier", present: strings.TrimSpace(request.ServiceTier) != ""},
	} {
		if field.present {
			return fmt.Errorf("%s requires a native Messages upstream and cannot be converted to %s", field.name, target)
		}
	}
	if claudeOutputConfigHasUnsupportedFields(request.OutputConfig) {
		return fmt.Errorf("output_config fields other than effort require a native Messages upstream and cannot be converted to %s", target)
	}
	return nil
}

func meaningfulClaudeRawField(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	var value any
	if kitutil.Unmarshal(trimmed, &value) != nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return value != nil
	}
}

func claudeOutputConfigHasUnsupportedFields(raw []byte) bool {
	if !meaningfulClaudeRawField(raw) {
		return false
	}
	var config map[string]any
	if kitutil.Unmarshal(raw, &config) != nil || config == nil {
		return true
	}
	for field, value := range config {
		if field != "effort" && value != nil {
			return true
		}
	}
	return false
}
