package bridge

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type ToolKind string

const (
	ToolKindFunction   ToolKind = "function"
	ToolKindCustom     ToolKind = "custom"
	ToolKindToolSearch ToolKind = "tool_search"
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

const contextKey = "relayconvert_protocol_bridge_tool_state"

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

func SetToolState(c *gin.Context, state *ToolState) {
	if c == nil || state == nil {
		return
	}
	c.Set(contextKey, state)
}

func ToolStateFromContext(c *gin.Context) *ToolState {
	if c == nil {
		return nil
	}
	value, exists := c.Get(contextKey)
	if !exists {
		return nil
	}
	state, _ := value.(*ToolState)
	return state
}

func ResetContext(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(contextKey, nil)
	c.Set(responseOutputContextKey, nil)
}

func DecodeCustomInput(arguments string) string {
	var wrapper map[string]any
	if common.Unmarshal([]byte(arguments), &wrapper) == nil {
		if input, ok := wrapper["input"].(string); ok {
			return input
		}
		if input, exists := wrapper["input"]; exists {
			raw, err := common.Marshal(input)
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
	if common.Unmarshal([]byte(trimmed), &value) == nil {
		return json.RawMessage(trimmed)
	}
	raw, _ := common.Marshal(map[string]any{"input": arguments})
	return raw
}
