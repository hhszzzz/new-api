package dify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDifyStreamHandlerRejectsUnsuccessfulWorkflowStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []string{"partial-succeeded", "failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			c, recorder, info := newDifyHandlerTestContext(true)
			body := difySSE(
				`{"event":"agent_message","workflow_run_id":"run-bad","answer":"placeholder"}`,
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

func TestDifyStreamHandlerDefaultModePreservesLegacyWorkflowCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, info := newDifyHandlerTestContext(false)
	body := difySSE(
		`{"event":"agent_message","answer":"legacy answer"}`,
		`{"event":"workflow_finished","workflow_run_id":"run-partial","data":{"status":"partial-succeeded","total_tokens":0}}`,
		`{"event":"message_end","metadata":{"usage":{"total_tokens":0}}}`,
	)

	usage, apiErr := difyStreamHandler(c, info, newDifyResponse(body))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, "partial-succeeded", info.DifyWorkflowStatus)
	assert.Contains(t, recorder.Body.String(), "legacy answer")
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
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
