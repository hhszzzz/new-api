package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/klauspost/compress/zstd"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrPromptAuditContentUnavailable = errors.New("prompt audit content is unavailable; import its archive to view it")

// A content identity describes the uncompressed bytes, independently of codecs,
// event direction, inspection policy and the user who owns the event.
type PromptAuditContent struct {
	Hash        string `gorm:"type:varchar(64);primaryKey" json:"hash"`
	Data        []byte `json:"-"`
	RawBytes    int64  `json:"raw_bytes"`
	StoredBytes int64  `json:"stored_bytes"`
	CreatedAt   int64  `gorm:"index" json:"created_at"`
}

type PromptAuditContentRef struct {
	AuditID int64  `gorm:"primaryKey;autoIncrement:false"`
	Hash    string `gorm:"type:varchar(64);primaryKey;index"`
	Active  bool   `gorm:"index"`
}

type PromptAuditImportedContent struct {
	Hash        string `gorm:"type:varchar(64);primaryKey"`
	Data        []byte
	RawBytes    int64
	StoredBytes int64
	ExpiresAt   int64 `gorm:"index"`
}

type promptAuditStoredSegment struct {
	Fields  map[string]json.RawMessage `json:"fields"`
	TextRef string                     `json:"text_ref"`
}

type promptAuditStoredPayload struct {
	Fields    map[string]json.RawMessage `json:"fields,omitempty"`
	Segments  []promptAuditStoredSegment `json:"segments,omitempty"`
	OutputRef string                     `json:"output_ref,omitempty"`
	OpaqueRef string                     `json:"opaque_ref,omitempty"`
}

type promptAuditContentManifest struct {
	Version       int                       `json:"version"`
	Raw           promptAuditStoredPayload  `json:"raw"`
	Scan          promptAuditStoredPayload  `json:"scan"`
	Snapshot      *promptAuditStoredPayload `json:"snapshot,omitempty"`
	FullTextRef   string                    `json:"full_text_ref,omitempty"`
	LegacyFullRef string                    `json:"legacy_full_ref,omitempty"`
}

var promptAuditContentEncoder = sync.OnceValues(func() (*zstd.Encoder, error) {
	return zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)), zstd.WithEncoderConcurrency(1), zstd.WithZeroFrames(true))
})

func encodePromptAuditContent(data []byte) ([]byte, error) {
	encoder, err := promptAuditContentEncoder()
	if err != nil {
		return nil, err
	}
	return encoder.EncodeAll(data, nil), nil
}

func decodePromptAuditContent(hash string, data []byte, rawBytes int64) ([]byte, error) {
	if rawBytes < 0 || rawBytes == int64(^uint64(0)>>1) {
		return nil, errors.New("invalid archived content size")
	}
	decoder, err := zstd.NewReader(bytes.NewReader(data), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(128<<20))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	decoded, err := io.ReadAll(io.LimitReader(decoder, rawBytes+1))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(decoded)
	if int64(len(decoded)) != rawBytes || hex.EncodeToString(digest[:]) != hash {
		return nil, errors.New("prompt audit content checksum mismatch")
	}
	return decoded, nil
}

