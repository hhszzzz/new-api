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
	// The username filter reaches into the users table for rows written before
	// the name was snapshotted.
	require.NoError(t, db.AutoMigrate(&PromptAudit{}, &User{}))
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
	audit.ScanPayload = append([]byte(nil), audit.ContentSnapshot...)
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
	assert.Equal(t, audit.ScanPayload, failed.ScanPayload)
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

func TestPromptAuditTerminalPayloadRetention(t *testing.T) {
	db := withPromptAuditTestDB(t)
	runPromptAuditTerminalPayloadRetention(t, db)
}

func runPromptAuditTerminalPayloadRetention(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Where("1=1").Delete(&PromptAudit{}).Error)
	text := strings.Repeat("中\"\n", 20000)
	payload, err := common.Marshal(map[string]any{"version": 1, "direction": "input", "coverage_complete": true, "segments": []dto.PromptAuditSegment{{Role: "user", Scope: dto.PromptScopeUser, User: true, Text: text}}})
	require.NoError(t, err)
	for _, terminal := range []string{"finish", "fail", "exhausted"} {
		t.Run(terminal, func(t *testing.T) {
			row := &PromptAudit{Status: PromptAuditStatusProcessing, LeaseOwner: terminal, LeaseUntil: 10, Attempts: 1, MaxAttempts: 1, ScanPayload: payload, ContentSnapshot: payload}
			require.NoError(t, CreatePromptAudit(row))
			assert.Equal(t, payload, row.ScanPayload, "queued work must remain complete")
			assert.True(t, row.ToResponse(true).ScanPayloadTruncated)
			switch terminal {
			case "finish":
				require.NoError(t, FinishPromptAudit(row.ID, terminal, PromptAuditCompletion{Decision: "pass"}))
			case "fail":
				require.NoError(t, FailPromptAudit(row.ID, terminal, "timeout", 0, true))
			default:
				_, _, err = ClaimPromptAudit("replacement", 11, 100)
				require.NoError(t, err)
			}
			stored, err := GetPromptAudit(row.ID)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(stored.ScanPayload), 64*1024)
			assert.True(t, stored.ScanPayloadTruncated)
			assert.Equal(t, stored.ScanPayload, stored.ContentSnapshot)
			var decoded struct{ Segments []dto.PromptAuditSegment }
			require.NoError(t, common.Unmarshal(stored.ScanPayload, &decoded))
			require.Len(t, decoded.Segments, 1)
			assert.NotEmpty(t, decoded.Segments[0].Text)
			assert.True(t, strings.HasPrefix(text, decoded.Segments[0].Text))
			assert.True(t, utf8.Valid(stored.ScanPayload))
			if terminal != "finish" {
				assert.ErrorIs(t, RetryPromptAudit(row.ID, 3), ErrPromptAuditPayloadMissing)
			}
			require.NoError(t, db.Model(row).Update("completed_at", 10).Error)
		})
	}
	purged, err := CleanupPromptAuditPromptsBefore(20, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 3, purged)
	var rows []PromptAudit
	require.NoError(t, db.Find(&rows).Error)
	for _, row := range rows {
		assert.Empty(t, row.ScanPayload)
		assert.Empty(t, row.ContentSnapshot)
	}
}

func TestPromptAuditQuestionGroupingAcrossModels(t *testing.T) {
	db := withPromptAuditTestDB(t)
	runPromptAuditQuestionGroupingAcrossModels(t, db)
}

