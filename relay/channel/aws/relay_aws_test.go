package aws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream/eventstreamapi"
	"github.com/aws/aws-sdk-go-v2/credentials"
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

const awsTestModel = "anthropic.claude-3-5-sonnet-20240620-v1:0"

type awsHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f awsHTTPClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

type awsNotifyingResponseWriter struct {
	*httptest.ResponseRecorder
	notifyOn []byte
	notified chan int
	once     sync.Once
}

func newAwsNotifyingResponseWriter(notifyOn string) *awsNotifyingResponseWriter {
	return &awsNotifyingResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		notifyOn:         []byte(notifyOn),
		notified:         make(chan int, 1),
	}
}

func (w *awsNotifyingResponseWriter) Write(data []byte) (int, error) {
	return w.ResponseRecorder.Write(data)
}

func (w *awsNotifyingResponseWriter) Flush() {
	w.ResponseRecorder.Flush()
	if bytes.Contains(w.Body.Bytes(), w.notifyOn) {
		w.once.Do(func() {
			w.notified <- w.Body.Len()
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

func newAwsTestClient(httpClient bedrockruntime.HTTPClient) *bedrockruntime.Client {
	return bedrockruntime.New(bedrockruntime.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String("https://bedrock.test"),
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			"access-key", "secret-key", "",
		)),
		HTTPClient: httpClient,
		Retryer:    aws.NopRetryer{},
	})
}

func newAwsTestContext(writer http.ResponseWriter, requestContext context.Context) *gin.Context {
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	return c
}

func newAwsTestRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		StartTime:          time.Now(),
		IsStream:           true,
		OriginModelName:    awsTestModel,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: awsTestModel,
		},
	}
}

func newAwsInvokeModelInput() *bedrockruntime.InvokeModelInput {
	return &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(awsTestModel),
		Body:        []byte(`{}`),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
	}
}

func newAwsStreamInput() *bedrockruntime.InvokeModelWithResponseStreamInput {
	return &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(awsTestModel),
		Body:        []byte(`{}`),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
	}
}

func writeAwsStreamEvent(writer io.Writer, data string) error {
	payload, err := common.Marshal(struct {
		Bytes []byte `json:"bytes"`
	}{Bytes: []byte(data)})
	if err != nil {
		return err
	}

	return eventstream.NewEncoder().Encode(writer, eventstream.Message{
		Headers: eventstream.Headers{
			{Name: eventstreamapi.MessageTypeHeader, Value: eventstream.StringValue(eventstreamapi.EventMessageType)},
			{Name: eventstreamapi.EventTypeHeader, Value: eventstream.StringValue("chunk")},
			{Name: eventstreamapi.ContentTypeHeader, Value: eventstream.StringValue("application/json")},
		},
		Payload: payload,
	})
}

func newAwsStreamResponse(request *http.Request, body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type":                []string{"application/vnd.amazon.eventstream"},
			"X-Amzn-Bedrock-Content-Type": []string{"application/json"},
		},
		Body:    body,
		Request: request,
	}
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

func TestNewAwsInvokeContextInheritsParent(t *testing.T) {
	originalRelayTimeout := common.RelayTimeout
	t.Cleanup(func() {
		common.RelayTimeout = originalRelayTimeout
	})

	tests := []struct {
		name         string
		relayTimeout int
		wantDeadline bool
	}{
		{name: "without relay timeout", relayTimeout: 0, wantDeadline: false},
		{name: "with relay timeout", relayTimeout: 30, wantDeadline: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			common.RelayTimeout = test.relayTimeout
			parent, cancelParent := context.WithCancel(context.Background())
			invokeContext, cancelInvoke := newAwsInvokeContext(parent)
			defer cancelInvoke()

			_, hasDeadline := invokeContext.Deadline()
			assert.Equal(t, test.wantDeadline, hasDeadline)

			cancelParent()
			require.ErrorIs(t, invokeContext.Err(), context.Canceled)
		})
	}
}

