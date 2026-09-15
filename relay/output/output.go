// Package output renders protocol messages to a client transport. It never
// interprets EOF, invents terminal events, converts protocols, or executes tools.
package output

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

type Message struct {
	Event  string
	Data   []byte
	Status int
}

type Sink interface{ WriteMessage(Message) error }

type JSON struct{ Writer http.ResponseWriter }

func (s JSON) WriteMessage(message Message) error {
	status := message.Status
	if status == 0 {
		status = http.StatusOK
	}
	s.Writer.WriteHeader(status)
	n, err := s.Writer.Write(message.Data)
	if err == nil && n != len(message.Data) {
		err = io.ErrShortWrite
	}
	return err
}

type SSE struct{ Writer http.ResponseWriter }

func (s SSE) WriteMessage(message Message) error {
	var frame bytes.Buffer
	if message.Event != "" {
		if bytes.ContainsAny([]byte(message.Event), "\r\n") {
			return fmt.Errorf("invalid protocol event name")
		}
		frame.WriteString("event: ")
		frame.WriteString(message.Event)
		frame.WriteByte('\n')
	}
	for line := range bytes.SplitSeq(message.Data, []byte("\n")) {
		frame.WriteString("data: ")
		frame.Write(line)
		frame.WriteByte('\n')
	}
	frame.WriteByte('\n')
	n, err := s.Writer.Write(frame.Bytes())
	if err == nil && n != frame.Len() {
		return io.ErrShortWrite
	}
	return err
}

type WebSocket struct{ Send func([]byte) error }

func (s WebSocket) WriteMessage(message Message) error { return s.Send(message.Data) }
