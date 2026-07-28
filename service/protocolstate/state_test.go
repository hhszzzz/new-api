package protocolstate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesStateSeparatesPublicAndUpstreamIDsAndReplaysHistory(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("root", 11, 22)
	rootRequest := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, []map[string]any{{"role": "user", "content": "first"}}),
	}
	rootInfo := protocolStateRelayInfo("gpt-public", 7)
	nativePlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	require.NoError(t, PrepareResponsesRequest(rootContext, rootInfo, nativePlan, rootRequest))
	rootResponse := &dto.OpenAIResponsesResponse{
		ID:     "upstream_resp_1",
		Model:  "provider-model",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  true,
		Output: []dto.ResponsesOutput{{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []dto.ResponsesOutputContent{
				{Type: "output_text", Text: "answer"},
			},
		}},
	}
	publicID := CaptureResponsesResponse(rootContext, rootResponse.ID, rootResponse)
	assert.NotEqual(t, "upstream_resp_1", publicID)
	assert.Equal(t, publicID, rootResponse.ID)
	assert.Equal(t, "gpt-public", rootResponse.Model)
	require.NoError(t, Commit(rootContext))

	continuationContext := protocolStateTestContext("continuation", 11, 22)
	continuationBody := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": publicID,
		"input":                []map[string]any{{"role": "user", "content": "second"}},
	})
	binding, err := ResolveSelectionBinding(continuationContext, "/v1/responses", "gpt-public", continuationBody)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 7, binding.ChannelID)

	nativeContinuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, []map[string]any{{"role": "user", "content": "second"}}),
	}
	require.NoError(t, PrepareResponsesRequest(continuationContext, protocolStateRelayInfo("gpt-public", 7), nativePlan, nativeContinuation))
	assert.Equal(t, "upstream_resp_1", nativeContinuation.PreviousResponseID)

	replayContext := protocolStateTestContext("replay", 11, 22)
	_, err = ResolveSelectionBinding(replayContext, "/v1/responses", "gpt-public", continuationBody)
	require.NoError(t, err)
	replayRequest := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, []map[string]any{{"role": "user", "content": "second"}}),
	}
	chatPlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		Status:           channelcompat.StatusConvertible,
	}
	require.NoError(t, PrepareResponsesRequest(replayContext, protocolStateRelayInfo("gpt-public", 9), chatPlan, replayRequest))
	assert.Empty(t, replayRequest.PreviousResponseID)
	var replayed []map[string]any
	require.NoError(t, common.Unmarshal(replayRequest.Input, &replayed))
	require.Len(t, replayed, 3)
	assert.Equal(t, "first", replayed[0]["content"])
	assert.Equal(t, "message", replayed[1]["type"])
	assert.Equal(t, "second", replayed[2]["content"])
}

func TestResponsesStateReplaysUnstoredNativeResponse(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("unstored-root", 61, 62)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	rootRequest := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, "remember this"),
	}
	require.NoError(t, PrepareResponsesRequest(rootContext, protocolStateRelayInfo("gpt-public", 63), plan, rootRequest))
	rootResponse := &dto.OpenAIResponsesResponse{
		ID:     "upstream_unstored",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  false,
		Output: []dto.ResponsesOutput{{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []dto.ResponsesOutputContent{{
				Type: "output_text",
				Text: "acknowledged",
			}},
		}},
	}
	publicID := CaptureResponsesResponse(rootContext, rootResponse.ID, rootResponse)
	require.NoError(t, Commit(rootContext))

	continuationContext := protocolStateTestContext("unstored-next", 61, 62)
	body := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": publicID,
	})
	_, err := ResolveSelectionBinding(continuationContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	continuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "what did I say?"),
	}
	require.NoError(t, PrepareResponsesRequest(continuationContext, protocolStateRelayInfo("gpt-public", 63), plan, continuation))
	assert.Empty(t, continuation.PreviousResponseID)
	var replayed []map[string]any
	require.NoError(t, common.Unmarshal(continuation.Input, &replayed))
	require.Len(t, replayed, 3)
	assert.Equal(t, "remember this", replayed[0]["content"])
	assert.Equal(t, "message", replayed[1]["type"])
	assert.Equal(t, "what did I say?", replayed[2]["content"])
}

