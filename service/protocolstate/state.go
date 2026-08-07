package protocolstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const (
	stateCacheNamespace   cachex.Namespace = "protocol_bridge:responses:v1"
	ownerCacheNamespace   cachex.Namespace = "protocol_bridge:responses_owner:v1"
	messageCacheNamespace cachex.Namespace = "protocol_bridge:messages:v1"
	stateCacheCapacity                     = 4096
	stateVersion                           = 1
)

type ResponseNode struct {
	Version              int             `json:"version"`
	UserID               int             `json:"user_id"`
	TokenID              int             `json:"token_id"`
	PublicResponseID     string          `json:"public_response_id"`
	ParentResponseID     string          `json:"parent_response_id,omitempty"`
	ChannelID            int             `json:"channel_id"`
	RequestProtocol      string          `json:"request_protocol"`
	UpstreamProtocol     string          `json:"upstream_protocol"`
	BridgeManaged        bool            `json:"bridge_managed,omitempty"`
	UpstreamResponseID   string          `json:"upstream_response_id,omitempty"`
	UpstreamStored       bool            `json:"upstream_stored"`
	PublicModel          string          `json:"public_model"`
	UpstreamModel        string          `json:"upstream_model,omitempty"`
	NormalizedInput      json.RawMessage `json:"normalized_input"`
	NormalizedOutput     json.RawMessage `json:"normalized_output"`
	NormalizedTools      json.RawMessage `json:"normalized_tools,omitempty"`
	Turn                 int             `json:"turn"`
	CumulativeStateBytes int             `json:"cumulative_state_bytes"`
	CreatedAt            int64           `json:"created_at"`
}

type SelectionBinding struct {
	ChannelID        int
	UpstreamProtocol channelcompat.Protocol
}

type pendingKind string

const (
	pendingResponses pendingKind = "responses"
	pendingMessages  pendingKind = "messages"
)

type pendingState struct {
	kind                     pendingKind
	stream                   bool
	publicID                 string
	publicModel              string
	upstreamModel            string
	channelID                int
	requestProtocol          string
	upstreamProtocol         string
	bridgeManaged            bool
	parent                   *ResponseNode
	parentResponseID         string
	originalInput            json.RawMessage
	normalizedTools          json.RawMessage
	continuationID           string
	upstreamResponseID       string
	upstreamStored           bool
	normalizedOutput         json.RawMessage
	streamOutput             []capturedResponsesOutput
	completed                bool
	terminal                 bool
	usedContinuation         bool
	continuationAcknowledged bool

	messageSelection *messageSelection
	assistantMessage json.RawMessage
	claudeBlocks     map[int]*dto.ClaudeMediaMessage
	upstreamOutput   []json.RawMessage
}

type capturedResponsesOutput struct {
	outputIndex    int
	hasOutputIndex bool
	item           json.RawMessage
}

type responseNodeCodec struct{}

func (responseNodeCodec) Encode(value ResponseNode) (string, error) {
	data, err := common.Marshal(value)
	return string(data), err
}

func (responseNodeCodec) Decode(value string) (ResponseNode, error) {
	var node ResponseNode
	err := common.UnmarshalJsonStr(value, &node)
	return node, err
}

var (
	cacheMu            sync.Mutex
	responseStateCache *cachex.HybridCache[ResponseNode]
	responseOwnerCache *cachex.HybridCache[string]
	messageStateCache  *cachex.HybridCache[MessageSession]
	warnNoRedisOnce    sync.Once
)

func Enabled() bool {
	return model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled
}

func Active(c *gin.Context) bool {
	return getPending(c, "") != nil
}

func ResolveSelectionBinding(c *gin.Context, requestPath, publicModel string, body []byte) (*SelectionBinding, error) {
	if c == nil {
		return nil, nil
	}
	path := strings.Split(strings.TrimSpace(requestPath), "?")[0]
	switch path {
	case "/v1/responses":
		var request struct {
			PreviousResponseID string `json:"previous_response_id"`
		}
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, err
		}
		previousID := strings.TrimSpace(request.PreviousResponseID)
		if previousID == "" {
			return nil, nil
		}
		node, managed, err := findResponseNode(c, previousID, publicModel)
		if err != nil {
			return nil, err
		}
		if !managed {
			if Enabled() {
				return nil, fmt.Errorf("previous_response_id is unknown or expired")
			}
			return nil, nil
		}
		common.SetContextKey(c, constant.ContextKeyProtocolStateParent, node)
		features, err := channelcompat.ExtractRequestFeatureSet(channelcompat.ProtocolResponses, body)
		if err != nil {
			return nil, err
		}
		chain, err := loadResponseHistoryChain(c, node, responseNodeUsesBridgeState(node))
		if err != nil {
			return nil, err
		}
		for _, historicalNode := range chain {
			for _, storedRequest := range []map[string]json.RawMessage{
				{"input": historicalNode.NormalizedInput, "tools": historicalNode.NormalizedTools},
				{"input": historicalNode.NormalizedOutput},
			} {
				encoded, marshalErr := common.Marshal(storedRequest)
				if marshalErr != nil {
					return nil, marshalErr
				}
				historicalFeatures, extractErr := channelcompat.ExtractRequestFeatureSet(channelcompat.ProtocolResponses, encoded)
				if extractErr != nil {
					return nil, fmt.Errorf("invalid stored Responses feature state: %w", extractErr)
				}
				features = channelcompat.MergeRequestFeatureSets(features, historicalFeatures)
			}
		}
		common.SetContextKey(c, constant.ContextKeyRequestFeatureSet, features)
		return &SelectionBinding{
			ChannelID:        node.ChannelID,
			UpstreamProtocol: channelcompat.Protocol(node.UpstreamProtocol),
		}, nil
	case "/v1/messages":
		return resolveMessageSelectionBinding(c, publicModel, body)
	default:
		return nil, nil
	}
}

func PrepareResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, plan channelcompat.ProtocolPlan, request *dto.OpenAIResponsesRequest) error {
	if c == nil || info == nil || request == nil || plan.RequestProtocol != channelcompat.ProtocolResponses {
		return nil
	}
	common.SetContextKey(c, constant.ContextKeyProtocolRequestNormalized, false)
	_, hasManagedParent := common.GetContextKeyType[*ResponseNode](c, constant.ContextKeyProtocolStateParent)
	manageState := Enabled() || plan.StateEnabled || hasManagedParent
	if !manageState {
		normalized, err := normalizeResponsesRequest(request, plan.UpstreamProtocol, nil)
		if err != nil {
			return err
		}
		common.SetContextKey(c, constant.ContextKeyProtocolRequestNormalized, normalized)
		common.SetContextKey(c, constant.ContextKeyProtocolStatePending, nil)
		common.SetContextKey(c, constant.ContextKeyProtocolStatePublicID, "")
		return nil
	}

	publicID := ensurePublicResponseID(c)
	originalInput := append(json.RawMessage(nil), request.Input...)
	policy := currentPolicy()
	previousID := strings.TrimSpace(request.PreviousResponseID)
	var parent *ResponseNode
	if previousID != "" {
		if cached, ok := common.GetContextKeyType[*ResponseNode](c, constant.ContextKeyProtocolStateParent); ok && cached != nil {
			parent = cloneResponseNode(cached)
		} else {
			loaded, err := loadResponseNode(c, previousID, info.OriginModelName)
			if err != nil {
				return err
			}
			parent = loaded
			common.SetContextKey(c, constant.ContextKeyProtocolStateParent, parent)
		}
	}
	bridgeManaged := plan.StateEnabled ||
		(plan.UpstreamProtocol != "" && plan.RequestProtocol != plan.UpstreamProtocol) ||
		responseNodeUsesBridgeState(parent)
	if bridgeManaged && len(originalInput) > policy.MaxStateBytes {
		return fmt.Errorf("Responses input exceeds the maximum serialized state size of %d bytes", policy.MaxStateBytes)
	}

	pending := &pendingState{
		kind:             pendingResponses,
		stream:           info.IsStream,
		publicID:         publicID,
		publicModel:      info.OriginModelName,
		upstreamModel:    strings.TrimSpace(info.UpstreamModelName),
		channelID:        info.ChannelId,
		requestProtocol:  string(plan.RequestProtocol),
		upstreamProtocol: string(plan.UpstreamProtocol),
		bridgeManaged:    bridgeManaged,
		parent:           parent,
		parentResponseID: previousID,
		originalInput:    originalInput,
	}
	historicalTools := make([]json.RawMessage, 0)

	if parent != nil {
		if bridgeManaged && parent.Turn >= policy.MaxStateTurns {
			return fmt.Errorf("previous_response_id exceeds the maximum conversation length of %d turns", policy.MaxStateTurns)
		}
		if bridgeManaged && parent.CumulativeStateBytes+len(originalInput) > policy.MaxStateBytes {
			return fmt.Errorf("previous_response_id state exceeds the maximum serialized size of %d bytes", policy.MaxStateBytes)
		}

		forceReplay := common.GetContextKeyBool(c, constant.ContextKeyProtocolStateForceReplay)
		canContinueNatively := !forceReplay &&
			plan.UpstreamProtocol == channelcompat.ProtocolResponses &&
			info.ChannelId == parent.ChannelID &&
			parent.UpstreamProtocol == string(channelcompat.ProtocolResponses) &&
			strings.TrimSpace(info.UpstreamModelName) == strings.TrimSpace(parent.UpstreamModel) &&
			parent.UpstreamStored &&
			strings.TrimSpace(parent.UpstreamResponseID) != ""
		if canContinueNatively {
			chain, err := loadResponseHistoryChain(c, parent, bridgeManaged)
			if err != nil {
				return err
			}
			historicalTools = collectResponsesHistoryTools(chain)
			request.PreviousResponseID = parent.UpstreamResponseID
			pending.continuationID = parent.UpstreamResponseID
			pending.usedContinuation = true
		} else {
			replayedInput, replayedTools, err := replayResponsesHistory(c, parent, originalInput, info.ChannelId, plan.UpstreamProtocol, info.UpstreamModelName, bridgeManaged)
			if err != nil {
				return err
			}
			request.Input = replayedInput
			historicalTools = replayedTools
			request.PreviousResponseID = ""
		}
	}
	normalized, err := normalizeResponsesRequest(request, plan.UpstreamProtocol, historicalTools)
	if err != nil {
		return err
	}
	common.SetContextKey(c, constant.ContextKeyProtocolRequestNormalized, normalized)
	pending.normalizedTools = append(json.RawMessage(nil), request.Tools...)
	if bridgeManaged && len(pending.originalInput)+len(pending.normalizedTools) > policy.MaxStateBytes {
		return fmt.Errorf("Responses input and tools exceed the maximum serialized state size of %d bytes", policy.MaxStateBytes)
	}

	common.SetContextKey(c, constant.ContextKeyProtocolStatePending, pending)
	return nil
}

func PublicResponseID(c *gin.Context, fallback string) string {
	if c == nil {
		return fallback
	}
	if value := common.GetContextKeyString(c, constant.ContextKeyProtocolStatePublicID); value != "" {
		return value
	}
	return fallback
}

func ResponsesRequestNormalized(c *gin.Context) bool {
	return c != nil && common.GetContextKeyBool(c, constant.ContextKeyProtocolRequestNormalized)
}

func SetUpstreamResponseID(c *gin.Context, upstreamResponseID string) {
	pending := getPending(c, "")
	upstreamResponseID = strings.TrimSpace(upstreamResponseID)
	if pending == nil || upstreamResponseID == "" {
		return
	}
	pending.upstreamResponseID = upstreamResponseID
}

