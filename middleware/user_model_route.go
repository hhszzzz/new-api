package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func applyUserModelRoute(c *gin.Context, sourceModel, usingGroup string) (*model.UserModelRoute, error) {
	if c == nil || c.GetInt("id") <= 0 {
		return nil, nil
	}
	sourceModel = strings.TrimSpace(sourceModel)
	usingGroup = strings.TrimSpace(usingGroup)
	if sourceModel == "" || usingGroup == "" {
		return nil, nil
	}
	applicableGroups := []string{usingGroup}
	if usingGroup == "auto" {
		userGroups := common.GetContextKeyStringSlice(c, constant.ContextKeyUserGroups)
		applicableGroups = applicableGroups[:0]
		for _, group := range service.GetUserAutoGroups(userGroups) {
			if groupAllowsRequestClient(c, group) {
				applicableGroups = append(applicableGroups, group)
			}
		}
	} else if !groupAllowsRequestClient(c, usingGroup) {
		return nil, fmt.Errorf("client is not allowed to use request group %s", usingGroup)
	}
	if len(applicableGroups) == 0 {
		return nil, nil
	}
	route, err := model.GetApplicableUserModelRouteForGroups(c.GetInt("id"), sourceModel, applicableGroups)
	if err != nil {
		return nil, err
	}
	if route == nil {
		// Responses compaction internally adds a suffix, while administrators
		// normally configure the public model without that transport suffix.
		baseModel := strings.TrimSuffix(sourceModel, ratio_setting.CompactModelSuffix)
		if baseModel != sourceModel {
			route, err = model.GetApplicableUserModelRouteForGroups(c.GetInt("id"), baseModel, applicableGroups)
			if err != nil {
				return nil, err
			}
		}
	}
	if route == nil || strings.TrimSpace(route.TargetModel) == "" || strings.TrimSpace(route.ExecutionGroup) == "" {
		return nil, nil
	}
	executionGroup := strings.TrimSpace(route.ExecutionGroup)
	if !groupAllowsRequestClient(c, executionGroup) {
		return nil, fmt.Errorf("client is not allowed to use routed execution group %s", executionGroup)
	}
	channels := append([]int(nil), route.ChannelIds...)
	common.SetContextKey(c, constant.ContextKeyUserModelRouteId, route.Id)
	common.SetContextKey(c, constant.ContextKeyUserModelRouteTarget, strings.TrimSpace(route.TargetModel))
	common.SetContextKey(c, constant.ContextKeyUserModelRouteGroup, executionGroup)
	common.SetContextKey(c, constant.ContextKeyUserModelRoutePool, strings.TrimSpace(route.PoolName))
	common.SetContextKey(c, constant.ContextKeyUserModelRoutePrompt, strings.TrimSpace(route.InjectPrompt))
	if usingGroup == "auto" {
		// A routed request has already resolved the otherwise dynamic auto group.
		// Publish that concrete group so quota calculation and logs use the same
		// group that channel selection uses.
		common.SetContextKey(c, constant.ContextKeyAutoGroup, executionGroup)
	}
	// A non-nil slice, including an empty one, means the route is a strict
	// channel pool. The channel selector treats nil as unrestricted.
	common.SetContextKey(c, constant.ContextKeyUserModelRouteChannel, channels)
	return route, nil
}

// ApplyUserModelRoute exposes route resolution to controllers that must defer
// selection until an origin task has been loaded (for example video remix).
func ApplyUserModelRoute(c *gin.Context, sourceModel, usingGroup string) (*model.UserModelRoute, error) {
	return applyUserModelRoute(c, sourceModel, usingGroup)
}

func userModelRouteActive(c *gin.Context) bool {
	_, ok := common.GetContextKey(c, constant.ContextKeyUserModelRouteId)
	return ok && common.GetContextKeyInt(c, constant.ContextKeyUserModelRouteId) > 0
}

func routeSelectionModel(c *gin.Context, sourceModel string) string {
	if target := common.GetContextKeyString(c, constant.ContextKeyUserModelRouteTarget); target != "" {
		return target
	}
	return sourceModel
}

func routeSelectionGroup(c *gin.Context, fallback string) string {
	if group := common.GetContextKeyString(c, constant.ContextKeyUserModelRouteGroup); group != "" {
		return group
	}
	return fallback
}

func routeSelectionChannelIds(c *gin.Context) []int {
	ids, ok := common.GetContextKeyType[[]int](c, constant.ContextKeyUserModelRouteChannel)
	if !ok {
		return nil
	}
	return ids
}

func routeChannelAllowed(c *gin.Context, channelId int) bool {
	if !userModelRouteActive(c) {
		return true
	}
	for _, allowed := range routeSelectionChannelIds(c) {
		if allowed == channelId {
			return true
		}
	}
	return false
}

func validateSelectedRouteChannel(c *gin.Context, channel *model.Channel, requestPath string) error {
	if channel == nil {
		return fmt.Errorf("channel is nil")
	}
	if !userModelRouteActive(c) {
		return nil
	}
	selectionGroup := routeSelectionGroup(c, common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
	selectionModel := routeSelectionModel(c, common.GetContextKeyString(c, constant.ContextKeyOriginalModel))
	if !groupAllowsRequestClient(c, selectionGroup) {
		return fmt.Errorf("selected route execution group does not allow this client")
	}
	if !routeChannelAllowed(c, channel.Id) || !model.IsChannelEnabledForGroupModel(selectionGroup, selectionModel, channel.Id) {
		return fmt.Errorf("selected channel is outside the user's model route")
	}
	if !channelSupportsRequestPath(channel, requestPath, selectionModel) {
		return fmt.Errorf("selected channel does not support this request path")
	}
	if !BuildChannelCandidateFilter(c, selectionModel)(channel) {
		return fmt.Errorf("selected channel does not allow this protocol or client")
	}
	return nil
}

// ValidateSelectedRouteChannel lets deferred task handlers enforce the same
// channel-pool, execution-group, model, and request-path checks as Distribute.
func ValidateSelectedRouteChannel(c *gin.Context, channel *model.Channel, requestPath string) error {
	return validateSelectedRouteChannel(c, channel, requestPath)
}
