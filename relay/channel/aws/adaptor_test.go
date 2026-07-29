package aws

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyModeUsesSDKBearerAuthentication(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:            "bedrock-secret|us-west-2",
		UpstreamModelName: "claude-sonnet-4-6",
		ChannelOtherSettings: dto.ChannelOtherSettings{
			AwsKeyType: dto.AwsKeyTypeApiKey,
		},
	}}
	adaptor := &Adaptor{}

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Empty(t, requestURL)

	header := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(ctx, &header, info))
	assert.Empty(t, header.Get("Authorization"))

	client, err := newAwsClient(ctx, info)
	require.NoError(t, err)
	options := client.Options()
	assert.Equal(t, "us-west-2", options.Region)
	assert.Equal(t, []string{"httpBearerAuth"}, options.AuthSchemePreference)
	require.NotNil(t, options.BearerAuthTokenProvider)
	token, err := options.BearerAuthTokenProvider.RetrieveBearerToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "bedrock-secret", token.Value)
}

func TestAPIKeyModeBuildsAnthropicInvokeModelRequest(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:            "bedrock-secret|us-west-2",
		UpstreamModelName: "claude-sonnet-4-6",
		ChannelOtherSettings: dto.ChannelOtherSettings{
			AwsKeyType: dto.AwsKeyTypeApiKey,
		},
	}}
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`))
	require.NoError(t, err)

	invokeRequest, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)
	assert.Equal(t, "us.anthropic.claude-sonnet-4-6", *invokeRequest.ModelId)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(invokeRequest.Body, &payload))
	assert.Equal(t, "bedrock-2023-05-31", payload["anthropic_version"])
	assert.Contains(t, payload, "messages")
	assert.NotContains(t, payload, "inferenceConfig")
}

func TestAPIKeyModeRejectsMissingCredentialOrRegion(t *testing.T) {
	tests := []string{"bedrock-secret", "|us-west-2", "bedrock-secret|"}
	for _, apiKey := range tests {
		t.Run(apiKey, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				ApiKey: apiKey,
				ChannelOtherSettings: dto.ChannelOtherSettings{
					AwsKeyType: dto.AwsKeyTypeApiKey,
				},
			}}

			_, err := (&Adaptor{}).GetRequestURL(info)
			require.Error(t, err)
		})
	}
}

func TestConvertOpenAIRequestBuildsNovaMessagesV1ToolLoop(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	zeroFloat := 0.0
	zeroInt := 0
	zeroTokens := uint(0)
	assistant := dto.Message{Role: "assistant"}
	assistant.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_lookup",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{"q":"weather"}`,
		},
	}})
	request := &dto.GeneralOpenAIRequest{
		Model:       "nova-pro-v1:0",
		MaxTokens:   &zeroTokens,
		Temperature: &zeroFloat,
		TopP:        &zeroFloat,
		TopK:        &zeroInt,
		Messages: []dto.Message{
			{Role: "system", Content: "follow policy"},
			{Role: "user", Content: "check weather"},
			assistant,
			{Role: "tool", ToolCallId: "call_lookup", Content: `{"temperature":23}`},
		},
		Tools: []dto.ToolCallRequest{{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        "lookup",
				Description: "Lookup weather",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
			},
		}},
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: request.Model}}
	adaptor := &Adaptor{}

	converted, err := adaptor.ConvertOpenAIRequest(ctx, info, request)

	require.NoError(t, err)
	assert.True(t, adaptor.IsNova)
	novaRequest, ok := converted.(*NovaRequest)
	require.True(t, ok)
	require.Len(t, novaRequest.System, 1)
	assert.Equal(t, "follow policy", novaRequest.System[0].Text)
	require.Len(t, novaRequest.Messages, 3)
	require.NotNil(t, novaRequest.Messages[1].Content[0].ToolUse)
	assert.Equal(t, "call_lookup", novaRequest.Messages[1].Content[0].ToolUse.ToolUseID)
	assert.Equal(t, map[string]any{"q": "weather"}, novaRequest.Messages[1].Content[0].ToolUse.Input)
	require.NotNil(t, novaRequest.Messages[2].Content[0].ToolResult)
	assert.Equal(t, "call_lookup", novaRequest.Messages[2].Content[0].ToolResult.ToolUseID)
	assert.Equal(t, `{"temperature":23}`, novaRequest.Messages[2].Content[0].ToolResult.Content[0].Text)
	require.NotNil(t, novaRequest.InferenceConfig)
	require.NotNil(t, novaRequest.InferenceConfig.MaxTokens)
	require.NotNil(t, novaRequest.InferenceConfig.Temperature)
	require.NotNil(t, novaRequest.InferenceConfig.TopP)
	require.NotNil(t, novaRequest.InferenceConfig.TopK)
	assert.Zero(t, *novaRequest.InferenceConfig.MaxTokens)
	assert.Zero(t, *novaRequest.InferenceConfig.Temperature)
	assert.Zero(t, *novaRequest.InferenceConfig.TopP)
	assert.Zero(t, *novaRequest.InferenceConfig.TopK)
	require.NotNil(t, novaRequest.ToolConfig)
	require.Len(t, novaRequest.ToolConfig.Tools, 1)
	assert.Equal(t, "lookup", novaRequest.ToolConfig.Tools[0].ToolSpec.Name)
	assert.Equal(t, map[string]any{"tool": map[string]any{"name": "lookup"}}, novaRequest.ToolConfig.ToolChoice)
}

func TestConvertOpenAIRequestRejectsNovaContentItCannotRepresent(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request := &dto.GeneralOpenAIRequest{
		Model: "nova-pro-v1:0",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{map[string]any{
				"type":      dto.ContentTypeImageURL,
				"image_url": map[string]any{"url": "https://example.com/image.png"},
			}},
		}},
	}

	_, err := (&Adaptor{}).ConvertOpenAIRequest(ctx, &relaycommon.RelayInfo{}, request)

	require.ErrorContains(t, err, "unsupported image_url content")
}

func TestNovaToolUseResponseRestoresResponsesCustomTool(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, engine := gin.CreateTestContext(recorder)
	engine.ContextWithFallback = true
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request = ctx.Request.WithContext(relayconvert.WithProtocolBridgeContext(ctx.Request.Context()))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "public-nova",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "amazon.nova-pro-v1:0",
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model: "amazon.nova-pro-v1:0",
		Input: mustAWSRaw(t, "run pwd"),
		Tools: mustAWSRaw(t, []map[string]any{{
			"type": "custom",
			"name": "exec",
		}}),
	}
	converted, err := relayconvert.ConvertRequest(ctx, info, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	chatRequest, ok := converted.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Tools, 1)
	upstreamToolName := chatRequest.Tools[0].Function.Name

	responseErr, usage := relayNovaResponse(ctx, info, []byte(`{
		"output":{"message":{"content":[{"toolUse":{"toolUseId":"call_exec","name":"`+upstreamToolName+`","input":{"input":"pwd"}}}]}},
		"stopReason":"tool_use",
		"usage":{"inputTokens":5,"outputTokens":3,"totalTokens":8}
	}`))

	require.Nil(t, responseErr)
	require.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), `"type":"custom_tool_call"`)
	assert.Contains(t, recorder.Body.String(), `"name":"exec"`)
	assert.Contains(t, recorder.Body.String(), `"input":"pwd"`)
}

func mustAWSRaw(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}
