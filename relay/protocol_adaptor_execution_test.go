package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiProtocolAdaptorsUseFinalWireFormatForMessagesURL(t *testing.T) {
	tests := []struct {
		name        string
		apiType     int
		channelType int
		baseURL     string
		model       string
		wantURL     string
	}{
		{name: "ali", apiType: constant.APITypeAli, channelType: constant.ChannelTypeAli, baseURL: "https://dashscope.example", model: "qwen-max", wantURL: "https://dashscope.example/apps/anthropic/v1/messages"},
		{name: "ali explicit messages", apiType: constant.APITypeAli, channelType: constant.ChannelTypeAli, baseURL: "https://dashscope.example", model: "wan2.6", wantURL: "https://dashscope.example/apps/anthropic/v1/messages"},
		{name: "deepseek", apiType: constant.APITypeDeepSeek, channelType: constant.ChannelTypeDeepSeek, baseURL: "https://deepseek.example", model: "deepseek-v4", wantURL: "https://deepseek.example/anthropic/v1/messages"},
		{name: "moonshot", apiType: constant.APITypeMoonshot, channelType: constant.ChannelTypeMoonshot, baseURL: "https://moonshot.example", model: "kimi-k2.6", wantURL: "https://moonshot.example/anthropic/v1/messages"},
		{name: "minimax", apiType: constant.APITypeMiniMax, channelType: constant.ChannelTypeMiniMax, baseURL: "https://minimax.example", model: "MiniMax-M2.5", wantURL: "https://minimax.example/anthropic/v1/messages"},
		{name: "zhipu v4", apiType: constant.APITypeZhipuV4, channelType: constant.ChannelTypeZhipu_v4, baseURL: "https://zhipu.example", model: "glm-5", wantURL: "https://zhipu.example/api/anthropic/v1/messages"},
		{name: "volcengine coding plan", apiType: constant.APITypeVolcEngine, channelType: constant.ChannelTypeVolcEngine, baseURL: "doubao-coding-plan", model: "doubao-seed-code", wantURL: "https://ark.cn-beijing.volces.com/api/coding/v1/messages"},
		{name: "volcengine explicit messages", apiType: constant.APITypeVolcEngine, channelType: constant.ChannelTypeVolcEngine, baseURL: "https://ark.example", model: "doubao-seed-code", wantURL: "https://ark.example/v1/messages"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adaptor := GetAdaptor(test.apiType)
			require.NotNil(t, adaptor)
			info := protocolAdaptorRelayInfo(test.channelType, test.apiType, test.baseURL, test.model)
			info.RelayFormat = types.RelayFormatOpenAIResponses
			info.RequestConversionChain = []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude}
			adaptor.Init(info)

			got, err := adaptor.GetRequestURL(info)

			require.NoError(t, err)
			assert.Equal(t, test.wantURL, got)
		})
	}
}

func TestExplicitMessagesPlanKeepsProviderAdaptorOnMessagesDTO(t *testing.T) {
	tests := []struct {
		name        string
		apiType     int
		channelType int
		baseURL     string
		model       string
		wantURL     string
	}{
		{name: "ali model outside built-in heuristic", apiType: constant.APITypeAli, channelType: constant.ChannelTypeAli, baseURL: "https://dashscope.example", model: "wan2.6", wantURL: "https://dashscope.example/apps/anthropic/v1/messages"},
		{name: "volcengine custom messages base", apiType: constant.APITypeVolcEngine, channelType: constant.ChannelTypeVolcEngine, baseURL: "https://ark.example", model: "doubao-seed-code", wantURL: "https://ark.example/v1/messages"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			plan := channelcompat.ProtocolPlan{
				RequestProtocol:      channelcompat.ProtocolMessages,
				UpstreamProtocol:     channelcompat.ProtocolMessages,
				Status:               channelcompat.StatusNative,
				ExplicitCapabilities: true,
			}
			common.SetContextKey(ctx, constant.ContextKeyProtocolPlan, plan)

			info := protocolAdaptorRelayInfo(test.channelType, test.apiType, test.baseURL, test.model)
			info.RelayFormat = types.RelayFormatClaude
			info.InitRequestConversionChain()
			adaptor := GetAdaptorForProtocol(test.apiType, plan.UpstreamProtocol)
			require.NotNil(t, adaptor)
			restore := applyProtocolPlan(info, plan)
			defer restore()

			request := &dto.ClaudeRequest{Model: "public-model", MaxTokens: common.GetPointer(uint(64))}
			converted, err := convertRequestForProtocolPlan(ctx, info, adaptor, plan, request)

			require.NoError(t, err)
			require.IsType(t, &dto.ClaudeRequest{}, converted)
			relaycommon.AppendRequestConversionFromRequest(info, converted)
			assert.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())

			requestURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, test.wantURL, requestURL)

			header := http.Header{}
			require.NoError(t, adaptor.SetupRequestHeader(ctx, &header, info))
			assert.Equal(t, "2023-06-01", header.Get("anthropic-version"))
		})
	}
}

