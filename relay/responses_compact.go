package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/gin-gonic/gin"
)

// compactSummaryWriter keeps the generated answer private until it has been
// validated and packaged as a reusable compact window. Normal protocol parsers
// still own response conversion, usage extraction and upstream errors.
type compactSummaryWriter struct {
	gin.ResponseWriter
	body    bytes.Buffer
	header  http.Header
	status  int
	written bool
}

func (w *compactSummaryWriter) Header() http.Header { return w.header }
func (w *compactSummaryWriter) Status() int         { return w.status }
func (w *compactSummaryWriter) Written() bool       { return w.written }
func (w *compactSummaryWriter) Size() int {
	if !w.written {
		return -1
	}
	return w.body.Len()
}
func (w *compactSummaryWriter) WriteHeader(status int) {
	if !w.written {
		w.status = status
	}
}
func (w *compactSummaryWriter) WriteHeaderNow() { w.written = true }
func (w *compactSummaryWriter) Flush()          { w.WriteHeaderNow() }
func (w *compactSummaryWriter) Write(body []byte) (int, error) {
	w.WriteHeaderNow()
	return w.body.Write(body)
}
func (w *compactSummaryWriter) WriteString(body string) (int, error) {
	w.WriteHeaderNow()
	return w.body.WriteString(body)
}

// enforceCompactionSummaryRequest also runs after channel parameter overrides:
// historical tool schemas must never become executable during summarization.
func enforceCompactionSummaryRequest(body []byte, protocol relayconvert.Protocol) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := common.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	for _, field := range []string{"tools", "tool_choice", "parallel_tool_calls", "functions", "function_call", "max_tool_calls", "web_search_options", "stream_options", "toolConfig", "tool_config"} {
		delete(request, field)
	}
	maxTokens, err := common.Marshal(relayconvert.CompactionSummaryMaxTokens)
	if err != nil {
		return nil, err
	}
	switch protocol {
	case relayconvert.ProtocolGemini:
		var config map[string]json.RawMessage
		if raw := request["generationConfig"]; len(raw) > 0 {
			if err := common.Unmarshal(raw, &config); err != nil {
				return nil, err
			}
		}
		if config == nil {
			config = make(map[string]json.RawMessage)
		}
		config["maxOutputTokens"] = maxTokens
		delete(config, "max_output_tokens")
		request["generationConfig"], err = common.Marshal(config)
		if err != nil {
			return nil, err
		}
	case relayconvert.ProtocolResponses:
		request["max_output_tokens"] = maxTokens
		request["stream"] = json.RawMessage("false")
		request["store"] = json.RawMessage("false")
	case relayconvert.ProtocolChat:
		if _, exists := request["max_completion_tokens"]; exists {
			request["max_completion_tokens"] = maxTokens
			delete(request, "max_tokens")
		} else {
			request["max_tokens"] = maxTokens
		}
		request["stream"] = json.RawMessage("false")
		request["store"] = json.RawMessage("false")
	case relayconvert.ProtocolMessages:
		request["max_tokens"] = maxTokens
		request["stream"] = json.RawMessage("false")
	}
	return common.Marshal(request)
}

func (w *compactSummaryWriter) compactResponse(usage *dto.Usage) ([]byte, error) {
	var response dto.OpenAIResponsesResponse
	if err := common.Unmarshal(w.body.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("invalid summary response: %w", err)
	}
	var status string
	if err := common.Unmarshal(response.Status, &status); err != nil || status != "completed" || response.IncompleteDetails != nil {
		return nil, fmt.Errorf("compaction summary did not complete; retain the original conversation and retry")
	}
	var summary strings.Builder
	for _, item := range response.Output {
		if item.Type == "reasoning" {
			continue
		}
		if item.Type != "message" || item.Role != "assistant" {
			return nil, fmt.Errorf("compaction summary returned an unexpected output item")
		}
		for _, part := range item.Content {
			if part.Type != "output_text" {
				return nil, fmt.Errorf("compaction summary did not return readable text")
			}
			summary.WriteString(part.Text)
		}
		summary.WriteByte('\n')
	}
	text := strings.TrimSpace(summary.String())
	if text == "" {
		return nil, fmt.Errorf("compaction summary was empty; retain the original conversation and retry")
	}
	output, err := common.Marshal([]dto.ResponsesOutput{{
		Type: "message", Role: "assistant", Status: "completed", ID: "msg_" + common.GetUUID(),
		Content: []dto.ResponsesOutputContent{{
			Type: "output_text", Text: "Summary of the earlier conversation for continuation:\n\n" + text,
			Annotations: []any{},
		}},
	}})
	if err != nil {
		return nil, err
	}
	compactUsage := *usage
	compactUsage.InputTokens = usage.PromptTokens
	compactUsage.OutputTokens = usage.CompletionTokens
	compactUsage.InputTokensDetails = &compactUsage.PromptTokensDetails
	compactUsage.OutputTokensDetails = &compactUsage.CompletionTokenDetails
	return common.Marshal(dto.OpenAIResponsesCompactionResponse{
		ID: "cmp_" + common.GetUUID(), Object: "response.compaction", CreatedAt: dto.IntValue(time.Now().Unix()),
		Output: output, Usage: &compactUsage,
	})
}
