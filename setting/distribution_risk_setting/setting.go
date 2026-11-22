package distribution_risk_setting

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ModeDisabled = "disabled"
	ModeShadow   = "shadow"
	ModeEnforce  = "enforce"
	ShadowDays   = 7
)

type DistributionRiskSetting struct {
	Enabled           bool  `json:"enabled"`
	StartedAt         int64 `json:"started_at"`
	IPFloor           int   `json:"ip_floor"`
	UserAgentFloor    int   `json:"user_agent_floor"`
	SessionFloor      int   `json:"session_floor"`
	RequestsPerMinute int   `json:"requests_per_minute_floor"`
	ConcurrencyFloor  int   `json:"concurrency_floor"`
	MinRequests       int   `json:"min_requests"`
	MinActiveDays     int   `json:"min_active_days"`
}

var setting = DistributionRiskSetting{
	IPFloor:           3,
	UserAgentFloor:    3,
	SessionFloor:      5,
	RequestsPerMinute: 60,
	ConcurrencyFloor:  20,
	MinRequests:       100,
	MinActiveDays:     3,
}

var snapshot atomic.Pointer[DistributionRiskSetting]

func init() {
	config.GlobalConfig.Register("distribution_risk_setting", &setting)
	publishSnapshot()
}

func GetSnapshot() DistributionRiskSetting {
	current := snapshot.Load()
	if current == nil {
		return setting
	}
	return *current
}

func (candidate *DistributionRiskSetting) ValidateConfig() error {
	if candidate.IPFloor < 2 || candidate.IPFloor > 1000 {
		return fmt.Errorf("IP floor must be between 2 and 1000")
	}
	if candidate.UserAgentFloor < 2 || candidate.UserAgentFloor > 1000 {
		return fmt.Errorf("user-agent floor must be between 2 and 1000")
	}
	if candidate.SessionFloor < 2 || candidate.SessionFloor > 10000 {
		return fmt.Errorf("session floor must be between 2 and 10000")
	}
	if candidate.RequestsPerMinute < 10 || candidate.RequestsPerMinute > 100000 {
		return fmt.Errorf("request-rate floor must be between 10 and 100000")
	}
	if candidate.ConcurrencyFloor < 2 || candidate.ConcurrencyFloor > 10000 {
		return fmt.Errorf("concurrency floor must be between 2 and 10000")
	}
	if candidate.MinRequests < 100 || candidate.MinRequests > 1000000 {
		return fmt.Errorf("minimum requests must be between 100 and 1000000")
	}
	if candidate.MinActiveDays < 3 || candidate.MinActiveDays > 30 {
		return fmt.Errorf("minimum active days must be between 3 and 30")
	}
	if candidate.StartedAt < 0 {
		return fmt.Errorf("start time cannot be negative")
	}
	return nil
}

func (candidate *DistributionRiskSetting) PublishConfig() {
	publishSnapshot()
}

func CurrentMode() string {
	current := GetSnapshot()
	if !current.Enabled {
		return ModeDisabled
	}
	if current.StartedAt <= 0 || time.Now().Unix() < current.StartedAt+int64(ShadowDays*24*time.Hour/time.Second) {
		return ModeShadow
	}
	return ModeEnforce
}

func publishSnapshot() {
	copy := setting
	snapshot.Store(&copy)
}
