package model

import (
	"math"
	"testing"

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

func TestChannelSelectionRanksNativeBeforeConvertibleAcrossPriorityTiers(t *testing.T) {
	setupUserModelRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	const modelName = "protocol-tier-model"
	nativeHighPriority := int64(20)
	nativeLowPriority := int64(5)
	convertiblePriority := int64(100)
	incompatiblePriority := int64(200)
	require.NoError(t, DB.Create(&[]Channel{
		{Id: 21, Name: "native-high", Key: "key-native-high", Type: 1, Status: common.ChannelStatusEnabled, Priority: &nativeHighPriority},
		{Id: 22, Name: "native-low", Key: "key-native-low", Type: 1, Status: common.ChannelStatusEnabled, Priority: &nativeLowPriority},
		{Id: 23, Name: "convertible", Key: "key-convertible", Type: 1, Status: common.ChannelStatusEnabled, Priority: &convertiblePriority},
		{Id: 24, Name: "incompatible", Key: "key-incompatible", Type: 1, Status: common.ChannelStatusEnabled, Priority: &incompatiblePriority},
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: modelName, ChannelId: 21, Enabled: true, Priority: &nativeHighPriority, Weight: 10},
		{Group: "default", Model: modelName, ChannelId: 22, Enabled: true, Priority: &nativeLowPriority, Weight: 10},
		{Group: "default", Model: modelName, ChannelId: 23, Enabled: true, Priority: &convertiblePriority, Weight: 10},
		{Group: "default", Model: modelName, ChannelId: 24, Enabled: true, Priority: &incompatiblePriority, Weight: 10},
	}).Error)

	classifier := func(channel *Channel) ChannelCandidateClass {
		switch channel.Id {
		case 21, 22:
			return ChannelCandidateNative
		case 23:
			return ChannelCandidateConvertible
		default:
			return ChannelCandidateIncompatible
		}
	}

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "memory cache"}[memoryCacheEnabled], func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCacheEnabled
			if memoryCacheEnabled {
				InitChannelCache()
			}

			first, err := GetRandomSatisfiedChannelInPoolWithClassifier("default", modelName, 0, "", nil, nil, classifier)
			require.NoError(t, err)
			require.NotNil(t, first)
			assert.Equal(t, 21, first.Id)

			second, err := GetRandomSatisfiedChannelInPoolWithClassifier("default", modelName, 1, "", nil, nil, classifier)
			require.NoError(t, err)
			require.NotNil(t, second)
			assert.Equal(t, 22, second.Id)

			third, err := GetRandomSatisfiedChannelInPoolWithClassifier("default", modelName, 2, "", nil, nil, classifier)
			require.NoError(t, err)
			require.NotNil(t, third)
			assert.Equal(t, 23, third.Id)
		})
	}
}

func TestChannelSelectionKeepsWeightingInsideProtocolTier(t *testing.T) {
	zeroWeight := uint(0)
	positiveWeight := uint(100)
	priority := int64(10)
	channels := []*Channel{
		{Id: 31, Weight: &zeroWeight, Priority: &priority},
		{Id: 32, Weight: &positiveWeight, Priority: &priority},
		{Id: 33, Weight: &positiveWeight, Priority: &priority},
	}
	classifier := func(channel *Channel) ChannelCandidateClass {
		if channel.Id == 33 {
			return ChannelCandidateConvertible
		}
		return ChannelCandidateNative
	}

	tiers := buildChannelSelectionTiers(channels, classifier)
	require.Equal(t, []channelSelectionTier{
		{Class: ChannelCandidateNative, Priority: priority},
		{Class: ChannelCandidateConvertible, Priority: priority},
	}, tiers)

	for range 20 {
		selected := selectWeightedChannel(channels[:2])
		require.NotNil(t, selected)
		assert.Equal(t, 32, selected.Id)
	}
}

func TestChannelSelectionReportsWhenEveryCandidateIsProtocolIncompatible(t *testing.T) {
	setupUserModelRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	const modelName = "incompatible-protocol-model"
	priority := int64(10)
	require.NoError(t, DB.Create(&Channel{
		Id:       41,
		Name:     "incompatible",
		Key:      "key-incompatible",
		Type:     1,
		Status:   common.ChannelStatusEnabled,
		Priority: &priority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: 41,
		Enabled:   true,
		Priority:  &priority,
	}).Error)
	classifier := func(*Channel) ChannelCandidateClass {
		return ChannelCandidateIncompatible
	}

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "memory cache"}[memoryCacheEnabled], func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCacheEnabled
			if memoryCacheEnabled {
				InitChannelCache()
			}

			selected, err := GetRandomSatisfiedChannelInPoolWithClassifier(
				"default",
				modelName,
				0,
				"",
				nil,
				nil,
				classifier,
			)
			assert.Nil(t, selected)
			require.ErrorIs(t, err, ErrNoCompatibleChannel)
		})
	}
}
