package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

const perfMetricsUnavailableMessage = "performance metrics are temporarily unavailable"

func GetPerfMetricsSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	pricing, usableGroups, _, _ := getVisiblePricing(c)
	activeGroupRatios := ratio_setting.GetGroupRatioCopy()
	activeGroups := make([]string, 0, len(usableGroups))
	for group := range usableGroups {
		if _, ok := activeGroupRatios[group]; ok {
			activeGroups = append(activeGroups, group)
		}
	}
	result, err := perfmetrics.QuerySummaryAll(hours, activeGroups)
	if err != nil {
		logger.LogError(c, "failed to query performance metrics summary: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": perfMetricsUnavailableMessage,
		})
		return
	}
	visibleModels := make(map[string]struct{}, len(pricing))
	for _, item := range pricing {
		visibleModels[item.ModelName] = struct{}{}
	}
	result.Models = lo.Filter(result.Models, func(item perfmetrics.ModelSummary, _ int) bool {
		_, ok := visibleModels[item.ModelName]
		return ok
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}
	pricing, usableGroups, _, _ := getVisiblePricing(c)
	visibleModels := make(map[string]struct{}, len(pricing))
	for _, item := range pricing {
		visibleModels[item.ModelName] = struct{}{}
	}
	if _, ok := visibleModels[modelName]; !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "model is not available",
		})
		return
	}
	activeGroupRatios := ratio_setting.GetGroupRatioCopy()
	requestedGroup := c.Query("group")
	if requestedGroup != "" {
		_, usable := usableGroups[requestedGroup]
		_, active := activeGroupRatios[requestedGroup]
		if !usable || !active {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "model is not available",
			})
			return
		}
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model: modelName,
		Group: requestedGroup,
		Hours: hours,
	})
	if err != nil {
		logger.LogError(c, "failed to query performance metrics: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": perfMetricsUnavailableMessage,
		})
		return
	}

	result.Groups = lo.Filter(result.Groups, func(group perfmetrics.GroupResult, _ int) bool {
		if _, ok := usableGroups[group.Group]; !ok {
			return false
		}
		_, active := activeGroupRatios[group.Group]
		return active
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetricsStatus(c *gin.Context) {
	pricing, usableGroups, _, _ := getVisiblePricing(c)
	vendorNames := make(map[int]string)
	for _, vendor := range model.GetVendors() {
		vendorNames[vendor.ID] = vendor.Name
	}

	models := make([]perfmetrics.StatusModelSource, 0, len(pricing))
	for _, item := range pricing {
		models = append(models, perfmetrics.StatusModelSource{
			ModelName: item.ModelName,
			Vendor:    vendorNames[item.VendorID],
		})
	}

	activeGroupRatios := ratio_setting.GetGroupRatioCopy()
	activeGroups := make([]string, 0, len(usableGroups))
	for group := range usableGroups {
		if _, ok := activeGroupRatios[group]; ok {
			activeGroups = append(activeGroups, group)
		}
	}
	result, err := perfmetrics.QueryStatus(models, activeGroups)
	if err != nil {
		logger.LogError(c, "failed to query model status metrics: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": perfMetricsUnavailableMessage,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func visiblePerfModelSet(c *gin.Context) map[string]struct{} {
	modelNames := getVisibleModelNames(c)
	visibleModels := make(map[string]struct{}, len(modelNames))
	for _, modelName := range modelNames {
		visibleModels[modelName] = struct{}{}
	}
	return visibleModels
}
