package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func modelRadarTestPayloads(t *testing.T) ([]byte, []byte) {
	t.Helper()
	efficiency, err := common.Marshal(map[string]any{
		"schema":            2,
		"type":              modelRadarEfficiencyType,
		"source_updated_at": "2026-07-26T08:00:00+08:00",
		"points": []map[string]any{{
			"model": "gpt-test", "effort": "high", "iq": 90.0,
			"harness": " Codex ", "runs_24h": 2, "runs_48h": 4,
			"average_price_usd_by_band": map[string]float64{"off_peak": 0.5, "peak": 1.5},
			"passed":                    3, "valid_tasks": 5, "average_price_usd": 1.5,
			"price_samples": 5, "average_minutes": 8.0, "duration_samples": 5,
			"total_runs": 7, "latest_graded_at": "2026-07-26T00:00:00Z",
			"average_agent_steps": 12.0, "agent_steps_samples": 5,
			"average_total_tokens": 4000.0, "token_samples": 5,
			"cache_hit_rate": 0.75, "cache_token_samples": 5,
			"combined_cost_index": 25.0,
		}},
		"history": []map[string]any{
			{
				"at": "2026-07-25T00:00:00Z",
				"points": []map[string]any{{
					"model": "gpt-test", "effort": "high", "iq": 75.0,
					"passed": 2, "valid_tasks": 4, "average_price_usd": 1.2,
				}},
			},
			{
				"at": "2026-07-26T00:00:00Z",
				"points": []map[string]any{{
					"model": "gpt-test", "effort": "high", "iq": 90.0,
					"passed": 3, "valid_tasks": 5, "average_price_usd": 1.5,
				}},
			},
		},
	})
	require.NoError(t, err)
	insights, err := common.Marshal(map[string]any{
		"schema": 1, "source_updated_at": "2026-07-26T00:01:00Z",
		"recommendations": []map[string]any{{"title": "must not persist"}},
		"comprehensive_points": []map[string]any{{
			"model": "gpt-test", "effort": "high", "iq": 95.0,
			"software_iq": 90.0, "visual_iq": 120.0, "samples": 10,
		}},
		"degradation_alerts": map[string]any{
			"items": []map[string]any{{
				"model": "gpt-test", "effort": "high", "iq": 90.0,
				"degradation_12h_iq": 1.0, "degradation_24h_iq": 2.0,
				"degradation_48h_iq": 3.0,
			}},
		},
	})
	require.NoError(t, err)
	return efficiency, insights
}

func newModelRadarSourceServer(t *testing.T, efficiency []byte, insights []byte, insightsStatus int) *httptest.Server {
	t.Helper()
	validEfficiency, _ := modelRadarTestPayloads(t)
	var published modelRadarEfficiencyPayload
	require.NoError(t, common.Unmarshal(validEfficiency, &published))
	point := published.Points[0]
	metrics, err := common.Marshal(modelRadarMetricsPayload{
		Schema: 3, Mode: "equal_latest_3", BenchmarkID: "deep-swe", ScoringMode: "binary-majority",
		SourceUpdatedAt: published.SourceUpdatedAt,
		Points:          []modelRadarMetricsPoint{{modelRadarUpstreamPoint: point, Total: point.ValidTasks, RunsTotal: point.TotalRuns}},
	})
	require.NoError(t, err)
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/efficiency":
			_, _ = writer.Write(efficiency)
		case "/metrics":
			_, _ = writer.Write(metrics)
		case "/insights":
			writer.WriteHeader(insightsStatus)
			_, _ = writer.Write(insights)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
}

func setupModelRadarServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-radar-service.db")), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.ModelRadarSnapshot{}))
	t.Cleanup(func() { model.DB = previousDB })
	return db
}

