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

// QuotaPin displays one quota window at 100% until one release condition is
// met. ReleaseMode decides whether the pin waits for a wall-clock time or for
// the next refresh round.
type QuotaPin struct {
	ReleaseMode string     `json:"release_mode"`
	ReleaseAt   *time.Time `json:"release_at,omitempty"`
}

type QuotaWindowOverride struct {
	OffsetPercent float64   `json:"offset_percent"`
	Pin           *QuotaPin `json:"pin,omitempty"`
}

// QuotaOverride adjusts the two limits of one account independently: the
// primary (5-hour) window and the secondary (weekly or monthly) window.
type QuotaOverride struct {
	Primary   QuotaWindowOverride `json:"primary"`
	Secondary QuotaWindowOverride `json:"secondary"`
	// UpdatedAt is written by the server and guards refresh-triggered release
	// from clearing a pin an administrator set while a slow round was running.
	UpdatedAt time.Time `json:"updated_at"`

	// Legacy fields from the first, single-window shape. They are only read
	// while loading persisted configuration and are migrated into both
	// windows, then dropped on the next write.
	LegacyOffsetPercent *float64  `json:"offset_percent,omitempty"`
	LegacyPin           *QuotaPin `json:"pin,omitempty"`
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
		if !IsQuotaOverridePublicID(publicID) {
			delete(setting.Overrides, publicID)
			continue
		}
		override = migrateLegacyQuotaOverride(override)
		if !normalizeLoadedQuotaWindow(&override.Primary) || !normalizeLoadedQuotaWindow(&override.Secondary) {
			delete(setting.Overrides, publicID)
			continue
		}
		if isEmptyQuotaOverride(override) {
			delete(setting.Overrides, publicID)
			continue
		}
		setting.Overrides[publicID] = override
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
	if override.LegacyOffsetPercent != nil || override.LegacyPin != nil {
		return fmt.Errorf("legacy quota override fields are not accepted")
	}
	if err := validateQuotaWindowOverride(override.Primary); err != nil {
		return fmt.Errorf("primary: %w", err)
	}
	if err := validateQuotaWindowOverride(override.Secondary); err != nil {
		return fmt.Errorf("secondary: %w", err)
	}
	return nil
}

func validateQuotaWindowOverride(override QuotaWindowOverride) error {
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

// normalizeLoadedQuotaWindow reports false when the window entry is beyond
// repair. Recoverable problems (an unknown release mode, a missing release
// time, an offset-only remainder) are normalized in place.
func normalizeLoadedQuotaWindow(window *QuotaWindowOverride) bool {
	if math.IsNaN(window.OffsetPercent) ||
		math.IsInf(window.OffsetPercent, 0) ||
		window.OffsetPercent < -100 ||
		window.OffsetPercent > 100 {
		return false
	}
	if window.Pin == nil {
		return true
	}
	switch window.Pin.ReleaseMode {
	case QuotaPinReleaseAtTime:
		if window.Pin.ReleaseAt == nil {
			window.Pin = nil
		}
	case QuotaPinReleaseAuto, QuotaPinReleaseManual, QuotaPinReleaseAnyRefresh:
		window.Pin.ReleaseAt = nil
	default:
		window.Pin = nil
	}
	return true
}

func migrateLegacyQuotaOverride(override QuotaOverride) QuotaOverride {
	if override.LegacyOffsetPercent == nil && override.LegacyPin == nil {
		return override
	}
	legacy := QuotaWindowOverride{Pin: override.LegacyPin}
	if override.LegacyOffsetPercent != nil {
		legacy.OffsetPercent = *override.LegacyOffsetPercent
	}
	if isEmptyQuotaWindowOverride(override.Primary) && isEmptyQuotaWindowOverride(override.Secondary) {
		override.Primary = legacy
		override.Secondary = cloneQuotaWindowOverride(legacy)
	}
	override.LegacyOffsetPercent = nil
	override.LegacyPin = nil
	return override
}

func isEmptyQuotaOverride(override QuotaOverride) bool {
	return isEmptyQuotaWindowOverride(override.Primary) && isEmptyQuotaWindowOverride(override.Secondary)
}

func isEmptyQuotaWindowOverride(override QuotaWindowOverride) bool {
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
	copied.Primary = cloneQuotaWindowOverride(override.Primary)
	copied.Secondary = cloneQuotaWindowOverride(override.Secondary)
	if override.LegacyOffsetPercent != nil {
		value := *override.LegacyOffsetPercent
		copied.LegacyOffsetPercent = &value
	}
	copied.LegacyPin = cloneQuotaPin(override.LegacyPin)
	return copied
}

func cloneQuotaWindowOverride(override QuotaWindowOverride) QuotaWindowOverride {
	copied := override
	copied.Pin = cloneQuotaPin(override.Pin)
	return copied
}

func cloneQuotaPin(pin *QuotaPin) *QuotaPin {
	if pin == nil {
		return nil
	}
	copied := *pin
	if pin.ReleaseAt != nil {
		releaseAt := *pin.ReleaseAt
		copied.ReleaseAt = &releaseAt
	}
	return &copied
}
