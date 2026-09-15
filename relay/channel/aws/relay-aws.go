package aws

import (
	"bytes"
	"context"
	"fmt"
	hostdto "github.com/QuantumNous/new-api/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/smithy-go/auth/bearer"
)

// getAwsErrorStatusCode extracts HTTP status code from AWS SDK error
func getAwsErrorStatusCode(err error) int {
	// Check for HTTP response error which contains status code
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		return httpErr.HTTPStatusCode()
	}
	// Default to 500 if we can't determine the status code
	return http.StatusInternalServerError
}

func newAwsInvokeContext(parent context.Context) (context.Context, context.CancelFunc) {
	if common.RelayTimeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, time.Duration(common.RelayTimeout)*time.Second)
}

func newAwsInvokeError(requestContext context.Context, err error, operation string) *hosttypes.NewAPIError {
	options := make([]hosttypes.NewAPIErrorOptions, 0, 1)
	if requestContext.Err() != nil {
		options = append(options, hosttypes.ErrOptionWithSkipRetry())
	}
	return hosttypes.NewOpenAIError(
		errors.Wrap(err, operation),
		hosttypes.ErrorCodeAwsInvokeError,
		getAwsErrorStatusCode(err),
		options...,
	)
}

func newAwsClient(c *gin.Context, info *relaycommon.RelayInfo) (*bedrockruntime.Client, error) {
	httpClient, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}

	var client *bedrockruntime.Client
	if info.ChannelOtherSettings.AwsKeyType == hostdto.AwsKeyTypeApiKey {
		apiKey, region, err := parseBedrockAPIKey(info.ApiKey)
		if err != nil {
			return nil, err
		}
		client = bedrockruntime.New(bedrockruntime.Options{
			Region:                  region,
			BearerAuthTokenProvider: bearer.StaticTokenProvider{Token: bearer.Token{Value: apiKey}},
			AuthSchemePreference:    []string{"httpBearerAuth"},
			HTTPClient:              httpClient,
		})
	} else {
		awsSecret := strings.SplitN(info.ApiKey, "|", 3)
		if len(awsSecret) != 3 || strings.TrimSpace(awsSecret[0]) == "" || strings.TrimSpace(awsSecret[1]) == "" || strings.TrimSpace(awsSecret[2]) == "" {
			return nil, errors.New("invalid aws secret key, should be in format of <access-key>|<secret-key>|<region>")
		}
		ak := strings.TrimSpace(awsSecret[0])
		sk := strings.TrimSpace(awsSecret[1])
		region := strings.TrimSpace(awsSecret[2])
		client = bedrockruntime.New(bedrockruntime.Options{
			Region:      region,
			Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(ak, sk, "")),
			HTTPClient:  httpClient,
		})
	}

	return client, nil
}

func prepareAwsRequest(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor, requestBody io.Reader) error {
	awsCli, err := newAwsClient(c, info)
	if err != nil {
		return hosttypes.NewError(err, hosttypes.ErrorCodeChannelAwsClientError)
	}
	a.AwsClient = awsCli

	awsModelId := resolveAwsModelID(info.UpstreamModelName, awsCli.Options().Region)

	requestHeader, err := buildAwsRequestHeader(c, info, a)
	if err != nil {
		return err
	}

	a.IsNova = isNovaModel(awsModelId)
	if a.IsNova {
		var novaReq *NovaRequest
		err = common.DecodeJson(requestBody, &novaReq)
		if err != nil {
			return hosttypes.NewError(errors.Wrap(err, "decode nova request fail"), hosttypes.ErrorCodeBadRequestBody)
		}

		// 使用InvokeModel API，但使用Nova格式的请求体
		awsReq := &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(awsModelId),
			Accept:      aws.String("application/json"),
			ContentType: aws.String("application/json"),
		}

		reqBody, err := common.Marshal(novaReq)
		if err != nil {
			return hosttypes.NewError(errors.Wrap(err, "marshal nova request"), hosttypes.ErrorCodeBadResponseBody)
		}
		awsReq.Body = reqBody
		a.InvokeInput = awsReq
		return nil
	} else {
		awsClaudeReq, err := formatRequest(requestBody, requestHeader)
		if err != nil {
			return hosttypes.NewError(errors.Wrap(err, "format aws request fail"), hosttypes.ErrorCodeBadRequestBody)
		}

		if info.IsStream {
			awsReq := &bedrockruntime.InvokeModelWithResponseStreamInput{
				ModelId:     aws.String(awsModelId),
				Accept:      aws.String("application/json"),
				ContentType: aws.String("application/json"),
			}
			awsReq.Body, err = buildAwsRequestBody(c, info, awsClaudeReq)
			if err != nil {
				return hosttypes.NewError(errors.Wrap(err, "marshal aws request fail"), hosttypes.ErrorCodeBadRequestBody)
			}
			a.StreamInput = awsReq
			return nil
		} else {
			awsReq := &bedrockruntime.InvokeModelInput{
				ModelId:     aws.String(awsModelId),
				Accept:      aws.String("application/json"),
				ContentType: aws.String("application/json"),
			}
			awsReq.Body, err = buildAwsRequestBody(c, info, awsClaudeReq)
			if err != nil {
				return hosttypes.NewError(errors.Wrap(err, "marshal aws request fail"), hosttypes.ErrorCodeBadRequestBody)
			}
			a.InvokeInput = awsReq
			return nil
		}
	}
}

func buildAwsRequestHeader(c *gin.Context, info *relaycommon.RelayInfo, adaptor *Adaptor) (http.Header, error) {
	requestHeader := http.Header{}
	if err := adaptor.SetupRequestHeader(c, &requestHeader, info); err != nil {
		return nil, err
	}
	headerOverride, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	for key, value := range headerOverride {
		requestHeader.Set(key, value)
	}
	return requestHeader, nil
}

