package model

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func pricingCacheModelNames() map[string]struct{} {
	models := make(map[string]struct{})
	for _, pricing := range GetPricing() {
		models[pricing.ModelName] = struct{}{}
	}
	return models
}

func insertPricingCacheChannel(t *testing.T, channelID int, modelName string) {
	t.Helper()
	insertPricingEndpointChannel(t, channelID, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, channelID, modelName)
}

func TestPricingCacheCachesValidEmptySnapshotAndRefreshesDirectGetters(t *testing.T) {
	resetPricingEndpointTestTables(t)

	assert.Empty(t, GetPricing())
	require.NoError(t, DB.Create(&Vendor{Name: "cached-empty-vendor", Status: 1}).Error)
	insertPricingCacheChannel(t, 490, "cached-empty-model")

	assert.Empty(t, GetPricing(), "a valid empty pricing result should remain cached")
	assert.Empty(t, GetVendors(), "all data from the valid empty snapshot should remain cached")

	InvalidatePricingCache()
	vendors := GetVendors()
	require.Len(t, vendors, 1)
	assert.Equal(t, "cached-empty-vendor", vendors[0].Name)

	InvalidatePricingCache()
	assert.Contains(t, pricingCacheModelNames(), "cached-empty-model")
}

func TestPricingCacheGettersReturnIndependentSnapshots(t *testing.T) {
	resetPricingEndpointTestTables(t)

	cacheRatio := 0.1
	createCacheRatio := 1.1
	imageRatio := 2.1
	audioRatio := 3.1
	audioCompletionRatio := 4.1

	updatePricingLock.Lock()
	modelSupportEndpointsLock.Lock()
	modelEnableGroupsLock.Lock()
	pricingMap = []Pricing{{
		ModelName:              "copy-model",
		EnableGroup:            []string{"default", "vip"},
		SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic},
		CacheRatio:             &cacheRatio,
		CreateCacheRatio:       &createCacheRatio,
		ImageRatio:             &imageRatio,
		AudioRatio:             &audioRatio,
		AudioCompletionRatio:   &audioCompletionRatio,
	}}
	vendorsList = []PricingVendor{{ID: 1, Name: "copy-vendor"}}
	modelSupportEndpointTypes = map[string][]constant.EndpointType{
		"copy-model": {constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic},
	}
	supportedEndpointMap = map[string]common.EndpointInfo{
		string(constant.EndpointTypeOpenAI): {Path: "/v1/chat/completions", Method: "POST"},
	}
	modelEnableGroups = map[string][]string{"copy-model": {"default", "vip"}}
	modelQuotaTypeMap = map[string]int{"copy-model": 0}
	lastGetPricingTime = time.Now()
	pricingCacheValid = true
	modelEnableGroupsLock.Unlock()
	modelSupportEndpointsLock.Unlock()
	updatePricingLock.Unlock()

	pricing := GetPricing()
	require.Len(t, pricing, 1)
	pricing[0].ModelName = "changed"
	pricing[0].EnableGroup[0] = "changed"
	pricing[0].SupportedEndpointTypes[0] = constant.EndpointTypeGemini
	*pricing[0].CacheRatio = 9
	*pricing[0].CreateCacheRatio = 9
	*pricing[0].ImageRatio = 9
	*pricing[0].AudioRatio = 9
	*pricing[0].AudioCompletionRatio = 9

	vendors := GetVendors()
	require.Len(t, vendors, 1)
	vendors[0].Name = "changed"

	endpoints := GetModelSupportEndpointTypes("copy-model")
	require.Len(t, endpoints, 2)
	endpoints[0] = constant.EndpointTypeGemini

	endpointMap := GetSupportedEndpointMap()
	endpointMap[string(constant.EndpointTypeOpenAI)] = common.EndpointInfo{Path: "/changed", Method: "GET"}
	endpointMap["changed"] = common.EndpointInfo{Path: "/changed", Method: "GET"}

	groups := GetModelEnableGroups("copy-model")
	require.Len(t, groups, 2)
	groups[0] = "changed"

	pricing = GetPricing()
	require.Len(t, pricing, 1)
	assert.Equal(t, "copy-model", pricing[0].ModelName)
	assert.Equal(t, []string{"default", "vip"}, pricing[0].EnableGroup)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic}, pricing[0].SupportedEndpointTypes)
	assert.Equal(t, 0.1, *pricing[0].CacheRatio)
	assert.Equal(t, 1.1, *pricing[0].CreateCacheRatio)
	assert.Equal(t, 2.1, *pricing[0].ImageRatio)
	assert.Equal(t, 3.1, *pricing[0].AudioRatio)
	assert.Equal(t, 4.1, *pricing[0].AudioCompletionRatio)
	assert.Equal(t, "copy-vendor", GetVendors()[0].Name)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic}, GetModelSupportEndpointTypes("copy-model"))
	assert.Equal(t, common.EndpointInfo{Path: "/v1/chat/completions", Method: "POST"}, GetSupportedEndpointMap()[string(constant.EndpointTypeOpenAI)])
	assert.NotContains(t, GetSupportedEndpointMap(), "changed")
	assert.Equal(t, []string{"default", "vip"}, GetModelEnableGroups("copy-model"))
}

