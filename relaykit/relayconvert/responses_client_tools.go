package relayconvert

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	oairesponses "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_responses"
)

type ResponsesClientToolBridge = oairesponses.ResponsesClientToolBridge
type ResponsesClientToolStreamRestorer = oairesponses.ResponsesClientToolStreamRestorer

// LowerResponsesClientTools converts Codex-only Responses client tool shapes
// into ordinary function tools for strict Responses-compatible upstreams.
func LowerResponsesClientTools(request *dto.OpenAIResponsesRequest) (*ResponsesClientToolBridge, bool, error) {
	return oairesponses.LowerResponsesClientTools(request)
}
