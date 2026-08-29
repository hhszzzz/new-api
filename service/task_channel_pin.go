package service

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// ApplyTaskChannelPin copies plugin-declared origin-task facts onto RelayInfo
// and locks retries to an origin-task channel when the resolved pin requires it.
func ApplyTaskChannelPin(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil {
		return nil
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	if tasks, ok := common.GetContextKeyType[[]*model.Task](c, constant.ContextKeyOriginTasks); ok {
		refs := make([]relaycommon.OriginTaskRef, 0, len(tasks))
		for _, task := range tasks {
			if task == nil {
				continue
			}
			refs = append(refs, relaycommon.OriginTaskRef{
				TaskID:         task.TaskID,
				UpstreamTaskID: task.GetUpstreamTaskID(),
				Action:         task.Action,
				Status:         string(task.Status),
				Data:           append([]byte(nil), task.Data...),
			})
		}
		info.OriginTasks = refs
	}
	pin, found, _ := GetChannelConstraints(c).ResolvedPin()
	if !found || pin.RetryMode != dto.PinRetrySameChannel {
		return nil
	}
	channel, err := model.CacheGetChannel(pin.ChannelId)
	if err != nil {
		return TaskErrorWrapperLocal(err, "origin_task_channel_disabled", http.StatusBadRequest)
	}
	if channel.Status != common.ChannelStatusEnabled {
		return TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "origin_task_channel_disabled", http.StatusBadRequest)
	}
	info.LockedChannel = channel
	return nil
}
