package oaichat

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/reasonmap"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	sharedchat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/chat"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
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
	return sharedclaude.UsageFromOpenAI(oaiUsage)
}

func clientVisibleClaudeStreamUsage(state *convmeta.ClaudeConvertInfo, incoming *dto.Usage) *dto.ClaudeUsage {
	prior := buildClaudeUsageFromOpenAIUsage(state.Usage)
	converted := buildClaudeUsageFromOpenAIUsage(incoming)
	if incoming != nil {
		state.Usage = dto.MergeUsageNonZero(state.Usage, incoming)
	}
	if prior == nil {
		return converted
	}
	if converted == nil {
		return prior
	}
	return dto.MergeClaudeUsageNonZero(prior, converted)
}

func ClaudeUsageFromOpenAIUsage(oaiUsage *dto.Usage) *dto.ClaudeUsage {
	return buildClaudeUsageFromOpenAIUsage(oaiUsage)
}

func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	return sharedclaude.NormalizeCacheCreationSplit(totalTokens, tokens5m, tokens1h)
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
			state.ToolCallByIndex = nil
			state.ToolCallByID = nil
		default:
			state.Index++
		}
		state.LastMessagesType = convmeta.LastMessageTypeNone
	}
	appendCitationDeltas := func(raw []byte) {
		citations := chatAnnotationsToClaude(raw, "")
		if len(citations) == 0 {
			return
		}
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
			state.LastMessagesType = convmeta.LastMessageTypeText
		}
		for _, citation := range citations {
			idx := state.Index
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Index: &idx,
				Type:  "content_block_delta",
				Delta: &dto.ClaudeMediaMessage{
					Type:     "citations_delta",
					Citation: citation,
				},
			})
		}
	}
	if info.GetSendResponseCount() == 1 {
		// Client-visible Claude stream usage matches billing merge: first
		// frame records first, a later non-zero field overrides, and a later
		// zero/missing field never erases a first-frame positive. Anthropic
		// clients treat message_delta as authoritative for the fields it
		// carries, including a corrected input_tokens.
		startUsage := &dto.ClaudeUsage{
			InputTokens:  info.GetEstimatePromptTokens(),
			OutputTokens: 0,
		}
		if openAIResponse.Usage != nil && dto.HasOpenAIUsageTokens(openAIResponse.Usage) {
			if real := buildClaudeUsageFromOpenAIUsage(openAIResponse.Usage); real != nil {
				startUsage = real
			}
			state.Usage = dto.MergeUsageNonZero(state.Usage, openAIResponse.Usage)
		}
		msg := &dto.ClaudeMediaMessage{
			Id:    openAIResponse.Id,
			Model: openAIResponse.Model,
			Type:  "message",
			Role:  "assistant",
			Usage: startUsage,
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

	if len(openAIResponse.Choices) == 0 {
		// Some OpenAI-compatible upstreams end with a usage-only SSE chunk.
		oaiUsage := clientVisibleClaudeStreamUsage(state, openAIResponse.Usage)
		if oaiUsage != nil && state.FinishReason != "" {
			appendStopOpenBlocks()
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type:  "message_delta",
				Usage: oaiUsage,
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
				state.ToolCalls = nil
				state.ToolCallByIndex = make(map[int]*convmeta.ClaudeStreamToolCall)
				state.ToolCallByID = make(map[string]*convmeta.ClaudeStreamToolCall)
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
		appendCitationDeltas(chosenChoice.Delta.Annotations)

		if doneChunk || state.Done {
			oaiUsage := clientVisibleClaudeStreamUsage(state, openAIResponse.Usage)
			if oaiUsage == nil {
				// Some upstreams emit finish_reason first, then send a final usage-only chunk.
				// Keep content blocks open until usage is available so the terminal message_delta
				// can carry both usage and the final stop reason.
				return claudeResponses
			}
			appendStopOpenBlocks()
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type:  "message_delta",
				Usage: oaiUsage,
				Delta: &dto.ClaudeMediaMessage{
					StopReason: kitutil.GetPointer[string](terminalClaudeStopReason(state)),
				},
			})
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
    if state == nil || len(toolCalls) == 0 { return nil }
    if state.ToolCallByIndex == nil { state.ToolCallByIndex = make(map[int]*convmeta.ClaudeStreamToolCall) }
    if state.ToolCallByID == nil { state.ToolCallByID = make(map[string]*convmeta.ClaudeStreamToolCall) }
    responses := make([]*dto.ClaudeResponse, 0, len(toolCalls)*2)
    for index, toolCall := range toolCalls {
        offset := index
        if toolCall.Index != nil && *toolCall.Index >= 0 { offset = *toolCall.Index }
        incomingID := strings.TrimSpace(toolCall.ID)
        tool := state.ToolCallByIndex[offset]
        if toolCall.Index == nil && incomingID != "" && state.ToolCallByID[incomingID] != nil { tool = state.ToolCallByID[incomingID] }
        if tool != nil && incomingID != "" && tool.SourceID != "" && tool.SourceID != incomingID { tool = nil }
        if tool == nil {
            tool = &convmeta.ClaudeStreamToolCall{ChatIndex: offset}
            state.ToolCalls = append(state.ToolCalls, tool)
            state.ToolCallByIndex[offset] = tool
        }
        if !tool.Started {
            if incomingID != "" { tool.ID = incomingID; tool.SourceID = incomingID; state.ToolCallByID[incomingID] = tool }
            if name := strings.TrimSpace(toolCall.Function.Name); name != "" { tool.Name = name }
        }
        if toolCall.Function.Arguments != "" {
            if tool.Started {
                arguments := toolCall.Function.Arguments
                blockIndex := tool.BlockIndex
                responses = append(responses, &dto.ClaudeResponse{Index: &blockIndex, Type: "content_block_delta", Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: &arguments}})
            } else { tool.PendingArguments += toolCall.Function.Arguments }
        }
        if offset < state.ToolCallNextIndex && !tool.Started && tool.Name != "" { responses = append(responses, startClaudeToolCall(state, tool)...) }
    }
    responses = append(responses, flushClaudeToolCalls(state, false)...)
    if state.ToolCallStartedCount > 0 { state.Index = state.ToolCallBaseIndex + state.ToolCallStartedCount - 1 }
    return responses
}

func flushClaudeToolCalls(state *convmeta.ClaudeConvertInfo, final bool) []*dto.ClaudeResponse {
    if state == nil { return nil }
    responses := make([]*dto.ClaudeResponse, 0)
    if !final {
        for {
            tool := state.ToolCallByIndex[state.ToolCallNextIndex]
            if tool == nil || strings.TrimSpace(tool.Name) == "" { break }
            responses = append(responses, startClaudeToolCall(state, tool)...)
            state.ToolCallNextIndex++
        }
        return responses
    }
    tools := append([]*convmeta.ClaudeStreamToolCall(nil), state.ToolCalls...)
    sort.SliceStable(tools, func(i, j int) bool { return tools[i].ChatIndex < tools[j].ChatIndex })
    for _, tool := range tools { responses = append(responses, startClaudeToolCall(state, tool)...) }
    return responses
}

func startClaudeToolCall(state *convmeta.ClaudeConvertInfo, toolState *convmeta.ClaudeStreamToolCall) []*dto.ClaudeResponse {
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
			Input: map[string]any{},
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
			Usage: clientVisibleClaudeStreamUsage(state, nil),
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
			citations := chatAnnotationsToClaude(choice.Message.Annotations, textContent)
			if len(citations) > 0 {
				claudeContent.Citations, _ = kitutil.Marshal(citations)
			}
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
		Input: map[string]any{},
	}
	if strings.TrimSpace(toolUse.Function.Arguments) == "" {
		return content
	}
	var input map[string]any
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
