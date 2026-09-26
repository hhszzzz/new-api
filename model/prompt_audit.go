package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"

	"gorm.io/gorm"
)

type PromptAuditStatus string

const (
	PromptAuditStatusQueued     PromptAuditStatus = "queued"
	PromptAuditStatusProcessing PromptAuditStatus = "processing"
	PromptAuditStatusRetry      PromptAuditStatus = "retry"
	PromptAuditStatusDone       PromptAuditStatus = "done"
	PromptAuditStatusFailed     PromptAuditStatus = "failed"
)

var (
	ErrPromptAuditNotFound       = errors.New("prompt audit not found")
	ErrPromptAuditActive         = errors.New("active prompt audit cannot be deleted")
	ErrPromptAuditNotRetryable   = errors.New("prompt audit is not retryable")
	ErrPromptAuditNotReviewable  = errors.New("active prompt audit cannot be reviewed")
	ErrPromptAuditPayloadMissing = errors.New("full prompt is unavailable or truncated and cannot be retried safely")
	ErrPromptAuditDeleteChanged  = errors.New("prompt audit deletion candidates changed; preview again")
)

// PromptAudit is both the durable async work item and the final audit event.
// ScanPayload stays complete while queued and is replaced by the capped content
// snapshot on completion. Retained content is restricted to authorized review.
//
// Ip, UserAgent, Method, RequestPath, Origin and Referer capture the client once,
// when the row is created; the async worker must never rewrite them. Their widths
// mirror AuditLog and are enforced by clampPromptAuditColumn, because MySQL and
// PostgreSQL reject an over-long or non-UTF-8 value and the failed insert would
// drop the whole audit row. TokenName, ModelName and GenerationID are clamped in
// the same pass for the same reason: a long administrator-chosen token name, a
// client-supplied model name or an upstream response id would drop every row
// carrying it.
type PromptAudit struct {
	InspectionType       string            `json:"inspection_type" gorm:"type:varchar(16);index"`
	WordlistID           string            `json:"wordlist_id" gorm:"type:varchar(32);index"`
	WordlistName         string            `json:"wordlist_name" gorm:"type:varchar(128)"`
	WordlistVersion      string            `json:"wordlist_version" gorm:"type:varchar(64)"`
	MatchedScope         string            `json:"matched_scope" gorm:"type:varchar(32)"`
	InspectedScopes      string            `json:"-" gorm:"type:text"`
	ID                   int64             `json:"id" gorm:"primaryKey"`
	RequestID            string            `json:"request_id" gorm:"type:varchar(64);index"`
	UserID               int               `json:"user_id" gorm:"index"`
	TokenID              int               `json:"token_id" gorm:"index"`
	TokenName            string            `json:"token_name" gorm:"type:varchar(255)"`
	Username             string            `json:"username" gorm:"type:varchar(64);index"`
	GroupName            string            `json:"group" gorm:"type:varchar(64);index"`
	Protocol             string            `json:"protocol" gorm:"type:varchar(64);index"`
	ModelName            string            `json:"model" gorm:"type:varchar(255);index"`
	Stage                string            `json:"stage" gorm:"type:varchar(32)"`
	Direction            string            `json:"direction" gorm:"type:varchar(16);index"`
	GenerationID         string            `json:"generation_id" gorm:"type:varchar(128);index"`
	DeliveryStatus       string            `json:"delivery_status" gorm:"type:varchar(32);index"`
	CoverageComplete     bool              `json:"coverage_complete"`
	ConfigVersion        string            `json:"config_version" gorm:"type:varchar(64);index"`
	ExecutionMode        string            `json:"execution_mode" gorm:"type:varchar(32);index"`
	Status               PromptAuditStatus `json:"status" gorm:"type:varchar(32);index"`
	PromptHash           string            `json:"prompt_hash" gorm:"type:varchar(64);index"`
	GroupKey             string            `json:"group_key" gorm:"type:varchar(64);index"`
	SessionKey           string            `json:"session_key" gorm:"type:varchar(64);index"`
	RequestKind          string            `json:"request_kind" gorm:"type:varchar(32);index"`
	PromptLength         int               `json:"prompt_length"`
	SegmentCount         int               `json:"segment_count"`
	ChunkCount           int               `json:"chunk_count"`
	FullPrompt           []byte            `json:"-"`
	FullPromptTruncated  bool              `json:"full_prompt_truncated"`
	RedactedPreview      string            `json:"redacted_preview" gorm:"type:varchar(512)"`
	ScanPayload          []byte            `json:"-"`
	ScanPayloadTruncated bool              `json:"scan_payload_truncated"`
	ContentSnapshot      []byte            `json:"-"`
	PolicyCategories     string            `json:"-" gorm:"type:text"`
	PolicySnapshot       string            `json:"-" gorm:"type:text"`
	Safety               string            `json:"safety" gorm:"type:varchar(32);index"`
	Refusal              string            `json:"refusal" gorm:"type:varchar(16)"`
	Decision             string            `json:"decision" gorm:"type:varchar(32);index"`
	Action               string            `json:"action" gorm:"type:varchar(32);index"`
	WouldAction          string            `json:"would_action" gorm:"type:varchar(32);index"`
	Categories           string            `json:"-" gorm:"type:text"`
	UnknownCategories    string            `json:"-" gorm:"type:text"`
	EndpointID           string            `json:"endpoint_id" gorm:"type:varchar(128);index"`
	EndpointModel        string            `json:"endpoint_model" gorm:"type:varchar(255)"`
	ReviewStatus         string            `json:"review_status" gorm:"type:varchar(32);index"`
	ReviewDecision       string            `json:"review_decision" gorm:"type:varchar(16)"`
	ReviewCodes          string            `json:"-" gorm:"type:text"`
	// Scores holds the raw model probabilities a verdict was derived from, keyed
	// by category. It is stored as JSON and is the record an operator retunes
	// node thresholds against.
	Scores             string `json:"-" gorm:"type:text"`
	ReviewReason       string `json:"review_reason" gorm:"type:varchar(512)"`
	ReviewerEndpointID string `json:"reviewer_endpoint_id" gorm:"type:varchar(128)"`
	HumanReview        string `json:"human_review" gorm:"type:varchar(32);index"`
	HumanReviewReason  string `json:"human_review_reason" gorm:"type:varchar(512)"`
	ReviewedBy         int    `json:"reviewed_by" gorm:"index"`
	ReviewerName       string `json:"reviewer_name" gorm:"type:varchar(255)"`
	ReviewedAt         int64  `json:"reviewed_at" gorm:"index"`
	LatencyMS          int64  `json:"latency_ms"`
	Attempts           int    `json:"attempts"`
	MaxAttempts        int    `json:"max_attempts"`
	NextAttemptAt      int64  `json:"next_attempt_at" gorm:"index"`
	LeaseOwner         string `json:"-" gorm:"type:varchar(128);index"`
	LeaseUntil         int64  `json:"-" gorm:"index"`
	ErrorCode          string `json:"error_code" gorm:"type:varchar(64);index"`
	Ip                 string `json:"ip" gorm:"type:varchar(64)"`
	UserAgent          string `json:"user_agent" gorm:"type:varchar(512)"`
	Method             string `json:"method" gorm:"type:varchar(16)"`
	RequestPath        string `json:"request_path" gorm:"type:varchar(255)"`
	Origin             string `json:"origin" gorm:"type:varchar(255)"`
	Referer            string `json:"referer" gorm:"type:varchar(255)"`
	CreatedAt          int64  `json:"created_at" gorm:"index"`
	UpdatedAt          int64  `json:"updated_at" gorm:"index"`
	CompletedAt        int64  `json:"completed_at" gorm:"index"`
}

