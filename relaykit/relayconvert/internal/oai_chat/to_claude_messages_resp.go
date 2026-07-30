package oaichat

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/reasonmap"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	sharedchat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/chat"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/samber/lo"
)

func generateStopBlock(index int) *dto.ClaudeResponse {
	return &dto.ClaudeResponse{
		Type:  "content_block_stop",
		Index: kitutil.GetPointer[int](index),
	}
}

func stopOpenBlocks(state *convmeta.ClaudeConvertInfo) []*dto.ClaudeResponse {
	if state == nil {
		return nil
	}
	switch state.LastMessagesType {
	case convmeta.LastMessageTypeText, convmeta.LastMessageTypeThinking:
		return []*dto.ClaudeResponse{generateStopBlock(state.Index)}
	case convmeta.LastMessageTypeTools:
		blockIndexes := make([]int, 0, len(state.ToolCalls))
		for _, toolCall := range state.ToolCalls {
			if toolCall != nil && toolCall.Started {
				blockIndexes = append(blockIndexes, toolCall.BlockIndex)
			}
		}
		sort.Ints(blockIndexes)
		responses := make([]*dto.ClaudeResponse, 0, len(blockIndexes))
		for _, blockIndex := range blockIndexes {
			responses = append(responses, generateStopBlock(blockIndex))
		}
		return responses
	default:
		return nil
	}
}

func buildClaudeUsageFromOpenAIUsage(oaiUsage *dto.Usage) *dto.ClaudeUsage {
	if oaiUsage == nil {
		return nil
	}
	if billingUsage := dto.CloneBillingUsage(oaiUsage.BillingUsage); billingUsage != nil && billingUsage.ClaudeUsage != nil {
		if billingUsage.Source == dto.BillingUsageSourceClaudeMessages || billingUsage.Semantic == dto.BillingUsageSemanticAnthropic {
			return billingUsage.ClaudeUsage
		}
	}
	billingUsage := dto.NewOpenAIChatBillingUsage(oaiUsage)
	if existingBillingUsage := dto.CloneBillingUsage(oaiUsage.BillingUsage); existingBillingUsage != nil && existingBillingUsage.OpenAIUsage != nil {
		if existingBillingUsage.Source == dto.BillingUsageSourceOAIChat ||
			existingBillingUsage.Source == dto.BillingUsageSourceOAIResponses ||
			existingBillingUsage.Semantic == dto.BillingUsageSemanticOpenAI {
			billingUsage = existingBillingUsage
		}
	}
	cacheCreation5m, cacheCreation1h := NormalizeCacheCreationSplit(
		oaiUsage.PromptTokensDetails.CachedCreationTokens,
		oaiUsage.ClaudeCacheCreation5mTokens,
		oaiUsage.ClaudeCacheCreation1hTokens,
	)
	cacheCreationTokens := oaiUsage.PromptTokensDetails.CacheCreationTokensTotal()
	cacheReadTokens := lo.Max([]int{oaiUsage.PromptTokensDetails.CachedTokens, 0})
	// OpenAI prompt/input tokens include cache reads and cache writes, while
	// Claude reports fresh input_tokens separately from both cache counters.
	// Cached prefixes can overlap on compatible upstreams, so clamp a negative
	// remainder instead of emitting an impossible negative fresh-token count.
	inputTokens := oaiUsage.PromptTokens - cacheReadTokens - cacheCreationTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	usage := &dto.ClaudeUsage{
		InputTokens:              inputTokens,
		OutputTokens:             oaiUsage.CompletionTokens,
		CacheCreationInputTokens: cacheCreationTokens,
		CacheReadInputTokens:     cacheReadTokens,
		BillingUsage:             billingUsage,
	}
	if cacheCreation5m > 0 || cacheCreation1h > 0 {
		usage.CacheCreation = &dto.ClaudeCacheCreationUsage{
			Ephemeral5mInputTokens: cacheCreation5m,
			Ephemeral1hInputTokens: cacheCreation1h,
		}
	}
	return usage
}

