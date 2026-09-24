package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type wordlistTestTransport func(*http.Request) (*http.Response, error)

func (transport wordlistTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestPromptWordlistImportNormalizesSupportedFilesWithoutExecutingContent(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"words.txt", "\ufeff# comment\r\nAlpha\r\nalpha\n// comment\n测试词\n", "alpha\n测试词"},
		{"words.dic", " beta \nALPHA\n", "alpha\nbeta"},
		{"words.json", "{\"lastUpdateDate\":\"2026-01-01\",\"words\":[\"Beta\",\"alpha\",\"alpha\"]}", "alpha\nbeta"},
		{"words.json", "[\"Beta\",\"alpha\"]", "alpha\nbeta"},
	} {
		t.Run(tc.name+"/"+tc.want, func(t *testing.T) {
			client := &http.Client{Transport: wordlistTestTransport(func(request *http.Request) (*http.Response, error) {
				assert.Empty(t, request.Header.Get("Authorization"))
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.content))}, nil
			})}
			result, err := downloadPromptWordlist(context.Background(), client, &model.PromptWordlist{SourceURL: "https://wordlists.example/" + tc.name})
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(result.Content))
			assert.Equal(t, 2, result.WordCount)
		})
	}
	for _, tc := range []struct {
		name    string
		content []byte
	}{
		{"words.json", []byte("{\"metadata\":[\"not a wordlist\"]}")},
		{"words.json", []byte("[{\"word\":\"unsupported object\"}]")},
		{"words.json", []byte("[\"embedded\\nnewline\"]")},
		{"words.txt", []byte{0xff, 0xfe}},
		{"words.txt", []byte("<!DOCTYPE html><html>error page</html>")},
		{"plugin.js", []byte("require('fs').unlinkSync('/')")},
	} {
		assert.Error(t, parsePromptWordlist(tc.name, tc.content, map[string]struct{}{}))
	}
}

func TestPromptWordlistGithubImportPinsCommitAndSkipsDocumentationAndExamples(t *testing.T) {
	sha := strings.Repeat("a", 40)
	var downloaded []string
	client := &http.Client{Transport: wordlistTestTransport(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.Host + request.URL.Path {
		case "api.github.com/repos/acme/wordlists":
			body = "{\"default_branch\":\"main\"}"
		case "api.github.com/repos/acme/wordlists/commits/main":
			body = "{\"sha\":\"" + sha + "\"}"
		case "api.github.com/repos/acme/wordlists/git/trees/" + sha:
			body = "{\"tree\":[{\"path\":\"README.txt\",\"type\":\"blob\"},{\"path\":\"examples/words.txt\",\"type\":\"blob\"},{\"path\":\"words/list.txt\",\"type\":\"blob\",\"size\":12}]}"
		case "raw.githubusercontent.com/acme/wordlists/" + sha + "/words/list.txt":
			downloaded = append(downloaded, request.URL.Path)
			body = "alpha\nbeta"
		default:
			t.Fatalf("unexpected request: %s", request.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	row := &model.PromptWordlist{SourceURL: "https://github.com/acme/wordlists"}
	result, err := downloadPromptWordlist(context.Background(), client, row)
	require.NoError(t, err)
	assert.Equal(t, sha, result.Revision)
	assert.Equal(t, []string{"words/list.txt"}, result.Files)
	assert.Len(t, downloaded, 1)
	row.SourceRevision, row.ContentHash = result.Revision, result.Hash
	unchanged, err := downloadPromptWordlist(context.Background(), client, row)
	require.NoError(t, err)
	assert.True(t, unchanged.Unchanged)
	assert.Len(t, downloaded, 1)
}

func TestPromptWordlistRejectsPrivateTargetsAndOversizedDownloads(t *testing.T) {
	for _, source := range []string{"http://example.com/list.txt", "https://127.0.0.1/list.txt", "https://[::1]/list.txt", "https://169.254.169.254/list.txt", "https://10.0.0.1/list.txt", "https://example.com:8443/list.txt", "https://user:password@example.com/list.txt"} {
		_, err := NormalizePromptWordlistURL(source)
		assert.Error(t, err, source)
	}
	client := &http.Client{Transport: wordlistTestTransport(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: io.NopCloser(strings.NewReader(strings.Repeat("a", wordlistMaxFileBytes+1)))}, nil
	})}
	_, err := downloadPromptWordlist(context.Background(), client, &model.PromptWordlist{SourceURL: "https://wordlists.example/list.txt"})
	require.EqualError(t, err, "file_too_large")
}

func withPromptWordlistTestDB(t *testing.T) {
	t.Helper()
	previousDB, previousSnapshot, previousOptions := model.DB, promptWordlists.Load(), common.OptionMap
	previousSettings := prompt_audit_setting.GetSetting()
	previousWords := setting.SensitiveWordsToString()
	previousEnabled, previousPrompt := setting.CheckSensitiveEnabled, setting.CheckSensitiveOnPromptEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.PromptWordlist{}, &model.PromptAudit{}, &model.Option{}))
	model.DB = db
	common.OptionMap = make(map[string]string)
	setting.SensitiveWordsFromString("")
	setting.SetCheckSensitiveEnabled(true)
	setting.SetCheckSensitiveOnPromptEnabled(true)
	t.Cleanup(func() {
		model.DB = previousDB
		common.OptionMap = previousOptions
		promptWordlists.Store(previousSnapshot)
		previousSettings.PublishConfig()
		setting.SensitiveWordsFromString(previousWords)
		setting.SetCheckSensitiveEnabled(previousEnabled)
		setting.SetCheckSensitiveOnPromptEnabled(previousPrompt)
		require.NoError(t, sqlDB.Close())
	})
}

