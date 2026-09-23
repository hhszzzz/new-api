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
	"unicode/utf8"

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
		ContentSnapshot:  []byte(`{"version":1,"direction":"input","segments":[{"role":"user","scope":"user","text":"prompt text"}]}`),
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
	assert.Equal(t, audit.ContentSnapshot, retried.ScanPayload)
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

	exact := &PromptAudit{Status: PromptAuditStatusFailed, FullPrompt: []byte("partial"), FullPromptTruncated: true, ContentSnapshot: []byte(`{"version":1}`)}
	require.NoError(t, CreatePromptAudit(exact))
	require.NoError(t, RetryPromptAudit(exact.ID, 3))
	stored, err := GetPromptAudit(exact.ID)
	require.NoError(t, err)
	assert.Equal(t, exact.ContentSnapshot, stored.ScanPayload)
}

func TestReviewPromptAuditRequiresTerminalState(t *testing.T) {
	withPromptAuditTestDB(t)
	active := &PromptAudit{Status: PromptAuditStatusQueued}
	require.NoError(t, CreatePromptAudit(active))
	assert.ErrorIs(t, ReviewPromptAudit(active.ID, 7, "reviewer", "false_positive", "pending"), ErrPromptAuditNotReviewable)

	terminal := &PromptAudit{Status: PromptAuditStatusDone}
	require.NoError(t, CreatePromptAudit(terminal))
	require.NoError(t, ReviewPromptAudit(terminal.ID, 7, "reviewer", "confirmed_violation", "confirmed"))
	stored, err := GetPromptAudit(terminal.ID)
	require.NoError(t, err)
	assert.Equal(t, "confirmed_violation", stored.HumanReview)
	assert.Equal(t, "confirmed", stored.HumanReviewReason)
}