func TestExplicitResponsesPlanUsesResponsesWireForOpenAIAndAzure(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		baseURL     string
		wantURL     string
		wantHeader  string
	}{
		{
			name:        "openai",
			channelType: constant.ChannelTypeOpenAI,
			baseURL:     "https://api.openai.example",
			wantURL:     "https://api.openai.example/v1/responses",
			wantHeader:  "Authorization",
		},
		{
			name:        "azure",
			channelType: constant.ChannelTypeAzure,
			baseURL:     "https://resource.openai.azure.com",
			wantURL:     "https://resource.openai.azure.com/openai/v1/responses?api-version=preview",
			wantHeader:  "api-key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			plan := channelcompat.ProtocolPlan{
				RequestProtocol:      channelcompat.ProtocolMessages,
				UpstreamProtocol:     channelcompat.ProtocolResponses,
				RequestConverter:     relayconvert.ConverterClaudeMessagesToOpenAIResponses,
				Status:               channelcompat.StatusConvertible,
				ExplicitCapabilities: true,
			}
			common.SetContextKey(ctx, constant.ContextKeyProtocolPlan, plan)

			info := protocolAdaptorRelayInfo(test.channelType, constant.APITypeOpenAI, test.baseURL, "gpt-5.4")
			info.RelayFormat = types.RelayFormatClaude
			info.InitRequestConversionChain()
			adaptor := GetAdaptorForProtocol(info.ApiType, plan.UpstreamProtocol)
			require.NotNil(t, adaptor)
			adaptor.Init(info)
			restore := applyProtocolPlan(info, plan)
			defer restore()

			request := &dto.ClaudeRequest{
				Model:     "gpt-5.4",
				System:    "follow the system instruction",
				Messages:  []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
				MaxTokens: common.GetPointer(uint(64)),
				Tools: []dto.Tool{{
					Name: "lookup",
					InputSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"q": map[string]any{"type": "string"}},
					},
				}},
			}
			converted, err := convertRequestForProtocolPlan(ctx, info, adaptor, plan, request)
			require.NoError(t, err)
			responsesRequest, ok := converted.(dto.OpenAIResponsesRequest)
			require.True(t, ok)
			assert.Equal(t, "gpt-5.4", responsesRequest.Model)
			var instructions string
			require.NoError(t, common.Unmarshal(responsesRequest.Instructions, &instructions))
			assert.Equal(t, "follow the system instruction", instructions)
			assert.NotEmpty(t, responsesRequest.Input)
			assert.NotEmpty(t, responsesRequest.Tools)

			requestURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, test.wantURL, requestURL)
			header := http.Header{}
			require.NoError(t, adaptor.SetupRequestHeader(ctx, &header, info))
			if test.wantHeader == "Authorization" {
				assert.Equal(t, "Bearer sk-test", header.Get(test.wantHeader))
				assert.Empty(t, header.Get("api-key"))
			} else {
				assert.Equal(t, "sk-test", header.Get(test.wantHeader))
				assert.Empty(t, header.Get("Authorization"))
			}
		})
	}
}

