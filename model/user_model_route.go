package model

import (
	"errors"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

var ErrUserModelRouteConflict = errors.New("user model route overlaps an existing rule")

type UserModelRoute struct {
	Id             int      `json:"id"`
	UserId         int      `json:"user_id" gorm:"not null;index:idx_user_model_route_lookup,priority:1"`
	SourceModel    string   `json:"source_model" gorm:"type:varchar(191);not null;index:idx_user_model_route_lookup,priority:2"`
	TargetModel    string   `json:"target_model" gorm:"type:varchar(191);not null"`
	PoolName       string   `json:"pool_name" gorm:"type:varchar(191);not null"`
	AllGroups      bool     `json:"all_groups"`
	ExecutionGroup string   `json:"execution_group" gorm:"type:varchar(64);not null"`
	Enabled        bool     `json:"enabled" gorm:"index:idx_user_model_route_lookup,priority:3"`
	CreatedAt      int64    `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64    `json:"updated_at" gorm:"bigint"`
	Groups         []string `json:"groups" gorm:"-:all"`
	ChannelIds     []int    `json:"channel_ids" gorm:"-:all"`
}

type UserModelRouteGroup struct {
	Id        int    `json:"id"`
	RouteId   int    `json:"route_id" gorm:"not null;uniqueIndex:idx_user_model_route_group,priority:1;index"`
	GroupName string `json:"group_name" gorm:"type:varchar(64);not null;uniqueIndex:idx_user_model_route_group,priority:2;index"`
}

type UserModelRouteChannel struct {
	Id        int `json:"id"`
	RouteId   int `json:"route_id" gorm:"not null;uniqueIndex:idx_user_model_route_channel,priority:1;index"`
	ChannelId int `json:"channel_id" gorm:"not null;uniqueIndex:idx_user_model_route_channel,priority:2;index"`
}

type UserModelRouteCandidateChannel struct {
	Id                    int               `json:"id"`
	Name                  string            `json:"name"`
	Type                  int               `json:"type"`
	Priority              *int64            `json:"priority"`
	Weight                uint              `json:"weight"`
	AggregateId           *int              `json:"aggregate_id,omitempty"`
	AggregateName         string            `json:"aggregate_name,omitempty"`
	ProtocolCompatibility map[string]string `json:"protocol_compatibility" gorm:"-"`
	BaseURL               *string           `json:"-"`
	OtherSettings         string            `json:"-"`
	InheritAggregateURL   bool              `json:"-"`
	AggregateBaseURL      string            `json:"-"`
}

func (route *UserModelRoute) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if route.CreatedAt == 0 {
		route.CreatedAt = now
	}
	if route.UpdatedAt == 0 {
		route.UpdatedAt = now
	}
	return nil
}

func normalizeRouteChannelIds(channelIds []int) []int {
	seen := make(map[int]struct{}, len(channelIds))
	normalized := make([]int, 0, len(channelIds))
	for _, channelId := range channelIds {
		if channelId <= 0 {
			continue
		}
		if _, exists := seen[channelId]; exists {
			continue
		}
		seen[channelId] = struct{}{}
		normalized = append(normalized, channelId)
	}
	sort.Ints(normalized)
	return normalized
}

func hydrateUserModelRoutesWithTx(tx *gorm.DB, routes []*UserModelRoute) error {
	if len(routes) == 0 {
		return nil
	}
	routeById := make(map[int]*UserModelRoute, len(routes))
	ids := make([]int, 0, len(routes))
	for _, route := range routes {
		if route == nil || route.Id <= 0 {
			continue
		}
		route.Groups = []string{}
		route.ChannelIds = []int{}
		routeById[route.Id] = route
		ids = append(ids, route.Id)
	}
	if len(ids) == 0 {
		return nil
	}
	var groups []UserModelRouteGroup
	if err := tx.Where("route_id IN ?", ids).Order("group_name asc").Find(&groups).Error; err != nil {
		if policyTableMissing(err) {
			return nil
		}
		return err
	}
	for _, group := range groups {
		if route := routeById[group.RouteId]; route != nil {
			route.Groups = append(route.Groups, group.GroupName)
		}
	}
	var channels []UserModelRouteChannel
	if err := tx.Where("route_id IN ?", ids).Order("channel_id asc").Find(&channels).Error; err != nil {
		if policyTableMissing(err) {
			return nil
		}
		return err
	}
	for _, channel := range channels {
		if route := routeById[channel.RouteId]; route != nil {
			route.ChannelIds = append(route.ChannelIds, channel.ChannelId)
		}
	}
	return nil
}

func GetUserModelRoutes(userId int) ([]*UserModelRoute, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	var routes []*UserModelRoute
	if err := DB.Where("user_id = ?", userId).Order("source_model asc, all_groups asc, id asc").Find(&routes).Error; err != nil {
		if policyTableMissing(err) {
			return []*UserModelRoute{}, nil
		}
		return nil, err
	}
	if err := hydrateUserModelRoutesWithTx(DB, routes); err != nil {
		return nil, err
	}
	return routes, nil
}

func GetApplicableUserModelRoute(userId int, sourceModel string, group string) (*UserModelRoute, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil, nil
	}
	return GetApplicableUserModelRouteForGroups(userId, sourceModel, []string{group})
}

// GetApplicableUserModelRouteForGroups resolves scoped rules in the supplied
// group order and uses an all-groups rule only as a fallback. This makes auto
// tokens deterministic for users with ordered memberships.
func GetApplicableUserModelRouteForGroups(userId int, sourceModel string, groups []string) (*UserModelRoute, error) {
	sourceModel = strings.TrimSpace(sourceModel)
	groups = normalizeOrderedPolicyValues(groups)
	if userId <= 0 || sourceModel == "" || len(groups) == 0 {
		return nil, nil
	}
	var routes []*UserModelRoute
	if err := DB.Where("user_id = ? AND source_model = ? AND enabled = ?", userId, sourceModel, true).
		Order("all_groups asc, id asc").Find(&routes).Error; err != nil {
		if policyTableMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := hydrateUserModelRoutesWithTx(DB, routes); err != nil {
		return nil, err
	}
	var fallback *UserModelRoute
	for _, route := range routes {
		if route.AllGroups {
			if fallback == nil {
				fallback = route
			}
			continue
		}
	}
	for _, group := range groups {
		for _, route := range routes {
			if !route.AllGroups && userModelRouteAppliesToGroup(route, group) {
				return route, nil
			}
		}
	}
	return fallback, nil
}

func userModelRouteAppliesToGroup(route *UserModelRoute, group string) bool {
	if route == nil || strings.TrimSpace(group) == "" {
		return false
	}
	if route.AllGroups {
		return true
	}
	for _, routeGroup := range route.Groups {
		if routeGroup == group {
			return true
		}
	}
	return false
}

func GetEnabledUserModelRouteSources(userId int, groups []string) ([]string, error) {
	if userId <= 0 || len(groups) == 0 {
		return []string{}, nil
	}
	routes, err := GetUserModelRoutes(userId)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	sources := make([]string, 0)
	for _, route := range routes {
		if route == nil || !route.Enabled {
			continue
		}
		applies := false
		for _, group := range groups {
			if userModelRouteAppliesToGroup(route, group) {
				applies = true
				break
			}
		}
		if !applies {
			continue
		}
		if _, exists := seen[route.SourceModel]; exists {
			continue
		}
		seen[route.SourceModel] = struct{}{}
		sources = append(sources, route.SourceModel)
	}
	sort.Strings(sources)
	return sources, nil
}

func GetUserModelRouteTargetModels() ([]string, error) {
	models := make([]string, 0)
	err := DB.Table("abilities").
		Select("abilities.model").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.enabled = ? AND channels.status = ? AND abilities.model <> ?", true, common.ChannelStatusEnabled, "").
		Distinct().
		Order("abilities.model asc").
		Pluck("abilities.model", &models).Error
	return models, err
}

func GetUserModelRouteCandidateChannels(group, modelName string) ([]UserModelRouteCandidateChannel, error) {
	group = strings.TrimSpace(group)
	modelName = strings.TrimSpace(modelName)
	if group == "" || modelName == "" {
		return []UserModelRouteCandidateChannel{}, nil
	}

	query := DB.Table("abilities").
		Select("channels.id AS id, channels.name AS name, channels.type AS type, abilities.priority AS priority, abilities.weight AS weight, channels.aggregate_id AS aggregate_id, channel_aggregates.name AS aggregate_name, channels.base_url AS base_url, channels.settings AS other_settings, channels.inherit_aggregate_base_url AS inherit_aggregate_url, channel_aggregates.base_url AS aggregate_base_url").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Joins("LEFT JOIN channel_aggregates ON channel_aggregates.id = channels.aggregate_id").
		Where("abilities."+commonGroupCol+" = ? AND abilities.enabled = ? AND channels.status = ?", group, true, common.ChannelStatusEnabled).
		Order("abilities.priority DESC").
		Order("abilities.weight DESC").
		Order("channels.id ASC")

	channels := make([]UserModelRouteCandidateChannel, 0)
	if err := query.Where("abilities.model = ?", modelName).Scan(&channels).Error; err != nil {
		return nil, err
	}
	if len(channels) > 0 {
		return channels, nil
	}

	normalizedModel := ratio_setting.FormatMatchingModelName(modelName)
	if normalizedModel == "" || normalizedModel == modelName {
		return channels, nil
	}
	if err := query.Where("abilities.model = ?", normalizedModel).Scan(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}

func GetUserModelRouteExecutionGroupChannelCounts(modelName string) (map[string]int64, error) {
	modelName = strings.TrimSpace(modelName)
	counts := make(map[string]int64)
	if modelName == "" {
		return counts, nil
	}
	type groupCount struct {
		GroupName string
		Count     int64
	}
	load := func(candidateModel string) ([]groupCount, error) {
		rows := make([]groupCount, 0)
		err := DB.Table("abilities").
			Select("abilities."+commonGroupCol+" AS group_name, COUNT(DISTINCT abilities.channel_id) AS count").
			Joins("JOIN channels ON abilities.channel_id = channels.id").
			Where("abilities.model = ? AND abilities.enabled = ? AND channels.status = ?", candidateModel, true, common.ChannelStatusEnabled).
			Group("abilities." + commonGroupCol).
			Scan(&rows).Error
		return rows, err
	}
	rows, err := load(modelName)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(modelName)
		if normalizedModel != "" && normalizedModel != modelName {
			rows, err = load(normalizedModel)
			if err != nil {
				return nil, err
			}
		}
	}
	for _, row := range rows {
		counts[row.GroupName] = row.Count
	}
	return counts, nil
}

func SaveUserModelRoute(route *UserModelRoute) error {
	if route == nil || route.UserId <= 0 {
		return errors.New("invalid user model route")
	}
	route.SourceModel = strings.TrimSpace(route.SourceModel)
	route.TargetModel = strings.TrimSpace(route.TargetModel)
	route.PoolName = strings.TrimSpace(route.PoolName)
	if route.PoolName == "" {
		route.PoolName = route.SourceModel + " → " + route.TargetModel
	}
	route.ExecutionGroup = strings.TrimSpace(route.ExecutionGroup)
	route.Groups = normalizePolicyValues(route.Groups)
	route.ChannelIds = normalizeRouteChannelIds(route.ChannelIds)
	if route.SourceModel == "" || route.TargetModel == "" || route.ExecutionGroup == "" || len(route.ChannelIds) == 0 {
		return errors.New("incomplete user model route")
	}
	if len(route.PoolName) > 191 {
		return errors.New("user model route pool name is too long")
	}
	if !route.AllGroups && len(route.Groups) == 0 {
		return errors.New("a scoped user model route requires groups")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", route.UserId).First(&user).Error; err != nil {
			return err
		}
		if route.Id > 0 {
			var existing UserModelRoute
			if err := tx.Where("id = ? AND user_id = ?", route.Id, route.UserId).First(&existing).Error; err != nil {
				return err
			}
		}

		conflictQuery := tx.Model(&UserModelRoute{}).
			Where("user_id = ? AND source_model = ?", route.UserId, route.SourceModel)
		if route.Id > 0 {
			conflictQuery = conflictQuery.Where("id <> ?", route.Id)
		}
		var conflictCount int64
		if route.AllGroups {
			// An all-groups rule owns the source model for every scope. It must
			// therefore conflict with both existing all-groups and scoped rules.
			if err := conflictQuery.Count(&conflictCount).Error; err != nil {
				return err
			}
		} else {
			// Use a LEFT JOIN so an existing all-groups rule (which has no child
			// rows) is still considered a conflict. Distinct avoids counting one
			// route more than once when several selected groups overlap.
			conflictQuery = conflictQuery.
				Joins("LEFT JOIN user_model_route_groups ON user_model_route_groups.route_id = user_model_routes.id").
				Where("user_model_routes.all_groups = ? OR user_model_route_groups.group_name IN ?", true, route.Groups).
				Distinct("user_model_routes.id")
			if err := conflictQuery.Count(&conflictCount).Error; err != nil {
				return err
			}
		}
		if conflictCount > 0 {
			return ErrUserModelRouteConflict
		}

		route.UpdatedAt = common.GetTimestamp()
		if route.Id == 0 {
			if err := tx.Create(route).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&UserModelRoute{}).Where("id = ? AND user_id = ?", route.Id, route.UserId).
			Updates(map[string]interface{}{
				"source_model":    route.SourceModel,
				"target_model":    route.TargetModel,
				"pool_name":       route.PoolName,
				"all_groups":      route.AllGroups,
				"execution_group": route.ExecutionGroup,
				"enabled":         route.Enabled,
				"updated_at":      route.UpdatedAt,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("route_id = ?", route.Id).Delete(&UserModelRouteGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Where("route_id = ?", route.Id).Delete(&UserModelRouteChannel{}).Error; err != nil {
			return err
		}
		if !route.AllGroups {
			groups := make([]UserModelRouteGroup, 0, len(route.Groups))
			for _, group := range route.Groups {
				groups = append(groups, UserModelRouteGroup{RouteId: route.Id, GroupName: group})
			}
			if err := tx.Create(&groups).Error; err != nil {
				return err
			}
		}
		channels := make([]UserModelRouteChannel, 0, len(route.ChannelIds))
		for _, channelId := range route.ChannelIds {
			channels = append(channels, UserModelRouteChannel{RouteId: route.Id, ChannelId: channelId})
		}
		return tx.Create(&channels).Error
	})
}

func DeleteUserModelRoute(userId int, routeId int) error {
	if userId <= 0 || routeId <= 0 {
		return errors.New("invalid user model route")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var route UserModelRoute
		if err := tx.Where("id = ? AND user_id = ?", routeId, userId).First(&route).Error; err != nil {
			return err
		}
		if err := tx.Where("route_id = ?", routeId).Delete(&UserModelRouteGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Where("route_id = ?", routeId).Delete(&UserModelRouteChannel{}).Error; err != nil {
			return err
		}
		return tx.Delete(&route).Error
	})
}
