package types

import (
	"fmt"
	"strings"
)

type ConversionDiagnosticSeverity string

const (
	ConversionDiagnosticWarning ConversionDiagnosticSeverity = "warning"
	ConversionDiagnosticError   ConversionDiagnosticSeverity = "error"
)

type ConversionDiagnostic struct {
	LossClass string                       `json:"loss_class,omitempty"`
	Code      string                       `json:"code"`
	Path      string                       `json:"path,omitempty"`
	Message   string                       `json:"message"`
	Severity  ConversionDiagnosticSeverity `json:"severity"`
	From      RelayFormat                  `json:"from"`
	To        RelayFormat                  `json:"to"`
}

const (
	// ConversionLossPresentation classifies a loss that only changes how results
	// are shaped or displayed. That includes preferences about how much result
	// content a hosted tool run returns (search context size, a return-token
	// budget, response inclusion): they cap no run, so they are not tuning.
	ConversionLossPresentation = "presentation"
	// ConversionLossTuning classifies dropping a cap on how many times a hosted
	// tool may run (for example a web search max_uses limit). It neither widens
	// what the tool can reach nor changes the shape of its results, but it removes
	// a limit the client set, so the safe policy still rejects it; only an
	// explicit lossy opt-in tolerates it.
	ConversionLossTuning   = "tuning"
	ConversionLossSemantic = "semantic"
)

type ConversionLossPolicy string

const (
	// ConversionLossPolicySafe rejects request-phase conversions that would
	// change tool execution semantics, while returning non-fatal loss as
	// diagnostics. This is the default policy for conversion sessions.
	ConversionLossPolicySafe ConversionLossPolicy = "safe"
	// ConversionLossPolicyStrict rejects every lossy conversion, including
	// presentation-only metadata loss.
	ConversionLossPolicyStrict ConversionLossPolicy = "strict"
	// ConversionLossPolicyLossy rejects losses that change tool execution
	// semantics while tolerating presentation and execution-tuning loss. Hosts
	// select it only for a channel that explicitly opted into lossy conversion.
	ConversionLossPolicyLossy ConversionLossPolicy = "lossy"
	// ConversionLossPolicyAllow is retained for explicit legacy callers only.
	// Host protocol policies never select this unrestricted mode.
	ConversionLossPolicyAllow ConversionLossPolicy = "allow"
)

type ConversionLossError struct {
	Diagnostics []ConversionDiagnostic
}

func (e *ConversionLossError) Error() string {
	if e == nil || len(e.Diagnostics) == 0 {
		return "conversion would lose protocol semantics"
	}
	messages := make([]string, 0, len(e.Diagnostics))
	for _, diagnostic := range e.Diagnostics {
		message := diagnostic.Message
		if message == "" {
			message = diagnostic.Code
		}
		if diagnostic.Path != "" {
			message = fmt.Sprintf("%s: %s", diagnostic.Path, message)
		}
		messages = append(messages, message)
	}
	return "conversion would lose protocol semantics: " + strings.Join(messages, "; ")
}

func RejectConversionLoss(policy ConversionLossPolicy, diagnostics []ConversionDiagnostic) error {
	if policy == ConversionLossPolicyAllow || len(diagnostics) == 0 {
		return nil
	}
	rejected := make([]ConversionDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if conversionLossIsRejected(policy, diagnostic) {
			rejected = append(rejected, diagnostic)
		}
	}
	if len(rejected) == 0 {
		return nil
	}
	return &ConversionLossError{Diagnostics: rejected}
}

// conversionLossIsRejected reports whether the policy refuses a single loss.
// The tiers nest: strict refuses every loss, safe refuses everything except
// display metadata, and lossy refuses everything except display metadata and
// execution tuning. An error-severity diagnostic is fatal under both safe and
// lossy whatever its class, and losses of an unclassified class stay fatal under
// lossy, so neither a producer's explicit refusal nor a newly introduced class
// is ever tolerated by accident.
func conversionLossIsRejected(policy ConversionLossPolicy, diagnostic ConversionDiagnostic) bool {
	switch policy {
	case ConversionLossPolicyStrict:
		return true
	case ConversionLossPolicyLossy:
		return diagnostic.Severity == ConversionDiagnosticError ||
			(diagnostic.LossClass != ConversionLossPresentation && diagnostic.LossClass != ConversionLossTuning)
	default:
		return diagnostic.Severity == ConversionDiagnosticError || diagnostic.LossClass != ConversionLossPresentation
	}
}
