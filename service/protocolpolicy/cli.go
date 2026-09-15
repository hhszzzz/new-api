package protocolpolicy

import (
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
)

// MigrateOnStartup backs up the exact configuration before changing it. A
// blocked preflight aborts startup instead of silently selecting new defaults.
func MigrateOnStartup(db *gorm.DB, backupDirectory string) error {
	manifest, err := Preflight(db)
	if err != nil {
		return err
	}
	if len(manifest.Blockers) > 0 {
		review, _ := common.Marshal(manifest.Review())
		return fmt.Errorf("protocol migration blocked: %s", review)
	}
	if len(manifest.Channels) == 0 && len(manifest.Options) == 0 {
		return nil
	}
	name := "protocol-policy-v1-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".json"
	path := filepath.Join(backupDirectory, name)
	if err := SaveBackup(path, manifest); err != nil {
		return fmt.Errorf("save protocol migration backup: %w", err)
	}
	if err := Apply(db, manifest); err != nil {
		return err
	}
	common.SysLog(fmt.Sprintf("protocol policy migrated: channels=%d backup=%s", len(manifest.Channels), path))
	return nil
}

// RunCLI uses SQL_DSN / SQLITE_PATH without starting the app or running schema
// migrations. Preflight is read-only; apply always saves its rollback manifest.
func RunCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: new-api protocol-policy preflight|apply|rollback [--backup path]")
		return 2
	}
	mode := args[0]
	if mode != "preflight" && mode != "apply" && mode != "rollback" {
		fmt.Fprintln(stderr, "unknown protocol-policy mode")
		return 2
	}
	flags := flag.NewFlagSet("protocol-policy "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	backup := flags.String("backup", "", "private rollback manifest path (required for apply and rollback)")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return 2
	}
	if mode != "preflight" && *backup == "" {
		fmt.Fprintln(stderr, "--backup is required before changing protocol configuration")
		return 2
	}
	db, err := model.OpenConfigurationDatabase()
	if err != nil {
		fmt.Fprintln(stderr, "cannot open the configured primary database")
		return 1
	}
	sqlDB, err := db.DB()
	if err != nil {
		fmt.Fprintln(stderr, "cannot access the primary database connection")
		return 1
	}
	defer sqlDB.Close()
	var manifest *Manifest
	if mode == "rollback" {
		manifest, err = LoadBackup(*backup)
		if err == nil {
			err = Rollback(db, manifest)
		}
	} else {
		manifest, err = Preflight(db)
		if err == nil && len(manifest.Blockers) > 0 {
			err = errors.New("protocol migration preflight has blockers")
		}
		if err == nil && mode == "apply" {
			err = SaveBackup(*backup, manifest)
			if err == nil {
				err = Apply(db, manifest)
			}
		}
	}
	if manifest != nil {
		review, _ := common.Marshal(manifest.Review())
		fmt.Fprintln(stdout, string(review))
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func BackupDirectory() string {
	if directory := os.Getenv("PROTOCOL_MIGRATION_BACKUP_DIR"); directory != "" {
		return directory
	}
	return filepath.Join("data", "protocol-migrations")
}
