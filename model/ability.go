package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

type ChannelCandidateFilter func(channel *Channel) bool

type ChannelCandidateClass int

const (
	ChannelCandidateIncompatible ChannelCandidateClass = iota
	ChannelCandidateConvertible
	ChannelCandidateNative
)

type ChannelCandidateClassifier func(channel *Channel) ChannelCandidateClass

var ErrNoCompatibleChannel = errors.New("no channel supports the requested protocol or request features")

type channelSelectionTier struct {
	Class    ChannelCandidateClass
	Priority int64
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.enabled = ? AND channels.status = ?", true, common.ChannelStatusEnabled).
		Scan(&abilities).Error
	return abilities, err
}

func GetGroupEnabledModels(group string) []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where(commonGroupCol+" = ? and enabled = ?", group, true).Distinct("model").Pluck("model", &models)
	return models
}

func GetEnabledModels() []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	DB.Find(&abilities, "enabled = ?", true)
	return abilities
}

func getPriority(group string, model string, retry int, channelIds []int) (int, error) {
	var priorities []int
	query := DB.Model(&Ability{}).
		Select("DISTINCT(priority)").
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true)
	if channelIds != nil {
		query = query.Where("channel_id IN ?", channelIds)
	}
	if err := query.Order("priority DESC").Pluck("priority", &priorities).Error; err != nil {
		return 0, err
	}
	if len(priorities) == 0 {
		return 0, errors.New("数据库一致性被破坏")
	}
	if retry >= len(priorities) {
		return priorities[len(priorities)-1], nil
	}
	return priorities[retry], nil
}

func getChannelQuery(group string, model string, retry int, channelIds []int) (*gorm.DB, error) {
	baseQuery := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true)
	if channelIds != nil {
		baseQuery = baseQuery.Where("channel_id IN ?", channelIds)
	}
	if retry == 0 {
		maxPrioritySubQuery := baseQuery.Session(&gorm.Session{}).Select("MAX(priority)")
		return baseQuery.Where("priority = (?)", maxPrioritySubQuery), nil
	}
	priority, err := getPriority(group, model, retry, channelIds)
	if err != nil {
		return nil, err
	}
	return baseQuery.Where("priority = ?", priority), nil
}

func GetChannel(group string, model string, retry int, requestPath string) (*Channel, error) {
	return GetChannelInPool(group, model, retry, requestPath, nil)
}

func GetChannelInPool(group string, model string, retry int, requestPath string, channelIds []int) (*Channel, error) {
	return GetChannelInPoolWithFilter(group, model, retry, requestPath, channelIds, nil)
}

func GetChannelInPoolWithFilter(group string, modelName string, retry int, requestPath string, channelIds []int, candidateFilter ChannelCandidateFilter) (*Channel, error) {
	return GetChannelInPoolWithClassifier(group, modelName, retry, requestPath, channelIds, candidateFilter, nil)
}

func GetChannelInPoolWithClassifier(group string, modelName string, retry int, requestPath string, channelIds []int, candidateFilter ChannelCandidateFilter, candidateClassifier ChannelCandidateClassifier) (*Channel, error) {
	abilities, channels, err := findEligibleChannelAbilities(group, modelName, modelName, requestPath, channelIds, candidateFilter)
	if err != nil {
		return nil, err
	}
	if len(abilities) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(modelName)
		if normalizedModel != "" && normalizedModel != modelName {
			abilities, channels, err = findEligibleChannelAbilities(group, normalizedModel, modelName, requestPath, channelIds, candidateFilter)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(abilities) == 0 {
		return nil, nil
	}

	eligibleAbilities := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		channel := channels[ability.ChannelId]
		if channel == nil {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", ability.ChannelId)
		}
		if classifyChannel(channel, candidateClassifier) == ChannelCandidateIncompatible {
			continue
		}
		eligibleAbilities = append(eligibleAbilities, ability)
	}
	if len(eligibleAbilities) == 0 {
		if candidateClassifier != nil {
			return nil, ErrNoCompatibleChannel
		}
		return nil, nil
	}

	tiers := buildAbilitySelectionTiers(eligibleAbilities, channels, candidateClassifier)
	if retry >= len(tiers) {
		retry = len(tiers) - 1
	}
	if retry < 0 {
		retry = 0
	}
	targetTier := tiers[retry]
	targetAbilities := make([]Ability, 0, len(eligibleAbilities))
	for _, ability := range eligibleAbilities {
		priority := int64(0)
		if ability.Priority != nil {
			priority = *ability.Priority
		}
		channel := channels[ability.ChannelId]
		if priority == targetTier.Priority && classifyChannel(channel, candidateClassifier) == targetTier.Class {
			targetAbilities = append(targetAbilities, ability)
		}
	}
	if len(targetAbilities) == 0 {
		return nil, nil
	}

	targetChannels := make([]*Channel, 0, len(targetAbilities))
	for _, ability := range targetAbilities {
		channel := channels[ability.ChannelId]
		if channel == nil {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", ability.ChannelId)
		}
		targetChannels = append(targetChannels, channel)
	}
	channel := selectWeightedChannel(targetChannels)
	if channel == nil {
		return nil, errors.New("channel not found after weighted selection")
	}
	return channel, nil
}

