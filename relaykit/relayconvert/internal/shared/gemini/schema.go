package gemini

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func OutputFormat(config *dto.GeminiChatGenerationConfig) (*dto.ResponseFormat, error) {
	var schema any
	if len(config.ResponseJsonSchema) > 0 {
		if err := kitutil.Unmarshal(config.ResponseJsonSchema, &schema); err != nil {
			return nil, err
		}
	}
	if config.ResponseSchema != nil {
		legacy, err := kitutil.Any2Type[map[string]any](config.ResponseSchema)
		if err != nil {
			return nil, err
		}
		converted, err := openAPIOutputSchema(legacy)
		if err != nil {
			return nil, err
		}
		if schema != nil && !reflect.DeepEqual(schema, converted) {
			return nil, fmt.Errorf("conflicting responseSchema and responseJsonSchema")
		}
		schema = converted
	}
	if schema == nil {
		switch config.ResponseMimeType {
		case "", "text/plain":
			return nil, nil
		case "application/json":
			return &dto.ResponseFormat{Type: "json_object"}, nil
		default:
			return nil, fmt.Errorf("unsupported output MIME type %q", config.ResponseMimeType)
		}
	}
	if config.ResponseMimeType != "application/json" {
		return nil, fmt.Errorf("output schema requires application/json")
	}
	raw, err := kitutil.Marshal(map[string]any{"name": "response", "strict": true, "schema": schema})
	return &dto.ResponseFormat{Type: "json_schema", JsonSchema: raw}, err
}

func openAPIOutputSchema(node map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(node))
	for key, value := range node {
		switch key {
		case "type":
			kind, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("responseSchema type must be a string")
			}
			result[key] = strings.ToLower(kind)
		case "properties":
			properties, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("responseSchema properties must be an object")
			}
			children := make(map[string]any, len(properties))
			for name, child := range properties {
				object, ok := child.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("responseSchema property must be an object")
				}
				converted, err := openAPIOutputSchema(object)
				if err != nil {
					return nil, err
				}
				children[name] = converted
			}
			result[key] = children
		case "items":
			object, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("responseSchema items must be an object")
			}
			converted, err := openAPIOutputSchema(object)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		case "anyOf":
			values, ok := value.([]any)
			if !ok {
				return nil, fmt.Errorf("responseSchema anyOf must be an array")
			}
			children := make([]any, 0, len(values))
			for _, child := range values {
				object, ok := child.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("responseSchema anyOf entry must be an object")
				}
				converted, err := openAPIOutputSchema(object)
				if err != nil {
					return nil, err
				}
				children = append(children, converted)
			}
			result[key] = children
		case "nullable":
			if _, ok := value.(bool); !ok {
				return nil, fmt.Errorf("responseSchema nullable must be boolean")
			}
		default:
			result[key] = value
		}
	}
	if node["nullable"] == true {
		return map[string]any{"anyOf": []any{result, map[string]any{"type": "null"}}}, nil
	}
	return result, nil
}

// ApplyOutputFormat preserves the JSON Schema itself. The OpenAPI responseSchema
// channel cannot express all constraints accepted by OpenAI structured output.
func ApplyOutputFormat(config *dto.GeminiChatGenerationConfig, format *dto.ResponseFormat) error {
	if format == nil || format.Type == "" || format.Type == "text" {
		return nil
	}
	switch format.Type {
	case "json_object":
		config.ResponseMimeType = "application/json"
	case "json_schema":
		var schema dto.FormatJsonSchema
		if err := kitutil.Unmarshal(format.JsonSchema, &schema); err != nil {
			return fmt.Errorf("invalid output JSON Schema: %w", err)
		}
		if schema.Schema == nil {
			return fmt.Errorf("output JSON Schema is required")
		}
		if field := UnsupportedOutputSchemaField(schema.Schema, 0); field != "" {
			return fmt.Errorf("Gemini output schema does not preserve constraint %q", field)
		}
		encoded, err := kitutil.Marshal(schema.Schema)
		if err != nil {
			return err
		}
		config.ResponseMimeType = "application/json"
		config.ResponseJsonSchema = encoded
		config.ResponseSchema = nil
	default:
		return fmt.Errorf("output format %q has no verified Gemini mapping", format.Type)
	}
	return nil
}