// ValidateResponsesContinuation verifies that an upstream Responses server
// acknowledged the continuation ID before any response bytes are sent to the
// client. Some compatible servers accept previous_response_id but silently
// ignore it; treating that as unsupported lets the retry path replay the
// gateway-owned history instead of returning a context-free answer.
func ValidateResponsesContinuation(c *gin.Context, previousResponseID json.RawMessage) error {
	pending := getPending(c, "")
	if pending == nil || !pending.usedContinuation || pending.continuationAcknowledged {
		return nil
	}
	expected := strings.TrimSpace(pending.continuationID)
	actual := strings.TrimSpace(common.JsonRawMessageToString(previousResponseID))
	if expected != "" && actual == expected {
		pending.continuationAcknowledged = true
		return nil
	}
	return fmt.Errorf("previous_response_id is unsupported because the upstream response did not acknowledge the continuation")
}

func CaptureResponsesResponse(c *gin.Context, upstreamResponseID string, response *dto.OpenAIResponsesResponse) string {
	pending := getPending(c, pendingResponses)
	if pending == nil {
		if response == nil {
			return ""
		}
		return response.ID
	}
	if response == nil {
		return pending.publicID
	}
	return captureResponsesResponse(pending, upstreamResponseID, response, nil)
}

// CaptureResponsesResponseData rewrites only gateway-owned response fields while
// retaining provider fields that are not modeled by the relay DTO.
func CaptureResponsesResponseData(c *gin.Context, upstreamResponseID string, response *dto.OpenAIResponsesResponse, data []byte) ([]byte, error) {
	pending := getPending(c, pendingResponses)
	if pending == nil || response == nil {
		return data, nil
	}

	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	captureResponsesResponse(pending, upstreamResponseID, response, fields["output"])

	publicID, err := common.Marshal(response.ID)
	if err != nil {
		return nil, err
	}
	publicModel, err := common.Marshal(response.Model)
	if err != nil {
		return nil, err
	}
	fields["id"] = publicID
	fields["model"] = publicModel
	fields["previous_response_id"] = append(json.RawMessage(nil), response.PreviousResponseID...)
	return common.Marshal(fields)
}

func captureResponsesResponse(pending *pendingState, upstreamResponseID string, response *dto.OpenAIResponsesResponse, rawOutput json.RawMessage) string {
	if strings.TrimSpace(upstreamResponseID) != "" {
		pending.upstreamResponseID = strings.TrimSpace(upstreamResponseID)
	} else if pending.upstreamResponseID == "" {
		pending.upstreamResponseID = strings.TrimSpace(response.ID)
	}
	pending.upstreamStored = response.Store
	if common.GetJsonType(rawOutput) == "array" {
		pending.normalizedOutput = append(json.RawMessage(nil), rawOutput...)
	} else if len(response.Output) > 0 {
		if output, err := common.Marshal(response.Output); err == nil {
			pending.normalizedOutput = output
		}
	}
	status := common.JsonRawMessageToString(response.Status)
	pending.completed = status == "completed"
	pending.terminal = isResponsesTerminalStatus(status)
	response.ID = pending.publicID
	response.Model = pending.publicModel
	response.PreviousResponseID = publicPreviousResponseID(pending)
	return pending.publicID
}

func publicPreviousResponseID(pending *pendingState) json.RawMessage {
	if pending == nil || strings.TrimSpace(pending.parentResponseID) == "" {
		return json.RawMessage(`null`)
	}
	encoded, err := common.Marshal(strings.TrimSpace(pending.parentResponseID))
	if err != nil {
		return json.RawMessage(`null`)
	}
	return encoded
}

func captureResponsesStreamOutput(pending *pendingState, outputIndex *int, item json.RawMessage) {
	if pending == nil || common.GetJsonType(item) != "object" {
		return
	}
	captured := capturedResponsesOutput{item: append(json.RawMessage(nil), item...)}
	if outputIndex != nil && *outputIndex >= 0 {
		captured.outputIndex = *outputIndex
		captured.hasOutputIndex = true
	}
	itemID, _ := responsesOutputIdentity(item)
	for index := range pending.streamOutput {
		existing := pending.streamOutput[index]
		if captured.hasOutputIndex && existing.hasOutputIndex && captured.outputIndex == existing.outputIndex {
			pending.streamOutput[index] = captured
			return
		}
		if itemID == "" {
			continue
		}
		existingID, _ := responsesOutputIdentity(existing.item)
		if existingID == itemID {
			pending.streamOutput[index] = captured
			return
		}
	}
	pending.streamOutput = append(pending.streamOutput, captured)
}

func mergeResponsesStreamOutput(pending *pendingState, terminalOutput json.RawMessage) (json.RawMessage, error) {
	if pending == nil || len(pending.streamOutput) == 0 {
		if common.GetJsonType(terminalOutput) == "array" {
			return append(json.RawMessage(nil), terminalOutput...), nil
		}
		return json.RawMessage(`[]`), nil
	}

	streamItems := append([]capturedResponsesOutput(nil), pending.streamOutput...)
	sort.SliceStable(streamItems, func(i, j int) bool {
		left, right := streamItems[i], streamItems[j]
		if left.hasOutputIndex != right.hasOutputIndex {
			return left.hasOutputIndex
		}
		return left.hasOutputIndex && left.outputIndex < right.outputIndex
	})

	terminalItems := make([]json.RawMessage, 0)
	if common.GetJsonType(terminalOutput) == "array" {
		if err := common.Unmarshal(terminalOutput, &terminalItems); err != nil {
			return nil, fmt.Errorf("decode terminal Responses output: %w", err)
		}
	}
	terminalUsed := make([]bool, len(terminalItems))
	merged := make([]json.RawMessage, 0, len(streamItems)+len(terminalItems))
	for _, streamItem := range streamItems {
		match := -1
		streamID, streamType := responsesOutputIdentity(streamItem.item)
		if streamID != "" {
			for index, terminalItem := range terminalItems {
				if terminalUsed[index] {
					continue
				}
				terminalID, _ := responsesOutputIdentity(terminalItem)
				if terminalID == streamID {
					match = index
					break
				}
			}
		}
		if match < 0 && streamItem.hasOutputIndex && streamItem.outputIndex < len(terminalItems) && !terminalUsed[streamItem.outputIndex] {
			terminalID, terminalType := responsesOutputIdentity(terminalItems[streamItem.outputIndex])
			if (streamID == "" || terminalID == "") && streamType != "" && streamType == terminalType {
				match = streamItem.outputIndex
			}
		}
		if match < 0 {
			merged = append(merged, append(json.RawMessage(nil), streamItem.item...))
			continue
		}
		terminalUsed[match] = true
		item, err := mergeResponsesOutputFields(streamItem.item, terminalItems[match])
		if err != nil {
			return nil, err
		}
		merged = append(merged, item)
	}
	for index, terminalItem := range terminalItems {
		if !terminalUsed[index] {
			merged = append(merged, append(json.RawMessage(nil), terminalItem...))
		}
	}
	return common.Marshal(merged)
}

