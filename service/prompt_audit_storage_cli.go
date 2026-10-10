package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Host maintenance deliberately bypasses gateway startup and background workers.
func RunPromptAuditStorageCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "prompt-audit: migrate, stats, export, verify, import, cleanup, archives, needed")
		return 2
	}
	flags := flag.NewFlagSet("prompt-audit "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	batch := flags.Int("batch", 100, "migration batch size")
	enable := flags.Bool("enable", false, "activate complete shared storage after migration")
	out := flags.String("out", "", "archive output path")
	day := flags.String("day", time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02"), "archive day")
	full := flags.Bool("full", false, "include unchanged events")
	createdFrom := flags.Int64("created-from", 0, "include events created at or after this Unix timestamp")
	createdBefore := flags.Int64("created-before", 0, "include events created before this Unix timestamp")
	statePath := flags.String("snapshot-state", "", "snapshot handshake file")
	releasePath := flags.String("release-file", "", "release snapshot after pg_dump finishes")
	id := flags.String("id", "", "archive id")
	digest := flags.String("digest", "", "archive digest")
	volumesPath := flags.String("volumes", "", "verified remote volume manifest")
	restore := flags.Bool("restore", false, "restore recovery bodies instead of viewing cache")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	db, err := model.OpenConfigurationDatabase()
	if err != nil {
		fmt.Fprintln(stderr, "open audit database:", err)
		return 1
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	switch db.Dialector.Name() {
	case "mysql":
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	case "postgres":
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	default:
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	}
	var result any
	switch args[0] {
	case "migrate":
		err = model.MigratePromptAuditStorage(db)
		var total int64
		for err == nil {
			var count int64
			count, err = model.BackfillPromptAuditContents(db, *batch)
			total += count
			if count == 0 {
				break
			}
		}
		if err == nil && *enable {
			err = db.Transaction(func(tx *gorm.DB) error {
				for _, option := range []model.Option{{Key: "prompt_audit.shared_content_enabled", Value: "true"}, {Key: "prompt_audit.retention_days", Value: "7"}, {Key: "prompt_audit.full_prompt_max_runes", Value: "0"}} {
					if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&option).Error; err != nil {
						return err
					}
				}
				return nil
			})
		}
		result = map[string]any{"migrated": total, "enabled": *enable && err == nil}
	case "stats":
		result, err = model.GetPromptAuditStorageStats(db)
	case "archives":
		var archives []model.PromptAuditArchive
		archives, err = model.ListPromptAuditArchives(db)
		items := make([]map[string]any, 0, len(archives))
		for _, archive := range archives {
			var volumes []model.PromptAuditArchiveVolume
			_ = common.UnmarshalJsonStr(string(archive.Volumes), &volumes)
			items = append(items, map[string]any{"archive": archive, "volumes": volumes})
		}
		result = items
	case "needed":
		var archives []model.PromptAuditArchive
		archives, err = model.PromptAuditArchivesNeededForExport(db)
		items := make([]map[string]any, 0, len(archives))
		for _, archive := range archives {
			var volumes []model.PromptAuditArchiveVolume
			_ = common.UnmarshalJsonStr(string(archive.Volumes), &volumes)
			items = append(items, map[string]any{"archive": archive, "volumes": volumes})
		}
		result = items
	case "cleanup":
		var total int64
		for {
			var count int64
			count, err = model.CleanupPromptAuditSharedContent(db, common.GetTimestamp(), *batch)
			total += count
			if err != nil || count == 0 {
				break
			}
		}
		result = map[string]any{"evicted": total}
	case "export":
		result, err = exportPromptAuditSnapshot(db, *out, *day, *statePath, *releasePath, *full, model.PromptAuditArchiveWindow{CreatedFrom: *createdFrom, CreatedBefore: *createdBefore})
	case "verify":
		var data []byte
		data, err = os.ReadFile(*volumesPath)
		var volumes []model.PromptAuditArchiveVolume
		if err == nil {
			err = common.Unmarshal(data, &volumes)
		}
		if err == nil {
			err = model.VerifyPromptAuditArchive(db, *id, *digest, volumes)
		}
		result = map[string]any{"verified": err == nil, "id": *id}
	case "import":
		if len(flags.Args()) == 0 {
			err = errors.New("at least one archive file is required")
			break
		}
		if _, err = model.PromptAuditStorageSource(db); err != nil {
			break
		}
		readers := make([]io.Reader, 0, len(flags.Args()))
		files := make([]*os.File, 0, len(flags.Args()))
		defer func() {
			for _, file := range files {
				_ = file.Close()
			}
		}()
		for _, path := range flags.Args() {
			var file *os.File
			file, err = os.Open(path)
			if err != nil {
				break
			}
			files = append(files, file)
			readers = append(readers, file)
		}
		if err == nil {
			result, err = model.ImportPromptAuditArchive(db, io.MultiReader(readers...), *restore)
		}
	default:
		err = errors.New("unknown prompt-audit command")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	data, err := common.Marshal(result)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

type promptAuditSnapshotState struct {
	Status           string                   `json:"status"`
	Snapshot         string                   `json:"snapshot"`
	Archive          model.PromptAuditArchive `json:"archive"`
	RecoveryComplete bool                     `json:"recovery_complete"`
	ExcludeTableData []string                 `json:"exclude_table_data"`
}

func exportPromptAuditSnapshot(db *gorm.DB, path, day, statePath, releasePath string, full bool, window model.PromptAuditArchiveWindow) (model.PromptAuditArchive, error) {
	var archive model.PromptAuditArchive
	if path == "" {
		return archive, errors.New("archive output path is required")
	}
	if (statePath == "") != (releasePath == "") {
		return archive, errors.New("snapshot-state and release-file must be supplied together")
	}
	if statePath != "" && (window.CreatedFrom != 0 || window.CreatedBefore != 0) {
		return archive, errors.New("historical creation windows cannot be used to exclude bodies from a database backup")
	}
	if statePath != "" && db.Dialector.Name() != "postgres" {
		return archive, errors.New("external backup snapshots require PostgreSQL")
	}
	source, err := model.PromptAuditStorageSource(db)
	if err != nil {
		return archive, err
	}
	version, err := model.ReservePromptAuditArchiveVersion(db)
	if err != nil {
		return archive, err
	}
	openFlags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeNamedPipe != 0 {
		openFlags = os.O_WRONLY
	}
	file, err := os.OpenFile(path, openFlags, 0600)
	if err != nil {
		return archive, err
	}
	defer file.Close()
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()
	err = db.Transaction(func(tx *gorm.DB) error {
		state := promptAuditSnapshotState{Status: "writing"}
		if statePath != "" {
			if err := tx.Raw("SELECT pg_export_snapshot()").Scan(&state.Snapshot).Error; err != nil {
				return err
			}
			if err := writePromptAuditSnapshotState(statePath, state); err != nil {
				return err
			}
		}
		digest := sha256.New()
		stream := &promptAuditArchiveStream{Writer: io.MultiWriter(file, digest)}
		var exportErr error
		archive, exportErr = model.WritePromptAuditArchive(tx, stream, source, day, version, full, window)
		if exportErr != nil {
			return exportErr
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			if err := file.Sync(); err != nil {
				return err
			}
		}
		if err := file.Close(); err != nil {
			return err
		}
		archive.Bytes, archive.Digest = stream.Bytes, hex.EncodeToString(digest.Sum(nil))
		if statePath != "" {
			state.Status, state.Archive, state.RecoveryComplete = "ready", archive, true
			// An unverified or active body is in this package; unchanged covered
			// bodies remain recoverable from the verified archive catalog.
			state.ExcludeTableData = []string{"prompt_audit_contents", "prompt_audit_imported_contents", "prompt_audit_imported_records", "prompt_audit_archive_imports"}
			if err := writePromptAuditSnapshotState(statePath, state); err != nil {
				return err
			}
			deadline := time.Now().Add(2 * time.Hour)
			for {
				if _, err := os.Stat(releasePath); err == nil {
					break
				} else if !os.IsNotExist(err) {
					return err
				}
				if time.Now().After(deadline) {
					return errors.New("backup snapshot release timed out")
				}
				time.Sleep(time.Second)
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return archive, err
	}
	if err := db.Create(&archive).Error; err != nil {
		return archive, err
	}
	complete = true
	return archive, nil
}

type promptAuditArchiveStream struct {
	io.Writer
	Bytes int64
}

func (stream *promptAuditArchiveStream) Write(data []byte) (int, error) {
	count, err := stream.Writer.Write(data)
	stream.Bytes += int64(count)
	return count, err
}

func writePromptAuditSnapshotState(path string, state promptAuditSnapshotState) error {
	data, err := common.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
