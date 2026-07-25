package perfmetrics

import (
	"crypto/sha256"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	statusWindowHours = 24
	statusCacheTTL    = 20 * time.Second
)

type statusCacheEntry struct {
	expiresAt time.Time
	result    StatusResult
}

type statusCacheCall struct {
	done   chan struct{}
	result StatusResult
	err    error
}

type statusQueryLoader func([]StatusModelSource, []string, time.Time) (StatusResult, error)

var statusResultCache = struct {
	sync.Mutex
	entries map[[sha256.Size]byte]statusCacheEntry
	calls   map[[sha256.Size]byte]*statusCacheCall
}{
	entries: make(map[[sha256.Size]byte]statusCacheEntry),
	calls:   make(map[[sha256.Size]byte]*statusCacheCall),
}

func QueryStatus(models []StatusModelSource, groups []string) (StatusResult, error) {
	return queryStatusCachedAt(models, groups, time.Now(), queryStatusAt)
}

func queryStatusCachedAt(models []StatusModelSource, groups []string, now time.Time, load statusQueryLoader) (StatusResult, error) {
	models, groups, cacheKey := statusCacheInputs(models, groups)

	statusResultCache.Lock()
	if entry, ok := statusResultCache.entries[cacheKey]; ok && now.Before(entry.expiresAt) {
		statusResultCache.Unlock()
		return cloneStatusResult(entry.result), nil
	}
	if call, ok := statusResultCache.calls[cacheKey]; ok {
		statusResultCache.Unlock()
		<-call.done
		if call.err != nil {
			return StatusResult{}, call.err
		}
		return cloneStatusResult(call.result), nil
	}
	call := &statusCacheCall{done: make(chan struct{})}
	statusResultCache.calls[cacheKey] = call
	statusResultCache.Unlock()

	result, err := load(models, groups, now)

	statusResultCache.Lock()
	if err == nil {
		for key, entry := range statusResultCache.entries {
			if !now.Before(entry.expiresAt) {
				delete(statusResultCache.entries, key)
			}
		}
		statusResultCache.entries[cacheKey] = statusCacheEntry{
			expiresAt: now.Add(statusCacheTTL),
			result:    result,
		}
	}
	call.result = result
	call.err = err
	delete(statusResultCache.calls, cacheKey)
	close(call.done)
	statusResultCache.Unlock()

	if err != nil {
		return StatusResult{}, err
	}
	return cloneStatusResult(result), nil
}

func statusCacheInputs(models []StatusModelSource, groups []string) ([]StatusModelSource, []string, [sha256.Size]byte) {
	seenModels := make(map[string]struct{}, len(models))
	canonicalModels := make([]StatusModelSource, 0, len(models))
	for _, item := range models {
		if strings.TrimSpace(item.ModelName) == "" {
			continue
		}
		if _, exists := seenModels[item.ModelName]; exists {
			continue
		}
		seenModels[item.ModelName] = struct{}{}
		canonicalModels = append(canonicalModels, item)
	}
	sort.Slice(canonicalModels, func(i, j int) bool {
		return canonicalModels[i].ModelName < canonicalModels[j].ModelName
	})

	seenGroups := make(map[string]struct{}, len(groups))
	canonicalGroups := make([]string, 0, len(groups))
	for _, group := range groups {
		if strings.TrimSpace(group) == "" {
			continue
		}
		if _, exists := seenGroups[group]; exists {
			continue
		}
		seenGroups[group] = struct{}{}
		canonicalGroups = append(canonicalGroups, group)
	}
	sort.Strings(canonicalGroups)

	cacheKeyData := make([]byte, 0, len(canonicalModels)*32+len(canonicalGroups)*16)
	cacheKeyData = append(cacheKeyData, 'm', ':')
	for _, item := range canonicalModels {
		cacheKeyData = strconv.AppendInt(cacheKeyData, int64(len(item.ModelName)), 10)
		cacheKeyData = append(cacheKeyData, ':')
		cacheKeyData = append(cacheKeyData, item.ModelName...)
		cacheKeyData = strconv.AppendInt(cacheKeyData, int64(len(item.Vendor)), 10)
		cacheKeyData = append(cacheKeyData, ':')
		cacheKeyData = append(cacheKeyData, item.Vendor...)
		cacheKeyData = strconv.AppendInt(cacheKeyData, int64(len(item.Icon)), 10)
		cacheKeyData = append(cacheKeyData, ':')
		cacheKeyData = append(cacheKeyData, item.Icon...)
	}
	cacheKeyData = append(cacheKeyData, 'g', ':')
	for _, group := range canonicalGroups {
		cacheKeyData = strconv.AppendInt(cacheKeyData, int64(len(group)), 10)
		cacheKeyData = append(cacheKeyData, ':')
		cacheKeyData = append(cacheKeyData, group...)
	}
	return canonicalModels, canonicalGroups, sha256.Sum256(cacheKeyData)
}

