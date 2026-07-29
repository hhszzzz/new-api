package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThinkingBlockStateRoundTripIsChannelBound(t *testing.T) {
	const secret = "test-provider-state-secret"
	encoded, ok, err := EncodeThinkingBlock(dto.ClaudeMediaMessage{
		Type:      "thinking",
		Thinking:  kitutil.GetPointer("inspect inputs"),
		Signature: "signed-state",
	}, 17, secret)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, encoded)

	decoded, ok, err := DecodeThinkingBlock(encoded, 17, secret)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "thinking", decoded.Type)
	assert.Equal(t, "inspect inputs", *decoded.Thinking)
	assert.Equal(t, "signed-state", decoded.Signature)

	_, ok, err = DecodeThinkingBlock(encoded, 18, secret)
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = DecodeThinkingBlock(encoded, 17, "different-secret")
	require.Error(t, err)
	assert.False(t, ok)
}

func TestThinkingBlockStatePreservesRedactedDataAndRejectsUnsignedThinking(t *testing.T) {
	const secret = "test-provider-state-secret"
	encoded, ok, err := EncodeThinkingBlock(dto.ClaudeMediaMessage{Type: "redacted_thinking", Data: "opaque"}, 2, secret)
	require.NoError(t, err)
	require.True(t, ok)
	decoded, ok, err := DecodeThinkingBlock(encoded, 2, secret)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "redacted_thinking", decoded.Type)
	assert.Equal(t, "opaque", decoded.Data)

	encoded, ok, err = EncodeThinkingBlock(dto.ClaudeMediaMessage{
		Type:     "thinking",
		Thinking: kitutil.GetPointer("unsigned"),
	}, 2, secret)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, encoded)
}

func TestThinkingBlockStateRejectsTampering(t *testing.T) {
	const secret = "test-provider-state-secret"
	encoded, ok, err := EncodeThinkingBlock(dto.ClaudeMediaMessage{
		Type:      "thinking",
		Thinking:  kitutil.GetPointer("inspect inputs"),
		Signature: "signed-state",
	}, 17, secret)
	require.NoError(t, err)
	require.True(t, ok)

	index := len(anthropicThinkingStatePrefix) + 5
	original := encoded[index]
	replacement := byte('A')
	if original == replacement {
		replacement = 'B'
	}
	tampered := encoded[:index] + string(replacement) + encoded[index+1:]
	_, ok, err = DecodeThinkingBlock(tampered, 17, secret)
	require.Error(t, err)
	assert.False(t, ok)
}