func TestResponsesContinuationFallsBackWhenUpstreamDoesNotAcknowledgeIt(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("ack-root", 64, 65)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	rootRequest := &dto.OpenAIResponsesRequest{Model: "gpt-public", Input: mustProtocolStateJSON(t, "first")}
	require.NoError(t, PrepareResponsesRequest(rootContext, protocolStateRelayInfo("gpt-public", 66), plan, rootRequest))
	rootResponse := &dto.OpenAIResponsesResponse{
		ID:     "upstream_ack_root",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  true,
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant", Status: "completed"}},
	}
	publicID := CaptureResponsesResponse(rootContext, rootResponse.ID, rootResponse)
	require.NoError(t, Commit(rootContext))

	continuationContext := protocolStateTestContext("ack-next", 64, 65)
	body := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": publicID,
	})
	_, err := ResolveSelectionBinding(continuationContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	continuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "second"),
	}
	require.NoError(t, PrepareResponsesRequest(continuationContext, protocolStateRelayInfo("gpt-public", 66), plan, continuation))
	assert.Equal(t, "upstream_ack_root", continuation.PreviousResponseID)
	require.NoError(t, ValidateResponsesContinuation(continuationContext, mustProtocolStateJSON(t, "upstream_ack_root")))
	// Later stream events may carry a partial response object without repeating
	// previous_response_id; one valid acknowledgement is sufficient.
	require.NoError(t, ValidateResponsesContinuation(continuationContext, mustProtocolStateJSON(t, nil)))

	rejectedContext := protocolStateTestContext("ack-rejected", 64, 65)
	_, err = ResolveSelectionBinding(rejectedContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	rejected := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "second"),
	}
	require.NoError(t, PrepareResponsesRequest(rejectedContext, protocolStateRelayInfo("gpt-public", 66), plan, rejected))
	acknowledgementErr := ValidateResponsesContinuation(rejectedContext, mustProtocolStateJSON(t, nil))
	require.Error(t, acknowledgementErr)
	assert.Contains(t, acknowledgementErr.Error(), "did not acknowledge")
	apiErr := types.NewErrorWithStatusCode(acknowledgementErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	assert.True(t, EnableReplayFallback(rejectedContext, apiErr))

	replayed := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "second"),
	}
	require.NoError(t, PrepareResponsesRequest(rejectedContext, protocolStateRelayInfo("gpt-public", 66), plan, replayed))
	assert.Empty(t, replayed.PreviousResponseID)
}

func TestResponsesStatePreservesPhaseAndUnmodeledOutputFields(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("raw-output", 21, 22)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, []map[string]any{{
			"role":    "user",
			"content": "first",
		}}),
	}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-public", 31), plan, request))

	rawResponse := mustProtocolStateJSON(t, map[string]any{
		"id":       "resp_upstream_raw",
		"object":   "response",
		"model":    "provider-model",
		"status":   "completed",
		"provider": "kept-top-level",
		"output": []map[string]any{{
			"id":                 "msg_raw",
			"type":               "message",
			"role":               "assistant",
			"status":             "completed",
			"phase":              "commentary",
			"provider_extension": map[string]any{"opaque": true},
			"content": []map[string]any{{
				"type":        "output_text",
				"text":        "working",
				"annotations": []any{},
			}},
		}},
	})
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(rawResponse, &response))
	rewritten, err := CaptureResponsesResponseData(ctx, response.ID, &response, rawResponse)
	require.NoError(t, err)

	var public map[string]any
	require.NoError(t, common.Unmarshal(rewritten, &public))
	assert.Equal(t, PublicResponseID(ctx, ""), public["id"])
	assert.Equal(t, "gpt-public", public["model"])
	assert.Equal(t, "kept-top-level", public["provider"])
	publicOutput := public["output"].([]any)[0].(map[string]any)
	assert.Equal(t, "commentary", publicOutput["phase"])
	assert.Equal(t, true, publicOutput["provider_extension"].(map[string]any)["opaque"])
	require.NoError(t, Commit(ctx))

	replayContext := protocolStateTestContext("raw-output-replay", 21, 22)
	continuationBody := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": PublicResponseID(ctx, ""),
	})
	_, err = ResolveSelectionBinding(replayContext, "/v1/responses", "gpt-public", continuationBody)
	require.NoError(t, err)
	replayRequest := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: PublicResponseID(ctx, ""),
		Input:              mustProtocolStateJSON(t, "next"),
	}
	replayPlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		Status:           channelcompat.StatusConvertible,
	}
	require.NoError(t, PrepareResponsesRequest(replayContext, protocolStateRelayInfo("gpt-public", 32), replayPlan, replayRequest))

	var history []map[string]any
	require.NoError(t, common.Unmarshal(replayRequest.Input, &history))
	require.Len(t, history, 3)
	assert.Equal(t, "commentary", history[1]["phase"])
	assert.Equal(t, true, history[1]["provider_extension"].(map[string]any)["opaque"])
}

