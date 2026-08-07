package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"

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
	ErrPromptAuditPayloadMissing = errors.New("full prompt is unavailable or truncated and cannot be retried safely")
	ErrPromptAuditDeleteChanged  = errors.New("prompt audit deletion candidates changed; preview again")
)

// PromptAudit is both the durable async work item and the final audit event.
// ScanPayload is cleared on terminal completion; FullPrompt is separately capped
// for authorized administrator review.
type PromptAudit struct {
	ID                  int64             `json:"id" gorm:"primaryKey"`
	RequestID           string            `json:"request_id" gorm:"type:varchar(64);index"`
	UserID              int               `json:"user_id" gorm:"index"`
	TokenID             int               `json:"token_id" gorm:"index"`
	TokenName           string            `json:"token_name" gorm:"type:varchar(255)"`
	GroupName           string            `json:"group" gorm:"type:varchar(64);index"`
	Protocol            string            `json:"protocol" gorm:"type:varchar(64);index"`
	ModelName           string            `json:"model" gorm:"type:varchar(255);index"`
	Stage               string            `json:"stage" gorm:"type:varchar(32)"`
	ConfigVersion       string            `json:"config_version" gorm:"type:varchar(64);index"`
	ExecutionMode       string            `json:"execution_mode" gorm:"type:varchar(32);index"`
	Status              PromptAuditStatus `json:"status" gorm:"type:varchar(32);index"`
	PromptHash          string            `json:"prompt_hash" gorm:"type:varchar(64);index"`
	PromptLength        int               `json:"prompt_length"`
	SegmentCount        int               `json:"segment_count"`
	ChunkCount          int               `json:"chunk_count"`
	FullPrompt          []byte            `json:"-"`
	FullPromptTruncated bool              `json:"full_prompt_truncated"`
	RedactedPreview     string            `json:"redacted_preview" gorm:"type:varchar(512)"`
	ScanPayload         []byte            `json:"-"`
	PolicyCategories    string            `json:"-" gorm:"type:text"`
	Safety              string            `json:"safety" gorm:"type:varchar(32);index"`
	Decision            string            `json:"decision" gorm:"type:varchar(32);index"`
	WouldAction         string            `json:"would_action" gorm:"type:varchar(32);index"`
	Categories          string            `json:"-" gorm:"type:text"`
	UnknownCategories   string            `json:"-" gorm:"type:text"`
	EndpointID          string            `json:"endpoint_id" gorm:"type:varchar(128);index"`
	LatencyMS           int64             `json:"latency_ms"`
	Attempts            int               `json:"attempts"`
	MaxAttempts         int               `json:"max_attempts"`
	NextAttemptAt       int64             `json:"next_attempt_at" gorm:"index"`
	LeaseOwner          string            `json:"-" gorm:"type:varchar(128);index"`
	LeaseUntil          int64             `json:"-" gorm:"index"`
	ErrorCode           string            `json:"error_code" gorm:"type:varchar(64);index"`
	CreatedAt           int64             `json:"created_at" gorm:"index"`
	UpdatedAt           int64             `json:"updated_at" gorm:"index"`
	CompletedAt         int64             `json:"completed_at" gorm:"index"`
}

