package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutoGroupSelectionIgnoresProtocolClassAtEqualPriority(t *testing.T) {
	setupAutoGroupSelectionTest(t)
	createAutoGroupSelectionChannel(t, 301, "default", "auto-protocol-model")
	createAutoGroupSelectionChannel(t, 302, "vip", "auto-protocol-model")
	model.InitChannelCache()

	context := autoGroupSelectionContext()
	classifier := func(channel *model.Channel) model.ChannelCandidateClass {
		if channel.Id == 302 {
			return model.ChannelCandidateNative
		}
		return model.ChannelCandidateConvertible
	}
	// Both channels sit at the same priority number, so the protocol class no
	// longer reorders across groups: the configured group order wins.
	selected, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:                 context,
		TokenGroup:          "auto",
		ModelName:           "auto-protocol-model",
		CandidateClassifier: classifier,
		Retry:               common.GetPointer(0),
	})

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 301, selected.Id)
	assert.Equal(t, "default", group)
	assert.Equal(t, 0, common.GetContextKeyInt(context, constant.ContextKeyAutoGroupIndex))
}

func TestAutoGroupSelectionWithoutClassifierPreservesGroupOrder(t *testing.T) {
	setupAutoGroupSelectionTest(t)
	createAutoGroupSelectionChannel(t, 311, "default", "legacy-auto-model")
	createAutoGroupSelectionChannel(t, 312, "vip", "legacy-auto-model")
	model.InitChannelCache()

	selected, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        autoGroupSelectionContext(),
		TokenGroup: "auto",
		ModelName:  "legacy-auto-model",
		Retry:      common.GetPointer(0),
	})

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 311, selected.Id)
	assert.Equal(t, "default", group)
}

func TestAutoGroupNativePreselectionPreservesCrossGroupRetry(t *testing.T) {
	setupAutoGroupSelectionTest(t)
	createAutoGroupSelectionChannel(t, 321, "default", "cross-group-model")
	createAutoGroupSelectionChannel(t, 322, "vip", "cross-group-model")
	model.InitChannelCache()
	context := autoGroupSelectionContext()
	common.SetContextKey(context, constant.ContextKeyTokenCrossGroupRetry, true)
	classifier := func(*model.Channel) model.ChannelCandidateClass {
		return model.ChannelCandidateNative
	}
	param := &RetryParam{
		Ctx:                 context,
		TokenGroup:          "auto",
		ModelName:           "cross-group-model",
		CandidateClassifier: classifier,
		Retry:               common.GetPointer(0),
	}

	first, firstGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 321, first.Id)
	assert.Equal(t, "default", firstGroup)

	param.IncreaseRetry()
	second, secondGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, 322, second.Id)
	assert.Equal(t, "vip", secondGroup)
}

func TestAutoGroupSelectionReportsOnlyIncompatibleCandidatesAfterCheckingAllGroups(t *testing.T) {
	setupAutoGroupSelectionTest(t)
	createAutoGroupSelectionChannel(t, 331, "default", "all-incompatible-model")
	createAutoGroupSelectionChannel(t, 332, "vip", "all-incompatible-model")
	model.InitChannelCache()
	classifier := func(*model.Channel) model.ChannelCandidateClass {
		return model.ChannelCandidateIncompatible
	}

	selected, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:                 autoGroupSelectionContext(),
		TokenGroup:          "auto",
		ModelName:           "all-incompatible-model",
		CandidateClassifier: classifier,
		Retry:               common.GetPointer(0),
	})

	assert.Nil(t, selected)
	require.ErrorIs(t, err, model.ErrNoCompatibleChannel)
}

func TestExplicitAllowedGroupsSelectAcrossRouteGroupsWithoutGlobalAutoGroups(t *testing.T) {
	setupAutoGroupSelectionTest(t)
	createAutoGroupSelectionChannel(t, 341, "default", "routed-model")
	createAutoGroupSelectionChannel(t, 342, "vip", "routed-model")
	model.InitChannelCache()
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))

	context := autoGroupSelectionContext()
	common.SetContextKey(context, constant.ContextKeyUserModelRouteId, 9)
	selected, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:           context,
		TokenGroup:    "auto",
		ModelName:     "routed-model",
		AllowedGroups: []string{"vip"},
		Retry:         common.GetPointer(0),
	})

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 342, selected.Id)
	assert.Equal(t, "vip", group)
	assert.Equal(t, "vip", common.GetContextKeyString(context, constant.ContextKeyAutoGroup))
	assert.Equal(t, "vip", common.GetContextKeyString(context, constant.ContextKeyUserModelRouteGroup))
}

func setupAutoGroupSelectionTest(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousAutoGroups := setting.AutoGroups2JsonString()
	previousRetryTimes := common.RetryTimes
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	common.RetryTimes = 0
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.RetryTimes = previousRetryTimes
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		if previousMemoryCacheEnabled && previousDB != nil {
			model.InitChannelCache()
		}
	})
}

func createAutoGroupSelectionChannel(t *testing.T, id int, group, modelName string) {
	t.Helper()
	priority := int64(10)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:       id,
		Name:     group,
		Key:      "test-key",
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Priority: &priority,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    1,
	}).Error)
}

