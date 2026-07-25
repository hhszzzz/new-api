package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserModelRouteApplicableGroupsIncludesAuthorizedRequestGroups(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	originalSpecialGroups := specialGroups.ReadAll()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecialGroups)
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"auto":"Auto","default":"Default","vip":"VIP","retired":"Retired"}`,
	))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"default":1,"vip":1,"internal":1}`,
	))
	specialGroups.Clear()

	groups := getUserModelRouteApplicableGroups([]string{"default"})

	assert.Equal(t, []string{"default", "vip"}, groups)
}
