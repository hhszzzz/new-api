package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"golang.org/x/sync/errgroup"
)

const (
	modelRadarSchemaVersion      = 1
	modelRadarEfficiencySchema   = 2
	modelRadarInsightsSchema     = 1
	modelRadarEfficiencyType     = "distributed_intelligence_efficiency"
	modelRadarEfficiencyURL      = "https://codexradar.com/data/intelligence-efficiency.json"
	modelRadarInsightsURL        = "https://api.codexradar.com/api/v1/radar-insights"
	modelRadarSourceURL          = "https://codexradar.com"
	modelRadarAttribution        = "数据来自 Codex 雷达 codexradar.com"
	modelRadarRequestTimeout     = 15 * time.Second
	modelRadarEfficiencyMaxBytes = 4 << 20
	modelRadarInsightsMaxBytes   = 1 << 20
	modelRadarDefaultInterval    = 10 * time.Minute
	modelRadarMinimumInterval    = 10 * time.Minute
	modelRadarMinimumStaleAfter  = 30 * time.Minute
	modelRadarMaxConfigurations  = 256
	modelRadarMaxHistoryFrames   = 256
	modelRadarMaxAlerts          = 64
)

var ErrModelRadarUnavailable = errors.New("model radar data unavailable")

type ModelRadarSource struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Attribution string `json:"attribution"`
}

type ModelRadarConfiguration struct {
	Model                 string   `json:"model"`
	Effort                string   `json:"effort"`
	IQ                    float64  `json:"iq"`
	Passed                int      `json:"passed"`
	ValidTasks            int      `json:"valid_tasks"`
	AveragePriceUSD       *float64 `json:"average_price_usd"`
	PriceSamples          *int     `json:"price_samples"`
	AverageMinutes        *float64 `json:"average_minutes"`
	DurationSamples       *int     `json:"duration_samples"`
	IncompleteCostSamples *int     `json:"incomplete_cost_samples"`
	TotalRuns             *int     `json:"total_runs"`
	LatestGradedAt        *int64   `json:"latest_graded_at"`
	AverageAgentSteps     *float64 `json:"average_agent_steps"`
	AgentStepsSamples     *int     `json:"agent_steps_samples"`
	AverageTotalTokens    *float64 `json:"average_total_tokens"`
	TokenSamples          *int     `json:"token_samples"`
	CacheHitRate          *float64 `json:"cache_hit_rate"`
	CacheTokenSamples     *int     `json:"cache_token_samples"`
	CombinedCostIndex     *float64 `json:"combined_cost_index"`
}

type ModelRadarHistoryPoint struct {
	Model              string   `json:"model"`
	Effort             string   `json:"effort"`
	IQ                 float64  `json:"iq"`
	Passed             int      `json:"passed"`
	ValidTasks         int      `json:"valid_tasks"`
	AveragePriceUSD    *float64 `json:"average_price_usd"`
	AverageMinutes     *float64 `json:"average_minutes"`
	AverageAgentSteps  *float64 `json:"average_agent_steps"`
	AverageTotalTokens *float64 `json:"average_total_tokens"`
	CacheHitRate       *float64 `json:"cache_hit_rate"`
}

type ModelRadarHistoryFrame struct {
	Ts     int64                    `json:"ts"`
	Points []ModelRadarHistoryPoint `json:"points"`
}

type ModelRadarDegradationAlert struct {
	Model            string  `json:"model"`
	Effort           string  `json:"effort"`
	IQ               float64 `json:"iq"`
	Degradation12hIQ float64 `json:"degradation_12h_iq"`
	Degradation24hIQ float64 `json:"degradation_24h_iq"`
	Degradation48hIQ float64 `json:"degradation_48h_iq"`
}

type ModelRadarData struct {
	SchemaVersion      int                          `json:"schema_version"`
	FetchedAt          int64                        `json:"fetched_at"`
	SourceUpdatedAt    int64                        `json:"source_updated_at"`
	AlertsUpdatedAt    int64                        `json:"alerts_updated_at"`
	Stale              bool                         `json:"stale"`
	Source             ModelRadarSource             `json:"source"`
	ModelCount         int                          `json:"model_count"`
	ConfigurationCount int                          `json:"configuration_count"`
	Configurations     []ModelRadarConfiguration    `json:"configurations"`
	History            []ModelRadarHistoryFrame     `json:"history"`
	DegradationAlerts  []ModelRadarDegradationAlert `json:"degradation_alerts"`
}

