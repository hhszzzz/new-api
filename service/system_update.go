package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	defaultSystemUpdateRepository = "hhszzzz/new-api"
	defaultSystemUpdateWorkflow   = "publish-fork-image.yml"
	defaultSystemUpdateBranch     = "main"
	defaultSystemUpdateImage      = "ghcr.io/hhszzzz/new-api:main"
	systemUpdateResponseLimit     = 1 << 20
	systemUpdateTriggerDelay      = 750 * time.Millisecond
	systemUpdateTriggerTimeout    = 10 * time.Minute
	systemUpdateStateTTL          = 15 * time.Minute
)

var (
	ErrSystemUpdateNotConfigured = errors.New("one-click system update is not configured")
	ErrSystemUpdateInProgress    = errors.New("system update is already in progress")
)

type SystemUpdateTriggerState struct {
	Status        string `json:"status"`
	TargetVersion string `json:"target_version,omitempty"`
	Error         string `json:"error,omitempty"`
	StartedAt     int64  `json:"started_at,omitempty"`
}

type SystemUpdateInfo struct {
	CurrentVersion  string                   `json:"current_version"`
	CurrentRevision string                   `json:"current_revision,omitempty"`
	LatestVersion   string                   `json:"latest_version"`
	LatestRevision  string                   `json:"latest_revision"`
	UpdateAvailable bool                     `json:"update_available"`
	UpdateEnabled   bool                     `json:"update_enabled"`
	Image           string                   `json:"image"`
	Title           string                   `json:"title,omitempty"`
	PublishedAt     string                   `json:"published_at,omitempty"`
	WorkflowURL     string                   `json:"workflow_url,omitempty"`
	Trigger         SystemUpdateTriggerState `json:"trigger"`
}

type SystemUpdateStartResult struct {
	Started bool             `json:"started"`
	Update  SystemUpdateInfo `json:"update"`
}

type systemUpdateConfig struct {
	Repository   string
	Workflow     string
	Branch       string
	Image        string
	GitHubToken  string
	TriggerURL   string
	TriggerToken string
}

type githubWorkflowRunsResponse struct {
	WorkflowRuns []githubWorkflowRun `json:"workflow_runs"`
}

type githubWorkflowRun struct {
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	UpdatedAt  string `json:"updated_at"`
	HeadCommit struct {
		Message string `json:"message"`
	} `json:"head_commit"`
}

var systemUpdateRuntime = struct {
	sync.Mutex
	state SystemUpdateTriggerState
}{}

func CheckSystemUpdate(ctx context.Context, currentVersion string) (*SystemUpdateInfo, error) {
	config, err := loadSystemUpdateConfig()
	if err != nil {
		return nil, err
	}
	return checkSystemUpdateWithClient(ctx, GetHttpClient(), config, currentVersion)
}

func GetSystemUpdateTriggerState() SystemUpdateTriggerState {
	return getSystemUpdateTriggerState()
}

func StartSystemUpdate(ctx context.Context, currentVersion string) (*SystemUpdateStartResult, error) {
	config, err := loadSystemUpdateConfig()
	if err != nil {
		return nil, err
	}
	if !config.updateEnabled() {
		return nil, ErrSystemUpdateNotConfigured
	}

	info, err := checkSystemUpdateWithClient(ctx, GetHttpClient(), config, currentVersion)
	if err != nil {
		return nil, err
	}
	if !info.UpdateAvailable {
		return &SystemUpdateStartResult{Update: *info}, nil
	}

	startedAt := common.GetTimestamp()
	systemUpdateRuntime.Lock()
	if systemUpdateStateActive(systemUpdateRuntime.state, startedAt) {
		systemUpdateRuntime.Unlock()
		return nil, ErrSystemUpdateInProgress
	}
	systemUpdateRuntime.state = SystemUpdateTriggerState{
		Status:        "triggering",
		TargetVersion: info.LatestVersion,
		StartedAt:     startedAt,
	}
	info.Trigger = systemUpdateRuntime.state
	systemUpdateRuntime.Unlock()

	gopool.Go(func() {
		timer := time.NewTimer(systemUpdateTriggerDelay)
		defer timer.Stop()
		<-timer.C

		triggerCtx, cancel := context.WithTimeout(context.Background(), systemUpdateTriggerTimeout)
		defer cancel()
		err := triggerSystemUpdateWithClient(triggerCtx, &http.Client{
			Timeout:       systemUpdateTriggerTimeout,
			CheckRedirect: rejectSystemUpdateRedirect,
		}, config)

		systemUpdateRuntime.Lock()
		defer systemUpdateRuntime.Unlock()
		if err != nil {
			systemUpdateRuntime.state.Status = "failed"
			systemUpdateRuntime.state.Error = err.Error()
			logger.LogWarn(context.Background(), fmt.Sprintf("system update trigger failed: %v", err))
			return
		}
		systemUpdateRuntime.state.Status = "requested"
		systemUpdateRuntime.state.Error = ""
	})

	return &SystemUpdateStartResult{Started: true, Update: *info}, nil
}