func responsesOutputIdentity(item json.RawMessage) (string, string) {
	var fields map[string]json.RawMessage
	if common.Unmarshal(item, &fields) != nil {
		return "", ""
	}
	return strings.TrimSpace(common.JsonRawMessageToString(fields["id"])), strings.TrimSpace(common.JsonRawMessageToString(fields["type"]))
}

func mergeResponsesOutputFields(streamItem, terminalItem json.RawMessage) (json.RawMessage, error) {
	var streamFields map[string]json.RawMessage
	if err := common.Unmarshal(streamItem, &streamFields); err != nil {
		return nil, fmt.Errorf("decode streamed Responses output item: %w", err)
	}
	var terminalFields map[string]json.RawMessage
	if err := common.Unmarshal(terminalItem, &terminalFields); err != nil {
		return nil, fmt.Errorf("decode terminal Responses output item: %w", err)
	}
	for key, value := range terminalFields {
		streamFields[key] = value
	}
	return common.Marshal(streamFields)
}

func ObserveResponsesStream(c *gin.Context, event *dto.ResponsesStreamResponse) {
	pending := getPending(c, "")
	if pending == nil || event == nil {
		return
	}
	if event.Type == dto.ResponsesOutputTypeItemDone && event.Item != nil {
		if item, err := common.Marshal(event.Item); err == nil {
			captureResponsesStreamOutput(pending, event.OutputIndex, item)
		}
	}
	if event.Response == nil {
		return
	}
	upstreamID := event.Response.ID
	if pending.kind == pendingResponses {
		var rawOutput json.RawMessage
		if len(pending.streamOutput) > 0 {
			if len(event.Response.Output) > 0 {
				rawOutput, _ = common.Marshal(event.Response.Output)
			}
			if merged, err := mergeResponsesStreamOutput(pending, rawOutput); err == nil {
				rawOutput = merged
				_ = common.Unmarshal(rawOutput, &event.Response.Output)
			}
		}
		captureResponsesResponse(pending, upstreamID, event.Response, rawOutput)
		updateResponsesStreamCompletion(pending, event.Type)
		return
	}
	observeMessagesUpstreamResponse(pending, event, upstreamID, nil)
}

// ObserveResponsesStreamData captures the original output items for replay and
// rewrites nested public identifiers without dropping unknown SSE fields.
func ObserveResponsesStreamData(c *gin.Context, event *dto.ResponsesStreamResponse, data []byte) ([]byte, error) {
	pending := getPending(c, "")
	if pending == nil || event == nil {
		return data, nil
	}

	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if event.Type == dto.ResponsesOutputTypeItemDone {
		if item := fields["item"]; common.GetJsonType(item) == "object" {
			captureResponsesStreamOutput(pending, event.OutputIndex, item)
		} else if event.Item != nil {
			encoded, err := common.Marshal(event.Item)
			if err != nil {
				return nil, err
			}
			captureResponsesStreamOutput(pending, event.OutputIndex, encoded)
		}
	}
	if event.Response == nil {
		return data, nil
	}

	upstreamID := event.Response.ID
	responseFields := make(map[string]json.RawMessage)
	if rawResponse := fields["response"]; common.GetJsonType(rawResponse) == "object" {
		if err := common.Unmarshal(rawResponse, &responseFields); err != nil {
			return nil, err
		}
	}
	if pending.kind != pendingResponses {
		observeMessagesUpstreamResponse(pending, event, upstreamID, responseFields["output"])
		return data, nil
	}

	rawOutput := responseFields["output"]
	if len(pending.streamOutput) > 0 {
		var err error
		rawOutput, err = mergeResponsesStreamOutput(pending, rawOutput)
		if err != nil {
			return nil, err
		}
		responseFields["output"] = rawOutput
		_ = common.Unmarshal(rawOutput, &event.Response.Output)
	}
	captureResponsesResponse(pending, upstreamID, event.Response, rawOutput)
	updateResponsesStreamCompletion(pending, event.Type)

	publicID, err := common.Marshal(event.Response.ID)
	if err != nil {
		return nil, err
	}
	publicModel, err := common.Marshal(event.Response.Model)
	if err != nil {
		return nil, err
	}
	responseFields["id"] = publicID
	responseFields["model"] = publicModel
	responseFields["previous_response_id"] = append(json.RawMessage(nil), event.Response.PreviousResponseID...)
	encodedResponse, err := common.Marshal(responseFields)
	if err != nil {
		return nil, err
	}
	fields["response"] = encodedResponse
	return common.Marshal(fields)
}

func updateResponsesStreamCompletion(pending *pendingState, eventType string) {
	if pending == nil || pending.kind != pendingResponses {
		return
	}
	switch eventType {
	case "response.completed", "response.done":
		pending.completed = true
		pending.terminal = true
	case "response.incomplete", "response.cancelled", "response.canceled", "response.failed", "response.error", "error":
		pending.completed = false
		pending.terminal = true
	}
}

func isResponsesTerminalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "incomplete", "cancelled", "canceled", "failed":
		return true
	default:
		return false
	}
}

func EnableReplayFallback(c *gin.Context, apiError *types.NewAPIError) bool {
	pending := getPending(c, "")
	if pending == nil || !pending.usedContinuation || common.GetContextKeyBool(c, constant.ContextKeyProtocolStateForceReplay) {
		return false
	}
	if c.Writer != nil && c.Writer.Written() {
		return false
	}
	if !isMissingContinuationError(apiError) {
		return false
	}
	common.SetContextKey(c, constant.ContextKeyProtocolStateForceReplay, true)
	return true
}

