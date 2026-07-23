package service

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	rankingCacheTTL         = 5 * time.Minute
	rankingCacheMaxEntries  = 256
	rankingLeaderboardLimit = 20
	rankingHistoryLimit     = 10
	rankingVendorLimit      = 5
	rankingMoverLimit       = 6
	rankingUserLimit        = 20
	rankingOthersLabel      = "Others"
	rankingUnknownVendor    = "Unknown"
	rankingOtherUsersLabel  = "Other users"
	rankingUnknownGroup     = "Unknown"
	rankingMaxCustomDays    = 366
)

const (
	RankingViewerAnonymous RankingViewer = "anonymous"
	RankingViewerUser      RankingViewer = "user"
	RankingViewerAdmin     RankingViewer = "admin"
)

type RankingViewer string

// RankingsRequest describes the dimensions that affect a rankings response.
// Timestamps are Unix seconds and are only required for the custom period.
type RankingsRequest struct {
	Period         string
	StartTimestamp *int64
	EndTimestamp   *int64
	VisibleModels  []string
	Viewer         RankingViewer
}

// RankingsOptions is kept as a descriptive alias for callers that build
// rankings queries outside the controller package.
type RankingsOptions = RankingsRequest

type RankingsResponse struct {
	Range              RankingRange       `json:"range"`
	TotalTokens        int64              `json:"total_tokens"`
	TotalQuota         int64              `json:"total_quota"`
	TotalUSD           float64            `json:"total_usd"`
	Models             []RankedModel      `json:"models"`
	Vendors            []RankedVendor     `json:"vendors"`
	TopMovers          []RankingMover     `json:"top_movers"`
	TopDroppers        []RankingMover     `json:"top_droppers"`
	ModelsHistory      ModelHistorySeries `json:"models_history"`
	VendorShareHistory VendorShareSeries  `json:"vendor_share_history"`
	UserUsage          *RankingUserUsage  `json:"user_usage,omitempty"`
}

type RankingRange struct {
	Period                 string `json:"period"`
	StartTimestamp         int64  `json:"start_timestamp"`
	EndTimestamp           int64  `json:"end_timestamp"`
	PreviousStartTimestamp int64  `json:"previous_start_timestamp"`
	PreviousEndTimestamp   int64  `json:"previous_end_timestamp"`
	Bucket                 string `json:"bucket"`
	BucketSeconds          int64  `json:"bucket_seconds"`
}

