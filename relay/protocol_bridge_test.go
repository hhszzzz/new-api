package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAdaptorForProtocolUsesWireProtocolForGenericProtocolChannels(t *testing.T) {
	assert.IsType(t, &openai.Adaptor{}, GetAdaptorForProtocol(constant.APITypeOpenAI, channelcompat.ProtocolChat))
	assert.IsType(t, &openai.Adaptor{}, GetAdaptorForProtocol(constant.APITypeOpenAI, channelcompat.ProtocolResponses))
	assert.IsType(t, &claude.Adaptor{}, GetAdaptorForProtocol(constant.APITypeOpenAI, channelcompat.ProtocolMessages))
	assert.IsType(t, &gemini.Adaptor{}, GetAdaptorForProtocol(constant.APITypeOpenAI, channelcompat.ProtocolGemini))

	assert.IsType(t, &claude.Adaptor{}, GetAdaptorForProtocol(constant.APITypeAnthropic, channelcompat.ProtocolMessages))
	assert.IsType(t, &openai.Adaptor{}, GetAdaptorForProtocol(constant.APITypeAnthropic, channelcompat.ProtocolChat))
	assert.IsType(t, &openai.Adaptor{}, GetAdaptorForProtocol(constant.APITypeAnthropic, channelcompat.ProtocolResponses))
	assert.IsType(t, &gemini.Adaptor{}, GetAdaptorForProtocol(constant.APITypeAnthropic, channelcompat.ProtocolGemini))

	assert.IsType(t, &gemini.Adaptor{}, GetAdaptorForProtocol(constant.APITypeGemini, channelcompat.ProtocolGemini))
	assert.IsType(t, &openai.Adaptor{}, GetAdaptorForProtocol(constant.APITypeGemini, channelcompat.ProtocolChat))
	assert.IsType(t, &openai.Adaptor{}, GetAdaptorForProtocol(constant.APITypeGemini, channelcompat.ProtocolResponses))
	assert.IsType(t, &claude.Adaptor{}, GetAdaptorForProtocol(constant.APITypeGemini, channelcompat.ProtocolMessages))

	assert.IsType(t, &openai.Adaptor{}, GetAdaptorForProtocol(constant.APITypeOpenRouter, channelcompat.ProtocolResponses))
	assert.IsType(t, &claude.Adaptor{}, GetAdaptorForProtocol(constant.APITypeOpenRouter, channelcompat.ProtocolMessages))
	assert.IsType(t, &gemini.Adaptor{}, GetAdaptorForProtocol(constant.APITypeOpenRouter, channelcompat.ProtocolGemini))
	assert.IsType(t, &openai.Adaptor{}, GetAdaptorForProtocol(constant.APITypeXinference, channelcompat.ProtocolChat))
	assert.IsType(t, &claude.Adaptor{}, GetAdaptorForProtocol(constant.APITypeXinference, channelcompat.ProtocolMessages))
}

func TestProtocolPlanRequiresStructuredRequestForXAIResponses(t *testing.T) {
	plan := channelcompat.ProtocolPlan{UpstreamProtocol: channelcompat.ProtocolResponses}
	xaiInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeXai}}
	openAIInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI}}
	assert.True(t, protocolPlanRequiresStructuredRequest(xaiInfo, plan))
	assert.False(t, protocolPlanRequiresStructuredRequest(openAIInfo, plan))
	assert.False(t, protocolPlanRequiresStructuredRequest(xaiInfo, channelcompat.ProtocolPlan{UpstreamProtocol: channelcompat.ProtocolChat}))
}

