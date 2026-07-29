package oaichat

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToClaudeMessagesLinksLegacyFunctionResult(t *testing.T) {
	var req dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"max_tokens":128,
		"messages":[
			{"role":"assistant","content":null,"function_call":{"name":"lookup","arguments":"{\"q\":\"x\"}"}},
			{"role":"function","name":"lookup","content":"found"}
		]
	}`), &req))

	got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	assert.Equal(t, "user", got.Messages[0].Role)
	assert.Equal(t, "assistant", got.Messages[1].Role)
	assistantContent, ok := got.Messages[1].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, assistantContent, 1)
	assert.Equal(t, "tool_use", assistantContent[0].Type)
	assert.Equal(t, "call_0", assistantContent[0].Id)
	assert.Equal(t, "lookup", assistantContent[0].Name)
	assert.Equal(t, map[string]any{"q": "x"}, assistantContent[0].Input)

	assert.Equal(t, "user", got.Messages[2].Role)
	resultContent, ok := got.Messages[2].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, resultContent, 1)
	assert.Equal(t, "tool_result", resultContent[0].Type)
	assert.Equal(t, "call_0", resultContent[0].ToolUseId)
	assert.Equal(t, "found", resultContent[0].Content)
}

func TestOpenAIChatRequestToClaudeMessagesRejectsUnmatchedToolResult(t *testing.T) {
	maxTokens := uint(128)
	_, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
		Model:     "claude-test",
		MaxTokens: &maxTokens,
		Messages: []dto.Message{{
			Role:    "function",
			Content: "orphaned result",
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing tool_call_id")
}