func createPromptWordlistFixture(t *testing.T, name, words string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(words))
	row := &model.PromptWordlist{Name: name, SourceURL: "https://example.com/" + name + ".txt", SourceHash: name, Enabled: true, Content: []byte(words), ContentHash: hex.EncodeToString(digest[:]), WordCount: len(strings.Split(words, "\n"))}
	require.NoError(t, model.CreatePromptWordlist(row))
	return strconv.FormatInt(row.ID, 10)
}

func TestPromptWordlistPoliciesApplyPerSourceAndDisabledLibrariesNeverMatch(t *testing.T) {
	withPromptWordlistTestDB(t)
	one := createPromptWordlistFixture(t, "one", "alpha\nshared")
	two := createPromptWordlistFixture(t, "two", "beta\nshared")
	three := createPromptWordlistFixture(t, "three", "gamma")
	require.NoError(t, RefreshPromptWordlists())
	require.NoError(t, compilePromptWordlists())
	configured := prompt_audit_setting.GetSetting()
	configured.Mode = prompt_audit_setting.ModeOff
	configured.ScopePolicies = map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy{
		dto.PromptScopeSystem: {LibraryIDs: []string{one, two}},
		dto.PromptScopeUser:   {LibraryIDs: []string{one, two, three}},
		dto.PromptScopeTask:   {LibraryIDs: []string{three}},
	}
	configured.PublishConfig()
	for _, tc := range []struct {
		scope      dto.PromptAuditScope
		text, want string
	}{
		{dto.PromptScopeSystem, "gamma", ""},
		{dto.PromptScopeUser, "gamma", three},
		{dto.PromptScopeTask, "gamma", three},
		{dto.PromptScopeSystem, "alpha", one},
		{dto.PromptScopeToolResult, "gamma", ""},
		{dto.PromptScopeAssistant, "alpha", ""},
	} {
		match, _, err := TestPromptWordlists(tc.scope, tc.text)
		require.NoError(t, err)
		if tc.want == "" {
			assert.Nil(t, match)
			continue
		}
		require.NotNil(t, match)
		assert.Equal(t, tc.want, match.ID)
		assert.Equal(t, tc.scope, match.Scope)
	}
	oneID, err := strconv.ParseInt(one, 10, 64)
	require.NoError(t, err)
	disabled := false
	require.NoError(t, model.UpdatePromptWordlist(oneID, model.PromptWordlistUpdate{Enabled: &disabled}))
	require.NoError(t, RefreshPromptWordlists())
	match, _, err := TestPromptWordlists(dto.PromptScopeSystem, "alpha")
	require.NoError(t, err)
	assert.Nil(t, match)
	match, _, err = TestPromptWordlists(dto.PromptScopeSystem, "shared")
	require.NoError(t, err)
	require.NotNil(t, match)
	assert.Equal(t, two, match.ID)

	// A failed content read for a different library cannot retain disabled or
	// deleted dictionaries in the active metadata snapshot.
	createPromptWordlistFixture(t, "broken", "unavailable")
	configured.PublishConfig()
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("wordlist_content_unavailable", func(db *gorm.DB) {
		if db.Statement.Table == "prompt_wordlists" && len(db.Statement.Omits) == 0 {
			db.AddError(errors.New("content read failed"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Query().Remove("wordlist_content_unavailable")) })
	require.NoError(t, RefreshPromptWordlists())
	require.Error(t, compilePromptWordlists())
	match, _, err = TestPromptWordlists(dto.PromptScopeSystem, "alpha")
	require.NoError(t, err)
	assert.Nil(t, match)
}

func TestPromptWordlistPoliciesFailClosedForMissingOrStaleRuntime(t *testing.T) {
	withPromptWordlistTestDB(t)
	id := createPromptWordlistFixture(t, "required", "blocked-marker")
	require.NoError(t, RefreshPromptWordlists())
	require.NoError(t, compilePromptWordlists())

	configured := prompt_audit_setting.GetSetting()
	configured.ScopePolicies = map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy{
		dto.PromptScopeUser: {LibraryIDs: []string{id}},
	}
	configured.PublishConfig()

	promptWordlists.Store(&promptWordlistSnapshot{Libraries: map[string]promptWordlistRuntime{}})
	match, _, err := TestPromptWordlists(dto.PromptScopeUser, "safe text")
	assert.Nil(t, match)
	require.ErrorContains(t, err, "wordlist is unavailable")

	require.NoError(t, RefreshPromptWordlists())
	require.NoError(t, compilePromptWordlists())
	snapshot := promptWordlists.Load()
	entry := snapshot.Libraries[id]
	entry.TargetVersion = "new-content-version"
	promptWordlists.Store(&promptWordlistSnapshot{Libraries: map[string]promptWordlistRuntime{id: entry}})
	match, _, err = TestPromptWordlists(dto.PromptScopeUser, "blocked-marker")
	assert.Nil(t, match)
	require.ErrorContains(t, err, "wordlist is unavailable")
}

func TestPromptWordlistManualDisablePreservesWordsAndBindings(t *testing.T) {
	withPromptWordlistTestDB(t)
	setting.SensitiveWordsFromString("manual-marker")
	configured := prompt_audit_setting.GetSetting()
	configured.ScopePolicies = nil
	configured.Mode = prompt_audit_setting.ModeOff
	configured.ManualWordlistEnabled = nil
	configured.PublishConfig()
	match, _, err := TestPromptWordlists(dto.PromptScopeUser, "manual-marker")
	require.NoError(t, err)
	require.NotNil(t, match)
	require.NoError(t, model.UpdateOption("prompt_audit.manual_wordlist_enabled", "false"))
	match, _, err = TestPromptWordlists(dto.PromptScopeUser, "manual-marker")
	require.NoError(t, err)
	assert.Nil(t, match)
	assert.Equal(t, "manual-marker", setting.SensitiveWordsToString())
	assert.Equal(t, []string{"manual"}, prompt_audit_setting.GetSetting().PolicyFor(dto.PromptScopeUser).LibraryIDs)
}

func TestPromptWordlistAuditRetainsFullTextForAuthorizedReview(t *testing.T) {
	withPromptWordlistTestDB(t)
	setting.SensitiveWordsFromString("blocked-marker")
	configured := prompt_audit_setting.GetSetting()
	configured.Mode = prompt_audit_setting.ModeOff
	configured.ScopePolicies = map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy{
		dto.PromptScopeUser: {LibraryIDs: []string{prompt_audit_setting.ManualWordlistID}},
	}
	configured.PublishConfig()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	fullText := "  blocked-marker from person@example.com  \n"
	storedText := strings.TrimSpace(fullText)
	result, apiErr := InspectPrompt(c, PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{
			Scope: dto.PromptScopeUser, Role: "user", User: true, Text: fullText,
		}}},
		Protocol: "openai_chat", Model: "guarded-model",
	})

	require.NotNil(t, apiErr)
	assert.True(t, result.Blocked)
	assert.Positive(t, result.AuditID)
	audit, err := model.GetPromptAudit(result.AuditID)
	require.NoError(t, err)
	assert.Equal(t, "wordlist", audit.InspectionType)
	assert.Equal(t, []byte(storedText), audit.FullPrompt)
	assert.False(t, audit.FullPromptTruncated)
	assert.NotContains(t, audit.RedactedPreview, "person@example.com")
	assert.Nil(t, audit.ToResponse(false).FullPrompt)
	review := audit.ToResponse(true)
	require.NotNil(t, review.FullPrompt)
	assert.Equal(t, storedText, *review.FullPrompt)
}