func ClaudeUsageFromOpenAIUsage(oaiUsage *dto.Usage) *dto.ClaudeUsage {
	return buildClaudeUsageFromOpenAIUsage(oaiUsage)
}

func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	remainder := lo.Max([]int{totalTokens - tokens5m - tokens1h, 0})
	return tokens5m + remainder, tokens1h
}

func StreamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info convmeta.Meta) []*dto.ClaudeResponse {
	if info == nil {
		info = &convmeta.Values{}
	}
	state := info.EnsureClaudeConvertInfo()
	if state.Done {
		return nil
	}
	for choiceIndex := range openAIResponse.Choices {
		choice := &openAIResponse.Choices[choiceIndex]
		finished := choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != ""
		sharedchat.SplitThinkTagStreamDelta(&state.ThinkTagSplitter, &choice.Delta, finished)
	}

	var claudeResponses []*dto.ClaudeResponse
	// stopOpenBlocks emits the required content_block_stop event(s) for the currently open block(s)
	// according to Anthropic's SSE streaming state machine:
	// content_block_start -> content_block_delta* -> content_block_stop (per index).
	//
	// For text/thinking, there is at most one open block at state.Index.
	// For tools, OpenAI tool_calls can stream multiple parallel tool_use blocks (indexed from 0),
	// so we may have multiple open blocks and must stop each one explicitly.
	appendStopOpenBlocks := func() {
		if state.LastMessagesType == convmeta.LastMessageTypeTools {
			claudeResponses = append(claudeResponses, flushClaudeToolCalls(state, true)...)
		}
		claudeResponses = append(claudeResponses, stopOpenBlocks(state)...)
	}
	// stopOpenBlocksAndAdvance closes the currently open block(s) and advances the content block index
	// to the next available slot for subsequent content_block_start events.
	//
	// This prevents invalid streams where a content_block_delta (e.g. thinking_delta) is emitted for an
	// index whose active content_block type is different (the typical cause of "Mismatched content block type").
	stopOpenBlocksAndAdvance := func() {
		if state.LastMessagesType == convmeta.LastMessageTypeNone {
			return
		}
		appendStopOpenBlocks()
		switch state.LastMessagesType {
		case convmeta.LastMessageTypeTools:
			state.Index = state.ToolCallBaseIndex + state.ToolCallStartedCount
			state.ToolCallBaseIndex = 0
			state.ToolCallMaxIndexOffset = 0
			state.ToolCallNextIndex = 0
			state.ToolCallStartedCount = 0
			state.ToolCalls = nil
		default:
			state.Index++
		}
		state.LastMessagesType = convmeta.LastMessageTypeNone
	}
	firstResponse := info.GetSendResponseCount() == 1
	if firstResponse {
		msg := &dto.ClaudeMediaMessage{
			Id:    openAIResponse.Id,
			Model: openAIResponse.Model,
			Type:  "message",
			Role:  "assistant",
			Usage: &dto.ClaudeUsage{
				InputTokens:  info.GetEstimatePromptTokens(),
				OutputTokens: 0,
			},
		}
		msg.SetContent(make([]any, 0))
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Type:    "message_start",
			Message: msg,
		})
	}
	if strings.TrimSpace(openAIResponse.ReasoningEncryptedContent) != "" && state.LastMessagesType == convmeta.LastMessageTypeThinking {
		// ReasoningEncryptedContent is internal provider state, never an
		// Anthropic signature. Treat it only as a boundary marker and close the
		// visible thinking block without emitting signature_delta.
		stopOpenBlocksAndAdvance()
	}
	if openAIResponse.ProviderReasoningItem != nil && state.LastMessagesType == convmeta.LastMessageTypeThinking {
		// A completed Responses reasoning item is likewise only a boundary. Its
		// encrypted_content is retained by the host session store, not exposed to
		// the Messages client.
		stopOpenBlocksAndAdvance()
	}
	if firstResponse {
		if openAIResponse.IsToolCall() {
			if state.LastMessagesType != convmeta.LastMessageTypeTools {
				stopOpenBlocksAndAdvance()
				state.ToolCallBaseIndex = state.Index
				state.ToolCallMaxIndexOffset = 0
				state.ToolCallNextIndex = 0
				state.ToolCallStartedCount = 0
				state.ToolCalls = make(map[int]*convmeta.ClaudeToolCallStreamState)
			}
			state.LastMessagesType = convmeta.LastMessageTypeTools
			if len(openAIResponse.Choices) > 0 {
				claudeResponses = append(claudeResponses, appendClaudeToolCallDeltas(state, openAIResponse.Choices[0].Delta.ParseToolCalls())...)
			}
		} else {

		}
		// 判断首个响应是否存在内容（非标准的 OpenAI 响应）
		if len(openAIResponse.Choices) > 0 {
			reasoning := openAIResponse.Choices[0].Delta.GetReasoningContent()
			content := openAIResponse.Choices[0].Delta.GetContentString()
			if content == "" {
				content = openAIResponse.Choices[0].Delta.GetRefusalContent()
			}

			if reasoning != "" {
				if state.LastMessagesType != convmeta.LastMessageTypeThinking {
					stopOpenBlocksAndAdvance()
					idx := state.Index
					claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
						Index: &idx,
						Type:  "content_block_start",
						ContentBlock: &dto.ClaudeMediaMessage{
							Type:     "thinking",
							Thinking: kitutil.GetPointer[string](""),
						},
					})
				}
				idx := state.Index
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx,
					Type:  "content_block_delta",
					Delta: &dto.ClaudeMediaMessage{
						Type:     "thinking_delta",
						Thinking: &reasoning,
					},
				})
				state.LastMessagesType = convmeta.LastMessageTypeThinking
			} else if content != "" {
				if state.LastMessagesType != convmeta.LastMessageTypeText {
					stopOpenBlocksAndAdvance()
				}
				idx := state.Index
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx,
					Type:  "content_block_start",
					ContentBlock: &dto.ClaudeMediaMessage{
						Type: "text",
						Text: kitutil.GetPointer[string](""),
					},
				})
				idx2 := idx
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx2,
					Type:  "content_block_delta",
					Delta: &dto.ClaudeMediaMessage{
						Type: "text_delta",
						Text: kitutil.GetPointer[string](content),
					},
				})
				state.LastMessagesType = convmeta.LastMessageTypeText
			}
		}

		// A first chunk can carry finish_reason before usage; defer terminal events until usage arrives.
		if len(openAIResponse.Choices) > 0 && openAIResponse.Choices[0].FinishReason != nil && *openAIResponse.Choices[0].FinishReason != "" {
			state.FinishReason = *openAIResponse.Choices[0].FinishReason
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = state.Usage
			}
			if oaiUsage == nil {
				return claudeResponses
			}
			appendStopOpenBlocks()
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type:  "message_delta",
				Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
				Delta: &dto.ClaudeMediaMessage{
					StopReason: kitutil.GetPointer[string](terminalClaudeStopReason(state)),
				},
			})
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			state.Done = true
		}
		return claudeResponses
	}

	if len(openAIResponse.Choices) == 0 {
		// Some OpenAI-compatible upstreams end with a usage-only SSE chunk.
		oaiUsage := openAIResponse.Usage
		if oaiUsage == nil {
			oaiUsage = state.Usage
		}
		if oaiUsage != nil {
			appendStopOpenBlocks()
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type:  "message_delta",
				Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
				Delta: &dto.ClaudeMediaMessage{
					StopReason: kitutil.GetPointer[string](terminalClaudeStopReason(state)),
				},
			})
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			state.Done = true
		}
		return claudeResponses
	} else {
		chosenChoice := openAIResponse.Choices[0]
		doneChunk := chosenChoice.FinishReason != nil && *chosenChoice.FinishReason != ""
		if doneChunk {
			state.FinishReason = *chosenChoice.FinishReason
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = state.Usage
				// Some upstreams emit finish_reason first, then send a final usage-only chunk.
				// Defer closing until usage is available so the final message_delta carries it.
				if oaiUsage == nil {
					return claudeResponses
				}
			}
		}

		var claudeResponse dto.ClaudeResponse
		var isEmpty bool
		claudeResponse.Type = "content_block_delta"
		toolCalls := chosenChoice.Delta.ParseToolCalls()
		if len(toolCalls) > 0 {
			if state.LastMessagesType != convmeta.LastMessageTypeTools {
				stopOpenBlocksAndAdvance()
				state.ToolCallBaseIndex = state.Index
				state.ToolCallMaxIndexOffset = 0
				state.ToolCallNextIndex = 0
				state.ToolCallStartedCount = 0
				state.ToolCalls = make(map[int]*convmeta.ClaudeToolCallStreamState)
			}
			state.LastMessagesType = convmeta.LastMessageTypeTools
			claudeResponses = append(claudeResponses, appendClaudeToolCallDeltas(state, toolCalls)...)
		} else {
			reasoning := chosenChoice.Delta.GetReasoningContent()
			textContent := chosenChoice.Delta.GetContentString()
			if textContent == "" {
				textContent = chosenChoice.Delta.GetRefusalContent()
			}
			if reasoning != "" || textContent != "" {
				if reasoning != "" {
					if state.LastMessagesType != convmeta.LastMessageTypeThinking {
						stopOpenBlocksAndAdvance()
						idx := state.Index
						claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
							Index: &idx,
							Type:  "content_block_start",
							ContentBlock: &dto.ClaudeMediaMessage{
								Type:     "thinking",
								Thinking: kitutil.GetPointer[string](""),
							},
						})
					}
					state.LastMessagesType = convmeta.LastMessageTypeThinking
					claudeResponse.Delta = &dto.ClaudeMediaMessage{
						Type:     "thinking_delta",
						Thinking: &reasoning,
					}
				} else {
					if state.LastMessagesType != convmeta.LastMessageTypeText {
						stopOpenBlocksAndAdvance()
						idx := state.Index
						claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
							Index: &idx,
							Type:  "content_block_start",
							ContentBlock: &dto.ClaudeMediaMessage{
								Type: "text",
								Text: kitutil.GetPointer[string](""),
							},
						})
					}
					state.LastMessagesType = convmeta.LastMessageTypeText
					claudeResponse.Delta = &dto.ClaudeMediaMessage{
						Type: "text_delta",
						Text: kitutil.GetPointer[string](textContent),
					}
				}
			} else {
				isEmpty = true
			}
		}

		claudeResponse.Index = kitutil.GetPointer[int](state.Index)
		if !isEmpty && claudeResponse.Delta != nil {
			claudeResponses = append(claudeResponses, &claudeResponse)
		}

		if doneChunk || state.Done {
			appendStopOpenBlocks()
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = state.Usage
			}
			if oaiUsage != nil {
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Type:  "message_delta",
					Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
					Delta: &dto.ClaudeMediaMessage{
						StopReason: kitutil.GetPointer[string](terminalClaudeStopReason(state)),
					},
				})
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			state.Done = true
			return claudeResponses
		}
	}

	return claudeResponses
}

