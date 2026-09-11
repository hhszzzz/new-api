package perfmetrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAtomicBucketDrainAndRestorePreserveCompleteSamples(t *testing.T) {
	bucket := &atomicBucket{}
	first := Sample{
		Success:         true,
		LatencyMs:       120,
		HasTtft:         true,
		TtftMs:          30,
		OutputTokens:    24,
		GenerationMs:    600,
		CacheHitTokens:  100,
		CacheMissTokens: 300,
	}
	bucket.add(first)

	drained := bucket.drain()
	assert.Equal(t, counters{
		requestCount:    1,
		successCount:    1,
		totalLatencyMs:  120,
		ttftSumMs:       30,
		ttftCount:       1,
		outputTokens:    24,
		generationMs:    600,
		cacheHitTokens:  100,
		cacheMissTokens: 300,
	}, drained)
	assert.Equal(t, counters{}, bucket.snapshot())
	assert.Equal(t, drained, bucket.totalSnapshot())

	bucket.add(Sample{LatencyMs: 80})
	bucket.addCounters(drained)
	assert.Equal(t, counters{
		requestCount:    2,
		successCount:    1,
		totalLatencyMs:  200,
		ttftSumMs:       30,
		ttftCount:       1,
		outputTokens:    24,
		generationMs:    600,
		cacheHitTokens:  100,
		cacheMissTokens: 300,
	}, bucket.snapshot())
	assert.Equal(t, counters{
		requestCount:    2,
		successCount:    1,
		totalLatencyMs:  200,
		ttftSumMs:       30,
		ttftCount:       1,
		outputTokens:    24,
		generationMs:    600,
		cacheHitTokens:  100,
		cacheMissTokens: 300,
	}, bucket.totalSnapshot())
}

func TestCacheHitRateNeedsCacheableInputAndRounds(t *testing.T) {
	assert.Nil(t, (counters{}).CacheHitRate())
	assert.Nil(t, (counters{requestCount: 5}).CacheHitRate())

	rate := counters{cacheHitTokens: 750, cacheMissTokens: 250}.CacheHitRate()
	if assert.NotNil(t, rate) {
		assert.InDelta(t, 75.0, *rate, 0.0001)
	}

	zeroHitRate := counters{cacheHitTokens: 0, cacheMissTokens: 100}.CacheHitRate()
	if assert.NotNil(t, zeroHitRate) {
		assert.InDelta(t, 0.0, *zeroHitRate, 0.0001)
	}

	// Miss-only and hit-only buckets add up before the ratio is taken, so the
	// aggregate is a true token-weighted rate rather than an average of ratios.
	combined := counters{cacheHitTokens: 300, cacheMissTokens: 700}
	combined.add(counters{cacheHitTokens: 300, cacheMissTokens: 100})
	rounded := roundedCacheHitRate(combined)
	if assert.NotNil(t, rounded) {
		assert.InDelta(t, 42.86, *rounded, 0.001)
	}
}