func TestPromptWordlistActionsSeparateDirectBlockFromModelConfirmation(t *testing.T) {
	withPromptWordlistTestDB(t)
	setting.SensitiveWordsFromString("review-marker")
	var guardCalls atomic.Int32
	guard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		guardCalls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`)
	}))
	defer guard.Close()

	configured := promptAuditTestSetting(guard.URL, "")
	configured.ManualWordlistAction = prompt_audit_setting.WordlistActionReview
	configured.ScopePolicies = map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy{
		dto.PromptScopeUser: {LibraryIDs: []string{prompt_audit_setting.ManualWordlistID}, ModelAudit: true},
	}
	configured.PublishConfig()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request := PromptAuditRequest{Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{
		Scope: dto.PromptScopeUser, Role: "user", User: true, Text: "review-marker",
	}}}}

	result, apiErr := InspectPrompt(c, request)
	require.Nil(t, apiErr)
	assert.False(t, result.Blocked)
	assert.Equal(t, "wordlist_model", result.InspectionType)
	require.NotNil(t, result.Wordlist)
	assert.Equal(t, prompt_audit_setting.WordlistActionReview, result.Wordlist.Action)
	assert.EqualValues(t, 1, guardCalls.Load())

	configured.ManualWordlistAction = prompt_audit_setting.WordlistActionBlock
	configured.PublishConfig()
	result, apiErr = InspectPrompt(c, request)
	require.NotNil(t, apiErr)
	assert.True(t, result.Blocked)
	assert.Equal(t, "wordlist", result.InspectionType)
	assert.Equal(t, PromptAuditActionBlock, result.ActualAction)
	assert.EqualValues(t, 1, guardCalls.Load(), "direct blocks must not call or be overridden by the model")
}

func TestPromptInspectionUsesBlockingSnapshot(t *testing.T) {
	// Model scope depends on its own mode and switch, independently of wordlists.
	withPromptWordlistTestDB(t)
	previousEnabled, previousPrompt := setting.CheckSensitiveEnabled, setting.CheckSensitiveOnPromptEnabled
	t.Cleanup(func() {
		setting.SetCheckSensitiveEnabled(previousEnabled)
		setting.SetCheckSensitiveOnPromptEnabled(previousPrompt)
	})
	setting.SetCheckSensitiveEnabled(false)
	setting.SetCheckSensitiveOnPromptEnabled(false)

	tests := []struct {
		name         string
		mode         string
		latest       bool
		direct       string
		wantModel    bool
		wantWordlist bool
	}{
		{name: "blocking with the switch on narrows both", mode: prompt_audit_setting.ModeBlocking, latest: true, direct: PromptAuditDirectionInput, wantModel: true, wantWordlist: true},
		{name: "blocking with the switch off widens both", mode: prompt_audit_setting.ModeBlocking, latest: false, direct: PromptAuditDirectionInput},
		// The wordlist gate refuses synchronously in every mode, so it follows the
		// switch even where the model audit reads the whole request.
		{name: "async audit narrows only the wordlist gate", mode: prompt_audit_setting.ModeAsyncAudit, latest: true, direct: PromptAuditDirectionInput, wantWordlist: true},
		{name: "model audit off still narrows the wordlist gate", mode: prompt_audit_setting.ModeOff, latest: true, direct: PromptAuditDirectionInput, wantWordlist: true},
		{name: "the switch off widens the wordlist gate in every mode", mode: prompt_audit_setting.ModeOff, latest: false, direct: PromptAuditDirectionInput},
		{name: "output direction never narrows", mode: prompt_audit_setting.ModeBlocking, latest: true, direct: PromptAuditDirectionOutput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configured := prompt_audit_setting.PromptAuditSetting{Mode: test.mode, BlockingLatestTurnOnly: test.latest}
			assert.Equal(t, test.wantModel, promptInspectionUsesBlockingSnapshot(test.direct, configured))
			assert.Equal(t, test.wantWordlist, promptWordlistUsesBlockingSnapshot(test.direct, configured))
		})
	}

	t.Run("wordlist gate does not change model scope", func(t *testing.T) {
		setting.SetCheckSensitiveEnabled(true)
		setting.SetCheckSensitiveOnPromptEnabled(true)
		configured := prompt_audit_setting.PromptAuditSetting{Mode: prompt_audit_setting.ModeBlocking, BlockingLatestTurnOnly: false}
		assert.False(t, promptInspectionUsesBlockingSnapshot(PromptAuditDirectionInput, configured))
	})
}

func TestInspectPromptBlockingScopeToggleChangesWhatTheModelSees(t *testing.T) {
	// End-to-end proof that the switch changes the inspected text rather than just
	// a field value: the guard blocks only when it receives the historical turn.
	// Wordlists stay enabled to protect the independence of the two detectors.
	withPromptWordlistTestDB(t)
	previousEnabled, previousPrompt := setting.CheckSensitiveEnabled, setting.CheckSensitiveOnPromptEnabled
	t.Cleanup(func() {
		setting.SetCheckSensitiveEnabled(previousEnabled)
		setting.SetCheckSensitiveOnPromptEnabled(previousPrompt)
	})
	setting.SetCheckSensitiveEnabled(true)
	setting.SetCheckSensitiveOnPromptEnabled(true)

	var sawHistory atomic.Bool
	guard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		if strings.Contains(string(body), "old_forbidden_word") {
			sawHistory.Store(true)
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Safety: Unsafe\nCategories: None"}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`)
	}))
	defer guard.Close()

	configured := promptAuditTestSetting(guard.URL, "")
	configured.ScopePolicies = map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy{
		dto.PromptScopeUser: {ModelAudit: true},
	}
	configured.CacheTTLSeconds = 0
	configured.PublishConfig()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	multiTurnCleanLatest := PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
			{Role: "user", User: true, Text: "tell me about old_forbidden_word"},
			{Role: "assistant", User: false, Text: "I cannot discuss that."},
			{Role: "user", User: true, Text: "okay, tell me a poem about the sea then"},
		}},
		Protocol: "openai",
		Model:    "gpt-4o",
	}

	result, apiErr := InspectPrompt(c, multiTurnCleanLatest)
	require.Nil(t, apiErr)
	assert.False(t, result.Blocked)
	assert.False(t, sawHistory.Load(), "the historical turn must not reach the model")
	preview, err := TestPromptAuditPolicy(context.Background(), PromptAuditDirectionInput, multiTurnCleanLatest.Snapshot, "")
	require.NoError(t, err)
	assert.Equal(t, result.Decision, preview.Decision)
	assert.False(t, sawHistory.Load(), "preview must use the same model window as live requests")

	widened := configured
	widened.BlockingLatestTurnOnly = false
	widened.ConfigVersion = "wide-blocking-v1"
	widened.PublishConfig()

	wideResult, wideErr := InspectPrompt(c, multiTurnCleanLatest)
	require.NotNil(t, wideErr)
	assert.True(t, wideResult.Blocked)
	assert.True(t, sawHistory.Load(), "the historical turn must reach the model once widened")
	preview, err = TestPromptAuditPolicy(context.Background(), PromptAuditDirectionInput, multiTurnCleanLatest.Snapshot, "")
	require.NoError(t, err)
	assert.Equal(t, wideResult.Decision, preview.Decision)
}