type ModelRadarSyncResult struct {
	FetchedAt          int64 `json:"fetched_at"`
	SourceUpdatedAt    int64 `json:"source_updated_at"`
	AlertsUpdatedAt    int64 `json:"alerts_updated_at"`
	ModelCount         int   `json:"model_count"`
	ConfigurationCount int   `json:"configuration_count"`
	AlertCount         int   `json:"alert_count"`
}

type modelRadarUpstreamPoint struct {
	Model                 string   `json:"model"`
	Effort                string   `json:"effort"`
	IQ                    *float64 `json:"iq"`
	Passed                *float64 `json:"passed"`
	ValidTasks            *float64 `json:"valid_tasks"`
	AveragePriceUSD       *float64 `json:"average_price_usd"`
	PriceSamples          *int     `json:"price_samples"`
	AverageMinutes        *float64 `json:"average_minutes"`
	DurationSamples       *int     `json:"duration_samples"`
	IncompleteCostSamples *int     `json:"incomplete_cost_samples"`
	TotalRuns             *int     `json:"total_runs"`
	LatestGradedAt        *string  `json:"latest_graded_at"`
	AverageAgentSteps     *float64 `json:"average_agent_steps"`
	AgentStepsSamples     *int     `json:"agent_steps_samples"`
	AverageTotalTokens    *float64 `json:"average_total_tokens"`
	TokenSamples          *int     `json:"token_samples"`
	CacheHitRate          *float64 `json:"cache_hit_rate"`
	CacheTokenSamples     *int     `json:"cache_token_samples"`
	CombinedCostIndex     *float64 `json:"combined_cost_index"`
}

type modelRadarUpstreamHistoryFrame struct {
	At     string                    `json:"at"`
	Points []modelRadarUpstreamPoint `json:"points"`
}

type modelRadarEfficiencyPayload struct {
	Schema          int                              `json:"schema"`
	Type            string                           `json:"type"`
	SourceUpdatedAt string                           `json:"source_updated_at"`
	Points          []modelRadarUpstreamPoint        `json:"points"`
	History         []modelRadarUpstreamHistoryFrame `json:"history"`
}

type modelRadarUpstreamAlert struct {
	Model            string   `json:"model"`
	Effort           string   `json:"effort"`
	IQ               *float64 `json:"iq"`
	Degradation12hIQ *float64 `json:"degradation_12h_iq"`
	Degradation24hIQ *float64 `json:"degradation_24h_iq"`
	Degradation48hIQ *float64 `json:"degradation_48h_iq"`
}

type modelRadarInsightsPayload struct {
	Schema            int    `json:"schema"`
	SourceUpdatedAt   string `json:"source_updated_at"`
	DegradationAlerts struct {
		Items []modelRadarUpstreamAlert `json:"items"`
	} `json:"degradation_alerts"`
}

func ModelRadarSyncEnabled() bool {
	return common.GetEnvOrDefaultBool("MODEL_RADAR_SYNC_ENABLED", true)
}

func ModelRadarSyncInterval() time.Duration {
	interval := time.Duration(common.GetEnvOrDefault("MODEL_RADAR_SYNC_INTERVAL_MINUTES", 10)) * time.Minute
	if interval < modelRadarMinimumInterval {
		return modelRadarDefaultInterval
	}
	return interval
}

func ModelRadarStaleAfter() time.Duration {
	staleAfter := 3 * ModelRadarSyncInterval()
	if staleAfter < modelRadarMinimumStaleAfter {
		return modelRadarMinimumStaleAfter
	}
	return staleAfter
}

// IsModelRadarStale tracks the last successful local sync; upstream data has its own update cadence.
func IsModelRadarStale(now int64, fetchedAt int64, staleAfter time.Duration) bool {
	if now <= 0 || fetchedAt <= 0 {
		return true
	}
	threshold := int64(staleAfter.Seconds())
	if threshold <= 0 {
		threshold = int64(modelRadarMinimumStaleAfter.Seconds())
	}
	return now-fetchedAt > threshold
}