func TestResponsesStatePreservesStreamItemsWhenTerminalEventOmitsOutput(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("raw-stream", 41, 42)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-public", Input: mustProtocolStateJSON(t, "first")}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-public", 51), plan, request))

	itemData := mustProtocolStateJSON(t, map[string]any{
		"type":         dto.ResponsesOutputTypeItemDone,
		"output_index": 0,
		"item": map[string]any{
			"id":                 "msg_stream",
			"type":               "message",
			"role":               "assistant",
			"status":             "completed",
			"phase":              "final_answer",
			"provider_extension": "kept-stream-item",
			"content": []map[string]any{{
				"type":        "output_text",
				"text":        "answer",
				"annotations": []any{},
			}},
		},
	})
	var itemEvent dto.ResponsesStreamResponse
	require.NoError(t, common.Unmarshal(itemData, &itemEvent))
	_, err := ObserveResponsesStreamData(ctx, &itemEvent, itemData)
	require.NoError(t, err)

	terminalData := mustProtocolStateJSON(t, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":       "resp_upstream_stream",
			"object":   "response",
			"model":    "provider-model",
			"status":   "completed",
			"provider": "kept-terminal",
		},
	})
	var terminalEvent dto.ResponsesStreamResponse
	require.NoError(t, common.Unmarshal(terminalData, &terminalEvent))
	rewritten, err := ObserveResponsesStreamData(ctx, &terminalEvent, terminalData)
	require.NoError(t, err)
	require.NoError(t, Commit(ctx))

	var terminal map[string]any
	require.NoError(t, common.Unmarshal(rewritten, &terminal))
	response := terminal["response"].(map[string]any)
	assert.Equal(t, PublicResponseID(ctx, ""), response["id"])
	assert.Equal(t, "gpt-public", response["model"])
	assert.Equal(t, "kept-terminal", response["provider"])
	output := response["output"].([]any)
	require.Len(t, output, 1)
	assert.Equal(t, "final_answer", output[0].(map[string]any)["phase"])
	assert.Equal(t, "kept-stream-item", output[0].(map[string]any)["provider_extension"])
}

func TestResponsesStateRejectsCrossTokenModelAndOversizedInput(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("root-ownership", 1, 2)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "hello")}
	plan := channelcompat.ProtocolPlan{RequestProtocol: channelcompat.ProtocolResponses, UpstreamProtocol: channelcompat.ProtocolResponses, Status: channelcompat.StatusNative}
	require.NoError(t, PrepareResponsesRequest(rootContext, protocolStateRelayInfo("gpt-a", 3), plan, request))
	response := &dto.OpenAIResponsesResponse{
		ID:     "upstream-ownership",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant"}},
	}
	publicID := CaptureResponsesResponse(rootContext, response.ID, response)
	require.NoError(t, Commit(rootContext))
	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})

	_, err := ResolveSelectionBinding(protocolStateTestContext("cross-token", 1, 99), "/v1/responses", "gpt-a", body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different user or API token")

	_, err = ResolveSelectionBinding(protocolStateTestContext("wrong-model", 1, 2), "/v1/responses", "gpt-b", body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")

	policy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	oversized := make([]byte, policy.MaxStateBytes+1)
	for index := range oversized {
		oversized[index] = 'x'
	}
	oversizedRequest := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, string(oversized))}
	err = PrepareResponsesRequest(protocolStateTestContext("oversized", 1, 2), protocolStateRelayInfo("gpt-a", 3), plan, oversizedRequest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum serialized state size")
}

