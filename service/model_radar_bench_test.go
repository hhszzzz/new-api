package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	modelRadarTestBenchmark      = "radar-bench-test"
	modelRadarTestBenchCatalog   = "catalog-test"
	modelRadarTestBenchTaskSetID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

var modelRadarTestBenchSourceOrder = []string{"deepswe", "pompeii", "tb4", "science"}

// modelRadarTestBenchBinding builds a binding for the given model|effort
// selections with the four-library task counts of the production contract.
func modelRadarTestBenchBinding(t *testing.T, selections []string) []byte {
	t.Helper()
	efforts := make([]map[string]string, 0, len(selections))
	for _, selection := range selections {
		modelName, effort, found := strings.Cut(selection, "|")
		require.True(t, found, "selection %q must be model|effort", selection)
		efforts = append(efforts, map[string]string{"model": modelName, "effort": effort})
	}
	binding, err := common.Marshal(map[string]any{
		"enabled": true, "production_read_verified": true, "interface_confirmed": true,
		"benchmark": modelRadarTestBenchmark, "catalog_version": modelRadarTestBenchCatalog,
		"task_set_sha256": modelRadarTestBenchTaskSetID, "score_version": modelRadarBenchScoreVersion,
		"score_max": 150, "total_tasks": 64,
		"source_counts": map[string]int{"deepswe": 15, "pompeii": 16, "tb4": 14, "science": 19},
		"model_efforts": efforts,
	})
	require.NoError(t, err)
	return binding
}

// modelRadarTestBenchSummary builds one consistent public summary: coverage is
// distributed over the libraries in order, statistics use one known sample per
// graded run (5 min, $2), and the two-day history ends on the current score.
func modelRadarTestBenchSummary(t *testing.T, selection string, score float64, coverage int) []byte {
	t.Helper()
	modelName, effort, found := strings.Cut(selection, "|")
	require.True(t, found)
	require.LessOrEqual(t, coverage, 64)

	sourceCounts := map[string]int{"deepswe": 15, "pompeii": 16, "tb4": 14, "science": 19}
	remaining := coverage
	sourceScores := map[string]any{}
	for index, source := range modelRadarTestBenchSourceOrder {
		covered := min(sourceCounts[source], remaining)
		remaining -= covered
		row := map[string]any{"coverage": covered, "required_tasks": sourceCounts[source]}
		if covered > 0 {
			row["score"] = score - float64(index)*5
			row["graded_runs_count"] = covered
		} else {
			row["score"] = nil
		}
		sourceScores[source] = row
	}
	if coverage == 0 {
		score = 0
	}
	status := "provisional"
	switch coverage {
	case 0:
		status = "missing_current_result"
	case 64:
		status = "complete"
	}
	historyPoints := []map[string]any{}
	if coverage > 0 {
		previousCoverage := max(coverage-4, 1)
		previousScore := max(score*0.8, 0.5)
		historyPoints = append(historyPoints,
			map[string]any{"at": "2026-10-10T00:00:00Z", "score": previousScore, "coverage": previousCoverage,
				"required_tasks": 64, "score_status": benchTestStatus(previousCoverage)},
			map[string]any{"at": "2026-10-13T00:00:00Z", "score": score, "coverage": coverage,
				"required_tasks": 64, "score_status": status})
	}
	summary := map[string]any{
		"schema": modelRadarBenchSummarySchema, "benchmark": modelRadarTestBenchmark,
		"score_version": modelRadarBenchScoreVersion, "score_max": 150,
		"catalog_version": modelRadarTestBenchCatalog, "task_set_sha256": modelRadarTestBenchTaskSetID,
		"model": modelName, "effort": effort, "coverage": coverage, "required_tasks": 64,
		"score_status": status, "graded_runs_count": coverage,
		"source_counts":          map[string]int{"deepswe": 15, "pompeii": 16, "tb4": 14, "science": 19},
		"source_scores":          sourceScores,
		"scoring_policy_version": "radar-bench-v2-last3-per-task-equal-weight-150",
		"detail_metrics":         modelRadarTestBenchDetailMetrics(coverage),
	}
	if coverage == 0 {
		summary["score"] = nil
		summary["history"] = nil
		summary["history_state"] = "not_provided"
	} else {
		summary["score"] = score
		summary["history"] = map[string]any{
			"schema": "radar-bench-public-history-v1-150-daily", "score_version": modelRadarBenchScoreVersion,
			"score_max": 150, "model": modelName, "effort": effort, "time_zone": "UTC", "bucket": "day",
			"basis":          "last canonical score per UTC grading day; days without grades omitted",
			"updated_bucket": "2026-10-13T00:00:00.000Z", "points": historyPoints,
		}
		summary["history_state"] = "available"
	}
	if coverage > 0 {
		summary["statistics"] = map[string]any{
			"sample_count": coverage,
			"cost_basis":   "official_standard_api_equivalent; no estimates or claimed cash charges",
			"metrics": map[string]any{
				"elapsed_ms": map[string]any{
					"total": 300000.0 * float64(coverage), "mean": 300000.0,
					"known_total": 300000.0 * float64(coverage), "known_samples": coverage,
					"missing_samples": 0, "missing_reason": nil,
				},
				"api_equivalent_usd": map[string]any{
					"total": 2.0 * float64(coverage), "mean": 2.0,
					"known_total": 2.0 * float64(coverage), "known_samples": coverage,
					"missing_samples": 0, "missing_reason": nil,
				},
			},
		}
		summary["statistics_state"] = "available"
	} else {
		summary["statistics"] = nil
		summary["statistics_state"] = "not_provided"
	}
	body, err := common.Marshal(summary)
	require.NoError(t, err)
	return body
}

