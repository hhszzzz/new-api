package controller

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/account_pool_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type accountPoolSettingsResponse struct {
	account_pool_setting.Setting
	service.AccountPoolSyncStatus
}

func GetAccountPool(c *gin.Context) {
	role, allowed := authorizeAccountPool(c)
	if !allowed {
		respondAccountPoolError(c, http.StatusForbidden, "account_pool_forbidden", "Account pool access is not allowed")
		return
	}

	view, err := service.GetAccountPool(c.Request.Context(), role >= common.RoleAdminUser)
	if err != nil {
		respondAccountPoolServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": view})
}

func RefreshAccountPool(c *gin.Context) {
	role, allowed := authorizeAccountPool(c)
	if !allowed {
		respondAccountPoolError(c, http.StatusForbidden, "account_pool_forbidden", "Account pool access is not allowed")
		return
	}

	view, err := service.RefreshAccountPool(c.Request.Context(), role >= common.RoleAdminUser)
	if err != nil {
		var cooldownError *service.AccountPoolManualRefreshCooldownError
		if errors.As(err, &cooldownError) {
			retryAfter := int(math.Ceil(cooldownError.RetryAfter.Seconds()))
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			respondAccountPoolError(c, http.StatusTooManyRequests, "account_pool_refresh_cooldown", "Manual refresh is cooling down")
			return
		}
		respondAccountPoolServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": view})
}

func GetAccountPoolSettings(c *gin.Context) {
	setting := account_pool_setting.GetSettingSnapshot()
	if setting == nil {
		respondAccountPoolError(c, http.StatusInternalServerError, "account_pool_settings_unavailable", "Account pool settings are unavailable")
		return
	}
	response := accountPoolSettingsResponse{
		Setting:               *setting,
		AccountPoolSyncStatus: service.GetAccountPoolSyncStatus(),
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}

func UpdateAccountPoolSettings(c *gin.Context) {
	var request account_pool_setting.Setting
	if err := c.ShouldBindJSON(&request); err != nil {
		respondAccountPoolError(c, http.StatusBadRequest, "account_pool_settings_invalid", "Invalid account pool settings")
		return
	}
	prepared, err := account_pool_setting.PrepareSetting(request)
	if err != nil {
		respondAccountPoolError(c, http.StatusBadRequest, "account_pool_settings_invalid", err.Error())
		return
	}
	availableGroups := ratio_setting.GetGroupRatioCopy()
	for _, group := range prepared.AllowedGroups {
		if _, exists := availableGroups[group]; !exists {
			respondAccountPoolError(c, http.StatusBadRequest, "account_pool_settings_invalid", "Allowed groups must reference existing user groups")
			return
		}
	}
	if err := model.UpdateAccountPoolSetting(prepared); err != nil {
		respondAccountPoolError(c, http.StatusInternalServerError, "account_pool_settings_update_failed", "Failed to update account pool settings")
		return
	}
	service.InvalidateAccountPoolCache()
	recordManageAudit(c, "option.account_pool.update", map[string]interface{}{
		"enabled":        prepared.Enabled,
		"allowed_groups": prepared.AllowedGroups,
	})

	response := accountPoolSettingsResponse{
		Setting:               prepared,
		AccountPoolSyncStatus: service.GetAccountPoolSyncStatus(),
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}

func authorizeAccountPool(c *gin.Context) (int, bool) {
	role := c.GetInt("role")
	groups := common.GetContextKeyStringSlice(c, constant.ContextKeyUserGroups)
	if len(groups) == 0 {
		if legacyGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup); legacyGroup != "" {
			groups = []string{legacyGroup}
		}
	}
	return role, account_pool_setting.CanAccess(role, groups)
}

func respondAccountPoolServiceError(c *gin.Context, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		respondAccountPoolError(c, http.StatusServiceUnavailable, "account_pool_timeout", "Account pool refresh timed out")
		return
	}
	if errors.Is(err, service.ErrAccountPoolNotConfigured) {
		respondAccountPoolError(c, http.StatusServiceUnavailable, "account_pool_not_configured", "Account pool management connection is not configured")
		return
	}
	respondAccountPoolError(c, http.StatusServiceUnavailable, "account_pool_unavailable", "Account pool quota data is unavailable")
}

func respondAccountPoolError(c *gin.Context, status int, code string, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"success": false,
		"message": message,
		"code":    code,
	})
}
