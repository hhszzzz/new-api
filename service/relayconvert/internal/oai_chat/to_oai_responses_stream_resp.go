package oaichat

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/dto"
	sharedbridge "github.com/QuantumNous/new-api/service/relayconvert/internal/shared/bridge"
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

	status             string
	incompleteDetails  *dto.IncompleteDetails
	sentCreated        bool
	sentInProgress     bool
	hasToolCalls       bool
	textOutputIndex    int
	textStarted        bool
	textDone           bool
	reasoningIndex     int
	reasoningStarted   bool
	reasoningDone      bool
	finalized          bool
	nextOutputIndex    int
	nextSequenceNumber int
	toolsByIndex       map[int]*chatToResponsesStreamTool
	outputOrder        []chatToResponsesOutputRef
	text               strings.Builder
	reasoning          strings.Builder
}

type chatToResponsesStreamTool struct {
	ChatIndex     int
	OutputIndex   int
	ID            string
	Name          string
	Identity      sharedbridge.ToolIdentity
	HasIdentity   bool
	Arguments     strings.Builder
	ArgumentsSent int
	Added         bool
	Done          bool
}

type chatToResponsesOutputRef struct {
	Kind      string
	ToolIndex int
}

func NewChatToResponsesStreamState(id string, model string) *ChatToResponsesStreamState {
	return &ChatToResponsesStreamState{
		ID:              id,
		Model:           model,
		Created:         time.Now().Unix(),
		Usage:           &dto.Usage{},
		status:          "completed",
		textOutputIndex: -1,
		reasoningIndex:  -1,
		toolsByIndex:    make(map[int]*chatToResponsesStreamTool),
	}
}

func ChatCompletionsStreamChunkToResponsesEvents(chunk *dto.ChatCompletionsStreamResponse, state *ChatToResponsesStreamState) ([]ChatToResponsesStreamEvent, error) {
	if chunk == nil || state == nil {
		return nil, nil
	}
	if state.ID == "" {
		state.ID = chunk.Id
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
	for _, choice := range chunk.Choices {
		if choice.Delta.GetReasoningContent() != "" {
			events = append(events, state.appendReasoningDelta(choice.Delta.GetReasoningContent())...)
		}
		if choice.Delta.GetContentString() != "" {
			events = append(events, state.appendTextDelta(choice.Delta.GetContentString())...)
		}
		for _, toolCall := range choice.Delta.ToolCalls {
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

func (s *ChatToResponsesStreamState) UsageText() string {
	if s == nil {
		return ""
	}
	return s.text.String()
}

func (s *ChatToResponsesStreamState) appendTextDelta(delta string) []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0, 2)
	if !s.textStarted {
		s.textStarted = true
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
		events = append(events, responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemAdded,
			OutputIndex: intPtr(s.textOutputIndex),
			Item:        addedMessage,
		}))
		events = append(events, responsesStreamEvent(responsesEventContentPartAdded, dto.ResponsesStreamResponse{
			Type:         responsesEventContentPartAdded,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(0),
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
		ContentIndex: intPtr(0),
		Delta:        delta,
		ItemID:       s.messageID(),
	}))
	return events
}

func (s *ChatToResponsesStreamState) appendReasoningDelta(delta string) []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0, 2)
	if !s.reasoningStarted {
		s.reasoningStarted = true
		s.reasoningIndex = s.nextIndex("reasoning", -1)
		events = append(events, responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemAdded,
			OutputIndex: intPtr(s.reasoningIndex),
			Item: &dto.ResponsesOutput{
				Type:   responsesOutputTypeReasoning,
				ID:     s.reasoningID(),
				Status: "in_progress",
			},
		}))
		events = append(events, responsesStreamEvent(responsesEventReasoningPartAdded, dto.ResponsesStreamResponse{
			Type:         responsesEventReasoningPartAdded,
			OutputIndex:  intPtr(s.reasoningIndex),
			SummaryIndex: intPtr(0),
			ItemID:       s.reasoningID(),
			Part: &dto.ResponsesReasoningSummaryPart{
				Type: "summary_text",
				Text: "",
			},
		}))
	}
	s.reasoning.WriteString(delta)
	events = append(events, responsesStreamEvent(responsesEventReasoningSummaryDelta, dto.ResponsesStreamResponse{
		Type:         responsesEventReasoningSummaryDelta,
		OutputIndex:  intPtr(s.reasoningIndex),
		SummaryIndex: intPtr(0),
		Delta:        delta,
		ItemID:       s.reasoningID(),
	}))
	return events
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
			ChatIndex:   chatIndex,
			OutputIndex: s.nextIndex("tool", chatIndex),
			ID:          strings.TrimSpace(toolCall.ID),
			Name:        strings.TrimSpace(toolCall.Function.Name),
		}
		if tool.ID == "" {
			tool.ID = fmt.Sprintf("%s_call_%d", s.ID, chatIndex)
		}
		s.toolsByIndex[chatIndex] = tool
	}
	if strings.TrimSpace(toolCall.ID) != "" {
		tool.ID = strings.TrimSpace(toolCall.ID)
	}
	if strings.TrimSpace(toolCall.Function.Name) != "" {
		tool.Name = strings.TrimSpace(toolCall.Function.Name)
		if identity, ok := s.ToolState.ResolveUpstream(tool.Name); ok {
			tool.Identity = identity
			tool.HasIdentity = true
		}
	}
	if !tool.Added && tool.Name != "" {
		tool.Added = true
		events = append(events, responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemAdded,
			OutputIndex: intPtr(tool.OutputIndex),
			Item:        s.toolOutput(tool, "in_progress"),
		}))
	}
	if toolCall.Function.Arguments != "" {
		tool.Arguments.WriteString(toolCall.Function.Arguments)
		if tool.Added && toolKind(tool) == sharedbridge.ToolKindFunction {
			events = append(events, responsesStreamEvent(responsesEventFunctionArgsDelta, dto.ResponsesStreamResponse{
				Type:        responsesEventFunctionArgsDelta,
				OutputIndex: intPtr(tool.OutputIndex),
				ItemID:      tool.ID,
				Delta:       toolCall.Function.Arguments,
			}))
			tool.ArgumentsSent = tool.Arguments.Len()
		}
	}
	return events, nil
}