func runPromptAuditQuestionGroupingAcrossModels(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Where("1=1").Delete(&PromptAudit{}).Error)
	var firstID int64
	for index, row := range []*PromptAudit{
		{UserID: 1, GroupKey: "question", PromptHash: "first", ModelName: "a"},
		{UserID: 1, GroupKey: "question", PromptHash: "second", ModelName: "b"},
		{UserID: 2, GroupKey: "question", ModelName: "b"},
		{UserID: 1, PromptHash: "legacy", ModelName: "a"},
		{UserID: 1, PromptHash: "legacy", ModelName: "b"},
		{UserID: 1}, {UserID: 1},
	} {
		row.Status = PromptAuditStatusDone
		require.NoError(t, CreatePromptAudit(row))
		if index == 0 {
			firstID = row.ID
		}
	}
	_, total, err := ListPromptAuditRepeats(PromptAuditFilter{}, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 5, total)
	members, total, err := ListPromptAuditGroupRows(PromptAuditFilter{}, firstID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.EqualValues(t, 2, total)
	filter := PromptAuditFilter{GroupIDs: []int64{firstID}}
	eligible, active, maxID, err := PreviewPromptAuditDelete(filter)
	require.NoError(t, err)
	assert.Zero(t, active)
	assert.EqualValues(t, 2, eligible)
	deleted, err := DeletePromptAudits(filter, eligible, maxID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)
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
	assert.Equal(t, exhausted.ScanPayload, terminal.ScanPayload)
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
	assert.Equal(t, audit.ScanPayload, finished.ScanPayload)
	assert.Nil(t, finished.ToResponse(false).ScanPayload)
	require.NotNil(t, finished.ToResponse(true).ScanPayload)
	assert.Equal(t, string(audit.ScanPayload), *finished.ToResponse(true).ScanPayload)
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

func TestPromptAuditListingFiltersSQLite(t *testing.T) {
	runPromptAuditListingFilters(t, withPromptAuditTestDB(t))
}

// runPromptAuditRepeatListing covers one audited text resent by every step of an
// agent run. The group key is a CASE expression, the group count comes from a
// derived table, the worst decision and the enforcement counts are CASE
// aggregates, and the username filter reaches into the users table, so the
// statements are dialect-sensitive: sqlite cannot stand in for MySQL or
// PostgreSQL here.
func runPromptAuditRepeatListing(t *testing.T, db *gorm.DB) {
	t.Helper()

	const repeatUsername = "repeat-user"
	const repeatModel = "guarded-model"
	sharedHash := strings.Repeat("a", 64)
	markHash := strings.Repeat("b", 64)
	asyncHash := strings.Repeat("c", 64)
	pendingHash := strings.Repeat("e", 64)

	type seedRow struct {
		requestID        string
		userID           int
		hash             string
		decision, action string
		status           PromptAuditStatus
		createdAt        int64
		promptLength     int
	}
	seeded := map[string]int64{}
	for _, seed := range []seedRow{
		// One text resent three times with a mixed outcome: a pass, an attempt the
		// node could not answer, then an enforced block.
		{"repeat-block-first", 7, sharedHash, "pass", "allow", PromptAuditStatusDone, 1000, 100},
		{"repeat-block-second", 7, sharedHash, "unavailable", "unavailable", PromptAuditStatusDone, 1010, 200},
		{"repeat-mark-first", 7, markHash, "flag", "mark", PromptAuditStatusDone, 1020, 300},
		// Rows from before the hash was recorded carry none, so each must stand
		// alone rather than merge with the other hashless row of its user and model.
		{"repeat-legacy-first", 7, "", "pass", "allow", PromptAuditStatusDone, 1030, 400},
		{"repeat-block-third", 7, sharedHash, "block", "block", PromptAuditStatusDone, 1040, 500},
		// Async observation stores a block decision with a mark action: the text was
		// judged a block even though nothing was refused.
		{"repeat-async-first", 7, asyncHash, "pass", "allow", PromptAuditStatusDone, 1050, 600},
		{"repeat-legacy-second", 7, "", "pass", "allow", PromptAuditStatusDone, 1060, 700},
		{"repeat-async-second", 7, asyncHash, "block", "mark", PromptAuditStatusDone, 1070, 800},
		{"repeat-mark-second", 7, markHash, "pass", "allow", PromptAuditStatusDone, 1080, 900},
		// A queued async request has no decision yet and an allow action: its group
		// has not passed until it is decided.
		{"repeat-pending-first", 7, pendingHash, "pass", "allow", PromptAuditStatusDone, 1090, 1000},
		{"repeat-pending-second", 7, pendingHash, "", "allow", PromptAuditStatusQueued, 1100, 1100},
		// The same hash under another user is a different text as far as the
		// operator is concerned, and must not be folded into the group above.
		{"repeat-other-user", 8, sharedHash, "pass", "allow", PromptAuditStatusDone, 1110, 1200},
	} {
		audit := &PromptAudit{
			RequestID: seed.requestID, UserID: seed.userID, Username: repeatUsername, ModelName: repeatModel,
			Status: seed.status, PromptHash: seed.hash, Decision: seed.decision, Action: seed.action,
			PromptLength: seed.promptLength, CreatedAt: seed.createdAt,
		}
		require.NoError(t, CreatePromptAudit(audit))
		seeded[seed.requestID] = audit.ID
	}

	filter := PromptAuditFilter{Username: repeatUsername}
	rows, groups, err := ListPromptAuditRepeats(filter, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 7, groups)
	require.Len(t, rows, 7)

	// Groups are ordered by their newest request, so the text resent most
	// recently comes first even when it was first submitted long ago; each row
	// still shows the group's first request.
	representatives := make([]string, 0, len(rows))
	for _, row := range rows {
		representatives = append(representatives, row.Audit.RequestID)
	}
	assert.Equal(t, []string{
		"repeat-other-user", "repeat-pending-first", "repeat-mark-first", "repeat-async-first",
		"repeat-legacy-second", "repeat-block-first", "repeat-legacy-first",
	}, representatives)

	groupFor := func(requestID string) PromptAuditRepeatRow {
		t.Helper()
		index := slices.IndexFunc(rows, func(row PromptAuditRepeatRow) bool { return row.Audit.RequestID == requestID })
		require.GreaterOrEqual(t, index, 0, requestID)
		return rows[index]
	}

	blocked := groupFor("repeat-block-first")
	assert.Equal(t, PromptAuditRepeat{Count: 3, FirstAt: 1000, LastAt: 1040, WorstDecision: "block", Blocks: 1, Unavailable: 1}, blocked.Repeat)
	assert.EqualValues(t, 100, blocked.Audit.PromptLength)
	assert.Equal(t, PromptAuditRepeat{Count: 2, FirstAt: 1020, LastAt: 1080, WorstDecision: "flag"}, groupFor("repeat-mark-first").Repeat)
	// The worst decision is read from the decisions themselves: an observed block
	// stays a block although its action only marked the request.
	assert.Equal(t, PromptAuditRepeat{Count: 2, FirstAt: 1050, LastAt: 1070, WorstDecision: "block"}, groupFor("repeat-async-first").Repeat)
	assert.Equal(t, PromptAuditRepeat{Count: 2, FirstAt: 1090, LastAt: 1100, WorstDecision: ""}, groupFor("repeat-pending-first").Repeat)
	assert.Equal(t, PromptAuditRepeat{Count: 1, FirstAt: 1030, LastAt: 1030, WorstDecision: "pass"}, groupFor("repeat-legacy-first").Repeat)
	assert.Equal(t, PromptAuditRepeat{Count: 1, FirstAt: 1110, LastAt: 1110, WorstDecision: "pass"}, groupFor("repeat-other-user").Repeat)

	// The group count must agree with the plain listing's record count for the
	// same filter.
	_, uncollapsedTotal, err := ListPromptAudits(filter, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 12, uncollapsedTotal)

	// Paging counts groups, and the pages must not repeat one.
	paged := map[int64]bool{}
	for page := 1; page <= 4; page++ {
		pageRows, pagedGroups, err := ListPromptAuditRepeats(filter, page, 2)
		require.NoError(t, err)
		assert.EqualValues(t, 7, pagedGroups)
		for _, row := range pageRows {
			assert.False(t, paged[row.Audit.ID], "group %d repeated on page %d", row.Audit.ID, page)
			paged[row.Audit.ID] = true
		}
	}
	assert.Len(t, paged, 7)

	// Expanding a collapsed row returns exactly the requests it counted, newest
	// first, together with how many the group holds.
	expanded, expandedTotal, err := ListPromptAuditGroupRows(filter, seeded["repeat-block-first"])
	require.NoError(t, err)
	assert.EqualValues(t, 3, expandedTotal)
	require.Len(t, expanded, 3)
	assert.Equal(t, "repeat-block-third", expanded[0].RequestID)
	assert.Equal(t, "repeat-block-first", expanded[2].RequestID)

	legacyRows, legacyTotal, err := ListPromptAuditGroupRows(filter, seeded["repeat-legacy-first"])
	require.NoError(t, err)
	assert.EqualValues(t, 1, legacyTotal)
	require.Len(t, legacyRows, 1)
	assert.Equal(t, seeded["repeat-legacy-first"], legacyRows[0].ID)

	// The expansion re-applies the listing's filter, so a group the filter hides
	// stays hidden even when its representative id is known, and a representative
	// that no longer exists expands to nothing instead of failing.
	hidden, _, err := ListPromptAuditGroupRows(PromptAuditFilter{Username: repeatUsername, Model: "another-model"}, seeded["repeat-other-user"])
	require.NoError(t, err)
	assert.Empty(t, hidden)
	missing, missingTotal, err := ListPromptAuditGroupRows(filter, seeded["repeat-other-user"]+1000)
	require.NoError(t, err)
	assert.Empty(t, missing)
	assert.Zero(t, missingTotal)

	// Deleting a merged row reaches every request of its group within the same
	// filter, and a group that still holds an active request cannot be deleted.
	eligible, active, maxID, err := PreviewPromptAuditDelete(PromptAuditFilter{Username: repeatUsername, GroupIDs: []int64{seeded["repeat-block-first"]}})
	require.NoError(t, err)
	assert.EqualValues(t, 3, eligible)
	assert.Zero(t, active)
	_, pendingActive, _, err := PreviewPromptAuditDelete(PromptAuditFilter{Username: repeatUsername, GroupIDs: []int64{seeded["repeat-pending-first"]}})
	require.NoError(t, err)
	assert.EqualValues(t, 1, pendingActive)
	// A representative that is gone names no group: the scope is empty, never the
	// whole filter.
	goneEligible, goneActive, _, err := PreviewPromptAuditDelete(PromptAuditFilter{Username: repeatUsername, GroupIDs: []int64{seeded["repeat-other-user"] + 1000}})
	require.NoError(t, err)
	assert.Zero(t, goneEligible)
	assert.Zero(t, goneActive)
	deleted, err := DeletePromptAudits(PromptAuditFilter{Username: repeatUsername, GroupIDs: []int64{seeded["repeat-block-first"]}}, eligible, maxID)
	require.NoError(t, err)
	assert.EqualValues(t, 3, deleted)
	_, afterDelete, err := ListPromptAuditRepeats(filter, 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 6, afterDelete)
	var remaining int64
	require.NoError(t, db.Model(&PromptAudit{}).Where("user_id = ? AND prompt_hash = ?", 8, sharedHash).Count(&remaining).Error)
	assert.EqualValues(t, 1, remaining, "the other user's copy of the text is not part of the group")
}

// runPromptAuditListingFilters covers the filters whose predicates reach beyond
// one column: the username filter finds rows written before the name was
// snapshotted through the account holding the name now, and the detector filter
// separates bare wordlist hits from everything the screen labels a model audit.
func runPromptAuditListingFilters(t *testing.T, db *gorm.DB) {
	t.Helper()

	require.NoError(t, db.Create(&User{Id: 4242, Username: "legacy-owner", Password: strings.Repeat("p", 16), AffCode: "legacy-owner-aff"}).Error)
	for _, seed := range []struct {
		requestID, username, inspectionType string
		userID                              int
	}{
		{"filters-legacy-model", "", "", 4242},
		{"filters-named-wordlist", "legacy-owner", "wordlist", 4242},
		{"filters-named-mixed", "legacy-owner", "wordlist_model", 4242},
		{"filters-other-probe", "someone-else", "probe_fast_pass", 4343},
	} {
		require.NoError(t, CreatePromptAudit(&PromptAudit{
			RequestID: seed.requestID, UserID: seed.userID, Username: seed.username,
			InspectionType: seed.inspectionType, Status: PromptAuditStatusDone, ModelName: "filters-model",
		}))
	}

	requestIDs := func(filter PromptAuditFilter) []string {
		t.Helper()
		filter.Model = "filters-model"
		listed, _, err := ListPromptAudits(filter, 1, 20)
		require.NoError(t, err)
		ids := make([]string, 0, len(listed))
		for _, audit := range listed {
			ids = append(ids, audit.RequestID)
		}
		slices.Sort(ids)
		return ids
	}

	assert.Equal(t, []string{"filters-legacy-model", "filters-named-mixed", "filters-named-wordlist"}, requestIDs(PromptAuditFilter{Username: "legacy-owner"}))
	assert.Empty(t, requestIDs(PromptAuditFilter{Username: "no-such-user"}))
	assert.Equal(t, []string{"filters-named-wordlist"}, requestIDs(PromptAuditFilter{Detector: "wordlist"}))
	assert.Equal(t, []string{"filters-legacy-model", "filters-named-mixed"}, requestIDs(PromptAuditFilter{Detector: "model"}))
	assert.Equal(t, []string{"filters-other-probe"}, requestIDs(PromptAuditFilter{Detector: "probe"}))
	assert.Equal(t, []string{"filters-other-probe"}, requestIDs(PromptAuditFilter{Detector: "probe_fast_pass"}))

	// The same predicates scope a deletion.
	eligible, _, _, err := PreviewPromptAuditDelete(PromptAuditFilter{Username: "legacy-owner", Model: "filters-model"})
	require.NoError(t, err)
	assert.EqualValues(t, 3, eligible)
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
				_ = db.Migrator().DropTable(&User{})
				DB = previousDB
				common.SetDatabaseTypes(previousMainType, common.LogDatabaseType())
				_ = sqlDB.Close()
			})
			var databaseVersion string
			require.NoError(t, db.Raw("SELECT VERSION()").Scan(&databaseVersion).Error)
			t.Logf("%s version: %s", test.name, databaseVersion)

			runPromptAuditWordlistUpgrade(t, db)
			runPromptWordlistStorage(t, db)
			runPromptAuditAgentUpgrade(t, db)
			require.NoError(t, db.Migrator().DropTable(&PromptAudit{}, &PromptWordlist{}, &Option{}))
			require.NoError(t, db.AutoMigrate(&PromptAudit{}, &PromptWordlist{}, &Option{}))
			require.NoError(t, db.AutoMigrate(&PromptAudit{}, &PromptWordlist{}, &Option{}))
			assert.True(t, db.Migrator().HasTable(&PromptAudit{}))
			// The username filter reaches into the users table for rows written
			// before the name was snapshotted.
			require.NoError(t, db.AutoMigrate(&User{}))

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
				// The probabilities are a text column, so the round trip has to
				// hold on every engine rather than only where it was written.
				Scores: map[string]float64{"pii": 0.91, "probe": 0.5},
			}))

			listed, total, err := ListPromptAudits(PromptAuditFilter{
				Status: string(PromptAuditStatusDone), Category: "pii", RequestID: "dialect-request",
			}, 1, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, listed, 1)
			storedScores := listed[0].ToResponse(false).Scores
			assert.InDelta(t, 0.91, storedScores["pii"], 1e-9)
			assert.InDelta(t, 0.5, storedScores["probe"], 1e-9)

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
			runPromptAuditListingFilters(t, db)
			runPromptAuditUnsafeClientValues(t, db)
			runPromptAuditQuestionGroupingAcrossModels(t, db)
			runPromptAuditTerminalPayloadRetention(t, db)
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
	runPromptAuditAgentUpgrade(t, db)
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
		"GroupKey", "SessionKey", "RequestKind", "ScanPayloadTruncated", "Scores",
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
	// A third startup has to be a no-op. A tag one engine reads differently, or a
	// column whose declared type does not match what was written, shows up here as
	// an ALTER TABLE issued on every boot rather than as a failed migration.
	recorder := &migrationSQLRecorder{}
	recordingDB := db.Session(&gorm.Session{Logger: recorder})
	require.NoError(t, recordingDB.AutoMigrate(&PromptAudit{}, &PromptWordlist{}))
	require.NoError(t, MigratePromptAuditDefaults())
	assert.Empty(t, recorder.schemaMutations())
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
		"group_key": "64", "session_key": "64", "request_kind": "32",
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

