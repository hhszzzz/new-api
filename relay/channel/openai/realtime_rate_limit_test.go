package openai

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeResponsePacingTracksExplicitAndAutomaticResponsesIndependently(t *testing.T) {
	coordinator := newRealtimeResponsePacing(service.NewUserStreamPacer(10, "gpt-4o", nil))
	startedAt := time.Unix(100, 0)
	coordinator.observeClientEvent([]byte(`{"type":"response.create"}`), startedAt)
	require.NoError(t, coordinator.paceServerEvent(t.Context(), []byte(`{"type":"response.created","response":{"id":"resp-explicit"}}`), startedAt.Add(100*time.Millisecond)))
	require.NoError(t, coordinator.paceServerEvent(t.Context(), []byte(`{"type":"response.created","response":{"id":"resp-auto"}}`), startedAt.Add(time.Second)))

	coordinator.mu.Lock()
	assert.Len(t, coordinator.responses, 2)
	assert.Empty(t, coordinator.pendingCreates)
	coordinator.mu.Unlock()

	require.NoError(t, coordinator.paceServerEvent(t.Context(), []byte(`{"type":"response.done","response":{"id":"resp-explicit"}}`), startedAt.Add(2*time.Second)))
	coordinator.mu.Lock()
	assert.NotContains(t, coordinator.responses, "resp-explicit")
	assert.Contains(t, coordinator.responses, "resp-auto")
	coordinator.mu.Unlock()
}

func TestRealtimeResponsePacingCancellationIsResponseScoped(t *testing.T) {
	coordinator := newRealtimeResponsePacing(service.NewUserStreamPacer(10, "gpt-4o", nil))
	now := time.Unix(200, 0)
	require.NoError(t, coordinator.paceServerEvent(t.Context(), []byte(`{"type":"response.created","response":{"id":"one"}}`), now))
	require.NoError(t, coordinator.paceServerEvent(t.Context(), []byte(`{"type":"response.created","response":{"id":"two"}}`), now))
	coordinator.observeClientEvent([]byte(`{"type":"response.cancel","response_id":"one"}`), now)

	coordinator.mu.Lock()
	assert.True(t, coordinator.cancelled["one"])
	assert.NotContains(t, coordinator.responses, "one")
	assert.Contains(t, coordinator.responses, "two")
	coordinator.mu.Unlock()
	require.NoError(t, coordinator.paceServerEvent(t.Context(), []byte(`{"type":"response.done","response":{"id":"one"}}`), now))
}

func TestRealtimeEventResponseIDSupportsDeltaAndResponseObjects(t *testing.T) {
	assert.Equal(t, "delta-id", realtimeEventResponseID([]byte(`{"response_id":"delta-id"}`)))
	assert.Equal(t, "object-id", realtimeEventResponseID([]byte(`{"response":{"id":"object-id"}}`)))
	assert.Empty(t, realtimeEventResponseID([]byte(`{"type":"session.updated"}`)))
}
