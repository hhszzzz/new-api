package openai

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type realtimeReserveRecorder struct {
	preConsumed  int
	reserves     []int
	settles      []int
	reserveErr   error
	panicReserve bool
}

func (r *realtimeReserveRecorder) Settle(actualQuota int) error {
	r.settles = append(r.settles, actualQuota)
	return nil
}

func (r *realtimeReserveRecorder) Refund(*gin.Context) {}

func (r *realtimeReserveRecorder) NeedsRefund() bool { return false }

func (r *realtimeReserveRecorder) GetPreConsumedQuota() int { return r.preConsumed }

func (r *realtimeReserveRecorder) Reserve(targetQuota int) error {
	r.reserves = append(r.reserves, targetQuota)
	if r.panicReserve {
		panic("reserve panic")
	}
	if r.reserveErr != nil {
		err := r.reserveErr
		r.reserveErr = nil
		return err
	}
	if targetQuota > r.preConsumed {
		r.preConsumed = targetQuota
	}
	return nil
}

func TestPreConsumeUsageReservesCumulativeQuotaWithoutSettling(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recorder := &realtimeReserveRecorder{}
	info := &relaycommon.RelayInfo{
		Billing: recorder,
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			AudioRatio:      1,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
	}
	total := &dto.RealtimeUsage{}

	require.NoError(t, preConsumeUsage(ctx, info, &dto.RealtimeUsage{
		TotalTokens: 10,
		InputTokens: 10,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 10,
		},
	}, total))
	require.NoError(t, preConsumeUsage(ctx, info, &dto.RealtimeUsage{
		TotalTokens:  5,
		OutputTokens: 5,
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens: 5,
		},
	}, total))

	assert.Equal(t, []int{10, 20}, recorder.reserves)
	assert.Empty(t, recorder.settles)
	assert.Equal(t, 15, total.TotalTokens)
	assert.Equal(t, 10, total.InputTokens)
	assert.Equal(t, 5, total.OutputTokens)
}

func TestPreConsumeUsageCommitsTotalOnlyAfterReserveSucceeds(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recorder := &realtimeReserveRecorder{reserveErr: errors.New("reserve failed")}
	info := &relaycommon.RelayInfo{
		Billing: recorder,
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			AudioRatio:      1,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
	}
	total := &dto.RealtimeUsage{}
	batch := &dto.RealtimeUsage{
		TotalTokens: 10,
		InputTokens: 10,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 10,
		},
	}

	require.EqualError(t, preConsumeUsage(ctx, info, batch, total), "reserve failed")
	assert.Equal(t, dto.RealtimeUsage{}, *total)

	require.NoError(t, preConsumeUsage(ctx, info, batch, total))
	assert.Equal(t, []int{10, 10}, recorder.reserves)
	assert.Equal(t, 10, total.TotalTokens)
	assert.Equal(t, 10, total.InputTokens)
	assert.Equal(t, 10, total.InputTokenDetails.TextTokens)
}

