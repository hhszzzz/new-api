package model

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ChannelAggregate struct {
	Id         int    `json:"id"`
	Name       string `json:"name" gorm:"type:varchar(191);not null;uniqueIndex"`
	BaseURL    string `json:"base_url" gorm:"column:base_url;type:text"`
	Remark     string `json:"remark" gorm:"type:varchar(255)"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint"`
	ChildCount int64  `json:"child_count" gorm:"-"`
}

func normalizeChannelAggregate(aggregate *ChannelAggregate) error {
	if aggregate == nil {
		return errors.New("channel aggregate is required")
	}
	aggregate.Name = strings.TrimSpace(aggregate.Name)
	aggregate.BaseURL = strings.TrimRight(strings.TrimSpace(aggregate.BaseURL), "/")
	aggregate.Remark = strings.TrimSpace(aggregate.Remark)
	if aggregate.Name == "" {
		return errors.New("channel aggregate name is required")
	}
	if len(aggregate.Name) > 191 {
		return errors.New("channel aggregate name is too long")
	}
	if len(aggregate.Remark) > 255 {
		return errors.New("channel aggregate remark is too long")
	}
	if aggregate.BaseURL != "" {
		parsed, err := url.Parse(aggregate.BaseURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
			return errors.New("channel aggregate base URL must be a valid HTTP or HTTPS URL")
		}
	}
	return nil
}

func SaveChannelAggregate(aggregate *ChannelAggregate) error {
	if err := normalizeChannelAggregate(aggregate); err != nil {
		return err
	}
	now := common.GetTimestamp()
	aggregate.UpdatedAt = now
	if aggregate.Id == 0 {
		aggregate.CreatedAt = now
		return DB.Create(aggregate).Error
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing ChannelAggregate
		if err := lockForUpdate(tx).Select("id").First(&existing, "id = ?", aggregate.Id).Error; err != nil {
			return err
		}
		return tx.Model(&ChannelAggregate{}).Where("id = ?", aggregate.Id).Updates(map[string]interface{}{
			"name":       aggregate.Name,
			"base_url":   aggregate.BaseURL,
			"remark":     aggregate.Remark,
			"updated_at": aggregate.UpdatedAt,
		}).Error
	})
}

func GetChannelAggregateById(id int) (*ChannelAggregate, error) {
	if id <= 0 {
		return nil, errors.New("invalid channel aggregate id")
	}
	aggregate := &ChannelAggregate{}
	if err := DB.First(aggregate, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&Channel{}).Where("aggregate_id = ?", id).Count(&aggregate.ChildCount).Error; err != nil {
		return nil, err
	}
	return aggregate, nil
}

func GetChannelAggregates() ([]*ChannelAggregate, error) {
	aggregates := make([]*ChannelAggregate, 0)
	if err := DB.Order("name asc, id asc").Find(&aggregates).Error; err != nil {
		return nil, err
	}
	if len(aggregates) == 0 {
		return aggregates, nil
	}
	type aggregateCount struct {
		AggregateId int
		Count       int64
	}
	counts := make([]aggregateCount, 0)
	if err := DB.Model(&Channel{}).
		Select("aggregate_id, count(*) AS count").
		Where("aggregate_id IS NOT NULL").
		Group("aggregate_id").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	countById := make(map[int]int64, len(counts))
	for _, count := range counts {
		countById[count.AggregateId] = count.Count
	}
	for _, aggregate := range aggregates {
		aggregate.ChildCount = countById[aggregate.Id]
	}
	return aggregates, nil
}

