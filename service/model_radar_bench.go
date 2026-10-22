package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// The October 2026 station refactor moved the live radar to the RadarBench
// system: a fixed four-library task set graded per model/effort selection and
// published by the site as one public summary document per selection. The site
// later repositioned the 64-task board as its older benchmark, but it remains
// the only machine-readable multi-tier score feed, so it is still the
// authoritative capability source we consume. The legacy
// intelligence-efficiency pipeline is a frozen archive.
const (
	modelRadarBenchBindingURL = "https://codexradar.com/data/radar-bench-binding.json"
	modelRadarBenchScoreURL   = "https://codexradar.com/api/radar-bench-score"
	// The public summary endpoint requires an explicit selection marker; a
	// request without one is rejected with HTTP 428. The dashboard sends the
	// same header for user reads.
	modelRadarBenchSelectionHeader = "X-Radar-Bench-Selection"
	modelRadarBenchSelectionValue  = "user-selected-v1"
	// Public summaries are versioned additively: v6 added detail metrics and a
	// scoring policy without changing the score contract. Accept every revision
	// at or above the oldest supported one so the next revision cannot reject
	// every selection the way an exact-string match did for v6.
	modelRadarBenchSummarySchemaPrefix     = "radar-bench-public-summary-v"
	modelRadarBenchSummarySchema           = "radar-bench-public-summary-v6-150-details"
	modelRadarBenchSummaryMinSchemaVersion = 5
	modelRadarBenchScoreVersion            = "radar-bench-v2-150"
	modelRadarBenchScoreMax                = 150.0
	// The published score is the 0-100 base score times this display multiplier.
	// Auto-effort divides it back out so user-configured IQ thresholds keep their
	// 0-100 meaning.
	modelRadarBenchDisplayMultiplier = 1.5
	modelRadarBenchTaskCount         = 64
	modelRadarBenchMaxCells          = 64
	modelRadarBenchMaxHistoryPoints  = 512
	modelRadarBenchBindingMaxBytes   = 1 << 20
	modelRadarBenchSummaryMaxBytes   = 1 << 20
	// The dashboard reads selections through a bounded pool and retries a busy
	// source once; the origin also answers 503 public_read_busy while its
	// statistics aggregation is saturated, so a single quiet retry matters.
	modelRadarBenchCellTimeout = 20 * time.Second
	modelRadarBenchConcurrency = 3
	modelRadarBenchRetryDelay  = 2 * time.Second
	// One sync round may take a couple of minutes when the origin throttles
	// reads; the system task runs every 10 minutes, so this stays well clear.
	modelRadarBenchSyncTimeout = 3 * time.Minute
)

// Detail aggregates repeat the same sample accounting as the cell statistics.
const (
	modelRadarBenchDetailAvailable = "available"
	modelRadarBenchDetailNotGiven  = "not_provided"
	modelRadarBenchDetailInvalid   = "invalid_source_statistics"
)

// modelRadarBenchDetailMetricNames is the published detail aggregate set and
// the order the frontend renders it in.
var modelRadarBenchDetailMetricNames = []string{
	"total_tokens",
	"input_tokens",
	"output_tokens",
	"cache_tokens",
	"reasoning_tokens",
	"cache_write_tokens",
	"turn_count",
}

// ModelRadarBenchMetricStat is one observed aggregate of a bench cell. Mean is
// computed from the known samples only, mirroring the dashboard: when any
// sample lacks the measurement, upstream nulls its total/mean and the page
// shows a partial-sample mean instead.
type ModelRadarBenchMetricStat struct {
	Mean         *float64 `json:"mean"`
	KnownSamples *int     `json:"known_samples"`
	TotalSamples *int     `json:"total_samples"`
}

// ModelRadarBenchDetailMetric is one token or turn aggregate over all retained
// valid grades of the selection, not just the three grades the score uses.
type ModelRadarBenchDetailMetric struct {
	Mean         *float64 `json:"mean"`
	KnownSamples int      `json:"known_samples"`
	TotalSamples int      `json:"total_samples"`
}

// ModelRadarBenchDetailMetrics is the v6 grading-activity and usage detail.
// MetricsState is "available", "not_provided", or "invalid_source_statistics";
// Metrics is empty unless it is "available".
type ModelRadarBenchDetailMetrics struct {
	ValidGradeEvents int    `json:"valid_grade_events"`
	LatestGradedAt   *int64 `json:"latest_graded_at"`
	AsOf             int64  `json:"as_of"`
	Graded24h        int    `json:"graded_24h"`
	Graded48h        int    `json:"graded_48h"`
	// MetricsState reports whether the token aggregates below are usable.
	MetricsState string `json:"metrics_state"`
	// Metrics is keyed by modelRadarBenchDetailMetricNames.
	Metrics map[string]ModelRadarBenchDetailMetric `json:"metrics,omitempty"`
}

