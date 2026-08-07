package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestParseQwen3GuardDecisionTable(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		enabled    []string
		decision   string
		categories []string
		unknown    bool
	}{
		{name: "safe always passes", content: "Safety: Safe\nCategories: Jailbreak", enabled: []string{"jailbreak"}, decision: PromptAuditDecisionPass, categories: []string{"jailbreak"}},
		{name: "controversial regular category flags", content: "Safety: Controversial\nCategories: Violent", enabled: []string{"violent"}, decision: PromptAuditDecisionFlag, categories: []string{"violent"}},
		{name: "controversial elevated category blocks", content: "Safety: Controversial\nCategories: PII", enabled: []string{"pii"}, decision: PromptAuditDecisionBlock, categories: []string{"pii"}},
		{name: "controversial disabled elevated category flags", content: "Safety: Controversial\nCategories: PII", enabled: []string{"violent"}, decision: PromptAuditDecisionFlag, categories: []string{"pii"}},
		{name: "unsafe enabled category blocks", content: "Safety: Unsafe\nCategories: Copyright Violation", enabled: []string{"copyright_violation"}, decision: PromptAuditDecisionBlock, categories: []string{"copyright_violation"}},
		{name: "unsafe only disabled known categories flags", content: "Safety: Unsafe\nCategories: Copyright Violation", enabled: []string{"violent"}, decision: PromptAuditDecisionFlag, categories: []string{"copyright_violation"}},
		{name: "unsafe no categories blocks", content: "Safety: Unsafe\nCategories: None", enabled: []string{"violent"}, decision: PromptAuditDecisionBlock},
		{name: "unsafe blank categories blocks", content: "Safety: Unsafe\nCategories:", enabled: []string{"violent"}, decision: PromptAuditDecisionBlock},
		{name: "safe blank categories passes", content: "Safety: Safe\nCategories:", enabled: []string{"violent"}, decision: PromptAuditDecisionPass},
		{name: "unsafe unknown category blocks", content: "Safety: Unsafe\nCategories: Future Risk Name", enabled: nil, decision: PromptAuditDecisionBlock, unknown: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := ParseQwen3Guard(test.content, test.enabled)
			require.NoError(t, err)
			assert.Equal(t, test.decision, result.Decision)
			if test.categories == nil {
				assert.Empty(t, result.Categories)
			} else {
				assert.Equal(t, test.categories, result.Categories)
			}
			if test.unknown {
				require.Len(t, result.UnknownCategories, 1)
				assert.True(t, strings.HasPrefix(result.UnknownCategories[0], "unknown:"))
				assert.NotContains(t, result.UnknownCategories[0], "future")
			}
		})
	}
}

func TestParseQwen3GuardRejectsNonStrictOutput(t *testing.T) {
	tests := []string{
		"Safety: Safe",
		"Safety: Maybe\nCategories: None",
		"Safety: Safe\nCategories: None\nExplanation: okay",
		"Safety: Safe\nSafety: Unsafe\nCategories: None",
		"Categories: None\nCategories: PII",
		"Categories: None\nSafety: Safe",
	}
	for _, content := range tests {
		t.Run(strings.ReplaceAll(content, "\n", "_"), func(t *testing.T) {
			_, err := ParseQwen3Guard(content, prompt_audit_setting.AllCategoryIDs)
			require.Error(t, err)
			assert.Equal(t, "invalid_response", promptAuditErrorCode(err))
		})
	}
}

func TestSplitPromptAuditRunesUsesUnicodeAndOverlap(t *testing.T) {
	chunks := splitPromptAuditRunes("甲乙😀丁戊己", 4, 1)
	require.Equal(t, []string{"甲乙😀丁", "丁戊己"}, chunks)
	for _, chunk := range chunks {
		assert.LessOrEqual(t, utf8.RuneCountInString(chunk), 4)
	}
}

func TestEvaluatePromptAuditScansAllChunksUnlessBlocked(t *testing.T) {
	t.Run("flagged chunks are all reviewed", func(t *testing.T) {
		var calls atomic.Int32
		guard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Controversial\nCategories: Violent"}}]}`))
		}))
		defer guard.Close()

		setting := promptAuditTestSetting(guard.URL, guard.URL)
		setting.Endpoints = setting.Endpoints[:1]
		setting.Endpoints[0].InputLimit = 4
		setting.ChunkOverlap = 1
		result, err := evaluatePromptAudit(context.Background(), setting, "abcdefghij", strings.Repeat("1", 64))
		require.NoError(t, err)
		assert.Equal(t, PromptAuditDecisionFlag, result.Decision)
		assert.Equal(t, 3, result.ChunkCount)
		assert.EqualValues(t, 3, calls.Load())
	})

	t.Run("block stops remaining chunks", func(t *testing.T) {
		var calls atomic.Int32
		guard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Unsafe\nCategories: Jailbreak"}}]}`))
		}))
		defer guard.Close()

		setting := promptAuditTestSetting(guard.URL, guard.URL)
		setting.Endpoints = setting.Endpoints[:1]
		setting.Endpoints[0].InputLimit = 4
		setting.ChunkOverlap = 1
		result, err := evaluatePromptAudit(context.Background(), setting, "abcdefghij", strings.Repeat("2", 64))
		require.NoError(t, err)
		assert.Equal(t, PromptAuditDecisionBlock, result.Decision)
		assert.EqualValues(t, 1, calls.Load())
	})
}

