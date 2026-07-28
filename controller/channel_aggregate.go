package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetChannelAggregates(c *gin.Context) {
	aggregates, err := model.GetChannelAggregates()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, aggregates)
}

func CreateChannelAggregate(c *gin.Context) {
	aggregate := &model.ChannelAggregate{}
	if err := common.DecodeJson(c.Request.Body, aggregate); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	aggregate.Id = 0
	if err := model.SaveChannelAggregate(aggregate); err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.aggregate.create", map[string]interface{}{
		"id":   aggregate.Id,
		"name": aggregate.Name,
	})
	common.ApiSuccess(c, aggregate)
}

func UpdateChannelAggregate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	aggregate := &model.ChannelAggregate{}
	if err := common.DecodeJson(c.Request.Body, aggregate); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	aggregate.Id = id
	if err := model.SaveChannelAggregate(aggregate); err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	service.ResetProxyClientCache()
	recordManageAudit(c, "channel.aggregate.update", map[string]interface{}{
		"id":   aggregate.Id,
		"name": aggregate.Name,
	})
	common.ApiSuccess(c, aggregate)
}

type mergeChannelsIntoAggregateRequest struct {
	IDs                     []int                   `json:"ids"`
	AggregateID             *int                    `json:"aggregate_id"`
	NewAggregate            *model.ChannelAggregate `json:"new_aggregate"`
	InheritAggregateBaseURL bool                    `json:"inherit_aggregate_base_url"`
}

func MergeChannelsIntoAggregate(c *gin.Context) {
	request := mergeChannelsIntoAggregateRequest{}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	aggregate, updated, err := model.MergeChannelsIntoAggregate(
		request.IDs,
		request.AggregateID,
		request.NewAggregate,
		request.InheritAggregateBaseURL,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	model.InitChannelCache()
	service.ResetProxyClientCache()
	recordManageAudit(c, "channel.aggregate.merge", map[string]interface{}{
		"aggregate_id":               aggregate.Id,
		"aggregate_name":             aggregate.Name,
		"count":                      updated,
		"created":                    request.NewAggregate != nil,
		"inherit_aggregate_base_url": request.InheritAggregateBaseURL,
	})
	common.ApiSuccess(c, gin.H{
		"aggregate": aggregate,
		"updated":   updated,
	})
}

type detachChannelsFromAggregatesRequest struct {
	IDs []int `json:"ids"`
}

func DetachChannelsFromAggregates(c *gin.Context) {
	request := detachChannelsFromAggregatesRequest{}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	updated, err := model.DetachChannelsFromAggregates(request.IDs)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	model.InitChannelCache()
	service.ResetProxyClientCache()
	recordManageAudit(c, "channel.aggregate.detach", map[string]interface{}{
		"count": updated,
	})
	common.ApiSuccess(c, gin.H{"updated": updated})
}

func DeleteChannelAggregate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	aggregate, err := model.GetChannelAggregateById(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteChannelAggregate(id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	service.ResetProxyClientCache()
	recordManageAudit(c, "channel.aggregate.delete", map[string]interface{}{
		"id":             id,
		"name":           aggregate.Name,
		"detached_count": aggregate.ChildCount,
	})
	common.ApiSuccess(c, nil)
}