type ModelRadarBenchSourceScore struct {
	Score           *float64 `json:"score"`
	Coverage        int      `json:"coverage"`
	RequiredTasks   int      `json:"required_tasks"`
	GradedRunsCount *int     `json:"graded_runs_count"`
}

// ModelRadarBenchHistoryPoint is one UTC grading day: the last canonical
// cumulative score of that day. Days without grades are omitted upstream.
type ModelRadarBenchHistoryPoint struct {
	Ts          int64    `json:"ts"`
	Score       *float64 `json:"score"`
	Coverage    int      `json:"coverage"`
	ScoreStatus string   `json:"score_status"`
}

// ModelRadarBenchCell is one bound model/effort selection of the live
// RadarBench. ReadAt records when this cell's values were actually read, so a
// cell that survived a partial round from an earlier sync stays identifiable.
type ModelRadarBenchCell struct {
	Model           string                                `json:"model"`
	Effort          string                                `json:"effort"`
	Score           *float64                              `json:"score"`
	Coverage        int                                   `json:"coverage"`
	RequiredTasks   int                                   `json:"required_tasks"`
	ScoreStatus     string                                `json:"score_status"`
	GradedRunsCount *int                                  `json:"graded_runs_count"`
	SourceScores    map[string]ModelRadarBenchSourceScore `json:"source_scores"`
	// Price is the official-standard API-equivalent cost of one grading run
	// (not a subscription charge); Duration is its mean wall-clock minutes.
	Price           ModelRadarBenchMetricStat `json:"price"`
	Duration        ModelRadarBenchMetricStat `json:"duration"`
	StatisticsState string                    `json:"statistics_state"`
	// ScoringPolicyVersion identifies how the score was aggregated. It is empty
	// in pre-v6 summaries.
	ScoringPolicyVersion string `json:"scoring_policy_version,omitempty"`
	// DetailMetrics is the v6 grading-activity and token usage detail; nil when
	// an older summary did not publish it.
	DetailMetrics *ModelRadarBenchDetailMetrics `json:"detail_metrics"`
	HistoryState  string                        `json:"history_state"`
	History       []ModelRadarBenchHistoryPoint `json:"history"`
	LastGradedDay int64                         `json:"last_graded_day"`
	ReadAt        int64                         `json:"read_at"`
}

// ModelRadarBench is the live RadarBench section of the radar snapshot.
// ReadAt is the time of the last round that read every bound selection; a
// partial round leaves it unchanged and reports the failures.
type ModelRadarBench struct {
	Benchmark      string                `json:"benchmark"`
	CatalogVersion string                `json:"catalog_version"`
	TaskSetSHA256  string                `json:"task_set_sha256"`
	ScoreVersion   string                `json:"score_version"`
	ScoreMax       int                   `json:"score_max"`
	SourceCounts   map[string]int        `json:"source_counts"`
	ReadAt         int64                 `json:"read_at"`
	LoadedCount    int                   `json:"loaded_count"`
	TotalCount     int                   `json:"total_count"`
	FailedCells    []string              `json:"failed_cells"`
	Cells          []ModelRadarBenchCell `json:"cells"`
}

type modelRadarBenchSelection struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type modelRadarBenchBindingPayload struct {
	Enabled                bool                       `json:"enabled"`
	ProductionReadVerified bool                       `json:"production_read_verified"`
	InterfaceConfirmed     bool                       `json:"interface_confirmed"`
	Benchmark              string                     `json:"benchmark"`
	CatalogVersion         string                     `json:"catalog_version"`
	TaskSetSHA256          string                     `json:"task_set_sha256"`
	ScoreVersion           string                     `json:"score_version"`
	ScoreMax               float64                    `json:"score_max"`
	TotalTasks             int                        `json:"total_tasks"`
	SourceCounts           map[string]int             `json:"source_counts"`
	ModelEfforts           []modelRadarBenchSelection `json:"model_efforts"`
}

type modelRadarBenchBinding struct {
	benchmark      string
	catalogVersion string
	taskSetSHA256  string
	sourceCounts   map[string]int
	selections     []modelRadarBenchSelection
}

type modelRadarBenchSourceScorePayload struct {
	Score           *float64 `json:"score"`
	Coverage        int      `json:"coverage"`
	RequiredTasks   int      `json:"required_tasks"`
	GradedRunsCount *int     `json:"graded_runs_count"`
}

type modelRadarBenchMetricPayload struct {
	KnownTotal     *float64 `json:"known_total"`
	KnownSamples   *int     `json:"known_samples"`
	MissingSamples *int     `json:"missing_samples"`
}

type modelRadarBenchStatisticsPayload struct {
	SampleCount int                                     `json:"sample_count"`
	Metrics     map[string]modelRadarBenchMetricPayload `json:"metrics"`
}

