package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeMessagesRequestToOpenAIChatPreservesMixedAssistantContentAndRequestControls(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"tool_choice":{"type":"tool","name":"lookup","disable_parallel_tool_use":true},
		"messages":[
			{
				"role":"user",
				"content":[
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"YWJj"}},
					{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}}
				]
			},
			{
				"role":"assistant",
				"content":[
					{"type":"thinking","thinking":"inspect the inputs","signature":"anthropic-signature"},
					{"type":"text","text":"I will use both tools."},
					{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}},
					{"type":"tool_use","id":"toolu_2","name":"fetch","input":{"id":2}}
				]
			}
		]
	}`), &request))

	got, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.NoError(t, err)

	require.NotNil(t, got.ParallelTooCalls)
	assert.False(t, *got.ParallelTooCalls)
	assert.Equal(t, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "lookup",
		},
	}, got.ToolChoice)

	require.Len(t, got.Messages, 2)
	userParts := got.Messages[0].ParseContent()
	require.Len(t, userParts, 2)
	assert.Equal(t, "data:image/png;base64,YWJj", userParts[0].GetImageMedia().Url)
	assert.Equal(t, "https://example.test/image.png", userParts[1].GetImageMedia().Url)

	assistant := got.Messages[1]
	assert.Equal(t, "inspect the inputs", assistant.GetReasoningContent())
	assistantParts := assistant.ParseContent()
	require.Len(t, assistantParts, 1)
	assert.Equal(t, "I will use both tools.", assistantParts[0].Text)
	toolCalls := assistant.ParseToolCalls()
	require.Len(t, toolCalls, 2)
	assert.Equal(t, "toolu_1", toolCalls[0].ID)
	assert.Equal(t, "lookup", toolCalls[0].Function.Name)
	assert.JSONEq(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, "toolu_2", toolCalls[1].ID)
	assert.Equal(t, "fetch", toolCalls[1].Function.Name)
	assert.JSONEq(t, `{"id":2}`, toolCalls[1].Function.Arguments)
}

func TestClaudeMessagesRequestToOpenAIChatRejectsImageWithoutSource(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "image"},
				},
			},
		},
	}

	_, err := ClaudeMessagesRequestToOpenAIChat(request, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image source")
}
