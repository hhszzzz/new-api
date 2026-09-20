package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/account_pool_setting"
	"golang.org/x/sync/singleflight"
)

const (
	accountPoolManagementURLEnv = "CLIPROXY_MANAGEMENT_URL"
	accountPoolManagementKeyEnv = "CLIPROXY_MANAGEMENT_KEY"

	accountPoolAuthFilesPath = "/auth-files"
	accountPoolAPICallPath   = "/api-call"
	accountPoolCodexUsageURL = "https://chatgpt.com/backend-api/wham/usage"
	accountPoolUsageLimited  = "usage_limit_reached"

	accountPoolClaudeUsageURL   = "https://api.anthropic.com/api/oauth/usage"
	accountPoolClaudeProfileURL = "https://api.anthropic.com/api/oauth/profile"
	accountPoolClaudeBetaHeader = "oauth-2025-04-20"

	accountPoolAntigravityQuotaDailyURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
	accountPoolAntigravityQuotaProdURL  = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
	accountPoolAntigravityAssistURL     = "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	accountPoolAntigravityUserAgent     = "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)"

	accountPoolPerRequestTimeout = 12 * time.Second
	accountPoolRoundTimeout      = 20 * time.Second
	accountPoolMaxConcurrency    = 4
	accountPoolMaxResponseBytes  = 8 << 20
	accountPoolMaxResetAfter     = 366 * 24 * 60 * 60
)

var (
	ErrAccountPoolNotConfigured = errors.New("account pool management connection is not configured")
	ErrAccountPoolUnavailable   = errors.New("account pool quota data is unavailable")
	errAccountPoolUsageLimited  = errors.New("account pool usage limit reached")

	accountPoolAntigravityQuotaURLs = []string{accountPoolAntigravityQuotaDailyURL, accountPoolAntigravityQuotaProdURL}
	accountPoolProviderLabels       = map[string]string{
		account_pool_setting.ProviderCodex:       "Codex",
		account_pool_setting.ProviderClaude:      "Claude",
		account_pool_setting.ProviderAntigravity: "Antigravity",
	}
)

type AccountPoolWindow struct {
	UsedPercent        *float64   `json:"used_percent"`
	RemainingPercent   *float64   `json:"remaining_percent"`
	ResetAt            *time.Time `json:"reset_at"`
	LimitWindowSeconds *int64     `json:"limit_window_seconds"`
}

type AccountPoolWindowGroup struct {
	Label           string             `json:"label"`
	PrimaryWindow   *AccountPoolWindow `json:"primary_window"`
	SecondaryWindow *AccountPoolWindow `json:"secondary_window"`
}

type AccountPoolViewAccount struct {
	PublicID                string                   `json:"public_id"`
	Provider                string                   `json:"provider"`
	DisplayName             string                   `json:"display_name"`
	Email                   *string                  `json:"email,omitempty"`
	Status                  string                   `json:"status"`
	Plan                    string                   `json:"plan"`
	SubscriptionActiveUntil *time.Time               `json:"subscription_active_until"`
	PrimaryWindow           *AccountPoolWindow       `json:"primary_window"`
	SecondaryWindow         *AccountPoolWindow       `json:"secondary_window"`
	WindowGroups            []AccountPoolWindowGroup `json:"window_groups"`
	UpdatedAt               time.Time                `json:"updated_at"`
	Stale                   bool                     `json:"stale"`
}

type AccountPoolSummary struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Limited   int `json:"limited"`
	Error     int `json:"error"`
}

type AccountPoolView struct {
	ServerTime               time.Time                     `json:"server_time"`
	UpdatedAt                time.Time                     `json:"updated_at"`
	NextRefreshAt            time.Time                     `json:"next_refresh_at"`
	ManualRefreshAvailableAt time.Time                     `json:"manual_refresh_available_at"`
	Stale                    bool                          `json:"stale"`
	Partial                  bool                          `json:"partial"`
	Summary                  AccountPoolSummary            `json:"summary"`
	ProviderSummaries        map[string]AccountPoolSummary `json:"provider_summaries"`
	Accounts                 []AccountPoolViewAccount      `json:"accounts"`
}

type AccountPoolSyncStatus struct {
	ManagementKeyConfigured bool       `json:"management_key_configured"`
	ManagementReady         bool       `json:"management_ready"`
	LastSyncAt              *time.Time `json:"last_sync_at"`
	LastSyncStatus          string     `json:"last_sync_status"`
}

type AccountPoolManualRefreshCooldownError struct {
	RetryAfter time.Duration
}

func (err *AccountPoolManualRefreshCooldownError) Error() string {
	return "account pool manual refresh is cooling down"
}

type accountPoolRuntimeConfig struct {
	baseURL *url.URL
	key     string
}

type accountPoolAccount struct {
	PublicID                string
	Provider                string
	DisplayName             string
	Email                   string
	Status                  string
	Plan                    string
	SubscriptionActiveUntil *time.Time
	PrimaryWindow           *AccountPoolWindow
	SecondaryWindow         *AccountPoolWindow
	WindowGroups            []AccountPoolWindowGroup
	UpdatedAt               time.Time
	Stale                   bool
}

type accountPoolSnapshot struct {
	UpdatedAt     time.Time
	NextRefreshAt time.Time
	Stale         bool
	Partial       bool
	Summary       AccountPoolSummary
	Accounts      []accountPoolAccount
}

type accountPoolFetchResult struct {
	accounts  []accountPoolAccount
	attempted int
	succeeded int
	failed    int
}

type accountPoolManager struct {
	mu                       sync.RWMutex
	snapshot                 *accountPoolSnapshot
	manualRefreshAvailableAt time.Time
	lastSyncAt               *time.Time
	lastSyncStatus           string
	now                      func() time.Time
	client                   *http.Client
	refreshGroup             singleflight.Group
	loadRuntimeConfig        func() (accountPoolRuntimeConfig, error)
	loadSetting              func() *account_pool_setting.Setting
}

var defaultAccountPoolManager = newAccountPoolManager()

func newAccountPoolManager() *accountPoolManager {
	return &accountPoolManager{
		now:               time.Now,
		client:            newAccountPoolHTTPClient(),
		loadRuntimeConfig: loadAccountPoolRuntimeConfig,
		loadSetting:       account_pool_setting.GetSettingSnapshot,
		lastSyncStatus:    "never",
	}
}

func newAccountPoolHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func GetAccountPool(ctx context.Context, role int, userGroups []string, includeEmail bool) (AccountPoolView, error) {
	snapshot, err := defaultAccountPoolManager.get(ctx)
	if err != nil {
		return AccountPoolView{}, err
	}
	return defaultAccountPoolManager.buildView(snapshot, role, userGroups, includeEmail), nil
}

func RefreshAccountPool(ctx context.Context, role int, userGroups []string, includeEmail bool) (AccountPoolView, error) {
	snapshot, err := defaultAccountPoolManager.refreshManually(ctx)
	if err != nil {
		return AccountPoolView{}, err
	}
	return defaultAccountPoolManager.buildView(snapshot, role, userGroups, includeEmail), nil
}

func GetAccountPoolSyncStatus() AccountPoolSyncStatus {
	return defaultAccountPoolManager.syncStatus()
}

