package account_pool_setting

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	QuotaOverridesConfigName  = "account_pool_overrides"
	QuotaOverridesOptionKey   = QuotaOverridesConfigName + ".overrides"
	QuotaOverrideMaxEntries   = 10000
	QuotaRefreshAuto          = "auto"
	QuotaRefreshManual        = "manual"
	QuotaPinReleaseAtTime     = "at_time"
	QuotaPinReleaseAuto       = "auto_refresh"
	QuotaPinReleaseManual     = "manual_refresh"
	QuotaPinReleaseAnyRefresh = "any_refresh"
)

// QuotaPin displays an account's remaining quota at 100% until one release
// condition is met. ReleaseMode decides whether the pin waits for a wall-clock
// time or for the next refresh round.
type QuotaPin struct {
	ReleaseMode string     `json:"release_mode"`
	ReleaseAt   *time.Time `json:"release_at,omitempty"`
}

type QuotaOverride struct {
	OffsetPercent float64   `json:"offset_percent"`
	Pin           *QuotaPin `json:"pin,omitempty"`
	// UpdatedAt is written by the server and guards refresh-triggered release
	// from clearing a pin an administrator set while a slow round was running.
	UpdatedAt time.Time `json:"updated_at"`
}

type quotaOverridesSetting struct {
	Overrides map[string]QuotaOverride `json:"overrides"`
}

var (
	quotaOverrides         = quotaOverridesSetting{Overrides: map[string]QuotaOverride{}}
	quotaOverridesSnapshot atomic.Pointer[quotaOverridesSetting]
)

func init() {
	config.GlobalConfig.Register(QuotaOverridesConfigName, &quotaOverrides)
	quotaOverrides.PublishConfig()
}

func (pin *QuotaPin) ActiveAt(now time.Time) bool {
	if pin == nil {
		return false
	}
	if pin.ReleaseMode != QuotaPinReleaseAtTime {
		return true
	}
	return pin.ReleaseAt != nil && now.Before(*pin.ReleaseAt)
}

func (pin *QuotaPin) ReleasedByRefresh(kind string) bool {
	if pin == nil {
		return false
	}
	switch pin.ReleaseMode {
	case QuotaPinReleaseAuto:
		return kind == QuotaRefreshAuto
	case QuotaPinReleaseManual:
		return kind == QuotaRefreshManual
	case QuotaPinReleaseAnyRefresh:
		return true
	default:
		return false
	}
}

