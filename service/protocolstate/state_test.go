package protocolstate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
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
	assert.Equal(t, channelcompat.ProtocolResponses, binding.UpstreamProtocol)

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
		"id":                   "resp_upstream_raw",
		"object":               "response",
		"model":                "provider-model",
		"status":               "completed",
		"previous_response_id": "resp_provider_parent",
		"provider":             "kept-top-level",
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
	assert.Nil(t, public["previous_response_id"])
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

func TestResponsesStateDropsProviderStateWhenReplayingAcrossResponsesUpstreams(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("cross-provider-root", 121, 122)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	request := &dto.OpenAIResponsesRequest{Model: "gpt-public", Input: mustProtocolStateJSON(t, "first")}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-public", 123), plan, request))

	rawResponse := mustProtocolStateJSON(t, map[string]any{
		"id":     "resp_provider_a",
		"model":  "provider-a",
		"status": "completed",
		"store":  false,
		"output": []map[string]any{
			{
				"id":                 "msg_provider_a",
				"type":               "message",
				"role":               "assistant",
				"status":             "completed",
				"phase":              "commentary",
				"provider_extension": map[string]any{"opaque": true},
				"content": []map[string]any{{
					"type":        "output_text",
					"text":        "working",
					"annotations": []any{},
					"vendor_data": "drop-me",
				}},
			},
			{
				"id":                "rs_provider_a",
				"type":              "reasoning",
				"encrypted_content": "provider-secret",
				"summary":           []map[string]any{{"type": "summary_text", "text": "hidden"}},
			},
			{"id": "ws_provider_a", "type": "web_search_call", "status": "completed"},
		},
	})
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(rawResponse, &response))
	_, err := CaptureResponsesResponseData(ctx, response.ID, &response, rawResponse)
	require.NoError(t, err)
	require.NoError(t, Commit(ctx))

	next := protocolStateTestContext("cross-provider-next", 121, 122)
	body := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": PublicResponseID(ctx, ""),
	})
	_, err = ResolveSelectionBinding(next, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	continuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: PublicResponseID(ctx, ""),
		Input: mustProtocolStateJSON(t, []map[string]any{
			{
				"id":     "msg_gateway_0",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{{
					"type": "output_text",
					"text": "client-replayed assistant",
				}},
			},
			{
				"id":        "fc_gateway_0",
				"type":      "function_call",
				"call_id":   "call_exec",
				"name":      "exec",
				"arguments": `{}`,
			},
			{
				"id":      "fco_gateway_0",
				"type":    "function_call_output",
				"call_id": "call_exec",
				"output":  "ok",
			},
			{"role": "user", "content": "next"},
		}),
	}
	require.NoError(t, PrepareResponsesRequest(next, protocolStateRelayInfo("gpt-public", 124), plan, continuation))

	var replayed []map[string]any
	require.NoError(t, common.Unmarshal(continuation.Input, &replayed))
	require.Len(t, replayed, 6)
	message := replayed[1]
	assert.Equal(t, "message", message["type"])
	assert.Equal(t, "commentary", message["phase"])
	assert.NotContains(t, message, "id")
	assert.NotContains(t, message, "provider_extension")
	content := message["content"].([]any)[0].(map[string]any)
	assert.Equal(t, "input_text", content["type"])
	assert.Equal(t, "working", content["text"])
	assert.NotContains(t, content, "annotations")
	assert.NotContains(t, content, "vendor_data")
	encoded := string(continuation.Input)
	assert.NotContains(t, encoded, "provider-secret")
	assert.NotContains(t, encoded, "web_search_call")
	assert.NotContains(t, replayed[2], "id")
	clientContent := replayed[2]["content"].([]any)[0].(map[string]any)
	assert.Equal(t, "input_text", clientContent["type"])
	assert.NotContains(t, replayed[3], "id")
	assert.Equal(t, "call_exec", replayed[3]["call_id"])
	assert.NotContains(t, replayed[4], "id")
	assert.Equal(t, "call_exec", replayed[4]["call_id"])
}

func TestResponsesStateDoesNotReuseOpaqueStateAcrossMappedModels(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("mapped-model-root", 221, 222)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	rootInfo := protocolStateRelayInfo("gpt-public", 223)
	rootInfo.UpstreamModelName = "provider-model-a"
	rootRequest := &dto.OpenAIResponsesRequest{Model: "gpt-public", Input: mustProtocolStateJSON(t, "first")}
	require.NoError(t, PrepareResponsesRequest(rootContext, rootInfo, plan, rootRequest))
	rawResponse := mustProtocolStateJSON(t, map[string]any{
		"id":     "resp_provider_model_a",
		"model":  "provider-model-a",
		"status": "completed",
		"store":  true,
		"output": []map[string]any{
			{
				"id":                "rs_provider_model_a",
				"type":              "reasoning",
				"encrypted_content": "provider-model-a-secret",
				"summary":           []map[string]any{{"type": "summary_text", "text": "inspect"}},
			},
			{
				"id":     "msg_provider_model_a",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{{
					"type": "output_text",
					"text": "answer",
				}},
			},
		},
	})
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(rawResponse, &response))
	_, err := CaptureResponsesResponseData(rootContext, response.ID, &response, rawResponse)
	require.NoError(t, err)
	require.NoError(t, Commit(rootContext))

	nextContext := protocolStateTestContext("mapped-model-next", 221, 222)
	publicID := PublicResponseID(rootContext, "")
	body := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": publicID,
	})
	_, err = ResolveSelectionBinding(nextContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	nextInfo := protocolStateRelayInfo("gpt-public", 223)
	nextInfo.UpstreamModelName = "provider-model-b"
	continuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "next"),
	}
	require.NoError(t, PrepareResponsesRequest(nextContext, nextInfo, plan, continuation))

	assert.Empty(t, continuation.PreviousResponseID)
	assert.NotContains(t, string(continuation.Input), "provider-model-a-secret")
	var replayed []map[string]any
	require.NoError(t, common.Unmarshal(continuation.Input, &replayed))
	require.Len(t, replayed, 3)
	assert.Equal(t, "message", replayed[1]["type"])
}

func TestResponsesStateStripsAnthropicOpaqueStateAcrossMappedModels(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("anthropic-model-root", 224, 225)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolMessages,
		Status:           channelcompat.StatusConvertible,
	}
	rootInfo := protocolStateRelayInfo("gpt-public", 226)
	rootInfo.UpstreamModelName = "claude-model-a"
	rootRequest := &dto.OpenAIResponsesRequest{Model: "gpt-public", Input: mustProtocolStateJSON(t, "first")}
	require.NoError(t, PrepareResponsesRequest(rootContext, rootInfo, plan, rootRequest))
	response := &dto.OpenAIResponsesResponse{
		ID:     "resp_anthropic_model_a",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{
			ID:               "rs_anthropic_model_a",
			Type:             "reasoning",
			EncryptedContent: "newapi-anthropic-thinking-v1:model-a-only",
		}},
	}
	CaptureResponsesResponse(rootContext, response.ID, response)
	require.NoError(t, Commit(rootContext))

	nextContext := protocolStateTestContext("anthropic-model-next", 224, 225)
	publicID := PublicResponseID(rootContext, "")
	body := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": publicID,
	})
	_, err := ResolveSelectionBinding(nextContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	nextInfo := protocolStateRelayInfo("gpt-public", 226)
	nextInfo.UpstreamModelName = "claude-model-b"
	continuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "next"),
	}
	require.NoError(t, PrepareResponsesRequest(nextContext, nextInfo, plan, continuation))

	assert.NotContains(t, string(continuation.Input), "model-a-only")
	var replayed []map[string]any
	require.NoError(t, common.Unmarshal(continuation.Input, &replayed))
	require.Len(t, replayed, 3)
	assert.Equal(t, "reasoning", replayed[1]["type"])
	assert.NotContains(t, replayed[1], "encrypted_content")
}