func TestPromptAuditRetentionPurgesOnlyTerminalFullPrompt(t *testing.T) {
	db := withPromptAuditTestDB(t)
	terminal := &PromptAudit{
		Status: PromptAuditStatusDone, FullPrompt: []byte("retained secret"), ContentSnapshot: []byte("structured secret"),
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
	assert.Empty(t, rows[0].ContentSnapshot)
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

// TestPromptAuditRepeatWorstDecision pins the vocabulary a collapsed row is
// displayed in: the worst outcome of a group is tallied as an action, and the
// decision badge reports it in decisions. The two line up one for one, and an
// unknown action claims no decision at all.
func TestPromptAuditRepeatWorstDecision(t *testing.T) {
	for _, testCase := range []struct{ action, decision string }{
		{promptAuditActionBlock, promptAuditDecisionBlock},
		{promptAuditActionUnavailable, promptAuditDecisionUnavailable},
		{promptAuditActionMark, promptAuditDecisionFlag},
		{promptAuditActionAllow, promptAuditDecisionPass},
		{"", ""},
		{"legacy", ""},
	} {
		assert.Equal(t, testCase.decision, PromptAuditDecisionForAction(testCase.action), "action %q", testCase.action)
	}
}

// TestPromptAuditRepeatListingSQLite runs the collapsed listing against the
// engine every contributor has. The MySQL and PostgreSQL runs live in
// TestPromptAuditStorageConfiguredDatabases.
func TestPromptAuditRepeatListingSQLite(t *testing.T) {
	db := withPromptAuditTestDB(t)
	var version string
	require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	t.Logf("sqlite version: %s", version)
	runPromptAuditRepeatListing(t, db)
}

// runPromptAuditRepeatListing covers one audited text resent by every step of an
// agent run. The group key is a CASE expression, the group count comes from a
// derived table and the action tally is a second grouped scan, so the statements
// are dialect-sensitive: sqlite cannot stand in for MySQL or PostgreSQL here.
func runPromptAuditRepeatListing(t *testing.T, db *gorm.DB) {
	t.Helper()

	const repeatUsername = "repeat-user"
	const repeatModel = "guarded-model"
	sharedHash := strings.Repeat("a", 64)
	markHash := strings.Repeat("b", 64)

	seed := func(requestID string, userID int, hash, action string, createdAt int64, promptLength int) {
		t.Helper()
		require.NoError(t, CreatePromptAudit(&PromptAudit{
			RequestID: requestID, UserID: userID, Username: repeatUsername, ModelName: repeatModel,
			Status: PromptAuditStatusDone, PromptHash: hash, Action: action,
			PromptLength: promptLength, CreatedAt: createdAt,
		}))
	}

	// One text resent three times, with a mixed outcome: the verdict cache only
	// holds successes, so the attempt that timed out is retried and recorded as
	// unavailable next to the allow that followed it.
	seed("repeat-block-first", 7, sharedHash, promptAuditActionBlock, 1000, 100)
	seed("repeat-block-second", 7, sharedHash, promptAuditActionUnavailable, 1010, 200)
	seed("repeat-block-third", 7, sharedHash, promptAuditActionAllow, 1020, 300)
	seed("repeat-mark-first", 7, markHash, promptAuditActionMark, 1030, 400)
	seed("repeat-mark-second", 7, markHash, promptAuditActionAllow, 1040, 500)
	// Rows from before the hash was recorded carry none, so each must stand alone
	// rather than merge with the other hashless row sharing its user and model.
	seed("repeat-legacy-first", 7, "", promptAuditActionAllow, 1050, 600)
	seed("repeat-legacy-second", 7, "", promptAuditActionAllow, 1060, 700)
	// The same hash under another user is a different text as far as the operator
	// is concerned, and must not be folded into the group above.
	seed("repeat-other-user", 8, sharedHash, promptAuditActionAllow, 1070, 800)

	filter := PromptAuditFilter{Username: repeatUsername}
	rows, groups, recordsTotal, err := ListPromptAuditRepeats(filter, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 5, groups)
	assert.EqualValues(t, 8, recordsTotal)
	require.Len(t, rows, 5)

	// Groups are ordered by their first request, newest group first.
	assert.Equal(t, "repeat-other-user", rows[0].Audit.RequestID)
	assert.Equal(t, "repeat-legacy-second", rows[1].Audit.RequestID)
	assert.Equal(t, "repeat-legacy-first", rows[2].Audit.RequestID)
	assert.Equal(t, "repeat-mark-first", rows[3].Audit.RequestID)
	assert.Equal(t, "repeat-block-first", rows[4].Audit.RequestID)

	groupFor := func(userID int, hash, requestID string) PromptAuditRepeatRow {
		for _, row := range rows {
			if row.Audit.UserID == userID && row.Audit.PromptHash == hash && row.Audit.RequestID == requestID {
				return row
			}
		}
		t.Fatalf("no collapsed row represents %s", requestID)
		return PromptAuditRepeatRow{}
	}

	// The representative is the group's first request: the one that carried the
	// audited text before later steps appended their context.
	blocked := groupFor(7, sharedHash, "repeat-block-first")
	assert.EqualValues(t, 3, blocked.Repeat.Count)
	assert.EqualValues(t, 1000, blocked.Repeat.FirstAt)
	assert.EqualValues(t, 1020, blocked.Repeat.LastAt)
	assert.Equal(t, promptAuditActionBlock, blocked.Repeat.WorstAction)
	assert.EqualValues(t, 1, blocked.Repeat.Blocks)
	assert.EqualValues(t, 1, blocked.Repeat.Unavailable)
	assert.EqualValues(t, 100, blocked.Audit.PromptLength)

	marked := groupFor(7, markHash, "repeat-mark-first")
	assert.EqualValues(t, 2, marked.Repeat.Count)
	assert.EqualValues(t, 1030, marked.Repeat.FirstAt)
	assert.EqualValues(t, 1040, marked.Repeat.LastAt)
	assert.Equal(t, promptAuditActionMark, marked.Repeat.WorstAction)
	assert.Zero(t, marked.Repeat.Blocks)
	assert.Zero(t, marked.Repeat.Unavailable)

	legacy := groupFor(7, "", "repeat-legacy-first")
	assert.EqualValues(t, 1, legacy.Repeat.Count)
	assert.Equal(t, promptAuditActionAllow, legacy.Repeat.WorstAction)

	otherUser := groupFor(8, sharedHash, "repeat-other-user")
	assert.EqualValues(t, 1, otherUser.Repeat.Count)

	// records_total must agree with the uncollapsed listing, or the screen would
	// show two different record counts for the same filter.
	uncollapsed, uncollapsedTotal, err := ListPromptAudits(filter, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 8, uncollapsedTotal)
	assert.Len(t, uncollapsed, 8)
	assert.EqualValues(t, uncollapsedTotal, recordsTotal)

	// Paging counts groups, and the pages must not repeat one.
	pageOne, pagedGroups, pagedRecords, err := ListPromptAuditRepeats(filter, 1, 2)
	require.NoError(t, err)
	require.Len(t, pageOne, 2)
	assert.EqualValues(t, 5, pagedGroups)
	assert.EqualValues(t, 8, pagedRecords)
	pageThree, _, _, err := ListPromptAuditRepeats(filter, 3, 2)
	require.NoError(t, err)
	require.Len(t, pageThree, 1)
	paged := map[int64]bool{}
	for _, row := range append(append([]PromptAuditRepeatRow{}, pageOne...), pageThree...) {
		paged[row.Audit.ID] = true
	}
	assert.Len(t, paged, 3)

	// Expanding a collapsed row returns exactly the requests it counted.
	expanded, err := ListPromptAuditGroupRows(filter, blocked.Audit.ID)
	require.NoError(t, err)
	require.Len(t, expanded, 3)
	assert.EqualValues(t, blocked.Repeat.Count, len(expanded))
	assert.Equal(t, "repeat-block-third", expanded[0].RequestID)
	assert.Equal(t, "repeat-block-first", expanded[2].RequestID)

	legacyRows, err := ListPromptAuditGroupRows(filter, legacy.Audit.ID)
	require.NoError(t, err)
	require.Len(t, legacyRows, 1)
	assert.Equal(t, legacy.Audit.ID, legacyRows[0].ID)

	// The expansion re-applies the listing's filter, so a group the filter hides
	// stays hidden even when its representative id is known.
	hidden, err := ListPromptAuditGroupRows(PromptAuditFilter{Username: repeatUsername, Model: "another-model"}, otherUser.Audit.ID)
	require.NoError(t, err)
	assert.Empty(t, hidden)
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
			var databaseVersion string
			require.NoError(t, db.Raw("SELECT VERSION()").Scan(&databaseVersion).Error)
			t.Logf("%s version: %s", test.name, databaseVersion)

			runPromptAuditWordlistUpgrade(t, db)
			runPromptWordlistStorage(t, db)
			require.NoError(t, db.Migrator().DropTable(&PromptAudit{}, &PromptWordlist{}, &Option{}))
			require.NoError(t, db.AutoMigrate(&PromptAudit{}, &PromptWordlist{}, &Option{}))
			require.NoError(t, db.AutoMigrate(&PromptAudit{}, &PromptWordlist{}, &Option{}))
			assert.True(t, db.Migrator().HasTable(&PromptAudit{}))

			queued := &PromptAudit{
				RequestID: "dialect-request", UserID: 7, Username: "dialect-user", GroupName: "default",
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

			// The records screen filters by username, which is snapshotted at
			// write time, so the same rows must be reachable by the name alone.
			byUser, userTotal, err := ListPromptAudits(PromptAuditFilter{Username: "dialect-user"}, 1, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 1, userTotal)
			require.Len(t, byUser, 1)
			_, missingTotal, err := ListPromptAudits(PromptAuditFilter{Username: "dialect-user-2"}, 1, 20)
			require.NoError(t, err)
			assert.Zero(t, missingTotal)

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

			runPromptAuditRepeatListing(t, db)
		})
	}
}