func TestPricingCacheGettersRetryWhenInvalidatedBetweenRefreshAndSnapshot(t *testing.T) {
	getters := []struct {
		name string
		read func() bool
	}{
		{
			name: "pricing",
			read: func() bool {
				for _, pricing := range GetPricing() {
					if pricing.ModelName == "gpt-interleaving-model" {
						return true
					}
				}
				return false
			},
		},
		{
			name: "vendors",
			read: func() bool {
				return len(GetVendors()) > 0
			},
		},
		{
			name: "model endpoint types",
			read: func() bool {
				return len(GetModelSupportEndpointTypes("gpt-interleaving-model")) > 0
			},
		},
		{
			name: "supported endpoint map",
			read: func() bool {
				return len(GetSupportedEndpointMap()) > 0
			},
		},
		{
			name: "model enable groups",
			read: func() bool {
				return len(GetModelEnableGroups("gpt-interleaving-model")) > 0
			},
		},
		{
			name: "model quota types",
			read: func() bool {
				return len(GetModelQuotaTypes("gpt-interleaving-model")) > 0
			},
		},
	}

	for _, getter := range getters {
		t.Run(getter.name, func(t *testing.T) {
			resetPricingEndpointTestTables(t)
			insertPricingCacheChannel(t, 489, "gpt-interleaving-model")
			InvalidatePricingCache()

			refreshStarted := make(chan struct{})
			releaseRefresh := make(chan struct{})
			var blockOnce sync.Once
			var releaseOnce sync.Once
			blockRefresh := func(tx *gorm.DB) {
				if tx.Statement == nil || tx.Statement.Table != "abilities" {
					return
				}
				blockOnce.Do(func() {
					close(refreshStarted)
					<-releaseRefresh
				})
			}
			queryCallback := "test:block_pricing_query_refresh_" + getter.name
			rowCallback := "test:block_pricing_row_refresh_" + getter.name
			require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(queryCallback, blockRefresh))
			require.NoError(t, DB.Callback().Row().Before("gorm:row").Register(rowCallback, blockRefresh))
			t.Cleanup(func() {
				_ = DB.Callback().Query().Remove(queryCallback)
				_ = DB.Callback().Row().Remove(rowCallback)
			})
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(releaseRefresh) })
			})

			getterResult := make(chan bool, 1)
			go func() {
				getterResult <- getter.read()
			}()

			select {
			case <-refreshStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("pricing refresh did not reach the controlled query interleaving")
			}

			invalidationAttempted := make(chan struct{})
			invalidationDone := make(chan struct{})
			go func() {
				close(invalidationAttempted)
				InvalidatePricingCache()
				close(invalidationDone)
			}()
			<-invalidationAttempted
			runtime.Gosched()
			releaseOnce.Do(func() { close(releaseRefresh) })

			select {
			case <-invalidationDone:
			case <-time.After(5 * time.Second):
				t.Fatal("pricing invalidation did not complete")
			}

			select {
			case found := <-getterResult:
				assert.True(t, found, "getter returned an empty cache snapshot after concurrent invalidation")
			case <-time.After(5 * time.Second):
				t.Fatal("pricing getter did not retry after concurrent invalidation")
			}
		})
	}
}

func TestGetAllEnableAbilityWithChannelsRequiresEnabledAbilityAndChannel(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingCacheChannel(t, 491, "available-model")
	require.NoError(t, DB.Create(&Channel{
		Id:     492,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "disabled-channel-key",
		Name:   "disabled-channel",
		Status: common.ChannelStatusManuallyDisabled,
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "disabled-ability-model", ChannelId: 491, Enabled: false},
		{Group: "default", Model: "disabled-channel-model", ChannelId: 492, Enabled: true},
		{Group: "default", Model: "missing-channel-model", ChannelId: 499, Enabled: true},
	}).Error)

	abilities, err := GetAllEnableAbilityWithChannels()
	require.NoError(t, err)
	require.Len(t, abilities, 1)
	assert.Equal(t, "available-model", abilities[0].Model)
	assert.Equal(t, 491, abilities[0].ChannelId)
	assert.Equal(t, constant.ChannelTypeOpenAI, abilities[0].ChannelType)
}

