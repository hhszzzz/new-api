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

	accountPoolPerRequestTimeout = 12 * time.Second
	accountPoolRoundTimeout      = 20 * time.Second
	accountPoolMaxConcurrency    = 4
	accountPoolMaxResponseBytes  = 8 << 20
	accountPoolMaxResetAfter     = 366 * 24 * 60 * 60
)

var (
	ErrAccountPoolNotConfigured = errors.New("account pool management connection is not configured")
	ErrAccountPoolUnavailable   = errors.New("account pool quota data is unavailable")
)

type AccountPoolWindow struct {
	UsedPercent        *float64   `json:"used_percent"`
	RemainingPercent   *float64   `json:"remaining_percent"`
	ResetAt            *time.Time `json:"reset_at"`
	LimitWindowSeconds *int64     `json:"limit_window_seconds"`
}

type AccountPoolViewAccount struct {
	PublicID                string             `json:"public_id"`
	DisplayName             string             `json:"display_name"`
	Email                   *string            `json:"email,omitempty"`
	Status                  string             `json:"status"`
	Plan                    string             `json:"plan"`
	SubscriptionActiveUntil *time.Time         `json:"subscription_active_until"`
	PrimaryWindow           *AccountPoolWindow `json:"primary_window"`
	SecondaryWindow         *AccountPoolWindow `json:"secondary_window"`
	UpdatedAt               time.Time          `json:"updated_at"`
	Stale                   bool               `json:"stale"`
}

type AccountPoolSummary struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Error     int `json:"error"`
}

