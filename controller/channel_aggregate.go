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
