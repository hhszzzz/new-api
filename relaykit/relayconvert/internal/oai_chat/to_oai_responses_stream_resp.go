package oaichat

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
)

type ChatToResponsesStreamEvent struct {
	Type    string
	Payload dto.ResponsesStreamResponse
}

type ChatToResponsesStreamState struct {
	ID        string
	Model     string
	Created   int64
	Usage     *dto.Usage
	ToolState *sharedbridge.ToolState

	status               string
	incompleteDetails    *dto.IncompleteDetails
	sentCreated          bool
	sentInProgress       bool
	hasToolCalls         bool
	textOutputIndex      int
	messageStarted       bool
	messageDone          bool
	nextContentIndex     int
	textContentIndex     int
	textStarted          bool
	textDone             bool
	refusalContentIndex  int
	refusalStarted       bool
	refusalDone          bool
	activeReasoningIndex int
	sawFinishReason      bool
	finalized            bool
	nextOutputIndex      int
	nextSequenceNumber   int
	nextToolIndexToAdd   int
	toolsByIndex         map[int]*chatToResponsesStreamTool
	usedToolCallIDs      map[string]struct{}
	reasoningItems       []*chatToResponsesStreamReasoning
	outputOrder          []chatToResponsesOutputRef
	text                 strings.Builder
	refusal              strings.Builder
}

type chatToResponsesStreamTool struct {
	ChatIndex     int
	OutputIndex   int
	CallID        string
	SourceCallID  string
	ItemID        string
	Name          string
	Identity      sharedbridge.ToolIdentity
	HasIdentity   bool
	Arguments     strings.Builder
	ArgumentsSent int
	Added         bool
	Done          bool
	Skipped       bool
}

type chatToResponsesStreamReasoning struct {
	OutputIndex      int
	ItemID           string
	EncryptedContent string
	Status           string
	Text             strings.Builder
	PartStarted      bool
	Done             bool
}

type chatToResponsesOutputRef struct {
	Kind           string
	ToolIndex      int
	ReasoningIndex int
}

func NewChatToResponsesStreamState(id string, model string) *ChatToResponsesStreamState {
	return &ChatToResponsesStreamState{
		ID:                   normalizeResponsesResponseID(id),
		Model:                model,
		Created:              time.Now().Unix(),
		Usage:                &dto.Usage{},
		status:               "completed",
		textOutputIndex:      -1,
		textContentIndex:     -1,
		refusalContentIndex:  -1,
		activeReasoningIndex: -1,
		toolsByIndex:         make(map[int]*chatToResponsesStreamTool),
		usedToolCallIDs:      make(map[string]struct{}),
	}
}

func ChatCompletionsStreamChunkToResponsesEvents(chunk *dto.ChatCompletionsStreamResponse, state *ChatToResponsesStreamState) ([]ChatToResponsesStreamEvent, error) {
	if chunk == nil || state == nil {
		return nil, nil
	}
	if state.ID == "" {
		state.ID = normalizeResponsesResponseID(chunk.Id)
	}
	if state.Model == "" {
		state.Model = chunk.Model
	}
	if state.Created == 0 {
		state.Created = chunk.Created
	}
	if chunk.Usage != nil {
		state.Usage = UsageFromChatUsage(chunk.Usage)
	}

	events := state.startEvents()
	if encryptedContent := strings.TrimSpace(chunk.ReasoningEncryptedContent); encryptedContent != "" {
		reasoning, reasoningEvents := state.ensureReasoningItem()
		events = append(events, reasoningEvents...)
		if reasoning.EncryptedContent != "" && reasoning.EncryptedContent != encryptedContent {
			return nil, fmt.Errorf("chat reasoning item contains multiple incompatible encrypted states")
		}
		reasoning.EncryptedContent = encryptedContent
		events = append(events, state.finishReasoningItem(reasoning, "completed")...)
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.GetReasoningContent() != "" {
			events = append(events, state.appendReasoningDelta(choice.Delta.GetReasoningContent())...)
		}
		if choice.Delta.GetContentString() != "" {
			events = append(events, state.finishActiveReasoningItem("completed")...)
			events = append(events, state.appendTextDelta(choice.Delta.GetContentString())...)
		}
		if choice.Delta.GetRefusalContent() != "" {
			events = append(events, state.finishActiveReasoningItem("completed")...)
			events = append(events, state.appendRefusalDelta(choice.Delta.GetRefusalContent())...)
		}
		toolCalls := choice.Delta.ParseToolCalls()
		if len(toolCalls) > 0 {
			events = append(events, state.finishActiveReasoningItem("completed")...)
		}
		for _, toolCall := range toolCalls {
			toolEvents, err := state.appendToolCallDelta(toolCall)
			if err != nil {
				return nil, err
			}
			events = append(events, toolEvents...)
		}
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			state.applyFinishReason(*choice.FinishReason)
			events = append(events, state.doneDeltaEvents()...)
		}
	}
	return state.numberEvents(events), nil
}

