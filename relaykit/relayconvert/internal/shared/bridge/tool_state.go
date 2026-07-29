package bridge

import (
	"context"
	"encoding/json"
	"strings"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

type ToolKind string

const (
	ToolKindFunction   ToolKind = "function"
	ToolKindCustom     ToolKind = "custom"
	ToolKindToolSearch ToolKind = "tool_search"

	ClaudeToolResultErrorMarker = "[new-api:tool-result-error]"
)

type ToolIdentity struct {
	Kind         ToolKind
	Name         string
	Namespace    string
	UpstreamName string
}

type ToolState struct {
	byUpstream map[string]ToolIdentity
	byOriginal map[string]string
}

type contextKey struct{}

type contextState struct {
	toolState           *ToolState
	responseOutputState *ResponseOutputState
}

func WithContext(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, contextKey{}, &contextState{})
}

func contextStateFrom(ctx context.Context) *contextState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(contextKey{}).(*contextState)
	return state
}

func NewToolState() *ToolState {
	return &ToolState{
		byUpstream: make(map[string]ToolIdentity),
		byOriginal: make(map[string]string),
	}
}

func (s *ToolState) Register(identity ToolIdentity) bool {
	if s == nil {
		return false
	}
	identity.Name = strings.TrimSpace(identity.Name)
	identity.Namespace = strings.TrimSpace(identity.Namespace)
	identity.UpstreamName = strings.TrimSpace(identity.UpstreamName)
	if identity.Name == "" || identity.UpstreamName == "" {
		return false
	}
	if _, exists := s.byUpstream[identity.UpstreamName]; exists {
		return false
	}
	key := originalKey(identity.Kind, identity.Namespace, identity.Name)
	if _, exists := s.byOriginal[key]; exists {
		return false
	}
	s.byUpstream[identity.UpstreamName] = identity
	s.byOriginal[key] = identity.UpstreamName
	return true
}

func (s *ToolState) ResolveUpstream(name string) (ToolIdentity, bool) {
	if s == nil {
		return ToolIdentity{}, false
	}
	identity, ok := s.byUpstream[strings.TrimSpace(name)]
	return identity, ok
}

func (s *ToolState) UpstreamName(kind ToolKind, namespace, name string) (string, bool) {
	if s == nil {
		return "", false
	}
	upstream, ok := s.byOriginal[originalKey(kind, namespace, name)]
	return upstream, ok
}

func originalKey(kind ToolKind, namespace, name string) string {
	return string(kind) + "\x00" + strings.TrimSpace(namespace) + "\x00" + strings.TrimSpace(name)
}

func SetToolState(ctx context.Context, state *ToolState) {
	bridgeState := contextStateFrom(ctx)
	if bridgeState == nil || state == nil {
		return
	}
	bridgeState.toolState = state
}

func ToolStateFromContext(ctx context.Context) *ToolState {
	bridgeState := contextStateFrom(ctx)
	if bridgeState == nil {
		return nil
	}
	return bridgeState.toolState
}

func ResetContext(ctx context.Context) {
	bridgeState := contextStateFrom(ctx)
	if bridgeState == nil {
		return
	}
	bridgeState.toolState = nil
	bridgeState.responseOutputState = nil
}

func DecodeCustomInput(arguments string) string {
	var wrapper map[string]any
	if kitutil.Unmarshal([]byte(arguments), &wrapper) == nil {
		if input, ok := wrapper["input"].(string); ok {
			return input
		}
		if input, exists := wrapper["input"]; exists {
			raw, err := kitutil.Marshal(input)
			if err == nil {
				return string(raw)
			}
		}
	}
	return arguments
}

func ToolSearchArgumentsRaw(arguments string) json.RawMessage {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	var value any
	if kitutil.Unmarshal([]byte(trimmed), &value) == nil {
		return json.RawMessage(trimmed)
	}
	raw, _ := kitutil.Marshal(map[string]any{"input": arguments})
	return raw
}