func TestCommonProtocolAdaptorsAcceptCompleteEndpointBaseURLs(t *testing.T) {
	tests := []struct {
		name        string
		apiType     int
		channelType int
		protocol    channelcompat.Protocol
		relayMode   int
		requestPath string
		baseURL     string
		wantURL     string
	}{
		{
			name:        "OpenAI Chat",
			apiType:     constant.APITypeOpenAI,
			channelType: constant.ChannelTypeOpenAI,
			protocol:    channelcompat.ProtocolChat,
			relayMode:   relayconstant.RelayModeChatCompletions,
			requestPath: "/v1/chat/completions?trace=1",
			baseURL:     "https://relay.example/v1/chat/completions?gateway=1",
			wantURL:     "https://relay.example/v1/chat/completions?gateway=1&trace=1",
		},
		{
			name:        "OpenAI Responses",
			apiType:     constant.APITypeOpenAI,
			channelType: constant.ChannelTypeOpenAI,
			protocol:    channelcompat.ProtocolResponses,
			relayMode:   relayconstant.RelayModeResponses,
			requestPath: "/v1/responses",
			baseURL:     "https://relay.example/api/v1/responses",
			wantURL:     "https://relay.example/api/v1/responses",
		},
		{
			name:        "Anthropic Messages",
			apiType:     constant.APITypeAnthropic,
			channelType: constant.ChannelTypeAnthropic,
			protocol:    channelcompat.ProtocolMessages,
			relayMode:   relayconstant.RelayModeUnknown,
			requestPath: "/v1/messages",
			baseURL:     "https://relay.example/api/v1/messages",
			wantURL:     "https://relay.example/api/v1/messages",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := protocolAdaptorRelayInfo(test.channelType, test.apiType, test.baseURL, "provider-model")
			info.RelayMode = test.relayMode
			info.RequestURLPath = test.requestPath
			adaptor := GetAdaptorForProtocol(test.apiType, test.protocol)
			require.NotNil(t, adaptor)
			adaptor.Init(info)

			got, err := adaptor.GetRequestURL(info)

			require.NoError(t, err)
			assert.Equal(t, test.wantURL, got)
		})
	}
}

func TestMultiProtocolAdaptorsApplyMessagesHeadersWithoutLeakingConvertedClientBeta(t *testing.T) {
	for _, test := range multiProtocolAdaptorCases() {
		t.Run(test.name+"/native messages", func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			ctx.Request.Header.Set("anthropic-version", "2026-07-01")
			ctx.Request.Header.Set("anthropic-beta", "native-tools-2026-07-01")
			adaptor := GetAdaptor(test.apiType)
			require.NotNil(t, adaptor)
			info := protocolAdaptorRelayInfo(test.channelType, test.apiType, test.baseURL, test.model)
			info.RelayFormat = types.RelayFormatClaude
			info.RequestConversionChain = []types.RelayFormat{types.RelayFormatClaude}
			adaptor.Init(info)
			header := http.Header{}

			require.NoError(t, adaptor.SetupRequestHeader(ctx, &header, info))
			assert.Equal(t, "2026-07-01", header.Get("anthropic-version"))
			assert.Equal(t, "native-tools-2026-07-01", header.Get("anthropic-beta"))
		})

		t.Run(test.name+"/converted responses", func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx.Request.Header.Set("anthropic-beta", "must-not-leak")
			adaptor := GetAdaptor(test.apiType)
			require.NotNil(t, adaptor)
			info := protocolAdaptorRelayInfo(test.channelType, test.apiType, test.baseURL, test.model)
			info.RelayFormat = types.RelayFormatOpenAIResponses
			info.RequestConversionChain = []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude}
			adaptor.Init(info)
			header := http.Header{}

			require.NoError(t, adaptor.SetupRequestHeader(ctx, &header, info))
			assert.Equal(t, "2023-06-01", header.Get("anthropic-version"))
			assert.Empty(t, header.Get("anthropic-beta"))
		})
	}
}

