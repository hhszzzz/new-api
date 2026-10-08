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
	"github.com/QuantumNous/new-api/relay/output"
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
	if capture.maxBytes >= 0 && len(body) > capture.maxBytes-capture.size {
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
	if capture.maxBytes < 0 {
		return io.ReadAll(capture.file)
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
	if capture.maxBytes < 0 {
		return capture.file, nil
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

func newPromptAuditResponseWriter(writer gin.ResponseWriter, setting prompt_audit_setting.PromptAuditSetting, blocking bool) *promptAuditResponseWriter {
	return &promptAuditResponseWriter{
		ResponseWriter: writer,
		header:         writer.Header().Clone(),
		status:         http.StatusOK,
		blocking:       blocking,
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

// WriteMessage keeps the typed-event path transparent when the wrapped writer
// is a protocol sink (the Responses WebSocket bridge): without this method the
// relay's sink assertions would fail and fall back to rendering, which such a
// sink rejects as an incomplete protocol event. For plain writers the fallback
// follows the same contract as ChatWriter: a message carrying a status is a
// one-shot JSON body, everything else is a stream event. Captured events are
// stored newline-delimited so they stay parseable without SSE framing.
func (writer *promptAuditResponseWriter) WriteMessage(message output.Message) error {
	if sink, ok := writer.ResponseWriter.(output.Sink); ok {
		if writer.blocking {
			return writer.captureMessage(message)
		}
		if err := sink.WriteMessage(message); err != nil {
			return err
		}
		if !writer.capture.overflow {
			if captureErr := writer.captureMessage(message); captureErr != nil && !errors.Is(captureErr, errPromptOutputAuditLimit) {
				writer.capture.overflow = true
			}
		}
		return nil
	}
	if message.Status != 0 {
		return (output.JSON{Writer: writer}).WriteMessage(message)
	}
	if err := (output.SSE{Writer: writer}).WriteMessage(message); err != nil {
		return err
	}
	writer.Flush()
	return nil
}

func (writer *promptAuditResponseWriter) captureMessage(message output.Message) error {
	if len(message.Data) == 0 {
		return nil
	}
	line := make([]byte, 0, len(message.Data)+1)
	line = append(line, message.Data...)
	line = append(line, '\n')
	_, err := writer.capture.Write(line)
	return err
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
	if sink, ok := writer.ResponseWriter.(output.Sink); ok {
		// Typed events were captured newline-delimited; replay each line as
		// the protocol message it came from.
		for line := range bytes.SplitSeq(body, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			if err := sink.WriteMessage(output.Message{Data: line}); err != nil {
				return err
			}
		}
		return nil
	}
	_, err = writer.ResponseWriter.Write(body)
	return err
}

func extractPromptAuditOutput(body []byte) (string, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return "", errors.New("empty output")
	}
	collector := newPromptAuditTextCollector(len(trimmed), prompt_audit_setting.DefaultOutputMemoryBytes)
	defer collector.Close()
	if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.Contains(trimmed, []byte("\ndata:")) {
		scanner := bufio.NewScanner(bytes.NewReader(trimmed))
		scanner.Buffer(make([]byte, 4096), max(4096, len(trimmed)))
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
		if err := common.Unmarshal(trimmed, &value); err == nil {
			collector.Collect(value, false)
		} else {
			// Typed protocol events captured outside SSE framing are stored
			// one JSON document per line.
			matched := false
			for line := range bytes.SplitSeq(trimmed, []byte("\n")) {
				line = bytes.TrimSpace(line)
				if len(line) == 0 {
					continue
				}
				var event any
				if common.Unmarshal(line, &event) != nil {
					return "", errors.New("unsupported text output structure")
				}
				collector.Collect(event, true)
				matched = true
			}
			if !matched {
				return "", errors.New("unsupported text output structure")
			}
		}
	}
	text, err := collector.String()
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", errors.New("text output is unavailable")
	}
	return text, nil
}

type promptAuditTextCollector struct {
	order         []string
	parts         map[string]*promptAuditTextPart
	maxBytes      int
	memoryMax     int
	memorySize    int
	size          int
	capturedBytes int
	failed        error
	overflow      bool
}

type promptAuditTextPart struct {
	text strings.Builder
	file *os.File
}

func newPromptAuditTextCollector(maxBytes, memoryMax int) *promptAuditTextCollector {
	return &promptAuditTextCollector{parts: map[string]*promptAuditTextPart{}, maxBytes: maxBytes, memoryMax: memoryMax}
}

// CollectFrame bounds native WebSocket recording by the same wire-byte budget
// as HTTP capture. Once full, later events still reach the client normally.
func (collector *promptAuditTextCollector) CollectFrame(body []byte) {
	if collector.overflow {
		return
	}
	var value any
	if common.Unmarshal(body, &value) != nil {
		return
	}
	collector.Collect(value, true)
	if collector.maxBytes >= 0 {
		remaining := collector.maxBytes - collector.capturedBytes
		collector.capturedBytes += min(len(body), remaining)
		collector.overflow = collector.overflow || len(body) > remaining
	}
}

func (collector *promptAuditTextCollector) add(key string, values []string) {
	if len(values) == 0 || collector.overflow || collector.failed != nil {
		return
	}
	if key == "" {
		key = "default"
	}
	part, exists := collector.parts[key]
	if !exists {
		part = &promptAuditTextPart{}
		collector.parts[key] = part
		collector.order = append(collector.order, key)
	}
	for _, value := range values {
		remaining := collector.maxBytes - collector.size
		if collector.maxBytes >= 0 && len(value) > remaining {
			value = strings.ToValidUTF8(value[:remaining], "")
			collector.overflow = true
		}
		if part.file == nil && collector.memorySize+len(value) > collector.memoryMax {
			if err := collector.spill(); err != nil {
				collector.failed = err
				return
			}
		}
		if part.file != nil {
			written, err := io.WriteString(part.file, value)
			if err != nil {
				collector.failed = err
				return
			}
			if written != len(value) {
				collector.failed = io.ErrShortWrite
				return
			}
		} else {
			part.text.WriteString(value)
			collector.memorySize += len(value)
		}
		collector.size += len(value)
		if collector.overflow {
			return
		}
	}
}

func (collector *promptAuditTextCollector) spill() error {
	for _, key := range collector.order {
		part := collector.parts[key]
		if part.file != nil {
			continue
		}
		file, err := os.CreateTemp("", "new-api-prompt-output-text-*")
		if err != nil {
			return err
		}
		part.file = file
		text := part.text.String()
		written, err := io.WriteString(file, text)
		if err == nil && written != len(text) {
			err = io.ErrShortWrite
		}
		if err != nil {
			return err
		}
		part.text.Reset()
	}
	collector.memorySize = 0
	return nil
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

func (collector *promptAuditTextCollector) String() (string, error) {
	if collector.failed != nil {
		return "", collector.failed
	}
	var result strings.Builder
	result.Grow(collector.size + max(0, len(collector.order)-1)*2)
	writtenParts := 0
	for _, key := range collector.order {
		part := collector.parts[key]
		partSize := part.text.Len()
		if part.file != nil {
			info, err := part.file.Stat()
			if err != nil {
				return "", err
			}
			partSize = int(info.Size())
		}
		if partSize == 0 {
			continue
		}
		if writtenParts > 0 {
			result.WriteString("\n\n")
		}
		if part.file != nil {
			if _, err := part.file.Seek(0, io.SeekStart); err != nil {
				return "", err
			}
			if _, err := io.Copy(&result, part.file); err != nil {
				return "", err
			}
		} else {
			result.WriteString(part.text.String())
		}
		writtenParts++
	}
	return result.String(), nil
}

func (collector *promptAuditTextCollector) Close() error {
	var closeErrs []error
	for _, part := range collector.parts {
		if part.file == nil {
			continue
		}
		file := part.file
		part.file = nil
		name := file.Name()
		if err := file.Close(); err != nil {
			closeErrs = append(closeErrs, err)
		}
		if err := os.Remove(name); err != nil {
			closeErrs = append(closeErrs, err)
		}
	}
	return errors.Join(closeErrs...)
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