func TestResponsesStateDoesNotPersistIncompleteStreamAndExpiresDeterministically(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("incomplete", 4, 5)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "hello")}
	plan := channelcompat.ProtocolPlan{RequestProtocol: channelcompat.ProtocolResponses, UpstreamProtocol: channelcompat.ProtocolResponses, Status: channelcompat.StatusNative}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-a", 3), plan, request))
	event := &dto.ResponsesStreamResponse{
		Type: "response.incomplete",
		Response: &dto.OpenAIResponsesResponse{
			ID:     "upstream-incomplete",
			Status: mustProtocolStateJSON(t, "incomplete"),
		},
	}
	ObserveResponsesStream(ctx, event)
	publicID := PublicResponseID(ctx, "")
	require.NoError(t, Commit(ctx))
	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	_, err := ResolveSelectionBinding(protocolStateTestContext("incomplete-next", 4, 5), "/v1/responses", "gpt-a", body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown or expired")

	identity := identity{userID: 4, tokenID: 5}
	stateCache, ownerCache, _ := protocolCaches()
	expiredID := "resp_expired"
	node := ResponseNode{Version: stateVersion, UserID: 4, TokenID: 5, PublicResponseID: expiredID, PublicModel: "gpt-a", Turn: 1}
	require.NoError(t, stateCache.SetWithTTL(responseNodeKey(identity, expiredID), node, -time.Second))
	require.NoError(t, ownerCache.SetWithTTL(expiredID, identity.String(), -time.Second))
	_, err = loadResponseNode(protocolStateTestContext("expired", 4, 5), expiredID, "gpt-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown or expired")
}

func TestResponsesStateDoesNotPersistErrorStream(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("error", 44, 45)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "hello")}
	plan := channelcompat.ProtocolPlan{RequestProtocol: channelcompat.ProtocolResponses, UpstreamProtocol: channelcompat.ProtocolResponses, Status: channelcompat.StatusNative}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-a", 3), plan, request))

	ObserveResponsesStream(ctx, &dto.ResponsesStreamResponse{
		Type:    "error",
		Code:    "server_error",
		Message: "provider failed",
	})
	publicID := PublicResponseID(ctx, "")
	require.NoError(t, Commit(ctx))

	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	_, err := ResolveSelectionBinding(protocolStateTestContext("error-next", 44, 45), "/v1/responses", "gpt-a", body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown or expired")
}

func TestResponsesStatePersistsCompletedNonStreamResponseAfterClientCancellation(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("completed-non-stream", 14, 15)
	requestContext, cancel := context.WithCancel(ctx.Request.Context())
	ctx.Request = ctx.Request.WithContext(requestContext)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "hello")}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	relayInfo := protocolStateRelayInfo("gpt-a", 3)
	relayInfo.IsStream = false
	require.NoError(t, PrepareResponsesRequest(ctx, relayInfo, plan, request))
	response := &dto.OpenAIResponsesResponse{
		ID:     "upstream-completed-non-stream",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant"}},
	}
	publicID := CaptureResponsesResponse(ctx, response.ID, response)
	cancel()
	require.NoError(t, Commit(ctx))

	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	binding, err := ResolveSelectionBinding(protocolStateTestContext("completed-non-stream-next", 14, 15), "/v1/responses", "gpt-a", body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 3, binding.ChannelID)
}

