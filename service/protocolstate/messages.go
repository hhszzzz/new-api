package protocolstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

type MessageSession struct {
	Version            int                       `json:"version"`
	UserID             int                       `json:"user_id"`
	TokenID            int                       `json:"token_id"`
	SessionKey         string                    `json:"session_key"`
	ChannelID          int                       `json:"channel_id"`
	UpstreamResponseID string                    `json:"upstream_response_id"`
	UpstreamStored     bool                      `json:"upstream_stored"`
	PublicModel        string                    `json:"public_model"`
	UpstreamModel      string                    `json:"upstream_model,omitempty"`
	History            []json.RawMessage         `json:"history"`
	ProviderOutputs    map[int][]json.RawMessage `json:"provider_outputs,omitempty"`
	Turn               int                       `json:"turn"`
	SerializedBytes    int                       `json:"serialized_bytes"`
	CreatedAt          int64                     `json:"created_at"`
}

type messageSelection struct {
	key                    string
	currentHistory         []json.RawMessage
	serializedHistoryBytes int
	session                *MessageSession
	strictAppend           bool
}

type messageSessionCodec struct{}

func (messageSessionCodec) Encode(value MessageSession) (string, error) {
	data, err := common.Marshal(value)
	return string(data), err
}

func (messageSessionCodec) Decode(value string) (MessageSession, error) {
	var session MessageSession
	err := common.UnmarshalJsonStr(value, &session)
	return session, err
}

func resolveMessageSelectionBinding(c *gin.Context, publicModel string, body []byte) (*SelectionBinding, error) {
	var request dto.ClaudeRequest
	if err := common.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	selection, err := buildMessageSelection(c, publicModel, &request)
	if err != nil || selection == nil {
		return nil, err
	}
	common.SetContextKey(c, constant.ContextKeyProtocolStateSession, selection)
	if selection.strictAppend && selection.session != nil {
		return &SelectionBinding{
			ChannelID:        selection.session.ChannelID,
			UpstreamProtocol: channelcompat.ProtocolResponses,
		}, nil
	}
	return nil, nil
}

func PrepareMessagesRequest(c *gin.Context, info *relaycommon.RelayInfo, plan channelcompat.ProtocolPlan, request *dto.ClaudeRequest) error {
	manageState := Enabled() || plan.StateEnabled
	if !manageState || c == nil || info == nil || request == nil || plan.RequestProtocol != channelcompat.ProtocolMessages {
		return nil
	}
	if plan.UpstreamProtocol != channelcompat.ProtocolResponses {
		common.SetContextKey(c, constant.ContextKeyProtocolStatePending, nil)
		return nil
	}

	selection, ok := common.GetContextKeyType[*messageSelection](c, constant.ContextKeyProtocolStateSession)
	if !ok || selection == nil {
		var err error
		selection, err = buildMessageSelection(c, info.OriginModelName, request)
		if err != nil {
			return err
		}
		if selection == nil {
			return nil
		}
		common.SetContextKey(c, constant.ContextKeyProtocolStateSession, selection)
	}
	policy := currentPolicy()
	if selection.serializedHistoryBytes > policy.MaxStateBytes {
		return fmt.Errorf("Claude Code message history exceeds the maximum serialized state size of %d bytes", policy.MaxStateBytes)
	}

	pending := &pendingState{
		kind:             pendingMessages,
		stream:           info.IsStream,
		publicModel:      info.OriginModelName,
		upstreamModel:    strings.TrimSpace(info.UpstreamModelName),
		channelID:        info.ChannelId,
		requestProtocol:  string(plan.RequestProtocol),
		upstreamProtocol: string(plan.UpstreamProtocol),
		messageSelection: selection,
		claudeBlocks:     make(map[int]*dto.ClaudeMediaMessage),
	}
	forceReplay := common.GetContextKeyBool(c, constant.ContextKeyProtocolStateForceReplay)
	sameUpstream := selection.session != nil &&
		selection.session.ChannelID == info.ChannelId &&
		strings.TrimSpace(selection.session.UpstreamModel) == strings.TrimSpace(info.UpstreamModelName)
	if !forceReplay && selection.strictAppend && selection.session != nil &&
		sameUpstream && selection.session.UpstreamStored &&
		strings.TrimSpace(selection.session.UpstreamResponseID) != "" {
		prefixLength := len(selection.session.History)
		if prefixLength < len(request.Messages) {
			request.Messages = append([]dto.ClaudeMessage(nil), request.Messages[prefixLength:]...)
			pending.continuationID = selection.session.UpstreamResponseID
			pending.usedContinuation = true
		}
	} else if selection.strictAppend && sameUpstream {
		attachProviderResponsesOutput(request.Messages, selection.session.ProviderOutputs)
	}
	common.SetContextKey(c, constant.ContextKeyProtocolStatePending, pending)
	return nil
}

