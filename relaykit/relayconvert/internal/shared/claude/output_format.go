package claude

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// OutputFormat reads both the current Messages wire field and its legacy alias.
// The schema is retained verbatim; unsupported constraints are never removed.
func OutputFormat(request *dto.ClaudeRequest) (*dto.ResponseFormat, error) {
	var config map[string]any
	if len(request.OutputConfig) > 0 {
		if err := kitutil.Unmarshal(request.OutputConfig, &config); err != nil {
			return nil, fmt.Errorf("invalid output_config: %w", err)
		}
	}
	format := config["format"]
	if len(request.OutputFormat) > 0 {
		var legacy any
		if err := kitutil.Unmarshal(request.OutputFormat, &legacy); err != nil {
			return nil, fmt.Errorf("invalid output_format: %w", err)
		}
		if format != nil && legacy != nil && !reflect.DeepEqual(format, legacy) {
			return nil, fmt.Errorf("output_format conflicts with output_config.format")
		}
		if format == nil {
			format = legacy
		}
	}
	if format == nil {
		return nil, nil
	}
	object, ok := format.(map[string]any)
	if !ok || object["type"] != "json_schema" || object["schema"] == nil {
		return nil, fmt.Errorf("output_config.format / output_format requires a json_schema and schema")
	}
	for key := range object {
		if key != "type" && key != "schema" {
			return nil, fmt.Errorf("unsupported output_config.format field %q", key)
		}
	}
	if err := ValidateOutputSchema(object["schema"], false); err != nil {
		return nil, err
	}
	raw, err := kitutil.Marshal(map[string]any{"name": "response", "strict": true, "schema": object["schema"]})
	return &dto.ResponseFormat{Type: "json_schema", JsonSchema: raw}, err
}

func ApplyOutputFormat(request *dto.ClaudeRequest, format *dto.ResponseFormat) error {
	if format == nil || format.Type == "" || format.Type == "text" {
		return nil
	}
	if format.Type != "json_schema" {
		return fmt.Errorf("output format %q has no verified Messages mapping", format.Type)
	}
	var schema dto.FormatJsonSchema
	if err := kitutil.Unmarshal(format.JsonSchema, &schema); err != nil {
		return fmt.Errorf("invalid output JSON Schema: %w", err)
	}
	if err := ValidateOutputSchema(schema.Schema, false); err != nil {
		return err
	}
	config := map[string]any{}
	if len(request.OutputConfig) > 0 {
		if err := kitutil.Unmarshal(request.OutputConfig, &config); err != nil {
			return err
		}
	}
	config["format"] = map[string]any{"type": "json_schema", "schema": schema.Schema}
	var err error
	request.OutputConfig, err = kitutil.Marshal(config)
	return err
}

// ValidateOutputSchema accepts a conservative documented subset. OpenAI's strict
// output additionally requires every object property to be required. Adding
// missing required entries would change the client's output contract.
func ValidateOutputSchema(schema any, openAI bool) error {
	root, ok := schema.(map[string]any)
	if !ok || root["type"] != "object" {
		return fmt.Errorf("output format requires an object JSON Schema")
	}
	return validateOutputSchemaNode(root, root, openAI, map[string]bool{}, 0)
}

func validateOutputSchemaNode(value any, root map[string]any, openAI bool, refs map[string]bool, depth int) error {
	if depth > 64 {
		return fmt.Errorf("output schema exceeds supported depth")
	}
	node, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("output schema node must be an object")
	}
	if node["type"] == "object" || node["properties"] != nil {
		if node["additionalProperties"] != false {
			return fmt.Errorf("output schema requires additionalProperties:false")
		}
		if openAI {
			properties, _ := node["properties"].(map[string]any)
			required, _ := node["required"].([]any)
			for name := range properties {
				if !slices.Contains(required, any(name)) {
					return fmt.Errorf("OpenAI strict output schema cannot preserve optional property %q", name)
				}
			}
		}
	}
	keys := make([]string, 0, len(node))
	for key := range node {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		child := node[key]
		switch key {
		case "type", "title", "description", "required", "additionalProperties", "$schema":
		case "enum":
			values, ok := child.([]any)
			if !ok {
				return fmt.Errorf("output schema enum must be an array")
			}
			for _, item := range values {
				switch item.(type) {
				case map[string]any, []any:
					return fmt.Errorf("output schema complex enum is unsupported")
				}
			}
		case "const":
			switch child.(type) {
			case map[string]any, []any:
				return fmt.Errorf("output schema complex const is unsupported")
			}
		case "properties", "$defs", "definitions":
			children, ok := child.(map[string]any)
			if !ok {
				return fmt.Errorf("output schema %s must be an object", key)
			}
			for _, item := range children {
				if err := validateOutputSchemaNode(item, root, openAI, refs, depth+1); err != nil {
					return err
				}
			}
		case "items":
			if err := validateOutputSchemaNode(child, root, openAI, refs, depth+1); err != nil {
				return err
			}
		case "anyOf":
			children, ok := child.([]any)
			if !ok {
				return fmt.Errorf("output schema anyOf must be an array")
			}
			for _, item := range children {
				if err := validateOutputSchemaNode(item, root, openAI, refs, depth+1); err != nil {
					return err
				}
			}
		case "$ref":
			ref, ok := child.(string)
			if !ok || !strings.HasPrefix(ref, "#/") || refs[ref] {
				return fmt.Errorf("recursive or external output schema reference is unsupported")
			}
			var target any = root
			for part := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
				object, ok := target.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid output schema reference %q", ref)
				}
				target = object[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
			}
			refs[ref] = true
			err := validateOutputSchemaNode(target, root, openAI, refs, depth+1)
			delete(refs, ref)
			if err != nil {
				return err
			}
		case "minItems":
			if child != float64(0) && child != float64(1) {
				return fmt.Errorf("Messages output schema does not preserve minItems above 1")
			}
		case "pattern":
			pattern, ok := child.(string)
			if !ok || strings.Contains(pattern, "(?") || strings.Contains(pattern, `\b`) || strings.Contains(pattern, `\B`) {
				return fmt.Errorf("unsupported output schema pattern")
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("unsupported output schema pattern: %w", err)
			}
		case "format":
			if !slices.Contains([]any{"date-time", "date", "time", "duration", "email", "hostname", "uri", "ipv4", "ipv6", "uuid"}, child) {
				return fmt.Errorf("unsupported output schema format %v", child)
			}
		default:
			return fmt.Errorf("output schema does not preserve constraint %q", key)
		}
	}
	return nil
}
