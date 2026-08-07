package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	promptAuditContextKey          = "prompt_audit_result"
	promptAuditMaxResponseBytes    = 64 * 1024
	promptAuditFullPromptMaxRunes  = 65536
	promptAuditPreviewSourceRunes  = 96
	promptAuditLocalCacheMaxItems  = 4096
	promptAuditWorkerPollInterval  = 2 * time.Second
	promptAuditWorkerLeasePadding  = 30 * time.Second
	promptAuditRetentionBatchSize  = 500
	promptAuditRetentionCheckEvery = time.Hour

	PromptAuditDecisionPass        = "pass"
	PromptAuditDecisionFlag        = "flag"
	PromptAuditDecisionBlock       = "block"
	PromptAuditDecisionUnavailable = "unavailable"
)

type PromptAuditCategoryDefinition struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	LabelZH     string `json:"label_zh"`
	Description string `json:"description"`
}

var promptAuditCategoryCatalog = map[string]PromptAuditCategoryDefinition{
	"violent":                       {ID: "violent", Label: "Violent", LabelZH: "暴力", Description: "Violence or threats of violence"},
	"non_violent_illegal_acts":      {ID: "non_violent_illegal_acts", Label: "Non-violent Illegal Acts", LabelZH: "非暴力违法行为", Description: "Non-violent illegal activity"},
	"sexual_content_or_sexual_acts": {ID: "sexual_content_or_sexual_acts", Label: "Sexual Content or Sexual Acts", LabelZH: "性内容或性行为", Description: "Sexual content or sexual acts"},
	"pii":                           {ID: "pii", Label: "PII", LabelZH: "个人敏感信息", Description: "Personal identifying information"},
	"suicide_and_self_harm":         {ID: "suicide_and_self_harm", Label: "Suicide & Self-Harm", LabelZH: "自杀与自残", Description: "Suicide or self-harm"},
	"unethical_acts":                {ID: "unethical_acts", Label: "Unethical Acts", LabelZH: "不道德行为", Description: "Unethical behavior"},
	"politically_sensitive_topics":  {ID: "politically_sensitive_topics", Label: "Politically Sensitive Topics", LabelZH: "政治敏感话题", Description: "Politically sensitive topics"},
	"copyright_violation":           {ID: "copyright_violation", Label: "Copyright Violation", LabelZH: "版权侵权", Description: "Copyright infringement"},
	"jailbreak":                     {ID: "jailbreak", Label: "Jailbreak", LabelZH: "越狱攻击", Description: "Prompt injection or jailbreak attempt"},
}

var promptAuditCategoryAliases = map[string]string{
	"violent": "violent", "violence": "violent",
	"non violent illegal acts":      "non_violent_illegal_acts",
	"sexual content or sexual acts": "sexual_content_or_sexual_acts", "sexual": "sexual_content_or_sexual_acts",
	"pii": "pii", "personal identifying information": "pii", "personal identifiable information": "pii",
	"suicide self harm": "suicide_and_self_harm", "suicide and self harm": "suicide_and_self_harm",
	"unethical acts": "unethical_acts", "unethical": "unethical_acts",
	"politically sensitive topics": "politically_sensitive_topics", "political": "politically_sensitive_topics",
	"copyright violation": "copyright_violation", "copyright": "copyright_violation",
	"jailbreak": "jailbreak", "prompt injection": "jailbreak",
}