func SyncModelRadar(ctx context.Context) (*ModelRadarSyncResult, error) {
	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	return syncModelRadar(ctx, client, modelRadarEfficiencyURL, modelRadarInsightsURL, common.GetTimestamp())
}

func syncModelRadar(ctx context.Context, client *http.Client, efficiencyURL string, insightsURL string, fetchedAt int64) (*ModelRadarSyncResult, error) {
	data, err := fetchModelRadar(ctx, client, efficiencyURL, insightsURL)
	if err != nil {
		return nil, err
	}

	data.SchemaVersion = modelRadarSchemaVersion
	data.FetchedAt = fetchedAt
	data.Stale = false
	data.Source = ModelRadarSource{
		Name:        "Codex Radar",
		URL:         modelRadarSourceURL,
		Attribution: modelRadarAttribution,
	}
	payload, err := common.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal model radar snapshot: %w", err)
	}
	if err := model.SaveModelRadarSnapshot(ctx, &model.ModelRadarSnapshot{
		SchemaVersion:   modelRadarSchemaVersion,
		Payload:         payload,
		SourceUpdatedAt: data.SourceUpdatedAt,
		AlertsUpdatedAt: data.AlertsUpdatedAt,
		FetchedAt:       fetchedAt,
	}); err != nil {
		return nil, fmt.Errorf("save model radar snapshot: %w", err)
	}

	return &ModelRadarSyncResult{
		FetchedAt:          fetchedAt,
		SourceUpdatedAt:    data.SourceUpdatedAt,
		AlertsUpdatedAt:    data.AlertsUpdatedAt,
		ModelCount:         data.ModelCount,
		ConfigurationCount: data.ConfigurationCount,
		AlertCount:         len(data.DegradationAlerts),
	}, nil
}

func GetModelRadar(ctx context.Context) (*ModelRadarData, error) {
	snapshot, err := model.GetModelRadarSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("load model radar snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, ErrModelRadarUnavailable
	}

	var data ModelRadarData
	if err := common.Unmarshal(snapshot.Payload, &data); err != nil {
		return nil, fmt.Errorf("decode model radar snapshot: %w", err)
	}
	data.SchemaVersion = snapshot.SchemaVersion
	data.FetchedAt = snapshot.FetchedAt
	data.SourceUpdatedAt = snapshot.SourceUpdatedAt
	data.AlertsUpdatedAt = snapshot.AlertsUpdatedAt
	data.Stale = IsModelRadarStale(
		common.GetTimestamp(),
		snapshot.FetchedAt,
		ModelRadarStaleAfter(),
	)
	return &data, nil
}

func fetchModelRadar(ctx context.Context, client *http.Client, efficiencyURL string, insightsURL string) (*ModelRadarData, error) {
	if client == nil {
		return nil, errors.New("model radar HTTP client is required")
	}
	requestCtx, cancel := context.WithTimeout(ctx, modelRadarRequestTimeout)
	defer cancel()

	var efficiency modelRadarEfficiencyPayload
	var insights modelRadarInsightsPayload
	group, groupCtx := errgroup.WithContext(requestCtx)
	group.Go(func() error {
		return fetchModelRadarJSON(groupCtx, client, efficiencyURL, modelRadarEfficiencyMaxBytes, &efficiency)
	})
	group.Go(func() error {
		return fetchModelRadarJSON(groupCtx, client, insightsURL, modelRadarInsightsMaxBytes, &insights)
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}

	configurations, history, sourceUpdatedAt, err := normalizeModelRadarEfficiency(efficiency)
	if err != nil {
		return nil, fmt.Errorf("validate model radar efficiency data: %w", err)
	}
	alerts, alertsUpdatedAt, err := normalizeModelRadarInsights(insights)
	if err != nil {
		return nil, fmt.Errorf("validate model radar insights data: %w", err)
	}

	models := make(map[string]struct{}, len(configurations))
	for _, configuration := range configurations {
		models[configuration.Model] = struct{}{}
	}
	return &ModelRadarData{
		SourceUpdatedAt:    sourceUpdatedAt,
		AlertsUpdatedAt:    alertsUpdatedAt,
		ModelCount:         len(models),
		ConfigurationCount: len(configurations),
		Configurations:     configurations,
		History:            history,
		DegradationAlerts:  alerts,
	}, nil
}

func fetchModelRadarJSON(ctx context.Context, client *http.Client, sourceURL string, maxBytes int64, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fmt.Errorf("create model radar request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "new-api-model-radar/1.0")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", request.URL.Host, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: unexpected HTTP status %d", request.URL.Host, response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || mediaType != "application/json" {
			return fmt.Errorf("fetch %s: expected JSON response", request.URL.Host)
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read %s response: %w", request.URL.Host, err)
	}
	if int64(len(body)) > maxBytes {
		return fmt.Errorf("fetch %s: response exceeds %d bytes", request.URL.Host, maxBytes)
	}
	if err := common.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode %s response: %w", request.URL.Host, err)
	}
	return nil
}

