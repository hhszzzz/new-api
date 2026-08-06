package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/gin-gonic/gin"
)

type RetryParam struct {
	Ctx                 *gin.Context
	TokenGroup          string
	ModelName           string
	RequestPath         string
	AllowedChannelIds   []int
	AllowedGroups       []string
	CandidateFilter     model.ChannelCandidateFilter
	CandidateClassifier model.ChannelCandidateClassifier
	Retry               *int
	resetNextTry        bool
	excludedChannelIds  map[int]struct{}
}

func publishSelectedGroupContext(ctx *gin.Context, group string) {
	if ctx == nil || group == "" {
		return
	}
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, group)
	if common.GetContextKeyInt(ctx, constant.ContextKeyUserModelRouteId) > 0 {
		common.SetContextKey(ctx, constant.ContextKeyUserModelRouteGroup, group)
	}
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

func (p *RetryParam) ExcludeChannel(channelID int) {
	if p == nil || channelID <= 0 {
		return
	}
	if p.excludedChannelIds == nil {
		p.excludedChannelIds = make(map[int]struct{})
	}
	p.excludedChannelIds[channelID] = struct{}{}
}

func (p *RetryParam) IsChannelExcluded(channelID int) bool {
	if p == nil || channelID <= 0 {
		return false
	}
	_, excluded := p.excludedChannelIds[channelID]
	return excluded
}

func (p *RetryParam) ClearChannelExclusions() {
	if p != nil {
		p.excludedChannelIds = nil
	}
}

func (p *RetryParam) AllowsChannel(channel *model.Channel) bool {
	filter := p.effectiveCandidateFilter()
	return filter == nil || filter(channel)
}

func (p *RetryParam) effectiveCandidateFilter() model.ChannelCandidateFilter {
	if p == nil || len(p.excludedChannelIds) == 0 {
		if p == nil {
			return nil
		}
		return p.CandidateFilter
	}
	return func(channel *model.Channel) bool {
		if channel == nil {
			return false
		}
		if _, excluded := p.excludedChannelIds[channel.Id]; excluded {
			return false
		}
		return p.CandidateFilter == nil || p.CandidateFilter(channel)
	}
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
	if param.TokenGroup != "auto" && len(param.AllowedGroups) <= 1 {
		if !clientpolicy.IsGroupAllowed(param.TokenGroup, client) {
			return nil, selectGroup, nil
		}
		channel, err := model.GetRandomSatisfiedChannelInPoolWithClassifier(
			param.TokenGroup,
			param.ModelName,
			param.GetRetry(),
			param.RequestPath,
			param.AllowedChannelIds,
			param.effectiveCandidateFilter(),
			param.CandidateClassifier,
		)
		return channel, selectGroup, err
	}

	autoGroups := make([]string, 0, len(param.AllowedGroups))
	if len(param.AllowedGroups) > 0 {
		seen := make(map[string]struct{}, len(param.AllowedGroups))
		for _, group := range param.AllowedGroups {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			if _, exists := seen[group]; exists {
				continue
			}
			seen[group] = struct{}{}
			autoGroups = append(autoGroups, group)
		}
	} else {
		userGroups := common.GetContextKeyStringSlice(param.Ctx, constant.ContextKeyUserGroups)
		if len(userGroups) == 0 {
			userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)
			if userGroup != "" {
				userGroups = []string{userGroup}
			}
		}
		autoGroups = GetRequestAutoGroupsForGroups(param.Ctx, userGroups)
	}
	if len(autoGroups) == 0 {
		if len(param.AllowedGroups) > 0 {
			return nil, selectGroup, errors.New("no usable routed execution groups")
		}
		return nil, selectGroup, errors.New("no usable auto groups")
	}

	startGroupIndex := 0
	crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)
	_, hasAutoGroupSelection := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex)
	if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
		if index, ok := lastGroupIndex.(int); ok {
			startGroupIndex = index
		}
	}

	// A new auto-group request must exhaust the native protocol layer across
	// groups before an earlier group's convertible channel can win. Once a group
	// has been selected, retries continue through the existing per-group tiers.
	if !hasAutoGroupSelection && param.CandidateClassifier != nil {
		nativeClassifier := func(channel *model.Channel) model.ChannelCandidateClass {
			if param.CandidateClassifier(channel) == model.ChannelCandidateNative {
				return model.ChannelCandidateNative
			}
			return model.ChannelCandidateIncompatible
		}
		for index, autoGroup := range autoGroups {
			if !clientpolicy.IsGroupAllowed(autoGroup, client) {
				continue
			}
			priorityRetry := param.GetRetry()
			if index > 0 {
				priorityRetry = 0
			}
			channel, err := model.GetRandomSatisfiedChannelInPoolWithClassifier(
				autoGroup,
				param.ModelName,
				priorityRetry,
				param.RequestPath,
				param.AllowedChannelIds,
				param.effectiveCandidateFilter(),
				nativeClassifier,
			)
			if err != nil {
				if errors.Is(err, model.ErrNoCompatibleChannel) {
					continue
				}
				return nil, autoGroup, err
			}
			if channel == nil {
				continue
			}

			publishSelectedGroupContext(param.Ctx, autoGroup)
			if crossGroupRetry && priorityRetry >= common.RetryTimes {
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index)
			}
			return channel, autoGroup, nil
		}
	}

	incompatibleCandidateSeen := false
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

		channel, err := model.GetRandomSatisfiedChannelInPoolWithClassifier(
			autoGroup,
			param.ModelName,
			priorityRetry,
			param.RequestPath,
			param.AllowedChannelIds,
			param.effectiveCandidateFilter(),
			param.CandidateClassifier,
		)
		if err != nil {
			if errors.Is(err, model.ErrNoCompatibleChannel) {
				incompatibleCandidateSeen = true
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				param.SetRetry(0)
				continue
			}
			return nil, autoGroup, err
		}
		if channel == nil {
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
			param.SetRetry(0)
			continue
		}

		publishSelectedGroupContext(param.Ctx, autoGroup)
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

	if incompatibleCandidateSeen {
		return nil, selectGroup, model.ErrNoCompatibleChannel
	}
	return nil, selectGroup, nil
}
