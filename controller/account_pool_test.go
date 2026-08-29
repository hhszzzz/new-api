package controller

import (
	"net/http/httptest"
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
	assert.NotContains(t, recorder.Body.String(), "management-key-secret")
	assert.NotContains(t, recorder.Body.String(), "internal-sensitive-host")
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