func TestPromptAuditModelAndAsyncPayloadContainOnlySelectedSources(t *testing.T) {
	withPromptWordlistTestDB(t)
	var sent []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Messages []struct{ Content string } }
		require.NoError(t, common.DecodeJson(r.Body, &request))
		for _, message := range request.Messages {
			sent = append(sent, message.Content)
		}
		_, _ = io.WriteString(w, "{\"choices\":[{\"message\":{\"content\":\"Safety: Safe\\nCategories: None\"}}]}")
	}))
	defer endpoint.Close()
	configured := promptAuditTestSetting(endpoint.URL, "")
	configured.ScopePolicies = map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy{
		dto.PromptScopeSystem: {LibraryIDs: []string{}, ModelAudit: true},
	}
	configured.PublishConfig()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request := PromptAuditRequest{Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
		{Role: "system", Text: "selected system instruction"},
		{Role: "user", User: true, Text: "unselected user content"},
		{Role: "tool", Text: "unselected tool content"},
	}}}
	result, apiErr := CheckPromptAudit(c, request)
	require.Nil(t, apiErr)
	assert.True(t, result.Reviewed)
	require.Len(t, sent, 1)
	var sentPayload promptAuditPayload
	require.NoError(t, common.UnmarshalJsonStr(sent[0], &sentPayload))
	require.Len(t, sentPayload.Segments, 1)
	assert.Equal(t, "selected system instruction", sentPayload.Segments[0].Text)
	assert.Equal(t, dto.PromptScopeSystem, sentPayload.Segments[0].SourceScope())
	configured.Mode = prompt_audit_setting.ModeAsyncAudit
	configured.PublishConfig()
	result, apiErr = CheckPromptAudit(c, request)
	require.Nil(t, apiErr)
	require.NotZero(t, result.AuditID)
	audit, err := model.GetPromptAudit(result.AuditID)
	require.NoError(t, err)
	var queuedPayload promptAuditPayload
	require.NoError(t, common.Unmarshal(audit.ScanPayload, &queuedPayload))
	require.Len(t, queuedPayload.Segments, 1)
	assert.Equal(t, "selected system instruction", queuedPayload.Segments[0].Text)
	assert.NotContains(t, string(audit.FullPrompt), "unselected")
	assert.Len(t, sent, 1)
}

