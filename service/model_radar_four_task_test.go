package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	modelRadarFourTaskTestRoundID   = "codex-four-test-round"
	modelRadarFourTaskTestSourceSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

var modelRadarFourTaskTestWeights = map[string]float64{
	"combined": 60,
	"pompeii":  45,
	"pelican":  22.5,
	"candy":    22.5,
}

func boolPointer(value bool) *bool { return &value }

func intPointer(value int) *int { return &value }

type modelRadarFourTaskTestRow struct {
	model  string
	effort string
	// scores holds each component's 0-100 result. A component omitted from the
	// map was not graded and must appear in the round's missing list.
	scores      map[string]float64
	missingKind string
}

func modelRadarFourTaskTestRowTotal(row modelRadarFourTaskTestRow) *float64 {
	if row.missingKind != "" {
		return nil
	}
	total := 0.0
	for name, weight := range modelRadarFourTaskTestWeights {
		total += row.scores[name] * weight / 100
	}
	return &total
}

func modelRadarFourTaskTestPayloads(t *testing.T, rows []modelRadarFourTaskTestRow) (modelRadarFourTaskResultsPayload, modelRadarFourTaskSummaryPayload) {
	t.Helper()
	results := modelRadarFourTaskResultsPayload{
		Schema:                "codex-four-task-results/1",
		Protocol:              "codex-four-weighted-150-v2",
		RoundID:               modelRadarFourTaskTestRoundID,
		PublicationStatus:     "authorized_partial",
		PublicationAuthorized: boolPointer(true),
		SourceSHA256:          modelRadarFourTaskTestSourceSHA,
		Expected:              len(rows) * modelRadarFourTaskTaskCount,
		AuthorTestInterval:    modelRadarFourTaskIntervalPayload{StartedAt: "2026-11-13T06:58:34Z", CompletedAt: "2026-11-13T08:50:02Z"},
		VerifiedAt:            "2026-11-13T09:05:17Z",
		Configurations:        len(rows),
		Efforts:               []string{"ultra", "max", "xhigh", "high", "medium", "low"},
		TaskWeights:           modelRadarFourTaskTestWeights,
		ScoreMax:              modelRadarFourTaskScoreMax,
		ModelNames:            map[string]string{},
		ModelEfforts:          map[string][]string{},
	}
	summary := modelRadarFourTaskSummaryPayload{
		Protocol:              results.Protocol,
		RoundID:               results.RoundID,
		PublicationStatus:     results.PublicationStatus,
		PublicationAuthorized: boolPointer(true),
		SourceSHA256:          results.SourceSHA256,
		Expected:              intPointer(results.Expected),
	}
	ungraded, validTotals := 0, 0
	for _, row := range rows {
		if _, seen := results.ModelNames[row.model]; !seen {
			results.Models = append(results.Models, row.model)
			results.ModelNames[row.model] = "GPT Test " + row.model
		}
		results.ModelEfforts[row.model] = append(results.ModelEfforts[row.model], row.effort)
		component := func(name string, score float64) modelRadarFourTaskComponentPayload {
			payload := modelRadarFourTaskComponentPayload{
				State:               modelRadarFourTaskGradedState,
				CostUSD:             float64Pointer(1),
				CostKind:            "api_equivalent_estimate",
				ModelElapsedSeconds: float64Pointer(600),
			}
			switch name {
			case "combined", "pelican":
				payload.Score = &score
			case "pompeii":
				payload.Score100 = &score
			case "candy":
				correct := score == 100
				payload.Correct = &correct
			}
			return payload
		}
		rowPayload := modelRadarFourTaskRowPayload{Model: row.model, Effort: row.effort}
		for _, name := range modelRadarFourTaskComponentNames {
			score, graded := row.scores[name]
			if graded {
				value := component(name, score)
				switch name {
				case "combined":
					rowPayload.Combined = value
				case "pompeii":
					rowPayload.Pompeii = value
				case "pelican":
					rowPayload.Pelican = value
				case "candy":
					rowPayload.Candy = value
				}
				results.Graded++
				continue
			}
			require.Equal(t, row.missingKind, name, "the test row must declare the component it omits")
			missing := modelRadarFourTaskComponentPayload{State: "needs_review", CostKind: "unknown", MissingReason: "required_output_not_delivered"}
			switch name {
			case "combined":
				rowPayload.Combined = missing
			case "pompeii":
				rowPayload.Pompeii = missing
			case "pelican":
				rowPayload.Pelican = missing
			case "candy":
				rowPayload.Candy = missing
			}
			ungraded++
			results.Missing = append(results.Missing, modelRadarFourTaskMissingPayload{
				Model: row.model, Effort: row.effort, Kind: name, Reason: "required_output_not_delivered",
			})
		}
		results.Rows = append(results.Rows, rowPayload)
		total := modelRadarFourTaskTestRowTotal(row)
		if total != nil {
			validTotals++
		}
		summaryRow := modelRadarFourTaskSummaryRowPayload{
			Model: row.model, Effort: row.effort, Score150: total,
			CostUSD: float64Pointer(4), CostKind: "api_equivalent_estimate",
			ModelElapsedSeconds: float64Pointer(2400),
		}
		if total == nil {
			summaryRow.CostUSD = nil
			summaryRow.CostKind = "unknown"
			summaryRow.ModelElapsedSeconds = nil
		}
		summary.Rows = append(summary.Rows, summaryRow)
	}
	results.Ungraded = intPointer(ungraded)
	results.ValidTotalConfigurations = intPointer(validTotals)
	summary.Graded = intPointer(results.Graded)
	summary.Ungraded = intPointer(ungraded)
	summary.ValidTotalConfigurations = intPointer(validTotals)
	return results, summary
}

