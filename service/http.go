package service

import (
	"bufio"
	"bytes"
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/output"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// protocolErrorEnvelopeProbeLimit bounds how much of a successful response body
// is buffered while probing for an HTTP-200 error envelope. Real endpoint or
// protocol error bodies are tiny; larger bodies are genuine completions and are
// passed through without full buffering.
const protocolErrorEnvelopeProbeLimit = 32 << 10

type replayReadCloser struct {
	io.Reader
	io.Closer
}

// ResponseIsEventStream identifies SSE responses by both Content-Type and the
// first meaningful body token. Some compatible gateways incorrectly label SSE
// as application/json; the body is always restored before this function
// returns, and a detected stream receives the canonical Content-Type.
func ResponseIsEventStream(resp *http.Response) bool {
	if resp == nil || resp.Body == nil {
		return false
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return true
	}
	return ResponseBodyIsEventStream(resp)
}

// ResponseBodyIsEventStream identifies an SSE response by its first meaningful
// body token and ignores Content-Type. Compatible gateways also mislabel a
// buffered JSON completion as text/event-stream; callers deciding whether an
// upstream body may be aggregated as a stream must use this instead of
// trusting the header. The body is restored before returning, and a detected
// stream receives the canonical Content-Type.
func ResponseBodyIsEventStream(resp *http.Response) bool {
	if resp == nil || resp.Body == nil {
		return false
	}

	originalBody := resp.Body
	reader := bufio.NewReader(originalBody)
	prefix := make([]byte, 0, 16)
	isEventStream := false
	for len(prefix) < 4096 {
		value, err := reader.ReadByte()
		if err != nil {
			break
		}
		prefix = append(prefix, value)
		meaningful := bytes.TrimLeft(prefix, " \t\r\n")
		utf8BOM := []byte{0xef, 0xbb, 0xbf}
		if len(meaningful) < len(utf8BOM) && bytes.HasPrefix(utf8BOM, meaningful) {
			continue
		}
		meaningful = bytes.TrimPrefix(meaningful, utf8BOM)
		meaningful = bytes.TrimLeft(meaningful, " \t\r\n")
		if len(meaningful) == 0 {
			continue
		}
		if meaningful[0] == ':' {
			isEventStream = true
			break
		}

		couldBeEventStream := false
		for _, field := range [][]byte{[]byte("data:"), []byte("event:"), []byte("id:"), []byte("retry:")} {
			if bytes.HasPrefix(meaningful, field) {
				isEventStream = true
				break
			}
			if bytes.HasPrefix(field, meaningful) {
				couldBeEventStream = true
			}
		}
		if isEventStream || !couldBeEventStream {
			break
		}
	}

	resp.Body = &replayReadCloser{
		Reader: io.MultiReader(bytes.NewReader(prefix), reader),
		Closer: originalBody,
	}
	if isEventStream {
		if resp.Header == nil {
			resp.Header = make(http.Header)
		}
		resp.Header.Set("Content-Type", "text/event-stream")
	}
	return isEventStream
}

// ResponseIsJSON identifies a buffered JSON response by media type or its
// first meaningful body byte. The body is restored before returning.
func ResponseIsJSON(resp *http.Response) bool {
	if resp == nil || resp.Body == nil {
		return false
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json") {
		return true
	}

	originalBody := resp.Body
	reader := bufio.NewReader(originalBody)
	prefix := make([]byte, 0, 16)
	isJSON := false
	for len(prefix) < 4096 {
		value, err := reader.ReadByte()
		if err != nil {
			break
		}
		prefix = append(prefix, value)
		meaningful := bytes.TrimLeft(prefix, " \t\r\n")
		utf8BOM := []byte{0xef, 0xbb, 0xbf}
		if len(meaningful) < len(utf8BOM) && bytes.HasPrefix(utf8BOM, meaningful) {
			continue
		}
		meaningful = bytes.TrimPrefix(meaningful, utf8BOM)
		meaningful = bytes.TrimLeft(meaningful, " \t\r\n")
		if len(meaningful) == 0 {
			continue
		}
		isJSON = meaningful[0] == '{' || meaningful[0] == '['
		break
	}

	resp.Body = &replayReadCloser{
		Reader: io.MultiReader(bytes.NewReader(prefix), reader),
		Closer: originalBody,
	}
	return isJSON
}

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

// DetectProtocolUnsupportedSuccessEnvelope handles compatible gateways that
// return an endpoint/protocol error as a JSON body with HTTP 200. It preserves
// the response body for the normal handler when no such envelope is present.
func DetectProtocolUnsupportedSuccessEnvelope(resp *http.Response) *hosttypes.NewAPIError {
	if resp == nil || resp.Body == nil || resp.StatusCode != http.StatusOK || ResponseIsEventStream(resp) {
		return nil
	}

	originalBody := resp.Body
	prefix, err := io.ReadAll(io.LimitReader(originalBody, protocolErrorEnvelopeProbeLimit+1))
	resp.Body = &replayReadCloser{
		Reader: io.MultiReader(bytes.NewReader(prefix), originalBody),
		Closer: originalBody,
	}
	if err != nil || len(prefix) > protocolErrorEnvelopeProbeLimit {
		return nil
	}

	var envelope dto.GeneralErrorResponse
	if err := common.Unmarshal(prefix, &envelope); err != nil {
		return nil
	}
	message := strings.TrimSpace(envelope.ToMessage())
	if message == "" {
		return nil
	}
	// Classify only the extracted error message plus structured error code/type.
	// Never match the raw body: a successful completion whose text merely
	// mentions e.g. "route not found" must not be classified as a protocol error.
	classification := message
	if openaiError := envelope.TryToOpenAIError(); openaiError != nil {
		classification = strings.Join([]string{
			message,
			strings.TrimSpace(openaiError.Type),
			strings.TrimSpace(fmt.Sprintf("%v", openaiError.Code)),
		}, "\n")
	}
	if !hosttypes.IsProtocolUnsupportedMessage(classification) {
		return nil
	}

	apiError := hosttypes.NewErrorWithStatusCode(
		fmt.Errorf("upstream returned an error envelope with HTTP 200: %s", message),
		hosttypes.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)
	apiError.MarkProtocolUnsupported()
	return apiError
}

// MarkProtocolUnsupportedStreamError records endpoint/protocol evidence carried
// by a successful HTTP SSE response. The caller must only pass errors decoded
// from an upstream stream event, not local conversion or validation failures.
func MarkProtocolUnsupportedStreamError(apiError *hosttypes.NewAPIError) {
	if apiError != nil && hosttypes.IsProtocolUnsupportedMessage(apiError.Error()) {
		apiError.MarkProtocolUnsupported()
	}
}

// ShouldCopyUpstreamHeader checks whether a given upstream response header
// should be copied to the client response. It returns false for Content-Length
// (managed separately) and X-Oneapi-Request-Id (to preserve the local instance
// ID). When the upstream header is X-Oneapi-Request-Id, the value is captured
// into the Gin context for later logging.
func ShouldCopyUpstreamHeader(c *gin.Context, k string, v []string) bool {
	if strings.EqualFold(k, "Content-Length") {
		return false
	}
	if strings.EqualFold(k, common.RequestIdKey) {
		if c != nil && len(v) > 0 {
			c.Set(common.UpstreamRequestIdKey, v[0])
		}
		return false
	}
	return true
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) error {
	if c == nil || c.Writer == nil {
		return fmt.Errorf("response writer is unavailable")
	}

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		for k, v := range src.Header {
			if !ShouldCopyUpstreamHeader(c, k, v) {
				continue
			}
			c.Writer.Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	status := http.StatusOK
	if src != nil {
		status = src.StatusCode
	}
	var sink output.Sink = output.JSON{Writer: c.Writer}
	if target, ok := c.Writer.(output.Sink); ok {
		sink = target
	}
	err := sink.WriteMessage(output.Message{Data: data, Status: status})
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to copy response body: %s", err.Error()))
		return err
	}
	c.Writer.Flush()
	return nil
}
