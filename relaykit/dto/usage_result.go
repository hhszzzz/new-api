package dto

// UsageResult is a closed set of upstream usage envelopes. Realtime accounting
// retains its bidirectional audio/text breakdown instead of coercing it into
// the one-response usage schema used by text and media operations.
type UsageResult interface {
	usageResult()
}

func (*Usage) usageResult()         {}
func (*RealtimeUsage) usageResult() {}
