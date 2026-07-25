package model

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PerfMetric stores aggregated relay performance metrics for the model square.
type PerfMetric struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	ModelName      string `json:"model_name" gorm:"size:128;uniqueIndex:idx_perf_model_group_bucket,priority:1"`
	Group          string `json:"group" gorm:"column:group;size:64;uniqueIndex:idx_perf_model_group_bucket,priority:2"`
	BucketTs       int64  `json:"bucket_ts" gorm:"uniqueIndex:idx_perf_model_group_bucket,priority:3;index:idx_perf_bucket_ts"`
	RequestCount   int64  `json:"-" gorm:"default:0"`
	SuccessCount   int64  `json:"-" gorm:"default:0"`
	TotalLatencyMs int64  `json:"-" gorm:"default:0"`
	TtftSumMs      int64  `json:"-" gorm:"default:0"`
	TtftCount      int64  `json:"-" gorm:"default:0"`
	OutputTokens   int64  `json:"-" gorm:"default:0"`
	GenerationMs   int64  `json:"-" gorm:"default:0"`
}

func (PerfMetric) TableName() string {
	return "perf_metrics"
}

// PerfMetricInstance stores the persisted contribution from one metrics
// writer. Keeping writer attribution lets status reads de-duplicate a writer's
// persisted counters from its in-flight Redis snapshot.
type PerfMetricInstance struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	WriterID       string `json:"writer_id" gorm:"size:64;uniqueIndex:idx_perf_instance_model_group_bucket_writer,priority:4"`
	ModelName      string `json:"model_name" gorm:"size:128;uniqueIndex:idx_perf_instance_model_group_bucket_writer,priority:1"`
	Group          string `json:"group" gorm:"column:group;size:64;uniqueIndex:idx_perf_instance_model_group_bucket_writer,priority:2"`
	BucketTs       int64  `json:"bucket_ts" gorm:"uniqueIndex:idx_perf_instance_model_group_bucket_writer,priority:3;index:idx_perf_instance_bucket_ts"`
	RequestCount   int64  `json:"-" gorm:"default:0"`
	SuccessCount   int64  `json:"-" gorm:"default:0"`
	TotalLatencyMs int64  `json:"-" gorm:"default:0"`
	TtftSumMs      int64  `json:"-" gorm:"default:0"`
	TtftCount      int64  `json:"-" gorm:"default:0"`
	OutputTokens   int64  `json:"-" gorm:"default:0"`
	GenerationMs   int64  `json:"-" gorm:"default:0"`
}

func (PerfMetricInstance) TableName() string {
	return "perf_metric_instances"
}

func UpsertPerfMetric(metric *PerfMetric) error {
	if metric == nil || metric.RequestCount == 0 {
		return nil
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "model_name"},
			{Name: "group"},
			{Name: "bucket_ts"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count":    gorm.Expr("perf_metrics.request_count + ?", metric.RequestCount),
			"success_count":    gorm.Expr("perf_metrics.success_count + ?", metric.SuccessCount),
			"total_latency_ms": gorm.Expr("perf_metrics.total_latency_ms + ?", metric.TotalLatencyMs),
			"ttft_sum_ms":      gorm.Expr("perf_metrics.ttft_sum_ms + ?", metric.TtftSumMs),
			"ttft_count":       gorm.Expr("perf_metrics.ttft_count + ?", metric.TtftCount),
			"output_tokens":    gorm.Expr("perf_metrics.output_tokens + ?", metric.OutputTokens),
			"generation_ms":    gorm.Expr("perf_metrics.generation_ms + ?", metric.GenerationMs),
		}),
	}).Create(metric).Error
}

