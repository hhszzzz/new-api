package service

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func getAssignedUserGroups(userGroups []string) map[string]string {
	descriptions := setting.GetUserUsableGroupsCopy()
	configuredGroups := ratio_setting.GetGroupRatioCopy()
	explicit := make(map[string]struct{}, len(userGroups))
	groups := make(map[string]string, len(userGroups))
	for _, group := range userGroups {
		group = strings.TrimSpace(group)
		if group == "" || group == "auto" {
			continue
		}
		if _, exists := configuredGroups[group]; !exists {
			continue
		}
		description := strings.TrimSpace(descriptions[group])
		if description == "" {
			description = group
		}
		explicit[group] = struct{}{}
		groups[group] = description
	}
	primaryGroup := ""
	if len(userGroups) > 0 {
		primaryGroup = strings.TrimSpace(userGroups[0])
	}
	granted := make(map[string]string)
	hidden := make(map[string]struct{})
	rules := ratio_setting.GetGroupSpecialUsableGroupCopy()[primaryGroup]
	if primaryGroup == "" {
		rules = nil
	}
	for rawTarget, description := range rules {
		visible, target := parseSpecialUsableGroupRule(rawTarget)
		if target == "" || target == "auto" {
			continue
		}
		if _, exists := configuredGroups[target]; !exists {
			continue
		}
		if visible {
			granted[target] = strings.TrimSpace(description)
		} else {
			hidden[target] = struct{}{}
		}
	}
	for group := range hidden {
		if _, assigned := explicit[group]; !assigned {
			delete(groups, group)
			delete(granted, group)
		}
	}
	for group, description := range granted {
		if _, assigned := explicit[group]; assigned {
			continue
		}
		if description == "" {
			description = strings.TrimSpace(descriptions[group])
		}
		if description == "" {
			description = group
		}
		groups[group] = description
	}
	return groups
}

func parseSpecialUsableGroupRule(rawTarget string) (bool, string) {
	if strings.HasPrefix(rawTarget, "-:") {
		return false, strings.TrimPrefix(rawTarget, "-:")
	}
	return true, strings.TrimPrefix(rawTarget, "+:")
}

// GetUserUsableGroups retains the legacy single-user-group contract.
func GetUserUsableGroups(userGroup string) map[string]string {
	return GetAuthorizedUserGroups([]string{userGroup})
}

// GetUserUsableGroupsForGroups retains the historical helper name. User group
// memberships assigned by administrators are the authorization source.
func GetUserUsableGroupsForGroups(userGroups []string) map[string]string {
	return GetAuthorizedUserGroups(userGroups)
}

func GetAuthorizedUserGroups(userGroups []string) map[string]string {
	groups := getAssignedUserGroups(userGroups)
	if len(getAutoGroups(groups)) > 0 {
		groups["auto"] = setting.GetUsableGroupDescription("auto")
	}
	return groups
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

func GroupInUserUsableGroupsForGroups(userGroups []string, groupName string) bool {
	_, ok := GetAuthorizedUserGroups(userGroups)[groupName]
	return ok
}

func UserHasGroup(groups []string, groupName string) bool {
	for _, group := range groups {
		if group == groupName {
			return true
		}
	}
	return false
}

// GetUserAutoGroup retains the legacy single-user-group contract.
func GetUserAutoGroup(userGroup string) []string {
	return GetUserAutoGroups([]string{userGroup})
}

func GetUserAutoGroups(userGroups []string) []string {
	return getAutoGroups(getAssignedUserGroups(userGroups))
}

func getAutoGroups(usableGroups map[string]string) []string {
	configured := make([]string, 0, len(usableGroups))
	for group := range usableGroups {
		if group != "auto" {
			configured = append(configured, group)
		}
	}
	slices.Sort(configured)
	return configured
}

func IsUserSelectableGroup(userGroup, groupName string) bool {
	return IsUserSelectableGroupForGroups([]string{userGroup}, groupName)
}

func IsUserSelectableGroupForGroups(userGroups []string, groupName string) bool {
	if groupName == "" || groupName == "auto" {
		return false
	}
	return GroupInUserUsableGroupsForGroups(userGroups, groupName) && ratio_setting.ContainsGroupRatio(groupName)
}

// FilterUserTokenAutoGroups applies current permissions to a token's ordered
// candidate list. The user-selected order is retained without a global limit.
func FilterUserTokenAutoGroups(userGroup string, groups []string) []string {
	return FilterUserTokenAutoGroupsForGroups([]string{userGroup}, groups)
}

func FilterUserTokenAutoGroupsForGroups(userGroups []string, groups []string) []string {
	filtered := make([]string, 0, len(groups))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsUserSelectableGroupForGroups(userGroups, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups saved on the current
// token. Missing token configuration fails closed.
func GetRequestAutoGroups(c *gin.Context, userGroup string) []string {
	return GetRequestAutoGroupsForGroups(c, []string{userGroup})
}

func GetRequestAutoGroupsForGroups(c *gin.Context, userGroups []string) []string {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return []string{}
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterUserTokenAutoGroupsForGroups(userGroups, groups)
}

// GetGroupsEnabledModels gets enabled models in group order and removes duplicates.
func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; ok {
				continue
			}
			seen[modelName] = struct{}{}
			models = append(models, modelName)
		}
	}
	return models
}

// GetGroupsEnabledModelsForClient keeps only models that at least one enabled
// channel in the requested groups allows for client. It mirrors channel
// distribution so a model list does not advertise entries every serving channel
// rejects for the current client.
func GetGroupsEnabledModelsForClient(groups []string, client string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, ability := range model.GetGroupEnabledAbilities(group) {
			if _, ok := seen[ability.Model]; ok {
				continue
			}
			channel, err := model.CacheGetChannel(ability.ChannelId)
			if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled ||
				!clientpolicy.IsChannelAllowed(channel, client) {
				continue
			}
			seen[ability.Model] = struct{}{}
			models = append(models, ability.Model)
		}
	}
	return models
}

// GetUserGroupRatio returns the ratio for a user group using a token group.
func GetUserGroupRatio(userGroup, group string) float64 {
	if ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group); ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
