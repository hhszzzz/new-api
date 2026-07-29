package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeMessagesRequestToOpenAIResponsesRejectsUnrepresentableFields(t *testing.T) {
	topK := 0
	tests := []struct {
		name    string
		request dto.ClaudeRequest
		field   string
	}{
		{name: "stop sequences", request: dto.ClaudeRequest{StopSequences: []string{"END"}}, field: "stop_sequences"},
		{name: "top k", request: dto.ClaudeRequest{TopK: &topK}, field: "top_k"},
		{name: "context management", request: dto.ClaudeRequest{ContextManagement: []byte(`{"type":"compaction"}`)}, field: "context_management"},
		{name: "output format", request: dto.ClaudeRequest{OutputFormat: []byte(`{"type":"json_schema"}`)}, field: "output_format"},
		{name: "container", request: dto.ClaudeRequest{Container: []byte(`{"id":"container_1"}`)}, field: "container"},
		{name: "mcp servers", request: dto.ClaudeRequest{McpServers: []byte(`[{"name":"tools"}]`)}, field: "mcp_servers"},
		{name: "inference geo", request: dto.ClaudeRequest{InferenceGeo: "us"}, field: "inference_geo"},
		{name: "speed", request: dto.ClaudeRequest{Speed: []byte(`"fast"`)}, field: "speed"},
		{name: "service tier", request: dto.ClaudeRequest{ServiceTier: "priority"}, field: "service_tier"},
		{name: "output config format", request: dto.ClaudeRequest{OutputConfig: []byte(`{"effort":"high","format":{"type":"json"}}`)}, field: "output_config"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.request.Model = "claude-public"
			_, err := ClaudeMessagesRequestToOpenAIResponses(test.request, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.field)
		})
	}
}

func TestClaudeMessagesRequestToOpenAIChatPreservesCompatibleFieldsAndMetadata(t *testing.T) {
	topK := 0
	request := dto.ClaudeRequest{
		Model:         "claude-public",
		StopSequences: []string{"END", "STOP"},
		TopK:          &topK,
		Metadata:      []byte(`{"user_id":"tenant-1"}`),
	}

	converted, err := ClaudeMessagesRequestToOpenAIChat(request, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"END", "STOP"}, converted.Stop)
	assert.Nil(t, converted.TopK)
	assert.JSONEq(t, `{"user_id":"tenant-1"}`, string(converted.Metadata))
}

func TestClaudeMessagesRequestToOpenAIResponsesAllowsEffortOnlyOutputConfigAndMetadata(t *testing.T) {
	request := dto.ClaudeRequest{
		Model:        "gpt-5.4",
		OutputConfig: []byte(`{"effort":"max"}`),
		Metadata:     []byte(`{"user_id":"tenant-1"}`),
	}

	converted, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)

	require.NoError(t, err)
	require.NotNil(t, converted.Reasoning)
	assert.Equal(t, "xhigh", converted.Reasoning.Effort)
	assert.JSONEq(t, `{"user_id":"tenant-1"}`, string(converted.Metadata))
}

func TestClaudeMessagesRequestToOpenAIResponsesOnlySendsReasoningForSupportedModels(t *testing.T) {
	request := dto.ClaudeRequest{
		Model:        "openpangu-2.0-flash",
		OutputConfig: []byte(`{"effort":"max"}`),
		Messages:     []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}

	converted, err := ClaudeMessagesRequestToOpenAIResponses(request, nil)

	require.NoError(t, err)
	assert.Nil(t, converted.Reasoning)
}

func TestClaudeMessagesRequestToOpenAIChatRejectsMessagesNativeFields(t *testing.T) {
	_, err := ClaudeMessagesRequestToOpenAIChat(dto.ClaudeRequest{
		Model:        "claude-public",
		OutputFormat: []byte(`{"type":"json_schema"}`),
	}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "output_format")
}
