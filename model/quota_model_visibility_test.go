package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type legacyQuotaDataMigrationRow struct {
	Id        int `gorm:"primaryKey"`
	ModelName string
}

func (legacyQuotaDataMigrationRow) TableName() string {
	return "quota_data"
}

func seedQuotaVisibilityRows(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Create(&[]QuotaData{
		{
			// A legacy routed model can also be present in the public catalog. Its
			// appearance in a request the user did not make still reveals routing.
			UserID: 1, Username: "alice", ModelName: "public-model", ModelScope: QuotaModelScopeLegacy,
			CreatedAt: 3600, UseGroup: "default", Count: 1, Quota: 10, TokenUsed: 10,
		},
		{
			UserID: 1, Username: "alice", ModelName: "private-upstream", ModelScope: QuotaModelScopeLegacy,
			CreatedAt: 3600, UseGroup: "default", Count: 2, Quota: 20, TokenUsed: 20,
		},
	}).Error)
	require.NoError(t, DB.Create(&[]ScopedQuotaData{
		{
			UserID: 1, Username: "alice", ModelName: "public-model", ModelScope: QuotaModelScopeRequested,
			CreatedAt: 3600, UseGroup: "default", Count: 1, Quota: 15, TokenUsed: 15,
		},
		{
			UserID: 1, Username: "alice", ModelName: "requested-private", ModelScope: QuotaModelScopeRequested,
			CreatedAt: 3600, UseGroup: "default", Count: 3, Quota: 30, TokenUsed: 30,
		},
		{
			UserID: 1, Username: "alice", ModelName: "admin-secret", ModelScope: QuotaModelScopeAdminOnly,
			CreatedAt: 3600, UseGroup: "default", Count: 4, Quota: 40, TokenUsed: 40,
		},
	}).Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM quota_data_scoped")
	})
}

func TestQuotaDataModelScopeProtectsSelfViewsAndPreservesTotals(t *testing.T) {
	seedQuotaVisibilityRows(t)

	rows, err := GetQuotaDataByUserId(1, 0, 7200, false)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "", rows[0].ModelName)
	assert.Equal(t, 70, rows[0].Quota)
	assert.Equal(t, 70, rows[0].TokenUsed)
	assert.Equal(t, "public-model", rows[1].ModelName)
	assert.Equal(t, 15, rows[1].Quota)
	assert.Equal(t, "requested-private", rows[2].ModelName)

	totalQuota := 0
	totalTokens := 0
	for _, row := range rows {
		totalQuota += row.Quota
		totalTokens += row.TokenUsed
	}
	assert.Equal(t, 115, totalQuota)
	assert.Equal(t, 115, totalTokens)

	adminRows, err := GetQuotaDataByUserId(1, 0, 7200, true)
	require.NoError(t, err)
	require.Len(t, adminRows, 4)
	adminModels := make(map[string]struct{}, len(adminRows))
	for _, row := range adminRows {
		adminModels[row.ModelName] = struct{}{}
	}
	assert.Contains(t, adminModels, "private-upstream")
	assert.Contains(t, adminModels, "admin-secret")
}

func TestQuotaDataModelScopeProtectsSelfFlowAndAnonymousRankings(t *testing.T) {
	seedQuotaVisibilityRows(t)

	flowRows, err := GetFlowQuotaData(0, 7200, "", 1, common.RoleCommonUser, false)
	require.NoError(t, err)
	require.Len(t, flowRows, 3)
	flowByModel := make(map[string]*FlowQuotaData, len(flowRows))
	for _, row := range flowRows {
		flowByModel[row.ModelName] = row
	}
	assert.Equal(t, 70, flowByModel[""].Quota)
	assert.Equal(t, 30, flowByModel["requested-private"].Quota)
	assert.Equal(t, 15, flowByModel["public-model"].Quota)

	totals, err := GetRankingQuotaTotals(0, 7200, []string{"public-model"}, false)
	require.NoError(t, err)
	require.Len(t, totals, 2)
	assert.Equal(t, "", totals[0].ModelName)
	assert.Equal(t, int64(100), totals[0].TotalTokens)
	assert.Equal(t, "public-model", totals[1].ModelName)
	assert.Equal(t, int64(15), totals[1].TotalTokens)

	buckets, err := GetRankingQuotaBuckets(0, 7200, 3600, 0, []string{"public-model"}, false)
	require.NoError(t, err)
	require.Len(t, buckets, 2)
	assert.Equal(t, int64(115), buckets[0].Tokens+buckets[1].Tokens)
	assert.NotContains(t, []string{buckets[0].ModelName, buckets[1].ModelName}, "private-upstream")
	assert.NotContains(t, []string{buckets[0].ModelName, buckets[1].ModelName}, "requested-private")

	adminTotals, err := GetRankingQuotaTotals(0, 7200, nil, true)
	require.NoError(t, err)
	require.Len(t, adminTotals, 4)
	adminModels := make(map[string]struct{}, len(adminTotals))
	for _, row := range adminTotals {
		adminModels[row.ModelName] = struct{}{}
	}
	assert.Contains(t, adminModels, "private-upstream")
	assert.Contains(t, adminModels, "requested-private")
}