func TestPromptAuditRecordsUnavailablePreviousResponseContext(t *testing.T) {
	withPromptWordlistTestDB(t)
	configured := promptAuditTestSetting("http://127.0.0.1:1", "")
	configured.PublishConfig()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	result, apiErr := CheckPromptAudit(c, PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{}, Protocol: "openai_responses", Model: "test-model",
		Stage: "pre_distribution", CoverageIncomplete: true,
	})
	require.Nil(t, apiErr)
	assert.Equal(t, "coverage_incomplete", result.Outcome)
	assert.Equal(t, PromptAuditDecisionFlag, result.Decision)
	assert.False(t, result.CoverageComplete)
	audit, err := model.GetPromptAudit(result.AuditID)
	require.NoError(t, err)
	assert.Equal(t, PromptAuditDirectionInput, audit.Direction)
	assert.False(t, audit.CoverageComplete)
}

func TestIsProbeRequest(t *testing.T) {
	tests := []struct {
		name     string
		snapshot dto.PromptAuditSnapshot
		want     bool
	}{
		{
			name: "single digit 1",
			snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: "user", User: true, Text: "1"},
			}},
			want: true,
		},
		{
			name: "single digit 1 with punctuation and spaces",
			snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: "user", User: true, Text: "  1.  "},
			}},
			want: true,
		},
		{
			name: "english probes",
			snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: "user", User: true, Text: "hi!"},
			}},
			want: true,
		},
		{
			name: "chinese probes",
			snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: "user", User: true, Text: "你好"},
			}},
			want: true,
		},
		{
			name: "system prompt plus probe user prompt",
			snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: "system", User: false, Text: "You are a helpful assistant."},
				{Role: "user", User: true, Text: "ping"},
			}},
			want: false,
		},
		{
			name: "multi turn conversation with assistant is not probe",
			snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: "user", User: true, Text: "hi"},
				{Role: "assistant", User: false, Text: "Hello! How can I help you?"},
				{Role: "user", User: true, Text: "1"},
			}},
			want: false,
		},
		{
			name: "long prompt is not probe",
			snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: "user", User: true, Text: "hi, please write an essay explaining quantum computing in simple terms"},
			}},
			want: false,
		},
		{
			name: "ordinary user query is not probe",
			snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: "user", User: true, Text: "what is the capital of France?"},
			}},
			want: false,
		},
	}
	for _, scope := range []dto.PromptAuditScope{dto.PromptScopeSystem, dto.PromptScopeDeveloper, dto.PromptScopeAssistant, dto.PromptScopeToolCall, dto.PromptScopeToolResult, dto.PromptScopeTask} {
		tests = append(tests, struct {
			name     string
			snapshot dto.PromptAuditSnapshot
			want     bool
		}{name: "extra " + string(scope), snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
			{Role: string(scope), Scope: scope, Text: "extra payload"},
			{Role: "user", User: true, Text: "hi"},
		}}})
		tests = append(tests, struct {
			name     string
			snapshot dto.PromptAuditSnapshot
			want     bool
		}{name: "non user scope " + string(scope), snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
			{Role: "user", Scope: scope, User: true, Text: "hi"},
		}}})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsProbeRequest(tt.snapshot))
		})
	}
}

