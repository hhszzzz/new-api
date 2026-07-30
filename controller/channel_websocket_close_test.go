package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChannelWebSocketCloseTest(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		service.ResetProxyClientCache()
	})
}

func createChannelWebSocketCloseTestChannel(t *testing.T, name, tag string, status int) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:   name,
		Key:    "test-key-" + name,
		Status: status,
		Tag:    common.GetPointer(tag),
	}
	require.NoError(t, model.DB.Create(channel).Error)
	return channel
}

func registerChannelCloseProbe(t *testing.T, channelID int) <-chan string {
	t.Helper()
	closed := make(chan string, 1)
	unregister := wsmanager.Register(channelID, wsmanager.KindResponses, func(reason string) {
		closed <- reason
	})
	t.Cleanup(unregister)
	return closed
}

func requireChannelClose(t *testing.T, closed <-chan string) {
	t.Helper()
	select {
	case reason := <-closed:
		assert.Equal(t, service.ChannelDisabledCloseReason, reason)
	default:
		t.Fatal("expected active websocket to be closed")
	}
}

func assertChannelNotClosed(t *testing.T, closed <-chan string) {
	t.Helper()
	select {
	case reason := <-closed:
		t.Fatalf("unrelated active websocket was closed: %s", reason)
	default:
	}
}

func TestUpdateChannelStatusClosesOnlyWhenDisabling(t *testing.T) {
	setupChannelWebSocketCloseTest(t)
	disabled := createChannelWebSocketCloseTestChannel(t, "disable", "status", common.ChannelStatusEnabled)
	unchanged := createChannelWebSocketCloseTestChannel(t, "keep-enabled", "status", common.ChannelStatusEnabled)
	disabledProbe := registerChannelCloseProbe(t, disabled.Id)
	unchangedProbe := registerChannelCloseProbe(t, unchanged.Id)

	disableBody, err := common.Marshal(ChannelStatusRequest{Status: common.ChannelStatusManuallyDisabled})
	require.NoError(t, err)
	disableRecorder := httptest.NewRecorder()
	disableContext, _ := gin.CreateTestContext(disableRecorder)
	disableContext.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", disabled.Id)}}
	disableContext.Request = httptest.NewRequest(http.MethodPut, "/api/channel/status", bytes.NewReader(disableBody))
	disableContext.Request.Header.Set("Content-Type", "application/json")

	UpdateChannelStatus(disableContext)

	assert.Contains(t, disableRecorder.Body.String(), `"data":true`)
	requireChannelClose(t, disabledProbe)
	assertChannelNotClosed(t, unchangedProbe)

	enableBody, err := common.Marshal(ChannelStatusRequest{Status: common.ChannelStatusEnabled})
	require.NoError(t, err)
	enableRecorder := httptest.NewRecorder()
	enableContext, _ := gin.CreateTestContext(enableRecorder)
	enableContext.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", unchanged.Id)}}
	enableContext.Request = httptest.NewRequest(http.MethodPut, "/api/channel/status", bytes.NewReader(enableBody))
	enableContext.Request.Header.Set("Content-Type", "application/json")

	UpdateChannelStatus(enableContext)

	assert.Contains(t, enableRecorder.Body.String(), `"data":false`)
	assertChannelNotClosed(t, unchangedProbe)
}

