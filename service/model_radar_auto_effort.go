package service

import (
	"context"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
)

const (
	// A radar configuration graded on fewer tasks than this is too noisy to
	// drive an automatic reasoning-tier change.
	modelRadarAutoEffortMinValidTasks = 3
	// How long a built index may serve requests before it is reloaded from the
	// stored snapshot. The snapshot itself only changes when a sync runs, so
	// this bounds how quickly another instance observes a fresh snapshot.
	modelRadarAutoEffortIndexTTL = 60 * time.Second
	// Radar's ultra tier is a Codex sub-agent workflow (xhigh plus spawned
	// agents), not a reasoning effort the gateway can send, so it is never
	// eligible for automatic selection. Excluded by name so that even an effort
	// that ever shares the name cannot quietly start being picked.
	modelRadarAutoEffortExcludedTier = "ultra"
)

// ModelRadarEffortCandidate is one graded radar tier mapped onto a gateway
// reasoning effort.
type ModelRadarEffortCandidate struct {
	Effort      kitreasoning.Effort
	RadarEffort string
	IQ          float64
	ValidTasks  int
	PriceUSD    *float64
}

// ModelRadarCandidates holds the usable tiers of one radar model, ordered by
// descending IQ with the lower effort first on ties.
type ModelRadarCandidates struct {
	Model      string
	FetchedAt  int64
	Candidates []ModelRadarEffortCandidate
}

type modelRadarAutoEffortIndex struct {
	byModel     map[string]ModelRadarCandidates
	settingsRaw string
	loadedAt    int64
}

var (
	modelRadarAutoEffortIndexCache struct {
		sync.RWMutex
		index       *modelRadarAutoEffortIndex
		lastAttempt int64
	}
	modelRadarAutoEffortIndexLoad sync.Mutex
)

var modelRadarEffortRank = map[kitreasoning.Effort]int{
	kitreasoning.EffortNone:    0,
	kitreasoning.EffortMinimal: 1,
	kitreasoning.EffortLow:     2,
	kitreasoning.EffortMedium:  3,
	kitreasoning.EffortHigh:    4,
	kitreasoning.EffortXHigh:   5,
	kitreasoning.EffortMax:     6,
}

// LookupModelRadarCandidates resolves a gateway model name to the radar tiers
// that may replace a client-selected effort, including administrator-declared
// aliases. The bool is false when the name is not covered by the snapshot or no
// snapshot has been indexed yet.
func LookupModelRadarCandidates(gatewayModel string) (ModelRadarCandidates, bool) {
	key := normalizeModelRadarLookupName(gatewayModel)
	if key == "" {
		return ModelRadarCandidates{}, false
	}
	index := loadModelRadarAutoEffortIndex()
	if index == nil {
		return ModelRadarCandidates{}, false
	}
	if candidates, ok := index.byModel[key]; ok {
		return candidates, true
	}
	// Provider-prefixed names (openai/gpt-5.1-codex) fall back to the tail.
	if slash := strings.LastIndex(key, "/"); slash >= 0 {
		if candidates, ok := index.byModel[key[slash+1:]]; ok {
			return candidates, true
		}
	}
	return ModelRadarCandidates{}, false
}

// RebuildModelRadarAutoEffortIndex refreshes the in-process index from a freshly
// synced snapshot, so the syncing instance serves the new data without a
// database read.
func RebuildModelRadarAutoEffortIndex(data *ModelRadarData) {
	if data == nil {
		return
	}
	built := buildModelRadarAutoEffortIndex(
		data.Configurations,
		data.FetchedAt,
		setting.GetModelRadarSettings(),
		setting.ModelRadarSettingsRaw(),
	)
	modelRadarAutoEffortIndexCache.Lock()
	modelRadarAutoEffortIndexCache.index = built
	modelRadarAutoEffortIndexCache.Unlock()
}

func buildModelRadarAutoEffortIndex(configurations []ModelRadarConfiguration, fetchedAt int64, settings setting.ModelRadarSettings, settingsRaw string) *modelRadarAutoEffortIndex {
	byModel := make(map[string]ModelRadarCandidates)
	for _, configuration := range configurations {
		if settings.Models[configuration.Model].Hidden {
			continue
		}
		if configuration.ValidTasks < modelRadarAutoEffortMinValidTasks {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(configuration.Effort), modelRadarAutoEffortExcludedTier) {
			continue
		}
		effort, err := kitreasoning.ParseEffort(configuration.Effort)
		if err != nil || effort == "" {
			// Radar publishes other tiers the gateway cannot express.
			continue
		}
		key := normalizeModelRadarLookupName(configuration.Model)
		if key == "" {
			continue
		}
		bucket := byModel[key]
		bucket.Model = configuration.Model
		bucket.FetchedAt = fetchedAt
		bucket.Candidates = append(bucket.Candidates, ModelRadarEffortCandidate{
			Effort:      effort,
			RadarEffort: configuration.Effort,
			IQ:          configuration.IQ,
			ValidTasks:  configuration.ValidTasks,
			PriceUSD:    modelRadarCandidatePrice(configuration),
		})
		byModel[key] = bucket
	}
	for key, bucket := range byModel {
		bucket.Candidates = sortModelRadarCandidates(bucket.Candidates)
		byModel[key] = bucket
	}
	// Aliases resolve to the same graded tier list as the radar model itself.
	for radarModel, override := range settings.Models {
		if override.Hidden || len(override.Aliases) == 0 {
			continue
		}
		bucket, ok := byModel[normalizeModelRadarLookupName(radarModel)]
		if !ok {
			continue
		}
		for _, alias := range override.Aliases {
			// Lookups normalize the requested name, so the alias key must be
			// normalized too or a mixed-case alias never matches.
			byModel[normalizeModelRadarLookupName(alias)] = bucket
		}
	}
	return &modelRadarAutoEffortIndex{byModel: byModel, settingsRaw: settingsRaw, loadedAt: common.GetTimestamp()}
}

