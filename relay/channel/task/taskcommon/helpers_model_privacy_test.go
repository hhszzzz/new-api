package taskcommon

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactModelRoutingTextOnlyReplacesCompleteIdentifier(t *testing.T) {
	tests := []struct {
		name              string
		text              string
		upstreamModelName string
		want              string
	}{
		{
			name: "standalone model",
			text: "gpt-4",
			want: "requested-model",
		},
		{
			name: "model in provider path",
			text: "projects/demo/models/gpt-4/operations/123",
			want: "projects/demo/models/requested-model/operations/123",
		},
		{
			name: "longer model with same prefix",
			text: "gpt-4o",
			want: "gpt-4o",
		},
		{
			name: "hyphenated error suffix is redacted",
			text: "gpt-4-turbo",
			want: "requested-model-turbo",
		},
		{
			name: "model adjacent to Chinese text",
			text: "模型gpt-4不可用",
			want: "模型requested-model不可用",
		},
		{
			name: "model inside Unicode quotes",
			text: "“gpt-4”不可用",
			want: "“requested-model”不可用",
		},
		{
			name: "model before sentence period",
			text: "model gpt-4.",
			want: "model requested-model.",
		},
		{
			name: "model in dotted error code",
			text: "error.model.gpt-4.not_found",
			want: "error.model.requested-model.not_found",
		},
		{
			name: "model in underscored error code",
			text: "invalid_model_gpt-4",
			want: "invalid_model_requested-model",
		},
		{
			name:              "mixed-case model",
			text:              "Provider-Internal-Model not found",
			upstreamModelName: "provider-internal-model",
			want:              "requested-model not found",
		},
		{
			name: "mixed-case longer model with same prefix",
			text: "GPT-4o",
			want: "GPT-4o",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstreamModelName := test.upstreamModelName
			if upstreamModelName == "" {
				upstreamModelName = "gpt-4"
			}
			assert.Equal(t, test.want, RedactModelRoutingText(test.text, "requested-model", upstreamModelName))
		})
	}
}

func TestSanitizePublicTaskDataPreservesLongerModelKey(t *testing.T) {
	data := []byte(`{
		"gpt-4o_result":"preserved",
		"route/gpt-4":"redacted",
		"gpt-4_internal":"also-redacted",
		"gpt-4.metadata":"metadata-redacted"
	}`)

	sanitized, err := SanitizePublicTaskData(data, "requested-model", "gpt-4")
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, common.Unmarshal(sanitized, &result))
	assert.Equal(t, "preserved", result["gpt-4o_result"])
	assert.NotContains(t, result, "route/gpt-4")
	assert.Equal(t, "redacted", result["route/requested-model"])
	assert.Equal(t, "also-redacted", result["requested-model_internal"])
	assert.Equal(t, "metadata-redacted", result["requested-model.metadata"])
}

func TestSanitizePublicTaskDataRedactsMixedCaseModelIdentifiers(t *testing.T) {
	data := []byte(`{
		"Provider-Internal-Model_result":"Provider-Internal-Model failed",
		"nested":{"model_name":"Provider-Internal-Model"}
	}`)

	sanitized, err := SanitizePublicTaskData(data, "requested-model", "provider-internal-model")
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, common.Unmarshal(sanitized, &result))
	assert.NotContains(t, result, "Provider-Internal-Model_result")
	assert.Equal(t, "requested-model failed", result["requested-model_result"])
	nested, ok := result["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "requested-model", nested["model_name"])
}

func TestSanitizePublicTaskDataPreservesUnrelatedEngineAndModelType(t *testing.T) {
	data := []byte(`{
		"engine":"render-v2",
		"model_type":"video",
		"deployment":"production",
		"deployment_id":42,
		"deployment_name":"primary"
	}`)

	sanitized, err := SanitizePublicTaskData(data, "requested-model", "provider-internal-model")
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, common.Unmarshal(sanitized, &result))
	assert.Equal(t, "render-v2", result["engine"])
	assert.Equal(t, "video", result["model_type"])
	assert.Equal(t, "production", result["deployment"])
	assert.Equal(t, float64(42), result["deployment_id"])
	assert.Equal(t, "primary", result["deployment_name"])
}

func TestSanitizePublicTaskDataRedactsEngineAndModelTypeRoutingReferences(t *testing.T) {
	data := []byte(`{
		"engine":"provider-internal-model",
		"model_type":"provider-internal-model",
		"deployment":"provider-internal-model",
		"deployment_id":"provider-internal-model",
		"deployment_name":"provider-internal-model"
	}`)

	sanitized, err := SanitizePublicTaskData(data, "requested-model", "provider-internal-model")
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, common.Unmarshal(sanitized, &result))
	assert.Equal(t, "requested-model", result["engine"])
	assert.Equal(t, "requested-model", result["model_type"])
	assert.Equal(t, "requested-model", result["deployment"])
	assert.Equal(t, "requested-model", result["deployment_id"])
	assert.Equal(t, "requested-model", result["deployment_name"])
}

func TestRedactModelRoutingTextIsIdempotentWhenOriginContainsUpstreamModel(t *testing.T) {
	const (
		originModel   = "openai/gpt-4"
		upstreamModel = "gpt-4"
	)

	once := RedactModelRoutingText("model gpt-4 is unavailable", originModel, upstreamModel)
	twice := RedactModelRoutingText(once, originModel, upstreamModel)

	assert.Equal(t, "model openai/gpt-4 is unavailable", once)
	assert.Equal(t, once, twice)
}

func TestSanitizePublicTaskDataUsesOriginalPublicKeyOnRedactionCollision(t *testing.T) {
	const (
		originModel   = "openai/gpt-4"
		upstreamModel = "gpt-4"
	)
	data := []byte(`{
		"route/gpt-4":"internal value",
		"route/openai/gpt-4":"public value"
	}`)

	once, err := SanitizePublicTaskData(data, originModel, upstreamModel)
	require.NoError(t, err)
	twice, err := SanitizePublicTaskData(once, originModel, upstreamModel)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, common.Unmarshal(twice, &result))
	assert.Len(t, result, 1)
	assert.Equal(t, "public value", result["route/openai/gpt-4"])
	assert.JSONEq(t, string(once), string(twice))
}
