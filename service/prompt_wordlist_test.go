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
	assert.Equal(t, []string{"selected system instruction"}, sent)
	configured.Mode = prompt_audit_setting.ModeAsyncAudit
	configured.PublishConfig()
	result, apiErr = CheckPromptAudit(c, request)
	require.Nil(t, apiErr)
	require.NotZero(t, result.AuditID)
	audit, err := model.GetPromptAudit(result.AuditID)
	require.NoError(t, err)
	assert.Equal(t, "selected system instruction", string(audit.ScanPayload))
	assert.NotContains(t, string(audit.FullPrompt), "unselected")
	assert.Len(t, sent, 1)
}
