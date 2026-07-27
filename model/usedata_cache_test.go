package model

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resetQuotaDataCacheForTest(t *testing.T) {
	t.Helper()
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		CacheQuotaDataLock.Lock()
		CacheQuotaData = make(map[string]*QuotaData)
		CacheQuotaDataLock.Unlock()
	})
}

func TestSaveQuotaDataCacheDoesNotBlockNewLogsDuringFlush(t *testing.T) {
	truncateTables(t)
	resetQuotaDataCacheForTest(t)

	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	var signalOnce sync.Once
	callbackName := "test:block_quota_data_flush_query"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != (ScopedQuotaData{}).TableName() {
			return
		}
		signalOnce.Do(func() { close(queryStarted) })
		<-releaseQuery
	}))
	t.Cleanup(func() {
		select {
		case <-releaseQuery:
		default:
			close(releaseQuery)
		}
		_ = DB.Callback().Query().Remove(callbackName)
	})

	LogQuotaData(QuotaDataLogParams{
		UserID: 1, Username: "alice", ModelName: "first", CreatedAt: 3600,
		UseGroup: "default", Quota: 10, TokenUsed: 5,
	})
	flushDone := make(chan error, 1)
	go func() { flushDone <- SaveQuotaDataCache() }()

	select {
	case <-queryStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "quota data flush did not reach the database query")
	}

	logDone := make(chan struct{})
	go func() {
		LogQuotaData(QuotaDataLogParams{
			UserID: 1, Username: "alice", ModelName: "second", CreatedAt: 3600,
			UseGroup: "default", Quota: 20, TokenUsed: 10,
		})
		close(logDone)
	}()
	select {
	case <-logDone:
	case <-time.After(time.Second):
		close(releaseQuery)
		require.FailNow(t, "new quota data was blocked by database I/O")
	}

	CacheQuotaDataLock.Lock()
	assert.Len(t, CacheQuotaData, 1)
	CacheQuotaDataLock.Unlock()
	close(releaseQuery)
	require.NoError(t, <-flushDone)

	var persisted []ScopedQuotaData
	require.NoError(t, DB.Find(&persisted).Error)
	require.Len(t, persisted, 1)
	assert.Equal(t, "first", persisted[0].ModelName)

	require.NoError(t, SaveQuotaDataCache())
	require.NoError(t, DB.Order("model_name ASC").Find(&persisted).Error)
	require.Len(t, persisted, 2)
	assert.Equal(t, "second", persisted[1].ModelName)
}

func TestSaveQuotaDataCacheRequeuesOnlyFailedRows(t *testing.T) {
	truncateTables(t)
	resetQuotaDataCacheForTest(t)

	LogQuotaData(QuotaDataLogParams{
		UserID: 1, Username: "alice", ModelName: "shared", CreatedAt: 3600,
		UseGroup: "default", Quota: 10, TokenUsed: 5,
	})
	LogQuotaData(QuotaDataLogParams{
		UserID: 2, Username: "bob", ModelName: "shared", CreatedAt: 3600,
		UseGroup: "default", Quota: 10, TokenUsed: 5,
	})

	callbackName := "test:fail_bob_quota_data_create"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		row, ok := tx.Statement.Dest.(*ScopedQuotaData)
		if ok && row.Username == "bob" {
			tx.AddError(errors.New("forced bob quota data failure"))
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Create().Remove(callbackName) })

	err := SaveQuotaDataCache()
	require.ErrorContains(t, err, "forced bob quota data failure")

	var rows []ScopedQuotaData
	require.NoError(t, DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, "alice", rows[0].Username)
	assert.Equal(t, 10, rows[0].Quota)
	CacheQuotaDataLock.Lock()
	require.Len(t, CacheQuotaData, 1)
	for _, cached := range CacheQuotaData {
		assert.Equal(t, "bob", cached.Username)
		assert.Equal(t, 10, cached.Quota)
	}
	CacheQuotaDataLock.Unlock()

	LogQuotaData(QuotaDataLogParams{
		UserID: 2, Username: "bob", ModelName: "shared", CreatedAt: 3600,
		UseGroup: "default", Quota: 5, TokenUsed: 2,
	})
	require.NoError(t, DB.Callback().Create().Remove(callbackName))
	require.NoError(t, SaveQuotaDataCache())
	require.NoError(t, SaveQuotaDataCache())

	rows = nil
	require.NoError(t, DB.Order("username ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, "alice", rows[0].Username)
	assert.Equal(t, 1, rows[0].Count)
	assert.Equal(t, 10, rows[0].Quota)
	assert.Equal(t, "bob", rows[1].Username)
	assert.Equal(t, 2, rows[1].Count)
	assert.Equal(t, 15, rows[1].Quota)
	assert.Equal(t, 7, rows[1].TokenUsed)
}
