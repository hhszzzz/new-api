package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupContextLimitTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Model{}, &Option{}, &Vendor{}))
	clear := func() {
		require.NoError(t, DB.Exec("DELETE FROM models").Error)
		require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
		require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	}
	clear()
	t.Cleanup(clear)
}

func TestUpdateWithTxPersistsContextLimit(t *testing.T) {
	setupContextLimitTables(t)

	record := &Model{ModelName: "ctx-persist-model", Status: 1, SyncOfficial: 1}
	require.NoError(t, record.Insert())

	record.ContextLimit = 272000
	require.NoError(t, record.Update())
	var reloaded Model
	require.NoError(t, DB.Where("model_name = ?", "ctx-persist-model").First(&reloaded).Error)
	assert.Equal(t, 272000, reloaded.ContextLimit)

	// Zero must persist too: clearing the declaration needs the explicit Select.
	record.ContextLimit = 0
	require.NoError(t, record.Update())
	require.NoError(t, DB.Where("model_name = ?", "ctx-persist-model").First(&reloaded).Error)
	assert.Equal(t, 0, reloaded.ContextLimit)
}

func TestGetModelContextLimits(t *testing.T) {
	setupContextLimitTables(t)

	seed := []Model{
		{ModelName: "ctx-exact", ContextLimit: 200000, Status: 1},
		{ModelName: "ctx-prefix", NameRule: NameRulePrefix, ContextLimit: 128000, Status: 1},
		{ModelName: "ctx-prefix-special", ContextLimit: 64000, Status: 1},
		{ModelName: "ctx-zero", ContextLimit: 0, Status: 1},
	}
	for i := range seed {
		require.NoError(t, DB.Create(&seed[i]).Error)
	}

	limits, err := GetModelContextLimits([]string{"ctx-exact", "ctx-prefix-special", "ctx-zero", "ctx-missing"})
	require.NoError(t, err)
	// Exact rule wins over the prefix rule for the same name.
	assert.Equal(t, map[string]int{"ctx-exact": 200000, "ctx-prefix-special": 64000}, limits)

	limits, err = GetModelContextLimits([]string{"ctx-prefix-other"})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"ctx-prefix-other": 128000}, limits)

	limits, err = GetModelContextLimits(nil)
	require.NoError(t, err)
	assert.Empty(t, limits)
}

func TestGetChannelContextLimitFloor(t *testing.T) {
	setupContextLimitTables(t)

	withSettings := func(channelID int, rules ...hostdto.ChannelContextLimitRule) *Channel {
		channel := &Channel{
			Id:     channelID,
			Type:   1,
			Key:    "key",
			Name:   "channel",
			Status: common.ChannelStatusEnabled,
		}
		channel.SetOtherSettings(hostdto.ChannelOtherSettings{ContextLimits: rules})
		return channel
	}
	require.NoError(t, DB.Create(withSettings(11,
		hostdto.ChannelContextLimitRule{ModelPattern: `^floor-model$`, ContextLimit: 256000},
	)).Error)
	require.NoError(t, DB.Create(withSettings(12,
		hostdto.ChannelContextLimitRule{ModelPattern: `^floor-`, ContextLimit: 128000},
	)).Error)
	disabled := withSettings(13,
		hostdto.ChannelContextLimitRule{ModelPattern: `^floor-model$`, ContextLimit: 64000},
	)
	disabled.Status = common.ChannelStatusManuallyDisabled
	require.NoError(t, DB.Create(disabled).Error)

	abilities := []Ability{
		{Group: "default", Model: "floor-model", ChannelId: 11, Enabled: true},
		{Group: "default", Model: "floor-model", ChannelId: 12, Enabled: true},
		{Group: "default", Model: "floor-model", ChannelId: 13, Enabled: true},
		{Group: "vip", Model: "floor-model", ChannelId: 11, Enabled: true},
		{Group: "default", Model: "floor-other", ChannelId: 12, Enabled: false},
	}
	for i := range abilities {
		require.NoError(t, DB.Create(&abilities[i]).Error)
	}

	// The smallest matching declaration wins; the disabled channel is ignored.
	floors, err := GetChannelContextLimitFloor([]string{"floor-model"}, []string{"default"})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"floor-model": 128000}, floors)

	// Group filtering narrows the channels under consideration.
	floors, err = GetChannelContextLimitFloor([]string{"floor-model"}, []string{"vip"})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"floor-model": 256000}, floors)

	// Disabled abilities and unmatched rules declare nothing.
	floors, err = GetChannelContextLimitFloor([]string{"floor-other", "floor-missing"}, nil)
	require.NoError(t, err)
	assert.Empty(t, floors)
}

func TestModelContextLimitNullScansAsZero(t *testing.T) {
	setupContextLimitTables(t)

	// Rows that predate the column hold NULL; reads must not fail.
	require.NoError(t, DB.Exec("INSERT INTO models (model_name, status, sync_official, name_rule, created_time, updated_time) VALUES ('ctx-legacy-null', 1, 1, 0, 1, 1)").Error)
	require.NoError(t, DB.Exec("UPDATE models SET context_limit = NULL WHERE model_name = 'ctx-legacy-null'").Error)

	var reloaded Model
	require.NoError(t, DB.Where("model_name = ?", "ctx-legacy-null").First(&reloaded).Error)
	assert.Equal(t, 0, reloaded.ContextLimit)

	limits, err := GetModelContextLimits([]string{"ctx-legacy-null"})
	require.NoError(t, err)
	assert.Empty(t, limits)
}

func TestGetModelContextLimitUsesPricingCache(t *testing.T) {
	setupContextLimitTables(t)
	t.Cleanup(InvalidatePricingCache)

	require.NoError(t, DB.Create(&Channel{
		Id: 41, Type: 1, Key: "key", Name: "ctx-cache-channel", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "ctx-cache-model", ChannelId: 41, Enabled: true}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "ctx-cache-model", ContextLimit: 123456, Status: 1}).Error)

	RefreshPricing()
	limit, ok := GetModelContextLimit("ctx-cache-model")
	require.True(t, ok)
	assert.Equal(t, 123456, limit)

	// Undeclared and unknown models resolve to nothing.
	require.NoError(t, DB.Create(&Model{ModelName: "ctx-cache-zero", ContextLimit: 0, Status: 1}).Error)
	RefreshPricing()
	limit, ok = GetModelContextLimit("ctx-cache-zero")
	assert.False(t, ok)
	assert.Equal(t, 0, limit)

	limit, ok = GetModelContextLimit("ctx-cache-missing")
	assert.False(t, ok)
	assert.Equal(t, 0, limit)

	limit, ok = GetModelContextLimit("")
	assert.False(t, ok)
	assert.Equal(t, 0, limit)
}
