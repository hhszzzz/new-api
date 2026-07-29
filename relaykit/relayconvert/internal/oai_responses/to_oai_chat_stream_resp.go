package oairesponses

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

type ResponsesToChatStreamState struct {
	ID           string
	Model        string
	Created      int64
	IncludeUsage bool

	Usage *dto.Usage

	sentStart                  bool
	finalized                  bool
	sawTerminal                bool
	failed                     bool
	truncated                  bool
	sawSubstantiveOutput       bool
	sawToolCall                bool
	hasSentReasoning           bool
	needsReasoningSummaryBreak bool
	nextToolIndex              int
	nextTextIndex              int
	nextReasoningIndex         int
	textByKey                  map[string]string
	textOutputIndexToKey       map[int]string
	textItemIDToKey            map[string]string
	textIdentifiedKeys         map[string]struct{}
	currentTextKey             string
	toolByKey                  map[string]*responsesStreamTool
	outputIndexToKey           map[int]string
	itemIDToKey                map[string]string
	callIDToKey                map[string]string
	pendingArgsByOutputIndex   map[int]string
	pendingArgsByItemID        map[string]string
	reasoningOpenByKey         map[string]struct{}
	reasoningTextByKey         map[string]string
	reasoningOutputIndexToKey  map[int]string
	reasoningItemIDToKey       map[string]string
	currentReasoningKey        string
	reasoningStateItems        map[string]struct{}
	usageText                  strings.Builder
}

type responsesStreamTool struct {
	Key         string
	OutputType  string
	CallID      string
	ItemID      string
	Name        string
	Arguments   string
	CustomInput string
	Index       int
	Sent        bool
	NameSent    bool
	ArgsSentAt  int
	Done        bool
}

func NewResponsesToChatStreamState(model string, includeUsage bool) *ResponsesToChatStreamState {
	return &ResponsesToChatStreamState{
		Model:                     model,
		Created:                   time.Now().Unix(),
		IncludeUsage:              includeUsage,
		Usage:                     &dto.Usage{},
		textByKey:                 make(map[string]string),
		textOutputIndexToKey:      make(map[int]string),
		textItemIDToKey:           make(map[string]string),
		textIdentifiedKeys:        make(map[string]struct{}),
		toolByKey:                 make(map[string]*responsesStreamTool),
		outputIndexToKey:          make(map[int]string),
		itemIDToKey:               make(map[string]string),
		callIDToKey:               make(map[string]string),
		pendingArgsByOutputIndex:  make(map[int]string),
		pendingArgsByItemID:       make(map[string]string),
		reasoningOpenByKey:        make(map[string]struct{}),
		reasoningTextByKey:        make(map[string]string),
		reasoningOutputIndexToKey: make(map[int]string),
		reasoningItemIDToKey:      make(map[string]string),
		reasoningStateItems:       make(map[string]struct{}),
	}
}

func (s *ResponsesToChatStreamState) UsageText() string {
	if s == nil {
		return ""
	}
	return s.usageText.String()
}

func ResponsesStreamEventToChatChunks(event *dto.ResponsesStreamResponse, state *ResponsesToChatStreamState) ([]dto.ChatCompletionsStreamResponse, error) {
	if event == nil || state == nil {
		return nil, nil
	}

	switch event.Type {
	case responsesEventCreated:
		state.applyResponseMetadata(event.Response)
		return state.ensureStart(), nil
	case responsesEventReasoningSummaryDelta, responsesEventReasoningTextDelta:
		if event.Delta != "" {
			state.sawSubstantiveOutput = true
			key := state.bindReasoningEvent(event)
			state.reasoningOpenByKey[key] = struct{}{}
			return state.reasoningDeltaForKey(key, event.Delta), nil
		}
		return nil, nil
	case responsesEventReasoningSummaryDone, responsesEventReasoningTextDone:
		if state.hasSentReasoning {
			state.needsReasoningSummaryBreak = true
		}
		return nil, nil
	case responsesEventOutputTextDelta, responsesEventRefusalDelta:
		if event.Delta != "" {
			state.sawSubstantiveOutput = true
		}
		return state.textDeltaForKey(state.bindTextEvent(event), event.Delta), nil
	case responsesEventOutputTextDone, responsesEventRefusalDone:
		key := state.bindTextEvent(event)
		finalText := event.Text
		if event.Type == responsesEventRefusalDone && event.Refusal != "" {
			finalText = event.Refusal
		}
		return state.textDeltaForKey(key, missingStreamSuffix(state.textByKey[key], finalText)), nil
	case responsesEventOutputItemAdded:
		if event.Item == nil {
			return nil, nil
		}
		if event.Item.Type == responsesOutputTypeReasoning {
			key := state.bindReasoningEvent(event)
			state.reasoningOpenByKey[key] = struct{}{}
			return nil, nil
		}
		if event.Item.Type == responsesOutputTypeMessage {
			state.bindTextEvent(event)
			return nil, nil
		}
		if !isResponsesToolOutputType(event.Item.Type) {
			return nil, nil
		}
		state.sawSubstantiveOutput = true
		return state.toolItem(event), nil
	case responsesEventOutputItemDone:
		if event.Item == nil {
			return nil, nil
		}
		if event.Item.Type == responsesOutputTypeReasoning {
			state.sawSubstantiveOutput = true
			key := state.bindReasoningEvent(event)
			delete(state.reasoningOpenByKey, key)
			if state.currentReasoningKey == key {
				state.currentReasoningKey = ""
			}
			chunks := make([]dto.ChatCompletionsStreamResponse, 0, 2)
			complete := extractReasoningTextFromOutput(event.Item)
			chunks = append(chunks, state.reasoningCompletionForKey(key, complete)...)
			chunks = append(chunks, state.providerReasoningItem(event.Item, event.OutputIndex)...)
			return chunks, nil
		}
		if event.Item.Type == responsesOutputTypeMessage {
			key := state.bindTextEvent(event)
			chunks := state.textDeltaForKey(key, missingStreamSuffix(state.textByKey[key], responsesMessageOutputText(event.Item)))
			if state.currentTextKey == key {
				state.currentTextKey = ""
			}
			return chunks, nil
		}
		if !isResponsesToolOutputType(event.Item.Type) {
			return nil, nil
		}
		state.sawSubstantiveOutput = true
		chunks := state.toolItem(event)
		if tool := state.findToolForEvent(event); tool != nil {
			tool.Done = true
		}
		return chunks, nil
	case responsesEventFunctionArgsDelta:
		if event.Delta != "" {
			state.sawSubstantiveOutput = true
		}
		return state.toolArgumentsDelta(event), nil
	case responsesEventCustomToolInputDelta:
		if event.Delta != "" {
			state.sawSubstantiveOutput = true
		}
		return state.customToolInputDelta(event), nil
	case responsesEventFunctionArgsDone:
		state.sawSubstantiveOutput = true
		return state.flushPendingTool(event), nil
	case responsesEventCustomToolInputDone:
		state.sawSubstantiveOutput = true
		return state.flushCustomToolInput(event), nil
	case responsesEventCompleted, responsesEventDone, responsesEventIncomplete:
		state.sawTerminal = true
		response := event.Response
		if event.Type == responsesEventIncomplete {
			response = ensureIncompleteResponse(response)
		}
		if err := validateResponsesTerminalResponse(response); err != nil {
			state.failed = true
			return nil, err
		}
		state.applyResponseMetadata(response)
		chunks := state.terminalOutputChunks(response)
		chunks = append(chunks, state.finalize(response)...)
		return chunks, nil
	case responsesEventCancelled, responsesEventCanceled:
		state.failed = true
		if upstreamError := event.GetOpenAIError(); upstreamError != nil && strings.TrimSpace(upstreamError.Message) != "" {
			return nil, fmt.Errorf("responses stream error: %s: %s", event.Type, strings.TrimSpace(upstreamError.Message))
		}
		return nil, fmt.Errorf("responses stream error: %s", event.Type)
	case responsesEventFailed, responsesEventError, responsesEventLegacyError:
		state.failed = true
		if upstreamError := event.GetOpenAIError(); upstreamError != nil && strings.TrimSpace(upstreamError.Message) != "" {
			return nil, fmt.Errorf("responses stream error: %s: %s", event.Type, strings.TrimSpace(upstreamError.Message))
		}
		return nil, fmt.Errorf("responses stream error: %s", event.Type)
	default:
		return nil, nil
	}
}

