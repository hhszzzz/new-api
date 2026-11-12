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

type channelUserRateLimitPersistenceFixture struct {
	ID             uint                  `gorm:"primaryKey"`
	UserRateLimits ChannelUserRateLimits `gorm:"type:text"`
}

func testChannelUserRateLimitPersistence(t *testing.T, db *gorm.DB) {
	t.Helper()
	tableName := fmt.Sprintf("channel_user_rate_limit_persistence_%d", time.Now().UnixNano())
	tableDB := db.Table(tableName)
	t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(tableName)) })
	require.NoError(t, tableDB.AutoMigrate(&channelUserRateLimitPersistenceFixture{}))

	rpmLimit := 60
	concurrencyLimit := 5
	fixture := channelUserRateLimitPersistenceFixture{
		UserRateLimits: ChannelUserRateLimits{
			{UserId: 7, RPMLimit: &rpmLimit, ConcurrencyLimit: &concurrencyLimit, Username: "ignored"},
			{UserId: 8, Priority: 100},
		},
	}
	require.NoError(t, tableDB.Create(&fixture).Error)

	var stored string
	require.NoError(t, tableDB.Select("user_rate_limits").Where("id = ?", fixture.ID).Scan(&stored).Error)
	assert.JSONEq(t, `[{"user_id":7,"rpm_limit":60,"concurrency_limit":5},{"user_id":8,"rpm_limit":null,"concurrency_limit":null,"priority":100}]`, stored, "display names must not be persisted")

	var loaded channelUserRateLimitPersistenceFixture
	require.NoError(t, tableDB.First(&loaded, fixture.ID).Error)
	limit, ok := loaded.UserRateLimits.ForUser(7)
	require.True(t, ok)
	require.NotNil(t, limit.RPMLimit)
	require.NotNil(t, limit.ConcurrencyLimit)
	assert.Equal(t, 60, *limit.RPMLimit)
	assert.Equal(t, 5, *limit.ConcurrencyLimit)

	priorityLimit, ok := loaded.UserRateLimits.ForUser(8)
	require.True(t, ok)
	assert.Equal(t, 100, priorityLimit.Priority)

	emptyValue, err := ChannelUserRateLimits{}.Value()
	require.NoError(t, err)
	assert.Equal(t, "[]", emptyValue)
}

func TestChannelUserRateLimitPersistenceSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	testChannelUserRateLimitPersistence(t, db)
}

func TestChannelUserRateLimitPersistenceMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	testChannelUserRateLimitPersistence(t, db)
}

func TestChannelUserRateLimitPersistencePostgreSQL(t *testing.T) {
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
	testChannelUserRateLimitPersistence(t, db)
}

func TestChannelUserRateLimitPersistsThroughChannelUpdate(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	channel := &Channel{
		Id:     1,
		Name:   "per-user limits",
		Key:    "key",
		Status: 1,
		Models: "gpt-4o",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)

	rpm := 60
	concurrency := 2
	channel.UserRateLimits = ChannelUserRateLimits{
		{UserId: 7, RPMLimit: &rpm, ConcurrencyLimit: &concurrency, Priority: 100},
	}
	require.NoError(t, channel.Update())

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	limit, ok := stored.UserRateLimits.ForUser(7)
	require.True(t, ok)
	require.NotNil(t, limit.RPMLimit)
	assert.Equal(t, 60, *limit.RPMLimit)
	require.NotNil(t, limit.ConcurrencyLimit)
	assert.Equal(t, 2, *limit.ConcurrencyLimit)
	assert.Equal(t, 100, limit.Priority)

	channel.UserRateLimits = ChannelUserRateLimits{}
	require.NoError(t, channel.Update())
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Empty(t, stored.UserRateLimits)
}

func testChannelUserRateLimitMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Migrator().DropTable(&Channel{}))
	t.Cleanup(func() { _ = db.Migrator().DropTable(&Channel{}) })

	require.NoError(t, db.AutoMigrate(&Channel{}))
	require.True(t, db.Migrator().HasColumn(&Channel{}, "user_rate_limits"))

	legacy := &Channel{Id: 1, Name: "legacy", Key: "key", Status: 1, Group: "default"}
	require.NoError(t, db.Create(legacy).Error)
	require.NoError(t, db.Migrator().DropColumn(&Channel{}, "user_rate_limits"))
	require.False(t, db.Migrator().HasColumn(&Channel{}, "user_rate_limits"))

	require.NoError(t, db.AutoMigrate(&Channel{}), "upgrading the previous schema must add the column")
	require.True(t, db.Migrator().HasColumn(&Channel{}, "user_rate_limits"))
	var stored Channel
	require.NoError(t, db.First(&stored, legacy.Id).Error)
	assert.Equal(t, "legacy", stored.Name, "existing rows must be preserved")

	rpm := 30
	stored.UserRateLimits = ChannelUserRateLimits{{UserId: 8, RPMLimit: &rpm}}
	require.NoError(t, db.Model(&stored).Updates(map[string]any{"user_rate_limits": stored.UserRateLimits}).Error)
	require.NoError(t, db.First(&stored, legacy.Id).Error)
	limit, ok := stored.UserRateLimits.ForUser(8)
	require.True(t, ok)
	require.NotNil(t, limit.RPMLimit)
	assert.Equal(t, 30, *limit.RPMLimit)

	require.NoError(t, db.AutoMigrate(&Channel{}), "repeated migration must be idempotent")
	require.NoError(t, db.First(&stored, legacy.Id).Error)
	assert.Len(t, stored.UserRateLimits, 1)
}

func TestChannelUserRateLimitMigrationSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	testChannelUserRateLimitMigration(t, db)
}

func TestChannelUserRateLimitMigrationMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	testChannelUserRateLimitMigration(t, db)
}

func TestChannelUserRateLimitMigrationPostgreSQL(t *testing.T) {
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
	testChannelUserRateLimitMigration(t, db)
}

func TestChannelUserRateLimitsNormalize(t *testing.T) {
	zero := 0
	valid := 10
	limits := ChannelUserRateLimits{
		{UserId: 1, RPMLimit: &valid},
		{UserId: 2},
		{UserId: 3, ConcurrencyLimit: &valid, Username: "ignored"},
		{UserId: 4, Priority: 100},
	}
	normalized, err := limits.normalized()
	require.NoError(t, err)
	require.Len(t, normalized, 3, "entries with no limits and no priority must be dropped")
	assert.Zero(t, normalized[1].Username)
	assert.Equal(t, 100, normalized[2].Priority, "a priority-only override must be kept")

	_, err = ChannelUserRateLimits{{UserId: 1, RPMLimit: &valid}, {UserId: 1, RPMLimit: &valid}}.normalized()
	assert.Error(t, err, "duplicate user ids must be rejected")
	_, err = ChannelUserRateLimits{{UserId: 0, RPMLimit: &valid}}.normalized()
	assert.Error(t, err, "user ids must be positive")
	_, err = ChannelUserRateLimits{{UserId: 1, RPMLimit: &zero}}.normalized()
	assert.Error(t, err, "limits must be positive")

	tooMany := make(ChannelUserRateLimits, maxChannelUserRateLimits+1)
	for i := range tooMany {
		tooMany[i] = ChannelUserRateLimit{UserId: i + 1, RPMLimit: &valid}
	}
	_, err = tooMany.normalized()
	assert.Error(t, err, "the cap must keep the JSON below the MySQL TEXT limit")
}

func TestChannelUserRateLimitsEqualIgnoresOrder(t *testing.T) {
	first := 10
	second := 20
	left := ChannelUserRateLimits{{UserId: 1, RPMLimit: &first}, {UserId: 2, ConcurrencyLimit: &second}}
	right := ChannelUserRateLimits{{UserId: 2, ConcurrencyLimit: &second}, {UserId: 1, RPMLimit: &first}}
	assert.True(t, left.Equal(right))
	assert.False(t, left.Equal(ChannelUserRateLimits{{UserId: 1, RPMLimit: &second}, {UserId: 2, ConcurrencyLimit: &second}}))
	assert.False(t, left.Equal(ChannelUserRateLimits{{UserId: 1, RPMLimit: &first}, {UserId: 2, ConcurrencyLimit: &second, Priority: 100}}))
	assert.False(t, left.Equal(ChannelUserRateLimits{{UserId: 1, RPMLimit: &first}}))
}
