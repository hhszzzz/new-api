package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/account_pool_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type accountPoolFakeManagement struct {
	t                 *testing.T
	server            *httptest.Server
	mu                sync.Mutex
	paths             []string
	usageRequests     []map[string]interface{}
	authStatus        string
	authStatusMessage string
	failUsage         bool
	usageStatus       int
	usageBody         string
}

type accountPoolRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn accountPoolRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func newAccountPoolFakeManagement(t *testing.T) *accountPoolFakeManagement {
	fake := &accountPoolFakeManagement{t: t}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *accountPoolFakeManagement) handle(writer http.ResponseWriter, request *http.Request) {
	fake.mu.Lock()
	fake.paths = append(fake.paths, request.Method+" "+request.URL.Path)
	fake.mu.Unlock()
	require.Equal(fake.t, "Bearer management-secret", request.Header.Get("Authorization"))

	switch request.URL.Path {
	case "/v0/management/auth-files":
		require.Equal(fake.t, http.MethodGet, request.Method)
		fake.mu.Lock()
		authStatus := fake.authStatus
		authStatusMessage := fake.authStatusMessage
		fake.mu.Unlock()
		idTokenPayload, err := common.Marshal(map[string]interface{}{
			"https://api.openai.com/auth": map[string]interface{}{
				"chatgpt_account_id":                "account-id-secret",
				"plan_type":                         "team",
				"chatgpt_subscription_active_until": "2026-09-10T00:00:00Z",
			},
		})
		require.NoError(fake.t, err)
		idToken := "header." + base64.RawURLEncoding.EncodeToString(idTokenPayload) + ".signature"
		codexFile := map[string]interface{}{
			"name":       "/private/codex-admin@example.com.json",
			"type":       "codex",
			"auth_index": "auth-index-secret",
			"email":      "admin@example.com",
			"metadata": map[string]interface{}{
				"id_token": idToken,
			},
		}
		if authStatus != "" {
			codexFile["status"] = authStatus
			codexFile["status_message"] = authStatusMessage
		}
		writeAccountPoolTestJSON(fake.t, writer, http.StatusOK, map[string]interface{}{
			"files": []map[string]interface{}{
				codexFile,
			},
		})
	case "/v0/management/api-call":
		require.Equal(fake.t, http.MethodPost, request.Method)
		var payload map[string]interface{}
		require.NoError(fake.t, common.DecodeJson(request.Body, &payload))
		fake.mu.Lock()
		fake.usageRequests = append(fake.usageRequests, payload)
		failUsage := fake.failUsage
		usageStatus := fake.usageStatus
		usageBody := fake.usageBody
		fake.mu.Unlock()
		if failUsage {
			writeAccountPoolTestJSON(fake.t, writer, http.StatusOK, map[string]interface{}{
				"status_code": 401,
				"header":      map[string]interface{}{"Set-Cookie": []string{"secret-cookie"}},
				"body":        `{"error":"Bearer upstream-secret admin@example.com"}`,
			})
			return
		}
		if usageBody != "" {
			writeAccountPoolTestJSON(fake.t, writer, http.StatusOK, map[string]interface{}{
				"status_code": usageStatus,
				"body":        usageBody,
			})
			return
		}
		writeAccountPoolTestJSON(fake.t, writer, http.StatusOK, map[string]interface{}{
			"status_code": 200,
			"header":      map[string]interface{}{"X-Upstream-Secret": []string{"secret-header"}},
			"body": `{
				"plan_type":"plus",
				"rate_limit":{
					"primary_window":{"used_percent":120,"limit_window_seconds":18000,"reset_at":1893456000},
					"secondary_window":{"used_percent":"25.5","limit_window_seconds":604800,"reset_after_seconds":3600}
				}
			}`,
		})
	default:
		fake.t.Fatalf("unexpected management endpoint: %s %s", request.Method, request.URL.Path)
	}
}

func (fake *accountPoolFakeManagement) manager(now time.Time) *accountPoolManager {
	parsed, err := url.Parse(fake.server.URL + "/v0/management")
	require.NoError(fake.t, err)
	setting := accountPoolTestSetting()
	manager := newAccountPoolManager()
	manager.client = fake.server.Client()
	manager.now = func() time.Time { return now }
	manager.loadRuntimeConfig = func() (accountPoolRuntimeConfig, error) {
		return accountPoolRuntimeConfig{baseURL: parsed, key: "management-secret"}, nil
	}
	manager.loadSetting = func() *account_pool_setting.Setting {
		copy := setting
		copy.ProviderGroups = map[string][]string{}
		for provider, groups := range setting.ProviderGroups {
			copy.ProviderGroups[provider] = append([]string(nil), groups...)
		}
		return &copy
	}
	return manager
}