var (
	promptAuditBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+\-/]+=*`)
	promptAuditSecretPattern = regexp.MustCompile(`(?i)\b(sk|rk|pk|api[_-]?key|token|secret|password)[-_:=\s]+[A-Za-z0-9._~+\-/]{8,}`)
	promptAuditEmailPattern  = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	promptAuditPhonePattern  = regexp.MustCompile(`(?:\+?\d[\d\s().-]{8,}\d)`)
)

type PromptAuditRequest struct {
	Snapshot dto.PromptAuditSnapshot
	Protocol string
	Model    string
	Stage    string
	Stream   bool
}

// PromptAuditResult contains only non-secret decision metadata safe for logs,
// cache, and request context. Raw prompt and node tokens never enter this type.
type PromptAuditResult struct {
	Enabled           bool     `json:"enabled"`
	Reviewed          bool     `json:"reviewed"`
	Blocked           bool     `json:"blocked"`
	Outcome           string   `json:"outcome"`
	Mode              string   `json:"mode"`
	Safety            string   `json:"safety"`
	Decision          string   `json:"decision"`
	Categories        []string `json:"categories"`
	UnknownCategories []string `json:"unknown_categories"`
	EndpointID        string   `json:"endpoint_id"`
	LatencyMillis     int64    `json:"latency_ms"`
	InputChars        int      `json:"input_chars"`
	InputSHA256       string   `json:"input_sha256"`
	SegmentCount      int      `json:"segment_count"`
	ChunkCount        int      `json:"chunk_count"`
	ConfigVersion     string   `json:"config_version"`
	FailureKind       string   `json:"failure,omitempty"`
	CacheHit          bool     `json:"cache_hit"`
	AuditID           int64    `json:"audit_id,omitempty"`
}

type promptAuditGuardError struct {
	code       string
	retryable  bool
	timeout    bool
	httpStatus int
	cause      error
}

func (err *promptAuditGuardError) Error() string {
	if err == nil || err.code == "" {
		return "prompt audit unavailable"
	}
	return err.code
}

func (err *promptAuditGuardError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

type promptAuditCacheEntry struct {
	Result    PromptAuditResult
	ExpiresAt time.Time
	CreatedAt time.Time
}

type promptAuditResultCache struct {
	mu      sync.Mutex
	entries map[string]promptAuditCacheEntry
}

var promptAuditCache = promptAuditResultCache{entries: map[string]promptAuditCacheEntry{}}

var (
	promptAuditGlobalSlots   sync.Map
	promptAuditEndpointSlots sync.Map
	promptAuditHTTPClients   sync.Map
	promptAuditRunnerOnce    sync.Once
	promptAuditWorkerWakeup  = make(chan struct{}, 1)
)

var errPromptAuditRedirect = errors.New("prompt audit redirects are disabled")

var promptAuditNoRedirect = func(_ *http.Request, _ []*http.Request) error {
	return errPromptAuditRedirect
}

func PromptAuditCategories() []PromptAuditCategoryDefinition {
	result := make([]PromptAuditCategoryDefinition, 0, len(prompt_audit_setting.AllCategoryIDs))
	for _, category := range prompt_audit_setting.AllCategoryIDs {
		result = append(result, promptAuditCategoryCatalog[category])
	}
	return result
}

func CheckPromptAudit(c *gin.Context, request PromptAuditRequest) (PromptAuditResult, *types.NewAPIError) {
	setting := prompt_audit_setting.GetSetting()
	result := PromptAuditResult{Mode: setting.Mode, ConfigVersion: setting.ConfigVersion}
	group := effectivePromptAuditGroup(c)
	if !setting.AppliesToGroup(group) {
		AttachPromptAuditResult(c, result)
		return result, nil
	}
	result.Enabled = true

	segments := request.Snapshot.PrioritizedSegments()
	if len(segments) == 0 {
		result.Outcome = "skipped_no_text"
		AttachPromptAuditResult(c, result)
		return result, nil
	}
	texts := make([]string, 0, len(segments))
	for _, segment := range segments {
		texts = append(texts, segment.Text)
	}
	fullText := strings.Join(texts, "\n\n")
	result.InputChars = utf8.RuneCountInString(fullText)
	result.SegmentCount = len(segments)
	digest := sha256.Sum256([]byte(fullText))
	result.InputSHA256 = hex.EncodeToString(digest[:])
	chunks := splitPromptAuditRunes(fullText, minimumPromptAuditInputLimit(setting), setting.ChunkOverlap)
	result.ChunkCount = len(chunks)

	switch setting.Mode {
	case prompt_audit_setting.ModeAsyncAudit:
		result.Outcome = "queued"
		audit, err := newPromptAuditRecord(c, request, setting, result, fullText, model.PromptAuditStatusQueued)
		if err == nil {
			err = model.CreatePromptAudit(audit)
		}
		if err != nil {
			result.Outcome = "enqueue_failed"
			result.FailureKind = "queue_unavailable"
			logger.LogWarn(c, "prompt audit async enqueue failed")
		} else {
			result.AuditID = audit.ID
			notifyPromptAuditWorkers()
		}
		AttachPromptAuditResult(c, result)
		return result, nil
	case prompt_audit_setting.ModeBlocking:
		// continue below
	default:
		result.Enabled = false
		AttachPromptAuditResult(c, result)
		return result, nil
	}

	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(setting.TotalTimeoutMS)*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	evaluated, err := evaluatePromptAudit(ctx, setting, fullText, result.InputSHA256)
	result.LatencyMillis = time.Since(startedAt).Milliseconds()
	if err != nil {
		result.Outcome = PromptAuditDecisionUnavailable
		result.Decision = PromptAuditDecisionUnavailable
		result.Blocked = true
		result.FailureKind = promptAuditErrorCode(err)
		result.AuditID = persistPromptAuditDecision(c, request, setting, result, fullText, model.PromptAuditStatusFailed)
		AttachPromptAuditResult(c, result)
		logPromptAuditDecision(c, result)
		return result, types.NewErrorWithStatusCode(
			errors.New("prompt audit service is unavailable"),
			types.ErrorCodePromptAuditUnavailable,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	}

	result.Reviewed = true
	result.Safety = evaluated.Safety
	result.Decision = evaluated.Decision
	result.Outcome = evaluated.Decision
	result.Categories = append([]string(nil), evaluated.Categories...)
	result.UnknownCategories = append([]string(nil), evaluated.UnknownCategories...)
	result.EndpointID = evaluated.EndpointID
	result.ChunkCount = evaluated.ChunkCount
	result.CacheHit = evaluated.CacheHit
	result.Blocked = evaluated.Decision == PromptAuditDecisionBlock
	result.AuditID = persistPromptAuditDecision(c, request, setting, result, fullText, model.PromptAuditStatusDone)
	AttachPromptAuditResult(c, result)
	if result.Decision != PromptAuditDecisionPass {
		logPromptAuditDecision(c, result)
	}
	if !result.Blocked {
		return result, nil
	}
	return result, types.NewErrorWithStatusCode(
		errors.New("request blocked by prompt audit"),
		types.ErrorCodePromptAuditBlocked,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
	)
}

func evaluatePromptAudit(ctx context.Context, setting prompt_audit_setting.PromptAuditSetting, fullText, promptHash string) (PromptAuditResult, error) {
	result := PromptAuditResult{
		Enabled: true, Mode: setting.Mode, ConfigVersion: setting.ConfigVersion,
		InputChars: utf8.RuneCountInString(fullText), InputSHA256: promptHash,
	}
	cacheKey := promptAuditCacheKey(setting.ConfigVersion, setting.EnabledCategories, promptHash)
	if setting.CacheTTLSeconds > 0 {
		if cached, ok := getPromptAuditCache(ctx, cacheKey); ok {
			cached.CacheHit = true
			cached.ConfigVersion = setting.ConfigVersion
			cached.InputChars = result.InputChars
			cached.InputSHA256 = promptHash
			return cached, nil
		}
	}

	endpoints := enabledPromptAuditEndpoints(setting)
	if len(endpoints) == 0 {
		return result, &promptAuditGuardError{code: "configuration_invalid"}
	}
	if ctx.Err() != nil {
		return result, &promptAuditGuardError{code: "total_timeout", timeout: true, retryable: true, cause: ctx.Err()}
	}
	globalSlots := promptAuditSlots(&promptAuditGlobalSlots, "global|"+setting.ConfigVersion, setting.GlobalConcurrency)
	select {
	case globalSlots <- struct{}{}:
		defer func() { <-globalSlots }()
	default:
		return result, &promptAuditGuardError{code: "concurrency_saturated", retryable: true}
	}

	chunks := splitPromptAuditRunes(fullText, minimumPromptAuditInputLimit(setting), setting.ChunkOverlap)
	if len(chunks) == 0 {
		return result, &promptAuditGuardError{code: "empty_input"}
	}
	result.ChunkCount = len(chunks)
	result.Decision = PromptAuditDecisionPass
	result.Safety = "Safe"
	categorySet := map[string]struct{}{}
	unknownSet := map[string]struct{}{}
	for _, chunk := range chunks {
		chunkResult, err := scanPromptAuditChunk(ctx, setting, endpoints, chunk)
		if err != nil {
			return PromptAuditResult{}, err
		}
		if promptAuditDecisionSeverity(chunkResult.Decision) > promptAuditDecisionSeverity(result.Decision) {
			result.Decision = chunkResult.Decision
			result.Safety = chunkResult.Safety
			result.EndpointID = chunkResult.EndpointID
		}
		if result.EndpointID == "" {
			result.EndpointID = chunkResult.EndpointID
		}
		for _, category := range chunkResult.Categories {
			categorySet[category] = struct{}{}
		}
		for _, category := range chunkResult.UnknownCategories {
			unknownSet[category] = struct{}{}
		}
		if result.Decision == PromptAuditDecisionBlock {
			break
		}
	}
	result.Reviewed = true
	result.Blocked = result.Decision == PromptAuditDecisionBlock
	result.Outcome = result.Decision
	result.Categories = orderedPromptAuditCategories(categorySet)
	result.UnknownCategories = sortedPromptAuditKeys(unknownSet)
	if setting.CacheTTLSeconds > 0 {
		setPromptAuditCache(cacheKey, result, time.Duration(setting.CacheTTLSeconds)*time.Second)
	}
	return result, nil
}

func scanPromptAuditChunk(ctx context.Context, setting prompt_audit_setting.PromptAuditSetting, endpoints []prompt_audit_setting.Endpoint, chunk string) (PromptAuditResult, error) {
	var lastErr error
	for _, endpoint := range endpoints {
		if ctx.Err() != nil {
			return PromptAuditResult{}, &promptAuditGuardError{code: "total_timeout", retryable: true, timeout: true, cause: ctx.Err()}
		}
		limit := endpoint.Concurrency
		if limit <= 0 {
			limit = setting.EndpointConcurrency
		}
		slots := promptAuditSlots(&promptAuditEndpointSlots, setting.ConfigVersion+"|"+endpoint.ID+"|"+strconv.Itoa(limit), limit)
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return PromptAuditResult{}, &promptAuditGuardError{code: "total_timeout", retryable: true, timeout: true, cause: ctx.Err()}
		default:
			return PromptAuditResult{}, &promptAuditGuardError{code: "endpoint_concurrency_saturated", retryable: true}
		}

		endpointCtx, cancel := context.WithTimeout(ctx, time.Duration(endpoint.TimeoutMS)*time.Millisecond)
		result, err := callPromptAuditEndpoint(endpointCtx, endpoint, chunk, setting.EnabledCategories)
		cancel()
		<-slots
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return PromptAuditResult{}, &promptAuditGuardError{code: "total_timeout", retryable: true, timeout: true, cause: ctx.Err()}
		}
		lastErr = err
		var guardErr *promptAuditGuardError
		if !errors.As(err, &guardErr) || !guardErr.retryable {
			return PromptAuditResult{}, err
		}
	}
	if lastErr == nil {
		lastErr = &promptAuditGuardError{code: "no_enabled_endpoint"}
	}
	return PromptAuditResult{}, lastErr
}

func callPromptAuditEndpoint(ctx context.Context, endpoint prompt_audit_setting.Endpoint, chunk string, enabledCategories []string) (PromptAuditResult, error) {
	requestURL, err := promptAuditChatCompletionsURL(endpoint.BaseURL)
	if err != nil {
		return PromptAuditResult{}, &promptAuditGuardError{code: "configuration_invalid", cause: err}
	}
	payload := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Temperature int `json:"temperature"`
		MaxTokens   int `json:"max_tokens"`
		Seed        int `json:"seed"`
	}{Model: endpoint.Model, Temperature: 0, MaxTokens: 64, Seed: 42}
	payload.Messages = append(payload.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: chunk})
	body, err := common.Marshal(payload)
	if err != nil {
		return PromptAuditResult{}, &promptAuditGuardError{code: "request_encode_failed", cause: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return PromptAuditResult{}, &promptAuditGuardError{code: "request_create_failed", cause: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "new-api-prompt-audit")
	if endpoint.Token != "" {
		request.Header.Set("Authorization", "Bearer "+endpoint.Token)
	}

	response, err := promptAuditHTTPClient(endpoint).Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if errors.Is(err, errPromptAuditRedirect) {
			return PromptAuditResult{}, &promptAuditGuardError{code: "redirect_not_allowed", cause: err}
		}
		timedOut := errors.Is(err, context.DeadlineExceeded)
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			timedOut = true
		}
		code := "network_error"
		if timedOut {
			code = "endpoint_timeout"
		}
		return PromptAuditResult{}, &promptAuditGuardError{code: code, retryable: true, timeout: timedOut, cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError
		return PromptAuditResult{}, &promptAuditGuardError{
			code: "endpoint_http_" + strconv.Itoa(response.StatusCode), retryable: retryable, httpStatus: response.StatusCode,
		}
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, promptAuditMaxResponseBytes+1))
	if err != nil {
		return PromptAuditResult{}, &promptAuditGuardError{code: "response_read_failed", retryable: true, cause: err}
	}
	if len(responseBody) > promptAuditMaxResponseBytes {
		return PromptAuditResult{}, &promptAuditGuardError{code: "invalid_response"}
	}
	content, err := extractPromptAuditOpenAIContent(responseBody)
	if err != nil {
		return PromptAuditResult{}, &promptAuditGuardError{code: "invalid_response", cause: err}
	}
	result, err := ParseQwen3Guard(content, enabledCategories)
	if err != nil {
		return PromptAuditResult{}, err
	}
	result.EndpointID = endpoint.ID
	return result, nil
}

func ParseQwen3Guard(content string, enabledCategories []string) (PromptAuditResult, error) {
	lines := make([]string, 0, 2)
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 {
		return PromptAuditResult{}, &promptAuditGuardError{code: "invalid_response"}
	}
	if !strings.HasPrefix(strings.ToLower(lines[0]), "safety:") ||
		!strings.HasPrefix(strings.ToLower(lines[1]), "categories:") {
		return PromptAuditResult{}, &promptAuditGuardError{code: "invalid_response"}
	}
	safety := strings.TrimSpace(lines[0][len("safety:"):])
	categoryLine := strings.TrimSpace(lines[1][len("categories:"):])
	switch strings.ToLower(safety) {
	case "safe":
		safety = "Safe"
	case "controversial":
		safety = "Controversial"
	case "unsafe":
		safety = "Unsafe"
	default:
		return PromptAuditResult{}, &promptAuditGuardError{code: "invalid_response"}
	}
	enabled := make(map[string]struct{}, len(enabledCategories))
	for _, category := range enabledCategories {
		enabled[normalizePromptAuditCategory(category)] = struct{}{}
	}
	known := map[string]struct{}{}
	unknown := map[string]struct{}{}
	for _, raw := range strings.Split(categoryLine, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "n/a") {
			continue
		}
		category := normalizePromptAuditCategory(raw)
		if _, ok := promptAuditCategoryCatalog[category]; ok {
			known[category] = struct{}{}
		} else {
			unknown[promptAuditUnknownCategoryID(category)] = struct{}{}
		}
	}
	result := PromptAuditResult{
		Reviewed: true, Safety: safety, Decision: PromptAuditDecisionPass,
		Categories: orderedPromptAuditCategories(known), UnknownCategories: sortedPromptAuditKeys(unknown),
	}
	matched := make([]string, 0, len(known))
	for _, category := range result.Categories {
		if _, ok := enabled[category]; ok {
			matched = append(matched, category)
		}
	}
	if safety == "Controversial" {
		result.Decision = PromptAuditDecisionFlag
		for _, category := range matched {
			if category == "jailbreak" || category == "pii" || category == "suicide_and_self_harm" {
				result.Decision = PromptAuditDecisionBlock
				break
			}
		}
	}
	if safety == "Unsafe" {
		if len(matched) > 0 || len(result.UnknownCategories) > 0 || len(result.Categories) == 0 {
			result.Decision = PromptAuditDecisionBlock
		} else {
			result.Decision = PromptAuditDecisionFlag
		}
	}
	result.Blocked = result.Decision == PromptAuditDecisionBlock
	result.Outcome = result.Decision
	return result, nil
}

func extractPromptAuditOpenAIContent(body []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := common.Unmarshal(body, &response); err != nil || len(response.Choices) == 0 {
		return "", errors.New("prompt audit response envelope is invalid")
	}
	switch content := response.Choices[0].Message.Content.(type) {
	case string:
		if strings.TrimSpace(content) == "" {
			return "", errors.New("prompt audit response content is empty")
		}
		return content, nil
	case []any:
		parts := make([]string, 0, len(content))
		for _, value := range content {
			object, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := object["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n"), nil
		}
	}
	return "", errors.New("prompt audit response content is invalid")
}

func promptAuditChatCompletionsURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("invalid prompt audit base URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid prompt audit base URL")
	}
	path := strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/v1/chat/completions", "/chat/completions"} {
		if strings.HasSuffix(strings.ToLower(path), suffix) {
			path = path[:len(path)-len(suffix)]
			break
		}
	}
	if path == "" {
		path = "/v1"
	} else if !strings.HasSuffix(strings.ToLower(path), "/v1") {
		path += "/v1"
	}
	parsed.Path = strings.TrimRight(path, "/") + "/chat/completions"
	parsed.RawPath = ""
	return parsed.String(), nil
}

func promptAuditHTTPClient(endpoint prompt_audit_setting.Endpoint) *http.Client {
	key := endpoint.ID + "|" + endpoint.BaseURL
	if cached, ok := promptAuditHTTPClients.Load(key); ok {
		return cached.(*http.Client)
	}
	transport := http.DefaultTransport
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = defaultTransport.Clone()
	}
	client := &http.Client{Transport: transport, CheckRedirect: promptAuditNoRedirect}
	actual, _ := promptAuditHTTPClients.LoadOrStore(key, client)
	return actual.(*http.Client)
}

func splitPromptAuditRunes(value string, limit, overlap int) []string {
	if limit <= 0 {
		return nil
	}
	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= limit {
		overlap = limit - 1
	}
	step := limit - overlap
	chunks := make([]string, 0, (len(runes)+step-1)/step)
	for start := 0; start < len(runes); start += step {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}

func minimumPromptAuditInputLimit(setting prompt_audit_setting.PromptAuditSetting) int {
	minimum := 0
	for _, endpoint := range setting.Endpoints {
		if !endpoint.Enabled {
			continue
		}
		if minimum == 0 || endpoint.InputLimit < minimum {
			minimum = endpoint.InputLimit
		}
	}
	if minimum == 0 {
		minimum = prompt_audit_setting.DefaultEndpointInputLimit
	}
	return minimum
}

func enabledPromptAuditEndpoints(setting prompt_audit_setting.PromptAuditSetting) []prompt_audit_setting.Endpoint {
	result := make([]prompt_audit_setting.Endpoint, 0, len(setting.Endpoints))
	for _, endpoint := range setting.Endpoints {
		if endpoint.Enabled {
			result = append(result, endpoint)
		}
	}
	return result
}

func promptAuditSlots(registry *sync.Map, key string, capacity int) chan struct{} {
	if capacity < 1 {
		capacity = 1
	}
	actual, _ := registry.LoadOrStore(key, make(chan struct{}, capacity))
	return actual.(chan struct{})
}

func normalizePromptAuditCategory(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("_", " ", "&", " and ", "/", " ", "-", " ", "–", " ", "—", " ").Replace(normalized)
	normalized = strings.Join(strings.Fields(normalized), " ")
	if canonical, ok := promptAuditCategoryAliases[normalized]; ok {
		return canonical
	}
	return strings.ReplaceAll(normalized, " ", "_")
}

func promptAuditUnknownCategoryID(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(value))))
	return "unknown:" + hex.EncodeToString(digest[:8])
}

func orderedPromptAuditCategories(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	remaining := make(map[string]struct{}, len(values))
	for key := range values {
		remaining[key] = struct{}{}
	}
	for _, category := range prompt_audit_setting.AllCategoryIDs {
		if _, ok := remaining[category]; ok {
			result = append(result, category)
			delete(remaining, category)
		}
	}
	return append(result, sortedPromptAuditKeys(remaining)...)
}

func sortedPromptAuditKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func promptAuditDecisionSeverity(decision string) int {
	switch decision {
	case PromptAuditDecisionBlock:
		return 3
	case PromptAuditDecisionFlag:
		return 2
	default:
		return 1
	}
}

func promptAuditCacheKey(configVersion string, enabledCategories []string, promptHash string) string {
	categories := append([]string(nil), enabledCategories...)
	sort.Strings(categories)
	digest := sha256.Sum256([]byte(configVersion + "|" + strings.Join(categories, ",") + "|" + promptHash))
	return "new-api:prompt-audit:result:" + hex.EncodeToString(digest[:])
}

func getPromptAuditCache(ctx context.Context, key string) (PromptAuditResult, bool) {
	if common.RedisEnabled && common.RDB != nil {
		redisCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		value, err := common.RDB.Get(redisCtx, key).Result()
		cancel()
		if err == nil {
			var result PromptAuditResult
			if common.UnmarshalJsonStr(value, &result) == nil {
				return clonePromptAuditResult(result), true
			}
		}
	}
	now := time.Now()
	promptAuditCache.mu.Lock()
	defer promptAuditCache.mu.Unlock()
	entry, ok := promptAuditCache.entries[key]
	if !ok || !entry.ExpiresAt.After(now) {
		if ok {
			delete(promptAuditCache.entries, key)
		}
		return PromptAuditResult{}, false
	}
	return clonePromptAuditResult(entry.Result), true
}

func setPromptAuditCache(key string, result PromptAuditResult, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	cacheResult := clonePromptAuditResult(result)
	cacheResult.CacheHit = false
	if common.RedisEnabled && common.RDB != nil {
		if data, err := common.Marshal(cacheResult); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_ = common.RDB.Set(ctx, key, string(data), ttl).Err()
			cancel()
		}
	}
	now := time.Now()
	promptAuditCache.mu.Lock()
	defer promptAuditCache.mu.Unlock()
	if len(promptAuditCache.entries) >= promptAuditLocalCacheMaxItems {
		var oldestKey string
		var oldestTime time.Time
		for itemKey, entry := range promptAuditCache.entries {
			if !entry.ExpiresAt.After(now) {
				delete(promptAuditCache.entries, itemKey)
				continue
			}
			if oldestKey == "" || entry.CreatedAt.Before(oldestTime) {
				oldestKey = itemKey
				oldestTime = entry.CreatedAt
			}
		}
		if len(promptAuditCache.entries) >= promptAuditLocalCacheMaxItems && oldestKey != "" {
			delete(promptAuditCache.entries, oldestKey)
		}
	}
	promptAuditCache.entries[key] = promptAuditCacheEntry{Result: cacheResult, ExpiresAt: now.Add(ttl), CreatedAt: now}
}

func clonePromptAuditResult(result PromptAuditResult) PromptAuditResult {
	result.Categories = append([]string(nil), result.Categories...)
	result.UnknownCategories = append([]string(nil), result.UnknownCategories...)
	return result
}

func newPromptAuditRecord(c *gin.Context, request PromptAuditRequest, setting prompt_audit_setting.PromptAuditSetting, result PromptAuditResult, fullText string, status model.PromptAuditStatus) (*model.PromptAudit, error) {
	fullPrompt, truncated := promptAuditStoredFullPrompt(fullText)
	policyCategories, err := common.Marshal(setting.EnabledCategories)
	if err != nil {
		return nil, err
	}
	audit := &model.PromptAudit{
		RequestID: resultRequestID(c), UserID: contextInt(c, "id"), TokenID: contextInt(c, "token_id"),
		TokenName: contextString(c, "token_name"), GroupName: effectivePromptAuditGroup(c),
		Protocol: strings.TrimSpace(request.Protocol), ModelName: strings.TrimSpace(request.Model),
		Stage: normalizedPromptAuditStage(request.Stage), ConfigVersion: setting.ConfigVersion,
		ExecutionMode: setting.Mode, Status: status, PromptHash: result.InputSHA256,
		PromptLength: result.InputChars, SegmentCount: result.SegmentCount, ChunkCount: result.ChunkCount,
		FullPrompt: fullPrompt, FullPromptTruncated: truncated,
		RedactedPreview: promptAuditPreview(fullText), PolicyCategories: string(policyCategories),
		MaxAttempts: setting.MaxAttempts,
	}
	if status == model.PromptAuditStatusQueued {
		audit.ScanPayload = []byte(fullText)
		audit.WouldAction = "pending"
	}
	return audit, nil
}

func persistPromptAuditDecision(c *gin.Context, request PromptAuditRequest, setting prompt_audit_setting.PromptAuditSetting, result PromptAuditResult, fullText string, status model.PromptAuditStatus) int64 {
	audit, err := newPromptAuditRecord(c, request, setting, result, fullText, status)
	if err != nil {
		logger.LogWarn(c, "prompt audit event encoding failed")
		return 0
	}
	audit.Safety = result.Safety
	audit.Decision = result.Decision
	audit.WouldAction = result.Decision
	audit.EndpointID = result.EndpointID
	audit.LatencyMS = result.LatencyMillis
	audit.Attempts = 1
	audit.MaxAttempts = 1
	audit.ErrorCode = result.FailureKind
	if status == model.PromptAuditStatusDone || status == model.PromptAuditStatusFailed {
		audit.CompletedAt = common.GetTimestamp()
	}
	if data, marshalErr := common.Marshal(result.Categories); marshalErr == nil {
		audit.Categories = string(data)
	}
	if data, marshalErr := common.Marshal(result.UnknownCategories); marshalErr == nil {
		audit.UnknownCategories = string(data)
	}
	if err := model.CreatePromptAudit(audit); err != nil {
		logger.LogWarn(c, "prompt audit event persistence failed")
		return 0
	}
	return audit.ID
}

func promptAuditStoredFullPrompt(value string) ([]byte, bool) {
	runes := []rune(value)
	if len(runes) <= promptAuditFullPromptMaxRunes {
		return []byte(value), false
	}
	return []byte(string(runes[:promptAuditFullPromptMaxRunes])), true
}

func promptAuditPreview(value string) string {
	value = promptAuditBearerPattern.ReplaceAllString(value, "Bearer ***")
	value = promptAuditSecretPattern.ReplaceAllStringFunc(value, func(match string) string {
		if index := strings.IndexAny(match, ":= \t"); index >= 0 {
			return match[:index+1] + "***"
		}
		return "***"
	})
	value = promptAuditEmailPattern.ReplaceAllString(value, "***@***")
	value = promptAuditPhonePattern.ReplaceAllString(value, "***PHONE***")
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 {
		return ""
	}
	truncated := len(runes) > promptAuditPreviewSourceRunes
	if truncated {
		runes = runes[:promptAuditPreviewSourceRunes]
	}
	if len(runes) < 32 {
		if truncated {
			return "***…"
		}
		return "***"
	}
	keep := len(runes) / 4
	if keep > 24 {
		keep = 24
	}
	preview := string(runes[:keep]) + "***"
	if truncated || keep < len(runes) {
		preview += "…"
	}
	return preview
}

func normalizedPromptAuditStage(stage string) string {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return "http"
	}
	return stage
}

func resultRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(common.RequestIdKey)
}

func contextString(c *gin.Context, key string) string {
	if c == nil {
		return ""
	}
	return c.GetString(key)
}

func effectivePromptAuditGroup(c *gin.Context) string {
	if c == nil {
		return ""
	}
	for _, key := range []constant.ContextKey{
		constant.ContextKeyUsingGroup,
		constant.ContextKeyTokenGroup,
		constant.ContextKeyUserGroup,
	} {
		if group := strings.TrimSpace(common.GetContextKeyString(c, key)); group != "" {
			return group
		}
	}
	return ""
}

func contextInt(c *gin.Context, key string) int {
	if c == nil {
		return 0
	}
	return c.GetInt(key)
}

func promptAuditErrorCode(err error) string {
	var guardErr *promptAuditGuardError
	if errors.As(err, &guardErr) && guardErr.code != "" {
		return guardErr.code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "total_timeout"
	}
	return "unavailable"
}

func logPromptAuditDecision(c *gin.Context, result PromptAuditResult) {
	logger.LogWarn(c, fmt.Sprintf(
		"prompt audit: outcome=%s blocked=%t mode=%s endpoint=%s latency_ms=%d input_chars=%d input_sha256=%s failure=%s cache_hit=%t",
		result.Outcome, result.Blocked, result.Mode, result.EndpointID, result.LatencyMillis,
		result.InputChars, result.InputSHA256, result.FailureKind, result.CacheHit,
	))
}

func AttachPromptAuditResult(c *gin.Context, result PromptAuditResult) {
	if c != nil {
		c.Set(promptAuditContextKey, result)
	}
}

func promptAuditResultFromContext(c *gin.Context) (PromptAuditResult, bool) {
	if c == nil {
		return PromptAuditResult{}, false
	}
	value, exists := c.Get(promptAuditContextKey)
	if !exists {
		return PromptAuditResult{}, false
	}
	result, ok := value.(PromptAuditResult)
	return result, ok
}

func (result PromptAuditResult) auditMap() map[string]interface{} {
	audit := map[string]interface{}{
		"outcome": result.Outcome, "mode": result.Mode, "safety": result.Safety,
		"decision": result.Decision, "reviewed": result.Reviewed, "blocked": result.Blocked,
		"latency_ms": result.LatencyMillis, "input_chars": result.InputChars,
		"input_sha256": result.InputSHA256, "segment_count": result.SegmentCount,
		"chunk_count": result.ChunkCount, "config_version": result.ConfigVersion,
		"cache_hit": result.CacheHit,
	}
	if result.EndpointID != "" {
		audit["endpoint_id"] = result.EndpointID
	}
	if len(result.Categories) > 0 {
		audit["categories"] = append([]string(nil), result.Categories...)
	}
	if len(result.UnknownCategories) > 0 {
		audit["unknown_categories"] = append([]string(nil), result.UnknownCategories...)
	}
	if result.FailureKind != "" {
		audit["failure"] = result.FailureKind
	}
	if result.AuditID > 0 {
		audit["audit_id"] = result.AuditID
	}
	return audit
}

func AppendPromptAuditAdminInfo(c *gin.Context, other map[string]interface{}) {
	if other == nil {
		return
	}
	result, ok := promptAuditResultFromContext(c)
	if !ok || !result.Enabled || result.Outcome == "" {
		return
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = map[string]interface{}{}
		other["admin_info"] = adminInfo
	}
	adminInfo["prompt_audit"] = result.auditMap()
}

func RecordPromptAuditError(c *gin.Context, result PromptAuditResult, apiErr *types.NewAPIError, modelName string, isStream bool) {
	if c == nil || apiErr == nil || !constant.ErrorLogEnabled || !types.IsRecordErrorLog(apiErr) {
		return
	}
	other := map[string]interface{}{
		"error_type": apiErr.GetErrorType(), "error_code": apiErr.GetErrorCode(), "status_code": apiErr.StatusCode,
	}
	AttachPromptAuditResult(c, result)
	AppendPromptAuditAdminInfo(c, other)
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	model.RecordErrorLog(c, c.GetInt("id"), 0, modelName, c.GetString("token_name"),
		apiErr.MaskSensitiveErrorWithStatusCode(), c.GetInt("token_id"), int(time.Since(startTime).Seconds()),
		isStream, c.GetString("group"), other)
}

func TestPromptAuditEndpoint(ctx context.Context, endpoint prompt_audit_setting.Endpoint) (PromptAuditResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(endpoint.TimeoutMS)*time.Millisecond)
	defer cancel()
	return callPromptAuditEndpoint(ctx, endpoint, "Hello", prompt_audit_setting.AllCategoryIDs)
}

func StartPromptAuditRunner() {
	promptAuditRunnerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(runPromptAuditSupervisor)
	})
}

func notifyPromptAuditWorkers() {
	select {
	case promptAuditWorkerWakeup <- struct{}{}:
	default:
	}
}

func NotifyPromptAuditWorkers() {
	notifyPromptAuditWorkers()
}

func runPromptAuditSupervisor() {
	runnerPrefix := common.NodeName + "-prompt-audit-" + common.GetRandomString(8)
	var workerCancels []context.CancelFunc
	reconcileTicker := time.NewTicker(2 * time.Second)
	retentionTicker := time.NewTicker(promptAuditRetentionCheckEvery)
	defer reconcileTicker.Stop()
	defer retentionTicker.Stop()
	reconcile := func() {
		wanted := prompt_audit_setting.GetSetting().WorkerCount
		if wanted < 1 {
			wanted = prompt_audit_setting.DefaultWorkerCount
		}
		for len(workerCancels) < wanted {
			index := len(workerCancels)
			ctx, cancel := context.WithCancel(context.Background())
			workerCancels = append(workerCancels, cancel)
			workerID := runnerPrefix + "-" + strconv.Itoa(index+1)
			gopool.Go(func() { runPromptAuditWorker(ctx, workerID) })
		}
		for len(workerCancels) > wanted {
			last := len(workerCancels) - 1
			workerCancels[last]()
			workerCancels = workerCancels[:last]
		}
	}
	reconcile()
	runPromptAuditRetentionCleanup()
	for {
		select {
		case <-reconcileTicker.C:
			reconcile()
		case <-retentionTicker.C:
			runPromptAuditRetentionCleanup()
		}
	}
}

func runPromptAuditWorker(ctx context.Context, workerID string) {
	ticker := time.NewTicker(promptAuditWorkerPollInterval)
	defer ticker.Stop()
	for {
		processed := processNextPromptAudit(ctx, workerID)
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-promptAuditWorkerWakeup:
		}
	}
}

func processNextPromptAudit(ctx context.Context, workerID string) bool {
	if ctx.Err() != nil {
		return false
	}
	setting := prompt_audit_setting.GetSetting()
	now := common.GetTimestamp()
	leaseDuration := time.Duration(setting.TotalTimeoutMS)*time.Millisecond + promptAuditWorkerLeasePadding
	if leaseDuration < time.Minute {
		leaseDuration = time.Minute
	}
	audit, claimed, err := model.ClaimPromptAudit(workerID, now, now+int64(leaseDuration.Seconds()))
	if err != nil {
		logger.LogWarn(context.Background(), "prompt audit worker claim failed")
		return false
	}
	if !claimed || audit == nil {
		return false
	}
	if len(audit.ScanPayload) == 0 {
		_ = model.FailPromptAudit(audit.ID, workerID, "payload_missing", 0, true)
		return true
	}

	var policyCategories []string
	if err := common.UnmarshalJsonStr(audit.PolicyCategories, &policyCategories); err != nil {
		_ = model.FailPromptAudit(audit.ID, workerID, "policy_invalid", 0, true)
		return true
	}
	setting.Mode = prompt_audit_setting.ModeAsyncAudit
	setting.EnabledCategories = policyCategories
	workCtx, cancel := context.WithTimeout(ctx, time.Duration(setting.TotalTimeoutMS)*time.Millisecond)
	startedAt := time.Now()
	result, scanErr := evaluatePromptAudit(workCtx, setting, string(audit.ScanPayload), audit.PromptHash)
	cancel()
	latency := time.Since(startedAt).Milliseconds()
	if scanErr != nil {
		var guardErr *promptAuditGuardError
		retryable := errors.As(scanErr, &guardErr) && guardErr.retryable
		terminal := !retryable || audit.Attempts >= audit.MaxAttempts
		retryAt := int64(0)
		if !terminal {
			retryAt = common.GetTimestamp() + int64(promptAuditRetryDelay(audit.Attempts).Seconds())
		}
		if err := model.FailPromptAudit(audit.ID, workerID, promptAuditErrorCode(scanErr), retryAt, terminal); err != nil {
			logger.LogWarn(context.Background(), "prompt audit worker failed to persist retry state")
		}
		return true
	}
	completion := model.PromptAuditCompletion{
		Safety: result.Safety, Decision: result.Decision, WouldAction: result.Decision,
		Categories: result.Categories, UnknownCategories: result.UnknownCategories,
		EndpointID: result.EndpointID, ChunkCount: result.ChunkCount, LatencyMS: latency,
	}
	if err := model.FinishPromptAudit(audit.ID, workerID, completion); err != nil {
		logger.LogWarn(context.Background(), "prompt audit worker failed to persist completion")
	}
	return true
}

func promptAuditRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 5 * time.Second
	case 2:
		return 30 * time.Second
	default:
		return 2 * time.Minute
	}
}

func runPromptAuditRetentionCleanup() {
	setting := prompt_audit_setting.GetSetting()
	if setting.RetentionDays == 0 {
		return
	}
	cutoff := common.GetTimestamp() - int64((time.Duration(setting.RetentionDays) * 24 * time.Hour).Seconds())
	for {
		purged, err := model.CleanupPromptAuditPromptsBefore(cutoff, promptAuditRetentionBatchSize)
		if err != nil {
			logger.LogWarn(context.Background(), "prompt audit retention cleanup failed")
			return
		}
		if purged < promptAuditRetentionBatchSize {
			return
		}
	}
}
