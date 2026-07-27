package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
			"passed": 3, "valid_tasks": 5, "average_price_usd": 1.5,
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
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/efficiency":
			_, _ = writer.Write(efficiency)
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

	data, err := fetchModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", server.URL+"/insights")
	require.NoError(t, err)
	assert.Equal(t, 1, data.ModelCount)
	assert.Equal(t, 1, data.ConfigurationCount)
	require.Len(t, data.Configurations, 1)
	assert.Equal(t, "gpt-test", data.Configurations[0].Model)
	assert.Equal(t, 90.0, data.Configurations[0].IQ)
	require.NotNil(t, data.Configurations[0].LatestGradedAt)
	assert.Equal(t, int64(1785024000), *data.Configurations[0].LatestGradedAt)
	require.Len(t, data.History, 2)
	require.Len(t, data.DegradationAlerts, 1)
	assert.Equal(t, 3.0, data.DegradationAlerts[0].Degradation48hIQ)

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
		{name: "wrong efficiency schema", efficiency: []byte(`{"schema":3,"type":"distributed_intelligence_efficiency","source_updated_at":"2026-07-26T00:00:00Z","points":[{}],"history":[{}]}`), insights: insights, status: http.StatusOK, want: "unsupported source schema"},
		{name: "insights unavailable", efficiency: efficiency, insights: insights, status: http.StatusBadGateway, want: "unexpected HTTP status 502"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newModelRadarSourceServer(t, test.efficiency, test.insights, test.status)
			defer server.Close()
			_, err := fetchModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", server.URL+"/insights")
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestFetchModelRadarRejectsOversizedResponse(t *testing.T) {
	_, insights := modelRadarTestPayloads(t)
	server := newModelRadarSourceServer(t, []byte(strings.Repeat("x", modelRadarEfficiencyMaxBytes+1)), insights, http.StatusOK)
	defer server.Close()

	_, err := fetchModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", server.URL+"/insights")
	require.ErrorContains(t, err, "response exceeds")
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

func TestNormalizeModelRadarEfficiencyRejectsDuplicateAndOutOfRangeData(t *testing.T) {
	efficiency, _ := modelRadarTestPayloads(t)
	var payload modelRadarEfficiencyPayload
	require.NoError(t, common.Unmarshal(efficiency, &payload))

	t.Run("duplicate configuration", func(t *testing.T) {
		duplicate := payload
		duplicate.Points = append(duplicate.Points, duplicate.Points[0])
		_, _, _, err := normalizeModelRadarEfficiency(duplicate)
		require.ErrorContains(t, err, "duplicate configuration")
	})

	t.Run("out of range IQ", func(t *testing.T) {
		invalid := payload
		invalid.Points = append([]modelRadarUpstreamPoint(nil), payload.Points...)
		invalidIQ := 151.0
		invalid.Points[0].IQ = &invalidIQ
		_, _, _, err := normalizeModelRadarEfficiency(invalid)
		require.ErrorContains(t, err, "iq is out of range")
	})
}

func TestNormalizeModelRadarInsightsAllowsNoAlerts(t *testing.T) {
	alerts, updatedAt, err := normalizeModelRadarInsights(modelRadarInsightsPayload{
		Schema:          modelRadarInsightsSchema,
		SourceUpdatedAt: "2026-07-26T00:01:00Z",
	})
	require.NoError(t, err)
	assert.Empty(t, alerts)
	assert.Equal(t, int64(1785024060), updatedAt)
}

func TestNormalizeModelRadarInsightsPreservesSignedDegradation(t *testing.T) {
	iq := 39.0
	degradation12h := 6.5
	degradation24h := 6.5
	degradation48h := -0.2
	payload := modelRadarInsightsPayload{
		Schema:          modelRadarInsightsSchema,
		SourceUpdatedAt: "2026-07-26T13:22:03Z",
	}
	payload.DegradationAlerts.Items = []modelRadarUpstreamAlert{{
		Model:            "gpt-5.6-terra",
		Effort:           "low",
		IQ:               &iq,
		Degradation12hIQ: &degradation12h,
		Degradation24hIQ: &degradation24h,
		Degradation48hIQ: &degradation48h,
	}}

	alerts, _, err := normalizeModelRadarInsights(payload)

	require.NoError(t, err)
	require.Len(t, alerts, 1)
	assert.Equal(t, -0.2, alerts[0].Degradation48hIQ)
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

	_, err := syncModelRadar(ctx, server.Client(), server.URL+"/efficiency", server.URL+"/insights", 1000)
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

	result, err := syncModelRadar(context.Background(), server.Client(), server.URL+"/efficiency", server.URL+"/insights", 2_000_000_000)
	require.NoError(t, err)
	assert.Equal(t, 1, result.ModelCount)
	assert.Equal(t, 1, result.AlertCount)

	snapshot, err := model.GetModelRadarSnapshot(context.Background())
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Equal(t, int64(2_000_000_000), snapshot.FetchedAt)
	assert.NotContains(t, string(snapshot.Payload), "recommendations")
}

func TestIsModelRadarStaleChecksFetchAndBothSourceTimes(t *testing.T) {
	now := int64(10_000)
	threshold := 30 * time.Minute
	assert.False(t, IsModelRadarStale(now, 9_000, 9_000, 9_000, threshold))
	assert.True(t, IsModelRadarStale(now, 8_000, 9_000, 9_000, threshold))
	assert.True(t, IsModelRadarStale(now, 9_000, 8_000, 9_000, threshold))
	assert.True(t, IsModelRadarStale(now, 9_000, 9_000, 8_000, threshold))
	assert.True(t, IsModelRadarStale(now, 0, 9_000, 9_000, threshold))
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
