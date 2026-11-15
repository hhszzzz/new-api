package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// In November 2026 the station replaced its 64-task RadarBench board with the
// four-task weighted board it now presents as the degradation radar. Each round
// grades four independent tasks — Qin Shi Huang (combined), Pompeii, Pelican and
// Candy — weighted 60/45/22.5/22.5 over a 150-point total, and publishes two
// documents that describe the same priced source: one results document with the
// per-task evidence and one compact score summary. A round is accepted only
// when both agree on the round, the source hash and every total, so a partially
// updated publication never reaches the page.
const (
	modelRadarFourTaskResultsURL = "https://codexradar.com/data/codex-four-task-results.json"
	modelRadarFourTaskSummaryURL = "https://codexradar.com/data/codex-four-task-summary.json"

	modelRadarFourTaskResultsSchemaPrefix     = "codex-four-task-results/"
	modelRadarFourTaskResultsMinSchemaVersion = 1
	modelRadarFourTaskProtocolPrefix          = "codex-four-weighted-150-v"
	modelRadarFourTaskMinProtocolVersion      = 2
	modelRadarFourTaskScoreMax                = 150.0
	// The published score is the 0-100 base score times this display multiplier.
	// Auto-effort divides it back out so user-configured IQ thresholds keep their
	// 0-100 meaning.
	modelRadarFourTaskDisplayMultiplier = 1.5
	modelRadarFourTaskTaskCount         = 4
	modelRadarFourTaskResultsMaxBytes   = 1 << 20
	modelRadarFourTaskSummaryMaxBytes   = 256 << 10
	modelRadarFourTaskGradedState       = "graded"
	// Costs are published as API-equivalent estimates; the page must not present
	// another cost basis as one.
	modelRadarFourTaskCostKind = "api_equivalent_estimate"
)

// modelRadarFourTaskComponentNames is the published component set and the order
// the page renders it in. The component names are the upstream contract; the
// display labels live in the frontend translations.
var modelRadarFourTaskComponentNames = []string{"combined", "pompeii", "pelican", "candy"}

// modelRadarFourTaskEfforts is the published reasoning tier set. The station's
// board has fixed effort columns; an unknown tier would otherwise be fetched
// and then never rendered, so it rejects the round instead.
var modelRadarFourTaskEfforts = []string{"ultra", "max", "xhigh", "high", "medium", "low"}

// ModelRadarFourTaskComponent is one graded task of a configuration.
type ModelRadarFourTaskComponent struct {
	// Score is the component result on its 0-100 scale. It is nil when the
	// component was not graded; a missing component never counts as zero.
	Score *float64 `json:"score"`
	// Weight is the component's share of the 150-point total.
	Weight float64 `json:"weight"`
	// Contribution is Weight * Score / 100.
	Contribution *float64 `json:"contribution"`
	// CostUSD is the component's API-equivalent cost; nil when unmetered.
	CostUSD *float64 `json:"cost_usd"`
	// DurationMinutes is the component's successful author runtime; nil when
	// the component was not graded or the source did not record it.
	DurationMinutes *float64 `json:"duration_minutes"`
	State           string   `json:"state"`
	MissingReason   string   `json:"missing_reason,omitempty"`
}

// ModelRadarFourTaskCell is one model/effort configuration of the round.
type ModelRadarFourTaskCell struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
	// Score is the weighted total over all four components, nil unless every
	// component was graded.
	Score         *float64 `json:"score"`
	GradedTasks   int      `json:"graded_tasks"`
	RequiredTasks int      `json:"required_tasks"`
	// Rank is the configuration's place among the published totals, highest
	// score first; nil when the configuration has no total.
	Rank *int `json:"rank"`
	// Components is keyed by modelRadarFourTaskComponentNames.
	Components map[string]ModelRadarFourTaskComponent `json:"components"`
	// CostUSD is the sum of the four component costs; nil when any component is
	// unmetered. DurationMinutes is the sum of the graded component runtimes.
	CostUSD         *float64 `json:"cost_usd"`
	DurationMinutes *float64 `json:"duration_minutes"`
}