func FinalizeChatCompletionsStreamToResponses(state *ChatToResponsesStreamState) []ChatToResponsesStreamEvent {
	if state == nil || state.finalized {
		return nil
	}
	events := state.startEvents()
	events = append(events, state.doneDeltaEvents()...)
	state.finalized = true
	resp := state.finalResponse()
	eventType := responsesEventCompleted
	if state.status == "incomplete" {
		eventType = responsesEventIncomplete
	}
	events = append(events, responsesStreamEvent(eventType, dto.ResponsesStreamResponse{
		Type:     eventType,
		Response: resp,
	}))
	return state.numberEvents(events)
}

func FinalizeChatCompletionsStreamToResponsesChecked(state *ChatToResponsesStreamState) ([]ChatToResponsesStreamEvent, error) {
	if state == nil || state.finalized {
		return nil, nil
	}
	if !state.sawFinishReason {
		if !state.messageStarted && len(state.reasoningItems) == 0 && len(state.toolsByIndex) == 0 {
			return nil, errors.New("chat stream ended before producing output or a finish reason")
		}
		if len(state.toolsByIndex) > 0 {
			return nil, errors.New("chat stream ended before tool calls were terminated")
		}
		state.status = "incomplete"
		state.incompleteDetails = &dto.IncompleteDetails{Reason: responsesIncompleteReasonMaxTokens}
	}
	return FinalizeChatCompletionsStreamToResponses(state), nil
}

func (s *ChatToResponsesStreamState) UsageText() string {
	if s == nil {
		return ""
	}
	return s.text.String() + s.refusal.String()
}

func (s *ChatToResponsesStreamState) appendTextDelta(delta string) []ChatToResponsesStreamEvent {
	events := s.ensureMessage()
	if !s.textStarted {
		s.textStarted = true
		s.textContentIndex = s.nextContentIndex
		s.nextContentIndex++
		events = append(events, responsesStreamEvent(responsesEventContentPartAdded, dto.ResponsesStreamResponse{
			Type:         responsesEventContentPartAdded,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(s.textContentIndex),
			ItemID:       s.messageID(),
			Part: &dto.ResponsesOutputContent{
				Type:        "output_text",
				Text:        "",
				Annotations: []interface{}{},
			},
		}))
	}
	s.text.WriteString(delta)
	events = append(events, responsesStreamEvent(responsesEventOutputTextDelta, dto.ResponsesStreamResponse{
		Type:         responsesEventOutputTextDelta,
		OutputIndex:  intPtr(s.textOutputIndex),
		ContentIndex: intPtr(s.textContentIndex),
		Delta:        delta,
		ItemID:       s.messageID(),
	}))
	return events
}

func (s *ChatToResponsesStreamState) appendRefusalDelta(delta string) []ChatToResponsesStreamEvent {
	events := s.ensureMessage()
	if !s.refusalStarted {
		s.refusalStarted = true
		s.refusalContentIndex = s.nextContentIndex
		s.nextContentIndex++
		events = append(events, responsesStreamEvent(responsesEventContentPartAdded, dto.ResponsesStreamResponse{
			Type:         responsesEventContentPartAdded,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(s.refusalContentIndex),
			ItemID:       s.messageID(),
			Part: &dto.ResponsesOutputContent{
				Type:    "refusal",
				Refusal: "",
			},
		}))
	}
	s.refusal.WriteString(delta)
	events = append(events, responsesStreamEvent(responsesEventRefusalDelta, dto.ResponsesStreamResponse{
		Type:         responsesEventRefusalDelta,
		OutputIndex:  intPtr(s.textOutputIndex),
		ContentIndex: intPtr(s.refusalContentIndex),
		Delta:        delta,
		ItemID:       s.messageID(),
	}))
	return events
}

