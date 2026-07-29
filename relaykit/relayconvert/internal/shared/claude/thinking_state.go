package claude

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const anthropicThinkingStatePrefix = "newapi-anthropic-thinking-v1:"

const maxAnthropicThinkingStateSize = 4 << 20

type anthropicThinkingState struct {
	Version   int    `json:"version"`
	ChannelID int    `json:"channel_id"`
	Type      string `json:"type"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
}

func EncodeThinkingBlock(block dto.ClaudeMediaMessage, channelID int, secret string) (string, bool, error) {
	if channelID <= 0 || secret == "" {
		return "", false, nil
	}
	state := anthropicThinkingState{
		Version:   1,
		ChannelID: channelID,
		Type:      strings.TrimSpace(block.Type),
		Signature: strings.TrimSpace(block.Signature),
		Data:      block.Data,
	}
	if block.Thinking != nil {
		state.Thinking = *block.Thinking
	}
	switch state.Type {
	case "thinking":
		if state.Signature == "" {
			return "", false, nil
		}
	case "redacted_thinking":
		if state.Data == "" {
			return "", false, nil
		}
	default:
		return "", false, nil
	}
	encoded, err := kitutil.Marshal(state)
	if err != nil {
		return "", false, fmt.Errorf("marshal Anthropic thinking state: %w", err)
	}
	if len(encoded) > maxAnthropicThinkingStateSize {
		return "", false, fmt.Errorf("Anthropic thinking state exceeds %d bytes", maxAnthropicThinkingStateSize)
	}
	aead, err := thinkingStateAEAD(secret)
	if err != nil {
		return "", false, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", false, fmt.Errorf("generate Anthropic thinking state nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, encoded, []byte(anthropicThinkingStatePrefix))
	return anthropicThinkingStatePrefix + base64.RawURLEncoding.EncodeToString(sealed), true, nil
}

func DecodeThinkingBlock(encoded string, channelID int, secret string) (dto.ClaudeMediaMessage, bool, error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, anthropicThinkingStatePrefix) {
		return dto.ClaudeMediaMessage{}, false, nil
	}
	if channelID <= 0 || secret == "" {
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("Anthropic thinking state cannot be restored without channel metadata and a state secret")
	}
	encodedPayload := strings.TrimPrefix(encoded, anthropicThinkingStatePrefix)
	if len(encodedPayload) > base64.RawURLEncoding.EncodedLen(maxAnthropicThinkingStateSize+64) {
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("Anthropic thinking state exceeds %d bytes", maxAnthropicThinkingStateSize)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("decode Anthropic thinking state: %w", err)
	}
	aead, err := thinkingStateAEAD(secret)
	if err != nil {
		return dto.ClaudeMediaMessage{}, false, err
	}
	if len(payload) < aead.NonceSize() {
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("decode Anthropic thinking state: payload is too short")
	}
	nonce, ciphertext := payload[:aead.NonceSize()], payload[aead.NonceSize():]
	payload, err = aead.Open(nil, nonce, ciphertext, []byte(anthropicThinkingStatePrefix))
	if err != nil {
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("authenticate Anthropic thinking state: %w", err)
	}
	if len(payload) > maxAnthropicThinkingStateSize {
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("Anthropic thinking state exceeds %d bytes", maxAnthropicThinkingStateSize)
	}
	var state anthropicThinkingState
	if err := kitutil.Unmarshal(payload, &state); err != nil {
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("decode Anthropic thinking state: %w", err)
	}
	if state.Version != 1 {
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("unsupported Anthropic thinking state version %d", state.Version)
	}
	if state.ChannelID != channelID {
		return dto.ClaudeMediaMessage{}, false, nil
	}
	block := dto.ClaudeMediaMessage{
		Type:      state.Type,
		Signature: state.Signature,
		Data:      state.Data,
	}
	if state.Thinking != "" {
		block.Thinking = kitutil.GetPointer(state.Thinking)
	}
	switch state.Type {
	case "thinking":
		if state.Signature == "" {
			return dto.ClaudeMediaMessage{}, false, fmt.Errorf("Anthropic thinking state is missing signature")
		}
	case "redacted_thinking":
		if state.Data == "" {
			return dto.ClaudeMediaMessage{}, false, fmt.Errorf("Anthropic redacted thinking state is missing data")
		}
	default:
		return dto.ClaudeMediaMessage{}, false, fmt.Errorf("Anthropic thinking state has unsupported type %q", state.Type)
	}
	return block, true, nil
}

func thinkingStateAEAD(secret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("new-api/relaykit/anthropic-thinking-state/v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("initialize Anthropic thinking state cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize Anthropic thinking state AEAD: %w", err)
	}
	return aead, nil
}