func InvalidateAccountPoolCache() {
	defaultAccountPoolManager.invalidate()
}

func (manager *accountPoolManager) get(ctx context.Context) (*accountPoolSnapshot, error) {
	now := manager.now()
	manager.mu.RLock()
	snapshot := cloneAccountPoolSnapshot(manager.snapshot)
	manager.mu.RUnlock()
	if snapshot != nil && now.Before(snapshot.NextRefreshAt) {
		return snapshot, nil
	}
	return manager.refresh(ctx)
}

func (manager *accountPoolManager) refreshManually(ctx context.Context) (*accountPoolSnapshot, error) {
	now := manager.now()
	setting := manager.loadSetting()
	if setting == nil {
		return nil, ErrAccountPoolUnavailable
	}

	manager.mu.Lock()
	if now.Before(manager.manualRefreshAvailableAt) {
		retryAfter := manager.manualRefreshAvailableAt.Sub(now)
		manager.mu.Unlock()
		return nil, &AccountPoolManualRefreshCooldownError{RetryAfter: retryAfter}
	}
	manager.manualRefreshAvailableAt = now.Add(time.Duration(setting.ManualRefreshCooldown) * time.Second)
	manager.mu.Unlock()

	return manager.refresh(ctx)
}

func (manager *accountPoolManager) refresh(ctx context.Context) (*accountPoolSnapshot, error) {
	resultChannel := manager.refreshGroup.DoChan("account-pool", func() (interface{}, error) {
		return manager.performRefresh()
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultChannel:
		if result.Err != nil {
			return nil, result.Err
		}
		snapshot, ok := result.Val.(*accountPoolSnapshot)
		if !ok || snapshot == nil {
			return nil, ErrAccountPoolUnavailable
		}
		return cloneAccountPoolSnapshot(snapshot), nil
	}
}

func (manager *accountPoolManager) performRefresh() (*accountPoolSnapshot, error) {
	setting := manager.loadSetting()
	if setting == nil {
		return nil, ErrAccountPoolUnavailable
	}
	config, err := manager.loadRuntimeConfig()
	if err != nil {
		manager.recordFailedSync()
		return manager.fallbackSnapshot(setting, err)
	}

	roundContext, cancel := context.WithTimeout(context.Background(), accountPoolRoundTimeout)
	defer cancel()
	result, err := manager.fetchRound(roundContext, config)
	if err != nil {
		manager.recordFailedSync()
		return manager.fallbackSnapshot(setting, err)
	}

	now := manager.now()
	manager.mu.RLock()
	previous := cloneAccountPoolSnapshot(manager.snapshot)
	manager.mu.RUnlock()
	if result.failed > 0 && previous != nil {
		mergeStaleAccountPoolRows(result.accounts, previous.Accounts)
	}
	sortAccountPoolAccounts(result.accounts)
	renumberAccountPoolDisplayNames(result.accounts)

	nextRefreshAt, overdue := calculateAccountPoolNextRefresh(result.accounts, *setting, now)
	partial := result.failed > 0
	snapshot := &accountPoolSnapshot{
		UpdatedAt:     now,
		NextRefreshAt: nextRefreshAt,
		Stale:         partial || overdue,
		Partial:       partial,
		Accounts:      result.accounts,
	}
	snapshot.Summary = summarizeAccountPoolAccounts(snapshot.Accounts)

	manager.mu.Lock()
	manager.snapshot = cloneAccountPoolSnapshot(snapshot)
	manager.lastSyncAt = copyTimePointer(&now)
	if partial {
		manager.lastSyncStatus = "partial"
	} else {
		manager.lastSyncStatus = "success"
	}
	manager.mu.Unlock()
	return snapshot, nil
}

func (manager *accountPoolManager) fallbackSnapshot(setting *account_pool_setting.Setting, cause error) (*accountPoolSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.snapshot == nil {
		if errors.Is(cause, ErrAccountPoolNotConfigured) {
			return nil, ErrAccountPoolNotConfigured
		}
		return nil, ErrAccountPoolUnavailable
	}

	now := manager.now()
	fallback := cloneAccountPoolSnapshot(manager.snapshot)
	fallback.Stale = true
	fallback.Partial = true
	retrySeconds := setting.NearResetRefreshSeconds
	if retrySeconds < 30 {
		retrySeconds = 30
	}
	fallback.NextRefreshAt = nextAccountPoolRefreshBoundary(
		now,
		time.Duration(retrySeconds)*time.Second,
	)
	manager.snapshot = cloneAccountPoolSnapshot(fallback)
	return fallback, nil
}

func (manager *accountPoolManager) recordFailedSync() {
	now := manager.now()
	manager.mu.Lock()
	manager.lastSyncAt = copyTimePointer(&now)
	manager.lastSyncStatus = "failed"
	manager.mu.Unlock()
}

func (manager *accountPoolManager) buildView(snapshot *accountPoolSnapshot, role int, userGroups []string, includeEmail bool) AccountPoolView {
	now := manager.now()
	setting := manager.loadSetting()
	manager.mu.RLock()
	manualAvailableAt := manager.manualRefreshAvailableAt
	manager.mu.RUnlock()
	if manualAvailableAt.IsZero() || manualAvailableAt.Before(now) {
		manualAvailableAt = now
	}

	view := AccountPoolView{
		ServerTime:               now,
		UpdatedAt:                snapshot.UpdatedAt,
		NextRefreshAt:            snapshot.NextRefreshAt,
		ManualRefreshAvailableAt: manualAvailableAt,
		Stale:                    snapshot.Stale,
		Partial:                  snapshot.Partial,
		ProviderSummaries:        map[string]AccountPoolSummary{},
		Accounts:                 make([]AccountPoolViewAccount, 0, len(snapshot.Accounts)),
	}
	for _, account := range snapshot.Accounts {
		if setting == nil || !setting.CanAccessProvider(role, userGroups, account.Provider) {
			continue
		}
		item := AccountPoolViewAccount{
			PublicID:                account.PublicID,
			Provider:                account.Provider,
			DisplayName:             account.DisplayName,
			Status:                  account.Status,
			Plan:                    account.Plan,
			SubscriptionActiveUntil: copyTimePointer(account.SubscriptionActiveUntil),
			PrimaryWindow:           copyAccountPoolWindow(account.PrimaryWindow),
			SecondaryWindow:         copyAccountPoolWindow(account.SecondaryWindow),
			WindowGroups:            copyAccountPoolWindowGroups(account.WindowGroups),
			UpdatedAt:               account.UpdatedAt,
			Stale:                   account.Stale,
		}
		if includeEmail && strings.TrimSpace(account.Email) != "" {
			email := account.Email
			item.Email = &email
		}
		view.ProviderSummaries[account.Provider] = incrementAccountPoolSummary(view.ProviderSummaries[account.Provider], account.Status)
		view.Summary = incrementAccountPoolSummary(view.Summary, account.Status)
		view.Accounts = append(view.Accounts, item)
	}
	return view
}

func (manager *accountPoolManager) invalidate() {
	manager.mu.Lock()
	manager.snapshot = nil
	manager.mu.Unlock()
	manager.refreshGroup.Forget("account-pool")
}

func (manager *accountPoolManager) syncStatus() AccountPoolSyncStatus {
	config, configErr := manager.loadRuntimeConfig()
	keyConfigured := strings.TrimSpace(os.Getenv(accountPoolManagementKeyEnv)) != ""
	if configErr == nil {
		keyConfigured = config.key != ""
	}

	manager.mu.RLock()
	status := AccountPoolSyncStatus{
		ManagementKeyConfigured: keyConfigured,
		ManagementReady:         configErr == nil,
		LastSyncAt:              copyTimePointer(manager.lastSyncAt),
		LastSyncStatus:          manager.lastSyncStatus,
	}
	manager.mu.RUnlock()
	return status
}

func (manager *accountPoolManager) fetchRound(ctx context.Context, config accountPoolRuntimeConfig) (accountPoolFetchResult, error) {
	requestContext, cancel := context.WithTimeout(ctx, accountPoolPerRequestTimeout)
	files, err := manager.fetchAuthFiles(requestContext, config)
	cancel()
	if err != nil {
		return accountPoolFetchResult{}, ErrAccountPoolUnavailable
	}

	now := manager.now()
	result := accountPoolFetchResult{accounts: make([]accountPoolAccount, 0, len(files))}
	type preparedAccount struct {
		account   accountPoolAccount
		authIndex string
		accountID string
		projectID string
		ready     bool
	}
	prepared := make([]preparedAccount, 0, len(files))
	preparedByID := make(map[string]int, len(files))
	for _, file := range files {
		provider := accountPoolFileProvider(file)
		account, authIndex, accountID, projectID, ready := buildAccountPoolAccount(file, provider, config.key, now)
		if existingIndex, exists := preparedByID[account.PublicID]; exists {
			existing := &prepared[existingIndex]
			if !existing.ready && ready {
				*existing = preparedAccount{account: account, authIndex: authIndex, accountID: accountID, projectID: projectID, ready: true}
				continue
			}
			if existing.account.Email == "" {
				existing.account.Email = account.Email
			}
			if existing.account.Plan == "unknown" && account.Plan != "unknown" {
				existing.account.Plan = account.Plan
			}
			if existing.account.SubscriptionActiveUntil == nil {
				existing.account.SubscriptionActiveUntil = copyTimePointer(account.SubscriptionActiveUntil)
			}
			continue
		}
		preparedByID[account.PublicID] = len(prepared)
		prepared = append(prepared, preparedAccount{
			account: account, authIndex: authIndex, accountID: accountID, projectID: projectID, ready: ready,
		})
	}
	type pendingAccount struct {
		index     int
		provider  string
		authIndex string
		accountID string
		projectID string
	}
	pending := make([]pendingAccount, 0, len(prepared))
	for _, candidate := range prepared {
		result.accounts = append(result.accounts, candidate.account)
		if candidate.ready {
			pending = append(pending, pendingAccount{
				index:     len(result.accounts) - 1,
				provider:  candidate.account.Provider,
				authIndex: candidate.authIndex,
				accountID: candidate.accountID,
				projectID: candidate.projectID,
			})
		} else if candidate.account.Status == "error" {
			result.failed++
		}
	}
	result.attempted = len(pending)
	if len(pending) == 0 {
		if result.failed > 0 {
			return result, ErrAccountPoolUnavailable
		}
		return result, nil
	}

	semaphore := make(chan struct{}, accountPoolMaxConcurrency)
	type quotaResult struct {
		index   int
		payload map[string]interface{}
		err     error
	}
	resultChannel := make(chan quotaResult, len(pending))
	var waitGroup sync.WaitGroup
	for _, account := range pending {
		account := account
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				resultChannel <- quotaResult{index: account.index, err: ctx.Err()}
				return
			}
			requestContext, requestCancel := context.WithTimeout(ctx, accountPoolPerRequestTimeout)
			payload, requestErr := manager.fetchAccountPoolQuota(requestContext, config, account.provider, account.authIndex, account.accountID, account.projectID)
			requestCancel()
			resultChannel <- quotaResult{index: account.index, err: requestErr, payload: payload}
		}()
	}
	waitGroup.Wait()
	close(resultChannel)

	for quota := range resultChannel {
		if errors.Is(quota.err, errAccountPoolUsageLimited) {
			result.accounts[quota.index].Status = "limited"
			result.accounts[quota.index].UpdatedAt = now
			result.accounts[quota.index].Stale = false
			result.succeeded++
			continue
		}
		if quota.err != nil {
			result.failed++
			result.accounts[quota.index].Status = "error"
			result.accounts[quota.index].Stale = true
			continue
		}
		limited, applyErr := applyAccountPoolUsagePayload(&result.accounts[quota.index], quota.payload, now)
		if applyErr != nil {
			result.failed++
			result.accounts[quota.index].Status = "error"
			result.accounts[quota.index].Stale = true
			continue
		}
		if limited {
			result.accounts[quota.index].Status = "limited"
		} else {
			result.accounts[quota.index].Status = "available"
		}
		result.accounts[quota.index].UpdatedAt = now
		result.accounts[quota.index].Stale = false
		result.succeeded++
	}
	if result.attempted > 0 && result.succeeded == 0 {
		return result, ErrAccountPoolUnavailable
	}
	return result, nil
}

