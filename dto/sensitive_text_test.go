package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneralOpenAIRequestGetSensitiveTextOnlyReturnsUserInput(t *testing.T) {
	request := GeneralOpenAIRequest{
		Prompt: "hidden prompt hello",
		Messages: []Message{
			{Role: "system", Content: "hidden system hello"},
			{Role: "assistant", Content: "hidden assistant hello"},
			{Role: "user", Content: "用户正文"},
		},
		Tools: []ToolCallRequest{{
			Type: "function",
			Function: FunctionRequest{
				Name:        "hello_tool",
				Description: "hidden tool hello",
			},
		}},
		Metadata: []byte(`{"hidden":"hello"}`),
	}

	assert.Equal(t, "用户正文", request.GetSensitiveText())
	assert.Contains(t, request.GetTokenCountMeta().CombineText, "hidden system hello")
	assert.Contains(t, request.GetTokenCountMeta().CombineText, "hidden tool hello")
}

func TestOpenAIResponsesRequestGetSensitiveTextOnlyReturnsUserInput(t *testing.T) {
	var request OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gpt-5",
		"input":[
			{"role":"system","content":[{"type":"input_text","text":"hidden system hello"}]},
			{"role":"assistant","content":[{"type":"input_text","text":"hidden assistant hello"}]},
			{"role":"user","content":[{"type":"input_text","text":"用户正文"}]}
		],
		"instructions":"hidden instructions hello",
		"tools":[{"type":"function","name":"hello_tool"}],
		"metadata":{"hidden":"hello"}
	}`), &request))

	assert.Equal(t, "用户正文", request.GetSensitiveText())
	assert.Contains(t, request.GetTokenCountMeta().CombineText, "hidden instructions hello")
	assert.Contains(t, request.GetTokenCountMeta().CombineText, "hello_tool")
}

func TestClaudeRequestGetSensitiveTextOnlyReturnsUserMessages(t *testing.T) {
	request := ClaudeRequest{
		Prompt: "hidden prompt hello",
		System: "hidden system hello",
		Messages: []ClaudeMessage{
			{Role: "assistant", Content: "hidden assistant hello"},
			{Role: "user", Content: "用户正文"},
		},
	}

	assert.Equal(t, "用户正文", request.GetSensitiveText())
	assert.Contains(t, request.GetTokenCountMeta().CombineText, "hidden system hello")
}

func TestGeminiChatRequestGetSensitiveTextOnlyReturnsUserContents(t *testing.T) {
	request := GeminiChatRequest{
		SystemInstructions: &GeminiChatContent{
			Role:  "system",
			Parts: []GeminiPart{{Text: "hidden system hello"}},
		},
		Contents: []GeminiChatContent{
			{Role: "model", Parts: []GeminiPart{{Text: "hidden model hello"}}},
			{Role: "user", Parts: []GeminiPart{{Text: "用户正文"}}},
		},
		Tools: []byte(`{"functionDeclarations":[{"name":"hello_tool"}]}`),
	}

	assert.Equal(t, "用户正文", request.GetSensitiveText())
}

func TestAlphaSearchRequestGetSensitiveTextReturnsUserInputAndQueries(t *testing.T) {
	request := AlphaSearchRequest{RawBody: []byte(`{
		"model":"gpt-5.1",
		"input":[
			{"role":"system","content":"hidden system hello"},
			{"role":"user","content":"用户正文"}
		],
		"commands":{"search_query":[{"q":"查询关键词"}]},
		"settings":{"hidden":"hello"}
	}`)}

	assert.Equal(t, "用户正文\n查询关键词", request.GetSensitiveText())
}