func TestInspectPromptProbeFastPassRespectsExplicitWordlistBlocks(t *testing.T) {
	withPromptWordlistTestDB(t)

	original := setting.SensitiveWordsToString()
	t.Cleanup(func() { setting.SensitiveWordsFromString(original) })
	setting.SensitiveWordsFromString("blocked_word")

	configured := prompt_audit_setting.PromptAuditSetting{
		ConfigVersion:        "probe-test-v1",
		Mode:                 prompt_audit_setting.ModeBlocking,
		AllGroups:            true,
		ManualWordlistAction: prompt_audit_setting.WordlistActionBlock,
		ScopePolicies: map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy{
			dto.PromptScopeUser: {LibraryIDs: []string{prompt_audit_setting.ManualWordlistID}, ModelAudit: true},
		},
	}
	configured.PublishConfig()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	// A standalone probe can skip the model after explicit wordlist rules pass.
	request := PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
			{Role: "user", User: true, Text: "1"},
		}},
		Protocol: "openai",
		Model:    "gpt-4o-mini",
	}

	result, apiErr := InspectPrompt(c, request)
	require.Nil(t, apiErr)
	assert.False(t, result.Blocked)
	assert.Equal(t, "probe_fast_pass", result.InspectionType)
	assert.Equal(t, PromptAuditDecisionPass, result.Decision)
	require.NotZero(t, result.AuditID)

	audit, err := model.GetPromptAudit(result.AuditID)
	require.NoError(t, err)
	assert.Equal(t, "probe_fast_pass", audit.InspectionType)
	assert.Equal(t, PromptAuditDecisionPass, audit.Decision)
	assert.Equal(t, "Safe", audit.Safety)

	setting.SensitiveWordsFromString("1\nblocked_word")
	blockedProbe, blockedErr := InspectPrompt(c, request)
	require.NotNil(t, blockedErr)
	assert.True(t, blockedProbe.Blocked)
	assert.Equal(t, "wordlist", blockedProbe.InspectionType)

	// Non-probe request containing "blocked_word" must still be blocked
	badRequest := PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
			{Role: "user", User: true, Text: "this sentence contains blocked_word and should be caught"},
		}},
		Protocol: "openai",
		Model:    "gpt-4o-mini",
	}
	badResult, badErr := InspectPrompt(c, badRequest)
	require.NotNil(t, badErr)
	assert.True(t, badResult.Blocked)
	assert.Equal(t, "wordlist", badResult.InspectionType)
}

