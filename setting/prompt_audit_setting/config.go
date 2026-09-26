package prompt_audit_setting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ModeOff        = "off"
	ModeAsyncAudit = "async_audit"
	ModeBlocking   = "blocking"

	EndpointPurposeClassify = "classify"
	EndpointPurposeReview   = "review"
	WordlistActionBlock     = "block"
	WordlistActionReview    = "review"

	// EndpointProtocolQwen3Guard is the original audit node: an OpenAI-compatible
	// guard model that answers with a safety label.
	EndpointProtocolQwen3Guard = "qwen3guard"
	// EndpointProtocolTypeSafe is a TypeSafe/JEV node. It answers with a
	// per-question probability instead of a label, so the verdict comes from the
	// thresholds below.
	EndpointProtocolTypeSafe = "typesafe"

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
	DefaultChunkConcurrency    = 4
	// DefaultBlockThreshold and DefaultReviewThreshold follow the TypeSafe
	// Guardrails cookbook "strict" policy: a question at or above
	// DefaultBlockThreshold is Unsafe, at or above DefaultReviewThreshold is
	// Controversial, and anything below is Safe.
	DefaultBlockThreshold  = 0.70
	DefaultReviewThreshold = 0.35
	// DefaultTypeSafeModel is the alias TypeSafe resolves to the current JEV
	// release, so an operator does not have to pin a version to get updates.
	DefaultTypeSafeModel = "jev-latest"
	// TypeSafeMaxInputLimit is the largest prompt a TypeSafe node accepts.
	// TypeSafe caps a request at 64k tokens, with state plus the longest
	// question under 32k; a character budget far above that can only produce
	// rejected requests.
	TypeSafeMaxInputLimit = 16000
	// DefaultProbeSemanticThreshold is the probability at or above which a
	// message is treated as a liveness probe. 0.6 sits above the noise floor
	// of a genuine question and below the confidence a real probe usually
	// reaches; it is a starting point to be calibrated from recorded scores.
	DefaultProbeSemanticThreshold = 0.6
	// MaxProbeSemanticRunes bounds the text a semantic probe scores. A real
	// probe question is a few words; a long first message is somebody working.
	MaxProbeSemanticRunes    = 128
	DefaultOutputMaxBytes    = 8 * 1024 * 1024
	DefaultOutputMemoryBytes = 1024 * 1024
	MaxAttemptsLimit         = 4
	// DefaultFullPromptMaxRunes bounds how much of the whole request is kept on
	// every audit record. It matches the cap the audit pipeline shipped with, so
	// an existing deployment keeps the same written volume until an operator
	// changes it.
	DefaultFullPromptMaxRunes = 65536
	// MaxFullPromptMaxRunes is the largest retention limit an operator may set.
	// The records table grows with every character kept and retention is often
	// unlimited, so the limit stays bounded by configuration rather than relying
	// on operators noticing the growth.
	MaxFullPromptMaxRunes = 1048576
	// MinFullPromptMaxRunes is the shortest limit accepted besides 0. A shorter
	// one would drop nearly the whole request, so it is far more likely a typo
	// than an intent.
	MinFullPromptMaxRunes = 1024
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
	ID string `json:"id"`
	// Protocol selects how this node is called and how its answer is read:
	// EndpointProtocolQwen3Guard (the default) or EndpointProtocolTypeSafe.
	Protocol    string   `json:"protocol,omitempty"`
	Name        string   `json:"name"`
	BaseURL     string   `json:"base_url"`
	Token       string   `json:"token,omitempty"`
	Model       string   `json:"model"`
	TimeoutMS   int      `json:"timeout_ms"`
	InputLimit  int      `json:"input_limit"`
	Concurrency int      `json:"concurrency"`
	Enabled     bool     `json:"enabled"`
	Purpose     string   `json:"purpose,omitempty"`
	Directions  []string `json:"directions,omitempty"`
	// BlockThreshold and ReviewThreshold only apply to a TypeSafe node. A zero
	// value means "use the default", so an endpoint persisted before these
	// fields existed keeps the default policy instead of blocking everything.
	BlockThreshold  float64 `json:"block_threshold,omitempty"`
	ReviewThreshold float64 `json:"review_threshold,omitempty"`
}

