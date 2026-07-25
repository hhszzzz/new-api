package middleware

import (
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

func TestResolveAffinitySelectionGroupPreservesAutoGroupSelection(t *testing.T) {
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousAutoGroups := setting.AutoGroups2JsonString()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["premium","default"]`))
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		if previousMemoryCacheEnabled && previousDB != nil {
			model.InitChannelCache()
		}
	})

	require.NoError(t, db.Create(&model.Channel{
		Id:     17,
		Key:    "affinity-test-key",
		Name:   "affinity-test-channel",
		Status: common.ChannelStatusEnabled,
		Models: "gpt-5.4",
		Group:  "default",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: 17,
		Enabled:   true,
	}).Error)
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroups, []string{"premium", "default"})

	group, usable := resolveAffinitySelectionGroup(ctx, "auto", "gpt-5.4", 17)

	assert.True(t, usable)
	assert.Equal(t, "default", group)

	group, usable = resolveAffinitySelectionGroup(ctx, "premium", "gpt-5.4", 17)
	assert.False(t, usable)
	assert.Equal(t, "premium", group)
}