func TestFetchModelRadarNormalizesCapabilityDataAndDropsRecommendations(t *testing.T) {
	efficiency, insights := modelRadarTestPayloads(t)
	server := newModelRadarSourceServer(t, efficiency, insights, http.StatusOK)
	defer server.Close()

	data, err := fetchModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", server.URL+"/metrics", server.URL+"/insights")
	require.NoError(t, err)
	assert.Equal(t, 1, data.ModelCount)
	assert.Equal(t, 1, data.ConfigurationCount)
	require.Len(t, data.Configurations, 1)
	assert.Equal(t, "gpt-test", data.Configurations[0].Model)
	assert.Equal(t, 90.0, data.Configurations[0].IQ)
	require.NotNil(t, data.Configurations[0].ComprehensiveIQ)
	assert.Equal(t, 95.0, *data.Configurations[0].ComprehensiveIQ)
	require.NotNil(t, data.Configurations[0].VisualIQ)
	assert.Equal(t, 120.0, *data.Configurations[0].VisualIQ)
	configuration := data.Configurations[0]
	assert.Equal(t, "codex", configuration.Harness)
	require.NotNil(t, configuration.Runs24h)
	assert.Equal(t, 2, *configuration.Runs24h)
	require.NotNil(t, configuration.Runs48h)
	assert.Equal(t, 4, *configuration.Runs48h)
	require.NotNil(t, configuration.AveragePriceUSDByBand)
	require.NotNil(t, configuration.AveragePriceUSDByBand.OffPeak)
	require.NotNil(t, configuration.AveragePriceUSDByBand.Peak)
	assert.Equal(t, 0.5, *configuration.AveragePriceUSDByBand.OffPeak)
	assert.Equal(t, 1.5, *configuration.AveragePriceUSDByBand.Peak)
	require.NotNil(t, data.Configurations[0].LatestGradedAt)
	assert.Equal(t, int64(1785024000), *data.Configurations[0].LatestGradedAt)
	require.Len(t, data.History, 2)
	require.Len(t, data.DegradationAlerts, 1)
	require.NotNil(t, data.DegradationAlerts[0].Degradation48hIQ)
	assert.Equal(t, 3.0, *data.DegradationAlerts[0].Degradation48hIQ)

	encoded, err := common.Marshal(data)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "recommendations")
	assert.NotContains(t, string(encoded), "must not persist")
}

func TestFetchModelRadarRejectsInvalidSourceContracts(t *testing.T) {
	efficiency, insights := modelRadarTestPayloads(t)

	tests := []struct {
		name       string
		efficiency []byte
		insights   []byte
		status     int
		want       string
	}{
		{name: "invalid JSON", efficiency: []byte(`{"schema":`), insights: insights, status: http.StatusOK, want: "decode"},
		{name: "wrong efficiency schema", efficiency: []byte(`{"schema":3,"type":"distributed_intelligence_efficiency","source_updated_at":"2026-07-26T00:00:00Z","points":[{}],"history":[{"at":"2026-07-26T00:00:00Z","points":[]}]}`), insights: insights, status: http.StatusOK, want: "unsupported source schema"},
		{name: "insights unavailable", efficiency: efficiency, insights: insights, status: http.StatusBadGateway, want: "unexpected HTTP status 502"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newModelRadarSourceServer(t, test.efficiency, test.insights, test.status)
			defer server.Close()
			_, err := fetchModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", server.URL+"/metrics", server.URL+"/insights")
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestFetchModelRadarSourceRejectsResponseOverLimit(t *testing.T) {
	// A body over the cap must report the cap, not the truncated-JSON decode
	// error it causes, on both the streaming and the buffered decode paths.
	efficiency, _ := modelRadarTestPayloads(t)
	server := newModelRadarSourceServer(t, efficiency, nil, http.StatusOK)
	defer server.Close()

	for name, decode := range map[string]func(io.Reader) error{
		"streaming": func(body io.Reader) error {
			_, err := decodeModelRadarEfficiency(body)
			return err
		},
		"buffered": func(body io.Reader) error {
			var payload modelRadarEfficiencyPayload
			data, err := io.ReadAll(body)
			if err != nil {
				return err
			}
			return common.Unmarshal(data, &payload)
		},
	} {
		t.Run(name, func(t *testing.T) {
			read, err := fetchModelRadarSource(context.Background(), server.Client(), server.URL+"/efficiency", 64, decode)
			require.ErrorContains(t, err, "response exceeds 64 bytes")
			assert.Equal(t, int64(65), read)
		})
	}
}

func TestFetchModelRadarHonorsRequestDeadline(t *testing.T) {
	efficiency, _ := modelRadarTestPayloads(t)
	server := newModelRadarSourceServer(t, efficiency, nil, http.StatusOK)
	defer server.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	var payload modelRadarEfficiencyPayload
	err := fetchModelRadarJSON(ctx, server.Client(), server.URL+"/efficiency", modelRadarEfficiencyMaxBytes, &payload)
	require.ErrorContains(t, err, "context deadline exceeded")
}

func TestFetchModelRadarRejectsNonJSONContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte(`{"schema":2}`))
	}))
	defer server.Close()

	var payload modelRadarEfficiencyPayload
	err := fetchModelRadarJSON(context.Background(), server.Client(), server.URL, modelRadarEfficiencyMaxBytes, &payload)
	require.ErrorContains(t, err, "expected JSON response")
}

