package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeTokenCountMetaIncludesDynamicToolsAndToolChoice(t *testing.T) {
	var request ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"claude-test",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"name":"lookup","description":"find a record","input_schema":{"type":"object","properties":{"id":{"type":"string"}}}}],
		"tool_choice":{"type":"tool","name":"lookup"}
	}`), &request))

	meta := request.GetTokenCountMeta()
	require.NotNil(t, meta)
	assert.Equal(t, 1, meta.ToolsCount)
	assert.True(t, strings.Contains(meta.CombineText, "lookup"))
	assert.True(t, strings.Contains(meta.CombineText, "input_schema"))
	assert.True(t, strings.Contains(meta.CombineText, "tool_choice") || strings.Contains(meta.CombineText, `"type":"tool"`))
}