func IsQuotaOverridePublicID(publicID string) bool {
	if len(publicID) != 24 {
		return false
	}
	for _, char := range publicID {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func GetQuotaOverrides() map[string]QuotaOverride {
	snapshot := quotaOverridesSnapshot.Load()
	if snapshot == nil {
		return map[string]QuotaOverride{}
	}
	return cloneQuotaOverrides(snapshot.Overrides)
}

func GetQuotaOverride(publicID string) (QuotaOverride, bool) {
	snapshot := quotaOverridesSnapshot.Load()
	if snapshot == nil {
		return QuotaOverride{}, false
	}
	override, ok := snapshot.Overrides[publicID]
	if !ok {
		return QuotaOverride{}, false
	}
	return cloneQuotaOverride(override), true
}

// PrepareQuotaOverrides validates the complete map before it is persisted and
// drops entries that would have no visible effect.
func PrepareQuotaOverrides(overrides map[string]QuotaOverride) (map[string]QuotaOverride, error) {
	if len(overrides) > QuotaOverrideMaxEntries {
		return nil, fmt.Errorf("too many quota overrides")
	}
	prepared := make(map[string]QuotaOverride, len(overrides))
	for publicID, override := range overrides {
		if err := validateQuotaOverride(publicID, override); err != nil {
			return nil, err
		}
		if isEmptyQuotaOverride(override) {
			continue
		}
		prepared[publicID] = cloneQuotaOverride(override)
	}
	return prepared, nil
}

func (setting *quotaOverridesSetting) ValidateConfig() error {
	if len(setting.Overrides) > QuotaOverrideMaxEntries {
		return fmt.Errorf("too many quota overrides")
	}
	for publicID, override := range setting.Overrides {
		if err := validateQuotaOverride(publicID, override); err != nil {
			return err
		}
	}
	return nil
}

// ValidateLoadedConfig keeps startup resilient: a single malformed persisted
// entry must not prevent every other config module from loading. Invalid
// entries are dropped instead of rejecting the whole map.
func (setting *quotaOverridesSetting) ValidateLoadedConfig() error {
	if setting.Overrides == nil {
		setting.Overrides = map[string]QuotaOverride{}
		return nil
	}
	for publicID, override := range setting.Overrides {
		if !IsQuotaOverridePublicID(publicID) ||
			math.IsNaN(override.OffsetPercent) ||
			math.IsInf(override.OffsetPercent, 0) ||
			override.OffsetPercent < -100 ||
			override.OffsetPercent > 100 {
			delete(setting.Overrides, publicID)
			continue
		}
		if override.Pin != nil {
			switch override.Pin.ReleaseMode {
			case QuotaPinReleaseAtTime:
				if override.Pin.ReleaseAt == nil {
					override.Pin = nil
				}
			case QuotaPinReleaseAuto, QuotaPinReleaseManual, QuotaPinReleaseAnyRefresh:
				override.Pin.ReleaseAt = nil
			default:
				override.Pin = nil
			}
			setting.Overrides[publicID] = override
		}
		if isEmptyQuotaOverride(override) {
			delete(setting.Overrides, publicID)
		}
	}
	if len(setting.Overrides) > QuotaOverrideMaxEntries {
		kept := 0
		for publicID := range setting.Overrides {
			if kept >= QuotaOverrideMaxEntries {
				delete(setting.Overrides, publicID)
				continue
			}
			kept++
		}
	}
	return nil
}

func (setting *quotaOverridesSetting) PublishConfig() {
	copy := quotaOverridesSetting{Overrides: cloneQuotaOverrides(setting.Overrides)}
	quotaOverridesSnapshot.Store(&copy)
}

func validateQuotaOverride(publicID string, override QuotaOverride) error {
	if !IsQuotaOverridePublicID(publicID) {
		return fmt.Errorf("invalid account id")
	}
	if math.IsNaN(override.OffsetPercent) ||
		math.IsInf(override.OffsetPercent, 0) ||
		override.OffsetPercent < -100 ||
		override.OffsetPercent > 100 {
		return fmt.Errorf("offset_percent must be between -100 and 100")
	}
	if override.Pin == nil {
		return nil
	}
	switch override.Pin.ReleaseMode {
	case QuotaPinReleaseAtTime:
		if override.Pin.ReleaseAt == nil {
			return fmt.Errorf("release_at is required for at_time")
		}
	case QuotaPinReleaseAuto, QuotaPinReleaseManual, QuotaPinReleaseAnyRefresh:
		if override.Pin.ReleaseAt != nil {
			return fmt.Errorf("release_at is only valid for at_time")
		}
	default:
		return fmt.Errorf("invalid pin release mode")
	}
	return nil
}

func isEmptyQuotaOverride(override QuotaOverride) bool {
	return override.OffsetPercent == 0 && override.Pin == nil
}

func cloneQuotaOverrides(overrides map[string]QuotaOverride) map[string]QuotaOverride {
	if overrides == nil {
		return map[string]QuotaOverride{}
	}
	copied := make(map[string]QuotaOverride, len(overrides))
	for publicID, override := range overrides {
		copied[publicID] = cloneQuotaOverride(override)
	}
	return copied
}

func cloneQuotaOverride(override QuotaOverride) QuotaOverride {
	copied := override
	if override.Pin != nil {
		pin := *override.Pin
		if override.Pin.ReleaseAt != nil {
			releaseAt := *override.Pin.ReleaseAt
			pin.ReleaseAt = &releaseAt
		}
		copied.Pin = &pin
	}
	return copied
}