func TestPromptAuditStoredFullPromptUsesUnicodeCapWithoutTruncatingScanInput(t *testing.T) {
	value := strings.Repeat("甲", promptAuditFullPromptMaxRunes+2)
	stored, truncated := promptAuditStoredFullPrompt(value)
	assert.True(t, truncated)
	assert.Equal(t, promptAuditFullPromptMaxRunes, utf8.RuneCount(stored))
	assert.Equal(t, promptAuditFullPromptMaxRunes+2, utf8.RuneCountInString(value))
}

func TestPromptAuditStoredFullPromptPreservesNullCharacters(t *testing.T) {
	stored, truncated := promptAuditStoredFullPrompt("before\x00after")
	assert.False(t, truncated)
	assert.Equal(t, []byte("before\x00after"), stored)
}

func TestPromptAuditStoredFullPromptPreservesOuterWhitespace(t *testing.T) {
	value := "  leading and trailing  \n"
	stored, truncated := promptAuditStoredFullPrompt(value)
	assert.False(t, truncated)
	assert.Equal(t, value, string(stored))
}

func TestPromptAuditEndpointFailoverPolicy(t *testing.T) {
	validBody := `{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validBody))
	}))
	defer second.Close()

	t.Run("retryable 5xx switches node", func(t *testing.T) {
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer first.Close()
		secondCalls.Store(0)
		setting := promptAuditTestSetting(first.URL, second.URL)
		result, err := scanPromptAuditChunk(context.Background(), setting, setting.Endpoints, "hello")
		require.NoError(t, err)
		assert.Equal(t, "second", result.EndpointID)
		assert.EqualValues(t, 1, secondCalls.Load())
	})

	t.Run("terminal 401 does not switch node", func(t *testing.T) {
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer first.Close()
		secondCalls.Store(0)
		setting := promptAuditTestSetting(first.URL, second.URL)
		_, err := scanPromptAuditChunk(context.Background(), setting, setting.Endpoints, "hello")
		require.Error(t, err)
		assert.Equal(t, "endpoint_http_401", promptAuditErrorCode(err))
		assert.EqualValues(t, 0, secondCalls.Load())
	})

	t.Run("invalid output does not switch node", func(t *testing.T) {
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"not guard output"}}]}`))
		}))
		defer first.Close()
		secondCalls.Store(0)
		setting := promptAuditTestSetting(first.URL, second.URL)
		_, err := scanPromptAuditChunk(context.Background(), setting, setting.Endpoints, "hello")
		require.Error(t, err)
		assert.Equal(t, "invalid_response", promptAuditErrorCode(err))
		assert.EqualValues(t, 0, secondCalls.Load())
	})

	t.Run("redirect is rejected without switching node", func(t *testing.T) {
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", second.URL)
			w.WriteHeader(http.StatusFound)
		}))
		defer first.Close()
		secondCalls.Store(0)
		setting := promptAuditTestSetting(first.URL, second.URL)
		_, err := scanPromptAuditChunk(context.Background(), setting, setting.Endpoints, "hello")
		require.Error(t, err)
		assert.Equal(t, "redirect_not_allowed", promptAuditErrorCode(err))
		assert.EqualValues(t, 0, secondCalls.Load())
	})
}

func TestEvaluatePromptAuditFailsFastWhenBulkheadIsFull(t *testing.T) {
	setting := promptAuditTestSetting("http://127.0.0.1:1", "http://127.0.0.1:2")
	setting.ConfigVersion = fmt.Sprintf("bulkhead-%d", time.Now().UnixNano())
	setting.GlobalConcurrency = 1
	slots := promptAuditSlots(&promptAuditGlobalSlots, "global|"+setting.ConfigVersion, 1)
	slots <- struct{}{}
	defer func() { <-slots }()

	_, err := evaluatePromptAudit(context.Background(), setting, "hello", strings.Repeat("a", 64))
	require.Error(t, err)
	assert.Equal(t, "concurrency_saturated", promptAuditErrorCode(err))
}

