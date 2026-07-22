package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCanViewModelRoutingRequiresAdminRole(t *testing.T) {
	tests := []struct {
		name     string
		role     int
		expected bool
	}{
		{name: "common user", role: common.RoleCommonUser, expected: false},
		{name: "administrator", role: common.RoleAdminUser, expected: true},
		{name: "root", role: common.RoleRootUser, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("role", tt.role)
			require.Equal(t, tt.expected, canViewModelRouting(ctx))
		})
	}
}

func TestIsAllowedUserLogModelFilterUsesExactVisibleModel(t *testing.T) {
	visibleModels := map[string]struct{}{
		"public-model": {},
		"model_1":      {},
	}
	tests := []struct {
		name      string
		modelName string
		expected  bool
	}{
		{name: "empty filter", modelName: "", expected: true},
		{name: "visible exact model", modelName: "public-model", expected: true},
		{name: "percent wildcard", modelName: "%model", expected: false},
		{name: "hidden model", modelName: "private-model", expected: false},
		{name: "underscore is literal", modelName: "model_1", expected: true},
		{name: "underscore does not wildcard", modelName: "modelA1", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, isAllowedUserLogModelFilter(tt.modelName, visibleModels))
		})
	}
}