type modelRadarBenchHistoryPointPayload struct {
	At            string   `json:"at"`
	Score         *float64 `json:"score"`
	Coverage      int      `json:"coverage"`
	RequiredTasks int      `json:"required_tasks"`
	ScoreStatus   string   `json:"score_status"`
}

type modelRadarBenchHistoryPayload struct {
	TimeZone      string                               `json:"time_zone"`
	Bucket        string                               `json:"bucket"`
	UpdatedBucket *string                              `json:"updated_bucket"`
	Points        []modelRadarBenchHistoryPointPayload `json:"points"`
}

type modelRadarBenchSummaryPayload struct {
	Schema          string                                       `json:"schema"`
	Benchmark       string                                       `json:"benchmark"`
	ScoreVersion    string                                       `json:"score_version"`
	ScoreMax        float64                                      `json:"score_max"`
	CatalogVersion  string                                       `json:"catalog_version"`
	TaskSetSHA256   string                                       `json:"task_set_sha256"`
	Model           string                                       `json:"model"`
	Effort          string                                       `json:"effort"`
	Score           *float64                                     `json:"score"`
	Coverage        int                                          `json:"coverage"`
	RequiredTasks   int                                          `json:"required_tasks"`
	ScoreStatus     string                                       `json:"score_status"`
	GradedRunsCount *int                                         `json:"graded_runs_count"`
	SourceCounts    map[string]int                               `json:"source_counts"`
	SourceScores    map[string]modelRadarBenchSourceScorePayload `json:"source_scores"`
	// ScoringPolicyVersion and DetailMetrics arrived with schema v6.
	ScoringPolicyVersion string                               `json:"scoring_policy_version"`
	DetailMetrics        *modelRadarBenchDetailMetricsPayload `json:"detail_metrics"`
	Statistics           *modelRadarBenchStatisticsPayload    `json:"statistics"`
	StatisticsState      string                               `json:"statistics_state"`
	History              *modelRadarBenchHistoryPayload       `json:"history"`
	HistoryState         string                               `json:"history_state"`
}

type modelRadarBenchDetailMetricPayload struct {
	KnownTotal     *float64 `json:"known_total"`
	KnownSamples   *int     `json:"known_samples"`
	MissingSamples *int     `json:"missing_samples"`
}

type modelRadarBenchDetailMetricsPayload struct {
	ValidGradeEvents *int                                          `json:"valid_grade_events"`
	LatestGradedAt   *string                                       `json:"latest_graded_at"`
	AsOf             *string                                       `json:"as_of"`
	Graded24h        *int                                          `json:"graded_24h"`
	Graded48h        *int                                          `json:"graded_48h"`
	Metrics          map[string]modelRadarBenchDetailMetricPayload `json:"metrics"`
	MetricsState     string                                        `json:"metrics_state"`
}

// fetchModelRadarBench reads the authoritative binding and every bound
// selection's public summary. A selection that cannot be read keeps the
// previous snapshot's cell while the binding identity still matches, matching
// the dashboard's "refresh failed; previous data retained" behavior. A nil
// result with a non-nil error means no usable bench data at all (binding
// unavailable or changed without any fresh read); callers keep the previous
// section in that case.
func fetchModelRadarBench(ctx context.Context, client *http.Client, sources modelRadarSources, previous *ModelRadarBench, fetchedAt int64) (*ModelRadarBench, error) {
	var bindingPayload modelRadarBenchBindingPayload
	if err := fetchModelRadarJSON(ctx, client, sources.BenchBinding, modelRadarBenchBindingMaxBytes, &bindingPayload); err != nil {
		return nil, fmt.Errorf("bench binding: %w", err)
	}
	binding, err := validateModelRadarBenchBinding(bindingPayload)
	if err != nil {
		return nil, fmt.Errorf("bench binding: %w", err)
	}

	read, failed := readModelRadarBenchCells(ctx, client, sources.BenchScore, binding, fetchedAt)

	bench := &ModelRadarBench{
		Benchmark:      binding.benchmark,
		CatalogVersion: binding.catalogVersion,
		TaskSetSHA256:  binding.taskSetSHA256,
		ScoreVersion:   modelRadarBenchScoreVersion,
		ScoreMax:       int(modelRadarBenchScoreMax),
		SourceCounts:   binding.sourceCounts,
		LoadedCount:    len(read),
		TotalCount:     len(binding.selections),
		FailedCells:    failed,
	}
	// The previous section only stands in while it describes the same benchmark
	// generation; a rebind resets every carried cell.
	var carried map[string]ModelRadarBenchCell
	if previous != nil && previous.Benchmark == binding.benchmark &&
		previous.CatalogVersion == binding.catalogVersion && previous.TaskSetSHA256 == binding.taskSetSHA256 {
		carried = make(map[string]ModelRadarBenchCell, len(previous.Cells))
		for _, cell := range previous.Cells {
			carried[cell.Model+"|"+cell.Effort] = cell
		}
		bench.ReadAt = previous.ReadAt
	}
	bench.Cells = make([]ModelRadarBenchCell, 0, len(binding.selections))
	for _, selection := range binding.selections {
		key := selection.Model + "|" + selection.Effort
		if cell, ok := read[key]; ok {
			bench.Cells = append(bench.Cells, cell)
			continue
		}
		if cell, ok := carried[key]; ok {
			bench.Cells = append(bench.Cells, cell)
		}
	}
	if len(bench.Cells) == 0 {
		return nil, errors.New("bench binding published no readable selection")
	}
	if bench.LoadedCount == bench.TotalCount {
		bench.ReadAt = fetchedAt
	}
	return bench, nil
}