// ModelRadarFourTask is the live scored section of the radar snapshot.
type ModelRadarFourTask struct {
	Protocol          string                   `json:"protocol"`
	RoundID           string                   `json:"round_id"`
	ScoreMax          int                      `json:"score_max"`
	SourceSHA256      string                   `json:"source_sha256"`
	PublicationStatus string                   `json:"publication_status"`
	VerifiedAt        int64                    `json:"verified_at"`
	TestStartedAt     int64                    `json:"test_started_at"`
	TestCompletedAt   int64                    `json:"test_completed_at"`
	Graded            int                      `json:"graded"`
	Expected          int                      `json:"expected"`
	Ungraded          int                      `json:"ungraded"`
	ValidTotals       int                      `json:"valid_totals"`
	TaskWeights       map[string]float64       `json:"task_weights"`
	ModelNames        map[string]string        `json:"model_names"`
	ModelOrder        []string                 `json:"model_order"`
	ReadAt            int64                    `json:"read_at"`
	Cells             []ModelRadarFourTaskCell `json:"cells"`
}

type modelRadarFourTaskIntervalPayload struct {
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

type modelRadarFourTaskComponentPayload struct {
	State               string   `json:"state"`
	Score               *float64 `json:"score"`
	Score100            *float64 `json:"score100"`
	Correct             *bool    `json:"correct"`
	CostUSD             *float64 `json:"cost_usd"`
	CostKind            string   `json:"cost_kind"`
	ModelElapsedSeconds *float64 `json:"model_elapsed_seconds"`
	MissingReason       string   `json:"missing_reason"`
}

type modelRadarFourTaskRowPayload struct {
	Model    string                             `json:"model"`
	Effort   string                             `json:"effort"`
	Combined modelRadarFourTaskComponentPayload `json:"combined"`
	Pompeii  modelRadarFourTaskComponentPayload `json:"pompeii"`
	Pelican  modelRadarFourTaskComponentPayload `json:"pelican"`
	Candy    modelRadarFourTaskComponentPayload `json:"candy"`
}

func (row modelRadarFourTaskRowPayload) components() map[string]modelRadarFourTaskComponentPayload {
	return map[string]modelRadarFourTaskComponentPayload{
		"combined": row.Combined,
		"pompeii":  row.Pompeii,
		"pelican":  row.Pelican,
		"candy":    row.Candy,
	}
}

type modelRadarFourTaskMissingPayload struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type modelRadarFourTaskResultsPayload struct {
	Schema                   string                             `json:"schema"`
	Protocol                 string                             `json:"protocol"`
	RoundID                  string                             `json:"round_id"`
	PublicationStatus        string                             `json:"publication_status"`
	PublicationContract      string                             `json:"publication_contract"`
	PublicationAuthorized    *bool                              `json:"publication_authorized"`
	AllGraded                *bool                              `json:"all_graded"`
	SourceSHA256             string                             `json:"source_sha256"`
	Graded                   int                                `json:"graded"`
	Expected                 int                                `json:"expected"`
	Ungraded                 *int                               `json:"ungraded"`
	ValidTotalConfigurations *int                               `json:"valid_total_configurations"`
	AuthorTestInterval       modelRadarFourTaskIntervalPayload  `json:"author_test_interval"`
	VerifiedAt               string                             `json:"verified_at"`
	Configurations           int                                `json:"configurations"`
	Models                   []string                           `json:"models"`
	ModelEfforts             map[string][]string                `json:"model_efforts"`
	ModelNames               map[string]string                  `json:"model_names"`
	Efforts                  []string                           `json:"efforts"`
	TaskWeights              map[string]float64                 `json:"task_weights"`
	ScoreMax                 float64                            `json:"score_max"`
	Missing                  []modelRadarFourTaskMissingPayload `json:"missing"`
	Rows                     []modelRadarFourTaskRowPayload     `json:"rows"`
}

type modelRadarFourTaskSummaryRowPayload struct {
	Model               string   `json:"model"`
	Effort              string   `json:"effort"`
	Score150            *float64 `json:"score150"`
	CostUSD             *float64 `json:"cost_usd"`
	CostKind            string   `json:"cost_kind"`
	ModelElapsedSeconds *float64 `json:"model_elapsed_seconds"`
}

type modelRadarFourTaskSummaryPayload struct {
	Protocol                 string                                `json:"protocol"`
	RoundID                  string                                `json:"round_id"`
	PublicationStatus        string                                `json:"publication_status"`
	PublicationContract      string                                `json:"publication_contract"`
	PublicationAuthorized    *bool                                 `json:"publication_authorized"`
	AllGraded                *bool                                 `json:"all_graded"`
	SourceSHA256             string                                `json:"source_sha256"`
	Graded                   *int                                  `json:"graded"`
	Expected                 *int                                  `json:"expected"`
	Ungraded                 *int                                  `json:"ungraded"`
	ValidTotalConfigurations *int                                  `json:"valid_total_configurations"`
	Rows                     []modelRadarFourTaskSummaryRowPayload `json:"rows"`
}

// fetchModelRadarFourTask reads both documents of the current round. A missing
// or inconsistent document rejects the whole round; the caller keeps the
// previous snapshot instead of publishing a partial board.
func fetchModelRadarFourTask(ctx context.Context, client *http.Client, sources modelRadarSources, readAt int64) (*ModelRadarFourTask, error) {
	var results modelRadarFourTaskResultsPayload
	if err := fetchModelRadarJSON(ctx, client, sources.FourTaskResults, modelRadarFourTaskResultsMaxBytes, &results); err != nil {
		return nil, fmt.Errorf("four-task results: %w", err)
	}
	var summary modelRadarFourTaskSummaryPayload
	if err := fetchModelRadarJSON(ctx, client, sources.FourTaskSummary, modelRadarFourTaskSummaryMaxBytes, &summary); err != nil {
		return nil, fmt.Errorf("four-task summary: %w", err)
	}
	board, err := normalizeModelRadarFourTask(results, summary, readAt)
	if err != nil {
		return nil, fmt.Errorf("four-task board: %w", err)
	}
	return board, nil
}

func normalizeModelRadarFourTask(results modelRadarFourTaskResultsPayload, summary modelRadarFourTaskSummaryPayload, readAt int64) (*ModelRadarFourTask, error) {
	if _, ok := modelRadarFourTaskResultsVersion(results.Schema); !ok {
		return nil, fmt.Errorf("unsupported four-task results schema %q", results.Schema)
	}
	if _, ok := modelRadarFourTaskProtocolVersion(results.Protocol); !ok {
		return nil, fmt.Errorf("unsupported four-task protocol %q", results.Protocol)
	}
	// Two publication contracts coexist: a partial round must be explicitly
	// authorized and list what is missing, while a complete round publishes
	// every task and marks itself complete instead of repeating the flag.
	publicationStatus := strings.TrimSpace(results.PublicationStatus)
	authorized := results.PublicationAuthorized != nil && *results.PublicationAuthorized
	complete := results.AllGraded != nil && *results.AllGraded && publicationStatus == "complete"
	if publicationStatus == "" || (!authorized && !complete) {
		return nil, errors.New("four-task round is not authorized for publication")
	}
	if results.AllGraded != nil && *results.AllGraded && results.Graded != results.Expected {
		return nil, errors.New("four-task round claims every task is graded but the counts differ")
	}
	if results.ScoreMax != modelRadarFourTaskScoreMax {
		return nil, errors.New("four-task score max is out of contract")
	}
	if results.RoundID == "" || results.RoundID != summary.RoundID {
		return nil, errors.New("four-task summary describes a different round")
	}
	if len(results.SourceSHA256) != 64 || results.SourceSHA256 != summary.SourceSHA256 {
		return nil, errors.New("four-task summary describes a different source")
	}
	if summary.Protocol != results.Protocol ||
		(summary.PublicationStatus != "" && summary.PublicationStatus != results.PublicationStatus) ||
		(summary.PublicationAuthorized != nil && !*summary.PublicationAuthorized) ||
		(summary.PublicationContract != "" && results.PublicationContract != "" && summary.PublicationContract != results.PublicationContract) {
		return nil, errors.New("four-task summary publication differs from the results")
	}
	weights, err := validateModelRadarFourTaskWeights(results.TaskWeights)
	if err != nil {
		return nil, err
	}
	if results.Configurations <= 0 || results.Configurations != len(results.Rows) {
		return nil, errors.New("four-task configuration count is out of contract")
	}
	if results.Expected != len(results.Rows)*modelRadarFourTaskTaskCount {
		return nil, errors.New("four-task expected task count is out of contract")
	}
	startedAt, err := parseModelRadarTimestamp(results.AuthorTestInterval.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid four-task test start: %w", err)
	}
	completedAt, err := parseModelRadarTimestamp(results.AuthorTestInterval.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid four-task test completion: %w", err)
	}
	if completedAt < startedAt {
		return nil, errors.New("four-task test interval is inverted")
	}
	verifiedAt, err := parseModelRadarTimestamp(results.VerifiedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid four-task verification time: %w", err)
	}

	modelOrder, modelNames, err := validateModelRadarFourTaskModels(results)
	if err != nil {
		return nil, err
	}
	effortSet, err := validateModelRadarFourTaskEfforts(results.Efforts)
	if err != nil {
		return nil, err
	}
	for model, efforts := range results.ModelEfforts {
		if _, ok := modelNames[model]; !ok {
			return nil, fmt.Errorf("four-task efforts reference unknown model %q", model)
		}
		seen := make(map[string]struct{}, len(efforts))
		for _, effort := range efforts {
			if _, ok := effortSet[effort]; !ok {
				return nil, fmt.Errorf("four-task model %q has unsupported effort %q", model, effort)
			}
			if _, duplicate := seen[effort]; duplicate {
				return nil, fmt.Errorf("four-task model %q repeats effort %q", model, effort)
			}
			seen[effort] = struct{}{}
		}
	}
	missing, err := validateModelRadarFourTaskMissing(results.Missing)
	if err != nil {
		return nil, err
	}
	summaryRows := make(map[string]modelRadarFourTaskSummaryRowPayload, len(summary.Rows))
	for _, row := range summary.Rows {
		key, err := validateModelRadarFourTaskRowKey(row.Model, row.Effort)
		if err != nil {
			return nil, err
		}
		if _, duplicate := summaryRows[key]; duplicate {
			return nil, fmt.Errorf("duplicate four-task summary row %s", key)
		}
		summaryRows[key] = row
	}
	if len(summaryRows) != len(results.Rows) {
		return nil, errors.New("four-task summary row count differs from the results")
	}
	if results.Graded < 0 || results.Graded > results.Expected {
		return nil, errors.New("four-task graded count is out of contract")
	}
	ungradedTasks := results.Expected - results.Graded
	if results.Ungraded != nil && *results.Ungraded != ungradedTasks {
		return nil, errors.New("four-task ungraded count differs from the task counts")
	}
	if summary.Graded != nil && *summary.Graded != results.Graded ||
		summary.Expected != nil && *summary.Expected != results.Expected ||
		summary.Ungraded != nil && *summary.Ungraded != ungradedTasks {
		return nil, errors.New("four-task summary counts differ from the results")
	}

	cellByKey := make(map[string]ModelRadarFourTaskCell, len(results.Rows))
	gradedEvents := 0
	validTotals := 0
	for _, row := range results.Rows {
		key, err := validateModelRadarFourTaskRowKey(row.Model, row.Effort)
		if err != nil {
			return nil, err
		}
		if _, ok := modelNames[row.Model]; !ok {
			return nil, fmt.Errorf("four-task row references unknown model %q", row.Model)
		}
		if _, duplicate := cellByKey[key]; duplicate {
			return nil, fmt.Errorf("duplicate four-task row %s", key)
		}
		summaryRow, ok := summaryRows[key]
		if !ok {
			return nil, fmt.Errorf("four-task summary is missing %s", key)
		}
		components, rowGraded, rowTotal, err := normalizeModelRadarFourTaskComponents(row, weights, missing)
		if err != nil {
			return nil, fmt.Errorf("four-task row %s: %w", key, err)
		}
		gradedEvents += rowGraded
		cell := ModelRadarFourTaskCell{
			Model:         row.Model,
			Effort:        row.Effort,
			GradedTasks:   rowGraded,
			RequiredTasks: modelRadarFourTaskTaskCount,
			Components:    components,
		}
		if rowGraded == modelRadarFourTaskTaskCount {
			if summaryRow.Score150 == nil ||
				!isFiniteInRange(*summaryRow.Score150, 0, modelRadarFourTaskScoreMax) {
				return nil, fmt.Errorf("four-task summary total for %s is invalid", key)
			}
			if math.Abs(rowTotal-*summaryRow.Score150) > 1e-6 {
				return nil, fmt.Errorf("four-task summary total for %s does not match the components", key)
			}
			cell.Score = summaryRow.Score150
			validTotals++
		} else if summaryRow.Score150 != nil {
			return nil, fmt.Errorf("four-task summary publishes a total for incomplete row %s", key)
		}
		cost, duration, err := normalizeModelRadarFourTaskTotals(components, summaryRow)
		if err != nil {
			return nil, fmt.Errorf("four-task row %s: %w", key, err)
		}
		cell.CostUSD = cost
		cell.DurationMinutes = duration
		cellByKey[key] = cell
	}
	if gradedEvents != results.Graded {
		return nil, errors.New("four-task graded count differs from the components")
	}
	if len(missing) != ungradedTasks {
		return nil, errors.New("four-task missing entries do not match the ungraded count")
	}
	if results.ValidTotalConfigurations != nil && *results.ValidTotalConfigurations != validTotals ||
		summary.ValidTotalConfigurations != nil && *summary.ValidTotalConfigurations != validTotals {
		return nil, errors.New("four-task valid total count is out of contract")
	}

	ordered, err := orderModelRadarFourTaskCells(results, modelOrder, cellByKey)
	if err != nil {
		return nil, err
	}
	assignModelRadarFourTaskRanks(ordered)
	return &ModelRadarFourTask{
		Protocol:          results.Protocol,
		RoundID:           results.RoundID,
		ScoreMax:          int(results.ScoreMax),
		SourceSHA256:      results.SourceSHA256,
		PublicationStatus: results.PublicationStatus,
		VerifiedAt:        verifiedAt,
		TestStartedAt:     startedAt,
		TestCompletedAt:   completedAt,
		Graded:            results.Graded,
		Expected:          results.Expected,
		Ungraded:          ungradedTasks,
		ValidTotals:       validTotals,
		TaskWeights:       weights,
		ModelNames:        modelNames,
		ModelOrder:        modelOrder,
		ReadAt:            readAt,
		Cells:             ordered,
	}, nil
}

// modelRadarFourTaskResultsVersion accepts additive results revisions at or
// above the oldest supported one, so a new revision cannot freeze the board.
func modelRadarFourTaskResultsVersion(schema string) (int, bool) {
	return modelRadarVersionedPrefix(schema, modelRadarFourTaskResultsSchemaPrefix, modelRadarFourTaskResultsMinSchemaVersion)
}

func modelRadarFourTaskProtocolVersion(protocol string) (int, bool) {
	return modelRadarVersionedPrefix(protocol, modelRadarFourTaskProtocolPrefix, modelRadarFourTaskMinProtocolVersion)
}

func modelRadarVersionedPrefix(value string, prefix string, minVersion int) (int, bool) {
	rest, ok := strings.CutPrefix(value, prefix)
	if !ok {
		return 0, false
	}
	digits, _, _ := strings.Cut(rest, "-")
	version, err := strconv.Atoi(digits)
	if err != nil || version < minVersion {
		return 0, false
	}
	return version, true
}

func validateModelRadarFourTaskWeights(raw map[string]float64) (map[string]float64, error) {
	if len(raw) != len(modelRadarFourTaskComponentNames) {
		return nil, errors.New("four-task weights do not cover every component")
	}
	weights := make(map[string]float64, len(raw))
	var sum float64
	for _, name := range modelRadarFourTaskComponentNames {
		weight, ok := raw[name]
		if !ok || !isFiniteInRange(weight, 0, modelRadarFourTaskScoreMax) || weight == 0 {
			return nil, errors.New("four-task weights are invalid")
		}
		weights[name] = weight
		sum += weight
	}
	if math.Abs(sum-modelRadarFourTaskScoreMax) > 1e-9 {
		return nil, errors.New("four-task weights do not add up to the score max")
	}
	return weights, nil
}

func validateModelRadarFourTaskModels(payload modelRadarFourTaskResultsPayload) ([]string, map[string]string, error) {
	if len(payload.Models) == 0 {
		return nil, nil, errors.New("four-task models are missing")
	}
	order := make([]string, 0, len(payload.Models))
	names := make(map[string]string, len(payload.Models))
	for _, model := range payload.Models {
		model = strings.TrimSpace(model)
		if model == "" {
			return nil, nil, errors.New("four-task model name is empty")
		}
		if _, duplicate := names[model]; duplicate {
			return nil, nil, fmt.Errorf("duplicate four-task model %q", model)
		}
		name := strings.TrimSpace(payload.ModelNames[model])
		if name == "" {
			return nil, nil, fmt.Errorf("four-task model %q has no display name", model)
		}
		order = append(order, model)
		names[model] = name
	}
	return order, names, nil
}

func validateModelRadarFourTaskEfforts(raw []string) (map[string]struct{}, error) {
	if len(raw) == 0 {
		return nil, errors.New("four-task efforts are missing")
	}
	efforts := make(map[string]struct{}, len(raw))
	for _, effort := range raw {
		effort = strings.TrimSpace(effort)
		if !slices.Contains(modelRadarFourTaskEfforts, effort) {
			return nil, fmt.Errorf("unsupported four-task effort %q", effort)
		}
		if _, duplicate := efforts[effort]; duplicate {
			return nil, fmt.Errorf("duplicate four-task effort %q", effort)
		}
		efforts[effort] = struct{}{}
	}
	return efforts, nil
}

// validateModelRadarFourTaskMissing indexes the published missing components so
// every ungraded component must be accounted for exactly once.
func validateModelRadarFourTaskMissing(raw []modelRadarFourTaskMissingPayload) (map[string]struct{}, error) {
	missing := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		key, err := validateModelRadarFourTaskRowKey(entry.Model, entry.Effort)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(modelRadarFourTaskComponentNames, entry.Kind) {
			return nil, fmt.Errorf("four-task missing entry has unknown component %q", entry.Kind)
		}
		componentKey := key + "|" + entry.Kind
		if _, duplicate := missing[componentKey]; duplicate {
			return nil, fmt.Errorf("duplicate four-task missing component %s", componentKey)
		}
		missing[componentKey] = struct{}{}
	}
	return missing, nil
}