func ApplyMessagesContinuation(c *gin.Context, request *dto.OpenAIResponsesRequest) {
	pending := getPending(c, pendingMessages)
	if pending == nil || request == nil {
		return
	}
	if pending.messageSelection != nil && strings.TrimSpace(pending.messageSelection.key) != "" {
		identity := requestIdentity(c)
		material := fmt.Sprintf("%d:%d:%s:%s", identity.userID, identity.tokenID, pending.publicModel, pending.messageSelection.key)
		digest := sha256.Sum256([]byte(material))
		if encoded, err := common.Marshal("newapi_messages_" + hex.EncodeToString(digest[:])); err == nil {
			request.PromptCacheKey = encoded
		}
	}
	if pending.usedContinuation {
		request.PreviousResponseID = pending.continuationID
	}
}

func CaptureMessagesResponse(c *gin.Context, upstream *dto.OpenAIResponsesResponse, response *dto.ClaudeResponse) {
	CaptureMessagesResponseData(c, upstream, nil, response)
}

// CaptureMessagesResponseData preserves the upstream Responses output exactly
// for same-channel stateless replay while keeping the public Claude response
// free of provider-owned state.
func CaptureMessagesResponseData(c *gin.Context, upstream *dto.OpenAIResponsesResponse, rawOutput json.RawMessage, response *dto.ClaudeResponse) {
	pending := getPending(c, pendingMessages)
	if pending == nil {
		return
	}
	if upstream != nil {
		pending.upstreamResponseID = strings.TrimSpace(upstream.ID)
		pending.upstreamStored = upstream.Store
		status := common.JsonRawMessageToString(upstream.Status)
		pending.completed = status == "completed"
		pending.terminal = isResponsesTerminalStatus(status)
		if common.GetJsonType(rawOutput) == "array" || len(pending.upstreamOutput) == 0 {
			pending.upstreamOutput = responsesOutputRawMessages(rawOutput, upstream.Output)
		}
	}
	if response == nil {
		return
	}
	content := cloneClaudeContent(response.Content)
	content = sanitizeResponsesBackedClaudeContent(content)
	assistant := dto.ClaudeMessage{Role: "assistant", Content: content}
	encoded, err := canonicalJSONRaw(assistant)
	if err == nil {
		pending.assistantMessage = encoded
	}
}

func ObserveClaudeStream(c *gin.Context, response *dto.ClaudeResponse) {
	pending := getPending(c, pendingMessages)
	if pending == nil || response == nil {
		return
	}
	index := response.GetIndex()
	switch response.Type {
	case "content_block_start":
		if response.ContentBlock != nil {
			block := *response.ContentBlock
			if block.Type == "redacted_thinking" {
				return
			}
			if block.Type == "thinking" {
				block.Signature = ""
			}
			pending.claudeBlocks[index] = &block
		}
	case "content_block_delta":
		block := pending.claudeBlocks[index]
		if block == nil || response.Delta == nil {
			return
		}
		switch response.Delta.Type {
		case "text_delta":
			text := block.GetText() + response.Delta.GetText()
			block.SetText(text)
		case "thinking_delta":
			thinking := ""
			if block.Thinking != nil {
				thinking = *block.Thinking
			}
			if response.Delta.Thinking != nil {
				thinking += *response.Delta.Thinking
			}
			block.Thinking = &thinking
		case "input_json_delta":
			if response.Delta.PartialJson != nil {
				block.Delta += *response.Delta.PartialJson
			}
		}
	case "message_stop":
		indexes := make([]int, 0, len(pending.claudeBlocks))
		for blockIndex := range pending.claudeBlocks {
			indexes = append(indexes, blockIndex)
		}
		for i := 1; i < len(indexes); i++ {
			for j := i; j > 0 && indexes[j] < indexes[j-1]; j-- {
				indexes[j], indexes[j-1] = indexes[j-1], indexes[j]
			}
		}
		content := make([]dto.ClaudeMediaMessage, 0, len(indexes))
		for _, blockIndex := range indexes {
			block := *pending.claudeBlocks[blockIndex]
			if block.Type == "tool_use" && block.Delta != "" {
				var input any
				if common.Unmarshal([]byte(block.Delta), &input) == nil {
					block.Input = input
				}
				block.Delta = ""
			}
			content = append(content, block)
		}
		assistant := dto.ClaudeMessage{Role: "assistant", Content: content}
		encoded, err := canonicalJSONRaw(assistant)
		if err == nil {
			pending.assistantMessage = encoded
		}
	}
}