// readModelRadarBenchCells reads every bound selection with a bounded pool,
// retrying one busy or transiently failing request. Failed selections are
// returned by key so the caller can report and backfill them.
func readModelRadarBenchCells(ctx context.Context, client *http.Client, scoreURL string, binding modelRadarBenchBinding, readAt int64) (map[string]ModelRadarBenchCell, []string) {
	var mu sync.Mutex
	read := make(map[string]ModelRadarBenchCell, len(binding.selections))
	failed := make([]string, 0)
	var group sync.WaitGroup
	slots := make(chan struct{}, modelRadarBenchConcurrency)
	for _, selection := range binding.selections {
		group.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			cell, err := readModelRadarBenchCell(ctx, client, scoreURL, binding, selection, readAt)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, selection.Model+"|"+selection.Effort)
				common.SysLog(fmt.Sprintf("model radar bench skipped %s|%s: %v", selection.Model, selection.Effort, err))
				return
			}
			read[selection.Model+"|"+selection.Effort] = cell
		})
	}
	group.Wait()
	return read, failed
}

func readModelRadarBenchCell(ctx context.Context, client *http.Client, scoreURL string, binding modelRadarBenchBinding, selection modelRadarBenchSelection, readAt int64) (ModelRadarBenchCell, error) {
	query := url.Values{"model": {selection.Model}, "effort": {selection.Effort}, "view": {"summary"}}
	requestURL := scoreURL + "?" + query.Encode()
	var payload modelRadarBenchSummaryPayload
	var lastErr error
	for attempt := range 2 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ModelRadarBenchCell{}, ctx.Err()
			case <-time.After(modelRadarBenchRetryDelay):
			}
		}
		cellCtx, cancel := context.WithTimeout(ctx, modelRadarBenchCellTimeout)
		lastErr = fetchModelRadarJSONWithHeaders(cellCtx, client, requestURL, modelRadarBenchSummaryMaxBytes,
			map[string]string{modelRadarBenchSelectionHeader: modelRadarBenchSelectionValue}, &payload)
		cancel()
		if lastErr == nil || !isRetryableModelRadarError(lastErr) {
			break
		}
	}
	if lastErr != nil {
		return ModelRadarBenchCell{}, lastErr
	}
	return normalizeModelRadarBenchCell(payload, binding, selection, readAt)
}

// isRetryableModelRadarError reports whether one more attempt can plausibly
// succeed: the origin answers 503 public_read_busy while its statistics
// aggregation is saturated and Cloudflare answers 524 when the proxy gives up.
func isRetryableModelRadarError(err error) bool {
	var statusErr *modelRadarHTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusTooManyRequests || statusErr.StatusCode >= 500
	}
	// Connection and timeout failures carry no status; a cancelled parent
	// request must not be retried.
	return !errors.Is(err, context.Canceled)
}