func validateModelRadarFourTaskRowKey(model string, effort string) (string, error) {
	modelName, effortName, key, err := validateModelRadarIdentity(model, effort)
	if err != nil {
		return "", err
	}
	if modelName != model || effortName != effort {
		return "", errors.New("four-task row identity is not normalized")
	}
	return key, nil
}

func normalizeModelRadarFourTaskComponents(row modelRadarFourTaskRowPayload, weights map[string]float64, missing map[string]struct{}) (map[string]ModelRadarFourTaskComponent, int, float64, error) {
	raw := row.components()
	components := make(map[string]ModelRadarFourTaskComponent, len(raw))
	graded := 0
	var total float64
	for _, name := range modelRadarFourTaskComponentNames {
		component, err := normalizeModelRadarFourTaskComponent(name, raw[name], weights[name])
		if err != nil {
			return nil, 0, 0, fmt.Errorf("component %s: %w", name, err)
		}
		if component.Score != nil {
			graded++
			total += *component.Contribution
		} else if _, ok := missing[row.Model+"|"+row.Effort+"|"+name]; !ok {
			return nil, 0, 0, fmt.Errorf("missing component %s is not declared", name)
		}
		components[name] = component
	}
	return components, graded, total, nil
}

func normalizeModelRadarFourTaskComponent(name string, payload modelRadarFourTaskComponentPayload, weight float64) (ModelRadarFourTaskComponent, error) {
	component := ModelRadarFourTaskComponent{
		Weight:        weight,
		State:         payload.State,
		MissingReason: strings.TrimSpace(payload.MissingReason),
	}
	if payload.CostUSD != nil {
		if payload.CostKind != modelRadarFourTaskCostKind {
			return ModelRadarFourTaskComponent{}, errors.New("cost kind is out of contract")
		}
		if !isFiniteInRange(*payload.CostUSD, 0, math.MaxFloat64) {
			return ModelRadarFourTaskComponent{}, errors.New("cost is out of range")
		}
		component.CostUSD = payload.CostUSD
	}
	if payload.ModelElapsedSeconds != nil {
		seconds := *payload.ModelElapsedSeconds
		if !isFiniteInRange(seconds, 0, math.MaxFloat64) {
			return ModelRadarFourTaskComponent{}, errors.New("elapsed seconds are out of range")
		}
		minutes := seconds / 60
		component.DurationMinutes = &minutes
	}
	if payload.State != "" && payload.State != modelRadarFourTaskGradedState {
		if payload.Score != nil || payload.Score100 != nil || payload.Correct != nil {
			return ModelRadarFourTaskComponent{}, errors.New("an ungraded component published a score")
		}
		return component, nil
	}
	// The complete-round contract omits the per-component state; the published
	// evidence decides whether the component was graded.
	hasResult := payload.Score != nil || payload.Score100 != nil || payload.Correct != nil
	if payload.State == "" {
		if !hasResult {
			component.State = "missing"
			return component, nil
		}
		component.State = modelRadarFourTaskGradedState
	} else if !hasResult {
		return ModelRadarFourTaskComponent{}, errors.New("component result is missing")
	}
	var score float64
	switch name {
	case "combined", "pelican":
		if payload.Score == nil || !isFiniteInRange(*payload.Score, 0, 100) {
			return ModelRadarFourTaskComponent{}, errors.New("component score is out of range")
		}
		score = *payload.Score
	case "pompeii":
		if payload.Score100 == nil || !isFiniteInRange(*payload.Score100, 0, 100) {
			return ModelRadarFourTaskComponent{}, errors.New("component score is out of range")
		}
		score = *payload.Score100
	case "candy":
		if payload.Correct == nil {
			return ModelRadarFourTaskComponent{}, errors.New("component result is missing")
		}
		if *payload.Correct {
			score = 100
		}
	}
	contribution := score * weight / 100
	if !isFiniteInRange(contribution, 0, modelRadarFourTaskScoreMax) {
		return ModelRadarFourTaskComponent{}, errors.New("component contribution is out of range")
	}
	component.Score = &score
	component.Contribution = &contribution
	return component, nil
}

