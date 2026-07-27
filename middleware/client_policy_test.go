package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChannelCandidateFilterCombinesProtocolAndClientPolicy(t *testing.T) {
	settings := dto.ChannelOtherSettings{
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