func modelRadarFourTaskTestRows() []modelRadarFourTaskTestRow {
	return []modelRadarFourTaskTestRow{
		{model: "gpt-test", effort: "high", scores: map[string]float64{"combined": 80, "pompeii": 90, "pelican": 70, "candy": 100}},
		{model: "gpt-test", effort: "low", scores: map[string]float64{"pompeii": 50, "pelican": 60, "candy": 0}, missingKind: "combined"},
		{model: "gpt-other", effort: "high", scores: map[string]float64{"combined": 100, "pompeii": 100, "pelican": 100, "candy": 100}},
	}
}

type modelRadarFourTaskTestServerOptions struct {
	rows      []modelRadarFourTaskTestRow
	intercept func(writer http.ResponseWriter, request *http.Request, key string) bool
}

func newModelRadarFourTaskTestServer(t *testing.T, options modelRadarFourTaskTestServerOptions) *httptest.Server {
	t.Helper()
	rows := options.rows
	if rows == nil {
		rows = modelRadarFourTaskTestRows()
	}
	results, summary := modelRadarFourTaskTestPayloads(t, rows)
	resultsBody, err := common.Marshal(results)
	require.NoError(t, err)
	summaryBody, err := common.Marshal(summary)
	require.NoError(t, err)
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		key := strings.TrimPrefix(request.URL.Path, "/")
		if options.intercept != nil && options.intercept(writer, request, key) {
			return
		}
		var body []byte
		switch request.URL.Path {
		case "/four-task-results":
			body = resultsBody
		case "/four-task-summary":
			body = summaryBody
		default:
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
	}))
}

func modelRadarFourTaskTestSources(server *httptest.Server) modelRadarSources {
	return modelRadarSources{
		FourTaskResults: server.URL + "/four-task-results",
		FourTaskSummary: server.URL + "/four-task-summary",
	}
}

