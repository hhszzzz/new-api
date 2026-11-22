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
	assert.Contains(t, unassigned, "auto")
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

func TestGetAuthorizedUserGroupsAppliesPrimaryGroupRules(t *testing.T) {
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	rules := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	previousRules := rules.ReadAll()
	clonedRules := make(map[string]map[string]string, len(previousRules))
	for primary, values := range previousRules {
		clonedRules[primary] = make(map[string]string, len(values))
		for target, description := range values {
			clonedRules[primary][target] = description
		}
	}
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		rules.Clear()
		rules.AddAll(clonedRules)
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"vip":"VIP","partner":"Partner","hidden":"Hidden"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1,"partner":1,"hidden":1}`))
	rules.Clear()
	rules.Set("vip", map[string]string{
		"+:partner": "Inherited partner access",
		"-:hidden":  "",
	})

	groups := GetAuthorizedUserGroups([]string{"vip"})

	assert.Equal(t, "Inherited partner access", groups["partner"])
	assert.NotContains(t, groups, "hidden")
	assert.Contains(t, groups, "auto")

	withExplicitHidden := GetAuthorizedUserGroups([]string{"vip", "hidden"})
	assert.Equal(t, "Hidden", withExplicitHidden["hidden"])
}