func loadSystemUpdateConfig() (systemUpdateConfig, error) {
	config := systemUpdateConfig{
		Repository:   strings.TrimSpace(common.GetEnvOrDefaultString("SYSTEM_UPDATE_REPOSITORY", defaultSystemUpdateRepository)),
		Workflow:     strings.TrimSpace(common.GetEnvOrDefaultString("SYSTEM_UPDATE_WORKFLOW", defaultSystemUpdateWorkflow)),
		Branch:       strings.TrimSpace(common.GetEnvOrDefaultString("SYSTEM_UPDATE_BRANCH", defaultSystemUpdateBranch)),
		Image:        strings.TrimSpace(common.GetEnvOrDefaultString("SYSTEM_UPDATE_IMAGE", defaultSystemUpdateImage)),
		GitHubToken:  strings.TrimSpace(os.Getenv("SYSTEM_UPDATE_GITHUB_TOKEN")),
		TriggerURL:   strings.TrimSpace(os.Getenv("SYSTEM_UPDATE_TRIGGER_URL")),
		TriggerToken: strings.TrimSpace(os.Getenv("SYSTEM_UPDATE_TRIGGER_TOKEN")),
	}

	repositoryParts := strings.Split(config.Repository, "/")
	if len(repositoryParts) != 2 || repositoryParts[0] == "" || repositoryParts[1] == "" {
		return systemUpdateConfig{}, errors.New("SYSTEM_UPDATE_REPOSITORY must use owner/repository format")
	}
	if config.Workflow == "" || config.Branch == "" || config.Image == "" {
		return systemUpdateConfig{}, errors.New("system update source configuration is incomplete")
	}
	return config, nil
}

func (config systemUpdateConfig) updateEnabled() bool {
	return config.TriggerURL != "" && config.TriggerToken != ""
}

func checkSystemUpdateWithClient(ctx context.Context, client *http.Client, config systemUpdateConfig, currentVersion string) (*SystemUpdateInfo, error) {
	requestURL, err := systemUpdateWorkflowRunsURL(config)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "new-api-system-updater")
	if config.GitHubToken != "" {
		request.Header.Set("Authorization", "Bearer "+config.GitHubToken)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("check GHCR workflow: %w", err)
	}
	defer CloseResponseBodyGracefully(response)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check GHCR workflow: GitHub returned HTTP %d", response.StatusCode)
	}

	payload := githubWorkflowRunsResponse{}
	if err := common.DecodeJson(io.LimitReader(response.Body, systemUpdateResponseLimit), &payload); err != nil {
		return nil, fmt.Errorf("decode GHCR workflow response: %w", err)
	}
	if len(payload.WorkflowRuns) == 0 {
		return nil, errors.New("no successful GHCR workflow run found")
	}

	latestRun := payload.WorkflowRuns[0]
	latestRevision := strings.ToLower(strings.TrimSpace(latestRun.HeadSHA))
	if latestRun.Status != "completed" || latestRun.Conclusion != "success" || len(latestRevision) < 8 {
		return nil, errors.New("latest GHCR workflow run is not a completed successful build")
	}

	currentVersion = strings.TrimSpace(currentVersion)
	latestVersion := config.Branch + "-" + latestRevision[:8]
	return &SystemUpdateInfo{
		CurrentVersion:  currentVersion,
		CurrentRevision: systemUpdateRevisionFromVersion(currentVersion, config.Branch),
		LatestVersion:   latestVersion,
		LatestRevision:  latestRevision,
		UpdateAvailable: !systemUpdateVersionMatchesRevision(currentVersion, config.Branch, latestRevision),
		UpdateEnabled:   config.updateEnabled(),
		Image:           config.Image,
		Title:           firstLine(latestRun.HeadCommit.Message),
		PublishedAt:     latestRun.UpdatedAt,
		WorkflowURL:     latestRun.HTMLURL,
		Trigger:         getSystemUpdateTriggerState(),
	}, nil
}