func accountPoolTestSetting() account_pool_setting.Setting {
	return account_pool_setting.Setting{
		Enabled:                   true,
		HideEmailFromNonAdmins:    true,
		ProviderGroups:            map[string][]string{"codex": {"vip"}, "claude": {"vip"}, "antigravity": {"vip"}},
		RegularRefreshSeconds:     300,
		NearResetThresholdSeconds: 600,
		NearResetRefreshSeconds:   60,
		PostResetDelaySeconds:     10,
		ManualRefreshCooldown:     60,
	}
}

func writeAccountPoolTestJSON(t *testing.T, writer http.ResponseWriter, status int, value interface{}) {
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, err = writer.Write(encoded)
	require.NoError(t, err)
}

func TestAccountPoolManagerUsesOnlyFixedReadOnlyManagementContract(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fake := newAccountPoolFakeManagement(t)
	manager := fake.manager(now)

	snapshot, err := manager.get(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Accounts, 1)
	account := snapshot.Accounts[0]
	assert.Equal(t, "limited", account.Status)
	assert.Equal(t, "plus", account.Plan)
	require.NotNil(t, account.PrimaryWindow)
	require.NotNil(t, account.PrimaryWindow.UsedPercent)
	require.NotNil(t, account.PrimaryWindow.RemainingPercent)
	assert.Equal(t, float64(100), *account.PrimaryWindow.UsedPercent)
	assert.Equal(t, float64(0), *account.PrimaryWindow.RemainingPercent)
	assert.Equal(t, AccountPoolSummary{Total: 1, Limited: 1}, snapshot.Summary)

	fake.mu.Lock()
	paths := append([]string(nil), fake.paths...)
	requests := append([]map[string]interface{}(nil), fake.usageRequests...)
	fake.mu.Unlock()
	assert.Equal(t, []string{
		"GET /v0/management/auth-files",
		"POST /v0/management/api-call",
	}, paths)
	require.Len(t, requests, 1)
	assert.Equal(t, "auth-index-secret", requests[0]["auth_index"])
	assert.Equal(t, http.MethodGet, requests[0]["method"])
	assert.Equal(t, accountPoolCodexUsageURL, requests[0]["url"])
	assert.NotContains(t, requests[0], "data")
	headers, ok := requests[0]["header"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Bearer $TOKEN$", headers["Authorization"])
	assert.Equal(t, "account-id-secret", headers["Chatgpt-Account-Id"])

	normalJSON, err := common.Marshal(manager.buildView(snapshot, common.RoleRootUser, nil, false))
	require.NoError(t, err)
	normalResponse := string(normalJSON)
	assert.NotContains(t, normalResponse, "admin@example.com")
	assert.NotContains(t, normalResponse, "auth-index-secret")
	assert.NotContains(t, normalResponse, "/private/")
	assert.NotContains(t, normalResponse, "account-id-secret")
	assert.NotContains(t, normalResponse, "secret-header")

	adminJSON, err := common.Marshal(manager.buildView(snapshot, common.RoleRootUser, nil, true))
	require.NoError(t, err)
	assert.Contains(t, string(adminJSON), `"email":"admin@example.com"`)
}

func TestAccountPoolManagerClassifiesUsageLimitReachedAsLimited(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fake := newAccountPoolFakeManagement(t)
	fake.usageStatus = http.StatusTooManyRequests
	fake.usageBody = `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached"}}`
	manager := fake.manager(now)

	snapshot, err := manager.get(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Accounts, 1)
	assert.Equal(t, "limited", snapshot.Accounts[0].Status)
	assert.False(t, snapshot.Accounts[0].Stale)
	assert.False(t, snapshot.Partial)
	assert.False(t, snapshot.Stale)
	assert.Equal(t, AccountPoolSummary{Total: 1, Limited: 1}, snapshot.Summary)
	assert.Equal(t, "success", manager.syncStatus().LastSyncStatus)
}

func TestAccountPoolManagerClassifiesAuthFileUsageLimitStatusAsLimited(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 30, 0, 0, time.UTC)
	fake := newAccountPoolFakeManagement(t)
	fake.authStatus = "error"
	fake.authStatusMessage = `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached"}}`
	manager := fake.manager(now)

	snapshot, err := manager.get(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Accounts, 1)
	assert.Equal(t, "limited", snapshot.Accounts[0].Status)
	assert.False(t, snapshot.Accounts[0].Stale)
	require.NotNil(t, snapshot.Accounts[0].PrimaryWindow)
	require.NotNil(t, snapshot.Accounts[0].PrimaryWindow.RemainingPercent)
	assert.Equal(t, float64(0), *snapshot.Accounts[0].PrimaryWindow.RemainingPercent)
	require.NotNil(t, snapshot.Accounts[0].SecondaryWindow)
	assert.False(t, snapshot.Partial)
	assert.False(t, snapshot.Stale)
	assert.Equal(t, AccountPoolSummary{Total: 1, Limited: 1}, snapshot.Summary)
	assert.Equal(t, "success", manager.syncStatus().LastSyncStatus)

	fake.mu.Lock()
	paths := append([]string(nil), fake.paths...)
	requests := append([]map[string]interface{}(nil), fake.usageRequests...)
	fake.mu.Unlock()
	assert.Equal(t, []string{
		"GET /v0/management/auth-files",
		"POST /v0/management/api-call",
	}, paths)
	require.Len(t, requests, 1)
	assert.Equal(t, "auth-index-secret", requests[0]["auth_index"])
}