func TestNewAwsInvokeErrorSkipsRetryOnlyForClientCancellation(t *testing.T) {
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name           string
		requestContext context.Context
		err            error
		wantSkipRetry  bool
	}{
		{
			name:           "client context canceled",
			requestContext: canceledContext,
			err:            context.Canceled,
			wantSkipRetry:  true,
		},
		{
			name:           "relay timeout with live client context",
			requestContext: context.Background(),
			err:            context.DeadlineExceeded,
			wantSkipRetry:  false,
		},
		{
			name:           "upstream error with live client context",
			requestContext: context.Background(),
			err:            errors.New("upstream failed"),
			wantSkipRetry:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := newAwsInvokeError(test.requestContext, test.err, "InvokeModel")
			assert.Equal(t, test.wantSkipRetry, types.IsSkipRetryError(err))
		})
	}
}

func TestAwsHandlersCancelSdkRequestAndSkipRetry(t *testing.T) {
	originalRelayTimeout := common.RelayTimeout
	common.RelayTimeout = 0
	t.Cleanup(func() {
		common.RelayTimeout = originalRelayTimeout
	})

	tests := []struct {
		name    string
		request any
		handle  func(*gin.Context, *relaycommon.RelayInfo, *Adaptor) (*types.NewAPIError, *dto.Usage)
	}{
		{name: "non-stream", request: newAwsInvokeModelInput(), handle: awsHandler},
		{name: "stream", request: newAwsStreamInput(), handle: awsStreamHandler},
		{name: "nova", request: newAwsInvokeModelInput(), handle: handleNovaRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestContext, cancelRequest := context.WithCancel(context.Background())
			t.Cleanup(cancelRequest)

			upstreamContexts := make(chan context.Context, 1)
			client := newAwsTestClient(awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
				upstreamContexts <- request.Context()
				<-request.Context().Done()
				return nil, request.Context().Err()
			}))
			adaptor := &Adaptor{AwsClient: client, AwsReq: test.request}
			c := newAwsTestContext(httptest.NewRecorder(), requestContext)
			info := newAwsTestRelayInfo()

			type handlerResult struct {
				err   *types.NewAPIError
				usage *dto.Usage
			}
			results := make(chan handlerResult, 1)
			go func() {
				err, usage := test.handle(c, info, adaptor)
				results <- handlerResult{err: err, usage: usage}
			}()

			var upstreamContext context.Context
			select {
			case upstreamContext = <-upstreamContexts:
			case result := <-results:
				t.Fatalf("handler returned before issuing AWS request: %v", result.err)
			case <-time.After(5 * time.Second):
				t.Fatal("AWS request did not start")
			}

			cancelRequest()

			var result handlerResult
			select {
			case result = <-results:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not stop after client cancellation")
			}

			require.ErrorIs(t, upstreamContext.Err(), context.Canceled)
			require.NotNil(t, result.err)
			assert.True(t, types.IsSkipRetryError(result.err))
			assert.Nil(t, result.usage)
		})
	}
}

func TestAwsStreamHandlerUsesFinalUpstreamUsage(t *testing.T) {
	originalRelayTimeout := common.RelayTimeout
	common.RelayTimeout = 0
	t.Cleanup(func() {
		common.RelayTimeout = originalRelayTimeout
	})

	events := []string{
		`{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":100,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":423}}`,
		`{"type":"message_stop"}`,
	}
	client := newAwsTestClient(awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		var body bytes.Buffer
		for _, event := range events {
			if err := writeAwsStreamEvent(&body, event); err != nil {
				return nil, err
			}
		}
		return newAwsStreamResponse(request, io.NopCloser(bytes.NewReader(body.Bytes()))), nil
	}))
	adaptor := &Adaptor{AwsClient: client, AwsReq: newAwsStreamInput()}
	recorder := httptest.NewRecorder()
	c := newAwsTestContext(recorder, context.Background())

	handlerErr, usage := awsStreamHandler(c, newAwsTestRelayInfo(), adaptor)

	require.Nil(t, handlerErr)
	require.NotNil(t, usage)
	require.NotNil(t, usage.BillingUsage)
	require.NotNil(t, usage.BillingUsage.ClaudeUsage)
	assert.Equal(t, 100, usage.BillingUsage.ClaudeUsage.InputTokens)
	assert.Equal(t, 423, usage.BillingUsage.ClaudeUsage.OutputTokens)
	assert.Contains(t, recorder.Body.String(), "[DONE]")
}