func normalizeModelRadarEfficiency(payload modelRadarEfficiencyPayload) ([]ModelRadarConfiguration, []ModelRadarHistoryFrame, int64, error) {
	if payload.Schema != modelRadarEfficiencySchema || payload.Type != modelRadarEfficiencyType {
		return nil, nil, 0, errors.New("unsupported source schema")
	}
	sourceUpdatedAt, err := parseModelRadarTimestamp(payload.SourceUpdatedAt)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("invalid source_updated_at: %w", err)
	}
	if len(payload.Points) == 0 || len(payload.Points) > modelRadarMaxConfigurations {
		return nil, nil, 0, errors.New("configuration count is out of range")
	}
	if len(payload.History) == 0 || len(payload.History) > modelRadarMaxHistoryFrames {
		return nil, nil, 0, errors.New("history frame count is out of range")
	}

	configurations := make([]ModelRadarConfiguration, 0, len(payload.Points))
	seen := make(map[string]struct{}, len(payload.Points))
	for _, point := range payload.Points {
		configuration, key, err := normalizeModelRadarConfiguration(point)
		if err != nil {
			return nil, nil, 0, err
		}
		if _, exists := seen[key]; exists {
			return nil, nil, 0, fmt.Errorf("duplicate configuration %s", key)
		}
		seen[key] = struct{}{}
		configurations = append(configurations, configuration)
	}

	history := make([]ModelRadarHistoryFrame, 0, len(payload.History))
	var previousTs int64
	for _, frame := range payload.History {
		ts, parseErr := parseModelRadarTimestamp(frame.At)
		if parseErr != nil {
			return nil, nil, 0, fmt.Errorf("invalid history timestamp: %w", parseErr)
		}
		if previousTs != 0 && ts <= previousTs {
			return nil, nil, 0, errors.New("history timestamps must be strictly increasing")
		}
		previousTs = ts
		if len(frame.Points) == 0 || len(frame.Points) > modelRadarMaxConfigurations {
			return nil, nil, 0, errors.New("history point count is out of range")
		}
		points := make([]ModelRadarHistoryPoint, 0, len(frame.Points))
		frameSeen := make(map[string]struct{}, len(frame.Points))
		for _, point := range frame.Points {
			historyPoint, key, pointErr := normalizeModelRadarHistoryPoint(point)
			if pointErr != nil {
				return nil, nil, 0, pointErr
			}
			if _, exists := frameSeen[key]; exists {
				return nil, nil, 0, fmt.Errorf("duplicate history configuration %s", key)
			}
			frameSeen[key] = struct{}{}
			points = append(points, historyPoint)
		}
		history = append(history, ModelRadarHistoryFrame{Ts: ts, Points: points})
	}
	return configurations, history, sourceUpdatedAt, nil
}

