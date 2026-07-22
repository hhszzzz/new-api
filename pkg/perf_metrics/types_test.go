package perfmetrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAtomicBucketDrainAndRestorePreserveCompleteSamples(t *testing.T) {
	bucket := &atomicBucket{}
	first := Sample{
		Success:      true,
		LatencyMs:    120,
		HasTtft:      true,
		TtftMs:       30,
		OutputTokens: 24,
		GenerationMs: 600,
	}
	bucket.add(first)

	drained := bucket.drain()
	assert.Equal(t, counters{
		requestCount:   1,
		successCount:   1,
		totalLatencyMs: 120,
		ttftSumMs:      30,
		ttftCount:      1,
		outputTokens:   24,
		generationMs:   600,
	}, drained)
	assert.Equal(t, counters{}, bucket.snapshot())
	assert.Equal(t, drained, bucket.totalSnapshot())

	bucket.add(Sample{LatencyMs: 80})
	bucket.addCounters(drained)
	assert.Equal(t, counters{
		requestCount:   2,
		successCount:   1,
		totalLatencyMs: 200,
		ttftSumMs:      30,
		ttftCount:      1,
		outputTokens:   24,
		generationMs:   600,
	}, bucket.snapshot())
	assert.Equal(t, counters{
		requestCount:   2,
		successCount:   1,
		totalLatencyMs: 200,
		ttftSumMs:      30,
		ttftCount:      1,
		outputTokens:   24,
		generationMs:   600,
	}, bucket.totalSnapshot())
}
