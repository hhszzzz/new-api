package controller

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func GetUserModelRoutes(c *gin.Context) {
	userId, user, ok := getManageableRouteUser(c)
	if !ok {
		return
	}
	routes, err := model.GetUserModelRoutes(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	_ = user
	common.ApiSuccess(c, routes)
}

func getUserModelRouteApplicableGroups(userGroups []string) []string {
	usableGroups := service.GetAuthorizedUserGroups(userGroups)
	groups := make([]string, 0, len(usableGroups))
	for group := range usableGroups {
		group = strings.TrimSpace(group)
		if group == "" || group == "auto" || !ratio_setting.ContainsGroupRatio(group) {
			continue
		}
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}

func GetUserModelRouteCandidates(c *gin.Context) {
	_, user, ok := getManageableRouteUser(c)
	if !ok {
		return
	}

	sourceSet := make(map[string]struct{})
	for _, pricing := range model.GetPricing() {
		modelName := strings.TrimSpace(pricing.ModelName)
		if modelName != "" {
			sourceSet[modelName] = struct{}{}
		}
	}
	sourceModels := make([]string, 0, len(sourceSet))
	for modelName := range sourceSet {
		sourceModels = append(sourceModels, modelName)
	}
	sort.Strings(sourceModels)

	targetModels, err := model.GetUserModelRouteTargetModels()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	executionGroups := make([]string, 0)
	for group := range ratio_setting.GetGroupRatioCopy() {
		if group != "auto" {
			executionGroups = append(executionGroups, group)
		}
	}
	sort.Strings(executionGroups)

	channels := make([]model.UserModelRouteCandidateChannel, 0)
	channelCounts := make(map[string]int64)
	recommendedExecutionGroup := ""
	targetModel := strings.TrimSpace(c.Query("target_model"))
	executionGroup := strings.TrimSpace(c.Query("execution_group"))
	if targetModel != "" {
		channelCounts, err = model.GetUserModelRouteExecutionGroupChannelCounts(targetModel)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, group := range executionGroups {
			if channelCounts[group] > 0 {
				recommendedExecutionGroup = group
				break
			}
		}
	}
	if targetModel != "" && executionGroup != "" {
		if executionGroup == "auto" || !ratio_setting.ContainsGroupRatio(executionGroup) {
			common.ApiErrorMsg(c, fmt.Sprintf("执行分组不存在：%s", executionGroup))
			return
		}
		channels, err = model.GetUserModelRouteCandidateChannels(executionGroup, targetModel)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for i := range channels {
			candidate := &channels[i]
			channel := &model.Channel{
				Id:                      candidate.Id,
				Name:                    candidate.Name,
				Type:                    candidate.Type,
				BaseURL:                 candidate.BaseURL,
				OtherSettings:           candidate.OtherSettings,
				AggregateId:             candidate.AggregateId,
				AggregateName:           candidate.AggregateName,
				AggregateBaseURL:        candidate.AggregateBaseURL,
				InheritAggregateBaseURL: candidate.InheritAggregateURL,
			}
			matrix := channelcompat.Matrix(channel, targetModel)
			candidate.ProtocolCompatibility = make(map[string]string, len(matrix))
			for protocol, compatibility := range matrix {
				candidate.ProtocolCompatibility[protocol] = string(compatibility.Status)
			}
		}
	}

	common.ApiSuccess(c, gin.H{
		"source_models":                  sourceModels,
		"target_models":                  targetModels,
		"applicable_groups":              getUserModelRouteApplicableGroups(user.Groups),
		"execution_groups":               executionGroups,
		"execution_group_channel_counts": channelCounts,
		"recommended_execution_group":    recommendedExecutionGroup,
		"channels":                       channels,
	})
}

func CreateUserModelRoute(c *gin.Context) {
	userId, user, ok := getManageableRouteUser(c)
	if !ok {
		return
	}
	var route model.UserModelRoute
	if err := common.DecodeJson(c.Request.Body, &route); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	route.Id = 0
	route.UserId = userId
	if !validateUserModelRoute(c, user, &route) {
		return
	}
	if err := model.SaveUserModelRoute(&route); err != nil {
		if errors.Is(err, model.ErrUserModelRouteConflict) {
			common.ApiErrorMsg(c, "同一请求模型的适用分组不能与已有规则重叠")
			return
		}
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, userId, "user.model_route.create", map[string]interface{}{
		"route_id": route.Id,
		"source":   route.SourceModel,
	})
	common.ApiSuccess(c, &route)
}

func UpdateUserModelRoute(c *gin.Context) {
	userId, user, ok := getManageableRouteUser(c)
	if !ok {
		return
	}
	routeId, err := strconv.Atoi(c.Param("route_id"))
	if err != nil || routeId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var route model.UserModelRoute
	if err := common.DecodeJson(c.Request.Body, &route); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	route.Id = routeId
	route.UserId = userId
	if !validateUserModelRoute(c, user, &route) {
		return
	}
	if err := model.SaveUserModelRoute(&route); err != nil {
		if errors.Is(err, model.ErrUserModelRouteConflict) {
			common.ApiErrorMsg(c, "同一请求模型的适用分组不能与已有规则重叠")
			return
		}
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, userId, "user.model_route.update", map[string]interface{}{
		"route_id": route.Id,
		"source":   route.SourceModel,
	})
	common.ApiSuccess(c, &route)
}

func DeleteUserModelRoute(c *gin.Context) {
	userId, _, ok := getManageableRouteUser(c)
	if !ok {
		return
	}
	routeId, err := strconv.Atoi(c.Param("route_id"))
	if err != nil || routeId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.DeleteUserModelRoute(userId, routeId); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, userId, "user.model_route.delete", map[string]interface{}{
		"route_id": routeId,
	})
	common.ApiSuccess(c, nil)
}

func getManageableRouteUser(c *gin.Context) (int, *model.User, bool) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return 0, nil, false
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return 0, nil, false
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return 0, nil, false
	}
	return userId, user, true
}

