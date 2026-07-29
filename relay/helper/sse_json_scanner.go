package helper

import (
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ScanJSONSSE reads JSON-bearing SSE frames without assuming that every
// provider inserts a blank line between events. Standard multi-line data
// fields are joined with newlines, while the common one-data-line-per-event
// variant is emitted as soon as the previous payload is complete JSON.
func ScanJSONSSE(reader io.Reader, handler func(data string) (bool, error)) error {
	if reader == nil || handler == nil {
		return nil
	}

	scanner := NewStreamScanner(reader)
	dataLines := make([]string, 0, 1)
	flush := func() (bool, error) {
		if len(dataLines) == 0 {
			return false, nil
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if data == "" {
			return false, nil
		}
		return handler(data)
	}
	payloadComplete := func() bool {
		if len(dataLines) == 0 {
			return false
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		if data == "[DONE]" {
			return true
		}
		var value any
		return common.Unmarshal([]byte(data), &value) == nil
	}

	firstLine := true
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if firstLine {
			line = strings.TrimPrefix(line, "\uFEFF")
			firstLine = false
		}
		if line == "" {
			stop, err := flush()
			if err != nil || stop {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		if payloadComplete() {
			stop, err := flush()
			if err != nil || stop {
				return err
			}
		}
		dataLine := strings.TrimPrefix(line, "data:")
		dataLine = strings.TrimPrefix(dataLine, " ")
		dataLines = append(dataLines, dataLine)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	_, err := flush()
	return err
}