type PromptAuditResponse struct {
	InspectionType       string             `json:"inspection_type"`
	WordlistID           string             `json:"wordlist_id"`
	WordlistName         string             `json:"wordlist_name"`
	WordlistVersion      string             `json:"wordlist_version"`
	MatchedScope         string             `json:"matched_scope"`
	InspectedScopes      []string           `json:"inspected_scopes"`
	ID                   int64              `json:"id"`
	RequestID            string             `json:"request_id"`
	UserID               int                `json:"user_id"`
	TokenID              int                `json:"token_id"`
	TokenName            string             `json:"token_name"`
	Username             string             `json:"username"`
	GroupName            string             `json:"group"`
	Protocol             string             `json:"protocol"`
	ModelName            string             `json:"model"`
	Stage                string             `json:"stage"`
	Direction            string             `json:"direction"`
	GenerationID         string             `json:"generation_id"`
	DeliveryStatus       string             `json:"delivery_status"`
	CoverageComplete     bool               `json:"coverage_complete"`
	ConfigVersion        string             `json:"config_version"`
	ExecutionMode        string             `json:"execution_mode"`
	Status               PromptAuditStatus  `json:"status"`
	PromptHash           string             `json:"prompt_hash"`
	GroupKey             string             `json:"group_key"`
	SessionKey           string             `json:"session_key"`
	RequestKind          string             `json:"request_kind"`
	PromptLength         int                `json:"prompt_length"`
	SegmentCount         int                `json:"segment_count"`
	ChunkCount           int                `json:"chunk_count"`
	FullPrompt           *string            `json:"full_prompt,omitempty"`
	FullPromptAvailable  bool               `json:"full_prompt_available"`
	FullPromptTruncated  bool               `json:"full_prompt_truncated"`
	ScanPayload          *string            `json:"scan_payload,omitempty"`
	ScanPayloadTruncated bool               `json:"scan_payload_truncated"`
	RedactedPreview      string             `json:"redacted_preview"`
	Safety               string             `json:"safety"`
	Refusal              string             `json:"refusal"`
	Decision             string             `json:"decision"`
	Action               string             `json:"action"`
	WouldAction          string             `json:"would_action"`
	Categories           []string           `json:"categories"`
	UnknownCategories    []string           `json:"unknown_categories"`
	EndpointID           string             `json:"endpoint_id"`
	EndpointModel        string             `json:"endpoint_model"`
	ReviewStatus         string             `json:"review_status"`
	ReviewDecision       string             `json:"review_decision"`
	ReviewCodes          []string           `json:"review_codes"`
	Scores               map[string]float64 `json:"scores,omitempty"`
	ReviewReason         string             `json:"review_reason"`
	ReviewerEndpointID   string             `json:"reviewer_endpoint_id"`
	HumanReview          string             `json:"human_review"`
	HumanReviewReason    string             `json:"human_review_reason"`
	ReviewedBy           int                `json:"reviewed_by"`
	ReviewerName         string             `json:"reviewer_name"`
	ReviewedAt           int64              `json:"reviewed_at"`
	LatencyMS            int64              `json:"latency_ms"`
	Attempts             int                `json:"attempts"`
	MaxAttempts          int                `json:"max_attempts"`
	NextAttemptAt        int64              `json:"next_attempt_at"`
	ErrorCode            string             `json:"error_code"`
	Ip                   string             `json:"ip"`
	UserAgent            string             `json:"user_agent"`
	Method               string             `json:"method"`
	RequestPath          string             `json:"request_path"`
	Origin               string             `json:"origin"`
	Referer              string             `json:"referer"`
	CreatedAt            int64              `json:"created_at"`
	UpdatedAt            int64              `json:"updated_at"`
	CompletedAt          int64              `json:"completed_at"`
	Repeat               *PromptAuditRepeat `json:"repeat,omitempty"`
}

// PromptAuditRepeat describes the requests a collapsed row stands for. One
// audited text is resent by every step of an agent run, so the listing can merge
// those rows; the merged row must still report what the whole group decided,
// which is why the worst decision and the two enforcement counts travel with it.
//
// WorstDecision is the most severe decision any request of the group recorded,
// ranked block, unavailable, flag, then pending (an empty decision: a request
// that has not been decided yet), then pass. It is read from the decision
// column itself: an action cannot stand in for it, because async observation
// stores a block decision with a mark action and a queued request with an allow
// action. Blocks and Unavailable count the requests actually refused or left
// without a verdict (their action), so an async-observed group can report a
// block decision with no blocked request.
type PromptAuditRepeat struct {
	Count         int64  `json:"count"`
	FirstAt       int64  `json:"first_at"`
	LastAt        int64  `json:"last_at"`
	WorstDecision string `json:"worst_decision"`
	Blocks        int64  `json:"blocks"`
	Unavailable   int64  `json:"unavailable"`
}