func TestPrepareResponsesRequestRepairsLegacyItemIDsAndDeclaresHistoricalTools(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("repair-native-input", 131, 132)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, []map[string]any{
			{"type": "message", "id": "resp_legacy_msg_0", "role": "assistant", "content": "old"},
			{"type": "message", "id": "msg_valid", "role": "assistant", "content": "valid"},
			{"type": "custom_tool_call", "id": "resp_legacy_custom_0", "call_id": "call_exec", "name": "exec", "input": "pwd"},
			{"type": "custom_tool_call_output", "id": "resp_legacy_output_0", "call_id": "call_exec", "output": "ok"},
			{"type": "function_call", "id": "fc_valid", "call_id": "call_lookup", "name": "lookup", "arguments": `{}`},
		}),
	}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-public", 133), plan, request))

	var items []map[string]any
	require.NoError(t, common.Unmarshal(request.Input, &items))
	assert.NotContains(t, items[0], "id")
	assert.Equal(t, "msg_valid", items[1]["id"])
	assert.NotContains(t, items[2], "id")
	assert.NotContains(t, items[3], "id")
	assert.Equal(t, "fc_valid", items[4]["id"])

	var tools []map[string]any
	require.NoError(t, common.Unmarshal(request.Tools, &tools))
	require.Len(t, tools, 2)
	assert.Equal(t, "custom", tools[0]["type"])
	assert.Equal(t, "exec", tools[0]["name"])
	assert.Equal(t, "function", tools[1]["type"])
	assert.Equal(t, "lookup", tools[1]["name"])
}

func TestPrepareResponsesRequestNormalizesCompatibleToolDeclarationShapes(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("normalize-tool-shapes", 231, 232)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, "hello"),
		Tools: mustProtocolStateJSON(t, []any{
			"apply_patch",
			map[string]any{
				"type": "namespace",
				"name": "workspace",
				"children": []any{
					"shell",
					map[string]any{
						"type": "function",
						"function": map[string]any{
							"name":       "read_file",
							"parameters": map[string]any{"type": "object"},
						},
					},
				},
			},
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":       "lookup",
					"parameters": map[string]any{"type": "object"},
				},
				"strict": true,
			},
		}),
	}

	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-public", 233), plan, request))

	var tools []map[string]any
	require.NoError(t, common.Unmarshal(request.Tools, &tools))
	require.Len(t, tools, 3)
	assert.Equal(t, "custom", tools[0]["type"])
	assert.Equal(t, "apply_patch", tools[0]["name"])
	assert.Equal(t, "namespace", tools[1]["type"])
	assert.NotContains(t, tools[1], "children")
	children := tools[1]["tools"].([]any)
	require.Len(t, children, 2)
	assert.Equal(t, "custom", children[0].(map[string]any)["type"])
	assert.Equal(t, "shell", children[0].(map[string]any)["name"])
	assert.Equal(t, "function", children[1].(map[string]any)["type"])
	assert.Equal(t, "read_file", children[1].(map[string]any)["name"])
	assert.NotContains(t, children[1].(map[string]any), "function")
	assert.Equal(t, "function", tools[2]["type"])
	assert.Equal(t, "lookup", tools[2]["name"])
	assert.Equal(t, true, tools[2]["strict"])
	assert.NotContains(t, tools[2], "function")
}

func TestPrepareResponsesRequestRepairsLegacyIDsAndToolsDuringSameProviderReplay(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("same-provider-legacy-root", 134, 135)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	info := protocolStateRelayInfo("gpt-public", 136)
	rootRequest := &dto.OpenAIResponsesRequest{Model: "gpt-public", Input: mustProtocolStateJSON(t, "first")}
	require.NoError(t, PrepareResponsesRequest(rootContext, info, plan, rootRequest))

	rawResponse := mustProtocolStateJSON(t, map[string]any{
		"id":     "resp_upstream_legacy",
		"model":  "gpt-upstream",
		"status": "completed",
		"store":  false,
		"output": []map[string]any{
			{
				"type": "message", "id": "resp_202607281104531692329568268d9d6Z4V2JptQ_msg_0",
				"role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "working"}},
			},
			{
				"type": "function_call", "id": "resp_upstream_legacy_fc_0", "call_id": "call_exec",
				"name": "exec", "arguments": `{"command":"pwd"}`,
			},
		},
	})
	var response dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(rawResponse, &response))
	publicID := CaptureResponsesResponse(rootContext, response.ID, &response)
	require.NoError(t, Commit(rootContext))

	nextContext := protocolStateTestContext("same-provider-legacy-next", 134, 135)
	body := mustProtocolStateJSON(t, map[string]any{"model": "gpt-public", "previous_response_id": publicID})
	_, err := ResolveSelectionBinding(nextContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	nextRequest := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input: mustProtocolStateJSON(t, []map[string]any{
			{"type": "function_call_output", "call_id": "call_exec", "output": "ok"},
			{"role": "user", "content": "next"},
		}),
	}
	require.NoError(t, PrepareResponsesRequest(nextContext, info, plan, nextRequest))
	assert.Empty(t, nextRequest.PreviousResponseID)

	var replayed []map[string]any
	require.NoError(t, common.Unmarshal(nextRequest.Input, &replayed))
	for _, item := range replayed {
		id := strings.TrimSpace(common.Interface2String(item["id"]))
		assert.NotContains(t, id, "_msg_0")
		if item["type"] == "message" && item["role"] == "assistant" {
			assert.Empty(t, id)
		}
		if item["type"] == "function_call" {
			assert.Equal(t, "exec", item["name"])
			assert.Empty(t, id)
		}
	}

	var tools []map[string]any
	require.NoError(t, common.Unmarshal(nextRequest.Tools, &tools))
	require.Len(t, tools, 1)
	assert.Equal(t, "function", tools[0]["type"])
	assert.Equal(t, "exec", tools[0]["name"])
}

func TestPrepareResponsesRequestRestoresStoredToolDeclarations(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("tool-declaration-root", 141, 142)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	rootRequest := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, "first"),
		Tools: mustProtocolStateJSON(t, []map[string]any{{
			"type": "function",
			"name": "lookup",
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{"q": map[string]any{"type": "string"}},
			},
		}}),
	}
	require.NoError(t, PrepareResponsesRequest(rootContext, protocolStateRelayInfo("gpt-public", 143), plan, rootRequest))
	response := &dto.OpenAIResponsesResponse{
		ID:     "resp_tool_root",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  false,
		Output: []dto.ResponsesOutput{{
			Type:      "function_call",
			ID:        "fc_lookup",
			CallId:    "call_lookup",
			Name:      "lookup",
			Arguments: mustProtocolStateJSON(t, `{"q":"x"}`),
		}},
	}
	publicID := CaptureResponsesResponse(rootContext, response.ID, response)
	require.NoError(t, Commit(rootContext))

	nextContext := protocolStateTestContext("tool-declaration-next", 141, 142)
	body := mustProtocolStateJSON(t, map[string]any{"model": "gpt-public", "previous_response_id": publicID})
	_, err := ResolveSelectionBinding(nextContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	nextRequest := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input: mustProtocolStateJSON(t, []map[string]any{
			{"type": "function_call_output", "call_id": "call_lookup", "output": "done"},
			{"role": "user", "content": "next"},
		}),
	}
	require.NoError(t, PrepareResponsesRequest(nextContext, protocolStateRelayInfo("gpt-public", 144), plan, nextRequest))

	var tools []map[string]any
	require.NoError(t, common.Unmarshal(nextRequest.Tools, &tools))
	require.Len(t, tools, 1)
	assert.Equal(t, "lookup", tools[0]["name"])
	properties := tools[0]["parameters"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, properties, "q")
}