func systemUpdateWorkflowRunsURL(config systemUpdateConfig) (string, error) {
	repositoryParts := strings.Split(config.Repository, "/")
	if len(repositoryParts) != 2 {
		return "", errors.New("invalid system update repository")
	}
	requestURL := &url.URL{
		Scheme: "https",
		Host:   "api.github.com",
		Path: fmt.Sprintf(
			"/repos/%s/%s/actions/workflows/%s/runs",
			url.PathEscape(repositoryParts[0]),
			url.PathEscape(repositoryParts[1]),
			url.PathEscape(config.Workflow),
		),
	}
	query := requestURL.Query()
	query.Set("branch", config.Branch)
	query.Set("status", "success")
	query.Set("per_page", "1")
	requestURL.RawQuery = query.Encode()
	return requestURL.String(), nil
}

func triggerSystemUpdateWithClient(ctx context.Context, client *http.Client, config systemUpdateConfig) error {
	if !config.updateEnabled() {
		return ErrSystemUpdateNotConfigured
	}
	triggerURL, err := url.Parse(config.TriggerURL)
	if err != nil || triggerURL.Host == "" || (triggerURL.Scheme != "http" && triggerURL.Scheme != "https") {
		return errors.New("SYSTEM_UPDATE_TRIGGER_URL must be an absolute HTTP or HTTPS URL")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, triggerURL.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+config.TriggerToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "new-api-system-updater")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request update trigger: %w", err)
	}
	defer CloseResponseBodyGracefully(response)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			return fmt.Errorf("update trigger returned HTTP %d", response.StatusCode)
		}
		return fmt.Errorf("update trigger returned HTTP %d: %s", response.StatusCode, message)
	}
	return nil
}

func getSystemUpdateTriggerState() SystemUpdateTriggerState {
	now := common.GetTimestamp()
	systemUpdateRuntime.Lock()
	defer systemUpdateRuntime.Unlock()
	if systemUpdateRuntime.state.Status == "" ||
		(systemUpdateRuntime.state.StartedAt > 0 && now-systemUpdateRuntime.state.StartedAt >= int64(systemUpdateStateTTL.Seconds())) {
		systemUpdateRuntime.state = SystemUpdateTriggerState{Status: "idle"}
	}
	return systemUpdateRuntime.state
}

func systemUpdateStateActive(state SystemUpdateTriggerState, now int64) bool {
	if state.Status != "triggering" && state.Status != "requested" {
		return false
	}
	return state.StartedAt > 0 && now-state.StartedAt < int64(systemUpdateStateTTL.Seconds())
}

func systemUpdateRevisionFromVersion(version string, branch string) string {
	prefix := strings.ToLower(strings.TrimSpace(branch)) + "-"
	version = strings.ToLower(strings.TrimSpace(version))
	if !strings.HasPrefix(version, prefix) {
		return ""
	}
	revision := strings.TrimPrefix(version, prefix)
	if len(revision) < 7 || len(revision) > 40 {
		return ""
	}
	for _, char := range revision {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return revision
}

func systemUpdateVersionMatchesRevision(version string, branch string, revision string) bool {
	currentRevision := systemUpdateRevisionFromVersion(version, branch)
	return currentRevision != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(revision)), currentRevision)
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return value
}

func rejectSystemUpdateRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}
