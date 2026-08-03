package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func getAssignedUserGroups(userGroups []string) map[string]string {
	descriptions := setting.GetUserUsableGroupsCopy()
	configuredGroups := ratio_setting.GetGroupRatioCopy()
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
		groups[group] = description
	}
	return groups
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
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range setting.GetAutoGroups() {
		if _, ok := usableGroups[group]; !ok {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
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

// FilterUserTokenAutoGroups applies current permissions before the current
// per-token limit. It intentionally does not fall back to the global Auto list.
func FilterUserTokenAutoGroups(userGroup string, groups []string) []string {
	return FilterUserTokenAutoGroupsForGroups([]string{userGroup}, groups)
}

func FilterUserTokenAutoGroupsForGroups(userGroups []string, groups []string) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
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
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups for the current token.
// The absence of the context value means that the token inherits the complete
// global Auto list; a present (even empty) value is an explicit token snapshot.
func GetRequestAutoGroups(c *gin.Context, userGroup string) []string {
	return GetRequestAutoGroupsForGroups(c, []string{userGroup})
}

func GetRequestAutoGroupsForGroups(c *gin.Context, userGroups []string) []string {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetUserAutoGroups(userGroups)
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

// GetUserGroupRatio returns the ratio for a user group using a token group.
func GetUserGroupRatio(userGroup, group string) float64 {
	if ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group); ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