func validateModelRadarBenchBinding(payload modelRadarBenchBindingPayload) (modelRadarBenchBinding, error) {
	if !payload.Enabled || !payload.ProductionReadVerified || !payload.InterfaceConfirmed {
		return modelRadarBenchBinding{}, errors.New("bench binding is not production-verified")
	}
	if payload.ScoreVersion != modelRadarBenchScoreVersion || payload.ScoreMax != modelRadarBenchScoreMax {
		return modelRadarBenchBinding{}, errors.New("unsupported bench score version")
	}
	if payload.TotalTasks != modelRadarBenchTaskCount {
		return modelRadarBenchBinding{}, errors.New("bench task count is out of contract")
	}
	if strings.TrimSpace(payload.Benchmark) == "" || strings.TrimSpace(payload.CatalogVersion) == "" ||
		len(payload.TaskSetSHA256) != 64 {
		return modelRadarBenchBinding{}, errors.New("bench binding identity is incomplete")
	}
	if len(payload.SourceCounts) == 0 {
		return modelRadarBenchBinding{}, errors.New("bench binding has no source counts")
	}
	for source, count := range payload.SourceCounts {
		if strings.TrimSpace(source) == "" || count <= 0 || count > modelRadarBenchTaskCount {
			return modelRadarBenchBinding{}, errors.New("bench source counts are invalid")
		}
	}
	if len(payload.ModelEfforts) == 0 || len(payload.ModelEfforts) > modelRadarBenchMaxCells {
		return modelRadarBenchBinding{}, errors.New("bench selection count is out of range")
	}
	selections := make([]modelRadarBenchSelection, 0, len(payload.ModelEfforts))
	seen := make(map[string]struct{}, len(payload.ModelEfforts))
	for _, selection := range payload.ModelEfforts {
		modelName, effort, key, err := validateModelRadarIdentity(selection.Model, selection.Effort)
		if err != nil {
			return modelRadarBenchBinding{}, fmt.Errorf("invalid bench selection: %w", err)
		}
		if _, exists := seen[key]; exists {
			return modelRadarBenchBinding{}, fmt.Errorf("duplicate bench selection %s", key)
		}
		seen[key] = struct{}{}
		selections = append(selections, modelRadarBenchSelection{Model: modelName, Effort: effort})
	}
	return modelRadarBenchBinding{
		benchmark:      payload.Benchmark,
		catalogVersion: payload.CatalogVersion,
		taskSetSHA256:  payload.TaskSetSHA256,
		sourceCounts:   payload.SourceCounts,
		selections:     selections,
	}, nil
}

func normalizeModelRadarBenchCell(payload modelRadarBenchSummaryPayload, binding modelRadarBenchBinding, selection modelRadarBenchSelection, readAt int64) (ModelRadarBenchCell, error) {
	schemaVersion, ok := modelRadarBenchSummaryVersion(payload.Schema)
	if !ok {
		return ModelRadarBenchCell{}, fmt.Errorf("unsupported bench summary schema %q", payload.Schema)
	}
	if schemaVersion >= 6 && strings.TrimSpace(payload.ScoringPolicyVersion) == "" {
		return ModelRadarBenchCell{}, errors.New("bench scoring policy version is missing")
	}
	if payload.Benchmark != binding.benchmark || payload.CatalogVersion != binding.catalogVersion ||
		payload.TaskSetSHA256 != binding.taskSetSHA256 || payload.ScoreVersion != modelRadarBenchScoreVersion ||
		payload.ScoreMax != modelRadarBenchScoreMax {
		return ModelRadarBenchCell{}, errors.New("bench summary does not match the binding")
	}
	if payload.Model != selection.Model || payload.Effort != selection.Effort {
		return ModelRadarBenchCell{}, errors.New("bench summary does not match the requested selection")
	}
	if err := validateModelRadarBenchScore(payload.Coverage, payload.RequiredTasks, payload.ScoreStatus, payload.Score); err != nil {
		return ModelRadarBenchCell{}, err
	}
	if payload.GradedRunsCount != nil && (*payload.GradedRunsCount < payload.Coverage || *payload.GradedRunsCount > payload.Coverage*3) {
		return ModelRadarBenchCell{}, errors.New("bench graded runs count is out of range")
	}
	detailMetrics, err := normalizeModelRadarBenchDetailMetrics(payload.DetailMetrics)
	if err != nil {
		return ModelRadarBenchCell{}, err
	}
	if detailMetrics != nil && payload.GradedRunsCount != nil && detailMetrics.ValidGradeEvents < *payload.GradedRunsCount {
		return ModelRadarBenchCell{}, errors.New("bench detail grade events are fewer than graded runs")
	}
	if len(payload.SourceCounts) != len(binding.sourceCounts) {
		return ModelRadarBenchCell{}, errors.New("bench source counts do not match the binding")
	}
	for source, count := range binding.sourceCounts {
		if payload.SourceCounts[source] != count {
			return ModelRadarBenchCell{}, errors.New("bench source counts do not match the binding")
		}
	}
	sourceScores, err := normalizeModelRadarBenchSourceScores(payload, binding)
	if err != nil {
		return ModelRadarBenchCell{}, err
	}
	history, lastGradedDay, err := normalizeModelRadarBenchHistory(payload)
	if err != nil {
		return ModelRadarBenchCell{}, err
	}
	price, duration, err := normalizeModelRadarBenchStatistics(payload)
	if err != nil {
		return ModelRadarBenchCell{}, err
	}
	return ModelRadarBenchCell{
		Model:                selection.Model,
		Effort:               selection.Effort,
		Score:                payload.Score,
		Coverage:             payload.Coverage,
		RequiredTasks:        payload.RequiredTasks,
		ScoreStatus:          payload.ScoreStatus,
		GradedRunsCount:      payload.GradedRunsCount,
		SourceScores:         sourceScores,
		Price:                price,
		Duration:             duration,
		StatisticsState:      payload.StatisticsState,
		ScoringPolicyVersion: payload.ScoringPolicyVersion,
		DetailMetrics:        detailMetrics,
		HistoryState:         payload.HistoryState,
		History:              history,
		LastGradedDay:        lastGradedDay,
		ReadAt:               readAt,
	}, nil
}

