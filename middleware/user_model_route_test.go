package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserModelRouteResolutionTest(t *testing.T) func(role int) *gin.Context {
	t.Helper()
	previousDB := model.DB
	previousAutoGroups := setting.AutoGroups2JsonString()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "user-model-route.db")), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserModelRoute{},
		&model.UserModelRouteGroup{},
		&model.UserModelRouteExecutionGroup{},
		&model.UserModelRouteChannel{},
	))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "route-user", Group: "default"}).Error)
	require.NoError(t, model.SaveUserModelRoute(&model.UserModelRoute{
		UserId:         1,
		SourceModel:    "gpt-5.4",
		TargetModel:    "gpt-5.5",
		Groups:         []string{"vip"},
		ExecutionGroup: "internal",
		Enabled:        true,
		ChannelIds:     []int{10},
	}))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip","default"]`))
	t.Cleanup(func() {
		model.DB = previousDB
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
	})

	return func(role int) *gin.Context {
		ctx, _ := gin.CreateTestContext(nil)
		ctx.Set("id", 1)
		ctx.Set("role", role)
		common.SetContextKey(ctx, constant.ContextKeyUserGroups, []string{"default", "vip"})
		return ctx
	}
}

func TestApplyUserModelRouteUsesActualRequestGroup(t *testing.T) {
	newContext := setupUserModelRouteResolutionTest(t)

	normalContext := newContext(common.RoleCommonUser)
	route, err := applyUserModelRoute(normalContext, "gpt-5.4", "default")
	require.NoError(t, err)
	assert.Nil(t, route)
	assert.Zero(t, common.GetContextKeyInt(normalContext, constant.ContextKeyUserModelRouteId))

	autoContext := newContext(common.RoleCommonUser)
	route, err = applyUserModelRoute(autoContext, "gpt-5.4", "auto")
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, "gpt-5.5", route.TargetModel)
	assert.Equal(t, "internal", common.GetContextKeyString(autoContext, constant.ContextKeyAutoGroup))
}

func TestApplyUserModelRouteDoesNotBypassRootUser(t *testing.T) {
	newContext := setupUserModelRouteResolutionTest(t)
	ctx := newContext(common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5.4")

	route, err := applyUserModelRoute(ctx, "gpt-5.4", "auto")

	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, "gpt-5.5", common.GetContextKeyString(ctx, constant.ContextKeyUserModelRouteTarget))
	assert.Equal(t, route.Id, common.GetContextKeyInt(ctx, constant.ContextKeyUserModelRouteId))

	request := &dto.GeneralOpenAIRequest{Model: "gpt-5.4"}
	info := relaycommon.GenRelayInfoOpenAI(ctx, request)
	require.NoError(t, relayhelper.ModelMappedHelper(ctx, info, request))
	assert.Equal(t, route.Id, info.UserModelRouteId)
	assert.Equal(t, "gpt-5.5", info.RouteTargetModelName)
	assert.Equal(t, "gpt-5.5", request.Model)
	assert.True(t, info.IsModelMapped)
}

func TestApplyUserModelRouteKeepsMultipleExecutionGroupsUnresolvedUntilSelection(t *testing.T) {
	newContext := setupUserModelRouteResolutionTest(t)
	routes, err := model.GetUserModelRoutes(1)
	require.NoError(t, err)
	require.Len(t, routes, 1)
	routes[0].ExecutionGroups = []string{"internal", "backup"}
	require.NoError(t, model.SaveUserModelRoute(routes[0]))

	ctx := newContext(common.RoleCommonUser)
	route, err := applyUserModelRoute(ctx, "gpt-5.4", "auto")
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, []string{"internal", "backup"}, routeSelectionExecutionGroups(ctx))
	assert.Equal(t, "auto", routeSelectionGroup(ctx, "auto"))
	assert.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))

	commitRouteSelectionGroup(ctx, "auto", "backup")
	assert.Equal(t, "backup", common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
	assert.Equal(t, "backup", common.GetContextKeyString(ctx, constant.ContextKeyUserModelRouteGroup))
}
