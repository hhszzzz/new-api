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