// sortModelRadarCandidates deduplicates by gateway effort, keeps the highest IQ
// per effort, then orders by descending IQ and, on equal IQ, by ascending
// reasoning effort so a tie always resolves to the cheaper tier.
func sortModelRadarCandidates(candidates []ModelRadarEffortCandidate) []ModelRadarEffortCandidate {
	byEffort := make(map[kitreasoning.Effort]ModelRadarEffortCandidate, len(candidates))
	for _, candidate := range candidates {
		existing, ok := byEffort[candidate.Effort]
		if !ok || candidate.IQ > existing.IQ {
			byEffort[candidate.Effort] = candidate
		}
	}
	ordered := make([]ModelRadarEffortCandidate, 0, len(byEffort))
	for _, candidate := range byEffort {
		ordered = append(ordered, candidate)
	}
	slices.SortFunc(ordered, func(left, right ModelRadarEffortCandidate) int {
		if left.IQ != right.IQ {
			if left.IQ > right.IQ {
				return -1
			}
			return 1
		}
		return modelRadarEffortRank[left.Effort] - modelRadarEffortRank[right.Effort]
	})
	return ordered
}

// modelRadarCandidatePrice prefers the published average price per task and
// falls back to the mean of the off-peak/peak bands.
func modelRadarCandidatePrice(configuration ModelRadarConfiguration) *float64 {
	if configuration.AveragePriceUSD != nil {
		return configuration.AveragePriceUSD
	}
	bands := configuration.AveragePriceUSDByBand
	if bands == nil || bands.OffPeak == nil || bands.Peak == nil {
		return nil
	}
	mean := (*bands.OffPeak + *bands.Peak) / 2
	if math.IsNaN(mean) || math.IsInf(mean, 0) {
		return nil
	}
	return &mean
}

// loadModelRadarAutoEffortIndex serves the in-process index while it matches the
// stored settings option and is younger than the TTL. Only one request at a
// time may attempt the database reload, and a failed attempt keeps serving the
// last good index until the next TTL window.
func loadModelRadarAutoEffortIndex() *modelRadarAutoEffortIndex {
	settingsRaw := setting.ModelRadarSettingsRaw()
	now := common.GetTimestamp()
	ttl := int64(modelRadarAutoEffortIndexTTL.Seconds())

	if index := reusableModelRadarAutoEffortIndex(settingsRaw, now, ttl); index != nil {
		return index
	}
	modelRadarAutoEffortIndexCache.RLock()
	lastAttempt := modelRadarAutoEffortIndexCache.lastAttempt
	modelRadarAutoEffortIndexCache.RUnlock()
	if now-lastAttempt < ttl {
		return lastModelRadarAutoEffortIndex()
	}

	modelRadarAutoEffortIndexLoad.Lock()
	defer modelRadarAutoEffortIndexLoad.Unlock()
	if index := reusableModelRadarAutoEffortIndex(settingsRaw, now, ttl); index != nil {
		return index
	}
	modelRadarAutoEffortIndexCache.Lock()
	lastAttempt = modelRadarAutoEffortIndexCache.lastAttempt
	modelRadarAutoEffortIndexCache.lastAttempt = now
	modelRadarAutoEffortIndexCache.Unlock()
	if now-lastAttempt < ttl {
		return lastModelRadarAutoEffortIndex()
	}

	built := buildModelRadarAutoEffortIndexFromSnapshot(settingsRaw)
	if built == nil {
		return lastModelRadarAutoEffortIndex()
	}
	modelRadarAutoEffortIndexCache.Lock()
	modelRadarAutoEffortIndexCache.index = built
	modelRadarAutoEffortIndexCache.Unlock()
	return built
}

func buildModelRadarAutoEffortIndexFromSnapshot(settingsRaw string) *modelRadarAutoEffortIndex {
	snapshot, err := model.GetModelRadarSnapshot(context.Background())
	if err != nil {
		common.SysLog("failed to load model radar snapshot for auto-effort: " + err.Error())
		return nil
	}
	if snapshot == nil {
		return &modelRadarAutoEffortIndex{
			byModel:     map[string]ModelRadarCandidates{},
			settingsRaw: settingsRaw,
			loadedAt:    common.GetTimestamp(),
		}
	}
	var data ModelRadarData
	if err := common.Unmarshal(snapshot.Payload, &data); err != nil {
		common.SysLog("failed to decode model radar snapshot for auto-effort: " + err.Error())
		return nil
	}
	return buildModelRadarAutoEffortIndex(data.Configurations, snapshot.FetchedAt, setting.GetModelRadarSettings(), settingsRaw)
}

func reusableModelRadarAutoEffortIndex(settingsRaw string, now int64, ttl int64) *modelRadarAutoEffortIndex {
	modelRadarAutoEffortIndexCache.RLock()
	index := modelRadarAutoEffortIndexCache.index
	modelRadarAutoEffortIndexCache.RUnlock()
	if index == nil || index.settingsRaw != settingsRaw {
		return nil
	}
	if now-index.loadedAt >= ttl {
		return nil
	}
	return index
}

// lastModelRadarAutoEffortIndex returns the cached index regardless of age, so a
// failed reload or a just-changed settings option does not drop every request
// back to "no radar data".
func lastModelRadarAutoEffortIndex() *modelRadarAutoEffortIndex {
	modelRadarAutoEffortIndexCache.RLock()
	defer modelRadarAutoEffortIndexCache.RUnlock()
	return modelRadarAutoEffortIndexCache.index
}

func normalizeModelRadarLookupName(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}