func TestNormalizeModelRadarEfficiencyIgnoresUnusablePublishedPoints(t *testing.T) {
	// Published points only supply the runner name; the live metrics API owns
	// the capability data. A malformed, placeholder, or duplicate published
	// point must not block the sync, and history placeholders are dropped.
	efficiency, _ := modelRadarTestPayloads(t)
	var payload modelRadarEfficiencyPayload
	require.NoError(t, common.Unmarshal(efficiency, &payload))
	outOfRange := 151.0
	payload.Points = append(payload.Points,
		modelRadarUpstreamPoint{Model: "gpt-test", Effort: "high", Harness: "other"},
		modelRadarUpstreamPoint{Model: "gpt-new", Effort: "max", Harness: "DSH", IQ: &outOfRange},
		modelRadarUpstreamPoint{Model: "", Effort: "low", Harness: "codex"},
		modelRadarUpstreamPoint{Model: "gpt-long", Effort: "low", Harness: strings.Repeat("a", 33)},
	)
	payload.History[1].Points = append(payload.History[1].Points, modelRadarUpstreamPoint{Model: "gpt-new", Effort: "max"})

	harnesses, history, err := normalizeModelRadarEfficiency(payload)

	require.NoError(t, err)
	assert.Equal(t, map[string]string{"gpt-test|high": "codex", "gpt-new|max": "dsh"}, harnesses)
	require.Len(t, history, 2)
	require.Len(t, history[1].Points, 1)
	assert.Equal(t, "gpt-test", history[1].Points[0].Model)
}

func TestNormalizeModelRadarStationMetrics(t *testing.T) {
	efficiency, _ := modelRadarTestPayloads(t)
	var payload modelRadarEfficiencyPayload
	require.NoError(t, common.Unmarshal(efficiency, &payload))
	negative := -1
	zero := 0
	negativePrice := -0.1
	infinitePrice := math.Inf(1)
	nanPrice := math.NaN()
	tests := []struct {
		name    string
		harness string
		runs24h *int
		runs48h *int
		band    *ModelRadarPriceBand
		want    string
	}{
		{name: "legacy fields absent"},
		{name: "zero runs are preserved", harness: " DSH ", runs24h: &zero, runs48h: &zero, band: &ModelRadarPriceBand{}},
		{name: "negative daily runs", runs24h: &negative, want: "runs_24h"},
		{name: "negative two day runs", runs48h: &negative, want: "runs_48h"},
		{name: "overlong harness", harness: strings.Repeat("a", 33), want: "harness"},
		{name: "negative off peak price", band: &ModelRadarPriceBand{OffPeak: &negativePrice}, want: "off_peak"},
		{name: "negative peak price", band: &ModelRadarPriceBand{Peak: &negativePrice}, want: "peak"},
		{name: "infinite price", band: &ModelRadarPriceBand{Peak: &infinitePrice}, want: "peak"},
		{name: "nan price", band: &ModelRadarPriceBand{OffPeak: &nanPrice}, want: "off_peak"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			point := payload.Points[0]
			point.Harness, point.Runs24h, point.Runs48h, point.AveragePriceUSDByBand = test.harness, test.runs24h, test.runs48h, test.band
			configuration, _, err := normalizeModelRadarConfiguration(point)
			if test.want != "" {
				require.ErrorContains(t, err, test.want)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, strings.ToLower(strings.TrimSpace(test.harness)), configuration.Harness)
			assert.Equal(t, test.runs24h, configuration.Runs24h)
			assert.Equal(t, test.runs48h, configuration.Runs48h)
			assert.Equal(t, test.band, configuration.AveragePriceUSDByBand)
		})
	}
}

