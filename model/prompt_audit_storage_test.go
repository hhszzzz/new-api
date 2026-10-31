package model

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestPromptAuditOutputResponseContainsOnlyReply(t *testing.T) {
	audit := &PromptAudit{
		Direction: "output", Status: PromptAuditStatusStored,
		FullPrompt:  []byte("request context\n\nmodel reply"),
		ScanPayload: []byte(`{"version":1,"direction":"output","segments":[{"role":"user","text":"request context"}],"output":"model reply","coverage_complete":true}`),
	}
	response := audit.ToResponse(true)
	require.NotNil(t, response.FullPrompt)
	assert.Equal(t, "model reply", *response.FullPrompt)
	require.NotNil(t, response.ScanPayload)
	assert.NotContains(t, *response.ScanPayload, "request context")
}

func TestPromptAuditSharedStorageMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var driver gorm.Dialector
			typeID := common.DatabaseTypeSQLite
			switch engine {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "audit.sqlite") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate")
			case "mysql":
				if os.Getenv("TEST_MYSQL_DSN") == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver, typeID = mysql.Open(os.Getenv("TEST_MYSQL_DSN")), common.DatabaseTypeMySQL
			case "postgres":
				if os.Getenv("TEST_POSTGRES_DSN") == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver, typeID = postgres.New(postgres.Config{DSN: os.Getenv("TEST_POSTGRES_DSN"), PreferSimpleProtocol: true}), common.DatabaseTypePostgreSQL
			}
			prefix := fmt.Sprintf("pas_%x_", time.Now().UnixNano())
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			if engine == "sqlite" {
				sqlDB.SetMaxOpenConns(8)
			}
			previousDB, previousType, previousSetting := DB, common.MainDatabaseType(), prompt_audit_setting.GetSetting()
			DB = db
			common.SetMainDatabaseType(typeID)
			t.Cleanup(func() {
				_ = db.Migrator().DropTable(&PromptAudit{}, &PromptAuditContentRef{}, &PromptAuditContent{}, &PromptAuditImportedContent{}, &PromptAuditArchive{}, &PromptAuditArchiveImport{}, &PromptAuditImportedRecord{}, &PromptAuditStorageState{})
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				previousSetting.PublishConfig()
				_ = sqlDB.Close()
			})
			var version string
			versionQuery := "SELECT VERSION()"
			if engine == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("%s %s", engine, version)
			for _, phase := range []string{"fresh", "upgrade"} {
				t.Run(phase, func(t *testing.T) {
					require.NoError(t, db.Migrator().DropTable(&PromptAudit{}, &PromptAuditContentRef{}, &PromptAuditContent{}, &PromptAuditImportedContent{}, &PromptAuditArchive{}, &PromptAuditArchiveImport{}, &PromptAuditImportedRecord{}, &PromptAuditStorageState{}))
					if phase == "upgrade" {
						table := db.NamingStrategy.TableName("PromptAudit")
						// The released audit schema has every current event column except
						// the six storage/reference columns introduced by this migration.
						require.NoError(t, db.AutoMigrate(&PromptAudit{}))
						for _, column := range []string{"archived_bundle_id", "related_input_id"} {
							require.NoError(t, db.Migrator().DropIndex(&PromptAudit{}, db.NamingStrategy.IndexName(table, column)))
						}
						for _, column := range []string{"content_manifest", "content_evicted", "archived_digest", "archived_at", "archived_bundle_id", "related_input_id"} {
							require.NoError(t, db.Migrator().DropColumn(&PromptAudit{}, column))
						}
						legacy := promptAuditLegacyStorageRow{ID: 40, RequestID: "legacy", UserID: 7, Direction: "output", Status: "stored", FullPrompt: []byte("unverifiable mixed historical snapshot"), ScanPayload: []byte(`{"version":1,"direction":"output","segments":[{"role":"user","text":"old question"}],"output":"old reply"}`), CreatedAt: 1, CompletedAt: 2, DeliveryStatus: "delivered", ReviewCodes: `["historical-result"]`}
						require.NoError(t, db.Table(table).Create(&legacy).Error)
					}
					require.NoError(t, MigratePromptAuditStorage(db))
					require.NoError(t, MigratePromptAuditStorage(db))
					assert.False(t, db.Migrator().HasIndex(&PromptAuditContentRef{}, db.NamingStrategy.IndexName(db.NamingStrategy.TableName("PromptAuditContentRef"), "active")))
					assert.True(t, db.Migrator().HasColumn(&PromptAuditContentRef{}, "active"))
					if phase == "upgrade" {
						var rejected bytes.Buffer
						_, err := WritePromptAuditArchive(db, &rejected, "unmigrated-source", "2026-10-09", 1, false)
						assert.ErrorContains(t, err, "migrate legacy audit bodies")
					}
					count, err := BackfillPromptAuditContents(db, 100)
					require.NoError(t, err)
					if phase == "upgrade" {
						assert.EqualValues(t, 1, count)
						historical, err := GetPromptAudit(40)
						require.NoError(t, err)
						assert.True(t, historical.ToResponse(true).LegacyContent)
						assert.Equal(t, "old reply", *historical.ToResponse(true).FullPrompt)
						assert.Equal(t, "delivered", historical.DeliveryStatus)
						assert.Equal(t, `["historical-result"]`, historical.ReviewCodes)
						assert.True(t, db.Migrator().HasIndex(&PromptAudit{}, db.NamingStrategy.IndexName(db.NamingStrategy.TableName("PromptAudit"), "request_id")))
					}
					count, err = BackfillPromptAuditContents(db, 100)
					require.NoError(t, err)
					assert.Zero(t, count)
					computed, err := GetPromptAuditStorageStats(db)
					require.NoError(t, err)
					require.NoError(t, CachePromptAuditStorageStats(db, computed))
					cached, err := GetCachedPromptAuditStorageStats(db)
					require.NoError(t, err)
					assert.Positive(t, cached.StatsUpdatedAt)
					assert.False(t, cached.Stale)
					cached.StatsUpdatedAt, cached.Stale = 0, false
					assert.Equal(t, computed, cached)
					// A refresh delayed past the cache TTL still serves the last
					// measurement, marked stale, instead of failing the read.
					require.NoError(t, db.Model(&PromptAuditStorageState{}).Where("id = ?", 1).UpdateColumn("stats_updated_at", common.GetTimestamp()-2*int64(promptAuditStorageStatsCacheTTL/time.Second)).Error)
					stale, err := GetCachedPromptAuditStorageStats(db)
					require.NoError(t, err)
					assert.True(t, stale.Stale)
					stale.StatsUpdatedAt, stale.Stale = 0, false
					assert.Equal(t, computed, stale)
					configured := previousSetting
					configured.SharedContentEnabled = true
					configured.PublishConfig()
					runPromptAuditArchiveLifecycle(t, db, engine)
					runPromptAuditOrphanCleanup(t, db)
				})
			}
		})
	}
}