func (s *ChatToResponsesStreamState) ensureMessage() []ChatToResponsesStreamEvent {
	if s.messageStarted {
		return nil
	}
	s.messageStarted = true
	s.textOutputIndex = s.nextIndex("message", -1)
	addedMessage := &dto.ResponsesOutput{
		Type:    responsesOutputTypeMessage,
		ID:      s.messageID(),
		Status:  "in_progress",
		Role:    "assistant",
		Content: []dto.ResponsesOutputContent{},
	}
	if s.hasToolCalls {
		addedMessage.Phase = "commentary"
	}
	return []ChatToResponsesStreamEvent{responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: intPtr(s.textOutputIndex),
		Item:        addedMessage,
	})}
}

func (s *ChatToResponsesStreamState) appendReasoningDelta(delta string) []ChatToResponsesStreamEvent {
	reasoning, events := s.ensureReasoningItem()
	if !reasoning.PartStarted {
		reasoning.PartStarted = true
		events = append(events, responsesStreamEvent(responsesEventReasoningPartAdded, dto.ResponsesStreamResponse{
			Type:         responsesEventReasoningPartAdded,
			OutputIndex:  intPtr(reasoning.OutputIndex),
			SummaryIndex: intPtr(0),
			ItemID:       reasoning.ItemID,
			Part: &dto.ResponsesReasoningSummaryPart{
				Type: "summary_text",
				Text: "",
			},
		}))
	}
	reasoning.Text.WriteString(delta)
	events = append(events, responsesStreamEvent(responsesEventReasoningSummaryDelta, dto.ResponsesStreamResponse{
		Type:         responsesEventReasoningSummaryDelta,
		OutputIndex:  intPtr(reasoning.OutputIndex),
		SummaryIndex: intPtr(0),
		Delta:        delta,
		ItemID:       reasoning.ItemID,
	}))
	return events
}

func (s *ChatToResponsesStreamState) ensureReasoningItem() (*chatToResponsesStreamReasoning, []ChatToResponsesStreamEvent) {
	if s.activeReasoningIndex >= 0 && s.activeReasoningIndex < len(s.reasoningItems) {
		reasoning := s.reasoningItems[s.activeReasoningIndex]
		if reasoning != nil && !reasoning.Done {
			return reasoning, nil
		}
	}
	reasoningIndex := len(s.reasoningItems)
	reasoning := &chatToResponsesStreamReasoning{
		ItemID:      s.reasoningID(reasoningIndex),
		OutputIndex: s.nextIndex("reasoning", -1, reasoningIndex),
	}
	s.reasoningItems = append(s.reasoningItems, reasoning)
	s.activeReasoningIndex = reasoningIndex
	return reasoning, []ChatToResponsesStreamEvent{
		responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemAdded,
			OutputIndex: intPtr(reasoning.OutputIndex),
			Item: &dto.ResponsesOutput{
				Type:   responsesOutputTypeReasoning,
				ID:     reasoning.ItemID,
				Status: "in_progress",
			},
		}),
	}
}

func (s *ChatToResponsesStreamState) finishReasoningItem(reasoning *chatToResponsesStreamReasoning, status string) []ChatToResponsesStreamEvent {
	if reasoning == nil || reasoning.Done {
		return nil
	}
	reasoning.Done = true
	reasoning.Status = status
	if s.activeReasoningIndex >= 0 && s.activeReasoningIndex < len(s.reasoningItems) && s.reasoningItems[s.activeReasoningIndex] == reasoning {
		s.activeReasoningIndex = -1
	}

	events := make([]ChatToResponsesStreamEvent, 0, 3)
	if reasoning.PartStarted {
		events = append(events, responsesStreamEvent(responsesEventReasoningSummaryDone, dto.ResponsesStreamResponse{
			Type:         responsesEventReasoningSummaryDone,
			OutputIndex:  intPtr(reasoning.OutputIndex),
			SummaryIndex: intPtr(0),
			ItemID:       reasoning.ItemID,
			Text:         reasoning.Text.String(),
		}))
		events = append(events, responsesStreamEvent(responsesEventReasoningPartDone, dto.ResponsesStreamResponse{
			Type:         responsesEventReasoningPartDone,
			OutputIndex:  intPtr(reasoning.OutputIndex),
			SummaryIndex: intPtr(0),
			ItemID:       reasoning.ItemID,
			Part: &dto.ResponsesReasoningSummaryPart{
				Type: "summary_text",
				Text: reasoning.Text.String(),
			},
		}))
	}
	events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemDone,
		OutputIndex: intPtr(reasoning.OutputIndex),
		Item:        s.reasoningOutput(reasoning, status),
	}))
	return events
}