func FinalizeResponsesToChatStream(state *ResponsesToChatStreamState) []dto.ChatCompletionsStreamResponse {
	if state == nil {
		return nil
	}
	return state.finalize(nil)
}

func FinalizeResponsesToChatStreamChecked(state *ResponsesToChatStreamState) ([]dto.ChatCompletionsStreamResponse, error) {
	if state == nil || state.finalized {
		return nil, nil
	}
	if state.failed {
		return nil, errors.New("responses stream failed before a terminal response")
	}
	if !state.sawTerminal {
		if !state.sawSubstantiveOutput {
			return nil, errors.New("responses stream ended before producing output or a terminal response")
		}
		if len(state.reasoningOpenByKey) > 0 || state.hasOpenToolCall() {
			return nil, errors.New("responses stream ended with an incomplete reasoning or tool-call item")
		}
		state.truncated = true
	}
	return state.finalize(nil), nil
}

func (s *ResponsesToChatStreamState) applyResponseMetadata(response *dto.OpenAIResponsesResponse) {
	if response == nil {
		return
	}
	if response.ID != "" && s.ID == "" {
		s.ID = response.ID
	}
	if response.Model != "" && s.Model == "" {
		s.Model = response.Model
	}
	if response.CreatedAt != 0 {
		s.Created = int64(response.CreatedAt)
	}
	if response.Usage != nil {
		s.Usage = UsageFromResponsesUsage(response.Usage)
	}
}

func (s *ResponsesToChatStreamState) ensureStart() []dto.ChatCompletionsStreamResponse {
	if s.sentStart {
		return nil
	}
	s.sentStart = true
	return []dto.ChatCompletionsStreamResponse{s.makeChunk(dto.ChatCompletionsStreamResponseChoiceDelta{
		Role:    "assistant",
		Content: kitutil.GetPointer(""),
	}, nil)}
}

func (s *ResponsesToChatStreamState) textDeltaForKey(key, delta string) []dto.ChatCompletionsStreamResponse {
	if delta == "" {
		return nil
	}
	s.usageText.WriteString(delta)
	if key != "" {
		s.textByKey[key] += delta
	}
	chunks := s.ensureStart()
	chunks = append(chunks, s.makeChunk(dto.ChatCompletionsStreamResponseChoiceDelta{
		Content: &delta,
	}, nil))
	return chunks
}

func (s *ResponsesToChatStreamState) terminalOutputChunks(response *dto.OpenAIResponsesResponse) []dto.ChatCompletionsStreamResponse {
	if s == nil || response == nil || len(response.Output) == 0 {
		return nil
	}

	var chunks []dto.ChatCompletionsStreamResponse
	for i := range response.Output {
		out := &response.Output[i]
		switch {
		case out.Type == responsesOutputTypeMessage:
			outputIndex := i
			key := s.bindTextEvent(&dto.ResponsesStreamResponse{Item: out, OutputIndex: &outputIndex})
			complete := responsesMessageOutputText(out)
			chunks = append(chunks, s.textDeltaForKey(key, missingStreamSuffix(s.textByKey[key], complete))...)
		case out.Type == responsesOutputTypeReasoning:
			outputIndex := i
			key := s.bindReasoningEvent(&dto.ResponsesStreamResponse{Item: out, OutputIndex: &outputIndex})
			chunks = append(chunks, s.reasoningCompletionForKey(key, extractReasoningTextFromOutput(out))...)
			chunks = append(chunks, s.providerReasoningItem(out, &outputIndex)...)
		case isResponsesToolOutputType(out.Type):
			chunks = append(chunks, s.toolItem(&dto.ResponsesStreamResponse{Item: out})...)
		}
	}
	return chunks
}

func responsesMessageOutputText(output *dto.ResponsesOutput) string {
	if output == nil {
		return ""
	}
	var text strings.Builder
	for _, content := range output.Content {
		switch content.Type {
		case "output_text":
			text.WriteString(content.Text)
		case "refusal":
			text.WriteString(content.Refusal)
		}
	}
	return text.String()
}

func missingStreamSuffix(sent, complete string) string {
	if complete == "" || sent == complete || strings.HasPrefix(sent, complete) {
		return ""
	}
	if strings.HasPrefix(complete, sent) {
		return complete[len(sent):]
	}
	maxOverlap := len(sent)
	if len(complete) < maxOverlap {
		maxOverlap = len(complete)
	}
	for overlap := maxOverlap; overlap > 0; overlap-- {
		if strings.HasSuffix(sent, complete[:overlap]) {
			return complete[overlap:]
		}
	}
	return complete
}

