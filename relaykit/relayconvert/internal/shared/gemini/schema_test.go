package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareFunctionDeclarationUsesRestrictedParametersForSimpleSchema(t *testing.T) {
	function := dto.FunctionRequest{
		Name: "lookup",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"q": map[string]interface{}{"type": "string"},
			},
		},
	}

	PrepareFunctionDeclaration(&function)

	assert.Nil(t, function.ParametersJsonSchema)
	parameters, ok := function.Parameters.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "OBJECT", parameters["type"])
	properties, ok := parameters["properties"].(map[string]interface{})
	require.True(t, ok)
	query, ok := properties["q"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "STRING", query["type"])
}

func TestPrepareFunctionDeclarationUsesJSONSchemaForRichConstraints(t *testing.T) {
	function := dto.FunctionRequest{
		Name: "lookup",
		Parameters: map[string]interface{}{
			"$schema":              "https://json-schema.org/draft/2020-12/schema",
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"q": map[string]interface{}{
					"type":             "number",
					"exclusiveMinimum": 0,
				},
			},
		},
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
	assert.Equal(t, 0, query["exclusiveMinimum"])
}

func TestPrepareFunctionDeclarationKeepsExplicitObjectForNoArgumentTool(t *testing.T) {
	function := dto.FunctionRequest{Name: "status"}

	PrepareFunctionDeclaration(&function)

	parameters, ok := function.Parameters.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "OBJECT", parameters["type"])
	properties, ok := parameters["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, properties)
}
