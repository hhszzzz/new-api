package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRankingVisibilityCacheKeyIsScopedAndCanonical(t *testing.T) {
	canonical, firstKey := rankingVisibilityCacheKey([]string{"model-b", "model-a", "model-a"}, false)
	require.Equal(t, []string{"model-a", "model-b"}, canonical)

	_, sameKey := rankingVisibilityCacheKey([]string{"model-a", "model-b"}, false)
	_, differentKey := rankingVisibilityCacheKey([]string{"model-a"}, false)
	privateModels, privateKey := rankingVisibilityCacheKey([]string{"model-a"}, true)

	assert.Equal(t, firstKey, sameKey)
	assert.NotEqual(t, firstKey, differentKey)
	assert.Nil(t, privateModels)
	assert.Equal(t, "private", privateKey)
}

func TestNormalizeRedactedRankingRowsPreservesUsageWithoutExposingNames(t *testing.T) {
	totals := normalizeRedactedRankingTotals([]model.RankingQuotaTotal{
		{ModelName: "", TotalTokens: 30},
		{ModelName: rankingOthersLabel, TotalTokens: 20},
		{ModelName: "public-model", TotalTokens: 10},
	})
	require.Len(t, totals, 2)
	assert.Equal(t, rankingOthersLabel, totals[0].ModelName)
	assert.Equal(t, int64(50), totals[0].TotalTokens)
	assert.Equal(t, "public-model", totals[1].ModelName)

	buckets := normalizeRedactedRankingBuckets([]model.RankingQuotaBucket{
		{ModelName: "", Bucket: 3600, Tokens: 30},
		{ModelName: rankingOthersLabel, Bucket: 3600, Tokens: 20},
		{ModelName: "public-model", Bucket: 3600, Tokens: 10},
	})
	require.Len(t, buckets, 2)
	assert.Equal(t, rankingOthersLabel, buckets[0].ModelName)
	assert.Equal(t, int64(50), buckets[0].Tokens)
}

func TestBuildModelHistoryDoesNotDuplicatePreAggregatedOthers(t *testing.T) {
	totals := []model.RankingQuotaTotal{{ModelName: rankingOthersLabel, TotalTokens: 100}}
	for i := 0; i < 11; i++ {
		totals = append(totals, model.RankingQuotaTotal{
			ModelName:   fmt.Sprintf("model-%02d", i),
			TotalTokens: int64(90 - i),
		})
	}

	history := buildModelHistory(nil, totals, map[string]rankingModelMeta{
		rankingOthersLabel: {vendor: "Various"},
	}, rankingPeriodConfig{}, 500000)
	require.Len(t, history.Models, rankingHistoryLimit)
	othersCount := 0
	for _, item := range history.Models {
		if item.Name != rankingOthersLabel {
			continue
		}
		othersCount++
		assert.Equal(t, int64(261), item.Total)
	}
	assert.Equal(t, 1, othersCount)
}
