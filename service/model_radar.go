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
	"github.com/QuantumNous/new-api/setting"
)

// The station publishes the 64-task RadarBench as its only machine-readable
// score feed for every graded tier. The older distributed benchmark was retired
// in October 2026: its live APIs answer 502 and its published file is a frozen
// snapshot, so the radar no longer reads or stores that archive.
const (
	modelRadarSchemaVersion     = 2
	modelRadarSourceURL         = "https://codexradar.com"
	modelRadarAttribution       = "数据来自 Codex 雷达 codexradar.com"
	modelRadarRequestTimeout    = 15 * time.Second
	modelRadarDefaultInterval   = 10 * time.Minute
	modelRadarMinimumInterval   = 10 * time.Minute
	modelRadarMinimumStaleAfter = 30 * time.Minute
)

var ErrModelRadarUnavailable = errors.New("model radar data unavailable")

type ModelRadarSource struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Attribution string `json:"attribution"`
}

type ModelRadarData struct {
	Settings           *setting.ModelRadarSettings `json:"settings,omitempty"`
	SchemaVersion      int                         `json:"schema_version"`
	FetchedAt          int64                       `json:"fetched_at"`
	SourceUpdatedAt    int64                       `json:"source_updated_at"`
	Stale              bool                        `json:"stale"`
	Source             ModelRadarSource            `json:"source"`
	ModelCount         int                         `json:"model_count"`
	ConfigurationCount int                         `json:"configuration_count"`
	// Bench is the only scored section: the live RadarBench board.
	Bench *ModelRadarBench `json:"bench"`
}

// modelRadarSources carries the upstream endpoints one sync reads. Tests
// redirect them to a local server.
type modelRadarSources struct {
	BenchBinding string
	BenchScore   string
}

func defaultModelRadarSources() modelRadarSources {
	return modelRadarSources{
		BenchBinding: modelRadarBenchBindingURL,
		BenchScore:   modelRadarBenchScoreURL,
	}
}

type ModelRadarSyncResult struct {
	FetchedAt          int64 `json:"fetched_at"`
	SourceUpdatedAt    int64 `json:"source_updated_at"`
	ModelCount         int   `json:"model_count"`
	ConfigurationCount int   `json:"configuration_count"`
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
	return syncModelRadar(ctx, client, defaultModelRadarSources(), loadPreviousModelRadarData(ctx), common.GetTimestamp())
}

// loadPreviousModelRadarData decodes the stored snapshot so a partial round can
// carry cells that could not be read this time. A missing or corrupt snapshot
// is not an error; there is simply nothing to carry.
func loadPreviousModelRadarData(ctx context.Context) *ModelRadarData {
	snapshot, err := model.GetModelRadarSnapshot(ctx)
	if err != nil {
		common.SysLog("model radar sync could not load the previous snapshot: " + err.Error())
		return nil
	}
	if snapshot == nil {
		return nil
	}
	var data ModelRadarData
	if err := common.Unmarshal(snapshot.Payload, &data); err != nil {
		common.SysLog("model radar sync could not decode the previous snapshot: " + err.Error())
		return nil
	}
	return &data
}

func syncModelRadar(ctx context.Context, client *http.Client, sources modelRadarSources, previous *ModelRadarData, fetchedAt int64) (*ModelRadarSyncResult, error) {
	if client == nil {
		return nil, errors.New("model radar HTTP client is required")
	}
	if sources.BenchBinding == "" || sources.BenchScore == "" {
		return nil, errors.New("model radar bench sources are not configured")
	}
	var previousBench *ModelRadarBench
	if previous != nil {
		previousBench = previous.Bench
	}
	// The bench reads every bound selection, so it gets its own budget instead
	// of the legacy sources' short shared timeout.
	benchCtx, cancel := context.WithTimeout(ctx, modelRadarBenchSyncTimeout)
	defer cancel()
	bench, err := fetchModelRadarBench(benchCtx, client, sources, previousBench, fetchedAt)
	if err != nil {
		return nil, fmt.Errorf("model radar bench: %w", err)
	}

	data := &ModelRadarData{
		SchemaVersion: modelRadarSchemaVersion,
		FetchedAt:     fetchedAt,
		Stale:         false,
		Source:        ModelRadarSource{Name: "Codex Radar", URL: modelRadarSourceURL, Attribution: modelRadarAttribution},
		Bench:         bench,
	}
	applyModelRadarBenchSummary(data)
	payload, err := common.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal model radar snapshot: %w", err)
	}
	if err := model.SaveModelRadarSnapshot(ctx, &model.ModelRadarSnapshot{
		SchemaVersion:   modelRadarSchemaVersion,
		Payload:         payload,
		SourceUpdatedAt: data.SourceUpdatedAt,
		FetchedAt:       fetchedAt,
	}); err != nil {
		return nil, fmt.Errorf("save model radar snapshot: %w", err)
	}

	// The syncing instance can serve radar auto-effort from the snapshot it just
	// produced instead of re-reading the row on the next relayed request.
	RebuildModelRadarAutoEffortIndex(data)

	return &ModelRadarSyncResult{
		FetchedAt:          fetchedAt,
		SourceUpdatedAt:    data.SourceUpdatedAt,
		ModelCount:         data.ModelCount,
		ConfigurationCount: data.ConfigurationCount,
	}, nil
}