func TestResponsesStateDoesNotPersistCancelledStream(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("cancelled-stream", 14, 15)
	requestContext, cancel := context.WithCancel(ctx.Request.Context())
	ctx.Request = ctx.Request.WithContext(requestContext)
	stream := true
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "hello"), Stream: &stream}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	relayInfo := protocolStateRelayInfo("gpt-a", 3)
	relayInfo.IsStream = true
	require.NoError(t, PrepareResponsesRequest(ctx, relayInfo, plan, request))
	response := &dto.OpenAIResponsesResponse{
		ID:     "upstream-cancelled-stream",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant"}},
	}
	publicID := CaptureResponsesResponse(ctx, response.ID, response)
	cancel()
	require.NoError(t, Commit(ctx))

	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	_, err := ResolveSelectionBinding(protocolStateTestContext("cancelled-stream-next", 14, 15), "/v1/responses", "gpt-a", body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown or expired")
}

func TestResponsesStateEnforcesMaximumTurnsBeforeRelay(t *testing.T) {
	resetProtocolStateCaches(t)
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.MaxStateTurns = 1
	rootContext := protocolStateTestContext("turn-root", 24, 25)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	rootRequest := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "first")}
	require.NoError(t, PrepareResponsesRequest(rootContext, protocolStateRelayInfo("gpt-a", 3), plan, rootRequest))
	rootResponse := &dto.OpenAIResponsesResponse{
		ID:     "upstream-turn-root",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant"}},
	}
	publicID := CaptureResponsesResponse(rootContext, rootResponse.ID, rootResponse)
	require.NoError(t, Commit(rootContext))

	continuationContext := protocolStateTestContext("turn-next", 24, 25)
	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	_, err := ResolveSelectionBinding(continuationContext, "/v1/responses", "gpt-a", body)
	require.NoError(t, err)
	continuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-a",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "second"),
	}
	err = PrepareResponsesRequest(continuationContext, protocolStateRelayInfo("gpt-a", 3), plan, continuation)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum conversation length of 1 turns")
}

func TestResponsesStateIsSharedThroughRedisAcrossCacheInstances(t *testing.T) {
	resetProtocolStateCaches(t)
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	originalRedis := common.RDB
	common.RDB = redisClient
	common.RedisEnabled = true
	t.Cleanup(func() { common.RDB = originalRedis })

	cacheMu.Lock()
	responseStateCache = nil
	responseOwnerCache = nil
	messageStateCache = nil
	cacheMu.Unlock()

	rootContext := protocolStateTestContext("redis-root", 34, 35)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "first")}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	require.NoError(t, PrepareResponsesRequest(rootContext, protocolStateRelayInfo("gpt-a", 43), plan, request))
	response := &dto.OpenAIResponsesResponse{
		ID:     "upstream-redis-root",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant"}},
	}
	publicID := CaptureResponsesResponse(rootContext, response.ID, response)
	require.NoError(t, Commit(rootContext))

	// Recreate every cache instance to model a request landing on another node.
	cacheMu.Lock()
	responseStateCache = nil
	responseOwnerCache = nil
	messageStateCache = nil
	cacheMu.Unlock()

	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	binding, err := ResolveSelectionBinding(protocolStateTestContext("redis-next", 34, 35), "/v1/responses", "gpt-a", body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 43, binding.ChannelID)
}

func TestResponsesStateTTLRefreshesWhileConversationIsActive(t *testing.T) {
	resetProtocolStateCaches(t)
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	originalRedis := common.RDB
	common.RDB = redisClient
	common.RedisEnabled = true
	t.Cleanup(func() { common.RDB = originalRedis })
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.StateTTLSeconds = 10

	cacheMu.Lock()
	responseStateCache = nil
	responseOwnerCache = nil
	messageStateCache = nil
	cacheMu.Unlock()

	rootContext := protocolStateTestContext("ttl-root", 44, 45)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "first")}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	require.NoError(t, PrepareResponsesRequest(rootContext, protocolStateRelayInfo("gpt-a", 46), plan, request))
	response := &dto.OpenAIResponsesResponse{
		ID:     "upstream-ttl-root",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant"}},
	}
	publicID := CaptureResponsesResponse(rootContext, response.ID, response)
	require.NoError(t, Commit(rootContext))

	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	redisServer.FastForward(6 * time.Second)
	binding, err := ResolveSelectionBinding(protocolStateTestContext("ttl-active-1", 44, 45), "/v1/responses", "gpt-a", body)
	require.NoError(t, err)
	require.NotNil(t, binding)

	redisServer.FastForward(6 * time.Second)
	binding, err = ResolveSelectionBinding(protocolStateTestContext("ttl-active-2", 44, 45), "/v1/responses", "gpt-a", body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 46, binding.ChannelID)

	redisServer.FastForward(11 * time.Second)
	_, err = ResolveSelectionBinding(protocolStateTestContext("ttl-expired", 44, 45), "/v1/responses", "gpt-a", body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown or expired")
}