func (s *ResponsesToChatStreamState) providerReasoningItem(item *dto.ResponsesOutput, outputIndex *int) []dto.ChatCompletionsStreamResponse {
	if s == nil || item == nil || strings.TrimSpace(item.Type) != responsesOutputTypeReasoning || strings.TrimSpace(item.EncryptedContent) == "" {
		return nil
	}
	key := strings.TrimSpace(item.ID)
	if key != "" {
		key = "item:" + key
	} else if outputIndex != nil {
		key = fmt.Sprintf("output:%d", *outputIndex)
	}
	if key != "" {
		if _, seen := s.reasoningStateItems[key]; seen {
			return nil
		}
		s.reasoningStateItems[key] = struct{}{}
	}
	itemCopy := *item
	chunk := s.makeChunk(dto.ChatCompletionsStreamResponseChoiceDelta{}, nil)
	chunk.ProviderReasoningItem = &itemCopy
	return []dto.ChatCompletionsStreamResponse{chunk}
}

func (s *ResponsesToChatStreamState) reasoningDeltaForKey(key string, delta string) []dto.ChatCompletionsStreamResponse {
	if delta == "" {
		return nil
	}
	if s.needsReasoningSummaryBreak {
		if strings.HasPrefix(delta, "\n\n") {
			s.needsReasoningSummaryBreak = false
		} else if strings.HasPrefix(delta, "\n") {
			delta = "\n" + delta
			s.needsReasoningSummaryBreak = false
		} else {
			delta = "\n\n" + delta
			s.needsReasoningSummaryBreak = false
		}
	}
	s.usageText.WriteString(delta)
	chunks := s.ensureStart()
	chunks = append(chunks, s.makeChunk(dto.ChatCompletionsStreamResponseChoiceDelta{
		ReasoningContent: &delta,
	}, nil))
	s.hasSentReasoning = true
	if key != "" {
		s.reasoningTextByKey[key] += delta
	}
	return chunks
}

func (s *ResponsesToChatStreamState) reasoningCompletionForKey(key, complete string) []dto.ChatCompletionsStreamResponse {
	delta := missingStreamSuffix(s.reasoningTextByKey[key], complete)
	if delta != "" && !strings.HasPrefix(delta, "\n") {
		s.needsReasoningSummaryBreak = false
	}
	return s.reasoningDeltaForKey(key, delta)
}

func (s *ResponsesToChatStreamState) bindTextEvent(event *dto.ResponsesStreamResponse) string {
	if s == nil {
		return ""
	}
	itemID := ""
	if event != nil {
		itemID = strings.TrimSpace(event.ItemID)
		if itemID == "" && event.Item != nil {
			itemID = strings.TrimSpace(event.Item.ID)
		}
		if event.OutputIndex != nil {
			if key := s.textOutputIndexToKey[*event.OutputIndex]; key != "" {
				s.textIdentifiedKeys[key] = struct{}{}
				if itemID != "" {
					s.textItemIDToKey[itemID] = key
				}
				s.currentTextKey = key
				return key
			}
		}
		if itemID != "" {
			if key := s.textItemIDToKey[itemID]; key != "" {
				s.textIdentifiedKeys[key] = struct{}{}
				if event.OutputIndex != nil {
					s.textOutputIndexToKey[*event.OutputIndex] = key
				}
				s.currentTextKey = key
				return key
			}
		}
	}

	key := ""
	if s.currentTextKey != "" {
		_, identified := s.textIdentifiedKeys[s.currentTextKey]
		if (event == nil || (event.OutputIndex == nil && itemID == "")) || !identified {
			key = s.currentTextKey
		}
	}
	if key == "" && event != nil && event.OutputIndex != nil {
		key = fmt.Sprintf("output:%d", *event.OutputIndex)
	}
	if key == "" && itemID != "" {
		key = "item:" + itemID
	}
	if key == "" {
		key = fmt.Sprintf("text:%d", s.nextTextIndex)
		s.nextTextIndex++
	}
	if event != nil && event.OutputIndex != nil {
		s.textOutputIndexToKey[*event.OutputIndex] = key
		s.textIdentifiedKeys[key] = struct{}{}
	}
	if itemID != "" {
		s.textItemIDToKey[itemID] = key
		s.textIdentifiedKeys[key] = struct{}{}
	}
	s.currentTextKey = key
	return key
}

func (s *ResponsesToChatStreamState) bindReasoningEvent(event *dto.ResponsesStreamResponse) string {
	if s == nil {
		return ""
	}
	itemID := ""
	if event != nil {
		itemID = strings.TrimSpace(event.ItemID)
		if itemID == "" && event.Item != nil {
			itemID = strings.TrimSpace(event.Item.ID)
		}
		if event.OutputIndex != nil {
			if key := s.reasoningOutputIndexToKey[*event.OutputIndex]; key != "" {
				if itemID != "" {
					s.reasoningItemIDToKey[itemID] = key
				}
				s.currentReasoningKey = key
				return key
			}
		}
		if itemID != "" {
			if key := s.reasoningItemIDToKey[itemID]; key != "" {
				if event.OutputIndex != nil {
					s.reasoningOutputIndexToKey[*event.OutputIndex] = key
				}
				s.currentReasoningKey = key
				return key
			}
		}
	}

	key := ""
	if event != nil && event.OutputIndex != nil {
		key = fmt.Sprintf("output:%d", *event.OutputIndex)
	}
	if key == "" && itemID != "" {
		key = "item:" + itemID
	}
	if key == "" && s.currentReasoningKey != "" {
		if _, open := s.reasoningOpenByKey[s.currentReasoningKey]; open {
			key = s.currentReasoningKey
		}
	}
	if key == "" {
		key = fmt.Sprintf("reasoning:%d", s.nextReasoningIndex)
		s.nextReasoningIndex++
	}
	if event != nil && event.OutputIndex != nil {
		s.reasoningOutputIndexToKey[*event.OutputIndex] = key
	}
	if itemID != "" {
		s.reasoningItemIDToKey[itemID] = key
	}
	s.currentReasoningKey = key
	return key
}

