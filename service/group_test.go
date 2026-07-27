package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAuthorizedUserGroupsUsesAdminMembershipsAsOnlySource(t *testing.T) {
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousAutoGroups := setting.AutoGroups2JsonString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"default":"Default description","vip":"VIP description","staff":"Staff description"}`,
	))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"default":1,"vip":0.8,"staff":1}`,
	))

	assigned := GetAuthorizedUserGroups([]string{"vip"})
	assert.Equal(t, "VIP description", assigned["vip"])
	assert.Contains(t, assigned, "auto")
	assert.NotContains(t, assigned, "default")
	assert.NotContains(t, assigned, "staff")

	unassigned := GetAuthorizedUserGroups([]string{"staff"})
	assert.Equal(t, "Staff description", unassigned["staff"])
	assert.NotContains(t, unassigned, "vip")
	assert.NotContains(t, unassigned, "default")
	assert.NotContains(t, unassigned, "auto")
}

func TestGetAuthorizedUserGroupsFallsBackToAssignedGroupName(t *testing.T) {
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":0.8}`))

	groups := GetAuthorizedUserGroups([]string{"vip"})

	assert.Equal(t, "vip", groups["vip"])
	assert.NotContains(t, groups, "default")
}