// PromptAuditRepeatRow pairs a collapsed group's first request with its summary.
// The first request is the representative: it is the one that carried the audited
// text before later steps appended their context, so it is the row an operator
// expects to read.
type PromptAuditRepeatRow struct {
	Audit  *PromptAudit
	Repeat PromptAuditRepeat
}

// PromptAuditFilter mirrors the fields an operator can narrow a listing by. It
// keeps no user id, node id or prompt hash filter: the records screen does not
// offer them, and a request that still sends one is refused by the controller
// rather than run with the filter silently dropped.
//
// GroupIDs names collapsed rows by their representative requests. Each stands
// for every request its group holds within the other fields, so a merged row is
// deleted as the whole group it shows rather than as its first request.
type PromptAuditFilter struct {
	IDs       []int64
	GroupIDs  []int64
	Status    string
	Decision  string
	Category  string
	Username  string
	Group     string
	Protocol  string
	Model     string
	RequestID string
	Direction string
	Detector  string
	StartTime int64
	EndTime   int64
	MaxID     int64

	// groups holds the keys resolvePromptAuditGroups found for GroupIDs.
	groups []promptAuditGroupKey
}

type PromptAuditStats struct {
	Total      int64            `json:"total"`
	Statuses   map[string]int64 `json:"statuses"`
	Decisions  map[string]int64 `json:"decisions"`
	Categories map[string]int64 `json:"categories"`
	Unknown    int64            `json:"unknown_categories"`
}

type PromptAuditCompletion struct {
	Safety             string
	Decision           string
	Action             string
	WouldAction        string
	Categories         []string
	UnknownCategories  []string
	EndpointID         string
	EndpointModel      string
	ChunkCount         int
	LatencyMS          int64
	ErrorCode          string
	Refusal            string
	ReviewStatus       string
	ReviewDecision     string
	ReviewCodes        []string
	Scores             map[string]float64
	ReviewReason       string
	ReviewerEndpointID string
}

func (audit *PromptAudit) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if audit.CreatedAt == 0 {
		audit.CreatedAt = now
	}
	if audit.UpdatedAt == 0 {
		audit.UpdatedAt = now
	}
	// Clamp so the in-memory row matches the stored row, and so no single
	// over-long or non-UTF-8 value can cost the whole audit record. The identity
	// and model names are included because they are just as unbounded: a token
	// name comes from an administrator, a model name from the client's request
	// body and a generation id from the upstream response, and an over-long one
	// would otherwise drop every audit row that carries it.
	audit.TokenName = clampPromptAuditColumn(audit.TokenName, 255)
	audit.Username = clampPromptAuditColumn(audit.Username, 64)
	audit.ModelName = clampPromptAuditColumn(audit.ModelName, 255)
	audit.GenerationID = clampPromptAuditColumn(audit.GenerationID, 128)
	audit.EndpointModel = clampPromptAuditColumn(audit.EndpointModel, 255)
	audit.Ip = clampPromptAuditColumn(audit.Ip, 64)
	audit.UserAgent = clampPromptAuditColumn(audit.UserAgent, 512)
	audit.Method = clampPromptAuditColumn(audit.Method, 16)
	audit.RequestPath = clampPromptAuditColumn(audit.RequestPath, 255)
	audit.Origin = clampPromptAuditColumn(audit.Origin, 255)
	audit.Referer = clampPromptAuditColumn(audit.Referer, 255)
	return nil
}

func (audit *PromptAudit) ToResponse(includeFullPrompt bool) PromptAuditResponse {
	response := PromptAuditResponse{
		InspectionType: audit.InspectionType, WordlistID: audit.WordlistID, WordlistName: audit.WordlistName,
		WordlistVersion: audit.WordlistVersion, MatchedScope: audit.MatchedScope, InspectedScopes: decodePromptAuditStrings(audit.InspectedScopes),
		ID: audit.ID, RequestID: audit.RequestID, UserID: audit.UserID,
		TokenID: audit.TokenID, TokenName: audit.TokenName, GroupName: audit.GroupName, Username: audit.Username,
		Protocol: audit.Protocol, ModelName: audit.ModelName, Stage: audit.Stage,
		Direction: audit.Direction, GenerationID: audit.GenerationID, DeliveryStatus: audit.DeliveryStatus,
		CoverageComplete: audit.CoverageComplete,
		ConfigVersion:    audit.ConfigVersion, ExecutionMode: audit.ExecutionMode,
		Status: audit.Status, PromptHash: audit.PromptHash, GroupKey: audit.GroupKey, SessionKey: audit.SessionKey,
		RequestKind: audit.RequestKind, PromptLength: audit.PromptLength,
		SegmentCount: audit.SegmentCount, ChunkCount: audit.ChunkCount,
		FullPromptAvailable: len(audit.FullPrompt) > 0,
		FullPromptTruncated: audit.FullPromptTruncated, ScanPayloadTruncated: audit.ScanPayloadTruncated, RedactedPreview: audit.RedactedPreview,
		Safety: audit.Safety, Refusal: audit.Refusal, Decision: audit.Decision, Action: audit.Action, WouldAction: audit.WouldAction,
		Categories:        decodePromptAuditStrings(audit.Categories),
		UnknownCategories: decodePromptAuditStrings(audit.UnknownCategories),
		EndpointID:        audit.EndpointID, EndpointModel: audit.EndpointModel,
		ReviewStatus: audit.ReviewStatus, ReviewDecision: audit.ReviewDecision,
		ReviewCodes: decodePromptAuditStrings(audit.ReviewCodes), Scores: decodePromptAuditScores(audit.Scores), ReviewReason: audit.ReviewReason,
		ReviewerEndpointID: audit.ReviewerEndpointID, HumanReview: audit.HumanReview,
		HumanReviewReason: audit.HumanReviewReason, ReviewedBy: audit.ReviewedBy,
		ReviewerName: audit.ReviewerName, ReviewedAt: audit.ReviewedAt,
		LatencyMS: audit.LatencyMS, Attempts: audit.Attempts,
		MaxAttempts: audit.MaxAttempts, NextAttemptAt: audit.NextAttemptAt,
		ErrorCode: audit.ErrorCode, CreatedAt: audit.CreatedAt, UpdatedAt: audit.UpdatedAt,
		CompletedAt: audit.CompletedAt,
		Ip:          audit.Ip, UserAgent: audit.UserAgent, Method: audit.Method,
		RequestPath: audit.RequestPath, Origin: audit.Origin, Referer: audit.Referer,
	}
	if response.InspectionType == "" {
		response.InspectionType = "model"
	}
	if includeFullPrompt && len(audit.FullPrompt) > 0 {
		value := string(audit.FullPrompt)
		response.FullPrompt = &value
	}
	if includeFullPrompt && len(audit.ScanPayload) > 0 {
		payload, truncated := RetainPromptAuditPayload(audit.ScanPayload)
		response.ScanPayloadTruncated = response.ScanPayloadTruncated || truncated
		value := string(payload)
		response.ScanPayload = &value
	}
	return response
}

