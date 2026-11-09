package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withSiteClosureSetting(t *testing.T, setting operation_setting.SiteClosureSetting) {
	t.Helper()
	original := operation_setting.GetSiteClosureSetting()
	t.Cleanup(func() {
		updateSiteClosureSetting(t, original)
	})
	updateSiteClosureSetting(t, setting)
}

func updateSiteClosureSetting(t *testing.T, setting operation_setting.SiteClosureSetting) {
	t.Helper()
	handled, err := config.GlobalConfig.Update(operation_setting.SiteClosureConfigName, map[string]string{
		"web_closed":           strconv.FormatBool(setting.WebClosed),
		"api_paused":           strconv.FormatBool(setting.APIPaused),
		"title":                setting.Title,
		"message":              setting.Message,
		"expected_recovery_at": strconv.FormatInt(setting.ExpectedRecoveryAt, 10),
	})
	require.True(t, handled)
	require.NoError(t, err)
}

// runSiteClosureMiddleware calls the handler directly. A middleware that does
// not abort has passed the request to the next handler in a real Gin chain.
func runSiteClosureMiddleware(handler gin.HandlerFunc, method, path string, role int) (int, string, bool) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, nil)
	if role > 0 {
		c.Set("role", role)
	}
	handler(c)
	return recorder.Code, recorder.Body.String(), !c.IsAborted()
}