func TestMergeResponsesToolDeclarationsRejectsToolSearchExecutionConflicts(t *testing.T) {
	tests := []struct {
		name            string
		tools           []map[string]any
		inputExecution  string
		historicalTools []json.RawMessage
	}{
		{
			name:           "client declaration with server call",
			tools:          []map[string]any{{"type": "tool_search", "execution": "client"}},
			inputExecution: "server",
		},
		{
			name:           "server declaration with client call",
			tools:          []map[string]any{{"type": "tool_search", "execution": "server"}},
			inputExecution: "client",
		},
		{
			name:            "current and historical declarations disagree",
			tools:           []map[string]any{{"type": "tool_search", "execution": "client"}},
			historicalTools: []json.RawMessage{mustProtocolStateJSON(t, []map[string]any{{"type": "tool_search", "execution": "server"}})},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := &dto.OpenAIResponsesRequest{
				Tools: mustProtocolStateJSON(t, test.tools),
			}
			if test.inputExecution != "" {
				request.Input = mustProtocolStateJSON(t, []map[string]any{{
					"type":      "tool_search_call",
					"call_id":   "call_search",
					"execution": test.inputExecution,
				}})
			}

			_, err := mergeResponsesToolDeclarations(request, test.historicalTools)

			require.ErrorContains(t, err, "tool_search")
			require.ErrorContains(t, err, "execution")
		})
	}
}

func TestPrepareResponsesRequestRestoresStoredToolsForNativeContinuation(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("native-tool-root", 151, 152)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	rootRequest := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, "first"),
		Tools: mustProtocolStateJSON(t, []map[string]any{{
			"type": "function",
			"name": "exec",
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
			},
		}}),
	}
	relayInfo := protocolStateRelayInfo("gpt-public", 153)
	require.NoError(t, PrepareResponsesRequest(rootContext, relayInfo, plan, rootRequest))
	response := &dto.OpenAIResponsesResponse{
		ID:     "resp_native_tool_root",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  true,
		Output: []dto.ResponsesOutput{{
			Type:      "function_call",
			ID:        "fc_exec",
			CallId:    "call_exec",
			Name:      "exec",
			Arguments: mustProtocolStateJSON(t, `{"command":"pwd"}`),
		}},
	}
	publicID := CaptureResponsesResponse(rootContext, response.ID, response)
	require.NoError(t, Commit(rootContext))

	nextContext := protocolStateTestContext("native-tool-next", 151, 152)
	body := mustProtocolStateJSON(t, map[string]any{"model": "gpt-public", "previous_response_id": publicID})
	_, err := ResolveSelectionBinding(nextContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	nextRequest := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: publicID,
		Input: mustProtocolStateJSON(t, []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_exec",
			"output":  "ok",
		}}),
	}
	require.NoError(t, PrepareResponsesRequest(nextContext, relayInfo, plan, nextRequest))

	assert.Equal(t, "resp_native_tool_root", nextRequest.PreviousResponseID)
	var tools []map[string]any
	require.NoError(t, common.Unmarshal(nextRequest.Tools, &tools))
	require.Len(t, tools, 1)
	assert.Equal(t, "exec", tools[0]["name"])
	properties := tools[0]["parameters"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, properties, "command")
}

func TestResolveSelectionBindingIncludesStoredResponsesFeatures(t *testing.T) {
	resetProtocolStateCaches(t)
	rootContext := protocolStateTestContext("stored-features-root", 161, 162)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	rootRequest := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, "search"),
		Tools: mustProtocolStateJSON(t, []map[string]any{{
			"type": "web_search_preview",
		}}),
	}
	require.NoError(t, PrepareResponsesRequest(rootContext, protocolStateRelayInfo("gpt-public", 163), plan, rootRequest))
	response := &dto.OpenAIResponsesResponse{
		ID:     "resp_stored_features",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  true,
		Output: []dto.ResponsesOutput{{
			Type:   "web_search_call",
			ID:     "ws_stored_features",
			Status: "completed",
		}},
	}
	publicID := CaptureResponsesResponse(rootContext, response.ID, response)
	require.NoError(t, Commit(rootContext))

	nextContext := protocolStateTestContext("stored-features-next", 161, 162)
	body := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": publicID,
		"input":                "next",
	})
	binding, err := ResolveSelectionBinding(nextContext, "/v1/responses", "gpt-public", body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	features, ok := common.GetContextKeyType[channelcompat.RequestFeatureSet](nextContext, constant.ContextKeyRequestFeatureSet)
	require.True(t, ok)
	assert.Contains(t, features.DeclaredHostedTools, "web_search_preview")
	assert.Contains(t, features.HistoricalHostedTools, "web_search_call")
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
			"id":                   "resp_upstream_stream",
			"object":               "response",
			"model":                "provider-model",
			"status":               "completed",
			"previous_response_id": "resp_provider_parent",
			"provider":             "kept-terminal",
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
	assert.Nil(t, response["previous_response_id"])
	assert.Equal(t, "kept-terminal", response["provider"])
	output := response["output"].([]any)
	require.Len(t, output, 1)
	assert.Equal(t, "final_answer", output[0].(map[string]any)["phase"])
	assert.Equal(t, "kept-stream-item", output[0].(map[string]any)["provider_extension"])

	continuationContext := protocolStateTestContext("raw-stream-continuation", 41, 42)
	continuationBody := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-public",
		"previous_response_id": PublicResponseID(ctx, ""),
	})
	_, err = ResolveSelectionBinding(continuationContext, "/v1/responses", "gpt-public", continuationBody)
	require.NoError(t, err)
	continuationRequest := &dto.OpenAIResponsesRequest{
		Model:              "gpt-public",
		PreviousResponseID: PublicResponseID(ctx, ""),
		Input:              mustProtocolStateJSON(t, "next"),
		Stream:             common.GetPointer(true),
	}
	require.NoError(t, PrepareResponsesRequest(continuationContext, protocolStateRelayInfo("gpt-public", 51), plan, continuationRequest))
	continuationTerminalData := mustProtocolStateJSON(t, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":                   "resp_upstream_stream_next",
			"model":                "provider-model",
			"status":               "completed",
			"previous_response_id": "resp_upstream_stream",
			"output":               []any{},
		},
	})
	var continuationTerminalEvent dto.ResponsesStreamResponse
	require.NoError(t, common.Unmarshal(continuationTerminalData, &continuationTerminalEvent))
	rewrittenContinuation, err := ObserveResponsesStreamData(continuationContext, &continuationTerminalEvent, continuationTerminalData)
	require.NoError(t, err)
	var publicContinuation map[string]any
	require.NoError(t, common.Unmarshal(rewrittenContinuation, &publicContinuation))
	continuationResponse := publicContinuation["response"].(map[string]any)
	assert.Equal(t, PublicResponseID(ctx, ""), continuationResponse["previous_response_id"])
}

func TestResponsesStateMergesStreamItemsWithPartialTerminalOutput(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("partial-terminal-stream", 71, 72)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-public", Input: mustProtocolStateJSON(t, "first")}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-public", 73), plan, request))

	streamItems := []map[string]any{
		{
			"type":         dto.ResponsesOutputTypeItemDone,
			"output_index": 0,
			"item": map[string]any{
				"id":                 "msg_stream_0",
				"type":               "message",
				"role":               "assistant",
				"status":             "completed",
				"provider_extension": "keep-from-stream",
				"content": []map[string]any{{
					"type": "output_text",
					"text": "answer",
				}},
			},
		},
		{
			"type":         dto.ResponsesOutputTypeItemDone,
			"output_index": 1,
			"item": map[string]any{
				"id":        "fc_stream_1",
				"type":      "function_call",
				"status":    "completed",
				"call_id":   "call_lookup",
				"name":      "lookup",
				"arguments": `{}`,
			},
		},
	}
	for _, streamItem := range streamItems {
		data := mustProtocolStateJSON(t, streamItem)
		var event dto.ResponsesStreamResponse
		require.NoError(t, common.Unmarshal(data, &event))
		_, err := ObserveResponsesStreamData(ctx, &event, data)
		require.NoError(t, err)
	}

	terminalData := mustProtocolStateJSON(t, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     "resp_upstream_partial",
			"model":  "provider-model",
			"status": "completed",
			"output": []map[string]any{{
				"id":              "msg_stream_0",
				"type":            "message",
				"role":            "assistant",
				"status":          "completed",
				"terminal_marker": "keep-from-terminal",
				"content": []map[string]any{{
					"type": "output_text",
					"text": "answer",
				}},
			}},
		},
	})
	var terminalEvent dto.ResponsesStreamResponse
	require.NoError(t, common.Unmarshal(terminalData, &terminalEvent))
	rewritten, err := ObserveResponsesStreamData(ctx, &terminalEvent, terminalData)
	require.NoError(t, err)
	require.NoError(t, Commit(ctx))

	var terminal map[string]any
	require.NoError(t, common.Unmarshal(rewritten, &terminal))
	output := terminal["response"].(map[string]any)["output"].([]any)
	require.Len(t, output, 2)
	message := output[0].(map[string]any)
	assert.Equal(t, "msg_stream_0", message["id"])
	assert.Equal(t, "keep-from-stream", message["provider_extension"])
	assert.Equal(t, "keep-from-terminal", message["terminal_marker"])
	toolCall := output[1].(map[string]any)
	assert.Equal(t, "fc_stream_1", toolCall["id"])
	assert.Equal(t, "call_lookup", toolCall["call_id"])
}