func TestClaudeCodeResponsesContinuationRequiresStrictAppend(t *testing.T) {
	resetProtocolStateCaches(t)
	initialContext := protocolStateTestContext("claude-root", 8, 9)
	initialRequest := claudeSessionRequest("hello")
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
	}
	require.NoError(t, PrepareMessagesRequest(initialContext, protocolStateRelayInfo("claude-public", 12), plan, initialRequest))
	upstream := &dto.OpenAIResponsesResponse{ID: "resp_upstream_claude", Status: mustProtocolStateJSON(t, "completed"), Store: true}
	assistantText := "answer"
	claudeResponse := &dto.ClaudeResponse{
		Type: "message",
		Content: []dto.ClaudeMediaMessage{
			{Type: "text", Text: &assistantText},
		},
	}
	CaptureMessagesResponse(initialContext, upstream, claudeResponse)
	require.NoError(t, Commit(initialContext))
	initialSelection, ok := common.GetContextKeyType[*messageSelection](initialContext, constant.ContextKeyProtocolStateSession)
	require.True(t, ok)
	require.NotNil(t, initialSelection)
	_, _, messageCache := protocolCaches()
	storedSession, found, err := messageCache.Get(messageSessionKey(identity{userID: 8, tokenID: 9}, initialSelection.key, "claude-public"))
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, storedSession.History, 2)

	nextRequest := claudeSessionRequest("hello")
	nextRequest.Messages = append(nextRequest.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &assistantText}}},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	nextBody := mustProtocolStateJSON(t, nextRequest)
	nextContext := protocolStateTestContext("claude-next", 8, 9)
	var parsedNext dto.ClaudeRequest
	require.NoError(t, common.Unmarshal(nextBody, &parsedNext))
	nextSelection, err := buildMessageSelection(nextContext, "claude-public", &parsedNext)
	require.NoError(t, err)
	require.NotNil(t, nextSelection)
	assert.Equal(t, initialSelection.key, nextSelection.key)
	require.NotNil(t, nextSelection.session)
	assert.True(t, nextSelection.strictAppend, "stored=%q current=%q", storedSession.History, nextSelection.currentHistory)
	binding, err := ResolveSelectionBinding(nextContext, "/v1/messages", "claude-public", nextBody)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 12, binding.ChannelID)

	outbound := claudeSessionRequest("hello")
	outbound.Messages = append(outbound.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &assistantText}}},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	require.NoError(t, PrepareMessagesRequest(nextContext, protocolStateRelayInfo("claude-public", 12), plan, outbound))
	require.Len(t, outbound.Messages, 1)
	assert.Equal(t, "next", outbound.Messages[0].GetStringContent())
	responsesRequest := &dto.OpenAIResponsesRequest{}
	ApplyMessagesContinuation(nextContext, responsesRequest)
	assert.Equal(t, "resp_upstream_claude", responsesRequest.PreviousResponseID)

	retryError := types.NewErrorWithStatusCode(fmt.Errorf("previous_response_id not found"), types.ErrorCodeInvalidRequest, 404)
	assert.True(t, EnableReplayFallback(nextContext, retryError))
	replayedOutbound := claudeSessionRequest("hello")
	replayedOutbound.Messages = append(replayedOutbound.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &assistantText}}},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	require.NoError(t, PrepareMessagesRequest(nextContext, protocolStateRelayInfo("claude-public", 12), plan, replayedOutbound))
	assert.Len(t, replayedOutbound.Messages, 3)
	responsesRequest = &dto.OpenAIResponsesRequest{}
	ApplyMessagesContinuation(nextContext, responsesRequest)
	assert.Empty(t, responsesRequest.PreviousResponseID)

	nonAppend := claudeSessionRequest("different")
	nonAppendContext := protocolStateTestContext("claude-nonappend", 8, 9)
	binding, err = ResolveSelectionBinding(nonAppendContext, "/v1/messages", "claude-public", mustProtocolStateJSON(t, nonAppend))
	require.NoError(t, err)
	assert.Nil(t, binding)
}

