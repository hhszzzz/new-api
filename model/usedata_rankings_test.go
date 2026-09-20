package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRankingAggregatesQuotaOnlyRowsAndScopedUserGroups(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Exec("DELETE FROM quota_data_scoped").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM quota_data_scoped")
	})

	require.NoError(t, DB.Create(&QuotaData{
		UserID: 1, Username: "alice", ModelName: "legacy-secret", CreatedAt: 3600,
		UseGroup: "", TokenUsed: 0, Quota: 500000, Count: 1,
	}).Error)
	require.NoError(t, DB.Create(&[]ScopedQuotaData{
		{
			UserID: 2, Username: "bob", ModelName: "visible-model", ModelScope: QuotaModelScopeRequested,
			CreatedAt: 3600, UseGroup: "team", TokenUsed: 100, Quota: 250000, Count: 1,
		},
		{
			UserID: 2, Username: "bob", ModelName: "admin-only", ModelScope: QuotaModelScopeAdminOnly,
			CreatedAt: 3600, UseGroup: "secret", TokenUsed: 0, Quota: 750000, Count: 1,
		},
	}).Error)

	totals, err := GetRankingQuotaTotals(0, 7200, []string{"visible-model"}, false)
	require.NoError(t, err)
	require.Len(t, totals, 2)
	assert.Equal(t, "visible-model", totals[0].ModelName)
	assert.Equal(t, int64(100), totals[0].TotalTokens)
	assert.Equal(t, int64(250000), totals[0].TotalQuota)
	assert.Equal(t, "", totals[1].ModelName)
	assert.Equal(t, int64(0), totals[1].TotalTokens)
	assert.Equal(t, int64(1250000), totals[1].TotalQuota)

	buckets, err := GetRankingQuotaBuckets(0, 7200, 3600, 0, []string{"visible-model"}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1500000), sumRankingTestQuota(buckets))

	users, err := GetRankingUserQuotaTotals(0, 7200, nil, true)
	require.NoError(t, err)
	require.Len(t, users, 3)
	var userQuota int64
	for _, row := range users {
		userQuota += row.TotalQuota
	}
	assert.Equal(t, int64(1500000), userQuota)
}

func TestRankingUserQuotaTotalsResolvesMissingUsernameAndDropsUnattributedRows(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Exec("DELETE FROM quota_data_scoped").Error)
	const userID = 909001
	require.NoError(t, DB.Unscoped().Delete(&User{}, userID).Error)
	require.NoError(t, DB.Create(&User{Id: userID, Username: "resolved-user", Password: "password"}).Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM quota_data_scoped")
		DB.Unscoped().Delete(&User{}, userID)
	})

	require.NoError(t, DB.Create(&[]ScopedQuotaData{
		{UserID: userID, Username: "", CreatedAt: 3600, UseGroup: "default", Quota: 100, Count: 1},
		{UserID: 0, Username: "", CreatedAt: 3600, UseGroup: "default", Quota: 200, Count: 1},
		{UserID: 0, Username: "legacy-user", CreatedAt: 3600, UseGroup: "default", Quota: 300, Count: 1},
	}).Error)

	rows, err := GetRankingUserQuotaTotals(0, 7200, nil, true)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	quotaByUsername := make(map[string]int64, len(rows))
	for _, row := range rows {
		quotaByUsername[row.Username] += row.TotalQuota
	}
	assert.Equal(t, int64(100), quotaByUsername["resolved-user"])
	assert.Equal(t, int64(300), quotaByUsername["legacy-user"])
	assert.NotContains(t, quotaByUsername, "")
}

func TestRankingQuotaBucketsCanAnchorToRequestedRangeStart(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Exec("DELETE FROM quota_data_scoped").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM quota_data_scoped")
	})

	const start = int64(28_800)
	require.NoError(t, DB.Create(&[]QuotaData{
		{ModelName: "anchored-model", CreatedAt: start + 60, TokenUsed: 1, Count: 1},
		{ModelName: "anchored-model", CreatedAt: start + 3600 + 60, TokenUsed: 1, Count: 1},
	}).Error)

	buckets, err := GetRankingQuotaBuckets(start, start+7200, 3600, start, nil, true)
	require.NoError(t, err)
	require.Len(t, buckets, 2)
	assert.Equal(t, start, buckets[0].Bucket)
	assert.Equal(t, start+3600, buckets[1].Bucket)
}

func TestRankingAggregatesConfiguredDatabases(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		dbType    common.DatabaseType
		dialector func(string) gorm.Dialector
	}{
		{name: "mysql", env: "TEST_MYSQL_DSN", dbType: common.DatabaseTypeMySQL, dialector: func(dsn string) gorm.Dialector {
			return mysql.Open(dsn)
		}},
		{name: "postgres", env: "TEST_POSTGRES_DSN", dbType: common.DatabaseTypePostgreSQL, dialector: func(dsn string) gorm.Dialector {
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

			previousDB := DB
			previousMainType := common.MainDatabaseType()
			DB = db
			common.SetDatabaseTypes(test.dbType, common.LogDatabaseType())
			t.Cleanup(func() {
				_ = db.Migrator().DropTable(&ScopedQuotaData{}, &QuotaData{}, &User{})
				DB = previousDB
				common.SetDatabaseTypes(previousMainType, common.LogDatabaseType())
				_ = sqlDB.Close()
			})

			require.NoError(t, db.Migrator().DropTable(&ScopedQuotaData{}, &QuotaData{}, &User{}))
			require.NoError(t, db.AutoMigrate(&User{}, &QuotaData{}, &ScopedQuotaData{}))
			require.NoError(t, db.Create(&User{Id: 901, Username: "resolved", Password: "password"}).Error)
			require.NoError(t, db.Create(&[]ScopedQuotaData{
				{UserID: 901, Username: "", ModelName: "visible", ModelScope: QuotaModelScopeRequested, CreatedAt: 3600, UseGroup: "team", TokenUsed: 100, Quota: 250000, Count: 1},
				{UserID: 902, Username: "hidden", ModelName: "private", ModelScope: QuotaModelScopeAdminOnly, CreatedAt: 3600, UseGroup: "secret", TokenUsed: 0, Quota: 750000, Count: 1},
			}).Error)

			totals, err := GetRankingQuotaTotals(0, 7200, []string{"visible"}, false)
			require.NoError(t, err)
			var totalQuota int64
			for _, row := range totals {
				totalQuota += row.TotalQuota
			}
			assert.Equal(t, int64(1000000), totalQuota)
			buckets, err := GetRankingQuotaBuckets(0, 7200, 3600, 0, []string{"visible"}, false)
			require.NoError(t, err)
			assert.Equal(t, int64(1000000), sumRankingTestQuota(buckets))
			users, err := GetRankingUserQuotaTotals(0, 7200, nil, true)
			require.NoError(t, err)
			require.Len(t, users, 2)
			usernames := []string{users[0].Username, users[1].Username}
			assert.ElementsMatch(t, []string{"resolved", "hidden"}, usernames)
		})
	}
}

func sumRankingTestQuota(rows []RankingQuotaBucket) int64 {
	var total int64
	for _, row := range rows {
		total += row.Quota
	}
	return total
}