func classifyChannel(channel *Channel, classifier ChannelCandidateClassifier) ChannelCandidateClass {
	if channel == nil {
		return ChannelCandidateIncompatible
	}
	if classifier == nil {
		return ChannelCandidateNative
	}
	class := classifier(channel)
	if class != ChannelCandidateNative && class != ChannelCandidateConvertible {
		return ChannelCandidateIncompatible
	}
	return class
}

func buildChannelSelectionTiers(channels []*Channel, classifier ChannelCandidateClassifier) []channelSelectionTier {
	prioritiesByClass := map[ChannelCandidateClass]map[int64]struct{}{
		ChannelCandidateNative:      {},
		ChannelCandidateConvertible: {},
	}
	for _, channel := range channels {
		class := classifyChannel(channel, classifier)
		if class == ChannelCandidateIncompatible {
			continue
		}
		prioritiesByClass[class][channel.GetPriority()] = struct{}{}
	}

	tiers := make([]channelSelectionTier, 0)
	for _, class := range []ChannelCandidateClass{ChannelCandidateNative, ChannelCandidateConvertible} {
		priorities := make([]int64, 0, len(prioritiesByClass[class]))
		for priority := range prioritiesByClass[class] {
			priorities = append(priorities, priority)
		}
		sort.Slice(priorities, func(i, j int) bool {
			return priorities[i] > priorities[j]
		})
		for _, priority := range priorities {
			tiers = append(tiers, channelSelectionTier{Class: class, Priority: priority})
		}
	}
	return tiers
}

func buildAbilitySelectionTiers(abilities []Ability, channels map[int]*Channel, classifier ChannelCandidateClassifier) []channelSelectionTier {
	prioritiesByClass := map[ChannelCandidateClass]map[int64]struct{}{
		ChannelCandidateNative:      {},
		ChannelCandidateConvertible: {},
	}
	for _, ability := range abilities {
		class := classifyChannel(channels[ability.ChannelId], classifier)
		if class == ChannelCandidateIncompatible {
			continue
		}
		priority := int64(0)
		if ability.Priority != nil {
			priority = *ability.Priority
		}
		prioritiesByClass[class][priority] = struct{}{}
	}

	tiers := make([]channelSelectionTier, 0)
	for _, class := range []ChannelCandidateClass{ChannelCandidateNative, ChannelCandidateConvertible} {
		priorities := make([]int64, 0, len(prioritiesByClass[class]))
		for priority := range prioritiesByClass[class] {
			priorities = append(priorities, priority)
		}
		sort.Slice(priorities, func(i, j int) bool {
			return priorities[i] > priorities[j]
		})
		for _, priority := range priorities {
			tiers = append(tiers, channelSelectionTier{Class: class, Priority: priority})
		}
	}
	return tiers
}