// ResetAttempt clears only state produced by the previous upstream attempt.
// Conversation ownership, parent/session bindings, the public response ID, and
// a requested replay fallback remain request-scoped and must survive retries.
func ResetAttempt(c *gin.Context) {
	if c == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyProtocolStatePending, nil)
	common.SetContextKey(c, constant.ContextKeyProtocolRequestNormalized, false)
	common.SetContextKey(c, constant.ContextKeyProtocolStreamCompleted, false)
}

// ResetLogicalRequest clears state that belongs to one client request while
// preserving the selected transport plan for a reused connection.
func ResetLogicalRequest(c *gin.Context) {
	if c == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyProtocolStateParent, nil)
	common.SetContextKey(c, constant.ContextKeyProtocolStateSession, nil)
	common.SetContextKey(c, constant.ContextKeyProtocolStateBinding, nil)
	common.SetContextKey(c, constant.ContextKeyProtocolStatePending, nil)
	common.SetContextKey(c, constant.ContextKeyProtocolStatePublicID, "")
	common.SetContextKey(c, constant.ContextKeyProtocolStateForceReplay, false)
	common.SetContextKey(c, constant.ContextKeyProtocolRequestNormalized, false)
	common.SetContextKey(c, constant.ContextKeyProtocolResponseStreamState, nil)
	common.SetContextKey(c, constant.ContextKeyProtocolStreamCompleted, false)
	common.SetContextKey(c, constant.ContextKeyProtocolAutoAttempt, nil)
	common.SetContextKey(c, constant.ContextKeyProtocolIncompatibleReason, nil)
	common.SetContextKey(c, constant.ContextKeyRequestFeatureSet, nil)
}

func MarkStreamCompleted(c *gin.Context) {
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyProtocolStreamCompleted, true)
	}
}

func AttemptCompleted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	plan, hasPlan := common.GetContextKeyType[channelcompat.ProtocolPlan](c, constant.ContextKeyProtocolPlan)
	if !hasPlan {
		return true
	}
	if pending := getPending(c, ""); pending != nil && !pending.terminal {
		return false
	}
	return !plan.Features.Stream || common.GetContextKeyBool(c, constant.ContextKeyProtocolStreamCompleted)
}

func Commit(c *gin.Context) error {
	pending := getPending(c, "")
	if pending == nil || !pending.completed {
		return nil
	}
	if c == nil || c.Request == nil {
		return nil
	}
	if pending.stream {
		// Stream completion is marked only after the successful protocol terminal
		// event has been written and flushed. A client context can be canceled in
		// the tiny interval after that delivery; do not discard already-delivered
		// state merely because Commit observes the cancellation later.
		if !common.GetContextKeyBool(c, constant.ContextKeyProtocolStreamCompleted) {
			return nil
		}
	}
	if pending.kind == pendingMessages {
		return commitMessageSession(c, pending)
	}
	if len(pending.normalizedOutput) == 0 && len(pending.streamOutput) > 0 {
		output, err := mergeResponsesStreamOutput(pending, nil)
		if err != nil {
			return err
		}
		pending.normalizedOutput = output
	}
	if len(pending.normalizedOutput) == 0 {
		pending.normalizedOutput = json.RawMessage(`[]`)
	}

	identity := requestIdentity(c)
	turn := 1
	cumulativeBytes := len(pending.originalInput) + len(pending.normalizedOutput) + len(pending.normalizedTools)
	if pending.parent != nil {
		turn = pending.parent.Turn + 1
		cumulativeBytes += pending.parent.CumulativeStateBytes
	}
	policy := currentPolicy()
	if pending.bridgeManaged && turn > policy.MaxStateTurns {
		return fmt.Errorf("protocol bridge state exceeds %d turns", policy.MaxStateTurns)
	}
	if pending.bridgeManaged && cumulativeBytes > policy.MaxStateBytes {
		return fmt.Errorf("protocol bridge state exceeds %d serialized bytes", policy.MaxStateBytes)
	}

	node := ResponseNode{
		Version:              stateVersion,
		UserID:               identity.userID,
		TokenID:              identity.tokenID,
		PublicResponseID:     pending.publicID,
		ParentResponseID:     pending.parentResponseID,
		ChannelID:            pending.channelID,
		RequestProtocol:      pending.requestProtocol,
		UpstreamProtocol:     pending.upstreamProtocol,
		BridgeManaged:        pending.bridgeManaged,
		UpstreamResponseID:   pending.upstreamResponseID,
		UpstreamStored:       pending.upstreamStored,
		PublicModel:          pending.publicModel,
		UpstreamModel:        pending.upstreamModel,
		NormalizedInput:      append(json.RawMessage(nil), pending.originalInput...),
		NormalizedOutput:     append(json.RawMessage(nil), pending.normalizedOutput...),
		NormalizedTools:      append(json.RawMessage(nil), pending.normalizedTools...),
		Turn:                 turn,
		CumulativeStateBytes: cumulativeBytes,
		CreatedAt:            time.Now().Unix(),
	}
	ttl := time.Duration(policy.StateTTLSeconds) * time.Second
	stateCache, ownerCache, _ := protocolCaches()
	if err := stateCache.SetWithTTL(responseNodeKey(identity, pending.publicID), node, ttl); err != nil {
		return err
	}
	return ownerCache.SetWithTTL(pending.publicID, identity.String(), ttl)
}