func appendClaudeToolCallDeltas(state *convmeta.ClaudeConvertInfo, toolCalls []dto.ToolCallResponse) []*dto.ClaudeResponse {
	if state == nil || len(toolCalls) == 0 {
		return nil
	}
	if state.ToolCalls == nil {
		state.ToolCalls = make(map[int]*convmeta.ClaudeToolCallStreamState)
	}
	if state.UsedToolCallIDs == nil {
		state.UsedToolCallIDs = make(map[string]struct{})
	}

	responses := make([]*dto.ClaudeResponse, 0, len(toolCalls)*2)
	for index, toolCall := range toolCalls {
		offset := index
		if toolCall.Index != nil && *toolCall.Index >= 0 {
			offset = *toolCall.Index
		}
		if offset > state.ToolCallMaxIndexOffset {
			state.ToolCallMaxIndexOffset = offset
		}
		toolState := state.ToolCalls[offset]
		if toolState == nil {
			toolState = &convmeta.ClaudeToolCallStreamState{}
			state.ToolCalls[offset] = toolState
		}
		if !toolState.Started {
			if id := strings.TrimSpace(toolCall.ID); id != "" {
				toolState.ID = id
			}
			if name := strings.TrimSpace(toolCall.Function.Name); name != "" {
				toolState.Name = name
			}
		}
		if toolCall.Function.Arguments != "" {
			if toolState.Started {
				arguments := toolCall.Function.Arguments
				blockIndex := toolState.BlockIndex
				responses = append(responses, &dto.ClaudeResponse{
					Index: &blockIndex,
					Type:  "content_block_delta",
					Delta: &dto.ClaudeMediaMessage{
						Type:        "input_json_delta",
						PartialJson: &arguments,
					},
				})
			} else {
				toolState.PendingArguments += toolCall.Function.Arguments
			}
		}
	}
	responses = append(responses, flushClaudeToolCalls(state, false)...)
	if state.ToolCallStartedCount > 0 {
		state.Index = state.ToolCallBaseIndex + state.ToolCallStartedCount - 1
	}
	return responses
}