func TestSiteClosureAPIBlocksNonAdminWithProtocolEnvelopes(t *testing.T) {
	withSiteClosureSetting(t, operation_setting.SiteClosureSetting{
		APIPaused: true,
		Message:   "maintenance",
	})

	cases := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "openai",
			path:     "/v1/chat/completions",
			expected: `{"error":{"message":"maintenance","type":"new_api_error","param":"","code":"site_closed"}}`,
		},
		{
			name:     "claude",
			path:     "/v1/messages",
			expected: `{"type":"error","error":{"type":"api_error","message":"maintenance"}}`,
		},
		{
			name:     "gemini",
			path:     "/v1beta/models/gemini-2.0-flash:generateContent",
			expected: `{"error":{"code":503,"message":"maintenance","status":"UNAVAILABLE"}}`,
		},
		{
			name:     "gemini legacy",
			path:     "/v1/engines/gemini/embeddings",
			expected: `{"error":{"code":503,"message":"maintenance","status":"UNAVAILABLE"}}`,
		},
		{
			name:     "midjourney",
			path:     "/mj/submit/imagine",
			expected: `{"description":"maintenance","type":"new_api_error","code":503}`,
		},
		{
			name:     "midjourney image",
			path:     "/mj/image/123",
			expected: `{"description":"maintenance","type":"new_api_error","code":503}`,
		},
		{
			name:     "task artifact content",
			path:     "/v1/tasks/foo/artifacts/bar/content",
			expected: `{"error":{"message":"maintenance","type":"new_api_error","param":"","code":"site_closed"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body, passed := runSiteClosureMiddleware(
				SiteClosureAPI(), http.MethodPost, tc.path, common.RoleCommonUser)
			assert.Equal(t, http.StatusServiceUnavailable, status)
			assert.False(t, passed)
			assert.JSONEq(t, tc.expected, body)
		})
	}

	status, _, passed := runSiteClosureMiddleware(
		SiteClosureAPI(), http.MethodPost, "/v1/chat/completions", common.RoleAdminUser)
	assert.Equal(t, http.StatusOK, status)
	assert.True(t, passed)
}

func TestSiteClosureAPIPassesWhenNotPaused(t *testing.T) {
	withSiteClosureSetting(t, operation_setting.SiteClosureSetting{APIPaused: false})

	status, _, passed := runSiteClosureMiddleware(
		SiteClosureAPI(), http.MethodPost, "/v1/chat/completions", common.RoleCommonUser)
	assert.Equal(t, http.StatusOK, status)
	assert.True(t, passed)
}

func TestSiteClosureWebAllowlistAndBlocking(t *testing.T) {
	withSiteClosureSetting(t, operation_setting.SiteClosureSetting{
		WebClosed: true,
		Message:   "maintenance",
	})

	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/status"},
		{http.MethodGet, "/api/setup"},
		{http.MethodGet, "/api/notice"},
		{http.MethodPost, "/api/user/login"},
		{http.MethodGet, "/api/user/login/encryption-key"},
		{http.MethodPost, "/api/user/login/2fa"},
		{http.MethodPost, "/api/user/passkey/login/begin"},
		{http.MethodPost, "/api/oauth/state"},
		{http.MethodGet, "/api/oauth/github"},
		{http.MethodGet, "/api/oauth/wechat"},
		{http.MethodGet, "/api/oauth/telegram/login"},
		{http.MethodGet, "/api/usage/token/"},
		{http.MethodGet, "/api/log/token"},
		{http.MethodGet, "/api/option/"},
		{http.MethodPost, "/api/stripe/webhook"},
		{http.MethodPost, "/api/creem/webhook"},
		{http.MethodPost, "/api/waffo/webhook"},
		{http.MethodPost, "/api/waffo-pancake/webhook/prod"},
		{http.MethodPost, "/api/user/epay/notify"},
		{http.MethodPost, "/api/subscription/epay/notify"},
		{http.MethodGet, "/api/subscription/epay/return"},
	}
	for _, tc := range allowed {
		t.Run("allow "+tc.path, func(t *testing.T) {
			status, _, passed := runSiteClosureMiddleware(
				SiteClosureWeb(), tc.method, tc.path, 0)
			assert.Equal(t, http.StatusOK, status)
			assert.True(t, passed)
		})
	}

	blocked := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/user/self"},
		{http.MethodGet, "/api/pricing"},
		{http.MethodPost, "/api/user/register"},
		{http.MethodPost, "/api/oauth/email/bind/start"},
		{http.MethodPost, "/api/oauth/wechat/bind"},
	}
	for _, tc := range blocked {
		t.Run("block "+tc.path, func(t *testing.T) {
			status, body, passed := runSiteClosureMiddleware(
				SiteClosureWeb(), tc.method, tc.path, 0)
			assert.Equal(t, http.StatusServiceUnavailable, status)
			assert.False(t, passed)
			assert.Contains(t, body, "SITE_CLOSED")
			assert.Contains(t, body, "maintenance")
		})
	}

	status, _, passed := runSiteClosureMiddleware(
		SiteClosureWeb(), http.MethodGet, "/api/user/self", common.RoleAdminUser)
	assert.Equal(t, http.StatusOK, status)
	assert.True(t, passed)
}

func TestIsAdminRequestUsesContextRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	assert.False(t, IsAdminRequest(c))

	c.Set("role", common.RoleCommonUser)
	assert.False(t, IsAdminRequest(c))

	c.Set("role", common.RoleAdminUser)
	assert.True(t, IsAdminRequest(c))
}

func TestSiteClosureSettingValidationAndSnapshot(t *testing.T) {
	withSiteClosureSetting(t, operation_setting.SiteClosureSetting{})

	handled, err := config.GlobalConfig.Update(operation_setting.SiteClosureConfigName, map[string]string{
		"expected_recovery_at": "-1",
	})
	require.True(t, handled)
	require.Error(t, err)

	handled, err = config.GlobalConfig.Update(operation_setting.SiteClosureConfigName, map[string]string{
		"title": strings.Repeat("a", operation_setting.SiteClosureMaxTitleRunes+1),
	})
	require.True(t, handled)
	require.Error(t, err)

	handled, err = config.GlobalConfig.UpdateFromDB(operation_setting.SiteClosureConfigName, map[string]string{
		"expected_recovery_at": "-10",
		"title":                strings.Repeat("a", operation_setting.SiteClosureMaxTitleRunes+10),
	})
	require.True(t, handled)
	require.NoError(t, err)

	got := operation_setting.GetSiteClosureSetting()
	require.Equal(t, int64(0), got.ExpectedRecoveryAt)
	require.Len(t, []rune(got.Title), operation_setting.SiteClosureMaxTitleRunes)
}
