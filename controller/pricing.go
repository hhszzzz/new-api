package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func getVisiblePricing(c *gin.Context) ([]model.Pricing, map[string]string, string, bool) {
	allPricing := model.GetPricing()
	pricing := allPricing
	userId, exists := c.Get("id")
	var group string
	hasUser := false
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			if len(user.Groups) > 0 {
				group = user.Groups[0]
			}
			usableGroup := service.GetAuthorizedUserGroups(user.Groups)
			pricing = filterPricingByUsableGroups(pricing, usableGroup)
			if user.Role != common.RoleRootUser {
				// A routed source model may have no native ability in the user's
				// groups; it is still a public model when an enabled route exposes it.
				routeSources, routeErr := model.GetEnabledUserModelRouteSources(user.Id, user.Groups)
				if routeErr == nil && len(routeSources) > 0 {
					visible := make(map[string]struct{}, len(pricing))
					for _, item := range pricing {
						visible[item.ModelName] = struct{}{}
					}
					for _, source := range routeSources {
						if _, exists := visible[source]; exists {
							continue
						}
						for _, item := range allPricing {
							if item.ModelName == source {
								pricing = append(pricing, item)
								visible[source] = struct{}{}
								break
							}
						}
					}
				}
			}
			if user.Role != common.RoleRootUser && user.ModelLimitsEnabled {
				allowed := make(map[string]struct{}, len(user.ModelLimits))
				for _, modelName := range user.ModelLimits {
					modelName = strings.TrimSpace(modelName)
					if modelName == "" {
						continue
					}
					allowed[modelName] = struct{}{}
					allowed[ratio_setting.FormatMatchingModelName(modelName)] = struct{}{}
				}
				filtered := make([]model.Pricing, 0, len(pricing))
				for _, item := range pricing {
					if _, exists := allowed[item.ModelName]; exists {
						filtered = append(filtered, item)
						continue
					}
					if _, exists := allowed[ratio_setting.FormatMatchingModelName(item.ModelName)]; exists {
						filtered = append(filtered, item)
					}
				}
				pricing = filtered
			}
			return pricing, usableGroup, group, true
		}
	}

	usableGroup := setting.GetUserUsableGroupsCopy()
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	return pricing, usableGroup, group, hasUser
}

func GetPricing(c *gin.Context) {
	pricing, usableGroup, group, hasUser := getVisiblePricing(c)
	autoGroups := setting.GetAutoGroups()
	if hasUser {
		if userId, ok := c.Get("id"); ok {
			if user, err := model.GetUserCache(userId.(int)); err == nil {
				autoGroups = service.GetUserAutoGroups(user.Groups)
			}
		}
	}
	groupRatio := map[string]float64{}
	for s, f := range ratio_setting.GetGroupRatioCopy() {
		groupRatio[s] = f
		if hasUser {
			if ratio, ok := ratio_setting.GetGroupGroupRatio(group, s); ok {
				groupRatio[s] = ratio
			}
		}
	}

	// check groupRatio contains usableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}

	c.JSON(200, gin.H{
		"success":            true,
		"data":               pricing,
		"vendors":            model.GetVendors(),
		"group_ratio":        groupRatio,
		"usable_group":       usableGroup,
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"auto_groups":        autoGroups,
		"pricing_version":    "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
