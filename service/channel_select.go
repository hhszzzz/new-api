package service

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service/clientpolicy"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func GetChannelConstraints(c *gin.Context) *dto.ChannelConstraints {
	if c == nil {
		return &dto.ChannelConstraints{}
	}
	if existing, ok := common.GetContextKeyType[*dto.ChannelConstraints](c, constant.ContextKeyChannelConstraints); ok && existing != nil {
		return existing
	}
	constraints := &dto.ChannelConstraints{}
	common.SetContextKey(c, constant.ContextKeyChannelConstraints, constraints)
	return constraints
}

func AppendTaskPluginIdentityFilter(c *gin.Context, pluginKey string) {
	if c == nil {
		return
	}
	channelTypes, pluginKeys := pinnedTaskPluginIdentities(c, pluginKey)
	GetChannelConstraints(c).AddFilter(dto.ChannelFilter{
		Kind:                   dto.FilterTaskPluginIdentity,
		TaskPluginKey:          pluginKey,
		TaskPluginChannelTypes: channelTypes,
		TaskPluginKeys:         pluginKeys,
	})
}

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
	if channel == nil {
		return false
	}
	if p != nil {
		if len(p.AllowedChannelIds) > 0 && !slices.Contains(p.AllowedChannelIds, channel.Id) {
			return false
		}
		if p.CandidateClassifier != nil {
			class := p.CandidateClassifier(channel)
			if class != model.ChannelCandidateNative && class != model.ChannelCandidateConvertible {
				return false
			}
		}
	}
	filter := p.effectiveCandidateFilter()
	return filter == nil || filter(channel)
}

