package relay

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/gin-gonic/gin"
)

var errPromptOutputAuditLimit = errors.New("prompt audit output capture limit exceeded")

type promptOutputCapture struct {
	memory      bytes.Buffer
	file        *os.File
	size        int
	maxBytes    int
	memoryBytes int
	overflow    bool
	failure     error
}

func (capture *promptOutputCapture) Write(body []byte) (int, error) {
	if capture.overflow {
		return len(body), nil
	}
	if len(body) > capture.maxBytes-capture.size {
		capture.overflow = true
		capture.failure = errPromptOutputAuditLimit
		return 0, errPromptOutputAuditLimit
	}
	if capture.file == nil && capture.size+len(body) > capture.memoryBytes {
		file, err := os.CreateTemp("", "new-api-prompt-output-*")
		if err != nil {
			capture.failure = err
			return 0, err
		}
		if _, err = file.Write(capture.memory.Bytes()); err != nil {
			capture.failure = err
			_ = file.Close()
			_ = os.Remove(file.Name())
			return 0, err
		}
		capture.memory.Reset()
		capture.file = file
	}
	var n int
	var err error
	if capture.file != nil {
		n, err = capture.file.Write(body)
	} else {
		n, err = capture.memory.Write(body)
	}
	capture.size += n
	if err != nil {
		capture.failure = err
	}
	return n, err
}

func (capture *promptOutputCapture) Bytes() ([]byte, error) {
	if capture.file == nil {
		return append([]byte(nil), capture.memory.Bytes()...), nil
	}
	if _, err := capture.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(capture.file, int64(capture.maxBytes)+1))
}

func (capture *promptOutputCapture) Reader() (io.Reader, error) {
	if capture.file == nil {
		return bytes.NewReader(capture.memory.Bytes()), nil
	}
	if _, err := capture.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.LimitReader(capture.file, int64(capture.maxBytes)+1), nil
}

func (capture *promptOutputCapture) WriteFrame(kind int, body []byte) error {
	var header [8]byte
	binary.BigEndian.PutUint32(header[:4], uint32(kind))
	binary.BigEndian.PutUint32(header[4:], uint32(len(body)))
	if _, err := capture.Write(header[:]); err != nil {
		return err
	}
	_, err := capture.Write(body)
	return err
}

func readPromptOutputFrames(reader io.Reader, visit func(int, []byte) error) error {
	for {
		var header [8]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		length := binary.BigEndian.Uint32(header[4:])
		body := make([]byte, int(length))
		if _, err := io.ReadFull(reader, body); err != nil {
			return err
		}
		if err := visit(int(binary.BigEndian.Uint32(header[:4])), body); err != nil {
			return err
		}
	}
}

func (capture *promptOutputCapture) Close() error {
	if capture.file == nil {
		return nil
	}
	name := capture.file.Name()
	err := capture.file.Close()
	removeErr := os.Remove(name)
	return errors.Join(err, removeErr)
}

type promptAuditResponseWriter struct {
	gin.ResponseWriter
	header   http.Header
	status   int
	written  bool
	blocking bool
	capture  promptOutputCapture
}

func newPromptAuditResponseWriter(writer gin.ResponseWriter, setting prompt_audit_setting.PromptAuditSetting) *promptAuditResponseWriter {
	return &promptAuditResponseWriter{
		ResponseWriter: writer,
		header:         writer.Header().Clone(),
		status:         http.StatusOK,
		blocking:       setting.OutputMode == prompt_audit_setting.ModeBlocking,
		capture: promptOutputCapture{
			maxBytes: setting.OutputMaxBytes, memoryBytes: setting.OutputMemoryBytes,
		},
	}
}

func (writer *promptAuditResponseWriter) Header() http.Header {
	if writer.blocking {
		return writer.header
	}
	return writer.ResponseWriter.Header()
}

func (writer *promptAuditResponseWriter) Status() int {
	if !writer.blocking {
		return writer.ResponseWriter.Status()
	}
	return writer.status
}
func (writer *promptAuditResponseWriter) Size() int {
	if !writer.blocking {
		return writer.ResponseWriter.Size()
	}
	return writer.capture.size
}
func (writer *promptAuditResponseWriter) Written() bool {
	if !writer.blocking {
		return writer.ResponseWriter.Written()
	}
	return writer.written
}

func (writer *promptAuditResponseWriter) WriteHeader(status int) {
	if writer.written {
		return
	}
	writer.status = status
	writer.written = true
	if !writer.blocking {
		writer.ResponseWriter.WriteHeader(status)
	}
}

func (writer *promptAuditResponseWriter) WriteHeaderNow() {
	if !writer.written {
		writer.WriteHeader(writer.status)
	}
}

func (writer *promptAuditResponseWriter) Write(body []byte) (int, error) {
	writer.WriteHeaderNow()
	if writer.blocking {
		return writer.capture.Write(body)
	}
	n, err := writer.ResponseWriter.Write(body)
	if n > 0 && !writer.capture.overflow {
		if _, captureErr := writer.capture.Write(body[:n]); captureErr != nil && !errors.Is(captureErr, errPromptOutputAuditLimit) {
			writer.capture.overflow = true
		}
	}
	return n, err
}

func (writer *promptAuditResponseWriter) WriteString(body string) (int, error) {
	return writer.Write([]byte(body))
}

func (writer *promptAuditResponseWriter) Flush() {
	writer.WriteHeaderNow()
	if !writer.blocking {
		writer.ResponseWriter.Flush()
	}
}

