package controller

import (
	"fmt"
	"net/http"
	"strconv"

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
	period := c.DefaultQuery("period", "week")
	var startTimestamp *int64
	var endTimestamp *int64
	if period == "custom" {
		var err error
		startTimestamp, err = parseRankingTimestamp(c, "start_timestamp")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		endTimestamp, err = parseRankingTimestamp(c, "end_timestamp")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
	}

	viewer := service.RankingViewerAnonymous
	if canViewPrivate {
		viewer = service.RankingViewerAdmin
	} else if c.GetInt("id") > 0 {
		viewer = service.RankingViewerUser
	}
	result, err := service.GetRankingsSnapshotWithOptions(service.RankingsRequest{
		Period:         period,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		VisibleModels:  visibleModelNames,
		Viewer:         viewer,
	})
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

func parseRankingTimestamp(c *gin.Context, key string) (*int64, error) {
	value, ok := c.GetQuery(key)
	if !ok || value == "" {
		return nil, fmt.Errorf("custom ranking period requires %s", key)
	}
	timestamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil || timestamp <= 0 {
		return nil, fmt.Errorf("invalid %s", key)
	}
	return &timestamp, nil
}