type SanitizedEndpoint struct {
	ID              string   `json:"id"`
	Protocol        string   `json:"protocol"`
	Name            string   `json:"name"`
	BaseURL         string   `json:"base_url"`
	Model           string   `json:"model"`
	TimeoutMS       int      `json:"timeout_ms"`
	InputLimit      int      `json:"input_limit"`
	Concurrency     int      `json:"concurrency"`
	Enabled         bool     `json:"enabled"`
	HasToken        bool     `json:"has_token"`
	Purpose         string   `json:"purpose"`
	Directions      []string `json:"directions"`
	BlockThreshold  float64  `json:"block_threshold"`
	ReviewThreshold float64  `json:"review_threshold"`
}

// PromptAuditSetting is persisted through the modular option manager. The
// endpoints field deliberately ends in "secret" so the legacy root-only option
// listing omits the complete JSON value (which contains endpoint tokens).
type PromptAuditSetting struct {
	ManualWordlistEnabled  *bool                                `json:"manual_wordlist_enabled"`
	ManualWordlistAction   string                               `json:"manual_wordlist_action"`
	ScopePolicies          map[dto.PromptAuditScope]ScopePolicy `json:"scope_policies"`
	Mode                   string                               `json:"mode"`
	OutputMode             string                               `json:"output_mode"`
	BlockingLatestTurnOnly bool                                 `json:"blocking_latest_turn_only"`
	ProbeBlockEnabled      bool                                 `json:"probe_block_enabled"`
	ProbePhrases           []string                             `json:"probe_phrases"`
	// ProbeSemanticEnabled asks a TypeSafe node whether a short standalone first
	// message is a liveness probe when no configured phrase matches it.
	ProbeSemanticEnabled bool `json:"probe_semantic_enabled"`
	// ProbeSemanticThreshold is the probability at or above which that answer
	// counts as a probe.
	ProbeSemanticThreshold float64 `json:"probe_semantic_threshold"`
	// ProbeIncludeAdmins applies the probe gate to administrators as well. The
	// gate refuses liveness probes, and an administrator's own health checks are
	// the traffic it would otherwise refuse by accident, so they stay exempt
	// until an operator asks for the gate to apply to them too.
	ProbeIncludeAdmins bool `json:"probe_include_admins"`
	// ExpandBase64 decodes base64 runs in the scanned text before it is audited.
	// A guard model that only sees the encoded form cannot judge the content, so
	// leaving this off lets an encoded request through unclassified. The full
	// request keeps the original text either way.
	ExpandBase64        bool       `json:"expand_base64"`
	EnabledCategories   []string   `json:"enabled_categories"`
	ControversialBlocks []string   `json:"controversial_block_categories"`
	ReviewEnabled       bool       `json:"review_enabled"`
	ReviewPrompt        string     `json:"review_prompt"`
	AllGroups           bool       `json:"all_groups"`
	Groups              []string   `json:"groups"`
	Endpoints           []Endpoint `json:"endpoints_secret"`
	TotalTimeoutMS      int        `json:"total_timeout_ms"`
	ChunkOverlap        int        `json:"chunk_overlap"`
	ChunkConcurrency    int        `json:"chunk_concurrency"`
	CacheTTLSeconds     int        `json:"cache_ttl_seconds"`
	WorkerCount         int        `json:"worker_count"`
	MaxAttempts         int        `json:"max_attempts"`
	RetentionDays       int        `json:"retention_days"`
	GlobalConcurrency   int        `json:"global_concurrency"`
	EndpointConcurrency int        `json:"endpoint_concurrency"`
	OutputMaxBytes      int        `json:"output_max_bytes"`
	OutputMemoryBytes   int        `json:"output_memory_bytes"`
	// FullPromptMaxRunes keeps exactly what the operator persisted: a pointer so
	// an absent key stays distinguishable from an explicit 0, which means "keep
	// the whole request". Without that distinction every deployment upgrading to
	// this version would silently switch to unlimited retention.
	FullPromptMaxRunes *int   `json:"full_prompt_max_runes"`
	ConfigVersion      string `json:"-"`
}