func TestObserveResponsesStreamMergesTypedPartialTerminalOutput(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("typed-partial-terminal", 81, 82)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-public", Input: mustProtocolStateJSON(t, "first")}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-public", 83), plan, request))

	for index, item := range []dto.ResponsesOutput{
		{ID: "msg_typed_0", Type: "message", Role: "assistant", Status: "completed"},
		{ID: "fc_typed_1", Type: "function_call", CallId: "call_typed", Name: "lookup", Status: "completed"},
	} {
		outputIndex := index
		captured := item
		ObserveResponsesStream(ctx, &dto.ResponsesStreamResponse{
			Type:        dto.ResponsesOutputTypeItemDone,
			OutputIndex: &outputIndex,
			Item:        &captured,
		})
	}

	terminal := &dto.ResponsesStreamResponse{
		Type: "response.completed",
		Response: &dto.OpenAIResponsesResponse{
			ID:     "resp_typed_partial",
			Model:  "provider-model",
			Status: mustProtocolStateJSON(t, "completed"),
			Output: []dto.ResponsesOutput{{ID: "msg_typed_0", Type: "message", Role: "assistant", Status: "completed"}},
		},
	}
	ObserveResponsesStream(ctx, terminal)

	require.Len(t, terminal.Response.Output, 2)
	assert.Equal(t, "msg_typed_0", terminal.Response.Output[0].ID)
	assert.Equal(t, "fc_typed_1", terminal.Response.Output[1].ID)
	assert.Equal(t, "call_typed", terminal.Response.Output[1].CallId)
}

func TestResponsesStateRejectsCrossTokenAndModel(t *testing.T) {
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
}

func TestResponsesStateLimitsOnlyBridgeManagedRequests(t *testing.T) {
	resetProtocolStateCaches(t)
	policy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	policy.MaxStateBytes = 64
	policy.MaxStateTurns = 1
	model_setting.GetGlobalSettings().ProtocolBridgePolicy = policy

	oversizedInput := mustProtocolStateJSON(t, strings.Repeat("x", policy.MaxStateBytes))
	nativePlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	nativeContext := protocolStateTestContext("native-oversized", 31, 32)
	nativeRequest := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: oversizedInput}
	require.NoError(t, PrepareResponsesRequest(nativeContext, protocolStateRelayInfo("gpt-a", 33), nativePlan, nativeRequest))
	nativeResponse := &dto.OpenAIResponsesResponse{
		ID:     "upstream-native-oversized",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  true,
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant", Status: "completed"}},
	}
	publicID := CaptureResponsesResponse(nativeContext, nativeResponse.ID, nativeResponse)
	require.NoError(t, Commit(nativeContext))
	nativeNode, err := loadResponseNode(nativeContext, publicID, "gpt-a")
	require.NoError(t, err)
	assert.False(t, nativeNode.BridgeManaged)
	assert.Greater(t, nativeNode.CumulativeStateBytes, policy.MaxStateBytes)

	continuationBody := mustProtocolStateJSON(t, map[string]any{
		"model":                "gpt-a",
		"previous_response_id": publicID,
	})
	nativeContinuationContext := protocolStateTestContext("native-oversized-continuation", 31, 32)
	binding, err := ResolveSelectionBinding(nativeContinuationContext, "/v1/responses", "gpt-a", continuationBody)
	require.NoError(t, err)
	require.NotNil(t, binding)
	nativeContinuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-a",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "continue natively"),
	}
	require.NoError(t, PrepareResponsesRequest(nativeContinuationContext, protocolStateRelayInfo("gpt-a", 33), nativePlan, nativeContinuation))
	assert.Equal(t, "upstream-native-oversized", nativeContinuation.PreviousResponseID)

	bridgePlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		Status:           channelcompat.StatusConvertible,
		StateEnabled:     true,
	}
	bridgeRoot := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: oversizedInput}
	err = PrepareResponsesRequest(protocolStateTestContext("bridge-oversized", 31, 32), protocolStateRelayInfo("gpt-a", 34), bridgePlan, bridgeRoot)
	require.ErrorContains(t, err, "maximum serialized state size")
	bridgeCommitContext := protocolStateTestContext("bridge-oversized-output", 31, 32)
	bridgeCommitRequest := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "small")}
	require.NoError(t, PrepareResponsesRequest(bridgeCommitContext, protocolStateRelayInfo("gpt-a", 34), bridgePlan, bridgeCommitRequest))
	bridgeCommitResponse := &dto.OpenAIResponsesResponse{
		ID:     "upstream-bridge-oversized-output",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{
			Type: "message",
			Role: "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type: "output_text",
				Text: strings.Repeat("x", policy.MaxStateBytes),
			}},
		}},
	}
	CaptureResponsesResponse(bridgeCommitContext, bridgeCommitResponse.ID, bridgeCommitResponse)
	err = Commit(bridgeCommitContext)
	require.ErrorContains(t, err, "protocol bridge state exceeds")

	bridgeContinuationContext := protocolStateTestContext("bridge-oversized-continuation", 31, 32)
	_, err = ResolveSelectionBinding(bridgeContinuationContext, "/v1/responses", "gpt-a", continuationBody)
	require.NoError(t, err)
	bridgeContinuation := &dto.OpenAIResponsesRequest{
		Model:              "gpt-a",
		PreviousResponseID: publicID,
		Input:              mustProtocolStateJSON(t, "bridge this history"),
	}
	err = PrepareResponsesRequest(bridgeContinuationContext, protocolStateRelayInfo("gpt-a", 34), bridgePlan, bridgeContinuation)
	require.ErrorContains(t, err, "maximum conversation length")
}

