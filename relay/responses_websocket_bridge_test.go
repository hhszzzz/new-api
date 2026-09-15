package relay

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relay/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesWebSocketOutputDeliversProtocolEventsAndHoldsTerminal(t *testing.T) {
	var sent []string
	writer := newResponsesWSEventWriter(func(payload []byte) error { sent = append(sent, string(payload)); return nil }, nil)
	assert.False(t, writer.Written())
	delta := `{"type":"response.output_text.delta","delta":"hi"}`
	terminal := `{"type":"response.completed","response":{"id":"resp_1"}}`
	require.NoError(t, writer.WriteMessage(output.Message{Event: "response.output_text.delta", Data: []byte(delta)}))
	require.NoError(t, writer.WriteMessage(output.Message{Event: "response.completed", Data: []byte(terminal)}))
	assert.Equal(t, []string{delta}, sent)
	assert.True(t, writer.Written())
	assert.False(t, writer.terminalDelivered)
	writer.flushHeldEvents()
	assert.Equal(t, []string{delta, terminal}, sent)
	assert.True(t, writer.terminalDelivered)
	require.Error(t, writer.WriteMessage(output.Message{Data: []byte(terminal)}))
	writer.flushHeldEvents()
	assert.Len(t, sent, 2)
}

func TestResponsesWebSocketOutputCancelsOnSendFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sendErr := errors.New("connection closed")
	writer := newResponsesWSEventWriter(func([]byte) error { return sendErr }, cancel)
	err := writer.WriteMessage(output.Message{Data: []byte(`{"type":"response.created"}`)})
	require.ErrorIs(t, err, sendErr)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.ErrorIs(t, writer.WriteMessage(output.Message{Data: []byte(`{"type":"response.in_progress"}`)}), sendErr)
}

func TestResponsesWebSocketOutputRejectsSSEAndDoesNotDeliverDoneMarker(t *testing.T) {
	var calls int
	writer := newResponsesWSEventWriter(func([]byte) error { calls++; return nil }, nil)
	_, err := writer.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	require.Error(t, err)
	require.NoError(t, writer.WriteMessage(output.Message{Data: []byte("[DONE]")}))
	assert.Zero(t, calls)
	assert.Equal(t, http.StatusOK, writer.Status())
	writer.WriteHeader(http.StatusBadGateway)
	assert.Equal(t, http.StatusBadGateway, writer.Status())
}