func TestClaudeCodeResponsesReplaysWhenUpstreamResponseWasNotStored(t *testing.T) {
	resetProtocolStateCaches(t)
	initialContext := protocolStateTestContext("claude-unstored-root", 71, 72)
	initialRequest := claudeSessionRequest("hello")
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
	}
	require.NoError(t, PrepareMessagesRequest(initialContext, protocolStateRelayInfo("claude-public", 73), plan, initialRequest))
	answer := "answer"
	CaptureMessagesResponse(initialContext,
		&dto.OpenAIResponsesResponse{
			ID:     "resp_upstream_claude_unstored",
			Status: mustProtocolStateJSON(t, "completed"),
			Store:  false,
		},
		&dto.ClaudeResponse{Type: "message", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &answer}}},
	)
	require.NoError(t, Commit(initialContext))

	nextRequest := claudeSessionRequest("hello")
	nextRequest.Messages = append(nextRequest.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &answer}}},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	nextContext := protocolStateTestContext("claude-unstored-next", 71, 72)
	require.NoError(t, PrepareMessagesRequest(nextContext, protocolStateRelayInfo("claude-public", 73), plan, nextRequest))
	assert.Len(t, nextRequest.Messages, 3)
	responsesRequest := &dto.OpenAIResponsesRequest{}
	ApplyMessagesContinuation(nextContext, responsesRequest)
	assert.Empty(t, responsesRequest.PreviousResponseID)
}

func TestClaudeCodeResponsesSessionTTLRefreshesWhileActive(t *testing.T) {
	resetProtocolStateCaches(t)
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	originalRedis := common.RDB
	common.RDB = redisClient
	common.RedisEnabled = true
	t.Cleanup(func() { common.RDB = originalRedis })
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.StateTTLSeconds = 10

	cacheMu.Lock()
	responseStateCache = nil
	responseOwnerCache = nil
	messageStateCache = nil
	cacheMu.Unlock()

	initialContext := protocolStateTestContext("claude-ttl-root", 18, 19)
	initialRequest := claudeSessionRequest("hello")
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
	}
	require.NoError(t, PrepareMessagesRequest(initialContext, protocolStateRelayInfo("claude-public", 20), plan, initialRequest))
	answer := "answer"
	CaptureMessagesResponse(initialContext,
		&dto.OpenAIResponsesResponse{ID: "resp_upstream_claude_ttl", Status: mustProtocolStateJSON(t, "completed")},
		&dto.ClaudeResponse{Type: "message", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &answer}}},
	)
	require.NoError(t, Commit(initialContext))

	continuation := claudeSessionRequest("hello")
	continuation.Messages = append(continuation.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &answer}}},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	body := mustProtocolStateJSON(t, continuation)

	redisServer.FastForward(6 * time.Second)
	binding, err := ResolveSelectionBinding(protocolStateTestContext("claude-ttl-active-1", 18, 19), "/v1/messages", "claude-public", body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 20, binding.ChannelID)

	redisServer.FastForward(6 * time.Second)
	binding, err = ResolveSelectionBinding(protocolStateTestContext("claude-ttl-active-2", 18, 19), "/v1/messages", "claude-public", body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 20, binding.ChannelID)

	redisServer.FastForward(11 * time.Second)
	binding, err = ResolveSelectionBinding(protocolStateTestContext("claude-ttl-expired", 18, 19), "/v1/messages", "claude-public", body)
	require.NoError(t, err)
	assert.Nil(t, binding)
}