func TestPromptWordlistStorageAndLegacyAuditUpgradeSQLite(t *testing.T) {
	db := withPromptAuditTestDB(t)
	var version string
	require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	t.Logf("sqlite version: %s", version)
	require.NoError(t, db.Migrator().DropTable(&PromptAudit{}))
	runPromptAuditWordlistUpgrade(t, db)
	runPromptWordlistStorage(t, db)
}

func runPromptAuditWordlistUpgrade(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Preserve every pre-wordlist field/tag when constructing the released
	// schema, then verify a populated database through two startup migrations.
	current := reflect.TypeFor[PromptAudit]()
	added := []string{
		"InspectionType", "WordlistID", "WordlistName", "WordlistVersion", "MatchedScope", "InspectedScopes",
		"Direction", "GenerationID", "DeliveryStatus", "CoverageComplete", "Refusal", "Action", "ContentSnapshot", "PolicySnapshot",
		"ReviewStatus", "ReviewDecision", "ReviewCodes", "ReviewReason", "ReviewerEndpointID",
		"HumanReview", "HumanReviewReason", "ReviewedBy", "ReviewerName", "ReviewedAt",
		"Username", "EndpointModel", "Ip", "UserAgent", "Method", "RequestPath", "Origin", "Referer",
	}
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
		"request_id": "legacy-before-wordlists", "group_name": "旧分组", "user_id": 4242,
		"prompt_hash": strings.Repeat("b", 64), "full_prompt": []byte("preserved 原文"),
		"status": "done", "categories": "null", "unknown_categories": "null",
	}).Error)
	wordlistType := reflect.TypeFor[PromptWordlist]()
	wordlistFields := make([]reflect.StructField, 0, wordlistType.NumField()-1)
	for index := range wordlistType.NumField() {
		field := wordlistType.Field(index)
		if field.Name != "Action" {
			wordlistFields = append(wordlistFields, field)
		}
	}
	legacyWordlist := reflect.New(reflect.StructOf(wordlistFields)).Interface()
	wordlistTable := db.NamingStrategy.TableName("PromptWordlist")
	require.NoError(t, db.Table(wordlistTable).AutoMigrate(legacyWordlist))
	require.NoError(t, db.Table(wordlistTable).Create(map[string]any{
		"name": "legacy-list", "source_url": "https://example.com/legacy.txt", "source_hash": strings.Repeat("9", 64), "enabled": true,
	}).Error)
	before, err := db.Migrator().GetIndexes(&PromptAudit{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&PromptAudit{}, &PromptWordlist{}))
	require.NoError(t, MigratePromptAuditDefaults())
	require.NoError(t, db.AutoMigrate(&PromptAudit{}, &PromptWordlist{}))
	require.NoError(t, MigratePromptAuditDefaults())
	after, err := db.Migrator().GetIndexes(&PromptAudit{})
	require.NoError(t, err)
	names := make([]string, 0, len(after))
	for _, index := range after {
		names = append(names, index.Name())
	}
	for _, index := range before {
		assert.Contains(t, names, index.Name())
	}
	// The records screen narrows by username, so the column carries an index the
	// same way group_name and model_name do. Assert it by the name GORM derives
	// from the prefixed test table rather than the literal one.
	assert.True(t, db.Migrator().HasIndex(&PromptAudit{}, db.NamingStrategy.IndexName(table, "username")))
	// The declared varchar widths are a load-bearing cross-database contract: they
	// are the second line of defence that makes MySQL and PostgreSQL reject an
	// over-long value. A tag GORM cannot parse degrades to an unbounded column
	// without failing the migration, so lock the widths here, where all three
	// engines are exercised.
	columnTypes, err := db.Migrator().ColumnTypes(&PromptAudit{})
	require.NoError(t, err)
	widths := map[string]string{
		"username": "64", "endpoint_model": "255", "ip": "64", "user_agent": "512",
		"method": "16", "request_path": "255", "origin": "255", "referer": "255",
	}
	for _, columnType := range columnTypes {
		width, tracked := widths[columnType.Name()]
		if !tracked {
			continue
		}
		delete(widths, columnType.Name())
		declared, known := columnType.ColumnType()
		require.True(t, known, columnType.Name())
		assert.Contains(t, strings.ToLower(declared), "("+width+")", columnType.Name())
	}
	assert.Empty(t, widths)
	var row PromptAudit
	require.NoError(t, db.Where("request_id = ?", "legacy-before-wordlists").First(&row).Error)
	assert.Equal(t, "旧分组", row.GroupName)
	assert.Equal(t, []byte("preserved 原文"), row.FullPrompt)
	assert.Equal(t, "input", row.Direction)
	assert.Equal(t, "not_applicable", row.DeliveryStatus)
	assert.True(t, row.CoverageComplete)
	assert.Equal(t, "allow", row.Action)
	// A row written before the column existed keeps an empty username: like the
	// two log tables, the name is snapshotted when the row is written and never
	// backfilled from the users table.
	assert.Empty(t, row.Username)
	response := row.ToResponse(false)
	assert.Equal(t, "model", response.InspectionType)
	assert.Equal(t, []string{}, response.InspectedScopes)
	assert.Equal(t, []string{}, response.Categories)
	assert.Nil(t, response.FullPrompt)
	require.NoError(t, db.Delete(&row).Error)
	var migratedWordlist PromptWordlist
	require.NoError(t, db.Where("source_hash = ?", strings.Repeat("9", 64)).First(&migratedWordlist).Error)
	assert.Equal(t, prompt_audit_setting.WordlistActionBlock, migratedWordlist.Action)
	require.NoError(t, db.Delete(&migratedWordlist).Error)
}

