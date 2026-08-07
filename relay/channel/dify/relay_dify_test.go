package dify

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockingDifyStream struct {
	first  []byte
	offset int
	closed chan struct{}
	once   sync.Once
}

func newBlockingDifyStream(first string) *blockingDifyStream {
	return &blockingDifyStream{
		first:  []byte(first),
		closed: make(chan struct{}),
	}
}

func (b *blockingDifyStream) Read(p []byte) (int, error) {
	if b.offset < len(b.first) {
		n := copy(p, b.first[b.offset:])
		b.offset += n
		return n, nil
	}
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingDifyStream) Close() error {
	b.once.Do(func() {
		close(b.closed)
	})
	return nil
}

type cancelAfterDifyChunkWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
	once   sync.Once
}

func (w *cancelAfterDifyChunkWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if bytes.Contains(data, []byte("chat.completion.chunk")) {
		w.once.Do(w.cancel)
	}
	return n, err
}

type recordedDifyStopRequest struct {
	method        string
	path          string
	authorization string
	body          string
}

func newDifyHandlerTestContext(strict bool) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	if constant.StreamingTimeout <= 0 {
		constant.StreamingTimeout = 60
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		IsStream:  true,
		StartTime: time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "dify-test",
			ChannelOtherSettings: relaydto.ChannelOtherSettings{
				DifyRequireSuccessfulWorkflow: strict,
			},
		},
	}
	return c, recorder, info
}

func newDifyResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func difySSE(events ...string) string {
	var body strings.Builder
	for _, event := range events {
		body.WriteString("data: ")
		body.WriteString(event)
		body.WriteString("\n\n")
	}
	return body.String()
}

func TestRequestOpenAI2DifyStoresUserForTaskCancellation(t *testing.T) {
	c, _, info := newDifyHandlerTestContext(false)
	request := relaydto.GeneralOpenAIRequest{User: []byte(`"client-user"`)}

	difyRequest := requestOpenAI2Dify(c, info, request)

	require.NotNil(t, difyRequest)
	assert.Equal(t, "client-user", difyRequest.User)
	assert.Equal(t, difyRequest.User, info.DifyUser)
}

func TestStopDifyTaskUsesServiceAPIContract(t *testing.T) {
	requests := make(chan recordedDifyStopRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- recordedDifyStopRequest{
			method:        r.Method,
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			body:          string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	info := &relaycommon.RelayInfo{
		DifyTaskID: "task-123",
		DifyUser:   "client-user",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "dify-key",
		},
	}

	require.NoError(t, stopDifyTask(info))
	request := <-requests
	assert.Equal(t, http.MethodPost, request.method)
	assert.Equal(t, "/v1/chat-messages/task-123/stop", request.path)
	assert.Equal(t, "Bearer dify-key", request.authorization)
	assert.JSONEq(t, `{"user":"client-user"}`, request.body)
}

func TestDifyStreamHandlerStopsUpstreamTaskAfterClientDisconnect(t *testing.T) {
	previousDrainTimeout := difyStreamDrainTimeout
	difyStreamDrainTimeout = 25 * time.Millisecond
	t.Cleanup(func() { difyStreamDrainTimeout = previousDrainTimeout })

	requests := make(chan recordedDifyStopRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- recordedDifyStopRequest{
			method:        r.Method,
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			body:          string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, recorder, info := newDifyHandlerTestContext(false)
	c.Request = c.Request.WithContext(requestContext)
	c.Writer = &cancelAfterDifyChunkWriter{ResponseWriter: c.Writer, cancel: cancel}
	info.ChannelBaseUrl = server.URL
	info.ApiKey = "dify-key"
	info.DifyUser = "client-user"
	body := newBlockingDifyStream(difySSE(
		`{"event":"workflow_started","task_id":"task-123","workflow_run_id":"run-123","data":{"id":"run-123","workflow_id":"workflow-1"}}`,
	))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}

	usage, apiErr := difyStreamHandler(c, info, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeClientDisconnected, apiErr.GetErrorCode())
	assert.Equal(t, 499, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr), "client disconnect must not trigger channel retry/auto-ban")
	assert.False(t, types.IsRecordErrorLog(apiErr), "client disconnect must not be recorded as a user-visible error")
	assert.Equal(t, "task-123", info.DifyTaskID)
	assert.NotContains(t, recorder.Body.String(), "data: [DONE]")
	request := <-requests
	assert.Equal(t, http.MethodPost, request.method)
	assert.Equal(t, "/v1/chat-messages/task-123/stop", request.path)
	assert.Equal(t, "Bearer dify-key", request.authorization)
	assert.JSONEq(t, `{"user":"client-user"}`, request.body)
	select {
	case <-body.closed:
	default:
		t.Fatal("upstream Dify response body was not closed after client disconnect")
	}
}

func TestDifyStreamHandlerStrictSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, info := newDifyHandlerTestContext(true)
	body := difySSE(
		`{"event":"workflow_started","workflow_run_id":"run-123","data":{"id":"run-123","workflow_id":"workflow-1"}}`,
		`{"event":"agent_message","workflow_run_id":"run-123","answer":"real answer"}`,
		`{"event":"workflow_finished","workflow_run_id":"run-123","data":{"id":"run-123","workflow_id":"workflow-1","status":"succeeded","total_tokens":9}}`,
		`{"event":"message_end","workflow_run_id":"run-123","metadata":{"usage":{"prompt_tokens":4,"completion_tokens":5,"total_tokens":9}}}`,
	)

	usage, apiErr := difyStreamHandler(c, info, newDifyResponse(body))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 9, usage.TotalTokens)
	assert.Equal(t, "run-123", info.DifyWorkflowRunID)
	assert.Equal(t, "succeeded", info.DifyWorkflowStatus)
	assert.Contains(t, recorder.Body.String(), "real answer")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestDifyStreamHandlerChatSuccessWithoutWorkflowEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, info := newDifyHandlerTestContext(false)
	body := difySSE(
		`{"event":"message","task_id":"task-123","answer":"chat answer"}`,
		`{"event":"message_end","task_id":"task-123","metadata":{"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}}`,
	)

	usage, apiErr := difyStreamHandler(c, info, newDifyResponse(body))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 5, usage.TotalTokens)
	assert.Contains(t, recorder.Body.String(), "chat answer")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
	snapshot := info.StreamStatus.Snapshot()
	assert.Equal(t, relaycommon.StreamTerminalSuccess, snapshot.TerminalState)
	assert.True(t, snapshot.TerminalDelivered)
	assert.True(t, snapshot.UsageComplete)
	assert.True(t, snapshot.SemanticOutput)
}