func TestDecodeModelRadarEfficiencyStreamsOnlyRecentHistory(t *testing.T) {
	// Upstream keeps every frame since launch, which pushed the response past
	// the fetch cap twice. Decoding must keep only the 72h window of the newest
	// frame and skip fields sync does not read, however large they are.
	efficiency, _ := modelRadarTestPayloads(t)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(efficiency, &payload))
	frames := payload["history"].([]any)
	oldFrame := map[string]any{
		"at":     "2026-07-01T00:00:00Z",
		"points": []map[string]any{{"model": "gpt-test", "effort": "high", "iq": 80.0, "passed": 2, "valid_tasks": 4}},
	}
	payload["history"] = append([]any{oldFrame}, frames...)
	payload["method"] = map[string]any{"iq": "pass_rate * 150", "nested": []any{[]any{1, 2}, map[string]any{"a": "b"}}}
	payload["fingerprint"] = strings.Repeat("f", 4096)
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)

	decoded, err := decodeModelRadarEfficiency(bytes.NewReader(encoded))

	require.NoError(t, err)
	assert.Equal(t, modelRadarEfficiencySchema, decoded.Schema)
	assert.Equal(t, modelRadarEfficiencyType, decoded.Type)
	require.Len(t, decoded.Points, 1)
	require.Len(t, decoded.History, 2)
	assert.Equal(t, "2026-07-25T00:00:00Z", decoded.History[0].At)
	assert.Equal(t, "2026-07-26T00:00:00Z", decoded.History[1].At)
}

func TestDecodeModelRadarEfficiencyRejectsUnorderedHistory(t *testing.T) {
	body := `{"schema":2,"history":[{"at":"2026-07-26T00:00:00Z","points":[]},{"at":"2026-07-25T00:00:00Z","points":[]}]}`

	_, err := decodeModelRadarEfficiency(strings.NewReader(body))

	require.ErrorContains(t, err, "strictly increasing")
}

func TestNormalizeModelRadarInsightsAllowsNoAlerts(t *testing.T) {
	alerts, updatedAt, skipped, err := normalizeModelRadarInsights(modelRadarInsightsPayload{
		Schema:          modelRadarInsightsSchema,
		SourceUpdatedAt: "2026-07-26T00:01:00Z",
	})
	require.NoError(t, err)
	assert.Empty(t, alerts)
	assert.Zero(t, skipped)
	assert.Equal(t, int64(1785024060), updatedAt)
}