func runPromptAuditAgentUpgrade(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Migrator().DropTable(&PromptAudit{}))
	// The deployed 861bfbc15 PromptAudit fields and GORM tags match this model
	// with these five new fields removed (verified against that Git revision).
	current := reflect.TypeFor[PromptAudit]()
	var fields []reflect.StructField
	for i := range current.NumField() {
		field := current.Field(i)
		if !slices.Contains([]string{"GroupKey", "SessionKey", "RequestKind", "ScanPayloadTruncated", "Scores"}, field.Name) {
			fields = append(fields, field)
		}
	}
	legacy := reflect.New(reflect.StructOf(fields)).Interface()
	table := db.NamingStrategy.TableName("PromptAudit")
	require.NoError(t, db.Table(table).AutoMigrate(legacy))
	require.NoError(t, db.Table(table).Create(map[string]any{"request_id": "deployed-861bfbc15", "user_id": 42, "prompt_hash": strings.Repeat("c", 64), "status": "done", "full_prompt": []byte("retained 原文"), "scan_payload": []byte("retained inspection")}).Error)
	before, err := db.Migrator().GetIndexes(&PromptAudit{})
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&PromptAudit{}))
		require.NoError(t, MigratePromptAuditDefaults())
	}
	for _, index := range before {
		assert.True(t, db.Migrator().HasIndex(&PromptAudit{}, index.Name()), index.Name())
	}
	for _, name := range []string{"group_key", "session_key", "request_kind"} {
		assert.True(t, db.Migrator().HasIndex(&PromptAudit{}, db.NamingStrategy.IndexName(table, name)))
	}
	var row PromptAudit
	require.NoError(t, db.Where("request_id = ?", "deployed-861bfbc15").First(&row).Error)
	assert.Equal(t, "retained 原文", string(row.FullPrompt))
	assert.Equal(t, "retained inspection", string(row.ScanPayload))
	assert.Empty(t, row.GroupKey)
	assert.False(t, row.ScanPayloadTruncated)
	// A row written before the scores column existed reads as no scores at all,
	// which is what every layer treats as "this verdict carried no numbers".
	assert.Empty(t, row.Scores)
	assert.Nil(t, row.ToResponse(false).Scores)
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
		GenerationID:  strings.Repeat("g", 200),
	}
	require.NoError(t, CreatePromptAudit(audit))
	// BeforeCreate clamps in place, so the caller's copy matches the stored row.
	assert.Equal(t, 64, utf8.RuneCountInString(audit.Ip))
	assert.Equal(t, 64, utf8.RuneCountInString(audit.Username))
	assert.Equal(t, 16, utf8.RuneCountInString(audit.Method))
	assert.Equal(t, 128, utf8.RuneCountInString(audit.GenerationID))
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

	runPromptAuditUnsafeClientValues(t, db)
}

