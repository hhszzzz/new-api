package types

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaudeErrorTypeForStatus(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{status: http.StatusBadRequest, want: "invalid_request_error"},
		{status: http.StatusUnauthorized, want: "authentication_error"},
		{status: http.StatusForbidden, want: "permission_error"},
		{status: http.StatusNotFound, want: "not_found_error"},
		{status: http.StatusRequestEntityTooLarge, want: "request_too_large"},
		{status: http.StatusTooManyRequests, want: "rate_limit_error"},
		{status: 529, want: "overloaded_error"},
		{status: http.StatusInternalServerError, want: "api_error"},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, ClaudeErrorTypeForStatus(test.status))
	}
}

func TestToClaudeErrorConvertsOpenAIErrorAndPreservesClaudeType(t *testing.T) {
	openAIError := WithOpenAIError(OpenAIError{Message: "busy", Type: "server_error", Code: "server_error"}, http.StatusTooManyRequests)
	assert.Equal(t, ClaudeError{Type: "rate_limit_error", Message: "busy"}, openAIError.ToClaudeError())

	claudeError := WithClaudeError(ClaudeError{Type: "overloaded_error", Message: "try later"}, 529)
	assert.Equal(t, ClaudeError{Type: "overloaded_error", Message: "try later"}, claudeError.ToClaudeError())

	gatewayError := NewErrorWithStatusCode(errors.New("bridge failed"), ErrorCodeConvertRequestFailed, http.StatusBadRequest)
	assert.Equal(t, ClaudeError{Type: "invalid_request_error", Message: "bridge failed"}, gatewayError.ToClaudeError())
}

func TestIsProtocolUnsupportedMessageRequiresEndpointOrWireEvidence(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "endpoint phrase", message: "Unsupported endpoint /v1/responses", want: true},
		{name: "endpoint code", message: `{"code":"endpoint_not_found"}`, want: true},
		{name: "wire api suffix", message: "/v1/messages is not supported", want: true},
		{name: "wire api prefix", message: "This model does not support the Responses API", want: true},
		{name: "named unsupported protocol", message: "protocol not supported: chat completions", want: true},
		{name: "Gemini REST endpoint", message: "/v1beta/models/gemini-2.5-pro:generateContent is not supported", want: true},
		{name: "Gemini RPC endpoint", message: "models.generateContent was not found", want: true},
		{name: "method status", message: "method_not_allowed", want: true},
		{name: "schema capability", message: "Responses API does not support custom tools", want: false},
		{name: "tool declaration", message: `Responses tool call references undeclared tool "exec"`, want: false},
		{name: "input id", message: "Invalid input item id for Responses API", want: false},
		{name: "unrelated protocol", message: "unsupported protocol scheme for image URL", want: false},
		{name: "only supports parameter", message: "Responses API only supports function tools", want: false},
		{name: "Gemini schema capability", message: "Gemini API does not support this response schema", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, IsProtocolUnsupportedMessage(test.message))
		})
	}
}

func TestProtocolUnsupportedEvidenceTracksNegativeInspection(t *testing.T) {
	apiError := NewErrorWithStatusCode(errors.New("bad response status code 404"), ErrorCodeBadResponseStatusCode, http.StatusNotFound)
	assert.False(t, apiError.ProtocolUnsupportedChecked())
	assert.False(t, apiError.HasProtocolUnsupportedEvidence())

	apiError.MarkProtocolUnsupportedChecked()
	assert.True(t, apiError.ProtocolUnsupportedChecked())
	assert.False(t, apiError.HasProtocolUnsupportedEvidence())

	apiError.MarkProtocolUnsupported()
	assert.True(t, apiError.ProtocolUnsupportedChecked())
	assert.True(t, apiError.HasProtocolUnsupportedEvidence())
}
