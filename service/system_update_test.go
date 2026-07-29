package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type systemUpdateRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip systemUpdateRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestCheckSystemUpdateUsesLatestSuccessfulGHCRWorkflow(t *testing.T) {
	config := systemUpdateConfig{
		Repository:   "hhszzzz/new-api",
		Workflow:     "publish-fork-image.yml",
		Branch:       "main",
		Image:        "ghcr.io/hhszzzz/new-api:main",
		GitHubToken:  "github-token",
		TriggerURL:   "http://new-api-updater:8080/v1/update",
		TriggerToken: "trigger-token",
	}
	client := &http.Client{Transport: systemUpdateRoundTripper(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, "/repos/hhszzzz/new-api/actions/workflows/publish-fork-image.yml/runs", request.URL.Path)
		assert.Equal(t, "main", request.URL.Query().Get("branch"))
		assert.Equal(t, "success", request.URL.Query().Get("status"))
		assert.Equal(t, "1", request.URL.Query().Get("per_page"))
		assert.Equal(t, "Bearer github-token", request.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"workflow_runs": [{
					"head_sha": "9de2eea0ab7d1708e3708bb8eadc89bafa2744b7",
					"status": "completed",
					"conclusion": "success",
					"html_url": "https://github.com/hhszzzz/new-api/actions/runs/123",
					"updated_at": "2026-07-28T10:39:04Z",
					"head_commit": {"message": "Publish new image\n\nDetails"}
				}]
			}`)),
		}, nil
	})}

	info, err := checkSystemUpdateWithClient(context.Background(), client, config, "main-deadbeef")
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "main-9de2eea0", info.LatestVersion)
	assert.Equal(t, "9de2eea0ab7d1708e3708bb8eadc89bafa2744b7", info.LatestRevision)
	assert.Equal(t, "Publish new image", info.Title)
	assert.Equal(t, "2026-07-28T10:39:04Z", info.PublishedAt)
	assert.True(t, info.UpdateAvailable)
	assert.True(t, info.UpdateEnabled)
	assert.Equal(t, "idle", info.Trigger.Status)
}

func TestCheckSystemUpdateRecognizesRunningWorkflowRevision(t *testing.T) {
	config := systemUpdateConfig{
		Repository: "hhszzzz/new-api",
		Workflow:   "publish-fork-image.yml",
		Branch:     "main",
		Image:      "ghcr.io/hhszzzz/new-api:main",
	}
	client := &http.Client{Transport: systemUpdateRoundTripper(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"workflow_runs": [{
					"head_sha": "9de2eea0ab7d1708e3708bb8eadc89bafa2744b7",
					"status": "completed",
					"conclusion": "success",
					"head_commit": {"message": "Publish new image"}
				}]
			}`)),
		}, nil
	})}

	info, err := checkSystemUpdateWithClient(context.Background(), client, config, "main-9de2eea0")
	require.NoError(t, err)
	assert.False(t, info.UpdateAvailable)
	assert.Equal(t, "9de2eea0", info.CurrentRevision)
	assert.False(t, info.UpdateEnabled)
}

func TestTriggerSystemUpdateUsesAuthenticatedWebhook(t *testing.T) {
	config := systemUpdateConfig{
		TriggerURL:   "http://new-api-updater:8080/v1/update?image=ghcr.io/hhszzzz/new-api",
		TriggerToken: "trigger-token",
	}
	client := &http.Client{Transport: systemUpdateRoundTripper(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "new-api-updater:8080", request.URL.Host)
		assert.Equal(t, "ghcr.io/hhszzzz/new-api", request.URL.Query().Get("image"))
		assert.Equal(t, "Bearer trigger-token", request.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"accepted"}`)),
		}, nil
	})}

	err := triggerSystemUpdateWithClient(context.Background(), client, config)
	require.NoError(t, err)
}

func TestSystemUpdateVersionMatchesOnlyValidBranchRevision(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		revision string
		matches  bool
	}{
		{name: "same revision", version: "main-9de2eea0", revision: "9de2eea0ab7d1708e3708bb8eadc89bafa2744b7", matches: true},
		{name: "different revision", version: "main-deadbeef", revision: "9de2eea0ab7d1708e3708bb8eadc89bafa2744b7", matches: false},
		{name: "release version", version: "v0.9.0", revision: "9de2eea0ab7d1708e3708bb8eadc89bafa2744b7", matches: false},
		{name: "invalid hash", version: "main-not-a-hash", revision: "9de2eea0ab7d1708e3708bb8eadc89bafa2744b7", matches: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.matches, systemUpdateVersionMatchesRevision(test.version, "main", test.revision))
		})
	}
}
