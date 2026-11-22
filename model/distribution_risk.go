package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

const (
	DistributionRiskPending       = "pending"
	DistributionRiskConfirmed     = "confirmed"
	DistributionRiskFalsePositive = "false_positive"
)

var (
	ErrDistributionRiskNotFound   = errors.New("distribution risk event not found")
	ErrDistributionRiskNotPending = errors.New("distribution risk event is no longer pending")
	ErrDistributionRiskDecision   = errors.New("invalid distribution risk decision")
)

type DistributionRiskJSON string

func (DistributionRiskJSON) GormDataType() string {
	return "text"
}

func (DistributionRiskJSON) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "longtext"
	}
	return "text"
}

func (value DistributionRiskJSON) Value() (driver.Value, error) {
	return string(value), nil
}

func (value *DistributionRiskJSON) Scan(source any) error {
	switch source := source.(type) {
	case nil:
		*value = ""
	case string:
		*value = DistributionRiskJSON(source)
	case []byte:
		*value = DistributionRiskJSON(source)
	default:
		return fmt.Errorf("scan distribution risk JSON from %T", source)
	}
	return nil
}

// DistributionRiskEvent stores an incident summary, not a per-request log.
// Evidence contains the distinct IPs, user agents and hashed client sessions
// observed during the evidence window and is cleared after 30 days.
type DistributionRiskEvent struct {
	ID             int64                `json:"id" gorm:"primaryKey"`
	UserID         int                  `json:"user_id" gorm:"index"`
	ScopeKind      string               `json:"scope_kind" gorm:"type:varchar(16);index"`
	ScopeID        int                  `json:"scope_id" gorm:"index"`
	TokenIDs       DistributionRiskJSON `json:"token_ids"`
	Score          int                  `json:"score"`
	Level          string               `json:"level" gorm:"type:varchar(16);index"`
	CategoryScores DistributionRiskJSON `json:"category_scores"`
	Evidence       DistributionRiskJSON `json:"evidence"`
	Status         string               `json:"status" gorm:"type:varchar(24);index"`
	CreatedAt      int64                `json:"created_at" gorm:"index"`
	ReviewedAt     int64                `json:"reviewed_at"`
	ReviewerID     int                  `json:"reviewer_id"`
	ReviewReason   string               `json:"review_reason" gorm:"type:varchar(512)"`
}

type DistributionRiskEventFilters struct {
	Status    string
	Level     string
	ScopeKind string
	UserID    int
	Username  string
	StartTime int64
	EndTime   int64
}

type DistributionRiskEventDisplayNames struct {
	Usernames  map[int]string
	TokenNames map[int]string
}

// DistributionRiskHold keeps a high-risk token blocked until an administrator
// reviews the incident. The Redis mirror is only a hot-path cache.
type DistributionRiskHold struct {
	TokenID   int   `json:"token_id" gorm:"primaryKey;autoIncrement:false"`
	EventID   int64 `json:"event_id" gorm:"primaryKey;autoIncrement:false"`
	UserID    int   `json:"user_id" gorm:"index"`
	CreatedAt int64 `json:"created_at" gorm:"index"`
}

func CreateDistributionRiskEvent(event *DistributionRiskEvent, hold bool, tokenIDs []int) error {
	if event == nil || event.UserID <= 0 {
		return errors.New("invalid distribution risk event")
	}
	uniqueIDs := make([]int, 0, len(tokenIDs))
	seenIDs := make(map[int]struct{}, len(tokenIDs))
	for _, id := range tokenIDs {
		if _, seen := seenIDs[id]; id > 0 && !seen {
			uniqueIDs = append(uniqueIDs, id)
			seenIDs[id] = struct{}{}
		}
	}
	if len(uniqueIDs) == 0 {
		return errors.New("distribution risk event has no contributing tokens")
	}
	tokenIDsJSON, err := common.Marshal(uniqueIDs)
	if err != nil {
		return err
	}
	event.TokenIDs = DistributionRiskJSON(tokenIDsJSON)
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().Unix()
	}
	event.Status = DistributionRiskPending

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(event).Error; err != nil {
			return err
		}
		if !hold {
			return nil
		}
		holds := make([]DistributionRiskHold, 0, len(uniqueIDs))
		for _, tokenID := range uniqueIDs {
			holds = append(holds, DistributionRiskHold{
				TokenID: tokenID, UserID: event.UserID, EventID: event.ID, CreatedAt: event.CreatedAt,
			})
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "token_id"}, {Name: "event_id"}}, DoNothing: true}).Create(&holds).Error
	})
}

func GetDistributionRiskEvent(id int64) (*DistributionRiskEvent, error) {
	var event DistributionRiskEvent
	if err := DB.First(&event, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDistributionRiskNotFound
		}
		return nil, err
	}
	return &event, nil
}

