package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/output"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
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
	}, true)
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

	// Typed protocol events captured outside SSE framing arrive one JSON
	// document per line (the WebSocket bridge sink path).
	typedEvents := []byte("{\"type\":\"response.output_text.delta\",\"delta\":\"bridged\"}\n{\"type\":\"response.completed\",\"response\":{}}\n")
	text, err = extractPromptAuditOutput(typedEvents)
	require.NoError(t, err)
	assert.Equal(t, "bridged", text)

	_, err = extractPromptAuditOutput([]byte("{\"type\":\"response.output_text.delta\",\"delta\":\"kept\"}\nnot json\n"))
	require.Error(t, err)
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
	}, false)
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
	}, true)
	t.Cleanup(func() { require.NoError(t, writer.capture.Close()) })

	written, err := writer.Write([]byte("generated output"))
	assert.ErrorIs(t, err, errPromptOutputAuditLimit)
	assert.Zero(t, written)
	assert.Empty(t, recorder.Body.Bytes())
	assert.True(t, writer.capture.overflow)
}

func TestPromptAuditWriterKeepsTypedSinkTransparent(t *testing.T) {
	var sent []string
	base := newResponsesWSEventWriter(func(payload []byte) error { sent = append(sent, string(payload)); return nil }, nil)
	writer := newPromptAuditResponseWriter(base, prompt_audit_setting.PromptAuditSetting{
		OutputMode: prompt_audit_setting.ModeOff, OutputMaxBytes: 1024, OutputMemoryBytes: 1024,
	}, false)
	t.Cleanup(func() { require.NoError(t, writer.capture.Close()) })

	sink, ok := any(writer).(output.Sink)
	require.True(t, ok, "the audit writer must stay a protocol sink for typed bridges")
	delta := `{"type":"response.output_text.delta","delta":"hi"}`
	require.NoError(t, sink.WriteMessage(output.Message{Event: "response.output_text.delta", Data: []byte(delta)}))
	assert.Equal(t, []string{delta}, sent)

	body, err := writer.capture.Bytes()
	require.NoError(t, err)
	text, err := extractPromptAuditOutput(body)
	require.NoError(t, err)
	assert.Equal(t, "hi", text)
}

