package service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	assert.Equal(t, start, resolved.bucketAnchor)
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

func TestRankingCacheNowKeepsRollingAndFutureCustomRangesStable(t *testing.T) {
	first := time.Unix(2_000_000_110, 0)
	second := first.Add(2 * time.Minute)
	start := first.Add(-24 * time.Hour).Unix()
	futureEnd := first.Add(24 * time.Hour).Unix()

	assert.Equal(t, rankingCacheNow(first), rankingCacheNow(second))
	firstResolved, err := resolveRankingRange("custom", &start, &futureEnd, first)
	require.NoError(t, err)
	firstRange, err := resolveRankingCacheRange(RankingsRequest{
		Period:         "custom",
		StartTimestamp: &start,
		EndTimestamp:   &futureEnd,
	}, firstResolved, first)
	require.NoError(t, err)
	secondResolved, err := resolveRankingRange("custom", &start, &futureEnd, second)
	require.NoError(t, err)
	secondRange, err := resolveRankingCacheRange(RankingsRequest{
		Period:         "custom",
		StartTimestamp: &start,
		EndTimestamp:   &futureEnd,
	}, secondResolved, second)
	require.NoError(t, err)
	assert.Equal(t, firstRange.end, secondRange.end)

	pastEnd := first.Add(-time.Hour).Unix()
	pastResolved, err := resolveRankingRange("custom", &start, &pastEnd, first)
	require.NoError(t, err)
	pastRange, err := resolveRankingCacheRange(RankingsRequest{
		Period:         "custom",
		StartTimestamp: &start,
		EndTimestamp:   &pastEnd,
	}, pastResolved, first)
	require.NoError(t, err)
	assert.Equal(t, pastEnd, pastRange.end)
}

func TestRankingCacheRangeAcceptsStartInsideCurrentCacheWindow(t *testing.T) {
	now := time.Unix(2_000_000_110, 0)
	start := rankingCacheNow(now).Add(5 * time.Second).Unix()
	end := now.Add(time.Hour).Unix()
	resolved, err := resolveRankingRange("custom", &start, &end, now)
	require.NoError(t, err)

	cacheRange, err := resolveRankingCacheRange(RankingsRequest{
		Period:         "custom",
		StartTimestamp: &start,
		EndTimestamp:   &end,
	}, resolved, now)

	require.NoError(t, err)
	assert.Equal(t, start, cacheRange.start)
	assert.Equal(t, start, cacheRange.end)
}

func TestLoadRankingsSnapshotCoalescesConcurrentCacheMisses(t *testing.T) {
	InvalidateRankingsCache()
	t.Cleanup(InvalidateRankingsCache)

	const workerCount = 16
	var loadCount atomic.Int32
	ready := sync.WaitGroup{}
	ready.Add(workerCount)
	started := make(chan struct{})
	release := make(chan struct{})
	type result struct {
		data *RankingsResponse
		err  error
	}
	results := make(chan result, workerCount)
	load := func() (*RankingsResponse, error) {
		if loadCount.Add(1) == 1 {
			close(started)
		}
		<-release
		return &RankingsResponse{TotalTokens: 42}, nil
	}

	for range workerCount {
		go func() {
			ready.Done()
			data, err := loadRankingsSnapshot(t.Name(), load)
			results <- result{data: data, err: err}
		}()
	}
	ready.Wait()
	<-started
	close(release)

	for range workerCount {
		result := <-results
		require.NoError(t, result.err)
		require.NotNil(t, result.data)
		assert.Equal(t, int64(42), result.data.TotalTokens)
	}
	assert.Equal(t, int32(1), loadCount.Load())
}

func TestNormalizeRankingViewerFailsClosed(t *testing.T) {
	assert.Equal(t, RankingViewerAnonymous, normalizeRankingViewer(""))
	assert.Equal(t, RankingViewerAnonymous, normalizeRankingViewer("unexpected"))
	assert.Equal(t, RankingViewerUser, normalizeRankingViewer(RankingViewerUser))
	assert.Equal(t, RankingViewerAdmin, normalizeRankingViewer(RankingViewerAdmin))
}

func TestMaskRankingUsernameDoesNotRevealShortNames(t *testing.T) {
	tests := []struct {
		username string
		want     string
	}{
		{username: "", want: ""},
		{username: "a", want: "***"},
		{username: "ab", want: "a***"},
		{username: "abc", want: "a***c"},
		{username: "张三", want: "张***"},
	}
	for _, test := range tests {
		t.Run(test.username, func(t *testing.T) {
			assert.Equal(t, test.want, maskRankingUsername(test.username))
		})
	}
}

