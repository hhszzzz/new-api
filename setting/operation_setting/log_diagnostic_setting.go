package operation_setting

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

// LogDiagnosticSetting controls request diagnostics stored with usage/error
// logs.  The setting is deliberately global: a user must not be able to turn
// on collection of another user's network metadata.
type LogDiagnosticSetting struct {
	RecordIP      bool     `json:"record_ip"`
	RecordHeaders bool     `json:"record_headers"`
	ExtraHeaders  []string `json:"extra_headers"`
}

var logDiagnosticSetting = LogDiagnosticSetting{
	RecordIP:      false,
	RecordHeaders: false,
	ExtraHeaders:  []string{},
}

var logDiagnosticSettingSnapshot atomic.Pointer[LogDiagnosticSetting]

func init() {
	config.GlobalConfig.Register("log_diagnostic_setting", &logDiagnosticSetting)
	publishLogDiagnosticSettingSnapshot()
}

// GetLogDiagnosticSetting returns the mutable ConfigManager target. Request
// paths must use GetLogDiagnosticSettingSnapshot so hot updates cannot race
// with readers.
func GetLogDiagnosticSetting() *LogDiagnosticSetting {
	return &logDiagnosticSetting
}

// GetLogDiagnosticSettingSnapshot returns an immutable runtime snapshot.
func GetLogDiagnosticSettingSnapshot() *LogDiagnosticSetting {
	return logDiagnosticSettingSnapshot.Load()
}

func NormalizeLogDiagnosticSetting() {
	logDiagnosticSetting.PublishConfig()
}

func (setting *LogDiagnosticSetting) ValidateConfig() error {
	return ValidateLogDiagnosticHeaders(setting.ExtraHeaders)
}

func (setting *LogDiagnosticSetting) PublishConfig() {
	setting.ExtraHeaders = NormalizeLogDiagnosticHeaders(setting.ExtraHeaders)
	publishLogDiagnosticSettingSnapshot()
}

func publishLogDiagnosticSettingSnapshot() {
	extraHeaders := append([]string(nil), logDiagnosticSetting.ExtraHeaders...)
	if extraHeaders == nil {
		extraHeaders = []string{}
	}
	logDiagnosticSettingSnapshot.Store(&LogDiagnosticSetting{
		RecordIP:      logDiagnosticSetting.RecordIP,
		RecordHeaders: logDiagnosticSetting.RecordHeaders,
		ExtraHeaders:  extraHeaders,
	})
}

// NormalizeLogDiagnosticHeaders applies the server-side bounds used by both
// the settings endpoint and the runtime snapshotter. Header names are stored
// case-insensitively and duplicates are removed deterministically.
func NormalizeLogDiagnosticHeaders(headers []string) []string {
	seen := make(map[string]struct{}, len(headers))
	result := make([]string, 0, minInt(len(headers), 16))
	for _, header := range headers {
		header = strings.ToLower(strings.TrimSpace(header))
		if header == "" || len(result) >= 16 || !isValidHeaderName(header) {
			continue
		}
		if _, exists := seen[header]; exists {
			continue
		}
		seen[header] = struct{}{}
		result = append(result, header)
	}
	sort.Strings(result)
	return result
}

func ValidateLogDiagnosticHeaders(headers []string) error {
	if len(headers) > 16 {
		return fmt.Errorf("at most 16 extra headers may be recorded")
	}
	seen := make(map[string]struct{}, len(headers))
	for _, header := range headers {
		header = strings.ToLower(strings.TrimSpace(header))
		if header == "" || !isValidHeaderName(header) {
			return fmt.Errorf("invalid extra header name")
		}
		if _, exists := seen[header]; exists {
			return fmt.Errorf("duplicate extra header: %s", header)
		}
		if !IsDiagnosticHeaderAllowed(header) {
			return fmt.Errorf("header is not allowed for diagnostics: %s", header)
		}
		seen[header] = struct{}{}
	}
	return nil
}

// IsDiagnosticHeaderAllowed excludes credentials and session-bearing headers.
// The standard safe set is intentionally small; administrators can opt into
// additional non-sensitive names through the validated ExtraHeaders list.
func IsDiagnosticHeaderAllowed(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return isValidHeaderName(name) && !isSensitiveRequestHeaderName(name)
}

func isValidHeaderName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
