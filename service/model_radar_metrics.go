package service

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// The live DeepSWE API uses total/runs_total, unlike the published history
// snapshot's valid_tasks/total_runs. Keep that translation at the source boundary.
func normalizeModelRadarMetrics(payload modelRadarMetricsPayload, published []ModelRadarConfiguration) ([]ModelRadarConfiguration, ModelRadarHistoryFrame, error) {
	if payload.Schema != 3 || payload.Mode != "equal_latest_3" || payload.BenchmarkID != "deep-swe" || payload.ScoringMode != "binary-majority" {
		return nil, ModelRadarHistoryFrame{}, errors.New("unsupported live metrics schema or benchmark")
	}
	updatedAt, err := parseModelRadarTimestamp(payload.SourceUpdatedAt)
	if err != nil {
		return nil, ModelRadarHistoryFrame{}, fmt.Errorf("invalid source_updated_at: %w", err)
	}
	if len(payload.Points) == 0 || len(payload.Points) > modelRadarMaxConfigurations {
		return nil, ModelRadarHistoryFrame{}, errors.New("configuration count is out of range")
	}
	// The live API sends raw cost indices; the published/UI contract scales
	// the largest index in the comparable cohort to 100.
	var maxCost float64
	for _, point := range payload.Points {
		if err := validateOptionalFloat("combined_cost_index", point.CombinedCostIndex, 0, math.MaxFloat64); err != nil {
			return nil, ModelRadarHistoryFrame{}, err
		}
		if point.CombinedCostIndex != nil && *point.CombinedCostIndex > maxCost {
			maxCost = *point.CombinedCostIndex
		}
	}
	harnesses := make(map[[2]string]string, len(published))
	for _, configuration := range published {
		harnesses[[2]string{configuration.Model, configuration.Effort}] = configuration.Harness
	}
	configurations := make([]ModelRadarConfiguration, 0, len(payload.Points))
	frame := ModelRadarHistoryFrame{Ts: updatedAt, Points: make([]ModelRadarHistoryPoint, 0, len(payload.Points))}
	seen := make(map[string]struct{}, len(payload.Points))
	for _, metric := range payload.Points {
		point := metric.modelRadarUpstreamPoint
		point.ValidTasks = metric.Total
		point.TotalRuns = metric.RunsTotal
		if point.CombinedCostIndex != nil && maxCost > 0 {
			normalizedCost := *point.CombinedCostIndex / maxCost * 100
			point.CombinedCostIndex = &normalizedCost
		}
		if point.LatestGradedAt == nil {
			point.LatestGradedAt = metric.SourceUpdatedAt
		}
		if strings.TrimSpace(point.Harness) == "" {
			point.Harness = harnesses[[2]string{strings.TrimSpace(point.Model), strings.ToLower(strings.TrimSpace(point.Effort))}]
		}
		configuration, key, err := normalizeModelRadarConfiguration(point)
		if err != nil {
			return nil, ModelRadarHistoryFrame{}, err
		}
		if _, exists := seen[key]; exists {
			return nil, ModelRadarHistoryFrame{}, fmt.Errorf("duplicate configuration %s", key)
		}
		seen[key] = struct{}{}
		historyPoint, _, err := normalizeModelRadarHistoryPoint(point)
		if err != nil {
			return nil, ModelRadarHistoryFrame{}, err
		}
		configurations = append(configurations, configuration)
		frame.Points = append(frame.Points, historyPoint)
	}
	return configurations, frame, nil
}
