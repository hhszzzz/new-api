package model

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"

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
				_ = db.Migrator().DropTable(&PromptWordlist{})
				_ = db.Migrator().DropTable(&Option{})
				DB = previousDB
				common.SetDatabaseTypes(previousMainType, common.LogDatabaseType())
				_ = sqlDB.Close()
			})

			runPromptAuditWordlistUpgrade(t, db)
			runPromptWordlistStorage(t, db)
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

func TestPromptWordlistStorageAndLegacyAuditUpgradeSQLite(t *testing.T) {
	db := withPromptAuditTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&PromptAudit{}))
	runPromptAuditWordlistUpgrade(t, db)
	runPromptWordlistStorage(t, db)
}

func runPromptAuditWordlistUpgrade(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Preserve every pre-wordlist field/tag when constructing the released
	// schema, then verify a populated database through two startup migrations.
	current := reflect.TypeFor[PromptAudit]()
	added := []string{"InspectionType", "WordlistID", "WordlistName", "WordlistVersion", "MatchedScope", "InspectedScopes"}
	var fields []reflect.StructField
	for index := range current.NumField() {
		field := current.Field(index)
		if !slices.Contains(added, field.Name) {
			fields = append(fields, field)
		}
	}
	legacy := reflect.New(reflect.StructOf(fields)).Interface()
	table := db.NamingStrategy.TableName("PromptAudit")
	require.NoError(t, db.Table(table).AutoMigrate(legacy))
	require.NoError(t, db.Table(table).Create(map[string]any{
		"request_id": "legacy-before-wordlists", "group_name": "旧分组",
		"prompt_hash": strings.Repeat("b", 64), "full_prompt": []byte("preserved 原文"),
		"status": "done", "categories": "null", "unknown_categories": "null",
	}).Error)
	before, err := db.Migrator().GetIndexes(&PromptAudit{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&PromptAudit{}, &PromptWordlist{}))
	require.NoError(t, db.AutoMigrate(&PromptAudit{}, &PromptWordlist{}))
	after, err := db.Migrator().GetIndexes(&PromptAudit{})
	require.NoError(t, err)
	names := make([]string, 0, len(after))
	for _, index := range after {
		names = append(names, index.Name())
	}
	for _, index := range before {
		assert.Contains(t, names, index.Name())
	}
	var row PromptAudit
	require.NoError(t, db.Where("request_id = ?", "legacy-before-wordlists").First(&row).Error)
	assert.Equal(t, "旧分组", row.GroupName)
	assert.Equal(t, []byte("preserved 原文"), row.FullPrompt)
	response := row.ToResponse(false)
	assert.Equal(t, "model", response.InspectionType)
	assert.Equal(t, []string{}, response.InspectedScopes)
	assert.Equal(t, []string{}, response.Categories)
	assert.Nil(t, response.FullPrompt)
	require.NoError(t, db.Delete(&row).Error)
}