// BlockingLatestTurnOnly defaults to the current conversation window to avoid
// repeatedly blocking on old turns. Explicit source policies still apply to
// system/developer instructions, tools and tasks. Administrators can opt into
// full conversation inspection by disabling this setting.
var promptAuditSetting = PromptAuditSetting{
	Mode:                   ModeOff,
	OutputMode:             ModeOff,
	BlockingLatestTurnOnly: true,
	ProbePhrases:           defaultProbePhrases(),
	ExpandBase64:           true,
	ProbeSemanticThreshold: DefaultProbeSemanticThreshold,
	EnabledCategories:      append([]string(nil), AllCategoryIDs...),
	ControversialBlocks:    []string{"jailbreak", "pii", "suicide_and_self_harm"},
	ManualWordlistAction:   WordlistActionBlock,
	AllGroups:              true,
	TotalTimeoutMS:         DefaultTotalTimeoutMS,
	ChunkOverlap:           DefaultChunkOverlap,
	// ChunkConcurrency defaults to 4 so a long prompt is audited in parallel
	// batches instead of one guard round trip per chunk. ChunkConcurrency only
	// affects how many chunks of one payload run at once, never which text is
	// inspected; sub2api's own parallelism is engine-vs-engine and has no
	// equivalent here, because new-api runs its wordlist locally before any
	// guard call and never pays for a model call it does not need.
	ChunkConcurrency:    DefaultChunkConcurrency,
	CacheTTLSeconds:     DefaultCacheTTLSeconds,
	WorkerCount:         DefaultWorkerCount,
	MaxAttempts:         DefaultMaxAttempts,
	RetentionDays:       DefaultRetentionDays,
	GlobalConcurrency:   DefaultGlobalConcurrency,
	EndpointConcurrency: DefaultEndpointConcurrency,
	OutputMaxBytes:      DefaultOutputMaxBytes,
	OutputMemoryBytes:   DefaultOutputMemoryBytes,
}

var promptAuditSettingSnapshot atomic.Pointer[PromptAuditSetting]

func defaultProbePhrases() []string {
	return []string{"hi", "hello", "hey", "ping", "test", "1", "你好", "在吗", "测试", "测活", "探活", "你是谁", "who are you"}
}

func init() {
	config.GlobalConfig.Register("prompt_audit", &promptAuditSetting)
	promptAuditSetting.PublishConfig()
}

func GetSetting() PromptAuditSetting {
	snapshot := promptAuditSettingSnapshot.Load()
	if snapshot == nil {
		return PromptAuditSetting{Mode: ModeOff, OutputMode: ModeOff}
	}
	return cloneSetting(*snapshot)
}

func (setting PromptAuditSetting) AppliesToGroup(group string) bool {
	return setting.AppliesToGroupForMode(group, setting.Mode)
}

// FullPromptRetentionLimit is how many characters of the whole request may be
// kept on one audit record. 0 means keep all of it, and an absent setting falls
// back to the default cap so only a deliberate operator choice removes the
// bound. The stored field is never rewritten, so what the operator set is what
// the settings screen shows.
func (setting PromptAuditSetting) FullPromptRetentionLimit() int {
	if setting.FullPromptMaxRunes == nil {
		return DefaultFullPromptMaxRunes
	}
	return *setting.FullPromptMaxRunes
}

