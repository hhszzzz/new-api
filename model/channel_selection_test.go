package model

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

func TestChannelSelectionOrdersTiersByPriorityNumberOnly(t *testing.T) {
	setupUserModelRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	const modelName = "protocol-tier-model"
	convertibleHighPriority := int64(100)
	nativeMidPriority := int64(20)
	nativeLowPriority := int64(5)
	incompatiblePriority := int64(200)
	require.NoError(t, DB.Create(&[]Channel{
		{Id: 23, Name: "convertible-high", Key: "key-convertible", Type: 1, Status: common.ChannelStatusEnabled, Priority: &convertibleHighPriority},
		{Id: 21, Name: "native-mid", Key: "key-native-high", Type: 1, Status: common.ChannelStatusEnabled, Priority: &nativeMidPriority},
		{Id: 22, Name: "native-low", Key: "key-native-low", Type: 1, Status: common.ChannelStatusEnabled, Priority: &nativeLowPriority},
		{Id: 24, Name: "incompatible", Key: "key-incompatible", Type: 1, Status: common.ChannelStatusEnabled, Priority: &incompatiblePriority},
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: modelName, ChannelId: 23, Enabled: true, Priority: &convertibleHighPriority, Weight: 10},
		{Group: "default", Model: modelName, ChannelId: 21, Enabled: true, Priority: &nativeMidPriority, Weight: 10},
		{Group: "default", Model: modelName, ChannelId: 22, Enabled: true, Priority: &nativeLowPriority, Weight: 10},
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

			// A convertible channel with a higher priority number outranks
			// native channels: ordering follows the configured priority alone.
			first, err := GetRandomSatisfiedChannelInPoolWithClassifier("default", modelName, 0, "", nil, nil, classifier)
			require.NoError(t, err)
			require.NotNil(t, first)
			assert.Equal(t, 23, first.Id)

			second, err := GetRandomSatisfiedChannelInPoolWithClassifier("default", modelName, 1, "", nil, nil, classifier)
			require.NoError(t, err)
			require.NotNil(t, second)
			assert.Equal(t, 21, second.Id)

			third, err := GetRandomSatisfiedChannelInPoolWithClassifier("default", modelName, 2, "", nil, nil, classifier)
			require.NoError(t, err)
			require.NotNil(t, third)
			assert.Equal(t, 22, third.Id)
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
		{Priority: priority},
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

func TestChannelSelectionReportsWhenEveryCandidateRejectsRequestPath(t *testing.T) {
	setupUserModelRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	const modelName = "path-gated-model"
	const unmatchedModel = "path-gated-unmatched-model"
	priority := int64(10)
	// The Advanced Custom channel routes chat completions only, so an
	// image-generation request matches no route and the channel is unusable.
	require.NoError(t, DB.Create(&Channel{
		Id:            51,
		Name:          "advanced-custom-chat-only",
		Key:           "key-advanced",
		Type:          constant.ChannelTypeAdvancedCustom,
		Status:        common.ChannelStatusEnabled,
		Models:        modelName,
		Group:         "default",
		Priority:      &priority,
		OtherSettings: `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/v1/chat/completions","target_protocol":"native","auth":{"type":"header","name":"Authorization","value":"Bearer {api_key}"}}]}}`,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: 51,
		Enabled:   true,
		Priority:  &priority,
	}).Error)

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "memory cache"}[memoryCacheEnabled], func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCacheEnabled
			if memoryCacheEnabled {
				InitChannelCache()
			}

			selected, err := GetRandomSatisfiedChannelInPoolWithClassifier(
				"default", modelName, 0, "/v1/images/generations", nil, nil, nil,
			)
			assert.Nil(t, selected)
			require.ErrorIs(t, err, ErrNoChannelSupportsRequestPath)

			// A path the channel routes still selects the channel normally.
			selected, err = GetRandomSatisfiedChannelInPoolWithClassifier(
				"default", modelName, 0, "/v1/chat/completions", nil, nil, nil,
			)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, 51, selected.Id)

			// Without any ability for the model the plain empty result is unchanged.
			selected, err = GetRandomSatisfiedChannelInPoolWithClassifier(
				"default", unmatchedModel, 0, "/v1/images/generations", nil, nil, nil,
			)
			require.NoError(t, err)
			assert.Nil(t, selected)
		})
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