func runPromptWordlistStorage(t *testing.T, db *gorm.DB) {
	t.Helper()
	previousSettings, previousOptions := prompt_audit_setting.GetSetting(), common.OptionMap
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() { previousSettings.PublishConfig(); common.OptionMap = previousOptions })
	require.NoError(t, db.AutoMigrate(&PromptWordlist{}, &Option{}))
	row := &PromptWordlist{Name: "跨库测试", SourceURL: "https://example.com/words.txt", SourceHash: strings.Repeat("c", 64), Enabled: true, AutoUpdate: true}
	require.NoError(t, CreatePromptWordlist(row, dto.PromptScopeSystem))
	assert.Contains(t, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeSystem).LibraryIDs, strconv.FormatInt(row.ID, 10))
	claimed, err := ClaimPromptWordlist("first-import", common.GetTimestamp())
	require.NoError(t, err)
	require.NotNil(t, claimed)
	content := []byte(strings.Repeat("保留词条\n", 10000))
	published, err := FinishPromptWordlist(claimed, map[string]any{
		"content": content, "content_hash": strings.Repeat("a", 64), "word_count": 10000,
		"source_revision": "first-revision", "status": "ready", "next_sync_at": common.GetTimestamp() + 86400,
		"etag": "\"first-version\"",
	})
	require.NoError(t, err)
	require.True(t, published)
	require.NoError(t, RequestPromptWordlistSync(row.ID))
	stale, err := ClaimPromptWordlist("stale-import", common.GetTimestamp())
	require.NoError(t, err)
	require.NotNil(t, stale)
	disabledName, disabled := "已禁用", false
	require.NoError(t, UpdatePromptWordlist(row.ID, PromptWordlistUpdate{Name: &disabledName, Enabled: &disabled}))
	published, err = FinishPromptWordlist(stale, map[string]any{"content": []byte("stale"), "content_hash": "stale"})
	require.NoError(t, err)
	assert.False(t, published)
	stored, err := GetPromptWordlist(row.ID)
	require.NoError(t, err)
	assert.False(t, stored.Enabled)
	assert.Equal(t, content, stored.Content)
	assert.Equal(t, "\"first-version\"", stored.ETag)
	assert.Equal(t, strings.Repeat("a", 64), stored.ContentHash)
	require.NoError(t, RequestPromptWordlistSync(row.ID))
	failed, err := ClaimPromptWordlist("failed-import", common.GetTimestamp())
	require.NoError(t, err)
	require.NotNil(t, failed)
	published, err = FinishPromptWordlist(failed, map[string]any{"status": "failed", "last_error": "download_failed"})
	require.NoError(t, err)
	assert.True(t, published)
	require.NoError(t, db.AutoMigrate(&PromptWordlist{}))
	require.NoError(t, db.AutoMigrate(&PromptWordlist{}))
	stored, err = GetPromptWordlist(row.ID)
	require.NoError(t, err)
	assert.Equal(t, content, stored.Content)
	assert.Equal(t, "failed", stored.Status)
	listed, err := ListPromptWordlists()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Empty(t, listed[0].Content)
	assert.Empty(t, listed[0].SourceFiles)
	duplicate := &PromptWordlist{Name: "duplicate", SourceURL: row.SourceURL, SourceHash: row.SourceHash}
	assert.ErrorIs(t, CreatePromptWordlist(duplicate), ErrPromptWordlistExists)

	// Independent partial changes must preserve one another, including updates
	// submitted using the same old view of a library.
	enabled := true
	var mutations sync.WaitGroup
	mutationErrors := make(chan error, 2)
	mutations.Go(func() { mutationErrors <- UpdatePromptWordlist(row.ID, PromptWordlistUpdate{Enabled: &enabled}) })
	mutations.Go(func() { mutationErrors <- UpdatePromptWordlist(row.ID, PromptWordlistUpdate{AutoUpdate: &disabled}) })
	mutations.Wait()
	close(mutationErrors)
	for err := range mutationErrors {
		require.NoError(t, err)
	}
	stored, err = GetPromptWordlist(row.ID)
	require.NoError(t, err)
	assert.True(t, stored.Enabled)
	assert.False(t, stored.AutoUpdate)

	require.NoError(t, RequestPromptWordlistSync(row.ID))
	start := make(chan struct{})
	claims := make(chan *PromptWordlist, 2)
	claimErrors := make(chan error, 2)
	var claimers sync.WaitGroup
	for _, owner := range []string{"node-one", "node-two"} {
		claimers.Go(func() {
			<-start
			claimed, err := ClaimPromptWordlist(owner, common.GetTimestamp())
			claims <- claimed
			claimErrors <- err
		})
	}
	close(start)
	claimers.Wait()
	close(claims)
	close(claimErrors)
	for err := range claimErrors {
		require.NoError(t, err)
	}
	var winner *PromptWordlist
	for claim := range claims {
		if claim != nil {
			require.Nil(t, winner)
			winner = claim
		}
	}
	require.NotNil(t, winner)
	require.NoError(t, db.Model(&PromptWordlist{}).Where("id = ?", row.ID).Update("lease_until", common.GetTimestamp()-1).Error)
	recovered, err := ClaimPromptWordlist("recovered-node", common.GetTimestamp())
	require.NoError(t, err)
	require.NotNil(t, recovered)
	published, err = FinishPromptWordlist(winner, map[string]any{"content": []byte("stale worker")})
	require.NoError(t, err)
	assert.False(t, published)
	published, err = FinishPromptWordlist(recovered, map[string]any{"status": "ready"})
	require.NoError(t, err)
	assert.True(t, published)

	// Revert only this process's cache to simulate a second node that has not
	// observed the previous binding yet. Creating another library merges DB state.
	previousSettings.PublishConfig()
	second := &PromptWordlist{Name: "second", SourceURL: "https://example.com/second.txt", SourceHash: "second"}
	require.NoError(t, CreatePromptWordlist(second, dto.PromptScopeSystem))
	assert.Equal(t, []string{strconv.FormatInt(row.ID, 10), strconv.FormatInt(second.ID, 10)}, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeSystem).LibraryIDs)
	require.NoError(t, DeletePromptWordlist(second.ID))
	require.NoError(t, DeletePromptWordlist(row.ID))
	assert.Empty(t, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeSystem).LibraryIDs)
}
