package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func canViewModelRouting(c *gin.Context) bool {
	return c.GetInt("role") >= common.RoleAdminUser
}

func isAllowedUserLogModelFilter(modelName string, visibleModels map[string]struct{}) bool {
	if modelName == "" {
		return true
	}
	// Percent is the only wildcard accepted by the log query layer. Public
	// callers must use an exact, currently visible catalog model instead.
	if strings.Contains(modelName, "%") {
		return false
	}
	_, ok := visibleModels[modelName]
	return ok
}