func TestMultiProtocolAdaptorsParseMessagesUpstreamForResponsesClient(t *testing.T) {
	for _, test := range multiProtocolAdaptorCases() {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

			adaptor := GetAdaptor(test.apiType)
			require.NotNil(t, adaptor)
			info := protocolAdaptorRelayInfo(test.channelType, test.apiType, test.baseURL, test.model)
			info.RelayFormat = types.RelayFormatOpenAIResponses
			info.RelayMode = relayconstant.RelayModeUnknown
			info.RequestConversionChain = []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude}
			adaptor.Init(info)

			usage, apiErr := adaptor.DoResponse(ctx, &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"id":"msg_upstream","type":"message","role":"assistant","model":"provider-model",
					"content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn",
					"usage":{"input_tokens":5,"output_tokens":2}
				}`)),
			}, info)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			var response dto.OpenAIResponsesResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, "response", response.Object)
			assert.Equal(t, "public-model", response.Model)
			require.Len(t, response.Output, 1)
			assert.Equal(t, "hello", response.Output[0].Content[0].Text)
		})
	}
}

func TestMultiProtocolAdaptorsParseChatUpstreamForMessagesClient(t *testing.T) {
	for _, test := range multiProtocolAdaptorCases() {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

			adaptor := GetAdaptor(test.apiType)
			require.NotNil(t, adaptor)
			info := protocolAdaptorRelayInfo(test.channelType, test.apiType, test.baseURL, test.model)
			info.RelayFormat = types.RelayFormatClaude
			info.RelayMode = relayconstant.RelayModeChatCompletions
			info.RequestConversionChain = []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI}
			info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
			adaptor.Init(info)

			usage, apiErr := adaptor.DoResponse(ctx, &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"id":"chatcmpl_upstream","object":"chat.completion","created":1710000000,"model":"provider-model",
					"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],
					"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
				}`)),
			}, info)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			var response dto.ClaudeResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, "message", response.Type)
			assert.Equal(t, "public-model", response.Model)
			require.Len(t, response.Content, 1)
			assert.Equal(t, "hello", response.Content[0].GetText())
		})
	}
}

type multiProtocolAdaptorCase struct {
	name        string
	apiType     int
	channelType int
	baseURL     string
	model       string
}

func multiProtocolAdaptorCases() []multiProtocolAdaptorCase {
	return []multiProtocolAdaptorCase{
		{name: "ali explicit messages", apiType: constant.APITypeAli, channelType: constant.ChannelTypeAli, baseURL: "https://dashscope.example", model: "wan2.6"},
		{name: "deepseek", apiType: constant.APITypeDeepSeek, channelType: constant.ChannelTypeDeepSeek, baseURL: "https://deepseek.example", model: "deepseek-v4"},
		{name: "moonshot", apiType: constant.APITypeMoonshot, channelType: constant.ChannelTypeMoonshot, baseURL: "https://moonshot.example", model: "kimi-k2.6"},
		{name: "minimax", apiType: constant.APITypeMiniMax, channelType: constant.ChannelTypeMiniMax, baseURL: "https://minimax.example", model: "MiniMax-M2.5"},
		{name: "zhipu v4", apiType: constant.APITypeZhipuV4, channelType: constant.ChannelTypeZhipu_v4, baseURL: "https://zhipu.example", model: "glm-5"},
		{name: "volcengine explicit messages", apiType: constant.APITypeVolcEngine, channelType: constant.ChannelTypeVolcEngine, baseURL: "https://ark.example", model: "doubao-seed-code"},
	}
}

func protocolAdaptorRelayInfo(channelType, apiType int, baseURL, model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: "public-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       channelType,
			ApiType:           apiType,
			ChannelBaseUrl:    baseURL,
			UpstreamModelName: model,
			ApiKey:            "sk-test",
			IsModelMapped:     true,
		},
	}
}
