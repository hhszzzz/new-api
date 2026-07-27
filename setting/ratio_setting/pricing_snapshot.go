package ratio_setting

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	ModelPriceOptionKey           = "ModelPrice"
	ModelRatioOptionKey           = "ModelRatio"
	CompletionRatioOptionKey      = "CompletionRatio"
	CacheRatioOptionKey           = "CacheRatio"
	CreateCacheRatioOptionKey     = "CreateCacheRatio"
	ImageRatioOptionKey           = "ImageRatio"
	AudioRatioOptionKey           = "AudioRatio"
	AudioCompletionRatioOptionKey = "AudioCompletionRatio"
)

var pricingOptionKeys = map[string]struct{}{
	ModelPriceOptionKey:           {},
	ModelRatioOptionKey:           {},
	CompletionRatioOptionKey:      {},
	CacheRatioOptionKey:           {},
	CreateCacheRatioOptionKey:     {},
	ImageRatioOptionKey:           {},
	AudioRatioOptionKey:           {},
	AudioCompletionRatioOptionKey: {},
}

// PricingSnapshot is immutable after publication. Callers that need several
// related ratios should capture one snapshot and use it for the whole request.
type PricingSnapshot struct {
	modelPrices          map[string]float64
	modelRatios          map[string]float64
	completionRatios     map[string]float64
	cacheRatios          map[string]float64
	createCacheRatios    map[string]float64
	imageRatios          map[string]float64
	audioRatios          map[string]float64
	audioCompletionRatio map[string]float64
}

var (
	pricingSnapshot   atomic.Pointer[PricingSnapshot]
	pricingSnapshotMu sync.Mutex
)

func init() {
	pricingSnapshot.Store(emptyPricingSnapshot())
}

func emptyPricingSnapshot() *PricingSnapshot {
	return &PricingSnapshot{
		modelPrices:          map[string]float64{},
		modelRatios:          map[string]float64{},
		completionRatios:     map[string]float64{},
		cacheRatios:          map[string]float64{},
		createCacheRatios:    map[string]float64{},
		imageRatios:          map[string]float64{},
		audioRatios:          map[string]float64{},
		audioCompletionRatio: map[string]float64{},
	}
}

