package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

type RetryParam struct {
	Ctx               *gin.Context
	TokenGroup        string
	ModelName         string
	RequestPath       string
	AllowedChannelIds []int
	CandidateFilter   model.ChannelCandidateFilter
	Retry             *int
	resetNextTry      bool
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *RetryParam) ResetRetryNextTry() {
	p.resetNextTry = true
}

// CacheGetRandomSatisfiedChannel preserves priority, weight, auto-group and
// cross-group retry behavior while optionally restricting candidates to a
// strict administrator-selected channel pool.
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	if param == nil || param.Ctx == nil {
		return nil, "", errors.New("invalid channel selection parameters")
	}
	selectGroup := param.TokenGroup
	client := common.GetContextKeyString(param.Ctx, constant.ContextKeyClientName)
	if client == "" {
		client = clientpolicy.Detect(param.Ctx.Request)
		common.SetContextKey(param.Ctx, constant.ContextKeyClientName, client)
	}
	if param.TokenGroup != "auto" {
		if !clientpolicy.IsGroupAllowed(param.TokenGroup, client) {
			return nil, selectGroup, nil
		}
		channel, err := model.GetRandomSatisfiedChannelInPoolWithFilter(
			param.TokenGroup,
			param.ModelName,
			param.GetRetry(),
			param.RequestPath,
			param.AllowedChannelIds,
			param.CandidateFilter,
		)
		return channel, selectGroup, err
	}

	if len(setting.GetAutoGroups()) == 0 {
		return nil, selectGroup, errors.New("auto groups is not enabled")
	}
	userGroups := common.GetContextKeyStringSlice(param.Ctx, constant.ContextKeyUserGroups)
	if len(userGroups) == 0 {
		userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)
		if userGroup != "" {
			userGroups = []string{userGroup}
		}
	}
	autoGroups := GetUserAutoGroups(userGroups)
	if len(autoGroups) == 0 {
		return nil, selectGroup, errors.New("no usable auto groups")
	}

	startGroupIndex := 0
	crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)
	if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
		if index, ok := lastGroupIndex.(int); ok {
			startGroupIndex = index
		}
	}

	for index := startGroupIndex; index < len(autoGroups); index++ {
		autoGroup := autoGroups[index]
		if !clientpolicy.IsGroupAllowed(autoGroup, client) {
			continue
		}
		priorityRetry := param.GetRetry()
		if index > startGroupIndex {
			priorityRetry = 0
		}
		logger.LogDebug(param.Ctx, "Auto selecting group: %s, priorityRetry: %d", autoGroup, priorityRetry)

		channel, err := model.GetRandomSatisfiedChannelInPoolWithFilter(
			autoGroup,
			param.ModelName,
			priorityRetry,
			param.RequestPath,
			param.AllowedChannelIds,
			param.CandidateFilter,
		)
		if err != nil {
			return nil, autoGroup, err
		}
		if channel == nil {
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
			param.SetRetry(0)
			continue
		}

		common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
		selectGroup = autoGroup
		if crossGroupRetry && priorityRetry >= common.RetryTimes {
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
			param.SetRetry(0)
			param.ResetRetryNextTry()
		} else {
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index)
		}
		return channel, selectGroup, nil
	}

	return nil, selectGroup, nil
}