func TestAccountPoolUsageLimitPayloadClassification(t *testing.T) {
	testCases := []struct {
		name    string
		payload map[string]interface{}
		limited bool
	}{
		{name: "nested type", payload: map[string]interface{}{"error": map[string]interface{}{"type": "usage_limit_reached"}}, limited: true},
		{name: "nested code", payload: map[string]interface{}{"error": map[string]interface{}{"code": "usage_limit_reached"}}, limited: true},
		{name: "top level type", payload: map[string]interface{}{"type": "usage_limit_reached"}, limited: true},
		{name: "top level code", payload: map[string]interface{}{"code": "usage_limit_reached"}, limited: true},
		{name: "string error", payload: map[string]interface{}{"error": "usage_limit_reached"}, limited: true},
		{name: "transient rate limit", payload: map[string]interface{}{"error": map[string]interface{}{"type": "rate_limit_exceeded"}}, limited: false},
		{name: "message only", payload: map[string]interface{}{"error": map[string]interface{}{"message": "usage_limit_reached"}}, limited: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.limited, isAccountPoolUsageLimitPayload(testCase.payload))
		})
	}
}

func TestApplyCodexUsagePayloadNormalizesProliteWeeklyOnlyQuota(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	account := accountPoolAccount{}

	_, err := applyCodexUsagePayload(&account, map[string]any{
		"plan_type": "Prolite",
		"rate_limit": map[string]any{
			"primary_window": map[string]any{
				"used_percent":         float64(25),
				"limit_window_seconds": float64(604800),
			},
		},
	}, now)
	require.NoError(t, err)

	assert.Equal(t, "Pro 5x", account.Plan)
	assert.Nil(t, account.PrimaryWindow)
	require.NotNil(t, account.SecondaryWindow)
	require.NotNil(t, account.SecondaryWindow.LimitWindowSeconds)
	assert.Equal(t, int64(604800), *account.SecondaryWindow.LimitWindowSeconds)
}

func TestApplyCodexUsagePayloadKeepsShortQuotaInPrimarySlot(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	account := accountPoolAccount{}

	_, err := applyCodexUsagePayload(&account, map[string]any{
		"rate_limit": map[string]any{
			"primary_window": map[string]any{
				"used_percent":         float64(25),
				"limit_window_seconds": float64(604800),
			},
			"secondary_window": map[string]any{
				"used_percent":         float64(50),
				"limit_window_seconds": float64(18000),
			},
		},
	}, now)
	require.NoError(t, err)

	require.NotNil(t, account.PrimaryWindow)
	require.NotNil(t, account.PrimaryWindow.LimitWindowSeconds)
	assert.Equal(t, int64(18000), *account.PrimaryWindow.LimitWindowSeconds)
	require.NotNil(t, account.SecondaryWindow)
	require.NotNil(t, account.SecondaryWindow.LimitWindowSeconds)
	assert.Equal(t, int64(604800), *account.SecondaryWindow.LimitWindowSeconds)
}