type RankedModel struct {
	Rank         int     `json:"rank"`
	PreviousRank *int    `json:"previous_rank,omitempty"`
	ModelName    string  `json:"model_name"`
	Vendor       string  `json:"vendor"`
	VendorIcon   string  `json:"vendor_icon,omitempty"`
	Category     string  `json:"category"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalQuota   int64   `json:"total_quota"`
	TotalUSD     float64 `json:"total_usd"`
	Share        float64 `json:"share"`
	QuotaShare   float64 `json:"quota_share"`
	GrowthPct    float64 `json:"growth_pct"`
}

type RankedVendor struct {
	Rank        int     `json:"rank"`
	Vendor      string  `json:"vendor"`
	VendorIcon  string  `json:"vendor_icon,omitempty"`
	TotalTokens int64   `json:"total_tokens"`
	TotalQuota  int64   `json:"total_quota"`
	TotalUSD    float64 `json:"total_usd"`
	Share       float64 `json:"share"`
	QuotaShare  float64 `json:"quota_share"`
	GrowthPct   float64 `json:"growth_pct"`
	ModelsCount int     `json:"models_count"`
	TopModel    string  `json:"top_model"`
}

type RankingMover struct {
	ModelName   string  `json:"model_name"`
	Vendor      string  `json:"vendor"`
	VendorIcon  string  `json:"vendor_icon,omitempty"`
	RankDelta   int     `json:"rank_delta"`
	CurrentRank int     `json:"current_rank"`
	GrowthPct   float64 `json:"growth_pct"`
}

type ModelHistoryPoint struct {
	Ts     string  `json:"ts"`
	Label  string  `json:"label"`
	Model  string  `json:"model"`
	Vendor string  `json:"vendor"`
	Tokens int64   `json:"tokens"`
	Quota  int64   `json:"quota"`
	USD    float64 `json:"usd"`
}

type ModelHistoryModel struct {
	Name       string  `json:"name"`
	Vendor     string  `json:"vendor"`
	Total      int64   `json:"total"`
	TotalQuota int64   `json:"total_quota"`
	TotalUSD   float64 `json:"total_usd"`
}

type ModelHistorySeries struct {
	Points  []ModelHistoryPoint `json:"points"`
	Models  []ModelHistoryModel `json:"models"`
	Buckets int                 `json:"buckets"`
}

type VendorSharePoint struct {
	Ts         string  `json:"ts"`
	Label      string  `json:"label"`
	Vendor     string  `json:"vendor"`
	Share      float64 `json:"share"`
	QuotaShare float64 `json:"quota_share"`
	Tokens     int64   `json:"tokens"`
	Quota      int64   `json:"quota"`
	USD        float64 `json:"usd"`
}

type VendorShareVendor struct {
	Name       string  `json:"name"`
	Total      int64   `json:"total"`
	TotalQuota int64   `json:"total_quota"`
	TotalUSD   float64 `json:"total_usd"`
	Share      float64 `json:"share"`
	QuotaShare float64 `json:"quota_share"`
}

type VendorShareSeries struct {
	Points  []VendorSharePoint  `json:"points"`
	Vendors []VendorShareVendor `json:"vendors"`
	Buckets int                 `json:"buckets"`
}

type RankingUserUsage struct {
	TotalTokens int64         `json:"total_tokens"`
	TotalQuota  int64         `json:"total_quota"`
	TotalUSD    float64       `json:"total_usd"`
	Users       []RankingUser `json:"users"`
}

type RankingUser struct {
	Rank        int                `json:"rank"`
	Username    string             `json:"username"`
	TotalTokens int64              `json:"total_tokens"`
	TotalQuota  int64              `json:"total_quota"`
	TotalUSD    float64            `json:"total_usd"`
	QuotaShare  float64            `json:"quota_share"`
	TokenShare  float64            `json:"token_share"`
	Groups      []RankingUserGroup `json:"groups"`
}

type RankingUserGroup struct {
	UseGroup    string  `json:"use_group"`
	TotalTokens int64   `json:"total_tokens"`
	TotalQuota  int64   `json:"total_quota"`
	TotalUSD    float64 `json:"total_usd"`
	QuotaShare  float64 `json:"quota_share"`
	TokenShare  float64 `json:"token_share"`
}

type rankingPeriodConfig struct {
	id          string
	duration    time.Duration
	bucketSize  int64
	labelLayout string
	hasPrevious bool
	bucketName  string
}

type rankingResolvedRange struct {
	config        rankingPeriodConfig
	start         int64
	end           int64
	previousStart int64
	previousEnd   int64
}

type rankingCacheItem struct {
	expiresAt time.Time
	data      *RankingsResponse
}

type rankingModelMeta struct {
	vendor     string
	vendorIcon string
}

type vendorAggregate struct {
	name           string
	icon           string
	totalTokens    int64
	totalQuota     int64
	previousTokens int64
	models         map[string]struct{}
	topModel       string
	topModelTokens int64
}

type historyAggregate struct {
	tokens int64
	quota  int64
}

type rankingUserAggregate struct {
	key         string
	username    string
	totalTokens int64
	totalQuota  int64
	groups      map[string]*historyAggregate
}

var (
	rankingCacheMu sync.Mutex
	rankingCache   = map[string]rankingCacheItem{}
)

// GetRankingsSnapshot preserves the original service entry point. New callers
// should use GetRankingsSnapshotWithOptions so the viewer level and custom
// timestamps are explicit.
func GetRankingsSnapshot(period string, visibleModelNames []string, canViewPrivate bool) (*RankingsResponse, error) {
	viewer := RankingViewerAnonymous
	if canViewPrivate {
		viewer = RankingViewerAdmin
	}
	return GetRankingsSnapshotWithOptions(RankingsRequest{
		Period:        period,
		VisibleModels: visibleModelNames,
		Viewer:        viewer,
	})
}

func GetRankingsSnapshotWithOptions(options RankingsRequest) (*RankingsResponse, error) {
	viewer := normalizeRankingViewer(options.Viewer)
	canViewPrivate := viewer == RankingViewerAdmin
	requestNow := time.Now()
	resolved, err := resolveRankingRange(
		options.Period,
		options.StartTimestamp,
		options.EndTimestamp,
		requestNow,
	)
	if err != nil {
		return nil, err
	}
	visibleModelNames, visibilityCacheKey := rankingVisibilityCacheKey(options.VisibleModels, canViewPrivate)
	cacheRange := resolved
	if options.Period != "custom" {
		// Keep the response range current while sharing the cache entry for
		// requests within the same five-minute snapshot window.
		cacheRange, err = resolveRankingRange(
			options.Period,
			options.StartTimestamp,
			options.EndTimestamp,
			rankingResolutionNow(options.Period, requestNow),
		)
		if err != nil {
			return nil, err
		}
	}
	cacheKey := fmt.Sprintf(
		"%s:%d:%d:%s:%s:%x",
		cacheRange.config.id,
		cacheRange.start,
		cacheRange.end,
		viewer,
		visibilityCacheKey,
		math.Float64bits(common.QuotaPerUnit),
	)

	now := time.Now()
	rankingCacheMu.Lock()
	cleanRankingCacheLocked(now)
	if item, ok := rankingCache[cacheKey]; ok {
		rankingCacheMu.Unlock()
		return item.data, nil
	}
	rankingCacheMu.Unlock()

	data, err := buildRankingsSnapshot(resolved, visibleModelNames, canViewPrivate, viewer)
	if err != nil {
		return nil, err
	}

	rankingCacheMu.Lock()
	now = time.Now()
	cleanRankingCacheLocked(now)
	rankingCache[cacheKey] = rankingCacheItem{
		expiresAt: now.Add(rankingCacheTTL),
		data:      data,
	}
	trimRankingCacheLocked()
	rankingCacheMu.Unlock()

	return data, nil
}

func normalizeRankingViewer(viewer RankingViewer) RankingViewer {
	switch viewer {
	case RankingViewerAdmin, RankingViewerUser, RankingViewerAnonymous:
		return viewer
	default:
		// An omitted or unknown viewer must fail closed. In particular, callers
		// that provide a visibility list without an explicit auth level must not
		// receive the private user_usage block.
		return RankingViewerAnonymous
	}
}

// Preset periods are intentionally resolved on the cache window boundary. A
// rolling range that changes every second would otherwise create a new cache
// key for every request and defeat the five-minute snapshot TTL. Custom ranges
// remain exact because their timestamps are part of the caller's query.
func rankingResolutionNow(period string, now time.Time) time.Time {
	if period == "custom" {
		return now
	}
	return now.Truncate(rankingCacheTTL)
}

func cleanRankingCacheLocked(now time.Time) {
	for key, item := range rankingCache {
		if !now.Before(item.expiresAt) {
			delete(rankingCache, key)
		}
	}
}

func trimRankingCacheLocked() {
	for len(rankingCache) > rankingCacheMaxEntries {
		var oldestKey string
		var oldestExpiry time.Time
		for key, item := range rankingCache {
			if oldestKey == "" || item.expiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = item.expiresAt
			}
		}
		delete(rankingCache, oldestKey)
	}
}

func InvalidateRankingsCache() {
	rankingCacheMu.Lock()
	rankingCache = map[string]rankingCacheItem{}
	rankingCacheMu.Unlock()
}

func rankingVisibilityCacheKey(modelNames []string, canViewPrivate bool) ([]string, string) {
	if canViewPrivate {
		return nil, "private"
	}
	seen := make(map[string]struct{}, len(modelNames))
	canonical := make([]string, 0, len(modelNames))
	for _, modelName := range modelNames {
		if modelName == "" {
			continue
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		canonical = append(canonical, modelName)
	}
	sort.Strings(canonical)
	digest := sha256.Sum256([]byte(strings.Join(canonical, "\x00")))
	return canonical, fmt.Sprintf("visible:%x", digest)
}

func rankingConfig(period string) (rankingPeriodConfig, error) {
	switch period {
	case "", "week":
		return rankingPeriodConfig{id: "week", duration: 7 * 24 * time.Hour, bucketSize: 24 * 3600, labelLayout: "Jan 2", hasPrevious: true, bucketName: "day"}, nil
	case "today":
		return rankingPeriodConfig{id: "today", duration: 24 * time.Hour, bucketSize: 3600, labelLayout: "15:04", hasPrevious: true, bucketName: "hour"}, nil
	case "month":
		return rankingPeriodConfig{id: "month", duration: 30 * 24 * time.Hour, bucketSize: 24 * 3600, labelLayout: "Jan 2", hasPrevious: true, bucketName: "day"}, nil
	case "year":
		return rankingPeriodConfig{id: "year", duration: 365 * 24 * time.Hour, bucketSize: 7 * 24 * 3600, labelLayout: "Jan 2", hasPrevious: true, bucketName: "week"}, nil
	default:
		return rankingPeriodConfig{}, fmt.Errorf("invalid ranking period: %s", period)
	}
}

func resolveRankingRange(period string, startTimestamp *int64, endTimestamp *int64, now time.Time) (rankingResolvedRange, error) {
	if period == "" {
		period = "week"
	}
	config, err := rankingConfig(period)
	if err == nil {
		end := now.Unix()
		start := now.Add(-config.duration).Unix()
		return makeResolvedRankingRange(config, start, end), nil
	}
	if period != "custom" {
		return rankingResolvedRange{}, err
	}
	if startTimestamp == nil || endTimestamp == nil {
		return rankingResolvedRange{}, fmt.Errorf("custom ranking period requires start_timestamp and end_timestamp")
	}
	start := *startTimestamp
	end := *endTimestamp
	if start <= 0 || end <= 0 {
		return rankingResolvedRange{}, fmt.Errorf("ranking timestamps must be positive")
	}
	if end > now.Unix() {
		end = now.Unix()
	}
	if end < start {
		return rankingResolvedRange{}, fmt.Errorf("ranking start_timestamp must not be after end_timestamp")
	}
	maxSeconds := int64(rankingMaxCustomDays) * 24 * 60 * 60
	if end-start+1 > maxSeconds {
		return rankingResolvedRange{}, fmt.Errorf("custom ranking period cannot exceed %d days", rankingMaxCustomDays)
	}

	duration := time.Duration(end-start+1) * time.Second
	config = rankingPeriodConfig{
		id:          "custom",
		duration:    duration,
		hasPrevious: true,
	}
	switch {
	case duration <= 48*time.Hour:
		config.bucketSize = 3600
		config.labelLayout = "Jan 2 15:04"
		config.bucketName = "hour"
	case duration <= 60*24*time.Hour:
		config.bucketSize = 24 * 3600
		config.labelLayout = "Jan 2"
		config.bucketName = "day"
	default:
		config.bucketSize = 7 * 24 * 3600
		config.labelLayout = "Jan 2"
		config.bucketName = "week"
	}
	return makeResolvedRankingRange(config, start, end), nil
}

func makeResolvedRankingRange(config rankingPeriodConfig, start int64, end int64) rankingResolvedRange {
	span := end - start + 1
	previousEnd := start - 1
	previousStart := previousEnd - span + 1
	return rankingResolvedRange{
		config:        config,
		start:         start,
		end:           end,
		previousStart: previousStart,
		previousEnd:   previousEnd,
	}
}

func buildRankingsSnapshot(resolved rankingResolvedRange, visibleModelNames []string, canViewPrivate bool, viewer RankingViewer) (*RankingsResponse, error) {
	currentTotals, err := model.GetRankingQuotaTotals(resolved.start, resolved.end, visibleModelNames, canViewPrivate)
	if err != nil {
		return nil, err
	}
	currentBuckets, err := model.GetRankingQuotaBuckets(resolved.start, resolved.end, resolved.config.bucketSize, visibleModelNames, canViewPrivate)
	if err != nil {
		return nil, err
	}

	var previousTotals []model.RankingQuotaTotal
	if resolved.config.hasPrevious {
		previousTotals, err = model.GetRankingQuotaTotals(resolved.previousStart, resolved.previousEnd, visibleModelNames, canViewPrivate)
		if err != nil {
			return nil, err
		}
	}
	currentTotals = normalizeRedactedRankingTotals(currentTotals)
	currentBuckets = normalizeRedactedRankingBuckets(currentBuckets)
	previousTotals = normalizeRedactedRankingTotals(previousTotals)

	meta := buildRankingModelMeta()
	meta[rankingOthersLabel] = rankingModelMeta{vendor: "Various"}
	totalTokens := sumRankingTokens(currentTotals)
	totalQuota := sumRankingQuota(currentTotals)
	previousRankByModel := rankingRankMap(previousTotals)
	previousTokensByModel := rankingTokenMap(previousTotals)

	rankedModels := buildRankedModels(currentTotals, totalTokens, totalQuota, previousRankByModel, previousTokensByModel, meta, resolved.config.hasPrevious)
	vendors := buildRankedVendors(currentTotals, previousTotals, totalTokens, totalQuota, meta, resolved.config.hasPrevious)
	modelHistory := buildModelHistory(currentBuckets, currentTotals, meta, resolved.config)
	vendorHistory := buildVendorShareHistory(currentBuckets, vendors, totalTokens, totalQuota, meta, resolved.config)
	movers, droppers := buildRankingMovers(rankedModels)

	response := &RankingsResponse{
		Range: RankingRange{
			Period:                 resolved.config.id,
			StartTimestamp:         resolved.start,
			EndTimestamp:           resolved.end,
			PreviousStartTimestamp: resolved.previousStart,
			PreviousEndTimestamp:   resolved.previousEnd,
			Bucket:                 resolved.config.bucketName,
			BucketSeconds:          resolved.config.bucketSize,
		},
		TotalTokens:        totalTokens,
		TotalQuota:         totalQuota,
		TotalUSD:           rankingQuotaUSD(totalQuota),
		Models:             limitRankedModels(rankedModels, rankingLeaderboardLimit),
		Vendors:            vendors,
		TopMovers:          movers,
		TopDroppers:        droppers,
		ModelsHistory:      modelHistory,
		VendorShareHistory: vendorHistory,
	}
	if viewer != RankingViewerAnonymous {
		userRows, queryErr := model.GetRankingUserQuotaTotals(resolved.start, resolved.end, visibleModelNames, canViewPrivate)
		if queryErr != nil {
			return nil, queryErr
		}
		response.UserUsage = buildRankingUserUsage(userRows, totalTokens, totalQuota, viewer == RankingViewerAdmin)
	}
	return response, nil
}

func buildRankingModelMeta() map[string]rankingModelMeta {
	vendorByID := make(map[int]model.PricingVendor)
	for _, vendor := range model.GetVendors() {
		vendorByID[vendor.ID] = vendor
	}

	meta := make(map[string]rankingModelMeta)
	for _, pricing := range model.GetPricing() {
		item := rankingModelMeta{vendor: rankingUnknownVendor}
		if vendor, ok := vendorByID[pricing.VendorID]; ok {
			item.vendor = vendor.Name
			item.vendorIcon = vendor.Icon
		} else if pricing.OwnerBy != "" {
			item.vendor = pricing.OwnerBy
		}
		meta[pricing.ModelName] = item
	}
	return meta
}

func modelMeta(modelName string, meta map[string]rankingModelMeta) rankingModelMeta {
	if item, ok := meta[modelName]; ok && item.vendor != "" {
		return item
	}
	return rankingModelMeta{vendor: rankingUnknownVendor}
}

func normalizeRedactedRankingTotals(rows []model.RankingQuotaTotal) []model.RankingQuotaTotal {
	totals := make(map[string]*model.RankingQuotaTotal, len(rows))
	for _, row := range rows {
		modelName := row.ModelName
		if modelName == "" {
			modelName = rankingOthersLabel
		}
		aggregate, ok := totals[modelName]
		if !ok {
			aggregate = &model.RankingQuotaTotal{ModelName: modelName}
			totals[modelName] = aggregate
		}
		aggregate.TotalTokens += row.TotalTokens
		aggregate.TotalQuota += row.TotalQuota
	}
	result := make([]model.RankingQuotaTotal, 0, len(totals))
	for _, aggregate := range totals {
		result = append(result, *aggregate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TotalTokens != result[j].TotalTokens {
			return result[i].TotalTokens > result[j].TotalTokens
		}
		if result[i].TotalQuota != result[j].TotalQuota {
			return result[i].TotalQuota > result[j].TotalQuota
		}
		return result[i].ModelName < result[j].ModelName
	})
	return result
}

func normalizeRedactedRankingBuckets(rows []model.RankingQuotaBucket) []model.RankingQuotaBucket {
	type aggregateKey struct {
		modelName string
		bucket    int64
	}
	totals := make(map[aggregateKey]*model.RankingQuotaBucket, len(rows))
	for _, row := range rows {
		modelName := row.ModelName
		if modelName == "" {
			modelName = rankingOthersLabel
		}
		key := aggregateKey{modelName: modelName, bucket: row.Bucket}
		aggregate, ok := totals[key]
		if !ok {
			aggregate = &model.RankingQuotaBucket{ModelName: modelName, Bucket: row.Bucket}
			totals[key] = aggregate
		}
		aggregate.Tokens += row.Tokens
		aggregate.Quota += row.Quota
	}
	result := make([]model.RankingQuotaBucket, 0, len(totals))
	for _, aggregate := range totals {
		result = append(result, *aggregate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Bucket != result[j].Bucket {
			return result[i].Bucket < result[j].Bucket
		}
		return result[i].ModelName < result[j].ModelName
	})
	return result
}

func buildRankedModels(totals []model.RankingQuotaTotal, totalTokens int64, totalQuota int64, previousRanks map[string]int, previousTokens map[string]int64, meta map[string]rankingModelMeta, showGrowth bool) []RankedModel {
	rows := make([]RankedModel, 0, len(totals))
	for idx, item := range totals {
		modelMeta := modelMeta(item.ModelName, meta)
		var previousRank *int
		if rank, ok := previousRanks[item.ModelName]; ok {
			rankCopy := rank
			previousRank = &rankCopy
		}
		growth := 0.0
		if showGrowth {
			growth = rankingGrowthPct(item.TotalTokens, previousTokens[item.ModelName])
		}
		rows = append(rows, RankedModel{
			Rank:         idx + 1,
			PreviousRank: previousRank,
			ModelName:    item.ModelName,
			Vendor:       modelMeta.vendor,
			VendorIcon:   modelMeta.vendorIcon,
			Category:     "all",
			TotalTokens:  item.TotalTokens,
			TotalQuota:   item.TotalQuota,
			TotalUSD:     rankingQuotaUSD(item.TotalQuota),
			Share:        rankingShare(item.TotalTokens, totalTokens),
			QuotaShare:   rankingShare(item.TotalQuota, totalQuota),
			GrowthPct:    growth,
		})
	}
	return rows
}

func buildRankedVendors(currentTotals []model.RankingQuotaTotal, previousTotals []model.RankingQuotaTotal, totalTokens int64, totalQuota int64, meta map[string]rankingModelMeta, showGrowth bool) []RankedVendor {
	aggregates := make(map[string]*vendorAggregate)
	for _, item := range currentTotals {
		modelMeta := modelMeta(item.ModelName, meta)
		agg := ensureVendorAggregate(aggregates, modelMeta)
		agg.totalTokens += item.TotalTokens
		agg.totalQuota += item.TotalQuota
		agg.models[item.ModelName] = struct{}{}
		if item.TotalTokens > agg.topModelTokens {
			agg.topModel = item.ModelName
			agg.topModelTokens = item.TotalTokens
		}
	}
	for _, item := range previousTotals {
		modelMeta := modelMeta(item.ModelName, meta)
		agg := ensureVendorAggregate(aggregates, modelMeta)
		agg.previousTokens += item.TotalTokens
	}

	rows := make([]RankedVendor, 0, len(aggregates))
	for _, agg := range aggregates {
		if agg.totalTokens <= 0 && agg.totalQuota <= 0 {
			continue
		}
		growth := 0.0
		if showGrowth {
			growth = rankingGrowthPct(agg.totalTokens, agg.previousTokens)
		}
		rows = append(rows, RankedVendor{
			Vendor:      agg.name,
			VendorIcon:  agg.icon,
			TotalTokens: agg.totalTokens,
			TotalQuota:  agg.totalQuota,
			TotalUSD:    rankingQuotaUSD(agg.totalQuota),
			Share:       rankingShare(agg.totalTokens, totalTokens),
			QuotaShare:  rankingShare(agg.totalQuota, totalQuota),
			GrowthPct:   growth,
			ModelsCount: len(agg.models),
			TopModel:    agg.topModel,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalTokens != rows[j].TotalTokens {
			return rows[i].TotalTokens > rows[j].TotalTokens
		}
		if rows[i].TotalQuota != rows[j].TotalQuota {
			return rows[i].TotalQuota > rows[j].TotalQuota
		}
		return rows[i].Vendor < rows[j].Vendor
	})
	for idx := range rows {
		rows[idx].Rank = idx + 1
	}
	return rows
}

func ensureVendorAggregate(aggregates map[string]*vendorAggregate, meta rankingModelMeta) *vendorAggregate {
	name := meta.vendor
	if name == "" {
		name = rankingUnknownVendor
	}
	agg, ok := aggregates[name]
	if !ok {
		agg = &vendorAggregate{
			name:   name,
			icon:   meta.vendorIcon,
			models: make(map[string]struct{}),
		}
		aggregates[name] = agg
	}
	if agg.icon == "" && meta.vendorIcon != "" {
		agg.icon = meta.vendorIcon
	}
	return agg
}

func buildModelHistory(buckets []model.RankingQuotaBucket, totals []model.RankingQuotaTotal, meta map[string]rankingModelMeta, config rankingPeriodConfig) ModelHistorySeries {
	topModels := make(map[string]struct{})
	models := make([]ModelHistoryModel, 0, minInt(len(totals), rankingHistoryLimit)+1)
	otherTotal := historyAggregate{}
	for idx, item := range totals {
		if idx < rankingHistoryLimit {
			topModels[item.ModelName] = struct{}{}
			modelMeta := modelMeta(item.ModelName, meta)
			models = append(models, ModelHistoryModel{
				Name:       item.ModelName,
				Vendor:     modelMeta.vendor,
				Total:      item.TotalTokens,
				TotalQuota: item.TotalQuota,
				TotalUSD:   rankingQuotaUSD(item.TotalQuota),
			})
			continue
		}
		otherTotal.tokens += item.TotalTokens
		otherTotal.quota += item.TotalQuota
	}
	if otherTotal.tokens > 0 || otherTotal.quota > 0 {
		if _, alreadyVisible := topModels[rankingOthersLabel]; alreadyVisible {
			for i := range models {
				if models[i].Name == rankingOthersLabel {
					models[i].Total += otherTotal.tokens
					models[i].TotalQuota += otherTotal.quota
					models[i].TotalUSD = rankingQuotaUSD(models[i].TotalQuota)
					break
				}
			}
		} else {
			models = append(models, ModelHistoryModel{Name: rankingOthersLabel, Vendor: "Various", Total: otherTotal.tokens, TotalQuota: otherTotal.quota, TotalUSD: rankingQuotaUSD(otherTotal.quota)})
		}
	}

	bucketSet := make(map[int64]struct{})
	totalsByBucketAndModel := make(map[int64]map[string]historyAggregate)
	for _, item := range buckets {
		modelName := item.ModelName
		if _, ok := topModels[modelName]; !ok {
			modelName = rankingOthersLabel
		}
		bucketSet[item.Bucket] = struct{}{}
		if _, ok := totalsByBucketAndModel[item.Bucket]; !ok {
			totalsByBucketAndModel[item.Bucket] = make(map[string]historyAggregate)
		}
		agg := totalsByBucketAndModel[item.Bucket][modelName]
		agg.tokens += item.Tokens
		agg.quota += item.Quota
		totalsByBucketAndModel[item.Bucket][modelName] = agg
	}

	sortedBuckets := sortedRankingBuckets(bucketSet)
	points := make([]ModelHistoryPoint, 0, len(sortedBuckets)*len(models))
	for _, bucket := range sortedBuckets {
		for _, historyModel := range models {
			agg := totalsByBucketAndModel[bucket][historyModel.Name]
			if agg.tokens <= 0 && agg.quota <= 0 {
				continue
			}
			points = append(points, ModelHistoryPoint{
				Ts:     rankingBucketTs(bucket),
				Label:  rankingBucketLabel(bucket, config),
				Model:  historyModel.Name,
				Vendor: historyModel.Vendor,
				Tokens: agg.tokens,
				Quota:  agg.quota,
				USD:    rankingQuotaUSD(agg.quota),
			})
		}
	}

	return ModelHistorySeries{Points: points, Models: models, Buckets: len(sortedBuckets)}
}

func buildVendorShareHistory(buckets []model.RankingQuotaBucket, vendors []RankedVendor, totalTokens int64, totalQuota int64, meta map[string]rankingModelMeta, config rankingPeriodConfig) VendorShareSeries {
	topVendors := make(map[string]struct{})
	vendorRows := make([]VendorShareVendor, 0, minInt(len(vendors), rankingVendorLimit)+1)
	otherTotal := historyAggregate{}
	for idx, vendor := range vendors {
		if idx < rankingVendorLimit {
			topVendors[vendor.Vendor] = struct{}{}
			vendorRows = append(vendorRows, VendorShareVendor{Name: vendor.Vendor, Total: vendor.TotalTokens, TotalQuota: vendor.TotalQuota, TotalUSD: vendor.TotalUSD, Share: vendor.Share, QuotaShare: vendor.QuotaShare})
			continue
		}
		otherTotal.tokens += vendor.TotalTokens
		otherTotal.quota += vendor.TotalQuota
	}
	if otherTotal.tokens > 0 || otherTotal.quota > 0 {
		vendorRows = append(vendorRows, VendorShareVendor{Name: rankingOthersLabel, Total: otherTotal.tokens, TotalQuota: otherTotal.quota, TotalUSD: rankingQuotaUSD(otherTotal.quota), Share: rankingShare(otherTotal.tokens, totalTokens), QuotaShare: rankingShare(otherTotal.quota, totalQuota)})
	}

	bucketSet := make(map[int64]struct{})
	totalsByBucketAndVendor := make(map[int64]map[string]historyAggregate)
	totalsByBucket := make(map[int64]historyAggregate)
	for _, item := range buckets {
		modelMeta := modelMeta(item.ModelName, meta)
		vendorName := modelMeta.vendor
		if _, ok := topVendors[vendorName]; !ok {
			vendorName = rankingOthersLabel
		}
		bucketSet[item.Bucket] = struct{}{}
		if _, ok := totalsByBucketAndVendor[item.Bucket]; !ok {
			totalsByBucketAndVendor[item.Bucket] = make(map[string]historyAggregate)
		}
		agg := totalsByBucketAndVendor[item.Bucket][vendorName]
		agg.tokens += item.Tokens
		agg.quota += item.Quota
		totalsByBucketAndVendor[item.Bucket][vendorName] = agg
		bucketTotal := totalsByBucket[item.Bucket]
		bucketTotal.tokens += item.Tokens
		bucketTotal.quota += item.Quota
		totalsByBucket[item.Bucket] = bucketTotal
	}

	sortedBuckets := sortedRankingBuckets(bucketSet)
	points := make([]VendorSharePoint, 0, len(sortedBuckets)*len(vendorRows))
	for _, bucket := range sortedBuckets {
		for _, vendor := range vendorRows {
			agg := totalsByBucketAndVendor[bucket][vendor.Name]
			if agg.tokens <= 0 && agg.quota <= 0 {
				continue
			}
			bucketTotal := totalsByBucket[bucket]
			points = append(points, VendorSharePoint{
				Ts:         rankingBucketTs(bucket),
				Label:      rankingBucketLabel(bucket, config),
				Vendor:     vendor.Name,
				Share:      rankingShare(agg.tokens, bucketTotal.tokens),
				QuotaShare: rankingShare(agg.quota, bucketTotal.quota),
				Tokens:     agg.tokens,
				Quota:      agg.quota,
				USD:        rankingQuotaUSD(agg.quota),
			})
		}
	}

	return VendorShareSeries{Points: points, Vendors: vendorRows, Buckets: len(sortedBuckets)}
}

func buildRankingUserUsage(rows []model.RankingUserQuotaRow, totalTokens int64, totalQuota int64, canViewPrivate bool) *RankingUserUsage {
	aggregates := make(map[string]*rankingUserAggregate)
	for _, row := range rows {
		rawUsername := strings.TrimSpace(row.Username)
		if rawUsername == "" {
			rawUsername = "Unknown user"
		}
		key := fmt.Sprintf("name\x00%s", rawUsername)
		if row.UserID > 0 {
			// Usernames can change between exports; a positive user ID is the
			// stable identity for aggregation. Legacy rows without an ID fall
			// back to the recorded username.
			key = fmt.Sprintf("user\x00%d", row.UserID)
		}
		username := rawUsername
		if !canViewPrivate && row.HiddenModel {
			key = "__other_users__"
			username = rankingOtherUsersLabel
		}
		aggregate, ok := aggregates[key]
		if !ok {
			aggregate = &rankingUserAggregate{key: key, username: username, groups: make(map[string]*historyAggregate)}
			aggregates[key] = aggregate
		} else if key != "__other_users__" && username != "Unknown user" && (aggregate.username == "Unknown user" || username < aggregate.username) {
			// Keep the display name deterministic when a historical user row has
			// a stale or empty username.
			aggregate.username = username
		}
		aggregate.totalTokens += row.TotalTokens
		aggregate.totalQuota += row.TotalQuota
		groupName := strings.TrimSpace(row.UseGroup)
		if !canViewPrivate && row.HiddenModel {
			groupName = rankingUnknownGroup
		} else if groupName == "" {
			groupName = rankingUnknownGroup
		}
		group, ok := aggregate.groups[groupName]
		if !ok {
			group = &historyAggregate{}
			aggregate.groups[groupName] = group
		}
		group.tokens += row.TotalTokens
		group.quota += row.TotalQuota
	}

	usage := &RankingUserUsage{TotalTokens: totalTokens, TotalQuota: totalQuota, TotalUSD: rankingQuotaUSD(totalQuota), Users: make([]RankingUser, 0, minInt(len(aggregates), rankingUserLimit))}
	aggregateRows := make([]*rankingUserAggregate, 0, len(aggregates))
	for _, aggregate := range aggregates {
		aggregateRows = append(aggregateRows, aggregate)
	}
	sort.Slice(aggregateRows, func(i, j int) bool {
		if aggregateRows[i].totalQuota != aggregateRows[j].totalQuota {
			return aggregateRows[i].totalQuota > aggregateRows[j].totalQuota
		}
		if aggregateRows[i].totalTokens != aggregateRows[j].totalTokens {
			return aggregateRows[i].totalTokens > aggregateRows[j].totalTokens
		}
		if aggregateRows[i].username != aggregateRows[j].username {
			return aggregateRows[i].username < aggregateRows[j].username
		}
		return aggregateRows[i].key < aggregateRows[j].key
	})
	usedUsernames := make(map[string]struct{}, len(aggregateRows))
	for idx, aggregate := range aggregateRows {
		if idx >= rankingUserLimit {
			break
		}
		displayUsername := aggregate.username
		if !canViewPrivate && displayUsername != rankingOtherUsersLabel {
			displayUsername = maskRankingUsername(displayUsername)
		}
		baseDisplayUsername := displayUsername
		for suffix := 2; ; suffix++ {
			if _, exists := usedUsernames[displayUsername]; !exists {
				usedUsernames[displayUsername] = struct{}{}
				break
			}
			displayUsername = fmt.Sprintf("%s (%d)", baseDisplayUsername, suffix)
		}
		user := RankingUser{
			Rank:        idx + 1,
			Username:    displayUsername,
			TotalTokens: aggregate.totalTokens,
			TotalQuota:  aggregate.totalQuota,
			TotalUSD:    rankingQuotaUSD(aggregate.totalQuota),
			QuotaShare:  rankingShare(aggregate.totalQuota, totalQuota),
			TokenShare:  rankingShare(aggregate.totalTokens, totalTokens),
			Groups:      make([]RankingUserGroup, 0, len(aggregate.groups)),
		}
		groupNames := make([]string, 0, len(aggregate.groups))
		for name := range aggregate.groups {
			groupNames = append(groupNames, name)
		}
		sort.Slice(groupNames, func(i, j int) bool {
			left := aggregate.groups[groupNames[i]]
			right := aggregate.groups[groupNames[j]]
			if left.quota != right.quota {
				return left.quota > right.quota
			}
			if left.tokens != right.tokens {
				return left.tokens > right.tokens
			}
			return groupNames[i] < groupNames[j]
		})
		for _, groupName := range groupNames {
			group := aggregate.groups[groupName]
			user.Groups = append(user.Groups, RankingUserGroup{
				UseGroup:    groupName,
				TotalTokens: group.tokens,
				TotalQuota:  group.quota,
				TotalUSD:    rankingQuotaUSD(group.quota),
				QuotaShare:  rankingShare(group.quota, aggregate.totalQuota),
				TokenShare:  rankingShare(group.tokens, aggregate.totalTokens),
			})
		}
		usage.Users = append(usage.Users, user)
	}
	return usage
}

func maskRankingUsername(username string) string {
	runes := []rune(strings.TrimSpace(username))
	if len(runes) == 0 {
		return "Unknown user"
	}
	if len(runes) == 1 {
		return string(runes[0]) + "***"
	}
	return string(runes[0]) + "***" + string(runes[len(runes)-1])
}

func buildRankingMovers(models []RankedModel) ([]RankingMover, []RankingMover) {
	movers := make([]RankingMover, 0)
	droppers := make([]RankingMover, 0)
	for _, item := range models {
		if item.PreviousRank == nil {
			continue
		}
		delta := *item.PreviousRank - item.Rank
		if delta == 0 {
			continue
		}
		row := RankingMover{ModelName: item.ModelName, Vendor: item.Vendor, VendorIcon: item.VendorIcon, RankDelta: delta, CurrentRank: item.Rank, GrowthPct: item.GrowthPct}
		if delta > 0 {
			movers = append(movers, row)
		} else {
			droppers = append(droppers, row)
		}
	}
	sort.Slice(movers, func(i, j int) bool {
		if movers[i].RankDelta == movers[j].RankDelta {
			return movers[i].GrowthPct > movers[j].GrowthPct
		}
		return movers[i].RankDelta > movers[j].RankDelta
	})
	sort.Slice(droppers, func(i, j int) bool {
		if droppers[i].RankDelta == droppers[j].RankDelta {
			return droppers[i].GrowthPct < droppers[j].GrowthPct
		}
		return droppers[i].RankDelta < droppers[j].RankDelta
	})
	return limitRankingMovers(movers, rankingMoverLimit), limitRankingMovers(droppers, rankingMoverLimit)
}

func sortedRankingBuckets(bucketSet map[int64]struct{}) []int64 {
	buckets := make([]int64, 0, len(bucketSet))
	for bucket := range bucketSet {
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
	return buckets
}

func rankingBucketTs(bucket int64) string {
	return time.Unix(bucket, 0).UTC().Format(time.RFC3339)
}

func rankingBucketLabel(bucket int64, config rankingPeriodConfig) string {
	return time.Unix(bucket, 0).Format(config.labelLayout)
}

func rankingRankMap(totals []model.RankingQuotaTotal) map[string]int {
	ranks := make(map[string]int, len(totals))
	for idx, item := range totals {
		ranks[item.ModelName] = idx + 1
	}
	return ranks
}

func rankingTokenMap(totals []model.RankingQuotaTotal) map[string]int64 {
	tokens := make(map[string]int64, len(totals))
	for _, item := range totals {
		tokens[item.ModelName] = item.TotalTokens
	}
	return tokens
}

func sumRankingTokens(totals []model.RankingQuotaTotal) int64 {
	total := int64(0)
	for _, item := range totals {
		total += item.TotalTokens
	}
	return total
}

func sumRankingQuota(totals []model.RankingQuotaTotal) int64 {
	total := int64(0)
	for _, item := range totals {
		total += item.TotalQuota
	}
	return total
}

func rankingShare(value int64, total int64) float64 {
	if total <= 0 || value <= 0 {
		return 0
	}
	return roundRankingFloat(float64(value) / float64(total))
}

func rankingGrowthPct(current int64, previous int64) float64 {
	if previous <= 0 {
		if current > 0 {
			return 100
		}
		return 0
	}
	return roundRankingFloat((float64(current-previous) / float64(previous)) * 100)
}

func rankingQuotaUSD(quota int64) float64 {
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0
	}
	return float64(quota) / common.QuotaPerUnit
}

func roundRankingFloat(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func limitRankedModels(rows []RankedModel, limit int) []RankedModel {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return rows[:limit]
}

func limitRankingMovers(rows []RankingMover, limit int) []RankingMover {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return rows[:limit]
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