func (s *ResponsesToChatStreamState) toolItem(event *dto.ResponsesStreamResponse) []dto.ChatCompletionsStreamResponse {
	tool := s.ensureToolForEvent(event)
	if tool == nil {
		return nil
	}
	switch event.Item.Type {
	case responsesOutputTypeCustomToolCall:
		if event.Item.Input != "" {
			tool.CustomInput = event.Item.Input
		}
		if event.Type == responsesEventOutputItemDone || event.Item.Input != "" {
			tool.Arguments = customInputArguments(tool.CustomInput)
		}
	case responsesOutputTypeToolSearchCall:
		if args := toolSearchArguments(event.Item.Arguments); args != "" {
			tool.Arguments = args
		}
	default:
		if args := event.Item.ArgumentsString(); args != "" {
			tool.Arguments = args
		}
	}
	return s.toolDelta(tool, "")
}

func (s *ResponsesToChatStreamState) hasOpenToolCall() bool {
	if s == nil {
		return false
	}
	if len(s.pendingArgsByOutputIndex) > 0 || len(s.pendingArgsByItemID) > 0 {
		return true
	}
	seen := make(map[*responsesStreamTool]struct{}, len(s.toolByKey))
	for _, tool := range s.toolByKey {
		if tool == nil {
			continue
		}
		if _, ok := seen[tool]; ok {
			continue
		}
		seen[tool] = struct{}{}
		if !tool.Done {
			return true
		}
	}
	return false
}

func (s *ResponsesToChatStreamState) toolArgumentsDelta(event *dto.ResponsesStreamResponse) []dto.ChatCompletionsStreamResponse {
	if event.Delta == "" {
		return nil
	}
	tool := s.findToolForEvent(event)
	if tool == nil {
		if event.OutputIndex != nil {
			s.pendingArgsByOutputIndex[*event.OutputIndex] += event.Delta
		} else if itemID := strings.TrimSpace(event.ItemID); itemID != "" {
			s.pendingArgsByItemID[itemID] += event.Delta
		}
		return nil
	}
	tool.Arguments += event.Delta
	return s.toolDelta(tool, event.Delta)
}

func (s *ResponsesToChatStreamState) customToolInputDelta(event *dto.ResponsesStreamResponse) []dto.ChatCompletionsStreamResponse {
	if event == nil || event.Delta == "" {
		return nil
	}
	tool := s.findToolForEvent(event)
	if tool == nil {
		if event.OutputIndex != nil {
			s.pendingArgsByOutputIndex[*event.OutputIndex] += event.Delta
		} else if itemID := strings.TrimSpace(event.ItemID); itemID != "" {
			s.pendingArgsByItemID[itemID] += event.Delta
		}
		return nil
	}
	tool.CustomInput += event.Delta
	return nil
}

func (s *ResponsesToChatStreamState) flushPendingTool(event *dto.ResponsesStreamResponse) []dto.ChatCompletionsStreamResponse {
	tool := s.findToolForEvent(event)
	if tool == nil {
		tool = s.ensureFallbackToolForEvent(event)
	}
	if tool == nil {
		return nil
	}
	if event != nil && event.Arguments != "" {
		tool.Arguments = event.Arguments
	}
	return s.toolDelta(tool, "")
}

func (s *ResponsesToChatStreamState) flushCustomToolInput(event *dto.ResponsesStreamResponse) []dto.ChatCompletionsStreamResponse {
	tool := s.findToolForEvent(event)
	if tool == nil {
		tool = s.ensureFallbackToolForEvent(event)
	}
	if tool == nil {
		return nil
	}
	tool.OutputType = responsesOutputTypeCustomToolCall
	if event != nil && event.Input != "" {
		tool.CustomInput = event.Input
	}
	tool.Arguments = customInputArguments(tool.CustomInput)
	return s.toolDelta(tool, "")
}

func (s *ResponsesToChatStreamState) ensureToolForEvent(event *dto.ResponsesStreamResponse) *responsesStreamTool {
	if event == nil || event.Item == nil {
		return nil
	}
	key := s.keyForEvent(event)
	if key == "" {
		key = fallbackToolKey(event.Item.ID, event.Item.CallId, event.OutputIndex)
	}
	if key == "" {
		return nil
	}

	tool := s.toolByKey[key]
	if tool == nil {
		if itemID := responseStreamEventItemID(event); itemID != "" {
			if existingKey := s.itemIDToKey[itemID]; existingKey != "" {
				tool = s.toolByKey[existingKey]
			}
		}
		if tool == nil {
			if callID := strings.TrimSpace(event.Item.CallId); callID != "" {
				if existingKey := s.callIDToKey[callID]; existingKey != "" {
					tool = s.toolByKey[existingKey]
				}
			}
		}
		if tool != nil {
			s.toolByKey[key] = tool
		}
	}
	if tool == nil {
		tool = &responsesStreamTool{Key: key, Index: s.nextToolIndex}
		s.nextToolIndex++
		s.toolByKey[key] = tool
	}
	tool.OutputType = event.Item.Type

	if event.OutputIndex != nil {
		s.outputIndexToKey[*event.OutputIndex] = key
		if pending := s.pendingArgsByOutputIndex[*event.OutputIndex]; pending != "" {
			appendResponsesToolPendingDelta(tool, pending)
			delete(s.pendingArgsByOutputIndex, *event.OutputIndex)
		}
	}
	if itemID := responseStreamEventItemID(event); itemID != "" {
		tool.ItemID = itemID
		s.itemIDToKey[itemID] = key
		if pending := s.pendingArgsByItemID[itemID]; pending != "" {
			appendResponsesToolPendingDelta(tool, pending)
			delete(s.pendingArgsByItemID, itemID)
		}
	}
	if callID := strings.TrimSpace(event.Item.CallId); callID != "" {
		tool.CallID = callID
		s.callIDToKey[callID] = key
	} else if tool.CallID == "" {
		tool.CallID = strings.TrimSpace(event.Item.ID)
	}
	if name := strings.TrimSpace(event.Item.Name); name != "" {
		tool.Name = name
	} else if event.Item.Type == responsesOutputTypeToolSearchCall {
		tool.Name = "tool_search"
	}
	return tool
}

func appendResponsesToolPendingDelta(tool *responsesStreamTool, delta string) {
	if tool == nil || delta == "" {
		return
	}
	if tool.OutputType == responsesOutputTypeCustomToolCall {
		tool.CustomInput += delta
		return
	}
	tool.Arguments += delta
}