func TestProtocolBridgeRequestMatrixAppliesUpstreamModeAndConversion(t *testing.T) {
	tests := []struct {
		name             string
		requestProtocol  channelcompat.Protocol
		upstreamProtocol channelcompat.Protocol
		status           channelcompat.Status
		converter        string
		wantRelayMode    int
		wantUpstreamPath string
		requestForStream func(bool) dto.Request
	}{
		{
			name:             "Responses to Responses",
			requestProtocol:  channelcompat.ProtocolResponses,
			upstreamProtocol: channelcompat.ProtocolResponses,
			status:           channelcompat.StatusNative,
			wantRelayMode:    relayconstant.RelayModeResponses,
			wantUpstreamPath: "/v1/responses",
			requestForStream: protocolBridgeResponsesRequest,
		},
		{
			name:             "Responses to Chat",
			requestProtocol:  channelcompat.ProtocolResponses,
			upstreamProtocol: channelcompat.ProtocolChat,
			status:           channelcompat.StatusConvertible,
			converter:        relayconvert.ConverterOpenAIResponsesToOpenAIChat,
			wantRelayMode:    relayconstant.RelayModeChatCompletions,
			wantUpstreamPath: "/v1/chat/completions",
			requestForStream: protocolBridgeResponsesRequest,
		},
		{
			name:             "Responses to Messages",
			requestProtocol:  channelcompat.ProtocolResponses,
			upstreamProtocol: channelcompat.ProtocolMessages,
			status:           channelcompat.StatusConvertible,
			converter:        relayconvert.ConverterOpenAIResponsesToClaudeMessages,
			wantRelayMode:    relayconstant.RelayModeUnknown,
			wantUpstreamPath: "/v1/messages",
			requestForStream: protocolBridgeResponsesRequest,
		},
		{
			name:             "Messages to Messages",
			requestProtocol:  channelcompat.ProtocolMessages,
			upstreamProtocol: channelcompat.ProtocolMessages,
			status:           channelcompat.StatusNative,
			wantRelayMode:    relayconstant.RelayModeUnknown,
			wantUpstreamPath: "/v1/messages",
			requestForStream: protocolBridgeMessagesRequest,
		},
		{
			name:             "Messages to Chat",
			requestProtocol:  channelcompat.ProtocolMessages,
			upstreamProtocol: channelcompat.ProtocolChat,
			status:           channelcompat.StatusConvertible,
			converter:        relayconvert.ConverterClaudeMessagesToOpenAIChat,
			wantRelayMode:    relayconstant.RelayModeChatCompletions,
			wantUpstreamPath: "/v1/chat/completions",
			requestForStream: protocolBridgeMessagesRequest,
		},
		{
			name:             "Messages to Responses",
			requestProtocol:  channelcompat.ProtocolMessages,
			upstreamProtocol: channelcompat.ProtocolResponses,
			status:           channelcompat.StatusConvertible,
			converter:        relayconvert.ConverterClaudeMessagesToOpenAIResponses,
			wantRelayMode:    relayconstant.RelayModeResponses,
			wantUpstreamPath: "/v1/responses",
			requestForStream: protocolBridgeMessagesRequest,
		},
	}

	for _, test := range tests {
		for _, stream := range []bool{false, true} {
			streamName := "non-streaming"
			if stream {
				streamName = "streaming"
			}
			t.Run(test.name+"/"+streamName, func(t *testing.T) {
				entryPath := "/v1/responses"
				entryMode := relayconstant.RelayModeResponses
				entryFormat := types.RelayFormat(types.RelayFormatOpenAIResponses)
				if test.requestProtocol == channelcompat.ProtocolMessages {
					entryPath = "/v1/messages"
					entryMode = relayconstant.RelayModeUnknown
					entryFormat = types.RelayFormat(types.RelayFormatClaude)
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, entryPath, nil)
				info := &relaycommon.RelayInfo{
					RelayFormat:     entryFormat,
					RelayMode:       entryMode,
					RequestURLPath:  entryPath,
					OriginModelName: "public-model",
					ChannelMeta: &relaycommon.ChannelMeta{
						UpstreamModelName: "provider-model",
					},
				}
				plan := channelcompat.ProtocolPlan{
					RequestProtocol:   test.requestProtocol,
					UpstreamProtocol:  test.upstreamProtocol,
					RequestConverter:  test.converter,
					ResponseConverter: test.converter,
					Status:            test.status,
					Features:          channelcompat.RequestFeatureSet{Stream: stream},
				}

				restore := applyProtocolPlan(info, plan)
				assert.Equal(t, test.wantRelayMode, info.RelayMode)
				assert.Equal(t, test.wantUpstreamPath, info.RequestURLPath)

				adaptor := &protocolBridgeRecordingAdaptor{}
				converted, err := convertRequestForProtocolPlan(c, info, adaptor, plan, test.requestForStream(stream))
				require.NoError(t, err)
				require.NotNil(t, converted)
				assert.Equal(t, test.upstreamProtocol, adaptor.protocol)
				assertProtocolBridgeStream(t, adaptor, test.upstreamProtocol, stream)

				restore()
				assert.Equal(t, entryMode, info.RelayMode)
				assert.Equal(t, entryPath, info.RequestURLPath)
			})
		}
	}
}

