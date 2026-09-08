package controller

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
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

func TriggerModelRadarSync(c *gin.Context) {
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeModelRadarSync, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !created {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "a model radar sync is already running or pending",
			"data":    gin.H{"task_id": task.TaskID, "status": task.Status, "type": task.Type},
		})
		return
	}
	recordManageAudit(c, "model_radar.sync", map[string]interface{}{"task_id": task.TaskID})
	common.ApiSuccess(c, task.ToResponse())
}

func GetModelRadarManagement(c *gin.Context) {
	data, err := service.GetModelRadar(c.Request.Context())
	if err != nil && !errors.Is(err, service.ErrModelRadarUnavailable) {
		common.ApiError(c, err)
		return
	}
	task, err := model.GetActiveSystemTask(model.SystemTaskTypeModelRadarSync)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var currentTask *model.SystemTaskResponse
	if task != nil {
		response := task.ToResponse()
		currentTask = &response
	}
	var snapshot any
	if data != nil {
		type modelSummary struct {
			Model              string   `json:"model"`
			ConfigurationCount int      `json:"configuration_count"`
			Efforts            []string `json:"efforts"`
		}
		byModel := make(map[string]*modelSummary)
		for _, configuration := range data.Configurations {
			summary := byModel[configuration.Model]
			if summary == nil {
				summary = &modelSummary{Model: configuration.Model, Efforts: []string{}}
				byModel[configuration.Model] = summary
			}
			summary.ConfigurationCount++
			summary.Efforts = append(summary.Efforts, configuration.Effort)
		}
		models := make([]modelSummary, 0, len(byModel))
		for _, summary := range byModel {
			sort.Strings(summary.Efforts)
			models = append(models, *summary)
		}
		sort.Slice(models, func(i, j int) bool { return models[i].Model < models[j].Model })
		snapshot = gin.H{
			"fetched_at": data.FetchedAt, "source_updated_at": data.SourceUpdatedAt,
			"alerts_updated_at": data.AlertsUpdatedAt, "stale": data.Stale,
			"model_count": len(models), "configuration_count": len(data.Configurations),
			"alert_count": len(data.DegradationAlerts), "models": models,
		}
	}
	common.ApiSuccess(c, gin.H{
		"settings": setting.GetModelRadarSettings(),
		"sync": gin.H{
			"enabled":             service.ModelRadarSyncEnabled(),
			"interval_minutes":    int(service.ModelRadarSyncInterval() / time.Minute),
			"stale_after_minutes": int(service.ModelRadarStaleAfter() / time.Minute),
		},
		"snapshot": snapshot, "current_task": currentTask,
	})
}
