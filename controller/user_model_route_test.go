package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserModelRouteApplicableGroupsIncludesAuthorizedRequestGroups(t *testing.T) {
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"default":1,"vip":1,"internal":1}`,
	))

	groups := getUserModelRouteApplicableGroups([]string{"default", "vip"})

	assert.Equal(t, []string{"default", "vip"}, groups)
}
