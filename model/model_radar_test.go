package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestModelRadarSnapshotKeepsOnlyLatestPayload(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-radar.db")), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ModelRadarSnapshot{}))
	t.Cleanup(func() { DB = previousDB })

	ctx := context.Background()
	require.NoError(t, SaveModelRadarSnapshot(ctx, &ModelRadarSnapshot{
		SchemaVersion:   1,
		Payload:         []byte(`{"version":"first"}`),
		SourceUpdatedAt: 100,
		AlertsUpdatedAt: 101,
		FetchedAt:       102,
	}))
	require.NoError(t, SaveModelRadarSnapshot(ctx, &ModelRadarSnapshot{
		SchemaVersion:   1,
		Payload:         []byte(`{"version":"second"}`),
		SourceUpdatedAt: 200,
		AlertsUpdatedAt: 201,
		FetchedAt:       202,
	}))

	snapshot, err := GetModelRadarSnapshot(ctx)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Equal(t, []byte(`{"version":"second"}`), snapshot.Payload)
	assert.Equal(t, int64(200), snapshot.SourceUpdatedAt)
	assert.Equal(t, int64(201), snapshot.AlertsUpdatedAt)
	assert.Equal(t, int64(202), snapshot.FetchedAt)

	var count int64
	require.NoError(t, db.Model(&ModelRadarSnapshot{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestSaveModelRadarSnapshotRejectsEmptyPayload(t *testing.T) {
	require.Error(t, SaveModelRadarSnapshot(context.Background(), nil))
	require.Error(t, SaveModelRadarSnapshot(context.Background(), &ModelRadarSnapshot{}))
}

func TestModelRadarSnapshotStorageConfiguredDatabases(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		dialector func(string) gorm.Dialector
	}{
		{name: "mysql", env: "TEST_MYSQL_DSN", dialector: func(dsn string) gorm.Dialector {
			return mysql.Open(dsn)
		}},
		{name: "postgres", env: "TEST_POSTGRES_DSN", dialector: func(dsn string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := strings.TrimSpace(os.Getenv(test.env))
			if dsn == "" {
				t.Skip(test.env + " is not configured")
			}
			db, err := gorm.Open(test.dialector(dsn), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

			tableName := fmt.Sprintf("model_radar_snapshot_test_%d", time.Now().UnixNano())
			t.Cleanup(func() { _ = db.Migrator().DropTable(tableName) })
			require.NoError(t, db.Table(tableName).AutoMigrate(&ModelRadarSnapshot{}))

			versions := []struct {
				fetchedAt int64
				payload   []byte
			}{
				{fetchedAt: 100, payload: []byte(`{"version":"first"}`)},
				{fetchedAt: 200, payload: []byte(`{"version":"second"}`)},
			}
			for _, version := range versions {
				snapshot := &ModelRadarSnapshot{
					ID: 1, SchemaVersion: 1, Payload: version.payload,
					SourceUpdatedAt: version.fetchedAt - 2,
					AlertsUpdatedAt: version.fetchedAt - 1,
					FetchedAt:       version.fetchedAt,
				}
				require.NoError(t, db.Table(tableName).Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "id"}},
					DoUpdates: clause.AssignmentColumns([]string{
						"schema_version", "payload", "source_updated_at", "alerts_updated_at", "fetched_at",
					}),
				}).Create(snapshot).Error)
			}

			var stored ModelRadarSnapshot
			require.NoError(t, db.Table(tableName).First(&stored, "id = ?", 1).Error)
			assert.Equal(t, []byte(`{"version":"second"}`), stored.Payload)
			assert.Equal(t, int64(200), stored.FetchedAt)
		})
	}
}
