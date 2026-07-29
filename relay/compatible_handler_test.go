package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextHelperAppliesSystemPromptsBeforeProviderConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	globalSettings := model_setting.GetGlobalSettings()
	originalPassThrough := globalSettings.PassThroughRequestEnabled
	originalResponsesPolicy := globalSettings.ChatCompletionsToResponsesPolicy
	globalSettings.PassThroughRequestEnabled = false
	globalSettings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	t.Cleanup(func() {
		globalSettings.PassThroughRequestEnabled = originalPassThrough
		globalSettings.ChatCompletionsToResponsesPolicy = originalResponsesPolicy
	})

	const (
		routePrompt   = "route prompt"
		channelPrompt = "channel prompt"
		clientPrompt  = "client prompt"
	)
	wantPrompt := routePrompt + "\n" + channelPrompt + "\n" + clientPrompt

	tests := []struct {
		name         string
		channelType  int
		targetModel  string
		assertPrompt func(t *testing.T, body []byte)
	}{
		{
			name:        "Claude",
			channelType: constant.ChannelTypeAnthropic,
			targetModel: "claude-test",
			assertPrompt: func(t *testing.T, body []byte) {
				var request dto.ClaudeRequest
				require.NoError(t, common.Unmarshal(body, &request))
				system := request.ParseSystem()
				require.Len(t, system, 1)
				require.NotNil(t, system[0].Text)
				assert.Equal(t, wantPrompt, *system[0].Text)
			},
		},
		{
			name:        "Gemini",
			channelType: constant.ChannelTypeGemini,
			targetModel: "gemini-test",
			assertPrompt: func(t *testing.T, body []byte) {
				var request dto.GeminiChatRequest
				require.NoError(t, common.Unmarshal(body, &request))
				require.NotNil(t, request.SystemInstructions)
				require.Len(t, request.SystemInstructions.Parts, 1)
				assert.Equal(t, wantPrompt, request.SystemInstructions.Parts[0].Text)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody []byte
			var captureErr error
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedBody, captureErr = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"stop after capture","type":"invalid_request_error"}}`))
			}))
			defer upstream.Close()

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(ctx, constant.ContextKeyChannelType, tt.channelType)
			common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(ctx, constant.ContextKeyChannelKey, "test-key")
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "requested-model")
			common.SetContextKey(ctx, constant.ContextKeyUserModelRouteTarget, tt.targetModel)
			common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{
				SystemPrompt:         channelPrompt,
				SystemPromptOverride: true,
			})

			request := &dto.GeneralOpenAIRequest{
				Model: "requested-model",
				Messages: []dto.Message{
					{Role: "system", Content: clientPrompt},
					{Role: "user", Content: "hello"},
				},
			}
			info := &relaycommon.RelayInfo{
				RelayMode:            relayconstant.RelayModeChatCompletions,
				RelayFormat:          types.RelayFormatOpenAI,
				OriginModelName:      "requested-model",
				UserModelRouteId:     7,
				RouteTargetModelName: tt.targetModel,
				RouteInjectPrompt:    routePrompt,
				Request:              request,
			}

			relayErr := TextHelper(ctx, info)

			require.NotNil(t, relayErr)
			require.NoError(t, captureErr)
			require.NotEmpty(t, capturedBody)
			tt.assertPrompt(t, capturedBody)
		})
	}
}

func TestTextHelperBuffersUnexpectedChatStreamForNonStreamClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	globalSettings := model_setting.GetGlobalSettings()
	originalPassThrough := globalSettings.PassThroughRequestEnabled
	originalResponsesPolicy := globalSettings.ChatCompletionsToResponsesPolicy
	globalSettings.PassThroughRequestEnabled = false
	globalSettings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	t.Cleanup(func() {
		globalSettings.PassThroughRequestEnabled = originalPassThrough
		globalSettings.ChatCompletionsToResponsesPolicy = originalResponsesPolicy
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_partial\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "test-key")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-test")

	stream := false
	request := &dto.GeneralOpenAIRequest{
		Model:  "gpt-test",
		Stream: &stream,
		Messages: []dto.Message{{
			Role:    "user",
			Content: "hello",
		}},
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-test",
		Request:         request,
	}

	relayErr := TextHelper(ctx, info)

	require.NotNil(t, relayErr)
	assert.Contains(t, relayErr.Error(), "terminal finish_reason")
	assert.Empty(t, recorder.Body.String())
	assert.NotContains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
}