func TestPromptAuditEndpointSaturationDoesNotFailOver(t *testing.T) {
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer second.Close()

	setting := promptAuditTestSetting("http://127.0.0.1:1", second.URL)
	setting.ConfigVersion = fmt.Sprintf("endpoint-bulkhead-%d", time.Now().UnixNano())
	setting.Endpoints[0].Concurrency = 1
	slots := promptAuditSlots(
		&promptAuditEndpointSlots,
		setting.ConfigVersion+"|first|1",
		1,
	)
	slots <- struct{}{}
	defer func() { <-slots }()

	_, err := scanPromptAuditChunk(context.Background(), setting, setting.Endpoints, "hello")
	require.Error(t, err)
	assert.Equal(t, "endpoint_concurrency_saturated", promptAuditErrorCode(err))
	assert.Zero(t, secondCalls.Load())
}

func TestEvaluatePromptAuditCacheUsesConfigCategoriesAndPromptHash(t *testing.T) {
	var calls atomic.Int32
	guard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer guard.Close()

	setting := promptAuditTestSetting(guard.URL, guard.URL)
	setting.Endpoints = setting.Endpoints[:1]
	setting.CacheTTLSeconds = 60
	promptHash := strings.Repeat("c", 64)
	first, err := evaluatePromptAudit(context.Background(), setting, "same prompt", promptHash)
	require.NoError(t, err)
	assert.False(t, first.CacheHit)

	second, err := evaluatePromptAudit(context.Background(), setting, "same prompt", promptHash)
	require.NoError(t, err)
	assert.True(t, second.CacheHit)
	assert.EqualValues(t, 1, calls.Load())

	setting.EnabledCategories = []string{"violent"}
	third, err := evaluatePromptAudit(context.Background(), setting, "same prompt", promptHash)
	require.NoError(t, err)
	assert.False(t, third.CacheHit)
	assert.EqualValues(t, 2, calls.Load())
}

func TestEvaluatePromptAuditReportsRequestTotalTimeout(t *testing.T) {
	release := make(chan struct{})
	guard := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer guard.Close()

	setting := promptAuditTestSetting(guard.URL, guard.URL)
	setting.Endpoints = setting.Endpoints[:1]
	setting.Endpoints[0].TimeoutMS = 1000
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := evaluatePromptAudit(ctx, setting, "hello", strings.Repeat("d", 64))
	close(release)
	require.Error(t, err)
	assert.Equal(t, "total_timeout", promptAuditErrorCode(err))
}

