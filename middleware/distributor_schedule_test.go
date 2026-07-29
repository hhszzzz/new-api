package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupContextForSelectedChannelEnforcesScheduleUnlessBypassed(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	channel := &model.Channel{
		Id:       91,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "scheduled channel",
		Key:      "test-key",
		Status:   common.ChannelStatusEnabled,
		Schedule: model.ChannelSchedule{StartsAt: &future},
	}

	enforcedContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	enforcedContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	enforcedError := SetupContextForSelectedChannel(enforcedContext, channel, "gpt-test", true)
	require.NotNil(t, enforcedError)
	require.ErrorIs(t, enforcedError, ErrChannelOutsideSchedule)
	assert.Equal(t, types.ErrorCodeGetChannelFailed, enforcedError.GetErrorCode())
	assert.Equal(t, http.StatusServiceUnavailable, enforcedError.StatusCode)

	bypassedContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	bypassedContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.Nil(t, SetupContextForSelectedChannel(bypassedContext, channel, "gpt-test", false))
	assert.Equal(t, channel.Id, common.GetContextKeyInt(bypassedContext, constant.ContextKeyChannelId))
}

func TestSetupContextForSelectedChannelClearsPreviousProviderStateOnRetry(t *testing.T) {
	organization := "org-first"
	first := &model.Channel{
		Id:                 92,
		Type:               constant.ChannelTypeAzure,
		Name:               "first channel",
		Key:                "first-key",
		Status:             common.ChannelStatusEnabled,
		Other:              "2025-04-01-preview",
		OpenAIOrganization: &organization,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusEnabled},
		},
		Keys: []string{"first-key"},
	}
	second := &model.Channel{
		Id:     93,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "second channel",
		Key:    "second-key",
		Status: common.ChannelStatusEnabled,
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.Nil(t, SetupContextForSelectedChannel(ctx, first, "gpt-test", false))
	assert.Equal(t, organization, common.GetContextKeyString(ctx, constant.ContextKeyChannelOrganization))
	assert.Equal(t, "2025-04-01-preview", ctx.GetString("api_version"))
	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey))

	ctx.Set("region", "old-region")
	ctx.Set("plugin", "old-plugin")
	ctx.Set("bot_id", "old-bot")
	require.Nil(t, SetupContextForSelectedChannel(ctx, second, "gpt-test", false))

	assert.Equal(t, second.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	assert.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyChannelOrganization))
	assert.Zero(t, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
	assert.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey))
	assert.Empty(t, ctx.GetString("api_version"))
	assert.Empty(t, ctx.GetString("region"))
	assert.Empty(t, ctx.GetString("plugin"))
	assert.Empty(t, ctx.GetString("bot_id"))
}
