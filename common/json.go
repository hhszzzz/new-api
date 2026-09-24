package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/gin-gonic/gin/binding"
)

type RawMessage = json.RawMessage

// hostJSONCodec is the single place where the host chooses its JSON engine.
// Swap the implementation here (for example to sonic.ConfigStd) and every
// common.* and kitutil.* JSON helper, including relaykit DTO (un)marshalling,
// follows. Injected from init() rather than main() so tests run on the same
// engine as production: common is imported by virtually every root package
// and test binary, while main() never executes under `go test`.
type hostJSONCodec struct{}

func (hostJSONCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (hostJSONCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (hostJSONCodec) Decode(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

func (hostJSONCodec) Valid(data []byte) bool {
	return json.Valid(data)
}

func init() {
	kitutil.SetCodec(hostJSONCodec{})
}

func Unmarshal(data []byte, v any) error {
	return kitutil.Unmarshal(data, v)
}

func UnmarshalJsonStr(data string, v any) error {
	return kitutil.UnmarshalJsonStr(data, v)
}

func DecodeJson(reader io.Reader, v any) error {
	return kitutil.DecodeJson(reader, v)
}

// DecodeJsonUseNumber preserves integer precision in dynamically shaped data.
func DecodeJsonUseNumber(reader io.Reader, v any) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	return decoder.Decode(v)
}

// DecodeJsonWithValidation decodes JSON and applies Gin's configured binding-tag
// validator, including binding:"required" and any registered custom validators.
func DecodeJsonWithValidation(reader io.Reader, v any) error {
	if err := DecodeJson(reader, v); err != nil {
		return err
	}
	if binding.Validator == nil {
		return nil
	}
	return binding.Validator.ValidateStruct(v)
}

func Marshal(v any) ([]byte, error) {
	return kitutil.Marshal(v)
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

// JsonStreamDecoder consumes one JSON document incrementally, so callers can
// read a large array element by element instead of materializing it at once.
type JsonStreamDecoder struct {
	decoder *json.Decoder
}

func NewJsonStreamDecoder(reader io.Reader) *JsonStreamDecoder {
	return &JsonStreamDecoder{decoder: json.NewDecoder(reader)}
}

// Expect consumes the next token and requires it to be the given delimiter:
// '{' or '[' to enter a value, '}' or ']' to leave it.
func (d *JsonStreamDecoder) Expect(delimiter byte) error {
	token, err := d.decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim(delimiter) {
		return fmt.Errorf("expected JSON delimiter %q, got %v", delimiter, token)
	}
	return nil
}

// More reports whether the current object or array has another element.
func (d *JsonStreamDecoder) More() bool {
	return d.decoder.More()
}

// Key reads the next object key.
func (d *JsonStreamDecoder) Key() (string, error) {
	token, err := d.decoder.Token()
	if err != nil {
		return "", err
	}
	key, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("JSON object key is not a string")
	}
	return key, nil
}

// Decode reads the next complete value into v.
func (d *JsonStreamDecoder) Decode(v any) error {
	return d.decoder.Decode(v)
}

// Skip discards the next complete value token by token, so a large value is
// never held in memory as a whole.
func (d *JsonStreamDecoder) Skip() error {
	depth := 0
	for {
		token, err := d.decoder.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
		if depth == 0 {
			return nil
		}
	}
}

func IndentJson(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, data, "", "  "); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func GetJsonType(data RawMessage) string {
	return kitutil.GetJsonType(data)
}

// JsonRawMessageToString returns JSON strings as their decoded value and other JSON values as raw text.
func JsonRawMessageToString(data RawMessage) string {
	return kitutil.JsonRawMessageToString(data)
}