func (s *ChatToResponsesStreamState) finishActiveReasoningItem(status string) []ChatToResponsesStreamEvent {
	if s.activeReasoningIndex < 0 || s.activeReasoningIndex >= len(s.reasoningItems) {
		return nil
	}
	return s.finishReasoningItem(s.reasoningItems[s.activeReasoningIndex], status)
}

func (s *ChatToResponsesStreamState) appendToolCallDelta(toolCall dto.ToolCallResponse) ([]ChatToResponsesStreamEvent, error) {
	s.hasToolCalls = true
	chatIndex := 0
	if toolCall.Index != nil {
		chatIndex = *toolCall.Index
	}
	tool := s.toolsByIndex[chatIndex]
	events := make([]ChatToResponsesStreamEvent, 0, 2)
	if tool == nil {
		tool = &chatToResponsesStreamTool{
			ChatIndex:    chatIndex,
			OutputIndex:  -1,
			CallID:       strings.TrimSpace(toolCall.ID),
			SourceCallID: strings.TrimSpace(toolCall.ID),
			Name:         strings.TrimSpace(toolCall.Function.Name),
		}
		s.toolsByIndex[chatIndex] = tool
	}
	if callID := strings.TrimSpace(toolCall.ID); callID != "" {
		if tool.Added && tool.SourceCallID != "" && tool.SourceCallID != callID {
			return nil, fmt.Errorf("chat tool call %d changed id from %q to %q after streaming started", chatIndex, tool.SourceCallID, callID)
		}
		if !tool.Added {
			tool.SourceCallID = callID
			tool.CallID = callID
		}
	}
	if strings.TrimSpace(toolCall.Function.Name) != "" {
		tool.Name = strings.TrimSpace(toolCall.Function.Name)
		if identity, ok := s.ToolState.ResolveUpstream(tool.Name); ok {
			tool.Identity = identity
			tool.HasIdentity = true
		}
	}
	if toolCall.Function.Arguments != "" {
		tool.Arguments.WriteString(toolCall.Function.Arguments)
		if tool.Added && toolKind(tool) == sharedbridge.ToolKindFunction {
			events = append(events, responsesStreamEvent(responsesEventFunctionArgsDelta, dto.ResponsesStreamResponse{
				Type:        responsesEventFunctionArgsDelta,
				OutputIndex: intPtr(tool.OutputIndex),
				ItemID:      tool.ItemID,
				Delta:       toolCall.Function.Arguments,
			}))
			tool.ArgumentsSent = tool.Arguments.Len()
		}
	}
	events = append(events, s.flushReadyToolCalls()...)
	return events, nil
}

func (s *ChatToResponsesStreamState) flushReadyToolCalls() []ChatToResponsesStreamEvent {
	if s == nil {
		return nil
	}
	events := make([]ChatToResponsesStreamEvent, 0)
	for {
		tool := s.toolsByIndex[s.nextToolIndexToAdd]
		if tool == nil {
			break
		}
		if tool.Done || tool.Skipped {
			s.nextToolIndexToAdd++
			continue
		}
		if tool.Added {
			s.nextToolIndexToAdd++
			continue
		}
		if strings.TrimSpace(tool.Name) == "" || strings.TrimSpace(tool.CallID) == "" {
			break
		}
		events = append(events, s.startToolCall(tool)...)
		s.nextToolIndexToAdd++
	}
	return events
}