func (s *ResponsesToChatStreamState) findToolForEvent(event *dto.ResponsesStreamResponse) *responsesStreamTool {
	if event == nil {
		return nil
	}
	if event.OutputIndex != nil {
		if key := s.outputIndexToKey[*event.OutputIndex]; key != "" {
			return s.toolByKey[key]
		}
	}
	if itemID := strings.TrimSpace(event.ItemID); itemID != "" {
		if key := s.itemIDToKey[itemID]; key != "" {
			return s.toolByKey[key]
		}
	}
	if event.Item != nil {
		if key := s.keyForEvent(event); key != "" {
			return s.toolByKey[key]
		}
	}
	return nil
}

func (s *ResponsesToChatStreamState) ensureFallbackToolForEvent(event *dto.ResponsesStreamResponse) *responsesStreamTool {
	if event == nil {
		return nil
	}
	key := ""
	if event.OutputIndex != nil {
		key = fmt.Sprintf("output:%d", *event.OutputIndex)
	}
	if key == "" && strings.TrimSpace(event.ItemID) != "" {
		key = "item:" + strings.TrimSpace(event.ItemID)
	}
	if key == "" {
		return nil
	}
	tool := s.toolByKey[key]
	if tool == nil {
		tool = &responsesStreamTool{
			Key:    key,
			Index:  s.nextToolIndex,
			CallID: fallbackCallID(event),
		}
		s.nextToolIndex++
		s.toolByKey[key] = tool
	}
	if event.Type == responsesEventCustomToolInputDelta || event.Type == responsesEventCustomToolInputDone {
		tool.OutputType = responsesOutputTypeCustomToolCall
	}
	if name := strings.TrimSpace(event.Name); name != "" {
		tool.Name = name
	}
	if event.OutputIndex != nil {
		s.outputIndexToKey[*event.OutputIndex] = key
		if pending := s.pendingArgsByOutputIndex[*event.OutputIndex]; pending != "" {
			appendResponsesToolPendingDelta(tool, pending)
			delete(s.pendingArgsByOutputIndex, *event.OutputIndex)
		}
	}
	if itemID := responseStreamEventItemID(event); itemID != "" {
		tool.ItemID = itemID
		s.itemIDToKey[itemID] = key
		if pending := s.pendingArgsByItemID[itemID]; pending != "" {
			appendResponsesToolPendingDelta(tool, pending)
			delete(s.pendingArgsByItemID, itemID)
		}
	}
	return tool
}

func (s *ResponsesToChatStreamState) toolDelta(tool *responsesStreamTool, explicitDelta string) []dto.ChatCompletionsStreamResponse {
	if tool == nil {
		return nil
	}

	argsDelta := explicitDelta
	if argsDelta == "" && len(tool.Arguments) > tool.ArgsSentAt {
		argsDelta = tool.Arguments[tool.ArgsSentAt:]
	}
	if tool.Sent && argsDelta == "" && (tool.Name == "" || tool.NameSent) {
		return nil
	}
	if !tool.Sent && strings.TrimSpace(tool.Name) == "" {
		return nil
	}

	chunks := s.ensureStart()
	callID := strings.TrimSpace(tool.CallID)
	if callID == "" {
		callID = tool.Key
	}
	responseTool := dto.ToolCallResponse{
		ID:   callID,
		Type: "function",
		Function: dto.FunctionResponse{
			Arguments: argsDelta,
		},
	}
	responseTool.SetIndex(tool.Index)
	if !tool.NameSent && tool.Name != "" {
		responseTool.Function.Name = tool.Name
		tool.NameSent = true
	}
	if !tool.Sent {
		tool.Sent = true
	}
	if argsDelta != "" {
		tool.ArgsSentAt += len(argsDelta)
		s.usageText.WriteString(argsDelta)
	}
	if responseTool.Function.Name != "" {
		s.usageText.WriteString(responseTool.Function.Name)
	}

	chunks = append(chunks, s.makeChunk(dto.ChatCompletionsStreamResponseChoiceDelta{
		ToolCalls: []dto.ToolCallResponse{responseTool},
	}, nil))
	s.sawToolCall = true
	return chunks
}

func (s *ResponsesToChatStreamState) finalize(response *dto.OpenAIResponsesResponse) []dto.ChatCompletionsStreamResponse {
	if s.finalized {
		return nil
	}
	s.finalized = true

	chunks := s.flushAllPendingTools()
	chunks = append(chunks, s.ensureStart()...)

	finishReason := "stop"
	if s.truncated {
		finishReason = "length"
	} else if mappedReason, ok := ResponsesFinishReasonFromStatus(response); ok {
		finishReason = mappedReason
	} else if s.sawToolCall {
		finishReason = "tool_calls"
	}
	chunks = append(chunks, s.makeChunk(dto.ChatCompletionsStreamResponseChoiceDelta{}, &finishReason))
	if s.IncludeUsage && s.Usage != nil {
		chunks = append(chunks, dto.ChatCompletionsStreamResponse{
			Id:      s.ID,
			Object:  "chat.completion.chunk",
			Created: s.Created,
			Model:   s.Model,
			Choices: make([]dto.ChatCompletionsStreamResponseChoice, 0),
			Usage:   s.Usage,
		})
	}
	return chunks
}

func (s *ResponsesToChatStreamState) flushAllPendingTools() []dto.ChatCompletionsStreamResponse {
	keys := make([]string, 0, len(s.toolByKey)+len(s.pendingArgsByOutputIndex)+len(s.pendingArgsByItemID))
	seen := make(map[string]bool)
	for key := range s.toolByKey {
		keys = append(keys, key)
		seen[key] = true
	}
	for outputIndex := range s.pendingArgsByOutputIndex {
		key := fmt.Sprintf("output:%d", outputIndex)
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	for itemID := range s.pendingArgsByItemID {
		key := "item:" + itemID
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	sort.Strings(keys)

	var chunks []dto.ChatCompletionsStreamResponse
	for _, key := range keys {
		tool := s.toolByKey[key]
		if tool == nil {
			callID := strings.TrimPrefix(key, "item:")
			if strings.HasPrefix(key, "output:") {
				callID = "call_output_" + strings.TrimPrefix(key, "output:")
			}
			tool = &responsesStreamTool{
				Key:    key,
				Index:  s.nextToolIndex,
				CallID: callID,
			}
			s.nextToolIndex++
			s.toolByKey[key] = tool
		}
		if strings.HasPrefix(key, "output:") {
			var outputIndex int
			if _, err := fmt.Sscanf(key, "output:%d", &outputIndex); err == nil {
				tool.Arguments += s.pendingArgsByOutputIndex[outputIndex]
				delete(s.pendingArgsByOutputIndex, outputIndex)
			}
		}
		if strings.HasPrefix(key, "item:") {
			itemID := strings.TrimPrefix(key, "item:")
			tool.Arguments += s.pendingArgsByItemID[itemID]
			delete(s.pendingArgsByItemID, itemID)
		}
		chunks = append(chunks, s.toolDelta(tool, "")...)
	}
	return chunks
}

func (s *ResponsesToChatStreamState) makeChunk(delta dto.ChatCompletionsStreamResponseChoiceDelta, finishReason *string) dto.ChatCompletionsStreamResponse {
	return dto.ChatCompletionsStreamResponse{
		Id:      s.ID,
		Object:  "chat.completion.chunk",
		Created: s.Created,
		Model:   s.Model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index:        0,
				Delta:        delta,
				FinishReason: finishReason,
			},
		},
	}
}

