package relay

import (
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/pkg/wsmanager"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func WssHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *hosttypes.NewAPIError) {
	info.InitChannelMeta(c)

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return hosttypes.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), hosttypes.ErrorCodeInvalidApiType, hosttypes.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	//var requestBody io.Reader
	//firstWssRequest, _ := c.Get("first_wss_request")
	//requestBody = bytes.NewBuffer(firstWssRequest.([]byte))

	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, info, nil)
	if err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeDoRequestFailed)
	}

	if resp != nil {
		info.TargetWs = resp.WebSocket
		if info.TargetWs == nil {
			return hosttypes.NewError(fmt.Errorf("realtime requires a WebSocket transport"), hosttypes.ErrorCodeBadResponseBody)
		}
		defer info.TargetWs.Close()
		var closeOnce sync.Once
		closeForPolicy := func(reason string) {
			closeOnce.Do(func() {
				deadline := time.Now().Add(time.Second)
				closeMessage := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason)
				_ = info.ClientWs.WriteControl(websocket.CloseMessage, closeMessage, deadline)
				_ = info.TargetWs.WriteControl(websocket.CloseMessage, closeMessage, deadline)
				_ = info.ClientWs.Close()
				_ = info.TargetWs.Close()
			})
		}
		unregister := wsmanager.Register(info.ChannelId, wsmanager.KindRealtime, closeForPolicy)
		defer unregister()
		if !service.IsChannelAvailableForActiveWebSocket(info.ChannelId) {
			closeForPolicy(service.ChannelDisabledCloseReason)
			return hosttypes.NewError(fmt.Errorf("channel %d is disabled or deleted", info.ChannelId), hosttypes.ErrorCodeGetChannelFailed, hosttypes.ErrOptionWithSkipRetry())
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, nil, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	service.PostWssConsumeQuota(c, info, usage.(*dto.RealtimeUsage), "")
	return nil
}
