package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageExtractsCompatibleReasoningFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "reasoning content wins", body: `{"reasoning_content":"primary","reasoning":{"text":"secondary"},"reasoning_details":[{"text":"third"}]}`, want: "primary"},
		{name: "reasoning string", body: `{"reasoning":"plain"}`, want: "plain"},
		{name: "reasoning object", body: `{"reasoning":{"summary":"object summary"}}`, want: "object summary"},
		{name: "reasoning details", body: `{"reasoning_details":[{"text":"first"},{"parts":[{"content":"second"}]}]}`, want: "first\n\nsecond"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var message Message
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), &message))
			assert.Equal(t, test.want, message.GetReasoningContent())
		})
	}
}

func TestLegacyFunctionCallNormalizesToToolCall(t *testing.T) {
	var message Message
	require.NoError(t, kitutil.Unmarshal([]byte(`{"function_call":{"name":"lookup","arguments":{"q":"x"}}}`), &message))

	calls := message.ParseToolCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "call_0", calls[0].ID)
	assert.Equal(t, "function", calls[0].Type)
	assert.Equal(t, "lookup", calls[0].Function.Name)
	assert.JSONEq(t, `{"q":"x"}`, calls[0].Function.Arguments)
}

func TestLegacyFunctionCallStreamDeltaNormalizesToIndexedToolCall(t *testing.T) {
	var delta ChatCompletionsStreamResponseChoiceDelta
	require.NoError(t, kitutil.Unmarshal([]byte(`{"function_call":{"id":"call_legacy","name":"lookup","arguments":"{\"q\":"}}`), &delta))

	calls := delta.ParseToolCalls()
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].Index)
	assert.Zero(t, *calls[0].Index)
	assert.Equal(t, "call_legacy", calls[0].ID)
	assert.Equal(t, `{"q":`, calls[0].Function.Arguments)
}

func TestResponsesRefusalContentUsesOfficialShape(t *testing.T) {
	encoded, err := kitutil.Marshal(ResponsesOutputContent{
		Type:    "refusal",
		Refusal: "I cannot help.",
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"refusal","refusal":"I cannot help."}`, string(encoded))
}