func cloneStatusResult(result StatusResult) StatusResult {
	cloned := result
	if result.Models == nil {
		return cloned
	}
	cloned.Models = make([]ModelStatus, len(result.Models))
	for i, item := range result.Models {
		cloned.Models[i] = item
		if item.SuccessRate != nil {
			value := *item.SuccessRate
			cloned.Models[i].SuccessRate = &value
		}
		if item.AvgTtftMs != nil {
			value := *item.AvgTtftMs
			cloned.Models[i].AvgTtftMs = &value
		}
		if item.AvgLatencyMs != nil {
			value := *item.AvgLatencyMs
			cloned.Models[i].AvgLatencyMs = &value
		}
		if item.AvgTps != nil {
			value := *item.AvgTps
			cloned.Models[i].AvgTps = &value
		}
		if item.Timeline == nil {
			continue
		}
		cloned.Models[i].Timeline = make([]StatusPoint, len(item.Timeline))
		copy(cloned.Models[i].Timeline, item.Timeline)
		for j, point := range item.Timeline {
			if point.SuccessRate == nil {
				cloned.Models[i].Timeline[j].SuccessRate = nil
			} else {
				value := *point.SuccessRate
				cloned.Models[i].Timeline[j].SuccessRate = &value
			}
			if point.AvgTtftMs != nil {
				value := *point.AvgTtftMs
				cloned.Models[i].Timeline[j].AvgTtftMs = &value
			}
			if point.AvgLatencyMs != nil {
				value := *point.AvgLatencyMs
				cloned.Models[i].Timeline[j].AvgLatencyMs = &value
			}
			if point.AvgTps != nil {
				value := *point.AvgTps
				cloned.Models[i].Timeline[j].AvgTps = &value
			}
		}
	}
	return cloned
}

