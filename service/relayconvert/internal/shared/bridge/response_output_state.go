package bridge

import "github.com/gin-gonic/gin"

type ResponseOutputKind string

const (
	ResponseOutputKindMessage   ResponseOutputKind = "message"
	ResponseOutputKindReasoning ResponseOutputKind = "reasoning"
	ResponseOutputKindTool      ResponseOutputKind = "tool"
)

type ResponseOutputItem struct {
	Kind      ResponseOutputKind
	Text      string
	ToolIndex int
}

type ResponseOutputState struct {
	Items []ResponseOutputItem
}

const responseOutputContextKey = "relayconvert_protocol_bridge_response_output_state"

func SetResponseOutputState(c *gin.Context, state *ResponseOutputState) {
	if c == nil || state == nil {
		return
	}
	c.Set(responseOutputContextKey, state)
}

func ResponseOutputStateFromContext(c *gin.Context) *ResponseOutputState {
	if c == nil {
		return nil
	}
	value, exists := c.Get(responseOutputContextKey)
	if !exists {
		return nil
	}
	state, _ := value.(*ResponseOutputState)
	return state
}