func autoGroupSelectionContext() *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(context, constant.ContextKeyUserGroups, []string{"default", "vip"})
	return context
}

func TestPinnedTaskPluginChannelTypesUsesPinnedGenerationIndex(t *testing.T) {
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(channelSelectTaskPluginSource("legacy-select", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})

	types, keys := pinnedTaskPluginIdentities(c, "legacy-select")
	assert.Equal(t, []int{constant.ChannelTypeKling}, types)
	assert.Equal(t, []string{"legacy-select"}, keys)
	types, keys = pinnedTaskPluginIdentities(c, "another-plugin")
	assert.Empty(t, types)
	assert.Empty(t, keys)
	types, keys = pinnedTaskPluginIdentities(nil, "legacy-select")
	assert.Empty(t, types)
	assert.Empty(t, keys)
}

func TestPinnedTaskPluginChannelTypesLeavesGenericChannelsKeyed(t *testing.T) {
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(channelSelectTaskPluginSource("generic-select", constant.ChannelTypeTaskPlugin), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})

	types, keys := pinnedTaskPluginIdentities(c, "generic-select")
	assert.Empty(t, types)
	assert.Equal(t, []string{"generic-select"}, keys)
}

func TestPinnedTaskPluginChannelTypesIncludesSharedEndpointProviders(t *testing.T) {
	registry := jsplugin.NewRegistry()
	_, err := registry.Register(channelSelectEndpointPluginSource("gemini-select", constant.ChannelTypeGemini), jsplugin.Options{})
	require.NoError(t, err)
	_, err = registry.Register(channelSelectEndpointPluginSource("vertex-select", constant.ChannelTypeVertexAi), jsplugin.Options{})
	require.NoError(t, err)
	candidates := registry.Generation().LookupEndpointCandidates("POST", "/v1/responses", "task-model")
	require.Len(t, candidates, 2)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: candidates[0].Plugin})
	c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{
		Generation: registry.Generation(),
		Plugin:     candidates[0].Plugin,
		Protocol:   candidates[0].Protocol,
		Operation:  candidates[0].Operation,
		Model:      "task-model",
		Candidates: candidates,
	})

	AppendTaskPluginIdentityFilter(c, candidates[0].Plugin.Meta.Key)
	filters := GetChannelConstraints(c).Filters
	require.Len(t, filters, 1)
	assert.Equal(t, []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi}, filters[0].TaskPluginChannelTypes)
	assert.Equal(t, []string{"gemini-select", "vertex-select"}, filters[0].TaskPluginKeys)
}

func channelSelectTaskPluginSource(key string, channelType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  %s
  models: ["task-model"],
  fetchMode: "per_task",
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`, key, key, channelSelectChannelTypesField(channelType))
}

func channelSelectEndpointPluginSource(key string, channelType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  %s
  models: ["task-model"],
  fetchMode: "per_task",
  protocols: [{name: "openai_responses", supports: ["stream", "sync", "background"]}],
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export const protocols = {openai_responses: {
  decodeRequest: function(ctx) { return {kind: "submit", model: "task-model", requestBody: ctx.body.value}; },
  renderEvents: function() { return {events: [], state: null, done: false}; },
  renderFinal: function() { return {output: []}; },
}};
`, key, key, channelSelectChannelTypesField(channelType))
}

func channelSelectChannelTypesField(channelType int) string {
	if channelType <= 0 || channelType == constant.ChannelTypeTaskPlugin {
		return ""
	}
	return fmt.Sprintf("channelTypes: [%d],", channelType)
}

func TestPinnedTaskPluginChannelTypesIncludesCompatibleTypes(t *testing.T) {
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(channelSelectCompatiblePluginSource("sora-select", constant.ChannelTypeSora, constant.ChannelTypeOpenAI), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})

	types, keys := pinnedTaskPluginIdentities(c, "sora-select")
	assert.Equal(t, []int{constant.ChannelTypeSora, constant.ChannelTypeOpenAI}, types)
	assert.Equal(t, []string{"sora-select"}, keys)
}

func channelSelectCompatiblePluginSource(key string, channelType, compatibleType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [%d, %d],
  models: ["task-model"],
  fetchMode: "per_task",
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`, key, key, channelType, compatibleType)
}

func TestSharedType61IdentityFilterContainsAllCandidateKeys(t *testing.T) {
	registry := jsplugin.NewRegistry()
	for _, key := range []string{"alpha", "beta"} {
		_, err := registry.Register(channelSelectEndpointPluginSource(key, 0), jsplugin.Options{})
		require.NoError(t, err)
	}
	generation := registry.Generation()
	candidates := generation.LookupEndpointCandidates("POST", "/v1/responses", "task-model")
	require.Len(t, candidates, 2)
	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{Generation: generation, Plugin: candidates[0].Plugin, Candidates: candidates})
	AppendTaskPluginIdentityFilter(c, "alpha")
	filters := GetChannelConstraints(c).Filters
	require.Len(t, filters, 1)
	assert.Equal(t, "alpha", filters[0].TaskPluginKey)
	assert.Equal(t, []string{"alpha", "beta"}, filters[0].TaskPluginKeys)
	assert.Empty(t, filters[0].TaskPluginChannelTypes)
}