func normalizeModelRadarConfiguration(point modelRadarUpstreamPoint) (ModelRadarConfiguration, string, error) {
	modelName, effort, key, err := validateModelRadarIdentity(point.Model, point.Effort)
	if err != nil {
		return ModelRadarConfiguration{}, "", err
	}
	if err := validateModelRadarCoreMetrics(point.IQ, point.Passed, point.ValidTasks); err != nil {
		return ModelRadarConfiguration{}, "", fmt.Errorf("invalid configuration %s: %w", key, err)
	}
	if err := validateModelRadarOptionalMetrics(point); err != nil {
		return ModelRadarConfiguration{}, "", fmt.Errorf("invalid configuration %s: %w", key, err)
	}

	var latestGradedAt *int64
	if point.LatestGradedAt != nil && strings.TrimSpace(*point.LatestGradedAt) != "" {
		parsed, parseErr := parseModelRadarTimestamp(*point.LatestGradedAt)
		if parseErr != nil {
			return ModelRadarConfiguration{}, "", fmt.Errorf("invalid configuration %s latest_graded_at: %w", key, parseErr)
		}
		latestGradedAt = &parsed
	}

	passed := int(*point.Passed)
	validTasks := int(*point.ValidTasks)

	return ModelRadarConfiguration{
		Model:                 modelName,
		Effort:                effort,
		IQ:                    *point.IQ,
		Passed:                passed,
		ValidTasks:            validTasks,
		AveragePriceUSD:       point.AveragePriceUSD,
		PriceSamples:          point.PriceSamples,
		AverageMinutes:        point.AverageMinutes,
		DurationSamples:       point.DurationSamples,
		IncompleteCostSamples: point.IncompleteCostSamples,
		TotalRuns:             point.TotalRuns,
		LatestGradedAt:        latestGradedAt,
		AverageAgentSteps:     point.AverageAgentSteps,
		AgentStepsSamples:     point.AgentStepsSamples,
		AverageTotalTokens:    point.AverageTotalTokens,
		TokenSamples:          point.TokenSamples,
		CacheHitRate:          point.CacheHitRate,
		CacheTokenSamples:     point.CacheTokenSamples,
		CombinedCostIndex:     point.CombinedCostIndex,
	}, key, nil
}

func normalizeModelRadarHistoryPoint(point modelRadarUpstreamPoint) (ModelRadarHistoryPoint, string, error) {
	modelName, effort, key, err := validateModelRadarIdentity(point.Model, point.Effort)
	if err != nil {
		return ModelRadarHistoryPoint{}, "", err
	}
	if err := validateModelRadarCoreMetrics(point.IQ, point.Passed, point.ValidTasks); err != nil {
		return ModelRadarHistoryPoint{}, "", fmt.Errorf("invalid history configuration %s: %w", key, err)
	}
	for field, value := range map[string]*float64{
		"average_price_usd":    point.AveragePriceUSD,
		"average_minutes":      point.AverageMinutes,
		"average_agent_steps":  point.AverageAgentSteps,
		"average_total_tokens": point.AverageTotalTokens,
	} {
		if err := validateOptionalFloat(field, value, 0, math.MaxFloat64); err != nil {
			return ModelRadarHistoryPoint{}, "", fmt.Errorf("invalid history configuration %s: %w", key, err)
		}
	}
	if err := validateOptionalFloat("cache_hit_rate", point.CacheHitRate, 0, 1); err != nil {
		return ModelRadarHistoryPoint{}, "", fmt.Errorf("invalid history configuration %s: %w", key, err)
	}

	passed := int(*point.Passed)
	validTasks := int(*point.ValidTasks)

	return ModelRadarHistoryPoint{
		Model:              modelName,
		Effort:             effort,
		IQ:                 *point.IQ,
		Passed:             passed,
		ValidTasks:         validTasks,
		AveragePriceUSD:    point.AveragePriceUSD,
		AverageMinutes:     point.AverageMinutes,
		AverageAgentSteps:  point.AverageAgentSteps,
		AverageTotalTokens: point.AverageTotalTokens,
		CacheHitRate:       point.CacheHitRate,
	}, key, nil
}