func benchTestStatus(coverage int) string {
	switch coverage {
	case 0:
		return "missing_current_result"
	case 64:
		return "complete"
	default:
		return "provisional"
	}
}

// modelRadarTestBenchDetailMetrics builds a v6 detail block: two more valid
// grade events than the current scoring samples, two recent grades, and one
// complete sample per event for every token aggregate.
func modelRadarTestBenchDetailMetrics(coverage int) map[string]any {
	detail := map[string]any{
		"valid_grade_events": coverage + 2,
		"latest_graded_at":   "2026-10-13T00:00:00.000Z",
		"as_of":              "2026-10-13T01:00:00.000Z",
		"graded_24h":         1,
		"graded_48h":         2,
	}
	if coverage == 0 {
		detail["valid_grade_events"] = 0
		detail["latest_graded_at"] = nil
		detail["graded_24h"] = 0
		detail["graded_48h"] = 0
		detail["metrics"] = nil
		detail["metrics_state"] = "not_provided"
		return detail
	}
	events := coverage + 2
	metric := func(totalPerSample float64) map[string]any {
		return map[string]any{
			"known_total":     totalPerSample * float64(events),
			"known_samples":   events,
			"missing_samples": 0,
		}
	}
	detail["metrics"] = map[string]any{
		"total_tokens":       metric(1000),
		"input_tokens":       metric(800),
		"output_tokens":      metric(200),
		"cache_tokens":       metric(600),
		"reasoning_tokens":   metric(50),
		"cache_write_tokens": metric(0),
		"turn_count":         metric(3),
	}
	detail["metrics_state"] = "available"
	return detail
}

type modelRadarBenchTestServerOptions struct {
	selections []string
	// intercept may short-circuit one request (key "" targets the binding);
	// returning true means the response has been written.
	intercept func(w http.ResponseWriter, r *http.Request, key string) bool
	// records the selection header seen on each score request.
	headers   *[]string
	headersMu *sync.Mutex
}

