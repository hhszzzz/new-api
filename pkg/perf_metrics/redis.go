package perfmetrics

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const (
	redisPublishInterval = time.Second
	redisPublishTimeout  = time.Second
	redisBucketTTL       = 25 * time.Hour
	redisBucketIndexKey  = "perf:v2:index"
)

var (
	redisWriterID = uuid.NewString()

	redisDirtyBuckets  sync.Map
	redisPublishWake   = make(chan struct{}, 1)
	redisPublisherOnce sync.Once
)

type redisBucketValues struct {
	legacy    counters
	instances map[string]counters
}

type statusInstanceBuckets map[bucketKey]map[string]counters

func startRedisPublisher() {
	redisPublisherOnce.Do(func() {
		go redisPublishLoop()
	})
}

func markRedisBucketDirty(key bucketKey, bucket *atomicBucket) {
	if bucket == nil || !common.RedisEnabled || common.RDB == nil {
		return
	}
	redisDirtyBuckets.Store(key, bucket)
	select {
	case redisPublishWake <- struct{}{}:
	default:
	}
}

func redisPublishLoop() {
	ticker := time.NewTicker(redisPublishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-redisPublishWake:
		case <-ticker.C:
		}
		publishRedisDirtyBuckets()
	}
}

// publishRedisDirtyBuckets publishes absolute per-process snapshots. Absolute
// counters make retries idempotent, while the per-process field namespace lets
// a status reader replace only its own Redis snapshot with a newer local one.
func publishRedisDirtyBuckets() {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	// A publisher snapshot must not race a completed-bucket drain. The flusher
	// holds the write side through the database upsert, so publication observes
	// either side of that complete transition.
	bucketFlushMu.RLock()
	defer bucketFlushMu.RUnlock()

	type dirtyBucket struct {
		key    bucketKey
		bucket *atomicBucket
		value  counters
	}
	dirty := make([]dirtyBucket, 0)
	redisDirtyBuckets.Range(func(rawKey, rawBucket any) bool {
		key := rawKey.(bucketKey)
		bucket := rawBucket.(*atomicBucket)
		if _, loaded := redisDirtyBuckets.LoadAndDelete(key); !loaded {
			return true
		}
		value := bucket.totalSnapshot()
		if value.requestCount > 0 {
			dirty = append(dirty, dirtyBucket{key: key, bucket: bucket, value: value})
		}
		return true
	})
	if len(dirty) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisPublishTimeout)
	defer cancel()
	pipe := common.RDB.TxPipeline()
	defer pipe.Close()
	for _, item := range dirty {
		redisKey := redisInstanceBucketKey(item.key)
		pipe.HSet(ctx, redisKey, redisInstanceFields(redisWriterID, item.value))
		pipe.Expire(ctx, redisKey, redisBucketTTL)
		pipe.ZAdd(ctx, redisBucketIndexKey, &redis.Z{Score: float64(item.key.bucketTs), Member: redisKey})
	}
	indexCutoff := time.Now().Add(-redisBucketTTL).Unix()
	pipe.ZRemRangeByScore(ctx, redisBucketIndexKey, "-inf", strconv.FormatInt(indexCutoff, 10))
	pipe.Expire(ctx, redisBucketIndexKey, redisBucketTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		for _, item := range dirty {
			redisDirtyBuckets.Store(item.key, item.bucket)
		}
	}
}

func redisInstanceFields(instanceID string, value counters) map[string]interface{} {
	return map[string]interface{}{
		redisInstanceField(instanceID, "req"):    value.requestCount,
		redisInstanceField(instanceID, "ok"):     value.successCount,
		redisInstanceField(instanceID, "lat"):    value.totalLatencyMs,
		redisInstanceField(instanceID, "ttft"):   value.ttftSumMs,
		redisInstanceField(instanceID, "ttft_n"): value.ttftCount,
		redisInstanceField(instanceID, "out"):    value.outputTokens,
		redisInstanceField(instanceID, "gen_ms"): value.generationMs,
	}
}

func redisInstanceField(instanceID string, metric string) string {
	return instanceID + ":" + metric
}

