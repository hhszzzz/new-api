package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/QuantumNous/new-api/model"
)

const (
	channelAdmissionMetricsWindow = 7 * 24 * time.Hour
	channelAdmissionMaximumPoints = 2000
)

type ChannelAdmissionActiveUser struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username,omitempty"`
	Priority int    `json:"priority"`
	Count    int64  `json:"count"`
}

type ChannelAdmissionWaitingUser struct {
	UserID       int    `json:"user_id"`
	Username     string `json:"username,omitempty"`
	Priority     int    `json:"priority"`
	Position     int    `json:"position"`
	Count        int64  `json:"count"`
	OldestWaitMs int64  `json:"oldest_wait_ms"`
}

type ChannelAdmissionWaitingRequest struct {
	ID       string `json:"id"`
	UserID   int    `json:"user_id"`
	Username string `json:"username,omitempty"`
	Priority int    `json:"priority"`
	Position int    `json:"position"`
	WaitMs   int64  `json:"wait_ms"`
}

type ChannelAdmissionMetricPoint struct {
	TimestampMs    int64 `json:"timestamp_ms"`
	Grants         int64 `json:"grants"`
	Waited         int64 `json:"waited"`
	SoupGrants     int64 `json:"soup_grants"`
	PriorityGrants int64 `json:"priority_grants"`
	QueueFull      int64 `json:"queue_full"`
	Timeouts       int64 `json:"timeouts"`
	WaitP50Ms      int64 `json:"wait_p50_ms"`
	WaitP95Ms      int64 `json:"wait_p95_ms"`
}

type ChannelAdmissionRPMUser struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username,omitempty"`
	Count    int64  `json:"count"`
}

