package controller

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/group_rate_limit_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGroupRateLimitOptionControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupModelPricingOptionControllerTest(t)
	previousSnapshot := group_rate_limit_setting.GetSettingSnapshot()
	previousRequestCounts, err := setting.ParseModelRequestRateLimitGroup(setting.ModelRequestRateLimitGroup2JSONString())
	require.NoError(t, err)
	require.NoError(t, model.UpdateGroupRateLimitOptions(false, false, map[string][2]int{}, map[string]group_rate_limit_setting.GroupPolicy{}))
	t.Cleanup(func() {
		require.NoError(t, model.UpdateGroupRateLimitOptions(
			previousSnapshot.MemberEnabled,
			previousSnapshot.SharedPoolEnabled,
			previousRequestCounts,
			previousSnapshot.Policies,
		))
	})
	return db
}

func performGroupRateLimitOptionUpdate(t *testing.T, body any) map[string]any {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option/group-rate-limits", bytes.NewReader(payload))

	UpdateGroupRateLimitOptions(context)

	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestUpdateGroupRateLimitOptionsRequiresCompletePayload(t *testing.T) {
	setupGroupRateLimitOptionControllerTest(t)
	response := performGroupRateLimitOptionUpdate(t, map[string]any{
		"member_enabled":      true,
		"shared_pool_enabled": true,
	})
	assert.Equal(t, false, response["success"])
	assert.Contains(t, response["message"], "必须完整提交")
}

func TestUpdateGroupRateLimitOptionsRejectsWholeInvalidPayload(t *testing.T) {
	db := setupGroupRateLimitOptionControllerTest(t)
	rpm := 60
	valid := GroupRateLimitOptionsRequest{
		MemberEnabled:              boolPointer(true),
		SharedPoolEnabled:          boolPointer(false),
		ModelRequestRateLimitGroup: map[string][2]int{"before": {10, 2}},
		Policies: map[string]group_rate_limit_setting.GroupPolicy{
			"before": {MemberLimits: group_rate_limit_setting.Limits{RPMLimit: &rpm}},
		},
	}
	response := performGroupRateLimitOptionUpdate(t, valid)
	require.Equal(t, true, response["success"])

	zero := 0
	invalid := valid
	invalid.SharedPoolEnabled = boolPointer(true)
	invalid.ModelRequestRateLimitGroup = map[string][2]int{"after": {20, 3}}
	invalid.Policies = map[string]group_rate_limit_setting.GroupPolicy{
		"after": {SharedPool: group_rate_limit_setting.Limits{ConcurrencyLimit: &zero}},
	}
	response = performGroupRateLimitOptionUpdate(t, invalid)
	assert.Equal(t, false, response["success"])

	snapshot := group_rate_limit_setting.GetSettingSnapshot()
	assert.True(t, snapshot.MemberEnabled)
	assert.False(t, snapshot.SharedPoolEnabled)
	assert.Contains(t, snapshot.Policies, "before")
	assert.NotContains(t, snapshot.Policies, "after")
	assert.JSONEq(t, `{"before":[10,2]}`, requireClientPolicyOptionValue(t, db, "ModelRequestRateLimitGroup"))
	assert.JSONEq(t, `{"before":{"member_limits":{"rpm_limit":60},"shared_pool":{}}}`, requireClientPolicyOptionValue(t, db, group_rate_limit_setting.PoliciesOptionKey))
}

func TestUpdateGroupRateLimitOptionsPersistsAndPublishesCompleteSetting(t *testing.T) {
	db := setupGroupRateLimitOptionControllerTest(t)
	rpm := 60
	memberConcurrency := 2
	sharedRPM := 3000
	sharedTPS := 1000

	response := performGroupRateLimitOptionUpdate(t, GroupRateLimitOptionsRequest{
		MemberEnabled:              boolPointer(true),
		SharedPoolEnabled:          boolPointer(true),
		ModelRequestRateLimitGroup: map[string][2]int{"vip": {200, 100}},
		Policies: map[string]group_rate_limit_setting.GroupPolicy{
			" vip ": {
				MemberLimits: group_rate_limit_setting.Limits{RPMLimit: &rpm, ConcurrencyLimit: &memberConcurrency},
				SharedPool:   group_rate_limit_setting.Limits{RPMLimit: &sharedRPM, StreamTPSLimit: &sharedTPS},
			},
		},
	})

	assert.Equal(t, true, response["success"])
	snapshot := group_rate_limit_setting.GetSettingSnapshot()
	assert.True(t, snapshot.MemberEnabled)
	assert.True(t, snapshot.SharedPoolEnabled)
	require.Contains(t, snapshot.Policies, "vip")
	assert.Equal(t, rpm, *snapshot.Policies["vip"].MemberLimits.RPMLimit)
	assert.Equal(t, sharedRPM, *snapshot.Policies["vip"].SharedPool.RPMLimit)
	assert.JSONEq(t, `{"vip":[200,100]}`, requireClientPolicyOptionValue(t, db, "ModelRequestRateLimitGroup"))
	assert.Equal(t, "true", requireClientPolicyOptionValue(t, db, group_rate_limit_setting.MemberEnabledOptionKey))
	assert.Equal(t, "true", requireClientPolicyOptionValue(t, db, group_rate_limit_setting.SharedPoolEnabledOptionKey))
	assert.JSONEq(t, `{"vip":{"member_limits":{"rpm_limit":60,"concurrency_limit":2},"shared_pool":{"rpm_limit":3000,"stream_tps_limit":1000}}}`, requireClientPolicyOptionValue(t, db, group_rate_limit_setting.PoliciesOptionKey))
}

func TestUpdateGroupRateLimitOptionsRollsBackOnPersistenceFailure(t *testing.T) {
	db := setupGroupRateLimitOptionControllerTest(t)
	const callbackName = "test:fail_group_rate_limit_option_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		option, ok := tx.Statement.Dest.(*model.Option)
		if ok && option.Key == group_rate_limit_setting.PoliciesOptionKey {
			tx.AddError(errors.New("forced group rate-limit persistence failure"))
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Update().Remove(callbackName))
	})

	rpm := 60
	response := performGroupRateLimitOptionUpdate(t, GroupRateLimitOptionsRequest{
		MemberEnabled:              boolPointer(true),
		SharedPoolEnabled:          boolPointer(true),
		ModelRequestRateLimitGroup: map[string][2]int{"vip": {200, 100}},
		Policies: map[string]group_rate_limit_setting.GroupPolicy{
			"vip": {MemberLimits: group_rate_limit_setting.Limits{RPMLimit: &rpm}},
		},
	})

	assert.Equal(t, false, response["success"])
	snapshot := group_rate_limit_setting.GetSettingSnapshot()
	assert.False(t, snapshot.MemberEnabled)
	assert.False(t, snapshot.SharedPoolEnabled)
	assert.Empty(t, snapshot.Policies)
	assert.JSONEq(t, `{}`, requireClientPolicyOptionValue(t, db, "ModelRequestRateLimitGroup"))
	assert.Equal(t, "false", requireClientPolicyOptionValue(t, db, group_rate_limit_setting.MemberEnabledOptionKey))
	assert.Equal(t, "false", requireClientPolicyOptionValue(t, db, group_rate_limit_setting.SharedPoolEnabledOptionKey))
	assert.JSONEq(t, `{}`, requireClientPolicyOptionValue(t, db, group_rate_limit_setting.PoliciesOptionKey))
}

func boolPointer(value bool) *bool {
	return &value
}