func observeMessagesUpstreamResponse(pending *pendingState, event *dto.ResponsesStreamResponse, upstreamID string, rawOutput json.RawMessage) {
	if pending == nil || event == nil || pending.kind != pendingMessages {
		return
	}
	if upstreamID != "" {
		pending.upstreamResponseID = upstreamID
	}
	pending.upstreamStored = event.Response.Store
	status := common.JsonRawMessageToString(event.Response.Status)
	pending.completed = event.Type == "response.completed" || event.Type == "response.done" || status == "completed"
	pending.terminal = isResponsesTerminalStatus(status)
	if event.Type == "response.completed" || event.Type == "response.done" || event.Type == "response.incomplete" || event.Type == "response.cancelled" || event.Type == "response.canceled" || event.Type == "response.failed" || event.Type == "response.error" {
		pending.terminal = true
	}
	if common.GetJsonType(rawOutput) != "array" && len(event.Response.Output) > 0 {
		rawOutput, _ = common.Marshal(event.Response.Output)
	}
	if len(pending.streamOutput) > 0 {
		if merged, err := mergeResponsesStreamOutput(pending, rawOutput); err == nil {
			rawOutput = merged
		}
	}
	if len(rawOutput) > 0 {
		pending.upstreamOutput = responsesOutputRawMessages(rawOutput, event.Response.Output)
	}
}

func commitMessageSession(c *gin.Context, pending *pendingState) error {
	selection := pending.messageSelection
	if selection == nil || selection.key == "" || len(pending.assistantMessage) == 0 || pending.upstreamResponseID == "" {
		return nil
	}
	history := cloneRawMessages(selection.currentHistory)
	history = append(history, append(json.RawMessage(nil), pending.assistantMessage...))
	providerOutputs := make(map[int][]json.RawMessage)
	if selection.strictAppend && selection.session != nil &&
		selection.session.ChannelID == pending.channelID &&
		strings.TrimSpace(selection.session.UpstreamModel) == strings.TrimSpace(pending.upstreamModel) {
		providerOutputs = cloneProviderOutputs(selection.session.ProviderOutputs)
	}
	if len(pending.upstreamOutput) > 0 {
		providerOutputs[len(history)-1] = cloneRawMessages(pending.upstreamOutput)
	}
	serialized, err := common.Marshal(struct {
		History         []json.RawMessage         `json:"history"`
		ProviderOutputs map[int][]json.RawMessage `json:"provider_outputs,omitempty"`
	}{
		History:         history,
		ProviderOutputs: providerOutputs,
	})
	if err != nil {
		return err
	}
	policy := currentPolicy()
	turn := 1
	if selection.strictAppend && selection.session != nil {
		turn = selection.session.Turn + 1
	}
	if turn > policy.MaxStateTurns {
		return fmt.Errorf("Claude Code Responses session exceeds %d turns", policy.MaxStateTurns)
	}
	if len(serialized) > policy.MaxStateBytes {
		return fmt.Errorf("Claude Code Responses session exceeds %d serialized bytes", policy.MaxStateBytes)
	}
	identity := requestIdentity(c)
	session := MessageSession{
		Version:            stateVersion,
		UserID:             identity.userID,
		TokenID:            identity.tokenID,
		SessionKey:         selection.key,
		ChannelID:          pending.channelID,
		UpstreamResponseID: pending.upstreamResponseID,
		UpstreamStored:     pending.upstreamStored,
		PublicModel:        pending.publicModel,
		UpstreamModel:      pending.upstreamModel,
		History:            history,
		ProviderOutputs:    providerOutputs,
		Turn:               turn,
		SerializedBytes:    len(serialized),
		CreatedAt:          time.Now().Unix(),
	}
	_, _, messageCache := protocolCaches()
	ttl := time.Duration(policy.StateTTLSeconds) * time.Second
	return messageCache.SetWithTTL(messageSessionKey(identity, selection.key, pending.publicModel), session, ttl)
}

