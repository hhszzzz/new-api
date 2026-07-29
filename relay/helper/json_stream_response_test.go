package helper

import (
	"io"
	"net/http"
	"strings"
	"testing"

	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromoteJSONResponseToSSEPreservesProtocolTerminalSemantics(t *testing.T) {
	tests := []struct {
		name     string
		format   relaytypes.RelayFormat
		body     string
		contains []string
		count    map[string]int
	}{
		{
			name:   "Chat Completions",
			format: relaytypes.RelayFormatOpenAI,
			body: `{
				"id":"chatcmpl_1","object":"chat.completion","created":1710000000,"model":"provider-chat",
				"choices":[{"index":0,"message":{"role":"assistant","content":"hello","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}],
				"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}
			}`,
			contains: []string{`"object":"chat.completion.chunk"`, `"content":"hello"`, `"finish_reason":"tool_calls"`, `data: [DONE]`},
		},
		{
			name:   "Anthropic Messages",
			format: relaytypes.RelayFormatClaude,
			body: `{
				"id":"msg_1","type":"message","role":"assistant","model":"provider-claude",
				"content":[{"type":"thinking","thinking":"inspect","signature":"sig"},{"type":"text","text":"hello"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}}],
				"stop_reason":"tool_use","usage":{"input_tokens":2,"output_tokens":3}
			}`,
			contains: []string{`"type":"message_start"`, `"type":"thinking_delta"`, `"type":"signature_delta"`, `"type":"text_delta"`, `"type":"input_json_delta"`, `"stop_reason":"tool_use"`, `"type":"message_stop"`},
		},
		{
			name:   "OpenAI Responses",
			format: relaytypes.RelayFormatOpenAIResponses,
			body: `{
				"id":"resp_1","object":"response","created_at":1710000000,"status":"completed","model":"provider-responses",
				"output":[
					{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"inspect"}]},
					{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]},
					{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}
				],
				"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}
			}`,
			contains: []string{`"type":"response.created"`, `"type":"response.reasoning_summary_text.delta"`, `"type":"response.output_text.delta"`, `"type":"response.function_call_arguments.delta"`, `"type":"response.output_item.done"`, `"type":"response.completed"`},
			count:    map[string]int{`"type":"response.output_item.done"`: 3},
		},
		{
			name:   "Gemini array",
			format: relaytypes.RelayFormatGemini,
			body: `[
				{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]},
				{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}
			]`,
			contains: []string{`"text":"hello"`, `"finishReason":"STOP"`},
			count:    map[string]int{"data: ": 2},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}, "Content-Length": []string{"123"}},
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}

			err := PromoteJSONResponseToSSE(resp, test.format)

			require.NoError(t, err)
			assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
			assert.Empty(t, resp.Header.Get("Content-Length"))
			assert.Equal(t, int64(-1), resp.ContentLength)
			body, readErr := io.ReadAll(resp.Body)
			require.NoError(t, readErr)
			stream := string(body)
			for _, value := range test.contains {
				assert.Contains(t, stream, value)
			}
			for value, count := range test.count {
				assert.Equal(t, count, strings.Count(stream, value))
			}
		})
	}
}

func TestPromoteJSONResponseToSSERejectsSemanticErrorsAndIncompleteChat(t *testing.T) {
	tests := []struct {
		name   string
		format relaytypes.RelayFormat
		body   string
		want   string
	}{
		{
			name:   "HTTP 200 error envelope",
			format: relaytypes.RelayFormatOpenAI,
			body:   `{"error":{"type":"invalid_request_error","message":"unsupported endpoint"}}`,
			want:   "unsupported endpoint",
		},
		{
			name:   "missing Chat finish reason",
			format: relaytypes.RelayFormatOpenAI,
			body:   `{"id":"chatcmpl_1","choices":[{"index":0,"message":{"role":"assistant","content":"partial"}}]}`,
			want:   "finish_reason",
		},
		{
			name:   "missing Claude stop reason",
			format: relaytypes.RelayFormatClaude,
			body:   `{"id":"msg_1","type":"message","content":[{"type":"text","text":"partial"}]}`,
			want:   "stop_reason",
		},
		{
			name:   "missing Responses status",
			format: relaytypes.RelayFormatOpenAIResponses,
			body:   `{"id":"resp_1","object":"response","output":[]}`,
			want:   "terminal status",
		},
		{
			name:   "non-terminal Responses status",
			format: relaytypes.RelayFormatOpenAIResponses,
			body:   `{"id":"resp_1","object":"response","status":"in_progress","output":[]}`,
			want:   "not terminal",
		},
		{
			name:   "unknown Responses status",
			format: relaytypes.RelayFormatOpenAIResponses,
			body:   `{"id":"resp_1","object":"response","status":"mystery","output":[]}`,
			want:   "unknown status",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(test.body))}

			err := PromoteJSONResponseToSSE(resp, test.format)

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}
