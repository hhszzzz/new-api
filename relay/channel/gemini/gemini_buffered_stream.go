package gemini

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"

	"github.com/gin-gonic/gin"
)

// GeminiBufferedStreamHandler converts an unexpectedly streamed Gemini
// generateContent response to Chat Completions chunks in memory, then reuses
// the existing non-stream response handlers for the client's protocol.
func GeminiBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseID := protocolstate.PublicResponseID(c, helper.GetResponseID(c))
	if responseID == "" {
		responseID = "chatcmpl-gemini-buffered"
	}
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatOpenAI, relayconvert.ResponseStreamOptions{
		ID:      responseID,
		Model:   info.PublicResponseModelName(),
		Created: common.GetTimestamp(),
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	var chatStream strings.Builder
	var usage *dto.Usage
	sawTerminal := false
	appendResults := func(results []relayconvert.ResponseResult) error {
		for _, result := range results {
			chunk, ok := result.Value.(*dto.ChatCompletionsStreamResponse)
			if !ok {
				return fmt.Errorf("expected Chat Completions stream chunk, got %T", result.Value)
			}
			chunk.Model = info.PublicResponseModelName()
			data, err := common.Marshal(chunk)
			if err != nil {
				return err
			}
			chatStream.WriteString("data: ")
			chatStream.Write(data)
			chatStream.WriteString("\n\n")
			if result.Usage != nil {
				usage = result.Usage
			}
		}
		return nil
	}

	err = helper.ScanJSONSSE(helper.BoundedStreamReader(resp.Body), func(data string) (bool, error) {
		if data == "[DONE]" {
			return true, nil
		}

		var envelope dto.GeneralErrorResponse
		if err := common.UnmarshalJsonStr(data, &envelope); err == nil {
			if message := strings.TrimSpace(envelope.ToMessage()); message != "" {
				apiError := types.NewOpenAIError(fmt.Errorf("Gemini stream error: %s", message), types.ErrorCodeBadResponse, http.StatusBadGateway)
				service.MarkProtocolUnsupportedStreamError(apiError)
				return false, apiError
			}
		}

		var geminiResponse dto.GeminiChatResponse
		if err := common.UnmarshalJsonStr(data, &geminiResponse); err != nil {
			return false, err
		}
		if blockReason := geminiPromptBlockReason(&geminiResponse); blockReason != "" {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", blockReason))
			sawTerminal = true
		}
		for _, candidate := range geminiResponse.Candidates {
			if candidate.FinishReason != nil && strings.TrimSpace(*candidate.FinishReason) != "" {
				sawTerminal = true
				break
			}
		}

		results, err := relayconvert.ConvertStreamResponseChunk(c, info, state, &geminiResponse)
		if err != nil {
			return false, err
		}
		return false, appendResults(results)
	})
	if err != nil {
		if apiError, ok := err.(*types.NewAPIError); ok {
			return nil, apiError
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if !sawTerminal {
		return nil, types.NewOpenAIError(
			fmt.Errorf("Gemini stream ended without a terminal finish reason"),
			types.ErrorCodeBadResponse,
			http.StatusInternalServerError,
		)
	}
	if usage != nil {
		state.SetUsage(usage)
	}
	finalResults, err := relayconvert.FinalizeStreamResponse(c, info, state)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	if err := appendResults(finalResults); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	chatStream.WriteString("data: [DONE]\n\n")

	header := resp.Header.Clone()
	header.Set("Content-Type", "text/event-stream")
	header.Del("Content-Length")
	bufferedResponse := &http.Response{
		StatusCode: resp.StatusCode,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(chatStream.String())),
	}
	return openai.OaiChatBufferedStreamHandler(c, info, bufferedResponse)
}