type ChannelAdmissionStats struct {
	ChannelID       int                              `json:"channel_id"`
	WindowStartMs   int64                            `json:"window_start_ms"`
	WindowEndMs     int64                            `json:"window_end_ms"`
	BucketSeconds   int64                            `json:"bucket_seconds"`
	Active          int64                            `json:"active"`
	ActiveUsers     int64                            `json:"active_users"`
	ActiveUserList  []ChannelAdmissionActiveUser     `json:"active_user_list"`
	RPM             int64                            `json:"rpm"`
	RPMUsers        []ChannelAdmissionRPMUser        `json:"rpm_users"`
	Waiting         int64                            `json:"waiting"`
	OldestWaitMs    int64                            `json:"oldest_wait_ms"`
	WaitP50Ms       int64                            `json:"wait_p50_ms"`
	WaitP95Ms       int64                            `json:"wait_p95_ms"`
	Grants          int64                            `json:"grants"`
	WaitedGrants    int64                            `json:"waited_grants"`
	SoupGrants      int64                            `json:"soup_grants"`
	PriorityGrants  int64                            `json:"priority_grants"`
	QueueFull       int64                            `json:"queue_full"`
	Timeouts        int64                            `json:"timeouts"`
	WaitingUsers    []ChannelAdmissionWaitingUser    `json:"waiting_users"`
	WaitingRequests []ChannelAdmissionWaitingRequest `json:"waiting_requests"`
	MinuteSeries    []ChannelAdmissionMetricPoint    `json:"minute_series"`
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
		ChannelID:       channelID,
		WindowStartMs:   startMs,
		WindowEndMs:     endMs,
		BucketSeconds:   channelAdmissionBucketSeconds(endMs - startMs),
		ActiveUserList:  make([]ChannelAdmissionActiveUser, 0),
		RPMUsers:        make([]ChannelAdmissionRPMUser, 0),
		WaitingUsers:    make([]ChannelAdmissionWaitingUser, 0),
		WaitingRequests: make([]ChannelAdmissionWaitingRequest, 0),
		MinuteSeries:    make([]ChannelAdmissionMetricPoint, 0),
	}
	if err := fillChannelAdmissionLiveStats(ctx, client, keys, stats); err != nil {
		return nil, err
	}

	members, err := client.ZRangeByScore(ctx, keys[9], &redis.ZRangeBy{
		Min: strconv.FormatInt(startMs, 10),
		Max: strconv.FormatInt(endMs, 10),
	}).Result()
	if err != nil {
		return nil, err
	}
	bucketMs := stats.BucketSeconds * 1000
	byBucket := make(map[int64]map[string]int64)
	windowCounts := make(map[string]int64)
	if len(members) > 0 {
		values, err := client.HMGet(ctx, keys[8], members...).Result()
		if err != nil {
			return nil, err
		}
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
			minuteMs := minute * 60_000
			if minuteMs < startMs || minuteMs > endMs {
				continue
			}
			bucketTs := (minuteMs / bucketMs) * bucketMs
			if byBucket[bucketTs] == nil {
				byBucket[bucketTs] = make(map[string]int64)
			}
			byBucket[bucketTs][metric] += value
			windowCounts[metric] += value
		}
	}
	stats.Grants = windowCounts["grants"]
	stats.SoupGrants = windowCounts["soup_grants"]
	stats.PriorityGrants = windowCounts["priority_grants"]
	stats.QueueFull = windowCounts["queue_full"]
	stats.Timeouts = windowCounts["timeout"]
	stats.WaitedGrants = channelAdmissionWaitedCount(windowCounts)
	stats.WaitP50Ms = channelAdmissionPercentile(windowCounts, 50)
	stats.WaitP95Ms = channelAdmissionPercentile(windowCounts, 95)

	firstBucket := (startMs / bucketMs) * bucketMs
	pointCount := (endMs-firstBucket)/bucketMs + 1
	if pointCount > channelAdmissionMaximumPoints {
		pointCount = channelAdmissionMaximumPoints
	}
	for index := range pointCount {
		timestamp := firstBucket + index*bucketMs
		if timestamp > endMs {
			break
		}
		counts := byBucket[timestamp]
		stats.MinuteSeries = append(stats.MinuteSeries, ChannelAdmissionMetricPoint{
			TimestampMs:    timestamp,
			Grants:         counts["grants"],
			Waited:         channelAdmissionWaitedCount(counts),
			SoupGrants:     counts["soup_grants"],
			PriorityGrants: counts["priority_grants"],
			QueueFull:      counts["queue_full"],
			Timeouts:       counts["timeout"],
			WaitP50Ms:      channelAdmissionPercentile(counts, 50),
			WaitP95Ms:      channelAdmissionPercentile(counts, 95),
		})
	}
	return stats, nil
}

func fillChannelAdmissionLiveStats(ctx context.Context, client *redis.Client, keys []string, stats *ChannelAdmissionStats) error {
	if err := channelAdmissionRPMStats(ctx, client, stats, keys[0], keys[12]); err != nil {
		return err
	}
	activeMembers, err := client.ZRange(ctx, keys[1], 0, -1).Result()
	if err != nil {
		return err
	}
	stats.Active = int64(len(activeMembers))
	stats.ActiveUserList, err = channelAdmissionActiveUserStats(ctx, client, keys[7], activeMembers)
	if err != nil {
		return err
	}
	stats.ActiveUsers = int64(len(stats.ActiveUserList))

	waitingMembers, err := client.ZRange(ctx, keys[4], 0, -1).Result()
	if err != nil {
		return err
	}
	stats.Waiting = int64(len(waitingMembers))
	stats.WaitingUsers, stats.WaitingRequests, err = channelAdmissionWaitingUserStats(ctx, client, keys[5], waitingMembers)
	if err != nil {
		return err
	}
	for _, user := range stats.WaitingUsers {
		if user.OldestWaitMs > stats.OldestWaitMs {
			stats.OldestWaitMs = user.OldestWaitMs
		}
	}
	return nil
}