func flushClaudeToolCalls(state *convmeta.ClaudeConvertInfo, final bool) []*dto.ClaudeResponse {
	if state == nil || len(state.ToolCalls) == 0 {
		return nil
	}
	responses := make([]*dto.ClaudeResponse, 0)
	if !final {
		for {
			toolState := state.ToolCalls[state.ToolCallNextIndex]
			if toolState == nil || strings.TrimSpace(toolState.Name) == "" {
				break
			}
			if !toolState.Started {
				responses = append(responses, startClaudeToolCall(state, toolState)...)
			}
			state.ToolCallNextIndex++
		}
		return responses
	}

	offsets := make([]int, 0, len(state.ToolCalls))
	for offset := range state.ToolCalls {
		offsets = append(offsets, offset)
	}
	sort.Ints(offsets)
	for _, offset := range offsets {
		toolState := state.ToolCalls[offset]
		if toolState == nil || toolState.Started || strings.TrimSpace(toolState.Name) == "" {
			continue
		}
		responses = append(responses, startClaudeToolCall(state, toolState)...)
	}
	return responses
}

func startClaudeToolCall(state *convmeta.ClaudeConvertInfo, toolState *convmeta.ClaudeToolCallStreamState) []*dto.ClaudeResponse {
	if state == nil || toolState == nil || toolState.Started || strings.TrimSpace(toolState.Name) == "" {
		return nil
	}
	toolState.ID = claimClaudeToolUseID(state, toolState.ID)
	toolState.BlockIndex = state.ToolCallBaseIndex + state.ToolCallStartedCount
	toolState.Started = true
	state.ToolCallStartedCount++
	blockIndex := toolState.BlockIndex
	responses := []*dto.ClaudeResponse{{
		Index: &blockIndex,
		Type:  "content_block_start",
		ContentBlock: &dto.ClaudeMediaMessage{
			Id:    toolState.ID,
			Type:  "tool_use",
			Name:  toolState.Name,
			Input: map[string]interface{}{},
		},
	}}
	if toolState.PendingArguments != "" {
		arguments := toolState.PendingArguments
		responses = append(responses, &dto.ClaudeResponse{
			Index: &blockIndex,
			Type:  "content_block_delta",
			Delta: &dto.ClaudeMediaMessage{
				Type:        "input_json_delta",
				PartialJson: &arguments,
			},
		})
		toolState.PendingArguments = ""
	}
	return responses
}