func (manager *accountPoolManager) fetchAuthFiles(ctx context.Context, config accountPoolRuntimeConfig) ([]map[string]interface{}, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, accountPoolManagementEndpoint(config, accountPoolAuthFilesPath), nil)
	if err != nil {
		return nil, err
	}
	setAccountPoolManagementHeaders(request, config.key)
	response, err := manager.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrAccountPoolUnavailable
	}

	var payload struct {
		Files []map[string]interface{} `json:"files"`
	}
	if err := decodeAccountPoolJSON(response.Body, &payload); err != nil {
		return nil, err
	}
	files := make([]map[string]interface{}, 0, len(payload.Files))
	for _, file := range payload.Files {
		if accountPoolFileProvider(file) != "" {
			files = append(files, file)
		}
	}
	return files, nil
}

// fetchAccountPoolQuota probes per-account usage via the management api-call
// endpoint, dispatching on the credential provider kind.
func (manager *accountPoolManager) fetchAccountPoolQuota(
	ctx context.Context,
	config accountPoolRuntimeConfig,
	provider string,
	authIndex string,
	accountID string,
	projectID string,
) (map[string]interface{}, error) {
	switch provider {
	case account_pool_setting.ProviderClaude:
		return manager.fetchClaudeUsage(ctx, config, authIndex)
	case account_pool_setting.ProviderAntigravity:
		return manager.fetchAntigravityQuota(ctx, config, authIndex, projectID)
	default:
		return manager.fetchCodexUsage(ctx, config, authIndex, accountID)
	}
}

func (manager *accountPoolManager) callAccountPoolAPI(
	ctx context.Context,
	config accountPoolRuntimeConfig,
	authIndex string,
	method string,
	url string,
	headers map[string]string,
	data map[string]any,
) (map[string]interface{}, error) {
	requestPayload := map[string]interface{}{
		"auth_index": authIndex,
		"method":     method,
		"url":        url,
		"header":     headers,
	}
	if data != nil {
		encodedData, err := common.Marshal(data)
		if err != nil {
			return nil, err
		}
		requestPayload["data"] = string(encodedData)
	}
	body, err := common.Marshal(requestPayload)
	if err != nil {
		return nil, err
	}
	return manager.postAccountPoolAPICall(ctx, config, body)
}

