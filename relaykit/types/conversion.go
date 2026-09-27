package types

import (
	"fmt"
	"regexp"
	"strings"
)

type ConversionDiagnosticSeverity string

const (
	ConversionDiagnosticWarning ConversionDiagnosticSeverity = "warning"
	ConversionDiagnosticError   ConversionDiagnosticSeverity = "error"
)

type ConversionDiagnostic struct {
	LossClass string `json:"loss_class,omitempty"`
	Code      string `json:"code"`
	Path      string `json:"path,omitempty"`
	Message   string `json:"message"`
	// MessageKey is Message with its per-request values replaced by {{name}}
	// slots, so a client can translate the sentence and re-fill it. It is empty
	// for a message that carries no values, where Message is its own key and the
	// recorded JSON stays exactly as it was before this field existed.
	MessageKey string                       `json:"message_key,omitempty"`
	Params     map[string]string            `json:"params,omitempty"`
	Severity   ConversionDiagnosticSeverity `json:"severity"`
	From       RelayFormat                  `json:"from"`
	To         RelayFormat                  `json:"to"`
}

// WithMessage fills in a slotted message on a diagnostic written as a literal.
// Message renders the template for callers that read the log as text, and
// MessageKey with Params carry what a client needs to translate the sentence
// and re-fill its slots.
func (d ConversionDiagnostic) WithMessage(template string, params ...map[string]string) ConversionDiagnostic {
	d.Message, d.MessageKey, d.Params = FillDiagnosticMessage(template, params...)
	return d
}

// FillDiagnosticMessage returns the rendered sentence, the template to use as a
// translation key, and the values that fill the template's slots. A template
// without values is its own key, so callers pass nothing and record no params.
func FillDiagnosticMessage(template string, params ...map[string]string) (string, string, map[string]string) {
	if len(params) == 0 || len(params[0]) == 0 {
		return template, "", nil
	}
	values := params[0]
	// One pass over the template: a value that itself looks like a slot (it can
	// come from the request) is written as-is instead of being filled again.
	message := diagnosticSlot.ReplaceAllStringFunc(template, func(slot string) string {
		if value, ok := values[slot[2:len(slot)-2]]; ok {
			return value
		}
		return slot
	})
	return message, template, values
}

var diagnosticSlot = regexp.MustCompile(`\{\{\w+\}\}`)

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