func TestApplyCodexUsagePayloadMarksAnyExhaustedWindowAsLimited(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	testCases := []struct {
		name      string
		rateLimit map[string]interface{}
	}{
		{
			name: "five hour window",
			rateLimit: map[string]interface{}{
				"primary_window": map[string]interface{}{"used_percent": float64(100), "limit_window_seconds": float64(18000)},
			},
		},
		{
			name: "weekly window",
			rateLimit: map[string]interface{}{
				"primary_window":   map[string]interface{}{"used_percent": float64(10), "limit_window_seconds": float64(18000)},
				"secondary_window": map[string]interface{}{"used_percent": float64(100), "limit_window_seconds": float64(604800)},
			},
		},
		{
			name: "explicit limit flag",
			rateLimit: map[string]interface{}{
				"limit_reached": true,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			account := accountPoolAccount{}
			limited, err := applyCodexUsagePayload(&account, map[string]any{
				"rate_limit": testCase.rateLimit,
			}, now)

			require.NoError(t, err)
			assert.True(t, limited)
		})
	}
}

func TestAccountPoolPublicIDDoesNotExposeCredentialIdentity(t *testing.T) {
	credentialName := "codex-admin@example.com.json"
	authIndex := "auth-index-secret"

	publicID := accountPoolPublicID("management-secret", account_pool_setting.ProviderCodex, credentialName, authIndex)

	assert.Len(t, publicID, 24)
	assert.NotContains(t, publicID, "admin@example.com")
	assert.NotContains(t, publicID, authIndex)
	assert.NotEqual(t, publicID, accountPoolPublicID("another-secret", account_pool_setting.ProviderCodex, credentialName, authIndex))
	assert.NotEqual(t, publicID, accountPoolPublicID("management-secret", account_pool_setting.ProviderClaude, credentialName, authIndex))
}

func TestAccountPoolManagerKeepsOldSnapshotWhenRefreshFails(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fake := newAccountPoolFakeManagement(t)
	manager := fake.manager(now)

	first, err := manager.get(context.Background())
	require.NoError(t, err)
	fake.mu.Lock()
	fake.failUsage = true
	fake.mu.Unlock()
	manager.now = func() time.Time { return now.Add(10 * time.Minute) }
	manager.mu.Lock()
	manager.snapshot.NextRefreshAt = now
	manager.mu.Unlock()

	fallback, err := manager.get(context.Background())
	require.NoError(t, err)
	assert.True(t, fallback.Stale)
	assert.True(t, fallback.Partial)
	assert.Equal(t, first.UpdatedAt, fallback.UpdatedAt)
	require.Len(t, fallback.Accounts, 1)
	assert.Equal(t, "limited", fallback.Accounts[0].Status)
	assert.Equal(t, "failed", manager.syncStatus().LastSyncStatus)

	encoded, err := common.Marshal(manager.buildView(fallback, common.RoleRootUser, nil, false))
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "upstream-secret")
	assert.NotContains(t, string(encoded), "secret-cookie")
}

func TestAccountPoolManagerReturnsPartialSnapshotWhenOneAccountFails(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	filesBody, err := common.Marshal(map[string]interface{}{
		"files": []map[string]interface{}{
			{"name": "good.json", "type": "codex", "auth_index": "good"},
			{"name": "bad.json", "type": "codex", "auth_index": "bad"},
		},
	})
	require.NoError(t, err)
	successBody, err := common.Marshal(map[string]interface{}{
		"status_code": 200,
		"body":        `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000}}}`,
	})
	require.NoError(t, err)
	failureBody, err := common.Marshal(map[string]interface{}{
		"status_code": 500,
		"body":        `{"error":"private upstream failure"}`,
	})
	require.NoError(t, err)

	transport := accountPoolRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v0/management/auth-files" {
			return accountPoolHTTPResponse(filesBody), nil
		}
		var payload map[string]interface{}
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		if payload["auth_index"] == "bad" {
			return accountPoolHTTPResponse(failureBody), nil
		}
		return accountPoolHTTPResponse(successBody), nil
	})

	parsed, err := url.Parse("http://cliproxy.test/v0/management")
	require.NoError(t, err)
	setting := accountPoolTestSetting()
	manager := newAccountPoolManager()
	manager.client = &http.Client{Transport: transport}
	manager.now = func() time.Time { return now }
	manager.loadRuntimeConfig = func() (accountPoolRuntimeConfig, error) {
		return accountPoolRuntimeConfig{baseURL: parsed, key: "management-secret"}, nil
	}
	manager.loadSetting = func() *account_pool_setting.Setting {
		copy := setting
		return &copy
	}

	snapshot, err := manager.get(context.Background())
	require.NoError(t, err)
	assert.True(t, snapshot.Partial)
	assert.True(t, snapshot.Stale)
	assert.Equal(t, AccountPoolSummary{Total: 2, Available: 1, Error: 1}, snapshot.Summary)
	assert.Equal(t, "partial", manager.syncStatus().LastSyncStatus)

	viewJSON, err := common.Marshal(manager.buildView(snapshot, common.RoleRootUser, nil, false))
	require.NoError(t, err)
	assert.NotContains(t, string(viewJSON), "private upstream failure")
}

