package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionValidatesClientGroupPolicies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body, err := common.Marshal(map[string]interface{}{
		"key":   "client_policy_setting.group_policies",
		"value": `{"default":{"mode":"sometimes","clients":["codex"]}}`,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option", bytes.NewReader(body))

	UpdateOption(context)

	var response map[string]interface{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, false, response["success"])
	assert.Contains(t, response["message"], "invalid client policy mode")
}