// normalizeModelRadarFourTaskTotals keeps the published round totals only when
// every component observed the measurement. A component that was graded but
// unmetered leaves the total unknown instead of understating it.
func normalizeModelRadarFourTaskTotals(components map[string]ModelRadarFourTaskComponent, summaryRow modelRadarFourTaskSummaryRowPayload) (*float64, *float64, error) {
	var cost, duration float64
	allCosts, allDurations := true, true
	for _, name := range modelRadarFourTaskComponentNames {
		component := components[name]
		if component.CostUSD == nil {
			allCosts = false
		} else {
			cost += *component.CostUSD
		}
		if component.DurationMinutes == nil {
			allDurations = false
		} else {
			duration += *component.DurationMinutes
		}
	}
	if summaryRow.CostUSD != nil {
		if summaryRow.CostKind != modelRadarFourTaskCostKind {
			return nil, nil, errors.New("summary cost kind is out of contract")
		}
		if !allCosts {
			return nil, nil, errors.New("summary cost covers an unmetered component")
		}
		if !isFiniteInRange(*summaryRow.CostUSD, 0, math.MaxFloat64) ||
			math.Abs(cost-*summaryRow.CostUSD) > 1e-6 {
			return nil, nil, errors.New("summary cost does not match the components")
		}
	} else if allCosts {
		return nil, nil, errors.New("summary omits a fully observed cost")
	}
	if summaryRow.ModelElapsedSeconds != nil {
		if !allDurations {
			return nil, nil, errors.New("summary runtime covers an unrecorded component")
		}
		minutes := *summaryRow.ModelElapsedSeconds / 60
		if !isFiniteInRange(minutes, 0, math.MaxFloat64) ||
			math.Abs(duration-minutes) > 1e-6 {
			return nil, nil, errors.New("summary runtime does not match the components")
		}
	} else if allDurations {
		return nil, nil, errors.New("summary omits a fully observed runtime")
	}
	return summaryRow.CostUSD, modelRadarFourTaskDurationMinutes(summaryRow.ModelElapsedSeconds), nil
}