func TestProtocolBridgeCompactUsesChatEndpointForConvertiblePlan(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponsesCompact,
		RequestURLPath: "/v1/responses/compact",
	}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		Status:           channelcompat.StatusConvertible,
	}

	restore := applyProtocolPlan(info, plan)
	assert.Equal(t, relayconstant.RelayModeChatCompletions, info.RelayMode)
	assert.Equal(t, "/v1/chat/completions", info.RequestURLPath)

	restore()
	assert.Equal(t, relayconstant.RelayModeResponsesCompact, info.RelayMode)
	assert.Equal(t, "/v1/responses/compact", info.RequestURLPath)
}

func TestApplyProtocolPlanUsesGeminiWirePathWithoutChangingEntryResponseMode(t *testing.T) {
	tests := []struct {
		name            string
		requestProtocol channelcompat.Protocol
		entryMode       int
		entryPath       string
	}{
		{
			name:            "Responses entry",
			requestProtocol: channelcompat.ProtocolResponses,
			entryMode:       relayconstant.RelayModeResponses,
			entryPath:       "/v1/responses",
		},
		{
			name:            "Messages entry",
			requestProtocol: channelcompat.ProtocolMessages,
			entryMode:       relayconstant.RelayModeUnknown,
			entryPath:       "/v1/messages",
		},
	}

	for _, test := range tests {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", test.name, stream), func(t *testing.T) {
				info := &relaycommon.RelayInfo{
					RelayMode:      test.entryMode,
					RequestURLPath: test.entryPath,
					ChannelMeta: &relaycommon.ChannelMeta{
						UpstreamModelName: "gemini-2.5-pro",
					},
				}
				plan := channelcompat.ProtocolPlan{
					RequestProtocol:        test.requestProtocol,
					UpstreamProtocol:       channelcompat.ProtocolGemini,
					EffectiveUpstreamModel: "gemini-2.5-flash",
					Status:                 channelcompat.StatusConvertible,
					Features:               channelcompat.RequestFeatureSet{Stream: stream},
				}

				restore := applyProtocolPlan(info, plan)

				assert.Equal(t, test.entryMode, info.RelayMode)
				if stream {
					assert.Equal(t, "/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse", info.RequestURLPath)
				} else {
					assert.Equal(t, "/v1beta/models/gemini-2.5-flash:generateContent", info.RequestURLPath)
				}

				restore()
				assert.Equal(t, test.entryMode, info.RelayMode)
				assert.Equal(t, test.entryPath, info.RequestURLPath)
			})
		}
	}
}

func TestResponsesHelperAllowsCompactBridgeForChatOnlyAPIType(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	originalPolicy := settings.ProtocolBridgePolicy
	settings.ProtocolBridgePolicy.Enabled = false
	t.Cleanup(func() { settings.ProtocolBridgePolicy = originalPolicy })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDeepSeek)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-public")
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		RequestConverter: relayconvert.ConverterOpenAIResponsesToOpenAIChat,
		Status:           channelcompat.StatusConvertible,
	})

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		RequestURLPath:  "/v1/responses/compact",
		OriginModelName: "gpt-public",
		Request: &dto.OpenAIResponsesCompactionRequest{
			Model: "gpt-public",
			Tools: json.RawMessage(`[
				{"type":"function","name":"crm__lookup"},
				{"type":"namespace","name":"crm","tools":[{"type":"function","name":"lookup"}]}
			]`),
		},
	}

	apiErr := ResponsesHelper(c, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeConvertRequestFailed, apiErr.GetErrorCode())
	assert.Contains(t, apiErr.Error(), "conflicts after Chat name encoding")
	assert.NotContains(t, apiErr.Error(), "unsupported endpoint")
}

