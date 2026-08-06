package openai

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/service"

	"github.com/tidwall/gjson"
)

const realtimeFallbackResponseID = "__unscoped_response__"

type realtimeResponsePacing struct {
	base *service.UserStreamPacer

	mu             sync.Mutex
	pendingCreates []time.Time
	responses      map[string]*service.UserStreamPacer
	cancelled      map[string]bool
}

func newRealtimeResponsePacing(base *service.UserStreamPacer) *realtimeResponsePacing {
	return &realtimeResponsePacing{
		base:      base,
		responses: make(map[string]*service.UserStreamPacer),
		cancelled: make(map[string]bool),
	}
}

func (p *realtimeResponsePacing) observeClientEvent(message []byte, receivedAt time.Time) {
	if p == nil || p.base == nil || !gjson.ValidBytes(message) {
		return
	}
	eventType := gjson.GetBytes(message, "type").String()
	p.mu.Lock()
	defer p.mu.Unlock()
	switch eventType {
	case "response.create":
		p.pendingCreates = append(p.pendingCreates, receivedAt)
	case "response.cancel":
		responseID := realtimeEventResponseID(message)
		if responseID != "" {
			p.cancelled[responseID] = true
			delete(p.responses, responseID)
			return
		}
		if len(p.pendingCreates) > 0 {
			p.pendingCreates = p.pendingCreates[:len(p.pendingCreates)-1]
		}
	}
}

func (p *realtimeResponsePacing) paceServerEvent(ctx context.Context, message []byte, receivedAt time.Time) error {
	if p == nil || p.base == nil {
		return nil
	}
	eventType := gjson.GetBytes(message, "type").String()
	responseID := realtimeEventResponseID(message)
	if eventType == "response.created" && responseID != "" {
		p.mu.Lock()
		startedAt := receivedAt
		if len(p.pendingCreates) > 0 {
			startedAt = p.pendingCreates[0]
			p.pendingCreates = p.pendingCreates[1:]
		}
		p.responses[responseID] = p.base.NewRealtimeResponsePacer(startedAt)
		delete(p.cancelled, responseID)
		p.mu.Unlock()
	}

	p.mu.Lock()
	if responseID != "" && p.cancelled[responseID] {
		p.mu.Unlock()
		if realtimeResponseTerminalEvent(eventType) {
			p.removeResponse(responseID)
		}
		return nil
	}
	pacer := p.responses[responseID]
	if responseID == "" && strings.HasPrefix(eventType, "response.") {
		responseID = realtimeFallbackResponseID
		pacer = p.responses[responseID]
	}
	if pacer == nil && responseID != "" {
		pacer = p.base.NewRealtimeResponsePacer(receivedAt)
		p.responses[responseID] = pacer
	}
	p.mu.Unlock()
	if pacer == nil {
		pacer = p.base
	}

	err := service.PaceUserStreamPayloadWithPacer(ctx, pacer, message)
	if realtimeResponseTerminalEvent(eventType) && responseID != "" {
		p.removeResponse(responseID)
	}
	return err
}

func (p *realtimeResponsePacing) removeResponse(responseID string) {
	p.mu.Lock()
	delete(p.responses, responseID)
	delete(p.cancelled, responseID)
	p.mu.Unlock()
}

func realtimeEventResponseID(message []byte) string {
	if !gjson.ValidBytes(message) {
		return ""
	}
	if responseID := gjson.GetBytes(message, "response_id"); responseID.Type == gjson.String {
		return responseID.String()
	}
	if responseID := gjson.GetBytes(message, "response.id"); responseID.Type == gjson.String {
		return responseID.String()
	}
	return ""
}

func realtimeResponseTerminalEvent(eventType string) bool {
	switch eventType {
	case "response.done", "response.completed", "response.failed", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}