func TestResponsesLegacyBridgeStateWithoutMarkerStillEnforcesLimits(t *testing.T) {
	resetProtocolStateCaches(t)
	policy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	policy.MaxStateBytes = 64
	policy.MaxStateTurns = 1
	model_setting.GetGlobalSettings().ProtocolBridgePolicy = policy

	owner := identity{userID: 35, tokenID: 36}
	stateCache, _, _ := protocolCaches()
	tests := []struct {
		name            string
		publicID        string
		turn            int
		cumulativeBytes int
		wantError       string
	}{
		{
			name:            "serialized size",
			publicID:        "resp_legacy_bridge_size",
			turn:            1,
			cumulativeBytes: policy.MaxStateBytes + 1,
			wantError:       "maximum serialized state size",
		},
		{
			name:            "conversation turns",
			publicID:        "resp_legacy_bridge_turns",
			turn:            policy.MaxStateTurns + 1,
			cumulativeBytes: 1,
			wantError:       "maximum conversation length",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := (responseNodeCodec{}).Encode(ResponseNode{
				Version:              stateVersion,
				UserID:               owner.userID,
				TokenID:              owner.tokenID,
				PublicResponseID:     test.publicID,
				ChannelID:            37,
				RequestProtocol:      string(channelcompat.ProtocolResponses),
				UpstreamProtocol:     string(channelcompat.ProtocolChat),
				PublicModel:          "gpt-a",
				NormalizedInput:      mustProtocolStateJSON(t, "legacy input"),
				NormalizedOutput:     mustProtocolStateJSON(t, []any{}),
				Turn:                 test.turn,
				CumulativeStateBytes: test.cumulativeBytes,
			})
			require.NoError(t, err)
			assert.NotContains(t, encoded, "bridge_managed")
			legacyNode, err := (responseNodeCodec{}).Decode(encoded)
			require.NoError(t, err)
			assert.False(t, legacyNode.BridgeManaged)
			require.NoError(t, stateCache.SetWithTTL(responseNodeKey(owner, test.publicID), legacyNode, time.Hour))

			body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": test.publicID})
			_, err = ResolveSelectionBinding(protocolStateTestContext("legacy-bridge", owner.userID, owner.tokenID), "/v1/responses", "gpt-a", body)
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestMessagesStateLimitsOnlyResponsesBridge(t *testing.T) {
	resetProtocolStateCaches(t)
	policy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	policy.MaxStateBytes = 64
	model_setting.GetGlobalSettings().ProtocolBridgePolicy = policy

	request := claudeSessionRequest(strings.Repeat("x", policy.MaxStateBytes))
	body := mustProtocolStateJSON(t, request)
	nativeContext := protocolStateTestContext("native-messages-oversized", 41, 42)
	binding, err := ResolveSelectionBinding(nativeContext, "/v1/messages", "claude-public", body)
	require.NoError(t, err)
	assert.Nil(t, binding)
	nativePlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolMessages,
		Status:           channelcompat.StatusNative,
	}
	require.NoError(t, PrepareMessagesRequest(nativeContext, protocolStateRelayInfo("claude-public", 43), nativePlan, request))

	bridgeContext := protocolStateTestContext("bridge-messages-oversized", 41, 42)
	_, err = ResolveSelectionBinding(bridgeContext, "/v1/messages", "claude-public", body)
	require.NoError(t, err)
	bridgePlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
		StateEnabled:     true,
	}
	err = PrepareMessagesRequest(bridgeContext, protocolStateRelayInfo("claude-public", 44), bridgePlan, request)
	require.ErrorContains(t, err, "maximum serialized state size")
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

func TestResponsesStateWorksForExplicitConversionWhenGlobalBridgeIsDisabled(t *testing.T) {
	resetProtocolStateCaches(t)
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled = false
	ctx := protocolStateTestContext("explicit-conversion", 54, 55)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "hello")}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		Status:           channelcompat.StatusConvertible,
		StateMode:        channelcompat.StateModeReplay,
		StateEnabled:     true,
	}
	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-a", 56), plan, request))
	response := &dto.OpenAIResponsesResponse{
		ID:     "chatcmpl_explicit",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant"}},
	}
	publicID := CaptureResponsesResponse(ctx, response.ID, response)
	require.NotEmpty(t, publicID)
	require.NoError(t, Commit(ctx))

	next := protocolStateTestContext("explicit-conversion-next", 54, 55)
	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	binding, err := ResolveSelectionBinding(next, "/v1/responses", "gpt-a", body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 56, binding.ChannelID)
	assert.Equal(t, channelcompat.ProtocolChat, binding.UpstreamProtocol)
}

func TestResponsesStateRemainsDisabledForLegacyPlanWhenGlobalBridgeIsDisabled(t *testing.T) {
	resetProtocolStateCaches(t)
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled = false
	ctx := protocolStateTestContext("legacy-disabled", 64, 65)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-a",
		Input: mustProtocolStateJSON(t, []map[string]any{
			{"type": "message", "id": "resp_legacy_msg_0", "role": "assistant", "content": "old"},
			{"type": "custom_tool_call", "id": "resp_legacy_custom_0", "call_id": "call_exec", "name": "exec", "input": "pwd"},
		}),
	}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}

	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-a", 66), plan, request))

	assert.False(t, Active(ctx))
	assert.Empty(t, PublicResponseID(ctx, ""))
	assert.True(t, ResponsesRequestNormalized(ctx))
	var items []map[string]any
	require.NoError(t, common.Unmarshal(request.Input, &items))
	assert.NotContains(t, items[0], "id")
	assert.NotContains(t, items[1], "id")
	var tools []map[string]any
	require.NoError(t, common.Unmarshal(request.Tools, &tools))
	require.Len(t, tools, 1)
	assert.Equal(t, "custom", tools[0]["type"])
	assert.Equal(t, "exec", tools[0]["name"])
}

func TestResponsesRequestKeepsPassthroughEligibleWhenNormalizationIsUnchanged(t *testing.T) {
	resetProtocolStateCaches(t)
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled = false
	ctx := protocolStateTestContext("legacy-unchanged", 67, 68)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-a",
		Input: mustProtocolStateJSON(t, []map[string]any{{
			"type": "message", "id": "msg_valid", "role": "user", "content": "hello",
		}}),
		Tools: jsonRaw(`[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`),
	}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}

	require.NoError(t, PrepareResponsesRequest(ctx, protocolStateRelayInfo("gpt-a", 69), plan, request))

	assert.False(t, Active(ctx))
	assert.False(t, ResponsesRequestNormalized(ctx))
}

func TestResponsesStateClearsFailedManagedAttemptBeforeLegacyRetry(t *testing.T) {
	resetProtocolStateCaches(t)
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled = false
	ctx := protocolStateTestContext("responses-retry", 74, 75)
	managedPlan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		Status:           channelcompat.StatusConvertible,
		StateEnabled:     true,
	}
	relayInfo := protocolStateRelayInfo("gpt-a", 76)

	require.NoError(t, PrepareResponsesRequest(ctx, relayInfo, managedPlan, &dto.OpenAIResponsesRequest{
		Model: "gpt-a",
		Input: mustProtocolStateJSON(t, "hello"),
	}))
	require.True(t, Active(ctx))
	require.NotEmpty(t, PublicResponseID(ctx, ""))

	legacyPlan := managedPlan
	legacyPlan.UpstreamProtocol = channelcompat.ProtocolResponses
	legacyPlan.Status = channelcompat.StatusNative
	legacyPlan.StateEnabled = false
	require.NoError(t, PrepareResponsesRequest(ctx, relayInfo, legacyPlan, &dto.OpenAIResponsesRequest{
		Model: "gpt-a",
		Input: mustProtocolStateJSON(t, "hello"),
	}))

	assert.False(t, Active(ctx))
	assert.Empty(t, PublicResponseID(ctx, ""))
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

func TestResponsesStatePersistsDeliveredStreamAfterClientCancellation(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("delivered-then-cancelled-stream", 16, 17)
	requestContext, cancel := context.WithCancel(ctx.Request.Context())
	ctx.Request = ctx.Request.WithContext(requestContext)
	stream := true
	request := &dto.OpenAIResponsesRequest{Model: "gpt-a", Input: mustProtocolStateJSON(t, "hello"), Stream: &stream}
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	relayInfo := protocolStateRelayInfo("gpt-a", 4)
	relayInfo.IsStream = true
	require.NoError(t, PrepareResponsesRequest(ctx, relayInfo, plan, request))
	response := &dto.OpenAIResponsesResponse{
		ID:     "upstream-delivered-stream",
		Status: mustProtocolStateJSON(t, "completed"),
		Output: []dto.ResponsesOutput{{Type: "message", Role: "assistant"}},
	}
	publicID := CaptureResponsesResponse(ctx, response.ID, response)
	MarkStreamCompleted(ctx)
	cancel()
	require.NoError(t, Commit(ctx))

	body := mustProtocolStateJSON(t, map[string]any{"previous_response_id": publicID})
	binding, err := ResolveSelectionBinding(protocolStateTestContext("delivered-then-cancelled-stream-next", 16, 17), "/v1/responses", "gpt-a", body)
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 4, binding.ChannelID)
}

