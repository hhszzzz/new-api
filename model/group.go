package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

var (
	ErrGroupNotFound                = errors.New("group not found")
	ErrGroupNameExists              = errors.New("group name already exists")
	ErrGroupHasSubscriptions        = errors.New("group is referenced by a subscription plan, active subscription, or unused redemption code")
	ErrGroupRemovalRequiresRegistry = errors.New("groups can only be removed through the group registry")
	ErrDefaultGroupProtected        = errors.New("default group cannot be removed or renamed")
	ErrGroupNameInvalid             = errors.New("group name is invalid")
)

// Group is the durable registry for user-facing groups. Legacy pricing options
// remain an import/export format while callers migrate to this table.
type Group struct {
	Name        string  `json:"name" gorm:"type:varchar(64);primaryKey"`
	Description string  `json:"description" gorm:"type:varchar(255);not null"`
	Ratio       float64 `json:"ratio" gorm:"not null"`
	SortOrder   int     `json:"sort_order" gorm:"not null"`
	CreatedAt   int64   `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt   int64   `json:"updated_at" gorm:"bigint;not null"`
}

// GroupNameHistory reserves former names for historical display and filters.
// It is never consulted by authorization or routing.
type GroupNameHistory struct {
	OldName   string `json:"old_name" gorm:"type:varchar(64);primaryKey"`
	NewName   string `json:"new_name" gorm:"type:varchar(64);not null"`
	ChangedAt int64  `json:"changed_at" gorm:"bigint;not null"`
}

func validateGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "auto" || len(name) > 64 || strings.ContainsAny(name, ",\r\n") {
		return "", ErrGroupNameInvalid
	}
	return name, nil
}

func ListGroups() ([]Group, error) {
	if DB == nil {
		return nil, errors.New("database is not initialized")
	}
	var groups []Group
	err := DB.Order("sort_order asc, name asc").Find(&groups).Error
	return groups, err
}

func GetGroupNames() ([]string, error) {
	groups, err := ListGroups()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		names = append(names, group.Name)
	}
	return names, nil
}

// SyncGroupRegistryFromSettings imports the pre-registry ratio and description
// maps. It is safe to run on every startup and does not remove registered rows.
func SyncGroupRegistryFromSettings() error {
	if DB == nil {
		return nil
	}
	ratios := ratio_setting.GetGroupRatioCopy()
	descriptions := setting.GetUserUsableGroupsCopy()
	names := make([]string, 0, len(ratios))
	for name := range ratios {
		names = append(names, name)
	}
	slices.Sort(names)
	return DB.Transaction(func(tx *gorm.DB) error {
		for position, name := range names {
			if _, err := validateGroupName(name); err != nil {
				return fmt.Errorf("import group %q: %w", name, err)
			}
			description := strings.TrimSpace(descriptions[name])
			if description == "" {
				description = name
			}
			group := Group{Name: name}
			if err := tx.Where("name = ?", name).First(&group).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				now := time.Now().Unix()
				group = Group{Name: name, Description: description, Ratio: ratios[name], SortOrder: position, CreatedAt: now, UpdatedAt: now}
				if err := tx.Create(&group).Error; err != nil {
					return err
				}
				continue
			}
			if group.Ratio != ratios[name] {
				if err := tx.Model(&Group{}).Where("name = ?", name).Updates(map[string]any{
					"ratio":      ratios[name],
					"updated_at": time.Now().Unix(),
				}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// UpsertGroup writes one registry entry and keeps the legacy option snapshot in sync.
func UpsertGroup(group Group) error {
	name, err := validateGroupName(group.Name)
	if err != nil {
		return err
	}
	if group.Ratio < 0 {
		return errors.New("group ratio must be non-negative")
	}
	description := strings.TrimSpace(group.Description)
	if len(description) > 255 {
		return errors.New("group description is too long")
	}
	if description == "" {
		description = name
	}
	now := time.Now().Unix()
	group.Name = name
	group.Description = description
	group.UpdatedAt = now
	if group.CreatedAt == 0 {
		group.CreatedAt = now
	}
	if err := withGroupRegistryTransaction(func(tx *gorm.DB) error {
		var existing Group
		err := tx.Where("name = ?", name).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := ensureGroupNameNotHistorical(tx, name); err != nil {
				return err
			}
			if err := persistGroupSettingsWithTx(tx, "", group.Name, false, &group); err != nil {
				return err
			}
			return tx.Create(&group).Error
		}
		if err != nil {
			return err
		}
		if existing.Ratio != group.Ratio {
			if err := SnapshotUnfinishedTaskGroupRatiosWithTx(tx, name, existing.Ratio); err != nil {
				return err
			}
		}
		if err := persistGroupSettingsWithTx(tx, "", group.Name, false, &group); err != nil {
			return err
		}
		return tx.Model(&Group{}).Where("name = ?", name).Updates(map[string]any{
			"description": description,
			"ratio":       group.Ratio,
			"sort_order":  group.SortOrder,
			"updated_at":  now,
		}).Error
	}); err != nil {
		return err
	}
	loadOptionsFromDatabase()
	return nil
}

func SnapshotUnfinishedTaskGroupRatiosWithTx(tx *gorm.DB, groupName string, ratio float64) error {
	if tx == nil || !tx.Migrator().HasTable(&Task{}) {
		return nil
	}
	var tasks []Task
	if err := tx.Where(commonGroupCol+" = ? AND status NOT IN ?", groupName, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Find(&tasks).Error; err != nil {
		return err
	}
	for i := range tasks {
		task := &tasks[i]
		billing := task.PrivateData.BillingContext
		if billing != nil && (billing.GroupRatioSnapshot || billing.GroupRatio > 0) {
			continue
		}
		primaryGroup := ""
		var user User
		if err := tx.Select("id, "+commonGroupCol).Where("id = ?", task.UserId).First(&user).Error; err == nil {
			primaryGroup = user.Group
		}
		groupRatio := ratio
		if specialRatio, ok := ratio_setting.GetGroupGroupRatio(primaryGroup, groupName); ok {
			groupRatio = specialRatio
		}
		if billing == nil {
			billing = &TaskBillingContext{}
			task.PrivateData.BillingContext = billing
		}
		billing.GroupRatio = groupRatio
		billing.GroupRatioSnapshot = true
		if err := tx.Model(&Task{}).Where("id = ?", task.ID).Update("private_data", task.PrivateData).Error; err != nil {
			return err
		}
	}
	return nil
}

func SnapshotChangedTaskGroupRatiosWithTx(tx *gorm.DB, values map[string]string) error {
	changedGroups := make(map[string]struct{})
	if raw, ok := values["GroupRatio"]; ok {
		var updated map[string]float64
		if err := common.UnmarshalJsonStr(raw, &updated); err != nil {
			return err
		}
		for name, currentRatio := range ratio_setting.GetGroupRatioCopy() {
			updatedRatio, exists := updated[name]
			if !exists || updatedRatio != currentRatio {
				changedGroups[name] = struct{}{}
			}
		}
	}
	if raw, ok := values["GroupGroupRatio"]; ok {
		var updated map[string]map[string]float64
		if err := common.UnmarshalJsonStr(raw, &updated); err != nil {
			return err
		}
		current := ratio_setting.GetGroupRatioSetting().GroupGroupRatio.ReadAll()
		for primary, groups := range current {
			for group, currentRatio := range groups {
				updatedRatio, exists := updated[primary][group]
				if !exists || updatedRatio != currentRatio {
					changedGroups[group] = struct{}{}
				}
			}
		}
		for primary, groups := range updated {
			for group, updatedRatio := range groups {
				currentRatio, exists := current[primary][group]
				if !exists || updatedRatio != currentRatio {
					changedGroups[group] = struct{}{}
				}
			}
		}
	}
	for group := range changedGroups {
		if err := SnapshotUnfinishedTaskGroupRatiosWithTx(tx, group, ratio_setting.GetGroupRatio(group)); err != nil {
			return err
		}
	}
	return nil
}

func withGroupRegistryTransaction(update func(tx *gorm.DB) error) error {
	optionUpdateMu.Lock()
	defer optionUpdateMu.Unlock()
	return DB.Transaction(update)
}

// RenameGroup applies a registry rename and the live database references in a
// single primary-database transaction. Historical logs and audit archives keep
// their original group strings.
func RenameGroup(oldName string, updated Group) error {
	oldName = strings.TrimSpace(oldName)
	newName, err := validateGroupName(updated.Name)
	if err != nil {
		return err
	}
	if oldName == "" || oldName == "auto" {
		return ErrGroupNameInvalid
	}
	if newName == oldName {
		return UpsertGroup(updated)
	}
	if oldName == "default" {
		return ErrDefaultGroupProtected
	}
	if updated.Ratio < 0 {
		return errors.New("group ratio must be non-negative")
	}
	updated.Description = strings.TrimSpace(updated.Description)
	if len(updated.Description) > 255 {
		return errors.New("group description is too long")
	}
	users := make([]int, 0)
	policyVersions := make(map[int]int64)
	if err := withGroupRegistryTransaction(func(tx *gorm.DB) error {
		var group Group
		if err := lockForUpdate(tx).Where("name = ?", oldName).First(&group).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrGroupNotFound
			}
			return err
		}
		var collision Group
		if err := tx.Where("name = ?", newName).First(&collision).Error; err == nil {
			return ErrGroupNameExists
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := ensureGroupNameNotHistorical(tx, newName); err != nil {
			return err
		}
		if err := SnapshotUnfinishedTaskGroupRatiosWithTx(tx, oldName, group.Ratio); err != nil {
			return err
		}
		if err := tx.Model(&UserGroupMembership{}).Where("group_name = ?", oldName).Distinct("user_id").Pluck("user_id", &users).Error; err != nil {
			return err
		}
		var legacyUsers []int
		if err := tx.Model(&User{}).Where(commonGroupCol+" = ? OR topup_group = ?", oldName, oldName).Distinct("id").Pluck("id", &legacyUsers).Error; err != nil {
			return err
		}
		users = uniqueInts(append(users, legacyUsers...))
		if err := renameGroupReferencesWithTx(tx, oldName, newName, &users); err != nil {
			return err
		}
		users = uniqueInts(users)
		if err := persistGroupSettingsWithTx(tx, oldName, newName, false, &updated); err != nil {
			return err
		}
		for _, userId := range users {
			version, err := IncrementUserPolicyVersionWithTx(tx, userId)
			if err != nil {
				return err
			}
			policyVersions[userId] = version
		}
		if err := tx.Create(&GroupNameHistory{OldName: oldName, NewName: newName, ChangedAt: time.Now().Unix()}).Error; err != nil {
			return err
		}
		return tx.Model(&Group{}).Where("name = ?", oldName).Updates(map[string]any{
			"name":        newName,
			"description": groupDescription(updated.Description, newName),
			"ratio":       updated.Ratio,
			"sort_order":  updated.SortOrder,
			"updated_at":  time.Now().Unix(),
		}).Error
	}); err != nil {
		return err
	}
	finishGroupReferenceUpdates(users, policyVersions)
	loadOptionsFromDatabase()
	InitChannelCache()
	return nil
}

func groupDescription(description, name string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return name
	}
	return description
}

func DeleteGroup(name string) error {
	name = strings.TrimSpace(name)
	if _, err := validateGroupName(name); err != nil {
		return err
	}
	if name == "default" {
		return ErrDefaultGroupProtected
	}
	users := make([]int, 0)
	policyVersions := make(map[int]int64)
	if err := withGroupRegistryTransaction(func(tx *gorm.DB) error {
		var group Group
		if err := lockForUpdate(tx).Where("name = ?", name).First(&group).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrGroupNotFound
			}
			return err
		}
		if err := ensureGroupHasNoPlanOrActiveSubscription(tx, name); err != nil {
			return err
		}
		if err := SnapshotUnfinishedTaskGroupRatiosWithTx(tx, name, group.Ratio); err != nil {
			return err
		}
		if err := tx.Model(&UserGroupMembership{}).Where("group_name = ?", name).Distinct("user_id").Pluck("user_id", &users).Error; err != nil {
			return err
		}
		var legacyUsers []int
		if err := tx.Model(&User{}).Where(commonGroupCol+" = ? OR topup_group = ?", name, name).Distinct("id").Pluck("id", &legacyUsers).Error; err != nil {
			return err
		}
		users = uniqueInts(append(users, legacyUsers...))
		if err := deleteGroupReferencesWithTx(tx, name, &users); err != nil {
			return err
		}
		users = uniqueInts(users)
		if err := persistGroupSettingsWithTx(tx, name, "", true, nil); err != nil {
			return err
		}
		for _, userId := range users {
			version, err := IncrementUserPolicyVersionWithTx(tx, userId)
			if err != nil {
				return err
			}
			policyVersions[userId] = version
		}
		if err := tx.Create(&GroupNameHistory{OldName: name, NewName: "", ChangedAt: time.Now().Unix()}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&Group{}, "name = ?", name).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	finishGroupReferenceUpdates(users, policyVersions)
	loadOptionsFromDatabase()
	InitChannelCache()
	return nil
}

func ensureGroupHasNoPlanOrActiveSubscription(tx *gorm.DB, groupName string) error {
	var plans int64
	if err := tx.Model(&SubscriptionPlan{}).Where("upgrade_group = ? OR downgrade_group = ?", groupName, groupName).Count(&plans).Error; err != nil {
		return err
	}
	if plans > 0 {
		return ErrGroupHasSubscriptions
	}
	var subscriptions int64
	if err := tx.Model(&UserSubscription{}).Where("status = ? AND end_time > ? AND (upgrade_group = ? OR downgrade_group = ? OR prev_user_group = ?)", SubscriptionStatusActive, GetDBTimestamp(), groupName, groupName, groupName).Count(&subscriptions).Error; err != nil {
		return err
	}
	if subscriptions > 0 {
		return ErrGroupHasSubscriptions
	}
	var registrationCodes int64
	if err := tx.Model(&Redemption{}).
		Where("type = ? AND target_group = ? AND status IN ?", RedemptionTypeRegistration, groupName, []int{common.RedemptionCodeStatusEnabled, common.RedemptionCodeStatusDisabled}).
		Count(&registrationCodes).Error; err != nil {
		return err
	}
	if registrationCodes > 0 {
		return ErrGroupHasSubscriptions
	}
	hasSnapshotReference, err := groupHasUnconsumedSubscriptionRedemptionSnapshotReference(tx, groupName)
	if err != nil {
		return err
	}
	if hasSnapshotReference {
		return ErrGroupHasSubscriptions
	}
	return nil
}

func GroupHistoryNames() ([]GroupNameHistory, error) {
	var history []GroupNameHistory
	err := DB.Order("changed_at desc, old_name asc").Find(&history).Error
	return history, err
}

func groupListContains(groups []string, name string) bool {
	return slices.Contains(groups, name)
}
