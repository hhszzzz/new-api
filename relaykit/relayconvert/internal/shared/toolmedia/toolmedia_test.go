package toolmedia

import (
	"strings"
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanChatToolOutputPreservesNoMediaRepresentation(t *testing.T) {
	plan, err := PlanChatToolOutput([]any{map[string]any{"type": "text", "text": "plain"}})
	require.NoError(t, err)
	assert.Nil(t, plan)
}

func TestPlanChatToolOutputExtractsNestedImageAndClampsResidualBase64(t *testing.T) {
	encoded := mustJSON(t, map[string]any{
		"content": []any{
			map[string]any{
				"type":      "image",
				"mimeType":  "image/webp",
				"data":      "MCP_IMAGE_SENTINEL",
				"transient": true,
			},
			map[string]any{"type": "video", "data": strings.Repeat("A", 20_000)},
		},
	})

	plan, err := PlanChatToolOutput(encoded)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Len(t, plan.Media, 1)
	assert.Equal(t, "data:image/webp;base64,MCP_IMAGE_SENTINEL", ImageURL(plan.Media[0]))
	assert.Contains(t, plan.Content, ToolResultMediaMovedMarker)
	assert.Contains(t, plan.Content, "[new-api: omitted 20000 bytes]")
	assert.NotContains(t, plan.Content, "MCP_IMAGE_SENTINEL")
	assert.NotContains(t, plan.Content, strings.Repeat("A", 64))
}

func TestInlineImagesOnlyRejectsRemoteAndMalformedImages(t *testing.T) {
	for _, value := range []any{
		map[string]any{
			"type": "image",
			"source": map[string]any{
				"type": "url",
				"url":  "https://example.test/image.png",
			},
		},
		map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": "data:image/png,NOT_BASE64"},
		},
	} {
		_, media, changed, err := StripAndClamp(
			value,
			InlineImagesOnly,
			map[string]any{"type": "text", "text": ToolResultMediaAttachedMarker},
			ToolResultMediaAttachedMarker,
		)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Empty(t, media)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}