func replayResponsesHistory(c *gin.Context, parent *ResponseNode, currentInput json.RawMessage, targetChannelID int, targetProtocol channelcompat.Protocol, targetUpstreamModel string, bridgeManaged bool) (json.RawMessage, []json.RawMessage, error) {
	chain, err := loadResponseHistoryChain(c, parent, bridgeManaged)
	if err != nil {
		return nil, nil, err
	}

	items := make([]json.RawMessage, 0, len(chain)*2+1)
	historicalTools := collectResponsesHistoryTools(chain)
	for _, node := range chain {
		sameProvider := node.ChannelID == targetChannelID &&
			node.UpstreamProtocol == string(targetProtocol) &&
			strings.TrimSpace(node.UpstreamModel) == strings.TrimSpace(targetUpstreamModel)
		sanitizeForNativeResponses := targetProtocol == channelcompat.ProtocolResponses && !sameProvider
		items, err = appendResponsesItems(items, node.NormalizedInput, true, sanitizeForNativeResponses, !sameProvider)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid stored Responses input: %w", err)
		}
		items, err = appendResponsesItems(items, node.NormalizedOutput, false, sanitizeForNativeResponses, !sameProvider)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid stored Responses output: %w", err)
		}
	}
	sameParentProvider := parent.ChannelID == targetChannelID &&
		parent.UpstreamProtocol == string(targetProtocol) &&
		strings.TrimSpace(parent.UpstreamModel) == strings.TrimSpace(targetUpstreamModel)
	sanitizeCurrentForNativeResponses := targetProtocol == channelcompat.ProtocolResponses && !sameParentProvider
	items, err = appendResponsesItems(items, currentInput, true, sanitizeCurrentForNativeResponses, !sameParentProvider)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid current Responses input: %w", err)
	}
	replayed, err := common.Marshal(items)
	if err != nil {
		return nil, nil, err
	}
	policy := currentPolicy()
	if bridgeManaged && len(replayed) > policy.MaxStateBytes {
		return nil, nil, fmt.Errorf("replayed Responses history exceeds %d serialized bytes", policy.MaxStateBytes)
	}
	return replayed, historicalTools, nil
}

func loadResponseHistoryChain(c *gin.Context, parent *ResponseNode, bridgeManaged bool) ([]*ResponseNode, error) {
	policy := currentPolicy()
	capacityHint := min(parent.Turn, policy.MaxStateTurns)
	chain := make([]*ResponseNode, 0, capacityHint)
	seen := make(map[string]struct{}, capacityHint)
	current := cloneResponseNode(parent)
	for current != nil {
		if bridgeManaged && len(chain) >= policy.MaxStateTurns {
			return nil, fmt.Errorf("previous_response_id exceeds the maximum conversation length of %d turns", policy.MaxStateTurns)
		}
		if _, exists := seen[current.PublicResponseID]; exists {
			return nil, fmt.Errorf("previous_response_id state contains a cycle")
		}
		seen[current.PublicResponseID] = struct{}{}
		chain = append(chain, current)
		if current.ParentResponseID == "" {
			break
		}
		next, err := loadResponseNode(c, current.ParentResponseID, current.PublicModel)
		if err != nil {
			return nil, fmt.Errorf("failed to load previous_response_id parent: %w", err)
		}
		current = next
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return chain, nil
}

func collectResponsesHistoryTools(chain []*ResponseNode) []json.RawMessage {
	historicalTools := make([]json.RawMessage, 0, len(chain))
	for _, node := range chain {
		if len(bytes.TrimSpace(node.NormalizedTools)) > 0 && !bytes.Equal(bytes.TrimSpace(node.NormalizedTools), []byte("null")) {
			historicalTools = append(historicalTools, append(json.RawMessage(nil), node.NormalizedTools...))
		}
	}
	return historicalTools
}

func appendResponsesItems(items []json.RawMessage, raw json.RawMessage, input bool, sanitizeForNativeResponses, stripOpaqueProviderState bool) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return items, nil
	}
	switch common.GetJsonType(trimmed) {
	case "array":
		var values []json.RawMessage
		if err := common.Unmarshal(trimmed, &values); err != nil {
			return nil, err
		}
		for _, value := range values {
			if sanitizeForNativeResponses {
				prepared, keep, err := sanitizeResponsesReplayItem(value)
				if err != nil {
					return nil, err
				}
				if !keep {
					continue
				}
				value = prepared
			} else if stripOpaqueProviderState {
				prepared, err := stripResponsesReplayProviderState(value)
				if err != nil {
					return nil, err
				}
				value = prepared
			}
			items = append(items, append(json.RawMessage(nil), value...))
		}
		return items, nil
	case "string":
		if !input {
			return nil, fmt.Errorf("stored output must be an array")
		}
		var text string
		if err := common.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
		message, err := common.Marshal(map[string]any{"role": "user", "content": text})
		if err != nil {
			return nil, err
		}
		return append(items, message), nil
	case "object":
		if !input {
			return nil, fmt.Errorf("stored output must be an array")
		}
		if sanitizeForNativeResponses {
			prepared, keep, err := sanitizeResponsesReplayItem(trimmed)
			if err != nil {
				return nil, err
			}
			if !keep {
				return items, nil
			}
			trimmed = prepared
		} else if stripOpaqueProviderState {
			prepared, err := stripResponsesReplayProviderState(trimmed)
			if err != nil {
				return nil, err
			}
			trimmed = prepared
		}
		return append(items, append(json.RawMessage(nil), trimmed...)), nil
	default:
		return nil, fmt.Errorf("unsupported input JSON type %s", common.GetJsonType(trimmed))
	}
}

func stripResponsesReplayProviderState(raw json.RawMessage) (json.RawMessage, error) {
	if common.GetJsonType(raw) != "object" {
		return raw, nil
	}
	var item map[string]any
	if err := common.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(common.Interface2String(item["type"])) != "reasoning" {
		return raw, nil
	}
	if _, exists := item["encrypted_content"]; !exists {
		return raw, nil
	}
	delete(item, "encrypted_content")
	return common.Marshal(item)
}