func (s *ResponsesToChatStreamState) keyForEvent(event *dto.ResponsesStreamResponse) string {
	if event == nil {
		return ""
	}
	if event.OutputIndex != nil {
		return fmt.Sprintf("output:%d", *event.OutputIndex)
	}
	if event.Item != nil {
		if itemID := strings.TrimSpace(event.Item.ID); itemID != "" {
			return "item:" + itemID
		}
		if callID := strings.TrimSpace(event.Item.CallId); callID != "" {
			return "call:" + callID
		}
	}
	if itemID := strings.TrimSpace(event.ItemID); itemID != "" {
		return "item:" + itemID
	}
	return ""
}

type ResponsesBufferedAccumulator struct {
	text                 strings.Builder
	reasoning            strings.Builder
	itemsByOutputIndex   map[int]dto.ResponsesOutput
	textByOutputIndex    map[int]*strings.Builder
	refusalByOutputIndex map[int]*strings.Builder
	summaryByOutputIndex map[int]*strings.Builder
	reasoningByOutputIdx map[int]*strings.Builder
	tools                []*responsesBufferedTool
	outputIndexToToolIdx map[int]int
	itemIDToToolIdx      map[string]int
	pendingByOutputIndex map[int]string
	pendingByItemID      map[string]string
}

type responsesBufferedTool struct {
	OutputIndex *int
	CallID      string
	ItemID      string
	Name        string
	Arguments   strings.Builder
}

func NewResponsesBufferedAccumulator() *ResponsesBufferedAccumulator {
	return &ResponsesBufferedAccumulator{
		itemsByOutputIndex:   make(map[int]dto.ResponsesOutput),
		textByOutputIndex:    make(map[int]*strings.Builder),
		refusalByOutputIndex: make(map[int]*strings.Builder),
		summaryByOutputIndex: make(map[int]*strings.Builder),
		reasoningByOutputIdx: make(map[int]*strings.Builder),
		outputIndexToToolIdx: make(map[int]int),
		itemIDToToolIdx:      make(map[string]int),
		pendingByOutputIndex: make(map[int]string),
		pendingByItemID:      make(map[string]string),
	}
}

func (a *ResponsesBufferedAccumulator) ProcessEvent(event *dto.ResponsesStreamResponse) {
	if a == nil || event == nil {
		return
	}
	switch event.Type {
	case responsesEventOutputTextDelta:
		if event.OutputIndex == nil {
			a.text.WriteString(event.Delta)
			return
		}
		a.outputBuilder(a.textByOutputIndex, *event.OutputIndex).WriteString(event.Delta)
	case responsesEventRefusalDelta:
		if event.OutputIndex == nil {
			a.text.WriteString(event.Delta)
			return
		}
		a.outputBuilder(a.refusalByOutputIndex, *event.OutputIndex).WriteString(event.Delta)
	case responsesEventReasoningSummaryDelta:
		if event.OutputIndex == nil {
			a.reasoning.WriteString(event.Delta)
			return
		}
		a.outputBuilder(a.summaryByOutputIndex, *event.OutputIndex).WriteString(event.Delta)
	case responsesEventReasoningTextDelta:
		if event.OutputIndex == nil {
			a.reasoning.WriteString(event.Delta)
			return
		}
		a.outputBuilder(a.reasoningByOutputIdx, *event.OutputIndex).WriteString(event.Delta)
	case responsesEventOutputItemAdded, responsesEventOutputItemDone:
		if event.Item == nil {
			return
		}
		if event.OutputIndex != nil {
			a.itemsByOutputIndex[*event.OutputIndex] = cloneResponsesOutput(*event.Item)
		}
		if isResponsesToolOutputType(event.Item.Type) {
			tool := a.ensureTool(event)
			if args := event.Item.ArgumentsString(); args != "" {
				tool.Arguments.Reset()
				tool.Arguments.WriteString(args)
			}
			if event.Item.Type == responsesOutputTypeCustomToolCall && event.Item.Input != "" {
				tool.Arguments.Reset()
				tool.Arguments.WriteString(event.Item.Input)
			}
		}
	case responsesEventFunctionArgsDelta, responsesEventCustomToolInputDelta:
		if idx, ok := a.findToolIndex(event); ok {
			a.tools[idx].Arguments.WriteString(event.Delta)
			return
		}
		if event.OutputIndex != nil {
			a.pendingByOutputIndex[*event.OutputIndex] += event.Delta
		} else if itemID := strings.TrimSpace(event.ItemID); itemID != "" {
			a.pendingByItemID[itemID] += event.Delta
		}
	case responsesEventFunctionArgsDone:
		if tool := a.ensureTool(event); tool != nil && event.Arguments != "" {
			tool.Arguments.Reset()
			tool.Arguments.WriteString(event.Arguments)
		}
	case responsesEventCustomToolInputDone:
		if tool := a.ensureTool(event); tool != nil && event.Input != "" {
			tool.Arguments.Reset()
			tool.Arguments.WriteString(event.Input)
		}
	case responsesEventOutputTextDone:
		if event.OutputIndex != nil && event.Text != "" {
			builder := a.outputBuilder(a.textByOutputIndex, *event.OutputIndex)
			builder.Reset()
			builder.WriteString(event.Text)
		}
	case responsesEventRefusalDone:
		refusalText := event.Refusal
		if refusalText == "" {
			refusalText = event.Text
		}
		if event.OutputIndex != nil && refusalText != "" {
			builder := a.outputBuilder(a.refusalByOutputIndex, *event.OutputIndex)
			builder.Reset()
			builder.WriteString(refusalText)
		}
	case responsesEventReasoningSummaryDone:
		if event.OutputIndex != nil && event.Text != "" {
			builder := a.outputBuilder(a.summaryByOutputIndex, *event.OutputIndex)
			builder.Reset()
			builder.WriteString(event.Text)
		}
	case responsesEventReasoningTextDone:
		if event.OutputIndex != nil && event.Text != "" {
			builder := a.outputBuilder(a.reasoningByOutputIdx, *event.OutputIndex)
			builder.Reset()
			builder.WriteString(event.Text)
		}
	}
}