func TestOpenaiRealtimeHandlerReleasesUsageLockAfterBillingPanic(t *testing.T) {
	clientConn, _ := newRealtimeWebsocketPair(t)
	targetConn, targetPeer := newRealtimeWebsocketPair(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	recorder := &realtimeReserveRecorder{panicReserve: true}
	info := &relaycommon.RelayInfo{
		ClientWs: clientConn,
		TargetWs: targetConn,
		Billing:  recorder,
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			AudioRatio:      1,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
	}
	type handlerResult struct {
		err   *relaytypes.NewAPIError
		usage *dto.RealtimeUsage
	}
	resultChan := make(chan handlerResult, 1)
	go func() {
		handlerErr, usage := OpenaiRealtimeHandler(ctx, info)
		resultChan <- handlerResult{err: handlerErr, usage: usage}
	}()

	require.NoError(t, targetPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"usage":{"total_tokens":10,"input_tokens":10,"input_token_details":{"text_tokens":10}}}}`)))

	select {
	case result := <-resultChan:
		require.NotNil(t, result.err)
		assert.Equal(t, relaytypes.ErrorCodeBadResponse, result.err.GetErrorCode())
		assert.Contains(t, result.err.Error(), "panic in target reader: reserve panic")
		assert.Nil(t, result.usage)
		assert.Empty(t, recorder.settles)
	case <-time.After(3 * time.Second):
		t.Fatal("realtime handler did not return after billing panic")
	}
}

func TestApplyRealtimeSessionPromptPreservesSessionUpdates(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UserModelRouteId:     7,
		RouteTargetModelName: "upstream-model",
		RouteInjectPrompt:    "Route identity.",
	}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{
		SystemPrompt:         "Channel policy.",
		SystemPromptOverride: true,
	}}
	promptApplied := false

	first := []byte(`{"type":"session.update","event_id":"evt-1","session":{"voice":"alloy","vendor_option":{"keep":true}},"top_level_unknown":17}`)
	got, err := applyRealtimeSessionPrompt(first, info, &promptApplied)
	require.NoError(t, err)
	assert.True(t, promptApplied)
	assert.Equal(t, "Route identity.\nChannel policy.", gjson.GetBytes(got, "session.instructions").String())
	assert.Equal(t, "alloy", gjson.GetBytes(got, "session.voice").String())
	assert.True(t, gjson.GetBytes(got, "session.vendor_option.keep").Bool())
	assert.Equal(t, int64(17), gjson.GetBytes(got, "top_level_unknown").Int())

	voiceOnly := []byte(`{"type":"session.update","session":{"voice":"verse"}}`)
	got, err = applyRealtimeSessionPrompt(voiceOnly, info, &promptApplied)
	require.NoError(t, err)
	assert.Equal(t, voiceOnly, got, "an update without instructions must not reset the active session prompt")

	replacement := []byte(`{"type":"session.update","session":{"instructions":"Client replacement."}}`)
	got, err = applyRealtimeSessionPrompt(replacement, info, &promptApplied)
	require.NoError(t, err)
	assert.Equal(t, "Route identity.\nChannel policy.\nClient replacement.", gjson.GetBytes(got, "session.instructions").String())

	cleared := []byte(`{"type":"session.update","session":{"instructions":null}}`)
	got, err = applyRealtimeSessionPrompt(cleared, info, &promptApplied)
	require.NoError(t, err)
	assert.Equal(t, "Route identity.\nChannel policy.", gjson.GetBytes(got, "session.instructions").String())
}

func TestApplyRealtimeSessionPromptHonorsChannelOverride(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UserModelRouteId:     7,
		RouteTargetModelName: "upstream-model",
		RouteInjectPrompt:    "Route identity.",
	}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{
		SystemPrompt:         "Channel policy.",
		SystemPromptOverride: false,
	}}
	promptApplied := false

	message := []byte(`{"type":"session.update","session":{"instructions":"Client policy."}}`)
	got, err := applyRealtimeSessionPrompt(message, info, &promptApplied)
	require.NoError(t, err)
	assert.Equal(t, "Route identity.\nClient policy.", gjson.GetBytes(got, "session.instructions").String())

	voiceOnly := []byte(`{"type":"session.update","session":{"voice":"verse"}}`)
	got, err = applyRealtimeSessionPrompt(voiceOnly, info, &promptApplied)
	require.NoError(t, err)
	assert.Equal(t, voiceOnly, got)
}

func TestApplyRealtimeSessionPromptPreservesNonStringInstructions(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UserModelRouteId:     7,
		RouteTargetModelName: "upstream-model",
		RouteInjectPrompt:    "Route identity.",
	}

	for _, value := range []string{`{"invalid":true}`, `["invalid"]`, `42`, `true`, `false`} {
		t.Run(value, func(t *testing.T) {
			message := []byte(`{"type":"session.update","session":{"instructions":` + value + `,"voice":"alloy"}}`)
			promptApplied := false

			got, err := applyRealtimeSessionPrompt(message, info, &promptApplied)
			require.NoError(t, err)
			assert.Equal(t, message, got)
			assert.False(t, promptApplied, "an invalid value must not consume a later valid update")
		})
	}
}

func TestApplyRealtimeSessionPromptLeavesUnrelatedMessagesUntouched(t *testing.T) {
	configured := &relaycommon.RelayInfo{
		UserModelRouteId:     7,
		RouteTargetModelName: "upstream-model",
		RouteInjectPrompt:    "Route identity.",
	}
	tests := []struct {
		name    string
		info    *relaycommon.RelayInfo
		message []byte
	}{
		{
			name:    "different event",
			info:    configured,
			message: []byte(`{"type":"response.create","response":{"instructions":"Client policy."}}`),
		},
		{
			name:    "invalid session shape",
			info:    configured,
			message: []byte(`{"type":"session.update","session":[]}`),
		},
		{
			name:    "no configured prompt",
			info:    &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}},
			message: []byte(`{"type":"session.update","session":{"instructions":"Client policy."}}`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			promptApplied := false
			got, err := applyRealtimeSessionPrompt(test.message, test.info, &promptApplied)
			require.NoError(t, err)
			assert.Equal(t, test.message, got)
			assert.False(t, promptApplied)
		})
	}
}

func TestOpenaiRealtimeHandlerForwardsNonStringInstructions(t *testing.T) {
	clientConn, clientPeer := newRealtimeWebsocketPair(t)
	targetConn, targetPeer := newRealtimeWebsocketPair(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		ClientWs:             clientConn,
		TargetWs:             targetConn,
		UserModelRouteId:     7,
		RouteTargetModelName: "upstream-model",
		RouteInjectPrompt:    "Route identity.",
		ChannelMeta:          &relaycommon.ChannelMeta{UpstreamModelName: "upstream-model"},
	}
	type handlerResult struct {
		err   *relaytypes.NewAPIError
		usage *dto.RealtimeUsage
	}
	resultChan := make(chan handlerResult, 1)
	go func() {
		handlerErr, usage := OpenaiRealtimeHandler(ctx, info)
		resultChan <- handlerResult{err: handlerErr, usage: usage}
	}()

	message := []byte(`{"type":"session.update","session":{"instructions":{"invalid":true},"voice":"alloy"},"unknown":"preserved"}`)
	require.NoError(t, clientPeer.WriteMessage(websocket.TextMessage, message))
	require.NoError(t, targetPeer.SetReadDeadline(time.Now().Add(3*time.Second)))
	messageType, got, err := targetPeer.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, messageType)
	assert.JSONEq(t, string(message), string(got))

	require.NoError(t, clientPeer.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second),
	))
	select {
	case result := <-resultChan:
		assert.Nil(t, result.err)
		require.NotNil(t, result.usage)
		assert.Zero(t, result.usage.TotalTokens)
	case <-time.After(3 * time.Second):
		t.Fatal("realtime handler did not stop after the client closed")
	}
}

func newRealtimeWebsocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConnChan := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnChan <- conn
	}))
	t.Cleanup(server.Close)

	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	serverConn := <-serverConnChan
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = peer.Close()
	})
	return serverConn, peer
}
