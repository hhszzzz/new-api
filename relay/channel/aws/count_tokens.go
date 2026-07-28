package aws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	awsSDK "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
)

var ErrCountTokensUnsupported = errors.New("AWS Bedrock CountTokens is unsupported")

type countTokensClient interface {
	CountTokens(context.Context, *bedrockruntime.CountTokensInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.CountTokensOutput, error)
	Options() bedrockruntime.Options
}

func CountTokens(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (int, *types.NewAPIError) {
	client, err := newAwsClient(c, info)
	if err != nil {
		return 0, types.NewError(err, types.ErrorCodeChannelAwsClientError, types.ErrOptionWithSkipRetry())
	}
	return countTokensWithClient(c, info, request, client)
}

func countTokensWithClient(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest, client countTokensClient) (int, *types.NewAPIError) {
	if info == nil || request == nil || client == nil {
		return 0, types.NewErrorWithStatusCode(errors.New("invalid AWS count_tokens request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertClaudeRequest(c, info, request)
	if err != nil {
		return 0, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	requestData, err := common.Marshal(converted)
	if err != nil {
		return 0, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	requestHeader, err := buildAwsRequestHeader(c, info, adaptor)
	if err != nil {
		return 0, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	awsClaudeRequest, err := formatRequest(bytes.NewReader(requestData), requestHeader)
	if err != nil {
		return 0, types.NewError(err, types.ErrorCodeBadRequestBody, types.ErrOptionWithSkipRetry())
	}
	body, err := buildAwsRequestBody(c, info, awsClaudeRequest)
	if err != nil {
		return 0, types.NewError(err, types.ErrorCodeBadRequestBody, types.ErrOptionWithSkipRetry())
	}

	modelID := resolveAwsModelID(info.UpstreamModelName, client.Options().Region)
	ctx, cancel := newAwsInvokeContext()
	defer cancel()
	output, err := client.CountTokens(ctx, &bedrockruntime.CountTokensInput{
		ModelId: awsSDK.String(modelID),
		Input: &bedrockruntimeTypes.CountTokensInputMemberInvokeModel{
			Value: bedrockruntimeTypes.InvokeModelTokensRequest{Body: body},
		},
	})
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented {
			unsupported := fmt.Errorf("%w: %v", ErrCountTokensUnsupported, err)
			return 0, types.NewErrorWithStatusCode(unsupported, types.ErrorCodeAwsInvokeError, statusCode, types.ErrOptionWithSkipRetry())
		}
		return 0, types.NewOpenAIError(fmt.Errorf("AWS CountTokens: %w", err), types.ErrorCodeAwsInvokeError, statusCode, types.ErrOptionWithSkipRetry())
	}
	if output == nil || output.InputTokens == nil || *output.InputTokens < 0 {
		return 0, types.NewOpenAIError(errors.New("AWS CountTokens returned invalid input_tokens"), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	return int(*output.InputTokens), nil
}