// buildAwsRequestBody prepares the payload for AWS requests, applying passthrough rules when enabled.
func buildAwsRequestBody(c *gin.Context, info *relaycommon.RelayInfo, awsClaudeReq any) ([]byte, error) {
	if info.ShouldPassThroughBody() {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, errors.Wrap(err, "get request body for pass-through fail")
		}
		body, err := storage.Bytes()
		if err != nil {
			return nil, errors.Wrap(err, "get request body bytes fail")
		}
		var data map[string]any
		if err := common.Unmarshal(body, &data); err != nil {
			return nil, errors.Wrap(err, "pass-through unmarshal request body fail")
		}
		delete(data, "model")
		delete(data, "stream")
		return common.Marshal(data)
	}
	return common.Marshal(awsClaudeReq)
}

func getAwsRegionPrefix(awsRegionId string) string {
	parts := strings.Split(awsRegionId, "-")
	regionPrefix := ""
	if len(parts) > 0 {
		regionPrefix = parts[0]
	}
	return regionPrefix
}

func awsModelCanCrossRegion(awsModelId, awsRegionPrefix string) bool {
	regionSet, exists := awsModelCanCrossRegionMap[awsModelId]
	return exists && regionSet[awsRegionPrefix]
}

func awsModelCrossRegion(awsModelId, awsRegionPrefix string) string {
	modelPrefix, find := awsRegionCrossModelPrefixMap[awsRegionPrefix]
	if !find {
		return awsModelId
	}
	return modelPrefix + "." + awsModelId
}

func getAwsModelID(requestModel string) string {
	if awsModelIDName, ok := awsModelIDMap[requestModel]; ok {
		return awsModelIDName
	}
	return requestModel
}

func resolveAwsModelID(requestModel, region string) string {
	modelID := getAwsModelID(requestModel)
	regionPrefix := getAwsRegionPrefix(region)
	if awsModelCanCrossRegion(modelID, regionPrefix) {
		return awsModelCrossRegion(modelID, regionPrefix)
	}
	return modelID
}

func decodeNovaResponse(c *gin.Context, info *relaycommon.RelayInfo, body []byte) (*http.Response, *hosttypes.NewAPIError) {
	// Parse the native Nova response, then hand a synthetic Chat Completions
	// response to the shared OpenAI response pipeline. The protocol plan sets
	// RelayFormat to the client protocol, so the shared handler also restores
	// Responses or Messages JSON/SSE rather than leaking Chat JSON to clients.
	var novaResp struct {
		Output struct {
			Message struct {
				Content []NovaContent `json:"content"`
			} `json:"message"`
		} `json:"output"`
		Usage struct {
			InputTokens           int `json:"inputTokens"`
			OutputTokens          int `json:"outputTokens"`
			TotalTokens           int `json:"totalTokens"`
			CacheReadInputTokens  int `json:"cacheReadInputTokenCount"`
			CacheWriteInputTokens int `json:"cacheWriteInputTokenCount"`
		} `json:"usage"`
		StopReason string `json:"stopReason"`
	}

	if err := common.Unmarshal(body, &novaResp); err != nil {
		return nil, hosttypes.NewError(errors.Wrap(err, "unmarshal nova response"), hosttypes.ErrorCodeBadResponseBody)
	}
	if len(novaResp.Output.Message.Content) == 0 {
		return nil, hosttypes.NewError(errors.New("nova response contains no content"), hosttypes.ErrorCodeBadResponseBody)
	}

	var content strings.Builder
	toolCalls := make([]dto.ToolCallResponse, 0)
	for _, block := range novaResp.Output.Message.Content {
		content.WriteString(block.Text)
		if block.ToolUse == nil {
			continue
		}
		arguments, err := common.Marshal(block.ToolUse.Input)
		if err != nil {
			return nil, hosttypes.NewError(errors.Wrap(err, "marshal nova tool input"), hosttypes.ErrorCodeBadResponseBody)
		}
		toolCalls = append(toolCalls, dto.ToolCallResponse{
			ID:   block.ToolUse.ToolUseID,
			Type: "function",
			Function: dto.FunctionResponse{
				Name:      block.ToolUse.Name,
				Arguments: string(arguments),
			},
		})
	}
	finishReason := "stop"
	switch strings.ToLower(strings.TrimSpace(novaResp.StopReason)) {
	case "max_tokens":
		finishReason = "length"
	case "tool_use":
		finishReason = "tool_calls"
	case "content_filtered", "guardrail_intervened":
		finishReason = "content_filter"
	}
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	message := dto.Message{Role: "assistant"}
	if content.Len() > 0 {
		message.Content = content.String()
	}
	if len(toolCalls) > 0 {
		message.SetToolCalls(toolCalls)
	}

	response := dto.OpenAITextResponse{
		Id:      helper.GetResponseID(c),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Model:   info.PublicResponseModelName(),
		Choices: []dto.OpenAITextResponseChoice{{
			Index:        0,
			Message:      message,
			FinishReason: finishReason,
		}},
		Usage: dto.Usage{
			PromptTokens:     novaResp.Usage.InputTokens,
			CompletionTokens: novaResp.Usage.OutputTokens,
			TotalTokens:      novaResp.Usage.TotalTokens,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         novaResp.Usage.CacheReadInputTokens,
				CachedCreationTokens: novaResp.Usage.CacheWriteInputTokens,
			},
		},
	}

	responseBody, err := common.Marshal(response)
	if err != nil {
		return nil, hosttypes.NewError(errors.Wrap(err, "marshal nova chat response"), hosttypes.ErrorCodeBadResponseBody)
	}
	httpResponse := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}
	return httpResponse, nil
}