func TestFetchModelRadarFourTaskReadsRound(t *testing.T) {
	server := newModelRadarFourTaskTestServer(t, modelRadarFourTaskTestServerOptions{})
	defer server.Close()

	board, err := fetchModelRadarFourTask(context.Background(), server.Client(), modelRadarFourTaskTestSources(server), 1_700_000_000)

	require.NoError(t, err)
	assert.Equal(t, "codex-four-weighted-150-v2", board.Protocol)
	assert.Equal(t, modelRadarFourTaskTestRoundID, board.RoundID)
	assert.Equal(t, 150, board.ScoreMax)
	assert.Equal(t, 11, board.Graded)
	assert.Equal(t, 12, board.Expected)
	assert.Equal(t, 1, board.Ungraded)
	assert.Equal(t, 2, board.ValidTotals)
	assert.Equal(t, []string{"gpt-test", "gpt-other"}, board.ModelOrder)
	assert.Equal(t, "GPT Test gpt-test", board.ModelNames["gpt-test"])
	assert.Equal(t, int64(1_700_000_000), board.ReadAt)
	require.Len(t, board.Cells, 3)

	cell := board.Cells[0]
	assert.Equal(t, "gpt-test", cell.Model)
	assert.Equal(t, "high", cell.Effort)
	require.NotNil(t, cell.Score)
	assert.InDelta(t, 126.75, *cell.Score, 1e-9)
	require.NotNil(t, cell.Rank)
	assert.Equal(t, 2, *cell.Rank)
	assert.Equal(t, 4, cell.GradedTasks)
	assert.Equal(t, 4, cell.RequiredTasks)
	require.NotNil(t, cell.Components["combined"].Score)
	assert.Equal(t, 80.0, *cell.Components["combined"].Score)
	require.NotNil(t, cell.Components["combined"].Contribution)
	assert.Equal(t, 48.0, *cell.Components["combined"].Contribution)
	require.NotNil(t, cell.CostUSD)
	assert.Equal(t, 4.0, *cell.CostUSD)
	require.NotNil(t, cell.DurationMinutes)
	assert.Equal(t, 40.0, *cell.DurationMinutes)

	missing := board.Cells[1]
	assert.Equal(t, "low", missing.Effort)
	assert.Nil(t, missing.Score)
	assert.Nil(t, missing.Rank)
	assert.Equal(t, 3, missing.GradedTasks)
	assert.Equal(t, "needs_review", missing.Components["combined"].State)
	assert.Nil(t, missing.Components["combined"].Score)
	assert.Equal(t, "required_output_not_delivered", missing.Components["combined"].MissingReason)
	assert.Nil(t, missing.CostUSD, "an unmetered component leaves the total unknown")
	assert.Nil(t, missing.DurationMinutes)

	other := board.Cells[2]
	require.NotNil(t, other.Score)
	assert.Equal(t, 150.0, *other.Score)
	require.NotNil(t, other.Rank)
	assert.Equal(t, 1, *other.Rank)
}

