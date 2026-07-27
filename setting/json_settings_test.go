package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidJSONPreservesLegacySettings(t *testing.T) {
	originalChats := Chats2JsonString()
	originalAutoGroups := AutoGroups2JsonString()
	originalUserUsableGroups := UserUsableGroups2JSONString()
	originalRateLimits := ModelRequestRateLimitGroup2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateChatsByJsonString(originalChats))
		require.NoError(t, UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, UpdateUserUsableGroupsByJSONString(originalUserUsableGroups))
		require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(originalRateLimits))
	})

	require.NoError(t, UpdateChatsByJsonString(`[{"name":"before"}]`))
	require.NoError(t, UpdateAutoGroupsByJsonString(`["before"]`))
	require.NoError(t, UpdateUserUsableGroupsByJSONString(`{"before":"Before"}`))
	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(`{"before":[10,2]}`))

	require.Error(t, UpdateChatsByJsonString(`{`))
	require.Error(t, UpdateAutoGroupsByJsonString(`{`))
	require.Error(t, UpdateUserUsableGroupsByJSONString(`{`))
	require.Error(t, UpdateModelRequestRateLimitGroupByJSONString(`{`))

	assert.JSONEq(t, `[{"name":"before"}]`, Chats2JsonString())
	assert.JSONEq(t, `["before"]`, AutoGroups2JsonString())
	assert.JSONEq(t, `{"before":"Before"}`, UserUsableGroups2JSONString())
	assert.JSONEq(t, `{"before":[10,2]}`, ModelRequestRateLimitGroup2JSONString())
}