func promptAuditContentHash(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func storePromptAuditPayload(payload []byte, blobs map[string][]byte) (promptAuditStoredPayload, error) {
	stored := promptAuditStoredPayload{}
	if len(payload) == 0 {
		return stored, nil
	}
	var fields map[string]json.RawMessage
	if common.Unmarshal(payload, &fields) != nil || fields == nil {
		stored.OpaqueRef = promptAuditContentHash(payload)
		blobs[stored.OpaqueRef] = payload
		return stored, nil
	}
	var segments []map[string]json.RawMessage
	if raw, ok := fields["segments"]; ok {
		if err := common.Unmarshal(raw, &segments); err != nil {
			return stored, err
		}
		delete(fields, "segments")
	}
	for _, segment := range segments {
		var text string
		if err := common.Unmarshal(segment["text"], &text); err != nil {
			return stored, err
		}
		hash := promptAuditContentHash([]byte(text))
		blobs[hash] = []byte(text)
		delete(segment, "text")
		stored.Segments = append(stored.Segments, promptAuditStoredSegment{Fields: segment, TextRef: hash})
	}
	if raw, ok := fields["output"]; ok {
		var output string
		if err := common.Unmarshal(raw, &output); err != nil {
			return stored, err
		}
		stored.OutputRef = promptAuditContentHash([]byte(output))
		blobs[stored.OutputRef] = []byte(output)
		delete(fields, "output")
	}
	stored.Fields = fields
	return stored, nil
}

func promptAuditPayloadText(payload []byte) (joined, output string, valid bool) {
	var envelope struct {
		Segments []struct {
			Text string `json:"text"`
		} `json:"segments"`
		Output string `json:"output"`
	}
	if common.Unmarshal(payload, &envelope) != nil {
		return "", "", false
	}
	texts := make([]string, 0, len(envelope.Segments)+1)
	for _, segment := range envelope.Segments {
		if strings.TrimSpace(segment.Text) != "" {
			texts = append(texts, segment.Text)
		}
	}
	if strings.TrimSpace(envelope.Output) != "" {
		texts = append(texts, envelope.Output)
	}
	return strings.Join(texts, "\n\n"), envelope.Output, true
}

func persistPromptAuditContents(tx *gorm.DB, audit *PromptAudit) error {
	blobs := make(map[string][]byte)
	manifest := promptAuditContentManifest{Version: 1}
	rawPayload := audit.RawPayload
	if len(rawPayload) == 0 {
		rawPayload = audit.ContentSnapshot
	}
	if len(rawPayload) == 0 {
		rawPayload = audit.ScanPayload
	}
	var err error
	manifest.Raw, err = storePromptAuditPayload(rawPayload, blobs)
	if err != nil {
		return err
	}
	manifest.Scan, err = storePromptAuditPayload(audit.ScanPayload, blobs)
	if err != nil {
		return err
	}
	if len(audit.ContentSnapshot) > 0 && !bytes.Equal(audit.ContentSnapshot, audit.ScanPayload) {
		snapshot, snapshotErr := storePromptAuditPayload(audit.ContentSnapshot, blobs)
		if snapshotErr != nil {
			return snapshotErr
		}
		manifest.Snapshot = &snapshot
	}
	joined, output, valid := promptAuditPayloadText(rawPayload)
	if audit.Direction == "output" && valid {
		// The raw view of an output contains only the reply. Request blocks stay
		// referenced by the work view and, independently, by the input event.
		manifest.Raw.Segments = nil
		if manifest.Raw.OutputRef == "" {
			manifest.Raw.OutputRef = promptAuditContentHash([]byte(output))
			blobs[manifest.Raw.OutputRef] = []byte(output)
		}
		if len(audit.RawPayload) == 0 && len(audit.FullPrompt) > 0 && string(audit.FullPrompt) != joined && string(audit.FullPrompt) != output {
			manifest.LegacyFullRef = promptAuditContentHash(audit.FullPrompt)
			blobs[manifest.LegacyFullRef] = audit.FullPrompt
		}
		audit.PromptLength = utf8.RuneCountInString(output)
		if audit.RelatedInputID == 0 && audit.RequestID != "" {
			var input PromptAudit
			queryErr := lockForUpdate(tx).Select("id").Where("request_id = ? AND user_id = ? AND direction = ?", audit.RequestID, audit.UserID, "input").Order("id asc").First(&input).Error
			if queryErr != nil && !errors.Is(queryErr, gorm.ErrRecordNotFound) {
				return queryErr
			}
			audit.RelatedInputID = input.ID
		}
	} else if audit.Direction == "output" && !valid && len(audit.FullPrompt) > 0 {
		manifest.LegacyFullRef = promptAuditContentHash(audit.FullPrompt)
		blobs[manifest.LegacyFullRef] = audit.FullPrompt
	} else if len(audit.FullPrompt) > 0 && (!valid || string(audit.FullPrompt) != joined) {
		manifest.FullTextRef = promptAuditContentHash(audit.FullPrompt)
		blobs[manifest.FullTextRef] = audit.FullPrompt
	}
	encoded, err := common.Marshal(manifest)
	if err != nil {
		return err
	}
	refs := promptAuditManifestRefs(manifest)
	for _, hash := range refs {
		data, exists := blobs[hash]
		if !exists {
			continue
		}
		compressed, compressErr := encodePromptAuditContent(data)
		if compressErr != nil {
			return compressErr
		}
		blob := PromptAuditContent{Hash: hash, Data: compressed, RawBytes: int64(len(data)), StoredBytes: int64(len(compressed)), CreatedAt: common.GetTimestamp()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&blob).Error; err != nil {
			return err
		}
		var existing PromptAuditContent
		if err := lockForUpdate(tx).Select("hash", "raw_bytes").Where("hash = ?", hash).First(&existing).Error; err != nil {
			return err
		}
		if existing.RawBytes != blob.RawBytes {
			return errors.New("prompt audit content identity collision")
		}
		if err := tx.Model(&PromptAuditContent{}).Where("hash = ? AND data IS NULL", hash).Updates(map[string]any{"data": compressed, "stored_bytes": blob.StoredBytes}).Error; err != nil {
			return err
		}
	}
	audit.ContentManifest = LongText(encoded)
	audit.ContentEvicted = false
	audit.FullPrompt, audit.ScanPayload, audit.ContentSnapshot = nil, nil, nil
	return nil
}

func promptAuditManifestRefs(manifest promptAuditContentManifest) []string {
	refs := make(map[string]bool)
	for _, payload := range []promptAuditStoredPayload{manifest.Raw, manifest.Scan} {
		for _, segment := range payload.Segments {
			refs[segment.TextRef] = true
		}
		refs[payload.OutputRef], refs[payload.OpaqueRef] = true, true
	}
	if manifest.Snapshot != nil {
		for _, segment := range manifest.Snapshot.Segments {
			refs[segment.TextRef] = true
		}
		refs[manifest.Snapshot.OutputRef], refs[manifest.Snapshot.OpaqueRef] = true, true
	}
	refs[manifest.FullTextRef], refs[manifest.LegacyFullRef] = true, true
	delete(refs, "")
	hashes := make([]string, 0, len(refs))
	for hash := range refs {
		hashes = append(hashes, hash)
	}
	slices.Sort(hashes)
	return hashes
}

func syncPromptAuditContentRefs(tx *gorm.DB, audit *PromptAudit) error {
	var manifest promptAuditContentManifest
	if err := common.UnmarshalJsonStr(string(audit.ContentManifest), &manifest); err != nil {
		return err
	}
	if err := tx.Where("audit_id = ?", audit.ID).Delete(&PromptAuditContentRef{}).Error; err != nil {
		return err
	}
	// The event manifest retains historical references. This table indexes only
	// references that still protect a local body from collection.
	if audit.ContentEvicted {
		return nil
	}
	refs := make([]PromptAuditContentRef, 0)
	for _, hash := range promptAuditManifestRefs(manifest) {
		refs = append(refs, PromptAuditContentRef{AuditID: audit.ID, Hash: hash, Active: !audit.ContentEvicted})
	}
	if len(refs) == 0 {
		return nil
	}
	return tx.CreateInBatches(&refs, 100).Error
}

func readPromptAuditContent(tx *gorm.DB, hash string, cache map[string][]byte, allowImported bool) ([]byte, error) {
	if hash == "" {
		return nil, nil
	}
	if data, ok := cache[hash]; ok {
		return data, nil
	}
	var blob PromptAuditContent
	err := tx.Where("hash = ? AND data IS NOT NULL", hash).First(&blob).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && allowImported {
		var imported PromptAuditImportedContent
		err = tx.Where("hash = ? AND expires_at > ?", hash, common.GetTimestamp()).First(&imported).Error
		blob = PromptAuditContent{Hash: imported.Hash, Data: imported.Data, RawBytes: imported.RawBytes}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPromptAuditContentUnavailable
	}
	if err != nil {
		return nil, err
	}
	data, err := decodePromptAuditContent(hash, blob.Data, blob.RawBytes)
	if err != nil {
		return nil, err
	}
	cache[hash] = data
	return data, nil
}

func loadPromptAuditPayload(tx *gorm.DB, payload promptAuditStoredPayload, cache map[string][]byte, context, imported bool) ([]byte, error) {
	if payload.OpaqueRef != "" {
		return readPromptAuditContent(tx, payload.OpaqueRef, cache, imported)
	}
	if payload.Fields == nil && len(payload.Segments) == 0 && payload.OutputRef == "" {
		return nil, nil
	}
	fields := make(map[string]json.RawMessage, len(payload.Fields)+2)
	for key, value := range payload.Fields {
		fields[key] = value
	}
	if context {
		segments := make([]map[string]json.RawMessage, 0, len(payload.Segments))
		for _, segment := range payload.Segments {
			text, err := readPromptAuditContent(tx, segment.TextRef, cache, imported)
			if err != nil {
				return nil, err
			}
			item := make(map[string]json.RawMessage, len(segment.Fields)+1)
			for key, value := range segment.Fields {
				item[key] = value
			}
			item["text"], err = common.Marshal(string(text))
			if err != nil {
				return nil, err
			}
			segments = append(segments, item)
		}
		if len(segments) > 0 {
			fields["segments"], _ = common.Marshal(segments)
		}
	}
	if payload.OutputRef != "" {
		output, err := readPromptAuditContent(tx, payload.OutputRef, cache, imported)
		if err != nil {
			return nil, err
		}
		fields["output"], err = common.Marshal(string(output))
		if err != nil {
			return nil, err
		}
	}
	return common.Marshal(fields)
}

func LoadPromptAuditContent(tx *gorm.DB, audit *PromptAudit, work bool) error {
	if work && audit.ContentEvicted {
		return ErrPromptAuditContentUnavailable
	}
	if audit.ContentManifest == "" {
		return nil
	}
	var manifest promptAuditContentManifest
	if err := common.UnmarshalJsonStr(string(audit.ContentManifest), &manifest); err != nil {
		return err
	}
	if manifest.Version != 1 {
		return errors.New("unsupported prompt audit content version")
	}
	cache := make(map[string][]byte)
	raw, err := loadPromptAuditPayload(tx, manifest.Raw, cache, audit.Direction != "output", !work)
	if err != nil {
		return err
	}
	joined, output, _ := promptAuditPayloadText(raw)
	if audit.Direction == "output" {
		joined = output
	}
	if manifest.FullTextRef != "" && audit.Direction != "output" {
		text, readErr := readPromptAuditContent(tx, manifest.FullTextRef, cache, !work)
		if readErr != nil {
			return readErr
		}
		joined = string(text)
	}
	audit.FullPrompt = []byte(joined)
	audit.ContentSnapshot = raw
	if work {
		audit.ScanPayload, err = loadPromptAuditPayload(tx, manifest.Scan, cache, true, false)
		if err != nil {
			return err
		}
		if manifest.Snapshot != nil {
			audit.ContentSnapshot, err = loadPromptAuditPayload(tx, *manifest.Snapshot, cache, true, false)
			if err != nil {
				return err
			}
		} else {
			audit.ContentSnapshot = audit.ScanPayload
		}
	} else {
		audit.ScanPayload, err = loadPromptAuditPayload(tx, manifest.Scan, cache, audit.Direction != "output", true)
		if err != nil {
			return err
		}
	}
	audit.ContentState = "hot"
	if audit.ContentEvicted {
		audit.ContentState = "imported"
	}
	audit.LegacyContent = manifest.LegacyFullRef != ""
	return nil
}

// Replace only the work view at completion. The immutable original request/reply
// is retained independently, while a finite request-time policy may cap work.
func retainedPromptAuditContentUpdates(tx *gorm.DB, audit *PromptAudit, payload []byte, truncated bool) (map[string]any, error) {
	if audit.ContentManifest == "" {
		return map[string]any{"scan_payload": payload, "content_snapshot": payload, "scan_payload_truncated": truncated}, nil
	}
	var manifest promptAuditContentManifest
	if err := common.UnmarshalJsonStr(string(audit.ContentManifest), &manifest); err != nil {
		return nil, err
	}
	copy := *audit
	copy.RawPayload, copy.FullPrompt = nil, nil
	copy.ScanPayload, copy.ContentSnapshot = payload, payload
	if err := persistPromptAuditContents(tx, &copy); err != nil {
		return nil, err
	}
	var updated promptAuditContentManifest
	if err := common.UnmarshalJsonStr(string(copy.ContentManifest), &updated); err != nil {
		return nil, err
	}
	manifest.Scan, manifest.Snapshot = updated.Scan, nil
	data, err := common.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	audit.ContentManifest = LongText(data)
	if err := syncPromptAuditContentRefs(tx, audit); err != nil {
		return nil, err
	}
	return map[string]any{"content_manifest": string(data), "scan_payload": nil, "content_snapshot": nil, "scan_payload_truncated": truncated, "archived_digest": ""}, nil
}

func BackfillPromptAuditContents(db *gorm.DB, batchSize int) (int64, error) {
	batchSize = min(max(batchSize, 1), 200)
	var rows []PromptAudit
	// Select identities first; each transaction reads only its own full body.
	if err := db.Select("id").Where("(content_manifest IS NULL OR content_manifest = '') AND (full_prompt IS NOT NULL OR scan_payload IS NOT NULL OR content_snapshot IS NOT NULL)").Order("id asc").Limit(batchSize).Find(&rows).Error; err != nil {
		return 0, err
	}
	var migrated int64
	for _, row := range rows {
		err := db.Transaction(func(tx *gorm.DB) error {
			var current PromptAudit
			if err := lockForUpdate(tx).Where("id = ?", row.ID).First(&current).Error; err != nil {
				return err
			}
			if current.ContentManifest != "" {
				return nil
			}
			if err := persistPromptAuditContents(tx, &current); err != nil {
				return err
			}
			if current.Direction == "output" {
				current.RedactedPreview = ""
			}
			if err := tx.Model(&PromptAudit{}).Where("id = ?", current.ID).Updates(map[string]any{"content_manifest": current.ContentManifest, "related_input_id": current.RelatedInputID, "prompt_length": current.PromptLength, "redacted_preview": current.RedactedPreview, "full_prompt": nil, "scan_payload": nil, "content_snapshot": nil, "archived_digest": ""}).Error; err != nil {
				return err
			}
			if err := syncPromptAuditContentRefs(tx, &current); err != nil {
				return err
			}
			migrated++
			return nil
		})
		if err != nil {
			return migrated, err
		}
	}
	return migrated, nil
}

func promptAuditReplyResponse(audit *PromptAudit, response *PromptAuditResponse) {
	if audit.Direction != "output" {
		return
	}
	payload := audit.ContentSnapshot
	if len(payload) == 0 {
		payload = audit.ScanPayload
	}
	joined, output, valid := promptAuditPayloadText(payload)
	if !valid {
		response.LegacyContent = audit.LegacyContent || len(audit.FullPrompt) > 0
		response.FullPrompt = nil
		response.FullPromptAvailable = false
		response.ScanPayload = nil
		return
	}
	response.LegacyContent = audit.LegacyContent || len(audit.FullPrompt) > 0 && string(audit.FullPrompt) != joined && string(audit.FullPrompt) != output
	response.FullPrompt = &output
	response.PromptLength = utf8.RuneCountInString(output)
	response.FullPromptAvailable = output != ""
	visiblePayload := audit.ScanPayload
	if audit.ContentManifest == "" || len(visiblePayload) == 0 {
		visiblePayload = payload
	}
	var fields map[string]json.RawMessage
	if common.Unmarshal(visiblePayload, &fields) == nil {
		delete(fields, "segments")
		if data, err := common.Marshal(fields); err == nil {
			text := string(data)
			response.ScanPayload = &text
		}
	}
}

func ensurePromptAuditStorageTables(db *gorm.DB) error {
	return db.AutoMigrate(&PromptAuditContent{}, &PromptAuditContentRef{}, &PromptAuditImportedContent{}, &PromptAuditArchive{}, &PromptAuditArchiveImport{}, &PromptAuditImportedRecord{}, &PromptAuditStorageState{})
}

func promptAuditStorageEnabled() bool { return prompt_audit_setting.GetSetting().SharedContentEnabled }

func validatePromptAuditHash(hash string) error {
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("invalid content identity")
	}
	return nil
}
