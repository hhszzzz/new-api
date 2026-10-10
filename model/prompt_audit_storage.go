package model

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type PromptAuditStorageStats struct {
	SharedEnabled   bool  `json:"shared_enabled"`
	LocalDays       int   `json:"local_days"`
	ArchiveRequired bool  `json:"archive_required"`
	RawBytes        int64 `json:"raw_bytes"`
	StoredBytes     int64 `json:"stored_bytes"`
	LegacyBytes     int64 `json:"legacy_bytes"`
	ContentBlocks   int64 `json:"content_blocks"`
	References      int64 `json:"references"`
	NewBytes24H     int64 `json:"new_bytes_24h"`
	ArchiveBacklog  int64 `json:"archive_backlog"`
	Unmigrated      int64 `json:"unmigrated"`
	ImportedBytes   int64 `json:"imported_bytes"`
	CacheLimitBytes int64 `json:"cache_limit_bytes"`
	CacheTTLSeconds int64 `json:"cache_ttl_seconds"`
}

func GetPromptAuditStorageStats(db *gorm.DB) (PromptAuditStorageStats, error) {
	stats := PromptAuditStorageStats{LocalDays: 7, ArchiveRequired: true, CacheLimitBytes: PromptAuditImportCacheBytes, CacheTTLSeconds: PromptAuditImportTTL}
	if db.Migrator().HasTable(&Option{}) {
		var options []Option
		if err := db.Where(map[string]any{"key": []string{"prompt_audit.shared_content_enabled", "prompt_audit.retention_days"}}).Find(&options).Error; err != nil {
			return stats, err
		}
		legacyDays := 0
		for _, option := range options {
			if option.Key == "prompt_audit.shared_content_enabled" {
				stats.SharedEnabled = option.Value == "true"
			} else {
				legacyDays, _ = strconv.Atoi(option.Value)
			}
		}
		if !stats.SharedEnabled {
			stats.LocalDays, stats.ArchiveRequired = legacyDays, false
		}
	}
	var sizes struct {
		Raw    int64
		Stored int64 `gorm:"column:compressed_size_total"`
		Count  int64
	}
	if err := db.Model(&PromptAuditContent{}).Where("data IS NOT NULL").Select("COALESCE(SUM(raw_bytes), 0) AS raw, COALESCE(SUM(stored_bytes), 0) AS compressed_size_total, COUNT(*) AS count").Scan(&sizes).Error; err != nil {
		return stats, err
	}
	stats.RawBytes, stats.StoredBytes, stats.ContentBlocks = sizes.Raw, sizes.Stored, sizes.Count
	if err := db.Model(&PromptAuditContentRef{}).Where("active = ?", true).Count(&stats.References).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&PromptAuditContent{}).Where("data IS NOT NULL AND created_at >= ?", common.GetTimestamp()-86400).Select("COALESCE(SUM(stored_bytes), 0)").Scan(&stats.NewBytes24H).Error; err != nil {
		return stats, err
	}
	legacySize := "COALESCE(SUM(COALESCE(" + promptAuditTextBytesExpression(db, "full_prompt") + ", 0) + COALESCE(" + promptAuditTextBytesExpression(db, "scan_payload") + ", 0) + COALESCE(" + promptAuditTextBytesExpression(db, "content_snapshot") + ", 0)), 0)"
	if err := db.Model(&PromptAudit{}).Select(legacySize).Scan(&stats.LegacyBytes).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&PromptAudit{}).Where("(content_manifest IS NULL OR content_manifest = '') AND (full_prompt IS NOT NULL OR scan_payload IS NOT NULL OR content_snapshot IS NOT NULL)").Count(&stats.Unmigrated).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&PromptAuditImportedContent{}).Where("expires_at > ?", common.GetTimestamp()).Select("COALESCE(SUM(stored_bytes), 0)").Scan(&stats.ImportedBytes).Error; err != nil {
		return stats, err
	}
	var metadataBytes int64
	if err := db.Model(&PromptAuditImportedRecord{}).Where("expires_at > ?", common.GetTimestamp()).Select("COALESCE(SUM(" + promptAuditTextBytesExpression(db, "record") + "), 0)").Scan(&metadataBytes).Error; err != nil {
		return stats, err
	}
	stats.ImportedBytes += metadataBytes
	// A fingerprint catches changes within the same second as the previous export.
	var after int64
	for {
		var rows []PromptAudit
		if err := db.Omit("full_prompt", "scan_payload", "content_snapshot").Where("id > ? AND content_manifest IS NOT NULL AND content_manifest <> ''", after).Order("id asc").Limit(200).Find(&rows).Error; err != nil {
			return stats, err
		}
		if len(rows) == 0 {
			break
		}
		for _, audit := range rows {
			after = audit.ID
			fingerprint, err := promptAuditArchiveFingerprint(audit)
			if err != nil {
				return stats, err
			}
			if fingerprint != audit.ArchivedDigest {
				stats.ArchiveBacklog++
			}
		}
	}
	return stats, nil
}

func ListPromptAuditArchives(db *gorm.DB) ([]PromptAuditArchive, error) {
	var result []PromptAuditArchive
	err := db.Omit("manifest").Order("created_at desc, id desc").Limit(400).Find(&result).Error
	return result, err
}

