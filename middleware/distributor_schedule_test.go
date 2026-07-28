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