func TestProtocolBridgeMessagesToChatStripsAnthropicCacheControl(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"claude-public",
		"max_tokens":64,
		"system":[{"type":"text","text":"rules","cache_control":{"type":"ephemeral"}}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}]
	}`), &request))
	original, err := common.Marshal(request)
	require.NoError(t, err)
	require.Contains(t, string(original), "cache_control")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-public",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-chat-model",
		},
	}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolChat,
		RequestConverter: relayconvert.ConverterClaudeMessagesToOpenAIChat,
		Status:           channelcompat.StatusConvertible,
	}
	adaptor := &protocolBridgeRecordingAdaptor{}

	_, err = convertRequestForProtocolPlan(c, info, adaptor, plan, &request)
	require.NoError(t, err)
	require.NotNil(t, adaptor.chatRequest)
	upstream, err := common.Marshal(adaptor.chatRequest)
	require.NoError(t, err)
	assert.NotContains(t, string(upstream), "cache_control")
}

func TestProtocolBridgeMessagesToResponsesAppliesSessionStateBeforeAdaptorConversion(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	common.SetContextKey(c, constant.ContextKeyUserId, 501)
	common.SetContextKey(c, constant.ContextKeyTokenId, 502)
	request := &dto.ClaudeRequest{
		Model:        "claude-public",
		Messages:     []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
		MaxTokens:    common.GetPointer[uint](64),
		CacheControl: json.RawMessage(`{"type":"ephemeral"}`),
	}
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-public",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         503,
			UpstreamModelName: "provider-responses-model",
		},
	}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		RequestConverter: relayconvert.ConverterClaudeMessagesToOpenAIResponses,
		Status:           channelcompat.StatusConvertible,
		StateEnabled:     true,
	}
	require.NoError(t, protocolstate.PrepareMessagesRequest(c, info, plan, request))
	adaptor := &protocolBridgeRecordingAdaptor{}

	_, err := convertRequestForProtocolPlan(c, info, adaptor, plan, request)

	require.NoError(t, err)
	require.NotNil(t, adaptor.responsesRequest)
	assert.NotEmpty(t, common.JsonRawMessageToString(adaptor.responsesRequest.PromptCacheKey))
}

func TestResponsesToChatUsesMappedModelBeforeOpenAIAdaptorRoleNormalization(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("model_mapping", `{"gpt-5.4":"openpangu-2.0-flash"}`)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.4",
		Input: json.RawMessage(`[
			{"role":"developer","content":[{"type":"input_text","text":"Follow project instructions."}]},
			{"role":"user","content":"hello"}
		]`),
	}
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-5.4",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	require.NoError(t, helper.ModelMappedHelper(c, info, request))
	assert.Equal(t, "openpangu-2.0-flash", request.Model)
	assert.Equal(t, "openpangu-2.0-flash", info.UpstreamModelName)

	converted, err := convertRequestForProtocolPlan(c, info, &openai.Adaptor{}, channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		RequestConverter: relayconvert.ConverterOpenAIResponsesToOpenAIChat,
		Status:           channelcompat.StatusConvertible,
	}, request)
	require.NoError(t, err)
	chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, "openpangu-2.0-flash", chatRequest.Model)
	require.Len(t, chatRequest.Messages, 2)
	assert.Equal(t, "system", chatRequest.Messages[0].Role)
	assert.Equal(t, "Follow project instructions.", chatRequest.Messages[0].StringContent())
}

func TestProtocolBridgeRequestConversionConflictIsClientError(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-public",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider-chat-model",
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Tools: json.RawMessage(`[
			{"type":"function","name":"crm__lookup"},
			{"type":"namespace","name":"crm","tools":[{"type":"function","name":"lookup"}]}
		]`),
	}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		RequestConverter: relayconvert.ConverterOpenAIResponsesToOpenAIChat,
		Status:           channelcompat.StatusConvertible,
	}

	_, err := convertRequestForProtocolPlan(c, info, &protocolBridgeRecordingAdaptor{}, plan, request)

	require.Error(t, err)
	var apiError *types.NewAPIError
	require.True(t, errors.As(err, &apiError))
	assert.Equal(t, http.StatusBadRequest, apiError.StatusCode)
	assert.Equal(t, types.ErrorCodeConvertRequestFailed, apiError.GetErrorCode())
	assert.Contains(t, apiError.Error(), "conflicts after Chat name encoding")
}

func protocolBridgeResponsesRequest(stream bool) dto.Request {
	return &dto.OpenAIResponsesRequest{
		Model:  "public-model",
		Input:  json.RawMessage(`"hello"`),
		Stream: common.GetPointer(stream),
	}
}

func protocolBridgeMessagesRequest(stream bool) dto.Request {
	return &dto.ClaudeRequest{
		Model:     "public-model",
		Messages:  []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
		MaxTokens: common.GetPointer[uint](64),
		Stream:    common.GetPointer(stream),
	}
}

func assertProtocolBridgeStream(t *testing.T, adaptor *protocolBridgeRecordingAdaptor, protocol channelcompat.Protocol, want bool) {
	t.Helper()
	switch protocol {
	case channelcompat.ProtocolChat:
		require.NotNil(t, adaptor.chatRequest)
		require.NotNil(t, adaptor.chatRequest.Stream)
		assert.Equal(t, want, *adaptor.chatRequest.Stream)
	case channelcompat.ProtocolMessages:
		require.NotNil(t, adaptor.messagesRequest)
		require.NotNil(t, adaptor.messagesRequest.Stream)
		assert.Equal(t, want, *adaptor.messagesRequest.Stream)
	case channelcompat.ProtocolResponses:
		require.NotNil(t, adaptor.responsesRequest)
		require.NotNil(t, adaptor.responsesRequest.Stream)
		assert.Equal(t, want, *adaptor.responsesRequest.Stream)
	default:
		require.Failf(t, "unsupported protocol", "protocol %q", protocol)
	}
}

type protocolBridgeRecordingAdaptor struct {
	protocol         channelcompat.Protocol
	chatRequest      *dto.GeneralOpenAIRequest
	messagesRequest  *dto.ClaudeRequest
	responsesRequest *dto.OpenAIResponsesRequest
}

var _ channel.Adaptor = (*protocolBridgeRecordingAdaptor)(nil)

func (a *protocolBridgeRecordingAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *protocolBridgeRecordingAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return "", nil
}

func (a *protocolBridgeRecordingAdaptor) SetupRequestHeader(*gin.Context, *http.Header, *relaycommon.RelayInfo) error {
	return nil
}

func (a *protocolBridgeRecordingAdaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	a.protocol = channelcompat.ProtocolChat
	a.chatRequest = request
	return request, nil
}

func (a *protocolBridgeRecordingAdaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *protocolBridgeRecordingAdaptor) ConvertEmbeddingRequest(*gin.Context, *relaycommon.RelayInfo, dto.EmbeddingRequest) (any, error) {
	return nil, nil
}

func (a *protocolBridgeRecordingAdaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, nil
}

func (a *protocolBridgeRecordingAdaptor) ConvertImageRequest(*gin.Context, *relaycommon.RelayInfo, dto.ImageRequest) (any, error) {
	return nil, nil
}

func (a *protocolBridgeRecordingAdaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	a.protocol = channelcompat.ProtocolResponses
	a.responsesRequest = &request
	return &request, nil
}

func (a *protocolBridgeRecordingAdaptor) DoRequest(*gin.Context, *relaycommon.RelayInfo, io.Reader) (any, error) {
	return nil, nil
}

func (a *protocolBridgeRecordingAdaptor) DoResponse(*gin.Context, *http.Response, *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	return nil, nil
}

func (a *protocolBridgeRecordingAdaptor) GetModelList() []string { return nil }

func (a *protocolBridgeRecordingAdaptor) GetChannelName() string { return "test" }

func (a *protocolBridgeRecordingAdaptor) ConvertClaudeRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	a.protocol = channelcompat.ProtocolMessages
	a.messagesRequest = request
	return request, nil
}

func (a *protocolBridgeRecordingAdaptor) ConvertGeminiRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return request, nil
}