func modelRadarFourTaskDurationMinutes(seconds *float64) *float64 {
	if seconds == nil {
		return nil
	}
	minutes := *seconds / 60
	return &minutes
}

// orderModelRadarFourTaskCells follows the published model order and each
// model's effort order. Every row must appear exactly once.
func orderModelRadarFourTaskCells(payload modelRadarFourTaskResultsPayload, modelOrder []string, cells map[string]ModelRadarFourTaskCell) ([]ModelRadarFourTaskCell, error) {
	ordered := make([]ModelRadarFourTaskCell, 0, len(cells))
	for _, model := range modelOrder {
		efforts, ok := payload.ModelEfforts[model]
		if !ok || len(efforts) == 0 {
			return nil, fmt.Errorf("four-task model %q has no efforts", model)
		}
		for _, effort := range efforts {
			cell, ok := cells[model+"|"+effort]
			if !ok {
				return nil, fmt.Errorf("four-task row %s|%s is missing", model, effort)
			}
			ordered = append(ordered, cell)
		}
	}
	if len(ordered) != len(cells) {
		return nil, errors.New("four-task rows do not match the published model efforts")
	}
	return ordered, nil
}

// assignModelRadarFourTaskRanks ranks configurations by descending total; a
// configuration without a total is unranked.
func assignModelRadarFourTaskRanks(cells []ModelRadarFourTaskCell) {
	for index := range cells {
		if cells[index].Score == nil {
			continue
		}
		rank := 1
		for _, other := range cells {
			if other.Score != nil && *other.Score > *cells[index].Score {
				rank++
			}
		}
		cells[index].Rank = &rank
	}
}