func queryStatusAt(models []StatusModelSource, groups []string, now time.Time) (StatusResult, error) {
	generatedAt := now.Unix()
	currentHour := hourStart(generatedAt)
	startHour := currentHour - int64(statusWindowHours-1)*3600
	activeBucketTs := bucketStart(generatedAt)
	result := StatusResult{
		GeneratedAt: generatedAt,
		WindowHours: statusWindowHours,
		Models:      make([]ModelStatus, 0, len(models)),
	}
	visibleModels := make(map[string]struct{}, len(models))
	modelNames := make([]string, 0, len(models))
	uniqueModels := make([]StatusModelSource, 0, len(models))
	for _, item := range models {
		if strings.TrimSpace(item.ModelName) == "" {
			continue
		}
		if _, exists := visibleModels[item.ModelName]; exists {
			continue
		}
		visibleModels[item.ModelName] = struct{}{}
		modelNames = append(modelNames, item.ModelName)
		uniqueModels = append(uniqueModels, item)
	}
	if len(uniqueModels) == 0 {
		return result, nil
	}

	// Status aggregation always uses an explicit group allowlist. Treat nil,
	// empty, and blank-only inputs alike so DB, hot, and Redis layers cannot
	// disagree about whether an omitted allowlist means "all" or "none".
	uniqueGroups := make([]string, 0, len(groups))
	seenGroups := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if strings.TrimSpace(group) == "" {
			continue
		}
		if _, exists := seenGroups[group]; exists {
			continue
		}
		seenGroups[group] = struct{}{}
		uniqueGroups = append(uniqueGroups, group)
	}
	groups = uniqueGroups

	// Keep the read lock across the database read and hot-bucket snapshot.
	// Otherwise a flush could drain a completed bucket after the DB snapshot
	// but before the hot snapshot, making that bucket disappear from this read.
	bucketFlushMu.RLock()
	rows, err := model.GetPerfMetricsHourlySummaryBucketsForModels(startHour, generatedAt, activeBucketTs, groups, modelNames)
	if err != nil {
		bucketFlushMu.RUnlock()
		return StatusResult{}, err
	}
	instanceRows, err := model.GetPerfMetricInstancesForModels(startHour, generatedAt, groups, modelNames)
	if err != nil {
		bucketFlushMu.RUnlock()
		return StatusResult{}, err
	}
	modelBuckets := make(map[string]map[int64]counters, len(models))
	for _, row := range rows {
		if _, ok := visibleModels[row.ModelName]; !ok {
			continue
		}
		mergeModelBucket(modelBuckets, row.ModelName, hourStart(row.BucketTs), counters{
			requestCount:   row.RequestCount,
			successCount:   row.SuccessCount,
			totalLatencyMs: row.TotalLatencyMs,
			ttftSumMs:      row.TtftSumMs,
			ttftCount:      row.TtftCount,
			outputTokens:   row.OutputTokens,
			generationMs:   row.GenerationMs,
		})
	}
	dbInstanceBuckets := make(statusInstanceBuckets)
	for _, row := range instanceRows {
		key := bucketKey{model: row.ModelName, group: row.Group, bucketTs: row.BucketTs}
		if dbInstanceBuckets[key] == nil {
			dbInstanceBuckets[key] = make(map[string]counters)
		}
		dbInstanceBuckets[key][row.WriterID] = counters{
			requestCount:   row.RequestCount,
			successCount:   row.SuccessCount,
			totalLatencyMs: row.TotalLatencyMs,
			ttftSumMs:      row.TtftSumMs,
			ttftCount:      row.TtftCount,
			outputTokens:   row.OutputTokens,
			generationMs:   row.GenerationMs,
		}
	}

	allowedGroups := allowedGroupSet(groups)
	localBuckets := make(map[bucketKey]counters)
	hotBuckets.Range(func(key, value any) bool {
		bucket := key.(bucketKey)
		if bucket.bucketTs < startHour || bucket.bucketTs > generatedAt {
			return true
		}
		if _, ok := visibleModels[bucket.model]; !ok {
			return true
		}
		if allowedGroups != nil {
			if _, ok := allowedGroups[bucket.group]; !ok {
				return true
			}
		}
		local := value.(*atomicBucket).totalSnapshot()
		if local.requestCount > 0 {
			localBuckets[bucket] = local
		}
		return true
	})
	bucketFlushMu.RUnlock()

	// Database, Redis, and local buckets are cumulative per-writer snapshots.
	// Selecting the most complete snapshot for each writer makes a DB commit and
	// its Redis publication order-independent without double-counting.
	recentBuckets := mergeStatusBuckets(
		dbInstanceBuckets,
		getStatusRedisBuckets(modelNames, groups, startHour, activeBucketTs),
		localBuckets,
	)
	for key, value := range recentBuckets {
		mergeModelBucket(modelBuckets, key.model, hourStart(key.bucketTs), value)
	}

	for _, item := range uniqueModels {
		buckets := modelBuckets[item.ModelName]
		total := counters{}
		timeline := make([]StatusPoint, 0, statusWindowHours)
		for hour := startHour; hour <= currentHour; hour += 3600 {
			value := buckets[hour]
			total.requestCount += value.requestCount
			total.successCount += value.successCount
			total.totalLatencyMs += value.totalLatencyMs
			total.ttftSumMs += value.ttftSumMs
			total.ttftCount += value.ttftCount
			total.outputTokens += value.outputTokens
			total.generationMs += value.generationMs
			point := StatusPoint{
				Ts:           hour,
				Status:       classifyStatus(value),
				RequestCount: value.requestCount,
				SuccessCount: value.successCount,
				SuccessRate:  statusSuccessRate(value),
			}
			if value.requestCount > 0 {
				avgLatencyMs := avg(value.totalLatencyMs, value.requestCount)
				avgThroughput := rounded(avgTps(value))
				point.AvgLatencyMs = &avgLatencyMs
				point.AvgTps = &avgThroughput
			}
			if value.ttftCount > 0 {
				avgTtftMs := avg(value.ttftSumMs, value.ttftCount)
				point.AvgTtftMs = &avgTtftMs
			}
			timeline = append(timeline, point)
		}

		modelStatus := ModelStatus{
			ModelName:    item.ModelName,
			Vendor:       item.Vendor,
			Icon:         item.Icon,
			RequestCount: total.requestCount,
			SuccessCount: total.successCount,
			Status:       classifyStatus(total),
			Timeline:     timeline,
		}
		if total.requestCount > 0 {
			successRate := rounded(statusSuccessRateValue(total))
			avgLatencyMs := avg(total.totalLatencyMs, total.requestCount)
			avgThroughput := rounded(avgTps(total))
			modelStatus.SuccessRate = &successRate
			modelStatus.AvgLatencyMs = &avgLatencyMs
			modelStatus.AvgTps = &avgThroughput
		}
		if total.ttftCount > 0 {
			avgTtftMs := avg(total.ttftSumMs, total.ttftCount)
			modelStatus.AvgTtftMs = &avgTtftMs
		}
		result.Models = append(result.Models, modelStatus)
	}

	sort.SliceStable(result.Models, func(i, j int) bool {
		leftPriority := statusPriority(result.Models[i].Status)
		rightPriority := statusPriority(result.Models[j].Status)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return result.Models[i].ModelName < result.Models[j].ModelName
	})
	return result, nil
}

