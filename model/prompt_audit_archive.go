package model

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/klauspost/compress/zstd"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PromptAuditImportCacheBytes int64 = 2 << 30
const PromptAuditImportTTL int64 = 24 * 60 * 60

type PromptAuditStorageState struct {
	ID             int    `gorm:"primaryKey;autoIncrement:false"`
	SourceID       string `gorm:"type:varchar(64)"`
	ExportSequence int64
}

type PromptAuditArchive struct {
	ID          string   `gorm:"type:varchar(64);primaryKey" json:"id"`
	SourceID    string   `gorm:"type:varchar(64);index" json:"source_id"`
	Day         string   `gorm:"type:varchar(10);index" json:"day"`
	Status      string   `gorm:"type:varchar(16);index" json:"status"`
	CreatedAt   int64    `json:"created_at"`
	Sequence    int64    `json:"sequence"`
	VerifiedAt  int64    `json:"verified_at"`
	RecordCount int64    `json:"record_count"`
	BlobCount   int64    `json:"blob_count"`
	Bytes       int64    `json:"bytes"`
	Digest      string   `gorm:"type:varchar(64)" json:"digest"`
	Manifest    LongText `json:"-"`
	Volumes     LongText `json:"-"`
}

type PromptAuditArchiveVolume struct {
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
	RemotePath string `json:"remote_path"`
}

type PromptAuditArchiveImport struct {
	ID          string `gorm:"type:varchar(64);primaryKey" json:"id"`
	SourceID    string `gorm:"type:varchar(64);index" json:"source_id"`
	Day         string `gorm:"type:varchar(10)" json:"day"`
	RecordCount int64  `json:"record_count"`
	ImportedAt  int64  `json:"imported_at"`
	ExpiresAt   int64  `gorm:"index" json:"expires_at"`
}

// Viewing imported metadata must never rewrite an audit verdict or enqueue work.
type PromptAuditImportedRecord struct {
	SourceID     string `gorm:"type:varchar(64);primaryKey"`
	AuditID      int64  `gorm:"primaryKey;autoIncrement:false"`
	ArchiveID    string `gorm:"type:varchar(64);index"`
	Record       LongText
	SnapshotAt   int64
	ExpiresAt    int64  `gorm:"index"`
	SessionKey   string `gorm:"type:varchar(64);index"`
	GroupKey     string `gorm:"type:varchar(64);index"`
	RequestID    string `gorm:"type:varchar(64);index"`
	GenerationID string `gorm:"type:varchar(128);index"`
	UserID       int    `gorm:"index"`
	Direction    string `gorm:"type:varchar(16)"`
	CreatedAt    int64  `gorm:"index"`
}

type promptAuditArchiveRecord struct {
	Audit             PromptAudit `json:"audit"`
	Manifest          LongText    `json:"manifest"`
	InspectedScopes   string      `json:"inspected_scopes"`
	PolicyCategories  string      `json:"policy_categories"`
	PolicySnapshot    string      `json:"policy_snapshot"`
	Categories        string      `json:"categories"`
	UnknownCategories string      `json:"unknown_categories"`
	ReviewCodes       string      `json:"review_codes"`
	Scores            string      `json:"scores"`
	LeaseOwner        string      `json:"lease_owner"`
	LeaseUntil        int64       `json:"lease_until"`
}

func promptAuditRecordForArchive(audit PromptAudit) promptAuditArchiveRecord {
	return promptAuditArchiveRecord{Audit: audit, Manifest: audit.ContentManifest, InspectedScopes: audit.InspectedScopes,
		PolicyCategories: audit.PolicyCategories, PolicySnapshot: audit.PolicySnapshot, Categories: audit.Categories,
		UnknownCategories: audit.UnknownCategories, ReviewCodes: audit.ReviewCodes, Scores: audit.Scores, LeaseOwner: audit.LeaseOwner, LeaseUntil: audit.LeaseUntil}
}