func (s *ChatToResponsesStreamState) doneDeltaEvents() []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0)
	status := s.outputStatus()
	if s.textStarted && !s.textDone {
		s.textDone = true
		events = append(events, responsesStreamEvent(responsesEventOutputTextDone, dto.ResponsesStreamResponse{
			Type:         responsesEventOutputTextDone,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(0),
			ItemID:       s.messageID(),
			Text:         s.text.String(),
		}))
		events = append(events, responsesStreamEvent(responsesEventContentPartDone, dto.ResponsesStreamResponse{
			Type:         responsesEventContentPartDone,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(0),
			ItemID:       s.messageID(),
			Part: &dto.ResponsesOutputContent{
				Type:        "output_text",
				Text:        s.text.String(),
				Annotations: []interface{}{},
			},
		}))
		events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(s.textOutputIndex),
			Item:        s.messageOutput(status),
		}))
	}
	if s.reasoningStarted && !s.reasoningDone {
		s.reasoningDone = true
		events = append(events, responsesStreamEvent(responsesEventReasoningSummaryDone, dto.ResponsesStreamResponse{
			Type:         responsesEventReasoningSummaryDone,
			OutputIndex:  intPtr(s.reasoningIndex),
			SummaryIndex: intPtr(0),
			ItemID:       s.reasoningID(),
			Text:         s.reasoning.String(),
		}))
		events = append(events, responsesStreamEvent(responsesEventReasoningPartDone, dto.ResponsesStreamResponse{
			Type:         responsesEventReasoningPartDone,
			OutputIndex:  intPtr(s.reasoningIndex),
			SummaryIndex: intPtr(0),
			ItemID:       s.reasoningID(),
			Part: &dto.ResponsesReasoningSummaryPart{
				Type: "summary_text",
				Text: s.reasoning.String(),
			},
		}))
		events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(s.reasoningIndex),
			Item:        s.reasoningOutput(status),
		}))
	}
	for _, tool := range s.sortedTools() {
		if tool.Done {
			continue
		}
		tool.Done = true
		if !tool.Added {
			tool.Added = true
			events = append(events, responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
				Type:        responsesEventOutputItemAdded,
				OutputIndex: intPtr(tool.OutputIndex),
				Item:        s.toolOutput(tool, "in_progress"),
			}))
		}
		switch toolKind(tool) {
		case sharedbridge.ToolKindCustom:
			input := sharedbridge.DecodeCustomInput(tool.Arguments.String())
			if input != "" {
				events = append(events, responsesStreamEvent("response.custom_tool_call_input.delta", dto.ResponsesStreamResponse{
					Type:        "response.custom_tool_call_input.delta",
					OutputIndex: intPtr(tool.OutputIndex),
					ItemID:      tool.ID,
					Delta:       input,
				}))
			}
			events = append(events, responsesStreamEvent("response.custom_tool_call_input.done", dto.ResponsesStreamResponse{
				Type:        "response.custom_tool_call_input.done",
				OutputIndex: intPtr(tool.OutputIndex),
				ItemID:      tool.ID,
				Input:       input,
			}))
		case sharedbridge.ToolKindFunction:
			if tool.ArgumentsSent < tool.Arguments.Len() {
				delta := tool.Arguments.String()[tool.ArgumentsSent:]
				events = append(events, responsesStreamEvent(responsesEventFunctionArgsDelta, dto.ResponsesStreamResponse{
					Type:        responsesEventFunctionArgsDelta,
					OutputIndex: intPtr(tool.OutputIndex),
					ItemID:      tool.ID,
					Delta:       delta,
				}))
				tool.ArgumentsSent = tool.Arguments.Len()
			}
			events = append(events, responsesStreamEvent(responsesEventFunctionArgsDone, dto.ResponsesStreamResponse{
				Type:        responsesEventFunctionArgsDone,
				OutputIndex: intPtr(tool.OutputIndex),
				ItemID:      tool.ID,
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
			output = append(output, *s.reasoningOutput(status))
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

func (s *ChatToResponsesStreamState) nextIndex(kind string, toolIndex int) int {
	index := s.nextOutputIndex
	s.nextOutputIndex++
	s.outputOrder = append(s.outputOrder, chatToResponsesOutputRef{Kind: kind, ToolIndex: toolIndex})
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
	return fmt.Sprintf("%s_msg_0", s.ID)
}

func (s *ChatToResponsesStreamState) reasoningID() string {
	return fmt.Sprintf("%s_reasoning_0", s.ID)
}

func (s *ChatToResponsesStreamState) messageOutput(status string) *dto.ResponsesOutput {
	return &dto.ResponsesOutput{
		Type:   responsesOutputTypeMessage,
		ID:     s.messageID(),
		Status: status,
		Role:   "assistant",
		Phase:  s.messagePhase(),
		Content: []dto.ResponsesOutputContent{
			{
				Type:        "output_text",
				Text:        s.text.String(),
				Annotations: []interface{}{},
			},
		},
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

func (s *ChatToResponsesStreamState) reasoningOutput(status string) *dto.ResponsesOutput {
	return &dto.ResponsesOutput{
		Type:   responsesOutputTypeReasoning,
		ID:     s.reasoningID(),
		Status: status,
		Summary: []dto.ResponsesReasoningSummaryPart{
			{
				Type: "summary_text",
				Text: s.reasoning.String(),
			},
		},
	}
}

func (s *ChatToResponsesStreamState) toolOutput(tool *chatToResponsesStreamTool, status string) *dto.ResponsesOutput {
	output := &dto.ResponsesOutput{
		Type:      responsesOutputTypeFunctionCall,
		ID:        tool.ID,
		Status:    status,
		CallId:    tool.ID,
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