func runPromptWordlistStorage(t *testing.T, db *gorm.DB) {
	t.Helper()
	previousSettings, previousOptions := prompt_audit_setting.GetSetting(), common.OptionMap
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() { previousSettings.PublishConfig(); common.OptionMap = previousOptions })
	require.NoError(t, db.AutoMigrate(&PromptWordlist{}, &Option{}))
	row := &PromptWordlist{Name: "跨库测试", SourceURL: "https://example.com/words.txt", SourceHash: strings.Repeat("c", 64), Enabled: true, AutoUpdate: true}
	require.NoError(t, CreatePromptWordlist(row, dto.PromptScopeSystem))
	assert.Equal(t, prompt_audit_setting.WordlistActionReview, row.Action)
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

	// Editing a source updates its assignments atomically, keeps the last good
	// content until the replacement succeeds, and fences the old download.
	require.NoError(t, RequestPromptWordlistSync(row.ID))
	oldSourceWorker, err := ClaimPromptWordlist("old-source", common.GetTimestamp())
	require.NoError(t, err)
	require.NotNil(t, oldSourceWorker)
	updatedSource := "https://example.com/replacement.txt"
	updatedSourceHash := strings.Repeat("d", 64)
	updatedScopes := []dto.PromptAuditScope{dto.PromptScopeUser, dto.PromptScopeTask}
	reenabled := true
	require.NoError(t, UpdatePromptWordlist(row.ID, PromptWordlistUpdate{
		SourceURL: &updatedSource, SourceHash: &updatedSourceHash, Scopes: &updatedScopes, Enabled: &reenabled,
	}))
	published, err = FinishPromptWordlist(oldSourceWorker, map[string]any{"content": []byte("old source worker")})
	require.NoError(t, err)
	assert.False(t, published)
	stored, err = GetPromptWordlist(row.ID)
	require.NoError(t, err)
	assert.Equal(t, updatedSource, stored.SourceURL)
	assert.Equal(t, updatedSourceHash, stored.SourceHash)
	assert.Equal(t, content, stored.Content)
	assert.Equal(t, strings.Repeat("a", 64), stored.ContentHash)
	assert.Empty(t, stored.SourceRevision)
	assert.Empty(t, stored.ETag)
	assert.Equal(t, "pending", stored.Status)
	assert.Positive(t, stored.RequestedAt)
	assert.NotContains(t, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeSystem).LibraryIDs, strconv.FormatInt(row.ID, 10))
	assert.Contains(t, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeUser).LibraryIDs, strconv.FormatInt(row.ID, 10))
	assert.Contains(t, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeTask).LibraryIDs, strconv.FormatInt(row.ID, 10))

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
	assert.Equal(t, []string{strconv.FormatInt(second.ID, 10)}, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeSystem).LibraryIDs)
	assert.ErrorIs(t, UpdatePromptWordlist(row.ID, PromptWordlistUpdate{SourceURL: &second.SourceURL, SourceHash: &second.SourceHash}), ErrPromptWordlistExists)
	require.NoError(t, DeletePromptWordlist(second.ID))
	require.NoError(t, DeletePromptWordlist(row.ID))
	assert.Empty(t, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeSystem).LibraryIDs)
}

