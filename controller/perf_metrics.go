package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

const perfMetricsUnavailableMessage = "performance metrics are temporarily unavailable"

type perfMetricsStatusPointView struct {
	Ts           int64              `json:"ts"`
	Status       perfmetrics.Status `json:"status"`
	RequestCount *int64             `json:"request_count"`
	SuccessCount *int64             `json:"success_count"`
	SuccessRate  *float64           `json:"success_rate"`
	AvgTtftMs    *int64             `json:"avg_ttft_ms"`
	AvgLatencyMs *int64             `json:"avg_latency_ms"`
	AvgTps       *float64           `json:"avg_tps"`
}

type perfMetricsStatusModelView struct {
	ModelName    string                       `json:"model_name"`
	Vendor       string                       `json:"vendor"`
	Icon         string                       `json:"icon"`
	RequestCount *int64                       `json:"request_count"`
	SuccessCount *int64                       `json:"success_count"`
	SuccessRate  *float64                     `json:"success_rate"`
	AvgTtftMs    *int64                       `json:"avg_ttft_ms"`
	AvgLatencyMs *int64                       `json:"avg_latency_ms"`
	AvgTps       *float64                     `json:"avg_tps"`
	Status       perfmetrics.Status           `json:"status"`
	Timeline     []perfMetricsStatusPointView `json:"timeline"`
}

type perfMetricsStatusView struct {
	GeneratedAt int64                        `json:"generated_at"`
	WindowHours int                          `json:"window_hours"`
	Models      []perfMetricsStatusModelView `json:"models"`
}

func buildPerfMetricsStatusView(result perfmetrics.StatusResult, includeCounts bool) perfMetricsStatusView {
	view := perfMetricsStatusView{
		GeneratedAt: result.GeneratedAt,
		WindowHours: result.WindowHours,
		Models:      make([]perfMetricsStatusModelView, 0, len(result.Models)),
	}
	for _, item := range result.Models {
		modelView := perfMetricsStatusModelView{
			ModelName:    item.ModelName,
			Vendor:       item.Vendor,
			Icon:         item.Icon,
			SuccessRate:  item.SuccessRate,
			AvgTtftMs:    item.AvgTtftMs,
			AvgLatencyMs: item.AvgLatencyMs,
			AvgTps:       item.AvgTps,
			Status:       item.Status,
			Timeline:     make([]perfMetricsStatusPointView, 0, len(item.Timeline)),
		}
		if includeCounts {
			modelView.RequestCount = lo.ToPtr(item.RequestCount)
			modelView.SuccessCount = lo.ToPtr(item.SuccessCount)
		}
		for _, point := range item.Timeline {
			pointView := perfMetricsStatusPointView{
				Ts:           point.Ts,
				Status:       point.Status,
				SuccessRate:  point.SuccessRate,
				AvgTtftMs:    point.AvgTtftMs,
				AvgLatencyMs: point.AvgLatencyMs,
				AvgTps:       point.AvgTps,
			}
			if includeCounts {
				pointView.RequestCount = lo.ToPtr(point.RequestCount)
				pointView.SuccessCount = lo.ToPtr(point.SuccessCount)
			}
			modelView.Timeline = append(modelView.Timeline, pointView)
		}
		view.Models = append(view.Models, modelView)
	}
	return view
}

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
	vendors := make(map[int]model.PricingVendor)
	for _, vendor := range model.GetVendors() {
		vendors[vendor.ID] = vendor
	}

	models := make([]perfmetrics.StatusModelSource, 0, len(pricing))
	for _, item := range pricing {
		vendor := vendors[item.VendorID]
		icon := vendor.Icon
		if strings.TrimSpace(icon) == "" {
			icon = item.Icon
		}
		models = append(models, perfmetrics.StatusModelSource{
			ModelName: item.ModelName,
			Vendor:    vendor.Name,
			Icon:      icon,
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
		"data":    buildPerfMetricsStatusView(result, c.GetInt("id") > 0),
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
