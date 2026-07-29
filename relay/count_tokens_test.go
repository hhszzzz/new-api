package relay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelaws "github.com/QuantumNous/new-api/relay/channel/aws"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountTokensHelperForwardsNativeAnthropicRequest(t *testing.T) {
	var (
		receivedPath   string
		receivedHeader string
		receivedBody   map[string]any
		decodeErr      error
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedHeader = r.Header.Get("x-api-key")
		decodeErr = common.DecodeJson(r.Body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":37}`))
	}))
	defer server.Close()

	c, info, recorder := newCountTokensTestContext(t, server.URL, constant.ChannelTypeAnthropic, dto.ChannelSettings{
		SystemPrompt:         "channel system",
		SystemPromptOverride: true,
	})
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolMessages,
		Status:           channelcompat.StatusNative,
	})

	require.Nil(t, CountTokensHelper(c, info))
	require.NoError(t, decodeErr)
	assert.Equal(t, "/v1/messages/count_tokens", receivedPath)
	assert.Equal(t, "test-key", receivedHeader)
	assert.Equal(t, "claude-upstream", receivedBody["model"])
	assert.Equal(t, "channel system\nclient system", receivedBody["system"])
	assert.NotContains(t, receivedBody, "stream")
	assert.NotContains(t, receivedBody, "temperature")
	assert.JSONEq(t, `{"input_tokens":37}`, recorder.Body.String())
}

func TestCountTokensHelperForwardsNativeMessagesCompatibleGateways(t *testing.T) {
	tests := []struct {
		name                 string
		channelType          int
		otherSettings        dto.ChannelOtherSettings
		explicitCapabilities bool
		wantPath             string
		wantAuth             string
		wantAPIKey           string
		wantAnthropicVersion string
	}{
		{
			name:                 "Sub2API",
			channelType:          constant.ChannelTypeSub2API,
			wantPath:             "/v1/messages/count_tokens",
			wantAuth:             "Bearer test-key",
			wantAPIKey:           "test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:                 "NewAPI",
			channelType:          constant.ChannelTypeNewAPI,
			wantPath:             "/v1/messages/count_tokens",
			wantAuth:             "Bearer test-key",
			wantAPIKey:           "test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:                 "OpenAI compatible explicitly declared as Messages",
			channelType:          constant.ChannelTypeOpenAI,
			explicitCapabilities: true,
			wantPath:             "/v1/messages/count_tokens",
			wantAPIKey:           "test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:                 "Ali",
			channelType:          constant.ChannelTypeAli,
			wantPath:             "/apps/anthropic/v1/messages/count_tokens",
			wantAuth:             "Bearer test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:                 "DeepSeek",
			channelType:          constant.ChannelTypeDeepSeek,
			wantPath:             "/anthropic/v1/messages/count_tokens",
			wantAuth:             "Bearer test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:                 "Moonshot",
			channelType:          constant.ChannelTypeMoonshot,
			wantPath:             "/anthropic/v1/messages/count_tokens",
			wantAuth:             "Bearer test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:                 "MiniMax",
			channelType:          constant.ChannelTypeMiniMax,
			wantPath:             "/anthropic/v1/messages/count_tokens",
			wantAuth:             "Bearer test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:                 "VolcEngine",
			channelType:          constant.ChannelTypeVolcEngine,
			wantPath:             "/v1/messages/count_tokens",
			wantAuth:             "Bearer test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:                 "Zhipu v4",
			channelType:          constant.ChannelTypeZhipu_v4,
			wantPath:             "/api/anthropic/v1/messages/count_tokens",
			wantAuth:             "Bearer test-key",
			wantAnthropicVersion: "2023-06-01",
		},
		{
			name:        "Advanced Custom",
			channelType: constant.ChannelTypeAdvancedCustom,
			otherSettings: dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/messages/count_tokens",
					UpstreamPath: "/provider/messages/count_tokens",
					Converter:    relayconvert.ConverterNone,
					Auth: &dto.AdvancedCustomRouteAuth{
						Type:  dto.AdvancedCustomAuthTypeHeader,
						Name:  "x-api-key",
						Value: "{api_key}",
					},
				},
			}}},
			wantPath:             "/provider/messages/count_tokens",
			wantAPIKey:           "test-key",
			wantAnthropicVersion: "2023-06-01",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				receivedPath             string
				receivedAuth             string
				receivedAPIKey           string
				receivedAnthropicVersion string
				receivedBody             map[string]any
				decodeErr                error
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				receivedAuth = r.Header.Get("Authorization")
				receivedAPIKey = r.Header.Get("x-api-key")
				receivedAnthropicVersion = r.Header.Get("anthropic-version")
				decodeErr = common.DecodeJson(r.Body, &receivedBody)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"input_tokens":41}`))
			}))
			defer server.Close()

			c, info, recorder := newCountTokensTestContext(t, server.URL, test.channelType, dto.ChannelSettings{}, test.otherSettings)
			common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
				RequestProtocol:      channelcompat.ProtocolMessages,
				UpstreamProtocol:     channelcompat.ProtocolMessages,
				Status:               channelcompat.StatusNative,
				ExplicitCapabilities: test.explicitCapabilities,
			})

			require.Nil(t, CountTokensHelper(c, info))
			require.NoError(t, decodeErr)
			assert.Equal(t, test.wantPath, receivedPath)
			assert.Equal(t, test.wantAuth, receivedAuth)
			assert.Equal(t, test.wantAPIKey, receivedAPIKey)
			assert.Equal(t, test.wantAnthropicVersion, receivedAnthropicVersion)
			assert.Equal(t, "claude-upstream", receivedBody["model"])
			assert.JSONEq(t, `{"input_tokens":41}`, recorder.Body.String())
			assert.Nil(t, info.Billing)
		})
	}
}

func TestCountTokensHelperFallsBackLocallyOnlyForUnsupportedStatuses(t *testing.T) {
	originalCountToken := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = originalCountToken })

	for _, statusCode := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"not_found_error","message":"unsupported"}}`))
			}))
			defer server.Close()

			c, info, recorder := newCountTokensTestContext(t, server.URL, constant.ChannelTypeAnthropic, dto.ChannelSettings{})
			common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
				RequestProtocol:  channelcompat.ProtocolMessages,
				UpstreamProtocol: channelcompat.ProtocolMessages,
				Status:           channelcompat.StatusNative,
			})

			require.Nil(t, CountTokensHelper(c, info))
			var response dto.ClaudeCountTokensResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Greater(t, response.InputTokens, 0)
		})
	}
}