func HydrateChannelAggregateSnapshots(channels []*Channel) error {
	ids := make([]int, 0)
	seen := make(map[int]struct{})
	for _, channel := range channels {
		if channel == nil || channel.AggregateId == nil || *channel.AggregateId <= 0 {
			continue
		}
		if _, ok := seen[*channel.AggregateId]; ok {
			continue
		}
		seen[*channel.AggregateId] = struct{}{}
		ids = append(ids, *channel.AggregateId)
	}
	if len(ids) == 0 {
		return nil
	}
	aggregates := make([]ChannelAggregate, 0, len(ids))
	if err := DB.Where("id IN ?", ids).Find(&aggregates).Error; err != nil {
		return err
	}
	byId := make(map[int]ChannelAggregate, len(aggregates))
	for _, aggregate := range aggregates {
		byId[aggregate.Id] = aggregate
	}
	for _, channel := range channels {
		if channel == nil || channel.AggregateId == nil {
			continue
		}
		aggregate, ok := byId[*channel.AggregateId]
		if !ok {
			channel.AggregateName = ""
			channel.AggregateBaseURL = ""
			continue
		}
		channel.AggregateName = aggregate.Name
		channel.AggregateBaseURL = aggregate.BaseURL
	}
	return nil
}

func ValidateChannelAggregateLink(channel *Channel) error {
	return validateChannelAggregateLinkWithTx(DB, channel, false)
}

func validateChannelAggregateLinkWithTx(tx *gorm.DB, channel *Channel, lock bool) error {
	if channel == nil {
		return errors.New("channel is required")
	}
	if channel.AggregateId == nil {
		if channel.InheritAggregateBaseURL {
			return errors.New("cannot inherit an aggregate base URL without an aggregate")
		}
		channel.AggregateName = ""
		channel.AggregateBaseURL = ""
		return nil
	}
	if *channel.AggregateId <= 0 {
		return errors.New("invalid channel aggregate id")
	}
	aggregate := &ChannelAggregate{}
	query := tx.Select("id", "name", "base_url")
	if lock {
		query = lockForUpdate(query)
	}
	err := query.First(aggregate, "id = ?", *channel.AggregateId).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("channel aggregate %d does not exist", *channel.AggregateId)
		}
		return err
	}
	channel.AggregateName = aggregate.Name
	channel.AggregateBaseURL = aggregate.BaseURL
	return nil
}

func UpdateChannelAggregateLink(channelId int, aggregateId *int, inheritBaseURL bool) error {
	if channelId <= 0 {
		return errors.New("invalid channel id")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		channel := &Channel{Id: channelId, AggregateId: aggregateId, InheritAggregateBaseURL: inheritBaseURL}
		// Lock the aggregate before the channel, matching DeleteChannelAggregate.
		// This makes link and delete operations serialize without a lock-order cycle.
		if err := validateChannelAggregateLinkWithTx(tx, channel, true); err != nil {
			return err
		}
		var existing Channel
		if err := lockForUpdate(tx).Select("id").First(&existing, "id = ?", channelId).Error; err != nil {
			return err
		}
		return tx.Model(&Channel{}).Where("id = ?", channelId).Updates(map[string]interface{}{
			"aggregate_id":               aggregateId,
			"inherit_aggregate_base_url": inheritBaseURL,
		}).Error
	})
}

func DeleteChannelAggregate(id int) error {
	if id <= 0 {
		return errors.New("invalid channel aggregate id")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		aggregate := &ChannelAggregate{}
		if err := lockForUpdate(tx).First(aggregate, "id = ?", id).Error; err != nil {
			return err
		}
		children := make([]Channel, 0)
		if err := lockForUpdate(tx).Where("aggregate_id = ?", id).Find(&children).Error; err != nil {
			return err
		}
		for _, child := range children {
			updates := map[string]interface{}{
				"aggregate_id":               nil,
				"inherit_aggregate_base_url": false,
			}
			if child.InheritAggregateBaseURL && aggregate.BaseURL != "" {
				updates["base_url"] = aggregate.BaseURL
			}
			if err := tx.Model(&Channel{}).Where("id = ?", child.Id).Updates(updates).Error; err != nil {
				return err
			}
		}
		return tx.Delete(aggregate).Error
	})
}