func TestFetchModelRadarKeepsAlertsWhoseWindowLacksHistory(t *testing.T) {
	// Regression for 2026-09-23: upstream published grok-4.7 alerts with a null
	// degradation_48h_iq because the tier was graded under 48 hours ago, and
	// the strict check failed every sync until the tier aged. The alert must
	// be kept with the missing window left empty, alongside the trend.
	efficiency, _ := modelRadarTestPayloads(t)
	insights := []byte(`{"schema":1,"source_updated_at":"2026-09-24T10:01:22+00:00","comprehensive_points":[],` +
		`"degradation_alerts":{"rule":"...","items":[{"model":"gpt-test","effort":"high","iq":105,` +
		`"degradation_12h_iq":45,"degradation_24h_iq":45,"degradation_48h_iq":null,` +
		`"average_iq_24h":142.81,"average_iq_48h":null,"degradation_severity_score":6.7,` +
		`"trend_48h":[{"timestamp":"2026-09-22T14:00:54+00:00","iq":150,"samples":2},` +
		`{"timestamp":"not-a-time","iq":140,"samples":3},` +
		`{"timestamp":"2026-09-24T10:31:51+00:00","iq":105,"samples":10}]}]}}`)
	server := newModelRadarSourceServer(t, efficiency, insights, http.StatusOK)
	defer server.Close()

	data, err := fetchModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", server.URL+"/metrics", server.URL+"/insights")

	require.NoError(t, err)
	require.Len(t, data.DegradationAlerts, 1)
	alert := data.DegradationAlerts[0]
	require.NotNil(t, alert.Degradation24hIQ)
	assert.Equal(t, 45.0, *alert.Degradation24hIQ)
	assert.Nil(t, alert.Degradation48hIQ)
	require.NotNil(t, alert.AverageIQ24h)
	assert.Equal(t, 142.81, *alert.AverageIQ24h)
	assert.Nil(t, alert.AverageIQ48h)
	assert.Equal(t, []ModelRadarTrendPoint{
		{Ts: time.Date(2026, 9, 22, 14, 0, 54, 0, time.UTC).Unix(), IQ: 150, Samples: 2},
		{Ts: time.Date(2026, 9, 24, 10, 31, 51, 0, time.UTC).Unix(), IQ: 105, Samples: 10},
	}, alert.Trend48h)

	encoded, err := common.Marshal(alert)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"degradation_48h_iq":null`)
}

func TestNormalizeModelRadarInsightsSkipsInvalidAlertsInsteadOfFailing(t *testing.T) {
	iq, decline, invalidDecline := 39.0, -0.2, 200.0
	valid := modelRadarUpstreamAlert{Model: "gpt-5.6-terra", Effort: "low", IQ: &iq, Degradation48hIQ: &decline}
	payload := modelRadarInsightsPayload{Schema: modelRadarInsightsSchema, SourceUpdatedAt: "2026-07-26T13:22:03Z"}
	payload.DegradationAlerts.Items = []modelRadarUpstreamAlert{
		valid,
		valid,
		{Model: "gpt-missing-iq", Effort: "low"},
		{Model: "gpt-bad-window", Effort: "low", IQ: &iq, Degradation12hIQ: &invalidDecline},
		{Model: "", Effort: "low", IQ: &iq},
	}

	alerts, _, skipped, err := normalizeModelRadarInsights(payload)

	require.NoError(t, err)
	require.Len(t, alerts, 1)
	require.NotNil(t, alerts[0].Degradation48hIQ)
	assert.Equal(t, -0.2, *alerts[0].Degradation48hIQ, "a recovering tier keeps its signed decline")
	assert.Nil(t, alerts[0].Degradation12hIQ)
	assert.Empty(t, alerts[0].Trend48h)
	assert.Equal(t, 4, skipped)

	payload.Schema = 2
	_, _, _, err = normalizeModelRadarInsights(payload)
	require.ErrorContains(t, err, "unsupported insights schema")
}

func TestNormalizeModelRadarInsightsKeepsTheMostSevereAlertsWithinLimit(t *testing.T) {
	iq := 80.0
	payload := modelRadarInsightsPayload{Schema: modelRadarInsightsSchema, SourceUpdatedAt: "2026-07-26T13:22:03Z"}
	for index := range modelRadarMaxAlerts + 2 {
		payload.DegradationAlerts.Items = append(payload.DegradationAlerts.Items,
			modelRadarUpstreamAlert{Model: fmt.Sprintf("model-%d", index), Effort: "high", IQ: &iq})
	}

	alerts, _, skipped, err := normalizeModelRadarInsights(payload)

	require.NoError(t, err)
	require.Len(t, alerts, modelRadarMaxAlerts)
	assert.Equal(t, "model-0", alerts[0].Model)
	assert.Equal(t, 2, skipped)
}

func TestNormalizeModelRadarComprehensiveIndexesValidPointsAndSkipsTheRest(t *testing.T) {
	iq, visual, outOfRange := 95.0, 120.0, 200.0
	samples, negative := 10, -1
	metrics, skipped, err := normalizeModelRadarComprehensive(modelRadarInsightsPayload{
		Schema: modelRadarInsightsSchema,
		ComprehensivePoints: []modelRadarComprehensivePoint{
			{Model: "gpt-test", Effort: "high", IQ: &iq, VisualIQ: &visual, Samples: &samples},
			{Model: "gpt-test", Effort: "high", IQ: &visual, VisualIQ: &visual},
			{Model: "gpt-range", Effort: "high", IQ: &iq, VisualIQ: &outOfRange},
			{Model: "gpt-no-visual", Effort: "high", IQ: &iq},
			{Model: "gpt-samples", Effort: "high", IQ: &iq, VisualIQ: &visual, Samples: &negative},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, map[string]modelRadarComprehensiveMetrics{"gpt-test|high": {IQ: 95, VisualIQ: 120}}, metrics)
	assert.Equal(t, 4, skipped)
}

func TestSyncModelRadarDoesNotReplaceSnapshotWhenOneSourceFails(t *testing.T) {
	setupModelRadarServiceTestDB(t)
	ctx := context.Background()
	require.NoError(t, model.SaveModelRadarSnapshot(ctx, &model.ModelRadarSnapshot{
		SchemaVersion: 1, Payload: []byte(`{"schema_version":1,"model_count":9}`),
		SourceUpdatedAt: 10, AlertsUpdatedAt: 11, FetchedAt: 12,
	}))
	efficiency, insights := modelRadarTestPayloads(t)
	server := newModelRadarSourceServer(t, efficiency, insights, http.StatusBadGateway)
	defer server.Close()

	_, err := syncModelRadar(ctx, server.Client(), server.URL+"/efficiency", server.URL+"/metrics", server.URL+"/insights", 1000)
	require.Error(t, err)
	snapshot, err := model.GetModelRadarSnapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, []byte(`{"schema_version":1,"model_count":9}`), snapshot.Payload)
	assert.Equal(t, int64(12), snapshot.FetchedAt)
}

func TestSyncModelRadarPersistsValidatedSnapshot(t *testing.T) {
	setupModelRadarServiceTestDB(t)
	efficiency, insights := modelRadarTestPayloads(t)
	server := newModelRadarSourceServer(t, efficiency, insights, http.StatusOK)
	defer server.Close()

	result, err := syncModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", server.URL+"/metrics", server.URL+"/insights", 2_000_000_000)
	require.NoError(t, err)
	assert.Equal(t, 1, result.ModelCount)
	assert.Equal(t, 1, result.AlertCount)

	snapshot, err := model.GetModelRadarSnapshot(context.Background())
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Equal(t, int64(2_000_000_000), snapshot.FetchedAt)
	assert.NotContains(t, string(snapshot.Payload), "recommendations")
	data, err := GetModelRadar(context.Background())
	require.NoError(t, err)
	require.Len(t, data.Configurations, 1)
	assert.Equal(t, "codex", data.Configurations[0].Harness)
	require.NotNil(t, data.Configurations[0].Runs24h)
	assert.Equal(t, 2, *data.Configurations[0].Runs24h)
	require.NotNil(t, data.Configurations[0].AveragePriceUSDByBand)
	require.NotNil(t, data.Configurations[0].AveragePriceUSDByBand.Peak)
	assert.Equal(t, 1.5, *data.Configurations[0].AveragePriceUSDByBand.Peak)
}

func TestGetModelRadarTreatsFreshFetchAsCurrentWhenSourceHasNotChanged(t *testing.T) {
	setupModelRadarServiceTestDB(t)
	t.Setenv("MODEL_RADAR_SYNC_INTERVAL_MINUTES", "10")
	payload, err := common.Marshal(ModelRadarData{SchemaVersion: modelRadarSchemaVersion})
	require.NoError(t, err)
	require.NoError(t, model.SaveModelRadarSnapshot(context.Background(), &model.ModelRadarSnapshot{
		SchemaVersion:   modelRadarSchemaVersion,
		Payload:         payload,
		SourceUpdatedAt: 1,
		AlertsUpdatedAt: 1,
		FetchedAt:       common.GetTimestamp(),
	}))

	data, err := GetModelRadar(context.Background())
	require.NoError(t, err)
	assert.False(t, data.Stale)
}

func TestIsModelRadarStaleUsesLastSuccessfulFetch(t *testing.T) {
	now := int64(10_000)
	threshold := 30 * time.Minute
	assert.False(t, IsModelRadarStale(now, 9_000, threshold))
	assert.True(t, IsModelRadarStale(now, 8_000, threshold))
	assert.True(t, IsModelRadarStale(now, 0, threshold))
}

func TestModelRadarSyncIntervalUsesTenMinuteFloor(t *testing.T) {
	t.Setenv("MODEL_RADAR_SYNC_INTERVAL_MINUTES", "1")
	assert.Equal(t, 10*time.Minute, ModelRadarSyncInterval())
	t.Setenv("MODEL_RADAR_SYNC_INTERVAL_MINUTES", "25")
	assert.Equal(t, 25*time.Minute, ModelRadarSyncInterval())
	assert.Equal(t, 75*time.Minute, ModelRadarStaleAfter())
}

func TestSyncModelRadarLiveSource(t *testing.T) {
	if os.Getenv("TEST_MODEL_RADAR_LIVE") != "1" {
		t.Skip("set TEST_MODEL_RADAR_LIVE=1 to run the live CodexRadar smoke test")
	}
	setupModelRadarServiceTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := SyncModelRadar(ctx)
	require.NoError(t, err)
	assert.Positive(t, result.ModelCount)
	assert.Positive(t, result.ConfigurationCount)

	snapshot, err := GetModelRadar(ctx)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.NotEmpty(t, snapshot.History)
	assert.Equal(t, modelRadarSourceURL, snapshot.Source.URL)
}

func float64Pointer(value float64) *float64 { return &value }

func TestBuildModelRadarAutoEffortIndexFiltersAndOrders(t *testing.T) {
	configurations := []ModelRadarConfiguration{
		{Model: "gpt-test", Effort: "high", IQ: 90, ValidTasks: 10, AveragePriceUSD: float64Pointer(2)},
		{Model: "gpt-test", Effort: "medium", IQ: 90, ValidTasks: 10},
		{Model: "gpt-test", Effort: "low", IQ: 70, ValidTasks: 10},
		{Model: "gpt-test", Effort: "xhigh", IQ: 80, ValidTasks: 10, AveragePriceUSDByBand: &ModelRadarPriceBand{OffPeak: float64Pointer(1), Peak: float64Pointer(3)}},
		// Excluded from automatic selection by name: far more expensive than the
		// next tier.
		{Model: "gpt-test", Effort: "ultra", IQ: 120, ValidTasks: 10},
		// Too few graded tasks to drive an automatic change.
		{Model: "gpt-test", Effort: "max", IQ: 99, ValidTasks: modelRadarAutoEffortMinValidTasks - 1},
		{Model: "hidden-model", Effort: "high", IQ: 95, ValidTasks: 10},
	}
	settings := setting.ModelRadarSettings{
		AutoEffortEnabled: true,
		Models: map[string]setting.ModelRadarModelOverride{
			"hidden-model": {Hidden: true},
			"gpt-test":     {Aliases: []string{"gpt-local", "GPT-Local-Upper"}},
		},
	}

	index := buildModelRadarAutoEffortIndex(configurations, 1700000000, settings, `{}`)

	candidates, ok := index.byModel["gpt-test"]
	require.True(t, ok)
	assert.Equal(t, "gpt-test", candidates.Model)
	assert.Equal(t, int64(1700000000), candidates.FetchedAt)
	require.Len(t, candidates.Candidates, 4)
	// Descending IQ; equal IQ resolves to the lower tier first.
	assert.Equal(t, []kitreasoning.Effort{
		kitreasoning.EffortMedium,
		kitreasoning.EffortHigh,
		kitreasoning.EffortXHigh,
		kitreasoning.EffortLow,
	}, []kitreasoning.Effort{
		candidates.Candidates[0].Effort,
		candidates.Candidates[1].Effort,
		candidates.Candidates[2].Effort,
		candidates.Candidates[3].Effort,
	})
	assert.Equal(t, 90.0, candidates.Candidates[0].IQ)
	require.NotNil(t, candidates.Candidates[1].PriceUSD)
	assert.Equal(t, 2.0, *candidates.Candidates[1].PriceUSD)
	// A missing average price falls back to the mean of the off-peak/peak bands.
	require.NotNil(t, candidates.Candidates[2].PriceUSD)
	assert.Equal(t, 2.0, *candidates.Candidates[2].PriceUSD)

	alias, ok := index.byModel["gpt-local"]
	require.True(t, ok)
	assert.Equal(t, candidates.Candidates, alias.Candidates)
	// Aliases are matched against a normalized lookup, so they are stored
	// normalized regardless of how the administrator spelled them.
	upper, ok := index.byModel["gpt-local-upper"]
	require.True(t, ok)
	assert.Equal(t, candidates.Candidates, upper.Candidates)
	_, hidden := index.byModel["hidden-model"]
	assert.False(t, hidden)
}

func TestBuildModelRadarAutoEffortIndexPrefersComprehensiveIQ(t *testing.T) {
	comprehensiveHigh, comprehensiveLow := 105.0, 60.0
	configurations := []ModelRadarConfiguration{
		{Model: "dual", Effort: "high", IQ: 90, ComprehensiveIQ: &comprehensiveHigh, ValidTasks: 10},
		{Model: "dual", Effort: "low", IQ: 95, ComprehensiveIQ: &comprehensiveLow, ValidTasks: 10},
		{Model: "software-only", Effort: "high", IQ: 88, ValidTasks: 10},
	}
	index := buildModelRadarAutoEffortIndex(configurations, 1, setting.ModelRadarSettings{AutoEffortEnabled: true}, `{}`)

	dual := index.byModel["dual"].Candidates
	require.Len(t, dual, 2)
	// Comprehensive IQ decides the order: high (105) beats low (60) even though
	// the software IQ ranks low higher.
	assert.Equal(t, kitreasoning.EffortHigh, dual[0].Effort)
	assert.Equal(t, 105.0, dual[0].IQ)
	assert.Equal(t, kitreasoning.EffortLow, dual[1].Effort)
	assert.Equal(t, 60.0, dual[1].IQ)

	fallback := index.byModel["software-only"].Candidates
	require.Len(t, fallback, 1)
	assert.Equal(t, 88.0, fallback[0].IQ)
}

func TestLookupModelRadarCandidatesResolvesNames(t *testing.T) {
	setupModelRadarServiceTestDB(t)
	RebuildModelRadarAutoEffortIndex(&ModelRadarData{
		FetchedAt: common.GetTimestamp(),
		Configurations: []ModelRadarConfiguration{
			{Model: "GPT-Test", Effort: "high", IQ: 90, ValidTasks: 10},
		},
	})

	candidates, ok := LookupModelRadarCandidates("gpt-test")
	require.True(t, ok)
	assert.Equal(t, "GPT-Test", candidates.Model)

	candidates, ok = LookupModelRadarCandidates("openai/gpt-test")
	require.True(t, ok)
	assert.Equal(t, "GPT-Test", candidates.Model)

	_, ok = LookupModelRadarCandidates("unlisted-model")
	assert.False(t, ok)
	_, ok = LookupModelRadarCandidates("")
	assert.False(t, ok)
}