func (setting PromptAuditSetting) AppliesToGroupForMode(group, mode string) bool {
	if mode == ModeOff {
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
			ID: endpoint.ID, Protocol: endpoint.Protocol, Name: endpoint.Name, BaseURL: endpoint.BaseURL,
			Model: endpoint.Model, TimeoutMS: endpoint.TimeoutMS,
			InputLimit: endpoint.InputLimit, Concurrency: endpoint.Concurrency,
			Enabled: endpoint.Enabled, HasToken: endpoint.Token != "", Purpose: endpoint.Purpose, Directions: append([]string(nil), endpoint.Directions...),
			BlockThreshold: endpoint.BlockThreshold, ReviewThreshold: endpoint.ReviewThreshold,
		})
	}
	return result
}

func (setting *PromptAuditSetting) ValidateConfig() error {
	if setting == nil {
		return fmt.Errorf("prompt audit setting is required")
	}
	if strings.TrimSpace(setting.Mode) == "" {
		setting.Mode = ModeOff
	}
	if strings.TrimSpace(setting.OutputMode) == "" {
		setting.OutputMode = ModeOff
	}
	if setting.OutputMaxBytes == 0 {
		setting.OutputMaxBytes = DefaultOutputMaxBytes
	}
	if setting.OutputMemoryBytes == 0 {
		setting.OutputMemoryBytes = DefaultOutputMemoryBytes
	}
	if setting.ControversialBlocks == nil {
		setting.ControversialBlocks = []string{"jailbreak", "pii", "suicide_and_self_harm"}
	}
	if setting.ProbePhrases == nil {
		setting.ProbePhrases = defaultProbePhrases()
	}
	if err := validateScopePolicies(setting.ScopePolicies); err != nil {
		return err
	}
	if len(setting.ProbePhrases) > 256 {
		return fmt.Errorf("probe phrases must not exceed 256 entries")
	}
	for _, phrase := range setting.ProbePhrases {
		if strings.TrimSpace(phrase) == "" || utf8.RuneCountInString(phrase) > 128 {
			return fmt.Errorf("probe phrases must contain between 1 and 128 characters")
		}
	}
	mode := strings.ToLower(strings.TrimSpace(setting.Mode))
	outputMode := strings.ToLower(strings.TrimSpace(setting.OutputMode))
	for label, value := range map[string]string{"input": mode, "output": outputMode} {
		if value != ModeOff && value != ModeAsyncAudit && value != ModeBlocking {
			return fmt.Errorf("prompt audit %s mode must be %q, %q, or %q", label, ModeOff, ModeAsyncAudit, ModeBlocking)
		}
	}
	if action := normalizedWordlistAction(setting.ManualWordlistAction); action != WordlistActionBlock && action != WordlistActionReview {
		return fmt.Errorf("manual wordlist action must be %q or %q", WordlistActionBlock, WordlistActionReview)
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
	if setting.ChunkConcurrency < 1 || setting.ChunkConcurrency > 16 {
		return fmt.Errorf("prompt audit chunk concurrency must be between 1 and 16")
	}
	// ChunkConcurrency above EndpointConcurrency is accepted on purpose: the
	// clamp in promptAuditBatchSize reduces the batch to the smaller budget, so
	// the configuration still works. Rejecting it instead would block every
	// settings save after an operator lowers endpoint concurrency, which is a
	// far worse failure than a batch that runs smaller than requested.
	if setting.OutputMaxBytes < 1024 || setting.OutputMaxBytes > 64*1024*1024 {
		return fmt.Errorf("prompt audit output limit must be between 1024 and 67108864 bytes")
	}
	if setting.OutputMemoryBytes < 1024 || setting.OutputMemoryBytes > setting.OutputMaxBytes {
		return fmt.Errorf("prompt audit output memory limit must be between 1024 and output_max_bytes")
	}
	// 0 is a valid choice and means the whole request is kept, so it is accepted
	// on its own. Values between 1 and the floor are rejected because a limit
	// that short drops nearly all of the text and is almost always a typo.
	if limit := setting.FullPromptRetentionLimit(); limit < 0 || limit > MaxFullPromptMaxRunes {
		return fmt.Errorf("prompt audit full prompt retention limit must be 0 or between %d and %d characters", MinFullPromptMaxRunes, MaxFullPromptMaxRunes)
	} else if limit != 0 && limit < MinFullPromptMaxRunes {
		return fmt.Errorf("prompt audit full prompt retention limit must be 0 or at least %d characters", MinFullPromptMaxRunes)
	}
	if utf8.RuneCountInString(setting.ReviewPrompt) > 20000 {
		return fmt.Errorf("prompt audit review prompt must not exceed 20000 characters")
	}
	// 0 means "not configured", so a setting persisted before this field existed
	// keeps the default instead of failing every save. Anything else out of range
	// is a real value the operator typed and is refused.
	if setting.ProbeSemanticThreshold == 0 {
		setting.ProbeSemanticThreshold = DefaultProbeSemanticThreshold
	}
	if setting.ProbeSemanticThreshold < 0 || setting.ProbeSemanticThreshold > 1 {
		return fmt.Errorf("prompt audit semantic probe threshold must be greater than 0 and at most 1")
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
	seenControversial := make(map[string]struct{}, len(setting.ControversialBlocks))
	for _, category := range setting.ControversialBlocks {
		category = strings.ToLower(strings.TrimSpace(category))
		if _, ok := categorySet[category]; !ok {
			return fmt.Errorf("unknown controversial block category %q", category)
		}
		if _, duplicate := seenControversial[category]; duplicate {
			return fmt.Errorf("duplicate controversial block category %q", category)
		}
		seenControversial[category] = struct{}{}
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
	inputClassifyCount, outputClassifyCount, reviewCount := 0, 0, 0
	minimumInputLimit := 0
	// The endpoints as they will be published. PublishConfig normalizes every
	// node, so a check that reads the raw fields would refuse a node the runtime
	// then accepts — an endpoint saved before purposes existed carries none.
	normalizedEndpoints := make([]Endpoint, 0, len(setting.Endpoints))
	for index, endpoint := range setting.Endpoints {
		// An unrecognised protocol is refused before normalization maps it to the
		// default. Coercing it would call the node the wrong way, which fails the
		// whole audit at request time instead of at save time.
		if raw := strings.ToLower(strings.TrimSpace(setting.Endpoints[index].Protocol)); raw != "" && raw != EndpointProtocolQwen3Guard && raw != EndpointProtocolTypeSafe {
			return fmt.Errorf("prompt audit endpoint %q protocol must be %q or %q", setting.Endpoints[index].ID, EndpointProtocolQwen3Guard, EndpointProtocolTypeSafe)
		}
		endpoint = normalizeEndpoint(endpoint, index)
		if _, duplicate := seenIDs[endpoint.ID]; duplicate {
			return fmt.Errorf("duplicate prompt audit endpoint id %q", endpoint.ID)
		}
		seenIDs[endpoint.ID] = struct{}{}
		if endpoint.Purpose != EndpointPurposeClassify && endpoint.Purpose != EndpointPurposeReview {
			return fmt.Errorf("prompt audit endpoint %q purpose must be %q or %q", endpoint.ID, EndpointPurposeClassify, EndpointPurposeReview)
		}
		// A TypeSafe node answers probabilities per question and never returns a
		// review verdict, so it can only classify. Letting it be a review node
		// would fail every grey-area review at request time.
		if endpoint.Protocol == EndpointProtocolTypeSafe && endpoint.Purpose != EndpointPurposeClassify {
			return fmt.Errorf("prompt audit endpoint %q uses the typesafe protocol and must have purpose %q", endpoint.ID, EndpointPurposeClassify)
		}
		if endpoint.Protocol == EndpointProtocolTypeSafe {
			if endpoint.ReviewThreshold <= 0 || endpoint.BlockThreshold < endpoint.ReviewThreshold || endpoint.BlockThreshold > 1 {
				return fmt.Errorf("prompt audit endpoint %q typesafe thresholds must satisfy 0 < review <= block <= 1", endpoint.ID)
			}
			if endpoint.InputLimit > TypeSafeMaxInputLimit {
				return fmt.Errorf("prompt audit endpoint %q typesafe input limit must not exceed %d characters", endpoint.ID, TypeSafeMaxInputLimit)
			}
		}
		normalizedEndpoints = append(normalizedEndpoints, endpoint)
		if endpoint.Purpose == EndpointPurposeClassify {
			if len(endpoint.Directions) == 0 {
				return fmt.Errorf("prompt audit endpoint %q requires at least one direction", endpoint.ID)
			}
			seenDirections := map[string]bool{}
			for _, direction := range endpoint.Directions {
				if direction != "input" && direction != "output" {
					return fmt.Errorf("prompt audit endpoint %q has invalid direction %q", endpoint.ID, direction)
				}
				if seenDirections[direction] {
					return fmt.Errorf("prompt audit endpoint %q has duplicate direction %q", endpoint.ID, direction)
				}
				seenDirections[direction] = true
			}
		}
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
			if endpoint.Purpose == EndpointPurposeReview {
				reviewCount++
			} else {
				for _, direction := range endpoint.Directions {
					if direction == "input" {
						inputClassifyCount++
					} else if direction == "output" {
						outputClassifyCount++
					}
				}
			}
			if endpoint.Purpose == EndpointPurposeClassify && (minimumInputLimit == 0 || endpoint.InputLimit < minimumInputLimit) {
				minimumInputLimit = endpoint.InputLimit
			}
		}
	}
	if mode != ModeOff && inputClassifyCount == 0 {
		return fmt.Errorf("prompt audit requires at least one enabled endpoint for input classification")
	}
	if outputMode != ModeOff && outputClassifyCount == 0 {
		return fmt.Errorf("prompt audit requires at least one enabled endpoint for output classification")
	}
	if setting.ReviewEnabled && reviewCount == 0 {
		return fmt.Errorf("prompt audit review requires at least one enabled review endpoint")
	}
	if setting.ProbeSemanticEnabled {
		// Detection draws a probe verdict from a model probability, which only a
		// TypeSafe node provides. Refusing the save is better than accepting a
		// switch that silently never fires.
		hasTypeSafeClassifier := false
		for _, endpoint := range normalizedEndpoints {
			if endpoint.Enabled && endpoint.Protocol == EndpointProtocolTypeSafe && endpoint.Purpose == EndpointPurposeClassify {
				hasTypeSafeClassifier = true
				break
			}
		}
		if !hasTypeSafeClassifier {
			return fmt.Errorf("prompt audit semantic probe detection requires at least one enabled typesafe classification endpoint")
		}
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
	if snapshot.Mode == "" {
		snapshot.Mode = ModeOff
	}
	snapshot.OutputMode = strings.ToLower(strings.TrimSpace(snapshot.OutputMode))
	if snapshot.OutputMode == "" {
		snapshot.OutputMode = ModeOff
	}
	snapshot.ManualWordlistAction = normalizedWordlistAction(snapshot.ManualWordlistAction)
	if snapshot.OutputMaxBytes == 0 {
		snapshot.OutputMaxBytes = DefaultOutputMaxBytes
	}
	if snapshot.OutputMemoryBytes == 0 {
		snapshot.OutputMemoryBytes = DefaultOutputMemoryBytes
	}
	if snapshot.ControversialBlocks == nil {
		snapshot.ControversialBlocks = []string{"jailbreak", "pii", "suicide_and_self_harm"}
	}
	if snapshot.ProbePhrases == nil {
		snapshot.ProbePhrases = defaultProbePhrases()
	}
	if snapshot.ProbeSemanticThreshold == 0 {
		snapshot.ProbeSemanticThreshold = DefaultProbeSemanticThreshold
	}
	for index := range snapshot.EnabledCategories {
		snapshot.EnabledCategories[index] = strings.ToLower(strings.TrimSpace(snapshot.EnabledCategories[index]))
	}
	for index := range snapshot.ControversialBlocks {
		snapshot.ControversialBlocks[index] = strings.ToLower(strings.TrimSpace(snapshot.ControversialBlocks[index]))
	}
	snapshot.ReviewPrompt = strings.TrimSpace(snapshot.ReviewPrompt)
	if snapshot.ChunkConcurrency == 0 {
		snapshot.ChunkConcurrency = DefaultChunkConcurrency
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
	endpoint.Protocol = NormalizeEndpointProtocol(endpoint.Protocol)
	endpoint.Purpose = strings.ToLower(strings.TrimSpace(endpoint.Purpose))
	if endpoint.Purpose == "" {
		endpoint.Purpose = EndpointPurposeClassify
	}
	if endpoint.Purpose == EndpointPurposeClassify && endpoint.Directions == nil {
		endpoint.Directions = []string{"input", "output"}
	}
	for index := range endpoint.Directions {
		endpoint.Directions[index] = strings.ToLower(strings.TrimSpace(endpoint.Directions[index]))
	}
	sort.Strings(endpoint.Directions)
	if endpoint.Model == "" {
		// A TypeSafe node has its own default: DefaultModel is a Qwen3Guard
		// repository id and would be rejected by the TypeSafe API.
		if endpoint.Protocol == EndpointProtocolTypeSafe {
			endpoint.Model = DefaultTypeSafeModel
		} else {
			endpoint.Model = DefaultModel
		}
	}
	if endpoint.Protocol == EndpointProtocolTypeSafe {
		if endpoint.BlockThreshold == 0 {
			endpoint.BlockThreshold = DefaultBlockThreshold
		}
		if endpoint.ReviewThreshold == 0 {
			endpoint.ReviewThreshold = DefaultReviewThreshold
		}
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

// NormalizeEndpointProtocol maps a stored protocol to one of the two supported
// values, treating an empty value as the original Qwen3Guard node. It is
// exported because async audit workers rebuild a snapshot from an older record
// and must resolve the same protocol the live configuration uses.
func NormalizeEndpointProtocol(protocol string) string {
	if strings.ToLower(strings.TrimSpace(protocol)) == EndpointProtocolTypeSafe {
		return EndpointProtocolTypeSafe
	}
	return EndpointProtocolQwen3Guard
}

// IsTypeSafeEndpoint reports whether the node is called over the TypeSafe
// /v1/systemone API rather than as an OpenAI-compatible chat completion.
func (endpoint Endpoint) IsTypeSafeEndpoint() bool {
	return NormalizeEndpointProtocol(endpoint.Protocol) == EndpointProtocolTypeSafe
}

func normalizedWordlistAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return WordlistActionBlock
	}
	return action
}

func NormalizeWordlistAction(action string) string {
	return normalizedWordlistAction(action)
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
	if setting.ManualWordlistEnabled != nil {
		value := *setting.ManualWordlistEnabled
		setting.ManualWordlistEnabled = &value
	}
	if setting.ScopePolicies != nil {
		setting.ScopePolicies = setting.EffectiveScopePolicies()
	}
	setting.EnabledCategories = append([]string(nil), setting.EnabledCategories...)
	setting.ControversialBlocks = append([]string(nil), setting.ControversialBlocks...)
	setting.Groups = append([]string(nil), setting.Groups...)
	setting.ProbePhrases = slices.Clone(setting.ProbePhrases)
	setting.Endpoints = append([]Endpoint(nil), setting.Endpoints...)
	for index := range setting.Endpoints {
		setting.Endpoints[index].Directions = append([]string(nil), setting.Endpoints[index].Directions...)
	}
	return setting
}

func settingFingerprint(setting PromptAuditSetting) string {
	var builder strings.Builder
	builder.WriteString(strconv.FormatBool(setting.ManualWordlistActive()) + "|")
	for _, scope := range dto.PromptAuditScopes() {
		policy := setting.PolicyFor(scope)
		ids := append([]string(nil), policy.LibraryIDs...)
		sort.Strings(ids)
		builder.WriteString(string(scope) + ":" + strconv.FormatBool(policy.ModelAudit) + ":" + strings.Join(ids, ",") + "|")
	}
	builder.WriteString(setting.Mode)
	builder.WriteByte('|')
	builder.WriteString(setting.OutputMode)
	builder.WriteByte('|')
	builder.WriteString(setting.ManualWordlistAction)
	builder.WriteByte('|')
	categories := append([]string(nil), setting.EnabledCategories...)
	sort.Strings(categories)
	builder.WriteString(strings.Join(categories, ","))
	builder.WriteByte('|')
	controversial := append([]string(nil), setting.ControversialBlocks...)
	sort.Strings(controversial)
	builder.WriteString(strings.Join(controversial, ","))
	builder.WriteByte('|')
	builder.WriteString(strconv.FormatBool(setting.ReviewEnabled))
	builder.WriteByte('|')
	builder.WriteString(setting.ReviewPrompt)
	builder.WriteByte('|')
	builder.WriteString(strconv.FormatBool(setting.AllGroups))
	builder.WriteByte('|')
	// Include the conversation window and execution limits so policy changes
	// cannot reuse verdicts produced under a different configuration.
	builder.WriteString(strconv.FormatBool(setting.BlockingLatestTurnOnly))
	builder.WriteByte('|')
	builder.WriteString(strconv.FormatBool(setting.ProbeBlockEnabled))
	builder.WriteByte('|')
	builder.WriteString(strings.Join(setting.ProbePhrases, ","))
	builder.WriteByte('|')
	// Semantic probe detection and base64 expansion change which text reaches a
	// verdict, and the semantic probe itself is a cached model call, so both the
	// switch and its threshold belong in the cache key.
	builder.WriteString(strconv.FormatBool(setting.ProbeSemanticEnabled))
	builder.WriteByte('|')
	builder.WriteString(strconv.FormatFloat(setting.ProbeSemanticThreshold, 'f', -1, 64))
	builder.WriteByte('|')
	builder.WriteString(strconv.FormatBool(setting.ProbeIncludeAdmins))
	builder.WriteByte('|')
	builder.WriteString(strconv.FormatBool(setting.ExpandBase64))
	builder.WriteByte('|')
	groups := append([]string(nil), setting.Groups...)
	sort.Strings(groups)
	builder.WriteString(strings.Join(groups, ","))
	for _, value := range []int{setting.TotalTimeoutMS, setting.ChunkOverlap, setting.ChunkConcurrency, setting.CacheTTLSeconds, setting.WorkerCount, setting.MaxAttempts, setting.RetentionDays, setting.GlobalConcurrency, setting.EndpointConcurrency, setting.OutputMaxBytes, setting.OutputMemoryBytes} {
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
		builder.WriteByte('|')
		builder.WriteString(endpoint.Purpose)
		builder.WriteByte('|')
		builder.WriteString(endpoint.Protocol)
		builder.WriteByte('|')
		builder.WriteString(strconv.FormatFloat(endpoint.BlockThreshold, 'f', -1, 64))
		builder.WriteByte('|')
		builder.WriteString(strconv.FormatFloat(endpoint.ReviewThreshold, 'f', -1, 64))
		builder.WriteByte('|')
		builder.WriteString(strings.Join(endpoint.Directions, ","))
		tokenDigest := sha256.Sum256([]byte(endpoint.Token))
		builder.WriteByte('|')
		builder.WriteString(hex.EncodeToString(tokenDigest[:]))
	}
	digest := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(digest[:])
}