func TestNormalizeModelRadarFourTaskRejectsInconsistentSources(t *testing.T) {
	results, summary := modelRadarFourTaskTestPayloads(t, modelRadarFourTaskTestRows())

	for _, test := range []struct {
		name   string
		change func(*modelRadarFourTaskResultsPayload, *modelRadarFourTaskSummaryPayload)
		want   string
	}{
		{"wrong schema", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.Schema = "radar-bench-public-summary-v6-150-details"
		}, "schema"},
		{"wrong protocol", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.Protocol = "codex-four-weighted-150-v1"
		}, "protocol"},
		{"unauthorized", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.PublicationAuthorized = boolPointer(false)
		}, "authorized"},
		{"wrong score max", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.ScoreMax = 100
		}, "score max"},
		{"different round", func(_ *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			s.RoundID = "other-round"
		}, "different round"},
		{"different source", func(_ *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			s.SourceSHA256 = strings.Repeat("b", 64)
		}, "different source"},
		{"weights do not add up", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.TaskWeights["candy"] = 0
		}, "weights"},
		{"missing weight", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			delete(r.TaskWeights, "candy")
		}, "weights"},
		{"configuration count", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.Configurations = 99
		}, "configuration count"},
		{"expected task count", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.Expected = 99
		}, "expected task count"},
		{"row count out of contract", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.Rows = append(r.Rows, r.Rows[0])
		}, "configuration count"},
		{"summary total differs", func(_ *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			wrong := 100.0
			s.Rows[0].Score150 = &wrong
		}, "does not match the components"},
		{"summary cost differs", func(_ *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			wrong := 5.0
			s.Rows[0].CostUSD = &wrong
		}, "does not match the components"},
		{"summary runtime differs", func(_ *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			wrong := 3600.0
			s.Rows[0].ModelElapsedSeconds = &wrong
		}, "does not match the components"},
		{"component cost kind", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.Rows[0].Pelican.CostKind = "actual_cash"
		}, "cost kind"},
		{"summary cost kind", func(_ *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			s.Rows[0].CostKind = "actual_cash"
		}, "cost kind"},
		{"summary omits cost", func(_ *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			s.Rows[0].CostUSD = nil
		}, "omits a fully observed cost"},
		{"total for incomplete row", func(_ *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			total := 100.0
			s.Rows[1].Score150 = &total
		}, "incomplete row"},
		{"component score out of range", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			wrong := 101.0
			r.Rows[0].Pelican.Score = &wrong
		}, "out of range"},
		{"ungraded component with a score", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			score := 50.0
			r.Rows[1].Combined.Score = &score
		}, "ungraded component"},
		{"unknown missing component", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.Missing[0].Kind = "unknown"
		}, "unknown component"},
		{"undeclared missing component", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.Missing = nil
		}, "not declared"},
		{"graded count differs", func(r *modelRadarFourTaskResultsPayload, s *modelRadarFourTaskSummaryPayload) {
			r.Graded = 10
			s.Graded = intPointer(10)
		}, "graded count"},
		{"valid total count", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.ValidTotalConfigurations = intPointer(1)
		}, "valid total"},
		{"unknown model effort", func(r *modelRadarFourTaskResultsPayload, _ *modelRadarFourTaskSummaryPayload) {
			r.ModelEfforts["gpt-test"] = append(r.ModelEfforts["gpt-test"], "insane")
		}, "unsupported effort"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changedResults := results
			changedSummary := summary
			changedResults.TaskWeights = map[string]float64{}
			for name, weight := range results.TaskWeights {
				changedResults.TaskWeights[name] = weight
			}
			changedResults.Rows = append([]modelRadarFourTaskRowPayload(nil), results.Rows...)
			changedResults.Missing = append([]modelRadarFourTaskMissingPayload(nil), results.Missing...)
			changedResults.ModelEfforts = map[string][]string{}
			for model, efforts := range results.ModelEfforts {
				changedResults.ModelEfforts[model] = append([]string(nil), efforts...)
			}
			changedSummary.Rows = append([]modelRadarFourTaskSummaryRowPayload(nil), summary.Rows...)
			test.change(&changedResults, &changedSummary)
			_, err := normalizeModelRadarFourTask(changedResults, changedSummary, 1)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestNormalizeModelRadarFourTaskAcceptsNextRevision(t *testing.T) {
	results, summary := modelRadarFourTaskTestPayloads(t, modelRadarFourTaskTestRows())
	// A later additive revision must not freeze the board the way an exact
	// schema match froze the previous radar source.
	results.Schema = "codex-four-task-results/2"
	results.Protocol = "codex-four-weighted-150-v3"
	summary.Protocol = results.Protocol

	board, err := normalizeModelRadarFourTask(results, summary, 1)

	require.NoError(t, err)
	assert.Equal(t, 2, board.ValidTotals)
}

func TestNormalizeModelRadarFourTaskAcceptsCompletePublication(t *testing.T) {
	rows := []modelRadarFourTaskTestRow{
		{model: "gpt-test", effort: "high", scores: map[string]float64{"combined": 80, "pompeii": 90, "pelican": 70, "candy": 100}},
		{model: "gpt-other", effort: "high", scores: map[string]float64{"combined": 100, "pompeii": 100, "pelican": 100, "candy": 100}},
	}
	results, summary := modelRadarFourTaskTestPayloads(t, rows)
	// A complete round publishes every task: the partial-round bookkeeping
	// fields and the per-component state disappear from both documents.
	results.PublicationStatus = "complete"
	results.PublicationContract = "codex-four-accepted-complete/1"
	results.PublicationAuthorized = nil
	results.AllGraded = boolPointer(true)
	results.Ungraded = nil
	results.ValidTotalConfigurations = nil
	results.Missing = nil
	for index := range results.Rows {
		results.Rows[index].Combined.State = ""
		results.Rows[index].Pompeii.State = ""
		results.Rows[index].Pelican.State = ""
		results.Rows[index].Candy.State = ""
	}
	summary.PublicationStatus = ""
	summary.PublicationContract = ""
	summary.PublicationAuthorized = nil
	summary.AllGraded = nil
	summary.Graded = nil
	summary.Expected = nil
	summary.Ungraded = nil
	summary.ValidTotalConfigurations = nil

	board, err := normalizeModelRadarFourTask(results, summary, 1)

	require.NoError(t, err)
	assert.Equal(t, "complete", board.PublicationStatus)
	assert.Equal(t, 0, board.Ungraded)
	assert.Equal(t, 2, board.ValidTotals)
	assert.Equal(t, modelRadarFourTaskGradedState, board.Cells[0].Components["combined"].State)

	partialResults, partialSummary := modelRadarFourTaskTestPayloads(t, modelRadarFourTaskTestRows())
	partialResults.PublicationStatus = "complete"
	partialResults.AllGraded = boolPointer(true)
	_, err = normalizeModelRadarFourTask(partialResults, partialSummary, 1)
	require.ErrorContains(t, err, "claims every task is graded")
}

func TestSyncModelRadarPersistsFourTaskSection(t *testing.T) {
	setupModelRadarServiceTestDB(t)
	fail := false
	server := newModelRadarFourTaskTestServer(t, modelRadarFourTaskTestServerOptions{
		intercept: func(writer http.ResponseWriter, _ *http.Request, key string) bool {
			if fail && key == "four-task-results" {
				writer.WriteHeader(http.StatusInternalServerError)
				return true
			}
			return false
		},
	})
	defer server.Close()
	sources := modelRadarFourTaskTestSources(server)

	first, err := syncModelRadar(context.Background(), server.Client(), sources, 2_000_000_000)
	require.NoError(t, err)
	assert.Equal(t, 2, first.ModelCount)
	assert.Equal(t, 3, first.ConfigurationCount)

	data, err := GetModelRadar(context.Background())
	require.NoError(t, err)
	require.NotNil(t, data.FourTask)
	assert.Equal(t, 3, len(data.FourTask.Cells))
	assert.Equal(t, int64(2_000_000_000), data.FourTask.ReadAt)
	assert.Equal(t, 3, data.ConfigurationCount)

	// The results source goes down: the sync fails and leaves the previous
	// snapshot in place instead of publishing a half-updated round.
	fail = true
	_, err = syncModelRadar(context.Background(), server.Client(), sources, 2_000_060_000)
	require.ErrorContains(t, err, "four-task results")
	data, err = GetModelRadar(context.Background())
	require.NoError(t, err)
	require.NotNil(t, data.FourTask)
	assert.Equal(t, int64(2_000_000_000), data.FourTask.ReadAt, "a failed round keeps the previous section")
	assert.Equal(t, 3, len(data.FourTask.Cells))
}
