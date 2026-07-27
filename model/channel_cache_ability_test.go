package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestInitChannelCachePreservesPreviousSnapshotWhenAggregateHydrationFails(t *testing.T) {
	resetPricingEndpointTestTables(t)
	require.NoError(t, DB.AutoMigrate(&ChannelAggregate{}))
	aggregate := &ChannelAggregate{Name: "cached aggregate", BaseURL: "https://shared.example.com"}
	require.NoError(t, DB.Create(aggregate).Error)
	const channelID = 7402
	channel := &Channel{
		Id:                      channelID,
		Type:                    constant.ChannelTypeOpenAI,
		Key:                     "cache-preservation-key",
		Name:                    "last known good",
		Status:                  common.ChannelStatusEnabled,
		AggregateId:             &aggregate.Id,
		InheritAggregateBaseURL: true,
	}
	require.NoError(t, DB.Create(channel).Error)
	InitChannelCache()

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channelID).Update("name", "partial refresh").Error)
	callbackName := "test:fail-channel-aggregate-hydration"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "channel_aggregates" {
			tx.AddError(errors.New("forced aggregate hydration failure"))
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(callbackName))
	})

	InitChannelCache()
	cached, err := CacheGetChannel(channelID)
	require.NoError(t, err)
	assert.Equal(t, "last known good", cached.Name)
	assert.Equal(t, aggregate.BaseURL, cached.GetBaseURL())
}