func UpsertPerfMetricInstance(writerID string, metric *PerfMetric) error {
	if metric == nil || metric.RequestCount == 0 {
		return nil
	}
	if strings.TrimSpace(writerID) == "" {
		return errors.New("perf metric writer id is required")
	}

	instance := &PerfMetricInstance{
		WriterID:       writerID,
		ModelName:      metric.ModelName,
		Group:          metric.Group,
		BucketTs:       metric.BucketTs,
		RequestCount:   metric.RequestCount,
		SuccessCount:   metric.SuccessCount,
		TotalLatencyMs: metric.TotalLatencyMs,
		TtftSumMs:      metric.TtftSumMs,
		TtftCount:      metric.TtftCount,
		OutputTokens:   metric.OutputTokens,
		GenerationMs:   metric.GenerationMs,
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "model_name"},
			{Name: "group"},
			{Name: "bucket_ts"},
			{Name: "writer_id"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count":    gorm.Expr("perf_metric_instances.request_count + ?", metric.RequestCount),
			"success_count":    gorm.Expr("perf_metric_instances.success_count + ?", metric.SuccessCount),
			"total_latency_ms": gorm.Expr("perf_metric_instances.total_latency_ms + ?", metric.TotalLatencyMs),
			"ttft_sum_ms":      gorm.Expr("perf_metric_instances.ttft_sum_ms + ?", metric.TtftSumMs),
			"ttft_count":       gorm.Expr("perf_metric_instances.ttft_count + ?", metric.TtftCount),
			"output_tokens":    gorm.Expr("perf_metric_instances.output_tokens + ?", metric.OutputTokens),
			"generation_ms":    gorm.Expr("perf_metric_instances.generation_ms + ?", metric.GenerationMs),
		}),
	}).Create(instance).Error
}

func GetPerfMetrics(modelName string, group string, startTs int64, endTs int64) ([]PerfMetric, error) {
	var metrics []PerfMetric
	query := DB.Model(&PerfMetric{}).
		Where("model_name = ? AND bucket_ts >= ? AND bucket_ts <= ?", modelName, startTs, endTs)
	if group != "" {
		query = query.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: group})
	}
	if err := query.Order("bucket_ts ASC").Find(&metrics).Error; err != nil {
		return nil, err
	}

	var instances []PerfMetricInstance
	instanceQuery := DB.Model(&PerfMetricInstance{}).
		Where("model_name = ? AND bucket_ts >= ? AND bucket_ts <= ?", modelName, startTs, endTs)
	if group != "" {
		instanceQuery = instanceQuery.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: group})
	}
	if err := instanceQuery.Find(&instances).Error; err != nil {
		return nil, err
	}

	type metricKey struct {
		modelName string
		group     string
		bucketTs  int64
	}
	merged := make(map[metricKey]PerfMetric, len(metrics)+len(instances))
	for _, metric := range metrics {
		key := metricKey{modelName: metric.ModelName, group: metric.Group, bucketTs: metric.BucketTs}
		value := merged[key]
		if value.Id == 0 {
			value.Id = metric.Id
		}
		value.ModelName = metric.ModelName
		value.Group = metric.Group
		value.BucketTs = metric.BucketTs
		value.RequestCount += metric.RequestCount
		value.SuccessCount += metric.SuccessCount
		value.TotalLatencyMs += metric.TotalLatencyMs
		value.TtftSumMs += metric.TtftSumMs
		value.TtftCount += metric.TtftCount
		value.OutputTokens += metric.OutputTokens
		value.GenerationMs += metric.GenerationMs
		merged[key] = value
	}
	for _, instance := range instances {
		key := metricKey{modelName: instance.ModelName, group: instance.Group, bucketTs: instance.BucketTs}
		value := merged[key]
		value.ModelName = instance.ModelName
		value.Group = instance.Group
		value.BucketTs = instance.BucketTs
		value.RequestCount += instance.RequestCount
		value.SuccessCount += instance.SuccessCount
		value.TotalLatencyMs += instance.TotalLatencyMs
		value.TtftSumMs += instance.TtftSumMs
		value.TtftCount += instance.TtftCount
		value.OutputTokens += instance.OutputTokens
		value.GenerationMs += instance.GenerationMs
		merged[key] = value
	}

	metrics = make([]PerfMetric, 0, len(merged))
	for _, metric := range merged {
		metrics = append(metrics, metric)
	}
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].BucketTs != metrics[j].BucketTs {
			return metrics[i].BucketTs < metrics[j].BucketTs
		}
		if metrics[i].ModelName != metrics[j].ModelName {
			return metrics[i].ModelName < metrics[j].ModelName
		}
		return metrics[i].Group < metrics[j].Group
	})
	return metrics, nil
}