// runPromptAuditUnsafeClientValues stores the bytes a client can put in its
// headers and path. Go's HTTP server accepts any byte above 0x7F in a header and
// decodes %00 in a path; MySQL and PostgreSQL refuse invalid UTF-8 and
// PostgreSQL refuses NUL, so without repair the whole audit row — in async mode
// the only trace that the request was ever meant to be inspected — is lost.
func runPromptAuditUnsafeClientValues(t *testing.T, db *gorm.DB) {
	t.Helper()
	audit := &PromptAudit{
		RequestID: "unsafe-client-bytes", Status: PromptAuditStatusQueued, MaxAttempts: 1,
		UserAgent: "caf\xe9-client/1.0", Origin: "http://\xff\xfe", Referer: "https://example.com/\x00page",
		RequestPath: "/v1/models/x\x00:generateContent", ModelName: "model-\xc3",
	}
	require.NoError(t, CreatePromptAudit(audit))
	var stored PromptAudit
	require.NoError(t, db.Where("request_id = ?", "unsafe-client-bytes").First(&stored).Error)
	for _, value := range []string{stored.UserAgent, stored.Origin, stored.Referer, stored.RequestPath, stored.ModelName} {
		assert.True(t, utf8.ValidString(value), "%q", value)
		assert.NotContains(t, value, "\x00")
	}
	assert.Equal(t, "caf\uFFFD-client/1.0", stored.UserAgent)
	assert.Equal(t, "/v1/models/x:generateContent", stored.RequestPath)
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
