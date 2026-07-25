package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryChannelCacheExcludesDisabledAbility(t *testing.T) {
	resetPricingEndpointTestTables(t)
	const channelID = 7401
	const modelName = "disabled-ability-model"

	channel := &Channel{
		Id:       channelID,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "disabled-ability-key",
		Name:     "disabled-ability-channel",
		Status:   common.ChannelStatusEnabled,
		Models:   modelName,
		Group:    "default",
		Priority: common.GetPointer[int64](10),
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   false,
		Priority:  common.GetPointer[int64](10),
	}).Error)

	InitChannelCache()
	selected, err := GetRandomSatisfiedChannel("default", modelName, 0, "")
	require.NoError(t, err)
	assert.Nil(t, selected)

	require.NoError(t, DB.Model(&Ability{}).
		Where("channel_id = ?", channelID).
		Update("enabled", true).Error)
	InitChannelCache()
	selected, err = GetRandomSatisfiedChannel("default", modelName, 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, channelID, selected.Id)
}
