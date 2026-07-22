package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetRankings(c *gin.Context) {
	canViewPrivate := c.GetInt("role") >= common.RoleAdminUser
	var visibleModelNames []string
	if !canViewPrivate {
		visibleModelNames = getVisibleModelNames(c)
	}
	result, err := service.GetRankingsSnapshot(
		c.DefaultQuery("period", "week"),
		visibleModelNames,
		canViewPrivate,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
