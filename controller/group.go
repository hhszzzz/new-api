package controller

import (
	"errors"
	"net/http"
	"sort"
	"strings"

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
	sort.Strings(groupNames)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

type groupMutationRequest struct {
	OldName     string  `json:"old_name"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Ratio       float64 `json:"ratio"`
	SortOrder   int     `json:"sort_order"`
}

func GetGroupRegistry(c *gin.Context) {
	groups, err := model.ListGroups()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, groups)
}

func GetGroupHistory(c *gin.Context) {
	history, err := model.GroupHistoryNames()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, history)
}

func CreateGroup(c *gin.Context) {
	var request groupMutationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpsertGroup(model.Group{
		Name:        request.Name,
		Description: request.Description,
		Ratio:       request.Ratio,
		SortOrder:   request.SortOrder,
	}); err != nil {
		writeGroupMutationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func UpdateGroup(c *gin.Context) {
	var request groupMutationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	group := model.Group{
		Name:        request.Name,
		Description: request.Description,
		Ratio:       request.Ratio,
		SortOrder:   request.SortOrder,
	}
	oldName := strings.TrimSpace(request.OldName)
	var err error
	if oldName != "" && oldName != strings.TrimSpace(request.Name) {
		err = model.RenameGroup(oldName, group)
	} else {
		err = model.UpsertGroup(group)
	}
	if err != nil {
		writeGroupMutationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func DeleteGroup(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		name = c.Query("name")
	}
	if err := model.DeleteGroup(name); err != nil {
		writeGroupMutationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func writeGroupMutationError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, model.ErrGroupNameExists) || errors.Is(err, model.ErrGroupHasSubscriptions) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]any)
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
		usableGroups[groupName] = map[string]any{
			"ratio":   service.GetUserGroupRatio(primaryGroup, groupName),
			"desc":    desc,
			"primary": groupName == primaryGroup,
		}
	}
	if _, ok := usable["auto"]; ok {
		usableGroups["auto"] = map[string]any{
			"ratio": "auto",
			"desc":  setting.GetUsableGroupDescription("auto"),
		}
	}
	common.ApiSuccess(c, usableGroups)
}
