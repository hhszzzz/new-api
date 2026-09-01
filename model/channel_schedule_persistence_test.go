package model

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type channelSchedulePersistenceFixture struct {
	ID       uint            `gorm:"primaryKey"`
	Schedule ChannelSchedule `gorm:"type:text"`
}

func testChannelSchedulePersistence(t *testing.T, db *gorm.DB) {
	t.Helper()
	tableName := fmt.Sprintf("channel_schedule_persistence_%d", time.Now().UnixNano())
	tableDB := db.Table(tableName)
	t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(tableName)) })
	require.NoError(t, tableDB.AutoMigrate(&channelSchedulePersistenceFixture{}))

	startsAt := int64(1785718800)
	want := channelSchedulePersistenceFixture{
		Schedule: ChannelSchedule{StartsAt: &startsAt},
	}
	require.NoError(t, tableDB.Create(&want).Error)

	var stored string
	require.NoError(t, tableDB.Select("schedule").Where("id = ?", want.ID).Scan(&stored).Error)
	assert.JSONEq(t, `{"timezone":"Asia/Shanghai","starts_at":1785718800}`, stored)

	var got channelSchedulePersistenceFixture
	require.NoError(t, tableDB.First(&got, want.ID).Error)
	assert.Equal(t, ChannelScheduleTimezone, got.Schedule.Timezone)
	require.NotNil(t, got.Schedule.StartsAt)
	assert.Equal(t, startsAt, *got.Schedule.StartsAt)
}

func TestChannelSchedulePersistenceSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	testChannelSchedulePersistence(t, db)
}

func TestChannelSchedulePersistenceMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	testChannelSchedulePersistence(t, db)
}

func TestChannelSchedulePersistencePostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	testChannelSchedulePersistence(t, db)
}
