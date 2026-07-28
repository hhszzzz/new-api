package protocolstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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
	UpstreamResponseID   string          `json:"upstream_response_id,omitempty"`
	UpstreamStored       bool            `json:"upstream_stored"`
	PublicModel          string          `json:"public_model"`
	NormalizedInput      json.RawMessage `json:"normalized_input"`
	NormalizedOutput     json.RawMessage `json:"normalized_output"`
	Turn                 int             `json:"turn"`
	CumulativeStateBytes int             `json:"cumulative_state_bytes"`
	CreatedAt            int64           `json:"created_at"`
}

type SelectionBinding struct {
	ChannelID int
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
	channelID                int
	requestProtocol          string
	upstreamProtocol         string
	parent                   *ResponseNode
	parentResponseID         string
	originalInput            json.RawMessage
	continuationID           string
	upstreamResponseID       string
	upstreamStored           bool
	normalizedOutput         json.RawMessage
	streamOutput             []json.RawMessage
	completed                bool
	usedContinuation         bool
	continuationAcknowledged bool

	messageSelection *messageSelection
	assistantMessage json.RawMessage
	claudeBlocks     map[int]*dto.ClaudeMediaMessage
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

func ResolveSelectionBinding(c *gin.Context, requestPath, publicModel string, body []byte) (*SelectionBinding, error) {
	if !Enabled() || c == nil {
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
		node, err := loadResponseNode(c, previousID, publicModel)
		if err != nil {
			return nil, err
		}
		common.SetContextKey(c, constant.ContextKeyProtocolStateParent, node)
		return &SelectionBinding{ChannelID: node.ChannelID}, nil
	case "/v1/messages":
		return resolveMessageSelectionBinding(c, publicModel, body)
	default:
		return nil, nil
	}
}

func PrepareResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, plan channelcompat.ProtocolPlan, request *dto.OpenAIResponsesRequest) error {
	if !Enabled() || c == nil || info == nil || request == nil || plan.RequestProtocol != channelcompat.ProtocolResponses {
		return nil
	}

	publicID := ensurePublicResponseID(c)
	originalInput := append(json.RawMessage(nil), request.Input...)
	policy := currentPolicy()
	if len(originalInput) > policy.MaxStateBytes {
		return fmt.Errorf("Responses input exceeds the maximum serialized state size of %d bytes", policy.MaxStateBytes)
	}
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

	pending := &pendingState{
		kind:             pendingResponses,
		stream:           info.IsStream,
		publicID:         publicID,
		publicModel:      info.OriginModelName,
		channelID:        info.ChannelId,
		requestProtocol:  string(plan.RequestProtocol),
		upstreamProtocol: string(plan.UpstreamProtocol),
		parent:           parent,
		parentResponseID: previousID,
		originalInput:    originalInput,
	}

	if parent != nil {
		if parent.Turn >= policy.MaxStateTurns {
			return fmt.Errorf("previous_response_id exceeds the maximum conversation length of %d turns", policy.MaxStateTurns)
		}
		if parent.CumulativeStateBytes+len(originalInput) > policy.MaxStateBytes {
			return fmt.Errorf("previous_response_id state exceeds the maximum serialized size of %d bytes", policy.MaxStateBytes)
		}

		forceReplay := common.GetContextKeyBool(c, constant.ContextKeyProtocolStateForceReplay)
		canContinueNatively := !forceReplay &&
			plan.UpstreamProtocol == channelcompat.ProtocolResponses &&
			info.ChannelId == parent.ChannelID &&
			parent.UpstreamProtocol == string(channelcompat.ProtocolResponses) &&
			parent.UpstreamStored &&
			strings.TrimSpace(parent.UpstreamResponseID) != ""
		if canContinueNatively {
			request.PreviousResponseID = parent.UpstreamResponseID
			pending.continuationID = parent.UpstreamResponseID
			pending.usedContinuation = true
		} else {
			replayedInput, err := replayResponsesHistory(c, parent, originalInput)
			if err != nil {
				return err
			}
			request.Input = replayedInput
			request.PreviousResponseID = ""
		}
	}

	common.SetContextKey(c, constant.ContextKeyProtocolStatePending, pending)
	return nil
}