func claimClaudeToolUseID(state *convmeta.ClaudeConvertInfo, candidate string) string {
	if state.UsedToolCallIDs == nil {
		state.UsedToolCallIDs = make(map[string]struct{})
	}
	return uniqueClaudeToolUseID(state.UsedToolCallIDs, candidate)
}

func uniqueClaudeToolUseID(used map[string]struct{}, candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate != "" {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate
		}
	}
	for {
		candidate = "toolu_" + kitutil.GetUUID()
		if _, exists := used[candidate]; exists {
			continue
		}
		used[candidate] = struct{}{}
		return candidate
	}
}

func FinalizeStreamResponseOpenAI2Claude(info convmeta.Meta) []*dto.ClaudeResponse {
	if info == nil {
		info = &convmeta.Values{}
	}
	state := info.EnsureClaudeConvertInfo()
	if state.Done {
		return nil
	}

	responses := flushClaudeToolCalls(state, true)
	responses = append(responses, stopOpenBlocks(state)...)
	responses = append(responses,
		&dto.ClaudeResponse{
			Type:  "message_delta",
			Usage: buildClaudeUsageFromOpenAIUsage(state.Usage),
			Delta: &dto.ClaudeMediaMessage{
				StopReason: kitutil.GetPointer[string](terminalClaudeStopReason(state)),
			},
		},
		&dto.ClaudeResponse{Type: "message_stop"},
	)
	state.Done = true
	return responses
}

