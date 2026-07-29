package bridge

import "context"

type ResponseOutputKind string

const (
	ResponseOutputKindMessage   ResponseOutputKind = "message"
	ResponseOutputKindReasoning ResponseOutputKind = "reasoning"
	ResponseOutputKindTool      ResponseOutputKind = "tool"
)

type ResponseOutputItem struct {
	Kind             ResponseOutputKind
	Text             string
	EncryptedContent string
	ToolIndex        int
}

type ResponseOutputState struct {
	Items []ResponseOutputItem
}

func SetResponseOutputState(ctx context.Context, state *ResponseOutputState) {
	bridgeState := contextStateFrom(ctx)
	if bridgeState == nil || state == nil {
		return
	}
	bridgeState.responseOutputState = state
}

func ResponseOutputStateFromContext(ctx context.Context) *ResponseOutputState {
	bridgeState := contextStateFrom(ctx)
	if bridgeState == nil {
		return nil
	}
	return bridgeState.responseOutputState
}
