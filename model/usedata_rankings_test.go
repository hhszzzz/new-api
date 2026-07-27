package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRankingAggregatesQuotaOnlyRowsAndScopedUserGroups(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Exec("DELETE FROM quota_data_scoped").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM quota_data_scoped")
	})

	require.NoError(t, DB.Create(&QuotaData{
		UserID: 1, Username: "alice", ModelName: "legacy-secret", CreatedAt: 3600,
		UseGroup: "", TokenUsed: 0, Quota: 500000, Count: 1,
	}).Error)
	require.NoError(t, DB.Create(&[]ScopedQuotaData{
		{
			UserID: 2, Username: "bob", ModelName: "visible-model", ModelScope: QuotaModelScopeRequested,
			CreatedAt: 3600, UseGroup: "team", TokenUsed: 100, Quota: 250000, Count: 1,
		},
		{
			UserID: 2, Username: "bob", ModelName: "admin-only", ModelScope: QuotaModelScopeAdminOnly,
			CreatedAt: 3600, UseGroup: "secret", TokenUsed: 0, Quota: 750000, Count: 1,
		},
	}).Error)

	totals, err := GetRankingQuotaTotals(0, 7200, []string{"visible-model"}, false)
	require.NoError(t, err)
	require.Len(t, totals, 2)
	assert.Equal(t, "visible-model", totals[0].ModelName)
	assert.Equal(t, int64(100), totals[0].TotalTokens)
	assert.Equal(t, int64(250000), totals[0].TotalQuota)
	assert.Equal(t, "", totals[1].ModelName)
	assert.Equal(t, int64(0), totals[1].TotalTokens)
	assert.Equal(t, int64(1250000), totals[1].TotalQuota)

	buckets, err := GetRankingQuotaBuckets(0, 7200, 3600, 0, []string{"visible-model"}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1500000), sumRankingTestQuota(buckets))

	users, err := GetRankingUserQuotaTotals(0, 7200, []string{"visible-model"}, false)
	require.NoError(t, err)
	require.Len(t, users, 3)
	var hiddenQuota int64
	for _, row := range users {
		if row.HiddenModel {
			hiddenQuota += row.TotalQuota
			assert.Contains(t, []string{"", "secret"}, row.UseGroup)
		}
	}
	assert.Equal(t, int64(1250000), hiddenQuota)

	adminUsers, err := GetRankingUserQuotaTotals(0, 7200, nil, true)
	require.NoError(t, err)
	assert.Len(t, adminUsers, 3)
	for _, row := range adminUsers {
		assert.False(t, row.HiddenModel)
	}
}

func TestRankingQuotaBucketsCanAnchorToRequestedRangeStart(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Exec("DELETE FROM quota_data_scoped").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM quota_data_scoped")
	})

	const start = int64(28_800)
	require.NoError(t, DB.Create(&[]QuotaData{
		{ModelName: "anchored-model", CreatedAt: start + 60, TokenUsed: 1, Count: 1},
		{ModelName: "anchored-model", CreatedAt: start + 3600 + 60, TokenUsed: 1, Count: 1},
	}).Error)

	buckets, err := GetRankingQuotaBuckets(start, start+7200, 3600, start, nil, true)
	require.NoError(t, err)
	require.Len(t, buckets, 2)
	assert.Equal(t, start, buckets[0].Bucket)
	assert.Equal(t, start+3600, buckets[1].Bucket)
}

func sumRankingTestQuota(rows []RankingQuotaBucket) int64 {
	var total int64
	for _, row := range rows {
		total += row.Quota
	}
	return total
}
