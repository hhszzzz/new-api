package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"not null"`
	OpenAIOrganization *string `json:"openai_organization"`
	TestModel          *string `json:"test_model"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	Weight             *uint   `json:"weight" gorm:"default:0"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(64);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:text"`
	//MaxInputTokens     *int    `json:"max_input_tokens" gorm:"default:0"`
	StatusCodeMapping       *string         `json:"status_code_mapping" gorm:"type:varchar(1024);default:''"`
	Priority                *int64          `json:"priority" gorm:"bigint;default:0"`
	AutoBan                 *int            `json:"auto_ban" gorm:"default:1"`
	OtherInfo               string          `json:"other_info"`
	Tag                     *string         `json:"tag" gorm:"index"`
	Setting                 *string         `json:"setting" gorm:"type:text"` // 渠道额外设置
	ParamOverride           *string         `json:"param_override" gorm:"type:text"`
	HeaderOverride          *string         `json:"header_override" gorm:"type:text"`
	Remark                  *string         `json:"remark" gorm:"type:varchar(255)" validate:"max=255"`
	Schedule                ChannelSchedule `json:"schedule" gorm:"type:text"`
	AggregateId             *int            `json:"aggregate_id,omitempty" gorm:"index"`
	InheritAggregateBaseURL bool            `json:"inherit_aggregate_base_url"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info" gorm:"type:json"`

	OtherSettings string `json:"settings" gorm:"column:settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys             []string             `json:"-" gorm:"-"`
	AggregateName    string               `json:"aggregate_name,omitempty" gorm:"-"`
	AggregateBaseURL string               `json:"aggregate_base_url,omitempty" gorm:"-"`
	EffectiveStatus  string               `json:"effective_status" gorm:"-"`
	ScheduleState    ChannelScheduleState `json:"schedule_state" gorm:"-"`
}

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize           int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
}

type ChannelSortOptions struct {
	SortBy    string
	SortOrder string
	IDSort    bool
}

var channelSortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"priority":      "priority",
	"balance":       "balance",
	"response_time": "response_time",
	"test_time":     "test_time",
}

func NewChannelSortOptions(sortBy string, sortOrder string, idSort bool) ChannelSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := channelSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = ""
		normalizedSortOrder = ""
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return ChannelSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
		IDSort:    idSort,
	}
}

func (options ChannelSortOptions) Apply(query *gorm.DB) *gorm.DB {
	columnName, descending := options.effectiveSort()
	query = query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: columnName},
		Desc:   descending,
	})
	if columnName == "id" {
		return query
	}
	return query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: "id"},
		Desc:   true,
	})
}

func (options ChannelSortOptions) effectiveSort() (string, bool) {
	if columnName, ok := channelSortColumns[options.SortBy]; ok {
		return columnName, options.SortOrder != "asc"
	}
	if options.IDSort {
		return "id", true
	}
	return "priority", true
}

func resolveChannelSortOptions(idSort bool, sortOptions []ChannelSortOptions) ChannelSortOptions {
	if len(sortOptions) == 0 {
		return NewChannelSortOptions("", "", idSort)
	}
	options := sortOptions[0]
	options.IDSort = options.IDSort || idSort
	return options
}

func NormalizeChannelGroupFilter(group string) string {
	group = strings.TrimSpace(group)
	if group == "" || strings.EqualFold(group, "all") || strings.EqualFold(group, "null") {
		return ""
	}
	return group
}

func channelGroupFilterCondition() string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return `CONCAT(',', ` + commonGroupCol + `, ',') LIKE ? ESCAPE '!'`
	}
	return `(',' || ` + commonGroupCol + ` || ',') LIKE ? ESCAPE '!'`
}

func channelGroupFilterPattern(group string) string {
	group = strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(group)
	return "%," + group + ",%"
}

func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	group = NormalizeChannelGroupFilter(group)
	if group == "" {
		return query
	}
	return query.Where(channelGroupFilterCondition(), channelGroupFilterPattern(group))
}

// Value implements driver.Valuer interface
func (c ChannelInfo) Value() (driver.Value, error) {
	return common.Marshal(&c)
}

// Scan implements sql.Scanner interface
func (c *ChannelInfo) Scan(value interface{}) error {
	bytesValue, _ := value.([]byte)
	return common.Unmarshal(bytesValue, c)
}

func (channel *Channel) GetKeys() []string {
	if channel.Key == "" {
		return []string{}
	}
	if len(channel.Keys) > 0 {
		return channel.Keys
	}
	trimmed := strings.TrimSpace(channel.Key)
	// If the key starts with '[', try to parse it as a JSON array (e.g., for Vertex AI scenarios)
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
			res := make([]string, len(arr))
			for i, v := range arr {
				res[i] = string(v)
			}
			return res
		}
	}
	// Otherwise, fall back to splitting by newline
	keys := strings.Split(strings.Trim(channel.Key, "\n"), "\n")
	return keys
}

func (channel *Channel) GetNextEnabledKey() (string, int, *types.NewAPIError) {
	// If not in multi-key mode, return the original key string directly.
	if !channel.ChannelInfo.IsMultiKey {
		return channel.Key, 0, nil
	}

	// Obtain all keys (split by \n)
	keys := channel.GetKeys()
	if len(keys) == 0 {
		// No keys available, return error, should disable the channel
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	statusList := channel.ChannelInfo.MultiKeyStatusList
	// helper to get key status, default to enabled when missing
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}

	// Collect indexes of enabled keys
	enabledIdx := make([]int, 0, len(keys))
	for i := range keys {
		if getStatus(i) == common.ChannelStatusEnabled {
			enabledIdx = append(enabledIdx, i)
		}
	}
	// If no specific status list or none enabled, return an explicit error so caller can
	// properly handle a channel with no available keys (e.g. mark channel disabled).
	// Returning the first key here caused requests to keep using an already-disabled key.
	if len(enabledIdx) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	switch channel.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		// Randomly pick one enabled key
		selectedIdx := enabledIdx[rand.Intn(len(enabledIdx))]
		return keys[selectedIdx], selectedIdx, nil
	case constant.MultiKeyModePolling:
		// Use channel-specific lock to ensure thread-safe polling

		channelInfo, err := CacheGetChannelInfo(channel.Id)
		if err != nil {
			return "", 0, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		defer func() {
			if common.DebugEnabled {
				logger.LogDebug(nil, "channel %d polling index: %d", channel.Id, channel.ChannelInfo.MultiKeyPollingIndex)
			}
			if !common.MemoryCacheEnabled {
				_ = channel.SaveChannelInfo()
			} else {
				// CacheUpdateChannel(channel)
			}
		}()
		// Start from the saved polling index and look for the next enabled key
		start := channelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for i := 0; i < len(keys); i++ {
			idx := (start + i) % len(keys)
			if getStatus(idx) == common.ChannelStatusEnabled {
				// update polling index for next call (point to the next position)
				channel.ChannelInfo.MultiKeyPollingIndex = (idx + 1) % len(keys)
				return keys[idx], idx, nil
			}
		}
		// Fallback – should not happen, but return first enabled key
		return keys[enabledIdx[0]], enabledIdx[0], nil
	default:
		// Unknown mode, default to first enabled key (or original key string)
		return keys[enabledIdx[0]], enabledIdx[0], nil
	}
}

func (channel *Channel) SaveChannelInfo() error {
	return DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error
}

func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	return strings.Split(strings.Trim(channel.Models, ","), ",")
}

func (channel *Channel) GetGroups() []string {
	if channel.Group == "" {
		return []string{}
	}
	groups := strings.Split(strings.Trim(channel.Group, ","), ",")
	for i, group := range groups {
		groups[i] = strings.TrimSpace(group)
	}
	return groups
}

func (channel *Channel) GetOtherInfo() map[string]interface{} {
	otherInfo := make(map[string]interface{})
	if channel.OtherInfo != "" {
		err := common.Unmarshal([]byte(channel.OtherInfo), &otherInfo)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		}
	}
	return otherInfo
}

func (channel *Channel) SetOtherInfo(otherInfo map[string]interface{}) {
	otherInfoBytes, err := common.Marshal(otherInfo)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		return
	}
	channel.OtherInfo = string(otherInfoBytes)
}

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

func (channel *Channel) Save() error {
	return DB.Save(channel).Error
}

func (channel *Channel) SaveWithoutKey() error {
	if channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	return DB.Omit("key").Save(channel).Error
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	var err error
	order := resolveChannelSortOptions(idSort, sortOptions)
	if selectAll {
		err = order.Apply(DB).Find(&channels).Error
	} else {
		err = order.Apply(DB).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	if err != nil {
		return nil, err
	}
	if err := HydrateChannelAggregateSnapshots(channels); err != nil {
		return nil, err
	}
	return channels, nil
}

type channelTopLevelRow struct {
	AggregateId *int `gorm:"column:aggregate_id"`
	ChannelId   int  `gorm:"column:channel_id"`
}

type channelTopLevelKey struct {
	AggregateId int
	ChannelId   int
}

// GetPaginatedTopLevelChannels paginates presentation rows before loading
// their children. A standalone channel is one row, while all matching
// channels with the same aggregate_id form one expandable row.
func GetPaginatedTopLevelChannels(query *gorm.DB, startIdx int, num int, sortOptions ChannelSortOptions) ([]*Channel, int64, error) {
	if query == nil {
		return nil, 0, errors.New("channel query is required")
	}
	if startIdx < 0 {
		startIdx = 0
	}

	var standaloneCount int64
	if err := query.Session(&gorm.Session{}).
		Where("aggregate_id IS NULL").
		Count(&standaloneCount).Error; err != nil {
		return nil, 0, err
	}
	var aggregateCount int64
	if err := query.Session(&gorm.Session{}).
		Where("aggregate_id IS NOT NULL").
		Distinct("aggregate_id").
		Count(&aggregateCount).Error; err != nil {
		return nil, 0, err
	}
	total := standaloneCount + aggregateCount
	if num <= 0 || int64(startIdx) >= total {
		return []*Channel{}, total, nil
	}

	sortColumn, descending := sortOptions.effectiveSort()
	sortExpression := sortColumn
	if sortColumn == "priority" {
		sortExpression = "COALESCE(priority, 0)"
	}
	aggregateFunction := "MIN"
	if descending {
		aggregateFunction = "MAX"
	}
	standaloneIdExpression := "CASE WHEN aggregate_id IS NULL THEN id ELSE 0 END"
	selectExpression := fmt.Sprintf(
		"aggregate_id, %s AS channel_id, %s(%s) AS sort_value, MAX(id) AS tie_id",
		standaloneIdExpression,
		aggregateFunction,
		sortExpression,
	)
	topRows := make([]channelTopLevelRow, 0, num)
	topQuery := query.Session(&gorm.Session{}).
		Select(selectExpression).
		Group("aggregate_id").
		Group(standaloneIdExpression).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "sort_value"}, Desc: descending}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tie_id"}, Desc: true}).
		Limit(num).
		Offset(startIdx)
	if err := topQuery.Scan(&topRows).Error; err != nil {
		return nil, 0, err
	}
	if len(topRows) == 0 {
		return []*Channel{}, total, nil
	}

	aggregateIds := make([]int, 0, len(topRows))
	channelIds := make([]int, 0, len(topRows))
	for _, row := range topRows {
		if row.AggregateId != nil {
			aggregateIds = append(aggregateIds, *row.AggregateId)
		} else {
			channelIds = append(channelIds, row.ChannelId)
		}
	}
	selectedQuery := query.Session(&gorm.Session{})
	switch {
	case len(aggregateIds) > 0 && len(channelIds) > 0:
		selectedQuery = selectedQuery.Where(
			"aggregate_id IN ? OR (aggregate_id IS NULL AND id IN ?)",
			aggregateIds,
			channelIds,
		)
	case len(aggregateIds) > 0:
		selectedQuery = selectedQuery.Where("aggregate_id IN ?", aggregateIds)
	default:
		selectedQuery = selectedQuery.Where("aggregate_id IS NULL AND id IN ?", channelIds)
	}

	selected := make([]*Channel, 0)
	if err := sortOptions.Apply(selectedQuery).Omit("key").Find(&selected).Error; err != nil {
		return nil, 0, err
	}
	buckets := make(map[channelTopLevelKey][]*Channel, len(topRows))
	for _, channel := range selected {
		key := channelTopLevelKey{ChannelId: channel.Id}
		if channel.AggregateId != nil {
			key = channelTopLevelKey{AggregateId: *channel.AggregateId}
		}
		buckets[key] = append(buckets[key], channel)
	}
	channels := make([]*Channel, 0, len(selected))
	for _, row := range topRows {
		key := channelTopLevelKey{ChannelId: row.ChannelId}
		if row.AggregateId != nil {
			key = channelTopLevelKey{AggregateId: *row.AggregateId}
		}
		channels = append(channels, buckets[key]...)
	}
	if err := HydrateChannelAggregateSnapshots(channels); err != nil {
		return nil, 0, err
	}
	return channels, total, nil
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)
	query := order.Apply(DB.Where("tag = ?", tag))
	if !selectAll {
		query = query.Omit("key")
	}
	if err := query.Find(&channels).Error; err != nil {
		return nil, err
	}
	if err := HydrateChannelAggregateSnapshots(channels); err != nil {
		return nil, err
	}
	return channels, nil
}

func BuildChannelSearchQuery(keyword string, group string, model string) *gorm.DB {
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	return ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)
}

func SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)

	// 执行查询
	err := order.Apply(BuildChannelSearchQuery(keyword, group, model)).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	if err := HydrateChannelAggregateSnapshots(channels); err != nil {
		return nil, err
	}
	return channels, nil
}

func GetChannelById(id int, selectAll bool) (*Channel, error) {
	channel := &Channel{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(channel, "id = ?", id).Error
	} else {
		err = DB.Omit("key").First(channel, "id = ?", id).Error
	}
	if err != nil {
		return nil, err
	}
	if err := HydrateChannelAggregateSnapshots([]*Channel{channel}); err != nil {
		return nil, err
	}
	return channel, nil
}

func BatchInsertChannels(channels []Channel) error {
	if len(channels) == 0 {
		return nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	aggregateIds := make(map[int]struct{})
	for index := range channels {
		channel := &channels[index]
		if channel.AggregateId == nil {
			if channel.InheritAggregateBaseURL {
				tx.Rollback()
				return errors.New("cannot inherit an aggregate base URL without an aggregate")
			}
			continue
		}
		if *channel.AggregateId <= 0 {
			tx.Rollback()
			return errors.New("invalid channel aggregate id")
		}
		aggregateIds[*channel.AggregateId] = struct{}{}
	}
	orderedAggregateIds := make([]int, 0, len(aggregateIds))
	for aggregateId := range aggregateIds {
		orderedAggregateIds = append(orderedAggregateIds, aggregateId)
	}
	sort.Ints(orderedAggregateIds)
	for _, aggregateId := range orderedAggregateIds {
		var aggregate ChannelAggregate
		if err := lockForUpdate(tx).Select("id").First(&aggregate, "id = ?", aggregateId).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	for _, chunk := range lo.Chunk(channels, 50) {
		if err := tx.Create(&chunk).Error; err != nil {
			tx.Rollback()
			return err
		}
		for _, channel_ := range chunk {
			if err := channel_.AddAbilities(tx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	InitChannelCache()
	return nil
}

func BatchDeleteChannels(ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	return deleteChannels(func(tx *gorm.DB) ([]int, error) {
		channelIds := make([]int, 0, len(ids))
		for _, chunk := range lo.Chunk(ids, 200) {
			var existingIds []int
			query := lockForUpdate(tx.Model(&Channel{})).
				Where("id IN ?", chunk).
				Order("id ASC")
			if err := query.Pluck("id", &existingIds).Error; err != nil {
				return nil, err
			}
			channelIds = append(channelIds, existingIds...)
		}
		return channelIds, nil
	})
}

// deleteChannels keeps channel rows and all channel-owned routing data in one
// transaction. The route-table checks happen before the transaction because a
// missing-table query aborts PostgreSQL transactions even when the error is
// otherwise safe to ignore during rolling upgrades and narrow model tests.
func deleteChannels(loadChannelIds func(tx *gorm.DB) ([]int, error)) (int64, error) {
	hasRouteChannels := DB.Migrator().HasTable(&UserModelRouteChannel{})
	hasRoutes := hasRouteChannels && DB.Migrator().HasTable(&UserModelRoute{})
	var deletedCount int64

	err := DB.Transaction(func(tx *gorm.DB) error {
		channelIds, err := loadChannelIds(tx)
		if err != nil {
			return err
		}
		if len(channelIds) == 0 {
			return nil
		}

		sort.Ints(channelIds)
		channelIds = lo.Uniq(channelIds)
		for _, chunk := range lo.Chunk(channelIds, 200) {
			if err := tx.Where("channel_id IN ?", chunk).Delete(&Ability{}).Error; err != nil {
				return err
			}
		}

		if hasRouteChannels {
			affectedRouteIds := make([]int, 0)
			for _, chunk := range lo.Chunk(channelIds, 200) {
				var routeIds []int
				if err := tx.Model(&UserModelRouteChannel{}).
					Where("channel_id IN ?", chunk).
					Distinct().
					Pluck("route_id", &routeIds).Error; err != nil {
					return err
				}
				affectedRouteIds = append(affectedRouteIds, routeIds...)
				if err := tx.Where("channel_id IN ?", chunk).Delete(&UserModelRouteChannel{}).Error; err != nil {
					return err
				}
			}

			if hasRoutes && len(affectedRouteIds) > 0 {
				sort.Ints(affectedRouteIds)
				affectedRouteIds = lo.Uniq(affectedRouteIds)
				for _, routeChunk := range lo.Chunk(affectedRouteIds, 200) {
					var routesWithChannels []int
					if err := tx.Model(&UserModelRouteChannel{}).
						Where("route_id IN ?", routeChunk).
						Distinct().
						Pluck("route_id", &routesWithChannels).Error; err != nil {
						return err
					}
					remaining := make(map[int]struct{}, len(routesWithChannels))
					for _, routeId := range routesWithChannels {
						remaining[routeId] = struct{}{}
					}
					emptyRouteIds := make([]int, 0, len(routeChunk))
					for _, routeId := range routeChunk {
						if _, ok := remaining[routeId]; !ok {
							emptyRouteIds = append(emptyRouteIds, routeId)
						}
					}
					if len(emptyRouteIds) > 0 {
						if err := tx.Model(&UserModelRoute{}).
							Where("id IN ?", emptyRouteIds).
							Updates(map[string]interface{}{
								"enabled":    false,
								"updated_at": common.GetTimestamp(),
							}).Error; err != nil {
							return err
						}
					}
				}
			}
		}

		for _, chunk := range lo.Chunk(channelIds, 200) {
			result := tx.Where("id IN ?", chunk).Delete(&Channel{})
			if result.Error != nil {
				return result.Error
			}
			deletedCount += result.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if deletedCount > 0 {
		InvalidatePricingCache()
	}
	return deletedCount, nil
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetWeight() uint {
	if channel.Weight == nil {
		return 0
	}
	return *channel.Weight
}

func (channel *Channel) GetBaseURL() string {
	if channel.AggregateId != nil && channel.InheritAggregateBaseURL {
		if aggregateBaseURL := strings.TrimSpace(channel.AggregateBaseURL); aggregateBaseURL != "" {
			return aggregateBaseURL
		}
	}
	if channel.BaseURL == nil {
		return ""
	}
	url := *channel.BaseURL
	if url == "" {
		url = constant.ChannelBaseURLs[channel.Type]
	}
	return url
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

func (channel *Channel) GetStatusCodeMapping() string {
	if channel.StatusCodeMapping == nil {
		return ""
	}
	return *channel.StatusCodeMapping
}

func (channel *Channel) Insert() error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := validateChannelAggregateLinkWithTx(tx, channel, true); err != nil {
			return err
		}
		if err := tx.Create(channel).Error; err != nil {
			return err
		}
		return channel.AddAbilities(tx)
	})
	if err != nil {
		return err
	}
	InvalidatePricingCache()
	return nil
}

func (channel *Channel) prepareMultiKeyState() {
	// If this is a multi-key channel, recalculate MultiKeySize based on the current key list to avoid inconsistency after editing keys
	if channel.ChannelInfo.IsMultiKey {
		var keyStr string
		if channel.Key != "" {
			keyStr = channel.Key
		} else {
			// If key is not provided, read the existing key from the database
			if existing, err := GetChannelById(channel.Id, true); err == nil {
				keyStr = existing.Key
			}
		}
		// Parse the key list (supports newline separation or JSON array)
		keys := []string{}
		if keyStr != "" {
			trimmed := strings.TrimSpace(keyStr)
			if strings.HasPrefix(trimmed, "[") {
				var arr []json.RawMessage
				if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
					keys = make([]string, len(arr))
					for i, v := range arr {
						keys[i] = string(v)
					}
				}
			}
			if len(keys) == 0 { // fallback to newline split
				keys = strings.Split(strings.Trim(keyStr, "\n"), "\n")
			}
		}
		channel.ChannelInfo.MultiKeySize = len(keys)
		// Clean up status data that exceeds the new key count to prevent index out of range
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			for idx := range channel.ChannelInfo.MultiKeyStatusList {
				if idx >= channel.ChannelInfo.MultiKeySize {
					delete(channel.ChannelInfo.MultiKeyStatusList, idx)
				}
			}
		}
	}
}

func (channel *Channel) update(includeAggregateLink bool) error {
	channel.prepareMultiKeyState()
	aggregateId := channel.AggregateId
	inheritAggregateBaseURL := channel.InheritAggregateBaseURL

	err := DB.Transaction(func(tx *gorm.DB) error {
		if includeAggregateLink {
			// Keep the same aggregate -> channel lock order as aggregate deletion.
			if err := validateChannelAggregateLinkWithTx(tx, channel, true); err != nil {
				return err
			}
			var existing Channel
			if err := lockForUpdate(tx).Select("id").First(&existing, "id = ?", channel.Id).Error; err != nil {
				return err
			}
		}

		// A generic update must not restore an aggregate relation from a stale
		// channel snapshot. Full edits opt in through UpdateWithAggregateLink.
		if err := tx.Model(channel).
			Omit("aggregate_id", "inherit_aggregate_base_url").
			Updates(channel).Error; err != nil {
			return err
		}
		if includeAggregateLink {
			if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]interface{}{
				"aggregate_id":               aggregateId,
				"inherit_aggregate_base_url": inheritAggregateBaseURL,
			}).Error; err != nil {
				return err
			}
		}

		// Reload zero-value fields such as status before rebuilding abilities.
		if err := tx.First(channel, "id = ?", channel.Id).Error; err != nil {
			return err
		}
		return channel.UpdateAbilities(tx)
	})
	if err != nil {
		return err
	}
	InvalidatePricingCache()
	return nil
}

func (channel *Channel) Update() error {
	return channel.update(false)
}

// UpdateWithAggregateLink atomically persists a complete channel edit,
// including its aggregate relation and derived abilities.
func (channel *Channel) UpdateWithAggregateLink() error {
	return channel.update(true)
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	err := DB.Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update response time: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) UpdateBalance(balance float64) {
	err := DB.Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: common.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update balance: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) Delete() error {
	_, err := BatchDeleteChannels([]int{channel.Id})
	return err
}

var channelStatusLock sync.Mutex

// channelPollingLocks stores locks for each channel.id to ensure thread-safe polling
var channelPollingLocks sync.Map

// GetChannelPollingLock returns or creates a mutex for the given channel ID
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if lock, exists := channelPollingLocks.Load(channelId); exists {
		return lock.(*sync.Mutex)
	}
	// Create new lock for this channel
	newLock := &sync.Mutex{}
	actual, _ := channelPollingLocks.LoadOrStore(channelId, newLock)
	return actual.(*sync.Mutex)
}

// CleanupChannelPollingLocks removes locks for channels that no longer exist
// This is optional and can be called periodically to prevent memory leaks
func CleanupChannelPollingLocks() {
	var activeChannelIds []int
	DB.Model(&Channel{}).Pluck("id", &activeChannelIds)

	activeChannelSet := make(map[int]bool)
	for _, id := range activeChannelIds {
		activeChannelSet[id] = true
	}

	channelPollingLocks.Range(func(key, value interface{}) bool {
		channelId := key.(int)
		if !activeChannelSet[channelId] {
			channelPollingLocks.Delete(channelId)
		}
		return true
	})
}

func handlerMultiKeyUpdate(channel *Channel, usingKey string, status int, reason string) {
	keys := channel.GetKeys()
	if len(keys) == 0 {
		channel.Status = status
	} else {
		keyIndex := -1
		for i, key := range keys {
			if key == usingKey {
				keyIndex = i
				break
			}
		}
		if keyIndex < 0 {
			if usingKey != "" {
				common.SysLog(fmt.Sprintf("failed to update multi-key status: channel_id=%d, using key not found", channel.Id))
				return
			}
			channel.Status = status
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			return
		}
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if status == common.ChannelStatusEnabled {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		} else {
			channel.ChannelInfo.MultiKeyStatusList[keyIndex] = status
			if channel.ChannelInfo.MultiKeyDisabledReason == nil {
				channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
			}
			if channel.ChannelInfo.MultiKeyDisabledTime == nil {
				channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
			}
			channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
			channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = common.GetTimestamp()
		}
		if !hasEnabledMultiKey(keys, channel.ChannelInfo.MultiKeyStatusList) {
			channel.Status = common.ChannelStatusAutoDisabled
			info := channel.GetOtherInfo()
			info["status_reason"] = "All keys are disabled"
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
		} else if status == common.ChannelStatusEnabled {
			channel.Status = common.ChannelStatusEnabled
		}
	}
}

func hasEnabledMultiKey(keys []string, statusList map[int]int) bool {
	for i := range keys {
		if statusList == nil {
			return true
		}
		status, ok := statusList[i]
		if !ok || status == common.ChannelStatusEnabled {
			return true
		}
	}
	return false
}

func UpdateChannelStatus(channelId int, usingKey string, status int, reason string) bool {
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()

		channelCache, _ := CacheGetChannel(channelId)
		if channelCache == nil {
			return false
		}
		if channelCache.ChannelInfo.IsMultiKey {
			// Use per-channel lock to prevent concurrent map read/write with GetNextEnabledKey
			beforeStatus := channelCache.Status
			pollingLock := GetChannelPollingLock(channelId)
			pollingLock.Lock()
			// 如果是多Key模式，更新缓存中的状态
			handlerMultiKeyUpdate(channelCache, usingKey, status, reason)
			pollingLock.Unlock()
			if beforeStatus != channelCache.Status {
				CacheUpdateChannelStatus(channelId, channelCache.Status)
			}
			//CacheUpdateChannel(channelCache)
			//return true
		} else {
			// 如果缓存渠道存在，且状态已是目标状态，直接返回
			if channelCache.Status == status {
				return false
			}
			CacheUpdateChannelStatus(channelId, status)
		}
	}

	shouldUpdateAbilities := false
	defer func() {
		if shouldUpdateAbilities {
			err := UpdateAbilityStatus(channelId, status == common.ChannelStatusEnabled)
			if err != nil {
				common.SysLog(fmt.Sprintf("failed to update ability status: channel_id=%d, error=%v", channelId, err))
			}
		}
	}()
	channel, err := GetChannelById(channelId, true)
	if err != nil {
		return false
	} else {
		if channel.Status == status {
			return false
		}

		if channel.ChannelInfo.IsMultiKey {
			beforeStatus := channel.Status
			// Protect map writes with the same per-channel lock used by readers
			pollingLock := GetChannelPollingLock(channelId)
			pollingLock.Lock()
			handlerMultiKeyUpdate(channel, usingKey, status, reason)
			pollingLock.Unlock()
			if beforeStatus != channel.Status {
				shouldUpdateAbilities = true
			}
		} else {
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			channel.Status = status
			shouldUpdateAbilities = true
		}
		err = channel.SaveWithoutKey()
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, err))
			return false
		}
		InvalidatePricingCache()
	}
	return true
}

func EnableChannelByTag(tag string) error {
	err := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusEnabled).Error
	if err != nil {
		return err
	}
	InvalidatePricingCache()
	err = UpdateAbilityStatusByTag(tag, true)
	return err
}

func DisableChannelByTag(tag string) error {
	err := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusManuallyDisabled).Error
	if err != nil {
		return err
	}
	InvalidatePricingCache()
	err = UpdateAbilityStatusByTag(tag, false)
	return err
}

func EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, priority *int64, weight *uint, paramOverride *string, headerOverride *string) error {
	updateData := Channel{}
	shouldReCreateAbilities := false
	updatedTag := tag
	// 如果 newTag 不为空且不等于 tag，则更新 tag
	if newTag != nil && *newTag != tag {
		updateData.Tag = newTag
		updatedTag = *newTag
	}
	if modelMapping != nil {
		updateData.ModelMapping = modelMapping
	}
	if models != nil && *models != "" {
		shouldReCreateAbilities = true
		updateData.Models = *models
	}
	if group != nil && *group != "" {
		shouldReCreateAbilities = true
		updateData.Group = *group
	}
	if priority != nil {
		updateData.Priority = priority
	}
	if weight != nil {
		updateData.Weight = weight
	}
	if paramOverride != nil {
		updateData.ParamOverride = paramOverride
	}
	if headerOverride != nil {
		updateData.HeaderOverride = headerOverride
	}

	err := DB.Model(&Channel{}).Where("tag = ?", tag).Updates(updateData).Error
	if err != nil {
		return err
	}
	if shouldReCreateAbilities {
		channels, err := GetChannelsByTag(updatedTag, false, false)
		if err == nil {
			for _, channel := range channels {
				err = channel.UpdateAbilities(nil)
				if err != nil {
					common.SysLog(fmt.Sprintf("failed to update abilities: channel_id=%d, tag=%s, error=%v", channel.Id, channel.GetTag(), err))
				}
			}
		}
	} else {
		err := UpdateAbilityByTag(tag, newTag, priority, weight)
		if err != nil {
			return err
		}
	}
	return nil
}

func UpdateChannelUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, quota)
		return
	}
	updateChannelUsedQuota(id, quota)
}

func updateChannelUsedQuota(id int, quota int) {
	err := DB.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update channel used quota: channel_id=%d, delta_quota=%d, error=%v", id, quota, err))
	}
}

func DeleteChannelByStatus(status int64) (int64, error) {
	return deleteChannels(func(tx *gorm.DB) ([]int, error) {
		var channelIds []int
		query := lockForUpdate(tx.Model(&Channel{})).
			Where("status = ?", status).
			Order("id ASC")
		if err := query.Pluck("id", &channelIds).Error; err != nil {
			return nil, err
		}
		return channelIds, nil
	})
}

func DeleteDisabledChannel() (int64, error) {
	return deleteChannels(func(tx *gorm.DB) ([]int, error) {
		var channelIds []int
		query := lockForUpdate(tx.Model(&Channel{})).
			Where("status IN ?", []int{common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled}).
			Order("id ASC")
		if err := query.Pluck("id", &channelIds).Error; err != nil {
			return nil, err
		}
		return channelIds, nil
	})
}

func GetPaginatedTags(offset int, limit int) ([]*string, error) {
	return GetPaginatedChannelTags(DB.Model(&Channel{}), offset, limit)
}

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	var tags []*string
	err := query.
		Select("DISTINCT tag").
		Where("tag is not null AND tag != ''").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tag"}}).
		Offset(offset).
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	var tags []*string
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := "priority desc"
	if idSort {
		order = "id desc"
	}

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	subQuery := baseQuery.
		Select("tag").
		Where("tag != ''").
		Order(order)

	err := DB.Table("(?) as sub", subQuery).
		Select("DISTINCT tag").
		Find(&tags).Error

	if err != nil {
		return nil, err
	}

	return tags, nil
}

func (channel *Channel) ValidateSettings() error {
	channelParams := &dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), channelParams)
		if err != nil {
			return err
		}
	}
	if _, err := common.ParseProxyURLStrict(channelParams.Proxy); err != nil {
		return fmt.Errorf("invalid channel proxy: %w", err)
	}
	if err := channelParams.ValidateHTTPTransport(); err != nil {
		return err
	}
	channelOtherSettings := &dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, channelOtherSettings)
		if err != nil {
			return err
		}
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if channelOtherSettings.AdvancedCustom == nil {
			return fmt.Errorf("advanced_custom is required")
		}
	}
	if channelOtherSettings.AdvancedCustom != nil {
		if err := channelOtherSettings.AdvancedCustom.Validate(); err != nil {
			return err
		}
	}
	if err := channelOtherSettings.ProtocolCapabilities.Validate(); err != nil {
		return err
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom && channelOtherSettings.UpstreamModelUpdateCheckEnabled {
		if _, ok := channelOtherSettings.AdvancedCustom.ModelListRoute(); !ok {
			return fmt.Errorf("advanced custom channels require a %s route when upstream model update checks are enabled", dto.AdvancedCustomModelListPath)
		}
	}
	if err := operation_setting.ValidateClientAccessPolicy(channelOtherSettings.ClientPolicy); err != nil {
		return fmt.Errorf("invalid channel client policy: %w", err)
	}
	return nil
}

func (channel *Channel) GetSetting() dto.ChannelSettings {
	setting := dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
			channel.Setting = nil // 清空设置以避免后续错误
			_ = channel.Save()    // 保存修改
		}
	}
	return setting
}

func (channel *Channel) SetSetting(setting dto.ChannelSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.Setting = common.GetPointer[string](string(settingBytes))
}

func (channel *Channel) GetOtherSettings() dto.ChannelOtherSettings {
	setting := dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
			channel.OtherSettings = "{}" // 清空设置以避免后续错误
			_ = channel.Save()           // 保存修改
		}
	}
	return setting
}

func (channel *Channel) SetOtherSettings(setting dto.ChannelOtherSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.OtherSettings = string(settingBytes)
}

func (channel *Channel) GetParamOverride() map[string]interface{} {
	paramOverride := make(map[string]interface{})
	if channel.ParamOverride != nil && *channel.ParamOverride != "" {
		err := common.Unmarshal([]byte(*channel.ParamOverride), &paramOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal param override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return paramOverride
}

func (channel *Channel) GetHeaderOverride() map[string]interface{} {
	headerOverride := make(map[string]interface{})
	if channel.HeaderOverride != nil && *channel.HeaderOverride != "" {
		err := common.Unmarshal([]byte(*channel.HeaderOverride), &headerOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal header override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return headerOverride
}

func GetChannelsByIds(ids []int) ([]*Channel, error) {
	var channels []*Channel
	err := DB.Where("id in (?)", ids).Find(&channels).Error
	return channels, err
}

func BatchSetChannelTag(ids []int, tag *string) error {
	// 开启事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// 更新标签
	err := tx.Model(&Channel{}).Where("id in (?)", ids).Update("tag", tag).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	// update ability status
	channels, err := GetChannelsByIds(ids)
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, channel := range channels {
		err = channel.UpdateAbilities(tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 提交事务
	return tx.Commit().Error
}

// CountAllChannels returns total channels in DB
func CountAllChannels() (int64, error) {
	var total int64
	err := DB.Model(&Channel{}).Count(&total).Error
	return total, err
}

// CountAllTags returns number of non-empty distinct tags
func CountAllTags() (int64, error) {
	return CountChannelTags(DB.Model(&Channel{}))
}

func CountChannelTags(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}

// Get channels of specified type with pagination
func GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	var channels []*Channel
	order := "priority desc"
	if idSort {
		order = "id desc"
	}
	err := DB.Where("type = ?", channelType).Order(order).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	return channels, err
}

// Count channels of specific type
func CountChannelsByType(channelType int) (int64, error) {
	var count int64
	err := DB.Model(&Channel{}).Where("type = ?", channelType).Count(&count).Error
	return count, err
}

// Return map[type]count for all channels
func CountChannelsGroupByType() (map[int64]int64, error) {
	type result struct {
		Type  int64 `gorm:"column:type"`
		Count int64 `gorm:"column:count"`
	}
	var results []result
	err := DB.Model(&Channel{}).Select("type, count(*) as count").Group("type").Find(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64)
	for _, r := range results {
		counts[r.Type] = r.Count
	}
	return counts, nil
}