// newModelRadarBenchTestServer serves the bench binding and per-selection
// summaries on one server.
func newModelRadarBenchTestServer(t *testing.T, options modelRadarBenchTestServerOptions) *httptest.Server {
	t.Helper()
	binding := modelRadarTestBenchBinding(t, options.selections)
	summaries := make(map[string][]byte)
	summaryMu := sync.Mutex{}
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/bench-binding":
			if options.intercept != nil && options.intercept(writer, request, "") {
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(binding)
			return
		case "/bench-score":
			modelName := request.URL.Query().Get("model")
			effort := request.URL.Query().Get("effort")
			key := modelName + "|" + effort
			if options.headers != nil {
				options.headersMu.Lock()
				*options.headers = append(*options.headers, request.Header.Get(modelRadarBenchSelectionHeader))
				options.headersMu.Unlock()
			}
			if options.intercept != nil && options.intercept(writer, request, key) {
				return
			}
			summaryMu.Lock()
			body, ok := summaries[key]
			if !ok {
				body = modelRadarTestBenchSummary(t, key, 100, 30)
				summaries[key] = body
			}
			summaryMu.Unlock()
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(body)
			return
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
}

func modelRadarBenchTestSources(server *httptest.Server) modelRadarSources {
	return modelRadarSources{
		BenchBinding: server.URL + "/bench-binding",
		BenchScore:   server.URL + "/bench-score",
	}
}

func modelRadarBenchTestFloat64(value float64) *float64 { return &value }

func TestFetchModelRadarBenchReadsEverySelection(t *testing.T) {
	var headers []string
	var headersMu sync.Mutex
	server := newModelRadarBenchTestServer(t, modelRadarBenchTestServerOptions{
		selections: []string{"gpt-test|high", "gpt-test|low"},
		headers:    &headers, headersMu: &headersMu,
	})
	defer server.Close()

	bench, err := fetchModelRadarBench(context.Background(), server.Client(), modelRadarBenchTestSources(server), nil, 1_700_000_000)

	require.NoError(t, err)
	assert.Equal(t, modelRadarTestBenchmark, bench.Benchmark)
	assert.Equal(t, modelRadarBenchScoreVersion, bench.ScoreVersion)
	assert.Equal(t, 150, bench.ScoreMax)
	assert.Equal(t, 2, bench.TotalCount)
	assert.Equal(t, 2, bench.LoadedCount)
	assert.Empty(t, bench.FailedCells)
	assert.Equal(t, int64(1_700_000_000), bench.ReadAt, "a full round stamps the read time")
	require.Len(t, bench.Cells, 2)

	cell := bench.Cells[0]
	assert.Equal(t, "gpt-test", cell.Model)
	assert.Equal(t, "high", cell.Effort)
	require.NotNil(t, cell.Score)
	assert.Equal(t, 100.0, *cell.Score)
	assert.Equal(t, 30, cell.Coverage)
	assert.Equal(t, 64, cell.RequiredTasks)
	assert.Equal(t, "provisional", cell.ScoreStatus)
	assert.Equal(t, int64(1_700_000_000), cell.ReadAt)
	require.NotNil(t, cell.SourceScores["deepswe"].Score)
	assert.Equal(t, 100.0, *cell.SourceScores["deepswe"].Score)
	assert.Equal(t, 15, cell.SourceScores["deepswe"].Coverage)
	assert.Nil(t, cell.SourceScores["science"].Score, "an uncovered library has no score")
	require.NotNil(t, cell.Price.Mean)
	assert.Equal(t, 2.0, *cell.Price.Mean)
	require.NotNil(t, cell.Duration.Mean)
	assert.Equal(t, 5.0, *cell.Duration.Mean, "elapsed_ms converts to minutes")
	require.Len(t, cell.History, 2)
	assert.Equal(t, int64(1_791_849_600), cell.History[1].Ts)
	assert.Equal(t, int64(1_791_849_600), cell.LastGradedDay)
	assert.Equal(t, "radar-bench-v2-last3-per-task-equal-weight-150", cell.ScoringPolicyVersion)
	require.NotNil(t, cell.DetailMetrics, "v6 summaries carry grading-activity detail")
	assert.Equal(t, 32, cell.DetailMetrics.ValidGradeEvents)
	assert.Equal(t, 1, cell.DetailMetrics.Graded24h)
	assert.Equal(t, 2, cell.DetailMetrics.Graded48h)
	require.NotNil(t, cell.DetailMetrics.LatestGradedAt)
	assert.Equal(t, "available", cell.DetailMetrics.MetricsState)
	totalTokens := cell.DetailMetrics.Metrics["total_tokens"]
	assert.Equal(t, 32, totalTokens.KnownSamples)
	assert.Equal(t, 32, totalTokens.TotalSamples)
	require.NotNil(t, totalTokens.Mean)
	assert.Equal(t, 1000.0, *totalTokens.Mean)

	headersMu.Lock()
	defer headersMu.Unlock()
	require.Len(t, headers, 2)
	for _, header := range headers {
		assert.Equal(t, modelRadarBenchSelectionValue, header, "every read must carry the explicit selection marker")
	}
}

func TestFetchModelRadarBenchRetriesOneBusyRead(t *testing.T) {
	busy := map[string]int{"gpt-test|high": 1}
	var busyMu sync.Mutex
	server := newModelRadarBenchTestServer(t, modelRadarBenchTestServerOptions{
		selections: []string{"gpt-test|high"},
		intercept: func(writer http.ResponseWriter, _ *http.Request, key string) bool {
			busyMu.Lock()
			defer busyMu.Unlock()
			if busy[key] > 0 {
				busy[key]--
				writer.WriteHeader(http.StatusServiceUnavailable)
				_, _ = writer.Write([]byte(`{"code":"public_read_busy"}`))
				return true
			}
			return false
		},
	})
	defer server.Close()

	bench, err := fetchModelRadarBench(context.Background(), server.Client(), modelRadarBenchTestSources(server), nil, 1_700_000_000)

	require.NoError(t, err)
	assert.Equal(t, 1, bench.LoadedCount, "the second attempt must succeed")
	assert.Empty(t, bench.FailedCells)
}

func TestFetchModelRadarBenchKeepsPreviousCellsOnPartialRound(t *testing.T) {
	previous := &ModelRadarBench{
		Benchmark: modelRadarTestBenchmark, CatalogVersion: modelRadarTestBenchCatalog,
		TaskSetSHA256: modelRadarTestBenchTaskSetID, ReadAt: 111,
		Cells: []ModelRadarBenchCell{{
			Model: "gpt-test", Effort: "low", Score: modelRadarBenchTestFloat64(42), Coverage: 20,
			RequiredTasks: 64, ScoreStatus: "provisional", ReadAt: 111,
		}},
	}
	server := newModelRadarBenchTestServer(t, modelRadarBenchTestServerOptions{
		selections: []string{"gpt-test|high", "gpt-test|low"},
		intercept: func(writer http.ResponseWriter, _ *http.Request, key string) bool {
			if key == "gpt-test|low" {
				writer.WriteHeader(http.StatusServiceUnavailable)
				_, _ = writer.Write([]byte(`{"code":"public_read_busy"}`))
				return true
			}
			return false
		},
	})
	defer server.Close()

	bench, err := fetchModelRadarBench(context.Background(), server.Client(), modelRadarBenchTestSources(server), previous, 1_700_000_000)

	require.NoError(t, err)
	assert.Equal(t, 1, bench.LoadedCount)
	assert.Equal(t, 2, bench.TotalCount)
	assert.Equal(t, []string{"gpt-test|low"}, bench.FailedCells)
	assert.Equal(t, int64(111), bench.ReadAt, "a partial round must not stamp the read time")
	require.Len(t, bench.Cells, 2)
	assert.Equal(t, int64(1_700_000_000), bench.Cells[0].ReadAt, "the fresh cell carries this round's time")
	assert.Equal(t, int64(111), bench.Cells[1].ReadAt, "the failed cell carries the previous round's time")
	require.NotNil(t, bench.Cells[1].Score)
	assert.Equal(t, 42.0, *bench.Cells[1].Score)
}

func TestFetchModelRadarBenchDropsPreviousCellsOnRebind(t *testing.T) {
	previous := &ModelRadarBench{
		Benchmark: "radar-bench-old", CatalogVersion: modelRadarTestBenchCatalog,
		TaskSetSHA256: modelRadarTestBenchTaskSetID, ReadAt: 111,
		Cells: []ModelRadarBenchCell{{Model: "gpt-test", Effort: "low", Score: modelRadarBenchTestFloat64(42), Coverage: 20, RequiredTasks: 64, ScoreStatus: "provisional", ReadAt: 111}},
	}
	server := newModelRadarBenchTestServer(t, modelRadarBenchTestServerOptions{
		selections: []string{"gpt-test|low"},
		intercept: func(writer http.ResponseWriter, _ *http.Request, key string) bool {
			if key == "gpt-test|low" {
				writer.WriteHeader(http.StatusServiceUnavailable)
				return true
			}
			return false
		},
	})
	defer server.Close()

	_, err := fetchModelRadarBench(context.Background(), server.Client(), modelRadarBenchTestSources(server), previous, 1_700_000_000)

	require.ErrorContains(t, err, "no readable selection", "cells from another benchmark generation must not be carried")
}

func TestNormalizeModelRadarBenchCellRejectsInconsistentSummaries(t *testing.T) {
	var bindingPayload modelRadarBenchBindingPayload
	require.NoError(t, common.Unmarshal(modelRadarTestBenchBinding(t, []string{"gpt-test|high"}), &bindingPayload))
	binding, err := validateModelRadarBenchBinding(bindingPayload)
	require.NoError(t, err)
	selection := modelRadarBenchSelection{Model: "gpt-test", Effort: "high"}
	fresh := func(t *testing.T) modelRadarBenchSummaryPayload {
		t.Helper()
		var payload modelRadarBenchSummaryPayload
		require.NoError(t, common.Unmarshal(modelRadarTestBenchSummary(t, "gpt-test|high", 100, 30), &payload))
		return payload
	}
	score := func(value float64) *float64 { return &value }
	integer := func(value int) *int { return &value }

	for _, test := range []struct {
		name   string
		change func(*modelRadarBenchSummaryPayload)
		want   string
	}{
		{"wrong schema", func(p *modelRadarBenchSummaryPayload) { p.Schema = "radar-bench-public-summary-v4-150-daily" }, "schema"},
		{"wrong benchmark", func(p *modelRadarBenchSummaryPayload) { p.Benchmark = "other" }, "binding"},
		{"wrong selection", func(p *modelRadarBenchSummaryPayload) { p.Effort = "low" }, "selection"},
		{"status differs from coverage", func(p *modelRadarBenchSummaryPayload) { p.ScoreStatus = "complete" }, "status"},
		{"graded runs out of range", func(p *modelRadarBenchSummaryPayload) { p.GradedRunsCount = integer(29) }, "graded runs"},
		{"source coverage sum differs", func(p *modelRadarBenchSummaryPayload) {
			row := p.SourceScores["deepswe"]
			row.Coverage = 14
			p.SourceScores["deepswe"] = row
		}, "add up"},
		{"source score without coverage", func(p *modelRadarBenchSummaryPayload) {
			row := p.SourceScores["science"]
			row.Score = score(50)
			p.SourceScores["science"] = row
		}, "source score"},
		{"history final day differs", func(p *modelRadarBenchSummaryPayload) { p.History.Points[1].Score = score(99) }, "final graded day"},
		{"statistics samples inconsistent", func(p *modelRadarBenchSummaryPayload) {
			metric := p.Statistics.Metrics["api_equivalent_usd"]
			metric.MissingSamples = integer(1)
			p.Statistics.Metrics["api_equivalent_usd"] = metric
		}, "samples"},
		{"statistics state with no payload", func(p *modelRadarBenchSummaryPayload) { p.Statistics = nil }, "statistics"},
		{"history state with no payload", func(p *modelRadarBenchSummaryPayload) { p.History = nil }, "history"},
		{"missing scoring policy", func(p *modelRadarBenchSummaryPayload) { p.ScoringPolicyVersion = "" }, "policy"},
		{"detail events fewer than graded runs", func(p *modelRadarBenchSummaryPayload) {
			events := 29
			p.DetailMetrics.ValidGradeEvents = &events
		}, "fewer than graded runs"},
		{"detail read time missing", func(p *modelRadarBenchSummaryPayload) { p.DetailMetrics.AsOf = nil }, "read time"},
		{"detail grade time differs from events", func(p *modelRadarBenchSummaryPayload) { p.DetailMetrics.LatestGradedAt = nil }, "latest grade time"},
		{"detail recent counts out of range", func(p *modelRadarBenchSummaryPayload) {
			recent := 3
			p.DetailMetrics.Graded24h = &recent
		}, "out of range"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := fresh(t)
			test.change(&payload)
			_, err := normalizeModelRadarBenchCell(payload, binding, selection, 1)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestNormalizeModelRadarBenchCellAcceptsNextAdditiveRevision(t *testing.T) {
	var bindingPayload modelRadarBenchBindingPayload
	require.NoError(t, common.Unmarshal(modelRadarTestBenchBinding(t, []string{"gpt-test|high"}), &bindingPayload))
	binding, err := validateModelRadarBenchBinding(bindingPayload)
	require.NoError(t, err)
	var payload modelRadarBenchSummaryPayload
	require.NoError(t, common.Unmarshal(modelRadarTestBenchSummary(t, "gpt-test|high", 100, 30), &payload))
	// A later revision that keeps the documented fields must not freeze the
	// board the way the v5 exact-string gate did.
	payload.Schema = "radar-bench-public-summary-v7-150-details"

	_, err = normalizeModelRadarBenchCell(payload, binding, modelRadarBenchSelection{Model: "gpt-test", Effort: "high"}, 1)

	require.NoError(t, err)
}

func TestNormalizeModelRadarBenchCellDegradesInvalidDetailStatistics(t *testing.T) {
	var bindingPayload modelRadarBenchBindingPayload
	require.NoError(t, common.Unmarshal(modelRadarTestBenchBinding(t, []string{"gpt-test|high"}), &bindingPayload))
	binding, err := validateModelRadarBenchBinding(bindingPayload)
	require.NoError(t, err)
	var payload modelRadarBenchSummaryPayload
	require.NoError(t, common.Unmarshal(modelRadarTestBenchSummary(t, "gpt-test|high", 100, 30), &payload))
	// One token aggregate no longer adds up to the valid grade population.
	metric := payload.DetailMetrics.Metrics["cache_tokens"]
	missing := 1
	metric.MissingSamples = &missing
	payload.DetailMetrics.Metrics["cache_tokens"] = metric

	cell, err := normalizeModelRadarBenchCell(payload, binding, modelRadarBenchSelection{Model: "gpt-test", Effort: "high"}, 1)

	require.NoError(t, err, "broken token aggregates must not reject the score")
	require.NotNil(t, cell.DetailMetrics)
	assert.Equal(t, "invalid_source_statistics", cell.DetailMetrics.MetricsState)
	assert.Empty(t, cell.DetailMetrics.Metrics)
	assert.Equal(t, 32, cell.DetailMetrics.ValidGradeEvents, "activity counts survive the metrics failure")
	require.NotNil(t, cell.Score)
	assert.Equal(t, 100.0, *cell.Score)
}

func TestFetchModelRadarBenchUsesPartialSampleMean(t *testing.T) {
	var payload modelRadarBenchSummaryPayload
	require.NoError(t, common.Unmarshal(modelRadarTestBenchSummary(t, "gpt-test|high", 100, 30), &payload))
	// One sample lacks the price measurement: upstream nulls total/mean but
	// keeps the known aggregates, and the dashboard shows the known mean.
	metric := payload.Statistics.Metrics["api_equivalent_usd"]
	known := 29
	missing := 1
	total := 58.0
	metric.KnownSamples = &known
	metric.MissingSamples = &missing
	metric.KnownTotal = &total
	payload.Statistics.Metrics["api_equivalent_usd"] = metric

	price, _, err := normalizeModelRadarBenchStatistics(payload)

	require.NoError(t, err)
	require.NotNil(t, price.Mean)
	assert.Equal(t, 2.0, *price.Mean)
	require.NotNil(t, price.KnownSamples)
	require.NotNil(t, price.TotalSamples)
	assert.Equal(t, 29, *price.KnownSamples)
	assert.Equal(t, 30, *price.TotalSamples)
}

func TestSyncModelRadarPersistsAndCarriesBenchSection(t *testing.T) {
	setupModelRadarServiceTestDB(t)
	var headers []string
	var headersMu sync.Mutex
	bindingOnlyFailure := false
	server := newModelRadarBenchTestServer(t, modelRadarBenchTestServerOptions{
		selections: []string{"gpt-test|high", "gpt-test|low"},
		headers:    &headers, headersMu: &headersMu,
		intercept: func(writer http.ResponseWriter, _ *http.Request, key string) bool {
			if bindingOnlyFailure && key == "" {
				writer.WriteHeader(http.StatusInternalServerError)
				return true
			}
			return false
		},
	})
	defer server.Close()
	client := server.Client()
	sources := modelRadarBenchTestSources(server)

	first, err := syncModelRadar(context.Background(), client, sources, nil, 2_000_000_000)
	require.NoError(t, err)
	assert.Equal(t, 2, first.ConfigurationCount)

	data, err := GetModelRadar(context.Background())
	require.NoError(t, err)
	require.NotNil(t, data.Bench)
	assert.Equal(t, 2, data.Bench.LoadedCount)
	assert.Equal(t, int64(2_000_000_000), data.Bench.ReadAt)

	// The binding source goes down: the sync fails and leaves the previous
	// snapshot in place instead of erasing it.
	bindingOnlyFailure = true
	previous := loadPreviousModelRadarData(context.Background())
	require.NotNil(t, previous)
	_, err = syncModelRadar(context.Background(), client, sources, previous, 2_000_060_000)
	require.ErrorContains(t, err, "bench binding")
	data, err = GetModelRadar(context.Background())
	require.NoError(t, err)
	require.NotNil(t, data.Bench)
	assert.Equal(t, int64(2_000_000_000), data.Bench.ReadAt, "a failed round keeps the previous section")
	assert.Equal(t, 2, data.Bench.LoadedCount)
}
