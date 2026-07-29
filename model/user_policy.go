package model

import (
	"errors"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultUserGroup = "default"

type UserGroupMembership struct {
	Id        int    `json:"id"`
	UserId    int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_group_membership,priority:1;index"`
	GroupName string `json:"group_name" gorm:"type:varchar(64);not null;uniqueIndex:idx_user_group_membership,priority:2;index"`
	SortOrder int    `json:"sort_order" gorm:"not null;default:0;index:idx_user_group_membership_order,priority:1"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	// nil is treated as a manual membership for rows created before this field
	// existed. false identifies a membership held only by active subscriptions.
	Manual *bool `json:"-"`
}

func (m *UserGroupMembership) BeforeCreate(tx *gorm.DB) error {
	if m.CreatedAt == 0 {
		m.CreatedAt = common.GetTimestamp()
	}
	return nil
}

type UserModelPermission struct {
	Id        int    `json:"id"`
	UserId    int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_model_permission,priority:1;index"`
	ModelName string `json:"model_name" gorm:"type:varchar(191);not null;uniqueIndex:idx_user_model_permission,priority:2;index"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

type UserModelBlock struct {
	Id        int    `json:"id"`
	UserId    int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_model_block,priority:1;index"`
	ModelName string `json:"model_name" gorm:"type:varchar(191);not null;uniqueIndex:idx_user_model_block,priority:2;index"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

type UserPolicyUpdate struct {
	Groups []string `json:"groups"`
	// PrimaryGroup is the compatibility/default group used by legacy clients
	// and tokens that do not select a billing group explicitly. It must be one
	// of Groups; membership order is kept aligned with it for old code paths.
	PrimaryGroup          string   `json:"primary_group"`
	TopupGroup            string   `json:"topup_group"`
	ModelLimitsEnabled    bool     `json:"model_limits_enabled"`
	ModelLimits           []string `json:"model_limits"`
	ModelBlocklistEnabled bool     `json:"model_blocklist_enabled"`
	ModelBlocklist        []string `json:"model_blocklist"`
}

func (p *UserModelPermission) BeforeCreate(tx *gorm.DB) error {
	if p.CreatedAt == 0 {
		p.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func (b *UserModelBlock) BeforeCreate(tx *gorm.DB) error {
	if b.CreatedAt == 0 {
		b.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func normalizePolicyValues(values []string) []string {
	normalized := normalizeOrderedPolicyValues(values)
	sort.Strings(normalized)
	return normalized
}

func normalizeOrderedPolicyValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func GetUserGroups(userId int) ([]string, error) {
	return getUserGroupsWithTx(DB, userId)
}

func getUserGroupsWithTx(tx *gorm.DB, userId int) ([]string, error) {
	if tx == nil || userId <= 0 {
		return nil, errors.New("invalid user group query")
	}
	var memberships []UserGroupMembership
	if err := tx.Where("user_id = ?", userId).Order("sort_order asc, id asc").Find(&memberships).Error; err != nil {
		if policyTableMissing(err) {
			var legacy User
			if legacyErr := tx.Where("id = ?", userId).First(&legacy).Error; legacyErr != nil {
				return nil, legacyErr
			}
			return legacyUserGroups(legacy.Group), nil
		}
		return nil, err
	}
	groups := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		groups = append(groups, membership.GroupName)
	}
	if len(groups) == 0 {
		// A rolling upgrade can have the membership table already created while
		// existing users have not been backfilled yet. Keep the legacy column as
		// the read fallback until the first policy write materializes memberships.
		var legacy User
		if err := tx.Where("id = ?", userId).First(&legacy).Error; err != nil {
			return nil, err
		}
		return legacyUserGroups(legacy.Group), nil
	}
	return groups, nil
}

// syncUserPrimaryGroupWithTx keeps the legacy users.group column aligned with
// the first ordered membership. New authorization reads use memberships, but
// a number of older paths and third-party integrations still read users.group.
// The helper deliberately treats the membership order as authoritative.
func syncUserPrimaryGroupWithTx(tx *gorm.DB, userId int) (string, bool, error) {
	if tx == nil || userId <= 0 {
		return "", false, errors.New("invalid user primary group sync")
	}
	groups, err := getUserGroupsWithTx(tx, userId)
	if err != nil {
		return "", false, err
	}
	primary := ""
	if len(groups) > 0 {
		primary = groups[0]
	}
	var user User
	if err := lockForUpdate(tx).Where("id = ?", userId).First(&user).Error; err != nil {
		return "", false, err
	}
	if user.Group == primary {
		return primary, false, nil
	}
	if err := tx.Model(&User{}).Where("id = ?", userId).Update("group", primary).Error; err != nil {
		return "", false, err
	}
	return primary, true, nil
}

// syncUserTopupGroupWithTx keeps the independently selected top-up group valid
// when a temporary subscription membership is removed. A still-authorized
// choice is preserved; otherwise the current primary membership is used.
func syncUserTopupGroupWithTx(tx *gorm.DB, userId int) (string, bool, error) {
	if tx == nil || userId <= 0 {
		return "", false, errors.New("invalid user top-up group sync")
	}
	groups, err := getUserGroupsWithTx(tx, userId)
	if err != nil {
		return "", false, err
	}
	var user User
	if err := lockForUpdate(tx).Select("id", "topup_group").Where("id = ?", userId).First(&user).Error; err != nil {
		return "", false, err
	}
	current := strings.TrimSpace(user.TopupGroup)
	for _, group := range groups {
		if group == current {
			return current, false, nil
		}
	}
	target := ""
	if len(groups) > 0 {
		target = groups[0]
	}
	if target == current {
		return target, false, nil
	}
	if err := tx.Model(&User{}).Where("id = ?", userId).Update("topup_group", target).Error; err != nil {
		return "", false, err
	}
	return target, true, nil
}

func policyTableMissing(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") ||
		strings.Contains(message, "doesn't exist") ||
		strings.Contains(message, "relation") && strings.Contains(message, "does not exist")
}

func legacyUserGroups(group string) []string {
	group = strings.TrimSpace(group)
	if group == "" || group == "auto" {
		group = defaultUserGroup
	}
	return []string{group}
}

func GetUserModelPermissions(userId int) ([]string, error) {
	return getUserModelPermissionsWithTx(DB, userId)
}

func getUserModelPermissionsWithTx(tx *gorm.DB, userId int) ([]string, error) {
	if tx == nil || userId <= 0 {
		return nil, errors.New("invalid user model permission query")
	}
	var permissions []UserModelPermission
	if err := tx.Where("user_id = ?", userId).Order("model_name asc").Find(&permissions).Error; err != nil {
		if policyTableMissing(err) {
			return []string{}, nil
		}
		return nil, err
	}
	models := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		models = append(models, permission.ModelName)
	}
	return models, nil
}

func getUserModelBlocksWithTx(tx *gorm.DB, userId int) ([]string, error) {
	if tx == nil || userId <= 0 {
		return nil, errors.New("invalid user model block query")
	}
	var blocks []UserModelBlock
	if err := tx.Where("user_id = ?", userId).Order("model_name asc").Find(&blocks).Error; err != nil {
		if policyTableMissing(err) {
			return []string{}, nil
		}
		return nil, err
	}
	models := make([]string, 0, len(blocks))
	for _, block := range blocks {
		models = append(models, block.ModelName)
	}
	return models, nil
}

func hydrateUserPolicyWithTx(tx *gorm.DB, user *User) error {
	if user == nil || user.Id <= 0 {
		return errors.New("invalid user policy hydrate")
	}
	groups, err := getUserGroupsWithTx(tx, user.Id)
	if err != nil {
		// During rolling upgrades (and in isolated legacy fixtures), the new
		// policy tables may not exist yet. Reads remain compatible with the
		// legacy single-group column; policy writes still fail until migration.
		if policyTableMissing(err) {
			user.Groups = legacyUserGroups(user.Group)
			user.ModelLimits = []string{}
			user.ModelBlocklist = []string{}
			if user.PolicyVersion < 1 {
				user.PolicyVersion = 1
			}
			return nil
		}
		return err
	}
	models, err := getUserModelPermissionsWithTx(tx, user.Id)
	if err != nil {
		return err
	}
	blockedModels, err := getUserModelBlocksWithTx(tx, user.Id)
	if err != nil {
		return err
	}
	user.Groups = groups
	user.ModelLimits = models
	user.ModelBlocklist = blockedModels
	return nil
}

func HydrateUserPolicy(user *User) error {
	return hydrateUserPolicyWithTx(DB, user)
}

func HydrateUsersPolicy(users []*User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]int, 0, len(users))
	byId := make(map[int]*User, len(users))
	for _, user := range users {
		if user == nil || user.Id <= 0 {
			continue
		}
		user.Groups = []string{}
		user.ModelLimits = []string{}
		user.ModelBlocklist = []string{}
		ids = append(ids, user.Id)
		byId[user.Id] = user
	}
	if len(ids) == 0 {
		return nil
	}
	var memberships []UserGroupMembership
	if err := DB.Where("user_id IN ?", ids).Order("sort_order asc, id asc").Find(&memberships).Error; err != nil {
		if policyTableMissing(err) {
			for _, user := range users {
				if user != nil {
					user.Groups = legacyUserGroups(user.Group)
					user.ModelLimits = []string{}
					user.ModelBlocklist = []string{}
					if user.PolicyVersion < 1 {
						user.PolicyVersion = 1
					}
				}
			}
			return nil
		}
		return err
	}
	for _, membership := range memberships {
		if user := byId[membership.UserId]; user != nil {
			user.Groups = append(user.Groups, membership.GroupName)
		}
	}
	for _, user := range users {
		if user != nil && len(user.Groups) == 0 {
			// See getUserGroupsWithTx: the table may exist before its rows are
			// backfilled. Returning the legacy group keeps list/search/auth views
			// usable during that deployment window.
			user.Groups = legacyUserGroups(user.Group)
		}
	}
	var permissions []UserModelPermission
	if err := DB.Where("user_id IN ?", ids).Order("model_name asc").Find(&permissions).Error; err != nil {
		if policyTableMissing(err) {
			for _, user := range users {
				if user != nil {
					user.ModelLimits = []string{}
				}
			}
			return nil
		}
		return err
	}
	for _, permission := range permissions {
		if user := byId[permission.UserId]; user != nil {
			user.ModelLimits = append(user.ModelLimits, permission.ModelName)
		}
	}
	var blocks []UserModelBlock
	if err := DB.Where("user_id IN ?", ids).Order("model_name asc").Find(&blocks).Error; err != nil {
		if policyTableMissing(err) {
			for _, user := range users {
				if user != nil {
					user.ModelBlocklist = []string{}
				}
			}
			return nil
		}
		return err
	}
	for _, block := range blocks {
		if user := byId[block.UserId]; user != nil {
			user.ModelBlocklist = append(user.ModelBlocklist, block.ModelName)
		}
	}
	return nil
}

func replaceUserGroupsWithTx(tx *gorm.DB, userId int, groups []string) error {
	if tx == nil || userId <= 0 {
		return errors.New("invalid user group update")
	}
	groups = normalizeOrderedPolicyValues(groups)
	selected := make(map[string]struct{}, len(groups))
	groupOrder := make(map[string]int, len(groups))
	for index, group := range groups {
		selected[group] = struct{}{}
		groupOrder[group] = index
	}

	var memberships []UserGroupMembership
	if err := lockForUpdate(tx).Where("user_id = ?", userId).Find(&memberships).Error; err != nil {
		return err
	}
	existing := make(map[string]UserGroupMembership, len(memberships))
	now := GetDBTimestamp()
	nextSubscriptionOrder := len(groups)
	for _, membership := range memberships {
		existing[membership.GroupName] = membership
		if _, keepManual := selected[membership.GroupName]; keepManual {
			desiredOrder := groupOrder[membership.GroupName]
			if membership.SortOrder != desiredOrder {
				if err := tx.Model(&UserGroupMembership{}).Where("id = ?", membership.Id).
					Update("sort_order", desiredOrder).Error; err != nil {
					return err
				}
			}
			if membership.Manual == nil || !*membership.Manual {
				if err := tx.Model(&UserGroupMembership{}).Where("id = ?", membership.Id).
					Update("manual", true).Error; err != nil {
					return err
				}
			}
			continue
		}
		var activeGrantCount int64
		if err := tx.Model(&UserSubscription{}).
			Where("user_id = ? AND status = ? AND end_time > ? AND upgrade_group = ?", userId, "active", now, membership.GroupName).
			Count(&activeGrantCount).Error; err != nil {
			return err
		}
		if activeGrantCount > 0 {
			if membership.SortOrder != nextSubscriptionOrder {
				if err := tx.Model(&UserGroupMembership{}).Where("id = ?", membership.Id).
					Update("sort_order", nextSubscriptionOrder).Error; err != nil {
					return err
				}
			}
			nextSubscriptionOrder++
			if membership.Manual == nil || *membership.Manual {
				if err := tx.Model(&UserGroupMembership{}).Where("id = ?", membership.Id).
					Update("manual", false).Error; err != nil {
					return err
				}
			}
			continue
		}
		if err := tx.Delete(&membership).Error; err != nil {
			return err
		}
	}

	manual := true
	newMemberships := make([]UserGroupMembership, 0, len(groups))
	for _, group := range groups {
		if _, exists := existing[group]; exists {
			continue
		}
		newMemberships = append(newMemberships, UserGroupMembership{
			UserId:    userId,
			GroupName: group,
			SortOrder: groupOrder[group],
			Manual:    &manual,
		})
	}
	if len(newMemberships) == 0 {
		return nil
	}
	return tx.Create(&newMemberships).Error
}

func replaceUserModelPermissionsWithTx(tx *gorm.DB, userId int, models []string) error {
	if tx == nil || userId <= 0 {
		return errors.New("invalid user model permission update")
	}
	models = normalizePolicyValues(models)
	if err := tx.Where("user_id = ?", userId).Delete(&UserModelPermission{}).Error; err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	permissions := make([]UserModelPermission, 0, len(models))
	for _, modelName := range models {
		permissions = append(permissions, UserModelPermission{UserId: userId, ModelName: modelName})
	}
	return tx.Create(&permissions).Error
}

func replaceUserModelBlocksWithTx(tx *gorm.DB, userId int, models []string) error {
	if tx == nil || userId <= 0 {
		return errors.New("invalid user model block update")
	}
	models = normalizePolicyValues(models)
	if err := tx.Where("user_id = ?", userId).Delete(&UserModelBlock{}).Error; err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	blocks := make([]UserModelBlock, 0, len(models))
	for _, modelName := range models {
		blocks = append(blocks, UserModelBlock{UserId: userId, ModelName: modelName})
	}
	return tx.Create(&blocks).Error
}

func replaceUserPolicyWithTx(tx *gorm.DB, userId int, update UserPolicyUpdate) (int64, error) {
	if tx == nil || userId <= 0 {
		return 0, errors.New("invalid user policy update")
	}
	// Policy and subscription membership mutations share the same lock order:
	// user first, then group memberships. This prevents concurrent admin edits
	// and subscription grants from waiting on each other in reverse order.
	var user User
	if err := lockForUpdate(tx).Select("id").Where("id = ?", userId).First(&user).Error; err != nil {
		return 0, err
	}
	groups := normalizeOrderedPolicyValues(update.Groups)
	primaryGroup := strings.TrimSpace(update.PrimaryGroup)
	if primaryGroup == "" && len(groups) > 0 {
		primaryGroup = groups[0]
	}
	if primaryGroup != "" {
		primaryFound := false
		orderedGroups := make([]string, 0, len(groups))
		orderedGroups = append(orderedGroups, primaryGroup)
		for _, group := range groups {
			if group == primaryGroup {
				primaryFound = true
				continue
			}
			orderedGroups = append(orderedGroups, group)
		}
		if !primaryFound {
			return 0, errors.New("primary group is not a user membership")
		}
		groups = orderedGroups
	}
	models := normalizePolicyValues(update.ModelLimits)
	blockedModels := normalizePolicyValues(update.ModelBlocklist)
	if err := replaceUserGroupsWithTx(tx, userId, groups); err != nil {
		return 0, err
	}
	if err := replaceUserModelPermissionsWithTx(tx, userId, models); err != nil {
		return 0, err
	}
	if err := replaceUserModelBlocksWithTx(tx, userId, blockedModels); err != nil {
		return 0, err
	}
	if _, _, err := syncUserPrimaryGroupWithTx(tx, userId); err != nil {
		return 0, err
	}
	if err := tx.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"topup_group":             strings.TrimSpace(update.TopupGroup),
		"model_limits_enabled":    update.ModelLimitsEnabled,
		"model_blocklist_enabled": update.ModelBlocklistEnabled,
	}).Error; err != nil {
		return 0, err
	}
	return IncrementUserPolicyVersionWithTx(tx, userId)
}

// ReplaceUserPolicyWithTx applies the complete account policy inside an
// existing transaction. Callers that also update the user profile can use it
// to keep the legacy group column, memberships, model allowlist/blocklist,
// and policy version in one atomic write.
func ReplaceUserPolicyWithTx(tx *gorm.DB, userId int, update UserPolicyUpdate) (int64, error) {
	return replaceUserPolicyWithTx(tx, userId, update)
}

func ReplaceUserPolicy(userId int, update UserPolicyUpdate) error {
	var invalidTokens []Token
	var policyVersion int64
	if err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		policyVersion, err = replaceUserPolicyWithTx(tx, userId, update)
		if err != nil {
			return err
		}
		if !common.RedisEnabled {
			return nil
		}
		return tx.Select("id", commonKeyCol).Where("user_id = ?", userId).Find(&invalidTokens).Error
	}); err != nil {
		return err
	}
	if err := publishCommittedUserPolicyVersion(userId, policyVersion); err != nil {
		return err
	}
	if err := invalidateTokensCache(invalidTokens); err != nil {
		return err
	}
	return PublishUserPolicyCache(userId)
}

func initializeUserPolicyWithTx(tx *gorm.DB, user *User) error {
	if tx == nil || user == nil || user.Id <= 0 {
		return errors.New("invalid user policy initialization")
	}
	var membershipCount int64
	if err := tx.Model(&UserGroupMembership{}).Where("user_id = ?", user.Id).Count(&membershipCount).Error; err != nil {
		if policyTableMissing(err) {
			user.Groups = legacyUserGroups(user.Group)
			user.ModelLimits = []string{}
			user.ModelBlocklist = []string{}
			if user.PolicyVersion < 1 {
				user.PolicyVersion = 1
			}
			return nil
		}
		return err
	}
	if membershipCount == 0 {
		group := strings.TrimSpace(user.Group)
		if group == "" || group == "auto" {
			group = defaultUserGroup
		}
		manual := true
		membership := UserGroupMembership{UserId: user.Id, GroupName: group, SortOrder: 0, Manual: &manual}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&membership).Error; err != nil {
			return err
		}
	}
	groups, err := getUserGroupsWithTx(tx, user.Id)
	if err != nil {
		return err
	}
	user.Groups = groups
	user.ModelLimits = []string{}
	user.ModelBlocklist = []string{}
	updates := map[string]interface{}{}
	if strings.TrimSpace(user.TopupGroup) == "" {
		topupGroup := strings.TrimSpace(user.Group)
		if topupGroup == "" || topupGroup == "auto" {
			topupGroup = defaultUserGroup
		}
		updates["topup_group"] = topupGroup
		user.TopupGroup = topupGroup
	}
	if user.PolicyVersion < 1 {
		updates["policy_version"] = 1
		user.PolicyVersion = 1
	}
	if len(updates) == 0 {
		return nil
	}
	return tx.Model(&User{}).Where("id = ?", user.Id).Updates(updates).Error
}

func InitializeUserPolicies() error {
	const batchSize = 500

	// Existing installations only need one membership row per legacy user. Do
	// this in batches instead of querying every account individually on every
	// startup; after the first successful migration the first query is empty.
	var lastMembershipUserId int
	for {
		var users []User
		if err := DB.Table("users").
			Select("users.id, users."+commonGroupCol).
			Joins("LEFT JOIN user_group_memberships ON user_group_memberships.user_id = users.id").
			Where("users.id > ? AND user_group_memberships.id IS NULL", lastMembershipUserId).
			Order("users.id asc").
			Limit(batchSize).
			Scan(&users).Error; err != nil {
			return err
		}
		if len(users) == 0 {
			break
		}
		manual := true
		memberships := make([]UserGroupMembership, 0, len(users))
		for i := range users {
			group := strings.TrimSpace(users[i].Group)
			if group == "" || group == "auto" {
				group = defaultUserGroup
			}
			memberships = append(memberships, UserGroupMembership{
				UserId:    users[i].Id,
				GroupName: group,
				SortOrder: 0,
				Manual:    &manual,
			})
		}
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&memberships).Error; err != nil {
			return err
		}
		lastMembershipUserId = users[len(users)-1].Id
	}

	if err := DB.Model(&User{}).
		Where("policy_version < ?", 1).
		Update("policy_version", 1).Error; err != nil {
		return err
	}

	// Top-up groups predate the multi-group policy and are derived from the
	// legacy primary group. Update only missing rows, grouping identical values
	// into one statement so the migration remains bounded by batch/group count.
	var lastTopupUserId int
	for {
		var users []User
		if err := DB.Select("id, "+commonGroupCol).
			Where("id > ? AND (topup_group = ? OR topup_group IS NULL)", lastTopupUserId, "").
			Order("id asc").
			Limit(batchSize).
			Find(&users).Error; err != nil {
			return err
		}
		if len(users) == 0 {
			return nil
		}

		userIdsByTopupGroup := make(map[string][]int)
		for i := range users {
			topupGroup := strings.TrimSpace(users[i].Group)
			if topupGroup == "" || topupGroup == "auto" {
				topupGroup = defaultUserGroup
			}
			userIdsByTopupGroup[topupGroup] = append(userIdsByTopupGroup[topupGroup], users[i].Id)
		}
		topupGroups := make([]string, 0, len(userIdsByTopupGroup))
		for topupGroup := range userIdsByTopupGroup {
			topupGroups = append(topupGroups, topupGroup)
		}
		sort.Strings(topupGroups)
		if err := DB.Transaction(func(tx *gorm.DB) error {
			for _, topupGroup := range topupGroups {
				if err := tx.Model(&User{}).
					Where("id IN ?", userIdsByTopupGroup[topupGroup]).
					Update("topup_group", topupGroup).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		lastTopupUserId = users[len(users)-1].Id
	}
}