func CreatePromptAudit(audit *PromptAudit) error {
	if audit == nil {
		return errors.New("prompt audit is required")
	}
	return DB.Create(audit).Error
}

func RetainPromptAuditPayload(payload []byte) ([]byte, bool) {
	const limit = 64 * 1024
	if len(payload) <= limit {
		return append([]byte(nil), payload...), false
	}
	var envelope map[string]json.RawMessage
	if common.Unmarshal(payload, &envelope) != nil {
		text := strings.ToValidUTF8(string(payload[:limit]), "\uFFFD")
		if len(text) > limit {
			text = text[:limit]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
		}
		return []byte(text), true
	}
	var segments []map[string]json.RawMessage
	_ = common.Unmarshal(envelope["segments"], &segments)
	var output string
	_ = common.Unmarshal(envelope["output"], &output)
	delete(envelope, "output")
	envelope["segments"] = json.RawMessage("[]")
	base, _ := common.Marshal(envelope)
	budget := limit - len(base) - 32
	retained := make([]json.RawMessage, 0)
	for _, segment := range segments {
		var text string
		_ = common.Unmarshal(segment["text"], &text)
		segment["text"] = json.RawMessage(`""`)
		empty, _ := common.Marshal(segment)
		available := budget - len(empty) - 1
		if available < 0 {
			break
		}
		encoded := promptAuditJSONTextPrefix(text, available+2)
		segment["text"] = encoded
		part, _ := common.Marshal(segment)
		retained = append(retained, part)
		budget -= len(part) + 1
		if budget <= 0 {
			break
		}
	}
	envelope["segments"], _ = common.Marshal(retained)
	if output != "" && budget > 16 {
		envelope["output"] = promptAuditJSONTextPrefix(output, budget-12)
	}
	data, _ := common.Marshal(envelope)
	if len(data) > limit {
		return []byte(`{"version":1,"segments":[],"coverage_complete":false}`), true
	}
	return data, true
}

func promptAuditJSONTextPrefix(text string, limit int) json.RawMessage {
	encoded, _ := common.Marshal(text)
	if len(encoded) <= limit {
		return encoded
	}
	runes := []rune(text)
	low, high := 0, min(len(runes), limit)
	for low < high {
		mid := (low + high + 1) / 2
		data, _ := common.Marshal(string(runes[:mid]))
		if len(data) <= limit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	encoded, _ = common.Marshal(string(runes[:low]))
	return encoded
}

func GetPromptAudit(id int64) (*PromptAudit, error) {
	var audit PromptAudit
	if err := DB.Where("id = ?", id).First(&audit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPromptAuditNotFound
		}
		return nil, err
	}
	return &audit, nil
}

func ListPromptAudits(filter PromptAuditFilter, page, pageSize int) ([]*PromptAudit, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	var total int64
	if err := applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var audits []*PromptAudit
	if err := applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).
		Order("id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&audits).Error; err != nil {
		return nil, 0, err
	}
	return audits, total, nil
}

// promptAuditRepeatEmptyHashKey keeps rows whose prompt hash was never recorded
// in groups of their own instead of merging every hashless row into one. The
// CASE form is the portable one: concatenating a nullable column differs between
// MySQL, PostgreSQL and SQLite.
const promptAuditEffectiveGroupKey = "CASE WHEN group_key IS NOT NULL AND group_key <> '' THEN group_key ELSE COALESCE(prompt_hash, '') END"
const promptAuditRepeatEmptyHashKey = "CASE WHEN (" + promptAuditEffectiveGroupKey + ") = '' THEN id ELSE 0 END"

// promptAuditRepeatRowLimit bounds one group expansion. The largest group seen in
// production held 61 requests.
const promptAuditRepeatRowLimit = 200

// promptAuditWorstDecisionRank ranks the decisions of a group so the merged row
// can report the most severe one. A request that has not been decided yet ranks
// above a pass: until it is decided, the group has not passed. Plain CASE
// aggregates are the portable form; aggregate FILTER clauses are what diverge
// across the three supported engines.
const promptAuditWorstDecisionRank = "MAX(CASE decision WHEN 'block' THEN 4 WHEN 'unavailable' THEN 3 WHEN 'flag' THEN 2 WHEN 'pass' THEN 0 ELSE 1 END)"

// promptAuditDecisionsByRank reads promptAuditWorstDecisionRank back into the
// decision it stands for; rank 1 is a request still pending.
var promptAuditDecisionsByRank = [...]string{0: "pass", 1: "", 2: "flag", 3: "unavailable", 4: "block"}

// promptAuditListingOmittedColumns are the blobs a listing never reads:
// ToResponse only needs to know whether a full prompt was retained.
var promptAuditListingOmittedColumns = []string{"scan_payload", "content_snapshot", "policy_snapshot", "policy_categories"}

func promptAuditRepeatGroupColumns() string {
	return "user_id, " + promptAuditEffectiveGroupKey + ", " + promptAuditRepeatEmptyHashKey
}