func TestBuildRankingUserUsageMasksNamesWithoutChangingAdminRanking(t *testing.T) {
	rows := []model.RankingUserQuotaRow{
		{UserID: 1, Username: "alice", UseGroup: "team", TotalTokens: 100, TotalQuota: 500000},
		{UserID: 1, Username: "alice", UseGroup: "", TotalTokens: 50, TotalQuota: 250000},
		{UserID: 2, Username: "bob", UseGroup: "secret", TotalTokens: 0, TotalQuota: 750000},
	}
	regularUsage := buildRankingUserUsage(rows, 150, 1500000, false, 500000)
	adminUsage := buildRankingUserUsage(rows, 150, 1500000, true, 500000)
	require.Len(t, regularUsage.Users, 2)
	require.Len(t, adminUsage.Users, 2)
	assert.Equal(t, adminUsage.TotalTokens, regularUsage.TotalTokens)
	assert.Equal(t, adminUsage.TotalQuota, regularUsage.TotalQuota)
	assert.Equal(t, adminUsage.TotalUSD, regularUsage.TotalUSD)
	assert.Equal(t, "a***e", regularUsage.Users[0].Username)
	assert.Equal(t, "b***b", regularUsage.Users[1].Username)
	assert.Equal(t, "alice", adminUsage.Users[0].Username)
	assert.Equal(t, "bob", adminUsage.Users[1].Username)
	for index := range adminUsage.Users {
		regularUser := regularUsage.Users[index]
		adminUser := adminUsage.Users[index]
		regularUser.Username = adminUser.Username
		assert.Equal(t, adminUser, regularUser)
	}
	assert.InDelta(t, 2.0/3.0, regularUsage.Users[0].Groups[0].QuotaShare, 0.0001)
	assert.Equal(t, "secret", regularUsage.Users[1].Groups[0].UseGroup)
}

func TestBuildRankingUserUsageOmitsUnattributedRows(t *testing.T) {
	usage := buildRankingUserUsage([]model.RankingUserQuotaRow{
		{Username: "", TotalTokens: 100, TotalQuota: 500000},
		{UserID: 2, Username: "bob", UseGroup: "default", TotalTokens: 50, TotalQuota: 250000},
	}, 150, 750000, false, 500000)

	require.Len(t, usage.Users, 1)
	assert.Equal(t, "b***b", usage.Users[0].Username)
	assert.Equal(t, int64(250000), usage.Users[0].TotalQuota)
	assert.InDelta(t, 1.0/3.0, usage.Users[0].QuotaShare, 0.0001)
	assert.Equal(t, int64(750000), usage.TotalQuota)
}

func TestBuildRankingUserUsageDisambiguatesMaskedUsernameCollisions(t *testing.T) {
	usage := buildRankingUserUsage([]model.RankingUserQuotaRow{
		{UserID: 1, Username: "alice", UseGroup: "default", TotalQuota: 2},
		{UserID: 2, Username: "annie", UseGroup: "default", TotalQuota: 1},
	}, 0, 3, false, 500000)

	require.Len(t, usage.Users, 2)
	assert.Equal(t, "a***e", usage.Users[0].Username)
	assert.Equal(t, "a***e (2)", usage.Users[1].Username)
}

func TestBuildRankingUserUsageAggregatesUsernamesByStableUserID(t *testing.T) {
	usage := buildRankingUserUsage([]model.RankingUserQuotaRow{
		{UserID: 7, Username: "alice", UseGroup: "team-a", TotalTokens: 10, TotalQuota: 100},
		{UserID: 7, Username: "alice-renamed", UseGroup: "team-b", TotalTokens: 20, TotalQuota: 200},
	}, 30, 300, true, 500000)

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

func TestRankingBucketLabelUsesUTCToAvoidTimezoneInconsistency(t *testing.T) {
	// 2026-08-16 12:00:00 UTC
	bucket := int64(1786881600)
	config := rankingPeriodConfig{labelLayout: "Jan 2 15:04"}

	label := rankingBucketLabel(bucket, config)

	// Label should reflect UTC time regardless of server timezone
	assert.Equal(t, "Aug 16 12:00", label)
}
