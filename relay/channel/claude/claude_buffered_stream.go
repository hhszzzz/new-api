package claude

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type bufferedClaudeBlockState struct {
	position    int
	partialJSON strings.Builder
	open        bool
}

// ClaudeBufferedStreamHandler aggregates an unexpectedly streamed Anthropic
// Messages response before reusing the normal non-stream response path. No
// client bytes are written until the upstream stream has completed cleanly.
func ClaudeBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}
	defer service.CloseResponseBodyGracefully(resp)

	message := dto.ClaudeResponse{
		Type: "message",
		Role: "assistant",
	}
	blocks := make(map[int]*bufferedClaudeBlockState)
	sawMessageStart := false
	sawMessageStop := false

	err := helper.ScanJSONSSE(helper.BoundedStreamReader(resp.Body), func(data string) (bool, error) {
		if data == "[DONE]" {
			return true, nil
		}

		var event dto.ClaudeResponse
		if err := common.UnmarshalJsonStr(data, &event); err != nil {
			return false, err
		}
		if claudeError := event.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
			apiError := types.WithClaudeError(*claudeError, http.StatusInternalServerError)
			service.MarkProtocolUnsupportedStreamError(apiError)
			return false, apiError
		}

		switch event.Type {
		case "ping":
			return false, nil
		case "message":
			message = event
			if message.Role == "" {
				message.Role = "assistant"
			}
			sawMessageStart = true
			sawMessageStop = true
			return true, nil
		case "message_start":
			if event.Message == nil {
				return false, fmt.Errorf("Claude message_start is missing message")
			}
			sawMessageStart = true
			message.Id = event.Message.Id
			message.Model = event.Message.Model
			message.Role = event.Message.Role
			if message.Role == "" {
				message.Role = "assistant"
			}
			if event.Message.StopReason != nil {
				message.StopReason = *event.Message.StopReason
			}
			message.Usage = cloneClaudeUsage(event.Message.Usage)
			if content := event.Message.ParseMediaContent(); len(content) > 0 {
				message.Content = append(message.Content, content...)
			}
		case "content_block_start":
			if !sawMessageStart {
				return false, fmt.Errorf("Claude content_block_start arrived before message_start")
			}
			if event.ContentBlock == nil {
				return false, fmt.Errorf("Claude content_block_start is missing content_block")
			}
			index := event.GetIndex()
			if state := blocks[index]; state != nil && state.open {
				return false, fmt.Errorf("Claude content block %d started more than once", index)
			}
			message.Content = append(message.Content, *event.ContentBlock)
			blocks[index] = &bufferedClaudeBlockState{
				position: len(message.Content) - 1,
				open:     true,
			}
		case "content_block_delta":
			index := event.GetIndex()
			state := blocks[index]
			if state == nil || !state.open {
				return false, fmt.Errorf("Claude content block %d received a delta before start", index)
			}
			if event.Delta == nil {
				return false, fmt.Errorf("Claude content block %d delta is missing delta data", index)
			}
			block := &message.Content[state.position]
			switch event.Delta.Type {
			case "text_delta":
				appendClaudeText(&block.Text, event.Delta.Text)
			case "thinking_delta":
				appendClaudeText(&block.Thinking, event.Delta.Thinking)
			case "signature_delta":
				block.Signature += event.Delta.Signature
			case "input_json_delta":
				if event.Delta.PartialJson != nil {
					state.partialJSON.WriteString(*event.Delta.PartialJson)
				}
			}
		case "content_block_stop":
			index := event.GetIndex()
			state := blocks[index]
			if state == nil || !state.open {
				return false, fmt.Errorf("Claude content block %d stopped before start", index)
			}
			if state.partialJSON.Len() > 0 {
				var input any
				if err := common.Unmarshal([]byte(state.partialJSON.String()), &input); err != nil {
					return false, fmt.Errorf("Claude tool input for content block %d is invalid: %w", index, err)
				}
				message.Content[state.position].Input = input
			}
			state.open = false
		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != nil {
				message.StopReason = *event.Delta.StopReason
			}
			mergeClaudeUsage(&message.Usage, event.Usage)
		case "message_stop":
			sawMessageStop = true
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		if apiError, ok := err.(*types.NewAPIError); ok {
			return nil, apiError
		}
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if !sawMessageStart {
		return nil, types.NewError(fmt.Errorf("Claude Messages stream ended without message_start"), types.ErrorCodeBadResponse)
	}
	if !sawMessageStop {
		return nil, types.NewError(fmt.Errorf("Claude Messages stream ended without message_stop"), types.ErrorCodeBadResponse)
	}
	if strings.TrimSpace(message.StopReason) == "" {
		return nil, types.NewError(fmt.Errorf("Claude Messages stream ended without a terminal stop_reason"), types.ErrorCodeBadResponse)
	}
	for index, state := range blocks {
		if state.open {
			return nil, types.NewError(fmt.Errorf("Claude Messages stream ended before content block %d stopped", index), types.ErrorCodeBadResponse)
		}
	}

	responseData, err := common.Marshal(message)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeJsonMarshalFailed)
	}
	header := resp.Header.Clone()
	header.Set("Content-Type", "application/json")
	header.Del("Content-Length")
	bufferedResponse := &http.Response{
		StatusCode: resp.StatusCode,
		Header:     header,
	}
	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Model:        info.PublicResponseModelName(),
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	if handleErr := HandleClaudeResponseData(c, info, claudeInfo, bufferedResponse, responseData); handleErr != nil {
		return nil, handleErr
	}
	return claudeInfo.Usage, nil
}