func channelAdmissionRPMStats(ctx context.Context, client *redis.Client, stats *ChannelAdmissionStats, rpmKey, usersKey string) error {
	nowMinute := time.Now().Unix() / 60
	channelValues, err := client.HMGet(ctx, rpmKey, "minute", "count").Result()
	if err != nil {
		return err
	}
	if len(channelValues) == 2 && channelValues[0] != nil && channelValues[1] != nil {
		minute, minuteErr := strconv.ParseInt(fmt.Sprint(channelValues[0]), 10, 64)
		count, countErr := strconv.ParseInt(fmt.Sprint(channelValues[1]), 10, 64)
		if minuteErr == nil && countErr == nil && minute == nowMinute {
			stats.RPM = count
		}
	}

	userIDs, err := client.ZRange(ctx, usersKey, 0, -1).Result()
	if err != nil || len(userIDs) == 0 {
		return err
	}
	tag := strings.TrimSuffix(rpmKey, ":rpm")
	pipe := client.Pipeline()
	commands := make([]*redis.SliceCmd, len(userIDs))
	parsedIDs := make([]int, len(userIDs))
	for index, userIDText := range userIDs {
		userID, parseErr := strconv.Atoi(userIDText)
		if parseErr != nil || userID <= 0 {
			continue
		}
		parsedIDs[index] = userID
		commands[index] = pipe.HMGet(ctx, fmt.Sprintf("%s:rpm:user:%d", tag, userID), "minute", "count")
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	for index, command := range commands {
		if command == nil {
			continue
		}
		values := command.Val()
		if len(values) != 2 || values[0] == nil || values[1] == nil {
			continue
		}
		minute, minuteErr := strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
		count, countErr := strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
		if minuteErr != nil || countErr != nil || minute != nowMinute || count <= 0 {
			continue
		}
		stats.RPMUsers = append(stats.RPMUsers, ChannelAdmissionRPMUser{
			UserID:   parsedIDs[index],
			Username: channelAdmissionUsername(parsedIDs[index]),
			Count:    count,
		})
	}
	sort.SliceStable(stats.RPMUsers, func(i, j int) bool {
		if stats.RPMUsers[i].Count != stats.RPMUsers[j].Count {
			return stats.RPMUsers[i].Count > stats.RPMUsers[j].Count
		}
		return stats.RPMUsers[i].UserID < stats.RPMUsers[j].UserID
	})
	return nil
}

func channelAdmissionActiveUserStats(ctx context.Context, client *redis.Client, metaKey string, members []string) ([]ChannelAdmissionActiveUser, error) {
	if len(members) == 0 {
		return []ChannelAdmissionActiveUser{}, nil
	}
	values, err := client.HMGet(ctx, metaKey, members...).Result()
	if err != nil {
		return nil, err
	}
	byUser := make(map[int]*ChannelAdmissionActiveUser, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		parts := strings.SplitN(fmt.Sprint(value), "|", 2)
		userID, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil || userID <= 0 {
			continue
		}
		priority := 0
		if len(parts) > 1 {
			if parsedPriority, priorityErr := strconv.Atoi(parts[1]); priorityErr == nil {
				priority = parsedPriority
			}
		}
		entry := byUser[userID]
		if entry == nil {
			entry = &ChannelAdmissionActiveUser{
				UserID:   userID,
				Priority: priority,
			}
			byUser[userID] = entry
		}
		entry.Count++
		if priority > entry.Priority {
			entry.Priority = priority
		}
	}
	users := make([]ChannelAdmissionActiveUser, 0, len(byUser))
	for _, entry := range byUser {
		entry.Username = channelAdmissionUsername(entry.UserID)
		users = append(users, *entry)
	}
	sort.SliceStable(users, func(i, j int) bool {
		if users[i].Count != users[j].Count {
			return users[i].Count > users[j].Count
		}
		return users[i].UserID < users[j].UserID
	})
	return users, nil
}

func channelAdmissionUsername(userID int) string {
	if model.DB == nil {
		return ""
	}
	user, err := model.GetUserCache(userID)
	if err != nil || user == nil {
		return ""
	}
	return user.Username
}

func channelAdmissionWaitingUserStats(ctx context.Context, client *redis.Client, metaKey string, members []string) ([]ChannelAdmissionWaitingUser, []ChannelAdmissionWaitingRequest, error) {
	if len(members) == 0 {
		return []ChannelAdmissionWaitingUser{}, []ChannelAdmissionWaitingRequest{}, nil
	}
	type userAggregate struct {
		count    int64
		oldest   int64
		priority int
		position int
	}
	nowMs := time.Now().UnixMilli()
	leaseIDs := make([]string, 0, len(members))
	enqueuedAt := make([]int64, 0, len(members))
	requestIDs := make([]string, 0, len(members))
	for _, member := range members {
		parts := strings.SplitN(member, "|", 3)
		if len(parts) != 3 {
			continue
		}
		enqueued, parseErr := strconv.ParseInt(parts[0], 10, 64)
		if parseErr != nil {
			continue
		}
		leaseIDs = append(leaseIDs, parts[2])
		enqueuedAt = append(enqueuedAt, enqueued)
		requestIDs = append(requestIDs, parts[2])
	}
	if len(leaseIDs) == 0 {
		return []ChannelAdmissionWaitingUser{}, []ChannelAdmissionWaitingRequest{}, nil
	}
	values, err := client.HMGet(ctx, metaKey, leaseIDs...).Result()
	if err != nil {
		return nil, nil, err
	}
	byUser := make(map[int]*userAggregate)
	usernames := make(map[int]string)
	requests := make([]ChannelAdmissionWaitingRequest, 0, len(values))
	for index, value := range values {
		if value == nil || index >= len(enqueuedAt) {
			continue
		}
		parts := strings.SplitN(fmt.Sprint(value), "|", 3)
		userID, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil || userID <= 0 {
			continue
		}
		priority := 0
		if len(parts) > 1 {
			if parsedPriority, priorityErr := strconv.Atoi(parts[1]); priorityErr == nil {
				priority = parsedPriority
			}
		}
		wait := nowMs - enqueuedAt[index]
		if wait < 0 {
			wait = 0
		}
		position := index + 1
		username, known := usernames[userID]
		if !known {
			username = channelAdmissionUsername(userID)
			usernames[userID] = username
		}
		requests = append(requests, ChannelAdmissionWaitingRequest{
			ID:       requestIDs[index],
			UserID:   userID,
			Username: username,
			Priority: priority,
			Position: position,
			WaitMs:   wait,
		})
		aggregate := byUser[userID]
		if aggregate == nil {
			aggregate = &userAggregate{priority: priority, position: position}
			byUser[userID] = aggregate
		}
		aggregate.count++
		if position < aggregate.position {
			aggregate.position = position
		}
		if priority > aggregate.priority {
			aggregate.priority = priority
		}
		if wait > aggregate.oldest {
			aggregate.oldest = wait
		}
	}
	userIDs := make([]int, 0, len(byUser))
	for userID := range byUser {
		userIDs = append(userIDs, userID)
	}
	sort.Ints(userIDs)
	users := make([]ChannelAdmissionWaitingUser, 0, len(userIDs))
	for _, userID := range userIDs {
		aggregate := byUser[userID]
		entries := ChannelAdmissionWaitingUser{
			UserID:       userID,
			Username:     usernames[userID],
			Priority:     aggregate.priority,
			Position:     aggregate.position,
			Count:        aggregate.count,
			OldestWaitMs: aggregate.oldest,
		}
		users = append(users, entries)
	}
	sort.SliceStable(users, func(i, j int) bool {
		if users[i].Priority != users[j].Priority {
			return users[i].Priority > users[j].Priority
		}
		if users[i].OldestWaitMs == users[j].OldestWaitMs {
			return users[i].UserID < users[j].UserID
		}
		return users[i].OldestWaitMs > users[j].OldestWaitMs
	})
	return users, requests, nil
}

func channelAdmissionBucketSeconds(windowMs int64) int64 {
	switch {
	case windowMs <= 3*time.Hour.Milliseconds():
		return 60
	case windowMs <= 72*time.Hour.Milliseconds():
		return 300
	default:
		return 3600
	}
}

func channelAdmissionWaitedCount(counts map[string]int64) int64 {
	var waited int64
	for bucket, count := range counts {
		if strings.HasPrefix(bucket, "wait_") {
			waited += count
		}
	}
	waited -= counts["timeout"]
	return max(0, waited)
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