func sanitizeResponsesReplayItem(raw json.RawMessage) (json.RawMessage, bool, error) {
	var item map[string]any
	if err := common.Unmarshal(raw, &item); err != nil {
		return nil, false, err
	}
	itemType := strings.TrimSpace(common.Interface2String(item["type"]))
	if itemType == "" && strings.TrimSpace(common.Interface2String(item["role"])) != "" {
		itemType = "message"
	}
	prepared := make(map[string]any)
	switch itemType {
	case "message":
		role := strings.ToLower(strings.TrimSpace(common.Interface2String(item["role"])))
		switch role {
		case "user", "assistant", "system", "developer":
		case "latest_reminder":
			role = "user"
		default:
			role = "user"
		}
		prepared["type"] = "message"
		prepared["role"] = role
		prepared["content"] = sanitizeResponsesReplayContent(item["content"])
		if role == "assistant" {
			phase := strings.TrimSpace(common.Interface2String(item["phase"]))
			if phase == "commentary" || phase == "final_answer" {
				prepared["phase"] = phase
			}
		}
	case "function_call":
		prepared = responsesReplayFields(item, "type", "call_id", "name", "namespace", "arguments")
	case "function_call_output":
		prepared = responsesReplayFields(item, "type", "call_id", "output")
	case "custom_tool_call":
		prepared = responsesReplayFields(item, "type", "call_id", "name", "namespace", "input")
	case "custom_tool_call_output":
		prepared = responsesReplayFields(item, "type", "call_id", "output")
	case "tool_search_call":
		execution := strings.TrimSpace(common.Interface2String(item["execution"]))
		if execution != "" && execution != "client" {
			return nil, false, nil
		}
		prepared = responsesReplayFields(item, "type", "call_id", "execution", "arguments")
	case "tool_search_output":
		prepared = responsesReplayFields(item, "type", "call_id")
		prepared["tools"] = sanitizeResponsesReplayTools(item["tools"])
	case "additional_tools":
		prepared["type"] = "additional_tools"
		prepared["tools"] = sanitizeResponsesReplayTools(item["tools"])
	case "input_text", "input_image", "input_file", "input_audio", "input_video":
		part, keep := sanitizeResponsesReplayContentPart(item)
		if !keep {
			return nil, false, nil
		}
		prepared = part
	default:
		return nil, false, nil
	}

	encoded, err := common.Marshal(prepared)
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

func sanitizeResponsesReplayContent(content any) any {
	parts, ok := content.([]any)
	if !ok {
		return content
	}
	result := make([]any, 0, len(parts))
	for _, value := range parts {
		part, ok := value.(map[string]any)
		if !ok {
			continue
		}
		prepared, keep := sanitizeResponsesReplayContentPart(part)
		if keep {
			result = append(result, prepared)
		}
	}
	return result
}

func sanitizeResponsesReplayContentPart(part map[string]any) (map[string]any, bool) {
	partType := strings.TrimSpace(common.Interface2String(part["type"]))
	switch partType {
	case "output_text", "text":
		return map[string]any{"type": "input_text", "text": common.Interface2String(part["text"])}, true
	case "refusal":
		return map[string]any{"type": "input_text", "text": common.Interface2String(part["refusal"])}, true
	case "input_text":
		return responsesReplayFields(part, "type", "text", "prompt_cache_breakpoint"), true
	case "input_image":
		return responsesReplayFields(part, "type", "detail", "file_id", "image_url", "prompt_cache_breakpoint"), true
	case "input_file":
		return responsesReplayFields(part, "type", "detail", "file_data", "file_id", "file_url", "filename", "prompt_cache_breakpoint"), true
	case "input_audio":
		return responsesReplayFields(part, "type", "input_audio", "audio_url", "data", "format"), true
	case "input_video":
		return responsesReplayFields(part, "type", "video_url", "file_id", "detail"), true
	default:
		return nil, false
	}
}

func sanitizeResponsesReplayTools(value any) []any {
	tools, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(tools))
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok {
			continue
		}
		toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
		var prepared map[string]any
		switch toolType {
		case "function":
			prepared = responsesReplayFields(tool, "type", "name", "description", "parameters", "strict", "defer_loading", "allowed_callers")
		case "custom", "freeform":
			prepared = responsesReplayFields(tool, "type", "name", "description", "format", "defer_loading", "allowed_callers")
		case "namespace":
			prepared = responsesReplayFields(tool, "type", "name", "description")
			prepared["tools"] = sanitizeResponsesReplayTools(tool["tools"])
		case "tool_search":
			execution := strings.TrimSpace(common.Interface2String(tool["execution"]))
			if execution != "" && execution != "client" {
				continue
			}
			prepared = responsesReplayFields(tool, "type", "description", "execution", "parameters")
		default:
			continue
		}
		result = append(result, prepared)
	}
	return result
}

func responsesReplayFields(source map[string]any, fields ...string) map[string]any {
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		if value, exists := source[field]; exists {
			result[field] = value
		}
	}
	return result
}

type identity struct {
	userID  int
	tokenID int
}

func requestIdentity(c *gin.Context) identity {
	if c == nil {
		return identity{}
	}
	return identity{
		userID:  common.GetContextKeyInt(c, constant.ContextKeyUserId),
		tokenID: common.GetContextKeyInt(c, constant.ContextKeyTokenId),
	}
}

func (i identity) String() string {
	return fmt.Sprintf("%d:%d", i.userID, i.tokenID)
}

func responseNodeKey(identity identity, publicID string) string {
	return identity.String() + ":" + strings.TrimSpace(publicID)
}

func loadResponseNode(c *gin.Context, publicID, publicModel string) (*ResponseNode, error) {
	node, managed, err := findResponseNode(c, publicID, publicModel)
	if err != nil {
		return nil, err
	}
	if !managed {
		return nil, fmt.Errorf("previous_response_id is unknown or expired")
	}
	return node, nil
}

