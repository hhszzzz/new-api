package model

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func withPromptAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousMainType := common.MainDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&PromptAudit{}))
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.LogDatabaseType())
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainType, common.LogDatabaseType())
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestPromptAuditQueueLeaseRetryAndTerminalCleanup(t *testing.T) {
	db := withPromptAuditTestDB(t)
	audit := &PromptAudit{
		Status: PromptAuditStatusQueued, PromptHash: strings.Repeat("a", 64),
		FullPrompt: []byte("prompt text"), ScanPayload: []byte("prompt text"),
		PolicyCategories: `["jailbreak"]`, MaxAttempts: 3,
	}
	require.NoError(t, CreatePromptAudit(audit))

	claimed, ok, err := ClaimPromptAudit("node-a", 100, 200)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, audit.ID, claimed.ID)
	assert.Equal(t, PromptAuditStatusProcessing, claimed.Status)
	assert.Equal(t, 1, claimed.Attempts)

	_, ok, err = ClaimPromptAudit("node-b", 100, 200)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, FailPromptAudit(audit.ID, "node-a", "endpoint_timeout", 110, false))
	var retry PromptAudit
	require.NoError(t, db.First(&retry, audit.ID).Error)
	assert.Equal(t, PromptAuditStatusRetry, retry.Status)
	assert.Equal(t, int64(110), retry.NextAttemptAt)
	assert.NotEmpty(t, retry.ScanPayload)

	reclaimed, ok, err := ClaimPromptAudit("node-b", 110, 220)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 2, reclaimed.Attempts)
	require.NoError(t, FailPromptAudit(audit.ID, "node-b", "invalid_response", 0, true))

	var failed PromptAudit
	require.NoError(t, db.First(&failed, audit.ID).Error)
	assert.Equal(t, PromptAuditStatusFailed, failed.Status)
	assert.Empty(t, failed.ScanPayload)
	assert.NotZero(t, failed.CompletedAt)

	require.NoError(t, db.Model(&PromptAudit{}).Where("id = ?", audit.ID).Updates(map[string]any{
		"safety": "Unsafe", "decision": "unavailable", "would_action": "unavailable",
		"categories": `["pii"]`, "unknown_categories": `["unknown:1234"]`,
		"endpoint_id": "old-node", "latency_ms": int64(99), "chunk_count": 2,
	}).Error)
	require.NoError(t, RetryPromptAudit(audit.ID, 4))
	var retried PromptAudit
	require.NoError(t, db.First(&retried, audit.ID).Error)
	assert.Equal(t, PromptAuditStatusQueued, retried.Status)
	assert.Equal(t, []byte("prompt text"), retried.ScanPayload)
	assert.Zero(t, retried.Attempts)
	assert.Equal(t, 4, retried.MaxAttempts)
	assert.Empty(t, retried.Safety)
	assert.Empty(t, retried.Decision)
	assert.Equal(t, "pending", retried.WouldAction)
	assert.Empty(t, retried.Categories)
	assert.Empty(t, retried.UnknownCategories)
	assert.Empty(t, retried.EndpointID)
	assert.Zero(t, retried.LatencyMS)
	assert.Zero(t, retried.ChunkCount)
}

func TestPromptAuditCategoryArraysNeverSerializeAsNull(t *testing.T) {
	audit := &PromptAudit{Categories: "null", UnknownCategories: "null"}
	response := audit.ToResponse(false)

	require.NotNil(t, response.Categories)
	require.NotNil(t, response.UnknownCategories)
	assert.Empty(t, response.Categories)
	assert.Empty(t, response.UnknownCategories)

	encoded, err := encodePromptAuditStrings(nil)
	require.NoError(t, err)
	assert.Equal(t, "[]", encoded)
}