func buildMessageSelection(c *gin.Context, publicModel string, request *dto.ClaudeRequest) (*messageSelection, error) {
	stableKey, ok, err := stableClaudeSessionKey(request)
	if err != nil || !ok {
		return nil, err
	}
	history, err := encodeClaudeHistory(request.Messages)
	if err != nil {
		return nil, err
	}
	serializedHistory, err := common.Marshal(history)
	if err != nil {
		return nil, err
	}
	identity := requestIdentity(c)
	_, _, messageCache := protocolCaches()
	session, found, err := messageCache.Get(messageSessionKey(identity, stableKey, publicModel))
	if err != nil {
		return nil, fmt.Errorf("failed to load Claude Code Responses session: %w", err)
	}
	selection := &messageSelection{
		key:                    stableKey,
		currentHistory:         history,
		serializedHistoryBytes: len(serializedHistory),
	}
	if !found {
		return selection, nil
	}
	policy := currentPolicy()
	if session.Version != stateVersion || session.UserID != identity.userID || session.TokenID != identity.tokenID ||
		strings.TrimSpace(session.PublicModel) != strings.TrimSpace(publicModel) ||
		strings.TrimSpace(session.SessionKey) != stableKey || session.ChannelID <= 0 ||
		strings.TrimSpace(session.UpstreamResponseID) == "" ||
		session.Turn < 1 || session.Turn > policy.MaxStateTurns ||
		session.SerializedBytes < 1 || session.SerializedBytes > policy.MaxStateBytes {
		return selection, nil
	}
	storedState, err := common.Marshal(struct {
		History         []json.RawMessage         `json:"history"`
		ProviderOutputs map[int][]json.RawMessage `json:"provider_outputs,omitempty"`
	}{
		History:         session.History,
		ProviderOutputs: session.ProviderOutputs,
	})
	if err != nil || len(storedState) != session.SerializedBytes || len(storedState) > policy.MaxStateBytes {
		return selection, nil
	}
	ttl := time.Duration(policy.StateTTLSeconds) * time.Second
	if err := messageCache.SetWithTTL(messageSessionKey(identity, stableKey, publicModel), session, ttl); err != nil {
		return nil, fmt.Errorf("failed to refresh Claude Code Responses session: %w", err)
	}
	selection.session = cloneMessageSession(&session)
	selection.strictAppend = isStrictMessageAppend(session.History, history)
	return selection, nil
}

func stableClaudeSessionKey(request *dto.ClaudeRequest) (string, bool, error) {
	if request == nil {
		return "", false, nil
	}
	marked := len(bytes.TrimSpace(request.CacheControl)) > 0 && !bytes.Equal(bytes.TrimSpace(request.CacheControl), []byte("null"))
	markers := make([]any, 0)
	if marked {
		markers = append(markers, request.CacheControl)
	}
	collectMarked := func(role string, value any) {
		raw, err := common.Marshal(value)
		if err != nil {
			return
		}
		var parts []map[string]any
		if common.Unmarshal(raw, &parts) != nil {
			return
		}
		for index, part := range parts {
			cacheControl, exists := part["cache_control"]
			if !exists || cacheControl == nil {
				continue
			}
			marked = true
			markers = append(markers, map[string]any{"role": role, "index": index, "content": part})
		}
	}
	collectMarked("system", request.System)
	for index, message := range request.Messages {
		collectMarked(fmt.Sprintf("message:%d:%s", index, message.Role), message.Content)
	}
	if !marked {
		return "", false, nil
	}
	material := map[string]any{
		"system":  canonicalJSONValue(request.System),
		"tools":   canonicalJSONValue(request.Tools),
		"markers": markers,
	}
	encoded, err := common.Marshal(material)
	if err != nil {
		return "", false, err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), true, nil
}