func (manager *accountPoolManager) postAccountPoolAPICall(ctx context.Context, config accountPoolRuntimeConfig, body []byte) (map[string]interface{}, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		accountPoolManagementEndpoint(config, accountPoolAPICallPath),
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	setAccountPoolManagementHeaders(request, config.key)
	request.Header.Set("Content-Type", "application/json")
	response, err := manager.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrAccountPoolUnavailable
	}

	var apiResponse struct {
		StatusCode int         `json:"status_code"`
		Body       interface{} `json:"body"`
	}
	if err := decodeAccountPoolJSON(response.Body, &apiResponse); err != nil {
		return nil, err
	}
	payload, err := normalizeAccountPoolPayload(apiResponse.Body)
	if err != nil {
		return nil, err
	}
	if isAccountPoolUsageLimitPayload(payload) {
		return payload, errAccountPoolUsageLimited
	}
	if apiResponse.StatusCode < http.StatusOK || apiResponse.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrAccountPoolUnavailable
	}
	return payload, nil
}

func (manager *accountPoolManager) fetchClaudeUsage(
	ctx context.Context,
	config accountPoolRuntimeConfig,
	authIndex string,
) (map[string]interface{}, error) {
	usagePayload, err := manager.callAccountPoolAPI(ctx, config, authIndex, http.MethodGet, accountPoolClaudeUsageURL, map[string]string{
		"Authorization":  "Bearer $TOKEN$",
		"Accept":         "application/json",
		"Content-Type":   "application/json",
		"anthropic-beta": accountPoolClaudeBetaHeader,
	}, nil)
	if err != nil {
		return nil, err
	}
	profilePayload, err := manager.callAccountPoolAPI(ctx, config, authIndex, http.MethodGet, accountPoolClaudeProfileURL, map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Accept":        "application/json",
	}, nil)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"kind":    "claude",
		"usage":   usagePayload,
		"profile": profilePayload,
	}, nil
}

func (manager *accountPoolManager) fetchAntigravityQuota(
	ctx context.Context,
	config accountPoolRuntimeConfig,
	authIndex string,
	projectID string,
) (map[string]interface{}, error) {
	if projectID == "" {
		return nil, ErrAccountPoolUnavailable
	}
	quotaHeaders := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    accountPoolAntigravityUserAgent,
	}
	var quotaPayload map[string]interface{}
	var quotaErr error
	for _, quotaURL := range accountPoolAntigravityQuotaURLs {
		quotaPayload, quotaErr = manager.callAccountPoolAPI(ctx, config, authIndex, http.MethodPost, quotaURL, quotaHeaders, map[string]interface{}{
			"project": projectID,
		})
		if quotaErr == nil {
			break
		}
		if errors.Is(quotaErr, errAccountPoolUsageLimited) {
			return nil, quotaErr
		}
	}
	if quotaErr != nil {
		return nil, quotaErr
	}
	assistPayload, err := manager.callAccountPoolAPI(ctx, config, authIndex, http.MethodPost, accountPoolAntigravityAssistURL, quotaHeaders, map[string]interface{}{
		"metadata": map[string]interface{}{"ideType": "ANTIGRAVITY"},
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"kind":        "antigravity",
		"quota":       quotaPayload,
		"code_assist": assistPayload,
	}, nil
}

func (manager *accountPoolManager) fetchCodexUsage(
	ctx context.Context,
	config accountPoolRuntimeConfig,
	authIndex string,
	accountID string,
) (map[string]interface{}, error) {
	headers := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Accept":        "application/json",
		"Content-Type":  "application/json",
		"OpenAI-Beta":   "codex-1",
		"Originator":    "Codex Desktop",
		"User-Agent":    "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
	}
	if accountID != "" {
		headers["Chatgpt-Account-Id"] = accountID
	}
	payload, err := manager.callAccountPoolAPI(ctx, config, authIndex, http.MethodGet, accountPoolCodexUsageURL, headers, nil)
	if err != nil {
		return nil, err
	}
	if hasAccountPoolUsageErrorPayload(payload) {
		return nil, ErrAccountPoolUnavailable
	}
	return payload, nil
}