// ListPromptAuditRepeats groups requests by user and question across models,
// falling back to the prompt hash for legacy rows. It returns the collapsed rows
// and group count within the filter's range. Groups are ordered by their newest
// request; each row represents the group's first request.
func ListPromptAuditRepeats(filter PromptAuditFilter, page, pageSize int) ([]PromptAuditRepeatRow, int64, error) {
	page = max(page, 1)
	if pageSize < 1 {
		pageSize = 20
	}
	pageSize = min(pageSize, 200)
	if err := resolvePromptAuditGroups(&filter); err != nil {
		return nil, 0, err
	}

	groupColumns := promptAuditRepeatGroupColumns()
	// Counting groups through a derived table keeps one query per dialect: GORM's
	// Count over a grouped query would count the rows inside the groups instead.
	var total int64
	groupQuery := applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).
		Select("MIN(id) AS id").
		Group(groupColumns)
	if err := DB.Table("(?) AS prompt_audit_repeat_groups", groupQuery).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []PromptAuditRepeatRow{}, 0, nil
	}

	// One grouped scan returns each group's span, its worst decision and its
	// enforcement counts together.
	type repeatRow struct {
		ID                int64
		RepeatCount       int64
		FirstAt           int64
		LastAt            int64
		WorstDecisionRank int
		Blocks            int64
		Unavailable       int64
	}
	rows := make([]repeatRow, 0, pageSize)
	if err := applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).
		Select("MIN(id) AS id, COUNT(*) AS repeat_count, MIN(created_at) AS first_at, MAX(created_at) AS last_at, " +
			promptAuditWorstDecisionRank + " AS worst_decision_rank, " +
			"SUM(CASE WHEN action = 'block' THEN 1 ELSE 0 END) AS blocks, " +
			"SUM(CASE WHEN action = 'unavailable' THEN 1 ELSE 0 END) AS unavailable").
		Group(groupColumns).
		Order("MAX(id) DESC").
		Limit(pageSize).Offset((page - 1) * pageSize).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return []PromptAuditRepeatRow{}, total, nil
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var audits []*PromptAudit
	if err := DB.Omit(promptAuditListingOmittedColumns...).Where("id IN ?", ids).Find(&audits).Error; err != nil {
		return nil, 0, err
	}
	byID := make(map[int64]*PromptAudit, len(audits))
	for _, audit := range audits {
		byID[audit.ID] = audit
	}
	collapsed := make([]PromptAuditRepeatRow, 0, len(rows))
	for _, row := range rows {
		audit, ok := byID[row.ID]
		if !ok {
			continue
		}
		worstDecision := ""
		if row.WorstDecisionRank >= 0 && row.WorstDecisionRank < len(promptAuditDecisionsByRank) {
			worstDecision = promptAuditDecisionsByRank[row.WorstDecisionRank]
		}
		collapsed = append(collapsed, PromptAuditRepeatRow{Audit: audit, Repeat: PromptAuditRepeat{
			Count: row.RepeatCount, FirstAt: row.FirstAt, LastAt: row.LastAt,
			WorstDecision: worstDecision, Blocks: row.Blocks, Unavailable: row.Unavailable,
		}})
	}
	return collapsed, total, nil
}

// ListPromptAuditGroupRows returns the requests behind one collapsed row, newest
// first and capped at promptAuditRepeatRowLimit, together with how many the group
// holds in all, so a capped expansion can say what it left out. The
// representative id identifies the group, so the caller never has to know the
// group key; the same filter is applied again, which keeps the expansion and the
// collapsed count on one definition. A representative that no longer exists
// expands to nothing.
func ListPromptAuditGroupRows(filter PromptAuditFilter, representativeID int64) ([]*PromptAudit, int64, error) {
	filter.GroupIDs = []int64{representativeID}
	if err := resolvePromptAuditGroups(&filter); err != nil {
		return nil, 0, err
	}
	if len(filter.groups) == 0 {
		return []*PromptAudit{}, 0, nil
	}
	var total int64
	if err := applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var audits []*PromptAudit
	if err := applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).Omit(promptAuditListingOmittedColumns...).
		Order("id desc").Limit(promptAuditRepeatRowLimit).Find(&audits).Error; err != nil {
		return nil, 0, err
	}
	return audits, total, nil
}

// promptAuditGroupKey is what a collapsed row stands for: every request one user
// sent for one question across models. A request whose hash was never
// recorded stands alone, so its own id is its key.
type promptAuditGroupKey struct {
	ID         int64
	UserID     int
	PromptHash string
	GroupKey   string
}

// resolvePromptAuditGroups looks up the representatives filter.GroupIDs names.
// A representative that no longer exists names no group; applyPromptAuditFilter
// then matches nothing for it rather than widening to every row.
func resolvePromptAuditGroups(filter *PromptAuditFilter) error {
	filter.groups = nil
	if len(filter.GroupIDs) == 0 {
		return nil
	}
	var keys []promptAuditGroupKey
	if err := DB.Model(&PromptAudit{}).Select("id, user_id, prompt_hash, group_key").
		Where("id IN ?", filter.GroupIDs).Scan(&keys).Error; err != nil {
		return err
	}
	filter.groups = keys
	return nil
}

func GetPromptAuditStats(filter PromptAuditFilter, categories []string) (PromptAuditStats, error) {
	stats := PromptAuditStats{
		Statuses:   map[string]int64{},
		Decisions:  map[string]int64{},
		Categories: map[string]int64{},
	}
	filtered := func() *gorm.DB {
		return applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter)
	}
	if err := filtered().Count(&stats.Total).Error; err != nil {
		return stats, err
	}
	type countRow struct {
		Value string
		Count int64
	}
	var rows []countRow
	if err := filtered().Select("status AS value, COUNT(*) AS count").Group("status").Scan(&rows).Error; err != nil {
		return stats, err
	}
	for _, row := range rows {
		stats.Statuses[row.Value] = row.Count
	}
	rows = nil
	if err := filtered().Select("decision AS value, COUNT(*) AS count").Group("decision").Scan(&rows).Error; err != nil {
		return stats, err
	}
	for _, row := range rows {
		if row.Value != "" {
			stats.Decisions[row.Value] = row.Count
		}
	}
	for _, category := range categories {
		var count int64
		pattern := "%\"" + category + "\"%"
		if err := filtered().Where("categories LIKE ?", pattern).Count(&count).Error; err != nil {
			return stats, err
		}
		stats.Categories[category] = count
	}
	if err := filtered().Where("unknown_categories <> '' AND unknown_categories <> '[]'").Count(&stats.Unknown).Error; err != nil {
		return stats, err
	}
	return stats, nil
}

