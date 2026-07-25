package service

import (
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func applySpecialUsableGroups(groups map[string]string, userGroup string) {
	if strings.TrimSpace(userGroup) == "" {
		return
	}
	specialSettings, ok := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
	if ok {
		for specialGroup, description := range specialSettings {
			switch {
			case strings.HasPrefix(specialGroup, "-:"):
				delete(groups, strings.TrimPrefix(specialGroup, "-:"))
			case strings.HasPrefix(specialGroup, "+:"):
				groups[strings.TrimPrefix(specialGroup, "+:")] = description
			default:
				groups[specialGroup] = description
			}
		}
	}
}

// GetUserUsableGroups retains the legacy single-user-group contract.
func GetUserUsableGroups(userGroup string) map[string]string {
	groups := setting.GetUserUsableGroupsCopy()
	applySpecialUsableGroups(groups, userGroup)
	return groups
}

// GetUserUsableGroupsForGroups combines each membership's independently
// usable token groups. A restriction on one membership must not revoke access
// granted by another membership. This preserves the existing special usable
// group rules while allowing multiple base memberships.
func GetUserUsableGroupsForGroups(userGroups []string) map[string]string {
	groups := make(map[string]string)
	for _, userGroup := range userGroups {
		userGroup = strings.TrimSpace(userGroup)
		if userGroup == "" {
			continue
		}
		for group, description := range GetUserUsableGroups(userGroup) {
			groups[group] = description
		}
	}
	return groups
}

func GetAuthorizedUserGroups(userGroups []string) map[string]string {
	return GetUserUsableGroupsForGroups(userGroups)
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

func GroupInUserUsableGroupsForGroups(userGroups []string, groupName string) bool {
	_, ok := GetUserUsableGroupsForGroups(userGroups)[groupName]
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
	return getAutoGroups(GetUserUsableGroups(userGroup))
}

func GetUserAutoGroups(userGroups []string) []string {
	return getAutoGroups(GetUserUsableGroupsForGroups(userGroups))
}

func getAutoGroups(usableGroups map[string]string) []string {
	autoGroups := make([]string, 0)
	for _, group := range setting.GetAutoGroups() {
		if _, ok := usableGroups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
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
