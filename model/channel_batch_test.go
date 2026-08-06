package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchUpdateChannelsUpdatesFieldsAndRebuildsAbilities(t *testing.T) {
	resetPricingEndpointTestTables(t)
	priority := int64(1)
	weight := uint(1)
	channels := []Channel{
		{Id: 8101, Name: "batch-one", Key: "key-one", Type: 1, Status: common.ChannelStatusEnabled, Models: "model-a", Group: "default", Priority: &priority, Weight: &weight},
		{Id: 8102, Name: "batch-two", Key: "key-two", Type: 1, Status: common.ChannelStatusEnabled, Models: "model-a", Group: "default", Priority: &priority, Weight: &weight},
	}
	require.NoError(t, DB.Create(&channels).Error)
	for index := range channels {
		require.NoError(t, channels[index].AddAbilities(nil))
	}
	InitChannelCache()
	assert.Empty(t, GetModelSupportEndpointTypes("model-b"))

	startsAt := time.Now().Add(time.Hour).Unix()
	updated, err := BatchUpdateChannels([]int{8102, 8101, 8101}, ChannelBatchUpdate{
		Group:        &ChannelBatchListUpdate{Mode: ChannelBatchListAdd, Values: []string{"premium", "default"}},
		Models:       &ChannelBatchListUpdate{Mode: ChannelBatchListAdd, Values: []string{"model-b"}},
		Priority:     &ChannelBatchInt64Update{Value: 10},
		Weight:       &ChannelBatchUintUpdate{Value: 25},
		Tag:          &ChannelBatchStringUpdate{Value: "batch-tag"},
		AutoBan:      &ChannelBatchIntUpdate{Value: 0},
		StartsAt:     &ChannelBatchTimestampUpdate{Value: &startsAt},
		ModelMapping: &ChannelBatchStringUpdate{Value: `{"model-a":"upstream-a"}`},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, updated)

	var stored []Channel
	require.NoError(t, DB.Where("id IN ?", []int{8101, 8102}).Order("id ASC").Find(&stored).Error)
	require.Len(t, stored, 2)
	for _, channel := range stored {
		assert.Equal(t, "default,premium", channel.Group)
		assert.Equal(t, "model-a,model-b", channel.Models)
		assert.Equal(t, int64(10), channel.GetPriority())
		assert.Equal(t, uint(25), channel.GetWeight())
		assert.Equal(t, "batch-tag", channel.GetTag())
		require.NotNil(t, channel.AutoBan)
		assert.Equal(t, 0, *channel.AutoBan)
		require.NotNil(t, channel.Schedule.StartsAt)
		assert.Equal(t, startsAt, *channel.Schedule.StartsAt)
	}
	cached, err := CacheGetChannel(8101)
	require.NoError(t, err)
	require.NotNil(t, cached)
	assert.Equal(t, int64(10), cached.GetPriority())
	assert.Equal(t, "batch-tag", cached.GetTag())
	require.NotNil(t, cached.Schedule.StartsAt)
	assert.Equal(t, startsAt, *cached.Schedule.StartsAt)
	assert.Contains(t, GetModelSupportEndpointTypes("model-b"), constant.EndpointTypeOpenAI)

	var abilityCount int64
	require.NoError(t, DB.Model(&Ability{}).
		Where("channel_id IN ?", []int{8101, 8102}).
		Count(&abilityCount).Error)
	assert.Equal(t, int64(8), abilityCount)
}

func TestBatchUpdateChannelsRollsBackWhenOneTargetWouldBecomeInvalid(t *testing.T) {
	resetPricingEndpointTestTables(t)
	channels := []Channel{
		{Id: 8111, Name: "rollback-one", Key: "key-one", Type: 1, Status: common.ChannelStatusEnabled, Models: "model-a,model-b", Group: "default"},
		{Id: 8112, Name: "rollback-two", Key: "key-two", Type: 1, Status: common.ChannelStatusEnabled, Models: "model-b", Group: "default"},
	}
	require.NoError(t, DB.Create(&channels).Error)
	for index := range channels {
		require.NoError(t, channels[index].AddAbilities(nil))
	}

	updated, err := BatchUpdateChannels([]int{8111, 8112}, ChannelBatchUpdate{
		Models: &ChannelBatchListUpdate{Mode: ChannelBatchListRemove, Values: []string{"model-b"}},
	})
	require.ErrorContains(t, err, "would leave the field empty")
	assert.Zero(t, updated)

	var stored []Channel
	require.NoError(t, DB.Where("id IN ?", []int{8111, 8112}).Order("id ASC").Find(&stored).Error)
	require.Len(t, stored, 2)
	assert.Equal(t, "model-a,model-b", stored[0].Models)
	assert.Equal(t, "model-b", stored[1].Models)
}

func TestBatchUpdateChannelsPreservesUnselectedScheduleFields(t *testing.T) {
	resetPricingEndpointTestTables(t)
	expiresAt := time.Now().Add(48 * time.Hour).Unix()
	channel := Channel{
		Id:     8121,
		Name:   "schedule-partial",
		Key:    "key",
		Type:   1,
		Status: common.ChannelStatusEnabled,
		Models: "model-a",
		Group:  "default",
		Schedule: ChannelSchedule{
			ExpiresAt: &expiresAt,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	startsAt := time.Now().Add(time.Hour).Unix()
	_, err := BatchUpdateChannels([]int{8121}, ChannelBatchUpdate{
		StartsAt: &ChannelBatchTimestampUpdate{Value: &startsAt},
	})
	require.NoError(t, err)

	var stored Channel
	require.NoError(t, DB.First(&stored, "id = ?", 8121).Error)
	require.NotNil(t, stored.Schedule.StartsAt)
	require.NotNil(t, stored.Schedule.ExpiresAt)
	assert.Equal(t, startsAt, *stored.Schedule.StartsAt)
	assert.Equal(t, expiresAt, *stored.Schedule.ExpiresAt)
}

func TestBatchUpdateChannelsRejectsNullModelMapping(t *testing.T) {
	resetPricingEndpointTestTables(t)
	existingMapping := `{"model-a":"upstream-a"}`
	channel := Channel{
		Id:           8131,
		Name:         "mapping-validation",
		Key:          "key",
		Type:         1,
		Status:       common.ChannelStatusEnabled,
		Models:       "model-a",
		Group:        "default",
		ModelMapping: &existingMapping,
	}
	require.NoError(t, DB.Create(&channel).Error)

	updated, err := BatchUpdateChannels([]int{channel.Id}, ChannelBatchUpdate{
		ModelMapping: &ChannelBatchStringUpdate{Value: "null"},
	})
	require.ErrorContains(t, err, "must be a JSON object")
	assert.Zero(t, updated)

	var stored Channel
	require.NoError(t, DB.First(&stored, "id = ?", channel.Id).Error)
	require.NotNil(t, stored.ModelMapping)
	assert.Equal(t, existingMapping, *stored.ModelMapping)
}

func TestBatchUpdateChannelsMergesClientPolicyAndUpstreamSettings(t *testing.T) {
	resetPricingEndpointTestTables(t)
	channel := Channel{
		Id:            8141,
		Name:          "settings-merge",
		Key:           "key",
		Type:          1,
		Status:        common.ChannelStatusEnabled,
		Models:        "model-a",
		Group:         "default",
		OtherSettings: `{"allow_service_tier":true,"future_setting":{"enabled":true},"upstream_model_update_last_check_time":123,"upstream_model_update_last_detected_models":["new-model"],"upstream_model_update_last_removed_models":["old-model"]}`,
	}
	require.NoError(t, DB.Create(&channel).Error)

	updated, err := BatchUpdateChannels([]int{channel.Id}, ChannelBatchUpdate{
		ClientPolicy: &ChannelBatchClientPolicyUpdate{
			Mode:    "deny",
			Clients: []string{" Codex ", "codex", "OpenAI"},
		},
		UpstreamModelUpdateCheckEnabled:    &ChannelBatchBoolUpdate{Value: true},
		UpstreamModelUpdateAutoSyncEnabled: &ChannelBatchBoolUpdate{Value: true},
		UpstreamModelUpdateIgnoredModels: &ChannelBatchStringListValueUpdate{
			Value: []string{"gpt-old", "regex:^legacy-.*", "gpt-old"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	var stored Channel
	require.NoError(t, DB.First(&stored, "id = ?", channel.Id).Error)
	settings := dto.ChannelOtherSettings{}
	require.NoError(t, common.UnmarshalJsonStr(stored.OtherSettings, &settings))
	assert.Equal(t, "deny", settings.ClientPolicy.Mode)
	assert.Equal(t, []string{"codex", "openai"}, settings.ClientPolicy.Clients)
	assert.True(t, settings.UpstreamModelUpdateCheckEnabled)
	assert.True(t, settings.UpstreamModelUpdateAutoSyncEnabled)
	assert.Equal(t, []string{"gpt-old", "regex:^legacy-.*"}, settings.UpstreamModelUpdateIgnoredModels)
	assert.Equal(t, int64(123), settings.UpstreamModelUpdateLastCheckTime)
	assert.Equal(t, []string{"new-model"}, settings.UpstreamModelUpdateLastDetectedModels)
	assert.Equal(t, []string{"old-model"}, settings.UpstreamModelUpdateLastRemovedModels)

	settingsRecord := map[string]any{}
	require.NoError(t, common.UnmarshalJsonStr(stored.OtherSettings, &settingsRecord))
	assert.Equal(t, true, settingsRecord["allow_service_tier"])
	assert.Equal(t, map[string]any{"enabled": true}, settingsRecord["future_setting"])
}

func TestBatchUpdateChannelsRejectsInvalidClientPolicyWithoutChangingSettings(t *testing.T) {
	resetPricingEndpointTestTables(t)
	channel := Channel{
		Id:            8142,
		Name:          "invalid-client-policy",
		Key:           "key",
		Type:          1,
		Status:        common.ChannelStatusEnabled,
		Models:        "model-a",
		Group:         "default",
		OtherSettings: `{"allow_service_tier":true}`,
	}
	require.NoError(t, DB.Create(&channel).Error)

	updated, err := BatchUpdateChannels([]int{channel.Id}, ChannelBatchUpdate{
		ClientPolicy: &ChannelBatchClientPolicyUpdate{
			Mode:    "invalid",
			Clients: []string{"codex"},
		},
	})
	require.ErrorContains(t, err, "invalid client policy mode")
	assert.Zero(t, updated)

	var stored Channel
	require.NoError(t, DB.First(&stored, "id = ?", channel.Id).Error)
	assert.JSONEq(t, `{"allow_service_tier":true}`, stored.OtherSettings)
}

func TestBatchUpdateChannelsAppliesNullableRateLimitModes(t *testing.T) {
	resetPricingEndpointTestTables(t)
	for _, column := range []string{"rpm_limit", "concurrency_limit"} {
		assert.True(t, DB.Migrator().HasColumn(&Channel{}, column))
	}

	channel := Channel{
		Id:               8151,
		Name:             "rate-limit-modes",
		Key:              "key",
		Type:             1,
		Status:           common.ChannelStatusEnabled,
		Models:           "model-a",
		Group:            "default",
		RpmLimit:         common.GetPointer(10),
		ConcurrencyLimit: common.GetPointer(2),
	}
	require.NoError(t, DB.Create(&channel).Error)

	updated, err := BatchUpdateChannels([]int{channel.Id}, ChannelBatchUpdate{
		RpmLimit:         &ChannelBatchNullableIntUpdate{Mode: ChannelBatchLimitCustom, Value: common.GetPointer(60)},
		ConcurrencyLimit: &ChannelBatchNullableIntUpdate{Mode: ChannelBatchLimitKeep},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	var stored Channel
	require.NoError(t, DB.First(&stored, "id = ?", channel.Id).Error)
	require.NotNil(t, stored.RpmLimit)
	assert.Equal(t, 60, *stored.RpmLimit)
	require.NotNil(t, stored.ConcurrencyLimit)
	assert.Equal(t, 2, *stored.ConcurrencyLimit)

	updated, err = BatchUpdateChannels([]int{channel.Id}, ChannelBatchUpdate{
		RpmLimit:         &ChannelBatchNullableIntUpdate{Mode: ChannelBatchLimitClear},
		ConcurrencyLimit: &ChannelBatchNullableIntUpdate{Mode: ChannelBatchLimitCustom, Value: common.GetPointer(4)},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	require.NoError(t, DB.First(&stored, "id = ?", channel.Id).Error)
	assert.Nil(t, stored.RpmLimit)
	require.NotNil(t, stored.ConcurrencyLimit)
	assert.Equal(t, 4, *stored.ConcurrencyLimit)
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Nil(t, cached.RpmLimit)
	require.NotNil(t, cached.ConcurrencyLimit)
	assert.Equal(t, 4, *cached.ConcurrencyLimit)
}

func TestBatchUpdateChannelsRejectsInvalidRateLimitModeWithoutChangingChannel(t *testing.T) {
	resetPricingEndpointTestTables(t)
	channel := Channel{
		Id:               8152,
		Name:             "invalid-rate-limit-mode",
		Key:              "key",
		Type:             1,
		Status:           common.ChannelStatusEnabled,
		Models:           "model-a",
		Group:            "default",
		RpmLimit:         common.GetPointer(10),
		ConcurrencyLimit: common.GetPointer(2),
	}
	require.NoError(t, DB.Create(&channel).Error)

	for _, test := range []struct {
		name    string
		update  *ChannelBatchNullableIntUpdate
		wantErr string
	}{
		{
			name:    "clear with value",
			update:  &ChannelBatchNullableIntUpdate{Mode: ChannelBatchLimitClear, Value: common.GetPointer(1)},
			wantErr: "clear mode must not include value",
		},
		{
			name:    "keep with value",
			update:  &ChannelBatchNullableIntUpdate{Mode: ChannelBatchLimitKeep, Value: common.GetPointer(1)},
			wantErr: "keep mode must not include value",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			updated, err := BatchUpdateChannels([]int{channel.Id}, ChannelBatchUpdate{RpmLimit: test.update})
			require.ErrorContains(t, err, test.wantErr)
			assert.Zero(t, updated)
		})
	}

	var stored Channel
	require.NoError(t, DB.First(&stored, "id = ?", channel.Id).Error)
	require.NotNil(t, stored.RpmLimit)
	assert.Equal(t, 10, *stored.RpmLimit)
	require.NotNil(t, stored.ConcurrencyLimit)
	assert.Equal(t, 2, *stored.ConcurrencyLimit)
}

func TestChannelUpdatePreservesOmittedAndClearsExplicitNullableRateLimits(t *testing.T) {
	resetPricingEndpointTestTables(t)
	channel := Channel{
		Id:               8153,
		Name:             "nullable-rate-limit-update",
		Key:              "key",
		Type:             1,
		Status:           common.ChannelStatusEnabled,
		Models:           "model-a",
		Group:            "default",
		RpmLimit:         common.GetPointer(60),
		ConcurrencyLimit: common.GetPointer(2),
	}
	require.NoError(t, DB.Create(&channel).Error)

	var update Channel
	require.NoError(t, DB.First(&update, "id = ?", channel.Id).Error)
	update.RpmLimit = nil
	update.ConcurrencyLimit = nil
	require.NoError(t, update.UpdateWithAggregateLinkAndNullableLimits(false, true))

	var stored Channel
	require.NoError(t, DB.First(&stored, "id = ?", channel.Id).Error)
	require.NotNil(t, stored.RpmLimit)
	assert.Equal(t, 60, *stored.RpmLimit, "an omitted RPM field must retain its previous value")
	assert.Nil(t, stored.ConcurrencyLimit, "an explicit null must clear the concurrency limit")

	stored.RpmLimit = common.GetPointer(90)
	require.NoError(t, stored.UpdateWithAggregateLinkAndNullableLimits(true, false))
	require.NoError(t, DB.First(&stored, "id = ?", channel.Id).Error)
	require.NotNil(t, stored.RpmLimit)
	assert.Equal(t, 90, *stored.RpmLimit)
	assert.Nil(t, stored.ConcurrencyLimit)
}