func TestPromptAuditStorageStatsRefreshCheckpoints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "refresh.sqlite")+"?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousDB, previousType, previousSetting := DB, common.MainDatabaseType(), prompt_audit_setting.GetSetting()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&PromptAudit{}, &PromptAuditContentRef{}, &PromptAuditContent{}, &PromptAuditImportedContent{}, &PromptAuditArchive{}, &PromptAuditArchiveImport{}, &PromptAuditImportedRecord{}, &PromptAuditStorageState{})
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		previousSetting.PublishConfig()
		_ = sqlDB.Close()
	})
	// The released storage-state table predates the checkpoint columns; migrating
	// must add them without disturbing the existing measurement row.
	legacyState := struct {
		ID             int `gorm:"primaryKey;autoIncrement:false"`
		SourceID       string
		ExportSequence int64
		Stats          string
		StatsUpdatedAt int64
	}{ID: 1, SourceID: "legacy-source", Stats: `{}`}
	require.NoError(t, db.Table(db.NamingStrategy.TableName("PromptAuditStorageState")).AutoMigrate(&legacyState))
	require.NoError(t, db.Table(db.NamingStrategy.TableName("PromptAuditStorageState")).Create(&legacyState).Error)
	require.NoError(t, MigratePromptAuditStorage(db))
	require.NoError(t, MigratePromptAuditStorage(db))
	for _, column := range []string{"stats_pass", "stats_scan_after", "stats_scan_limit", "stats_scan_backlog"} {
		assert.True(t, db.Migrator().HasColumn(&PromptAuditStorageState{}, column), column)
	}
	var migratedState PromptAuditStorageState
	require.NoError(t, db.First(&migratedState, "id = ?", 1).Error)
	assert.Equal(t, "legacy-source", migratedState.SourceID)
	assert.Zero(t, migratedState.StatsScanLimit)

	now := common.GetTimestamp()
	events := []PromptAudit{
		{RequestID: "archived", Status: PromptAuditStatusStored, ContentManifest: `{"version":1,"blocks":[]}`, CreatedAt: now, UpdatedAt: now, CompletedAt: now},
		{RequestID: "pending-one", Status: PromptAuditStatusStored, ContentManifest: `{"version":1,"blocks":[]}`, CreatedAt: now, UpdatedAt: now, CompletedAt: now},
		{RequestID: "pending-two", Status: PromptAuditStatusStored, ContentManifest: `{"version":1,"blocks":[]}`, CreatedAt: now, UpdatedAt: now, CompletedAt: now},
	}
	require.NoError(t, db.Create(&events).Error)
	var archived PromptAudit
	require.NoError(t, db.Omit("full_prompt", "scan_payload", "content_snapshot").First(&archived, events[0].ID).Error)
	digest, err := promptAuditArchiveFingerprint(archived)
	require.NoError(t, err)
	require.NoError(t, db.Model(&PromptAudit{}).Where("id = ?", archived.ID).UpdateColumn("archived_digest", digest).Error)

	// A spent budget publishes nothing, but the pass and its aggregates survive.
	_, complete, err := AdvancePromptAuditStorageStats(db, 0, true)
	require.NoError(t, err)
	assert.False(t, complete)
	var state PromptAuditStorageState
	require.NoError(t, db.First(&state, "id = ?", 1).Error)
	assert.NotEmpty(t, state.StatsPass)
	assert.Zero(t, state.StatsScanAfter)
	assert.Equal(t, events[2].ID, state.StatsScanLimit)
	assert.Zero(t, state.StatsUpdatedAt)

	// The next invocation resumes where the pass stopped and publishes the result.
	stats, complete, err := AdvancePromptAuditStorageStats(db, time.Minute, false)
	require.NoError(t, err)
	assert.True(t, complete)
	assert.EqualValues(t, 2, stats.ArchiveBacklog)
	cached, err := GetCachedPromptAuditStorageStats(db)
	require.NoError(t, err)
	assert.EqualValues(t, 2, cached.ArchiveBacklog)
	assert.False(t, cached.Stale)
	require.NoError(t, db.First(&state, "id = ?", 1).Error)
	assert.Zero(t, state.StatsScanLimit)
	assert.Empty(t, state.StatsPass)

	// A recent measurement is served as is; the refresh interval decides the work.
	stats, complete, err = AdvancePromptAuditStorageStats(db, time.Minute, false)
	require.NoError(t, err)
	assert.True(t, complete)
	assert.EqualValues(t, 2, stats.ArchiveBacklog)
	require.NoError(t, db.First(&state, "id = ?", 1).Error)
	assert.Zero(t, state.StatsScanLimit)
}

func runPromptAuditOrphanCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	events := []PromptAudit{{Status: PromptAuditStatusStored}, {Status: PromptAuditStatusStored}, {Status: PromptAuditStatusStored}}
	require.NoError(t, db.Create(&events).Error)
	blobs := make([]PromptAuditContent, 0, 4)
	for _, text := range []string{"referenced by two events", "referenced by one event", "reference to a deleted event", "no references"} {
		encoded, err := encodePromptAuditContent([]byte(text))
		require.NoError(t, err)
		blobs = append(blobs, PromptAuditContent{Hash: promptAuditContentHash([]byte(text)), Data: encoded, RawBytes: int64(len(text)), StoredBytes: int64(len(encoded))})
	}
	require.NoError(t, db.Create(&blobs).Error)
	references := []PromptAuditContentRef{
		{AuditID: events[0].ID, Hash: blobs[0].Hash, Active: true},
		{AuditID: events[1].ID, Hash: blobs[0].Hash, Active: true},
		{AuditID: events[2].ID, Hash: blobs[1].Hash, Active: false}, // Active is historical metadata; every row protects the body.
		{AuditID: 1 << 60, Hash: blobs[2].Hash, Active: true},
	}
	require.NoError(t, db.Create(&references).Error)
	// The sweep is paged; both pages must leave referenced bodies untouched.
	changed, err := CleanupPromptAuditOrphanContent(db, 100)
	require.NoError(t, err)
	assert.Positive(t, changed)
	changed, err = CleanupPromptAuditOrphanContent(db, 100)
	require.NoError(t, err)
	assert.Zero(t, changed)
	for i, original := range blobs {
		var stored PromptAuditContent
		require.NoError(t, db.First(&stored, "hash = ?", original.Hash).Error)
		if i <= 1 {
			assert.Equal(t, original.Data, stored.Data, "any surviving reference protects complete body bytes")
			assert.Equal(t, original.StoredBytes, stored.StoredBytes)
		} else {
			assert.Empty(t, stored.Data)
			assert.Zero(t, stored.StoredBytes)
		}
	}
	var orphanReferences, survivingEvents int64
	require.NoError(t, db.Model(&PromptAuditContentRef{}).Where("audit_id = ?", int64(1<<60)).Count(&orphanReferences).Error)
	assert.Zero(t, orphanReferences)
	require.NoError(t, db.Model(&PromptAudit{}).Where("id IN ?", []int64{events[0].ID, events[1].ID, events[2].ID}).Count(&survivingEvents).Error)
	assert.EqualValues(t, 3, survivingEvents, "body cleanup preserves event metadata")
}

