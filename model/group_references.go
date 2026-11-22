package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/account_pool_setting"
	"github.com/QuantumNous/new-api/setting/group_rate_limit_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func renameGroupReferencesWithTx(tx *gorm.DB, oldName, newName string, affectedUsers *[]int) error {
	if err := rewriteGroupOptionsWithTx(tx, oldName, newName, false); err != nil {
		return err
	}
	if err := mergeUserGroupMembershipsWithTx(tx, oldName, newName); err != nil {
		return err
	}
	if err := tx.Model(&User{}).Where(commonGroupCol+" = ?", oldName).Update("group", newName).Error; err != nil {
		return err
	}
	if err := tx.Model(&User{}).Where("topup_group = ?", oldName).Update("topup_group", newName).Error; err != nil {
		return err
	}
	var tokenUsers []int
	if err := tx.Model(&Token{}).Where(commonGroupCol+" = ?", oldName).Distinct("user_id").Pluck("user_id", &tokenUsers).Error; err != nil {
		return err
	}
	*affectedUsers = append(*affectedUsers, tokenUsers...)
	if err := tx.Model(&Token{}).Where(commonGroupCol+" = ?", oldName).Update("group", newName).Error; err != nil {
		return err
	}
	if err := renameTokenAutoGroupsWithTx(tx, oldName, newName, false, affectedUsers); err != nil {
		return err
	}
	if err := renameChannelGroupsWithTx(tx, oldName, newName, false); err != nil {
		return err
	}
	if err := renameAbilityGroupsWithTx(tx, oldName, newName); err != nil {
		return err
	}
	for _, update := range []struct {
		model any
		field string
	}{
		{&UserModelRoute{}, "execution_group"},
		{&SubscriptionPlan{}, "upgrade_group"},
		{&SubscriptionPlan{}, "downgrade_group"},
		{&UserSubscription{}, "upgrade_group"},
		{&UserSubscription{}, "prev_user_group"},
		{&UserSubscription{}, "downgrade_group"},
	} {
		if err := tx.Model(update.model).Where(update.field+" = ?", oldName).Update(update.field, newName).Error; err != nil {
			return err
		}
	}
	if err := renameRouteGroupRowsWithTx(tx, &UserModelRouteGroup{}, oldName, newName); err != nil {
		return err
	}
	if err := renameRouteGroupRowsWithTx(tx, &UserModelRouteExecutionGroup{}, oldName, newName); err != nil {
		return err
	}
	if err := tx.Model(&Task{}).Where(commonGroupCol+" = ?", oldName).Update("group", newName).Error; err != nil {
		return err
	}
	var tasks []Task
	if err := tx.Find(&tasks).Error; err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].PrivateData.RouteExecutionGroup != oldName {
			continue
		}
		tasks[i].PrivateData.RouteExecutionGroup = newName
		if err := tx.Model(&Task{}).Where("id = ?", tasks[i].ID).Update("private_data", tasks[i].PrivateData).Error; err != nil {
			return err
		}
	}
	return nil
}