func appendClaudeText(target **string, delta *string) {
	if delta == nil || *delta == "" {
		return
	}
	if *target == nil {
		value := *delta
		*target = &value
		return
	}
	value := **target + *delta
	*target = &value
}

func cloneClaudeUsage(usage *dto.ClaudeUsage) *dto.ClaudeUsage {
	if usage == nil {
		return nil
	}
	clone := *usage
	if usage.CacheCreation != nil {
		cacheCreation := *usage.CacheCreation
		clone.CacheCreation = &cacheCreation
	}
	if usage.ServerToolUse != nil {
		serverToolUse := *usage.ServerToolUse
		clone.ServerToolUse = &serverToolUse
	}
	clone.BillingUsage = dto.CloneBillingUsage(usage.BillingUsage)
	return &clone
}

func mergeClaudeUsage(target **dto.ClaudeUsage, next *dto.ClaudeUsage) {
	if next == nil {
		return
	}
	if *target == nil {
		*target = cloneClaudeUsage(next)
		return
	}
	current := *target
	if next.InputTokens > 0 {
		current.InputTokens = next.InputTokens
	}
	if next.CacheCreationInputTokens > 0 {
		current.CacheCreationInputTokens = next.CacheCreationInputTokens
	}
	if next.CacheReadInputTokens > 0 {
		current.CacheReadInputTokens = next.CacheReadInputTokens
	}
	if next.OutputTokens > 0 {
		current.OutputTokens = next.OutputTokens
	}
	if next.CacheCreation != nil {
		cacheCreation := *next.CacheCreation
		current.CacheCreation = &cacheCreation
	}
	if next.ClaudeCacheCreation5mTokens > 0 {
		current.ClaudeCacheCreation5mTokens = next.ClaudeCacheCreation5mTokens
	}
	if next.ClaudeCacheCreation1hTokens > 0 {
		current.ClaudeCacheCreation1hTokens = next.ClaudeCacheCreation1hTokens
	}
	if next.ServerToolUse != nil {
		serverToolUse := *next.ServerToolUse
		current.ServerToolUse = &serverToolUse
	}
	if next.BillingUsage != nil {
		current.BillingUsage = dto.CloneBillingUsage(next.BillingUsage)
	}
}
