package aws

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBedrockClaudeEventsProduceResponsesSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "claude-public",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-upstream",
		},
	}
	claudeInfo := &claude.ClaudeResponseInfo{
		Model:        info.PublicResponseModelName(),
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	events := []string{
		`{"type":"message_start","message":{"id":"msg_bedrock","model":"claude-upstream","content":[],"usage":{"input_tokens":4,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
	}
	for _, event := range events {
		require.Nil(t, claude.HandleStreamResponseData(c, info, claudeInfo, event))
	}
	require.Nil(t, claude.HandleStreamFinalResponse(c, info, claudeInfo))

	body := recorder.Body.String()
	assert.Contains(t, body, "event: response.created")
	assert.Contains(t, body, "event: response.output_text.delta")
	assert.Contains(t, body, `"delta":"hello"`)
	assert.Contains(t, body, "event: response.completed")
	assert.Contains(t, body, `"model":"claude-public"`)
	require.NotNil(t, claudeInfo.Usage)
	assert.Equal(t, 4, claudeInfo.Usage.PromptTokens)
	assert.Equal(t, 2, claudeInfo.Usage.CompletionTokens)
}

func TestRelayNovaResponseRestoresClientProtocol(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	tests := []struct {
		name           string
		format         types.RelayFormat
		stream         bool
		expectedOutput []string
	}{
		{
			name:           "Responses JSON",
			format:         types.RelayFormatOpenAIResponses,
			expectedOutput: []string{`"object":"response"`, `"model":"public-nova"`, `"type":"output_text"`, `"text":"hello world"`},
		},
		{
			name:           "Messages JSON",
			format:         types.RelayFormatClaude,
			expectedOutput: []string{`"type":"message"`, `"model":"public-nova"`, `"text":"hello world"`},
		},
		{
			name:           "Responses SSE",
			format:         types.RelayFormatOpenAIResponses,
			stream:         true,
			expectedOutput: []string{"event: response.created", "event: response.output_text.delta", `"delta":"hello world"`, "event: response.completed"},
		},
		{
			name:           "Messages SSE",
			format:         types.RelayFormatClaude,
			stream:         true,
			expectedOutput: []string{`"type":"message_start"`, `"type":"text_delta"`, `"text":"hello world"`, `"type":"message_stop"`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{
				RelayFormat:     test.format,
				RelayMode:       relayconstant.RelayModeChatCompletions,
				IsStream:        test.stream,
				OriginModelName: "public-nova",
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "amazon.nova-pro-v1:0",
				},
			}

			responseErr, usage := relayNovaResponse(c, info, []byte(`{
				"output":{"message":{"content":[{"text":"hello "},{"text":"world"}]}},
				"stopReason":"end_turn",
				"usage":{"inputTokens":4,"outputTokens":2,"totalTokens":6}
			}`))

			require.Nil(t, responseErr)
			require.NotNil(t, usage)
			assert.Equal(t, 4, usage.PromptTokens)
			assert.Equal(t, 2, usage.CompletionTokens)
			assert.Equal(t, 6, usage.TotalTokens)
			for _, expected := range test.expectedOutput {
				assert.Contains(t, recorder.Body.String(), expected)
			}
		})
	}
}

func TestRelayNovaResponseRejectsMissingContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		OriginModelName: "public-nova",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "amazon.nova-pro-v1:0",
		},
	}

	responseErr, usage := relayNovaResponse(c, info, []byte(`{"output":{"message":{"content":[]}}}`))

	require.NotNil(t, responseErr)
	assert.Contains(t, responseErr.Error(), "no content")
	assert.Nil(t, usage)
}

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}