type PromptAuditResponse struct {
	ID                  int64             `json:"id"`
	RequestID           string            `json:"request_id"`
	UserID              int               `json:"user_id"`
	TokenID             int               `json:"token_id"`
	TokenName           string            `json:"token_name"`
	GroupName           string            `json:"group"`
	Protocol            string            `json:"protocol"`
	ModelName           string            `json:"model"`
	Stage               string            `json:"stage"`
	ConfigVersion       string            `json:"config_version"`
	ExecutionMode       string            `json:"execution_mode"`
	Status              PromptAuditStatus `json:"status"`
	PromptHash          string            `json:"prompt_hash"`
	PromptLength        int               `json:"prompt_length"`
	SegmentCount        int               `json:"segment_count"`
	ChunkCount          int               `json:"chunk_count"`
	FullPrompt          *string           `json:"full_prompt,omitempty"`
	FullPromptAvailable bool              `json:"full_prompt_available"`
	FullPromptTruncated bool              `json:"full_prompt_truncated"`
	RedactedPreview     string            `json:"redacted_preview"`
	Safety              string            `json:"safety"`
	Decision            string            `json:"decision"`
	WouldAction         string            `json:"would_action"`
	Categories          []string          `json:"categories"`
	UnknownCategories   []string          `json:"unknown_categories"`
	EndpointID          string            `json:"endpoint_id"`
	LatencyMS           int64             `json:"latency_ms"`
	Attempts            int               `json:"attempts"`
	MaxAttempts         int               `json:"max_attempts"`
	NextAttemptAt       int64             `json:"next_attempt_at"`
	ErrorCode           string            `json:"error_code"`
	CreatedAt           int64             `json:"created_at"`
	UpdatedAt           int64             `json:"updated_at"`
	CompletedAt         int64             `json:"completed_at"`
}

type PromptAuditFilter struct {
	IDs        []int64
	Status     string
	Decision   string
	Category   string
	UserID     int
	Group      string
	Protocol   string
	Model      string
	EndpointID string
	PromptHash string
	RequestID  string
	StartTime  int64
	EndTime    int64
	MaxID      int64
}

type PromptAuditStats struct {
	Total      int64            `json:"total"`
	Statuses   map[string]int64 `json:"statuses"`
	Decisions  map[string]int64 `json:"decisions"`
	Categories map[string]int64 `json:"categories"`
	Unknown    int64            `json:"unknown_categories"`
}

type PromptAuditCompletion struct {
	Safety            string
	Decision          string
	WouldAction       string
	Categories        []string
	UnknownCategories []string
	EndpointID        string
	ChunkCount        int
	LatencyMS         int64
	ErrorCode         string
}

func (audit *PromptAudit) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if audit.CreatedAt == 0 {
		audit.CreatedAt = now
	}
	if audit.UpdatedAt == 0 {
		audit.UpdatedAt = now
	}
	return nil
}

func (audit *PromptAudit) ToResponse(includeFullPrompt bool) PromptAuditResponse {
	response := PromptAuditResponse{
		ID: audit.ID, RequestID: audit.RequestID, UserID: audit.UserID,
		TokenID: audit.TokenID, TokenName: audit.TokenName, GroupName: audit.GroupName,
		Protocol: audit.Protocol, ModelName: audit.ModelName, Stage: audit.Stage,
		ConfigVersion: audit.ConfigVersion, ExecutionMode: audit.ExecutionMode,
		Status: audit.Status, PromptHash: audit.PromptHash, PromptLength: audit.PromptLength,
		SegmentCount: audit.SegmentCount, ChunkCount: audit.ChunkCount,
		FullPromptAvailable: len(audit.FullPrompt) > 0,
		FullPromptTruncated: audit.FullPromptTruncated, RedactedPreview: audit.RedactedPreview,
		Safety: audit.Safety, Decision: audit.Decision, WouldAction: audit.WouldAction,
		Categories:        decodePromptAuditStrings(audit.Categories),
		UnknownCategories: decodePromptAuditStrings(audit.UnknownCategories),
		EndpointID:        audit.EndpointID, LatencyMS: audit.LatencyMS, Attempts: audit.Attempts,
		MaxAttempts: audit.MaxAttempts, NextAttemptAt: audit.NextAttemptAt,
		ErrorCode: audit.ErrorCode, CreatedAt: audit.CreatedAt, UpdatedAt: audit.UpdatedAt,
		CompletedAt: audit.CompletedAt,
	}
	if includeFullPrompt && len(audit.FullPrompt) > 0 {
		value := string(audit.FullPrompt)
		response.FullPrompt = &value
	}
	return response
}