func (a *ResponsesBufferedAccumulator) SupplementResponseOutput(resp *dto.OpenAIResponsesResponse) {
	if a == nil || resp == nil {
		return
	}
	buffered := a.BuildOutput()
	if len(buffered) == 0 {
		return
	}
	if len(resp.Output) == 0 {
		resp.Output = buffered
		return
	}

	terminalUsed := make([]bool, len(resp.Output))
	merged := make([]dto.ResponsesOutput, 0, len(buffered)+len(resp.Output))
	typeOrdinals := make(map[string]int)
	for _, bufferedItem := range buffered {
		terminalIndex := matchResponsesOutput(bufferedItem, resp.Output, terminalUsed, typeOrdinals)
		if terminalIndex < 0 {
			merged = append(merged, bufferedItem)
			continue
		}
		terminalUsed[terminalIndex] = true
		merged = append(merged, mergeResponsesOutput(bufferedItem, resp.Output[terminalIndex]))
	}
	for index, terminalItem := range resp.Output {
		if !terminalUsed[index] {
			merged = append(merged, terminalItem)
		}
	}
	resp.Output = merged
}

func (a *ResponsesBufferedAccumulator) BuildOutput() []dto.ResponsesOutput {
	if a == nil {
		return nil
	}
	indexes := make(map[int]struct{})
	for index := range a.itemsByOutputIndex {
		indexes[index] = struct{}{}
	}
	for index := range a.textByOutputIndex {
		indexes[index] = struct{}{}
	}
	for index := range a.refusalByOutputIndex {
		indexes[index] = struct{}{}
	}
	for index := range a.summaryByOutputIndex {
		indexes[index] = struct{}{}
	}
	for index := range a.reasoningByOutputIdx {
		indexes[index] = struct{}{}
	}
	for index := range a.outputIndexToToolIdx {
		indexes[index] = struct{}{}
	}
	orderedIndexes := make([]int, 0, len(indexes))
	for index := range indexes {
		orderedIndexes = append(orderedIndexes, index)
	}
	sort.Ints(orderedIndexes)

	out := make([]dto.ResponsesOutput, 0, len(orderedIndexes)+2+len(a.tools))
	if a.reasoning.Len() > 0 {
		out = append(out, dto.ResponsesOutput{
			Type: responsesOutputTypeReasoning,
			Summary: []dto.ResponsesReasoningSummaryPart{
				{Type: "summary_text", Text: a.reasoning.String()},
			},
		})
	}
	if a.text.Len() > 0 {
		out = append(out, dto.ResponsesOutput{
			Type: responsesOutputTypeMessage,
			Role: "assistant",
			Content: []dto.ResponsesOutputContent{
				{Type: "output_text", Text: a.text.String()},
			},
		})
	}
	for _, outputIndex := range orderedIndexes {
		item := cloneResponsesOutput(a.itemsByOutputIndex[outputIndex])
		if text := builderString(a.textByOutputIndex[outputIndex]); text != "" && len(item.Content) == 0 {
			item.Type = responsesOutputTypeMessage
			item.Role = "assistant"
			item.Content = []dto.ResponsesOutputContent{{Type: "output_text", Text: text}}
		}
		if refusal := builderString(a.refusalByOutputIndex[outputIndex]); refusal != "" && len(item.Content) == 0 {
			item.Type = responsesOutputTypeMessage
			item.Role = "assistant"
			item.Content = []dto.ResponsesOutputContent{{Type: "refusal", Refusal: refusal}}
		}
		if summary := builderString(a.summaryByOutputIndex[outputIndex]); summary != "" && len(item.Summary) == 0 {
			item.Type = responsesOutputTypeReasoning
			item.Summary = []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: summary}}
		}
		if reasoning := builderString(a.reasoningByOutputIdx[outputIndex]); reasoning != "" && len(item.Content) == 0 {
			item.Type = responsesOutputTypeReasoning
			item.Content = []dto.ResponsesOutputContent{{Type: "reasoning_text", Text: reasoning}}
		}
		if toolIndex, ok := a.outputIndexToToolIdx[outputIndex]; ok && toolIndex >= 0 && toolIndex < len(a.tools) {
			item = mergeResponsesOutput(a.bufferedToolOutput(a.tools[toolIndex]), item)
		}
		if strings.TrimSpace(item.Type) != "" {
			out = append(out, item)
		}
	}
	for _, tool := range a.tools {
		if tool == nil {
			continue
		}
		if tool.OutputIndex != nil {
			continue
		}
		out = append(out, a.bufferedToolOutput(tool))
	}
	return out
}

func (a *ResponsesBufferedAccumulator) ensureTool(event *dto.ResponsesStreamResponse) *responsesBufferedTool {
	if idx, ok := a.findToolIndex(event); ok {
		tool := a.tools[idx]
		a.applyToolMetadata(tool, event)
		return tool
	}
	tool := &responsesBufferedTool{}
	if event.OutputIndex != nil {
		outputIndex := *event.OutputIndex
		tool.OutputIndex = &outputIndex
	}
	a.applyToolMetadata(tool, event)
	idx := len(a.tools)
	a.tools = append(a.tools, tool)
	if event.OutputIndex != nil {
		a.outputIndexToToolIdx[*event.OutputIndex] = idx
		if pending := a.pendingByOutputIndex[*event.OutputIndex]; pending != "" {
			tool.Arguments.WriteString(pending)
			delete(a.pendingByOutputIndex, *event.OutputIndex)
		}
	}
	if tool.ItemID != "" {
		a.itemIDToToolIdx[tool.ItemID] = idx
		if pending := a.pendingByItemID[tool.ItemID]; pending != "" {
			tool.Arguments.WriteString(pending)
			delete(a.pendingByItemID, tool.ItemID)
		}
	}
	return tool
}