// findEligibleChannelAbilities loads all priorities first, then removes
// channels that cannot serve this request. Priority and weight selection must
// only run over the remaining legal candidates.
func findEligibleChannelAbilities(group, abilityModel, requestModel, requestPath string, channelIds []int, candidateFilter ChannelCandidateFilter) ([]Ability, map[int]*Channel, error) {
	query := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, abilityModel, true)
	if channelIds != nil {
		query = query.Where("channel_id IN ?", channelIds)
	}
	abilities := make([]Ability, 0)
	if err := query.Order("priority DESC").Order("weight DESC").Find(&abilities).Error; err != nil {
		return nil, nil, err
	}
	if len(abilities) == 0 {
		return abilities, map[int]*Channel{}, nil
	}

	ids := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		ids = append(ids, ability.ChannelId)
	}
	channelRows := make([]*Channel, 0, len(ids))
	if err := DB.Where("id IN ? AND status = ?", ids, common.ChannelStatusEnabled).Find(&channelRows).Error; err != nil {
		return nil, nil, err
	}
	if err := HydrateChannelAggregateSnapshots(channelRows); err != nil {
		return nil, nil, err
	}
	channels := make(map[int]*Channel, len(channelRows))
	for _, channel := range channelRows {
		if !channel.IsSchedulableAt(time.Now()) {
			continue
		}
		channels[channel.Id] = channel
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		channel := channels[ability.ChannelId]
		if channel == nil {
			continue
		}
		if requestPath != "" && channel.Type == constant.ChannelTypeAdvancedCustom {
			config := channel.GetOtherSettings().AdvancedCustom
			if config == nil || !config.SupportsPathForModel(requestPath, requestModel) {
				continue
			}
		}
		if candidateFilter != nil && !candidateFilter(channel) {
			continue
		}
		filtered = append(filtered, ability)
	}
	return filtered, channels, nil
}

// filterAbilitiesByRequestPathAndModel restricts candidates by request path and
// model for the DB (non-memory-cache) selection path. Only Advanced Custom
// (type 58) channels are path-checked: kept only when one of their routes matches
// requestPath and model; all other channel types always pass. When requestPath is
// empty, filtering is skipped.
func filterAbilitiesByRequestPathAndModel(abilities []Ability, requestPath string, model string) []Ability {
	if requestPath == "" || len(abilities) == 0 {
		return abilities
	}

	channelIds := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []*Channel
	if err := DB.Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		// On error, fall back to unfiltered candidates to avoid blocking selection
		return abilities
	}

	advancedConfigs := make(map[int]*dto.AdvancedCustomConfig)
	for _, channel := range channels {
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			advancedConfigs[channel.Id] = channel.GetOtherSettings().AdvancedCustom
		}
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		config, isAdvancedCustom := advancedConfigs[ability.ChannelId]
		if !isAdvancedCustom {
			filtered = append(filtered, ability)
			continue
		}
		if config != nil && config.SupportsPathForModel(requestPath, model) {
			filtered = append(filtered, ability)
		}
	}
	return filtered
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    channel.GetWeight(),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
	}

	// First delete all abilities of this channel
	err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    channel.GetWeight(),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		if err := tx.Commit().Error; err != nil {
			return err
		}
		InvalidatePricingCache()
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	err := DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
	if err != nil {
		return err
	}
	InvalidatePricingCache()
	return nil
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	err := DB.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error
	if err != nil {
		return err
	}
	InvalidatePricingCache()
	return nil
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	ability := Ability{}
	if newTag != nil {
		ability.Tag = newTag
	}
	if priority != nil {
		ability.Priority = priority
	}
	if weight != nil {
		ability.Weight = *weight
	}
	return DB.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	// truncate abilities table
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		err := DB.Exec("DELETE FROM abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	} else {
		err := DB.Exec("TRUNCATE TABLE abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Truncate abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	}
	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	if len(channels) == 0 {
		return 0, 0, nil
	}
	successCount := 0
	failCount := 0
	for _, chunk := range lo.Chunk(channels, 50) {
		ids := lo.Map(chunk, func(c *Channel, _ int) int { return c.Id })
		// Delete all abilities of this channel
		err = DB.Where("channel_id IN ?", ids).Delete(&Ability{}).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			failCount += len(chunk)
			continue
		}
		// Then add new abilities
		for _, channel := range chunk {
			err = channel.AddAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("Add abilities for channel %d failed: %s", channel.Id, err.Error()))
				failCount++
			} else {
				successCount++
			}
		}
	}
	InitChannelCache()
	return successCount, failCount, nil
}