func TestPromptAuditExpiredLeaseNeverExceedsAttemptCap(t *testing.T) {
	db := withPromptAuditTestDB(t)
	exhausted := &PromptAudit{
		Status: PromptAuditStatusProcessing, Attempts: 4, MaxAttempts: 4,
		LeaseOwner: "dead-worker", LeaseUntil: 10, ScanPayload: []byte("secret"),
	}
	recoverable := &PromptAudit{
		Status: PromptAuditStatusProcessing, Attempts: 3, MaxAttempts: 4,
		LeaseOwner: "dead-worker", LeaseUntil: 10, ScanPayload: []byte("prompt"),
	}
	require.NoError(t, CreatePromptAudit(exhausted))
	require.NoError(t, CreatePromptAudit(recoverable))

	claimed, ok, err := ClaimPromptAudit("replacement", 11, 100)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, recoverable.ID, claimed.ID)
	assert.Equal(t, 4, claimed.Attempts)

	var terminal PromptAudit
	require.NoError(t, db.First(&terminal, exhausted.ID).Error)
	assert.Equal(t, PromptAuditStatusFailed, terminal.Status)
	assert.Equal(t, "max_attempts_exhausted", terminal.ErrorCode)
	assert.Empty(t, terminal.ScanPayload)
	assert.NotZero(t, terminal.CompletedAt)
}

func TestPromptAuditConcurrentClaimsHaveSingleWinner(t *testing.T) {
	withPromptAuditTestDB(t)
	audit := &PromptAudit{
		Status: PromptAuditStatusQueued, MaxAttempts: 4, ScanPayload: []byte("prompt"),
	}
	require.NoError(t, CreatePromptAudit(audit))

	const contenders = 8
	start := make(chan struct{})
	results := make(chan bool, contenders)
	errorsCh := make(chan error, contenders)
	var wait sync.WaitGroup
	for index := 0; index < contenders; index++ {
		wait.Add(1)
		go func(owner string) {
			defer wait.Done()
			<-start
			_, claimed, err := ClaimPromptAudit(owner, 100, 200)
			errorsCh <- err
			results <- claimed
		}(fmt.Sprintf("worker-%d", index))
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	close(results)

	for err := range errorsCh {
		require.NoError(t, err)
	}
	winners := 0
	for claimed := range results {
		if claimed {
			winners++
		}
	}
	assert.Equal(t, 1, winners)
}

func TestPromptAuditFinishAndResponsesNeverLeakFullPromptByDefault(t *testing.T) {
	db := withPromptAuditTestDB(t)
	audit := &PromptAudit{
		Status: PromptAuditStatusQueued, PromptHash: strings.Repeat("b", 64),
		FullPrompt: []byte("top secret prompt"), ScanPayload: []byte("top secret prompt"),
		PolicyCategories: `["pii"]`, MaxAttempts: 3,
	}
	require.NoError(t, CreatePromptAudit(audit))
	claimed, ok, err := ClaimPromptAudit("node-a", 100, 200)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, FinishPromptAudit(claimed.ID, "node-a", PromptAuditCompletion{
		Safety: "Controversial", Decision: "block", WouldAction: "block",
		Categories: []string{"pii"}, UnknownCategories: []string{"unknown:1234"},
		EndpointID: "guard-a", ChunkCount: 2, LatencyMS: 25,
	}))

	var finished PromptAudit
	require.NoError(t, db.First(&finished, audit.ID).Error)
	assert.Equal(t, PromptAuditStatusDone, finished.Status)
	assert.Empty(t, finished.ScanPayload)
	assert.Equal(t, []string{"pii"}, finished.ToResponse(false).Categories)
	assert.Nil(t, finished.ToResponse(false).FullPrompt)
	require.NotNil(t, finished.ToResponse(true).FullPrompt)
	assert.Equal(t, "top secret prompt", *finished.ToResponse(true).FullPrompt)
}