func ResponseOpenAI2Claude(openAIResponse *dto.OpenAITextResponse, info convmeta.Meta) *dto.ClaudeResponse {
	return ResponseOpenAI2ClaudeWithBridgeState(openAIResponse, info, nil)
}

func ResponseOpenAI2ClaudeWithBridgeState(openAIResponse *dto.OpenAITextResponse, info convmeta.Meta, outputState *sharedbridge.ResponseOutputState) *dto.ClaudeResponse {
	var stopReason string
	contents := make([]dto.ClaudeMediaMessage, 0)
	usedToolCallIDs := make(map[string]struct{})
	claudeResponse := &dto.ClaudeResponse{
		Id:    openAIResponse.Id,
		Type:  "message",
		Role:  "assistant",
		Model: openAIResponse.Model,
	}
	if outputState != nil && len(outputState.Items) > 0 {
		toolCalls := make([]dto.ToolCallRequest, 0)
		for _, choice := range openAIResponse.Choices {
			stopReason = stopReasonOpenAI2Claude(choice.FinishReason)
			toolCalls = append(toolCalls, choice.Message.ParseToolCalls()...)
		}
		for _, item := range outputState.Items {
			switch item.Kind {
			case sharedbridge.ResponseOutputKindReasoning:
				if item.Text != "" {
					contents = append(contents, dto.ClaudeMediaMessage{
						Type:     "thinking",
						Thinking: kitutil.GetPointer(item.Text),
					})
				}
			case sharedbridge.ResponseOutputKindMessage:
				content := dto.ClaudeMediaMessage{Type: "text"}
				content.SetText(item.Text)
				contents = append(contents, content)
			case sharedbridge.ResponseOutputKindTool:
				if item.ToolIndex < 0 || item.ToolIndex >= len(toolCalls) {
					continue
				}
				contents = append(contents, openAIToolCallToClaudeContent(toolCalls[item.ToolIndex], usedToolCallIDs))
			}
		}
		for _, choice := range openAIResponse.Choices {
			if refusal := choice.Message.GetRefusalContent(); refusal != "" {
				content := dto.ClaudeMediaMessage{Type: "text"}
				content.SetText(refusal)
				contents = append(contents, content)
			}
		}
		claudeResponse.Content = contents
		claudeResponse.StopReason = stopReason
		claudeResponse.Usage = buildClaudeUsageFromOpenAIUsage(&openAIResponse.Usage)
		return claudeResponse
	}
	for _, reasoningState := range openAIResponse.ProviderReasoningStates {
		if reasoningState.Text != "" {
			contents = append(contents, dto.ClaudeMediaMessage{
				Type:     "thinking",
				Thinking: kitutil.GetPointer(reasoningState.Text),
			})
		}
	}
	for _, choice := range openAIResponse.Choices {
		stopReason = stopReasonOpenAI2Claude(choice.FinishReason)
		reasoning := choice.Message.GetReasoningContent()
		textContent := choice.Message.StringContent()
		if reasoning == "" {
			if split, answer, ok := sharedchat.SplitThinkTagText(textContent); ok {
				reasoning, textContent = split, answer
			}
		}
		if reasoning != "" && len(openAIResponse.ProviderReasoningStates) == 0 {
			contents = append(contents, dto.ClaudeMediaMessage{
				Type:     "thinking",
				Thinking: kitutil.GetPointer(reasoning),
			})
		}
		refusalContent := choice.Message.GetRefusalContent()
		toolCalls := choice.Message.ParseToolCalls()
		if textContent != "" {
			claudeContent := dto.ClaudeMediaMessage{}
			claudeContent.Type = "text"
			claudeContent.SetText(textContent)
			contents = append(contents, claudeContent)
		}
		if refusalContent != "" {
			claudeContent := dto.ClaudeMediaMessage{Type: "text"}
			claudeContent.SetText(refusalContent)
			contents = append(contents, claudeContent)
		}
		if textContent == "" && refusalContent == "" && len(toolCalls) == 0 {
			claudeContent := dto.ClaudeMediaMessage{Type: "text"}
			claudeContent.SetText("")
			contents = append(contents, claudeContent)
		}
		for _, toolUse := range toolCalls {
			contents = append(contents, openAIToolCallToClaudeContent(toolUse, usedToolCallIDs))
		}
	}
	claudeResponse.Content = contents
	claudeResponse.StopReason = stopReason
	claudeResponse.Usage = buildClaudeUsageFromOpenAIUsage(&openAIResponse.Usage)

	return claudeResponse
}

