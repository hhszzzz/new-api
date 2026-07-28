package relayconvert

import (
	"context"

	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
)

// WithProtocolBridgeContext creates attempt-scoped mutable conversion state.
// Hosts should install the returned context before request conversion so the
// corresponding response and stream converters can restore tool identities.
func WithProtocolBridgeContext(ctx context.Context) context.Context {
	return sharedbridge.WithContext(ctx)
}

func ResetProtocolBridgeContext(ctx context.Context) {
	sharedbridge.ResetContext(ctx)
}