func CreatePromptAudit(audit *PromptAudit) error {
	if audit == nil {
		return errors.New("prompt audit is required")
	}
	return DB.Create(audit).Error
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
	terminal := DB.Model(&PromptAudit{}).
		Where(
			"((status = ? AND next_attempt_at <= ?) OR (status = ? AND lease_until < ?)) AND attempts >= max_attempts",
			PromptAuditStatusRetry, now, PromptAuditStatusProcessing, now,
		).
		Updates(map[string]any{
			"status":          PromptAuditStatusFailed,
			"scan_payload":    []byte(nil),
			"would_action":    "unavailable",
			"error_code":      "max_attempts_exhausted",
			"lease_owner":     "",
			"lease_until":     int64(0),
			"next_attempt_at": int64(0),
			"completed_at":    now,
			"updated_at":      now,
		})
	if terminal.Error != nil {
		return nil, false, terminal.Error
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
	categories, err := encodePromptAuditStrings(completion.Categories)
	if err != nil {
		return err
	}
	unknown, err := encodePromptAuditStrings(completion.UnknownCategories)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := DB.Model(&PromptAudit{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, PromptAuditStatusProcessing, owner).
		Updates(map[string]any{
			"status":             PromptAuditStatusDone,
			"scan_payload":       []byte(nil),
			"safety":             completion.Safety,
			"decision":           completion.Decision,
			"would_action":       completion.WouldAction,
			"categories":         categories,
			"unknown_categories": unknown,
			"endpoint_id":        completion.EndpointID,
			"chunk_count":        completion.ChunkCount,
			"latency_ms":         completion.LatencyMS,
			"error_code":         completion.ErrorCode,
			"lease_owner":        "",
			"lease_until":        int64(0),
			"completed_at":       now,
			"updated_at":         now,
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
		updates["status"] = PromptAuditStatusFailed
		updates["scan_payload"] = []byte(nil)
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
		if audit.FullPromptTruncated || len(audit.FullPrompt) == 0 {
			return ErrPromptAuditPayloadMissing
		}
		now := common.GetTimestamp()
		return tx.Model(&PromptAudit{}).Where("id = ? AND status = ?", id, PromptAuditStatusFailed).Updates(map[string]any{
			"status":             PromptAuditStatusQueued,
			"scan_payload":       append([]byte(nil), audit.FullPrompt...),
			"safety":             "",
			"decision":           "",
			"would_action":       "pending",
			"categories":         "",
			"unknown_categories": "",
			"endpoint_id":        "",
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
		Where("status IN ? AND completed_at > 0 AND completed_at < ? AND full_prompt IS NOT NULL", promptAuditTerminalStatuses(), cutoff).
		Order("id asc").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Model(&PromptAudit{}).
		Where("id IN ? AND status IN ? AND completed_at > 0 AND completed_at < ?", ids, promptAuditTerminalStatuses(), cutoff).
		Updates(map[string]any{"full_prompt": []byte(nil), "updated_at": common.GetTimestamp()})
	return result.RowsAffected, result.Error
}

func applyPromptAuditFilter(query *gorm.DB, filter PromptAuditFilter) *gorm.DB {
	if len(filter.IDs) > 0 {
		query = query.Where("id IN ?", filter.IDs)
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
	if filter.UserID > 0 {
		query = query.Where("user_id = ?", filter.UserID)
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
	if filter.EndpointID != "" {
		query = query.Where("endpoint_id = ?", filter.EndpointID)
	}
	if filter.PromptHash != "" {
		query = query.Where("prompt_hash = ?", filter.PromptHash)
	}
	if filter.RequestID != "" {
		query = query.Where("request_id = ?", filter.RequestID)
	}
	if filter.StartTime > 0 {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("created_at <= ?", filter.EndTime)
	}
	return query
}

func promptAuditActiveStatuses() []PromptAuditStatus {
	return []PromptAuditStatus{PromptAuditStatusQueued, PromptAuditStatusProcessing, PromptAuditStatusRetry}
}

func promptAuditTerminalStatuses() []PromptAuditStatus {
	return []PromptAuditStatus{PromptAuditStatusDone, PromptAuditStatusFailed}
}

func encodePromptAuditStrings(values []string) (string, error) {
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
	return result
}
