package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

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

func TestFetchModelRadarSourceRejectsResponseOverLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"value":"` + string(make([]byte, 100)) + `"}`))
	}))
	defer server.Close()

	read, err := fetchModelRadarSource(context.Background(), server.Client(), server.URL, 64, nil, func(body io.Reader) error {
		_, readErr := io.ReadAll(body)
		return readErr
	})

	require.ErrorContains(t, err, "response exceeds 64 bytes")
	assert.Equal(t, int64(65), read, "the reader stops at the cap plus one byte")
}

func TestFetchModelRadarHonorsRequestDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	var payload struct{}
	err := fetchModelRadarJSON(ctx, server.Client(), server.URL, 1024, &payload)
	require.ErrorContains(t, err, "context deadline exceeded")
}

func TestFetchModelRadarRejectsNonJSONContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	var payload struct{}
	err := fetchModelRadarJSON(context.Background(), server.Client(), server.URL, 1024, &payload)
	require.ErrorContains(t, err, "expected JSON response")
}

func TestFetchModelRadarJSONUnwrapsDataEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":{"value":7}}`))
	}))
	defer server.Close()

	var payload struct {
		Value int `json:"value"`
	}
	require.NoError(t, fetchModelRadarJSON(context.Background(), server.Client(), server.URL, 1024, &payload))
	assert.Equal(t, 7, payload.Value)
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
		FetchedAt:       common.GetTimestamp(),
	}))

	data, err := GetModelRadar(context.Background())
	require.NoError(t, err)
	assert.False(t, data.Stale)
}

func TestGetModelRadarDerivesSummaryFromBench(t *testing.T) {
	setupModelRadarServiceTestDB(t)
	latest := int64(900)
	// A snapshot written by the legacy build still carries its own counts, update
	// time and archive. The page only shows the bench board, so the summary must
	// describe that board instead.
	payload, err := common.Marshal(map[string]any{
		"schema_version":      1,
		"model_count":         24,
		"configuration_count": 89,
		"source_updated_at":   100,
		"bench": ModelRadarBench{
			ReadAt: 50,
			Cells: []ModelRadarBenchCell{
				{Model: "gpt-a", Effort: "low"},
				{Model: "gpt-a", Effort: "high"},
				{
					Model:         "gpt-b",
					Effort:        "low",
					DetailMetrics: &ModelRadarBenchDetailMetrics{LatestGradedAt: &latest},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, model.SaveModelRadarSnapshot(context.Background(), &model.ModelRadarSnapshot{
		SchemaVersion:   1,
		Payload:         payload,
		SourceUpdatedAt: 100,
		FetchedAt:       common.GetTimestamp(),
	}))

	data, err := GetModelRadar(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, data.ModelCount)
	assert.Equal(t, 3, data.ConfigurationCount)
	assert.Equal(t, int64(900), data.SourceUpdatedAt)
	assert.Equal(t, modelRadarSchemaVersion, data.SchemaVersion)
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

func TestModelRadarBenchContentTime(t *testing.T) {
	assert.Zero(t, modelRadarBenchContentTime(nil))
	assert.Equal(t, int64(50), modelRadarBenchContentTime(&ModelRadarBench{ReadAt: 50}))

	latest := int64(90)
	bench := &ModelRadarBench{
		ReadAt: 50,
		Cells: []ModelRadarBenchCell{
			{LastGradedDay: 60},
			{LastGradedDay: 70, DetailMetrics: &ModelRadarBenchDetailMetrics{LatestGradedAt: &latest}},
			{LastGradedDay: 80},
		},
	}
	assert.Equal(t, int64(90), modelRadarBenchContentTime(bench), "the newest valid grade wins")
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
	require.NotNil(t, snapshot.Bench)
	assert.NotEmpty(t, snapshot.Bench.Cells)
	assert.Equal(t, modelRadarSourceURL, snapshot.Source.URL)
}

func float64Pointer(value float64) *float64 { return &value }

func TestBuildModelRadarAutoEffortIndexFiltersAndOrders(t *testing.T) {
	score := func(value float64) *float64 { return &value }
	data := &ModelRadarData{
		FetchedAt: 1700000000,
		Bench: &ModelRadarBench{Cells: []ModelRadarBenchCell{
			{Model: "gpt-test", Effort: "high", Score: score(135), Coverage: 64,
				Price: ModelRadarBenchMetricStat{Mean: float64Pointer(2)}},
			{Model: "gpt-test", Effort: "medium", Score: score(135), Coverage: 64},
			{Model: "gpt-test", Effort: "xhigh", Score: score(120), Coverage: 64},
			{Model: "gpt-test", Effort: "low", Score: score(105), Coverage: 64},
			// Excluded from automatic selection by name.
			{Model: "gpt-test", Effort: "ultra", Score: score(150), Coverage: 64},
			// Too few graded tasks to drive an automatic change.
			{Model: "gpt-test", Effort: "max", Score: score(148), Coverage: modelRadarAutoEffortMinValidTasks - 1},
			{Model: "hidden-model", Effort: "high", Score: score(142.5), Coverage: 64},
		}},
	}
	settings := setting.ModelRadarSettings{
		AutoEffortEnabled: true,
		Models: map[string]setting.ModelRadarModelOverride{
			"hidden-model": {Hidden: true},
			"gpt-test":     {Aliases: []string{"gpt-local", "GPT-Local-Upper"}},
		},
	}

	index := buildModelRadarAutoEffortIndex(data, settings, `{}`)

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
	assert.Equal(t, 90.0, candidates.Candidates[0].IQ, "150-point scores convert to the 0-100 IQ scale")
	require.NotNil(t, candidates.Candidates[1].PriceUSD)
	assert.Equal(t, 2.0, *candidates.Candidates[1].PriceUSD)

	alias, ok := index.byModel["gpt-local"]
	require.True(t, ok)
	assert.Equal(t, candidates.Candidates, alias.Candidates)
	// Aliases are matched against a normalized lookup, so they are stored
	// normalized regardless of how the administrator spelled them.
	upper, ok := index.byModel["gpt-local-upper"]
	require.True(t, ok)
	assert.Equal(t, candidates.Candidates, upper.Candidates)
	_, hidden := index.byModel["hidden-model"]
	assert.False(t, hidden, "a hidden model never reaches auto-effort")
}

func TestBuildModelRadarAutoEffortIndexRequiresBenchScores(t *testing.T) {
	settings := setting.ModelRadarSettings{AutoEffortEnabled: true}
	index := buildModelRadarAutoEffortIndex(&ModelRadarData{FetchedAt: 1}, settings, `{}`)
	assert.Empty(t, index.byModel, "there is no other score source to fall back to")

	index = buildModelRadarAutoEffortIndex(&ModelRadarData{
		FetchedAt: 1,
		Bench: &ModelRadarBench{Cells: []ModelRadarBenchCell{
			{Model: "gpt-test", Effort: "high", Coverage: 64},
		}},
	}, settings, `{}`)
	assert.Empty(t, index.byModel, "a tier without a score cannot be selected")
}

func TestLookupModelRadarCandidatesResolvesNames(t *testing.T) {
	setupModelRadarServiceTestDB(t)
	score := 120.0
	RebuildModelRadarAutoEffortIndex(&ModelRadarData{
		FetchedAt: common.GetTimestamp(),
		Bench: &ModelRadarBench{Cells: []ModelRadarBenchCell{
			{Model: "GPT-Test", Effort: "high", Score: &score, Coverage: 64},
		}},
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