func TestResponsesBridgeStateEnforcesMaximumTurnsBeforeRelay(t *testing.T) {
	resetProtocolStateCaches(t)
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.MaxStateTurns = 1
	rootContext := protocolStateTestContext("turn-root", 24, 25)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolMessages,
		Status:           channelcompat.StatusConvertible,
		StateEnabled:     true,
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
	upstream := &dto.OpenAIResponsesResponse{
		ID:     "resp_upstream_claude",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  true,
		Output: []dto.ResponsesOutput{
			{
				Type:             "reasoning",
				ID:               "rs_upstream_claude",
				Summary:          []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "inspect"}},
				EncryptedContent: "provider-reasoning-state",
			},
			{
				Type:   "message",
				ID:     "msg_upstream_claude",
				Role:   "assistant",
				Status: "completed",
				Content: []dto.ResponsesOutputContent{{
					Type: "output_text",
					Text: "answer",
				}},
			},
		},
	}
	assistantText := "answer"
	thinkingText := "inspect"
	assistantContent := []dto.ClaudeMediaMessage{
		{Type: "thinking", Thinking: &thinkingText},
		{Type: "text", Text: &assistantText},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type:    "message",
		Content: assistantContent,
	}
	rawProviderOutput := mustProtocolStateJSON(t, []any{
		map[string]any{
			"type":              "reasoning",
			"id":                "rs_upstream_claude",
			"summary":           []map[string]any{{"type": "summary_text", "text": "inspect"}},
			"encrypted_content": "provider-reasoning-state",
			"provider_extension": map[string]any{
				"opaque": true,
			},
		},
		map[string]any{
			"type":   "message",
			"id":     "msg_upstream_claude",
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{{
				"type": "output_text",
				"text": "answer",
			}},
		},
	})
	CaptureMessagesResponseData(initialContext, upstream, rawProviderOutput, claudeResponse)
	require.NoError(t, Commit(initialContext))
	initialSelection, ok := common.GetContextKeyType[*messageSelection](initialContext, constant.ContextKeyProtocolStateSession)
	require.True(t, ok)
	require.NotNil(t, initialSelection)
	_, _, messageCache := protocolCaches()
	storedSession, found, err := messageCache.Get(messageSessionKey(identity{userID: 8, tokenID: 9}, initialSelection.key, "claude-public"))
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, storedSession.History, 2)
	require.Len(t, storedSession.ProviderOutputs, 1)
	require.Len(t, storedSession.ProviderOutputs[1], 2)
	var storedReasoning map[string]any
	require.NoError(t, common.Unmarshal(storedSession.ProviderOutputs[1][0], &storedReasoning))
	assert.Equal(t, "provider-reasoning-state", storedReasoning["encrypted_content"])
	assert.Equal(t, map[string]any{"opaque": true}, storedReasoning["provider_extension"])

	nextRequest := claudeSessionRequest("hello")
	nextRequest.Messages = append(nextRequest.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: assistantContent},
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
	assert.Equal(t, channelcompat.ProtocolResponses, binding.UpstreamProtocol)

	outbound := claudeSessionRequest("hello")
	outbound.Messages = append(outbound.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: assistantContent},
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
		dto.ClaudeMessage{Role: "assistant", Content: assistantContent},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	require.NoError(t, PrepareMessagesRequest(nextContext, protocolStateRelayInfo("claude-public", 12), plan, replayedOutbound))
	assert.Len(t, replayedOutbound.Messages, 3)
	require.Len(t, replayedOutbound.Messages[1].ProviderResponsesRawOutput, 2)
	var replayedReasoning map[string]any
	require.NoError(t, common.Unmarshal(replayedOutbound.Messages[1].ProviderResponsesRawOutput[0], &replayedReasoning))
	assert.Equal(t, "rs_upstream_claude", replayedReasoning["id"])
	convertedReplay, err := relayconvert.ConvertRequest(nextContext, protocolStateRelayInfo("claude-public", 12), types.RelayFormatOpenAIResponses, &dto.ClaudeRequest{
		Model:    "claude-public",
		Messages: replayedOutbound.Messages,
	})
	require.NoError(t, err)
	replayRequest := convertedReplay.Value.(*dto.OpenAIResponsesRequest)
	var replayInput []map[string]any
	require.NoError(t, common.Unmarshal(replayRequest.Input, &replayInput))
	require.Len(t, replayInput, 4)
	assert.Equal(t, "reasoning", replayInput[1]["type"])
	assert.Equal(t, "provider-reasoning-state", replayInput[1]["encrypted_content"])
	assert.Equal(t, map[string]any{"opaque": true}, replayInput[1]["provider_extension"])
	responsesRequest = &dto.OpenAIResponsesRequest{}
	ApplyMessagesContinuation(nextContext, responsesRequest)
	assert.Empty(t, responsesRequest.PreviousResponseID)

	nonAppend := claudeSessionRequest("different")
	nonAppendContext := protocolStateTestContext("claude-nonappend", 8, 9)
	binding, err = ResolveSelectionBinding(nonAppendContext, "/v1/messages", "claude-public", mustProtocolStateJSON(t, nonAppend))
	require.NoError(t, err)
	assert.Nil(t, binding)
}

func TestClaudeCodeResponsesDoesNotRestoreProviderStateAcrossMappedModels(t *testing.T) {
	resetProtocolStateCaches(t)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
	}
	initialContext := protocolStateTestContext("claude-model-a", 218, 219)
	initialInfo := protocolStateRelayInfo("claude-public", 220)
	initialInfo.UpstreamModelName = "provider-model-a"
	initialRequest := claudeSessionRequest("hello")
	require.NoError(t, PrepareMessagesRequest(initialContext, initialInfo, plan, initialRequest))
	answer := "answer"
	CaptureMessagesResponseData(
		initialContext,
		&dto.OpenAIResponsesResponse{
			ID:     "resp_provider_model_a",
			Status: mustProtocolStateJSON(t, "completed"),
			Store:  false,
		},
		mustProtocolStateJSON(t, []map[string]any{{
			"type":              "reasoning",
			"id":                "rs_provider_model_a",
			"encrypted_content": "model-a-only",
		}}),
		&dto.ClaudeResponse{
			Type:    "message",
			Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &answer}},
		},
	)
	require.NoError(t, Commit(initialContext))

	nextRequest := claudeSessionRequest("hello")
	nextRequest.Messages = append(nextRequest.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &answer}}},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	nextContext := protocolStateTestContext("claude-model-b", 218, 219)
	nextInfo := protocolStateRelayInfo("claude-public", 220)
	nextInfo.UpstreamModelName = "provider-model-b"
	require.NoError(t, PrepareMessagesRequest(nextContext, nextInfo, plan, nextRequest))

	require.Len(t, nextRequest.Messages, 3)
	assert.Empty(t, nextRequest.Messages[1].ProviderResponsesRawOutput)
	responsesRequest := &dto.OpenAIResponsesRequest{}
	ApplyMessagesContinuation(nextContext, responsesRequest)
	assert.Empty(t, responsesRequest.PreviousResponseID)
}