func TestPromptAuditClampsOversizedColumns(t *testing.T) {
	db := withPromptAuditTestDB(t)
	// Multi-byte values on purpose: clamping counts runes, so a byte-wise
	// truncation would split a character and the three databases would store
	// different bytes for the same request.
	audit := &PromptAudit{
		RequestID: "clamp-overlong", Status: PromptAuditStatusDone,
		Ip:            strings.Repeat("7", 80),
		UserAgent:     strings.Repeat("界", 600),
		Method:        strings.Repeat("M", 40),
		RequestPath:   strings.Repeat("/p", 400),
		Origin:        strings.Repeat("o", 300),
		Referer:       strings.Repeat("r", 300),
		Username:      strings.Repeat("u", 100),
		EndpointModel: strings.Repeat("m", 300),
		TokenName:     strings.Repeat("t", 300),
		ModelName:     strings.Repeat("n", 300),
	}
	require.NoError(t, CreatePromptAudit(audit))
	// BeforeCreate clamps in place, so the caller's copy matches the stored row.
	assert.Equal(t, 64, utf8.RuneCountInString(audit.Ip))
	assert.Equal(t, 64, utf8.RuneCountInString(audit.Username))
	assert.Equal(t, 16, utf8.RuneCountInString(audit.Method))
	for _, value := range []string{audit.RequestPath, audit.Origin, audit.Referer, audit.EndpointModel, audit.TokenName, audit.ModelName} {
		assert.Equal(t, 255, utf8.RuneCountInString(value))
	}
	assert.Equal(t, 512, utf8.RuneCountInString(audit.UserAgent))

	var stored PromptAudit
	require.NoError(t, db.Where("request_id = ?", "clamp-overlong").First(&stored).Error)
	assert.Equal(t, strings.Repeat("界", 512), stored.UserAgent)
	assert.Equal(t, strings.Repeat("/p", 127)+"/", stored.RequestPath)

	// Values within their column are stored byte-for-byte.
	short := &PromptAudit{
		RequestID: "clamp-within-limit", Status: PromptAuditStatusDone,
		Ip: "203.0.113.7", UserAgent: "curl/8.7.1", Method: "POST",
		RequestPath: "/v1/messages", Origin: "https://example.com", Referer: "https://example.com/app",
		Username: "tester", EndpointModel: "qwen3guard-gen-0.6b",
		TokenName: "ui-demo-token", ModelName: "claude-sonnet-5",
	}
	require.NoError(t, CreatePromptAudit(short))
	var storedShort PromptAudit
	require.NoError(t, db.Where("request_id = ?", "clamp-within-limit").First(&storedShort).Error)
	assert.Equal(t, "203.0.113.7", storedShort.Ip)
	assert.Equal(t, "curl/8.7.1", storedShort.UserAgent)
	assert.Equal(t, "POST", storedShort.Method)
	assert.Equal(t, "/v1/messages", storedShort.RequestPath)
	assert.Equal(t, "https://example.com", storedShort.Origin)
	assert.Equal(t, "https://example.com/app", storedShort.Referer)
	assert.Equal(t, "tester", storedShort.Username)
	assert.Equal(t, "qwen3guard-gen-0.6b", storedShort.EndpointModel)
	assert.Equal(t, "ui-demo-token", storedShort.TokenName)
	assert.Equal(t, "claude-sonnet-5", storedShort.ModelName)
}

func TestPromptAuditClampsEndpointModelOnCompletion(t *testing.T) {
	db := withPromptAuditTestDB(t)
	queued := &PromptAudit{RequestID: "clamp-completion", Status: PromptAuditStatusQueued, MaxAttempts: 1, ScanPayload: []byte("x")}
	require.NoError(t, CreatePromptAudit(queued))
	now := common.GetTimestamp()
	claimed, ok, err := ClaimPromptAudit("clamp-node", now, now+1000)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, queued.ID, claimed.ID)
	// FinishPromptAudit writes through an Updates map, which BeforeCreate does
	// not cover.
	require.NoError(t, FinishPromptAudit(claimed.ID, "clamp-node", PromptAuditCompletion{
		EndpointModel: strings.Repeat("m", 300), Decision: "pass",
	}))
	var stored PromptAudit
	require.NoError(t, db.First(&stored, claimed.ID).Error)
	assert.Equal(t, 255, utf8.RuneCountInString(stored.EndpointModel))
}