func (s *ChatToResponsesStreamState) startToolCall(tool *chatToResponsesStreamTool) []ChatToResponsesStreamEvent {
	if s == nil || tool == nil || tool.Added || tool.Skipped || strings.TrimSpace(tool.Name) == "" {
		return nil
	}
	tool.CallID = uniqueResponsesToolCallID(s.usedToolCallIDs, tool.CallID, s.ID, tool.ChatIndex)
	tool.OutputIndex = s.nextIndex("tool", tool.ChatIndex)
	tool.ItemID = responsesSyntheticItemID(toolItemIDPrefix(tool), s.ID, tool.ChatIndex)
	tool.Added = true
	events := []ChatToResponsesStreamEvent{responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: intPtr(tool.OutputIndex),
		Item:        s.toolOutput(tool, "in_progress"),
	})}
	if toolKind(tool) == sharedbridge.ToolKindFunction && tool.ArgumentsSent < tool.Arguments.Len() {
		delta := tool.Arguments.String()[tool.ArgumentsSent:]
		events = append(events, responsesStreamEvent(responsesEventFunctionArgsDelta, dto.ResponsesStreamResponse{
			Type:        responsesEventFunctionArgsDelta,
			OutputIndex: intPtr(tool.OutputIndex),
			ItemID:      tool.ItemID,
			Delta:       delta,
		}))
		tool.ArgumentsSent = tool.Arguments.Len()
	}
	return events
}

func (s *ChatToResponsesStreamState) doneDeltaEvents() []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0)
	status := s.outputStatus()
	if s.textStarted && !s.textDone {
		s.textDone = true
		events = append(events, responsesStreamEvent(responsesEventOutputTextDone, dto.ResponsesStreamResponse{
			Type:         responsesEventOutputTextDone,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(s.textContentIndex),
			ItemID:       s.messageID(),
			Text:         s.text.String(),
		}))
		events = append(events, responsesStreamEvent(responsesEventContentPartDone, dto.ResponsesStreamResponse{
			Type:         responsesEventContentPartDone,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(s.textContentIndex),
			ItemID:       s.messageID(),
			Part: &dto.ResponsesOutputContent{
				Type:        "output_text",
				Text:        s.text.String(),
				Annotations: []interface{}{},
			},
		}))
	}
	if s.refusalStarted && !s.refusalDone {
		s.refusalDone = true
		events = append(events, responsesStreamEvent(responsesEventRefusalDone, dto.ResponsesStreamResponse{
			Type:         responsesEventRefusalDone,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(s.refusalContentIndex),
			ItemID:       s.messageID(),
			Refusal:      s.refusal.String(),
		}))
		events = append(events, responsesStreamEvent(responsesEventContentPartDone, dto.ResponsesStreamResponse{
			Type:         responsesEventContentPartDone,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(s.refusalContentIndex),
			ItemID:       s.messageID(),
			Part: &dto.ResponsesOutputContent{
				Type:    "refusal",
				Refusal: s.refusal.String(),
			},
		}))
	}
	if s.messageStarted && !s.messageDone {
		s.messageDone = true
		events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(s.textOutputIndex),
			Item:        s.messageOutput(status),
		}))
	}
	for _, reasoning := range s.reasoningItems {
		events = append(events, s.finishReasoningItem(reasoning, status)...)
	}
	for _, tool := range s.sortedTools() {
		if tool.Done {
			continue
		}
		if strings.TrimSpace(tool.Name) == "" {
			tool.Done = true
			tool.Skipped = true
			continue
		}
		tool.Done = true
		if !tool.Added {
			events = append(events, s.startToolCall(tool)...)
		}
		switch toolKind(tool) {
		case sharedbridge.ToolKindCustom:
			input := sharedbridge.DecodeCustomInput(tool.Arguments.String())
			if input != "" {
				events = append(events, responsesStreamEvent("response.custom_tool_call_input.delta", dto.ResponsesStreamResponse{
					Type:        "response.custom_tool_call_input.delta",
					OutputIndex: intPtr(tool.OutputIndex),
					ItemID:      tool.ItemID,
					Delta:       input,
				}))
			}
			events = append(events, responsesStreamEvent("response.custom_tool_call_input.done", dto.ResponsesStreamResponse{
				Type:        "response.custom_tool_call_input.done",
				OutputIndex: intPtr(tool.OutputIndex),
				ItemID:      tool.ItemID,
				Input:       input,
			}))
		case sharedbridge.ToolKindFunction:
			if tool.ArgumentsSent < tool.Arguments.Len() {
				delta := tool.Arguments.String()[tool.ArgumentsSent:]
				events = append(events, responsesStreamEvent(responsesEventFunctionArgsDelta, dto.ResponsesStreamResponse{
					Type:        responsesEventFunctionArgsDelta,
					OutputIndex: intPtr(tool.OutputIndex),
					ItemID:      tool.ItemID,
					Delta:       delta,
				}))
				tool.ArgumentsSent = tool.Arguments.Len()
			}
			events = append(events, responsesStreamEvent(responsesEventFunctionArgsDone, dto.ResponsesStreamResponse{
				Type:        responsesEventFunctionArgsDone,
				OutputIndex: intPtr(tool.OutputIndex),
				ItemID:      tool.ItemID,
				Name:        toolOutputName(tool),
				Arguments:   tool.Arguments.String(),
			}))
		}
		events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(tool.OutputIndex),
			Item:        s.toolOutput(tool, status),
		}))
	}
	return events
}

