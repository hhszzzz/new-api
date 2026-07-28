package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutoGroupSelectionRanksNativeAcrossGroupsBeforeConvertible(t *testing.T) {
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
	selected, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:                 context,
		TokenGroup:          "auto",
		ModelName:           "auto-protocol-model",
		CandidateClassifier: classifier,
		Retry:               common.GetPointer(0),
	})

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 302, selected.Id)
	assert.Equal(t, "vip", group)
	assert.Equal(t, 1, common.GetContextKeyInt(context, constant.ContextKeyAutoGroupIndex))
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