func TestBatchUpdateChannelStatusClosesChangedChannels(t *testing.T) {
	setupChannelWebSocketCloseTest(t)
	first := createChannelWebSocketCloseTestChannel(t, "first", "batch-status", common.ChannelStatusEnabled)
	second := createChannelWebSocketCloseTestChannel(t, "second", "batch-status", common.ChannelStatusEnabled)
	other := createChannelWebSocketCloseTestChannel(t, "other", "batch-status", common.ChannelStatusEnabled)
	firstProbe := registerChannelCloseProbe(t, first.Id)
	secondProbe := registerChannelCloseProbe(t, second.Id)
	otherProbe := registerChannelCloseProbe(t, other.Id)
	body, err := common.Marshal(ChannelStatusBatchRequest{
		Ids:    []int{first.Id, second.Id},
		Status: common.ChannelStatusManuallyDisabled,
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/status/batch", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	BatchUpdateChannelStatus(ctx)

	assert.Contains(t, recorder.Body.String(), `"data":2`)
	requireChannelClose(t, firstProbe)
	requireChannelClose(t, secondProbe)
	assertChannelNotClosed(t, otherProbe)
}

func TestChannelDeletionEndpointsCloseAffectedChannels(t *testing.T) {
	t.Run("single delete", func(t *testing.T) {
		setupChannelWebSocketCloseTest(t)
		channel := createChannelWebSocketCloseTestChannel(t, "single-delete", "delete", common.ChannelStatusEnabled)
		probe := registerChannelCloseProbe(t, channel.Id)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", channel.Id)}}
		ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/", nil)

		DeleteChannel(ctx)

		assert.Contains(t, recorder.Body.String(), `"success":true`)
		requireChannelClose(t, probe)
	})

	t.Run("batch delete", func(t *testing.T) {
		setupChannelWebSocketCloseTest(t)
		first := createChannelWebSocketCloseTestChannel(t, "batch-delete-first", "delete", common.ChannelStatusEnabled)
		second := createChannelWebSocketCloseTestChannel(t, "batch-delete-second", "delete", common.ChannelStatusEnabled)
		other := createChannelWebSocketCloseTestChannel(t, "batch-delete-other", "delete", common.ChannelStatusEnabled)
		firstProbe := registerChannelCloseProbe(t, first.Id)
		secondProbe := registerChannelCloseProbe(t, second.Id)
		otherProbe := registerChannelCloseProbe(t, other.Id)
		body, err := common.Marshal(ChannelBatch{Ids: []int{first.Id, second.Id}})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/batch", bytes.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")

		DeleteChannelBatch(ctx)

		assert.Contains(t, recorder.Body.String(), `"data":2`)
		requireChannelClose(t, firstProbe)
		requireChannelClose(t, secondProbe)
		assertChannelNotClosed(t, otherProbe)
	})

	t.Run("delete disabled", func(t *testing.T) {
		setupChannelWebSocketCloseTest(t)
		disabled := createChannelWebSocketCloseTestChannel(t, "disabled-delete", "delete", common.ChannelStatusAutoDisabled)
		enabled := createChannelWebSocketCloseTestChannel(t, "enabled-keep", "delete", common.ChannelStatusEnabled)
		disabledProbe := registerChannelCloseProbe(t, disabled.Id)
		enabledProbe := registerChannelCloseProbe(t, enabled.Id)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/disabled", nil)

		DeleteDisabledChannel(ctx)

		assert.Contains(t, recorder.Body.String(), `"data":1`)
		requireChannelClose(t, disabledProbe)
		assertChannelNotClosed(t, enabledProbe)
	})
}

func TestDisableTagChannelsClosesOnlyMatchingChannels(t *testing.T) {
	setupChannelWebSocketCloseTest(t)
	first := createChannelWebSocketCloseTestChannel(t, "tag-first", "target-tag", common.ChannelStatusEnabled)
	second := createChannelWebSocketCloseTestChannel(t, "tag-second", "target-tag", common.ChannelStatusEnabled)
	other := createChannelWebSocketCloseTestChannel(t, "tag-other", "other-tag", common.ChannelStatusEnabled)
	firstProbe := registerChannelCloseProbe(t, first.Id)
	secondProbe := registerChannelCloseProbe(t, second.Id)
	otherProbe := registerChannelCloseProbe(t, other.Id)
	body, err := common.Marshal(ChannelTag{Tag: "target-tag"})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/tag/disable", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DisableTagChannels(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	requireChannelClose(t, firstProbe)
	requireChannelClose(t, secondProbe)
	assertChannelNotClosed(t, otherProbe)
}

func TestManageMultiKeysClosesOnlyAfterAllKeysBecomeUnavailable(t *testing.T) {
	setupChannelWebSocketCloseTest(t)
	channel := createChannelWebSocketCloseTestChannel(t, "multi-key", "multi-key", common.ChannelStatusEnabled)
	channel.Key = "key-one\nkey-two"
	channel.ChannelInfo = model.ChannelInfo{
		IsMultiKey:         true,
		MultiKeySize:       2,
		MultiKeyStatusList: map[int]int{},
	}
	require.NoError(t, model.DB.Save(channel).Error)
	probe := registerChannelCloseProbe(t, channel.Id)

	disableKey := func(index int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := common.Marshal(MultiKeyManageRequest{
			ChannelId: channel.Id,
			Action:    "disable_key",
			KeyIndex:  common.GetPointer(index),
		})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/multi-key", bytes.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ManageMultiKeys(ctx)
		return recorder
	}

	firstResponse := disableKey(0)
	assert.Contains(t, firstResponse.Body.String(), `"success":true`)
	assertChannelNotClosed(t, probe)

	secondResponse := disableKey(1)
	assert.Contains(t, secondResponse.Body.String(), `"success":true`)
	requireChannelClose(t, probe)

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
}