func TestDifyStreamHandlerStrictSuccessWhenMessageEndPrecedesWorkflowFinished(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, info := newDifyHandlerTestContext(true)
	body := difySSE(
		`{"event":"workflow_started","workflow_run_id":"run-123","data":{"id":"run-123","workflow_id":"workflow-1"}}`,
		`{"event":"agent_message","workflow_run_id":"run-123","answer":"real answer"}`,
		`{"event":"message_end","workflow_run_id":"run-123","metadata":{"usage":{"prompt_tokens":4,"completion_tokens":5,"total_tokens":9}}}`,
		`{"event":"workflow_finished","workflow_run_id":"run-123","data":{"id":"run-123","workflow_id":"workflow-1","status":"succeeded","total_tokens":9}}`,
	)

	usage, apiErr := difyStreamHandler(c, info, newDifyResponse(body))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 9, usage.TotalTokens)
	assert.Equal(t, "run-123", info.DifyWorkflowRunID)
	assert.Equal(t, "succeeded", info.DifyWorkflowStatus)
	assert.Contains(t, recorder.Body.String(), "real answer")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestDifyStreamHandlerRejectsUnsuccessfulWorkflowStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []string{"partial-succeeded", "failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			c, recorder, info := newDifyHandlerTestContext(true)
			body := difySSE(
				`{"event":"agent_message","workflow_run_id":"run-bad","answer":"placeholder"}`,
				`{"event":"message_end","workflow_run_id":"run-bad","metadata":{"usage":{"prompt_tokens":4,"completion_tokens":4,"total_tokens":8}}}`,
				`{"event":"workflow_finished","workflow_run_id":"run-bad","data":{"status":"`+status+`","total_tokens":8}}`,
			)

			usage, apiErr := difyStreamHandler(c, info, newDifyResponse(body))

			assert.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
			assert.Equal(t, status, info.DifyWorkflowStatus)
			assert.NotContains(t, recorder.Body.String(), "data: [DONE]")
		})
	}
}

func TestDifyStreamHandlerStrictTerminalValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		body     string
		wantCode types.ErrorCode
	}{
		{
			name: "missing workflow terminal",
			body: difySSE(
				`{"event":"agent_message","answer":"answer"}`,
				`{"event":"message_end","metadata":{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}}`,
			),
			wantCode: types.ErrorCodeBadResponse,
		},
		{
			name: "empty answer",
			body: difySSE(
				`{"event":"workflow_finished","data":{"status":"succeeded","total_tokens":2}}`,
				`{"event":"message_end","metadata":{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}}`,
			),
			wantCode: types.ErrorCodeEmptyResponse,
		},
		{
			name: "zero message usage",
			body: difySSE(
				`{"event":"agent_message","answer":"answer"}`,
				`{"event":"workflow_finished","data":{"status":"succeeded","total_tokens":2}}`,
				`{"event":"message_end","metadata":{"usage":{"total_tokens":0}}}`,
			),
			wantCode: types.ErrorCodeBadResponse,
		},
		{
			name: "zero workflow usage",
			body: difySSE(
				`{"event":"agent_message","answer":"answer"}`,
				`{"event":"workflow_finished","data":{"status":"succeeded","total_tokens":0}}`,
				`{"event":"message_end","metadata":{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}}`,
			),
			wantCode: types.ErrorCodeBadResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, info := newDifyHandlerTestContext(true)
			usage, apiErr := difyStreamHandler(c, info, newDifyResponse(tt.body))

			assert.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.Equal(t, tt.wantCode, apiErr.GetErrorCode())
			assert.NotContains(t, recorder.Body.String(), "data: [DONE]")
		})
	}
}

func TestDifyStreamHandlerRejectsUniversalStreamFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
	}{
		{
			name: "explicit error event",
			body: difySSE(`{"event":"error","workflow_run_id":"run-error","code":"upstream_error","message":"agent failed","status":500}`),
		},
		{
			name: "malformed event",
			body: "data: {not-json}\n\n",
		},
		{
			name: "unexpected eof",
			body: difySSE(`{"event":"agent_message","answer":"partial"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, info := newDifyHandlerTestContext(false)
			usage, apiErr := difyStreamHandler(c, info, newDifyResponse(tt.body))

			assert.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
			assert.NotContains(t, recorder.Body.String(), "data: [DONE]")
		})
	}
}

func TestDifyStreamHandlerDefaultModeRejectsUnsuccessfulWorkflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, info := newDifyHandlerTestContext(false)
	body := difySSE(
		`{"event":"agent_message","answer":"legacy answer"}`,
		`{"event":"workflow_finished","workflow_run_id":"run-partial","data":{"status":"partial-succeeded","total_tokens":0}}`,
		`{"event":"message_end","metadata":{"usage":{"total_tokens":0}}}`,
	)

	usage, apiErr := difyStreamHandler(c, info, newDifyResponse(body))

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
	assert.Equal(t, "partial-succeeded", info.DifyWorkflowStatus)
	assert.Contains(t, recorder.Body.String(), "legacy answer")
	assert.NotContains(t, recorder.Body.String(), "data: [DONE]")
}

func TestDifyNonStreamHandlerContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		strict      bool
		body        string
		wantErrCode types.ErrorCode
		wantAnswer  string
	}{
		{
			name:       "strict success",
			strict:     true,
			body:       `{"conversation_id":"conversation-1","answer":"real answer","metadata":{"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}}`,
			wantAnswer: "real answer",
		},
		{
			name:        "strict empty answer",
			strict:      true,
			body:        `{"conversation_id":"conversation-1","answer":"  ","metadata":{"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}}`,
			wantErrCode: types.ErrorCodeEmptyResponse,
		},
		{
			name:        "strict zero usage",
			strict:      true,
			body:        `{"conversation_id":"conversation-1","answer":"answer","metadata":{"usage":{"total_tokens":0}}}`,
			wantErrCode: types.ErrorCodeBadResponse,
		},
		{
			name:        "HTTP 200 error envelope",
			body:        `{"event":"error","code":"workflow_error","message":"workflow failed","status":500}`,
			wantErrCode: types.ErrorCodeBadResponse,
		},
		{
			name:        "HTTP 200 status-only error envelope",
			body:        `{"status":"failed"}`,
			wantErrCode: types.ErrorCodeBadResponse,
		},
		{
			name:       "default mode empty response compatibility",
			body:       `{"conversation_id":"conversation-1","answer":"","metadata":{"usage":{"total_tokens":0}}}`,
			wantAnswer: `"content":""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, info := newDifyHandlerTestContext(tt.strict)
			resp := newDifyResponse(tt.body)
			resp.Header.Set("Content-Type", "application/json")
			usage, apiErr := difyHandler(c, info, resp)

			if tt.wantErrCode != "" {
				assert.Nil(t, usage)
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				assert.Equal(t, tt.wantErrCode, apiErr.GetErrorCode())
				assert.Empty(t, recorder.Body.String())
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Contains(t, recorder.Body.String(), tt.wantAnswer)
		})
	}
}
