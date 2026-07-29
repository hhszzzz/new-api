package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	claudechannel "github.com/QuantumNous/new-api/relay/channel/claude"
	geminichannel "github.com/QuantumNous/new-api/relay/channel/gemini"
	openai "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
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
	case channelcompat.ProtocolGemini:
		modelName := strings.TrimSpace(plan.EffectiveUpstreamModel)
		if modelName == "" {
			modelName = strings.TrimSpace(info.UpstreamModelName)
		}
		if modelName == "" {
			modelName = "model"
		}
		action := "generateContent"
		if plan.Features.Stream {
			action = "streamGenerateContent?alt=sse"
		}
		info.RequestURLPath = "/v1beta/models/" + modelName + ":" + action
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
		if plan.RequestProtocol == channelcompat.ProtocolMessages {
			protocolstate.ApplyMessagesContinuation(c, responsesRequest)
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

func protocolPlanRequiresStructuredRequest(info *relaycommon.RelayInfo, plan channelcompat.ProtocolPlan) bool {
	return info != nil &&
		plan.UpstreamProtocol == channelcompat.ProtocolResponses &&
		info.ApiType == constant.APITypeXai
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

// handleBufferedStreamResponse converts an upstream SSE stream into a buffered
// JSON response for a non-streaming client. When it reports handled=true the
// upstream body has been consumed, so the caller must not fall through to
// adaptor.DoResponse; a handled response without usage is a hard error because
// billing would otherwise be skipped silently.
func handleBufferedStreamResponse(c *gin.Context, info *relaycommon.RelayInfo, httpResp *http.Response, upstreamFormat types.RelayFormat, statusCodeMappingStr string) (*dto.Usage, bool, *types.NewAPIError) {
	var usage *dto.Usage
	var apiError *types.NewAPIError
	switch upstreamFormat {
	case types.RelayFormatOpenAI:
		usage, apiError = openai.OaiChatBufferedStreamHandler(c, info, httpResp)
	case types.RelayFormatOpenAIResponses:
		usage, apiError = openai.OaiResponsesToChatBufferedStreamHandler(c, info, httpResp)
	case types.RelayFormatClaude:
		usage, apiError = claudechannel.ClaudeBufferedStreamHandler(c, info, httpResp)
	case types.RelayFormatGemini:
		usage, apiError = geminichannel.GeminiBufferedStreamHandler(c, info, httpResp)
	default:
		return nil, false, nil
	}
	if apiError != nil {
		service.ResetStatusCode(apiError, statusCodeMappingStr)
		return nil, true, apiError
	}
	if usage == nil {
		return nil, true, types.NewOpenAIError(errors.New("buffered stream handler returned no usage"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	return usage, true, nil
}