func ListDistributionRiskEvents(filters DistributionRiskEventFilters, offset, limit int) ([]DistributionRiskEvent, int64, error) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := DB.Model(&DistributionRiskEvent{})
	if filters.Status != "" && filters.Status != "all" {
		query = query.Where("status = ?", filters.Status)
	}
	if filters.Level != "" && filters.Level != "all" {
		query = query.Where("level = ?", filters.Level)
	}
	if filters.ScopeKind != "" && filters.ScopeKind != "all" {
		query = query.Where("scope_kind = ?", filters.ScopeKind)
	}
	if filters.UserID > 0 {
		query = query.Where("user_id = ?", filters.UserID)
	}
	if filters.Username != "" {
		userIDs := DB.Model(&User{}).Select("id").Where("username LIKE ?", "%"+filters.Username+"%")
		query = query.Where("user_id IN (?)", userIDs)
	}
	if filters.StartTime > 0 {
		query = query.Where("created_at >= ?", filters.StartTime)
	}
	if filters.EndTime > 0 {
		query = query.Where("created_at <= ?", filters.EndTime)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []DistributionRiskEvent
	if err := query.Order("created_at DESC").Order("id DESC").Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

func GetDistributionRiskEventDisplayNames(events []DistributionRiskEvent) (DistributionRiskEventDisplayNames, error) {
	names := DistributionRiskEventDisplayNames{
		Usernames:  make(map[int]string),
		TokenNames: make(map[int]string),
	}
	if DB == nil {
		return names, errors.New("main database is not initialized")
	}

	userIDs := make([]int, 0, len(events))
	seenUserIDs := make(map[int]struct{}, len(events))
	tokenIDs := make([]int, 0, len(events))
	seenTokenIDs := make(map[int]struct{})
	for index := range events {
		if userID := events[index].UserID; userID > 0 {
			if _, exists := seenUserIDs[userID]; !exists {
				userIDs = append(userIDs, userID)
				seenUserIDs[userID] = struct{}{}
			}
		}
		ids, err := DistributionRiskEventTokenIDs(&events[index])
		if err != nil {
			return names, err
		}
		for _, tokenID := range ids {
			if tokenID <= 0 {
				continue
			}
			if _, exists := seenTokenIDs[tokenID]; !exists {
				tokenIDs = append(tokenIDs, tokenID)
				seenTokenIDs[tokenID] = struct{}{}
			}
		}
	}
	slices.Sort(userIDs)
	slices.Sort(tokenIDs)

	for chunk := range slices.Chunk(userIDs, 500) {
		var users []User
		if err := DB.Model(&User{}).Select("id", "username").Where("id IN ?", chunk).Find(&users).Error; err != nil {
			return names, fmt.Errorf("load distribution risk usernames: %w", err)
		}
		for _, user := range users {
			names.Usernames[user.Id] = user.Username
		}
	}
	for chunk := range slices.Chunk(tokenIDs, 500) {
		var tokens []Token
		if err := DB.Model(&Token{}).Select("id", "name").Where("id IN ?", chunk).Find(&tokens).Error; err != nil {
			return names, fmt.Errorf("load distribution risk token names: %w", err)
		}
		for _, token := range tokens {
			names.TokenNames[token.Id] = token.Name
		}
	}

	return names, nil
}

func CountPendingDistributionRiskEvents() (int64, error) {
	var total int64
	err := DB.Model(&DistributionRiskEvent{}).Where("status = ?", DistributionRiskPending).Count(&total).Error
	return total, err
}

func IsDistributionRiskTokenHeld(tokenID int) (bool, error) {
	if tokenID <= 0 {
		return false, nil
	}
	if DB == nil {
		return false, errors.New("main database is not initialized")
	}
	var count int64
	err := DB.Model(&DistributionRiskHold{}).Where("token_id = ?", tokenID).Count(&count).Error
	return count > 0, err
}

func DistributionRiskEventTokenIDs(event *DistributionRiskEvent) ([]int, error) {
	if event == nil || strings.TrimSpace(string(event.TokenIDs)) == "" {
		return nil, nil
	}
	var ids []int
	if err := common.UnmarshalJsonStr(string(event.TokenIDs), &ids); err != nil {
		return nil, fmt.Errorf("decode distribution risk token IDs: %w", err)
	}
	return ids, nil
}

func ReviewDistributionRiskEvent(id int64, decision string, reviewerID int, reason string) ([]int, error) {
	if decision != DistributionRiskConfirmed && decision != DistributionRiskFalsePositive {
		return nil, ErrDistributionRiskDecision
	}
	var tokenIDs []int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var event DistributionRiskEvent
		if err := lockForUpdate(tx).First(&event, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDistributionRiskNotFound
			}
			return err
		}
		if event.Status != DistributionRiskPending {
			return ErrDistributionRiskNotPending
		}
		var err error
		tokenIDs, err = DistributionRiskEventTokenIDs(&event)
		if err != nil {
			return err
		}
		if decision == DistributionRiskConfirmed && len(tokenIDs) > 0 {
			var tokens []Token
			if err := tx.Where("user_id = ? AND id IN ?", event.UserID, tokenIDs).Find(&tokens).Error; err != nil {
				return err
			}
			if err := invalidateTokensCache(tokens); err != nil {
				return fmt.Errorf("invalidate contributing token cache: %w", err)
			}
			if err := tx.Model(&Token{}).Where("user_id = ? AND id IN ?", event.UserID, tokenIDs).Update("status", common.TokenStatusDisabled).Error; err != nil {
				return err
			}
		}
		now := time.Now().Unix()
		if err := tx.Model(&DistributionRiskEvent{}).Where("id = ? AND status = ?", id, DistributionRiskPending).Updates(map[string]any{
			"status": decision, "reviewed_at": now, "reviewer_id": reviewerID, "review_reason": reason,
		}).Error; err != nil {
			return err
		}
		return tx.Where("event_id = ?", id).Delete(&DistributionRiskHold{}).Error
	})
	if err != nil {
		return nil, err
	}
	return tokenIDs, nil
}

func PruneDistributionRiskData(now time.Time) error {
	if DB == nil {
		return errors.New("main database is not initialized")
	}
	evidenceBefore := now.Add(-30 * 24 * time.Hour).Unix()
	if err := DB.Model(&DistributionRiskEvent{}).
		Where("created_at < ? AND evidence <> ''", evidenceBefore).
		Update("evidence", "").Error; err != nil {
		return err
	}
	metadataBefore := now.Add(-180 * 24 * time.Hour).Unix()
	return DB.Where("created_at < ? AND status <> ?", metadataBefore, DistributionRiskPending).
		Delete(&DistributionRiskEvent{}).Error
}