func TestObserveClaudeStreamDoesNotRetainProviderSignature(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("claude-stream-signature", 28, 29)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
	}
	info := protocolStateRelayInfo("claude-public", 30)
	info.IsStream = true
	require.NoError(t, PrepareMessagesRequest(ctx, info, plan, claudeSessionRequest("hello")))
	outputIndex := 0
	itemEvent := dto.ResponsesStreamResponse{
		Type:        dto.ResponsesOutputTypeItemDone,
		OutputIndex: &outputIndex,
		Item: &dto.ResponsesOutput{
			Type:             "reasoning",
			ID:               "rs_stream",
			Summary:          []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "inspect"}},
			EncryptedContent: "server-side-only",
		},
	}
	itemData := mustProtocolStateJSON(t, map[string]any{
		"type":         dto.ResponsesOutputTypeItemDone,
		"output_index": outputIndex,
		"item": map[string]any{
			"type":              "reasoning",
			"id":                "rs_stream",
			"summary":           []map[string]any{{"type": "summary_text", "text": "inspect"}},
			"encrypted_content": "server-side-only",
			"provider_extension": map[string]any{
				"streamed": true,
			},
		},
	})
	_, err := ObserveResponsesStreamData(ctx, &itemEvent, itemData)
	require.NoError(t, err)
	terminalEvent := dto.ResponsesStreamResponse{
		Type: "response.completed",
		Response: &dto.OpenAIResponsesResponse{
			ID:     "resp_stream",
			Status: mustProtocolStateJSON(t, "completed"),
			Store:  false,
			Output: []dto.ResponsesOutput{{
				Type:             "reasoning",
				ID:               "rs_stream",
				Summary:          []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "inspect"}},
				EncryptedContent: "server-side-only",
			}},
		},
	}
	terminalData := mustProtocolStateJSON(t, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     "resp_stream",
			"status": "completed",
			"store":  false,
			"output": []map[string]any{{
				"type":              "reasoning",
				"id":                "rs_stream",
				"summary":           []map[string]any{{"type": "summary_text", "text": "inspect"}},
				"encrypted_content": "server-side-only",
			}},
		},
	})
	_, err = ObserveResponsesStreamData(ctx, &terminalEvent, terminalData)
	require.NoError(t, err)

	blockIndex := 0
	emptyThinking := ""
	thinkingDelta := "inspect"
	ObserveClaudeStream(ctx, &dto.ClaudeResponse{
		Type:  "content_block_start",
		Index: &blockIndex,
		ContentBlock: &dto.ClaudeMediaMessage{
			Type:     "thinking",
			Thinking: &emptyThinking,
		},
	})
	ObserveClaudeStream(ctx, &dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: &blockIndex,
		Delta: &dto.ClaudeMediaMessage{
			Type:     "thinking_delta",
			Thinking: &thinkingDelta,
		},
	})
	ObserveClaudeStream(ctx, &dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: &blockIndex,
		Delta: &dto.ClaudeMediaMessage{
			Type:      "signature_delta",
			Signature: "must-not-be-persisted",
		},
	})
	ObserveClaudeStream(ctx, &dto.ClaudeResponse{Type: "message_stop"})

	pending := getPending(ctx, pendingMessages)
	require.NotNil(t, pending)
	require.Len(t, pending.upstreamOutput, 1)
	var streamedReasoning map[string]any
	require.NoError(t, common.Unmarshal(pending.upstreamOutput[0], &streamedReasoning))
	assert.Equal(t, "server-side-only", streamedReasoning["encrypted_content"])
	assert.Equal(t, map[string]any{"streamed": true}, streamedReasoning["provider_extension"])
	require.NotEmpty(t, pending.assistantMessage)
	var assistant dto.ClaudeMessage
	require.NoError(t, common.Unmarshal(pending.assistantMessage, &assistant))
	content, err := assistant.ParseContent()
	require.NoError(t, err)
	require.Len(t, content, 1)
	assert.Equal(t, "thinking", content[0].Type)
	require.NotNil(t, content[0].Thinking)
	assert.Equal(t, "inspect", *content[0].Thinking)
	assert.Empty(t, content[0].Signature)
	MarkStreamCompleted(ctx)
	require.NoError(t, Commit(ctx))

	nextRequest := claudeSessionRequest("hello")
	nextRequest.Messages = append(nextRequest.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "thinking", Thinking: &thinkingDelta}}},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	nextContext := protocolStateTestContext("claude-stream-replay", 28, 29)
	require.NoError(t, PrepareMessagesRequest(nextContext, protocolStateRelayInfo("claude-public", 30), plan, nextRequest))
	require.Len(t, nextRequest.Messages, 3)
	require.Len(t, nextRequest.Messages[1].ProviderResponsesRawOutput, 1)
	converted, convertErr := relayconvert.ConvertRequest(nextContext, protocolStateRelayInfo("claude-public", 30), types.RelayFormatOpenAIResponses, nextRequest)
	require.NoError(t, convertErr)
	convertedRequest := converted.Value.(*dto.OpenAIResponsesRequest)
	var replayInput []map[string]any
	require.NoError(t, common.Unmarshal(convertedRequest.Input, &replayInput))
	require.Len(t, replayInput, 3)
	assert.Equal(t, map[string]any{"streamed": true}, replayInput[1]["provider_extension"])
}

func TestClaudeCodeResponsesPromptCacheKeyIsTenantIsolated(t *testing.T) {
	resetProtocolStateCaches(t)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
	}
	buildKey := func(userID, tokenID int) string {
		ctx := protocolStateTestContext(fmt.Sprintf("prompt-key-%d-%d", userID, tokenID), userID, tokenID)
		require.NoError(t, PrepareMessagesRequest(ctx, protocolStateRelayInfo("claude-public", 12), plan, claudeSessionRequest("hello")))
		request := &dto.OpenAIResponsesRequest{}
		ApplyMessagesContinuation(ctx, request)
		key := common.JsonRawMessageToString(request.PromptCacheKey)
		require.NotEmpty(t, key)
		return key
	}

	first := buildKey(201, 301)
	assert.Equal(t, first, buildKey(201, 301))
	assert.NotEqual(t, first, buildKey(202, 301))
	assert.NotEqual(t, first, buildKey(201, 302))
}

func TestClaudeCodeResponsesBindingWorksForExplicitConversionWhenGlobalBridgeIsDisabled(t *testing.T) {
	resetProtocolStateCaches(t)
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled = false
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolMessages,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusConvertible,
		StateEnabled:     true,
	}
	initialContext := protocolStateTestContext("claude-explicit-root", 88, 89)
	initialRequest := claudeSessionRequest("hello")
	require.NoError(t, PrepareMessagesRequest(initialContext, protocolStateRelayInfo("claude-public", 90), plan, initialRequest))
	upstream := &dto.OpenAIResponsesResponse{
		ID:     "resp_explicit_claude",
		Status: mustProtocolStateJSON(t, "completed"),
		Store:  true,
	}
	assistantText := "answer"
	CaptureMessagesResponse(initialContext, upstream, &dto.ClaudeResponse{
		Type:    "message",
		Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &assistantText}},
	})
	require.NoError(t, Commit(initialContext))

	nextRequest := claudeSessionRequest("hello")
	nextRequest.Messages = append(nextRequest.Messages,
		dto.ClaudeMessage{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &assistantText}}},
		dto.ClaudeMessage{Role: "user", Content: "next"},
	)
	nextContext := protocolStateTestContext("claude-explicit-next", 88, 89)
	binding, err := ResolveSelectionBinding(nextContext, "/v1/messages", "claude-public", mustProtocolStateJSON(t, nextRequest))

	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, 90, binding.ChannelID)
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

func TestClaudeCodeResponsesIgnoresInvalidCachedSessionLimits(t *testing.T) {
	resetProtocolStateCaches(t)
	request := claudeSessionRequest("hello")
	stableKey, ok, err := stableClaudeSessionKey(request)
	require.NoError(t, err)
	require.True(t, ok)
	history, err := encodeClaudeHistory(request.Messages)
	require.NoError(t, err)
	identity := identity{userID: 181, tokenID: 182}
	validState, err := common.Marshal(struct {
		History         []json.RawMessage         `json:"history"`
		ProviderOutputs map[int][]json.RawMessage `json:"provider_outputs,omitempty"`
	}{History: history})
	require.NoError(t, err)

	tests := []struct {
		name            string
		turn            int
		serializedBytes int
	}{
		{name: "turn limit", turn: currentPolicy().MaxStateTurns + 1, serializedBytes: len(validState)},
		{name: "serialized size limit", turn: 1, serializedBytes: currentPolicy().MaxStateBytes + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, messageCache := protocolCaches()
			session := MessageSession{
				Version:            stateVersion,
				UserID:             identity.userID,
				TokenID:            identity.tokenID,
				SessionKey:         stableKey,
				ChannelID:          183,
				UpstreamResponseID: "resp_invalid_cached_session",
				UpstreamStored:     true,
				PublicModel:        "claude-public",
				History:            history,
				Turn:               test.turn,
				SerializedBytes:    test.serializedBytes,
			}
			require.NoError(t, messageCache.SetWithTTL(
				messageSessionKey(identity, stableKey, "claude-public"),
				session,
				time.Hour,
			))

			ctx := protocolStateTestContext("invalid-cached-session", identity.userID, identity.tokenID)
			selection, err := buildMessageSelection(ctx, "claude-public", request)
			require.NoError(t, err)
			require.NotNil(t, selection)
			assert.Nil(t, selection.session)
			assert.False(t, selection.strictAppend)
		})
	}
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