func TestAccountPoolManagerReturnsUnavailableWithoutAnOldSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fake := newAccountPoolFakeManagement(t)
	fake.failUsage = true
	manager := fake.manager(now)

	_, err := manager.get(context.Background())
	require.ErrorIs(t, err, ErrAccountPoolUnavailable)
}

func TestApplyAccountPoolUsagePayloadRejectsIncompleteProviderData(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		provider string
		payload  map[string]any
	}{
		{provider: account_pool_setting.ProviderCodex, payload: map[string]any{"rate_limit": map[string]any{}}},
		{provider: account_pool_setting.ProviderClaude, payload: map[string]any{"usage": map[string]any{}}},
		{provider: account_pool_setting.ProviderAntigravity, payload: map[string]any{"quota": map[string]any{"groups": []any{}}}},
	}

	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			account := accountPoolAccount{Provider: test.provider}
			_, err := applyAccountPoolUsagePayload(&account, test.payload, now)
			require.ErrorIs(t, err, ErrAccountPoolUnavailable)
		})
	}
}

func TestAccountPoolManagerRejectsMalformedSuccessfulQuotaPayload(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fake := newAccountPoolFakeManagement(t)
	fake.usageStatus = http.StatusOK
	fake.usageBody = `{}`
	manager := fake.manager(now)

	_, err := manager.get(context.Background())
	require.ErrorIs(t, err, ErrAccountPoolUnavailable)
}

func TestAccountPoolManagerManualRefreshUsesGlobalCooldown(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fake := newAccountPoolFakeManagement(t)
	manager := fake.manager(now)

	_, err := manager.refreshManually(context.Background())
	require.NoError(t, err)
	_, err = manager.refreshManually(context.Background())
	var cooldownError *AccountPoolManualRefreshCooldownError
	require.ErrorAs(t, err, &cooldownError)
	assert.Equal(t, 60*time.Second, cooldownError.RetryAfter)
}

func TestCalculateAccountPoolNextRefreshAdaptsToResetWindows(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	setting := accountPoolTestSetting()
	limitSeconds := int64(18000)

	resetSoon := now.Add(30 * time.Second)
	accounts := []accountPoolAccount{{
		PrimaryWindow: &AccountPoolWindow{ResetAt: &resetSoon, LimitWindowSeconds: &limitSeconds},
	}}
	next, overdue := calculateAccountPoolNextRefresh(accounts, setting, now)
	assert.False(t, overdue)
	assert.Equal(t, now.Add(40*time.Second), next)

	resetPast := now.Add(-30 * time.Second)
	accounts[0].PrimaryWindow.ResetAt = &resetPast
	next, overdue = calculateAccountPoolNextRefresh(accounts, setting, now)
	assert.True(t, overdue)
	assert.Equal(t, now.Add(60*time.Second), next)
}

func TestAccountPoolRefreshScheduleUsesSharedServerBoundaries(t *testing.T) {
	setting := accountPoolTestSetting()
	firstViewer := time.Date(2026, 8, 29, 12, 2, 13, 0, time.UTC)
	secondViewer := time.Date(2026, 8, 29, 12, 3, 44, 0, time.UTC)
	expected := time.Date(2026, 8, 29, 12, 5, 0, 0, time.UTC)

	firstNext, firstOverdue := calculateAccountPoolNextRefresh(nil, setting, firstViewer)
	secondNext, secondOverdue := calculateAccountPoolNextRefresh(nil, setting, secondViewer)

	assert.False(t, firstOverdue)
	assert.False(t, secondOverdue)
	assert.Equal(t, expected, firstNext)
	assert.Equal(t, expected, secondNext)
}

func TestAccountPoolViewIncludesServerTime(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 2, 13, 0, time.UTC)
	manager := newAccountPoolManager()
	manager.now = func() time.Time { return now }

	view := manager.buildView(&accountPoolSnapshot{}, common.RoleRootUser, nil, false)

	assert.Equal(t, now, view.ServerTime)
}

func TestLoadAccountPoolRuntimeConfigFailsClosed(t *testing.T) {
	t.Setenv(accountPoolManagementURLEnv, "https://user:password@example.com/v0/management")
	t.Setenv(accountPoolManagementKeyEnv, "management-secret")
	_, err := loadAccountPoolRuntimeConfig()
	require.ErrorIs(t, err, ErrAccountPoolNotConfigured)

	t.Setenv(accountPoolManagementURLEnv, "http://cliproxy-app:8317/v0/management")
	t.Setenv(accountPoolManagementKeyEnv, "")
	_, err = loadAccountPoolRuntimeConfig()
	require.ErrorIs(t, err, ErrAccountPoolNotConfigured)
}

