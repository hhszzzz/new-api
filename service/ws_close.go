package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
)

const ChannelDisabledCloseReason = "channel disabled or deleted"

func CloseActiveWebSocketsForChannel(channelID int, reason string) int {
	return wsmanager.CloseChannelsAndBroadcast([]int{channelID}, reason)
}

func CloseActiveWebSocketsForChannels(channelIDs []int, reason string) int {
	return wsmanager.CloseChannelsAndBroadcast(channelIDs, reason)
}

func IsChannelAvailableForActiveWebSocket(channelID int) bool {
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to check channel before registering active websocket: channel_id=%d, error=%v", channelID, err))
		return false
	}
	return channel.IsSchedulableAt(time.Now())
}
