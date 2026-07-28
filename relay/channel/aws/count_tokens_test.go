package aws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCountTokensClient struct {
	options bedrockruntime.Options
	input   *bedrockruntime.CountTokensInput
	output  *bedrockruntime.CountTokensOutput
	err     error
}

func (f *fakeCountTokensClient) CountTokens(_ context.Context, input *bedrockruntime.CountTokensInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.CountTokensOutput, error) {
	f.input = input
	return f.output, f.err
}

func (f *fakeCountTokensClient) Options() bedrockruntime.Options {
	return f.options
}

type fakeAWSHTTPError struct {
	status int
}

func (e fakeAWSHTTPError) Error() string {
	return http.StatusText(e.status)
}

func (e fakeAWSHTTPError) HTTPStatusCode() int {
	return e.status
}

func TestCountTokensWithClientUsesInvokeModelBodyAndCrossRegionModel(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}
	request := &dto.ClaudeRequest{
		Model:  "claude-3-5-sonnet-20240620",
		System: "system",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}
	inputTokens := int32(29)
	client := &fakeCountTokensClient{
		options: bedrockruntime.Options{Region: "us-east-1"},
		output:  &bedrockruntime.CountTokensOutput{InputTokens: &inputTokens},
	}

	tokens, apiError := countTokensWithClient(ctx, info, request, client)

	require.Nil(t, apiError)
	assert.Equal(t, 29, tokens)
	require.NotNil(t, client.input)
	require.NotNil(t, client.input.ModelId)
	assert.Equal(t, "us.anthropic.claude-3-5-sonnet-20240620-v1:0", *client.input.ModelId)
	member, ok := client.input.Input.(*bedrockruntimeTypes.CountTokensInputMemberInvokeModel)
	require.True(t, ok)
	var body map[string]any
	require.NoError(t, common.Unmarshal(member.Value.Body, &body))
	assert.Equal(t, "bedrock-2023-05-31", body["anthropic_version"])
	assert.Equal(t, "system", body["system"])
	assert.NotContains(t, body, "model")
}

func TestCountTokensWithClientMarksOnlyUnsupportedStatusesForFallback(t *testing.T) {
	for _, statusCode := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented, http.StatusInternalServerError} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "model"}}
			client := &fakeCountTokensClient{
				options: bedrockruntime.Options{Region: "us-east-1"},
				err:     fakeAWSHTTPError{status: statusCode},
			}

			_, apiError := countTokensWithClient(ctx, info, &dto.ClaudeRequest{Model: "model"}, client)

			require.NotNil(t, apiError)
			assert.Equal(t, statusCode, apiError.StatusCode)
			if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented {
				assert.True(t, errors.Is(apiError.Err, ErrCountTokensUnsupported))
			} else {
				assert.False(t, errors.Is(apiError.Err, ErrCountTokensUnsupported))
			}
		})
	}
}
