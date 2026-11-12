package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

const channelAdmissionMetricsWindow = 7 * 24 * time.Hour

type ChannelAdmissionMetricPoint struct {
	TimestampMs int64 `json:"timestamp_ms"`
	Grants      int64 `json:"grants"`
	SoupGrants  int64 `json:"soup_grants"`
	QueueFull   int64 `json:"queue_full"`
	Timeouts    int64 `json:"timeouts"`
	WaitP50Ms   int64 `json:"wait_p50_ms"`
	WaitP95Ms   int64 `json:"wait_p95_ms"`
}

type ChannelAdmissionStats struct {
	ChannelID     int                           `json:"channel_id"`
	WindowStartMs int64                         `json:"window_start_ms"`
	WindowEndMs   int64                         `json:"window_end_ms"`
	Active        int64                         `json:"active"`
	Waiting       int64                         `json:"waiting"`
	OldestWaitMs  int64                         `json:"oldest_wait_ms"`
	WaitP50Ms     int64                         `json:"wait_p50_ms"`
	WaitP95Ms     int64                         `json:"wait_p95_ms"`
	Grants        int64                         `json:"grants"`
	SoupGrants    int64                         `json:"soup_grants"`
	QueueFull     int64                         `json:"queue_full"`
	Timeouts      int64                         `json:"timeouts"`
	MinuteSeries  []ChannelAdmissionMetricPoint `json:"minute_series"`
}

func GetChannelAdmissionStats(ctx context.Context, channelID int, start, end time.Time) (*ChannelAdmissionStats, error) {
	if channelID <= 0 {
		return nil, fmt.Errorf("channel id must be positive")
	}
	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-channelAdmissionMetricsWindow)
	}
	if start.Before(end.Add(-channelAdmissionMetricsWindow)) {
		start = end.Add(-channelAdmissionMetricsWindow)
	}
	if start.After(end) {
		return nil, fmt.Errorf("start time must not be after end time")
	}
	client, enabled := getChannelRateLimitRedisClient()
	if !enabled {
		return nil, fmt.Errorf("redis is unavailable")
	}
	keys := channelRateLimitKeys(channelID, 0)
	startMs, endMs := start.UnixMilli(), end.UnixMilli()
	stats := &ChannelAdmissionStats{
		ChannelID:     channelID,
		WindowStartMs: startMs,
		WindowEndMs:   endMs,
		MinuteSeries:  make([]ChannelAdmissionMetricPoint, 0),
	}
	active, err := client.ZCard(ctx, keys[1]).Result()
	if err != nil {
		return nil, err
	}
	waiting, err := client.ZRange(ctx, keys[4], 0, -1).Result()
	if err != nil {
		return nil, err
	}
	stats.Active = active
	stats.Waiting = int64(len(waiting))
	nowMs := time.Now().UnixMilli()
	var oldestEnqueued int64
	for _, member := range waiting {
		prefix, _, found := strings.Cut(member, "|")
		if !found {
			continue
		}
		enqueued, parseErr := strconv.ParseInt(prefix, 10, 64)
		if parseErr == nil && (oldestEnqueued == 0 || enqueued < oldestEnqueued) {
			oldestEnqueued = enqueued
		}
	}
	if oldestEnqueued > 0 && nowMs > oldestEnqueued {
		stats.OldestWaitMs = nowMs - oldestEnqueued
	}

	members, err := client.ZRangeByScore(ctx, keys[9], &redis.ZRangeBy{
		Min: strconv.FormatInt(startMs, 10),
		Max: strconv.FormatInt(endMs, 10),
	}).Result()
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return stats, nil
	}
	values, err := client.HMGet(ctx, keys[8], members...).Result()
	if err != nil {
		return nil, err
	}
	byMinute := make(map[int64]map[string]int64)
	for index, member := range members {
		minuteText, metric, found := strings.Cut(member, "|")
		if !found || index >= len(values) || values[index] == nil {
			continue
		}
		minute, minuteErr := strconv.ParseInt(minuteText, 10, 64)
		value, valueErr := strconv.ParseInt(fmt.Sprint(values[index]), 10, 64)
		if minuteErr != nil || valueErr != nil {
			continue
		}
		if byMinute[minute] == nil {
			byMinute[minute] = make(map[string]int64)
		}
		byMinute[minute][metric] = value
	}
	minutes := make([]int64, 0, len(byMinute))
	for minute := range byMinute {
		minutes = append(minutes, minute)
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i] < minutes[j] })
	allWaits := make(map[string]int64)
	for _, minute := range minutes {
		counts := byMinute[minute]
		point := ChannelAdmissionMetricPoint{
			TimestampMs: minute * 60_000,
			Grants:      counts["grants"],
			SoupGrants:  counts["soup_grants"],
			QueueFull:   counts["queue_full"],
			Timeouts:    counts["timeout"],
			WaitP50Ms:   channelAdmissionPercentile(counts, 50),
			WaitP95Ms:   channelAdmissionPercentile(counts, 95),
		}
		stats.Grants += point.Grants
		stats.SoupGrants += point.SoupGrants
		stats.QueueFull += point.QueueFull
		stats.Timeouts += point.Timeouts
		for bucket, count := range counts {
			if strings.HasPrefix(bucket, "wait_") {
				allWaits[bucket] += count
			}
		}
		stats.MinuteSeries = append(stats.MinuteSeries, point)
	}
	stats.WaitP50Ms = channelAdmissionPercentile(allWaits, 50)
	stats.WaitP95Ms = channelAdmissionPercentile(allWaits, 95)
	return stats, nil
}

func channelAdmissionPercentile(counts map[string]int64, percentile int64) int64 {
	buckets := []struct {
		key string
		max int64
	}{
		{key: "wait_le_100ms", max: 100},
		{key: "wait_le_500ms", max: 500},
		{key: "wait_le_1s", max: 1_000},
		{key: "wait_le_5s", max: 5_000},
		{key: "wait_le_15s", max: 15_000},
		{key: "wait_le_30s", max: 30_000},
		{key: "wait_le_60s", max: 60_000},
		{key: "wait_le_120s", max: 120_000},
		{key: "wait_le_300s", max: 300_000},
		{key: "wait_gt_300s", max: 300_000},
	}
	var total int64
	for _, bucket := range buckets {
		total += counts[bucket.key]
	}
	if total == 0 {
		return 0
	}
	target := (total*percentile + 99) / 100
	var cumulative int64
	for _, bucket := range buckets {
		cumulative += counts[bucket.key]
		if cumulative >= target {
			return bucket.max
		}
	}
	return buckets[len(buckets)-1].max
}
