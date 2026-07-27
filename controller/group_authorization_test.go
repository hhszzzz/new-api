package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userGroupsResponse struct {
	Success bool                              `json:"success"`
	Data    map[string]map[string]interface{} `json:"data"`
}

func getUserGroupsResponse(t *testing.T, userId int) userGroupsResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/user/groups", nil)
	if userId > 0 {
		context.Set("id", userId)
	}

	GetUserGroups(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response userGroupsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	return response
}

func configureGroupAuthorizationTest(t *testing.T) {
	t.Helper()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(
		`{"default":"Default","vip":"VIP","staff":"Staff"}`,
	))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip","default"]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"default":1,"vip":0.8,"staff":1}`,
	))
}

func TestGetUserGroupsReflectsAssignmentAndRevocation(t *testing.T) {
	configureGroupAuthorizationTest(t)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.UserGroupMembership{}))
	user := &model.User{
		Username: "assigned-group-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)
	manual := true
	memberships := []model.UserGroupMembership{
		{UserId: user.Id, GroupName: "default", SortOrder: 0, Manual: &manual},
		{UserId: user.Id, GroupName: "vip", SortOrder: 1, Manual: &manual},
	}
	require.NoError(t, db.Create(&memberships).Error)

	assigned := getUserGroupsResponse(t, user.Id)
	assert.Contains(t, assigned.Data, "default")
	assert.Contains(t, assigned.Data, "vip")
	assert.Contains(t, assigned.Data, "auto")
	assert.NotContains(t, assigned.Data, "staff")

	require.NoError(t, db.Delete(&memberships[1]).Error)
	revoked := getUserGroupsResponse(t, user.Id)
	assert.Contains(t, revoked.Data, "default")
	assert.NotContains(t, revoked.Data, "vip")
	assert.NotContains(t, revoked.Data, "staff")
}

func TestGetUserGroupsUsesDefaultAssignmentForAnonymousRequests(t *testing.T) {
	configureGroupAuthorizationTest(t)

	response := getUserGroupsResponse(t, 0)

	assert.Contains(t, response.Data, "default")
	assert.Contains(t, response.Data, "auto")
	assert.NotContains(t, response.Data, "vip")
	assert.NotContains(t, response.Data, "staff")
}
