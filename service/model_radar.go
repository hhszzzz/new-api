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

// The station replaced the 64-task RadarBench board in November 2026 with the
// four-task weighted board it now presents as the degradation radar. That board
// is the only machine-readable score feed for the live round: every configured
// model/effort pair is published in one results document and one score summary.
// The older RadarBench and the distributed intelligence-efficiency pipeline are
// frozen archives, so the radar no longer reads or stores them.
const (
	modelRadarSchemaVersion     = 3
	modelRadarSourceURL         = "https://codexradar.com"
	modelRadarAttribution       = "数据来自 Codex 雷达 codexradar.com"
	modelRadarDefaultInterval   = 10 * time.Minute
	modelRadarMinimumInterval   = 10 * time.Minute
	modelRadarMinimumStaleAfter = 30 * time.Minute
	// One sync reads two documents; the system task runs every 10 minutes, so
	// this stays far below the task interval.
	modelRadarFourTaskSyncTimeout = 3 * time.Minute
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
	// FourTask is the only scored section: the station's live four-task board.
	FourTask *ModelRadarFourTask `json:"four_task"`
}

// modelRadarSources carries the upstream endpoints one sync reads. Tests
// redirect them to a local server.
type modelRadarSources struct {
	FourTaskResults string
	FourTaskSummary string
}

func defaultModelRadarSources() modelRadarSources {
	return modelRadarSources{
		FourTaskResults: modelRadarFourTaskResultsURL,
		FourTaskSummary: modelRadarFourTaskSummaryURL,
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
	return syncModelRadar(ctx, client, defaultModelRadarSources(), common.GetTimestamp())
}

func syncModelRadar(ctx context.Context, client *http.Client, sources modelRadarSources, fetchedAt int64) (*ModelRadarSyncResult, error) {
	if client == nil {
		return nil, errors.New("model radar HTTP client is required")
	}
	if sources.FourTaskResults == "" || sources.FourTaskSummary == "" {
		return nil, errors.New("model radar four-task sources are not configured")
	}
	boardCtx, cancel := context.WithTimeout(ctx, modelRadarFourTaskSyncTimeout)
	defer cancel()
	board, err := fetchModelRadarFourTask(boardCtx, client, sources, fetchedAt)
	if err != nil {
		return nil, fmt.Errorf("model radar four-task: %w", err)
	}

	data := &ModelRadarData{
		SchemaVersion: modelRadarSchemaVersion,
		FetchedAt:     fetchedAt,
		Stale:         false,
		Source:        ModelRadarSource{Name: "Codex Radar", URL: modelRadarSourceURL, Attribution: modelRadarAttribution},
		FourTask:      board,
	}
	applyModelRadarFourTaskSummary(data)
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

// applyModelRadarFourTaskSummary derives the snapshot summary from the board.
func applyModelRadarFourTaskSummary(data *ModelRadarData) {
	if data == nil || data.FourTask == nil {
		return
	}
	data.ModelCount = len(data.FourTask.ModelOrder)
	data.ConfigurationCount = len(data.FourTask.Cells)
	data.SourceUpdatedAt = data.FourTask.VerifiedAt
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
	// Applied last: the summary must follow the four-task board, not the stored
	// columns written by a build that still served the legacy archive.
	applyModelRadarFourTaskSummary(&data)
	if data.FourTask != nil {
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
		return 0, fmt.Errorf("fetch %s: unexpected HTTP status %d", request.URL.Host, response.StatusCode)
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
