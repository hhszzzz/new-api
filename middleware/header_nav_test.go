package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withHeaderNavModules(t *testing.T, raw string) {
	t.Helper()

	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	previous, hadPrevious := common.OptionMap["HeaderNavModules"]
	common.OptionMap["HeaderNavModules"] = raw
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if hadPrevious {
			common.OptionMap["HeaderNavModules"] = previous
			return
		}
		delete(common.OptionMap, "HeaderNavModules")
	})
}

func performHeaderNavRequest(t *testing.T, handler gin.HandlerFunc, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/test", handler, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"id":      c.GetInt("id"),
			"role":    c.GetInt("role"),
		})
	})

	var accessToken string
	if authenticated {
		previousDB, previousRedis := model.DB, common.RedisEnabled
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}))
		model.DB = db
		common.RedisEnabled = false
		require.NoError(t, model.EnsureLegacyAccessTokenRetireAt(time.Now().Unix()))
		t.Cleanup(func() {
			model.DB = previousDB
			common.RedisEnabled = previousRedis
		})
		accessToken = "header-nav-pat"
		user := model.User{
			Username:    "tester",
			Password:    "unused-password-hash",
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
			AuthVersion: 1,
		}
		user.SetAccessToken(accessToken)
		require.NoError(t, db.Create(&user).Error)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestHeaderNavModuleAuthAllowsDefaultPublicAccess(t *testing.T) {
	withHeaderNavModules(t, "")

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("pricing"), false)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHeaderNavModuleAuthPopulatesOptionalUserIdentity(t *testing.T) {
	withHeaderNavModules(t, "")

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("rankings"), true)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"id":1`)
	require.Contains(t, recorder.Body.String(), fmt.Sprintf(`"role":%d`, common.RoleCommonUser))
}

func TestHeaderNavModuleAuthRejectsDisabledPricing(t *testing.T) {
	raw := `{"pricing":{"enabled":false,"requireAuth":false}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("pricing"), false)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestDisabledPricingRequiresRootAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name       string
		module     string
		options    string
		role       int
		credential string
		wantStatus int
	}{
		{name: "root", role: common.RoleRootUser, wantStatus: http.StatusOK},
		{name: "root with login required", options: `{"pricing":{"enabled":false,"requireAuth":true}}`, role: common.RoleRootUser, wantStatus: http.StatusOK},
		{name: "root with legacy disabled setting", options: `{"pricing":false}`, role: common.RoleRootUser, wantStatus: http.StatusOK},
		{name: "ordinary user", role: common.RoleCommonUser, wantStatus: http.StatusForbidden},
		{name: "non-root administrator", role: common.RoleAdminUser, wantStatus: http.StatusForbidden},
		{name: "expired root token", role: common.RoleRootUser, credential: "expired", wantStatus: http.StatusUnauthorized},
		{name: "tampered root token", role: common.RoleRootUser, credential: "tampered", wantStatus: http.StatusUnauthorized},
		{name: "revoked root session", role: common.RoleRootUser, credential: "revoked", wantStatus: http.StatusUnauthorized},
		{name: "disabled root account", role: common.RoleRootUser, credential: "banned", wantStatus: http.StatusUnauthorized},
		{name: "root cannot bypass disabled rankings", module: "rankings", options: `{"rankings":false}`, role: common.RoleRootUser, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupDashboardAuthMiddlewareTest(t)
			gin.SetMode(gin.TestMode)
			options := tc.options
			if options == "" {
				options = `{"pricing":{"enabled":false,"requireAuth":false}}`
			}
			withHeaderNavModules(t, options)
			user := model.User{
				Username: "pricing-reader", Password: "unused-password-hash", Role: tc.role,
				Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
			}
			require.NoError(t, model.DB.Create(&user).Error)
			bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "header-nav-test")
			require.NoError(t, err)
			token := bundle.AccessToken
			switch tc.credential {
			case "expired":
				identity, internal, err := service.ParseDashboardAccessToken(token)
				require.NoError(t, err)
				require.True(t, internal)
				token = issueExpiredDashboardAccessToken(t, identity)
			case "tampered":
				token = tamperDashboardToken(token)
			case "revoked":
				revoked, err := model.RevokeUserSession(user.Id, bundle.Session.SID, "test")
				require.NoError(t, err)
				require.True(t, revoked)
			case "banned":
				require.NoError(t, model.DB.Model(&user).Update("status", common.UserStatusDisabled).Error)
			}

			module := tc.module
			if module == "" {
				module = "pricing"
			}
			handled := false
			router := gin.New()
			router.GET("/api/"+module, HeaderNavModuleAuth(module), func(c *gin.Context) {
				handled = true
				assert.Equal(t, user.Id, c.GetInt("id"))
				assert.Equal(t, common.RoleRootUser, c.GetInt("role"))
				c.JSON(http.StatusOK, gin.H{"success": true})
			})

			request := httptest.NewRequest(http.MethodGet, "/api/"+module, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			assert.Equal(t, tc.wantStatus, response.Code)
			assert.Equal(t, tc.wantStatus == http.StatusOK, handled)
		})
	}
}

