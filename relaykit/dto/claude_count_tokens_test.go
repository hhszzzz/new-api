package dto

import (
	"strings"
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeTokenCountMetaIncludesDynamicToolsAndToolChoice(t *testing.T) {
	var request ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"name":"lookup","description":"find a record","input_schema":{"type":"object","properties":{"id":{"type":"string"}}}}],
		"tool_choice":{"type":"tool","name":"lookup"}
	}`), &request))

	meta := request.GetTokenCountMeta()
	require.NotNil(t, meta)
	assert.Equal(t, 1, meta.ToolsCount)
	assert.True(t, strings.Contains(meta.CombineText, "lookup"))
	assert.True(t, strings.Contains(meta.CombineText, "find a record"))
	assert.True(t, strings.Contains(meta.CombineText, `"type":"tool"`))
}

func TestClaudeTokenCountMetaExtractsToolResultImagesAsFiles(t *testing.T) {
	const base64PNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	var request ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"messages":[
			{"role":"user","content":"inspect the page"},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"screenshot","input":{"tab":"main"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[
				{"type":"text","text":"captured the page"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+base64PNG+`"}}
			]}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"plain string result"}]}
		]
	}`), &request))

	meta := request.GetTokenCountMeta()
	require.NotNil(t, meta)
	assert.Contains(t, meta.CombineText, "captured the page")
	assert.Contains(t, meta.CombineText, "plain string result")
	assert.NotContains(t, meta.CombineText, base64PNG)
	require.Len(t, meta.Files, 1)
	assert.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
}