func normalizeModelRadarInsights(payload modelRadarInsightsPayload) ([]ModelRadarDegradationAlert, int64, error) {
	if payload.Schema != modelRadarInsightsSchema {
		return nil, 0, errors.New("unsupported insights schema")
	}
	updatedAt, err := parseModelRadarTimestamp(payload.SourceUpdatedAt)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid source_updated_at: %w", err)
	}
	if len(payload.DegradationAlerts.Items) > modelRadarMaxAlerts {
		return nil, 0, errors.New("degradation alert count is out of range")
	}

	alerts := make([]ModelRadarDegradationAlert, 0, len(payload.DegradationAlerts.Items))
	seen := make(map[string]struct{}, len(payload.DegradationAlerts.Items))
	for _, item := range payload.DegradationAlerts.Items {
		modelName, effort, key, identityErr := validateModelRadarIdentity(item.Model, item.Effort)
		if identityErr != nil {
			return nil, 0, identityErr
		}
		if _, exists := seen[key]; exists {
			return nil, 0, fmt.Errorf("duplicate degradation alert %s", key)
		}
		seen[key] = struct{}{}
		if item.IQ == nil {
			return nil, 0, fmt.Errorf("degradation alert %s is missing iq", key)
		}
		if !isFiniteInRange(*item.IQ, 0, 150) {
			return nil, 0, fmt.Errorf("degradation alert %s has invalid iq", key)
		}
		for field, value := range map[string]*float64{
			"degradation_12h_iq": item.Degradation12hIQ,
			"degradation_24h_iq": item.Degradation24hIQ,
			"degradation_48h_iq": item.Degradation48hIQ,
		} {
			if value == nil {
				return nil, 0, fmt.Errorf("degradation alert %s is missing %s", key, field)
			}
			if !isFiniteInRange(*value, -150, 150) {
				return nil, 0, fmt.Errorf("degradation alert %s has invalid %s", key, field)
			}
		}
		alerts = append(alerts, ModelRadarDegradationAlert{
			Model:            modelName,
			Effort:           effort,
			IQ:               *item.IQ,
			Degradation12hIQ: *item.Degradation12hIQ,
			Degradation24hIQ: *item.Degradation24hIQ,
			Degradation48hIQ: *item.Degradation48hIQ,
		})
	}
	return alerts, updatedAt, nil
}

func validateModelRadarIdentity(modelName string, effort string) (string, string, string, error) {
	modelName = strings.TrimSpace(modelName)
	effort = strings.TrimSpace(effort)
	if modelName == "" || effort == "" || len(modelName) > 128 || len(effort) > 32 {
		return "", "", "", errors.New("model and effort must be non-empty and within length limits")
	}
	return modelName, effort, modelName + "|" + effort, nil
}

func validateModelRadarCoreMetrics(iq *float64, passed *float64, validTasks *float64) error {
	if iq == nil || passed == nil || validTasks == nil {
		return errors.New("iq, passed, and valid_tasks are required")
	}
	if !isFiniteInRange(*iq, 0, 150) {
		return errors.New("iq is out of range")
	}
	if !isFiniteInRange(*passed, 0, math.MaxFloat64) || !isFiniteInRange(*validTasks, 0, math.MaxFloat64) {
		return errors.New("passed and valid_tasks must be non-negative finite numbers")
	}
	if *validTasks <= 0 || *passed < 0 || *passed > *validTasks {
		return errors.New("passed and valid_tasks are inconsistent")
	}
	return nil
}

func validateModelRadarOptionalMetrics(point modelRadarUpstreamPoint) error {
	for field, value := range map[string]*float64{
		"average_price_usd":    point.AveragePriceUSD,
		"average_minutes":      point.AverageMinutes,
		"average_agent_steps":  point.AverageAgentSteps,
		"average_total_tokens": point.AverageTotalTokens,
	} {
		if err := validateOptionalFloat(field, value, 0, math.MaxFloat64); err != nil {
			return err
		}
	}
	if err := validateOptionalFloat("cache_hit_rate", point.CacheHitRate, 0, 1); err != nil {
		return err
	}
	if err := validateOptionalFloat("combined_cost_index", point.CombinedCostIndex, 0, 100); err != nil {
		return err
	}
	for field, value := range map[string]*int{
		"price_samples":           point.PriceSamples,
		"duration_samples":        point.DurationSamples,
		"incomplete_cost_samples": point.IncompleteCostSamples,
		"total_runs":              point.TotalRuns,
		"agent_steps_samples":     point.AgentStepsSamples,
		"token_samples":           point.TokenSamples,
		"cache_token_samples":     point.CacheTokenSamples,
	} {
		if value != nil && *value < 0 {
			return fmt.Errorf("%s must not be negative", field)
		}
	}
	return nil
}

func validateOptionalFloat(field string, value *float64, minValue float64, maxValue float64) error {
	if value != nil && !isFiniteInRange(*value, minValue, maxValue) {
		return fmt.Errorf("%s is out of range", field)
	}
	return nil
}

func isFiniteInRange(value float64, minValue float64, maxValue float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minValue && value <= maxValue
}

func parseModelRadarTimestamp(value string) (int64, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return parsed.Unix(), nil
}