func TestAnonymousRankingsPreserveUsageWhoseLegacyModelNameIsBlank(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Exec("DELETE FROM quota_data_scoped").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM quota_data_scoped")
	})

	require.NoError(t, DB.Create(&QuotaData{
		ModelName: "", CreatedAt: 3600, TokenUsed: 7, Count: 1,
	}).Error)
	require.NoError(t, DB.Create(&ScopedQuotaData{
		ModelName: "visible-model", ModelScope: QuotaModelScopeRequested,
		CreatedAt: 3600, TokenUsed: 3, Count: 1,
	}).Error)

	totals, err := GetRankingQuotaTotals(0, 7200, []string{"visible-model"}, false)
	require.NoError(t, err)
	require.Len(t, totals, 2)
	assert.Equal(t, "", totals[0].ModelName)
	assert.Equal(t, int64(7), totals[0].TotalTokens)
	assert.Equal(t, "visible-model", totals[1].ModelName)
	assert.Equal(t, int64(3), totals[1].TotalTokens)

	buckets, err := GetRankingQuotaBuckets(0, 7200, 3600, 0, []string{"visible-model"}, false)
	require.NoError(t, err)
	require.Len(t, buckets, 2)
	assert.Equal(t, int64(10), buckets[0].Tokens+buckets[1].Tokens)
}

func TestLogQuotaDataPersistsModelScopeAsPartOfAggregationKey(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		CacheQuotaDataLock.Lock()
		CacheQuotaData = make(map[string]*QuotaData)
		CacheQuotaDataLock.Unlock()
		DB.Exec("DELETE FROM quota_data")
	})

	base := QuotaDataLogParams{
		UserID: 1, Username: "alice", ModelName: "same-model", CreatedAt: 3601,
		UseGroup: "default", Quota: 10, TokenUsed: 5,
	}
	LogQuotaData(base)
	base.ModelScope = QuotaModelScopeRequested
	LogQuotaData(base)
	base.ModelScope = QuotaModelScopeAdminOnly
	LogQuotaData(base)

	CacheQuotaDataLock.Lock()
	require.Len(t, CacheQuotaData, 3)
	CacheQuotaDataLock.Unlock()
	require.NoError(t, SaveQuotaDataCache())

	var rows []ScopedQuotaData
	require.NoError(t, DB.Order("model_scope ASC").Find(&rows).Error)
	require.Len(t, rows, 3)
	assert.Equal(t, QuotaModelScopeLegacy, rows[0].ModelScope)
	assert.Equal(t, QuotaModelScopeRequested, rows[1].ModelScope)
	assert.Equal(t, QuotaModelScopeAdminOnly, rows[2].ModelScope)

	selfRows, err := GetQuotaDataByUserId(1, 0, 7200, false)
	require.NoError(t, err)
	require.Len(t, selfRows, 2)
	assert.Equal(t, "", selfRows[0].ModelName)
}

func TestScopedQuotaTablePreventsRollingUpgradeAggregationPollution(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Exec("DELETE FROM quota_data_scoped").Error)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		CacheQuotaDataLock.Lock()
		CacheQuotaData = make(map[string]*QuotaData)
		CacheQuotaDataLock.Unlock()
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM quota_data_scoped")
	})

	LogQuotaData(QuotaDataLogParams{
		UserID: 1, Username: "alice", ModelName: "shared-model", ModelScope: QuotaModelScopeRequested,
		CreatedAt: 3600, UseGroup: "default", TokenID: 7, ChannelID: 8, NodeName: "node-a",
		Quota: 10, TokenUsed: 10,
	})
	require.NoError(t, SaveQuotaDataCache())

	// Simulate an old process, whose key has no model_scope, writing the same
	// dimensions to the legacy table during a rolling upgrade.
	legacy := QuotaData{
		UserID: 1, Username: "alice", ModelName: "shared-model", CreatedAt: 3600,
		UseGroup: "default", TokenID: 7, ChannelID: 8, NodeName: "node-a",
		Count: 2, Quota: 20, TokenUsed: 20,
	}
	require.NoError(t, DB.Table("quota_data").Create(&legacy).Error)
	require.NoError(t, DB.Table("quota_data").
		Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
			legacy.UserID, legacy.Username, legacy.ModelName, legacy.CreatedAt, legacy.UseGroup, legacy.TokenID, legacy.ChannelID, legacy.NodeName).
		Updates(map[string]interface{}{
			"count":      gorm.Expr("count + ?", 1),
			"quota":      gorm.Expr("quota + ?", 5),
			"token_used": gorm.Expr("token_used + ?", 5),
		}).Error)

	var scoped ScopedQuotaData
	require.NoError(t, DB.First(&scoped).Error)
	assert.Equal(t, 1, scoped.Count)
	assert.Equal(t, 10, scoped.Quota)
	assert.Equal(t, 10, scoped.TokenUsed)

	selfRows, err := GetQuotaDataByUserId(1, 0, 7200, false)
	require.NoError(t, err)
	require.Len(t, selfRows, 2)
	assert.Equal(t, "", selfRows[0].ModelName)
	assert.Equal(t, 25, selfRows[0].Quota)
	assert.Equal(t, "shared-model", selfRows[1].ModelName)
	assert.Equal(t, 10, selfRows[1].Quota)
}

func TestQuotaDataModelScopeMigrationPreservesLegacyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&legacyQuotaDataMigrationRow{}))
	require.NoError(t, db.Create(&legacyQuotaDataMigrationRow{ModelName: "legacy-model"}).Error)

	require.NoError(t, db.AutoMigrate(&ScopedQuotaData{}))
	require.True(t, db.Migrator().HasColumn(&ScopedQuotaData{}, "model_scope"))
	require.False(t, db.Migrator().HasColumn(&legacyQuotaDataMigrationRow{}, "model_scope"))

	var row legacyQuotaDataMigrationRow
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, "legacy-model", row.ModelName)
}
