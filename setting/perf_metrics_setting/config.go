package perf_metrics_setting

import (
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

type PerfMetricsSetting struct {
	Enabled       bool   `json:"enabled"`
	FlushInterval int    `json:"flush_interval"`
	BucketTime    string `json:"bucket_time"`
	RetentionDays int    `json:"retention_days"`
}

var perfMetricsSetting = PerfMetricsSetting{
	Enabled:       true,
	FlushInterval: 5,
	BucketTime:    "hour",
	RetentionDays: 0,
}
var perfMetricsSettingSnapshot atomic.Pointer[PerfMetricsSetting]

func init() {
	config.GlobalConfig.Register("perf_metrics_setting", &perfMetricsSetting)
	perfMetricsSetting.PublishConfig()
}

func (setting *PerfMetricsSetting) PublishConfig() {
	snapshot := *setting
	perfMetricsSettingSnapshot.Store(&snapshot)
}

func GetSetting() PerfMetricsSetting {
	return *perfMetricsSettingSnapshot.Load()
}

func GetBucketSeconds() int64 {
	switch GetSetting().BucketTime {
	case "minute":
		return 60
	case "5min":
		return 300
	case "hour":
		return 3600
	default:
		return 3600
	}
}

func GetFlushIntervalMinutes() int {
	flushInterval := GetSetting().FlushInterval
	if flushInterval < 1 {
		return 1
	}
	return flushInterval
}