func TestAwsStreamHandlerStopsAtClientCancellationAndKeepsPartialBillingUsage(t *testing.T) {
	originalRelayTimeout := common.RelayTimeout
	common.RelayTimeout = 0
	t.Cleanup(func() {
		common.RelayTimeout = originalRelayTimeout
	})

	requestContext, cancelRequest := context.WithCancel(context.Background())
	t.Cleanup(cancelRequest)
	releaseFinal := make(chan struct{})
	var releaseFinalOnce sync.Once
	release := func() {
		releaseFinalOnce.Do(func() {
			close(releaseFinal)
		})
	}
	t.Cleanup(release)

	producerResults := make(chan error, 1)
	upstreamContexts := make(chan context.Context, 1)
	client := newAwsTestClient(awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		upstreamContexts <- request.Context()
		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()
			initialEvents := []string{
				`{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":100,"output_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
			}
			for _, event := range initialEvents {
				if err := writeAwsStreamEvent(writer, event); err != nil {
					producerResults <- err
					return
				}
			}

			<-releaseFinal
			producerResults <- writeAwsStreamEvent(writer, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":423}}`)
		}()
		return newAwsStreamResponse(request, reader), nil
	}))

	responseWriter := newAwsNotifyingResponseWriter("partial")
	c := newAwsTestContext(responseWriter, requestContext)
	adaptor := &Adaptor{AwsClient: client, AwsReq: newAwsStreamInput()}

	type handlerResult struct {
		err   *types.NewAPIError
		usage *dto.Usage
	}
	results := make(chan handlerResult, 1)
	go func() {
		err, usage := awsStreamHandler(c, newAwsTestRelayInfo(), adaptor)
		results <- handlerResult{err: err, usage: usage}
	}()

	var upstreamContext context.Context
	select {
	case upstreamContext = <-upstreamContexts:
	case <-time.After(5 * time.Second):
		t.Fatal("AWS stream request did not start")
	}

	var bodyLengthBeforeCancel int
	select {
	case bodyLengthBeforeCancel = <-responseWriter.notified:
	case <-time.After(5 * time.Second):
		t.Fatal("partial response was not written")
	}
	cancelRequest()

	var result handlerResult
	select {
	case result = <-results:
	case <-time.After(5 * time.Second):
		t.Fatal("stream handler did not stop after client cancellation")
	}

	require.ErrorIs(t, upstreamContext.Err(), context.Canceled)
	require.Nil(t, result.err)
	require.NotNil(t, result.usage)
	require.NotNil(t, result.usage.BillingUsage)
	require.NotNil(t, result.usage.BillingUsage.ClaudeUsage)
	assert.Equal(t, dto.BillingUsageSourceClaudeMessages, result.usage.BillingUsage.Source)
	assert.Equal(t, dto.BillingUsageSemanticAnthropic, result.usage.BillingUsage.Semantic)
	assert.Equal(t, 100, result.usage.BillingUsage.ClaudeUsage.InputTokens)
	assert.Equal(t, 1, result.usage.BillingUsage.ClaudeUsage.OutputTokens)
	assert.Equal(t, bodyLengthBeforeCancel, responseWriter.Body.Len())
	assert.NotContains(t, responseWriter.Body.String(), "[DONE]")

	release()
	select {
	case producerErr := <-producerResults:
		require.Error(t, producerErr)
	case <-time.After(5 * time.Second):
		t.Fatal("upstream producer did not observe the closed stream")
	}
}