func hourStart(ts int64) int64 {
	return ts - ts%3600
}

// Success-rate thresholds shared with the model catalog ("模型广场") success
// rate grading in web/src/features/performance-metrics/lib/format.ts, so the
// same model never renders green in one page and amber/red in the other.
const (
	statusOperationalMinRate = 90.0
	statusDegradedMinRate    = 70.0
)

func classifyStatus(value counters) Status {
	if value.requestCount <= 0 {
		return StatusNoData
	}
	rate := statusSuccessRateValue(value)
	if rate >= statusOperationalMinRate {
		return StatusOperational
	}
	if rate >= statusDegradedMinRate {
		return StatusDegraded
	}
	return StatusFailed
}

func statusSuccessRate(value counters) *float64 {
	if value.requestCount <= 0 {
		return nil
	}
	rate := rounded(statusSuccessRateValue(value))
	return &rate
}

func statusSuccessRateValue(value counters) float64 {
	if value.requestCount <= 0 || value.successCount <= 0 {
		return 0
	}
	successCount := min(value.successCount, value.requestCount)
	return float64(successCount) / float64(value.requestCount) * 100
}

func rounded(value float64) float64 {
	return math.Round(value*100) / 100
}

func statusPriority(status Status) int {
	switch status {
	case StatusFailed:
		return 0
	case StatusDegraded:
		return 1
	case StatusOperational:
		return 2
	default:
		return 3
	}
}