func PublicResponseID(c *gin.Context, fallback string) string {
	if !Enabled() || c == nil {
		return fallback
	}
	if value := common.GetContextKeyString(c, constant.ContextKeyProtocolStatePublicID); value != "" {
		return value
	}
	return fallback
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
	response.ID = pending.publicID
	response.Model = pending.publicModel
	return pending.publicID
}

func ObserveResponsesStream(c *gin.Context, event *dto.ResponsesStreamResponse) {
	pending := getPending(c, "")
	if pending == nil || event == nil {
		return
	}
	if event.Type == dto.ResponsesOutputTypeItemDone && event.Item != nil && pending.kind == pendingResponses {
		if item, err := common.Marshal(event.Item); err == nil {
			pending.streamOutput = append(pending.streamOutput, item)
		}
	}
	if event.Response == nil {
		return
	}
	upstreamID := event.Response.ID
	if pending.kind == pendingResponses {
		if len(event.Response.Output) == 0 && len(pending.streamOutput) > 0 {
			output, err := common.Marshal(pending.streamOutput)
			if err == nil {
				_ = common.Unmarshal(output, &event.Response.Output)
			}
		}
		var rawOutput json.RawMessage
		if len(pending.streamOutput) > 0 {
			rawOutput, _ = common.Marshal(pending.streamOutput)
		}
		captureResponsesResponse(pending, upstreamID, event.Response, rawOutput)
		return
	}
	observeMessagesUpstreamResponse(pending, event, upstreamID)
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
	if event.Type == dto.ResponsesOutputTypeItemDone && pending.kind == pendingResponses {
		if item := fields["item"]; common.GetJsonType(item) == "object" {
			pending.streamOutput = append(pending.streamOutput, append(json.RawMessage(nil), item...))
		} else if event.Item != nil {
			encoded, err := common.Marshal(event.Item)
			if err != nil {
				return nil, err
			}
			pending.streamOutput = append(pending.streamOutput, encoded)
		}
	}
	if event.Response == nil {
		return data, nil
	}

	upstreamID := event.Response.ID
	if pending.kind != pendingResponses {
		observeMessagesUpstreamResponse(pending, event, upstreamID)
		return data, nil
	}

	var responseFields map[string]json.RawMessage
	if rawResponse := fields["response"]; common.GetJsonType(rawResponse) == "object" {
		if err := common.Unmarshal(rawResponse, &responseFields); err != nil {
			return nil, err
		}
	}
	rawOutput := responseFields["output"]
	if common.GetJsonType(rawOutput) != "array" && len(pending.streamOutput) > 0 {
		var err error
		rawOutput, err = common.Marshal(pending.streamOutput)
		if err != nil {
			return nil, err
		}
		responseFields["output"] = rawOutput
		if len(event.Response.Output) == 0 {
			_ = common.Unmarshal(rawOutput, &event.Response.Output)
		}
	}
	captureResponsesResponse(pending, upstreamID, event.Response, rawOutput)

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
	encodedResponse, err := common.Marshal(responseFields)
	if err != nil {
		return nil, err
	}
	fields["response"] = encodedResponse
	return common.Marshal(fields)
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

func Commit(c *gin.Context) error {
	pending := getPending(c, "")
	if pending == nil || !pending.completed {
		return nil
	}
	if c == nil || c.Request == nil {
		return nil
	}
	if pending.stream && c.Request.Context().Err() != nil {
		return nil
	}
	if pending.kind == pendingMessages {
		return commitMessageSession(c, pending)
	}
	if len(pending.normalizedOutput) == 0 && len(pending.streamOutput) > 0 {
		output, err := common.Marshal(pending.streamOutput)
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
	cumulativeBytes := len(pending.originalInput) + len(pending.normalizedOutput)
	if pending.parent != nil {
		turn = pending.parent.Turn + 1
		cumulativeBytes += pending.parent.CumulativeStateBytes
	}
	policy := currentPolicy()
	if turn > policy.MaxStateTurns {
		return fmt.Errorf("protocol bridge state exceeds %d turns", policy.MaxStateTurns)
	}
	if cumulativeBytes > policy.MaxStateBytes {
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
		UpstreamResponseID:   pending.upstreamResponseID,
		UpstreamStored:       pending.upstreamStored,
		PublicModel:          pending.publicModel,
		NormalizedInput:      append(json.RawMessage(nil), pending.originalInput...),
		NormalizedOutput:     append(json.RawMessage(nil), pending.normalizedOutput...),
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

func replayResponsesHistory(c *gin.Context, parent *ResponseNode, currentInput json.RawMessage) (json.RawMessage, error) {
	chain := make([]*ResponseNode, 0, parent.Turn)
	seen := make(map[string]struct{}, parent.Turn)
	current := cloneResponseNode(parent)
	policy := currentPolicy()
	for current != nil {
		if len(chain) >= policy.MaxStateTurns {
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

	items := make([]json.RawMessage, 0, len(chain)*2+1)
	for _, node := range chain {
		var err error
		items, err = appendResponsesItems(items, node.NormalizedInput, true)
		if err != nil {
			return nil, fmt.Errorf("invalid stored Responses input: %w", err)
		}
		items, err = appendResponsesItems(items, node.NormalizedOutput, false)
		if err != nil {
			return nil, fmt.Errorf("invalid stored Responses output: %w", err)
		}
	}
	var err error
	items, err = appendResponsesItems(items, currentInput, true)
	if err != nil {
		return nil, fmt.Errorf("invalid current Responses input: %w", err)
	}
	replayed, err := common.Marshal(items)
	if err != nil {
		return nil, err
	}
	if len(replayed) > policy.MaxStateBytes {
		return nil, fmt.Errorf("replayed Responses history exceeds %d serialized bytes", policy.MaxStateBytes)
	}
	return replayed, nil
}

func appendResponsesItems(items []json.RawMessage, raw json.RawMessage, input bool) ([]json.RawMessage, error) {
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
		return append(items, append(json.RawMessage(nil), trimmed...)), nil
	default:
		return nil, fmt.Errorf("unsupported input JSON type %s", common.GetJsonType(trimmed))
	}
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
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return nil, fmt.Errorf("previous_response_id is empty")
	}
	identity := requestIdentity(c)
	stateCache, ownerCache, _ := protocolCaches()
	node, found, err := stateCache.Get(responseNodeKey(identity, publicID))
	if err != nil {
		return nil, fmt.Errorf("failed to load previous_response_id state: %w", err)
	}
	if !found {
		owner, ownerFound, ownerErr := ownerCache.Get(publicID)
		if ownerErr != nil {
			return nil, fmt.Errorf("failed to verify previous_response_id ownership: %w", ownerErr)
		}
		if ownerFound && owner != identity.String() {
			return nil, fmt.Errorf("previous_response_id belongs to a different user or API token")
		}
		return nil, fmt.Errorf("previous_response_id is unknown or expired")
	}
	if node.Version != stateVersion {
		return nil, fmt.Errorf("previous_response_id uses an unsupported state version")
	}
	if node.UserID != identity.userID || node.TokenID != identity.tokenID {
		return nil, fmt.Errorf("previous_response_id belongs to a different user or API token")
	}
	if strings.TrimSpace(publicModel) != strings.TrimSpace(node.PublicModel) {
		return nil, fmt.Errorf("previous_response_id model %q does not match requested model %q", node.PublicModel, publicModel)
	}
	policy := currentPolicy()
	if node.Turn < 1 || node.Turn > policy.MaxStateTurns {
		return nil, fmt.Errorf("previous_response_id exceeds the maximum conversation length")
	}
	if node.CumulativeStateBytes < 0 || node.CumulativeStateBytes > policy.MaxStateBytes {
		return nil, fmt.Errorf("previous_response_id exceeds the maximum serialized state size")
	}
	ttl := time.Duration(policy.StateTTLSeconds) * time.Second
	if err := stateCache.SetWithTTL(responseNodeKey(identity, publicID), node, ttl); err != nil {
		return nil, fmt.Errorf("failed to refresh previous_response_id state: %w", err)
	}
	if err := ownerCache.SetWithTTL(publicID, identity.String(), ttl); err != nil {
		return nil, fmt.Errorf("failed to refresh previous_response_id ownership: %w", err)
	}
	return cloneResponseNode(&node), nil
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
	if !Enabled() || c == nil {
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
	if !common.RedisEnabled && Enabled() && (os.Getenv("NODE_TYPE") != "" || os.Getenv("NODE_NAME") != "") {
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