func (writer *promptAuditResponseWriter) CloseNotify() <-chan bool {
	return writer.ResponseWriter.CloseNotify()
}

func (writer *promptAuditResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if writer.blocking {
		return nil, nil, errors.New("buffered prompt audit response cannot be hijacked")
	}
	return writer.ResponseWriter.Hijack()
}

func (writer *promptAuditResponseWriter) Pusher() http.Pusher {
	return writer.ResponseWriter.Pusher()
}

func (writer *promptAuditResponseWriter) commit() error {
	if !writer.blocking {
		return nil
	}
	body, err := writer.capture.Bytes()
	if err != nil {
		return err
	}
	destination := writer.ResponseWriter.Header()
	clear(destination)
	for key, values := range writer.header {
		destination[key] = append([]string(nil), values...)
	}
	writer.ResponseWriter.WriteHeader(writer.status)
	_, err = writer.ResponseWriter.Write(body)
	return err
}

func extractPromptAuditOutput(body []byte) (string, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return "", errors.New("empty output")
	}
	collector := newPromptAuditTextCollector()
	if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.Contains(trimmed, []byte("\ndata:")) {
		scanner := bufio.NewScanner(bytes.NewReader(trimmed))
		scanner.Buffer(make([]byte, 4096), 8*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var value any
			if common.Unmarshal([]byte(data), &value) == nil {
				collector.Collect(value, true)
			}
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
	} else {
		var value any
		if err := common.Unmarshal(trimmed, &value); err != nil {
			return "", errors.New("unsupported text output structure")
		}
		collector.Collect(value, false)
	}
	text := collector.String()
	if text == "" {
		return "", errors.New("text output is unavailable")
	}
	return text, nil
}

type promptAuditTextCollector struct {
	order []string
	parts map[string]*strings.Builder
}

func newPromptAuditTextCollector() *promptAuditTextCollector {
	return &promptAuditTextCollector{parts: map[string]*strings.Builder{}}
}

func (collector *promptAuditTextCollector) add(key string, values []string) {
	if len(values) == 0 {
		return
	}
	if key == "" {
		key = "default"
	}
	builder, exists := collector.parts[key]
	if !exists {
		builder = &strings.Builder{}
		collector.parts[key] = builder
		collector.order = append(collector.order, key)
	}
	for _, value := range values {
		builder.WriteString(value)
	}
}

func (collector *promptAuditTextCollector) Collect(value any, streaming bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	eventType, _ := object["type"].(string)
	if strings.HasSuffix(eventType, ".done") || strings.HasSuffix(eventType, ".completed") || eventType == "response.completed" {
		return
	}
	if choices, ok := object["choices"].([]any); ok {
		for position, choice := range choices {
			item, _ := choice.(map[string]any)
			key := fmt.Sprint("choice:", position)
			if index, exists := item["index"]; exists {
				key = fmt.Sprint("choice:", index)
			}
			values := make([]string, 0)
			if delta, ok := item["delta"].(map[string]any); ok {
				collectPromptAuditNested(delta, &values)
			} else if message, ok := item["message"].(map[string]any); ok {
				collectPromptAuditNested(message, &values)
			}
			collector.add(key, values)
		}
		return
	}
	if candidates, ok := object["candidates"].([]any); ok {
		for position, candidate := range candidates {
			values := make([]string, 0)
			collectPromptAuditNested(candidate, &values)
			key := fmt.Sprint("candidate:", position)
			if item, ok := candidate.(map[string]any); ok {
				if index, exists := item["index"]; exists {
					key = fmt.Sprint("candidate:", index)
				}
			}
			collector.add(key, values)
		}
		return
	}
	key := "default"
	for _, name := range []string{"output_index", "content_index", "item_id", "call_id", "index"} {
		if part, exists := object[name]; exists {
			key += ":" + fmt.Sprint(part)
		}
	}
	values := make([]string, 0)
	if delta, ok := object["delta"].(string); ok && strings.Contains(eventType, "delta") {
		values = append(values, delta)
	} else if delta, ok := object["delta"].(map[string]any); ok {
		collectPromptAuditNested(delta, &values)
	} else if streaming && eventType != "" {
		collectPromptAuditNested(object, &values)
	} else {
		collectPromptAuditNested(object, &values)
	}
	collector.add(key, values)
}

func (collector *promptAuditTextCollector) String() string {
	values := make([]string, 0, len(collector.order))
	for _, key := range collector.order {
		if value := collector.parts[key].String(); value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, "\n\n")
}

func collectPromptAuditFields(object map[string]any, values *[]string) {
	for _, key := range []string{"content", "text", "output_text", "refusal", "reasoning_content", "thinking", "arguments", "partial_json"} {
		if text, ok := object[key].(string); ok && text != "" {
			*values = append(*values, text)
		}
	}
	for _, key := range []string{"args", "input"} {
		if nested, ok := object[key]; ok {
			if encoded, err := common.Marshal(nested); err == nil && len(encoded) > 0 && string(encoded) != "null" {
				*values = append(*values, string(encoded))
			}
		}
	}
}

func collectPromptAuditNested(value any, values *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		collectPromptAuditFields(typed, values)
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			switch key {
			case "args", "input":
				continue
			case "content", "text", "output_text", "refusal", "reasoning_content", "thinking", "arguments", "partial_json":
				if _, handled := typed[key].(string); handled {
					continue
				}
			}
			collectPromptAuditNested(typed[key], values)
		}
	case []any:
		for _, item := range typed {
			collectPromptAuditNested(item, values)
		}
	case string:
		// Only strings under explicitly selected field names are auditable output.
	}
}
