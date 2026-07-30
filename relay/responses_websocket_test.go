package relay

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResponsesWSCreateEventSupportsWrapperAndFlatForms(t *testing.T) {
	tests := []struct {
		name    string
		message string
		eventID string
	}{
		{
			name: "wrapper",
			message: `{
				"type":"response.create",
				"event_id":"evt_wrapper",
				"generate":false,
				"response":{"model":"gpt-test","input":"hi","store":false,"stream":true,"stream_options":{"include_usage":true}}
			}`,
			eventID: "evt_wrapper",
		},
		{
			name: "flat",
			message: `{
				"type":"response.create",
				"event_id":"evt_flat",
				"model":"gpt-test",
				"input":"hi",
				"store":false,
				"generate":false,
				"stream":true,
				"background":true
			}`,
			eventID: "evt_flat",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			create, eventID, err := normalizeResponsesWSCreateEvent([]byte(test.message))
			require.NoError(t, err)
			assert.Equal(t, test.eventID, eventID)
			assert.Equal(t, "gpt-test", create.Request.Model)
			assert.JSONEq(t, "false", string(create.Generate))
			assert.JSONEq(t, "false", string(create.Request.Store))
			assert.Nil(t, create.Request.Stream)
			assert.Nil(t, create.Request.StreamOptions)
		})
	}
}

func TestBuildResponsesWSCreateEventIsFlatAndPreservesExplicitFalse(t *testing.T) {
	payload := []byte(`{
		"model":"gpt-test",
		"input":"hi",
		"store":false,
		"event_id":"evt_upstream",
		"stream":true,
		"background":true,
		"stream_options":{"include_usage":true}
	}`)

	got, err := buildResponsesWSCreateEvent(payload, json.RawMessage(`false`))
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, common.Unmarshal(got, &data))
	assert.Equal(t, responsesWSEventTypeResponseCreate, data["type"])
	assert.Equal(t, "gpt-test", data["model"])
	assert.Equal(t, "hi", data["input"])
	assert.Equal(t, false, data["store"])
	assert.Equal(t, false, data["generate"])
	for _, key := range []string{"response", "event_id", "stream", "background", "stream_options"} {
		assert.NotContains(t, data, key)
	}
}

func TestBuildResponsesWSErrorPayloadIncludesStatusAndEventID(t *testing.T) {
	payload, err := buildResponsesWSErrorPayload("evt_err", types.NewErrorWithStatusCode(
		errors.New("model is required"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	))
	require.NoError(t, err)
	var event responsesWSErrorEvent
	require.NoError(t, common.Unmarshal(payload, &event))
	assert.Equal(t, "error", event.Type)
	assert.Equal(t, http.StatusBadRequest, event.Status)
	assert.Equal(t, "evt_err", event.EventID)
	require.NotNil(t, event.Error)
	assert.Equal(t, string(types.ErrorCodeInvalidRequest), event.Error.Code)
}

func TestResponsesWebSocketURLAndBetaHeader(t *testing.T) {
	assert.Equal(t, "wss://api.openai.com/v1/responses", toWebSocketURL("https://api.openai.com/v1/responses"))
	assert.Equal(t, "ws://127.0.0.1:3000/v1/responses", toWebSocketURL("http://127.0.0.1:3000/v1/responses"))
	assert.Equal(t, "wss://already.example/v1/responses", toWebSocketURL("wss://already.example/v1/responses"))
	assert.Equal(t, responsesWSBetaHeader, mergeResponsesWSBetaHeader(""))
	assert.Equal(t, "existing=1, "+responsesWSBetaHeader, mergeResponsesWSBetaHeader("existing=1"))
	assert.Equal(t, responsesWSBetaHeader, mergeResponsesWSBetaHeader(responsesWSBetaHeader))
}

func TestObserveUpstreamFailureReleasesCurrentAndCommitsFailure(t *testing.T) {
	var committed *bool
	session := &responsesWSSession{}
	state := &responsesWSCallState{
		info:  &relaycommon.RelayInfo{},
		usage: &dto.Usage{},
		commitRate: func(success bool) {
			committed = common.GetPointer(success)
		},
	}
	session.current = state

	message, apiErr := session.observeUpstreamMessage([]byte(`{"type":"response.failed"}`))

	require.Nil(t, apiErr)
	assert.JSONEq(t, `{"type":"response.failed"}`, string(message))
	assert.Nil(t, session.getCurrent())
	require.NotNil(t, committed)
	assert.False(t, *committed)
}

func TestResponsesWebSocketPolicyCloseReachesClientAndUpstream(t *testing.T) {
	clientPeer, gatewayClient, cleanupClient := newResponsesWebSocketTestPair(t)
	defer cleanupClient()
	gatewayTarget, upstreamPeer, cleanupTarget := newResponsesWebSocketTestPair(t)
	defer cleanupTarget()

	session := &responsesWSSession{
		client: gatewayClient,
		target: gatewayTarget,
	}
	session.closeForPolicy("channel disabled")

	assertWebSocketClose(t, clientPeer, websocket.ClosePolicyViolation, "channel disabled")
	assertWebSocketClose(t, upstreamPeer, websocket.ClosePolicyViolation, "channel disabled")
}

func assertWebSocketClose(t *testing.T, conn *websocket.Conn, code int, reason string) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := conn.ReadMessage()
	var closeErr *websocket.CloseError
	require.ErrorAs(t, err, &closeErr)
	assert.Equal(t, code, closeErr.Code)
	assert.Equal(t, reason, closeErr.Text)
}

func newResponsesWebSocketTestPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()
	type upgradeResult struct {
		conn *websocket.Conn
		err  error
	}
	upgraded := make(chan upgradeResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		upgraded <- upgradeResult{conn: conn, err: err}
	}))
	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	var result upgradeResult
	select {
	case result = <-upgraded:
	case <-time.After(time.Second):
		peer.Close()
		server.Close()
		t.Fatal("timed out waiting for websocket upgrade")
	}
	require.NoError(t, result.err)
	require.NotNil(t, result.conn)
	cleanup := func() {
		_ = peer.Close()
		_ = result.conn.Close()
		server.Close()
	}
	return peer, result.conn, cleanup
}