type promptAuditLegacyStorageRow struct {
	ID             int64  `gorm:"primaryKey"`
	RequestID      string `gorm:"type:varchar(64);index"`
	UserID         int
	Direction      string `gorm:"type:varchar(16)"`
	Status         string `gorm:"type:varchar(32)"`
	FullPrompt     []byte
	ScanPayload    []byte
	CreatedAt      int64
	CompletedAt    int64
	DeliveryStatus string
	ReviewCodes    string `gorm:"type:text"`
}

func runPromptAuditArchiveLifecycle(t *testing.T, db *gorm.DB, engine string) {
	t.Helper()
	now := common.GetTimestamp()
	original := `{"version":1,"direction":"input","segments":[{"role":"system","text":"system rules"},{"role":"user","scope":"user","text":"original request","future_metadata":"kept"}],"coverage_complete":true}`
	inspected := `{"version":1,"direction":"input","segments":[{"role":"user","scope":"user","text":"normalized request"}],"coverage_complete":true}`
	input := &PromptAudit{RequestID: "linked", UserID: 7, Direction: "input", Status: PromptAuditStatusStored, SessionKey: "session", GroupKey: "question", RawPayload: []byte(original), ScanPayload: []byte(inspected), FullPrompt: []byte("system rules\n\noriginal request"), CreatedAt: now - 9*86400, CompletedAt: now - 9*86400}
	require.NoError(t, CreatePromptAudit(input))
	inputView, err := GetPromptAudit(input.ID)
	require.NoError(t, err)
	assert.Equal(t, "system rules\n\noriginal request", string(inputView.FullPrompt))
	assert.Contains(t, string(inputView.ScanPayload), "normalized request")
	assert.NotContains(t, string(inputView.ScanPayload), "system rules")
	conversationSegments := make([]map[string]any, 400)
	for index := range conversationSegments {
		conversationSegments[index] = map[string]any{"role": "user", "scope": "user", "text": "long conversation message", "source_index": index}
	}
	conversationPayload, err := common.Marshal(map[string]any{"version": 1, "direction": "input", "segments": conversationSegments})
	require.NoError(t, err)
	conversationText := strings.TrimSuffix(strings.Repeat("long conversation message\n\n", len(conversationSegments)), "\n\n")
	conversation := &PromptAudit{RequestID: "long-conversation", Direction: "input", Status: PromptAuditStatusStored, RawPayload: conversationPayload, ScanPayload: conversationPayload, FullPrompt: []byte(conversationText), CreatedAt: now - 8*86400 + 1, CompletedAt: now - 8*86400 + 1}
	require.NoError(t, CreatePromptAudit(conversation))
	conversationView, err := GetPromptAudit(conversation.ID)
	require.NoError(t, err)
	assert.Equal(t, conversationText, string(conversationView.FullPrompt))
	reply := strings.Repeat("complete reply 中文🙂\n", 6000) + "reply ends here"
	outputPayload, err := common.Marshal(map[string]any{"version": 1, "direction": "output", "segments": []map[string]any{{"role": "user", "text": "actual inspected context"}}, "output": reply, "coverage_complete": true})
	require.NoError(t, err)
	outputScan := bytes.Replace(outputPayload, []byte(`"output":`+strconv.Quote(reply)), []byte(`"output":"normalized reply"`), 1)
	output := &PromptAudit{RequestID: "linked", UserID: 7, Direction: "output", Status: PromptAuditStatusFailed, SessionKey: "session", RawPayload: outputPayload, ScanPayload: outputScan, FullPrompt: []byte(reply), PolicySnapshot: `{"version":2,"full_prompt_max_runes":0}`, MaxAttempts: 2, CreatedAt: now - 8*86400, CompletedAt: now - 8*86400}
	require.NoError(t, CreatePromptAudit(output))
	assert.Equal(t, input.ID, output.RelatedInputID)
	var stored PromptAudit
	require.NoError(t, db.First(&stored, output.ID).Error)
	assert.Empty(t, stored.FullPrompt)
	assert.Empty(t, stored.ScanPayload)
	assert.Empty(t, stored.ContentSnapshot)
	shown, err := GetPromptAudit(output.ID)
	require.NoError(t, err)
	response := shown.ToResponse(true)
	require.NotNil(t, response.FullPrompt)
	assert.True(t, *response.FullPrompt == reply, "complete reply must survive storage")
	assert.NotContains(t, *response.ScanPayload, "actual inspected context")
	assert.Contains(t, *response.ScanPayload, "normalized reply")
	emptyReplyPayload := []byte(`{"version":1,"direction":"output","segments":[{"role":"user","text":"actual inspected context"}],"output":""}`)
	emptyReply := &PromptAudit{Direction: "output", Status: PromptAuditStatusStored, RawPayload: emptyReplyPayload, ScanPayload: emptyReplyPayload, CreatedAt: now - 8*86400, CompletedAt: now - 8*86400}
	require.NoError(t, CreatePromptAudit(emptyReply))
	emptyView, err := GetPromptAudit(emptyReply.ID)
	require.NoError(t, err)
	assert.Equal(t, "hot", emptyView.ContentState, "an empty reply is complete content, not an evicted NULL blob")
	require.NoError(t, LoadPromptAuditContent(db, emptyView, true))
	assert.Contains(t, string(emptyView.ScanPayload), "actual inspected context")
	require.NoError(t, RetryPromptAudit(output.ID, 2))
	claimed, ok, err := ClaimPromptAudit("storage-worker", now, now+60)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Contains(t, string(claimed.ScanPayload), "actual inspected context")
	assert.Contains(t, string(claimed.ScanPayload), "normalized reply")
	require.NoError(t, FinishPromptAudit(output.ID, "storage-worker", PromptAuditCompletion{Decision: "pass", Action: "allow"}))
	// Deterministic contention for one content identity across independent events.
	var wait sync.WaitGroup
	errorsCh := make(chan error, 4)
	for index := range 4 {
		wait.Go(func() {
			row := &PromptAudit{RequestID: fmt.Sprintf("repeat-%d", index), Direction: "input", Status: PromptAuditStatusStored, RawPayload: []byte(original), ScanPayload: []byte(original), FullPrompt: []byte("system rules\n\noriginal request"), CreatedAt: now - 9*86400, CompletedAt: now - 9*86400}
			errorsCh <- CreatePromptAudit(row)
		})
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
	var duplicateCount int64
	require.NoError(t, db.Model(&PromptAuditContent{}).Where("hash = ?", promptAuditContentHash([]byte("original request"))).Count(&duplicateCount).Error)
	assert.EqualValues(t, 1, duplicateCount)
	// The start is inclusive and the end exclusive. An earlier related request
	// is carried along, while other requests outside the window stay separate.
	window, windowBytes := archivePromptAuditFixture(t, db, "2026-10-07", PromptAuditArchiveWindow{CreatedFrom: output.CreatedAt, CreatedBefore: output.CreatedAt + 1})
	assert.EqualValues(t, 3, window.RecordCount)
	_, err = ImportPromptAuditArchive(db, bytes.NewReader(windowBytes), false)
	require.NoError(t, err)
	windowReply, err := GetPromptAuditImportedEvent(db, window.SourceID, output.ID, true)
	require.NoError(t, err)
	assert.Equal(t, reply, *windowReply.FullPrompt)
	windowInput, err := GetPromptAuditImportedEvent(db, window.SourceID, input.ID, true)
	require.NoError(t, err)
	assert.Equal(t, "system rules\n\noriginal request", *windowInput.FullPrompt)
	_, err = GetPromptAuditImportedEvent(db, window.SourceID, conversation.ID, true)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	before, err := GetPromptAuditStorageStats(db)
	require.NoError(t, err)
	assert.Greater(t, before.References, before.ContentBlocks)
	purged, err := CleanupPromptAuditSharedContent(db, now+8*86400, 100)
	require.NoError(t, err)
	assert.Zero(t, purged, "upload failure must retain every body")
	require.NoError(t, db.Model(&PromptAudit{}).Where("id = ?", input.ID).UpdateColumn("updated_at", now-86400).Error)
	first, firstBytes := archivePromptAuditFixture(t, db, "2026-10-08")
	// A metadata update between export and remote verification cannot be acknowledged.
	require.NoError(t, UpdatePromptAuditDelivery(output.ID, "delivery_failed"))
	require.NoError(t, VerifyPromptAuditArchive(db, first.ID, first.Digest, []PromptAuditArchiveVolume{{Name: "day.age", Bytes: 10, SHA256: strings.Repeat("a", 64), RemotePath: "/archives/day.age"}}))
	require.NoError(t, db.First(&stored, output.ID).Error)
	assert.Empty(t, stored.ArchivedDigest)
	second, secondBytes := archivePromptAuditFixture(t, db, "2026-10-09")
	require.NoError(t, VerifyPromptAuditArchive(db, second.ID, second.Digest, []PromptAuditArchiveVolume{{Name: "day2.age", Bytes: 11, SHA256: strings.Repeat("b", 64), RemotePath: "/archives/day2.age"}}))
	verifiedStats, err := GetPromptAuditStorageStats(db)
	require.NoError(t, err)
	assert.Zero(t, verifiedStats.ArchiveBacklog, "archive bookkeeping must not become another audit change")
	newer := &PromptAudit{RequestID: "recent", Direction: "input", Status: PromptAuditStatusStored, RawPayload: []byte(original), ScanPayload: []byte(original), FullPrompt: []byte("system rules\n\noriginal request"), CreatedAt: now + 8*86400, CompletedAt: now + 8*86400}
	// The writer and GC share hash locks; either ordering must retain the new reference.
	errorsCh = make(chan error, 2)
	wait.Go(func() { errorsCh <- CreatePromptAudit(newer) })
	wait.Go(func() { _, err := CleanupPromptAuditSharedContent(db, now+8*86400, 100); errorsCh <- err })
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
	shown, err = GetPromptAudit(newer.ID)
	require.NoError(t, err)
	assert.Equal(t, "system rules\n\noriginal request", string(shown.FullPrompt))
	missing, err := GetPromptAudit(output.ID)
	require.NoError(t, err)
	assert.Equal(t, "archived", missing.ContentState)
	assert.Nil(t, missing.ToResponse(true).FullPrompt)
	// Legacy rows may have a NULL eviction marker; cleanup must treat NULL as false.
	require.NoError(t, db.Model(&PromptAudit{}).Where("id = ?", output.ID).UpdateColumn("content_evicted", nil).Error)
	legacyPurged, err := CleanupPromptAuditSharedContent(db, now+16*86400, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 1, legacyPurged)
	var legacyEvicted PromptAudit
	require.NoError(t, db.First(&legacyEvicted, output.ID).Error)
	assert.True(t, legacyEvicted.ContentEvicted)
	var originalCount int64
	require.NoError(t, db.Model(&PromptAudit{}).Count(&originalCount).Error)
	_, err = ImportPromptAuditArchive(db, bytes.NewReader(secondBytes), false)
	require.NoError(t, err)
	_, err = ImportPromptAuditArchive(db, bytes.NewReader(firstBytes), false)
	require.NoError(t, err)
	_, err = ImportPromptAuditArchive(db, bytes.NewReader(secondBytes), false)
	require.NoError(t, err)
	var importedCount int64
	require.NoError(t, db.Model(&PromptAuditImportedRecord{}).Count(&importedCount).Error)
	assert.Equal(t, first.RecordCount, importedCount, "repeated imports must retain only the exported records")
	conversationImported, err := GetPromptAuditImportedEvent(db, first.SourceID, conversation.ID, true)
	require.NoError(t, err)
	assert.Equal(t, conversationText, *conversationImported.FullPrompt)
	emptyImported, err := GetPromptAuditImportedEvent(db, first.SourceID, emptyReply.ID, true)
	require.NoError(t, err)
	assert.Equal(t, "imported", emptyImported.ContentState)
	assert.Empty(t, *emptyImported.FullPrompt)
	imported, err := GetPromptAuditImportedEvent(db, first.SourceID, output.ID, true)
	require.NoError(t, err)
	assert.Equal(t, "delivery_failed", imported.DeliveryStatus, "older imports must not overwrite newer metadata")
	assert.True(t, *imported.FullPrompt == reply)
	_, total, err := ListPromptAuditImportedEvents(db, first.SourceID, output.ID, 1, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total, "cross-day session contains input and output")
	var currentCount int64
	require.NoError(t, db.Model(&PromptAudit{}).Count(&currentCount).Error)
	assert.Equal(t, originalCount, currentCount, "viewing imports must not create audit events")
	corrupt := bytes.Clone(secondBytes)
	corrupt[len(corrupt)/2] ^= 0x80
	_, err = ImportPromptAuditArchive(db, bytes.NewReader(corrupt), false)
	assert.Error(t, err)
	var afterFailedImport int64
	require.NoError(t, db.Model(&PromptAuditImportedRecord{}).Count(&afterFailedImport).Error)
	assert.Equal(t, importedCount, afterFailedImport, "rejected archives must not leave partial metadata")
	imported, err = GetPromptAuditImportedEvent(db, first.SourceID, output.ID, true)
	require.NoError(t, err)
	assert.Equal(t, "delivery_failed", imported.DeliveryStatus)
	// Restore one day alone into an empty database and verify generated IDs remain safe.
	recovery, err := gorm.Open(db.Dialector, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("par_%x_", time.Now().UnixNano())}, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	recoverySQL, err := recovery.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = recovery.Migrator().DropTable(&PromptAudit{}, &PromptAuditContentRef{}, &PromptAuditContent{}, &PromptAuditImportedContent{}, &PromptAuditArchive{}, &PromptAuditArchiveImport{}, &PromptAuditImportedRecord{}, &PromptAuditStorageState{})
		_ = recoverySQL.Close()
	})
	if engine == "sqlite" {
		recoverySQL.SetMaxOpenConns(8)
	}
	require.NoError(t, MigratePromptAuditStorage(recovery))
	_, err = ImportPromptAuditArchive(recovery, bytes.NewReader(secondBytes), true)
	require.NoError(t, err)
	var recoveryCacheRows int64
	require.NoError(t, recovery.Model(&PromptAuditImportedRecord{}).Count(&recoveryCacheRows).Error)
	assert.Zero(t, recoveryCacheRows, "recovery must not populate the temporary viewing cache")
	var restored PromptAudit
	require.NoError(t, recovery.First(&restored, output.ID).Error)
	require.NoError(t, LoadPromptAuditContent(recovery, &restored, true))
	assert.Contains(t, string(restored.ScanPayload), "actual inspected context")
	newRow := PromptAudit{Direction: "input", Status: PromptAuditStatusStored}
	require.NoError(t, recovery.Create(&newRow).Error)
	assert.Greater(t, newRow.ID, output.ID)
	// A late output has a self-contained package even when its request was evicted.
	latePayload := bytes.ReplaceAll(outputPayload, []byte(strconv.Quote(reply)), []byte(`"late reply"`))
	late := &PromptAudit{RequestID: "linked", UserID: 7, Direction: "output", Status: PromptAuditStatusStored, RawPayload: latePayload, ScanPayload: latePayload, FullPrompt: []byte("late reply"), CreatedAt: now, CompletedAt: now}
	require.NoError(t, CreatePromptAudit(late))
	needed, err := PromptAuditArchivesNeededForExport(db)
	require.NoError(t, err)
	assert.NotEmpty(t, needed, "late output must fetch an evicted request's previous package")
	_, lateBytes := archivePromptAuditFixture(t, db, "2026-10-10")
	_, err = ImportPromptAuditArchive(db, bytes.NewReader(lateBytes), false)
	require.NoError(t, err)
	lateView, err := GetPromptAuditImportedEvent(db, first.SourceID, late.ID, true)
	require.NoError(t, err)
	assert.Equal(t, "late reply", *lateView.FullPrompt)
	var evictedInput PromptAudit
	require.NoError(t, db.First(&evictedInput, input.ID).Error)
	assert.ErrorIs(t, LoadPromptAuditContent(db, &evictedInput, true), ErrPromptAuditContentUnavailable, "viewing an evicted body must not silently re-enable work")
	// A user may delete input metadata while retaining its output. The reply's
	// own context references must keep the independent archive recoverable.
	require.NoError(t, db.Delete(&PromptAudit{}, input.ID).Error)
	_, err = PromptAuditArchivesNeededForExport(db)
	require.NoError(t, err)
	_, withoutInput := archivePromptAuditFixture(t, db, "2026-10-11")
	_, err = ImportPromptAuditArchive(db, bytes.NewReader(withoutInput), false)
	require.NoError(t, err)
	lateView, err = GetPromptAuditImportedEvent(db, first.SourceID, late.ID, true)
	require.NoError(t, err)
	assert.Equal(t, "late reply", *lateView.FullPrompt)
	// Expiry clears the isolated cache without touching original audit metadata.
	require.NoError(t, cleanupPromptAuditImportCache(db, now+PromptAuditImportTTL+60))
	_, err = GetPromptAuditImportedEvent(db, first.SourceID, output.ID, true)
	assert.Error(t, err)
}

func archivePromptAuditFixture(t *testing.T, db *gorm.DB, day string, windows ...PromptAuditArchiveWindow) (PromptAuditArchive, []byte) {
	t.Helper()
	source, err := PromptAuditStorageSource(db)
	require.NoError(t, err)
	version, err := ReservePromptAuditArchiveVersion(db)
	require.NoError(t, err)
	var buffer bytes.Buffer
	var archive PromptAuditArchive
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		archive, err = WritePromptAuditArchive(tx, &buffer, source, day, version, false, windows...)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}))
	archive.Bytes, archive.Digest = int64(buffer.Len()), promptAuditContentHash(buffer.Bytes())
	require.NoError(t, db.Create(&archive).Error)
	return archive, bytes.Clone(buffer.Bytes())
}
