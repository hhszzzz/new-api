package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuildChannelCandidateFilterCombinesProtocolAndClientPolicy(t *testing.T) {
	settings := hostdto.ChannelOtherSettings{
		ClientPolicy: operation_setting.ClientAccessPolicy{
			Mode:    operation_setting.ClientPolicyModeAllow,
			Clients: []string{clientpolicy.ClientCodex},
		},
	}
	encoded, err := common.Marshal(settings)
	require.NoError(t, err)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	context.Request.Header.Set("User-Agent", "codex_cli_rs/1.0")
	filter := BuildChannelCandidateFilter(context, "gpt-5.5")
	require.NotNil(t, filter)

	allowed := &model.Channel{Type: 1, OtherSettings: string(encoded)}
	assert.True(t, filter(allowed))

	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	context.Request.Header.Set("User-Agent", "curl/8.0")
	filter = BuildChannelCandidateFilter(context, "gpt-5.5")
	assert.False(t, filter(allowed))
}

func TestGroupAllowsRequestClientEnforcesAllowAndDenyModes(t *testing.T) {
	setting := operation_setting.GetClientPolicySetting()
	original := *operation_setting.GetClientPolicySettingSnapshot()
	setting.GroupPolicies = map[string]operation_setting.ClientAccessPolicy{
		"codex-only": {
			Mode:    operation_setting.ClientPolicyModeAllow,
			Clients: []string{clientpolicy.ClientCodex},
		},
		"no-claude": {
			Mode:    operation_setting.ClientPolicyModeDeny,
			Clients: []string{clientpolicy.ClientClaudeCode},
		},
	}
	operation_setting.NormalizeClientPolicySetting()
	t.Cleanup(func() {
		*setting = original
		operation_setting.NormalizeClientPolicySetting()
	})

	codexContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	codexContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	codexContext.Request.Header.Set("User-Agent", "codex_cli_rs/1.0")
	assert.True(t, groupAllowsRequestClient(codexContext, "codex-only"))

	unknownContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	unknownContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	unknownContext.Request.Header.Set("User-Agent", "curl/8.0")
	assert.False(t, groupAllowsRequestClient(unknownContext, "codex-only"))
	assert.True(t, groupAllowsRequestClient(unknownContext, "no-claude"))

	claudeContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	claudeContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	claudeContext.Request.Header.Set("User-Agent", "claude-cli/2.0.0")
	assert.False(t, groupAllowsRequestClient(claudeContext, "no-claude"))
}

func TestDistributeRejectsRealRequestForDisallowedGroupClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setting := operation_setting.GetClientPolicySetting()
	original := *operation_setting.GetClientPolicySettingSnapshot()
	setting.GroupPolicies = map[string]operation_setting.ClientAccessPolicy{
		"codex-only": {
			Mode:    operation_setting.ClientPolicyModeAllow,
			Clients: []string{clientpolicy.ClientCodex},
		},
	}
	operation_setting.NormalizeClientPolicySetting()
	t.Cleanup(func() {
		*setting = original
		operation_setting.NormalizeClientPolicySetting()
	})

	called := false
	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "codex-only")
		common.SetContextKey(c, constant.ContextKeyUserGroups, []string{"codex-only"})
	}, Distribute(), func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "curl/8.0")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestSetupContextForSelectedChannelEnforcesClientPolicyForAutoGroupPin(t *testing.T) {
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousAutoGroups := setting.AutoGroups2JsonString()
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	clientPolicySetting := operation_setting.GetClientPolicySetting()
	previousClientPolicySetting := *operation_setting.GetClientPolicySettingSnapshot()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		*clientPolicySetting = previousClientPolicySetting
		operation_setting.NormalizeClientPolicySetting()
		if previousMemoryCacheEnabled && previousDB != nil {
			model.InitChannelCache()
		}
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	const modelName = "client-policy-auto-group-model"
	channel := &model.Channel{
		Id:     8801,
		Key:    "client-policy-test-key",
		Name:   "client-policy-test-channel",
		Type:   constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled,
		Models: modelName,
	}
	require.NoError(t, db.Create(channel).Error)
	for _, group := range []string{"default", "vip"} {
		require.NoError(t, db.Create(&model.Ability{
			Group:     group,
			Model:     modelName,
			ChannelId: channel.Id,
			Enabled:   true,
			Weight:    1,
		}).Error)
	}
	model.InitChannelCache()

	clientPolicySetting.GroupPolicies = map[string]operation_setting.ClientAccessPolicy{
		"default": {Mode: operation_setting.ClientPolicyModeDeny, Clients: []string{clientpolicy.ClientCodex}},
	}
	operation_setting.NormalizeClientPolicySetting()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("User-Agent", "codex_cli_rs/1.0")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "auto")
	common.SetContextKey(ctx, constant.ContextKeyUserGroups, []string{"default", "vip"})

	require.Nil(t, SetupContextForSelectedChannel(ctx, channel, modelName, false))
	assert.Equal(t, "vip", common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))

	clientPolicySetting.GroupPolicies["vip"] = operation_setting.ClientAccessPolicy{
		Mode:    operation_setting.ClientPolicyModeDeny,
		Clients: []string{clientpolicy.ClientCodex},
	}
	operation_setting.NormalizeClientPolicySetting()
	blocked, _ := gin.CreateTestContext(httptest.NewRecorder())
	blocked.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	blocked.Request.Header.Set("User-Agent", "codex_cli_rs/1.0")
	common.SetContextKey(blocked, constant.ContextKeyUsingGroup, "auto")
	common.SetContextKey(blocked, constant.ContextKeyUserGroups, []string{"default", "vip"})

	apiErr := SetupContextForSelectedChannel(blocked, channel, modelName, false)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
}