type PerfMetricSummary struct {
	ModelName      string `json:"model_name"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	OutputTokens   int64  `json:"output_tokens"`
	GenerationMs   int64  `json:"generation_ms"`
}

type PerfMetricSummaryBucket struct {
	ModelName      string `json:"model_name"`
	BucketTs       int64  `json:"bucket_ts"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	TtftSumMs      int64  `json:"ttft_sum_ms"`
	TtftCount      int64  `json:"ttft_count"`
	OutputTokens   int64  `json:"output_tokens"`
	GenerationMs   int64  `json:"generation_ms"`
}

func GetPerfMetricsSummaryAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummary, error) {
	var summaries []PerfMetricSummary
	if groups != nil && len(groups) == 0 {
		return summaries, nil
	}
	query := DB.Model(&PerfMetric{}).
		Select("model_name, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		query = query.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: groups})
	}
	if err := query.
		Group("model_name").
		Having("SUM(request_count) > 0").
		Find(&summaries).Error; err != nil {
		return nil, err
	}

	var instanceSummaries []PerfMetricSummary
	instanceQuery := DB.Model(&PerfMetricInstance{}).
		Select("model_name, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		instanceQuery = instanceQuery.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: groups})
	}
	if err := instanceQuery.
		Group("model_name").
		Having("SUM(request_count) > 0").
		Find(&instanceSummaries).Error; err != nil {
		return nil, err
	}

	merged := make(map[string]PerfMetricSummary, len(summaries)+len(instanceSummaries))
	for _, summary := range summaries {
		merged[summary.ModelName] = summary
	}
	for _, summary := range instanceSummaries {
		value := merged[summary.ModelName]
		value.ModelName = summary.ModelName
		value.RequestCount += summary.RequestCount
		value.SuccessCount += summary.SuccessCount
		value.TotalLatencyMs += summary.TotalLatencyMs
		value.OutputTokens += summary.OutputTokens
		value.GenerationMs += summary.GenerationMs
		merged[summary.ModelName] = value
	}
	summaries = make([]PerfMetricSummary, 0, len(merged))
	for _, summary := range merged {
		if summary.RequestCount > 0 {
			summaries = append(summaries, summary)
		}
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ModelName < summaries[j].ModelName
	})
	return summaries, nil
}

func GetPerfMetricsSummaryBucketsAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummaryBucket, error) {
	var summaries []PerfMetricSummaryBucket
	if groups != nil && len(groups) == 0 {
		return summaries, nil
	}
	query := DB.Model(&PerfMetric{}).
		Select("model_name, bucket_ts, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		query = query.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: groups})
	}
	if err := query.
		Group("model_name, bucket_ts").
		Having("SUM(request_count) > 0").
		Order("bucket_ts ASC").
		Find(&summaries).Error; err != nil {
		return nil, err
	}

	var instanceSummaries []PerfMetricSummaryBucket
	instanceQuery := DB.Model(&PerfMetricInstance{}).
		Select("model_name, bucket_ts, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		instanceQuery = instanceQuery.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: groups})
	}
	if err := instanceQuery.
		Group("model_name, bucket_ts").
		Having("SUM(request_count) > 0").
		Find(&instanceSummaries).Error; err != nil {
		return nil, err
	}

	type summaryBucketKey struct {
		modelName string
		bucketTs  int64
	}
	merged := make(map[summaryBucketKey]PerfMetricSummaryBucket, len(summaries)+len(instanceSummaries))
	for _, summary := range summaries {
		merged[summaryBucketKey{modelName: summary.ModelName, bucketTs: summary.BucketTs}] = summary
	}
	for _, summary := range instanceSummaries {
		key := summaryBucketKey{modelName: summary.ModelName, bucketTs: summary.BucketTs}
		value := merged[key]
		value.ModelName = summary.ModelName
		value.BucketTs = summary.BucketTs
		value.RequestCount += summary.RequestCount
		value.SuccessCount += summary.SuccessCount
		value.TotalLatencyMs += summary.TotalLatencyMs
		value.OutputTokens += summary.OutputTokens
		value.GenerationMs += summary.GenerationMs
		merged[key] = value
	}
	summaries = make([]PerfMetricSummaryBucket, 0, len(merged))
	for _, summary := range merged {
		if summary.RequestCount > 0 {
			summaries = append(summaries, summary)
		}
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].BucketTs != summaries[j].BucketTs {
			return summaries[i].BucketTs < summaries[j].BucketTs
		}
		return summaries[i].ModelName < summaries[j].ModelName
	})
	return summaries, nil
}

