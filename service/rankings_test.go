package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRankingRangeUsesCustomBucketsAndPreviousEqualSpan(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	start := now.Unix() - int64(48*time.Hour/time.Second) + 1
	end := now.Unix() + 24*60*60
	resolved, err := resolveRankingRange("custom", &start, &end, now)
	require.NoError(t, err)
	assert.Equal(t, now.Unix(), resolved.end)
	assert.Equal(t, int64(3600), resolved.config.bucketSize)
	assert.Equal(t, "hour", resolved.config.bucketName)
	span := resolved.end - resolved.start + 1
	assert.Equal(t, span, resolved.previousEnd-resolved.previousStart+1)
	assert.Equal(t, resolved.start-1, resolved.previousEnd)

	dayStart := now.Unix() - 60*24*60*60 + 1
	dayEnd := now.Unix()
	resolved, err = resolveRankingRange("custom", &dayStart, &dayEnd, now)
	require.NoError(t, err)
	assert.Equal(t, int64(24*60*60), resolved.config.bucketSize)

	weekStart := now.Unix() - 61*24*60*60 + 1
	resolved, err = resolveRankingRange("custom", &weekStart, &dayEnd, now)
	require.NoError(t, err)
	assert.Equal(t, int64(7*24*60*60), resolved.config.bucketSize)
}

func TestResolveRankingRangeRejectsInvalidAndOversizedCustomRanges(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	boundaryStart := now.Unix() - int64(366*24*time.Hour/time.Second) + 1
	end := now.Unix()
	_, err := resolveRankingRange("custom", &boundaryStart, &end, now)
	require.NoError(t, err)
	tooLongByOneSecond := boundaryStart - 1
	_, err = resolveRankingRange("custom", &tooLongByOneSecond, &end, now)
	require.Error(t, err)

	start := now.Unix() - int64(367*24*time.Hour/time.Second)
	_, err = resolveRankingRange("custom", &start, &end, now)
	require.Error(t, err)

	_, err = resolveRankingRange("custom", nil, &end, now)
	require.Error(t, err)
	start = now.Unix() + 1
	_, err = resolveRankingRange("custom", &start, &end, now)
	require.Error(t, err)
}

func TestRankingResolutionNowKeepsPresetCacheKeysStable(t *testing.T) {
	first := time.Unix(2_000_000_110, 0)
	second := first.Add(2 * time.Minute)

	assert.Equal(t, rankingResolutionNow("week", first), rankingResolutionNow("week", second))
	assert.Equal(t, second, rankingResolutionNow("custom", second))
}

func TestNormalizeRankingViewerFailsClosed(t *testing.T) {
	assert.Equal(t, RankingViewerAnonymous, normalizeRankingViewer(""))
	assert.Equal(t, RankingViewerAnonymous, normalizeRankingViewer("unexpected"))
	assert.Equal(t, RankingViewerUser, normalizeRankingViewer(RankingViewerUser))
	assert.Equal(t, RankingViewerAdmin, normalizeRankingViewer(RankingViewerAdmin))
}

func TestBuildRankingUserUsageMasksHiddenUsersAndComputesGroupShares(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	usage := buildRankingUserUsage([]model.RankingUserQuotaRow{
		{UserID: 1, Username: "alice", UseGroup: "team", TotalTokens: 100, TotalQuota: 500000},
		{UserID: 1, Username: "alice", UseGroup: "", TotalTokens: 50, TotalQuota: 250000},
		{UserID: 2, Username: "bob", UseGroup: "secret", HiddenModel: true, TotalTokens: 0, TotalQuota: 750000},
	}, 150, 1500000, false)
	require.NotNil(t, usage)
	require.Len(t, usage.Users, 2)
	usersByName := make(map[string]RankingUser, len(usage.Users))
	for _, user := range usage.Users {
		usersByName[user.Username] = user
	}
	assert.Equal(t, int64(750000), usersByName["Other users"].TotalQuota)
	require.Len(t, usersByName["Other users"].Groups, 1)
	assert.Equal(t, "Unknown", usersByName["Other users"].Groups[0].UseGroup)
	assert.InDelta(t, 2.0/3.0, usersByName["a***e"].Groups[0].QuotaShare, 0.0001)
	assert.Equal(t, 3.0, usage.TotalUSD)

	adminUsage := buildRankingUserUsage([]model.RankingUserQuotaRow{
		{UserID: 1, Username: "alice", UseGroup: "team", TotalTokens: 1, TotalQuota: 500000},
	}, 1, 500000, true)
	assert.Equal(t, "alice", adminUsage.Users[0].Username)
}

func TestBuildRankingUserUsageDisambiguatesMaskedUsernameCollisions(t *testing.T) {
	usage := buildRankingUserUsage([]model.RankingUserQuotaRow{
		{UserID: 1, Username: "alice", UseGroup: "default", TotalQuota: 2},
		{UserID: 2, Username: "annie", UseGroup: "default", TotalQuota: 1},
	}, 0, 3, false)

	require.Len(t, usage.Users, 2)
	assert.Equal(t, "a***e", usage.Users[0].Username)
	assert.Equal(t, "a***e (2)", usage.Users[1].Username)
}

func TestBuildRankingUserUsageAggregatesUsernamesByStableUserID(t *testing.T) {
	usage := buildRankingUserUsage([]model.RankingUserQuotaRow{
		{UserID: 7, Username: "alice", UseGroup: "team-a", TotalTokens: 10, TotalQuota: 100},
		{UserID: 7, Username: "alice-renamed", UseGroup: "team-b", TotalTokens: 20, TotalQuota: 200},
	}, 30, 300, true)

	require.Len(t, usage.Users, 1)
	assert.Equal(t, int64(300), usage.Users[0].TotalQuota)
	assert.Len(t, usage.Users[0].Groups, 2)
}

func TestRankingCacheDropsExpiredEntriesAndEnforcesCapacity(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	rankingCacheMu.Lock()
	rankingCache = make(map[string]rankingCacheItem, rankingCacheMaxEntries+2)
	rankingCache["expired"] = rankingCacheItem{expiresAt: now.Add(-time.Second)}
	for index := 0; index <= rankingCacheMaxEntries; index++ {
		rankingCache[fmt.Sprintf("active-%03d", index)] = rankingCacheItem{
			expiresAt: now.Add(time.Duration(index+1) * time.Second),
		}
	}
	cleanRankingCacheLocked(now)
	trimRankingCacheLocked()
	_, expiredExists := rankingCache["expired"]
	_, oldestExists := rankingCache["active-000"]
	cacheSize := len(rankingCache)
	rankingCache = map[string]rankingCacheItem{}
	rankingCacheMu.Unlock()

	assert.False(t, expiredExists)
	assert.False(t, oldestExists)
	assert.Equal(t, rankingCacheMaxEntries, cacheSize)
}