func openAIToolCallToClaudeContent(toolUse dto.ToolCallRequest, usedToolCallIDs map[string]struct{}) dto.ClaudeMediaMessage {
	content := dto.ClaudeMediaMessage{
		Type:  "tool_use",
		Id:    uniqueClaudeToolUseID(usedToolCallIDs, toolUse.ID),
		Name:  toolUse.Function.Name,
		Input: map[string]interface{}{},
	}
	if strings.TrimSpace(toolUse.Function.Arguments) == "" {
		return content
	}
	var input map[string]interface{}
	if err := kitutil.Unmarshal([]byte(toolUse.Function.Arguments), &input); err == nil && input != nil {
		content.Input = input
	}
	return content
}

func stopReasonOpenAI2Claude(reason string) string {
	return reasonmap.OpenAIFinishReasonToClaudeStopReason(reason)
}

// terminalClaudeStopReason resolves the stop reason for the closing
// message_delta. Claude clients treat stop_reason "tool_use" as a promise that
// tool_use blocks were streamed; a malformed upstream (tool call deltas that
// never carry a function name) would break that promise, so downgrade to
// end_turn whenever no tool_use block was actually emitted.
func terminalClaudeStopReason(state *convmeta.ClaudeConvertInfo) string {
	stopReason := stopReasonOpenAI2Claude(state.FinishReason)
	if stopReason == "" {
		stopReason = "end_turn"
	}
	if stopReason == "tool_use" && state.ToolCallStartedCount == 0 {
		stopReason = "end_turn"
	}
	return stopReason
}
