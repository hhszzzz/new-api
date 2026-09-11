package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanChannelAbilities keeps this file's cases independent of the shared
// in-memory service test database.
func cleanChannelAbilities(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM abilities")
	})
	require.NoError(t, model.DB.Exec("DELETE FROM abilities").Error)
}

func seedDisableModelChannel(
	t *testing.T,
	id int,
	otherSettings string,
	channelInfo model.ChannelInfo,
) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Id:            id,
		Name:          "model-disable",
		Key:           "sk-test",
		Status:        common.ChannelStatusEnabled,
		Models:        "m1,m2",
		Group:         "default",
		ChannelInfo:   channelInfo,
		OtherSettings: otherSettings,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))
	return channel
}

func abilityEnabled(t *testing.T, channelId int, modelName string) bool {
	t.Helper()
	var row model.Ability
	require.NoError(t, model.DB.Where(
		"channel_id = ? AND model = ?", channelId, modelName,
	).First(&row).Error)
	return row.Enabled
}

// The relay error path resolves the failing (group, model) in the gin context
// and calls DisableChannelOrModel. Group scoping of the ability rows itself is
// covered by the model package tests; this exercises the service policy branch.
func TestDisableChannelOrModelDisablesOnlyTheFailingModel(t *testing.T) {
	truncate(t)
	cleanChannelAbilities(t)

	channel := seedDisableModelChannel(
		t, 9001, `{"disable_model_on_error":true}`, model.ChannelInfo{},
	)

	DisableChannelOrModel(types.ChannelError{
		ChannelId:   channel.Id,
		ChannelName: channel.Name,
		UsingKey:    "sk-test",
		AutoBan:     true,
	}, "", "m1", "upstream 401")

	reloaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status,
		"the channel must stay usable for its other models")

	entries := reloaded.GetDisabledModels()
	require.Len(t, entries, 1)
	assert.Equal(t, "m1", entries[0].Model)
	assert.Equal(t, dto.DisabledModelSourceAuto, entries[0].Source)

	assert.False(t, abilityEnabled(t, channel.Id, "m1"))
	assert.True(t, abilityEnabled(t, channel.Id, "m2"))
}

func TestDisableChannelOrModelFallsBackToChannelDisableWhenFlagOff(t *testing.T) {
	truncate(t)
	cleanChannelAbilities(t)

	channel := seedDisableModelChannel(t, 9002, `{}`, model.ChannelInfo{})

	DisableChannelOrModel(types.ChannelError{
		ChannelId:   channel.Id,
		ChannelName: channel.Name,
		UsingKey:    "sk-test",
		AutoBan:     true,
	}, "", "m1", "upstream 401")

	reloaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.Status)
	assert.Empty(t, reloaded.GetDisabledModels())
}

func TestDisableChannelOrModelIgnoresMultiKeyChannels(t *testing.T) {
	truncate(t)
	cleanChannelAbilities(t)

	channel := seedDisableModelChannel(
		t, 9003, `{"disable_model_on_error":true}`,
		model.ChannelInfo{IsMultiKey: true, MultiKeySize: 1, MultiKeyMode: "random"},
	)

	DisableChannelOrModel(types.ChannelError{
		ChannelId:   channel.Id,
		ChannelName: channel.Name,
		UsingKey:    "sk-test",
		AutoBan:     true,
	}, "", "m1", "upstream 401")

	reloaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Empty(t, reloaded.GetDisabledModels(),
		"multi-key channels keep their key-level handling")
	require.Len(t, reloaded.ChannelInfo.MultiKeyStatusList, 1)
	assert.Equal(t, common.ChannelStatusAutoDisabled,
		reloaded.ChannelInfo.MultiKeyStatusList[0])
}