func validateUserModelRoute(c *gin.Context, user *model.User, route *model.UserModelRoute) bool {
	if user == nil || route == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return false
	}
	route.SourceModel = strings.TrimSpace(route.SourceModel)
	route.TargetModel = strings.TrimSpace(route.TargetModel)
	route.PoolName = strings.TrimSpace(route.PoolName)
	route.ExecutionGroup = strings.TrimSpace(route.ExecutionGroup)
	route.InjectPrompt = strings.TrimSpace(route.InjectPrompt)
	if route.SourceModel == "" || route.TargetModel == "" || route.ExecutionGroup == "" || len(route.ChannelIds) == 0 {
		common.ApiErrorMsg(c, "模型路由信息不完整")
		return false
	}
	if len(route.InjectPrompt) > model.UserModelRouteMaxInjectPrompt {
		common.ApiErrorMsg(c, fmt.Sprintf("注入提示词过长，最多 %d 个字符", model.UserModelRouteMaxInjectPrompt))
		return false
	}
	publicModels := make(map[string]struct{})
	for _, pricing := range model.GetPricing() {
		publicModels[pricing.ModelName] = struct{}{}
	}
	if _, exists := publicModels[route.SourceModel]; !exists {
		common.ApiErrorMsg(c, fmt.Sprintf("请求模型不是公开可用模型：%s", route.SourceModel))
		return false
	}
	if route.ExecutionGroup == "auto" || !ratio_setting.ContainsGroupRatio(route.ExecutionGroup) {
		common.ApiErrorMsg(c, fmt.Sprintf("执行分组不存在：%s", route.ExecutionGroup))
		return false
	}
	if route.AllGroups {
		route.Groups = []string{}
	} else {
		if len(route.Groups) == 0 {
			common.ApiErrorMsg(c, "请至少选择一个适用分组")
			return false
		}
		applicableGroups := make(map[string]struct{})
		for _, group := range getUserModelRouteApplicableGroups(user.Groups) {
			applicableGroups[group] = struct{}{}
		}
		for _, group := range route.Groups {
			group = strings.TrimSpace(group)
			if _, ok := applicableGroups[group]; !ok {
				common.ApiErrorMsg(c, fmt.Sprintf("用户未获授权使用分组：%s", group))
				return false
			}
		}
	}
	for _, channelId := range route.ChannelIds {
		channel, err := model.GetChannelById(channelId, false)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			common.ApiErrorMsg(c, fmt.Sprintf("渠道不可用：%d", channelId))
			return false
		}
		if !model.IsChannelEnabledForGroupModel(route.ExecutionGroup, route.TargetModel, channelId) {
			common.ApiErrorMsg(c, fmt.Sprintf("渠道 %d 不支持执行分组 %s 下的目标模型 %s", channelId, route.ExecutionGroup, route.TargetModel))
			return false
		}
	}
	return true
}
