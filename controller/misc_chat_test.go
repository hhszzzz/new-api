package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserChatPresetsDoesNotExposeOtherGroups(t *testing.T) {
	original := setting.Chats2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateChatsByJsonString(original))
	})
	require.NoError(t, setting.UpdateChatsByJsonString(`[
		{"Public":"https://public.example.com"},
		{"VIP":{"url":"https://vip.example.com","groups":["vip"]}}
	]`))

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(context, constant.ContextKeyUserGroups, []string{"default"})

	GetUserChatPresets(context)

	require.Equal(t, 200, recorder.Code)
	var response struct {
		Success bool                 `json:"success"`
		Data    []setting.ChatPreset `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, []setting.ChatPreset{
		{ID: "0", Name: "Public", URL: "https://public.example.com"},
	}, response.Data)
}
