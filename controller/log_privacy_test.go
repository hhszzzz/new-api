package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUserLogEndpointsRejectHiddenChannelSelectors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		target  string
		handler gin.HandlerFunc
	}{
		{name: "channel sort", target: "/api/log/self?sort_by=channel", handler: GetUserLogs},
		{name: "normalized channel sort", target: "/api/log/self?sort_by=CHANNEL", handler: GetUserLogs},
		{name: "model sort", target: "/api/log/self?sort_by=model_name", handler: GetUserLogs},
		{name: "upstream request filter", target: "/api/log/self?upstream_request_id=private", handler: GetUserLogs},
		{name: "channel statistics filter", target: "/api/log/self/stat?channel=7", handler: GetLogsSelfStat},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, test.target, nil)
			context.Set("id", 1)
			context.Set("role", common.RoleCommonUser)

			test.handler(context)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "not available")
		})
	}
}
