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

	buckets, err := GetRankingQuotaBuckets(0, 7200, 3600, []string{"visible-model"}, false)
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

func sumRankingTestQuota(rows []RankingQuotaBucket) int64 {
	var total int64
	for _, row := range rows {
		total += row.Quota
	}
	return total
}