func findResponseNode(c *gin.Context, publicID, publicModel string) (*ResponseNode, bool, error) {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return nil, false, fmt.Errorf("previous_response_id is empty")
	}
	identity := requestIdentity(c)
	stateCache, ownerCache, _ := protocolCaches()
	node, found, err := stateCache.Get(responseNodeKey(identity, publicID))
	if err != nil {
		return nil, false, fmt.Errorf("failed to load previous_response_id state: %w", err)
	}
	if !found {
		owner, ownerFound, ownerErr := ownerCache.Get(publicID)
		if ownerErr != nil {
			return nil, false, fmt.Errorf("failed to verify previous_response_id ownership: %w", ownerErr)
		}
		if ownerFound && owner != identity.String() {
			return nil, true, fmt.Errorf("previous_response_id belongs to a different user or API token")
		}
		if ownerFound {
			return nil, true, fmt.Errorf("previous_response_id is unknown or expired")
		}
		return nil, false, nil
	}
	if node.Version != stateVersion {
		return nil, true, fmt.Errorf("previous_response_id uses an unsupported state version")
	}
	if node.UserID != identity.userID || node.TokenID != identity.tokenID {
		return nil, true, fmt.Errorf("previous_response_id belongs to a different user or API token")
	}
	if strings.TrimSpace(publicModel) != strings.TrimSpace(node.PublicModel) {
		return nil, true, fmt.Errorf("previous_response_id model %q does not match requested model %q", node.PublicModel, publicModel)
	}
	if node.Turn < 1 {
		return nil, true, fmt.Errorf("previous_response_id exceeds the maximum conversation length")
	}
	if node.CumulativeStateBytes < 0 {
		return nil, true, fmt.Errorf("previous_response_id exceeds the maximum serialized state size")
	}
	policy := currentPolicy()
	if responseNodeUsesBridgeState(&node) {
		if node.Turn > policy.MaxStateTurns {
			return nil, true, fmt.Errorf("previous_response_id exceeds the maximum conversation length")
		}
		if node.CumulativeStateBytes > policy.MaxStateBytes {
			return nil, true, fmt.Errorf("previous_response_id exceeds the maximum serialized state size")
		}
	}
	ttl := time.Duration(policy.StateTTLSeconds) * time.Second
	if err := stateCache.SetWithTTL(responseNodeKey(identity, publicID), node, ttl); err != nil {
		return nil, true, fmt.Errorf("failed to refresh previous_response_id state: %w", err)
	}
	if err := ownerCache.SetWithTTL(publicID, identity.String(), ttl); err != nil {
		return nil, true, fmt.Errorf("failed to refresh previous_response_id ownership: %w", err)
	}
	return cloneResponseNode(&node), true, nil
}

func responseNodeUsesBridgeState(node *ResponseNode) bool {
	// The protocol mismatch keeps converted v1 cache entries written before the
	// explicit marker backward compatible.
	return node != nil && (node.BridgeManaged || node.RequestProtocol != node.UpstreamProtocol)
}

func ensurePublicResponseID(c *gin.Context) string {
	if value := common.GetContextKeyString(c, constant.ContextKeyProtocolStatePublicID); value != "" {
		return value
	}
	requestID := strings.TrimSpace(c.GetString(common.RequestIdKey))
	if requestID == "" {
		requestID = common.GetUUID()
	}
	requestID = strings.TrimPrefix(requestID, "req_")
	publicID := "resp_" + requestID
	common.SetContextKey(c, constant.ContextKeyProtocolStatePublicID, publicID)
	return publicID
}

func getPending(c *gin.Context, kind pendingKind) *pendingState {
	if c == nil {
		return nil
	}
	pending, ok := common.GetContextKeyType[*pendingState](c, constant.ContextKeyProtocolStatePending)
	if !ok || pending == nil || (kind != "" && pending.kind != kind) {
		return nil
	}
	return pending
}

func cloneResponseNode(node *ResponseNode) *ResponseNode {
	if node == nil {
		return nil
	}
	clone := *node
	clone.NormalizedInput = append(json.RawMessage(nil), node.NormalizedInput...)
	clone.NormalizedOutput = append(json.RawMessage(nil), node.NormalizedOutput...)
	clone.NormalizedTools = append(json.RawMessage(nil), node.NormalizedTools...)
	return &clone
}

func currentPolicy() model_setting.ProtocolBridgePolicy {
	policy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	if policy.StateTTLSeconds <= 0 {
		policy.StateTTLSeconds = model_setting.DefaultProtocolBridgeStateTTLSeconds
	}
	if policy.MaxStateTurns <= 0 {
		policy.MaxStateTurns = model_setting.DefaultProtocolBridgeMaxStateTurns
	}
	if policy.MaxStateBytes <= 0 {
		policy.MaxStateBytes = model_setting.DefaultProtocolBridgeMaxStateBytes
	}
	return policy
}

func protocolCaches() (*cachex.HybridCache[ResponseNode], *cachex.HybridCache[string], *cachex.HybridCache[MessageSession]) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if responseStateCache == nil {
		responseStateCache = cachex.NewHybridCache(cachex.HybridCacheConfig[ResponseNode]{
			Namespace:    stateCacheNamespace,
			Redis:        common.RDB,
			RedisCodec:   responseNodeCodec{},
			RedisEnabled: func() bool { return common.RedisEnabled },
			Memory: func() *hot.HotCache[string, ResponseNode] {
				return hot.NewHotCache[string, ResponseNode](hot.LRU, stateCacheCapacity).Build()
			},
		})
	}
	if responseOwnerCache == nil {
		responseOwnerCache = cachex.NewHybridCache(cachex.HybridCacheConfig[string]{
			Namespace:    ownerCacheNamespace,
			Redis:        common.RDB,
			RedisCodec:   cachex.StringCodec{},
			RedisEnabled: func() bool { return common.RedisEnabled },
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, stateCacheCapacity).Build()
			},
		})
	}
	if messageStateCache == nil {
		messageStateCache = newMessageStateCache()
	}
	if !common.RedisEnabled && (os.Getenv("NODE_TYPE") != "" || os.Getenv("NODE_NAME") != "") {
		warnNoRedisOnce.Do(func() {
			common.SysError("protocol bridge state is using process-local memory in a multi-node configuration; configure Redis to share continuation state")
		})
	}
	return responseStateCache, responseOwnerCache, messageStateCache
}

func isMissingContinuationError(apiError *types.NewAPIError) bool {
	if apiError == nil {
		return false
	}
	message := strings.ToLower(apiError.Error())
	mentionsPrevious := strings.Contains(message, "previous_response") || strings.Contains(message, "previous response")
	if !mentionsPrevious {
		return false
	}
	if apiError.StatusCode == 404 || apiError.StatusCode == 405 || apiError.StatusCode == 501 {
		return true
	}
	return strings.Contains(message, "not found") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "unsupported") ||
		strings.Contains(message, "not support") ||
		strings.Contains(message, "invalid")
}
