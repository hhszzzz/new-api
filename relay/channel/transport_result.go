package channel

import (
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gorilla/websocket"
)

// TransportResult separates transport resources from response interpretation.
// SDK transports expose a cancellable body in their native protocol; they do
// not write client output or settle billing while making the upstream call.
type TransportResult struct {
	*http.Response
	WebSocket *websocket.Conn
	Transport relayconvert.Transport
	Format    types.RelayFormat
}

func HTTPResult(response *http.Response, err error) (*TransportResult, error) {
	if err != nil || response == nil {
		return nil, err
	}
	return &TransportResult{Response: response, Transport: relayconvert.TransportHTTP}, nil
}

func WebSocketResult(connection *websocket.Conn, err error) (*TransportResult, error) {
	if err != nil || connection == nil {
		return nil, err
	}
	return &TransportResult{WebSocket: connection, Transport: relayconvert.TransportWebSocket}, nil
}