func clonePricingMap(source map[string]float64) map[string]float64 {
	cloned := make(map[string]float64, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func clonePricingSnapshot(source *PricingSnapshot) *PricingSnapshot {
	return &PricingSnapshot{
		modelPrices:          source.modelPrices,
		modelRatios:          source.modelRatios,
		completionRatios:     source.completionRatios,
		cacheRatios:          source.cacheRatios,
		createCacheRatios:    source.createCacheRatios,
		imageRatios:          source.imageRatios,
		audioRatios:          source.audioRatios,
		audioCompletionRatio: source.audioCompletionRatio,
	}
}

func GetPricingSnapshot() *PricingSnapshot {
	return pricingSnapshot.Load()
}

func IsPricingOptionKey(key string) bool {
	_, ok := pricingOptionKeys[key]
	return ok
}

func PricingOptionKeyCount() int {
	return len(pricingOptionKeys)
}

func parsePricingOptions(values map[string]string) (map[string]map[string]float64, error) {
	parsed := make(map[string]map[string]float64, len(values))
	for key, value := range values {
		if !IsPricingOptionKey(key) {
			return nil, fmt.Errorf("unsupported model pricing option: %s", key)
		}
		entries := make(map[string]float64)
		if err := common.UnmarshalJsonStr(value, &entries); err != nil {
			return nil, fmt.Errorf("model pricing option %s must be a numeric JSON object: %w", key, err)
		}
		for modelName, ratio := range entries {
			if strings.TrimSpace(modelName) == "" {
				return nil, fmt.Errorf("model pricing option %s contains an empty model name", key)
			}
			if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
				return nil, fmt.Errorf("model pricing option %s contains an invalid value for %s", key, modelName)
			}
		}
		parsed[key] = entries
	}
	return parsed, nil
}

func ValidatePricingOptionsByJSONString(values map[string]string) error {
	_, err := parsePricingOptions(values)
	return err
}

// UpdatePricingOptionsByJSONString publishes every supplied pricing map in one
// atomic snapshot after all input has been parsed and validated.
func UpdatePricingOptionsByJSONString(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	parsed, err := parsePricingOptions(values)
	if err != nil {
		return err
	}

	pricingSnapshotMu.Lock()
	next := clonePricingSnapshot(GetPricingSnapshot())
	for key, entries := range parsed {
		switch key {
		case ModelPriceOptionKey:
			next.modelPrices = entries
		case ModelRatioOptionKey:
			next.modelRatios = entries
		case CompletionRatioOptionKey:
			next.completionRatios = entries
		case CacheRatioOptionKey:
			next.cacheRatios = entries
		case CreateCacheRatioOptionKey:
			next.createCacheRatios = entries
		case ImageRatioOptionKey:
			next.imageRatios = entries
		case AudioRatioOptionKey:
			next.audioRatios = entries
		case AudioCompletionRatioOptionKey:
			next.audioCompletionRatio = entries
		}
	}
	pricingSnapshot.Store(next)
	pricingSnapshotMu.Unlock()

	InvalidateExposedDataCache()
	return nil
}

func pricingMapJSONString(values map[string]float64) string {
	bytes, err := common.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}

func (snapshot *PricingSnapshot) GetModelPrice(name string, printErr bool) (float64, bool) {
	name = FormatMatchingModelName(name)
	if price, ok := snapshot.modelPrices[name]; ok {
		return price, true
	}
	if strings.HasSuffix(name, CompactModelSuffix) {
		if price, ok := snapshot.modelPrices[CompactWildcardModelKey]; ok {
			return price, true
		}
	}
	if printErr {
		common.SysError("model price not found: " + name)
	}
	return -1, false
}

func (snapshot *PricingSnapshot) GetModelRatio(name string) (float64, bool, string) {
	name = FormatMatchingModelName(name)
	if ratio, ok := snapshot.modelRatios[name]; ok {
		return ratio, true, name
	}
	if strings.HasSuffix(name, CompactModelSuffix) {
		if ratio, ok := snapshot.modelRatios[CompactWildcardModelKey]; ok {
			return ratio, true, name
		}
	}
	return 37.5, operation_setting.SelfUseModeEnabled, name
}

func (snapshot *PricingSnapshot) GetCompletionRatio(name string) float64 {
	name = FormatMatchingModelName(name)
	if strings.Contains(name, "/") {
		if ratio, ok := snapshot.completionRatios[name]; ok {
			return ratio
		}
	}
	hardCodedRatio, contain := getHardcodedCompletionModelRatio(name)
	if contain {
		return hardCodedRatio
	}
	if ratio, ok := snapshot.completionRatios[name]; ok {
		return ratio
	}
	return hardCodedRatio
}

func (snapshot *PricingSnapshot) GetCompletionRatioInfo(name string) CompletionRatioInfo {
	name = FormatMatchingModelName(name)
	if strings.Contains(name, "/") {
		if ratio, ok := snapshot.completionRatios[name]; ok {
			return CompletionRatioInfo{Ratio: ratio, Locked: false}
		}
	}
	hardCodedRatio, locked := getHardcodedCompletionModelRatio(name)
	if locked {
		return CompletionRatioInfo{Ratio: hardCodedRatio, Locked: true}
	}
	if ratio, ok := snapshot.completionRatios[name]; ok {
		return CompletionRatioInfo{Ratio: ratio, Locked: false}
	}
	return CompletionRatioInfo{Ratio: hardCodedRatio, Locked: false}
}

func (snapshot *PricingSnapshot) GetCacheRatio(name string) (float64, bool) {
	ratio, ok := snapshot.cacheRatios[name]
	if !ok {
		return 1, false
	}
	return ratio, true
}

func (snapshot *PricingSnapshot) GetCreateCacheRatio(name string) (float64, bool) {
	ratio, ok := snapshot.createCacheRatios[name]
	if !ok {
		return 1.25, false
	}
	return ratio, true
}

func (snapshot *PricingSnapshot) GetImageRatio(name string) (float64, bool) {
	ratio, ok := snapshot.imageRatios[name]
	if !ok {
		return 1, false
	}
	return ratio, true
}

func (snapshot *PricingSnapshot) GetAudioRatio(name string) float64 {
	name = FormatMatchingModelName(name)
	if ratio, ok := snapshot.audioRatios[name]; ok {
		return ratio
	}
	return 1
}

func (snapshot *PricingSnapshot) GetAudioCompletionRatio(name string) float64 {
	name = FormatMatchingModelName(name)
	if ratio, ok := snapshot.audioCompletionRatio[name]; ok {
		return ratio
	}
	return 1
}

func (snapshot *PricingSnapshot) ContainsAudioRatio(name string) bool {
	name = FormatMatchingModelName(name)
	_, ok := snapshot.audioRatios[name]
	return ok
}

func (snapshot *PricingSnapshot) ContainsAudioCompletionRatio(name string) bool {
	name = FormatMatchingModelName(name)
	_, ok := snapshot.audioCompletionRatio[name]
	return ok
}
