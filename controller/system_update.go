package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const systemUpdateCheckTimeout = 20 * time.Second

func GetSystemUpdate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), systemUpdateCheckTimeout)
	defer cancel()

	info, err := service.CheckSystemUpdate(ctx, common.Version)
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf("system update check failed: %v", err))
		common.ApiErrorMsg(c, "Failed to check GHCR image updates")
		return
	}
	common.ApiSuccess(c, info)
}

func GetSystemUpdateState(c *gin.Context) {
	common.ApiSuccess(c, service.GetSystemUpdateTriggerState())
}

func ApplySystemUpdate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), systemUpdateCheckTimeout)
	defer cancel()

	result, err := service.StartSystemUpdate(ctx, common.Version)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSystemUpdateNotConfigured):
			common.ApiErrorMsg(c, "One-click update is not configured on this deployment.")
		case errors.Is(err, service.ErrSystemUpdateInProgress):
			common.ApiErrorMsg(c, "A system update is already in progress.")
		default:
			logger.LogWarn(c, fmt.Sprintf("system update start failed: %v", err))
			common.ApiErrorMsg(c, "Failed to trigger system update")
		}
		return
	}

	if result.Started {
		recordManageAudit(c, "system.update.trigger", map[string]interface{}{
			"image":           result.Update.Image,
			"target_version":  result.Update.LatestVersion,
			"target_revision": result.Update.LatestRevision,
		})
	}
	common.ApiSuccess(c, result)
}
