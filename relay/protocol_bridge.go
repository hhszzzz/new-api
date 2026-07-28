package relay

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func selectedProtocolPlan(c *gin.Context) (channelcompat.ProtocolPlan, bool) {
	if c == nil {
		return channelcompat.ProtocolPlan{}, false
	}
	return common.GetContextKeyType[channelcompat.ProtocolPlan](c, constant.ContextKeyProtocolPlan)
}

func applyProtocolPlan(info *relaycommon.RelayInfo, plan channelcompat.ProtocolPlan) func() {
	if info == nil || plan.Status == channelcompat.StatusIncompatible {
		return func() {}
	}
	savedRelayMode := info.RelayMode
	savedRequestURLPath := info.RequestURLPath
	switch plan.UpstreamProtocol {
	case channelcompat.ProtocolChat:
		info.RelayMode = relayconstant.RelayModeChatCompletions
		info.RequestURLPath = "/v1/chat/completions"
	case channelcompat.ProtocolMessages:
		info.RelayMode = relayconstant.RelayModeUnknown
		info.RequestURLPath = "/v1/messages"
	case channelcompat.ProtocolResponses:
		info.RelayMode = relayconstant.RelayModeResponses
		info.RequestURLPath = "/v1/responses"
	}
	return func() {
		info.RelayMode = savedRelayMode
		info.RequestURLPath = savedRequestURLPath
	}
}

func convertRequestForProtocolPlan(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, plan channelcompat.ProtocolPlan, request dto.Request) (any, error) {
	if request == nil {
		return nil, fmt.Errorf("protocol bridge request is nil")
	}
	converted := any(request)
	if plan.Status == channelcompat.StatusConvertible {
		result, err := service.ConvertRequestByID(c, info, plan.RequestConverter, request)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(
				err,
				types.ErrorCodeConvertRequestFailed,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
		converted = result.Value
	}

	switch plan.UpstreamProtocol {
	case channelcompat.ProtocolChat:
		chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
		if !ok {
			return nil, fmt.Errorf("expected OpenAI chat request for protocol plan, got %T", converted)
		}
		if plan.Status == channelcompat.StatusConvertible && plan.RequestProtocol == channelcompat.ProtocolMessages {
			for messageIndex := range chatRequest.Messages {
				if chatRequest.Messages[messageIndex].IsStringContent() {
					continue
				}
				content := chatRequest.Messages[messageIndex].ParseContent()
				changed := false
				for contentIndex := range content {
					if len(content[contentIndex].CacheControl) == 0 {
						continue
					}
					content[contentIndex].CacheControl = nil
					changed = true
				}
				if changed {
					chatRequest.Messages[messageIndex].SetMediaContent(content)
				}
			}
		}
		return adaptor.ConvertOpenAIRequest(c, info, chatRequest)
	case channelcompat.ProtocolMessages:
		messagesRequest, ok := converted.(*dto.ClaudeRequest)
		if !ok {
			return nil, fmt.Errorf("expected Anthropic Messages request for protocol plan, got %T", converted)
		}
		return adaptor.ConvertClaudeRequest(c, info, messagesRequest)
	case channelcompat.ProtocolResponses:
		responsesRequest, ok := converted.(*dto.OpenAIResponsesRequest)
		if !ok {
			return nil, fmt.Errorf("expected OpenAI Responses request for protocol plan, got %T", converted)
		}
		return adaptor.ConvertOpenAIResponsesRequest(c, info, *responsesRequest)
	case channelcompat.ProtocolGemini:
		geminiRequest, ok := converted.(*dto.GeminiChatRequest)
		if !ok {
			return nil, fmt.Errorf("expected Gemini generateContent request for protocol plan, got %T", converted)
		}
		return adaptor.ConvertGeminiRequest(c, info, geminiRequest)
	default:
		return nil, fmt.Errorf("unsupported upstream protocol %q", plan.UpstreamProtocol)
	}
}

func protocolPlanRequiresConversion(plan channelcompat.ProtocolPlan) bool {
	return plan.Status == channelcompat.StatusConvertible && plan.RequestProtocol != plan.UpstreamProtocol
}

func protocolPlanUses(c *gin.Context, requestProtocol, upstreamProtocol channelcompat.Protocol) bool {
	plan, ok := selectedProtocolPlan(c)
	return ok && plan.RequestProtocol == requestProtocol && plan.UpstreamProtocol == upstreamProtocol
}

func protocolFormatForPlan(plan channelcompat.ProtocolPlan) types.RelayFormat {
	switch plan.UpstreamProtocol {
	case channelcompat.ProtocolChat:
		return types.RelayFormatOpenAI
	case channelcompat.ProtocolMessages:
		return types.RelayFormatClaude
	case channelcompat.ProtocolResponses:
		return types.RelayFormatOpenAIResponses
	case channelcompat.ProtocolGemini:
		return types.RelayFormatGemini
	default:
		return ""
	}
}