func deleteUserGroupMembershipsWithTx(tx *gorm.DB, name string, affectedUsers []int) error {
	var membershipUsers []int
	if err := tx.Model(&UserGroupMembership{}).Where("group_name = ?", name).Distinct("user_id").Pluck("user_id", &membershipUsers).Error; err != nil {
		return err
	}
	var legacyUsers []int
	if err := tx.Model(&User{}).Where(commonGroupCol+" = ? OR topup_group = ?", name, name).Distinct("id").Pluck("id", &legacyUsers).Error; err != nil {
		return err
	}
	affectedUsers = uniqueInts(append(affectedUsers, legacyUsers...))
	affectedUsers = uniqueInts(append(affectedUsers, membershipUsers...))
	if err := tx.Where("group_name = ?", name).Delete(&UserGroupMembership{}).Error; err != nil {
		return err
	}
	for _, userId := range affectedUsers {
		var memberships []UserGroupMembership
		if err := tx.Where("user_id = ?", userId).Order("sort_order asc, id asc").Find(&memberships).Error; err != nil {
			return err
		}
		primary := ""
		if len(memberships) > 0 {
			primary = memberships[0].GroupName
		}
		var user User
		if err := tx.Select("id, "+commonGroupCol+", topup_group").Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		if primary == "" && user.Group != name {
			primary = strings.TrimSpace(user.Group)
		}
		if primary == "" && ratio_setting.ContainsGroupRatio("default") {
			primary = "default"
			manual := true
			if err := tx.Create(&UserGroupMembership{
				UserId:    userId,
				GroupName: primary,
				SortOrder: 0,
				Manual:    &manual,
			}).Error; err != nil {
				return err
			}
		}
		topup := strings.TrimSpace(user.TopupGroup)
		if topup == name || !groupListContains(groupNamesFromMemberships(memberships), topup) {
			topup = primary
		}
		if err := tx.Model(&User{}).Where("id = ?", userId).Updates(map[string]any{
			"group":       primary,
			"topup_group": topup,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func renameRouteGroupRowsWithTx(tx *gorm.DB, target any, oldName, newName string) error {
	selectColumns := "id, route_id, group_name"
	isExecutionGroup := false
	if _, ok := target.(*UserModelRouteExecutionGroup); ok {
		selectColumns += ", position"
		isExecutionGroup = true
	}
	var rows []struct {
		Id        int
		RouteId   int
		GroupName string
		Position  int
	}
	if err := tx.Model(target).Select(selectColumns).Where("group_name = ?", oldName).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var existing struct {
			Id        int
			RouteId   int
			GroupName string
			Position  int
		}
		query := tx.Model(target).Select(selectColumns).Where("route_id = ? AND group_name = ?", row.RouteId, newName)
		if isExecutionGroup {
			query = query.Order("position asc")
		}
		err := query.First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(target).Where("id = ?", row.Id).Update("group_name", newName).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if isExecutionGroup && row.Position < existing.Position {
			if err := tx.Model(target).Where("id = ?", existing.Id).Update("position", row.Position).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(target, "id = ?", row.Id).Error; err != nil {
			return err
		}
	}
	return nil
}

func deleteGroupReferencesWithTx(tx *gorm.DB, name string, affectedUsers *[]int) error {
	if err := rewriteGroupOptionsWithTx(tx, name, "", true); err != nil {
		return err
	}
	var tokenUsers []int
	if err := tx.Model(&Token{}).Where(commonGroupCol+" = ?", name).Distinct("user_id").Pluck("user_id", &tokenUsers).Error; err != nil {
		return err
	}
	*affectedUsers = append(*affectedUsers, tokenUsers...)
	if err := deleteUserGroupMembershipsWithTx(tx, name, tokenUsers); err != nil {
		return err
	}
	var tokens []Token
	if err := tx.Where(commonGroupCol+" = ?", name).Find(&tokens).Error; err != nil {
		return err
	}
	for _, token := range tokens {
		var user User
		if err := tx.Select("id", commonGroupCol).Where("id = ?", token.UserId).First(&user).Error; err != nil {
			return err
		}
		if err := tx.Model(&Token{}).Where("id = ?", token.Id).Update("group", user.Group).Error; err != nil {
			return err
		}
	}
	if err := deleteChannelGroupsWithTx(tx, name); err != nil {
		return err
	}
	if err := tx.Where(commonGroupCol+" = ?", name).Delete(&Ability{}).Error; err != nil {
		return err
	}
	if err := deleteRouteGroupsWithTx(tx, name); err != nil {
		return err
	}
	if err := deleteTokenAutoGroupsWithTx(tx, name, affectedUsers); err != nil {
		return err
	}
	return nil
}

func groupNamesFromMemberships(memberships []UserGroupMembership) []string {
	groups := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		groups = append(groups, membership.GroupName)
	}
	return groups
}

func uniqueInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	unique := make([]int, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func mergeUserGroupMembershipsWithTx(tx *gorm.DB, oldName, newName string) error {
	var rows []UserGroupMembership
	if err := tx.Where("group_name = ?", oldName).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var existing UserGroupMembership
		err := tx.Where("user_id = ? AND group_name = ?", row.UserId, newName).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(&UserGroupMembership{}).Where("id = ?", row.Id).Update("group_name", newName).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		updates := map[string]any{"sort_order": min(existing.SortOrder, row.SortOrder)}
		if row.Manual != nil && *row.Manual {
			updates["manual"] = true
		}
		if err := tx.Model(&UserGroupMembership{}).Where("id = ?", existing.Id).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Delete(&UserGroupMembership{}, "id = ?", row.Id).Error; err != nil {
			return err
		}
	}
	return nil
}

func renameChannelGroupsWithTx(tx *gorm.DB, oldName, newName string, deleting bool) error {
	var channels []Channel
	if err := tx.Find(&channels).Error; err != nil {
		return err
	}
	for i := range channels {
		groups := channels[i].GetGroups()
		changed := false
		next := make([]string, 0, len(groups))
		seen := make(map[string]struct{}, len(groups))
		for _, group := range groups {
			if group == oldName {
				changed = true
				if deleting {
					continue
				}
				group = newName
			}
			if _, exists := seen[group]; exists {
				continue
			}
			seen[group] = struct{}{}
			next = append(next, group)
		}
		if !changed {
			continue
		}
		channels[i].Group = strings.Join(next, ",")
		updates := map[string]any{"group": channels[i].Group}
		if deleting && len(next) == 0 {
			updates["status"] = common.ChannelStatusManuallyDisabled
		}
		if err := tx.Model(&Channel{}).Where("id = ?", channels[i].Id).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func renameAbilityGroupsWithTx(tx *gorm.DB, oldName, newName string) error {
	var abilities []Ability
	if err := tx.Where(commonGroupCol+" = ?", oldName).Find(&abilities).Error; err != nil {
		return err
	}
	for _, ability := range abilities {
		var duplicate Ability
		err := tx.Where(commonGroupCol+" = ? AND model = ? AND channel_id = ?", newName, ability.Model, ability.ChannelId).First(&duplicate).Error
		if err == nil {
			if err := tx.Delete(&Ability{}, commonGroupCol+" = ? AND model = ? AND channel_id = ?", oldName, ability.Model, ability.ChannelId).Error; err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Model(&Ability{}).Where(commonGroupCol+" = ? AND model = ? AND channel_id = ?", oldName, ability.Model, ability.ChannelId).Update("group", newName).Error; err != nil {
			return err
		}
	}
	return nil
}

func renameTokenAutoGroupsWithTx(tx *gorm.DB, oldName, newName string, deleting bool, affectedUsers *[]int) error {
	var tokens []Token
	if err := tx.Where("auto_groups <> ''").Find(&tokens).Error; err != nil {
		return err
	}
	for i := range tokens {
		groups, err := tokens[i].GetAutoGroups()
		if err != nil {
			return fmt.Errorf("read token %d Auto groups: %w", tokens[i].Id, err)
		}
		changed := false
		next := make([]string, 0, len(groups))
		for _, group := range groups {
			if group != oldName {
				next = append(next, group)
				continue
			}
			changed = true
			if !deleting {
				next = append(next, newName)
			}
		}
		if !changed {
			continue
		}
		*affectedUsers = append(*affectedUsers, tokens[i].UserId)
		if deleting && tokens[i].Group == "auto" && len(next) == 0 {
			var user User
			if err := tx.Select("id", commonGroupCol).Where("id = ?", tokens[i].UserId).First(&user).Error; err != nil {
				return err
			}
			if user.Group != "" && user.Group != "auto" && ratio_setting.ContainsGroupRatio(user.Group) {
				next = append(next, user.Group)
			} else if ratio_setting.ContainsGroupRatio("default") {
				next = append(next, "default")
			}
		}
		data, err := common.Marshal(next)
		if err != nil {
			return err
		}
		if err := tx.Model(&Token{}).Where("id = ?", tokens[i].Id).Update("auto_groups", string(data)).Error; err != nil {
			return err
		}
	}
	return nil
}

func deleteTokenAutoGroupsWithTx(tx *gorm.DB, name string, affectedUsers *[]int) error {
	return renameTokenAutoGroupsWithTx(tx, name, "", true, affectedUsers)
}

func deleteChannelGroupsWithTx(tx *gorm.DB, name string) error {
	return renameChannelGroupsWithTx(tx, name, "", true)
}

func deleteRouteGroupsWithTx(tx *gorm.DB, name string) error {
	var routes []UserModelRoute
	if err := tx.Find(&routes).Error; err != nil {
		return err
	}
	for i := range routes {
		route := &routes[i]
		var applicable []UserModelRouteGroup
		if err := tx.Where("route_id = ? AND group_name = ?", route.Id, name).Find(&applicable).Error; err != nil {
			return err
		}
		var execution []UserModelRouteExecutionGroup
		if err := tx.Where("route_id = ? AND group_name = ?", route.Id, name).Find(&execution).Error; err != nil {
			return err
		}
		if len(applicable) > 0 {
			if err := tx.Delete(&UserModelRouteGroup{}, "route_id = ? AND group_name = ?", route.Id, name).Error; err != nil {
				return err
			}
			if !route.AllGroups {
				var remaining int64
				if err := tx.Model(&UserModelRouteGroup{}).Where("route_id = ?", route.Id).Count(&remaining).Error; err != nil {
					return err
				}
				if remaining == 0 {
					route.Enabled = false
				}
			}
		}
		legacyExecutionChanged := route.ExecutionGroup == name
		if len(execution) > 0 {
			if err := tx.Delete(&UserModelRouteExecutionGroup{}, "route_id = ? AND group_name = ?", route.Id, name).Error; err != nil {
				return err
			}
		}
		if len(execution) > 0 || legacyExecutionChanged {
			var remaining []UserModelRouteExecutionGroup
			if err := tx.Where("route_id = ?", route.Id).Order("position asc, id asc").Find(&remaining).Error; err != nil {
				return err
			}
			if len(remaining) == 0 {
				route.ExecutionGroup = ""
				route.Enabled = false
			} else {
				route.ExecutionGroup = remaining[0].GroupName
			}
		}
		if len(applicable) > 0 || len(execution) > 0 || legacyExecutionChanged {
			if err := tx.Model(&UserModelRoute{}).Where("id = ?", route.Id).Updates(map[string]any{
				"enabled":         route.Enabled,
				"execution_group": route.ExecutionGroup,
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func rewriteGroupOptionsWithTx(tx *gorm.DB, oldName, newName string, deleting bool) error {
	for _, key := range []string{
		"GroupRatio",
		"UserUsableGroups",
		"TopupGroupRatio",
		"GroupGroupRatio",
		"AutoGroups",
		"ModelRequestRateLimitGroup",
		"prompt_audit.groups",
		"Chats",
		"group_ratio_setting.group_special_usable_group",
		group_rate_limit_setting.PoliciesOptionKey,
		operation_setting.ClientPolicyGroupsOptionKey,
		account_pool_setting.ProviderGroupsOptionKey,
	} {
		var option Option
		err := tx.Where(commonKeyCol+" = ?", key).First(&option).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		value, changed, err := rewriteGroupOption(key, option.Value, oldName, newName, deleting)
		if err != nil {
			return fmt.Errorf("rewrite %s for group %s: %w", key, oldName, err)
		}
		if changed {
			if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", key).Update("value", value).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func rewriteGroupOption(key, value, oldName, newName string, deleting bool) (string, bool, error) {
	switch key {
	case "GroupRatio", "TopupGroupRatio":
		var values map[string]float64
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		item, exists := values[oldName]
		if !exists {
			return value, false, nil
		}
		delete(values, oldName)
		if !deleting {
			values[newName] = item
		}
		encoded, err := common.Marshal(values)
		return string(encoded), true, err
	case "UserUsableGroups", "ModelRequestRateLimitGroup":
		var values map[string]any
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		item, exists := values[oldName]
		if !exists {
			return value, false, nil
		}
		delete(values, oldName)
		if !deleting {
			values[newName] = item
		}
		encoded, err := common.Marshal(values)
		return string(encoded), true, err
	case "GroupGroupRatio":
		var values map[string]map[string]float64
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		changed := false
		if row, exists := values[oldName]; exists {
			delete(values, oldName)
			if !deleting {
				values[newName] = row
			}
			changed = true
		}
		for source, row := range values {
			if ratio, exists := row[oldName]; exists {
				delete(row, oldName)
				if !deleting {
					row[newName] = ratio
				}
				values[source] = row
				changed = true
			}
		}
		if !changed {
			return value, false, nil
		}
		encoded, err := common.Marshal(values)
		return string(encoded), true, err
	case "AutoGroups":
		var values []string
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		return rewriteGroupSlice(value, values, oldName, newName, deleting)
	case "prompt_audit.groups":
		var values []string
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		return rewriteGroupSlice(value, values, oldName, newName, deleting)
	case "Chats":
		var values any
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		rewritten, changed, err := rewriteNestedGroupArrays(values, oldName, newName, deleting)
		if err != nil || !changed {
			return value, changed, err
		}
		encoded, err := common.Marshal(rewritten)
		return string(encoded), true, err
	case "group_ratio_setting.group_special_usable_group":
		var values map[string]map[string]string
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		changed := false
		if row, exists := values[oldName]; exists {
			delete(values, oldName)
			if !deleting {
				values[newName] = row
			}
			changed = true
		}
		for source, row := range values {
			for rawName, description := range row {
				visible, target := parseSpecialUsableGroup(rawName)
				if target != oldName {
					continue
				}
				delete(row, rawName)
				if !deleting {
					prefix := ""
					if visible {
						prefix = "+:"
					} else {
						prefix = "-:"
					}
					row[prefix+newName] = description
				}
				changed = true
			}
			values[source] = row
		}
		if !changed {
			return value, false, nil
		}
		encoded, err := common.Marshal(values)
		return string(encoded), true, err
	case group_rate_limit_setting.PoliciesOptionKey, operation_setting.ClientPolicyGroupsOptionKey:
		var values map[string]any
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		item, exists := values[oldName]
		if !exists {
			return value, false, nil
		}
		delete(values, oldName)
		if !deleting {
			values[newName] = item
		}
		encoded, err := common.Marshal(values)
		return string(encoded), true, err
	case account_pool_setting.ProviderGroupsOptionKey:
		var values map[string][]string
		if err := common.UnmarshalJsonStr(value, &values); err != nil {
			return "", false, err
		}
		changed := false
		for provider, groups := range values {
			next := make([]string, 0, len(groups))
			for _, group := range groups {
				if group != oldName {
					next = append(next, group)
					continue
				}
				changed = true
				if !deleting {
					next = append(next, newName)
				}
			}
			values[provider] = next
		}
		if !changed {
			return value, false, nil
		}
		encoded, err := common.Marshal(values)
		return string(encoded), true, err
	default:
		return value, false, nil
	}
}

func rewriteNestedGroupArrays(value any, oldName, newName string, deleting bool) (any, bool, error) {
	changed := false
	switch current := value.(type) {
	case []any:
		for i := range current {
			next, itemChanged, err := rewriteNestedGroupArrays(current[i], oldName, newName, deleting)
			if err != nil {
				return nil, false, err
			}
			current[i] = next
			changed = changed || itemChanged
		}
		return current, changed, nil
	case map[string]any:
		for key, item := range current {
			if key == "groups" {
				groups, ok := item.([]any)
				if !ok {
					return nil, false, fmt.Errorf("groups field must be an array")
				}
				next := make([]any, 0, len(groups))
				for _, rawGroup := range groups {
					group, ok := rawGroup.(string)
					if !ok {
						return nil, false, fmt.Errorf("groups array contains a non-string value")
					}
					if group != oldName {
						next = append(next, rawGroup)
						continue
					}
					changed = true
					if !deleting {
						next = append(next, newName)
					}
				}
				current[key] = next
				continue
			}
			next, itemChanged, err := rewriteNestedGroupArrays(item, oldName, newName, deleting)
			if err != nil {
				return nil, false, err
			}
			current[key] = next
			changed = changed || itemChanged
		}
		return current, changed, nil
	default:
		return value, false, nil
	}
}

func rewriteGroupSlice(original string, values []string, oldName, newName string, deleting bool) (string, bool, error) {
	changed := false
	groups := make([]string, 0, len(values))
	for _, group := range values {
		if group != oldName {
			groups = append(groups, group)
			continue
		}
		changed = true
		if !deleting {
			groups = append(groups, newName)
		}
	}
	if !changed {
		return original, false, nil
	}
	encoded, err := common.Marshal(groups)
	return string(encoded), true, err
}

func parseSpecialUsableGroup(rawName string) (bool, string) {
	if strings.HasPrefix(rawName, "+:") {
		return true, strings.TrimPrefix(rawName, "+:")
	}
	if strings.HasPrefix(rawName, "-:") {
		return false, strings.TrimPrefix(rawName, "-:")
	}
	return true, rawName
}

func finishGroupReferenceUpdates(users []int, policyVersions map[int]int64) {
	for _, userId := range users {
		if version := policyVersions[userId]; version > 0 {
			if err := publishCommittedUserPolicyVersion(userId, version); err != nil {
				common.SysLog(common.LogText("publish group policy update for user %d: %s", userId, err.Error()))
			}
		}
		if err := invalidateUserCache(userId); err != nil {
			common.SysLog(common.LogText("invalidate user cache after group update for user %d: %s", userId, err.Error()))
		}
		if err := InvalidateUserTokensCache(userId); err != nil {
			common.SysLog(common.LogText("invalidate token cache after group update for user %d: %s", userId, err.Error()))
		}
	}
}

func saveGroupOptionWithTx(tx *gorm.DB, key, value string) error {
	option := Option{Key: key, Value: value}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&option).Error
}

func persistGroupSettingsWithTx(tx *gorm.DB, oldName, newName string, deleting bool, updated *Group) error {
	ratios := ratio_setting.GetGroupRatioCopy()
	descriptions := setting.GetUserUsableGroupsCopy()
	if oldName != "" {
		ratio, exists := ratios[oldName]
		delete(ratios, oldName)
		description := descriptions[oldName]
		delete(descriptions, oldName)
		if !deleting && newName != "" {
			ratios[newName] = ratio
			if exists && description != "" {
				descriptions[newName] = description
			}
		}
	}
	if updated != nil {
		ratios[updated.Name] = updated.Ratio
		descriptions[updated.Name] = groupDescription(updated.Description, updated.Name)
	}
	ratioJSON, err := common.Marshal(ratios)
	if err != nil {
		return err
	}
	descriptionJSON, err := common.Marshal(descriptions)
	if err != nil {
		return err
	}
	if err := saveGroupOptionWithTx(tx, "GroupRatio", string(ratioJSON)); err != nil {
		return err
	}
	return saveGroupOptionWithTx(tx, "UserUsableGroups", string(descriptionJSON))
}

func ensureGroupNameNotHistorical(tx *gorm.DB, name string) error {
	var history GroupNameHistory
	err := tx.Where("old_name = ?", name).First(&history).Error
	if err == nil {
		return fmt.Errorf("%w: name is reserved for historical records", ErrGroupNameInvalid)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}
