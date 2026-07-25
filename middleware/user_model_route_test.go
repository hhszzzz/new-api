package middleware

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplyUserModelRouteUsesActualRequestGroup(t *testing.T) {
	previousDB := model.DB
	previousAutoGroups := setting.AutoGroups2JsonString()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "user-model-route.db")), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserModelRoute{},
		&model.UserModelRouteGroup{},
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

	newContext := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(nil)
		ctx.Set("id", 1)
		ctx.Set("role", common.RoleCommonUser)
		common.SetContextKey(ctx, constant.ContextKeyUserGroups, []string{"default", "vip"})
		return ctx
	}

	normalContext := newContext()
	route, err := applyUserModelRoute(normalContext, "gpt-5.4", "default")
	require.NoError(t, err)
	assert.Nil(t, route)
	assert.Zero(t, common.GetContextKeyInt(normalContext, constant.ContextKeyUserModelRouteId))

	autoContext := newContext()
	route, err = applyUserModelRoute(autoContext, "gpt-5.4", "auto")
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, "gpt-5.5", route.TargetModel)
	assert.Equal(t, "internal", common.GetContextKeyString(autoContext, constant.ContextKeyAutoGroup))
}