func ListPromptAuditArchiveImports(db *gorm.DB) ([]PromptAuditArchiveImport, error) {
	var result []PromptAuditArchiveImport
	err := db.Where("expires_at > ?", common.GetTimestamp()).Order("imported_at desc").Find(&result).Error
	return result, err
}

func GetPromptAuditImportedEvent(db *gorm.DB, source string, id int64, full bool) (PromptAuditResponse, error) {
	var stored PromptAuditImportedRecord
	if err := db.Where("source_id = ? AND audit_id = ? AND expires_at > ?", source, id, common.GetTimestamp()).First(&stored).Error; err != nil {
		return PromptAuditResponse{}, err
	}
	var record promptAuditArchiveRecord
	if err := common.UnmarshalJsonStr(string(stored.Record), &record); err != nil {
		return PromptAuditResponse{}, err
	}
	audit := record.event()
	audit.ContentEvicted, audit.ContentState = true, "imported"
	if full {
		if err := LoadPromptAuditContent(db, &audit, false); err != nil {
			if !errors.Is(err, ErrPromptAuditContentUnavailable) {
				return PromptAuditResponse{}, err
			}
			audit.ContentState = "missing"
		}
	}
	response := audit.ToResponse(full)
	if !full {
		response.FullPromptAvailable = true
	}
	return response, nil
}

func ListPromptAuditImportedEvents(db *gorm.DB, source string, anchor int64, page, pageSize int) ([]PromptAuditResponse, int64, error) {
	query := db.Model(&PromptAuditImportedRecord{}).Where("source_id = ? AND expires_at > ?", source, common.GetTimestamp())
	if anchor > 0 {
		var record PromptAuditImportedRecord
		if err := query.Where("audit_id = ?", anchor).First(&record).Error; err != nil {
			return nil, 0, err
		}
		// Association is scoped to the original user and source database.
		query = db.Model(&PromptAuditImportedRecord{}).Where("source_id = ? AND user_id = ? AND expires_at > ?", source, record.UserID, common.GetTimestamp())
		switch {
		case record.SessionKey != "":
			query = query.Where("session_key = ?", record.SessionKey)
		case record.RequestID != "":
			query = query.Where("request_id = ?", record.RequestID)
		case record.GenerationID != "":
			query = query.Where("generation_id = ?", record.GenerationID)
		default:
			query = query.Where("audit_id = ?", anchor)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []PromptAuditImportedRecord
	size := min(max(pageSize, 1), 200)
	if err := query.Order("created_at asc, audit_id asc").Offset((max(page, 1) - 1) * size).Limit(size).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	result := make([]PromptAuditResponse, 0, len(records))
	for _, stored := range records {
		var record promptAuditArchiveRecord
		if err := common.UnmarshalJsonStr(string(stored.Record), &record); err != nil {
			return nil, 0, err
		}
		audit := record.event()
		audit.ContentState = "imported"
		response := audit.ToResponse(false)
		response.FullPromptAvailable = true
		result = append(result, response)
	}
	return result, total, nil
}

func PromptAuditArchivesNeededForExport(db *gorm.DB) ([]PromptAuditArchive, error) {
	needed := make(map[string]bool)
	var after int64
	for {
		var rows []PromptAudit
		if err := db.Omit("full_prompt", "scan_payload", "content_snapshot").Where("id > ? AND content_manifest IS NOT NULL AND content_manifest <> ''", after).Order("id asc").Limit(200).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		for _, audit := range rows {
			after = audit.ID
			fingerprint, err := promptAuditArchiveFingerprint(audit)
			if err != nil {
				return nil, err
			}
			if fingerprint == audit.ArchivedDigest && !slices.Contains(promptAuditActiveStatuses(), audit.Status) {
				continue
			}
			if audit.ContentEvicted && audit.ArchivedBundleID != "" {
				needed[audit.ArchivedBundleID] = true
			}
			if audit.RelatedInputID <= 0 {
				continue
			}
			var input PromptAudit
			err = db.Select("content_evicted", "archived_bundle_id").Where("id = ?", audit.RelatedInputID).First(&input).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			if err == nil && input.ContentEvicted && input.ArchivedBundleID != "" {
				needed[input.ArchivedBundleID] = true
			}
		}
	}
	ids := make([]string, 0, len(needed))
	for id := range needed {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []PromptAuditArchive{}, nil
	}
	var archives []PromptAuditArchive
	if err := db.Omit("manifest").Where("id IN ? AND status = ?", ids, "verified").Find(&archives).Error; err != nil {
		return nil, err
	}
	if len(archives) != len(ids) {
		return nil, fmt.Errorf("a previous archive needed for export is not verified")
	}
	return archives, nil
}

func promptAuditTextBytesExpression(db *gorm.DB, column string) string {
	if db.Dialector.Name() == "sqlite" {
		return "LENGTH(CAST(" + column + " AS BLOB))"
	}
	return "OCTET_LENGTH(" + column + ")"
}

func MigratePromptAuditStorage(db *gorm.DB) error {
	if err := db.AutoMigrate(&PromptAudit{}); err != nil {
		return err
	}
	if err := ensurePromptAuditStorageTables(db); err != nil {
		return err
	}
	_, err := PromptAuditStorageSource(db)
	return err
}