// applyModelRadarBenchSummary derives the snapshot summary from the bench board.
// It also normalizes snapshots written before the legacy archive was removed, so
// their counts and update time no longer describe data the page never shows.
func applyModelRadarBenchSummary(data *ModelRadarData) {
	if data == nil || data.Bench == nil {
		return
	}
	models := make(map[string]struct{}, len(data.Bench.Cells))
	for _, cell := range data.Bench.Cells {
		models[cell.Model] = struct{}{}
	}
	data.ModelCount = len(models)
	data.ConfigurationCount = len(data.Bench.Cells)
	data.SourceUpdatedAt = modelRadarBenchContentTime(data.Bench)
}

// modelRadarBenchContentTime is the latest time the bench content itself
// changed: the newest valid grade, falling back to the last full read when no
// cell publishes grade detail.
func modelRadarBenchContentTime(bench *ModelRadarBench) int64 {
	if bench == nil {
		return 0
	}
	var latest int64
	for _, cell := range bench.Cells {
		if cell.DetailMetrics != nil && cell.DetailMetrics.LatestGradedAt != nil {
			latest = max(latest, *cell.DetailMetrics.LatestGradedAt)
		}
		latest = max(latest, cell.LastGradedDay)
	}
	if latest > 0 {
		return latest
	}
	return bench.ReadAt
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
	settings := setting.GetModelRadarSettings()
	data.Settings = &settings
	data.FetchedAt = snapshot.FetchedAt
	data.SourceUpdatedAt = snapshot.SourceUpdatedAt
	data.Stale = IsModelRadarStale(
		common.GetTimestamp(),
		snapshot.FetchedAt,
		ModelRadarStaleAfter(),
	)
	// Applied last: the summary must follow the bench board, not the stored
	// columns written by a build that still served the legacy archive.
	applyModelRadarBenchSummary(&data)
	if data.Bench != nil {
		// The served shape is current even when the stored payload predates it.
		data.SchemaVersion = max(data.SchemaVersion, modelRadarSchemaVersion)
	}
	return &data, nil
}

func fetchModelRadarJSON(ctx context.Context, client *http.Client, sourceURL string, maxBytes int64, target any) error {
	return fetchModelRadarJSONWithHeaders(ctx, client, sourceURL, maxBytes, nil, target)
}

func fetchModelRadarJSONWithHeaders(ctx context.Context, client *http.Client, sourceURL string, maxBytes int64, headers map[string]string, target any) error {
	_, err := fetchModelRadarSource(ctx, client, sourceURL, maxBytes, headers, func(body io.Reader) error {
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		// Upstream's own dashboard client unwraps a data envelope before use,
		// so these endpoints may return the document either directly or
		// wrapped in {"data": ...}.
		var envelope struct {
			Data common.RawMessage `json:"data"`
		}
		if err := common.Unmarshal(data, &envelope); err != nil {
			return err
		}
		if len(envelope.Data) > 0 && common.GetJsonType(envelope.Data) == "object" {
			data = envelope.Data
		}
		return common.Unmarshal(data, target)
	})
	return err
}

// modelRadarHTTPStatusError carries the HTTP status so the bench reader can
// decide whether one more attempt can plausibly succeed.
type modelRadarHTTPStatusError struct {
	URL        string
	StatusCode int
}

func (e *modelRadarHTTPStatusError) Error() string {
	return fmt.Sprintf("fetch %s: unexpected HTTP status %d", e.URL, e.StatusCode)
}

// fetchModelRadarSource requests one source document, hands its body to decode,
// and returns how many body bytes decode consumed.
func fetchModelRadarSource(ctx context.Context, client *http.Client, sourceURL string, maxBytes int64, headers map[string]string, decode func(io.Reader) error) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return 0, fmt.Errorf("create model radar request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "new-api-model-radar/1.0")
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("fetch %s: %w", request.URL.Host, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, &modelRadarHTTPStatusError{URL: request.URL.Host, StatusCode: response.StatusCode}
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || mediaType != "application/json" {
			return 0, fmt.Errorf("fetch %s: expected JSON response", request.URL.Host)
		}
	}
	body := &modelRadarCountingReader{reader: io.LimitReader(response.Body, maxBytes+1)}
	decodeErr := decode(body)
	// Check the cap first: a truncated body surfaces as a decode error.
	if body.read > maxBytes {
		return body.read, fmt.Errorf("fetch %s: response exceeds %d bytes", request.URL.Host, maxBytes)
	}
	if decodeErr != nil {
		return body.read, fmt.Errorf("decode %s response: %w", request.URL.Host, decodeErr)
	}
	return body.read, nil
}

type modelRadarCountingReader struct {
	reader io.Reader
	read   int64
}

func (r *modelRadarCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func validateModelRadarIdentity(modelName string, effort string) (string, string, string, error) {
	modelName = strings.TrimSpace(modelName)
	effort = strings.TrimSpace(effort)
	if modelName == "" || effort == "" || len(modelName) > 128 || len(effort) > 32 {
		return "", "", "", errors.New("model and effort must be non-empty and within length limits")
	}
	return modelName, effort, modelName + "|" + effort, nil
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
