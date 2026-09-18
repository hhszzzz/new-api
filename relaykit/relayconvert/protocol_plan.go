package relayconvert

import (
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/relaykit/types"
)

type ConversionPlan struct {
	RequestProtocol   Protocol            `json:"request_protocol"`
	UpstreamProtocol  Protocol            `json:"upstream_protocol"`
	Operation         Operation           `json:"operation"`
	Transport         Transport           `json:"transport"`
	CompactionMode    string              `json:"compaction_mode,omitempty"`
	RequestConverter  string              `json:"request_converter,omitempty"`
	ResponseConverter string              `json:"response_converter,omitempty"`
	RequestPath       []types.RelayFormat `json:"request_path,omitempty"`
	ResponsePath      []types.RelayFormat `json:"response_path,omitempty"`
	Losses            []string            `json:"losses,omitempty"`
	Rank              int                 `json:"-"`
}

// PlanConversions ranks complete round trips. A request route without a return
// route is never advertised as usable. Selection does not contact providers.
func PlanConversions(from Protocol, operation Operation, transport Transport, candidates []Protocol, features RequestFeatureSet, policy string) ([]ConversionPlan, error) {
	fromFormat, ok := from.RelayFormat()
	if !ok {
		return nil, fmt.Errorf("unknown request protocol %q", from)
	}
	if operation == "" {
		operation = OperationGenerate
	}
	if transport == "" {
		transport = TransportHTTP
	}
	if policy == "" {
		policy = "safe"
	}
	if !slices.Contains([]string{"native_only", "lossless", "safe"}, policy) {
		return nil, fmt.Errorf("unknown conversion policy %q", policy)
	}
	validOperation := false
	for _, spec := range operationSpecs {
		if spec.ID == operation && (spec.Protocol == from || spec.Protocol == "") && slices.Contains(spec.Transports, transport) {
			validOperation = true
			break
		}
	}
	if !validOperation {
		return nil, fmt.Errorf("%s does not support %s over %s", from, operation, transport)
	}
	plans := make([]ConversionPlan, 0, len(candidates))
	var rejection string
	for _, to := range candidates {
		toFormat, ok := to.RelayFormat()
		if !ok {
			return nil, fmt.Errorf("unknown upstream protocol %q", to)
		}
		plan := ConversionPlan{RequestProtocol: from, UpstreamProtocol: to, Operation: operation, Transport: transport}
		if features.HasMessagesState && to != ProtocolMessages {
			rejection = "Anthropic thinking state requires its originating Messages upstream"
			continue
		}
		if from == to {
			if operation == OperationCompact {
				plan.CompactionMode = CompactionNative
			}
			plans = append(plans, plan)
			continue
		}
		if operation != OperationGenerate && operation != OperationCompact {
			rejection = fmt.Sprintf("%s requires its native protocol", operation)
			continue
		}
		if transport == TransportWebSocket && to != ProtocolResponses {
			rejection = "upstream protocol does not support WebSocket generation"
			continue
		}
		if policy == "native_only" {
			rejection = "protocol conversion is disabled for this channel and model"
			continue
		}
		conversionFeatures := features
		if operation == OperationCompact {
			if err := ValidateCompactionSummaryFeatures(features); err != nil {
				rejection = err.Error()
				continue
			}
			plan.CompactionMode = CompactionSummary
			conversionFeatures = RequestFeatureSet{}
		}
		reason, losses := AnalyzeConversionFeatures(from, to, conversionFeatures, policy == "safe")
		if reason != "" {
			if rejection == "" {
				rejection = reason
			}
			continue
		}
		request, ok := lookupRequestRoute(fromFormat, toFormat)
		if !ok {
			continue
		}
		response, ok := lookupResponseRoute(toFormat, fromFormat)
		if !ok {
			continue
		}
		requestSteps, err := expandRequestConverterSteps(request)
		if err != nil {
			return nil, err
		}
		responseSteps, err := expandResponseConverterSteps(response)
		if err != nil {
			return nil, err
		}
		plan.RequestConverter, plan.ResponseConverter = request.ID, response.ID
		for _, step := range requestSteps {
			plan.RequestPath = append(plan.RequestPath, step.To)
		}
		for _, step := range responseSteps {
			plan.ResponsePath = append(plan.ResponsePath, step.To)
		}
		plan.Losses = losses
		plan.Rank = 1
		if len(requestSteps) > 1 || len(responseSteps) > 1 {
			plan.Rank = 2
		}
		if len(losses) > 0 {
			plan.Rank = 3
		}
		plans = append(plans, plan)
	}
	slices.SortStableFunc(plans, func(a, b ConversionPlan) int { return a.Rank - b.Rank })
	if len(plans) == 0 {
		if rejection == "" {
			rejection = fmt.Sprintf("no complete conversion route from %s to declared upstream protocols", from)
		}
		return nil, fmt.Errorf("%s", rejection)
	}
	return plans, nil
}