func (s *ChatToResponsesStreamState) applyFinishReason(finishReason string) {
	if strings.TrimSpace(finishReason) != "" {
		s.sawFinishReason = true
	}
	if status, details := ResponsesStatusFromChatFinishReason(finishReason); status != "" {
		s.status = status
		s.incompleteDetails = details
	}
}

func (s *ChatToResponsesStreamState) finalResponse() *dto.OpenAIResponsesResponse {
	output := make([]dto.ResponsesOutput, 0, len(s.outputOrder))
	status := s.outputStatus()
	for _, ref := range s.outputOrder {
		switch ref.Kind {
		case "message":
			output = append(output, *s.messageOutput(status))
		case "reasoning":
			if ref.ReasoningIndex >= 0 && ref.ReasoningIndex < len(s.reasoningItems) && s.reasoningItems[ref.ReasoningIndex] != nil {
				reasoning := s.reasoningItems[ref.ReasoningIndex]
				reasoningStatus := status
				if reasoning.Done && reasoning.Status != "" {
					reasoningStatus = reasoning.Status
				}
				output = append(output, *s.reasoningOutput(reasoning, reasoningStatus))
			}
		case "tool":
			if tool := s.toolsByIndex[ref.ToolIndex]; tool != nil {
				output = append(output, *s.toolOutput(tool, status))
			}
		}
	}
	return &dto.OpenAIResponsesResponse{
		ID:                s.ID,
		Object:            "response",
		CreatedAt:         int(s.Created),
		Status:            []byte(fmt.Sprintf("%q", s.status)),
		IncompleteDetails: s.incompleteDetails,
		Model:             s.Model,
		Output:            output,
		Usage:             s.Usage,
	}
}

func (s *ChatToResponsesStreamState) createdResponse() *dto.OpenAIResponsesResponse {
	return &dto.OpenAIResponsesResponse{
		ID:        s.ID,
		Object:    "response",
		CreatedAt: int(s.Created),
		Status:    []byte(`"in_progress"`),
		Model:     s.Model,
		Output:    []dto.ResponsesOutput{},
	}
}

func (s *ChatToResponsesStreamState) nextIndex(kind string, toolIndex int, reasoningIndex ...int) int {
	index := s.nextOutputIndex
	s.nextOutputIndex++
	ref := chatToResponsesOutputRef{Kind: kind, ToolIndex: toolIndex, ReasoningIndex: -1}
	if len(reasoningIndex) > 0 {
		ref.ReasoningIndex = reasoningIndex[0]
	}
	s.outputOrder = append(s.outputOrder, ref)
	return index
}

func (s *ChatToResponsesStreamState) sortedTools() []*chatToResponsesStreamTool {
	indexes := make([]int, 0, len(s.toolsByIndex))
	for index := range s.toolsByIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	tools := make([]*chatToResponsesStreamTool, 0, len(indexes))
	for _, index := range indexes {
		tools = append(tools, s.toolsByIndex[index])
	}
	return tools
}

func (s *ChatToResponsesStreamState) outputStatus() string {
	if s.status == "incomplete" {
		return "incomplete"
	}
	return "completed"
}

func (s *ChatToResponsesStreamState) messageID() string {
	return responsesSyntheticItemID("msg", s.ID, 0)
}

func (s *ChatToResponsesStreamState) reasoningID(index int) string {
	return responsesSyntheticItemID("rs", s.ID, index)
}