func TestProcessNextPromptAuditCompletesWithoutRequeue(t *testing.T) {
	previousDB := model.DB
	previousMainType := common.MainDatabaseType()
	previousSetting := prompt_audit_setting.GetSetting()
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousMainType, common.LogDatabaseType())
		previousSetting.PublishConfig()
	})

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PromptAudit{}))
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.LogDatabaseType())

	var invalidOutput atomic.Bool
	guard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if invalidOutput.Load() {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"invalid guard output"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer guard.Close()

	configured := promptAuditTestSetting(guard.URL, guard.URL)
	configured.Mode = prompt_audit_setting.ModeAsyncAudit
	configured.Endpoints = configured.Endpoints[:1]
	configured.PublishConfig()
	active := prompt_audit_setting.GetSetting()
	policy, err := common.Marshal(active.EnabledCategories)
	require.NoError(t, err)
	audit := &model.PromptAudit{
		Status:           model.PromptAuditStatusQueued,
		PromptHash:       strings.Repeat("e", 64),
		FullPrompt:       []byte("queued prompt"),
		ScanPayload:      []byte("queued prompt"),
		PolicyCategories: string(policy),
		MaxAttempts:      active.MaxAttempts,
		ExecutionMode:    prompt_audit_setting.ModeAsyncAudit,
		ConfigVersion:    active.ConfigVersion,
	}
	require.NoError(t, model.CreatePromptAudit(audit))

	assert.True(t, processNextPromptAudit(context.Background(), "worker-test"))
	var stored model.PromptAudit
	require.NoError(t, db.First(&stored, audit.ID).Error)
	assert.Equal(t, model.PromptAuditStatusDone, stored.Status)
	assert.Equal(t, PromptAuditDecisionPass, stored.WouldAction)
	assert.Empty(t, stored.ScanPayload)
	assert.Equal(t, 1, stored.Attempts)
	var count int64
	require.NoError(t, db.Model(&model.PromptAudit{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)

	invalidOutput.Store(true)
	failedAudit := &model.PromptAudit{
		Status:           model.PromptAuditStatusQueued,
		PromptHash:       strings.Repeat("f", 64),
		FullPrompt:       []byte("invalid output prompt"),
		ScanPayload:      []byte("invalid output prompt"),
		PolicyCategories: string(policy),
		MaxAttempts:      active.MaxAttempts,
		ExecutionMode:    prompt_audit_setting.ModeAsyncAudit,
		ConfigVersion:    active.ConfigVersion,
	}
	require.NoError(t, model.CreatePromptAudit(failedAudit))
	assert.True(t, processNextPromptAudit(context.Background(), "worker-test"))
	stored = model.PromptAudit{}
	require.NoError(t, db.First(&stored, failedAudit.ID).Error)
	assert.Equal(t, model.PromptAuditStatusFailed, stored.Status)
	assert.Equal(t, "invalid_response", stored.ErrorCode)
	assert.Equal(t, 1, stored.Attempts)
	assert.Zero(t, stored.NextAttemptAt)
	assert.Empty(t, stored.ScanPayload)
	require.NoError(t, db.Model(&model.PromptAudit{}).Count(&count).Error)
	assert.EqualValues(t, 2, count)
}

func TestCheckPromptAuditAsyncNeverBlocksMainRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousMainType := common.MainDatabaseType()
	previousSetting := prompt_audit_setting.GetSetting()
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousMainType, common.LogDatabaseType())
		previousSetting.PublishConfig()
	})

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PromptAudit{}))
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.LogDatabaseType())

	var guardCalls atomic.Int32
	guard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		guardCalls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Unsafe\nCategories: Jailbreak"}}]}`))
	}))
	defer guard.Close()
	configured := promptAuditTestSetting(guard.URL, guard.URL)
	configured.Mode = prompt_audit_setting.ModeAsyncAudit
	configured.Endpoints = configured.Endpoints[:1]
	configured.MaxAttempts = prompt_audit_setting.DefaultMaxAttempts
	configured.PublishConfig()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	result, apiErr := CheckPromptAudit(c, PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{
			Role: "user", Text: "dangerous prompt", User: true,
		}}},
		Protocol: "openai_responses", Model: "test-model", Stage: "http",
	})
	require.Nil(t, apiErr)
	assert.Equal(t, "queued", result.Outcome)
	assert.False(t, result.Blocked)
	assert.Positive(t, result.AuditID)
	assert.Zero(t, guardCalls.Load(), "async mode must not call the guard on the request path")

	var queued model.PromptAudit
	require.NoError(t, db.First(&queued, result.AuditID).Error)
	assert.Equal(t, model.PromptAuditStatusQueued, queued.Status)
	assert.Equal(t, []byte("dangerous prompt"), queued.ScanPayload)

	require.NoError(t, db.Migrator().DropTable(&model.PromptAudit{}))
	result, apiErr = CheckPromptAudit(c, PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{
			Role: "user", Text: "queue unavailable", User: true,
		}}},
		Protocol: "openai_responses", Model: "test-model", Stage: "http",
	})
	require.Nil(t, apiErr)
	assert.Equal(t, "enqueue_failed", result.Outcome)
	assert.False(t, result.Blocked)
	assert.Zero(t, guardCalls.Load())
}

func TestPromptAuditRetryScheduleAllowsThreeRetries(t *testing.T) {
	assert.Equal(t, 4, prompt_audit_setting.DefaultMaxAttempts)
	assert.Equal(t, 5*time.Second, promptAuditRetryDelay(1))
	assert.Equal(t, 30*time.Second, promptAuditRetryDelay(2))
	assert.Equal(t, 2*time.Minute, promptAuditRetryDelay(3))
}

func promptAuditTestSetting(firstURL, secondURL string) prompt_audit_setting.PromptAuditSetting {
	version := fmt.Sprintf("test-%d", time.Now().UnixNano())
	return prompt_audit_setting.PromptAuditSetting{
		Mode:              prompt_audit_setting.ModeBlocking,
		EnabledCategories: append([]string(nil), prompt_audit_setting.AllCategoryIDs...),
		AllGroups:         true,
		Endpoints: []prompt_audit_setting.Endpoint{
			{ID: "first", BaseURL: firstURL, Model: "guard", TimeoutMS: 500, InputLimit: 4000, Concurrency: 2, Enabled: true},
			{ID: "second", BaseURL: secondURL, Model: "guard", TimeoutMS: 500, InputLimit: 4000, Concurrency: 2, Enabled: true},
		},
		TotalTimeoutMS: 1000, ChunkOverlap: 64, CacheTTLSeconds: 0,
		WorkerCount: 1, MaxAttempts: 3, RetentionDays: 30,
		GlobalConcurrency: 2, EndpointConcurrency: 2, ConfigVersion: version,
	}
}