type AccountPoolView struct {
	UpdatedAt                time.Time                `json:"updated_at"`
	NextRefreshAt            time.Time                `json:"next_refresh_at"`
	ManualRefreshAvailableAt time.Time                `json:"manual_refresh_available_at"`
	Stale                    bool                     `json:"stale"`
	Partial                  bool                     `json:"partial"`
	Summary                  AccountPoolSummary       `json:"summary"`
	Accounts                 []AccountPoolViewAccount `json:"accounts"`
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
	DisplayName             string
	Email                   string
	Status                  string
	Plan                    string
	SubscriptionActiveUntil *time.Time
	PrimaryWindow           *AccountPoolWindow
	SecondaryWindow         *AccountPoolWindow
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

func GetAccountPool(ctx context.Context, includeEmail bool) (AccountPoolView, error) {
	snapshot, err := defaultAccountPoolManager.get(ctx)
	if err != nil {
		return AccountPoolView{}, err
	}
	return defaultAccountPoolManager.buildView(snapshot, includeEmail), nil
}

func RefreshAccountPool(ctx context.Context, includeEmail bool) (AccountPoolView, error) {
	snapshot, err := defaultAccountPoolManager.refreshManually(ctx)
	if err != nil {
		return AccountPoolView{}, err
	}
	return defaultAccountPoolManager.buildView(snapshot, includeEmail), nil
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
	for index := range result.accounts {
		result.accounts[index].DisplayName = fmt.Sprintf("Codex #%d", index+1)
	}

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
	fallback.NextRefreshAt = now.Add(time.Duration(retrySeconds) * time.Second)
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

func (manager *accountPoolManager) buildView(snapshot *accountPoolSnapshot, includeEmail bool) AccountPoolView {
	now := manager.now()
	manager.mu.RLock()
	manualAvailableAt := manager.manualRefreshAvailableAt
	manager.mu.RUnlock()
	if manualAvailableAt.IsZero() || manualAvailableAt.Before(now) {
		manualAvailableAt = now
	}

	view := AccountPoolView{
		UpdatedAt:                snapshot.UpdatedAt,
		NextRefreshAt:            snapshot.NextRefreshAt,
		ManualRefreshAvailableAt: manualAvailableAt,
		Stale:                    snapshot.Stale,
		Partial:                  snapshot.Partial,
		Summary:                  snapshot.Summary,
		Accounts:                 make([]AccountPoolViewAccount, 0, len(snapshot.Accounts)),
	}
	for _, account := range snapshot.Accounts {
		item := AccountPoolViewAccount{
			PublicID:                account.PublicID,
			DisplayName:             account.DisplayName,
			Status:                  account.Status,
			Plan:                    account.Plan,
			SubscriptionActiveUntil: copyTimePointer(account.SubscriptionActiveUntil),
			PrimaryWindow:           copyAccountPoolWindow(account.PrimaryWindow),
			SecondaryWindow:         copyAccountPoolWindow(account.SecondaryWindow),
			UpdatedAt:               account.UpdatedAt,
			Stale:                   account.Stale,
		}
		if includeEmail && strings.TrimSpace(account.Email) != "" {
			email := account.Email
			item.Email = &email
		}
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
		ready     bool
	}
	prepared := make([]preparedAccount, 0, len(files))
	preparedByID := make(map[string]int, len(files))
	for _, file := range files {
		account, authIndex, accountID, ready := buildAccountPoolAccount(file, config.key, now)
		if existingIndex, exists := preparedByID[account.PublicID]; exists {
			existing := &prepared[existingIndex]
			if !existing.ready && ready {
				*existing = preparedAccount{account: account, authIndex: authIndex, accountID: accountID, ready: true}
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
			account: account, authIndex: authIndex, accountID: accountID, ready: ready,
		})
	}
	type pendingAccount struct {
		index     int
		authIndex string
		accountID string
	}
	pending := make([]pendingAccount, 0, len(prepared))
	for _, candidate := range prepared {
		result.accounts = append(result.accounts, candidate.account)
		if candidate.ready {
			pending = append(pending, pendingAccount{
				index:     len(result.accounts) - 1,
				authIndex: candidate.authIndex,
				accountID: candidate.accountID,
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
			payload, requestErr := manager.fetchCodexUsage(requestContext, config, account.authIndex, account.accountID)
			requestCancel()
			resultChannel <- quotaResult{index: account.index, payload: payload, err: requestErr}
		}()
	}
	waitGroup.Wait()
	close(resultChannel)

	for quota := range resultChannel {
		if quota.err != nil {
			result.failed++
			result.accounts[quota.index].Status = "error"
			result.accounts[quota.index].Stale = true
			continue
		}
		applyCodexUsagePayload(&result.accounts[quota.index], quota.payload, now)
		result.accounts[quota.index].Status = "available"
		result.accounts[quota.index].UpdatedAt = now
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
		if isCodexAuthFile(file) {
			files = append(files, file)
		}
	}
	return files, nil
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
	body, err := common.Marshal(map[string]interface{}{
		"auth_index": authIndex,
		"method":     http.MethodGet,
		"url":        accountPoolCodexUsageURL,
		"header":     headers,
	})
	if err != nil {
		return nil, err
	}
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
	if apiResponse.StatusCode < http.StatusOK || apiResponse.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrAccountPoolUnavailable
	}
	return normalizeAccountPoolPayload(apiResponse.Body)
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

func isCodexAuthFile(file map[string]interface{}) bool {
	provider := strings.ToLower(firstAccountPoolString(file, "type", "provider"))
	if provider == "codex" {
		return true
	}
	return strings.ToLower(firstAccountPoolString(file, "provider", "type")) == "codex"
}

func buildAccountPoolAccount(file map[string]interface{}, idSecret string, now time.Time) (accountPoolAccount, string, string, bool) {
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
		PublicID:                accountPoolPublicID(idSecret, name, authIndex),
		Email:                   findAccountPoolString(file, "email"),
		Status:                  "available",
		Plan:                    normalizeAccountPoolPlan(plan),
		SubscriptionActiveUntil: subscriptionActiveUntil,
		UpdatedAt:               now,
	}
	if firstAccountPoolBool(file, "disabled") {
		account.Status = "disabled"
		return account, "", "", false
	}
	if firstAccountPoolBool(file, "unavailable") {
		account.Status = "unavailable"
		return account, "", "", false
	}
	status := strings.ToLower(firstAccountPoolString(file, "status"))
	if status == "disabled" || status == "error" || status == "unavailable" {
		account.Status = status
		return account, "", "", false
	}
	if authIndex == "" {
		account.Status = "error"
		return account, "", "", false
	}
	accountID := findAccountPoolString(file, "chatgpt_account_id", "chatgptAccountId")
	if accountID == "" {
		accountID = firstAccountPoolString(authInfo, "chatgpt_account_id", "chatgptAccountId")
	}
	return account, authIndex, accountID, true
}

func applyCodexUsagePayload(account *accountPoolAccount, payload map[string]interface{}, now time.Time) {
	plan := firstAccountPoolString(payload, "plan_type", "planType")
	if plan != "" {
		account.Plan = normalizeAccountPoolPlan(plan)
	}
	rateLimit := firstAccountPoolMap(payload, "rate_limit", "rateLimit")
	if rateLimit == nil {
		return
	}
	primary := firstAccountPoolMap(rateLimit, "primary_window", "primaryWindow")
	secondary := firstAccountPoolMap(rateLimit, "secondary_window", "secondaryWindow")
	primaryWindow := parseAccountPoolWindow(primary, rateLimit, now)
	secondaryWindow := parseAccountPoolWindow(secondary, rateLimit, now)
	primaryWindow, secondaryWindow = normalizeAccountPoolQuotaWindows(primaryWindow, secondaryWindow)
	account.PrimaryWindow = primaryWindow
	account.SecondaryWindow = secondaryWindow
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
		for _, window := range []*AccountPoolWindow{account.PrimaryWindow, account.SecondaryWindow} {
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

	next := now.Add(interval)
	if !earliestDue.IsZero() && earliestDue.After(now) && earliestDue.Before(next) {
		next = earliestDue
	}
	if overdue {
		next = now.Add(nearInterval)
	}
	return next, overdue
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
		accounts[index].UpdatedAt = old.UpdatedAt
		accounts[index].Stale = true
	}
}

func sortAccountPoolAccounts(accounts []accountPoolAccount) {
	sort.SliceStable(accounts, func(left int, right int) bool {
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

func summarizeAccountPoolAccounts(accounts []accountPoolAccount) AccountPoolSummary {
	summary := AccountPoolSummary{Total: len(accounts)}
	for _, account := range accounts {
		if account.Status == "available" {
			summary.Available++
		} else {
			summary.Error++
		}
	}
	return summary
}

func accountPoolPublicID(secret string, name string, authIndex string) string {
	digest := hmac.New(sha256.New, []byte(secret))
	_, _ = digest.Write([]byte(strings.TrimSpace(name) + "\x00" + authIndex))
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
	}
	return &copy
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
