package prompt_audit_setting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ModeOff        = "off"
	ModeAsyncAudit = "async_audit"
	ModeBlocking   = "blocking"

	DefaultModel               = "sileader/qwen3guard:0.6b"
	DefaultEndpointTimeoutMS   = 3000
	DefaultEndpointInputLimit  = 4000
	DefaultTotalTimeoutMS      = 10000
	DefaultChunkOverlap        = 64
	DefaultCacheTTLSeconds     = 600
	DefaultWorkerCount         = 4
	DefaultMaxAttempts         = 4
	DefaultRetentionDays       = 30
	DefaultGlobalConcurrency   = 64
	DefaultEndpointConcurrency = 16
	MaxAttemptsLimit           = 4
)

var AllCategoryIDs = []string{
	"violent",
	"non_violent_illegal_acts",
	"sexual_content_or_sexual_acts",
	"pii",
	"suicide_and_self_harm",
	"unethical_acts",
	"politically_sensitive_topics",
	"copyright_violation",
	"jailbreak",
}

var categorySet = func() map[string]struct{} {
	result := make(map[string]struct{}, len(AllCategoryIDs))
	for _, category := range AllCategoryIDs {
		result[category] = struct{}{}
	}
	return result
}()

// Endpoint is one ordered OpenAI-compatible Qwen3Guard node. Token is stored
// only in the modular option payload; management responses must use Sanitized.
type Endpoint struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Token       string `json:"token,omitempty"`
	Model       string `json:"model"`
	TimeoutMS   int    `json:"timeout_ms"`
	InputLimit  int    `json:"input_limit"`
	Concurrency int    `json:"concurrency"`
	Enabled     bool   `json:"enabled"`
}

type SanitizedEndpoint struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	TimeoutMS   int    `json:"timeout_ms"`
	InputLimit  int    `json:"input_limit"`
	Concurrency int    `json:"concurrency"`
	Enabled     bool   `json:"enabled"`
	HasToken    bool   `json:"has_token"`
}

// PromptAuditSetting is persisted through the modular option manager. The
// endpoints field deliberately ends in "secret" so the legacy root-only option
// listing omits the complete JSON value (which contains endpoint tokens).
type PromptAuditSetting struct {
	Mode                string     `json:"mode"`
	EnabledCategories   []string   `json:"enabled_categories"`
	AllGroups           bool       `json:"all_groups"`
	Groups              []string   `json:"groups"`
	Endpoints           []Endpoint `json:"endpoints_secret"`
	TotalTimeoutMS      int        `json:"total_timeout_ms"`
	ChunkOverlap        int        `json:"chunk_overlap"`
	CacheTTLSeconds     int        `json:"cache_ttl_seconds"`
	WorkerCount         int        `json:"worker_count"`
	MaxAttempts         int        `json:"max_attempts"`
	RetentionDays       int        `json:"retention_days"`
	GlobalConcurrency   int        `json:"global_concurrency"`
	EndpointConcurrency int        `json:"endpoint_concurrency"`
	ConfigVersion       string     `json:"-"`
}

var promptAuditSetting = PromptAuditSetting{
	Mode:                ModeOff,
	EnabledCategories:   append([]string(nil), AllCategoryIDs...),
	AllGroups:           true,
	TotalTimeoutMS:      DefaultTotalTimeoutMS,
	ChunkOverlap:        DefaultChunkOverlap,
	CacheTTLSeconds:     DefaultCacheTTLSeconds,
	WorkerCount:         DefaultWorkerCount,
	MaxAttempts:         DefaultMaxAttempts,
	RetentionDays:       DefaultRetentionDays,
	GlobalConcurrency:   DefaultGlobalConcurrency,
	EndpointConcurrency: DefaultEndpointConcurrency,
}

var promptAuditSettingSnapshot atomic.Pointer[PromptAuditSetting]

func init() {
	config.GlobalConfig.Register("prompt_audit", &promptAuditSetting)
	promptAuditSetting.PublishConfig()
}

func GetSetting() PromptAuditSetting {
	snapshot := promptAuditSettingSnapshot.Load()
	if snapshot == nil {
		return PromptAuditSetting{Mode: ModeOff}
	}
	return cloneSetting(*snapshot)
}

func (setting PromptAuditSetting) AppliesToGroup(group string) bool {
	if setting.Mode == ModeOff {
		return false
	}
	if setting.AllGroups {
		return true
	}
	group = strings.TrimSpace(group)
	// The concrete execution group is resolved only during channel selection,
	// which intentionally happens after prompt audit. Audit an automatic-group
	// request whenever any selected-group policy is enabled so "auto" cannot be
	// used to bypass that policy.
	if group == "auto" {
		return len(setting.Groups) > 0
	}
	for _, configured := range setting.Groups {
		if configured == group {
			return true
		}
	}
	return false
}

