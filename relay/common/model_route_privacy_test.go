package common

import (
	"encoding/json"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func routedPrivacyInfo() *RelayInfo {
	return &RelayInfo{
		OriginModelName:      "gpt-5.4",
		UserModelRouteId:     7,
		RouteTargetModelName: "provider-gpt-5.5",
		ChannelMeta: &ChannelMeta{
			UpstreamModelName: "provider-gpt-5.5",
		},
	}
}

func TestRedactUserModelRouteJSONPreservesNumbersAndNestedMetadata(t *testing.T) {
	input := []byte(`{"model":"provider-gpt-5.5","session":{"model":"provider-gpt-5.5"},"metadata":{"model":"client-label","model_name":"provider-gpt-5.5"},"count":18446744073709551615}`)
	output, err := RedactUserModelRouteJSON(input, routedPrivacyInfo())
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, appcommon.Unmarshal(output, &decoded))
	assert.JSONEq(t, `"gpt-5.4"`, string(decoded["model"]))
	assert.JSONEq(t, `{"model":"gpt-5.4"}`, string(decoded["session"]))
	assert.JSONEq(t, `{"model":"client-label","model_name":"provider-gpt-5.5"}`, string(decoded["metadata"]))
	assert.Equal(t, "18446744073709551615", string(decoded["count"]))
}

func TestRewriteUserModelRouteRequestJSONOnlyRewritesProtocolModelFields(t *testing.T) {
	input := []byte(`{"model":"gpt-5.4","session":{"model":"gpt-5.4","input_audio_transcription":{"model":"whisper-1"}},"metadata":{"model":"client-label","value":1}}`)
	output, err := RewriteUserModelRouteRequestJSON(input, "provider-gpt-5.5")
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, appcommon.Unmarshal(output, &decoded))
	assert.JSONEq(t, `"provider-gpt-5.5"`, string(decoded["model"]))
	assert.JSONEq(t, `{"model":"provider-gpt-5.5","input_audio_transcription":{"model":"whisper-1"}}`, string(decoded["session"]))
	assert.JSONEq(t, `{"model":"client-label","value":1}`, string(decoded["metadata"]))
}

func TestRedactUserModelRouteJSONRedactsProtocolErrorOnly(t *testing.T) {
	input := []byte(`{"error":{"message":"provider-gpt-5.5 is unavailable","type":"provider-gpt-5.5","code":"provider-gpt-5.5","param":{"model":"provider-gpt-5.5","arguments":"{\"model\":\"provider-gpt-5.5\"}"},"metadata":{"upstream_model":"provider-gpt-5.5"}},"response":{"error":{"message":"provider-gpt-5.5 failed"},"metadata":{"model":"provider-gpt-5.5"}},"message":{"content":"provider-gpt-5.5 is ordinary content"},"item":{"arguments":"{\"model\":\"provider-gpt-5.5\"}"}}`)
	output, err := RedactUserModelRouteJSON(input, routedPrivacyInfo())
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, appcommon.Unmarshal(output, &decoded))
	assert.JSONEq(t, `{"message":"gpt-5.4 is unavailable","type":"gpt-5.4","code":"gpt-5.4","param":{"model":"gpt-5.4","arguments":"{\"model\":\"provider-gpt-5.5\"}"},"metadata":{"upstream_model":"gpt-5.4"}}`, string(decoded["error"]))
	assert.JSONEq(t, `{"error":{"message":"gpt-5.4 failed"},"metadata":{"model":"provider-gpt-5.5"}}`, string(decoded["response"]))
	assert.JSONEq(t, `{"content":"provider-gpt-5.5 is ordinary content"}`, string(decoded["message"]))
	assert.JSONEq(t, `{"arguments":"{\"model\":\"provider-gpt-5.5\"}"}`, string(decoded["item"]))
}

func TestRedactUserModelRouteJSONHandlesGeminiAndRealtimeModelFields(t *testing.T) {
	input := []byte(`{"modelVersion":"provider-gpt-5.5","session":{"model":"provider-gpt-5.5"},"response":{"model":"provider-gpt-5.5"},"metadata":{"modelVersion":"provider-gpt-5.5"}}`)
	output, err := RedactUserModelRouteJSON(input, routedPrivacyInfo())
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, appcommon.Unmarshal(output, &decoded))
	assert.JSONEq(t, `"gpt-5.4"`, string(decoded["modelVersion"]))
	assert.JSONEq(t, `{"model":"gpt-5.4"}`, string(decoded["session"]))
	assert.JSONEq(t, `{"model":"gpt-5.4"}`, string(decoded["response"]))
	assert.JSONEq(t, `{"modelVersion":"provider-gpt-5.5"}`, string(decoded["metadata"]))
}

func TestSanitizeUserModelRouteStructuredErrors(t *testing.T) {
	openAIError := SanitizeUserModelRouteOpenAIError(types.OpenAIError{
		Message: "provider-gpt-5.5 is unavailable",
		Type:    "provider-gpt-5.5",
		Code:    "provider-gpt-5.5",
		Param: map[string]any{
			"model":     "provider-gpt-5.5",
			"arguments": `{"model":"provider-gpt-5.5"}`,
		},
		Metadata: json.RawMessage(`{"target_model":"provider-gpt-5.5","content":"provider-gpt-5.5"}`),
	}, routedPrivacyInfo())

	assert.Equal(t, "gpt-5.4 is unavailable", openAIError.Message)
	assert.Equal(t, "gpt-5.4", openAIError.Type)
	assert.Equal(t, "gpt-5.4", openAIError.Code)
	assert.Equal(t, map[string]any{
		"model":     "gpt-5.4",
		"arguments": `{"model":"provider-gpt-5.5"}`,
	}, openAIError.Param)
	assert.JSONEq(t, `{"target_model":"gpt-5.4","content":"provider-gpt-5.5"}`, string(openAIError.Metadata))

	claudeError := SanitizeUserModelRouteClaudeError(types.ClaudeError{
		Message: "provider-gpt-5.5 is unavailable",
		Type:    "provider-gpt-5.5",
	}, routedPrivacyInfo())
	assert.Equal(t, "gpt-5.4 is unavailable", claudeError.Message)
	assert.Equal(t, "gpt-5.4", claudeError.Type)
}

func TestRedactUserModelRouteTextLeavesUnrelatedIdentifiersUntouched(t *testing.T) {
	info := routedPrivacyInfo()
	assert.Equal(t, "gpt-5.4 is ready; provider-gpt-5.50 remains separate", RedactUserModelRouteText("provider-gpt-5.5 is ready; provider-gpt-5.50 remains separate", info))
}

func TestRedactUserModelRouteTextAllowsMissingRouteInfo(t *testing.T) {
	assert.Equal(t, "upstream failure", RedactUserModelRouteText("upstream failure", nil))
	data := []byte(`{"error":"upstream failure"}`)
	redacted, err := RedactUserModelRouteJSON(data, nil)
	require.NoError(t, err)
	assert.Equal(t, data, redacted)
}

func TestRedactUserModelRouteTextRedactsLongestPrivateModelFirst(t *testing.T) {
	info := routedPrivacyInfo()
	info.RouteTargetModelName = "gpt-5.5"
	info.UpstreamModelName = "provider/gpt-5.5"

	assert.Equal(t, "gpt-5.4 and gpt-5.4", RedactUserModelRouteText("provider/gpt-5.5 and gpt-5.5", info))
}