// Gemini accepts only a subset of JSON Schema for generated output. In
// particular, oneOf is interpreted as anyOf, so it cannot enforce exclusivity.
func UnsupportedOutputSchemaField(schema any, depth int) string {
	if depth > 64 {
		return "schema depth"
	}
	object, ok := schema.(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := object[key]
		switch key {
		case "$id", "$schema", "$anchor", "type", "format", "title", "description", "enum", "minItems", "maxItems", "minimum", "maximum", "required", "propertyOrdering":
		case "properties", "$defs":
			properties, _ := value.(map[string]any)
			for _, child := range properties {
				if field := UnsupportedOutputSchemaField(child, depth+1); field != "" {
					return field
				}
			}
		case "items", "additionalProperties":
			if field := UnsupportedOutputSchemaField(value, depth+1); field != "" {
				return field
			}
		case "anyOf", "prefixItems":
			children, _ := value.([]any)
			for _, child := range children {
				if field := UnsupportedOutputSchemaField(child, depth+1); field != "" {
					return field
				}
			}
		default:
			return key
		}
	}
	return ""
}

var geminiOpenAPISchemaAllowedFields = map[string]struct{}{
	"anyOf":            {},
	"default":          {},
	"description":      {},
	"enum":             {},
	"example":          {},
	"format":           {},
	"items":            {},
	"maxItems":         {},
	"maxLength":        {},
	"maxProperties":    {},
	"maximum":          {},
	"minItems":         {},
	"minLength":        {},
	"minProperties":    {},
	"minimum":          {},
	"nullable":         {},
	"pattern":          {},
	"properties":       {},
	"propertyOrdering": {},
	"required":         {},
	"title":            {},
	"type":             {},
}

const geminiFunctionSchemaMaxDepth = 64

func CleanFunctionParameters(params any) any {
	return cleanGeminiFunctionParametersWithDepth(params, 0)
}

// PrepareFunctionDeclaration follows Gemini's two schema channels: the
// restricted OpenAPI-style parameters field for simple schemas and
// parametersJsonSchema for richer JSON Schema. CC Switch uses the same split
// to avoid silently deleting constraints such as additionalProperties, $ref,
// oneOf, or exclusiveMinimum.
func PrepareFunctionDeclaration(function *dto.FunctionRequest) {
	if function == nil {
		return
	}
	schema := normalizeFunctionJSONSchema(function.Parameters)
	if schemaMap, ok := schema.(map[string]any); ok {
		typeName, _ := schemaMap["type"].(string)
		if strings.TrimSpace(typeName) == "" {
			schemaMap["type"] = "object"
			typeName = "object"
		}
		if strings.EqualFold(strings.TrimSpace(typeName), "object") {
			if _, exists := schemaMap["properties"]; !exists {
				schemaMap["properties"] = map[string]any{}
			}
		}
		schema = schemaMap
	} else if schema == nil {
		schema = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	if requiresParametersJSONSchema(schema) {
		function.Parameters = nil
		function.ParametersJsonSchema = schema
		return
	}
	function.Parameters = CleanFunctionParameters(schema)
	function.ParametersJsonSchema = nil
}

func normalizeFunctionJSONSchema(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "$schema" || key == "$id" {
				continue
			}
			result[key] = normalizeFunctionJSONSchema(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeFunctionJSONSchema(item)
		}
		return result
	default:
		return value
	}
}

func requiresParametersJSONSchema(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			switch key {
			case "type":
				if _, ok := item.([]any); ok {
					return true
				}
			case "format", "title", "description", "nullable", "enum", "maxItems", "minItems",
				"required", "minProperties", "maxProperties", "minLength", "maxLength", "pattern",
				"example", "propertyOrdering", "default", "minimum", "maximum":
			case "properties":
				properties, ok := item.(map[string]any)
				if !ok {
					return true
				}
				for _, property := range properties {
					if requiresParametersJSONSchema(property) {
						return true
					}
				}
			case "items":
				if _, ok := item.(map[string]any); !ok || requiresParametersJSONSchema(item) {
					return true
				}
			case "anyOf":
				items, ok := item.([]any)
				if !ok {
					return true
				}
				for _, nested := range items {
					if requiresParametersJSONSchema(nested) {
						return true
					}
				}
			default:
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if requiresParametersJSONSchema(item) {
				return true
			}
		}
	}
	return false
}

