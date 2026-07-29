package xai

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func streamResponseXAI2OpenAI(xAIResp *dto.ChatCompletionsStreamResponse, usage *dto.Usage) *dto.ChatCompletionsStreamResponse {
	if xAIResp == nil {
		return nil
	}
	if xAIResp.Usage != nil {
		xAIResp.Usage.CompletionTokens = usage.CompletionTokens
	}
	openAIResp := &dto.ChatCompletionsStreamResponse{
		Id:      xAIResp.Id,
		Object:  xAIResp.Object,
		Created: xAIResp.Created,
		Model:   xAIResp.Model,
		Choices: xAIResp.Choices,
		Usage:   xAIResp.Usage,
	}

	return openAIResp
}

func normalizeXAIUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	// xAI reports completion_tokens without reasoning tokens while total_tokens
	// includes them, so completion is recomputed from the total. Upstreams that
	// omit total_tokens must not have their completion tokens zeroed out.
	if usage.TotalTokens > usage.PromptTokens {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	} else {
		if usage.CompletionTokens < 0 {
			usage.CompletionTokens = 0
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	usage.CompletionTokenDetails.TextTokens = usage.CompletionTokens - usage.CompletionTokenDetails.ReasoningTokens
	if usage.CompletionTokenDetails.TextTokens < 0 {
		usage.CompletionTokenDetails.TextTokens = 0
	}
}

func xAIStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	usage := &dto.Usage{}
	var responseTextBuilder strings.Builder
	var toolCount int
	var containStreamUsage bool

	helper.SetEventStreamHeaders(c)

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var xAIResp *dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &xAIResp); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			sr.Error(err)
			return
		}

		// 把 xAI 的usage转换为 OpenAI 的usage
		if xAIResp.Usage != nil {
			containStreamUsage = true
			usage.PromptTokens = xAIResp.Usage.PromptTokens
			usage.TotalTokens = xAIResp.Usage.TotalTokens
			usage.CompletionTokenDetails = xAIResp.Usage.CompletionTokenDetails
			normalizeXAIUsage(usage)
		}

		openaiResponse := streamResponseXAI2OpenAI(xAIResp, usage)
		_ = openai.ProcessStreamResponse(*openaiResponse, &responseTextBuilder, &toolCount)
		if err := helper.ObjectData(c, openaiResponse); err != nil {
			common.SysLog(err.Error())
			sr.Error(err)
		}
	})

	if !containStreamUsage {
		usage = service.ResponseText2Usage(c, responseTextBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		usage.CompletionTokens += toolCount * 7
	}

	helper.Done(c)
	service.CloseResponseBodyGracefully(resp)
	return usage, nil
}

func xAIHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	var xaiResponse ChatCompletionResponse
	err = common.Unmarshal(responseBody, &xaiResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if xaiResponse.Usage != nil {
		normalizeXAIUsage(xaiResponse.Usage)
	}

	// new body
	encodeJson, err := common.Marshal(xaiResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	service.IOCopyBytesGracefully(c, resp, encodeJson)

	return xaiResponse.Usage, nil
}

func normalizeXAIChatResponse(resp *http.Response, stream bool) error {
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("xAI response body is unavailable")
	}
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	if stream {
		resp.Body = newXAIChatStreamBody(resp.Body)
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := resp.Body.Close(); err != nil {
		return err
	}

	var response ChatCompletionResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return err
	}
	if response.Usage != nil {
		normalizeXAIUsage(response.Usage)
		body, err = common.Marshal(response)
		if err != nil {
			return err
		}
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	return nil
}

type xaiChatStreamBody struct {
	*io.PipeReader
	source io.Closer
}

func (b *xaiChatStreamBody) Close() error {
	readerErr := b.PipeReader.Close()
	sourceErr := b.source.Close()
	if readerErr != nil {
		return readerErr
	}
	return sourceErr
}

func newXAIChatStreamBody(source io.ReadCloser) io.ReadCloser {
	reader, writer := io.Pipe()
	body := &xaiChatStreamBody{PipeReader: reader, source: source}
	go func() {
		defer func() { _ = source.Close() }()
		buffered := bufio.NewWriterSize(writer, 4*1024)
		writePayload := func(payload []byte) error {
			if _, err := buffered.WriteString("data: "); err != nil {
				return err
			}
			if _, err := buffered.Write(payload); err != nil {
				return err
			}
			if _, err := buffered.WriteString("\n\n"); err != nil {
				return err
			}
			return buffered.Flush()
		}

		err := helper.ScanJSONSSE(source, func(data string) (bool, error) {
			if data == "[DONE]" {
				return true, writePayload([]byte(data))
			}

			payload := []byte(data)
			var response dto.ChatCompletionsStreamResponse
			if err := common.Unmarshal(payload, &response); err != nil {
				return false, err
			}
			if response.Usage != nil {
				normalizeXAIUsage(response.Usage)
				encoded, err := common.Marshal(response)
				if err != nil {
					return false, err
				}
				payload = encoded
			}
			return false, writePayload(payload)
		})
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		_ = buffered.Flush()
		_ = writer.Close()
	}()
	return body
}