func TestDecodeAccountPoolJSONRejectsOversizedPayload(t *testing.T) {
	reader := io.LimitReader(strings.NewReader(strings.Repeat("x", accountPoolMaxResponseBytes+2)), accountPoolMaxResponseBytes+2)
	var payload map[string]interface{}
	err := decodeAccountPoolJSON(reader, &payload)
	require.ErrorIs(t, err, ErrAccountPoolUnavailable)
}

func TestAccountPoolRefreshSharesOneRoundAndLimitsQuotaConcurrency(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	files := make([]map[string]interface{}, 0, 6)
	for index := 0; index < 6; index++ {
		files = append(files, map[string]interface{}{
			"name":       fmt.Sprintf("codex-%d.json", index),
			"type":       "codex",
			"auth_index": fmt.Sprintf("auth-%d", index),
		})
	}
	authBody, err := common.Marshal(map[string]interface{}{"files": files})
	require.NoError(t, err)
	usageBody, err := common.Marshal(map[string]interface{}{
		"status_code": 200,
		"body": `{"plan_type":"plus","rate_limit":{` +
			`"primary_window":{"used_percent":10,"limit_window_seconds":18000}}}`,
	})
	require.NoError(t, err)

	var authCalls atomic.Int32
	var quotaCalls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	quotaStarted := make(chan struct{}, 6)
	releaseQuota := make(chan struct{})
	transport := accountPoolRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v0/management/auth-files" {
			authCalls.Add(1)
			return accountPoolHTTPResponse(authBody), nil
		}
		require.Equal(t, "/v0/management/api-call", request.URL.Path)
		quotaCalls.Add(1)
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		quotaStarted <- struct{}{}
		<-releaseQuota
		active.Add(-1)
		return accountPoolHTTPResponse(usageBody), nil
	})

	parsed, err := url.Parse("http://cliproxy.test/v0/management")
	require.NoError(t, err)
	setting := accountPoolTestSetting()
	manager := newAccountPoolManager()
	manager.client = &http.Client{Transport: transport}
	manager.now = func() time.Time { return now }
	manager.loadRuntimeConfig = func() (accountPoolRuntimeConfig, error) {
		return accountPoolRuntimeConfig{baseURL: parsed, key: "management-secret"}, nil
	}
	manager.loadSetting = func() *account_pool_setting.Setting {
		copy := setting
		return &copy
	}

	start := make(chan struct{})
	resultChannel := make(chan error, 8)
	for index := 0; index < 8; index++ {
		go func() {
			<-start
			_, getErr := manager.get(context.Background())
			resultChannel <- getErr
		}()
	}
	close(start)
	for index := 0; index < accountPoolMaxConcurrency; index++ {
		<-quotaStarted
	}
	assert.Equal(t, int32(accountPoolMaxConcurrency), maxActive.Load())
	close(releaseQuota)
	for index := 0; index < 8; index++ {
		require.NoError(t, <-resultChannel)
	}
	assert.Equal(t, int32(1), authCalls.Load())
	assert.Equal(t, int32(6), quotaCalls.Load())
	assert.Equal(t, int32(accountPoolMaxConcurrency), maxActive.Load())
}