func TestPromptAuditDeleteUsesPreviewHighWaterAndKeepsActiveRows(t *testing.T) {
	db := withPromptAuditTestDB(t)
	for _, audit := range []*PromptAudit{
		{Status: PromptAuditStatusDone, Decision: "pass", Categories: `["violent"]`, CompletedAt: 10},
		{Status: PromptAuditStatusFailed, Decision: "unavailable", CompletedAt: 20},
		{Status: PromptAuditStatusQueued, ScanPayload: []byte("active")},
	} {
		require.NoError(t, CreatePromptAudit(audit))
	}

	eligible, active, maxID, err := PreviewPromptAuditDelete(PromptAuditFilter{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, eligible)
	assert.EqualValues(t, 1, active)
	assert.EqualValues(t, 2, maxID)

	deleted, err := DeletePromptAudits(PromptAuditFilter{}, eligible, maxID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)
	var remaining []PromptAudit
	require.NoError(t, db.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	assert.Equal(t, PromptAuditStatusQueued, remaining[0].Status)
}

func TestRetryPromptAuditRejectsTruncatedFullPrompt(t *testing.T) {
	withPromptAuditTestDB(t)
	audit := &PromptAudit{Status: PromptAuditStatusFailed, FullPrompt: []byte("partial"), FullPromptTruncated: true}
	require.NoError(t, CreatePromptAudit(audit))
	assert.ErrorIs(t, RetryPromptAudit(audit.ID, 3), ErrPromptAuditPayloadMissing)

	purged := &PromptAudit{Status: PromptAuditStatusFailed}
	require.NoError(t, CreatePromptAudit(purged))
	assert.ErrorIs(t, RetryPromptAudit(purged.ID, 3), ErrPromptAuditPayloadMissing)
}

func TestPromptAuditRetentionPurgesOnlyTerminalFullPrompt(t *testing.T) {
	db := withPromptAuditTestDB(t)
	terminal := &PromptAudit{
		Status: PromptAuditStatusDone, FullPrompt: []byte("retained secret"),
		CompletedAt: 10,
	}
	active := &PromptAudit{
		Status: PromptAuditStatusQueued, FullPrompt: []byte("active secret"),
		ScanPayload: []byte("active secret"),
	}
	require.NoError(t, CreatePromptAudit(terminal))
	require.NoError(t, CreatePromptAudit(active))

	purged, err := CleanupPromptAuditPromptsBefore(20, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 1, purged)

	var rows []PromptAudit
	require.NoError(t, db.Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Empty(t, rows[0].FullPrompt)
	assert.Equal(t, []byte("active secret"), rows[1].FullPrompt)
	assert.Equal(t, []byte("active secret"), rows[1].ScanPayload)
	assert.Nil(t, rows[0].ToResponse(true).FullPrompt)
	assert.False(t, rows[0].ToResponse(true).FullPromptAvailable)
	assert.True(t, rows[1].ToResponse(false).FullPromptAvailable)
}

func TestPromptAuditStatsQueriesRemainIndependent(t *testing.T) {
	withPromptAuditTestDB(t)
	for _, audit := range []*PromptAudit{
		{Status: PromptAuditStatusDone, Decision: "pass", Categories: `["violent"]`},
		{Status: PromptAuditStatusDone, Decision: "block", Categories: `["pii"]`, UnknownCategories: `["unknown:1234"]`},
		{Status: PromptAuditStatusFailed, Decision: "unavailable"},
	} {
		require.NoError(t, CreatePromptAudit(audit))
	}
	stats, err := GetPromptAuditStats(PromptAuditFilter{}, []string{"violent", "pii"})
	require.NoError(t, err)
	assert.EqualValues(t, 3, stats.Total)
	assert.EqualValues(t, 2, stats.Statuses[string(PromptAuditStatusDone)])
	assert.EqualValues(t, 1, stats.Statuses[string(PromptAuditStatusFailed)])
	assert.EqualValues(t, 1, stats.Decisions["pass"])
	assert.EqualValues(t, 1, stats.Decisions["block"])
	assert.EqualValues(t, 1, stats.Categories["violent"])
	assert.EqualValues(t, 1, stats.Categories["pii"])
	assert.EqualValues(t, 1, stats.Unknown)
}

func TestPromptAuditStorageConfiguredDatabases(t *testing.T) {
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

			prefix := fmt.Sprintf("pa_%x_", time.Now().UnixNano())
			db, err := gorm.Open(test.dialector(dsn), &gorm.Config{
				NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
			})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)

			previousDB := DB
			previousMainType := common.MainDatabaseType()
			DB = db
			common.SetDatabaseTypes(test.dbType, common.LogDatabaseType())
			t.Cleanup(func() {
				_ = db.Migrator().DropTable(&PromptAudit{})
				DB = previousDB
				common.SetDatabaseTypes(previousMainType, common.LogDatabaseType())
				_ = sqlDB.Close()
			})

			require.NoError(t, db.AutoMigrate(&PromptAudit{}))
			assert.True(t, db.Migrator().HasTable(&PromptAudit{}))

			queued := &PromptAudit{
				RequestID: "dialect-request", UserID: 7, GroupName: "default",
				Protocol: "openai_responses", ModelName: "guarded-model",
				Status: PromptAuditStatusQueued, PromptHash: strings.Repeat("d", 64),
				FullPrompt: []byte("dialect prompt"), ScanPayload: []byte("dialect prompt"),
				PolicyCategories: `["pii"]`, MaxAttempts: 4,
			}
			require.NoError(t, CreatePromptAudit(queued))

			const contenders = 6
			start := make(chan struct{})
			claims := make(chan bool, contenders)
			errorsCh := make(chan error, contenders)
			var wait sync.WaitGroup
			for index := 0; index < contenders; index++ {
				wait.Add(1)
				go func(owner string) {
					defer wait.Done()
					<-start
					_, claimed, claimErr := ClaimPromptAudit(owner, 100, 200)
					errorsCh <- claimErr
					claims <- claimed
				}(fmt.Sprintf("worker-%d", index))
			}
			close(start)
			wait.Wait()
			close(errorsCh)
			close(claims)
			for claimErr := range errorsCh {
				require.NoError(t, claimErr)
			}
			winners := 0
			for claimed := range claims {
				if claimed {
					winners++
				}
			}
			assert.Equal(t, 1, winners)

			var processing PromptAudit
			require.NoError(t, db.First(&processing, queued.ID).Error)
			require.Equal(t, PromptAuditStatusProcessing, processing.Status)
			require.NoError(t, FailPromptAudit(processing.ID, processing.LeaseOwner, "endpoint_timeout", 110, false))

			reclaimed, ok, err := ClaimPromptAudit("retry-worker", 110, 220)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, queued.ID, reclaimed.ID)
			require.NoError(t, FinishPromptAudit(reclaimed.ID, "retry-worker", PromptAuditCompletion{
				Safety: "Controversial", Decision: "block", WouldAction: "block",
				Categories: []string{"pii"}, EndpointID: "guard-primary", ChunkCount: 1,
			}))

			listed, total, err := ListPromptAudits(PromptAuditFilter{
				Status: string(PromptAuditStatusDone), Category: "pii", RequestID: "dialect-request",
			}, 1, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, listed, 1)

			stats, err := GetPromptAuditStats(PromptAuditFilter{RequestID: "dialect-request"}, []string{"pii"})
			require.NoError(t, err)
			assert.EqualValues(t, 1, stats.Total)
			assert.EqualValues(t, 1, stats.Categories["pii"])

			stale := &PromptAudit{
				RequestID: "stale", Status: PromptAuditStatusDone,
				FullPrompt: []byte("expired secret"), CompletedAt: 10,
			}
			require.NoError(t, CreatePromptAudit(stale))
			purged, err := CleanupPromptAuditPromptsBefore(20, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, purged)

			eligible, active, maxID, err := PreviewPromptAuditDelete(PromptAuditFilter{})
			require.NoError(t, err)
			assert.EqualValues(t, 2, eligible)
			assert.Zero(t, active)
			deleted, err := DeletePromptAudits(PromptAuditFilter{}, eligible, maxID)
			require.NoError(t, err)
			assert.EqualValues(t, 2, deleted)
		})
	}
}