func (p *RetryParam) effectiveCandidateFilter() model.ChannelCandidateFilter {
	if p == nil {
		return nil
	}
	filters := GetChannelConstraints(p.Ctx).Filters
	if len(p.excludedChannelIds) == 0 && p.CandidateFilter == nil && len(filters) == 0 {
		return nil
	}
	return func(channel *model.Channel) bool {
		if channel == nil {
			return false
		}
		if _, excluded := p.excludedChannelIds[channel.Id]; excluded {
			return false
		}
		if p.CandidateFilter != nil && !p.CandidateFilter(channel) {
			return false
		}
		allowed, _ := model.ChannelSatisfiesFilters(channel, p.ModelName, filters)
		return allowed
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
	if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
		if index, ok := lastGroupIndex.(int); ok {
			startGroupIndex = index
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

func pinnedTaskPluginIdentities(c *gin.Context, expected string) ([]int, []string) {
	if c == nil || expected == "" {
		return nil, nil
	}
	if value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint); exists {
		pinned, ok := value.(jsplugin.PinnedEndpoint)
		if ok && pinned.Generation != nil && len(pinned.Candidates) > 1 {
			expectedFound := false
			channelTypes := make([]int, 0, len(pinned.Candidates))
			pluginKeys := make([]string, 0, len(pinned.Candidates))
			seen := make(map[int]struct{}, len(pinned.Candidates))
			for _, candidate := range pinned.Candidates {
				if candidate.Plugin == nil {
					continue
				}
				if candidate.Plugin.Meta.Key == expected {
					expectedFound = true
				}
				pluginKeys = append(pluginKeys, candidate.Plugin.Meta.Key)
				for _, channelType := range candidate.Plugin.Meta.ChannelTypes {
					if channelType == 0 || channelType == constant.ChannelTypeTaskPlugin {
						continue
					}
					if _, duplicate := seen[channelType]; duplicate {
						continue
					}
					if plugin, indexed := pinned.Generation.GetByChannelType(channelType); indexed && plugin == candidate.Plugin {
						seen[channelType] = struct{}{}
						channelTypes = append(channelTypes, channelType)
					}
				}
			}
			if expectedFound {
				return channelTypes, pluginKeys
			}
		}
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinned, ok := value.(jsplugin.PinnedPlugin)
	if !exists || !ok || pinned.Generation == nil || pinned.Plugin == nil || pinned.Plugin.Meta.Key != expected {
		return nil, nil
	}
	channelTypes := make([]int, 0, len(pinned.Plugin.Meta.ChannelTypes))
	for _, channelType := range pinned.Plugin.Meta.ChannelTypes {
		if channelType == 0 || channelType == constant.ChannelTypeTaskPlugin {
			continue
		}
		channelTypes = append(channelTypes, channelType)
	}
	return channelTypes, []string{expected}
}

// ChannelSelectError explains why SelectChannelForRequest found no channel.
// Callers render it for their transport: the HTTP distributor localizes
// MessageID with its own helpers and the Responses WebSocket relay wraps it in
// a NewAPIError. Message is set instead of MessageID when the text is a fixed
// error code that clients match on.
type ChannelSelectError struct {
	StatusCode int
	Code       types.ErrorCode
	MessageID  string
	Params     map[string]any
	Message    string
	// FilterKind and Channel identify a candidate rejected by request filters.
	FilterKind dto.ChannelFilterKind
	Channel    *model.Channel
	// NoAvailableChannel marks the "no channel for this group and model"
	// outcome so the distributor can name the claiming task plugin.
	NoAvailableChannel bool
}

// SelectChannelForRequest resolves the channel for one attempt with the rules
// shared by the HTTP distributor and the Responses WebSocket relay: a pinned
// channel wins, then session affinity (first attempt only), then a random
// eligible channel; every candidate must satisfy the request's channel
// filters. The group the channel was chosen from is returned for auto-group
// callers. The caller still applies SetupContextForSelectedChannel.
func SelectChannelForRequest(c *gin.Context, modelName string, retry *RetryParam) (*model.Channel, string, *ChannelSelectError) {
	constraints := GetChannelConstraints(c)
	if pin, found, overridden := constraints.ResolvedPin(); found {
		for _, lost := range overridden {
			logger.LogWarn(c, fmt.Sprintf(
				"channel pin overridden: winning_source=%s winning_channel_id=%d overridden_source=%s overridden_channel_id=%d",
				pin.Source, pin.ChannelId, lost.Source, lost.ChannelId,
			))
		}
		channel, err := model.CacheGetChannel(pin.ChannelId)
		if err != nil {
			return nil, "", pinnedChannelUnavailable(pin, http.StatusBadRequest, i18n.MsgDistributorInvalidChannelId)
		}
		if !channel.IsSchedulableAt(time.Now()) {
			return nil, "", pinnedChannelUnavailable(pin, http.StatusForbidden, i18n.MsgDistributorChannelDisabled)
		}
		if ok, kind := model.ChannelSatisfiesFilters(channel, modelName, constraints.Filters); !ok {
			return nil, "", &ChannelSelectError{
				StatusCode: http.StatusBadRequest, Code: types.ErrorCode(kind), MessageID: i18n.MsgDistributorNoAvailableChannel,
				Params:     map[string]any{"Group": common.GetContextKeyString(c, constant.ContextKeyUsingGroup), "Model": modelName},
				FilterKind: kind, Channel: channel,
			}
		}
		if !retry.AllowsChannel(channel) {
			return nil, "", &ChannelSelectError{StatusCode: http.StatusForbidden, Code: types.ErrorCodeAccessDenied, Message: "specified channel is not allowed for this request"}
		}
		return channel, "", nil
	}

	usingGroup := retry.TokenGroup
	var channel *model.Channel
	var selectGroup string
	if retry.GetRetry() == 0 {
		if preferredChannelID, found := GetPreferredChannelByAffinity(c, modelName, usingGroup); found {
			affinityUsable := false
			preferred, err := model.CacheGetChannel(preferredChannelID)
			affinitySatisfied := false
			if err == nil && preferred != nil && preferred.IsSchedulableAt(time.Now()) && retry.AllowsChannel(preferred) {
				affinitySatisfied, _ = model.ChannelSatisfiesFilters(preferred, modelName, constraints.Filters)
			}
			if affinitySatisfied {
				if usingGroup == "auto" {
					groups := retry.AllowedGroups
					if len(groups) == 0 {
						userGroups := common.GetContextKeyStringSlice(c, constant.ContextKeyUserGroups)
						if len(userGroups) == 0 {
							userGroups = []string{common.GetContextKeyString(c, constant.ContextKeyUserGroup)}
						}
						groups = GetRequestAutoGroupsForGroups(c, userGroups)
					}
					for _, g := range groups {
						if clientpolicy.IsGroupAllowed(g, common.GetContextKeyString(c, constant.ContextKeyClientName)) && model.IsChannelEnabledForGroupModel(g, modelName, preferred.Id) {
							selectGroup = g
							publishSelectedGroupContext(c, g)
							channel = preferred
							affinityUsable = true
							MarkChannelAffinityUsed(c, g, preferred.Id)
							break
						}
					}
				} else if clientpolicy.IsGroupAllowed(usingGroup, common.GetContextKeyString(c, constant.ContextKeyClientName)) && model.IsChannelEnabledForGroupModel(usingGroup, modelName, preferred.Id) {
					channel = preferred
					selectGroup = usingGroup
					affinityUsable = true
					MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
				}
			}
			if !affinityUsable && (!retry.AllowsChannel(preferred) || !ShouldKeepChannelAffinityOnChannelDisabled()) {
				ClearCurrentChannelAffinityCache(c)
			}
			if !affinityUsable && RequestPolicy(c).SessionMode == "strict" {
				return nil, "", &ChannelSelectError{StatusCode: http.StatusServiceUnavailable, Message: "strict_session_binding_unavailable"}
			}
		}
	}

	if channel == nil {
		var err error
		channel, selectGroup, err = CacheGetRandomSatisfiedChannel(retry)
		if err != nil {
			if errors.Is(err, model.ErrNoCompatibleChannel) {
				if c.GetString("expected_task_plugin_key") != "" {
					return nil, selectGroup, &ChannelSelectError{
						StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound,
						MessageID: i18n.MsgDistributorNoAvailableChannel, NoAvailableChannel: true,
						Params: map[string]any{"Group": usingGroup, "Model": modelName},
					}
				}
				message := err.Error()
				if reason := common.GetContextKeyString(c, constant.ContextKeyProtocolIncompatibleReason); reason != "" {
					message += ": " + reason
				}
				return nil, selectGroup, &ChannelSelectError{StatusCode: http.StatusBadRequest, Code: types.ErrorCodeInvalidRequest, Message: message}
			}
			showGroup := usingGroup
			if usingGroup == "auto" {
				showGroup = fmt.Sprintf("auto(%s)", selectGroup)
			}
			return nil, selectGroup, &ChannelSelectError{
				StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgDistributorGetChannelFailed,
				Params: map[string]any{"Group": showGroup, "Model": modelName, "Error": err.Error()},
			}
		}
		if channel == nil {
			return nil, selectGroup, &ChannelSelectError{
				StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgDistributorNoAvailableChannel,
				Params: map[string]any{"Group": usingGroup, "Model": modelName}, NoAvailableChannel: true,
			}
		}
	}
	if ok, kind := model.ChannelSatisfiesFilters(channel, modelName, constraints.Filters); !ok {
		return nil, selectGroup, &ChannelSelectError{
			StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgDistributorNoAvailableChannel,
			Params:     map[string]any{"Group": common.GetContextKeyString(c, constant.ContextKeyUsingGroup), "Model": modelName},
			FilterKind: kind, Channel: channel, NoAvailableChannel: true,
		}
	}
	return channel, selectGroup, nil
}

// Origin-task pins report a fixed code so task polling can tell a retired
// channel from a malformed request.
func pinnedChannelUnavailable(pin dto.ChannelPin, statusCode int, messageID string) *ChannelSelectError {
	if pin.Source == dto.PinSourceOriginTask {
		return &ChannelSelectError{StatusCode: http.StatusBadRequest, Code: "origin_task_channel_disabled", Message: "origin_task_channel_disabled"}
	}
	return &ChannelSelectError{StatusCode: statusCode, MessageID: messageID}
}

// AppendUsedChannel records an attempted channel in the request's channel
// trail, which the retry log and the consume log's admin_info both read.
func AppendUsedChannel(c *gin.Context, channelID int) {
	c.Set("use_channel", append(c.GetStringSlice("use_channel"), fmt.Sprintf("%d", channelID)))
}
