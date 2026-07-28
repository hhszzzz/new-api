package model

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSelectionHandlesLargeWeightsWithAndWithoutCache(t *testing.T) {
	setupUserModelRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	const modelName = "large-weight-model"
	priority := int64(10)
	weight := uint(math.MaxInt64)
	require.NoError(t, DB.Create(&[]Channel{
		{Id: 10, Name: "large-a", Key: "key-a", Type: 1, Status: common.ChannelStatusEnabled, Weight: &weight},
		{Id: 11, Name: "large-b", Key: "key-b", Type: 1, Status: common.ChannelStatusEnabled, Weight: &weight},
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: modelName, ChannelId: 10, Enabled: true, Priority: &priority, Weight: weight},
		{Group: "default", Model: modelName, ChannelId: 11, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)

	for _, memoryCacheEnabled := range []bool{false, true} {
		common.MemoryCacheEnabled = memoryCacheEnabled
		if memoryCacheEnabled {
			InitChannelCache()
		}

		selected, err := GetRandomSatisfiedChannel("default", modelName, 0, "")
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Contains(t, []int{10, 11}, selected.Id)
	}
}

func TestChannelSelectionExcludesScheduledDisabledChannelsWithAndWithoutCache(t *testing.T) {
	setupUserModelRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	const modelName = "scheduled-gate-model"
	priority := int64(10)
	future := time.Now().Add(time.Hour).Unix()
	require.NoError(t, DB.Create(&Channel{
		Id:       12,
		Name:     "scheduled-disabled",
		Key:      "key-scheduled",
		Type:     1,
		Status:   common.ChannelStatusEnabled,
		Models:   modelName,
		Group:    "default",
		Priority: &priority,
		Schedule: ChannelSchedule{StartsAt: &future},
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: 12,
		Enabled:   true,
		Priority:  &priority,
	}).Error)

	for _, memoryCacheEnabled := range []bool{false, true} {
		common.MemoryCacheEnabled = memoryCacheEnabled
		if memoryCacheEnabled {
			InitChannelCache()
		}
		selected, err := GetRandomSatisfiedChannel("default", modelName, 0, "")
		require.NoError(t, err)
		assert.Nil(t, selected)
	}
}
