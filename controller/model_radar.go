package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetModelRadar(c *gin.Context) {
	data, err := service.GetModelRadar(c.Request.Context())
	if errors.Is(err, service.ErrModelRadarUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"code":    "data_unavailable",
			"message": "model radar data is not available yet",
		})
		return
	}
	if err != nil {
		logger.LogError(c.Request.Context(), "failed to load model radar data: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"code":    "internal_error",
			"message": "model radar data is temporarily unavailable",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}
