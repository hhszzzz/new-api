package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupClientPolicyOptionControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupModelPricingOptionControllerTest(t)
	previous := *operation_setting.GetClientPolicySettingSnapshot()
	require.NoError(t, operation_setting.PublishClientPolicySetting(operation_setting.ClientPolicySetting{}))
	t.Cleanup(func() {
		require.NoError(t, operation_setting.PublishClientPolicySetting(previous))
	})
	return db
}

func performClientPolicyOptionUpdate(t *testing.T, body any) map[string]any {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/client-policy",
		bytes.NewReader(payload),
	)

	UpdateClientPolicyOptions(context)

	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func requireClientPolicyOptionValue(t *testing.T, db *gorm.DB, key string) string {
	t.Helper()
	var option model.Option
	require.NoError(t, db.First(&option, "key = ?", key).Error)
	return option.Value
}

func TestUpdateOptionValidatesClientGroupPolicies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body, err := common.Marshal(map[string]interface{}{
		"key":   "client_policy_setting.group_policies",
		"value": `{"default":{"mode":"sometimes","clients":["codex"]}}`,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option", bytes.NewReader(body))

	UpdateOption(context)

	var response map[string]interface{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, false, response["success"])
	assert.Contains(t, response["message"], "invalid client policy mode")
}

func TestUpdateClientPolicyOptionsRejectsWholeInvalidPayload(t *testing.T) {
	db := setupClientPolicyOptionControllerTest(t)
	before := operation_setting.ClientPolicySetting{
		Rules: []operation_setting.ClientIdentificationRule{
			{
				Name: "before",
				Matches: []operation_setting.ClientIdentificationMatch{
					{Source: "user_agent", Mode: "prefix", Value: "before/"},
				},
			},
		},
		GroupPolicies: map[string]operation_setting.ClientAccessPolicy{
			"default": {Mode: operation_setting.ClientPolicyModeAllow, Clients: []string{"before"}},
		},
	}
	require.NoError(t, model.UpdateClientPolicySetting(before))

	response := performClientPolicyOptionUpdate(t, operation_setting.ClientPolicySetting{
		Rules: []operation_setting.ClientIdentificationRule{
			{
				Name: "after",
				Matches: []operation_setting.ClientIdentificationMatch{
					{Source: "user_agent", Mode: "prefix", Value: "after/"},
				},
			},
		},
		GroupPolicies: map[string]operation_setting.ClientAccessPolicy{
			"default": {Mode: "sometimes", Clients: []string{"after"}},
		},
	})

	assert.Equal(t, false, response["success"])
	assert.Contains(t, response["message"], "invalid client policy mode")
	snapshot := operation_setting.GetClientPolicySettingSnapshot()
	require.Len(t, snapshot.Rules, 1)
	assert.Equal(t, "before", snapshot.Rules[0].Name)
	assert.Equal(t, []string{"before"}, snapshot.GroupPolicies["default"].Clients)
	assert.JSONEq(t, `[{"name":"before","matches":[{"source":"user_agent","mode":"prefix","value":"before/"}]}]`, requireClientPolicyOptionValue(t, db, operation_setting.ClientPolicyRulesOptionKey))
	assert.JSONEq(t, `{"default":{"mode":"allow","clients":["before"]}}`, requireClientPolicyOptionValue(t, db, operation_setting.ClientPolicyGroupsOptionKey))
}

func TestUpdateClientPolicyOptionsPersistsAndPublishesCompleteSetting(t *testing.T) {
	db := setupClientPolicyOptionControllerTest(t)
	beforeSnapshot := operation_setting.GetClientPolicySettingSnapshot()

	response := performClientPolicyOptionUpdate(t, operation_setting.ClientPolicySetting{
		Rules: []operation_setting.ClientIdentificationRule{
			{
				Name: " Desktop ",
				Matches: []operation_setting.ClientIdentificationMatch{
					{Source: " USER_AGENT ", Mode: " PREFIX ", Value: " codex/ "},
				},
			},
		},
		GroupPolicies: map[string]operation_setting.ClientAccessPolicy{
			" default ": {Mode: "ALLOW", Clients: []string{" Desktop "}},
		},
	})

	assert.Equal(t, true, response["success"])
	snapshot := operation_setting.GetClientPolicySettingSnapshot()
	require.NotSame(t, beforeSnapshot, snapshot)
	require.Len(t, snapshot.Rules, 1)
	assert.Equal(t, "desktop", snapshot.Rules[0].Name)
	require.Len(t, snapshot.Rules[0].Matches, 1)
	assert.Equal(t, operation_setting.ClientIdentificationMatch{
		Source: "user_agent",
		Mode:   "prefix",
		Value:  "codex/",
	}, snapshot.Rules[0].Matches[0])
	assert.Equal(t, operation_setting.ClientAccessPolicy{
		Mode:    operation_setting.ClientPolicyModeAllow,
		Clients: []string{"desktop"},
	}, snapshot.GroupPolicies["default"])

	wantRules := `[{"name":"desktop","matches":[{"source":"user_agent","mode":"prefix","value":"codex/"}]}]`
	wantGroups := `{"default":{"mode":"allow","clients":["desktop"]}}`
	assert.JSONEq(t, wantRules, requireClientPolicyOptionValue(t, db, operation_setting.ClientPolicyRulesOptionKey))
	assert.JSONEq(t, wantGroups, requireClientPolicyOptionValue(t, db, operation_setting.ClientPolicyGroupsOptionKey))
	common.OptionMapRWMutex.RLock()
	publishedRules := common.OptionMap[operation_setting.ClientPolicyRulesOptionKey]
	publishedGroups := common.OptionMap[operation_setting.ClientPolicyGroupsOptionKey]
	common.OptionMapRWMutex.RUnlock()
	assert.JSONEq(t, wantRules, publishedRules)
	assert.JSONEq(t, wantGroups, publishedGroups)
}
