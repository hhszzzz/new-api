package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistributorRejectsDirectRequestWhenUserModelPermissionIsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"restricted-model","messages":[]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyUserModelLimitEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyUserModelLimit, map[string]bool{})
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	Distribute()(ctx)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	require.True(t, ctx.IsAborted())
	assert.Contains(t, recorder.Body.String(), string(types.ErrorCodeModelNotFound))
	assert.NotContains(t, recorder.Body.String(), "restricted-model")
}
