package aws

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	kittypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
)

type bedrockEventStream interface {
	Events() <-chan types.ResponseStream
	Err() error
	Close() error
}

// sdkStreamBody exposes ordered SDK payloads to the common SSE decoder. It
// does not buffer a whole response, synthesize termination, or start a second
// producer. Closing it cancels the SDK and releases its event reader.
type sdkStreamBody struct {
	ctx       context.Context
	cancel    context.CancelFunc
	stream    bedrockEventStream
	buffer    bytes.Buffer
	closeOnce sync.Once
	closeErr  error
}

func (body *sdkStreamBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for body.buffer.Len() == 0 {
		if err := body.ctx.Err(); err != nil {
			return 0, err
		}
		select {
		case <-body.ctx.Done():
			return 0, body.ctx.Err()
		case event, ok := <-body.stream.Events():
			if !ok {
				if err := body.stream.Err(); err != nil {
					return 0, err
				}
				return 0, io.EOF
			}
			if err := body.ctx.Err(); err != nil {
				return 0, err
			}
			chunk, ok := event.(*types.ResponseStreamMemberChunk)
			if !ok || chunk == nil {
				return 0, fmt.Errorf("unsupported Bedrock event %T", event)
			}
			for line := range bytes.SplitSeq(chunk.Value.Bytes, []byte("\n")) {
				body.buffer.WriteString("data: ")
				body.buffer.Write(line)
				body.buffer.WriteByte('\n')
			}
			body.buffer.WriteByte('\n')
		}
	}
	return body.buffer.Read(p)
}

func (body *sdkStreamBody) Close() error {
	body.closeOnce.Do(func() {
		body.cancel()
		body.closeErr = body.stream.Close()
	})
	return body.closeErr
}

func (a *Adaptor) invoke(c *gin.Context, info *relaycommon.RelayInfo) (*channel.TransportResult, error) {
	requestContext := c.Request.Context()
	ctx, cancel := newAwsInvokeContext(requestContext)
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	if a.StreamInput != nil {
		result, err := a.AwsClient.InvokeModelWithResponseStream(ctx, a.StreamInput)
		if err != nil {
			cancel()
			return nil, newAwsInvokeError(requestContext, err, "InvokeModelWithResponseStream")
		}
		response.Header.Set("Content-Type", "text/event-stream")
		response.Body = &sdkStreamBody{ctx: ctx, cancel: cancel, stream: result.GetStream()}
	} else {
		defer cancel()
		result, err := a.AwsClient.InvokeModel(ctx, a.InvokeInput)
		if err != nil {
			return nil, newAwsInvokeError(requestContext, err, "InvokeModel")
		}
		response.Header.Set("Content-Type", "application/json")
		response.Body = io.NopCloser(bytes.NewReader(result.Body))
		if a.IsNova {
			normalized, err := decodeNovaResponse(c, info, result.Body)
			if err != nil {
				return nil, err
			}
			return &channel.TransportResult{Response: normalized, Transport: relayconvert.TransportSDK, Format: kittypes.RelayFormatOpenAI}, nil
		}
	}
	return &channel.TransportResult{Response: response, Transport: relayconvert.TransportSDK}, nil
}