func ClaimPromptAudit(owner string, now, leaseUntil int64) (*PromptAudit, bool, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, false, errors.New("prompt audit lease owner is required")
	}
	const exhaustedCondition = "((status = ? AND next_attempt_at <= ?) OR (status = ? AND lease_until < ?)) AND attempts >= max_attempts"
	var exhausted []PromptAudit
	if err := DB.Select("id", "scan_payload", "content_snapshot", "scan_payload_truncated").
		Where(exhaustedCondition, PromptAuditStatusRetry, now, PromptAuditStatusProcessing, now).
		Order("id asc").Limit(32).Find(&exhausted).Error; err != nil {
		return nil, false, err
	}
	for _, audit := range exhausted {
		payload, truncated := audit.retainedPayload()
		terminal := DB.Model(&PromptAudit{}).Where("id = ?", audit.ID).
			Where(
				exhaustedCondition,
				PromptAuditStatusRetry, now, PromptAuditStatusProcessing, now,
			).
			Updates(map[string]any{
				"status":                 PromptAuditStatusFailed,
				"scan_payload":           payload,
				"content_snapshot":       payload,
				"scan_payload_truncated": truncated,
				"would_action":           "unavailable",
				"error_code":             "max_attempts_exhausted",
				"lease_owner":            "",
				"lease_until":            int64(0),
				"next_attempt_at":        int64(0),
				"completed_at":           now,
				"updated_at":             now,
			})
		if terminal.Error != nil {
			return nil, false, terminal.Error
		}
	}
	var candidates []PromptAudit
	err := DB.Where(
		"attempts < max_attempts AND ((status = ?) OR (status = ? AND next_attempt_at <= ?) OR (status = ? AND lease_until < ?))",
		PromptAuditStatusQueued, PromptAuditStatusRetry, now, PromptAuditStatusProcessing, now,
	).Order("id asc").Limit(32).Find(&candidates).Error
	if err != nil {
		return nil, false, err
	}
	for _, candidate := range candidates {
		result := DB.Model(&PromptAudit{}).
			Where("id = ?", candidate.ID).
			Where(
				"attempts < max_attempts AND ((status = ?) OR (status = ? AND next_attempt_at <= ?) OR (status = ? AND lease_until < ?))",
				PromptAuditStatusQueued, PromptAuditStatusRetry, now, PromptAuditStatusProcessing, now,
			).
			Updates(map[string]any{
				"status":          PromptAuditStatusProcessing,
				"lease_owner":     owner,
				"lease_until":     leaseUntil,
				"attempts":        gorm.Expr("attempts + 1"),
				"next_attempt_at": int64(0),
				"updated_at":      now,
			})
		if result.Error != nil {
			return nil, false, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		var claimed PromptAudit
		if err := DB.Where("id = ?", candidate.ID).First(&claimed).Error; err != nil {
			return nil, false, err
		}
		return &claimed, true, nil
	}
	return nil, false, nil
}

func FinishPromptAudit(id int64, owner string, completion PromptAuditCompletion) error {
	var audit PromptAudit
	if err := DB.Select("scan_payload", "content_snapshot", "scan_payload_truncated").Where("id = ? AND status = ? AND lease_owner = ?", id, PromptAuditStatusProcessing, owner).First(&audit).Error; err != nil {
		return err
	}
	payload, truncated := audit.retainedPayload()
	categories, err := encodePromptAuditStrings(completion.Categories)
	if err != nil {
		return err
	}
	unknown, err := encodePromptAuditStrings(completion.UnknownCategories)
	if err != nil {
		return err
	}
	reviewCodes, err := encodePromptAuditStrings(completion.ReviewCodes)
	if err != nil {
		return err
	}
	scores, err := encodePromptAuditScores(completion.Scores)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := DB.Model(&PromptAudit{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, PromptAuditStatusProcessing, owner).
		Updates(map[string]any{
			"status":                 PromptAuditStatusDone,
			"scan_payload":           payload,
			"content_snapshot":       payload,
			"scan_payload_truncated": truncated,
			"safety":                 completion.Safety,
			"refusal":                completion.Refusal,
			"decision":               completion.Decision,
			"action":                 completion.Action,
			"would_action":           completion.WouldAction,
			"categories":             categories,
			"unknown_categories":     unknown,
			"endpoint_id":            completion.EndpointID,
			"endpoint_model":         clampPromptAuditColumn(completion.EndpointModel, 255),
			"review_status":          completion.ReviewStatus,
			"review_decision":        completion.ReviewDecision,
			"review_codes":           reviewCodes,
			"scores":                 scores,
			"review_reason":          completion.ReviewReason,
			"reviewer_endpoint_id":   completion.ReviewerEndpointID,
			"chunk_count":            completion.ChunkCount,
			"latency_ms":             completion.LatencyMS,
			"error_code":             completion.ErrorCode,
			"lease_owner":            "",
			"lease_until":            int64(0),
			"completed_at":           now,
			"updated_at":             now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("prompt audit lease lost")
	}
	return nil
}

func FailPromptAudit(id int64, owner, errorCode string, retryAt int64, terminal bool) error {
	now := common.GetTimestamp()
	updates := map[string]any{
		"error_code":   errorCode,
		"lease_owner":  "",
		"lease_until":  int64(0),
		"updated_at":   now,
		"would_action": "unavailable",
	}
	if terminal {
		var audit PromptAudit
		if err := DB.Select("scan_payload", "content_snapshot", "scan_payload_truncated").Where("id = ? AND status = ? AND lease_owner = ?", id, PromptAuditStatusProcessing, owner).First(&audit).Error; err != nil {
			return err
		}
		payload, truncated := audit.retainedPayload()
		updates["status"] = PromptAuditStatusFailed
		updates["scan_payload"] = payload
		updates["content_snapshot"] = payload
		updates["scan_payload_truncated"] = truncated
		updates["completed_at"] = now
	} else {
		updates["status"] = PromptAuditStatusRetry
		updates["next_attempt_at"] = retryAt
	}
	result := DB.Model(&PromptAudit{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, PromptAuditStatusProcessing, owner).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("prompt audit lease lost")
	}
	return nil
}

// queued payloads are complete; legacy rows may only have a content snapshot.
func (audit *PromptAudit) retainedPayload() ([]byte, bool) {
	payload := audit.ScanPayload
	if len(payload) == 0 {
		payload = audit.ContentSnapshot
	}
	retained, truncated := RetainPromptAuditPayload(payload)
	return retained, truncated || audit.ScanPayloadTruncated
}

func RetryPromptAudit(id int64, maxAttempts int) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var audit PromptAudit
		if err := lockForUpdate(tx).Where("id = ?", id).First(&audit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPromptAuditNotFound
			}
			return err
		}
		if audit.Status != PromptAuditStatusFailed {
			return ErrPromptAuditNotRetryable
		}
		payload := audit.ContentSnapshot
		if len(payload) == 0 {
			payload = audit.FullPrompt
		}
		if len(payload) == 0 || audit.ScanPayloadTruncated || len(audit.ContentSnapshot) == 0 && audit.FullPromptTruncated {
			return ErrPromptAuditPayloadMissing
		}
		now := common.GetTimestamp()
		return tx.Model(&PromptAudit{}).Where("id = ? AND status = ?", id, PromptAuditStatusFailed).Updates(map[string]any{
			"status":             PromptAuditStatusQueued,
			"scan_payload":       append([]byte(nil), payload...),
			"action":             "pending",
			"safety":             "",
			"decision":           "",
			"would_action":       "pending",
			"categories":         "",
			"unknown_categories": "",
			"endpoint_id":        "",
			"endpoint_model":     "",
			"latency_ms":         int64(0),
			"chunk_count":        0,
			"attempts":           0,
			"max_attempts":       maxAttempts,
			"next_attempt_at":    int64(0),
			"lease_owner":        "",
			"lease_until":        int64(0),
			"error_code":         "",
			"completed_at":       int64(0),
			"updated_at":         now,
		}).Error
	})
}