func TestInspectPromptKeepsClientContextAndTagsInModelInput(t *testing.T) {
	withPromptWordlistTestDB(t)
	var sent promptAuditPayload
	guard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Messages []struct{ Content string } }
		require.NoError(t, common.DecodeJson(r.Body, &request))
		require.Len(t, request.Messages, 1)
		require.NoError(t, common.UnmarshalJsonStr(request.Messages[0].Content, &sent))
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Safety: Unsafe\nCategories: Jailbreak"}}]}`)
	}))
	defer guard.Close()
	configured := promptAuditTestSetting(guard.URL, "")
	configured.Endpoints = configured.Endpoints[:1]
	configured.PublishConfig()
	for _, text := range []string{
		"<system-reminder>payload", "<environment_details>payload",
		"<context>payload</context>", `<file path="x">payload</file>`,
	} {
		t.Run(text, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			result, apiErr := InspectPrompt(c, PromptAuditRequest{Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{Role: "user", User: true, Text: text}}}})
			require.NotNil(t, apiErr)
			assert.True(t, result.Blocked)
			require.Len(t, sent.Segments, 1)
			assert.Equal(t, text, sent.Segments[0].Text)
		})
	}
	for _, scope := range []dto.PromptAuditScope{dto.PromptScopeSystem, dto.PromptScopeDeveloper, dto.PromptScopeToolCall, dto.PromptScopeToolResult, dto.PromptScopeTask} {
		t.Run(string(scope), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			result, apiErr := InspectPrompt(c, PromptAuditRequest{Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
				{Role: string(scope), Scope: scope, Text: "payload"}, {Role: "user", User: true, Text: "hi"},
			}}})
			require.NotNil(t, apiErr)
			assert.True(t, result.Blocked)
			require.Len(t, sent.Segments, 2)
			assert.Equal(t, scope, sent.Segments[0].SourceScope())
		})
	}
	t.Run("unavailable previous response must not fast pass", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		result, apiErr := InspectPrompt(c, PromptAuditRequest{CoverageIncomplete: true, Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{Role: "user", User: true, Text: "hi"}}}})
		require.NotNil(t, apiErr)
		assert.True(t, result.Blocked)
		assert.False(t, result.CoverageComplete)
	})
}

