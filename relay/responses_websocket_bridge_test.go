package relay

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesWSSSEForwarderForwardsDataFrames(t *testing.T) {
	var sent []string
	forwarder := newResponsesWSSSEForwarder(func(payload []byte) error {
		sent = append(sent, string(payload))
		return nil
	}, nil)

	// A frame split across writes must be reassembled before forwarding.
	_, err := forwarder.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.outp"))
	require.NoError(t, err)
	assert.Empty(t, sent)
	_, err = forwarder.Write([]byte("ut_text.delta\",\"delta\":\"hi\"}\n\n"))
	require.NoError(t, err)
	require.Len(t, sent, 1)
	assert.JSONEq(t, `{"type":"response.output_text.delta","delta":"hi"}`, sent[0])
	assert.True(t, forwarder.Written())

	// Comments and chat-style [DONE] markers carry no websocket payload.
	_, err = forwarder.Write([]byte(": PING\n\ndata: [DONE]\n\n"))
	require.NoError(t, err)
	assert.Len(t, sent, 1)
}

func TestResponsesWSSSEForwarderHoldsTerminalEventsUntilFlush(t *testing.T) {
	var sent []string
	forwarder := newResponsesWSSSEForwarder(func(payload []byte) error {
		sent = append(sent, string(payload))
		return nil
	}, nil)

	_, err := forwarder.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"a\"}\n\n"))
	require.NoError(t, err)
	_, err = forwarder.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"}}\n\n"))
	require.NoError(t, err)

	require.Len(t, sent, 1)
	assert.JSONEq(t, `{"type":"response.output_text.delta","delta":"a"}`, sent[0])
	assert.True(t, forwarder.Written())

	forwarder.flushHeldEvents()
	require.Len(t, sent, 2)
	assert.JSONEq(t, `{"type":"response.completed","response":{"id":"resp_1"}}`, sent[1])
}

func TestResponsesWSSSEForwarderCancelsOnSendFailure(t *testing.T) {
	sendErr := errors.New("client gone")
	cancelled := false
	forwarder := newResponsesWSSSEForwarder(func([]byte) error {
		return sendErr
	}, func() { cancelled = true })

	_, err := forwarder.Write([]byte("data: {\"type\":\"response.created\"}\n\n"))
	require.ErrorIs(t, err, sendErr)
	assert.True(t, cancelled)

	_, err = forwarder.Write([]byte("data: {\"type\":\"response.in_progress\"}\n\n"))
	require.ErrorIs(t, err, sendErr)
}

func TestResponsesWSSSEForwarderStatusSemantics(t *testing.T) {
	forwarder := newResponsesWSSSEForwarder(func([]byte) error { return nil }, nil)

	// gin renders stream chunks with code -1, which must not mark the response
	// as written; otherwise the bridge would refuse to retry a clean failure.
	forwarder.WriteHeader(-1)
	assert.False(t, forwarder.Written())
	assert.Equal(t, http.StatusOK, forwarder.Status())

	forwarder.WriteHeader(http.StatusBadGateway)
	assert.True(t, forwarder.Written())
	assert.Equal(t, http.StatusBadGateway, forwarder.Status())
}

func TestResponsesWSSSEFrameDataJoinsMultipleDataLines(t *testing.T) {
	payload := responsesWSSSEFrameData([]byte("event: x\ndata: {\"a\":\ndata: 1}\nretry: 100"))
	assert.Equal(t, "{\"a\":\n1}", string(payload))

	assert.Nil(t, responsesWSSSEFrameData([]byte("event: keep-alive\n: comment")))
}
