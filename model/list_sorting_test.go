package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPaginatedListSortingAppliesBeforeRowsAreLimited(t *testing.T) {
	originalDB := DB
	originalLogDB := LOG_DB
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
	})

	db, err := gorm.Open(sqlite.Open("file:list-sorting?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&Model{}, &Token{}, &Redemption{}, &Log{}, &Task{}, &Midjourney{}))

	require.NoError(t, db.Create(&[]Model{
		{Id: 1, ModelName: "zeta", Status: 1},
		{Id: 2, ModelName: "alpha", Status: 1},
	}).Error)
	models, total, err := SearchModels("", "", "", "", 0, 1, NewModelSortOptions("model_name", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, models, 1)
	assert.Equal(t, "alpha", models[0].ModelName)

	require.NoError(t, db.Create(&[]Token{
		{Id: 1, UserId: 10, Key: "sort-token-alpha", Name: "alpha"},
		{Id: 2, UserId: 10, Key: "sort-token-zeta", Name: "zeta"},
	}).Error)
	tokens, err := GetAllUserTokens(10, 0, 1, NewTokenSortOptions("name", "desc"))
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Equal(t, "zeta", tokens[0].Name)

	require.NoError(t, db.Create(&[]Redemption{
		{Id: 1, Key: "redemption-sort-1", Quota: 200},
		{Id: 2, Key: "redemption-sort-2", Quota: 100},
	}).Error)
	redemptions, total, err := GetAllRedemptions(0, 1, NewRedemptionSortOptions("quota", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, redemptions, 1)
	assert.Equal(t, 100, redemptions[0].Quota)

	require.NoError(t, db.Create(&[]Log{
		{Id: 1, CreatedAt: 1, Quota: 200, Other: "{}"},
		{Id: 2, CreatedAt: 2, Quota: 100, Other: "{}"},
	}).Error)
	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 1, 0, "", "", "", NewLogSortOptions("quota", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, logs, 1)
	assert.Equal(t, 100, logs[0].Quota)

	require.NoError(t, db.Create(&[]Task{
		{ID: 1, TaskID: "task-late", SubmitTime: 200},
		{ID: 2, TaskID: "task-early", SubmitTime: 100},
	}).Error)
	tasks := TaskGetAllTasks(0, 1, SyncTaskQueryParams{SortBy: "submit_time", SortOrder: "asc"})
	require.Len(t, tasks, 1)
	assert.Equal(t, "task-early", tasks[0].TaskID)

	require.NoError(t, db.Create(&[]Midjourney{
		{Id: 1, MjId: "mj-late", SubmitTime: 200},
		{Id: 2, MjId: "mj-early", SubmitTime: 100},
	}).Error)
	midjourneyTasks := GetAllTasks(0, 1, TaskQueryParams{SortBy: "submit_time", SortOrder: "asc"})
	require.Len(t, midjourneyTasks, 1)
	assert.Equal(t, "mj-early", midjourneyTasks[0].MjId)
}
