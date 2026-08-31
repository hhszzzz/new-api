package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/account_pool_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func publishAccountPoolControllerSetting(t *testing.T) {
	t.Helper()
	previous := account_pool_setting.GetSettingSnapshot()
	require.NotNil(t, previous)
	t.Cleanup(func() { previous.PublishConfig() })
	setting := account_pool_setting.Setting{
		Enabled:                   true,
		HideEmailFromNonAdmins:    true,
		AllowedGroups:             []string{"vip", "team"},
		RegularRefreshSeconds:     300,
		NearResetThresholdSeconds: 600,
		NearResetRefreshSeconds:   60,
		PostResetDelaySeconds:     10,
		ManualRefreshCooldown:     60,
	}
	setting.PublishConfig()
}

func accountPoolAuthorizationContext(role int, groups []string) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("role", role)
	common.SetContextKey(context, constant.ContextKeyUserGroups, groups)
	return context
}

func TestAuthorizeAccountPoolUsesAnyEffectiveGroupAndAdminBypass(t *testing.T) {
	publishAccountPoolControllerSetting(t)

	_, allowed := authorizeAccountPool(
		accountPoolAuthorizationContext(common.RoleCommonUser, []string{"default", "vip"}),
	)
	assert.True(t, allowed)

	_, allowed = authorizeAccountPool(
		accountPoolAuthorizationContext(common.RoleCommonUser, []string{"default"}),
	)
	assert.False(t, allowed)

	_, allowed = authorizeAccountPool(
		accountPoolAuthorizationContext(common.RoleAdminUser, nil),
	)
	assert.True(t, allowed)
}

func TestGetAccountPoolSettingsNeverReturnsManagementConnectionValues(t *testing.T) {
	publishAccountPoolControllerSetting(t)
	t.Setenv("CLIPROXY_MANAGEMENT_URL", "http://internal-sensitive-host:8317/v0/management")
	t.Setenv("CLIPROXY_MANAGEMENT_KEY", "management-key-secret")

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	GetAccountPoolSettings(context)

	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"management_key_configured":true`)
	assert.Contains(t, recorder.Body.String(), `"hide_email_from_non_admins":true`)
	assert.NotContains(t, recorder.Body.String(), "management-key-secret")
	assert.NotContains(t, recorder.Body.String(), "internal-sensitive-host")
}

func TestGetAccountPoolSettingsSerializesEmptyAllowedGroupsAsArray(t *testing.T) {
	previous := account_pool_setting.GetSettingSnapshot()
	require.NotNil(t, previous)
	t.Cleanup(func() { previous.PublishConfig() })
	setting := account_pool_setting.Setting{
		Enabled:                   true,
		HideEmailFromNonAdmins:    true,
		AllowedGroups:             []string{},
		RegularRefreshSeconds:     300,
		NearResetThresholdSeconds: 600,
		NearResetRefreshSeconds:   60,
		PostResetDelaySeconds:     10,
		ManualRefreshCooldown:     60,
	}
	setting.PublishConfig()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	GetAccountPoolSettings(context)

	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"allowed_groups":[]`)
	assert.NotContains(t, recorder.Body.String(), `"allowed_groups":null`)
}

func TestAccountPoolSettingsRequestKeepsEmailHiddenWhenFieldIsMissing(t *testing.T) {
	current := account_pool_setting.Setting{HideEmailFromNonAdmins: true}
	request := accountPoolSettingsRequest{}

	setting := request.withPrivacyDefault(&current)

	assert.True(t, setting.HideEmailFromNonAdmins)

	showEmail := false
	request.HideEmailFromNonAdmins = &showEmail
	setting = request.withPrivacyDefault(&current)
	assert.False(t, setting.HideEmailFromNonAdmins)
}

func TestAccountPoolSettingsRequestBindsExplicitEmailVisibility(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{
		"enabled": true,
		"hide_email_from_non_admins": false,
		"allowed_groups": ["vip"],
		"regular_refresh_seconds": 300,
		"near_reset_threshold_seconds": 600,
		"near_reset_refresh_seconds": 60,
		"post_reset_delay_seconds": 10,
		"manual_refresh_cooldown_seconds": 60
	}`))
	context.Request.Header.Set("Content-Type", "application/json")
	var request accountPoolSettingsRequest

	err := context.ShouldBindJSON(&request)

	require.NoError(t, err)
	setting := request.withPrivacyDefault(nil)
	assert.True(t, setting.Enabled)
	assert.False(t, setting.HideEmailFromNonAdmins)
	assert.Equal(t, []string{"vip"}, setting.AllowedGroups)
	assert.Equal(t, 300, setting.RegularRefreshSeconds)
}

func TestSelfUserDataIncludesServerCalculatedAccountPoolCapability(t *testing.T) {
	publishAccountPoolControllerSetting(t)

	allowed := buildSelfUserData(&model.User{
		Id:       1,
		Username: "allowed-user",
		Role:     common.RoleCommonUser,
		Groups:   []string{"default", "vip"},
	})
	allowedPermissions, ok := allowed["permissions"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, allowedPermissions["account_pool"])

	denied := buildSelfUserData(&model.User{
		Id:       2,
		Username: "denied-user",
		Role:     common.RoleCommonUser,
		Groups:   []string{"default"},
	})
	deniedPermissions, ok := denied["permissions"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, deniedPermissions["account_pool"])
}