func TestResetAttemptClearsPendingStateAndPreservesRequestState(t *testing.T) {
	ctx := protocolStateTestContext("reset-attempt", 11, 22)
	parent := &ResponseNode{PublicResponseID: "resp_parent"}
	session := &messageSelection{key: "session-key"}
	common.SetContextKey(ctx, constant.ContextKeyProtocolStatePending, &pendingState{usedContinuation: true})
	common.SetContextKey(ctx, constant.ContextKeyProtocolStateParent, parent)
	common.SetContextKey(ctx, constant.ContextKeyProtocolStateSession, session)
	common.SetContextKey(ctx, constant.ContextKeyProtocolStatePublicID, "resp_public")
	common.SetContextKey(ctx, constant.ContextKeyProtocolStateForceReplay, true)
	common.SetContextKey(ctx, constant.ContextKeyProtocolRequestNormalized, true)
	common.SetContextKey(ctx, constant.ContextKeyProtocolStreamCompleted, true)

	ResetAttempt(ctx)

	assert.Nil(t, getPending(ctx, ""))
	storedParent, ok := common.GetContextKeyType[*ResponseNode](ctx, constant.ContextKeyProtocolStateParent)
	require.True(t, ok)
	assert.Same(t, parent, storedParent)
	storedSession, ok := common.GetContextKeyType[*messageSelection](ctx, constant.ContextKeyProtocolStateSession)
	require.True(t, ok)
	assert.Same(t, session, storedSession)
	assert.Equal(t, "resp_public", common.GetContextKeyString(ctx, constant.ContextKeyProtocolStatePublicID))
	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyProtocolStateForceReplay))
	assert.False(t, ResponsesRequestNormalized(ctx))
	assert.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyProtocolStreamCompleted))
}

func TestResetLogicalRequestClearsConversationStateButPreservesPlan(t *testing.T) {
	ctx := protocolStateTestContext("reset-logical-request", 11, 22)
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
	}
	common.SetContextKey(ctx, constant.ContextKeyProtocolPlan, plan)
	common.SetContextKey(ctx, constant.ContextKeyProtocolStatePending, &pendingState{usedContinuation: true})
	common.SetContextKey(ctx, constant.ContextKeyProtocolStateParent, &ResponseNode{PublicResponseID: "resp_parent"})
	common.SetContextKey(ctx, constant.ContextKeyProtocolStateSession, &messageSelection{key: "session-key"})
	common.SetContextKey(ctx, constant.ContextKeyProtocolStateBinding, &SelectionBinding{ChannelID: 7})
	common.SetContextKey(ctx, constant.ContextKeyProtocolStatePublicID, "resp_public")
	common.SetContextKey(ctx, constant.ContextKeyProtocolStateForceReplay, true)
	common.SetContextKey(ctx, constant.ContextKeyProtocolRequestNormalized, true)
	common.SetContextKey(ctx, constant.ContextKeyProtocolStreamCompleted, true)

	ResetLogicalRequest(ctx)

	assert.Nil(t, getPending(ctx, ""))
	parent, parentOK := common.GetContextKeyType[*ResponseNode](ctx, constant.ContextKeyProtocolStateParent)
	assert.False(t, parentOK)
	assert.Nil(t, parent)
	session, sessionOK := common.GetContextKeyType[*messageSelection](ctx, constant.ContextKeyProtocolStateSession)
	assert.False(t, sessionOK)
	assert.Nil(t, session)
	binding, bindingOK := common.GetContextKeyType[*SelectionBinding](ctx, constant.ContextKeyProtocolStateBinding)
	assert.False(t, bindingOK)
	assert.Nil(t, binding)
	assert.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyProtocolStatePublicID))
	assert.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyProtocolStateForceReplay))
	assert.False(t, ResponsesRequestNormalized(ctx))
	assert.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyProtocolStreamCompleted))
	storedPlan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, plan, storedPlan)
}

func TestStreamStateRequiresVerifiedUpstreamTerminalBeforeCommit(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("verified-stream-terminal", 91, 92)
	info := protocolStateRelayInfo("gpt-public", 93)
	info.IsStream = true
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolChat,
		Status:           channelcompat.StatusConvertible,
		StateEnabled:     true,
		Features:         channelcompat.RequestFeatureSet{Stream: true},
	}
	common.SetContextKey(ctx, constant.ContextKeyProtocolPlan, plan)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-public",
		Input: mustProtocolStateJSON(t, []map[string]any{{
			"role":    "user",
			"content": "hello",
		}}),
	}
	require.NoError(t, PrepareResponsesRequest(ctx, info, plan, request))
	publicID := PublicResponseID(ctx, "")
	require.NotEmpty(t, publicID)

	event := dto.ResponsesStreamResponse{
		Type: "response.completed",
		Response: &dto.OpenAIResponsesResponse{
			ID:     "resp_upstream",
			Model:  "provider-model",
			Status: jsonRaw(`"completed"`),
			Output: []dto.ResponsesOutput{{
				ID:     "msg_upstream",
				Type:   "message",
				Role:   "assistant",
				Status: "completed",
			}},
		},
	}
	ObserveResponsesStream(ctx, &event)
	assert.False(t, AttemptCompleted(ctx))
	require.NoError(t, Commit(ctx))
	_, managed, err := findResponseNode(ctx, publicID, "gpt-public")
	require.NoError(t, err)
	assert.False(t, managed)

	MarkStreamCompleted(ctx)
	assert.True(t, AttemptCompleted(ctx))
	require.NoError(t, Commit(ctx))
	node, managed, err := findResponseNode(ctx, publicID, "gpt-public")
	require.NoError(t, err)
	require.True(t, managed)
	require.NotNil(t, node)
	assert.Equal(t, publicID, node.PublicResponseID)
}

func TestAttemptCompletedAcceptsVerifiedIncompleteResponseWithoutPersistingState(t *testing.T) {
	resetProtocolStateCaches(t)
	ctx := protocolStateTestContext("incomplete-affinity", 191, 192)
	info := protocolStateRelayInfo("gpt-public", 193)
	info.IsStream = true
	plan := channelcompat.ProtocolPlan{
		RequestProtocol:  channelcompat.ProtocolResponses,
		UpstreamProtocol: channelcompat.ProtocolResponses,
		Status:           channelcompat.StatusNative,
		Features:         channelcompat.RequestFeatureSet{Stream: true},
	}
	common.SetContextKey(ctx, constant.ContextKeyProtocolPlan, plan)
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-public",
		Input:  mustProtocolStateJSON(t, "hello"),
		Stream: common.GetPointer(true),
	}
	require.NoError(t, PrepareResponsesRequest(ctx, info, plan, request))
	response := &dto.OpenAIResponsesResponse{
		ID:     "resp_incomplete",
		Model:  "provider-model",
		Status: mustProtocolStateJSON(t, "incomplete"),
	}
	CaptureResponsesResponse(ctx, response.ID, response)

	assert.False(t, AttemptCompleted(ctx))
	MarkStreamCompleted(ctx)
	assert.True(t, AttemptCompleted(ctx))
	require.NoError(t, Commit(ctx))
	_, managed, err := findResponseNode(ctx, PublicResponseID(ctx, ""), "gpt-public")
	require.NoError(t, err)
	assert.False(t, managed)
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