func (record promptAuditArchiveRecord) event() PromptAudit {
	audit := record.Audit
	audit.ContentManifest, audit.InspectedScopes = record.Manifest, record.InspectedScopes
	audit.PolicyCategories, audit.PolicySnapshot = record.PolicyCategories, record.PolicySnapshot
	audit.Categories, audit.UnknownCategories, audit.ReviewCodes, audit.Scores = record.Categories, record.UnknownCategories, record.ReviewCodes, record.Scores
	audit.LeaseOwner, audit.LeaseUntil = record.LeaseOwner, record.LeaseUntil
	return audit
}

func promptAuditArchiveFingerprint(audit PromptAudit) (string, error) {
	data, err := common.Marshal(promptAuditRecordForArchive(audit))
	if err != nil {
		return "", err
	}
	return promptAuditContentHash(data), nil
}

type promptAuditArchiveEntry struct {
	Name     string `json:"name"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
	RawBytes int64  `json:"raw_bytes,omitempty"`
}

type promptAuditArchiveManifest struct {
	Version          int                       `json:"version"`
	ID               string                    `json:"id"`
	SourceID         string                    `json:"source_id"`
	Day              string                    `json:"day"`
	CreatedAt        int64                     `json:"created_at"`
	Sequence         int64                     `json:"sequence"`
	Entries          []promptAuditArchiveEntry `json:"entries"`
	RecoveryComplete bool                      `json:"recovery_complete"`
}

func PromptAuditStorageSource(db *gorm.DB) (string, error) {
	state := PromptAuditStorageState{ID: 1, SourceID: common.GetUUID()}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
		return "", err
	}
	if err := db.First(&state, 1).Error; err != nil {
		return "", err
	}
	return state.SourceID, nil
}

// The caller can hold a repeatable-read snapshot open for pg_dump. Export includes
// every active work item and every changed event, including late completions.
func ReservePromptAuditArchiveVersion(db *gorm.DB) (int64, error) {
	var version int64
	err := db.Transaction(func(tx *gorm.DB) error {
		var state PromptAuditStorageState
		if err := lockForUpdate(tx).First(&state, 1).Error; err != nil {
			return err
		}
		version = state.ExportSequence + 1
		return tx.Model(&PromptAuditStorageState{}).Where("id = ?", 1).Update("export_sequence", version).Error
	})
	return version, err
}

func WritePromptAuditArchive(tx *gorm.DB, writer io.Writer, sourceID, day string, sequence int64, full bool) (PromptAuditArchive, error) {
	archive := PromptAuditArchive{ID: common.GetUUID(), SourceID: sourceID, Day: day, Sequence: sequence, Status: "ready", CreatedAt: common.GetTimestamp()}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return archive, err
	}
	var legacyBodies int64
	legacySize := "COALESCE(" + promptAuditTextBytesExpression(tx, "full_prompt") + ", 0) + COALESCE(" + promptAuditTextBytesExpression(tx, "scan_payload") + ", 0) + COALESCE(" + promptAuditTextBytesExpression(tx, "content_snapshot") + ", 0)"
	if err := tx.Model(&PromptAudit{}).Where("(" + legacySize + ") > 0").Count(&legacyBodies).Error; err != nil {
		return archive, err
	}
	if legacyBodies != 0 {
		return archive, errors.New("migrate legacy audit bodies before exporting an archive")
	}
	manifest := promptAuditArchiveManifest{Version: 1, ID: archive.ID, SourceID: sourceID, Day: day, CreatedAt: archive.CreatedAt, Sequence: sequence, RecoveryComplete: true}
	encoder, err := zstd.NewWriter(writer, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return archive, err
	}
	defer encoder.Close()
	pack := tar.NewWriter(encoder)
	defer pack.Close()
	hashes := make(map[string]bool)
	records := make(map[int64]bool)
	var verifiedArchives []PromptAuditArchive
	if err := tx.Select("id").Where("status = ?", "verified").Find(&verifiedArchives).Error; err != nil {
		return archive, err
	}
	verified := make(map[string]bool, len(verifiedArchives))
	for _, item := range verifiedArchives {
		verified[item.ID] = true
	}
	var related []int64
	var after int64
	for {
		var rows []PromptAudit
		if err := tx.Omit("full_prompt", "scan_payload", "content_snapshot").Where("id > ?", after).Order("id asc").Limit(200).Find(&rows).Error; err != nil {
			return archive, err
		}
		if len(rows) == 0 {
			break
		}
		for _, audit := range rows {
			after = audit.ID
			if audit.ContentManifest == "" {
				if slices.Contains(promptAuditActiveStatuses(), audit.Status) {
					return archive, errors.New("active audit is missing its recovery payload")
				}
				continue
			}
			fingerprint, err := promptAuditArchiveFingerprint(audit)
			if err != nil {
				return archive, err
			}
			active := slices.Contains(promptAuditActiveStatuses(), audit.Status)
			if !full && !active && fingerprint == audit.ArchivedDigest && verified[audit.ArchivedBundleID] {
				continue
			}
			if err := writePromptAuditArchiveRecord(pack, audit, &manifest, hashes); err != nil {
				return archive, err
			}
			records[audit.ID] = true
			if audit.RelatedInputID > 0 {
				related = append(related, audit.RelatedInputID)
			}
		}
	}
	for _, id := range related {
		if records[id] {
			continue
		}
		var input PromptAudit
		if err := tx.Where("id = ?", id).First(&input).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Explicitly deleted request metadata must not block the remaining
				// reply's archive. Its work view still carries every context reference.
				continue
			}
			return archive, err
		}
		if input.ContentManifest == "" {
			return archive, errors.New("related request has not been migrated")
		}
		if err := writePromptAuditArchiveRecord(pack, input, &manifest, hashes); err != nil {
			return archive, err
		}
		records[id] = true
	}
	keys := make([]string, 0, len(hashes))
	for hash := range hashes {
		keys = append(keys, hash)
	}
	slices.Sort(keys)
	for _, hash := range keys {
		var blob PromptAuditContent
		err := tx.Where("hash = ? AND data IS NOT NULL", hash).First(&blob).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var imported PromptAuditImportedContent
			err = tx.Where("hash = ? AND expires_at > ?", hash, common.GetTimestamp()).First(&imported).Error
			blob = PromptAuditContent{Hash: imported.Hash, Data: imported.Data, RawBytes: imported.RawBytes}
		}
		if err != nil {
			return archive, fmt.Errorf("archive body %s is missing; import the previous day package: %w", hash, err)
		}
		if err := verifyPromptAuditCompressedContent(hash, blob.Data, blob.RawBytes); err != nil {
			return archive, err
		}
		name := "blobs/" + hash + ".zst"
		if err := writePromptAuditTarEntry(pack, name, blob.Data); err != nil {
			return archive, err
		}
		manifest.Entries = append(manifest.Entries, promptAuditArchiveEntry{Name: name, Bytes: int64(len(blob.Data)), SHA256: promptAuditContentHash(blob.Data), RawBytes: blob.RawBytes})
	}
	archive.RecordCount, archive.BlobCount = int64(len(records)), int64(len(keys))
	data, err := common.Marshal(manifest)
	if err != nil {
		return archive, err
	}
	archive.Manifest = LongText(data)
	if err := writePromptAuditTarEntry(pack, "manifest.json", data); err != nil {
		return archive, err
	}
	if err := pack.Close(); err != nil {
		return archive, err
	}
	if err := encoder.Close(); err != nil {
		return archive, err
	}
	return archive, nil
}

func writePromptAuditArchiveRecord(pack *tar.Writer, audit PromptAudit, manifest *promptAuditArchiveManifest, hashes map[string]bool) error {
	var content promptAuditContentManifest
	if err := common.UnmarshalJsonStr(string(audit.ContentManifest), &content); err != nil {
		return err
	}
	if err := validatePromptAuditManifest(content); err != nil {
		return err
	}
	for _, hash := range promptAuditManifestRefs(content) {
		hashes[hash] = true
	}
	data, err := common.Marshal(promptAuditRecordForArchive(audit))
	if err != nil {
		return err
	}
	name := "records/" + strconv.FormatInt(audit.ID, 10) + ".json"
	if err := writePromptAuditTarEntry(pack, name, data); err != nil {
		return err
	}
	manifest.Entries = append(manifest.Entries, promptAuditArchiveEntry{Name: name, Bytes: int64(len(data)), SHA256: promptAuditContentHash(data)})
	return nil
}

func writePromptAuditTarEntry(pack *tar.Writer, name string, data []byte) error {
	if err := pack.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := pack.Write(data)
	return err
}

func validatePromptAuditManifest(manifest promptAuditContentManifest) error {
	if manifest.Version != 1 {
		return errors.New("unsupported prompt audit manifest version")
	}
	for _, hash := range promptAuditManifestRefs(manifest) {
		if err := validatePromptAuditHash(hash); err != nil {
			return err
		}
	}
	return nil
}

func verifyPromptAuditCompressedContent(hash string, data []byte, rawBytes int64) error {
	if rawBytes < 0 || rawBytes == int64(^uint64(0)>>1) {
		return errors.New("invalid content length")
	}
	count, err := promptAuditCompressedContentSize(hash, data)
	if err != nil {
		return err
	}
	if count != rawBytes {
		return errors.New("archive content length mismatch")
	}
	return nil
}

func promptAuditCompressedContentSize(hash string, data []byte) (int64, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(data), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(128<<20))
	if err != nil {
		return 0, err
	}
	defer decoder.Close()
	digest := sha256.New()
	count, err := io.Copy(digest, decoder)
	if err != nil {
		return 0, err
	}
	if hex.EncodeToString(digest.Sum(nil)) != hash {
		return 0, errors.New("archive content checksum mismatch")
	}
	return count, nil
}

// Validation and import use the same parser. Until the final checksum manifest
// and all references are checked, the transaction remains uncommitted.
func ImportPromptAuditArchive(db *gorm.DB, reader io.Reader, restore bool) (PromptAuditArchiveImport, error) {
	var result PromptAuditArchiveImport
	err := db.Transaction(func(tx *gorm.DB) error {
		var state PromptAuditStorageState
		if err := lockForUpdate(tx).First(&state, 1).Error; err != nil {
			return err
		}
		now, expires := common.GetTimestamp(), common.GetTimestamp()+PromptAuditImportTTL
		if !restore {
			if err := cleanupPromptAuditImportCache(tx, now); err != nil {
				return err
			}
		}
		var used struct {
			Stored int64 `gorm:"column:compressed_size_total"`
		}
		if !restore {
			if err := tx.Model(&PromptAuditImportedContent{}).Select("COALESCE(SUM(stored_bytes), 0) AS compressed_size_total").Scan(&used).Error; err != nil {
				return err
			}
		}
		decoder, err := zstd.NewReader(reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(128<<20))
		if err != nil {
			return err
		}
		defer decoder.Close()
		pack := tar.NewReader(decoder)
		entries := make(map[string]promptAuditArchiveEntry)
		records := make(map[int64]bool)
		// Stage record metadata in the uncommitted transaction. Retaining every
		// record in memory can exhaust a small host when several days are imported.
		stagingSource := "import-" + common.GetUUID()
		var manifest promptAuditArchiveManifest
		var readBytes int64
		for {
			header, err := pack.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > PromptAuditImportCacheBytes {
				return errors.New("invalid archive entry")
			}
			readBytes += header.Size
			if !restore && readBytes > PromptAuditImportCacheBytes {
				return errors.New("archive exceeds the 2 GiB viewing cache limit")
			}
			if _, exists := entries[header.Name]; exists || manifest.ID != "" {
				return errors.New("duplicate entry or trailing data in archive")
			}
			data, err := io.ReadAll(io.LimitReader(pack, header.Size+1))
			if err != nil || int64(len(data)) != header.Size {
				return errors.New("truncated archive entry")
			}
			entry := promptAuditArchiveEntry{Name: header.Name, Bytes: header.Size, SHA256: promptAuditContentHash(data)}
			if header.Name == "manifest.json" {
				if err := common.Unmarshal(data, &manifest); err != nil {
					return err
				}
				continue
			}
			if value, ok := strings.CutPrefix(header.Name, "records/"); ok {
				value, ok = strings.CutSuffix(value, ".json")
				id, err := strconv.ParseInt(value, 10, 64)
				if !ok || err != nil || id <= 0 || len(data) > 16<<20 {
					return errors.New("invalid archive record")
				}
				var record promptAuditArchiveRecord
				if err := common.Unmarshal(data, &record); err != nil || record.Audit.ID != id {
					return errors.New("invalid audit identity")
				}
				var content promptAuditContentManifest
				if err := common.UnmarshalJsonStr(string(record.Manifest), &content); err != nil {
					return err
				}
				if err := validatePromptAuditManifest(content); err != nil {
					return err
				}
				if err := tx.Create(&PromptAuditImportedRecord{SourceID: stagingSource, AuditID: id, Record: LongText(data), ExpiresAt: now}).Error; err != nil {
					return err
				}
				records[id] = true
			} else if value, ok := strings.CutPrefix(header.Name, "blobs/"); ok {
				hash, ok := strings.CutSuffix(value, ".zst")
				if !ok {
					return errors.New("invalid archive blob")
				}
				if err := validatePromptAuditHash(hash); err != nil {
					return err
				}
				entry.RawBytes, err = promptAuditCompressedContentSize(hash, data)
				if err != nil {
					return err
				}
				// Content identity is checked before writing, and the final checksum
				// manifest is validated before the transaction can commit.
				if restore {
					blob := PromptAuditContent{Hash: hash, Data: data, RawBytes: entry.RawBytes, StoredBytes: int64(len(data)), CreatedAt: now}
					if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&blob).Error; err != nil {
						return err
					}
					if err := tx.Model(&PromptAuditContent{}).Where("hash = ? AND data IS NULL", hash).Updates(map[string]any{"data": data, "raw_bytes": entry.RawBytes, "stored_bytes": int64(len(data))}).Error; err != nil {
						return err
					}
				} else {
					var existing PromptAuditImportedContent
					err := tx.Select("hash", "raw_bytes").Where("hash = ?", hash).First(&existing).Error
					if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
						return err
					}
					if err == nil {
						if existing.RawBytes != entry.RawBytes {
							return errors.New("cached content identity mismatch")
						}
						if err := tx.Model(&PromptAuditImportedContent{}).Where("hash = ?", hash).Update("expires_at", expires).Error; err != nil {
							return err
						}
					} else {
						used.Stored += int64(len(data))
						if used.Stored > PromptAuditImportCacheBytes {
							return errors.New("viewing cache is full (2 GiB)")
						}
						blob := PromptAuditImportedContent{Hash: hash, Data: data, RawBytes: entry.RawBytes, StoredBytes: int64(len(data)), ExpiresAt: expires}
						if err := tx.Create(&blob).Error; err != nil {
							return err
						}
					}
				}
			} else {
				return errors.New("unexpected archive path")
			}
			entries[header.Name] = entry
		}
		if trailing, err := io.Copy(io.Discard, decoder); err != nil || trailing != 0 {
			return errors.New("truncated compression frame or trailing archive data")
		}
		if manifest.Version != 1 || !manifest.RecoveryComplete || manifest.ID == "" || manifest.SourceID == "" || manifest.Sequence <= 0 || len(manifest.Entries) != len(entries) {
			return errors.New("missing or invalid archive checksum manifest")
		}
		seen := make(map[string]bool)
		var existingEvents int64
		if restore {
			if err := tx.Model(&PromptAudit{}).Count(&existingEvents).Error; err != nil {
				return err
			}
			if existingEvents > 0 && state.SourceID != manifest.SourceID {
				return errors.New("archive source does not match the database snapshot")
			}
			if existingEvents == 0 {
				if err := tx.Model(&PromptAuditStorageState{}).Where("id = ?", 1).Updates(map[string]any{"source_id": manifest.SourceID, "export_sequence": manifest.Sequence}).Error; err != nil {
					return err
				}
			}
		}
		for _, expected := range manifest.Entries {
			actual, exists := entries[expected.Name]
			if !exists || seen[expected.Name] || actual.Bytes != expected.Bytes || actual.SHA256 != expected.SHA256 {
				return errors.New("archive checksum mismatch")
			}
			seen[expected.Name] = true
			if value, ok := strings.CutPrefix(expected.Name, "blobs/"); ok {
				hash, _ := strings.CutSuffix(value, ".zst")
				if actual.RawBytes != expected.RawBytes {
					return fmt.Errorf("archive content length mismatch: %s", hash)
				}
			}
		}
		for id := range records {
			var staged PromptAuditImportedRecord
			if err := tx.Select("record").Where("source_id = ? AND audit_id = ?", stagingSource, id).First(&staged).Error; err != nil {
				return err
			}
			data := []byte(staged.Record)
			var record promptAuditArchiveRecord
			if err := common.Unmarshal(data, &record); err != nil {
				return err
			}
			audit := record.event()
			var content promptAuditContentManifest
			if err := common.UnmarshalJsonStr(string(record.Manifest), &content); err != nil {
				return err
			}
			for _, hash := range promptAuditManifestRefs(content) {
				if !seen["blobs/"+hash+".zst"] {
					return errors.New("archive is not independently recoverable: a referenced body is missing")
				}
			}
			if restore {
				var current PromptAudit
				err := tx.Where("id = ?", id).First(&current).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if existingEvents > 0 {
						continue
					}
					if err := tx.Create(&audit).Error; err != nil {
						return err
					}
					current = audit
				} else if err != nil {
					return err
				}
				// A database snapshot's newer metadata always wins over an older day package.
				if current.ContentManifest == "" {
					if err := tx.Model(&PromptAudit{}).Where("id = ?", id).UpdateColumn("content_manifest", record.Manifest).Error; err != nil {
						return err
					}
					current.ContentManifest = record.Manifest
				}
				if err := syncPromptAuditContentRefs(tx, &current); err != nil {
					return err
				}
			} else {
				var current PromptAuditImportedRecord
				err := tx.Where("source_id = ? AND audit_id = ?", manifest.SourceID, id).First(&current).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err == nil && current.SnapshotAt > manifest.Sequence {
					continue
				}
				item := PromptAuditImportedRecord{SourceID: manifest.SourceID, AuditID: id, ArchiveID: manifest.ID, Record: LongText(data), SnapshotAt: manifest.Sequence, ExpiresAt: expires, SessionKey: audit.SessionKey, GroupKey: audit.GroupKey, RequestID: audit.RequestID, GenerationID: audit.GenerationID, UserID: audit.UserID, Direction: audit.Direction, CreatedAt: audit.CreatedAt}
				if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}, {Name: "audit_id"}}, DoUpdates: clause.AssignmentColumns([]string{"archive_id", "record", "snapshot_at", "expires_at", "session_key", "group_key", "request_id", "generation_id", "user_id", "direction", "created_at"})}).Create(&item).Error; err != nil {
					return err
				}
			}
		}
		result = PromptAuditArchiveImport{ID: manifest.ID, SourceID: manifest.SourceID, Day: manifest.Day, RecordCount: int64(len(records)), ImportedAt: now, ExpiresAt: expires}
		if err := tx.Where("source_id = ?", stagingSource).Delete(&PromptAuditImportedRecord{}).Error; err != nil {
			return err
		}
		if !restore {
			var cacheBytes int64
			if err := tx.Model(&PromptAuditImportedContent{}).Select("COALESCE(SUM(stored_bytes), 0)").Scan(&cacheBytes).Error; err != nil {
				return err
			}
			var metadataBytes int64
			if err := tx.Model(&PromptAuditImportedRecord{}).Select("COALESCE(SUM(" + promptAuditTextBytesExpression(tx, "record") + "), 0)").Scan(&metadataBytes).Error; err != nil {
				return err
			}
			if cacheBytes+metadataBytes > PromptAuditImportCacheBytes {
				return errors.New("viewing cache including metadata exceeds 2 GiB")
			}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"imported_at", "expires_at"})}).Create(&result).Error
		}
		if tx.Dialector.Name() == "postgres" && existingEvents == 0 {
			var maxID int64
			if err := tx.Model(&PromptAudit{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
				return err
			}
			if maxID > 0 {
				var sequence string
				if err := tx.Raw("SELECT pg_get_serial_sequence(?, 'id')", tx.NamingStrategy.TableName("PromptAudit")).Scan(&sequence).Error; err != nil {
					return err
				}
				if err := tx.Exec("SELECT setval(?::regclass, ?, true)", sequence, maxID).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelSerializable})
	return result, err
}

func cleanupPromptAuditImportCache(tx *gorm.DB, now int64) error {
	for _, table := range []any{&PromptAuditImportedContent{}, &PromptAuditImportedRecord{}, &PromptAuditArchiveImport{}} {
		if err := tx.Where("expires_at <= ?", now).Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}

// A host verifier supplies the digest of the remote encrypted volumes it actually
// downloaded and checked. Merely starting an upload never marks coverage.
func VerifyPromptAuditArchive(db *gorm.DB, id, digest string, volumes []PromptAuditArchiveVolume) error {
	if len(volumes) == 0 {
		return errors.New("verified remote volumes are required")
	}
	for _, volume := range volumes {
		if volume.Name == "" || volume.Bytes <= 0 || volume.RemotePath == "" {
			return errors.New("invalid remote volume")
		}
		if err := validatePromptAuditHash(volume.SHA256); err != nil {
			return err
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var archive PromptAuditArchive
		if err := lockForUpdate(tx).Where("id = ?", id).First(&archive).Error; err != nil {
			return err
		}
		if archive.Digest != digest || digest == "" {
			return errors.New("verified archive digest does not match")
		}
		var manifest promptAuditArchiveManifest
		if err := common.UnmarshalJsonStr(string(archive.Manifest), &manifest); err != nil {
			return err
		}
		if !manifest.RecoveryComplete {
			return errors.New("archive recovery materials are incomplete")
		}
		for _, entry := range manifest.Entries {
			value, ok := strings.CutPrefix(entry.Name, "records/")
			if !ok {
				continue
			}
			value, _ = strings.CutSuffix(value, ".json")
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return err
			}
			var audit PromptAudit
			if err := lockForUpdate(tx).Where("id = ?", id).First(&audit).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			current, err := promptAuditArchiveFingerprint(audit)
			if err != nil {
				return err
			}
			if current != entry.SHA256 {
				continue
			}
			if err := tx.Model(&PromptAudit{}).Where("id = ?", id).UpdateColumns(map[string]any{"archived_digest": current, "archived_at": common.GetTimestamp(), "archived_bundle_id": archive.ID}).Error; err != nil {
				return err
			}
		}
		data, err := common.Marshal(volumes)
		if err != nil {
			return err
		}
		return tx.Model(&PromptAuditArchive{}).Where("id = ?", id).Updates(map[string]any{"status": "verified", "verified_at": common.GetTimestamp(), "volumes": string(data)}).Error
	})
}

func CleanupPromptAuditSharedContent(db *gorm.DB, now int64, batchSize int) (int64, error) {
	cutoff := now - 7*24*60*60
	var candidates []PromptAudit
	dependencies := db.Model(&PromptAudit{}).Select("related_input_id").Where("related_input_id > 0 AND (status IN ? OR completed_at >= ?)", promptAuditActiveStatuses(), cutoff)
	if err := db.Where("content_manifest IS NOT NULL AND content_manifest <> '' AND content_evicted = ? AND status IN ? AND completed_at > 0 AND completed_at < ? AND archived_digest <> ''", false, promptAuditTerminalStatuses(), cutoff).Where("id NOT IN (?)", dependencies).Order("id asc").Limit(min(max(batchSize, 1), 500)).Find(&candidates).Error; err != nil {
		return 0, err
	}
	var purged int64
	for _, candidate := range candidates {
		err := db.Transaction(func(tx *gorm.DB) error {
			var audit PromptAudit
			if err := lockForUpdate(tx).Where("id = ?", candidate.ID).First(&audit).Error; err != nil {
				return err
			}
			if audit.ContentEvicted || !slices.Contains(promptAuditTerminalStatuses(), audit.Status) || audit.CompletedAt >= cutoff || audit.CompletedAt <= 0 {
				return nil
			}
			fingerprint, err := promptAuditArchiveFingerprint(audit)
			if err != nil {
				return err
			}
			if fingerprint != audit.ArchivedDigest {
				return nil
			}
			var verified int64
			if err := tx.Model(&PromptAuditArchive{}).Where("id = ? AND status = ?", audit.ArchivedBundleID, "verified").Count(&verified).Error; err != nil {
				return err
			}
			if verified != 1 {
				return nil
			}
			var dependent PromptAudit
			dependencyErr := lockForUpdate(tx).Select("id").Where("related_input_id = ? AND (status IN ? OR completed_at >= ?)", audit.ID, promptAuditActiveStatuses(), cutoff).First(&dependent).Error
			if dependencyErr != nil && !errors.Is(dependencyErr, gorm.ErrRecordNotFound) {
				return dependencyErr
			}
			if dependencyErr == nil {
				return nil
			}
			var manifest promptAuditContentManifest
			if err := common.UnmarshalJsonStr(string(audit.ContentManifest), &manifest); err != nil {
				return err
			}
			// Writers acquire the same hash locks before installing active references.
			for _, hash := range promptAuditManifestRefs(manifest) {
				var blob PromptAuditContent
				if err := lockForUpdate(tx).Select("hash").Where("hash = ?", hash).First(&blob).Error; err != nil {
					return err
				}
				if err := tx.Where("audit_id = ? AND hash = ?", audit.ID, hash).Delete(&PromptAuditContentRef{}).Error; err != nil {
					return err
				}
				var active PromptAuditContentRef
				activeErr := lockForUpdate(tx).Where("hash = ? AND active = ?", hash, true).First(&active).Error
				if activeErr != nil && !errors.Is(activeErr, gorm.ErrRecordNotFound) {
					return activeErr
				}
				if errors.Is(activeErr, gorm.ErrRecordNotFound) {
					if err := tx.Model(&PromptAuditContent{}).Where("hash = ?", hash).Updates(map[string]any{"data": nil, "stored_bytes": int64(0)}).Error; err != nil {
						return err
					}
				}
			}
			if err := tx.Model(&PromptAudit{}).Where("id = ?", audit.ID).UpdateColumn("content_evicted", true).Error; err != nil {
				return err
			}
			purged++
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return purged, err
		}
	}
	if err := cleanupPromptAuditImportCache(db, now); err != nil {
		return purged, err
	}
	if err := CleanupPromptAuditOrphanContent(db, batchSize); err != nil {
		return purged, err
	}
	return purged, nil
}

func CleanupPromptAuditOrphanContent(db *gorm.DB, batchSize int) error {
	if err := db.Where("audit_id NOT IN (?)", db.Model(&PromptAudit{}).Select("id")).Delete(&PromptAuditContentRef{}).Error; err != nil {
		return err
	}
	var hashes []string
	if err := db.Model(&PromptAuditContent{}).Where("data IS NOT NULL AND hash NOT IN (?)", db.Model(&PromptAuditContentRef{}).Select("hash").Where("active = ?", true)).Order("hash asc").Limit(min(max(batchSize, 1), 500)).Pluck("hash", &hashes).Error; err != nil {
		return err
	}
	for _, hash := range hashes {
		if err := db.Transaction(func(tx *gorm.DB) error {
			var blob PromptAuditContent
			if err := lockForUpdate(tx).Select("hash").Where("hash = ?", hash).First(&blob).Error; err != nil {
				return err
			}
			var ref PromptAuditContentRef
			err := lockForUpdate(tx).Where("hash = ? AND active = ?", hash, true).First(&ref).Error
			if err == nil {
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return tx.Model(&PromptAuditContent{}).Where("hash = ?", hash).Updates(map[string]any{"data": nil, "stored_bytes": int64(0)}).Error
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
			return err
		}
	}
	return nil
}
