package output

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// ChatWriter applies channel presentation settings to the final client payload,
// after protocol conversion and independently of the provider and billing usage.
type ChatWriter struct {
	gin.ResponseWriter
	ForceFormat       bool
	ThinkingToContent bool
	thinking          map[int]bool
}

func (w *ChatWriter) WriteMessage(message Message) error {
	data, err := w.format(message)
	if err != nil {
		return err
	}
	message.Data = data
	if message.Status != 0 {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	}
	if sink, ok := w.ResponseWriter.(Sink); ok {
		return sink.WriteMessage(message)
	}
	if message.Status != 0 {
		return (JSON{Writer: w.ResponseWriter}).WriteMessage(message)
	}
	if err := (SSE{Writer: w.ResponseWriter}).WriteMessage(message); err != nil {
		return err
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *ChatWriter) format(message Message) ([]byte, error) {
	data := message.Data
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) || len(data) == 0 || message.Event != "" {
		return data, nil
	}
	var envelope map[string]json.RawMessage
	if err := common.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if len(envelope["choices"]) == 0 {
		return data, nil
	}
	stream := message.Status == 0
	if w.ForceFormat {
		var response any = &dto.OpenAITextResponse{}
		if stream {
			response = &dto.ChatCompletionsStreamResponse{}
		}
		if err := common.Unmarshal(data, response); err != nil {
			return nil, err
		}
		var err error
		data, err = common.Marshal(response)
		if err != nil {
			return nil, err
		}
	}
	if !w.ThinkingToContent {
		return data, nil
	}
	envelope = nil
	if err := common.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	var choices []map[string]json.RawMessage
	if raw := envelope["choices"]; len(raw) > 0 {
		if err := common.Unmarshal(raw, &choices); err != nil {
			return nil, err
		}
	}
	changed := false
	for position, choice := range choices {
		field := "message"
		if stream {
			field = "delta"
		}
		if len(choice[field]) == 0 || bytes.Equal(choice[field], []byte("null")) {
			continue
		}
		var content map[string]json.RawMessage
		if err := common.Unmarshal(choice[field], &content); err != nil {
			return nil, err
		}
		var delta dto.ChatCompletionsStreamResponseChoiceDelta
		// A multimodal JSON response retains its native content array. Thinking
		// conversion applies only to text; opaque reasoning data stays intact.
		if raw := bytes.TrimSpace(content["content"]); len(raw) > 0 && raw[0] == '[' {
			continue
		}
		if err := common.Unmarshal(choice[field], &delta); err != nil {
			return nil, err
		}
		index := position
		if raw := choice["index"]; len(raw) > 0 {
			if err := common.Unmarshal(raw, &index); err != nil {
				return nil, err
			}
		}
		if w.thinking == nil {
			w.thinking = make(map[int]bool)
		}
		reasoning := delta.GetReasoningContent()
		text := delta.GetContentString()
		var rendered string
		if reasoning != "" {
			if !w.thinking[index] {
				rendered = "<think>\n"
				w.thinking[index] = true
			}
			rendered += reasoning
			delete(content, "reasoning_content")
			delete(content, "reasoning")
		}
		var finishReason string
		if raw := choice["finish_reason"]; len(raw) > 0 {
			if err := common.Unmarshal(raw, &finishReason); err != nil {
				return nil, err
			}
		}
		if w.thinking[index] && (!stream || text != "" || len(delta.ParseToolCalls()) > 0 || finishReason != "") {
			rendered += "\n</think>\n"
			w.thinking[index] = false
		}
		if reasoning == "" && rendered == "" {
			continue
		}
		rendered += text
		var err error
		content["content"], err = common.Marshal(rendered)
		if err != nil {
			return nil, err
		}
		choice[field], err = common.Marshal(content)
		if err != nil {
			return nil, err
		}
		changed = true
	}
	if !changed {
		return data, nil
	}
	var err error
	envelope["choices"], err = common.Marshal(choices)
	if err != nil {
		return nil, err
	}
	return common.Marshal(envelope)
}