func TestPrepareMessagesRequestClearsFailedResponsesAttemptBeforeProtocolRetry(t *testing.T) {
	resetProtocolStateCaches(t)
	context := protocolStateTestContext("claude-retry", 8, 9)
	request := claudeSessionRequest("hello")
	responsesPlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
	}
	relayInfo := protocolStateRelayInfo("claude-public", 12)

	require.NoError(t, PrepareMessagesRequest(context, relayInfo, responsesPlan, request))
	require.NotNil(t, getPending(context, pendingMessages))

	chatPlan := responsesPlan
	chatPlan.UpstreamProtocol = channelcompat.ProtocolChat
	require.NoError(t, PrepareMessagesRequest(context, relayInfo, chatPlan, claudeSessionRequest("hello")))
	assert.Nil(t, getPending(context, ""))
}

func TestReplayFallbackRequiresExplicitContinuationErrorBeforeOutput(t *testing.T) {
	resetProtocolStateCaches(t)

	unrelatedContext := protocolStateTestContext("unrelated-404", 8, 9)
	common.SetContextKey(unrelatedContext, constant.ContextKeyProtocolStatePending, &pendingState{usedContinuation: true})
	unrelatedError := types.NewErrorWithStatusCode(fmt.Errorf("model not found"), types.ErrorCodeInvalidRequest, 404)
	assert.False(t, EnableReplayFallback(unrelatedContext, unrelatedError))

	writtenRecorder := httptest.NewRecorder()
	writtenContext, _ := gin.CreateTestContext(writtenRecorder)
	writtenContext.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	common.SetContextKey(writtenContext, constant.ContextKeyProtocolStatePending, &pendingState{usedContinuation: true})
	_, err := writtenContext.Writer.Write([]byte("partial"))
	require.NoError(t, err)
	continuationError := types.NewErrorWithStatusCode(fmt.Errorf("previous_response_id not found"), types.ErrorCodeInvalidRequest, 404)
	assert.False(t, EnableReplayFallback(writtenContext, continuationError))
}

func protocolStateTestContext(requestID string, userID, tokenID int) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	context.Set(common.RequestIdKey, requestID)
	common.SetContextKey(context, constant.ContextKeyUserId, userID)
	common.SetContextKey(context, constant.ContextKeyTokenId, tokenID)
	return context
}

func protocolStateRelayInfo(model string, channelID int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
	}
}

func claudeSessionRequest(text string) *dto.ClaudeRequest {
	systemText := "stable system"
	return &dto.ClaudeRequest{
		Model: "claude-public",
		System: []dto.ClaudeMediaMessage{
			{Type: "text", Text: &systemText, CacheControl: jsonRaw(`{"type":"ephemeral"}`)},
		},
		Messages: []dto.ClaudeMessage{{Role: "user", Content: text}},
	}
}

func jsonRaw(value string) []byte {
	return []byte(value)
}

func mustProtocolStateJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	return encoded
}

func resetProtocolStateCaches(t *testing.T) {
	t.Helper()
	oldRedisEnabled := common.RedisEnabled
	oldPolicy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	common.RedisEnabled = false
	model_setting.GetGlobalSettings().ProtocolBridgePolicy = model_setting.ProtocolBridgePolicy{
		Enabled:                true,
		DefaultAllowConversion: true,
		StateTTLSeconds:        model_setting.DefaultProtocolBridgeStateTTLSeconds,
		MaxStateTurns:          model_setting.DefaultProtocolBridgeMaxStateTurns,
		MaxStateBytes:          model_setting.DefaultProtocolBridgeMaxStateBytes,
	}
	cacheMu.Lock()
	responseStateCache = nil
	responseOwnerCache = nil
	messageStateCache = nil
	warnNoRedisOnce = sync.Once{}
	cacheMu.Unlock()
	t.Cleanup(func() {
		cacheMu.Lock()
		responseStateCache = nil
		responseOwnerCache = nil
		messageStateCache = nil
		warnNoRedisOnce = sync.Once{}
		cacheMu.Unlock()
		common.RedisEnabled = oldRedisEnabled
		model_setting.GetGlobalSettings().ProtocolBridgePolicy = oldPolicy
	})
}
