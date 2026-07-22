package perfmetrics

import "sync"

type Store interface {
	Record(sample Sample)
	Query(params QueryParams) (QueryResult, error)
}

type Sample struct {
	Model        string
	Group        string
	LatencyMs    int64
	TtftMs       int64
	HasTtft      bool
	Success      bool
	OutputTokens int64
	GenerationMs int64
}

type QueryParams struct {
	Model string
	Group string
	Hours int
}

type BucketPoint struct {
	Ts           int64   `json:"ts"`
	AvgTtftMs    int64   `json:"avg_ttft_ms"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
}

type GroupResult struct {
	Group        string        `json:"group"`
	AvgTtftMs    int64         `json:"avg_ttft_ms"`
	AvgLatencyMs int64         `json:"avg_latency_ms"`
	SuccessRate  float64       `json:"success_rate"`
	AvgTps       float64       `json:"avg_tps"`
	Series       []BucketPoint `json:"series"`
}

type QueryResult struct {
	ModelName    string        `json:"model_name"`
	SeriesSchema string        `json:"series_schema"`
	Groups       []GroupResult `json:"groups"`
}

type ModelSummary struct {
	ModelName          string    `json:"model_name"`
	AvgLatencyMs       int64     `json:"avg_latency_ms"`
	SuccessRate        float64   `json:"success_rate"`
	AvgTps             float64   `json:"avg_tps"`
	RecentSuccessRates []float64 `json:"recent_success_rates,omitempty"`
	RequestCount       int64     `json:"-"`
}

type SummaryAllResult struct {
	Models []ModelSummary `json:"models"`
}

type Status string

const (
	StatusNoData      Status = "no_data"
	StatusOperational Status = "operational"
	StatusDegraded    Status = "degraded"
	StatusFailed      Status = "failed"
)

type StatusModelSource struct {
	ModelName string
	Vendor    string
}

type StatusPoint struct {
	Ts          int64    `json:"ts"`
	Status      Status   `json:"status"`
	SuccessRate *float64 `json:"success_rate"`
}

type ModelStatus struct {
	ModelName    string        `json:"model_name"`
	Vendor       string        `json:"vendor"`
	SuccessRate  *float64      `json:"success_rate"`
	AvgLatencyMs *int64        `json:"avg_latency_ms"`
	AvgTps       *float64      `json:"avg_tps"`
	Status       Status        `json:"status"`
	Timeline     []StatusPoint `json:"timeline"`
}

type StatusResult struct {
	GeneratedAt int64         `json:"generated_at"`
	WindowHours int           `json:"window_hours"`
	Models      []ModelStatus `json:"models"`
}

type bucketKey struct {
	model    string
	group    string
	bucketTs int64
}

type counters struct {
	requestCount   int64
	successCount   int64
	totalLatencyMs int64
	ttftSumMs      int64
	ttftCount      int64
	outputTokens   int64
	generationMs   int64
}

type atomicBucket struct {
	mu      sync.Mutex
	pending counters
	total   counters
}

func (b *atomicBucket) add(sample Sample) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delta := counters{requestCount: 1}
	if sample.Success {
		delta.successCount = 1
	}
	if sample.LatencyMs > 0 {
		delta.totalLatencyMs = sample.LatencyMs
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		delta.ttftSumMs = sample.TtftMs
		delta.ttftCount = 1
	}
	if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
		delta.outputTokens = sample.OutputTokens
		delta.generationMs = sample.GenerationMs
	}
	b.pending.add(delta)
	b.total.add(delta)
}

func (b *atomicBucket) snapshot() counters {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending
}

func (b *atomicBucket) totalSnapshot() counters {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

func (b *atomicBucket) drain() counters {
	b.mu.Lock()
	defer b.mu.Unlock()

	drained := b.pending
	b.pending = counters{}
	return drained
}

func (b *atomicBucket) addCounters(c counters) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending.add(c)
}

func (c *counters) add(value counters) {
	c.requestCount += value.requestCount
	c.successCount += value.successCount
	c.totalLatencyMs += value.totalLatencyMs
	c.ttftSumMs += value.ttftSumMs
	c.ttftCount += value.ttftCount
	c.outputTokens += value.outputTokens
	c.generationMs += value.generationMs
}
