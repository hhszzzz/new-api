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
	t             *testing.T
	server        *httptest.Server
	mu            sync.Mutex
	paths         []string
	usageRequests []map[string]interface{}
	failUsage     bool
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
		idTokenPayload, err := common.Marshal(map[string]interface{}{
			"https://api.openai.com/auth": map[string]interface{}{
				"chatgpt_account_id":                "account-id-secret",
				"plan_type":                         "team",
				"chatgpt_subscription_active_until": "2026-09-10T00:00:00Z",
			},
		})
		require.NoError(fake.t, err)
		idToken := "header." + base64.RawURLEncoding.EncodeToString(idTokenPayload) + ".signature"
		writeAccountPoolTestJSON(fake.t, writer, http.StatusOK, map[string]interface{}{
			"files": []map[string]interface{}{
				{
					"name":       "/private/codex-admin@example.com.json",
					"type":       "codex",
					"auth_index": "auth-index-secret",
					"email":      "admin@example.com",
					"metadata": map[string]interface{}{
						"id_token": idToken,
					},
				},
				{
					"name":       "claude.json",
					"type":       "claude",
					"auth_index": "must-not-be-called",
				},
			},
		})
	case "/v0/management/api-call":
		require.Equal(fake.t, http.MethodPost, request.Method)
		var payload map[string]interface{}
		require.NoError(fake.t, common.DecodeJson(request.Body, &payload))
		fake.mu.Lock()
		fake.usageRequests = append(fake.usageRequests, payload)
		failUsage := fake.failUsage
		fake.mu.Unlock()
		if failUsage {
			writeAccountPoolTestJSON(fake.t, writer, http.StatusOK, map[string]interface{}{
				"status_code": 401,
				"header":      map[string]interface{}{"Set-Cookie": []string{"secret-cookie"}},
				"body":        `{"error":"Bearer upstream-secret admin@example.com"}`,
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
		copy.AllowedGroups = append([]string(nil), setting.AllowedGroups...)
		return &copy
	}
	return manager
}

func accountPoolTestSetting() account_pool_setting.Setting {
	return account_pool_setting.Setting{
		Enabled:                   true,
		AllowedGroups:             []string{"vip"},
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
	assert.Equal(t, "available", account.Status)
	assert.Equal(t, "plus", account.Plan)
	require.NotNil(t, account.PrimaryWindow)
	require.NotNil(t, account.PrimaryWindow.UsedPercent)
	require.NotNil(t, account.PrimaryWindow.RemainingPercent)
	assert.Equal(t, float64(100), *account.PrimaryWindow.UsedPercent)
	assert.Equal(t, float64(0), *account.PrimaryWindow.RemainingPercent)

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

	normalJSON, err := common.Marshal(manager.buildView(snapshot, false))
	require.NoError(t, err)
	normalResponse := string(normalJSON)
	assert.NotContains(t, normalResponse, "admin@example.com")
	assert.NotContains(t, normalResponse, "auth-index-secret")
	assert.NotContains(t, normalResponse, "/private/")
	assert.NotContains(t, normalResponse, "account-id-secret")
	assert.NotContains(t, normalResponse, "secret-header")

	adminJSON, err := common.Marshal(manager.buildView(snapshot, true))
	require.NoError(t, err)
	assert.Contains(t, string(adminJSON), `"email":"admin@example.com"`)
}

func TestAccountPoolPublicIDDoesNotExposeCredentialIdentity(t *testing.T) {
	credentialName := "codex-admin@example.com.json"
	authIndex := "auth-index-secret"

	publicID := accountPoolPublicID("management-secret", credentialName, authIndex)

	assert.Len(t, publicID, 24)
	assert.NotContains(t, publicID, "admin@example.com")
	assert.NotContains(t, publicID, authIndex)
	assert.NotEqual(t, publicID, accountPoolPublicID("another-secret", credentialName, authIndex))
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
	assert.Equal(t, "available", fallback.Accounts[0].Status)
	assert.Equal(t, "failed", manager.syncStatus().LastSyncStatus)

	encoded, err := common.Marshal(manager.buildView(fallback, false))
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

	viewJSON, err := common.Marshal(manager.buildView(snapshot, false))
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