func TestCountTokensHelperUsesLocalEstimateForConvertedProtocol(t *testing.T) {
	originalCountToken := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = originalCountToken })

	c, info, recorder := newCountTokensTestContext(t, "http://127.0.0.1:1", constant.ChannelTypeOpenAI, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
	})

	require.Nil(t, CountTokensHelper(c, info))
	var response dto.ClaudeCountTokensResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Greater(t, response.InputTokens, 0)
}

func TestCountTokensHelperUsesLocalEstimateForAdvancedCustomRoutesWithoutAuxiliaryEndpoint(t *testing.T) {
	originalCountToken := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = originalCountToken })

	tests := []struct {
		name             string
		upstreamProtocol channelcompat.Protocol
		status           channelcompat.Status
		converter        string
	}{
		{
			name:             "Chat",
			upstreamProtocol: channelcompat.ProtocolChat,
			status:           channelcompat.StatusConvertible,
			converter:        relayconvert.ConverterClaudeMessagesToOpenAIChat,
		},
		{
			name:             "Responses",
			upstreamProtocol: channelcompat.ProtocolResponses,
			status:           channelcompat.StatusConvertible,
			converter:        relayconvert.ConverterClaudeMessagesToOpenAIResponses,
		},
		{
			name:             "Messages",
			upstreamProtocol: channelcompat.ProtocolMessages,
			status:           channelcompat.StatusNative,
			converter:        relayconvert.ConverterNone,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstreamRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				upstreamRequests++
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()

			c, info, recorder := newCountTokensTestContext(
				t,
				server.URL,
				constant.ChannelTypeAdvancedCustom,
				dto.ChannelSettings{},
				dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
					{
						IncomingPath: "/v1/messages",
						UpstreamPath: "/provider/text",
						Converter:    test.converter,
					},
				}}},
			)
			common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
				RequestProtocol:  channelcompat.ProtocolMessages,
				UpstreamProtocol: test.upstreamProtocol,
				Status:           test.status,
			})

			require.Nil(t, CountTokensHelper(c, info))
			var response dto.ClaudeCountTokensResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Greater(t, response.InputTokens, 0)
			assert.Zero(t, upstreamRequests)
			assert.Nil(t, info.Billing)
		})
	}
}

