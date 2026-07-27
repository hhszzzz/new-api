package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]interface{})
	userId := c.GetInt("id")
	userGroups := []string{"default"}
	primaryGroup := "default"
	if userId > 0 {
		user, err := model.GetUserCache(userId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userGroups = user.Groups
		primaryGroup = ""
		if len(user.Groups) > 0 {
			primaryGroup = user.Groups[0]
		}
	}
	usable := service.GetAuthorizedUserGroups(userGroups)
	for groupName, desc := range usable {
		if groupName == "auto" {
			continue
		}
		usableGroups[groupName] = map[string]interface{}{
			"ratio": service.GetUserGroupRatio(primaryGroup, groupName),
			"desc":  desc,
		}
	}
	if _, ok := usable["auto"]; ok {
		usableGroups["auto"] = map[string]interface{}{
			"ratio": "自动",
			"desc":  setting.GetUsableGroupDescription("auto"),
		}
	}
	common.ApiSuccess(c, usableGroups)
}
