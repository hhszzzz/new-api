package relay

import (
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptAuditBlockingWriterWithholdsAndSpillsBeforeCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writer := newPromptAuditResponseWriter(c.Writer, prompt_audit_setting.PromptAuditSetting{
		OutputMode: prompt_audit_setting.ModeBlocking, OutputMaxBytes: 1024, OutputMemoryBytes: 4,
	})
	t.Cleanup(func() { require.NoError(t, writer.capture.Close()) })

	body := []byte(`{"choices":[{"message":{"content":"visible"}}]}`)
	_, err := writer.Write(body)
	require.NoError(t, err)
	assert.Empty(t, recorder.Body.Bytes())
	require.NotNil(t, writer.capture.file)
	temporaryPath := writer.capture.file.Name()
	require.NoError(t, writer.commit())
	assert.Equal(t, body, recorder.Body.Bytes())
	require.NoError(t, writer.capture.Close())
	_, statErr := os.Stat(temporaryPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
	writer.capture.file = nil
}

func TestPromptAuditWebSocketFramesPreserveKindsAndOrder(t *testing.T) {
	capture := &promptOutputCapture{maxBytes: 1024, memoryBytes: 4}
	t.Cleanup(func() { require.NoError(t, capture.Close()) })
	require.NoError(t, capture.WriteFrame(1, []byte(`{"type":"response.output_text.delta","delta":"one"}`)))
	require.NoError(t, capture.WriteFrame(2, []byte("two")))

	reader, err := capture.Reader()
	require.NoError(t, err)
	var kinds []int
	var bodies []string
	require.NoError(t, readPromptOutputFrames(reader, func(kind int, body []byte) error {
		kinds = append(kinds, kind)
		bodies = append(bodies, string(body))
		return nil
	}))
	assert.Equal(t, []int{1, 2}, kinds)
	assert.Equal(t, []string{`{"type":"response.output_text.delta","delta":"one"}`, "two"}, bodies)
}

func TestExtractPromptAuditOutputFromJSONAndSSE(t *testing.T) {
	jsonBody := []byte(`{"choices":[{"message":{"content":"hello","tool_calls":[{"function":{"arguments":"{\"target\":\"example\"}"}}]}}]}`)
	text, err := extractPromptAuditOutput(jsonBody)
	require.NoError(t, err)
	assert.Equal(t, `hello{"target":"example"}`, text)

	sseBody := []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"first \"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"second\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n")
	text, err = extractPromptAuditOutput(sseBody)
	require.NoError(t, err)
	assert.Equal(t, "first second", text)

	multiChoice := []byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"A\"}},{\"index\":1,\"delta\":{\"content\":\"X\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"B\"}}]}\n\n")
	text, err = extractPromptAuditOutput(multiChoice)
	require.NoError(t, err)
	assert.Equal(t, "AB\n\nX", text)
}

func TestExtractPromptAuditOutputAcrossTextProtocols(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "responses json visible reasoning refusal and tool arguments",
			body: `{"id":"resp_1","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"visible reasoning"}]},{"type":"message","content":[{"type":"output_text","text":"answer"},{"type":"refusal","refusal":"cannot comply"}]},{"type":"function_call","call_id":"call_1","arguments":"{\"target\":\"example\"}"}]}`,
			want: `visible reasoninganswercannot comply{"target":"example"}`,
		},
		{
			name: "claude sse text thinking and tool input",
			body: "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"reply\"}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"tool_use\",\"input\":{\"query\":\"term\"}}}\n\n",
			want: `plan

reply

{"query":"term"}`,
		},
		{
			name: "gemini json choices and function args",
			body: `{"candidates":[{"index":0,"content":{"parts":[{"text":"gemini answer"},{"functionCall":{"name":"lookup","args":{"query":"term"}}}]}}]}`,
			want: `gemini answer{"query":"term"}`,
		},
		{
			name: "responses sse deltas ignore terminal snapshot",
			body: "data: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"delta\":\"reason\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":1,\"delta\":\"answer\"}\n\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":2,\"call_id\":\"call_1\",\"delta\":\"{\\\"x\\\":1}\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output_text\":\"duplicate\"}}\n\n",
			want: "reason\n\nanswer\n\n{\"x\":1}",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			text, err := extractPromptAuditOutput([]byte(test.body))
			require.NoError(t, err)
			assert.Equal(t, test.want, text)
		})
	}
}

func TestPromptAuditObserveOverflowDoesNotInterruptDelivery(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writer := newPromptAuditResponseWriter(c.Writer, prompt_audit_setting.PromptAuditSetting{
		OutputMode: prompt_audit_setting.ModeAsyncAudit, OutputMaxBytes: 4, OutputMemoryBytes: 4,
	})
	_, err := writer.Write([]byte("delivered"))
	require.NoError(t, err)
	assert.Equal(t, "delivered", recorder.Body.String())
	assert.True(t, writer.capture.overflow)
}

func TestPromptAuditBlockingOverflowStopsGenerationAndWithholdsOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writer := newPromptAuditResponseWriter(c.Writer, prompt_audit_setting.PromptAuditSetting{
		OutputMode: prompt_audit_setting.ModeBlocking, OutputMaxBytes: 4, OutputMemoryBytes: 4,
	})
	t.Cleanup(func() { require.NoError(t, writer.capture.Close()) })

	written, err := writer.Write([]byte("generated output"))
	assert.ErrorIs(t, err, errPromptOutputAuditLimit)
	assert.Zero(t, written)
	assert.Empty(t, recorder.Body.Bytes())
	assert.True(t, writer.capture.overflow)
}
