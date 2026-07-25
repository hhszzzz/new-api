package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserUsableGroupsDoesNotRestoreRemovedMembership(t *testing.T) {
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	previousSpecialGroups := specialGroups.ReadAll()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	specialGroups.Clear()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		specialGroups.Clear()
		specialGroups.AddAll(previousSpecialGroups)
	})

	groups := GetUserUsableGroupsForGroups([]string{"vip"})

	assert.NotContains(t, groups, "vip")
	assert.Equal(t, "Default", groups["default"])
}
