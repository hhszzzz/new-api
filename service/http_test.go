package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingResponseWriter struct {
	gin.ResponseWriter
}

type eventStreamProbeBody struct {
	reads int
	body  string
}

func (b *eventStreamProbeBody) Read(buffer []byte) (int, error) {
	b.reads++
	if b.reads > 1 {
		return 0, errors.New("event stream probe read past the first available frame")
	}
	return copy(buffer, b.body), nil
}

func (b *eventStreamProbeBody) Close() error {
	return nil
}

func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestIOCopyBytesGracefullyReturnsClientWriteError(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Writer = &failingResponseWriter{ResponseWriter: ctx.Writer}

	err := IOCopyBytesGracefully(ctx, nil, []byte(`{"ok":true}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "client disconnected")
}

func TestDetectProtocolUnsupportedSuccessEnvelopeMarksErrorAndPreservesBody(t *testing.T) {
	body := `{"error":{"type":"invalid_request_error","message":"Unsupported endpoint /v1/responses"}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	apiError := DetectProtocolUnsupportedSuccessEnvelope(resp)

	require.NotNil(t, apiError)
	assert.Equal(t, http.StatusBadGateway, apiError.StatusCode)
	assert.True(t, apiError.HasProtocolUnsupportedEvidence())
	replayed, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(replayed))
}

func TestDetectProtocolUnsupportedSuccessEnvelopeUsesStructuredErrorCode(t *testing.T) {
	body := `{"error":{"type":"invalid_request_error","code":"unsupported_endpoint","message":"Not Found"}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	apiError := DetectProtocolUnsupportedSuccessEnvelope(resp)

	require.NotNil(t, apiError)
	assert.True(t, apiError.HasProtocolUnsupportedEvidence())
	replayed, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(replayed))
}

func TestDetectProtocolUnsupportedSuccessEnvelopeIgnoresSuccessfulJSONAndSSE(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "successful JSON",
			contentType: "application/json",
			body:        `{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"unsupported endpoint is documentation text"}]}]}`,
		},
		{
			name:        "successful JSON with null error and protocol markers in model text",
			contentType: "application/json",
			body:        `{"id":"resp_2","error":null,"output":[{"type":"message","content":[{"type":"output_text","text":"In Express you handle a route not found with app.use, and method not allowed via 405"}]}]}`,
		},
		{
			name:        "SSE",
			contentType: "text/event-stream",
			body:        "data: {\"type\":\"response.completed\"}\n\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{test.contentType}},
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}

			assert.Nil(t, DetectProtocolUnsupportedSuccessEnvelope(resp))
			replayed, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, test.body, string(replayed))
		})
	}
}

func TestResponseIsEventStreamSniffsMislabeledSSEWithoutReadingToEOF(t *testing.T) {
	body := &eventStreamProbeBody{body: "data: {\"type\":\"response.created\"}\n\n"}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
	}

	assert.True(t, ResponseIsEventStream(resp))
	assert.Equal(t, 1, body.reads)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	prefix := make([]byte, len("data:"))
	_, err := io.ReadFull(resp.Body, prefix)
	require.NoError(t, err)
	assert.Equal(t, "data:", string(prefix))
}

func TestResponseIsEventStreamAcceptsBOMAndCommentPrefix(t *testing.T) {
	body := &eventStreamProbeBody{body: "\ufeff: PROCESSING\n\ndata: {\"type\":\"response.created\"}\n\n"}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	assert.True(t, ResponseIsEventStream(resp))
	assert.Equal(t, 1, body.reads)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
}

func TestResponseIsJSONUsesContentTypeOrBodySniffingAndPreservesBody(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        bool
	}{
		{name: "JSON content type", contentType: "application/problem+json", body: `not inspected`, want: true},
		{name: "sniffed object", contentType: "application/octet-stream", body: "\ufeff  {\"ok\":true}", want: true},
		{name: "sniffed array", body: "\n[1,2]", want: true},
		{name: "SSE", body: "data: {\"ok\":true}\n\n", want: false},
		{name: "binary", body: "\x00\x01", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{"Content-Type": []string{test.contentType}},
				Body:   io.NopCloser(strings.NewReader(test.body)),
			}

			assert.Equal(t, test.want, ResponseIsJSON(resp))
			replayed, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, test.body, string(replayed))
		})
	}
}