func getStatusRedisBuckets(modelNames []string, groups []string, startBucketTs int64, activeBucketTs int64) map[bucketKey]redisBucketValues {
	result := make(map[bucketKey]redisBucketValues)
	if !common.RedisEnabled || common.RDB == nil || len(modelNames) == 0 || len(groups) == 0 {
		return result
	}
	firstBucketTs := startBucketTs
	ttlStartTs := activeBucketTs - int64(redisBucketTTL/time.Second)
	if ttlStartTs > firstBucketTs {
		firstBucketTs = ttlStartTs
	}
	if firstBucketTs > activeBucketTs {
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisPublishTimeout)
	defer cancel()
	indexedKeys, _ := common.RDB.ZRangeByScore(ctx, redisBucketIndexKey, &redis.ZRangeBy{
		Min: strconv.FormatInt(firstBucketTs, 10),
		Max: strconv.FormatInt(activeBucketTs, 10),
	}).Result()
	pipe := common.RDB.Pipeline()
	defer pipe.Close()
	type redisBucketCommand struct {
		key       bucketKey
		legacy    *redis.StringStringMapCmd
		instances *redis.StringStringMapCmd
	}
	commands := make([]redisBucketCommand, 0, len(modelNames)*len(groups)+len(indexedKeys))
	scheduled := make(map[bucketKey]struct{}, cap(commands))
	allowedModels := make(map[string]struct{}, len(modelNames))
	for _, modelName := range modelNames {
		allowedModels[modelName] = struct{}{}
	}
	allowedGroups := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		allowedGroups[group] = struct{}{}
	}

	// Always address the active bucket directly so rolling deployments do not
	// depend on the v2 completed-bucket index already being populated.
	for _, modelName := range modelNames {
		for _, group := range groups {
			key := bucketKey{model: modelName, group: group, bucketTs: activeBucketTs}
			if _, ok := scheduled[key]; ok {
				continue
			}
			scheduled[key] = struct{}{}
			commands = append(commands, redisBucketCommand{
				key:       key,
				legacy:    pipe.HGetAll(ctx, redisBucketKey(key)),
				instances: pipe.HGetAll(ctx, redisInstanceBucketKey(key)),
			})
		}
	}
	for _, redisKey := range indexedKeys {
		key, ok := parseRedisInstanceBucketKey(redisKey)
		if !ok || key.bucketTs >= activeBucketTs || key.bucketTs < firstBucketTs {
			continue
		}
		if _, ok := allowedModels[key.model]; !ok {
			continue
		}
		if _, ok := allowedGroups[key.group]; !ok {
			continue
		}
		if _, ok := scheduled[key]; ok {
			continue
		}
		scheduled[key] = struct{}{}
		commands = append(commands, redisBucketCommand{
			key:       key,
			instances: pipe.HGetAll(ctx, redisKey),
		})
	}
	_, _ = pipe.Exec(ctx)
	for _, item := range commands {
		value := redisBucketValues{}
		if item.legacy != nil && item.legacy.Err() == nil {
			value.legacy = redisCounters(item.legacy.Val())
		}
		if item.instances.Err() == nil {
			value.instances = redisInstanceCounters(item.instances.Val())
		}
		if value.legacy.requestCount <= 0 && len(value.instances) == 0 {
			continue
		}
		result[item.key] = value
	}
	return result
}

func mergeStatusBuckets(dbBuckets statusInstanceBuckets, redisBuckets map[bucketKey]redisBucketValues, localBuckets map[bucketKey]counters) map[bucketKey]counters {
	merged := make(map[bucketKey]counters, len(dbBuckets)+len(redisBuckets)+len(localBuckets))
	selected := make(statusInstanceBuckets, len(dbBuckets)+len(redisBuckets)+len(localBuckets))
	for key, instances := range dbBuckets {
		for writerID, value := range instances {
			selectMostCompleteInstance(selected, key, writerID, value)
		}
	}
	for key, redisValue := range redisBuckets {
		mergeCounters(merged, key, redisValue.legacy)
		for writerID, value := range redisValue.instances {
			selectMostCompleteInstance(selected, key, writerID, value)
		}
	}
	for key, local := range localBuckets {
		selectMostCompleteInstance(selected, key, redisWriterID, local)
	}
	for key, instances := range selected {
		for _, value := range instances {
			mergeCounters(merged, key, value)
		}
	}
	return merged
}

func selectMostCompleteInstance(selected statusInstanceBuckets, key bucketKey, writerID string, value counters) {
	if value.requestCount <= 0 || writerID == "" {
		return
	}
	instances := selected[key]
	if instances == nil {
		instances = make(map[string]counters)
		selected[key] = instances
	}
	current, exists := instances[writerID]
	if !exists || value.requestCount > current.requestCount {
		instances[writerID] = value
	}
}

func redisCounters(values map[string]string) counters {
	return counters{
		requestCount:   parseRedisInt(values["req"]),
		successCount:   parseRedisInt(values["ok"]),
		totalLatencyMs: parseRedisInt(values["lat"]),
		ttftSumMs:      parseRedisInt(values["ttft"]),
		ttftCount:      parseRedisInt(values["ttft_n"]),
		outputTokens:   parseRedisInt(values["out"]),
		generationMs:   parseRedisInt(values["gen_ms"]),
	}
}

func redisInstanceCounters(values map[string]string) map[string]counters {
	instances := make(map[string]map[string]string)
	for field, value := range values {
		instanceID, metric, ok := strings.Cut(field, ":")
		if !ok || instanceID == "" || metric == "" {
			continue
		}
		if _, ok := instances[instanceID]; !ok {
			instances[instanceID] = make(map[string]string)
		}
		instances[instanceID][metric] = value
	}

	result := make(map[string]counters, len(instances))
	for instanceID, fields := range instances {
		value := redisCounters(fields)
		if value.requestCount > 0 {
			result[instanceID] = value
		}
	}
	return result
}

func parseRedisInt(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

// redisBucketKey is the legacy shared-counter key. It remains readable during
// rolling upgrades, but new processes write only per-process v2 snapshots.
func redisBucketKey(key bucketKey) string {
	return fmt.Sprintf("perf:%s:%s:%d", key.model, key.group, key.bucketTs)
}

func redisInstanceBucketKey(key bucketKey) string {
	modelName := base64.RawURLEncoding.EncodeToString([]byte(key.model))
	group := base64.RawURLEncoding.EncodeToString([]byte(key.group))
	return fmt.Sprintf("perf:v2:%s:%s:%d", modelName, group, key.bucketTs)
}

func parseRedisInstanceBucketKey(redisKey string) (bucketKey, bool) {
	parts := strings.Split(redisKey, ":")
	if len(parts) != 5 || parts[0] != "perf" || parts[1] != "v2" {
		return bucketKey{}, false
	}
	modelName, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return bucketKey{}, false
	}
	group, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return bucketKey{}, false
	}
	bucketTs, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return bucketKey{}, false
	}
	return bucketKey{model: string(modelName), group: string(group), bucketTs: bucketTs}, true
}