// modelRadarBenchSummaryVersion accepts additive public-summary revisions at or
// above the oldest supported one. A new revision must not freeze the board the
// way an exact-string match did when upstream moved from v5 to v6.
func modelRadarBenchSummaryVersion(schema string) (int, bool) {
	rest, ok := strings.CutPrefix(schema, modelRadarBenchSummarySchemaPrefix)
	if !ok {
		return 0, false
	}
	digits, _, _ := strings.Cut(rest, "-")
	version, err := strconv.Atoi(digits)
	if err != nil || version < modelRadarBenchSummaryMinSchemaVersion {
		return 0, false
	}
	return version, true
}

// normalizeModelRadarBenchDetailMetrics validates the v6 grading-activity
// envelope. Broken token aggregates do not erase the score or the activity
// counts: upstream itself marks them invalid_source_statistics while the score
// stays readable, so this keeps that split.
func normalizeModelRadarBenchDetailMetrics(payload *modelRadarBenchDetailMetricsPayload) (*ModelRadarBenchDetailMetrics, error) {
	if payload == nil {
		return nil, nil
	}
	if payload.ValidGradeEvents == nil || *payload.ValidGradeEvents < 0 {
		return nil, errors.New("bench detail grade event count is invalid")
	}
	events := *payload.ValidGradeEvents
	if payload.AsOf == nil {
		return nil, errors.New("bench detail read time is missing")
	}
	asOf, err := parseModelRadarTimestamp(*payload.AsOf)
	if err != nil {
		return nil, fmt.Errorf("invalid bench detail read time: %w", err)
	}
	var latestGradedAt *int64
	if payload.LatestGradedAt != nil && strings.TrimSpace(*payload.LatestGradedAt) != "" {
		parsed, parseErr := parseModelRadarTimestamp(*payload.LatestGradedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid bench detail latest grade time: %w", parseErr)
		}
		latestGradedAt = &parsed
	}
	if (events == 0) != (latestGradedAt == nil) {
		return nil, errors.New("bench detail latest grade time differs from the event count")
	}
	if payload.Graded24h == nil || payload.Graded48h == nil {
		return nil, errors.New("bench detail recent grade counts are missing")
	}
	if *payload.Graded24h < 0 || *payload.Graded24h > *payload.Graded48h || *payload.Graded48h > events {
		return nil, errors.New("bench detail recent grade counts are out of range")
	}
	state := payload.MetricsState
	switch state {
	case modelRadarBenchDetailAvailable:
	case modelRadarBenchDetailNotGiven, modelRadarBenchDetailInvalid:
		if len(payload.Metrics) != 0 {
			return nil, errors.New("bench detail metrics state differs from the payload")
		}
	default:
		return nil, errors.New("bench detail metrics state is unknown")
	}
	detail := &ModelRadarBenchDetailMetrics{
		ValidGradeEvents: events,
		LatestGradedAt:   latestGradedAt,
		AsOf:             asOf,
		Graded24h:        *payload.Graded24h,
		Graded48h:        *payload.Graded48h,
		MetricsState:     state,
	}
	if state != modelRadarBenchDetailAvailable {
		return detail, nil
	}
	metrics, err := normalizeModelRadarBenchDetailSamples(payload.Metrics, events)
	if err != nil {
		detail.MetricsState = modelRadarBenchDetailInvalid
		return detail, nil
	}
	detail.Metrics = metrics
	return detail, nil
}

// normalizeModelRadarBenchDetailSamples converts the detail aggregates into
// observed means. Each metric is measured over the same retained valid grade
// population, so every sample count must equal the event count.
func normalizeModelRadarBenchDetailSamples(payload map[string]modelRadarBenchDetailMetricPayload, events int) (map[string]ModelRadarBenchDetailMetric, error) {
	if len(payload) != len(modelRadarBenchDetailMetricNames) {
		return nil, errors.New("bench detail metric set is incomplete")
	}
	metrics := make(map[string]ModelRadarBenchDetailMetric, len(modelRadarBenchDetailMetricNames))
	for _, name := range modelRadarBenchDetailMetricNames {
		metric, ok := payload[name]
		if !ok || metric.KnownSamples == nil || metric.MissingSamples == nil ||
			*metric.KnownSamples < 0 || *metric.MissingSamples < 0 ||
			*metric.KnownSamples+*metric.MissingSamples != events {
			return nil, errors.New("bench detail metric samples are inconsistent")
		}
		stat := ModelRadarBenchDetailMetric{
			KnownSamples: *metric.KnownSamples,
			TotalSamples: events,
		}
		if stat.KnownSamples == 0 {
			// Upstream nulls the total when no sample carries the measurement.
			if metric.KnownTotal != nil {
				return nil, errors.New("bench detail metric total is invalid")
			}
			metrics[name] = stat
			continue
		}
		if metric.KnownTotal == nil || !isFiniteInRange(*metric.KnownTotal, 0, math.MaxFloat64) {
			return nil, errors.New("bench detail metric total is invalid")
		}
		mean := *metric.KnownTotal / float64(stat.KnownSamples)
		if !isFiniteInRange(mean, 0, math.MaxFloat64) {
			return nil, errors.New("bench detail metric mean is invalid")
		}
		stat.Mean = &mean
		metrics[name] = stat
	}
	input, cache := metrics["input_tokens"], metrics["cache_tokens"]
	if input.Mean != nil && cache.Mean != nil && input.KnownSamples == events && cache.KnownSamples == events &&
		*cache.Mean > *input.Mean {
		return nil, errors.New("bench detail cache tokens exceed input tokens")
	}
	return metrics, nil
}

