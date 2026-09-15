package aws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
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

func CountTokens(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (int, *hosttypes.NewAPIError) {
	client, err := newAwsClient(c, info)
	if err != nil {
		return 0, hosttypes.NewError(err, hosttypes.ErrorCodeChannelAwsClientError, hosttypes.ErrOptionWithSkipRetry())
	}
	return countTokensWithClient(c, info, request, client)
}

func countTokensWithClient(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest, client countTokensClient) (int, *hosttypes.NewAPIError) {
	if info == nil || request == nil || client == nil {
		return 0, hosttypes.NewErrorWithStatusCode(errors.New("invalid AWS count_tokens request"), hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}

	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertClaudeRequest(c, info, request)
	if err != nil {
		return 0, hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
	}
	requestData, err := common.Marshal(converted)
	if err != nil {
		return 0, hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
	}
	requestHeader, err := buildAwsRequestHeader(c, info, adaptor)
	if err != nil {
		return 0, hosttypes.NewError(err, hosttypes.ErrorCodeConvertRequestFailed, hosttypes.ErrOptionWithSkipRetry())
	}
	awsClaudeRequest, err := formatRequest(bytes.NewReader(requestData), requestHeader)
	if err != nil {
		return 0, hosttypes.NewError(err, hosttypes.ErrorCodeBadRequestBody, hosttypes.ErrOptionWithSkipRetry())
	}
	body, err := buildAwsRequestBody(c, info, awsClaudeRequest)
	if err != nil {
		return 0, hosttypes.NewError(err, hosttypes.ErrorCodeBadRequestBody, hosttypes.ErrOptionWithSkipRetry())
	}

	modelID := resolveAwsModelID(info.UpstreamModelName, client.Options().Region)
	requestContext := context.Background()
	if c != nil && c.Request != nil {
		requestContext = c.Request.Context()
	}
	ctx, cancel := newAwsInvokeContext(requestContext)
	defer cancel()
	output, err := client.CountTokens(ctx, &bedrockruntime.CountTokensInput{
		ModelId: awsSDK.String(modelID),
		Input: &bedrockruntimeTypes.CountTokensInputMemberInvokeModel{
			Value: bedrockruntimeTypes.InvokeModelTokensRequest{Body: body},
		},
	})
	if err != nil {
		if requestContext.Err() != nil {
			return 0, newAwsInvokeError(requestContext, err, "CountTokens")
		}
		statusCode := getAwsErrorStatusCode(err)
		if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented {
			unsupported := fmt.Errorf("%w: %v", ErrCountTokensUnsupported, err)
			return 0, hosttypes.NewErrorWithStatusCode(unsupported, hosttypes.ErrorCodeAwsInvokeError, statusCode, hosttypes.ErrOptionWithSkipRetry())
		}
		return 0, hosttypes.NewOpenAIError(fmt.Errorf("AWS CountTokens: %w", err), hosttypes.ErrorCodeAwsInvokeError, statusCode, hosttypes.ErrOptionWithSkipRetry())
	}
	if output == nil || output.InputTokens == nil || *output.InputTokens < 0 {
		return 0, hosttypes.NewOpenAIError(errors.New("AWS CountTokens returned invalid input_tokens"), hosttypes.ErrorCodeBadResponseBody, http.StatusBadGateway, hosttypes.ErrOptionWithSkipRetry())
	}
	return int(*output.InputTokens), nil
}
