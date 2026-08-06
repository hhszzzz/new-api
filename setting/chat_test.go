package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatPresetsAreFilteredByAuthorizedUserGroups(t *testing.T) {
	original := Chats2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdateChatsByJsonString(original))
	})

	require.NoError(t, UpdateChatsByJsonString(`[
		{"Public":"https://public.example.com"},
		{"VIP":{"url":"https://vip.example.com","groups":[" vip ","vip"]}},
		{"Pro":{"url":"https://pro.example.com","groups":["pro"]}}
	]`))

	assert.JSONEq(t, `[
		{"Public":"https://public.example.com"},
		{"VIP":{"url":"https://vip.example.com","groups":["vip"]}},
		{"Pro":{"url":"https://pro.example.com","groups":["pro"]}}
	]`, Chats2JsonString())
	assert.Equal(t, []map[string]string{
		{"Public": "https://public.example.com"},
	}, GetChats())
	assert.Equal(t, []ChatPreset{
		{ID: "0", Name: "Public", URL: "https://public.example.com"},
		{ID: "1", Name: "VIP", URL: "https://vip.example.com"},
	}, GetChatPresetsForGroups([]string{"default", "vip"}))
	assert.Equal(t, []ChatPreset{
		{ID: "0", Name: "Public", URL: "https://public.example.com"},
	}, GetChatPresetsForGroups([]string{"default"}))
}

func TestInvalidRestrictedChatConfigPreservesPublishedPresets(t *testing.T) {
	original := Chats2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdateChatsByJsonString(original))
	})

	require.NoError(t, UpdateChatsByJsonString(`[{"Before":"https://before.example.com"}]`))
	before := Chats2JsonString()

	require.Error(t, UpdateChatsByJsonString(`[{"Broken":{"url":"https://broken.example.com","groups":"vip"}}]`))
	assert.JSONEq(t, before, Chats2JsonString())
	require.Error(t, UpdateChatsByJsonString(`[{"Broken":{"url":"","groups":["vip"]}}]`))
	assert.JSONEq(t, before, Chats2JsonString())
}