func canonicalJSONValue(value any) any {
	if value == nil {
		return nil
	}
	encoded, err := common.Marshal(value)
	if err != nil {
		return value
	}
	var normalized any
	if err := common.Unmarshal(encoded, &normalized); err != nil {
		return value
	}
	return normalized
}

func canonicalJSONRaw(value any) (json.RawMessage, error) {
	encoded, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := common.Unmarshal(encoded, &normalized); err != nil {
		return nil, err
	}
	return common.Marshal(normalized)
}

func encodeClaudeHistory(messages []dto.ClaudeMessage) ([]json.RawMessage, error) {
	history := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		encoded, err := canonicalJSONRaw(message)
		if err != nil {
			return nil, err
		}
		history = append(history, encoded)
	}
	return history, nil
}

func isStrictMessageAppend(previous, current []json.RawMessage) bool {
	if len(previous) == 0 || len(current) <= len(previous) {
		return false
	}
	for index := range previous {
		if !bytes.Equal(bytes.TrimSpace(previous[index]), bytes.TrimSpace(current[index])) {
			return false
		}
	}
	return true
}

func cloneClaudeContent(content []dto.ClaudeMediaMessage) []dto.ClaudeMediaMessage {
	result := make([]dto.ClaudeMediaMessage, len(content))
	for index := range content {
		result[index] = content[index]
	}
	return result
}

func sanitizeResponsesBackedClaudeContent(content []dto.ClaudeMediaMessage) []dto.ClaudeMediaMessage {
	result := make([]dto.ClaudeMediaMessage, 0, len(content))
	for _, block := range content {
		if block.Type == "redacted_thinking" {
			continue
		}
		if block.Type == "thinking" {
			block.Signature = ""
		}
		result = append(result, block)
	}
	return result
}

func cloneRawMessages(values []json.RawMessage) []json.RawMessage {
	clone := make([]json.RawMessage, len(values))
	for index := range values {
		clone[index] = append(json.RawMessage(nil), values[index]...)
	}
	return clone
}

func cloneMessageSession(session *MessageSession) *MessageSession {
	if session == nil {
		return nil
	}
	clone := *session
	clone.History = cloneRawMessages(session.History)
	clone.ProviderOutputs = cloneProviderOutputs(session.ProviderOutputs)
	return &clone
}

func attachProviderResponsesOutput(messages []dto.ClaudeMessage, providerOutputs map[int][]json.RawMessage) {
	for index, output := range providerOutputs {
		if index < 0 || index >= len(messages) || strings.TrimSpace(messages[index].Role) != "assistant" || len(output) == 0 {
			continue
		}
		messages[index].ProviderResponsesRawOutput = cloneRawMessages(output)
	}
}

func cloneProviderOutputs(values map[int][]json.RawMessage) map[int][]json.RawMessage {
	if len(values) == 0 {
		return make(map[int][]json.RawMessage)
	}
	clone := make(map[int][]json.RawMessage, len(values))
	for index, output := range values {
		clone[index] = cloneRawMessages(output)
	}
	return clone
}

func responsesOutputRawMessages(raw json.RawMessage, fallback []dto.ResponsesOutput) []json.RawMessage {
	if common.GetJsonType(raw) == "array" {
		var output []json.RawMessage
		if common.Unmarshal(raw, &output) == nil {
			return cloneRawMessages(output)
		}
	}
	output := make([]json.RawMessage, 0, len(fallback))
	for _, item := range fallback {
		encoded, err := common.Marshal(item)
		if err == nil {
			output = append(output, encoded)
		}
	}
	return output
}

func messageSessionKey(identity identity, stableKey, publicModel string) string {
	modelDigest := sha256.Sum256([]byte(strings.TrimSpace(publicModel)))
	return identity.String() + ":" + stableKey + ":" + hex.EncodeToString(modelDigest[:8])
}

func newMessageStateCache() *cachex.HybridCache[MessageSession] {
	return cachex.NewHybridCache(cachex.HybridCacheConfig[MessageSession]{
		Namespace:    messageCacheNamespace,
		Redis:        common.RDB,
		RedisCodec:   messageSessionCodec{},
		RedisEnabled: func() bool { return common.RedisEnabled },
		Memory: func() *hot.HotCache[string, MessageSession] {
			return hot.NewHotCache[string, MessageSession](hot.LRU, stateCacheCapacity).Build()
		},
	})
}