func validateModelRadarBenchScore(coverage int, requiredTasks int, status string, score *float64) error {
	if requiredTasks != modelRadarBenchTaskCount {
		return errors.New("bench coverage must be x/64")
	}
	if coverage < 0 || coverage > modelRadarBenchTaskCount {
		return errors.New("bench coverage is out of range")
	}
	wantStatus := "provisional"
	switch coverage {
	case 0:
		wantStatus = "missing_current_result"
	case modelRadarBenchTaskCount:
		wantStatus = "complete"
	}
	if status != wantStatus {
		return errors.New("bench score status differs from coverage")
	}
	if coverage == 0 {
		if score != nil {
			return errors.New("bench score must be absent without coverage")
		}
		return nil
	}
	if score == nil || !isFiniteInRange(*score, 0, modelRadarBenchScoreMax) {
		return errors.New("bench score is out of range")
	}
	return nil
}

// normalizeModelRadarBenchSourceScores mirrors the dashboard's per-library
// projection: each library's score is the mean of its covered tasks, and the
// per-library coverage must add up to the cell's coverage.
func normalizeModelRadarBenchSourceScores(payload modelRadarBenchSummaryPayload, binding modelRadarBenchBinding) (map[string]ModelRadarBenchSourceScore, error) {
	if len(payload.SourceScores) != len(binding.sourceCounts) {
		return nil, errors.New("bench source scores do not match the binding")
	}
	scores := make(map[string]ModelRadarBenchSourceScore, len(binding.sourceCounts))
	coverageSum, gradedSum := 0, 0
	for source, required := range binding.sourceCounts {
		row, ok := payload.SourceScores[source]
		if !ok || row.RequiredTasks != required {
			return nil, errors.New("bench source scores do not match the binding")
		}
		if row.Coverage < 0 || row.Coverage > required {
			return nil, errors.New("bench source coverage is out of range")
		}
		if row.Coverage == 0 {
			if row.Score != nil {
				return nil, errors.New("bench source score must be absent without coverage")
			}
		} else if row.Score == nil || !isFiniteInRange(*row.Score, 0, modelRadarBenchScoreMax) {
			return nil, errors.New("bench source score is out of range")
		}
		if row.GradedRunsCount != nil && (*row.GradedRunsCount < row.Coverage || *row.GradedRunsCount > row.Coverage*3) {
			return nil, errors.New("bench source graded runs count is out of range")
		}
		coverageSum += row.Coverage
		if row.GradedRunsCount != nil {
			gradedSum += *row.GradedRunsCount
		}
		scores[source] = ModelRadarBenchSourceScore{
			Score:           row.Score,
			Coverage:        row.Coverage,
			RequiredTasks:   row.RequiredTasks,
			GradedRunsCount: row.GradedRunsCount,
		}
	}
	if coverageSum != payload.Coverage {
		return nil, errors.New("bench source coverage does not add up to the cell coverage")
	}
	if payload.GradedRunsCount != nil && gradedSum != *payload.GradedRunsCount {
		return nil, errors.New("bench source graded runs do not add up to the cell graded runs")
	}
	return scores, nil
}