func accountPoolHTTPResponse(body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func TestAccountPoolManagerFetchesClaudeAndAntigravityAccounts(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	filesBody, err := common.Marshal(map[string]interface{}{
		"files": []map[string]interface{}{
			{
				"name":       "codex-admin@example.com.json",
				"type":       "codex",
				"auth_index": "codex-auth",
				"email":      "admin@example.com",
			},
			{
				"name":       "claude-one@example.com.json",
				"type":       "claude",
				"auth_index": "claude-auth",
				"email":      "claude@example.com",
			},
			{
				"name":       "antigravity-a@example.com.json",
				"type":       "antigravity",
				"auth_index": "ag-auth",
				"email":      "a-antigravity@example.com",
				"project_id": "ag-project-secret",
			},
			{
				"name":       "antigravity-b@example.com.json",
				"type":       "antigravity",
				"auth_index": "ag-auth-no-project",
				"email":      "b-antigravity@example.com",
			},
			{
				"name":       "gemini-ignored@example.com.json",
				"type":       "gemini",
				"auth_index": "gemini-auth",
				"email":      "gemini@example.com",
			},
		},
	})
	require.NoError(t, err)

	var apiRequests []map[string]interface{}
	var apiMu sync.Mutex
	transport := accountPoolRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v0/management/auth-files" {
			return accountPoolHTTPResponse(filesBody), nil
		}
		require.Equal(t, "/v0/management/api-call", request.URL.Path)
		var payload map[string]interface{}
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		apiMu.Lock()
		apiRequests = append(apiRequests, payload)
		apiMu.Unlock()
		switch payload["url"] {
		case accountPoolCodexUsageURL:
			return accountPoolHTTPResponse([]byte(`{"status_code":200,"body":"{\"plan_type\":\"plus\",\"rate_limit\":{\"primary_window\":{\"used_percent\":10,\"limit_window_seconds\":18000}}}"}`)), nil
		case accountPoolClaudeUsageURL:
			return accountPoolHTTPResponse([]byte(`{"status_code":200,"body":"{\"five_hour\":{\"utilization\":0.25,\"resets_at\":\"2026-08-29T15:00:00Z\"},\"seven_day\":{\"utilization\":0.5,\"resets_at\":\"2026-09-01T00:00:00Z\"}}"}`)), nil
		case accountPoolClaudeProfileURL:
			return accountPoolHTTPResponse([]byte(`{"status_code":200,"body":"{\"account\":{\"has_claude_max\":true},\"organization\":{\"organization_type\":\"claude_max\"}}"}`)), nil
		case accountPoolAntigravityQuotaDailyURL:
			return accountPoolHTTPResponse([]byte(`{"status_code":200,"body":"{\"groups\":[{\"displayName\":\"Gemini Models\",\"buckets\":[{\"bucketId\":\"g-5h\",\"displayName\":\"Gemini 5h\",\"window\":\"5h\",\"resetTime\":\"2026-08-29T17:00:00Z\",\"remainingFraction\":0.8},{\"bucketId\":\"g-week\",\"displayName\":\"Gemini Weekly\",\"window\":\"weekly\",\"resetTime\":\"2026-09-01T00:00:00Z\",\"remainingFraction\":0.4}]},{\"displayName\":\"Claude and GPT models\",\"buckets\":[{\"bucketId\":\"c-5h\",\"displayName\":\"Claude 5h\",\"window\":\"5h\",\"resetTime\":\"2026-08-29T17:00:00Z\",\"remainingFraction\":1},{\"bucketId\":\"c-week\",\"displayName\":\"Claude Weekly\",\"window\":\"weekly\",\"resetTime\":\"2026-09-01T00:00:00Z\",\"remainingFraction\":0}]}]}"}`)), nil
		case accountPoolAntigravityAssistURL:
			return accountPoolHTTPResponse([]byte(`{"status_code":200,"body":"{\"paidTier\":{\"id\":\"g1-ultra-tier\"}}"}`)), nil
		default:
			return accountPoolHTTPResponse([]byte(`{"status_code":404,"body":"{}"}`)), nil
		}
	})

	parsed, err := url.Parse("http://cliproxy.test/v0/management")
	require.NoError(t, err)
	setting := accountPoolTestSetting()
	setting.ProviderGroups = map[string][]string{"claude": {"team"}}
	manager := newAccountPoolManager()
	manager.client = &http.Client{Transport: transport}
	manager.now = func() time.Time { return now }
	manager.loadRuntimeConfig = func() (accountPoolRuntimeConfig, error) {
		return accountPoolRuntimeConfig{baseURL: parsed, key: "management-secret"}, nil
	}
	manager.loadSetting = func() *account_pool_setting.Setting {
		copy := setting
		copy.ProviderGroups = map[string][]string{"claude": {"team"}}
		return &copy
	}

	snapshot, err := manager.get(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Accounts, 4)
	assert.Equal(t, AccountPoolSummary{Total: 4, Available: 2, Limited: 1, Error: 1}, snapshot.Summary)

	byDisplay := make(map[string]accountPoolAccount, 4)
	for _, account := range snapshot.Accounts {
		byDisplay[account.DisplayName] = account
	}

	codexAccount, ok := byDisplay["Codex #1"]
	require.True(t, ok)
	assert.Equal(t, "codex", codexAccount.Provider)
	assert.Equal(t, "available", codexAccount.Status)

	claudeAccount, ok := byDisplay["Claude #1"]
	require.True(t, ok)
	assert.Equal(t, "claude", claudeAccount.Provider)
	assert.Equal(t, "max", claudeAccount.Plan)
	assert.Equal(t, "available", claudeAccount.Status)
	require.NotNil(t, claudeAccount.PrimaryWindow)
	require.NotNil(t, claudeAccount.PrimaryWindow.UsedPercent)
	require.NotNil(t, claudeAccount.PrimaryWindow.RemainingPercent)
	assert.Equal(t, float64(25), *claudeAccount.PrimaryWindow.UsedPercent)
	assert.Equal(t, float64(75), *claudeAccount.PrimaryWindow.RemainingPercent)
	require.NotNil(t, claudeAccount.SecondaryWindow)
	require.NotNil(t, claudeAccount.SecondaryWindow.UsedPercent)
	assert.Equal(t, float64(50), *claudeAccount.SecondaryWindow.UsedPercent)
	require.Len(t, claudeAccount.WindowGroups, 1)

	antigravityAccount, ok := byDisplay["Antigravity #1"]
	require.True(t, ok)
	assert.Equal(t, "antigravity", antigravityAccount.Provider)
	assert.Equal(t, "ultra", antigravityAccount.Plan)
	assert.Equal(t, "limited", antigravityAccount.Status)
	require.Len(t, antigravityAccount.WindowGroups, 2)
	assert.Equal(t, "Gemini Models", antigravityAccount.WindowGroups[0].Label)
	require.NotNil(t, antigravityAccount.WindowGroups[0].PrimaryWindow)
	require.NotNil(t, antigravityAccount.WindowGroups[0].PrimaryWindow.UsedPercent)
	assert.Equal(t, float64(20), *antigravityAccount.WindowGroups[0].PrimaryWindow.UsedPercent)
	require.NotNil(t, antigravityAccount.WindowGroups[0].SecondaryWindow)
	require.NotNil(t, antigravityAccount.WindowGroups[0].SecondaryWindow.UsedPercent)
	assert.Equal(t, float64(60), *antigravityAccount.WindowGroups[0].SecondaryWindow.UsedPercent)
	assert.Equal(t, "Claude and GPT models", antigravityAccount.WindowGroups[1].Label)
	require.NotNil(t, antigravityAccount.WindowGroups[1].SecondaryWindow)
	require.NotNil(t, antigravityAccount.WindowGroups[1].SecondaryWindow.RemainingPercent)
	assert.Equal(t, float64(0), *antigravityAccount.WindowGroups[1].SecondaryWindow.RemainingPercent)
	require.NotNil(t, antigravityAccount.PrimaryWindow)
	assert.Equal(t, float64(20), *antigravityAccount.PrimaryWindow.UsedPercent)

	brokenAccount, ok := byDisplay["Antigravity #2"]
	require.True(t, ok)
	assert.Equal(t, "error", brokenAccount.Status)
	assert.True(t, brokenAccount.Stale)

	apiMu.Lock()
	requestCount := len(apiRequests)
	apiMu.Unlock()
	assert.Equal(t, 5, requestCount)
	for _, payload := range apiRequests {
		assert.NotEqual(t, "ag-auth-no-project", payload["auth_index"])
	}

	view := manager.buildView(snapshot, common.RoleRootUser, nil, false)
	require.Len(t, view.Accounts, 4)
	assert.Equal(t, AccountPoolSummary{Total: 4, Available: 2, Limited: 1, Error: 1}, view.Summary)
	assert.Equal(t, AccountPoolSummary{Total: 1, Available: 1}, view.ProviderSummaries["codex"])
	assert.Equal(t, AccountPoolSummary{Total: 1, Available: 1}, view.ProviderSummaries["claude"])
	assert.Equal(t, AccountPoolSummary{Total: 2, Limited: 1, Error: 1}, view.ProviderSummaries["antigravity"])

	viewJSON, err := common.Marshal(view)
	require.NoError(t, err)
	assert.Contains(t, string(viewJSON), `"provider_summaries"`)
	assert.NotContains(t, string(viewJSON), "ag-project-secret")

	claudeOnly := manager.buildView(snapshot, common.RoleCommonUser, []string{"team"}, false)
	require.Len(t, claudeOnly.Accounts, 1)
	assert.Equal(t, "claude", claudeOnly.Accounts[0].Provider)
	assert.Equal(t, AccountPoolSummary{Total: 1, Available: 1}, claudeOnly.Summary)
	require.Len(t, claudeOnly.ProviderSummaries, 1)

	// Only claude has a group configured, so any other group sees nothing.
	noAccess := manager.buildView(snapshot, common.RoleCommonUser, []string{"vip"}, false)
	require.Empty(t, noAccess.Accounts)
	require.Empty(t, noAccess.ProviderSummaries)
	assert.Equal(t, AccountPoolSummary{}, noAccess.Summary)
}