func TestPromptAuditWriterRendersTypedEventsForPlainWriters(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writer := newPromptAuditResponseWriter(c.Writer, prompt_audit_setting.PromptAuditSetting{
		OutputMode: prompt_audit_setting.ModeOff, OutputMaxBytes: 1024, OutputMemoryBytes: 1024,
	}, false)
	t.Cleanup(func() { require.NoError(t, writer.capture.Close()) })

	require.NoError(t, writer.WriteMessage(output.Message{Event: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","delta":"hi"}`)}))
	assert.Contains(t, recorder.Body.String(), "event: response.output_text.delta\n")
	body, err := writer.capture.Bytes()
	require.NoError(t, err)
	text, err := extractPromptAuditOutput(body)
	require.NoError(t, err)
	assert.Equal(t, "hi", text)
}

func TestPromptAuditBlockingWriterDeliversTypedEventsOnCommit(t *testing.T) {
	var sent []string
	base := newResponsesWSEventWriter(func(payload []byte) error { sent = append(sent, string(payload)); return nil }, nil)
	writer := newPromptAuditResponseWriter(base, prompt_audit_setting.PromptAuditSetting{
		OutputMode: prompt_audit_setting.ModeBlocking, OutputMaxBytes: 1024, OutputMemoryBytes: 1024,
	}, true)
	t.Cleanup(func() { require.NoError(t, writer.capture.Close()) })

	delta := `{"type":"response.output_text.delta","delta":"hi"}`
	terminal := `{"type":"response.completed","response":{"id":"resp_1"}}`
	require.NoError(t, writer.WriteMessage(output.Message{Data: []byte(delta)}))
	require.NoError(t, writer.WriteMessage(output.Message{Data: []byte(terminal)}))
	assert.Empty(t, sent)
	require.NoError(t, writer.commit())
	assert.Equal(t, []string{delta}, sent)
	base.flushHeldEvents()
	assert.Equal(t, []string{delta, terminal}, sent)
}

func TestPromptAuditTextRelayRecordsWithoutEnforcement(t *testing.T) {
	withProtocolBridgeStreamTestMode(t)
	service.InitHttpClient()
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.PromptAudit{}, &model.User{}, &model.Token{}))
	original := prompt_audit_setting.GetSetting()
	originalLogConsume, originalBatchUpdate := common.LogConsumeEnabled, common.BatchUpdateEnabled
	t.Cleanup(func() {
		original.PublishConfig()
		common.LogConsumeEnabled, common.BatchUpdateEnabled = originalLogConsume, originalBatchUpdate
	})
	common.LogConsumeEnabled, common.BatchUpdateEnabled = false, false
	for _, test := range []struct {
		name          string
		outputMode    string
		allGroups     bool
		includeAdmins bool
		role          int
		stream        bool
	}{
		{name: "audit off", outputMode: prompt_audit_setting.ModeOff, allGroups: true, includeAdmins: true},
		{name: "group outside blocking scope", outputMode: prompt_audit_setting.ModeBlocking, includeAdmins: true},
		{name: "stream outside blocking scope", outputMode: prompt_audit_setting.ModeBlocking, includeAdmins: true, stream: true},
		{name: "administrator outside blocking scope", outputMode: prompt_audit_setting.ModeBlocking, allGroups: true, role: common.RoleAdminUser},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, db.Where("1 = 1").Delete(&model.PromptAudit{}).Error)
			configured := original
			configured.OutputMode, configured.Mode = test.outputMode, prompt_audit_setting.ModeOff
			configured.RecordAll, configured.IncludeAdmins = true, test.includeAdmins
			configured.AllGroups, configured.Groups = test.allGroups, []string{"audited"}
			configured.OutputMaxBytes, configured.OutputMemoryBytes = 4096, 4096
			configured.PublishConfig()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_stored\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"recorded answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"chatcmpl_stored","choices":[{"message":{"role":"assistant","content":"recorded answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
			}))
			defer upstream.Close()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("role", test.role)
			common.SetContextKey(c, constant.ContextKeyUserName, "recording-user")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI,
				OriginModelName: "recording-model", IsStream: test.stream, DisablePing: true,
				Request: &dto.GeneralOpenAIRequest{Model: "recording-model", Stream: common.GetPointer(test.stream), Messages: []dto.Message{{Role: "user", Content: "ordinary question"}}},
			}
			require.Nil(t, executeText(c, info))
			assert.Contains(t, recorder.Body.String(), "recorded answer", "recording must preserve normal delivery")
			var rows []model.PromptAudit
			require.NoError(t, db.Find(&rows).Error)
			require.Len(t, rows, 1)
			assert.Equal(t, model.PromptAuditStatusStored, rows[0].Status)
			assert.Empty(t, rows[0].Decision)
			assert.Equal(t, "delivered", rows[0].DeliveryStatus)
			assert.Contains(t, string(rows[0].ScanPayload), "recorded answer")
		})
	}
}

func TestPromptAuditRecordsInterruptedOutputWithAuditingOff(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.PromptAudit{}))
	original := prompt_audit_setting.GetSetting()
	t.Cleanup(func() { original.PublishConfig() })
	configured := original
	configured.OutputMode, configured.RecordAll = prompt_audit_setting.ModeOff, true
	configured.PublishConfig()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	writer := newPromptAuditResponseWriter(c.Writer, configured, false)
	t.Cleanup(func() { require.NoError(t, writer.capture.Close()) })
	_, err := fmt.Fprint(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial answer\"}\n\n")
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{Request: &dto.OpenAIResponsesRequest{Model: "recording-model", Input: []byte(`"question"`)}, OriginModelName: "recording-model", RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true}
	auditIncompleteTextOutput(c, info, writer, "delivered_incomplete")
	var rows []model.PromptAudit
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, model.PromptAuditStatusStored, rows[0].Status)
	assert.Empty(t, rows[0].Decision)
	assert.False(t, rows[0].CoverageComplete)
	assert.Equal(t, "delivered_incomplete", rows[0].DeliveryStatus)
	assert.Contains(t, string(rows[0].ScanPayload), "partial answer")
}

func TestPromptAuditWebSocketRecordingTruncatesAtAValidTextBoundary(t *testing.T) {
	collector := newPromptAuditTextCollector(4)
	collector.CollectFrame([]byte(`{"type":"response.output_text.delta","delta":"甲乙"}`))
	assert.Equal(t, "甲", collector.String())
	assert.True(t, collector.overflow)
	collector.CollectFrame([]byte(`{"type":"response.output_text.delta","delta":"later text"}`))
	assert.Equal(t, "甲", collector.String())
}