// normalizeModelRadarBenchHistory reads the UTC daily buckets and checks the
// dashboard's invariant that the current score equals the final graded day.
func normalizeModelRadarBenchHistory(payload modelRadarBenchSummaryPayload) ([]ModelRadarBenchHistoryPoint, int64, error) {
	switch payload.HistoryState {
	case "not_provided":
		if payload.History != nil {
			return nil, 0, errors.New("bench history state differs from the payload")
		}
		return nil, 0, nil
	case "available":
	default:
		return nil, 0, errors.New("bench history state is unknown")
	}
	if payload.History == nil || payload.History.TimeZone != "UTC" || payload.History.Bucket != "day" {
		return nil, 0, errors.New("bench history envelope is invalid")
	}
	if len(payload.History.Points) > modelRadarBenchMaxHistoryPoints {
		return nil, 0, errors.New("bench history is out of range")
	}
	points := make([]ModelRadarBenchHistoryPoint, 0, len(payload.History.Points))
	var previousTs int64
	for _, point := range payload.History.Points {
		ts, err := parseModelRadarTimestamp(point.At)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid bench history timestamp: %w", err)
		}
		if previousTs != 0 && ts <= previousTs {
			return nil, 0, errors.New("bench history timestamps must be strictly increasing")
		}
		previousTs = ts
		if err := validateModelRadarBenchScore(point.Coverage, point.RequiredTasks, point.ScoreStatus, point.Score); err != nil {
			return nil, 0, fmt.Errorf("invalid bench history point: %w", err)
		}
		if point.Coverage == 0 {
			return nil, 0, errors.New("bench history point cannot have missing coverage")
		}
		points = append(points, ModelRadarBenchHistoryPoint{
			Ts:          ts,
			Score:       point.Score,
			Coverage:    point.Coverage,
			ScoreStatus: point.ScoreStatus,
		})
	}
	var lastGradedDay int64
	if len(points) > 0 {
		last := points[len(points)-1]
		if last.Score == nil || payload.Score == nil || *last.Score != *payload.Score || last.Coverage != payload.Coverage {
			return nil, 0, errors.New("bench current score must match the final graded day")
		}
		lastGradedDay = last.Ts
	} else if payload.Score != nil || payload.Coverage != 0 {
		return nil, 0, errors.New("bench current score must match the final graded day")
	}
	return points, lastGradedDay, nil
}

// normalizeModelRadarBenchStatistics converts the published aggregates into
// the observed means the dashboard shows. When any sample lacks a measurement
// upstream nulls total/mean but keeps known_total/known_samples, so the mean
// is computed from the known samples; elapsed_ms becomes minutes.
func normalizeModelRadarBenchStatistics(payload modelRadarBenchSummaryPayload) (ModelRadarBenchMetricStat, ModelRadarBenchMetricStat, error) {
	switch payload.StatisticsState {
	case "not_provided", "invalid_source_statistics":
		if payload.Statistics != nil {
			return ModelRadarBenchMetricStat{}, ModelRadarBenchMetricStat{}, errors.New("bench statistics state differs from the payload")
		}
		return ModelRadarBenchMetricStat{}, ModelRadarBenchMetricStat{}, nil
	case "available":
	default:
		return ModelRadarBenchMetricStat{}, ModelRadarBenchMetricStat{}, errors.New("bench statistics state is unknown")
	}
	if payload.Statistics == nil {
		return ModelRadarBenchMetricStat{}, ModelRadarBenchMetricStat{}, errors.New("bench statistics are missing")
	}
	if payload.Statistics.SampleCount < 0 {
		return ModelRadarBenchMetricStat{}, ModelRadarBenchMetricStat{}, errors.New("bench statistics sample count is invalid")
	}
	price, err := observedModelRadarBenchMetric(payload.Statistics, "api_equivalent_usd", 1)
	if err != nil {
		return ModelRadarBenchMetricStat{}, ModelRadarBenchMetricStat{}, err
	}
	duration, err := observedModelRadarBenchMetric(payload.Statistics, "elapsed_ms", 1.0/60000.0)
	if err != nil {
		return ModelRadarBenchMetricStat{}, ModelRadarBenchMetricStat{}, err
	}
	return price, duration, nil
}

func observedModelRadarBenchMetric(statistics *modelRadarBenchStatisticsPayload, name string, scale float64) (ModelRadarBenchMetricStat, error) {
	metric, ok := statistics.Metrics[name]
	if !ok {
		return ModelRadarBenchMetricStat{}, nil
	}
	if metric.KnownSamples == nil || metric.MissingSamples == nil ||
		*metric.KnownSamples < 0 || *metric.MissingSamples < 0 ||
		*metric.KnownSamples+*metric.MissingSamples != statistics.SampleCount {
		return ModelRadarBenchMetricStat{}, errors.New("bench statistics samples are inconsistent")
	}
	stat := ModelRadarBenchMetricStat{KnownSamples: metric.KnownSamples}
	total := *metric.KnownSamples + *metric.MissingSamples
	stat.TotalSamples = &total
	if *metric.KnownSamples == 0 {
		return stat, nil
	}
	if metric.KnownTotal == nil || !isFiniteInRange(*metric.KnownTotal, 0, math.MaxFloat64) {
		return ModelRadarBenchMetricStat{}, errors.New("bench statistics total is invalid")
	}
	mean := *metric.KnownTotal / float64(*metric.KnownSamples) * scale
	if !isFiniteInRange(mean, 0, math.MaxFloat64) {
		return ModelRadarBenchMetricStat{}, errors.New("bench statistics mean is invalid")
	}
	stat.Mean = &mean
	return stat, nil
}
