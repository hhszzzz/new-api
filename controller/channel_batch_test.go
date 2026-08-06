package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFingerprintChannelIDsIsStableAcrossOrderAndDuplicates(t *testing.T) {
	assert.Equal(t, fingerprintChannelIDs([]int{3, 1, 2}), fingerprintChannelIDs([]int{2, 3, 1, 2}))
	assert.NotEqual(t, fingerprintChannelIDs([]int{1, 2}), fingerprintChannelIDs([]int{1, 2, 3}))
}

func TestResolveChannelBatchFilterIDsMatchesLegacyNumericStatusFilters(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	enabled := &model.Channel{Name: "batch-enabled", Key: "enabled-key", Status: common.ChannelStatusEnabled}
	disabled := &model.Channel{Name: "batch-disabled", Key: "disabled-key", Status: common.ChannelStatusManuallyDisabled}
	require.NoError(t, db.Create(enabled).Error)
	require.NoError(t, db.Create(disabled).Error)

	enabledIDs, err := resolveChannelBatchFilterIDs(channelBatchFilter{Status: "1"})
	require.NoError(t, err)
	assert.Equal(t, []int{enabled.Id}, enabledIDs)

	disabledIDs, err := resolveChannelBatchFilterIDs(channelBatchFilter{Status: "0"})
	require.NoError(t, err)
	assert.Equal(t, []int{disabled.Id}, disabledIDs)
}

func TestBatchUpdateChannelsReturnsConflictWhenFilteredTargetDrifts(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	first := &model.Channel{Name: "drift-target-one", Key: "first-key", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(first).Error)

	filter := channelBatchFilter{Keyword: "drift-target"}
	previewIDs, err := resolveChannelBatchFilterIDs(filter)
	require.NoError(t, err)
	fingerprint := fingerprintChannelIDs(previewIDs)

	second := &model.Channel{Name: "drift-target-two", Key: "second-key", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(second).Error)

	body, err := common.Marshal(channelBatchUpdateRequest{
		Target: channelBatchTarget{
			Mode:        channelBatchTargetFiltered,
			Filter:      filter,
			Fingerprint: fingerprint,
		},
		Updates: model.ChannelBatchUpdate{
			Priority: &model.ChannelBatchInt64Update{Value: 9},
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/channel/batch", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	BatchUpdateChannels(ctx)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "preview and confirm again")
}

func TestValidateChannelRateLimitBounds(t *testing.T) {
	channel := &model.Channel{Type: 1}
	assert.NoError(t, validateChannel(channel, false))

	channel.RpmLimit = common.GetPointer(1)
	channel.ConcurrencyLimit = common.GetPointer(2_147_483_647)
	assert.NoError(t, validateChannel(channel, false))

	channel.RpmLimit = common.GetPointer(0)
	assert.ErrorContains(t, validateChannel(channel, false), "rpm_limit must be between")

	channel.RpmLimit = common.GetPointer(1)
	channel.ConcurrencyLimit = common.GetPointer(2_147_483_648)
	assert.ErrorContains(t, validateChannel(channel, false), "concurrency_limit must be between")
}
