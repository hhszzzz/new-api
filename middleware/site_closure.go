package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const siteClosedWebMessage = "The site is closed for maintenance. Please try again later."

var siteClosureAllowedExactPaths = map[string]struct{}{
	"/api/setup":                    {},
	"/api/status":                   {},
	"/api/notice":                   {},
	"/api/user/auth/refresh":        {},
	"/api/user/auth/logout":         {},
	"/api/oauth/telegram/login":     {},
	"/api/log/token":                {},
	"/api/stripe/webhook":           {},
	"/api/creem/webhook":            {},
	"/api/waffo/webhook":            {},
	"/api/user/epay/notify":         {},
	"/api/subscription/epay/notify": {},
	"/api/subscription/epay/return": {},
}

var siteClosureAllowedPrefixes = []string{
	"/api/user/login",
	"/api/user/passkey/login/",
	"/api/usage/token",
	"/api/option",
	"/api/waffo-pancake/webhook/",
}

// SiteClosureWeb blocks non-admin dashboard API requests while the web site is
// closed. It runs before route auth, so allowlisted endpoints keep their own
// authentication, and the admin lookup never aborts the request.
func SiteClosureWeb() gin.HandlerFunc {
	return func(c *gin.Context) {
		setting := operation_setting.GetSiteClosureSetting()
		if !setting.WebClosed ||
			c.Request.Method == http.MethodOptions ||
			isSiteClosureAllowedPath(c.Request.URL.Path) ||
			IsAdminRequest(c) {
			c.Next()
			return
		}
		message := strings.TrimSpace(setting.Message)
		if message == "" {
			message = siteClosedWebMessage
		}
		abortWithWebMessage(c, http.StatusServiceUnavailable, "SITE_CLOSED", common.NewMessage(message))
	}
}

// SiteClosureAPI blocks non-admin relay requests while the API service is
// paused. It must run after the route's TokenAuth/UserAuth middleware so the
// caller's role is available, and before rate limiting or channel selection.
func SiteClosureAPI() gin.HandlerFunc {
	return func(c *gin.Context) {
		setting := operation_setting.GetSiteClosureSetting()
		if !setting.APIPaused || c.GetInt("role") >= common.RoleAdminUser {
			c.Next()
			return
		}
		writeSiteClosurePausedError(c, setting.Message)
	}
}

// IsAdminRequest reports whether the request carries a valid enabled admin
// dashboard credential. It is intentionally lenient: invalid or expired
// credentials simply count as anonymous.
func IsAdminRequest(c *gin.Context) bool {
	if c.GetInt("role") >= common.RoleAdminUser {
		return true
	}
	user, _, kind, err := classifyDashboardCredential(c)
	if kind == dashboardCredentialUnmatched {
		return false
	}
	return err == nil && user != nil &&
		user.Status == common.UserStatusEnabled &&
		user.Role >= common.RoleAdminUser
}

func isSiteClosureAllowedPath(path string) bool {
	if _, ok := siteClosureAllowedExactPaths[path]; ok {
		return true
	}
	for _, prefix := range siteClosureAllowedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	// GET/POST /api/oauth/:provider and POST /api/oauth/state are login entry
	// points. Bind endpoints contain a second path segment and stay blocked.
	rest, ok := strings.CutPrefix(path, "/api/oauth/")
	return ok && rest != "" && !strings.Contains(rest, "/")
}

func writeSiteClosurePausedError(c *gin.Context, customMessage string) {
	message := strings.TrimSpace(customMessage)
	if message == "" {
		message = i18n.T(c, i18n.MsgSiteClosed)
	}
	path := c.Request.URL.Path
	if strings.Contains(path, "/mj/") {
		abortWithMidjourneyMessage(c, http.StatusServiceUnavailable, http.StatusServiceUnavailable, message)
		return
	}
	apiError := hosttypes.NewErrorWithStatusCode(
		errors.New(message), hosttypes.ErrorCodeSiteClosed, http.StatusServiceUnavailable)
	switch {
	case isSiteClosureClaudePath(c, path):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"type":  "error",
			"error": apiError.ToClaudeError(),
		})
	case isSiteClosureGeminiPath(c, path):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
			"code":    http.StatusServiceUnavailable,
			"message": message,
			"status":  "UNAVAILABLE",
		}})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": apiError.ToOpenAIError()})
	}
	c.Abort()
}

func isSiteClosureClaudePath(c *gin.Context, path string) bool {
	if strings.HasPrefix(path, "/v1/messages") {
		return true
	}
	if c.Request.Method == http.MethodGet && (path == "/v1/models" || strings.HasPrefix(path, "/v1/models/")) {
		return c.GetHeader("x-api-key") != "" && c.GetHeader("anthropic-version") != ""
	}
	return false
}

func isSiteClosureGeminiPath(c *gin.Context, path string) bool {
	if strings.HasPrefix(path, "/v1beta") {
		return !strings.HasPrefix(path, "/v1beta/openai/")
	}
	if strings.HasPrefix(path, "/v1/engines/") {
		return true
	}
	if c.Request.Method == http.MethodPost && strings.HasPrefix(path, "/v1/models/") {
		return true
	}
	if c.Request.Method == http.MethodGet && (path == "/v1/models" || strings.HasPrefix(path, "/v1/models/")) {
		return c.GetHeader("x-goog-api-key") != "" || c.Query("key") != ""
	}
	return false
}
