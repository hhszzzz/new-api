package model

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxPromptWordlists = 50

const promptScopePoliciesKey = "prompt_audit.scope_policies"

var (
	ErrPromptWordlistLimit  = errors.New("wordlist limit reached")
	ErrPromptWordlistExists = errors.New("wordlist already exists")
)

type PromptWordlist struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	Name           string `json:"name" gorm:"type:varchar(128)"`
	SourceURL      string `json:"source_url" gorm:"type:varchar(2048)"`
	SourceHash     string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	Enabled        bool   `json:"enabled"`
	Action         string `json:"action" gorm:"type:varchar(16);index"`
	AutoUpdate     bool   `json:"auto_update"`
	Status         string `json:"status" gorm:"type:varchar(32);index"`
	WordCount      int    `json:"word_count"`
	FileCount      int    `json:"file_count"`
	ContentHash    string `json:"content_hash" gorm:"type:varchar(64)"`
	SourceRevision string `json:"source_revision" gorm:"type:varchar(128)"`
	Content        []byte `json:"-" gorm:"size:31457280"`
	SourceFiles    []byte `json:"-" gorm:"size:262144"`
	ETag           string `json:"-" gorm:"column:etag;type:varchar(512)"`
	RequestedAt    int64  `json:"-"`
	NextSyncAt     int64  `json:"next_sync_at" gorm:"index"`
	LeaseOwner     string `json:"-" gorm:"type:varchar(64)"`
	LeaseUntil     int64  `json:"-" gorm:"index"`
	Generation     int64  `json:"-"`
	LastSuccessAt  int64  `json:"last_success_at"`
	LastError      string `json:"last_error" gorm:"type:varchar(256)"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

func (row *PromptWordlist) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(row.Action) == "" {
		row.Action = prompt_audit_setting.WordlistActionReview
	} else {
		row.Action = prompt_audit_setting.NormalizeWordlistAction(row.Action)
	}
	if row.Action == prompt_audit_setting.WordlistActionBlock || row.Action == prompt_audit_setting.WordlistActionReview {
		return nil
	}
	return errors.New("invalid wordlist action")
}

func ListPromptWordlists() ([]PromptWordlist, error) {
	rows := make([]PromptWordlist, 0)
	err := DB.Omit("content", "source_files", "etag").Order("id ASC").Find(&rows).Error
	return rows, err
}

func GetPromptWordlist(id int64) (*PromptWordlist, error) {
	var row PromptWordlist
	if err := DB.First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func CreatePromptWordlist(row *PromptWordlist, scopes ...dto.PromptAuditScope) error {
	return updatePromptWordlistBindings(func(tx *gorm.DB, policies map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy) error {
		var count int64
		if err := tx.Model(&PromptWordlist{}).Where("source_hash = ?", row.SourceHash).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrPromptWordlistExists
		}
		if err := tx.Model(&PromptWordlist{}).Count(&count).Error; err != nil {
			return err
		}
		if count >= MaxPromptWordlists {
			return ErrPromptWordlistLimit
		}
		now := common.GetTimestamp()
		row.CreatedAt, row.UpdatedAt, row.RequestedAt = now, now, now
		row.Generation, row.Status = 1, "pending"
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		id := strconv.FormatInt(row.ID, 10)
		for _, scope := range scopes {
			policy := policies[scope]
			if !slices.Contains(policy.LibraryIDs, id) {
				policy.LibraryIDs = append(policy.LibraryIDs, id)
			}
			policies[scope] = policy
		}
		return nil
	})
}

type PromptWordlistUpdate struct {
	Name       *string
	SourceURL  *string
	SourceHash *string
	Scopes     *[]dto.PromptAuditScope
	Enabled    *bool
	Action     *string
	AutoUpdate *bool
}

// Changing a library fences an in-flight download. It can never undo a disable
// or overwrite a newer administrator decision when that download completes.
func UpdatePromptWordlist(id int64, update PromptWordlistUpdate) error {
	if update.Scopes != nil {
		return updatePromptWordlistBindings(func(tx *gorm.DB, policies map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy) error {
			return updatePromptWordlistRow(tx, id, update, policies)
		})
	}
	optionUpdateMu.Lock()
	defer optionUpdateMu.Unlock()
	return DB.Transaction(func(tx *gorm.DB) error {
		// The shared option row also serializes SQLite read/modify/write operations.
		if _, err := lockPromptScopePolicies(tx); err != nil {
			return err
		}
		return updatePromptWordlistRow(tx, id, update, nil)
	})
}

func updatePromptWordlistRow(tx *gorm.DB, id int64, update PromptWordlistUpdate, policies map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy) error {
	var row PromptWordlist
	if err := lockForUpdate(tx).Omit("content", "source_files").First(&row, id).Error; err != nil {
		return err
	}
	if update.Name != nil {
		row.Name = *update.Name
	}
	sourceChanged := update.SourceURL != nil && *update.SourceURL != row.SourceURL
	if sourceChanged {
		if update.SourceHash == nil {
			return errors.New("wordlist source hash is required")
		}
		var count int64
		if err := tx.Model(&PromptWordlist{}).Where("source_hash = ? AND id <> ?", *update.SourceHash, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrPromptWordlistExists
		}
		row.SourceURL, row.SourceHash = *update.SourceURL, *update.SourceHash
	}
	if update.Enabled != nil {
		row.Enabled = *update.Enabled
	}
	if update.Action != nil {
		row.Action = prompt_audit_setting.NormalizeWordlistAction(*update.Action)
		if row.Action != prompt_audit_setting.WordlistActionBlock && row.Action != prompt_audit_setting.WordlistActionReview {
			return errors.New("invalid wordlist action")
		}
	}
	if update.AutoUpdate != nil {
		row.AutoUpdate = *update.AutoUpdate
	}
	if update.Scopes != nil {
		libraryID := strconv.FormatInt(id, 10)
		for _, scope := range dto.PromptAuditScopes() {
			policy := policies[scope]
			selected := slices.Contains(*update.Scopes, scope)
			assigned := slices.Contains(policy.LibraryIDs, libraryID)
			if selected && !assigned {
				policy.LibraryIDs = append(policy.LibraryIDs, libraryID)
			} else if !selected && assigned {
				policy.LibraryIDs = slices.DeleteFunc(policy.LibraryIDs, func(value string) bool { return value == libraryID })
			}
			policies[scope] = policy
		}
	}
	status := row.Status
	if sourceChanged {
		status = "pending"
	} else if status == "updating" {
		status = "pending"
		if row.ContentHash != "" {
			status = "ready"
		}
	}
	requested := row.RequestedAt
	if !row.Enabled {
		requested = 0
	} else if sourceChanged || row.ContentHash == "" || row.Status == "updating" || row.Status == "pending" {
		requested = common.GetTimestamp()
	}
	values := map[string]any{
		"name": row.Name, "enabled": row.Enabled, "action": row.Action, "auto_update": row.AutoUpdate,
		"generation": row.Generation + 1, "lease_owner": "", "lease_until": 0,
		"requested_at": requested, "status": status, "updated_at": common.GetTimestamp(),
	}
	if sourceChanged {
		values["source_url"], values["source_hash"] = row.SourceURL, row.SourceHash
		values["source_revision"], values["etag"], values["next_sync_at"], values["last_error"] = "", "", 0, ""
	}
	return tx.Model(&row).Updates(values).Error
}

func RequestPromptWordlistSync(id int64) error {
	result := DB.Model(&PromptWordlist{}).Where("id = ?", id).Updates(map[string]any{"requested_at": common.GetTimestamp(), "next_sync_at": 0})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		_, err := GetPromptWordlist(id)
		return err
	}
	return nil
}

func DeletePromptWordlist(id int64) error {
	return updatePromptWordlistBindings(func(tx *gorm.DB, policies map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy) error {
		if err := tx.Delete(&PromptWordlist{}, id).Error; err != nil {
			return err
		}
		libraryID := strconv.FormatInt(id, 10)
		for scope, policy := range policies {
			policy.LibraryIDs = slices.DeleteFunc(policy.LibraryIDs, func(value string) bool { return value == libraryID })
			policies[scope] = policy
		}
		return nil
	})
}

// Bindings and libraries share one database transaction and lock. Always merge
// persisted rules, because another node may have saved newer bindings.
func updatePromptWordlistBindings(mutate func(*gorm.DB, map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy) error) error {
	optionUpdateMu.Lock()
	defer optionUpdateMu.Unlock()
	var encoded string
	if err := DB.Transaction(func(tx *gorm.DB) error {
		option, err := lockPromptScopePolicies(tx)
		if err != nil {
			return err
		}
		var configured prompt_audit_setting.PromptAuditSetting
		if err := common.UnmarshalJsonStr(option.Value, &configured.ScopePolicies); err != nil {
			return err
		}
		policies := configured.EffectiveScopePolicies()
		if err := mutate(tx, policies); err != nil {
			return err
		}
		data, err := common.Marshal(policies)
		if err != nil {
			return err
		}
		encoded = string(data)
		return tx.Model(option).Update("value", encoded).Error
	}); err != nil {
		return err
	}
	return updateOptionMap(promptScopePoliciesKey, encoded)
}

func lockPromptScopePolicies(tx *gorm.DB) (*Option, error) {
	option := &Option{Key: promptScopePoliciesKey, Value: "null"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(option).Error; err != nil {
		return nil, err
	}
	if err := lockForUpdate(tx).Where(&Option{Key: promptScopePoliciesKey}).First(option).Error; err != nil {
		return nil, err
	}
	return option, nil
}

func validatePromptWordlistBindings(tx *gorm.DB, value string) error {
	if _, err := lockPromptScopePolicies(tx); err != nil {
		return err
	}
	var policies map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy
	if err := common.UnmarshalJsonStr(value, &policies); err != nil {
		return err
	}
	var ids []int64
	if err := tx.Model(&PromptWordlist{}).Pluck("id", &ids).Error; err != nil {
		return err
	}
	known := map[string]bool{prompt_audit_setting.ManualWordlistID: true}
	for _, id := range ids {
		known[strconv.FormatInt(id, 10)] = true
	}
	for _, policy := range policies {
		for _, id := range policy.LibraryIDs {
			if !known[id] {
				return errors.New("unknown wordlist")
			}
		}
	}
	return nil
}

func ClaimPromptWordlist(owner string, now int64) (*PromptWordlist, error) {
	var row PromptWordlist
	err := DB.Where("lease_until <= ? AND (requested_at > 0 OR status = ? OR (enabled = ? AND auto_update = ? AND next_sync_at <= ?))", now, "updating", true, true, now).
		Order("id ASC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := DB.Model(&PromptWordlist{}).Where("id = ? AND generation = ? AND lease_until <= ? AND (requested_at > 0 OR status = ? OR (enabled = ? AND auto_update = ? AND next_sync_at <= ?))", row.ID, row.Generation, now, "updating", true, true, now).
		Updates(map[string]any{"lease_owner": owner, "lease_until": now + 180, "requested_at": 0, "status": "updating"})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	row.LeaseOwner = owner
	return &row, nil
}

func FinishPromptWordlist(row *PromptWordlist, values map[string]any) (bool, error) {
	now := common.GetTimestamp()
	values["lease_owner"], values["lease_until"], values["updated_at"] = "", 0, now
	result := DB.Model(&PromptWordlist{}).Where("id = ? AND generation = ? AND lease_owner = ? AND lease_until > ?", row.ID, row.Generation, row.LeaseOwner, now).Updates(values)
	return result.RowsAffected > 0, result.Error
}

// Refresh only prompt inspection options so policy changes reach every node
// quickly without changing the global settings synchronization interval.
func SyncPromptInspectionOptions() error {
	optionUpdateMu.Lock()
	defer optionUpdateMu.Unlock()
	var rows []Option
	if err := DB.Where(clause.Or(
		clause.Like{Column: clause.Column{Name: "key"}, Value: "prompt_audit.%"},
		clause.IN{Column: clause.Column{Name: "key"}, Values: []any{"SensitiveWords", "CheckSensitiveEnabled", "CheckSensitiveOnPromptEnabled"}},
	)).Find(&rows).Error; err != nil {
		return err
	}
	fields := map[string]string{}
	for _, row := range rows {
		if field, ok := strings.CutPrefix(row.Key, "prompt_audit."); ok {
			fields[field] = row.Value
			continue
		}
		if err := updateOptionMap(row.Key, row.Value); err != nil {
			return err
		}
	}
	if len(fields) != 0 {
		_, err := config.GlobalConfig.UpdateFromDB("prompt_audit", fields)
		return err
	}
	return nil
}
