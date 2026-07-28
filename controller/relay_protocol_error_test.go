package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteRelayErrorResponseUsesResponsesStreamEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	sequenceNumber := 4
	data := `{"type":"response.output_text.delta","delta":"partial","sequence_number":4}`
	require.NoError(t, helper.ResponseChunkData(c, dto.ResponsesStreamResponse{
		Type:           "response.output_text.delta",
		SequenceNumber: &sequenceNumber,
	}, data))

	apiError := types.WithOpenAIError(types.OpenAIError{
		Type:    "server_error",
		Code:    "server_error",
		Message: "upstream failed",
	}, http.StatusInternalServerError)
	writeRelayErrorResponse(c, types.RelayFormatOpenAIResponses, apiError, &relaycommon.RelayInfo{IsStream: true}, nil)

	body := recorder.Body.String()
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Contains(t, body, "event: error")
	assert.Contains(t, body, `{"type":"error","code":"server_error","message":"upstream failed","param":null,"sequence_number":5}`)
	assert.NotContains(t, body, "event: response.error")
	assert.NotContains(t, body, `"error":{"message"`)
}

func TestWriteRelayErrorResponseUsesMessagesStreamEnvelopeBeforeFirstChunk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	apiError := types.WithOpenAIError(types.OpenAIError{
		Type:    "server_error",
		Code:    "server_error",
		Message: "upstream failed",
	}, http.StatusInternalServerError)
	writeRelayErrorResponse(c, types.RelayFormatClaude, apiError, &relaycommon.RelayInfo{IsStream: true}, nil)

	body := recorder.Body.String()
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Contains(t, body, "event: error")
	assert.Contains(t, body, `{"type":"error","error":{"type":"api_error","message":"upstream failed"}}`)
}

func TestWriteRelayErrorResponseUsesEntryProtocolForJSONErrors(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		relayFormat types.RelayFormat
		wantBody    string
	}{
		{
			name:        "Responses",
			path:        "/v1/responses",
			relayFormat: types.RelayFormatOpenAIResponses,
			wantBody:    `{"error":{"message":"request cannot be converted","type":"new_api_error","param":"","code":"convert_request_failed"}}`,
		},
		{
			name:        "Messages",
			path:        "/v1/messages",
			relayFormat: types.RelayFormatClaude,
			wantBody:    `{"type":"error","error":{"type":"invalid_request_error","message":"request cannot be converted"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, test.path, nil)
			apiError := types.NewErrorWithStatusCode(
				errors.New("request cannot be converted"),
				types.ErrorCodeConvertRequestFailed,
				http.StatusBadRequest,
			)

			writeRelayErrorResponse(c, test.relayFormat, apiError, nil, nil)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.JSONEq(t, test.wantBody, recorder.Body.String())
		})
	}
}

func TestShouldRetryStopsAfterResponseIsWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	apiError := types.NewErrorWithStatusCode(
		errors.New("upstream unavailable"),
		types.ErrorCodeBadResponse,
		http.StatusInternalServerError,
	)
	require.True(t, shouldRetry(c, apiError, 1))

	c.String(http.StatusOK, "partial stream")

	assert.False(t, shouldRetry(c, apiError, 1))
}
