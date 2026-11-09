package operation_setting

import (
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	SiteClosureConfigName = "site_closure"

	SiteClosureWebClosedKey          = SiteClosureConfigName + ".web_closed"
	SiteClosureAPIPausedKey          = SiteClosureConfigName + ".api_paused"
	SiteClosureTitleKey              = SiteClosureConfigName + ".title"
	SiteClosureMessageKey            = SiteClosureConfigName + ".message"
	SiteClosureExpectedRecoveryAtKey = SiteClosureConfigName + ".expected_recovery_at"

	SiteClosureMaxTitleRunes   = 200
	SiteClosureMaxMessageRunes = 8192
)

// SiteClosureSetting controls the maintenance mode exposed by /api/status and
// enforced by the site-closure middleware.
type SiteClosureSetting struct {
	WebClosed          bool   `json:"web_closed"`
	APIPaused          bool   `json:"api_paused"`
	Title              string `json:"title"`
	Message            string `json:"message"`
	ExpectedRecoveryAt int64  `json:"expected_recovery_at"` // Unix seconds, 0 = unset
}

var siteClosureSetting = SiteClosureSetting{}
var siteClosureSnapshot atomic.Pointer[SiteClosureSetting]

func init() {
	config.GlobalConfig.Register(SiteClosureConfigName, &siteClosureSetting)
	siteClosureSetting.PublishConfig()
}

// GetSiteClosureSetting returns an immutable snapshot. The middleware reads it
// on every relay request, so it must not depend on the config manager lock.
func GetSiteClosureSetting() SiteClosureSetting {
	snapshot := siteClosureSnapshot.Load()
	if snapshot == nil {
		return SiteClosureSetting{}
	}
	return *snapshot
}

func (setting *SiteClosureSetting) ValidateConfig() error {
	if setting.ExpectedRecoveryAt < 0 {
		return common.NewMessage("Expected recovery time must not be negative")
	}
	if utf8.RuneCountInString(setting.Title) > SiteClosureMaxTitleRunes {
		return common.NewMessage("Maintenance title must be at most 200 characters")
	}
	if utf8.RuneCountInString(setting.Message) > SiteClosureMaxMessageRunes {
		return common.NewMessage("Maintenance message must be at most 8192 characters")
	}
	return nil
}

// ValidateLoadedConfig keeps a persisted value from disabling the whole module.
func (setting *SiteClosureSetting) ValidateLoadedConfig() error {
	if setting.ExpectedRecoveryAt < 0 {
		setting.ExpectedRecoveryAt = 0
	}
	setting.Title = truncateSiteClosureRunes(setting.Title, SiteClosureMaxTitleRunes)
	setting.Message = truncateSiteClosureRunes(setting.Message, SiteClosureMaxMessageRunes)
	return nil
}

func (setting *SiteClosureSetting) PublishConfig() {
	setting.Title = strings.TrimSpace(setting.Title)
	snapshot := *setting
	siteClosureSnapshot.Store(&snapshot)
}

func truncateSiteClosureRunes(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	return string([]rune(value)[:maxRunes])
}