func (s *ChatToResponsesStreamState) messageOutput(status string) *dto.ResponsesOutput {
	content := make([]dto.ResponsesOutputContent, s.nextContentIndex)
	if s.textContentIndex >= 0 {
		content[s.textContentIndex] = dto.ResponsesOutputContent{
			Type:        "output_text",
			Text:        s.text.String(),
			Annotations: []interface{}{},
		}
	}
	if s.refusalContentIndex >= 0 {
		content[s.refusalContentIndex] = dto.ResponsesOutputContent{
			Type:    "refusal",
			Refusal: s.refusal.String(),
		}
	}
	return &dto.ResponsesOutput{
		Type:    responsesOutputTypeMessage,
		ID:      s.messageID(),
		Status:  status,
		Role:    "assistant",
		Phase:   s.messagePhase(),
		Content: content,
	}
}

func (s *ChatToResponsesStreamState) messagePhase() string {
	if s.hasToolCalls {
		return "commentary"
	}
	return "final_answer"
}

func (s *ChatToResponsesStreamState) startEvents() []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0, 2)
	if !s.sentCreated {
		s.sentCreated = true
		events = append(events, responsesStreamEvent(responsesEventCreated, dto.ResponsesStreamResponse{
			Type:     responsesEventCreated,
			Response: s.createdResponse(),
		}))
	}
	if !s.sentInProgress {
		s.sentInProgress = true
		events = append(events, responsesStreamEvent(responsesEventInProgress, dto.ResponsesStreamResponse{
			Type:     responsesEventInProgress,
			Response: s.createdResponse(),
		}))
	}
	return events
}

func (s *ChatToResponsesStreamState) reasoningOutput(reasoning *chatToResponsesStreamReasoning, status string) *dto.ResponsesOutput {
	if reasoning == nil {
		return &dto.ResponsesOutput{Type: responsesOutputTypeReasoning, Status: status}
	}
	output := &dto.ResponsesOutput{
		Type:             responsesOutputTypeReasoning,
		ID:               reasoning.ItemID,
		Status:           status,
		EncryptedContent: reasoning.EncryptedContent,
	}
	if reasoning.PartStarted {
		output.Summary = []dto.ResponsesReasoningSummaryPart{
			{
				Type: "summary_text",
				Text: reasoning.Text.String(),
			},
		}
	}
	return output
}

func (s *ChatToResponsesStreamState) toolOutput(tool *chatToResponsesStreamTool, status string) *dto.ResponsesOutput {
	output := &dto.ResponsesOutput{
		Type:      responsesOutputTypeFunctionCall,
		ID:        tool.ItemID,
		Status:    status,
		CallId:    tool.CallID,
		Name:      toolOutputName(tool),
		Namespace: toolNamespace(tool),
		Arguments: chatArgumentsRawMessage(tool.Arguments.String()),
	}
	switch toolKind(tool) {
	case sharedbridge.ToolKindCustom:
		output.Type = "custom_tool_call"
		output.Input = sharedbridge.DecodeCustomInput(tool.Arguments.String())
		output.Arguments = nil
	case sharedbridge.ToolKindToolSearch:
		output.Type = "tool_search_call"
		output.Name = ""
		output.Namespace = ""
		output.Execution = "client"
		output.Arguments = sharedbridge.ToolSearchArgumentsRaw(tool.Arguments.String())
	}
	return output
}

func toolItemIDPrefix(tool *chatToResponsesStreamTool) string {
	switch toolKind(tool) {
	case sharedbridge.ToolKindCustom:
		return "ctc"
	case sharedbridge.ToolKindToolSearch:
		return "tsc"
	default:
		return "fc"
	}
}

func toolKind(tool *chatToResponsesStreamTool) sharedbridge.ToolKind {
	if tool != nil && tool.HasIdentity {
		return tool.Identity.Kind
	}
	return sharedbridge.ToolKindFunction
}

func toolOutputName(tool *chatToResponsesStreamTool) string {
	if tool != nil && tool.HasIdentity {
		return tool.Identity.Name
	}
	if tool == nil {
		return ""
	}
	return tool.Name
}

func toolNamespace(tool *chatToResponsesStreamTool) string {
	if tool != nil && tool.HasIdentity {
		return tool.Identity.Namespace
	}
	return ""
}

func (s *ChatToResponsesStreamState) numberEvents(events []ChatToResponsesStreamEvent) []ChatToResponsesStreamEvent {
	for i := range events {
		sequence := s.nextSequenceNumber
		s.nextSequenceNumber++
		events[i].Payload.SequenceNumber = &sequence
	}
	return events
}