func TestPricingCacheInvalidatesAfterAbilityStatusChanges(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingCacheChannel(t, 501, "cache-baseline-model")
	insertPricingCacheChannel(t, 502, "cache-ability-model")

	warmed := pricingCacheModelNames()
	require.Contains(t, warmed, "cache-baseline-model")
	require.Contains(t, warmed, "cache-ability-model")

	require.NoError(t, UpdateAbilityStatus(502, false))
	disabled := pricingCacheModelNames()
	assert.Contains(t, disabled, "cache-baseline-model")
	assert.NotContains(t, disabled, "cache-ability-model")

	require.NoError(t, UpdateAbilityStatus(502, true))
	reenabled := pricingCacheModelNames()
	assert.Contains(t, reenabled, "cache-baseline-model")
	assert.Contains(t, reenabled, "cache-ability-model")
}

func TestPricingCacheInvalidatesAfterChannelStatusChangesAndDelete(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingCacheChannel(t, 511, "cache-status-baseline-model")
	insertPricingCacheChannel(t, 512, "cache-status-model")
	InitChannelCache()

	warmed := pricingCacheModelNames()
	require.Contains(t, warmed, "cache-status-baseline-model")
	require.Contains(t, warmed, "cache-status-model")

	require.True(t, UpdateChannelStatus(512, "", common.ChannelStatusManuallyDisabled, "test"))
	disabled := pricingCacheModelNames()
	assert.Contains(t, disabled, "cache-status-baseline-model")
	assert.NotContains(t, disabled, "cache-status-model")

	require.True(t, UpdateChannelStatus(512, "", common.ChannelStatusEnabled, "test"))
	reenabled := pricingCacheModelNames()
	assert.Contains(t, reenabled, "cache-status-baseline-model")
	assert.Contains(t, reenabled, "cache-status-model")

	channel, err := GetChannelById(512, true)
	require.NoError(t, err)
	require.NoError(t, channel.Delete())
	deleted := pricingCacheModelNames()
	assert.Contains(t, deleted, "cache-status-baseline-model")
	assert.NotContains(t, deleted, "cache-status-model")
}

func TestPricingCacheInvalidatesAfterBatchChannelInsertAndDelete(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingCacheChannel(t, 521, "cache-batch-baseline-model")
	warmed := pricingCacheModelNames()
	require.Contains(t, warmed, "cache-batch-baseline-model")
	require.NotContains(t, warmed, "cache-batch-model")

	require.NoError(t, BatchInsertChannels([]Channel{{
		Id:     522,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "cache-batch-key",
		Name:   "cache-batch-channel",
		Status: common.ChannelStatusEnabled,
		Models: "cache-batch-model",
		Group:  "default",
	}}))
	inserted := pricingCacheModelNames()
	assert.Contains(t, inserted, "cache-batch-baseline-model")
	assert.Contains(t, inserted, "cache-batch-model")

	deletedCount, err := BatchDeleteChannels([]int{522})
	require.NoError(t, err)
	assert.Equal(t, int64(1), deletedCount)
	deleted := pricingCacheModelNames()
	assert.Contains(t, deleted, "cache-batch-baseline-model")
	assert.NotContains(t, deleted, "cache-batch-model")
}

func TestPricingCacheInvalidatesAcrossSingleChannelMutationEntrypoints(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingCacheChannel(t, 531, "cache-single-baseline-model")
	warmed := pricingCacheModelNames()
	require.Contains(t, warmed, "cache-single-baseline-model")

	tag := "cache-single-tag"
	channel := &Channel{
		Id:     532,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "cache-single-key",
		Name:   "cache-single-channel",
		Status: common.ChannelStatusEnabled,
		Models: "cache-single-inserted-model",
		Group:  "default",
		Tag:    &tag,
	}
	require.NoError(t, channel.Insert())
	inserted := pricingCacheModelNames()
	assert.Contains(t, inserted, "cache-single-baseline-model")
	assert.Contains(t, inserted, "cache-single-inserted-model")

	channel.Models = "cache-single-updated-model"
	require.NoError(t, channel.Update())
	updated := pricingCacheModelNames()
	assert.Contains(t, updated, "cache-single-baseline-model")
	assert.NotContains(t, updated, "cache-single-inserted-model")
	assert.Contains(t, updated, "cache-single-updated-model")

	require.NoError(t, DisableChannelByTag(tag))
	disabled := pricingCacheModelNames()
	assert.Contains(t, disabled, "cache-single-baseline-model")
	assert.NotContains(t, disabled, "cache-single-updated-model")

	require.NoError(t, EnableChannelByTag(tag))
	reenabled := pricingCacheModelNames()
	assert.Contains(t, reenabled, "cache-single-baseline-model")
	assert.Contains(t, reenabled, "cache-single-updated-model")

	deletedCount, err := DeleteChannelByStatus(common.ChannelStatusEnabled)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deletedCount)
	assert.Empty(t, pricingCacheModelNames())
}
