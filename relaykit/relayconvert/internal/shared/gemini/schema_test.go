package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rawSchema(t *testing.T, schema any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(schema)
	require.NoError(t, err)
	return raw
}

func decodedSchema(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var schema map[string]any
	require.NoError(t, kitutil.Unmarshal(raw, &schema))
	return schema
}

func TestPrepareFunctionDeclarationUsesRestrictedParametersForSimpleSchema(t *testing.T) {
	function := dto.FunctionRequest{
		Name: "lookup",
		Parameters: rawSchema(t, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{"type": "string"},
			},
		}),
	}

	PrepareFunctionDeclaration(&function)

	assert.Nil(t, function.ParametersJsonSchema)
	parameters := decodedSchema(t, function.Parameters)
	assert.Equal(t, "OBJECT", parameters["type"])
	properties, ok := parameters["properties"].(map[string]any)
	require.True(t, ok)
	query, ok := properties["q"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "STRING", query["type"])
}

func TestPrepareFunctionDeclarationUsesJSONSchemaForRichConstraints(t *testing.T) {
	function := dto.FunctionRequest{
		Name: "lookup",
		Parameters: rawSchema(t, map[string]any{
			"$schema":              "https://json-schema.org/draft/2020-12/schema",
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"q": map[string]any{
					"type":             "number",
					"exclusiveMinimum": 0,
				},
			},
		}),
	}

	PrepareFunctionDeclaration(&function)

	assert.Nil(t, function.Parameters)
	schema, ok := function.ParametersJsonSchema.(map[string]interface{})
	require.True(t, ok)
	assert.NotContains(t, schema, "$schema")
	assert.Equal(t, false, schema["additionalProperties"])
	properties, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok)
	query, ok := properties["q"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(0), query["exclusiveMinimum"])
}

func TestPrepareFunctionDeclarationKeepsExplicitObjectForNoArgumentTool(t *testing.T) {
	function := dto.FunctionRequest{Name: "status"}

	PrepareFunctionDeclaration(&function)

	parameters := decodedSchema(t, function.Parameters)
	assert.Equal(t, "OBJECT", parameters["type"])
	properties, ok := parameters["properties"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, properties)
}
