package controller

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPolicyMutationDistinguishesOmittedNullAndCustomRateLimits(t *testing.T) {
	fallback := &model.UserPolicyUpdate{
		Groups:                []string{"default"},
		PrimaryGroup:          "default",
		TopupGroup:            "default",
		ModelLimits:           []string{},
		ModelBlocklist:        []string{},
		RpmLimit:              common.GetPointer(60),
		ConcurrencyLimit:      common.GetPointer(2),
		StreamTpsLimit:        common.GetPointer(12),
		ModelLimitsEnabled:    false,
		ModelBlocklistEnabled: false,
	}
	user := model.User{Group: "default"}
	raw := map[string]json.RawMessage{
		"rpm_limit":         json.RawMessage("90"),
		"concurrency_limit": json.RawMessage("null"),
	}

	update, err := userPolicyFromMutation(user, raw, fallback)
	require.NoError(t, err)
	require.NotNil(t, update)
	require.NotNil(t, update.RpmLimit)
	assert.Equal(t, 90, *update.RpmLimit)
	assert.Nil(t, update.ConcurrencyLimit)
	require.NotNil(t, update.StreamTpsLimit)
	assert.Equal(t, 12, *update.StreamTpsLimit, "omitted fields must retain their previous values")

	audit := userRateLimitAudit(raw, update)
	assert.Equal(t, map[string]interface{}{"mode": userBatchCheckinCustom, "value": 90}, audit["rpm_limit"])
	assert.Equal(t, map[string]interface{}{"mode": userBatchRateLimitClear}, audit["concurrency_limit"])
	assert.NotContains(t, audit, "stream_tps_limit")
}

func TestValidateUserRateLimitBounds(t *testing.T) {
	assert.NoError(t, validateUserRateLimit("RPM", nil))
	assert.NoError(t, validateUserRateLimit("RPM", common.GetPointer(1)))
	assert.NoError(t, validateUserRateLimit("RPM", common.GetPointer(maxUserRateLimit)))
	assert.Error(t, validateUserRateLimit("RPM", common.GetPointer(0)))
	assert.Error(t, validateUserRateLimit("RPM", common.GetPointer(-1)))
}