func TestHeaderNavModuleAuthRequiresLoginForPricing(t *testing.T) {
	raw := `{"pricing":{"enabled":true,"requireAuth":true}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("pricing"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavModuleAuthRequiresLoginForRankings(t *testing.T) {
	raw := `{"rankings":{"enabled":true,"requireAuth":true}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("rankings"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavModuleAuthInheritsPricingAndAllowsModelStatusOverrides(t *testing.T) {
	tests := []struct {
		name          string
		config        string
		authenticated bool
		wantStatus    int
	}{
		{
			name:       "model status defaults to enabled and public",
			config:     "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing model status inherits disabled pricing",
			config:     `{"pricing":{"enabled":false,"requireAuth":false}}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing model status inherits pricing login requirement",
			config:     `{"pricing":{"enabled":true,"requireAuth":true}}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "legacy model status boolean inherits pricing login requirement",
			config:     `{"pricing":{"enabled":true,"requireAuth":true},"modelStatus":true}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "partial model status object inherits disabled pricing",
			config:     `{"pricing":{"enabled":false,"requireAuth":true},"modelStatus":{"requireAuth":false}}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "explicit model status settings override disabled pricing",
			config:     `{"pricing":{"enabled":false,"requireAuth":false},"modelStatus":{"enabled":true,"requireAuth":false}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "model status can require login independently",
			config:     `{"pricing":{"enabled":true,"requireAuth":false},"modelStatus":{"enabled":true,"requireAuth":true}}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "authenticated user can access model status when login is required",
			config:        `{"pricing":{"enabled":true,"requireAuth":false},"modelStatus":{"enabled":true,"requireAuth":true}}`,
			authenticated: true,
			wantStatus:    http.StatusOK,
		},
		{
			name:       "model status can be disabled independently",
			config:     `{"pricing":{"enabled":true,"requireAuth":false},"modelStatus":{"enabled":false,"requireAuth":false}}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:          "disabled model status remains forbidden to authenticated users",
			config:        `{"pricing":{"enabled":true,"requireAuth":false},"modelStatus":{"enabled":false,"requireAuth":false}}`,
			authenticated: true,
			wantStatus:    http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withHeaderNavModules(t, test.config)
			recorder := performHeaderNavRequest(t, HeaderNavModuleAuth(HeaderNavModuleModelStatus), test.authenticated)
			require.Equal(t, test.wantStatus, recorder.Code)
		})
	}
}

func TestHeaderNavModuleAuthModelRadarInheritsModelStatusAndAllowsOverrides(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantStatus int
	}{
		{name: "defaults to public", config: "", wantStatus: http.StatusOK},
		{name: "inherits disabled model status", config: `{"modelStatus":{"enabled":false,"requireAuth":false}}`, wantStatus: http.StatusForbidden},
		{name: "inherits model status login requirement", config: `{"modelStatus":{"enabled":true,"requireAuth":true}}`, wantStatus: http.StatusUnauthorized},
		{name: "overrides inherited status", config: `{"modelStatus":{"enabled":false,"requireAuth":true},"modelRadar":{"enabled":true,"requireAuth":false}}`, wantStatus: http.StatusOK},
		{name: "can be disabled independently", config: `{"modelStatus":{"enabled":true,"requireAuth":false},"modelRadar":{"enabled":false,"requireAuth":false}}`, wantStatus: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withHeaderNavModules(t, test.config)
			recorder := performHeaderNavRequest(t, HeaderNavModuleAuth(HeaderNavModuleModelRadar), false)
			require.Equal(t, test.wantStatus, recorder.Code)
		})
	}
}

func TestHeaderNavModuleAuthRejectsLegacyDisabledModule(t *testing.T) {
	raw := `{"rankings":false}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("rankings"), false)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthAllowsDefaultPublicAccess(t *testing.T) {
	withHeaderNavModules(t, "")

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), false)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthRequiresLoginWhenDisabled(t *testing.T) {
	raw := `{"pricing":{"enabled":false,"requireAuth":false}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthAllowsLoggedInWhenDisabled(t *testing.T) {
	raw := `{"pricing":{"enabled":false,"requireAuth":false}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), true)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthRequiresLoginWhenRequireAuth(t *testing.T) {
	raw := `{"pricing":{"enabled":true,"requireAuth":true}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthRequiresLoginForLegacyDisabledModule(t *testing.T) {
	raw := `{"pricing":false}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavPublicRouteRejectsExpiredInternalAccessToken(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	withHeaderNavModules(t, "")
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/api/test", HeaderNavModuleAuth("pricing"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	request.Header.Set("Authorization", "Bearer "+issueExpiredDashboardAccessToken(t, service.AuthIdentity{
		UserID: 1, SessionID: "expired-header-nav-session", UserAuthVersion: 1, SessionVersion: 1,
	}))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), "AUTH_TOKEN_EXPIRED")
}