func loadAccountPoolRuntimeConfig() (accountPoolRuntimeConfig, error) {
	rawURL := strings.TrimSpace(os.Getenv(accountPoolManagementURLEnv))
	key := strings.TrimSpace(os.Getenv(accountPoolManagementKeyEnv))
	if rawURL == "" || key == "" {
		return accountPoolRuntimeConfig{}, ErrAccountPoolNotConfigured
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return accountPoolRuntimeConfig{}, ErrAccountPoolNotConfigured
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return accountPoolRuntimeConfig{}, ErrAccountPoolNotConfigured
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(parsed.Path, "/v0/management") {
		return accountPoolRuntimeConfig{}, ErrAccountPoolNotConfigured
	}
	return accountPoolRuntimeConfig{baseURL: parsed, key: key}, nil
}

func accountPoolManagementEndpoint(config accountPoolRuntimeConfig, suffix string) string {
	endpoint := *config.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + suffix
	endpoint.RawPath = ""
	return endpoint.String()
}

func setAccountPoolManagementHeaders(request *http.Request, key string) {
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
}

func decodeAccountPoolJSON(reader io.Reader, target interface{}) error {
	data, err := io.ReadAll(io.LimitReader(reader, accountPoolMaxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > accountPoolMaxResponseBytes {
		return ErrAccountPoolUnavailable
	}
	return common.Unmarshal(data, target)
}

func normalizeAccountPoolPayload(body interface{}) (map[string]interface{}, error) {
	if body == nil {
		return nil, ErrAccountPoolUnavailable
	}
	if payload, ok := body.(map[string]interface{}); ok {
		return payload, nil
	}
	if text, ok := body.(string); ok {
		var payload map[string]interface{}
		if err := common.Unmarshal([]byte(text), &payload); err != nil {
			return nil, ErrAccountPoolUnavailable
		}
		return payload, nil
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, ErrAccountPoolUnavailable
	}
	var payload map[string]interface{}
	if err := common.Unmarshal(encoded, &payload); err != nil {
		return nil, ErrAccountPoolUnavailable
	}
	return payload, nil
}

func isAccountPoolUsageLimitPayload(payload map[string]interface{}) bool {
	if strings.EqualFold(firstAccountPoolString(payload, "type", "code"), accountPoolUsageLimited) {
		return true
	}
	errorValue, exists := payload["error"]
	if !exists {
		return false
	}
	if errorText, ok := errorValue.(string); ok {
		return strings.EqualFold(strings.TrimSpace(errorText), accountPoolUsageLimited)
	}
	errorPayload, ok := errorValue.(map[string]interface{})
	return ok && strings.EqualFold(firstAccountPoolString(errorPayload, "type", "code"), accountPoolUsageLimited)
}

func isAccountPoolUsageLimitValue(value interface{}) bool {
	if text, ok := value.(string); ok && strings.EqualFold(strings.TrimSpace(text), accountPoolUsageLimited) {
		return true
	}
	payload, err := normalizeAccountPoolPayload(value)
	return err == nil && isAccountPoolUsageLimitPayload(payload)
}

func hasAccountPoolUsageErrorPayload(payload map[string]interface{}) bool {
	if errorValue, exists := payload["error"]; exists {
		switch typed := errorValue.(type) {
		case nil:
			return false
		case string:
			return strings.TrimSpace(typed) != ""
		case map[string]interface{}:
			return len(typed) > 0
		default:
			return true
		}
	}
	return firstAccountPoolString(payload, "type", "code") != "" &&
		firstAccountPoolMap(payload, "rate_limit", "rateLimit") == nil
}

func isCodexAuthFile(file map[string]interface{}) bool {
	return accountPoolFileProvider(file) == account_pool_setting.ProviderCodex
}

func accountPoolFileProvider(file map[string]interface{}) string {
	provider := strings.ToLower(firstAccountPoolString(file, "type", "provider"))
	switch provider {
	case account_pool_setting.ProviderCodex, account_pool_setting.ProviderClaude, account_pool_setting.ProviderAntigravity:
		return provider
	}
	provider = strings.ToLower(firstAccountPoolString(file, "provider", "type"))
	switch provider {
	case account_pool_setting.ProviderCodex, account_pool_setting.ProviderClaude, account_pool_setting.ProviderAntigravity:
		return provider
	}
	return ""
}

func buildAccountPoolAccount(file map[string]interface{}, provider string, idSecret string, now time.Time) (accountPoolAccount, string, string, string, bool) {
	authIndex := normalizeAccountPoolAuthIndex(file["auth_index"])
	name := firstAccountPoolString(file, "name")
	plan := findAccountPoolString(file, "plan_type", "planType", "chatgpt_plan_type")
	subscriptionActiveUntil := findAccountPoolTime(
		file,
		now,
		"subscription_active_until",
		"subscriptionActiveUntil",
		"chatgpt_subscription_active_until",
		"chatgptSubscriptionActiveUntil",
	)
	if subscriptionActiveUntil == nil {
		subscriptionActiveUntil = findAccountPoolNestedSubscriptionTime(file, now)
	}
	authInfo := findAccountPoolCodexAuthInfo(file)
	if plan == "" {
		plan = firstAccountPoolString(authInfo, "plan_type", "planType")
	}
	if subscriptionActiveUntil == nil {
		subscriptionActiveUntil = findAccountPoolSubscriptionTime(authInfo, now)
	}
	account := accountPoolAccount{
		PublicID:                accountPoolPublicID(idSecret, provider, name, authIndex),
		Provider:                provider,
		Email:                   findAccountPoolString(file, "email"),
		Status:                  "available",
		Plan:                    normalizeAccountPoolPlan(plan),
		SubscriptionActiveUntil: subscriptionActiveUntil,
		UpdatedAt:               now,
	}
	status := strings.ToLower(firstAccountPoolString(file, "status"))
	unavailable := firstAccountPoolBool(file, "unavailable")
	if firstAccountPoolBool(file, "disabled") {
		account.Status = "disabled"
		return account, "", "", "", false
	}
	statusMessage := firstAccountPoolValue(file, "status_message", "statusMessage")
	usageLimited := (status == "error" || status == "unavailable" || unavailable) && isAccountPoolUsageLimitValue(statusMessage)
	if usageLimited {
		account.Status = "limited"
	}
	if !usageLimited && unavailable {
		account.Status = "unavailable"
		return account, "", "", "", false
	}
	if !usageLimited && (status == "disabled" || status == "error" || status == "unavailable") {
		account.Status = status
		return account, "", "", "", false
	}
	if authIndex == "" {
		account.Status = "error"
		return account, "", "", "", false
	}
	projectID := ""
	accountID := ""
	if provider == account_pool_setting.ProviderCodex {
		accountID = findAccountPoolString(file, "chatgpt_account_id", "chatgptAccountId")
		if accountID == "" {
			accountID = firstAccountPoolString(authInfo, "chatgpt_account_id", "chatgptAccountId")
		}
	}
	if provider == account_pool_setting.ProviderAntigravity {
		projectID = firstAccountPoolString(file, "project_id", "projectId")
		if projectID == "" {
			projectID = firstAccountPoolString(authInfo, "project_id", "projectId")
		}
	}
	return account, authIndex, accountID, projectID, true
}

func applyCodexUsagePayload(account *accountPoolAccount, payload map[string]any, now time.Time) (bool, error) {
	plan := firstAccountPoolString(payload, "plan_type", "planType")
	if plan != "" {
		account.Plan = normalizeAccountPoolPlan(plan)
	}
	rateLimit := firstAccountPoolMap(payload, "rate_limit", "rateLimit")
	if rateLimit == nil {
		return false, ErrAccountPoolUnavailable
	}
	primary := firstAccountPoolMap(rateLimit, "primary_window", "primaryWindow")
	secondary := firstAccountPoolMap(rateLimit, "secondary_window", "secondaryWindow")
	primaryWindow := parseAccountPoolWindow(primary, rateLimit, now)
	secondaryWindow := parseAccountPoolWindow(secondary, rateLimit, now)
	primaryWindow, secondaryWindow = normalizeAccountPoolQuotaWindows(primaryWindow, secondaryWindow)
	account.PrimaryWindow = primaryWindow
	account.SecondaryWindow = secondaryWindow
	account.WindowGroups = nil
	if primaryWindow != nil || secondaryWindow != nil {
		account.WindowGroups = []AccountPoolWindowGroup{{
			PrimaryWindow:   primaryWindow,
			SecondaryWindow: secondaryWindow,
		}}
	}
	limited := firstAccountPoolBool(rateLimit, "limit_reached", "limitReached") ||
		hasExplicitAccountPoolFalse(rateLimit, "allowed") ||
		isAccountPoolQuotaWindowLimited(primary, primaryWindow) ||
		isAccountPoolQuotaWindowLimited(secondary, secondaryWindow)
	if primaryWindow == nil && secondaryWindow == nil && !limited {
		return false, ErrAccountPoolUnavailable
	}
	return limited, nil
}

// applyAccountPoolUsagePayload dispatches a successful api-call payload to the
// per-provider parser. A structurally incomplete quota response is unavailable,
// rather than evidence that the account has quota remaining.
func applyAccountPoolUsagePayload(account *accountPoolAccount, payload map[string]any, now time.Time) (bool, error) {
	switch account.Provider {
	case account_pool_setting.ProviderClaude:
		return applyClaudeUsagePayload(account, payload, now)
	case account_pool_setting.ProviderAntigravity:
		return applyAntigravityQuotaPayload(account, payload, now)
	default:
		return applyCodexUsagePayload(account, payload, now)
	}
}

func applyClaudeUsagePayload(account *accountPoolAccount, payload map[string]any, now time.Time) (bool, error) {
	usage := firstAccountPoolMap(payload, "usage")
	if usage == nil {
		return false, ErrAccountPoolUnavailable
	}
	fiveHour := firstAccountPoolMap(usage, "five_hour")
	sevenDay := firstAccountPoolMap(usage, "seven_day")
	account.PrimaryWindow = parseAccountPoolUtilizationWindow(fiveHour, now)
	account.SecondaryWindow = parseAccountPoolUtilizationWindow(sevenDay, now)
	account.WindowGroups = nil
	if account.PrimaryWindow != nil || account.SecondaryWindow != nil {
		account.WindowGroups = []AccountPoolWindowGroup{{
			PrimaryWindow:   account.PrimaryWindow,
			SecondaryWindow: account.SecondaryWindow,
		}}
	}
	if profile := firstAccountPoolMap(payload, "profile"); profile != nil {
		account.Plan = accountPoolClaudePlan(profile)
	}
	if account.PrimaryWindow == nil && account.SecondaryWindow == nil {
		return false, ErrAccountPoolUnavailable
	}
	return isAccountPoolQuotaWindowLimited(nil, account.PrimaryWindow) ||
		isAccountPoolQuotaWindowLimited(nil, account.SecondaryWindow), nil
}

func parseAccountPoolUtilizationWindow(source map[string]interface{}, now time.Time) *AccountPoolWindow {
	if source == nil {
		return nil
	}
	utilization, ok := firstAccountPoolNumber(source, "utilization")
	if !ok {
		return nil
	}
	used := math.Max(0, math.Min(100, utilization))
	remaining := math.Max(0, math.Min(100, 100-used))
	window := AccountPoolWindow{UsedPercent: &used, RemainingPercent: &remaining}
	if resetAt := parseAccountPoolTime(firstAccountPoolValue(source, "resets_at", "resetsAt"), now, false); resetAt != nil {
		window.ResetAt = resetAt
	}
	return &window
}

func accountPoolClaudePlan(profile map[string]interface{}) string {
	accountInfo := firstAccountPoolMap(profile, "account")
	organization := firstAccountPoolMap(profile, "organization")
	organizationType := ""
	if organization != nil {
		organizationType = strings.ToLower(firstAccountPoolString(organization, "organization_type", "organizationType"))
	}
	hasMax := accountInfo != nil && firstAccountPoolBool(accountInfo, "has_claude_max", "hasClaudeMax")
	hasPro := accountInfo != nil && firstAccountPoolBool(accountInfo, "has_claude_pro", "hasClaudePro")
	switch {
	case hasMax:
		return "max"
	case hasPro:
		return "pro"
	case organizationType == "claude_team":
		return "team"
	case organizationType != "" && organizationType != "claude_pro" && organizationType != "unknown":
		return organizationType
	default:
		return "free"
	}
}

func applyAntigravityQuotaPayload(account *accountPoolAccount, payload map[string]any, now time.Time) (bool, error) {
	quota := firstAccountPoolMap(payload, "quota")
	if quota == nil {
		return false, ErrAccountPoolUnavailable
	}
	if assist := firstAccountPoolMap(payload, "code_assist"); assist != nil {
		account.Plan = accountPoolAntigravityPlan(assist)
	}
	groups, _ := quota["groups"].([]interface{})
	if len(groups) == 0 {
		return false, ErrAccountPoolUnavailable
	}
	var windowGroups []AccountPoolWindowGroup
	limited := false
	for _, rawGroup := range groups {
		group, ok := rawGroup.(map[string]interface{})
		if !ok {
			continue
		}
		label := firstAccountPoolString(group, "displayName", "display_name")
		buckets, _ := group["buckets"].([]interface{})
		var primary *AccountPoolWindow
		var secondary *AccountPoolWindow
		for _, rawBucket := range buckets {
			bucket, ok := rawBucket.(map[string]interface{})
			if !ok {
				continue
			}
			window := parseAccountPoolRemainingWindow(bucket, now)
			if window == nil {
				continue
			}
			if isAccountPoolQuotaWindowLimited(nil, window) {
				limited = true
			}
			switch strings.ToLower(firstAccountPoolString(bucket, "window")) {
			case "weekly":
				if secondary == nil {
					secondary = window
				}
			default:
				if primary == nil {
					primary = window
				}
			}
		}
		if primary == nil && secondary == nil {
			continue
		}
		windowGroups = append(windowGroups, AccountPoolWindowGroup{
			Label:           label,
			PrimaryWindow:   primary,
			SecondaryWindow: secondary,
		})
	}
	if len(windowGroups) == 0 {
		return false, ErrAccountPoolUnavailable
	}
	account.WindowGroups = windowGroups
	account.PrimaryWindow = windowGroups[0].PrimaryWindow
	account.SecondaryWindow = windowGroups[0].SecondaryWindow
	return limited, nil
}

func parseAccountPoolRemainingWindow(bucket map[string]interface{}, now time.Time) *AccountPoolWindow {
	fraction, ok := firstAccountPoolNumber(bucket, "remainingFraction", "remaining_fraction")
	if !ok {
		return nil
	}
	fraction = math.Max(0, math.Min(1, fraction))
	remaining := math.Round(fraction * 100)
	used := math.Max(0, math.Min(100, 100-remaining))
	window := AccountPoolWindow{UsedPercent: &used, RemainingPercent: &remaining}
	if resetAt := parseAccountPoolTime(firstAccountPoolValue(bucket, "resetTime", "reset_time"), now, false); resetAt != nil {
		window.ResetAt = resetAt
	}
	return &window
}

func accountPoolAntigravityPlan(assist map[string]interface{}) string {
	tier := firstAccountPoolMap(assist, "paidTier")
	if tier == nil {
		tier = firstAccountPoolMap(assist, "currentTier")
	}
	if tier == nil {
		return "unknown"
	}
	switch strings.ToLower(firstAccountPoolString(tier, "id")) {
	case "free-tier":
		return "free"
	case "g1-pro-tier":
		return "pro"
	case "g1-ultra-tier":
		return "ultra"
	case "g1-ultra-lite-tier":
		return "ultra-lite"
	default:
		return normalizeAccountPoolPlan(firstAccountPoolString(tier, "id"))
	}
}

func isAccountPoolQuotaWindowLimited(source map[string]interface{}, window *AccountPoolWindow) bool {
	if firstAccountPoolBool(source, "limit_reached", "limitReached") || hasExplicitAccountPoolFalse(source, "allowed") {
		return true
	}
	return window != nil && window.RemainingPercent != nil && *window.RemainingPercent <= 0
}

func parseAccountPoolWindow(window map[string]interface{}, limit map[string]interface{}, now time.Time) *AccountPoolWindow {
	if window == nil {
		return nil
	}
	used, hasUsed := firstAccountPoolNumber(window, "used_percent", "usedPercent")
	if !hasUsed && (firstAccountPoolBool(limit, "limit_reached", "limitReached") || hasExplicitAccountPoolFalse(limit, "allowed")) {
		used = 100
		hasUsed = true
	}
	var usedPointer *float64
	var remainingPointer *float64
	if hasUsed {
		used = math.Max(0, math.Min(100, used))
		remaining := math.Max(0, math.Min(100, 100-used))
		usedPointer = &used
		remainingPointer = &remaining
	}
	limitSeconds, hasLimitSeconds := firstAccountPoolNumber(window, "limit_window_seconds", "limitWindowSeconds")
	var limitPointer *int64
	if hasLimitSeconds && limitSeconds >= 0 && limitSeconds <= math.MaxInt64 && limitSeconds == math.Trunc(limitSeconds) {
		seconds := int64(limitSeconds)
		limitPointer = &seconds
	}
	resetAt := parseAccountPoolTime(firstAccountPoolValue(window, "reset_at", "resetAt"), now, false)
	if resetAt == nil {
		resetAfter, ok := firstAccountPoolNumber(window, "reset_after_seconds", "resetAfterSeconds")
		if ok && resetAfter >= 0 && resetAfter <= accountPoolMaxResetAfter {
			parsed := now.Add(time.Duration(resetAfter * float64(time.Second)))
			resetAt = &parsed
		}
	}
	if usedPointer == nil && limitPointer == nil && resetAt == nil {
		return nil
	}
	return &AccountPoolWindow{
		UsedPercent:        usedPointer,
		RemainingPercent:   remainingPointer,
		ResetAt:            resetAt,
		LimitWindowSeconds: limitPointer,
	}
}

func calculateAccountPoolNextRefresh(accounts []accountPoolAccount, setting account_pool_setting.Setting, now time.Time) (time.Time, bool) {
	interval := time.Duration(setting.RegularRefreshSeconds) * time.Second
	nearInterval := time.Duration(setting.NearResetRefreshSeconds) * time.Second
	threshold := time.Duration(setting.NearResetThresholdSeconds) * time.Second
	postResetDelay := time.Duration(setting.PostResetDelaySeconds) * time.Second
	var earliestDue time.Time
	overdue := false

	for _, account := range accounts {
		for _, window := range accountPoolAccountWindows(account) {
			if window == nil || window.ResetAt == nil {
				continue
			}
			due := window.ResetAt.Add(postResetDelay)
			if earliestDue.IsZero() || due.Before(earliestDue) {
				earliestDue = due
			}
			untilReset := window.ResetAt.Sub(now)
			if untilReset > 0 && untilReset <= threshold && nearInterval < interval {
				interval = nearInterval
			}
			if !due.After(now) {
				overdue = true
			}
		}
	}

	next := nextAccountPoolRefreshBoundary(now, interval)
	if !earliestDue.IsZero() && earliestDue.After(now) && earliestDue.Before(next) {
		next = earliestDue
	}
	if overdue {
		next = nextAccountPoolRefreshBoundary(now, nearInterval)
	}
	return next, overdue
}

func accountPoolAccountWindows(account accountPoolAccount) []*AccountPoolWindow {
	if len(account.WindowGroups) == 0 {
		return []*AccountPoolWindow{account.PrimaryWindow, account.SecondaryWindow}
	}
	windows := make([]*AccountPoolWindow, 0, len(account.WindowGroups)*2)
	for _, group := range account.WindowGroups {
		windows = append(windows, group.PrimaryWindow, group.SecondaryWindow)
	}
	return windows
}

func nextAccountPoolRefreshBoundary(now time.Time, interval time.Duration) time.Time {
	if interval <= 0 {
		return now
	}
	return now.Truncate(interval).Add(interval)
}

func mergeStaleAccountPoolRows(accounts []accountPoolAccount, previous []accountPoolAccount) {
	previousByID := make(map[string]accountPoolAccount, len(previous))
	for _, account := range previous {
		previousByID[account.PublicID] = account
	}
	for index := range accounts {
		if accounts[index].Status != "error" {
			continue
		}
		old, ok := previousByID[accounts[index].PublicID]
		if !ok {
			continue
		}
		if accounts[index].Email == "" {
			accounts[index].Email = old.Email
		}
		if accounts[index].Plan == "unknown" {
			accounts[index].Plan = old.Plan
		}
		if accounts[index].SubscriptionActiveUntil == nil {
			accounts[index].SubscriptionActiveUntil = copyTimePointer(old.SubscriptionActiveUntil)
		}
		accounts[index].PrimaryWindow = copyAccountPoolWindow(old.PrimaryWindow)
		accounts[index].SecondaryWindow = copyAccountPoolWindow(old.SecondaryWindow)
		accounts[index].WindowGroups = copyAccountPoolWindowGroups(old.WindowGroups)
		accounts[index].UpdatedAt = old.UpdatedAt
		accounts[index].Stale = true
	}
}

func renumberAccountPoolDisplayNames(accounts []accountPoolAccount) {
	counters := make(map[string]int, 3)
	for index := range accounts {
		label := accountPoolProviderLabel(accounts[index].Provider)
		counters[accounts[index].Provider]++
		accounts[index].DisplayName = fmt.Sprintf("%s #%d", label, counters[accounts[index].Provider])
	}
}

func accountPoolProviderLabel(provider string) string {
	if label, ok := accountPoolProviderLabels[provider]; ok {
		return label
	}
	return strings.Title(provider)
}

func sortAccountPoolAccounts(accounts []accountPoolAccount) {
	sort.SliceStable(accounts, func(left int, right int) bool {
		leftProvider := accounts[left].Provider
		rightProvider := accounts[right].Provider
		if leftProvider != rightProvider {
			return providerSortOrder(leftProvider) < providerSortOrder(rightProvider)
		}
		leftEmail := strings.ToLower(accounts[left].Email)
		rightEmail := strings.ToLower(accounts[right].Email)
		if leftEmail != rightEmail {
			if leftEmail == "" {
				return false
			}
			if rightEmail == "" {
				return true
			}
			return leftEmail < rightEmail
		}
		return accounts[left].PublicID < accounts[right].PublicID
	})
}

func providerSortOrder(provider string) int {
	switch provider {
	case account_pool_setting.ProviderClaude:
		return 1
	case account_pool_setting.ProviderAntigravity:
		return 2
	default:
		return 0
	}
}

func incrementAccountPoolSummary(summary AccountPoolSummary, status string) AccountPoolSummary {
	switch status {
	case "available":
		summary.Available++
	case "limited":
		summary.Limited++
	default:
		summary.Error++
	}
	summary.Total++
	return summary
}

func summarizeAccountPoolAccounts(accounts []accountPoolAccount) AccountPoolSummary {
	summary := AccountPoolSummary{Total: len(accounts)}
	for _, account := range accounts {
		switch account.Status {
		case "available":
			summary.Available++
		case "limited":
			summary.Limited++
		default:
			summary.Error++
		}
	}
	return summary
}

func accountPoolPublicID(secret string, provider string, name string, authIndex string) string {
	digest := hmac.New(sha256.New, []byte(secret))
	_, _ = digest.Write([]byte(provider + "\x00" + strings.TrimSpace(name) + "\x00" + authIndex))
	return hex.EncodeToString(digest.Sum(nil)[:12])
}

func normalizeAccountPoolAuthIndex(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if !math.IsNaN(typed) && !math.IsInf(typed, 0) && typed == math.Trunc(typed) && typed >= math.MinInt64 && typed <= math.MaxInt64 {
			return strconv.FormatInt(int64(typed), 10)
		}
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	}
	return ""
}

func findAccountPoolString(file map[string]interface{}, keys ...string) string {
	for _, scope := range accountPoolScopes(file) {
		if value := firstAccountPoolString(scope, keys...); value != "" {
			return value
		}
	}
	return ""
}

func findAccountPoolTime(file map[string]interface{}, now time.Time, keys ...string) *time.Time {
	for _, scope := range accountPoolScopes(file) {
		for _, key := range keys {
			if parsed := parseAccountPoolTime(scope[key], now, false); parsed != nil {
				return parsed
			}
		}
	}
	return nil
}

func accountPoolScopes(file map[string]interface{}) []map[string]interface{} {
	scopes := []map[string]interface{}{file}
	for _, key := range []string{"metadata", "attributes"} {
		if nested, ok := file[key].(map[string]interface{}); ok {
			scopes = append(scopes, nested)
		}
	}
	return scopes
}

func findAccountPoolCodexAuthInfo(file map[string]interface{}) map[string]interface{} {
	for _, scope := range accountPoolScopes(file) {
		payload := parseAccountPoolTokenPayload(scope["id_token"])
		if payload == nil {
			continue
		}
		if nested := firstAccountPoolMap(payload, "https://api.openai.com/auth"); nested != nil {
			return nested
		}
		return payload
	}
	return nil
}

func parseAccountPoolTokenPayload(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	if payload, ok := value.(map[string]interface{}); ok {
		return payload
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var payload map[string]interface{}
	if common.Unmarshal([]byte(text), &payload) == nil && payload != nil {
		return payload
	}
	segments := strings.Split(text, ".")
	if len(segments) < 2 {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return nil
	}
	if common.Unmarshal(decoded, &payload) != nil {
		return nil
	}
	return payload
}

func findAccountPoolSubscriptionTime(authInfo map[string]interface{}, now time.Time) *time.Time {
	if authInfo == nil {
		return nil
	}
	for _, key := range []string{
		"chatgpt_subscription_active_until",
		"chatgptSubscriptionActiveUntil",
		"subscription_active_until",
		"subscriptionActiveUntil",
	} {
		if parsed := parseAccountPoolTime(authInfo[key], now, false); parsed != nil {
			return parsed
		}
	}
	subscription := firstAccountPoolMap(authInfo, "subscription")
	if subscription == nil {
		return nil
	}
	return parseAccountPoolTime(firstAccountPoolValue(subscription, "active_until", "activeUntil"), now, false)
}

func findAccountPoolNestedSubscriptionTime(file map[string]interface{}, now time.Time) *time.Time {
	for _, scope := range accountPoolScopes(file) {
		subscription := firstAccountPoolMap(scope, "subscription")
		if subscription == nil {
			continue
		}
		if parsed := parseAccountPoolTime(firstAccountPoolValue(subscription, "active_until", "activeUntil"), now, false); parsed != nil {
			return parsed
		}
	}
	return nil
}

func firstAccountPoolMap(data map[string]interface{}, keys ...string) map[string]interface{} {
	for _, key := range keys {
		if value, ok := data[key].(map[string]interface{}); ok {
			return value
		}
	}
	return nil
}

func firstAccountPoolString(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := data[key].(string); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func firstAccountPoolBool(data map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		switch value := data[key].(type) {
		case bool:
			if value {
				return true
			}
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(value))
			if err == nil && parsed {
				return true
			}
		}
	}
	return false
}

func hasExplicitAccountPoolFalse(data map[string]interface{}, key string) bool {
	value, exists := data[key]
	if !exists {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return !typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && !parsed
	default:
		return false
	}
}

func firstAccountPoolNumber(data map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		if value, ok := accountPoolNumber(data[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func accountPoolNumber(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case float32:
		converted := float64(typed)
		return converted, !math.IsNaN(converted) && !math.IsInf(converted, 0)
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	default:
		return 0, false
	}
}

func firstAccountPoolValue(data map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := data[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func parseAccountPoolTime(value interface{}, now time.Time, offset bool) *time.Time {
	if value == nil {
		return nil
	}
	if number, ok := accountPoolNumber(value); ok {
		if offset {
			parsed := now.Add(time.Duration(number * float64(time.Second)))
			return &parsed
		}
		if number > 1e12 {
			parsed := time.UnixMilli(int64(number)).UTC()
			return validAccountPoolTime(parsed)
		}
		if number > 0 {
			parsed := time.Unix(int64(number), 0).UTC()
			return validAccountPoolTime(parsed)
		}
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		parsed = parsed.UTC()
		return validAccountPoolTime(parsed)
	}
	if parsed, err := time.Parse("2006-01-02T15:04:05.999999999", text); err == nil {
		parsed = parsed.UTC()
		return validAccountPoolTime(parsed)
	}
	return nil
}

func validAccountPoolTime(value time.Time) *time.Time {
	if value.Year() < 2000 || value.Year() > 2200 {
		return nil
	}
	return &value
}

func normalizeAccountPoolPlan(plan string) string {
	normalized := strings.ToLower(strings.TrimSpace(plan))
	compact := strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
	if compact == "prolite" {
		return "Pro 5x"
	}
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func normalizeAccountPoolQuotaWindows(primary *AccountPoolWindow, secondary *AccountPoolWindow) (*AccountPoolWindow, *AccountPoolWindow) {
	if isAccountPoolLongWindow(primary) {
		if secondary == nil {
			return nil, primary
		}
		if !isAccountPoolLongWindow(secondary) {
			return secondary, primary
		}
	}
	if primary == nil && secondary != nil && !isAccountPoolLongWindow(secondary) {
		return secondary, nil
	}
	return primary, secondary
}

func isAccountPoolLongWindow(window *AccountPoolWindow) bool {
	return window != nil && window.LimitWindowSeconds != nil && *window.LimitWindowSeconds >= int64((24*time.Hour)/time.Second)
}

func cloneAccountPoolSnapshot(snapshot *accountPoolSnapshot) *accountPoolSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := *snapshot
	copy.Accounts = make([]accountPoolAccount, len(snapshot.Accounts))
	for index, account := range snapshot.Accounts {
		copy.Accounts[index] = account
		copy.Accounts[index].SubscriptionActiveUntil = copyTimePointer(account.SubscriptionActiveUntil)
		copy.Accounts[index].PrimaryWindow = copyAccountPoolWindow(account.PrimaryWindow)
		copy.Accounts[index].SecondaryWindow = copyAccountPoolWindow(account.SecondaryWindow)
		copy.Accounts[index].WindowGroups = copyAccountPoolWindowGroups(account.WindowGroups)
	}
	return &copy
}

func copyAccountPoolWindowGroups(groups []AccountPoolWindowGroup) []AccountPoolWindowGroup {
	if groups == nil {
		return nil
	}
	copied := make([]AccountPoolWindowGroup, len(groups))
	for index, group := range groups {
		copied[index] = AccountPoolWindowGroup{
			Label:           group.Label,
			PrimaryWindow:   copyAccountPoolWindow(group.PrimaryWindow),
			SecondaryWindow: copyAccountPoolWindow(group.SecondaryWindow),
		}
	}
	return copied
}

func copyAccountPoolWindow(window *AccountPoolWindow) *AccountPoolWindow {
	if window == nil {
		return nil
	}
	copy := *window
	if window.UsedPercent != nil {
		value := *window.UsedPercent
		copy.UsedPercent = &value
	}
	if window.RemainingPercent != nil {
		value := *window.RemainingPercent
		copy.RemainingPercent = &value
	}
	copy.ResetAt = copyTimePointer(window.ResetAt)
	if window.LimitWindowSeconds != nil {
		value := *window.LimitWindowSeconds
		copy.LimitWindowSeconds = &value
	}
	return &copy
}

func copyTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