func cleanGeminiFunctionParametersWithDepth(params any, depth int) any {
	if params == nil {
		return nil
	}

	if depth >= geminiFunctionSchemaMaxDepth {
		return cleanGeminiFunctionParametersShallow(params)
	}

	switch v := params.(type) {
	case map[string]any:
		cleanedMap := make(map[string]any, len(v))
		for key, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[key]; ok {
				cleanedMap[key] = val
			}
		}

		normalizeGeminiSchemaTypeAndNullable(cleanedMap)

		if props, ok := cleanedMap["properties"].(map[string]any); ok && props != nil {
			cleanedProps := make(map[string]any)
			for propName, propValue := range props {
				cleanedProps[propName] = cleanGeminiFunctionParametersWithDepth(propValue, depth+1)
			}
			cleanedMap["properties"] = cleanedProps
		}

		if items, ok := cleanedMap["items"].(map[string]any); ok && items != nil {
			cleanedMap["items"] = cleanGeminiFunctionParametersWithDepth(items, depth+1)
		}
		if itemsArray, ok := cleanedMap["items"].([]any); ok && len(itemsArray) > 0 {
			cleanedMap["items"] = cleanGeminiFunctionParametersWithDepth(itemsArray[0], depth+1)
		}

		if nested, ok := cleanedMap["anyOf"].([]any); ok && nested != nil {
			cleanedNested := make([]any, len(nested))
			for i, item := range nested {
				cleanedNested[i] = cleanGeminiFunctionParametersWithDepth(item, depth+1)
			}
			cleanedMap["anyOf"] = cleanedNested
		}

		return cleanedMap
	case []any:
		cleanedArray := make([]any, len(v))
		for i, item := range v {
			cleanedArray[i] = cleanGeminiFunctionParametersWithDepth(item, depth+1)
		}
		return cleanedArray
	default:
		return params
	}
}

func cleanGeminiFunctionParametersShallow(params any) any {
	switch v := params.(type) {
	case map[string]any:
		cleanedMap := make(map[string]any, len(v))
		for key, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[key]; ok {
				cleanedMap[key] = val
			}
		}
		normalizeGeminiSchemaTypeAndNullable(cleanedMap)
		delete(cleanedMap, "properties")
		delete(cleanedMap, "items")
		delete(cleanedMap, "anyOf")
		return cleanedMap
	case []any:
		return []any{}
	default:
		return params
	}
}

func normalizeGeminiSchemaTypeAndNullable(schema map[string]any) {
	rawType, ok := schema["type"]
	if !ok || rawType == nil {
		return
	}

	normalize := func(t string) (string, bool) {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "object":
			return "OBJECT", false
		case "array":
			return "ARRAY", false
		case "string":
			return "STRING", false
		case "integer":
			return "INTEGER", false
		case "number":
			return "NUMBER", false
		case "boolean":
			return "BOOLEAN", false
		case "null":
			return "", true
		default:
			return t, false
		}
	}

	switch typed := rawType.(type) {
	case string:
		normalized, isNull := normalize(typed)
		if isNull {
			schema["nullable"] = true
			delete(schema, "type")
			return
		}
		schema["type"] = normalized
	case []any:
		nullable := false
		var chosen string
		for _, item := range typed {
			if value, ok := item.(string); ok {
				normalized, isNull := normalize(value)
				if isNull {
					nullable = true
					continue
				}
				if chosen == "" {
					chosen = normalized
				}
			}
		}
		if nullable {
			schema["nullable"] = true
		}
		if chosen != "" {
			schema["type"] = chosen
		} else {
			delete(schema, "type")
		}
	}
}

func RemoveAdditionalProperties(schema any, depth int) any {
	if depth >= 5 {
		return schema
	}

	value, ok := schema.(map[string]any)
	if !ok || len(value) == 0 {
		return schema
	}
	delete(value, "title")
	delete(value, "$schema")
	if typeVal, exists := value["type"]; !exists || (typeVal != "object" && typeVal != "array") {
		return schema
	}
	switch value["type"] {
	case "object":
		delete(value, "additionalProperties")
		if properties, ok := value["properties"].(map[string]any); ok {
			for key, nested := range properties {
				properties[key] = RemoveAdditionalProperties(nested, depth+1)
			}
		}
		for _, field := range []string{"allOf", "anyOf", "oneOf"} {
			if nested, ok := value[field].([]any); ok {
				for i, item := range nested {
					nested[i] = RemoveAdditionalProperties(item, depth+1)
				}
			}
		}
	case "array":
		if items, ok := value["items"].(map[string]any); ok {
			value["items"] = RemoveAdditionalProperties(items, depth+1)
		}
	}

	return value
}