func (setting PromptAuditSetting) SanitizedEndpoints() []SanitizedEndpoint {
	result := make([]SanitizedEndpoint, 0, len(setting.Endpoints))
	for _, endpoint := range setting.Endpoints {
		result = append(result, SanitizedEndpoint{
			ID: endpoint.ID, Name: endpoint.Name, BaseURL: endpoint.BaseURL,
			Model: endpoint.Model, TimeoutMS: endpoint.TimeoutMS,
			InputLimit: endpoint.InputLimit, Concurrency: endpoint.Concurrency,
			Enabled: endpoint.Enabled, HasToken: endpoint.Token != "",
		})
	}
	return result
}

func (setting *PromptAuditSetting) ValidateConfig() error {
	if setting == nil {
		return fmt.Errorf("prompt audit setting is required")
	}
	mode := strings.ToLower(strings.TrimSpace(setting.Mode))
	if mode != ModeOff && mode != ModeAsyncAudit && mode != ModeBlocking {
		return fmt.Errorf("prompt audit mode must be %q, %q, or %q", ModeOff, ModeAsyncAudit, ModeBlocking)
	}
	if setting.TotalTimeoutMS < 100 || setting.TotalTimeoutMS > 120000 {
		return fmt.Errorf("prompt audit total timeout must be between 100 and 120000 milliseconds")
	}
	if setting.ChunkOverlap < 0 || setting.ChunkOverlap > 512 {
		return fmt.Errorf("prompt audit chunk overlap must be between 0 and 512 characters")
	}
	if setting.CacheTTLSeconds < 0 || setting.CacheTTLSeconds > 86400 {
		return fmt.Errorf("prompt audit cache TTL must be between 0 and 86400 seconds")
	}
	if setting.WorkerCount < 1 || setting.WorkerCount > 64 {
		return fmt.Errorf("prompt audit worker count must be between 1 and 64")
	}
	if setting.MaxAttempts < 1 || setting.MaxAttempts > MaxAttemptsLimit {
		return fmt.Errorf("prompt audit max attempts must be between 1 and %d", MaxAttemptsLimit)
	}
	if setting.RetentionDays < 0 || setting.RetentionDays > 3650 {
		return fmt.Errorf("prompt audit retention days must be between 0 and 3650")
	}
	if setting.GlobalConcurrency < 1 || setting.GlobalConcurrency > 1024 {
		return fmt.Errorf("prompt audit global concurrency must be between 1 and 1024")
	}
	if setting.EndpointConcurrency < 1 || setting.EndpointConcurrency > 256 {
		return fmt.Errorf("prompt audit endpoint concurrency must be between 1 and 256")
	}

	seenCategories := make(map[string]struct{}, len(setting.EnabledCategories))
	for _, category := range setting.EnabledCategories {
		category = strings.ToLower(strings.TrimSpace(category))
		if _, ok := categorySet[category]; !ok {
			return fmt.Errorf("unknown prompt audit category %q", category)
		}
		if _, duplicate := seenCategories[category]; duplicate {
			return fmt.Errorf("duplicate prompt audit category %q", category)
		}
		seenCategories[category] = struct{}{}
	}

	seenGroups := make(map[string]struct{}, len(setting.Groups))
	for _, group := range setting.Groups {
		group = strings.TrimSpace(group)
		if group == "" {
			return fmt.Errorf("prompt audit group names must not be empty")
		}
		if _, duplicate := seenGroups[group]; duplicate {
			return fmt.Errorf("duplicate prompt audit group %q", group)
		}
		seenGroups[group] = struct{}{}
	}
	if !setting.AllGroups && len(setting.Groups) == 0 && mode != ModeOff {
		return fmt.Errorf("prompt audit requires at least one group when all_groups is false")
	}

	seenIDs := make(map[string]struct{}, len(setting.Endpoints))
	enabledCount := 0
	minimumInputLimit := 0
	for index, endpoint := range setting.Endpoints {
		endpoint = normalizeEndpoint(endpoint, index)
		if _, duplicate := seenIDs[endpoint.ID]; duplicate {
			return fmt.Errorf("duplicate prompt audit endpoint id %q", endpoint.ID)
		}
		seenIDs[endpoint.ID] = struct{}{}
		if endpoint.TimeoutMS < 100 || endpoint.TimeoutMS > 120000 {
			return fmt.Errorf("prompt audit endpoint %q timeout must be between 100 and 120000 milliseconds", endpoint.ID)
		}
		if endpoint.InputLimit < 256 || endpoint.InputLimit > 1048576 {
			return fmt.Errorf("prompt audit endpoint %q input limit must be between 256 and 1048576 characters", endpoint.ID)
		}
		if endpoint.Concurrency < 1 || endpoint.Concurrency > 256 {
			return fmt.Errorf("prompt audit endpoint %q concurrency must be between 1 and 256", endpoint.ID)
		}
		if err := validateBaseURL(endpoint.BaseURL); err != nil {
			return fmt.Errorf("prompt audit endpoint %q: %w", endpoint.ID, err)
		}
		if endpoint.Model == "" {
			return fmt.Errorf("prompt audit endpoint %q model is required", endpoint.ID)
		}
		if endpoint.Enabled {
			enabledCount++
			if minimumInputLimit == 0 || endpoint.InputLimit < minimumInputLimit {
				minimumInputLimit = endpoint.InputLimit
			}
		}
	}
	if mode != ModeOff && enabledCount == 0 {
		return fmt.Errorf("prompt audit requires at least one enabled endpoint")
	}
	if minimumInputLimit > 0 && setting.ChunkOverlap >= minimumInputLimit {
		return fmt.Errorf("prompt audit chunk overlap must be smaller than the minimum enabled endpoint input limit")
	}
	return nil
}

