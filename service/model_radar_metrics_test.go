package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchModelRadarUsesLiveMetricsAndPublishedHistory(t *testing.T) {
	efficiency, insights := modelRadarTestPayloads(t)
	var published modelRadarEfficiencyPayload
	require.NoError(t, common.Unmarshal(efficiency, &published))
	oldFrame := published.History[0]
	oldFrame.At = "2026-07-23T00:00:00Z"
	published.History = append([]modelRadarUpstreamHistoryFrame{oldFrame}, published.History...)
	efficiency, err := common.Marshal(published)
	require.NoError(t, err)
	server := newModelRadarSourceServer(t, efficiency, insights, http.StatusOK)
	defer server.Close()
	metricsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema":3,"mode":"equal_latest_3","benchmark_id":"deep-swe","scoring_mode":"binary-majority","source_updated_at":"2026-07-27T00:00:00Z","points":[{"model":"gpt-test","effort":"high","passed":4,"total":5,"iq":120,"average_price_usd":2.5,"average_minutes":9,"runs_24h":8,"runs_48h":10,"runs_total":15,"source_updated_at":"2026-07-26T23:00:00Z"}]}`))
	}))
	defer metricsServer.Close()

	data, err := fetchModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", metricsServer.URL, server.URL+"/insights")
	require.NoError(t, err)
	require.Len(t, data.Configurations, 1)
	configuration := data.Configurations[0]
	assert.Equal(t, 120.0, configuration.IQ)
	assert.Equal(t, 4, configuration.Passed)
	assert.Equal(t, 5, configuration.ValidTasks)
	assert.Equal(t, "codex", configuration.Harness)
	require.NotNil(t, configuration.TotalRuns)
	assert.Equal(t, 15, *configuration.TotalRuns)
	require.NotNil(t, configuration.Runs24h)
	assert.Equal(t, 8, *configuration.Runs24h)
	require.NotNil(t, configuration.AveragePriceUSD)
	assert.Equal(t, 2.5, *configuration.AveragePriceUSD)
	assert.Nil(t, configuration.PriceSamples, "missing live metrics must not be filled from an older snapshot")
	assert.Nil(t, configuration.AveragePriceUSDByBand)
	assert.Equal(t, time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC).Unix(), data.SourceUpdatedAt)
	require.Len(t, data.History, 3)
	assert.Equal(t, 75.0, data.History[0].Points[0].IQ)
	assert.Equal(t, 120.0, data.History[2].Points[0].IQ)
	assert.Equal(t, data.SourceUpdatedAt, data.History[2].Ts)
	require.NotNil(t, configuration.LatestGradedAt)
	assert.Equal(t, time.Date(2026, 7, 26, 23, 0, 0, 0, time.UTC).Unix(), *configuration.LatestGradedAt)

	encoded, err := common.Marshal(data)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "recommendations")
}

func TestNormalizeModelRadarMetricsNormalizesLiveCostScale(t *testing.T) {
	var payload modelRadarMetricsPayload
	require.NoError(t, common.Unmarshal([]byte(`{"schema":3,"mode":"equal_latest_3","benchmark_id":"deep-swe","scoring_mode":"binary-majority","source_updated_at":"2026-07-27T00:00:00Z","points":[{"model":"a","effort":"high","passed":2,"total":3,"iq":100,"combined_cost_index":200},{"model":"b","effort":"high","passed":1,"total":3,"iq":50,"combined_cost_index":400}]}`), &payload))
	configurations, _, err := normalizeModelRadarMetrics(payload, nil)
	require.NoError(t, err)
	require.Len(t, configurations, 2)
	require.NotNil(t, configurations[0].CombinedCostIndex)
	require.NotNil(t, configurations[1].CombinedCostIndex)
	assert.Equal(t, 50.0, *configurations[0].CombinedCostIndex)
	assert.Equal(t, 100.0, *configurations[1].CombinedCostIndex)
	assert.Equal(t, 200.0, *payload.Points[0].CombinedCostIndex, "normalization must not mutate the source payload")
}

func TestNormalizeModelRadarMetricsSkipsUngradedPlaceholders(t *testing.T) {
	// The live API lists new model/effort pairs before their first graded run
	// as iq=null rows. They must not abort the sync, and a response containing
	// only placeholders must not blank the stored snapshot.
	var payload modelRadarMetricsPayload
	require.NoError(t, common.Unmarshal([]byte(`{"schema":3,"mode":"equal_latest_3","benchmark_id":"deep-swe","scoring_mode":"binary-majority","source_updated_at":"2026-09-11T01:10:00Z","points":[{"model":"deepseek-v4.1-flash","effort":"max","iq":null,"passed":0,"total":0,"runs_total":0},{"model":"gpt-test","effort":"high","iq":100,"passed":2,"total":3,"runs_total":9}]}`), &payload))

	configurations, frame, err := normalizeModelRadarMetrics(payload, nil)
	require.NoError(t, err)
	require.Len(t, configurations, 1)
	assert.Equal(t, "gpt-test", configurations[0].Model)
	require.Len(t, frame.Points, 1)
	assert.Equal(t, "gpt-test", frame.Points[0].Model)

	var placeholdersOnly modelRadarMetricsPayload
	require.NoError(t, common.Unmarshal([]byte(`{"schema":3,"mode":"equal_latest_3","benchmark_id":"deep-swe","scoring_mode":"binary-majority","source_updated_at":"2026-09-11T01:10:00Z","points":[{"model":"deepseek-v4.1-flash","effort":"max","iq":null,"passed":0,"total":0,"runs_total":0}]}`), &placeholdersOnly))
	_, _, err = normalizeModelRadarMetrics(placeholdersOnly, nil)
	require.ErrorContains(t, err, "no graded configurations")
}

func TestNormalizeModelRadarMetricsRejectsInvalidCohortsAndCounts(t *testing.T) {
	var payload modelRadarMetricsPayload
	require.NoError(t, common.Unmarshal([]byte(`{"schema":3,"mode":"equal_latest_3","benchmark_id":"deep-swe","scoring_mode":"binary-majority","source_updated_at":"2026-07-27T00:00:00Z","points":[{"model":"a","effort":"high","passed":2,"total":3,"iq":100,"combined_cost_index":200}]}`), &payload))
	for _, test := range []struct {
		name   string
		change func(*modelRadarMetricsPayload)
		want   string
	}{
		{"wrong benchmark", func(p *modelRadarMetricsPayload) { p.BenchmarkID = "pompeii-adjacency" }, "benchmark"},
		{"wrong scoring", func(p *modelRadarMetricsPayload) { p.ScoringMode = "continuous-macro" }, "benchmark"},
		{"wrong version", func(p *modelRadarMetricsPayload) { p.Schema = 4 }, "schema"},
		{"missing total", func(p *modelRadarMetricsPayload) { p.Points[0].Total = nil }, "required"},
		{"negative runs", func(p *modelRadarMetricsPayload) { v := -1; p.Points[0].RunsTotal = &v }, "total_runs"},
		{"negative cost", func(p *modelRadarMetricsPayload) { v := -1.0; p.Points[0].CombinedCostIndex = &v }, "combined_cost_index"},
		{"duplicate configuration", func(p *modelRadarMetricsPayload) { p.Points = append(p.Points, p.Points[0]) }, "duplicate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := payload
			input.Points = append([]modelRadarMetricsPoint(nil), payload.Points...)
			test.change(&input)
			_, _, err := normalizeModelRadarMetrics(input, nil)
			require.ErrorContains(t, err, test.want)
		})
	}
}