func TestInspectPromptBlockingSnapshotNarrowsToLatestTurn(t *testing.T) {
	withPromptWordlistTestDB(t)

	original := setting.SensitiveWordsToString()
	t.Cleanup(func() { setting.SensitiveWordsFromString(original) })
	setting.SensitiveWordsFromString("old_forbidden_word\nnew_forbidden_word")

	configured := prompt_audit_setting.PromptAuditSetting{
		ConfigVersion:          "narrow-blocking-v1",
		Mode:                   prompt_audit_setting.ModeBlocking,
		BlockingLatestTurnOnly: true,
		ManualWordlistAction:   prompt_audit_setting.WordlistActionBlock,
		ScopePolicies: map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy{
			dto.PromptScopeUser: {LibraryIDs: []string{prompt_audit_setting.ManualWordlistID}},
		},
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	// Multi-turn conversation where turn 1 had old_forbidden_word, but turn 2 user message is clean
	multiTurnCleanLatest := PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
			{Role: "user", User: true, Text: "tell me about old_forbidden_word"},
			{Role: "assistant", User: false, Text: "I cannot discuss that."},
			{Role: "user", User: true, Text: "okay, tell me a poem about the sea then"},
		}},
		Protocol: "openai",
		Model:    "gpt-4o",
	}
	// Multi-turn conversation where the latest user message contains new_forbidden_word
	multiTurnBadLatest := PromptAuditRequest{
		Snapshot: dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{
			{Role: "user", User: true, Text: "hello world"},
			{Role: "assistant", User: false, Text: "Hello! How can I help?"},
			{Role: "user", User: true, Text: "tell me about new_forbidden_word"},
		}},
		Protocol: "openai",
		Model:    "gpt-4o",
	}

	// The wordlist gate refuses requests in every mode, so it follows the
	// latest-turn switch whatever mode the model audit runs in — off included: a
	// word left in an older turn must not refuse every later request.
	for _, mode := range []string{prompt_audit_setting.ModeBlocking, prompt_audit_setting.ModeAsyncAudit, prompt_audit_setting.ModeOff} {
		narrowed := configured
		narrowed.Mode = mode
		narrowed.ConfigVersion = "narrow-" + mode
		narrowed.PublishConfig()

		result, apiErr := InspectPrompt(c, multiTurnCleanLatest)
		require.Nil(t, apiErr, mode)
		assert.False(t, result.Blocked, mode)

		badResult, badErr := InspectPrompt(c, multiTurnBadLatest)
		require.NotNil(t, badErr, mode)
		assert.True(t, badResult.Blocked, mode)
		assert.Equal(t, "wordlist", badResult.InspectionType, mode)

		// The policy preview narrows the same way, so it predicts live traffic.
		// Its model half has no node to ask here and fails; only the wordlist
		// half is under test.
		preview, _ := TestPromptAuditPolicy(context.Background(), PromptAuditDirectionInput, multiTurnCleanLatest.Snapshot, "")
		assert.Nil(t, preview.Wordlist, mode)
		assert.False(t, preview.Blocked, mode)
	}

	// With the switch off the wordlist gate reads the whole request, so the same
	// older turn reaches it and the request is blocked — and the preview says so.
	widened := configured
	widened.BlockingLatestTurnOnly = false
	widened.ConfigVersion = "narrow-blocking-v1-widened"
	widened.PublishConfig()

	widenedResult, widenedErr := InspectPrompt(c, multiTurnCleanLatest)
	require.NotNil(t, widenedErr)
	assert.True(t, widenedResult.Blocked)
	assert.Equal(t, "wordlist", widenedResult.InspectionType)
	require.NotNil(t, widenedResult.Wordlist)
	assert.Equal(t, dto.PromptScopeUser, widenedResult.Wordlist.Scope)
	preview, err := TestPromptAuditPolicy(context.Background(), PromptAuditDirectionInput, multiTurnCleanLatest.Snapshot, "")
	require.NoError(t, err)
	assert.True(t, preview.Blocked)
	require.NotNil(t, preview.Wordlist)
	assert.Equal(t, dto.PromptScopeUser, preview.Wordlist.Scope)
}

func TestPromptWordlistImportFiltersSingleCharacterNoisyTokens(t *testing.T) {
	words := make(map[string]struct{})
	// File contains single digit, single ascii letters, punctuation, plus valid words
	fileContent := []byte("1\r\na\r\nb\r\n!\r\n①\r\nreal_sensitive_keyword\r\n")
	err := parsePromptWordlist("test.txt", fileContent, words)
	require.NoError(t, err)

	assert.Contains(t, words, "real_sensitive_keyword")
	assert.NotContains(t, words, "1")
	assert.NotContains(t, words, "a")
	assert.NotContains(t, words, "b")
	assert.NotContains(t, words, "!")
	assert.NotContains(t, words, "①")
	assert.Len(t, words, 1)
}