// Invalid persisted prompt-audit configuration must never leave a previously
// enabled runtime policy active. The config manager starts from the safe default
// (off) and publishes persisted values only after this full validation passes.
func (setting *PromptAuditSetting) ValidateLoadedConfig() error {
	return setting.ValidateConfig()
}

func (setting *PromptAuditSetting) PublishConfig() {
	snapshot := cloneSetting(*setting)
	snapshot.Mode = strings.ToLower(strings.TrimSpace(snapshot.Mode))
	for index := range snapshot.EnabledCategories {
		snapshot.EnabledCategories[index] = strings.ToLower(strings.TrimSpace(snapshot.EnabledCategories[index]))
	}
	for index := range snapshot.Groups {
		snapshot.Groups[index] = strings.TrimSpace(snapshot.Groups[index])
	}
	for index := range snapshot.Endpoints {
		snapshot.Endpoints[index] = normalizeEndpoint(snapshot.Endpoints[index], index)
	}
	snapshot.ConfigVersion = settingFingerprint(snapshot)
	promptAuditSettingSnapshot.Store(&snapshot)
}

func normalizeEndpoint(endpoint Endpoint, index int) Endpoint {
	endpoint.ID = strings.TrimSpace(endpoint.ID)
	if endpoint.ID == "" {
		endpoint.ID = "endpoint-" + strconv.Itoa(index+1)
	}
	endpoint.Name = strings.TrimSpace(endpoint.Name)
	if endpoint.Name == "" {
		endpoint.Name = endpoint.ID
	}
	endpoint.BaseURL = strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	endpoint.Token = strings.TrimSpace(endpoint.Token)
	endpoint.Model = strings.TrimSpace(endpoint.Model)
	if endpoint.Model == "" {
		endpoint.Model = DefaultModel
	}
	if endpoint.TimeoutMS == 0 {
		endpoint.TimeoutMS = DefaultEndpointTimeoutMS
	}
	if endpoint.InputLimit == 0 {
		endpoint.InputLimit = DefaultEndpointInputLimit
	}
	if endpoint.Concurrency == 0 {
		endpoint.Concurrency = DefaultEndpointConcurrency
	}
	return endpoint
}

func validateBaseURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("base URL must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("base URL must not contain credentials, query parameters, or fragments")
	}
	return nil
}

func cloneSetting(setting PromptAuditSetting) PromptAuditSetting {
	setting.EnabledCategories = append([]string(nil), setting.EnabledCategories...)
	setting.Groups = append([]string(nil), setting.Groups...)
	setting.Endpoints = append([]Endpoint(nil), setting.Endpoints...)
	return setting
}

func settingFingerprint(setting PromptAuditSetting) string {
	var builder strings.Builder
	builder.WriteString(setting.Mode)
	builder.WriteByte('|')
	categories := append([]string(nil), setting.EnabledCategories...)
	sort.Strings(categories)
	builder.WriteString(strings.Join(categories, ","))
	builder.WriteByte('|')
	builder.WriteString(strconv.FormatBool(setting.AllGroups))
	builder.WriteByte('|')
	groups := append([]string(nil), setting.Groups...)
	sort.Strings(groups)
	builder.WriteString(strings.Join(groups, ","))
	for _, value := range []int{setting.TotalTimeoutMS, setting.ChunkOverlap, setting.CacheTTLSeconds, setting.WorkerCount, setting.MaxAttempts, setting.RetentionDays, setting.GlobalConcurrency, setting.EndpointConcurrency} {
		builder.WriteByte('|')
		builder.WriteString(strconv.Itoa(value))
	}
	for _, endpoint := range setting.Endpoints {
		builder.WriteByte('|')
		builder.WriteString(endpoint.ID)
		builder.WriteByte('|')
		builder.WriteString(endpoint.Name)
		builder.WriteByte('|')
		builder.WriteString(endpoint.BaseURL)
		builder.WriteByte('|')
		builder.WriteString(endpoint.Model)
		builder.WriteByte('|')
		builder.WriteString(strconv.Itoa(endpoint.TimeoutMS))
		builder.WriteByte('|')
		builder.WriteString(strconv.Itoa(endpoint.InputLimit))
		builder.WriteByte('|')
		builder.WriteString(strconv.Itoa(endpoint.Concurrency))
		builder.WriteByte('|')
		builder.WriteString(strconv.FormatBool(endpoint.Enabled))
		tokenDigest := sha256.Sum256([]byte(endpoint.Token))
		builder.WriteByte('|')
		builder.WriteString(hex.EncodeToString(tokenDigest[:]))
	}
	digest := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(digest[:])
}
