package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func UnmarshalJsonStr(data string, v any) error {
	return json.Unmarshal(StringToByteSlice(data), v)
}

func DecodeJson(reader io.Reader, v any) error {
	return json.NewDecoder(reader).Decode(v)
}

func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// WalkJsonStrings visits JSON string values in their source order. Object keys
// are appended to path; arrays preserve their item order without adding a path
// component. This keeps callers on the shared JSON implementation while
// allowing order-sensitive extraction without materializing objects as maps.
func WalkJsonStrings(reader io.Reader, visit func(path []string, value string) error) error {
	if reader == nil {
		return fmt.Errorf("JSON reader is required")
	}
	if visit == nil {
		return fmt.Errorf("JSON string visitor is required")
	}
	decoder := json.NewDecoder(reader)
	if err := walkJsonStringValue(decoder, nil, visit); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func walkJsonStringValue(decoder *json.Decoder, path []string, visit func([]string, string) error) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		if value, ok := token.(string); ok {
			return visit(append([]string(nil), path...), value)
		}
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			childPath := append(append([]string(nil), path...), key)
			if err := walkJsonStringValue(decoder, childPath, visit); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("invalid JSON object terminator")
		}
	case '[':
		for decoder.More() {
			if err := walkJsonStringValue(decoder, path, visit); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("invalid JSON array terminator")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func GetJsonType(data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "unknown"
	}
	firstChar := trimmed[0]
	switch firstChar {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// JsonRawMessageToString returns JSON strings as their decoded value and other JSON values as raw text.
func JsonRawMessageToString(data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] != '"' {
		return string(trimmed)
	}
	var value string
	if err := Unmarshal(trimmed, &value); err != nil {
		return string(trimmed)
	}
	return value
}