func PreviewPromptAuditDelete(filter PromptAuditFilter) (eligible, active, maxID int64, err error) {
	if err = resolvePromptAuditGroups(&filter); err != nil {
		return
	}
	filtered := func() *gorm.DB {
		return applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter)
	}
	if err = filtered().Where("status IN ?", promptAuditTerminalStatuses()).Count(&eligible).Error; err != nil {
		return
	}
	if err = filtered().Where("status IN ?", promptAuditActiveStatuses()).Count(&active).Error; err != nil {
		return
	}
	if eligible > 0 {
		var audit PromptAudit
		err = filtered().Where("status IN ?", promptAuditTerminalStatuses()).Order("id desc").Select("id").First(&audit).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		} else if err == nil {
			maxID = audit.ID
		}
	}
	return
}

func DeletePromptAudits(filter PromptAuditFilter, expectedCount, maxID int64) (int64, error) {
	if expectedCount < 0 || maxID <= 0 {
		return 0, errors.New("prompt audit deletion confirmation is invalid")
	}
	filter.MaxID = maxID
	if err := resolvePromptAuditGroups(&filter); err != nil {
		return 0, err
	}
	var count int64
	if err := applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).
		Where("status IN ?", promptAuditTerminalStatuses()).Count(&count).Error; err != nil {
		return 0, err
	}
	if count != expectedCount {
		return 0, ErrPromptAuditDeleteChanged
	}
	if count == 0 {
		return 0, nil
	}
	result := applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).
		Where("status IN ?", promptAuditTerminalStatuses()).Delete(&PromptAudit{})
	return result.RowsAffected, result.Error
}

func CleanupPromptAuditPromptsBefore(cutoff int64, batchSize int) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	if batchSize < 1 {
		batchSize = 500
	}
	var ids []int64
	if err := DB.Model(&PromptAudit{}).
		Where("status IN ? AND completed_at > 0 AND completed_at < ? AND (full_prompt IS NOT NULL OR content_snapshot IS NOT NULL OR scan_payload IS NOT NULL)", promptAuditTerminalStatuses(), cutoff).
		Order("id asc").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Model(&PromptAudit{}).
		Where("id IN ? AND status IN ? AND completed_at > 0 AND completed_at < ?", ids, promptAuditTerminalStatuses(), cutoff).
		Updates(map[string]any{"full_prompt": []byte(nil), "content_snapshot": []byte(nil), "scan_payload": []byte(nil), "updated_at": common.GetTimestamp()})
	return result.RowsAffected, result.Error
}

