package relay

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