func TestCountTokensHelperDoesNotFallbackForUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"upstream failed"}}`))
	}))
	defer server.Close()

	c, info, recorder := newCountTokensTestContext(t, server.URL, constant.ChannelTypeAnthropic, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolMessages,
		Status:           channelcompat.StatusNative,
	})

	newAPIError := CountTokensHelper(c, info)
	require.NotNil(t, newAPIError)
	assert.Equal(t, http.StatusInternalServerError, newAPIError.StatusCode)
	assert.Empty(t, recorder.Body.String())
}

func TestCountTokensHelperUsesAWSCountTokensAndOnlyFallsBackWhenUnsupported(t *testing.T) {
	originalCountToken := constant.CountToken
	originalAWSCountTokens := countAWSInputTokens
	constant.CountToken = false
	t.Cleanup(func() {
		constant.CountToken = originalCountToken
		countAWSInputTokens = originalAWSCountTokens
	})

	t.Run("success", func(t *testing.T) {
		countAWSInputTokens = func(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.ClaudeRequest) (int, *types.NewAPIError) {
			return 43, nil
		}
		c, info, recorder := newCountTokensTestContext(t, "", constant.ChannelTypeAws, dto.ChannelSettings{})
		common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
			RequestProtocol:  channelcompat.ProtocolMessages,
			UpstreamProtocol: channelcompat.ProtocolMessages,
			Status:           channelcompat.StatusNative,
		})

		require.Nil(t, CountTokensHelper(c, info))
		assert.JSONEq(t, `{"input_tokens":43}`, recorder.Body.String())
		assert.Nil(t, info.Billing)
	})

	t.Run("unsupported fallback", func(t *testing.T) {
		countAWSInputTokens = func(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.ClaudeRequest) (int, *types.NewAPIError) {
			return 0, types.NewErrorWithStatusCode(
				channelaws.ErrCountTokensUnsupported,
				types.ErrorCodeAwsInvokeError,
				http.StatusNotImplemented,
			)
		}
		c, info, recorder := newCountTokensTestContext(t, "", constant.ChannelTypeAws, dto.ChannelSettings{})
		common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
			RequestProtocol:  channelcompat.ProtocolMessages,
			UpstreamProtocol: channelcompat.ProtocolMessages,
			Status:           channelcompat.StatusNative,
		})

		require.Nil(t, CountTokensHelper(c, info))
		var response dto.ClaudeCountTokensResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.Greater(t, response.InputTokens, 0)
	})

	t.Run("upstream failure", func(t *testing.T) {
		countAWSInputTokens = func(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.ClaudeRequest) (int, *types.NewAPIError) {
			return 0, types.NewErrorWithStatusCode(
				errors.New("AWS unavailable"),
				types.ErrorCodeAwsInvokeError,
				http.StatusInternalServerError,
			)
		}
		c, info, recorder := newCountTokensTestContext(t, "", constant.ChannelTypeAws, dto.ChannelSettings{})
		common.SetContextKey(c, constant.ContextKeyProtocolPlan, channelcompat.ProtocolPlan{
			RequestProtocol:  channelcompat.ProtocolMessages,
			UpstreamProtocol: channelcompat.ProtocolMessages,
			Status:           channelcompat.StatusNative,
		})

		apiError := CountTokensHelper(c, info)
		require.NotNil(t, apiError)
		assert.Equal(t, http.StatusInternalServerError, apiError.StatusCode)
		assert.Empty(t, recorder.Body.String())
	})
}

func newCountTokensTestContext(t *testing.T, baseURL string, channelType int, channelSetting dto.ChannelSettings, otherSettings ...dto.ChannelOtherSettings) (*gin.Context, *relaycommon.RelayInfo, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-public","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-public")
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
	common.SetContextKey(c, constant.ContextKeyChannelName, "count-test")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channelSetting)
	channelOtherSettings := dto.ChannelOtherSettings{}
	if len(otherSettings) > 0 {
		channelOtherSettings = otherSettings[0]
	}
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channelOtherSettings)
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"claude-public":"claude-upstream"}`)

	request := &dto.ClaudeRequest{
		Model:  "claude-public",
		System: "client system",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
		Stream:      common.GetPointer(true),
		Temperature: common.GetPointer(0.2),
	}
	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatClaude, request, nil)
	require.NoError(t, err)
	return c, info, recorder
}