func (a *ResponsesBufferedAccumulator) outputBuilder(values map[int]*strings.Builder, outputIndex int) *strings.Builder {
	builder := values[outputIndex]
	if builder == nil {
		builder = &strings.Builder{}
		values[outputIndex] = builder
	}
	return builder
}

func (a *ResponsesBufferedAccumulator) bufferedToolOutput(tool *responsesBufferedTool) dto.ResponsesOutput {
	if tool == nil {
		return dto.ResponsesOutput{}
	}
	argsRaw, _ := kitutil.Marshal(tool.Arguments.String())
	output := dto.ResponsesOutput{
		Type:      responsesOutputTypeFunctionCall,
		ID:        tool.ItemID,
		CallId:    tool.CallID,
		Name:      tool.Name,
		Arguments: argsRaw,
	}
	if item, ok := a.itemForTool(tool); ok && item.Type == responsesOutputTypeCustomToolCall {
		output.Type = responsesOutputTypeCustomToolCall
		output.Input = tool.Arguments.String()
		output.Arguments = nil
	} else if ok && item.Type == responsesOutputTypeToolSearchCall {
		output.Type = responsesOutputTypeToolSearchCall
		output.Name = "tool_search"
		output.Arguments = toolSearchArgumentsRaw(tool.Arguments.String())
	}
	return output
}

func (a *ResponsesBufferedAccumulator) itemForTool(tool *responsesBufferedTool) (dto.ResponsesOutput, bool) {
	if a == nil || tool == nil || tool.OutputIndex == nil {
		return dto.ResponsesOutput{}, false
	}
	item, ok := a.itemsByOutputIndex[*tool.OutputIndex]
	return item, ok
}

func builderString(builder *strings.Builder) string {
	if builder == nil {
		return ""
	}
	return builder.String()
}

func cloneResponsesOutput(item dto.ResponsesOutput) dto.ResponsesOutput {
	item.Content = append([]dto.ResponsesOutputContent(nil), item.Content...)
	item.Summary = append([]dto.ResponsesReasoningSummaryPart(nil), item.Summary...)
	item.Arguments = append([]byte(nil), item.Arguments...)
	return item
}

func matchResponsesOutput(buffered dto.ResponsesOutput, terminal []dto.ResponsesOutput, used []bool, typeOrdinals map[string]int) int {
	for index, candidate := range terminal {
		if used[index] {
			continue
		}
		if buffered.ID != "" && buffered.ID == candidate.ID {
			return index
		}
		if buffered.CallId != "" && buffered.CallId == candidate.CallId {
			return index
		}
	}

	itemType := strings.TrimSpace(buffered.Type)
	if itemType == "" {
		return -1
	}
	hasIdentity := buffered.ID != "" || buffered.CallId != ""
	ordinalKey := itemType
	if hasIdentity {
		ordinalKey += ":terminal_without_identity"
	}
	targetOrdinal := typeOrdinals[ordinalKey]
	typeOrdinals[ordinalKey] = targetOrdinal + 1
	ordinal := 0
	for index, candidate := range terminal {
		if used[index] || strings.TrimSpace(candidate.Type) != itemType {
			continue
		}
		if hasIdentity && (candidate.ID != "" || candidate.CallId != "") {
			continue
		}
		if ordinal == targetOrdinal {
			return index
		}
		ordinal++
	}
	return -1
}

func mergeResponsesOutput(buffered dto.ResponsesOutput, terminal dto.ResponsesOutput) dto.ResponsesOutput {
	merged := cloneResponsesOutput(terminal)
	if merged.Type == "" {
		merged.Type = buffered.Type
	}
	if merged.ID == "" {
		merged.ID = buffered.ID
	}
	if merged.Status == "" {
		merged.Status = buffered.Status
	}
	if merged.Role == "" {
		merged.Role = buffered.Role
	}
	if merged.Phase == "" {
		merged.Phase = buffered.Phase
	}
	if len(merged.Content) == 0 {
		merged.Content = append([]dto.ResponsesOutputContent(nil), buffered.Content...)
	}
	if len(merged.Summary) == 0 {
		merged.Summary = append([]dto.ResponsesReasoningSummaryPart(nil), buffered.Summary...)
	}
	if merged.CallId == "" {
		merged.CallId = buffered.CallId
	}
	if merged.Name == "" {
		merged.Name = buffered.Name
	}
	if merged.Namespace == "" {
		merged.Namespace = buffered.Namespace
	}
	if merged.Input == "" {
		merged.Input = buffered.Input
	}
	if merged.Execution == "" {
		merged.Execution = buffered.Execution
	}
	if len(merged.Arguments) == 0 {
		merged.Arguments = append([]byte(nil), buffered.Arguments...)
	}
	if merged.EncryptedContent == "" {
		merged.EncryptedContent = buffered.EncryptedContent
	}
	return merged
}

func (a *ResponsesBufferedAccumulator) applyToolMetadata(tool *responsesBufferedTool, event *dto.ResponsesStreamResponse) {
	if tool == nil || event == nil || event.Item == nil {
		return
	}
	if itemID := strings.TrimSpace(event.Item.ID); itemID != "" {
		tool.ItemID = itemID
	}
	if callID := strings.TrimSpace(event.Item.CallId); callID != "" {
		tool.CallID = callID
	} else if tool.CallID == "" {
		tool.CallID = strings.TrimSpace(event.Item.ID)
	}
	if name := strings.TrimSpace(event.Item.Name); name != "" {
		tool.Name = name
	} else if event.Item.Type == responsesOutputTypeToolSearchCall {
		tool.Name = "tool_search"
	}
}

func (a *ResponsesBufferedAccumulator) findToolIndex(event *dto.ResponsesStreamResponse) (int, bool) {
	if event == nil {
		return 0, false
	}
	if event.OutputIndex != nil {
		if idx, ok := a.outputIndexToToolIdx[*event.OutputIndex]; ok {
			return idx, true
		}
	}
	itemID := strings.TrimSpace(event.ItemID)
	if itemID == "" && event.Item != nil {
		itemID = strings.TrimSpace(event.Item.ID)
	}
	if itemID != "" {
		idx, ok := a.itemIDToToolIdx[itemID]
		return idx, ok
	}
	return 0, false
}