func GetPerfMetricsHourlySummaryBucketsForModels(startTs int64, endTs int64, excludedBucketTs int64, groups []string, modelNames []string) ([]PerfMetricSummaryBucket, error) {
	var summaries []PerfMetricSummaryBucket
	// Both arguments are allowlists for the public status view. An omitted or
	// blank-only allowlist must not broaden the query to every stored row.
	normalizedModels := normalizePerfMetricAllowlist(modelNames)
	normalizedGroups := normalizePerfMetricAllowlist(groups)
	if len(normalizedModels) == 0 || len(normalizedGroups) == 0 {
		return summaries, nil
	}
	modelNames = normalizedModels
	groups = normalizedGroups

	hourBucketExpression := "(bucket_ts / 3600) * 3600"
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		hourBucketExpression = "FLOOR(bucket_ts / 3600) * 3600"
	}
	query := DB.Model(&PerfMetric{}).
		Select("model_name, "+hourBucketExpression+" as bucket_ts, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(ttft_sum_ms) as ttft_sum_ms, SUM(ttft_count) as ttft_count, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("model_name IN ? AND bucket_ts >= ? AND bucket_ts <= ? AND bucket_ts <> ?", modelNames, startTs, endTs, excludedBucketTs)
	query = query.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: groups})
	err := query.
		Group("model_name, " + hourBucketExpression).
		Having("SUM(request_count) > 0").
		Order("bucket_ts ASC").
		Find(&summaries).Error
	return summaries, err
}

// GetPerfMetricInstancesForModels returns exact per-writer bucket rows for the
// status merger. Both filters are required allowlists: an omitted or blank-only
// filter intentionally returns no data instead of widening the query.
func GetPerfMetricInstancesForModels(startTs int64, endTs int64, groups []string, modelNames []string) ([]PerfMetricInstance, error) {
	var instances []PerfMetricInstance
	modelNames = normalizePerfMetricAllowlist(modelNames)
	groups = normalizePerfMetricAllowlist(groups)
	if len(modelNames) == 0 || len(groups) == 0 {
		return instances, nil
	}

	err := DB.Model(&PerfMetricInstance{}).
		Where("model_name IN ? AND bucket_ts >= ? AND bucket_ts <= ?", modelNames, startTs, endTs).
		Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: groups}).
		Find(&instances).Error
	if err != nil {
		return nil, err
	}
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].BucketTs != instances[j].BucketTs {
			return instances[i].BucketTs < instances[j].BucketTs
		}
		if instances[i].ModelName != instances[j].ModelName {
			return instances[i].ModelName < instances[j].ModelName
		}
		if instances[i].Group != instances[j].Group {
			return instances[i].Group < instances[j].Group
		}
		return instances[i].WriterID < instances[j].WriterID
	})
	return instances, nil
}

func normalizePerfMetricAllowlist(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func DeletePerfMetricsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("bucket_ts < ?", cutoffTs).Delete(&PerfMetricInstance{}).Error; err != nil {
			return err
		}
		return tx.Where("bucket_ts < ?", cutoffTs).Delete(&PerfMetric{}).Error
	})
}

func PerfMetricStartTime(hours int) int64 {
	if hours <= 0 {
		hours = 24
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}