func applyPromptAuditFilter(query *gorm.DB, filter PromptAuditFilter) *gorm.DB {
	if len(filter.IDs) > 0 {
		query = query.Where("id IN ?", filter.IDs)
	}
	if len(filter.GroupIDs) > 0 {
		sql, args := promptAuditGroupCondition(filter.groups)
		query = query.Where(sql, args...)
	}
	if filter.MaxID > 0 {
		query = query.Where("id <= ?", filter.MaxID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Decision != "" {
		query = query.Where("decision = ?", filter.Decision)
	}
	if filter.Category != "" {
		query = query.Where("categories LIKE ?", "%\""+filter.Category+"\"%")
	}
	if filter.Username != "" {
		// The name is snapshotted when a row is written. Rows written before the
		// column existed, or whose lookup failed, hold none, so they are matched
		// through the account that holds the name now.
		query = query.Where("(username = ? OR ((username IS NULL OR username = '') AND user_id IN (?)))",
			filter.Username, DB.Model(&User{}).Select("id").Where("username = ?", filter.Username))
	}
	if filter.Group != "" {
		query = query.Where("group_name = ?", filter.Group)
	}
	if filter.Protocol != "" {
		query = query.Where("protocol = ?", filter.Protocol)
	}
	if filter.Model != "" {
		query = query.Where("model_name = ?", filter.Model)
	}
	if filter.RequestID != "" {
		query = query.Where("request_id = ?", filter.RequestID)
	}
	if filter.Direction != "" {
		query = query.Where("direction = ?", filter.Direction)
	}
	switch filter.Detector {
	case "":
	case "wordlist":
		query = query.Where("inspection_type = ?", "wordlist")
	case "model":
		query = query.Where("(inspection_type IS NULL OR inspection_type NOT IN ?)", []string{"wordlist", "probe_block", "probe_phrase", "probe_semantic", "probe_fast_pass"})
	case "probe":
		query = query.Where("inspection_type IN ?", []string{"probe_block", "probe_phrase", "probe_semantic", "probe_fast_pass"})
	default:
		query = query.Where("inspection_type = ?", filter.Detector)
	}
	if filter.StartTime > 0 {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("created_at <= ?", filter.EndTime)
	}
	return query
}

// promptAuditGroupCondition matches the requests the given groups hold. With no
// group left to match — every representative is gone — it matches nothing,
// never everything.
func promptAuditGroupCondition(groups []promptAuditGroupKey) (string, []any) {
	var sql strings.Builder
	sql.WriteString("(1 = 0")
	args := make([]any, 0, len(groups)*3)
	for _, group := range groups {
		key := group.GroupKey
		if key == "" {
			key = group.PromptHash
		}
		if key == "" {
			sql.WriteString(" OR id = ?")
			args = append(args, group.ID)
			continue
		}
		sql.WriteString(" OR (user_id = ? AND (" + promptAuditEffectiveGroupKey + ") = ?)")
		args = append(args, group.UserID, key)
	}
	sql.WriteString(")")
	return sql.String(), args
}

func ReviewPromptAudit(id int64, reviewerID int, reviewerName, status, reason string) error {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "false_positive" && status != "confirmed_violation" {
		return errors.New("review must be false_positive or confirmed_violation")
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 512 {
		return errors.New("review reason must not exceed 512 characters")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var audit PromptAudit
		if err := lockForUpdate(tx).Select("id", "status").Where("id = ?", id).First(&audit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPromptAuditNotFound
			}
			return err
		}
		if audit.Status != PromptAuditStatusDone && audit.Status != PromptAuditStatusFailed {
			return ErrPromptAuditNotReviewable
		}
		now := common.GetTimestamp()
		return tx.Model(&PromptAudit{}).Where("id = ? AND status IN ?", id, promptAuditTerminalStatuses()).Updates(map[string]any{
			"human_review": status, "human_review_reason": reason, "reviewed_by": reviewerID,
			"reviewer_name": strings.TrimSpace(reviewerName), "reviewed_at": now, "updated_at": now,
		}).Error
	})
}

func UpdatePromptAuditDelivery(id int64, status string) error {
	if id <= 0 {
		return nil
	}
	status = strings.TrimSpace(status)
	updates := map[string]any{"delivery_status": status, "updated_at": common.GetTimestamp()}
	if status == "delivery_failed" {
		updates["action"] = "unavailable"
	}
	result := DB.Model(&PromptAudit{}).Where("id = ?", id).Updates(updates)
	return result.Error
}

func MigratePromptAuditDefaults() error {
	if err := DB.Model(&PromptWordlist{}).Where("action IS NULL OR action = ?", "").Update("action", prompt_audit_setting.WordlistActionBlock).Error; err != nil {
		return err
	}
	if err := DB.Model(&PromptAudit{}).Where("direction IS NULL OR direction = ?", "").Updates(map[string]any{
		"direction": "input", "delivery_status": "not_applicable", "coverage_complete": true,
	}).Error; err != nil {
		return err
	}
	if err := DB.Model(&PromptAudit{}).Where("(action IS NULL OR action = ?) AND decision = ?", "", "block").Update("action", "block").Error; err != nil {
		return err
	}
	return DB.Model(&PromptAudit{}).Where("action IS NULL OR action = ?", "").Update("action", "allow").Error
}

func promptAuditActiveStatuses() []PromptAuditStatus {
	return []PromptAuditStatus{PromptAuditStatusQueued, PromptAuditStatusProcessing, PromptAuditStatusRetry}
}

func promptAuditTerminalStatuses() []PromptAuditStatus {
	return []PromptAuditStatus{PromptAuditStatusDone, PromptAuditStatusFailed}
}

// clampPromptAuditColumn makes a client- or upstream-supplied value storable in
// every supported database. It first repairs the bytes: Go's HTTP server accepts
// any byte above 0x7F in a header and a decoded %00 in a path, while MySQL and
// PostgreSQL reject invalid UTF-8 and PostgreSQL rejects NUL. It then truncates
// by rune, mirroring the treatment AuditLog applies to UserAgent, because MySQL
// and PostgreSQL reject a value wider than its column. Either rejection would
// lose the whole audit row where SQLite stored it silently — and in async mode a
// lost row is a request that is never inspected.
func clampPromptAuditColumn(value string, limit int) string {
	value = strings.ReplaceAll(strings.ToValidUTF8(value, "\uFFFD"), "\x00", "")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

// encodePromptAuditScores stores the raw model probabilities as JSON. A nil map
// is stored as an empty string so a label-based verdict, which has no scores,
// leaves the column empty rather than holding "null".
func encodePromptAuditScores(scores map[string]float64) (string, error) {
	if len(scores) == 0 {
		return "", nil
	}
	data, err := common.Marshal(scores)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodePromptAuditScores(value string) map[string]float64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return nil
	}
	var scores map[string]float64
	if err := common.UnmarshalJsonStr(value, &scores); err != nil || len(scores) == 0 {
		return nil
	}
	return scores
}

func encodePromptAuditStrings(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	data, err := common.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodePromptAuditStrings(value string) []string {
	if value == "" {
		return []string{}
	}
	var result []string
	if err := common.UnmarshalJsonStr(value, &result); err != nil {
		common.SysError(fmt.Sprintf("decode prompt audit categories failed: %v", err))
		return []string{}
	}
	if result == nil {
		return []string{}
	}
	return result
}
