package middleware

import (
	"errors"
	"fmt"
	hosttypes "github.com/QuantumNous/new-api/types"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/protocolstate"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var ErrChannelOutsideSchedule = errors.New("channel is outside its scheduled availability")

type ModelRequest struct {
	Model string `json:"model"`
	Group string `json:"group,omitempty"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		var channel *model.Channel
		defer func() {
			if c.Writer.Status() >= 400 {
				service.RecordRequestPolicyTermination(c, hosttypes.NewErrorWithStatusCode(errors.New("request rejected"), hosttypes.ErrorCodeInvalidRequest, c.Writer.Status(), hosttypes.ErrOptionWithSkipRetry()))
			}
		}()
		constraints := service.GetChannelConstraints(c)
		constraints.AddFilter(taskdto.ChannelFilter{
			Kind:        taskdto.FilterRequestPath,
			RequestPath: c.Request.URL.Path,
		})
		service.AppendTaskPluginIdentityFilter(c, c.GetString("expected_task_plugin_key"))
		channelId, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId)
		modelRequest, shouldSelectChannel, err := getModelRequest(c)
		if err != nil {
			abortWithProtocolMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		if modelRequest.Model != "" {
			matchName := ratio_setting.FormatMatchingModelName(modelRequest.Model)
			if common.GetContextKeyBool(c, constant.ContextKeyUserModelLimitEnabled) {
				allowed, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyUserModelLimit)
				if !allowed[matchName] {
					abortWithProtocolMessage(
						c,
						http.StatusNotFound,
						"The requested model does not exist or you do not have access to it",
						hosttypes.ErrorCodeModelNotFound,
					)
					return
				}
			}
			if common.GetContextKeyBool(c, constant.ContextKeyUserModelBlocklistEnabled) {
				blocked, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyUserModelBlocklist)
				if blocked[matchName] {
					abortWithProtocolMessage(
						c,
						http.StatusNotFound,
						"The requested model does not exist or you do not have access to it",
						hosttypes.ErrorCodeModelNotFound,
					)
					return
				}
			}
			if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
				allowed, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
				if !TokenModelLimitAllows(allowed, modelRequest.Model) {
					abortWithProtocolMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenModelForbidden, map[string]any{"Model": modelRequest.Model}))
					return
				}
			}
		}
		userGroups := common.GetContextKeyStringSlice(c, constant.ContextKeyUserGroups)
		usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		if usingGroup == "" && len(userGroups) > 0 {
			usingGroup = userGroups[0]
			common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
		}
		if shouldSelectChannel && strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
			playgroundRequest := &dto.PlayGroundRequest{}
			err = common.UnmarshalBodyReusable(c, playgroundRequest)
			if err != nil {
				abortWithProtocolMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidPlayground, map[string]any{"Error": err.Error()}))
				return
			}
			if playgroundRequest.Group != "" {
				if !service.GroupInUserUsableGroupsForGroups(userGroups, playgroundRequest.Group) {
					abortWithProtocolMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorGroupAccessDenied))
					return
				}
				usingGroup = playgroundRequest.Group
				common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
			}
		}
		if usingGroup != "auto" && !groupAllowsRequestClient(c, usingGroup) {
			abortWithProtocolMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorGroupAccessDenied))
			return
		}
		promptAuditCleanup, promptAuditAllowed := inspectPromptBeforeDistribution(c, modelRequest)
		if promptAuditCleanup != nil {
			defer promptAuditCleanup()
		}
		if !promptAuditAllowed {
			return
		}

		// Resolve the user-specific route before channel selection so every
		// configured user receives strict target-model/channel-pool selection.
		_, hasResolvedPin, _ := constraints.ResolvedPin()
		if (shouldSelectChannel || ok || hasResolvedPin) && modelRequest.Model != "" {
			if _, routeErr := applyUserModelRoute(c, modelRequest.Model, usingGroup); routeErr != nil {
				abortWithProtocolMessage(c, http.StatusServiceUnavailable, "用户模型路由暂时不可用")
				return
			}
		}
		var protocolBinding *protocolstate.SelectionBinding
		if shouldSelectChannel && modelRequest.Model != "" {
			storage, storageErr := common.GetBodyStorage(c)
			if storageErr != nil {
				abortWithProtocolMessage(c, http.StatusBadRequest, storageErr.Error(), hosttypes.ErrorCodeInvalidRequest)
				return
			}
			body, bytesErr := storage.Bytes()
			if bytesErr != nil {
				abortWithProtocolMessage(c, http.StatusBadRequest, bytesErr.Error(), hosttypes.ErrorCodeInvalidRequest)
				return
			}
			protocolBinding, err = protocolstate.ResolveSelectionBinding(c, c.Request.URL.Path, modelRequest.Model, body)
			if err != nil {
				abortWithProtocolMessage(c, http.StatusBadRequest, err.Error(), hosttypes.ErrorCodeInvalidRequest)
				return
			}
			if protocolBinding != nil {
				common.SetContextKey(c, constant.ContextKeyProtocolStateBinding, protocolBinding)
			}
		}
		if pin, found, overridden := constraints.ResolvedPin(); found {
			for _, lost := range overridden {
				logger.LogWarn(c, fmt.Sprintf(
					"channel pin overridden: winning_source=%s winning_channel_id=%d overridden_source=%s overridden_channel_id=%d",
					pin.Source, pin.ChannelId, lost.Source, lost.ChannelId,
				))
			}
			channel, err = model.CacheGetChannel(pin.ChannelId)
			if err != nil {
				if pin.Source == taskdto.PinSourceOriginTask {
					abortWithProtocolMessage(c, http.StatusBadRequest, "origin_task_channel_disabled", hosttypes.ErrorCode("origin_task_channel_disabled"))
				} else {
					abortWithProtocolMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				}
				return
			}
			if channel.Status != common.ChannelStatusEnabled || !channel.IsSchedulableAt(time.Now()) {
				if pin.Source == taskdto.PinSourceOriginTask {
					abortWithProtocolMessage(c, http.StatusBadRequest, "origin_task_channel_disabled", hosttypes.ErrorCode("origin_task_channel_disabled"))
				} else {
					abortWithProtocolMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorChannelDisabled))
				}
				return
			}
			if allowed, kind := model.ChannelSatisfiesFilters(channel, modelRequest.Model, constraints.Filters); !allowed {
				if kind == taskdto.FilterTaskPluginIdentity {
					logTaskPluginChannelDecision(c, channel, modelRequest.Model, "channel_rejected", "identity_mismatch")
				}
				abortWithProtocolMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{
					"Group": common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
					"Model": modelRequest.Model,
				}), hosttypes.ErrorCode(kind))
				return
			}
			if err := validateSelectedRouteChannel(c, channel, c.Request.URL.Path); err != nil {
				abortWithProtocolMessage(c, http.StatusForbidden, "指定渠道不符合该用户的模型路由")
				return
			}
		} else if ok {
			id, err := strconv.Atoi(channelId.(string))
			if err != nil {
				abortWithProtocolMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithProtocolMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			if !channel.IsSchedulableAt(time.Now()) {
				abortWithProtocolMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorChannelDisabled))
				return
			}
			if err := validateSelectedRouteChannel(c, channel, c.Request.URL.Path); err != nil {
				abortWithProtocolMessage(c, http.StatusForbidden, "指定渠道不符合该用户的模型路由")
				return
			}
		} else {
			// Select a channel for the user.
			if shouldSelectChannel {
				if modelRequest.Model == "" {
					abortWithProtocolMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorModelNameRequired))
					return
				}
				selectionModel := routeSelectionModel(c, modelRequest.Model)
				selectionGroup := routeSelectionGroup(c, usingGroup)
				candidateFilter := BuildChannelCandidateFilter(c, selectionModel)
				candidateClassifier := BuildChannelCandidateClassifier(c, selectionModel)
				if protocolBinding != nil && protocolBinding.ChannelID > 0 {
					bound, boundErr := model.CacheGetChannel(protocolBinding.ChannelID)
					if boundErr == nil && bound != nil && bound.IsSchedulableAt(time.Now()) &&
						(candidateFilter == nil || candidateFilter(bound)) &&
						channelMatchesCandidateClassifier(bound, candidateClassifier) &&
						channelSupportsRequestPath(bound, c.Request.URL.Path, selectionModel) &&
						channelPassesFilters(bound, selectionModel, constraints.Filters) {
						resolvedGroup, groupUsable := resolveAffinitySelectionGroup(c, selectionGroup, selectionModel, bound.Id)
						if routeChannelAllowed(c, bound.Id) && groupUsable {
							channel = bound
							commitRouteSelectionGroup(c, selectionGroup, resolvedGroup)
						}
					}
				}
				if channel == nil {
					var selectErr *service.ChannelSelectError
					channel, _, selectErr = service.SelectChannelForRequest(c, selectionModel, &service.RetryParam{
						Ctx: c, ModelName: selectionModel, TokenGroup: selectionGroup,
						RequestPath: c.Request.URL.Path, AllowedChannelIds: routeSelectionChannelIds(c),
						AllowedGroups: routeSelectionExecutionGroups(c), CandidateFilter: candidateFilter,
						CandidateClassifier: candidateClassifier, Retry: common.GetPointer(0),
					})
					if selectErr != nil {
						message := selectErr.Message
						if selectErr.MessageID != "" {
							message = i18n.T(c, selectErr.MessageID, selectErr.Params)
						}
						if selectErr.NoAvailableChannel {
							message = noAvailableChannelMessage(c, usingGroup, modelRequest.Model)
						}
						if selectErr.FilterKind == taskdto.FilterTaskPluginIdentity {
							logTaskPluginChannelDecision(c, selectErr.Channel, modelRequest.Model, "channel_rejected", "identity_mismatch")
						}
						abortWithProtocolMessage(c, selectErr.StatusCode, message, selectErr.Code)
						return
					}
				}

			}
		}
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		if channel != nil {
			if allowed, kind := model.ChannelSatisfiesFilters(channel, modelRequest.Model, constraints.Filters); !allowed {
				if kind == taskdto.FilterTaskPluginIdentity {
					logTaskPluginChannelDecision(c, channel, modelRequest.Model, "channel_rejected", "identity_mismatch")
				}
				abortWithProtocolMessage(c, http.StatusServiceUnavailable, noAvailableChannelMessage(c, common.GetContextKeyString(c, constant.ContextKeyUsingGroup), modelRequest.Model), hosttypes.ErrorCodeModelNotFound)
				return
			}
			if setupErr := SetupContextForSelectedChannel(c, channel, modelRequest.Model, true); setupErr != nil {
				abortWithProtocolMessage(c, setupErr.StatusCode, setupErr.Error(), setupErr.GetErrorCode())
				return
			}
		}
		c.Next()
		if channel != nil && c.Writer != nil && c.Writer.Status() < http.StatusBadRequest {
			service.RecordChannelAffinity(c, channel.Id)
		}
	}
}

func channelPassesFilters(channel *model.Channel, modelName string, filters []taskdto.ChannelFilter) bool {
	allowed, _ := model.ChannelSatisfiesFilters(channel, modelName, filters)
	return allowed
}

func channelMatchesCandidateClassifier(channel *model.Channel, classifier model.ChannelCandidateClassifier) bool {
	if channel == nil {
		return false
	}
	if classifier == nil {
		return true
	}
	class := classifier(channel)
	return class == model.ChannelCandidateNative || class == model.ChannelCandidateConvertible
}

// resolveAffinitySelectionGroup preserves cross-group selection semantics for an
// affinity hit. Multi-group routes use their configured order; ordinary auto
// tokens use the user's usable auto groups.
func resolveAffinitySelectionGroup(c *gin.Context, selectionGroup, selectionModel string, channelId int) (string, bool) {
	if selectionGroup != "auto" {
		return selectionGroup, groupAllowsRequestClient(c, selectionGroup) && model.IsChannelEnabledForGroupModel(selectionGroup, selectionModel, channelId)
	}

	selectionGroups := routeSelectionExecutionGroups(c)
	if len(selectionGroups) > 0 {
		for _, group := range selectionGroups {
			if groupAllowsRequestClient(c, group) && model.IsChannelEnabledForGroupModel(group, selectionModel, channelId) {
				return group, true
			}
		}
		return "", false
	}

	userGroups := common.GetContextKeyStringSlice(c, constant.ContextKeyUserGroups)
	if len(userGroups) == 0 {
		if userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup); userGroup != "" {
			userGroups = []string{userGroup}
		}
	}
	for _, group := range service.GetRequestAutoGroupsForGroups(c, userGroups) {
		if groupAllowsRequestClient(c, group) && model.IsChannelEnabledForGroupModel(group, selectionModel, channelId) {
			return group, true
		}
	}
	return "", false
}

// channelSupportsRequestPath reports whether a channel can serve the request path.
// Only Advanced Custom (type 58) channels are path-checked; all other channel types
// always pass. A type-58 channel is usable only when one of its routes matches.
func channelSupportsRequestPath(channel *model.Channel, requestPath string, requestModel string) bool {
	if channel == nil {
		return false
	}
	if channel.Type != constant.ChannelTypeAdvancedCustom {
		return true
	}
	config := channel.GetOtherSettings().AdvancedCustom
	if config == nil {
		return false
	}
	_, matched := channel.MatchAdvancedCustomRoute(requestPath, requestModel, config)
	return matched
}

// noAvailableChannelMessage explains a 503 for a task-plugin-claimed model.
// The response tells the caller the model is plugin-claimed without naming the
// plugin; the candidate plugin keys go to the server log under the request id.
func noAvailableChannelMessage(c *gin.Context, group, modelName string) string {
	value, exists := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinned, ok := value.(jsplugin.PinnedPlugin)
	if exists && ok && pinned.Plugin != nil {
		keys := []string{pinned.Plugin.Meta.Key}
		if value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint); exists {
			if endpoint, ok := value.(jsplugin.PinnedEndpoint); ok && len(endpoint.Candidates) > 0 {
				keys = nil
				for _, candidate := range endpoint.Candidates {
					if candidate.Plugin != nil {
						keys = append(keys, candidate.Plugin.Meta.Key)
					}
				}
			}
		}
		logger.LogWarn(c, "task_plugin subsystem=distribution event=no_available_channel group=%q model=%q plugins=%q reason=no_eligible_channel", group, modelName, strings.Join(keys, ","))
		return i18n.T(c, i18n.MsgDistributorNoAvailableChannelTaskPlugin, map[string]any{"Group": group, "Model": modelName})
	}
	return i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": group, "Model": modelName})
}

func channelMatchesExpectedTaskPlugin(c *gin.Context, channel *model.Channel, expected string) bool {
	if channel == nil {
		return false
	}
	if c != nil {
		if _, matched := pinnedEndpointCandidateForChannel(c, channel, expected); matched {
			return true
		}
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		return expected != "" && channel.GetSetting().TaskPluginKey == expected
	}
	if expected == "" {
		return true
	}
	if c == nil {
		return false
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinned, ok := value.(jsplugin.PinnedPlugin)
	if !exists || !ok || pinned.Generation == nil || pinned.Plugin == nil || pinned.Plugin.Meta.Key != expected {
		return false
	}
	plugin, ok := pinned.Generation.GetByChannelType(channel.Type)
	return ok && plugin == pinned.Plugin
}

func pinnedEndpointCandidateForChannel(c *gin.Context, channel *model.Channel, expected string) (jsplugin.ProtocolBinding, bool) {
	if c == nil || channel == nil || expected == "" {
		return jsplugin.ProtocolBinding{}, false
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint)
	pinned, ok := value.(jsplugin.PinnedEndpoint)
	if !exists || !ok || pinned.Generation == nil || pinned.Plugin == nil {
		return jsplugin.ProtocolBinding{}, false
	}
	candidates := pinned.Candidates
	if len(candidates) == 0 {
		candidates = []jsplugin.ProtocolBinding{{
			Plugin: pinned.Plugin, Protocol: pinned.Protocol, Operation: pinned.Operation, Model: pinned.Model,
		}}
	}
	expectedOwned := false
	selected := jsplugin.ProtocolBinding{}
	for _, candidate := range candidates {
		if candidate.Plugin == nil {
			continue
		}
		if candidate.Plugin.Meta.Key == expected {
			expectedOwned = true
		}
		if channel.Type == constant.ChannelTypeTaskPlugin {
			if channel.GetSetting().TaskPluginKey == candidate.Plugin.Meta.Key {
				selected = candidate
			}
			continue
		}
		plugin, indexed := pinned.Generation.GetByChannelType(channel.Type)
		if indexed && plugin == candidate.Plugin {
			selected = candidate
		}
	}
	return selected, expectedOwned && selected.Plugin != nil
}

// getModelFromRequest 从请求中读取模型信息
// 根据 Content-Type 自动处理：
// - application/json
// - application/x-www-form-urlencoded
// - multipart/form-data
func getModelFromRequest(c *gin.Context) (*ModelRequest, error) {
	if cached, exists := c.Get(contextKeyTaskPluginEndpointModel); exists {
		if modelRequest, ok := cached.(ModelRequest); ok {
			cachedRequest := modelRequest
			return &cachedRequest, nil
		}
	}
	if strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		modelRequest, err := getModelFromJSONBody(c)
		if err != nil {
			return nil, errors.New(i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
		}
		return modelRequest, nil
	}

	var modelRequest ModelRequest
	err := common.UnmarshalBodyReusable(c, &modelRequest)
	if err != nil {
		return nil, errors.New(i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
	}
	return &modelRequest, nil
}

func getModelFromJSONBody(c *gin.Context) (*ModelRequest, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if !gjson.ValidBytes(requestBody) {
		return nil, errors.New("invalid JSON request body")
	}
	if countTopLevelJSONKey(requestBody, "model") > 1 {
		return nil, errors.New("model must be provided once")
	}

	values := gjson.GetManyBytes(requestBody, "model", "group")
	model, err := getJSONStringValue(values[0], "model")
	if err != nil {
		return nil, err
	}
	group, err := getJSONStringValue(values[1], "group")
	if err != nil {
		return nil, err
	}

	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	c.Request.Body = io.NopCloser(storage)

	return &ModelRequest{
		Model: model,
		Group: group,
	}, nil
}

func countTopLevelJSONKey(data []byte, target string) int {
	depth := 0
	inString := false
	escaped := false
	stringStart := 0
	expectingKey := false
	count := 0
	for index, current := range data {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current != '"' {
				continue
			}
			inString = false
			if depth == 1 && expectingKey {
				key := string(data[stringStart:index])
				var decodedKey string
				if common.Unmarshal(data[stringStart-1:index+1], &decodedKey) == nil {
					key = decodedKey
				}
				cursor := index + 1
				for cursor < len(data) && (data[cursor] == ' ' || data[cursor] == '\t' || data[cursor] == '\r' || data[cursor] == '\n') {
					cursor++
				}
				if cursor < len(data) && data[cursor] == ':' && key == target {
					count++
				}
				expectingKey = false
			}
			continue
		}
		switch current {
		case '"':
			inString = true
			stringStart = index + 1
		case '{':
			depth++
			if depth == 1 {
				expectingKey = true
			}
		case '}':
			depth--
		case ',':
			if depth == 1 {
				expectingKey = true
			}
		}
	}
	return count
}

func getJSONStringValue(result gjson.Result, field string) (string, error) {
	if !result.Exists() || result.Type == gjson.Null {
		return "", nil
	}
	if result.Type != gjson.String {
		return "", fmt.Errorf("field %s must be a string", field)
	}
	return result.String(), nil
}

func getModelRequest(c *gin.Context) (*ModelRequest, bool, error) {
	var modelRequest ModelRequest
	shouldSelectChannel := true
	var err error
	if modelName := c.GetString("resolved_task_model"); modelName != "" {
		modelRequest.Model = modelName
	} else if strings.Contains(c.Request.URL.Path, "/mj/") {
		relayMode := relayconstant.Path2RelayModeMidjourney(c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeMidjourneyTaskFetch ||
			relayMode == relayconstant.RelayModeMidjourneyTaskFetchByCondition ||
			relayMode == relayconstant.RelayModeMidjourneyNotify ||
			relayMode == relayconstant.RelayModeMidjourneyTaskImageSeed {
			shouldSelectChannel = false
		} else {
			midjourneyRequest := taskdto.MidjourneyRequest{}
			err = common.UnmarshalBodyReusable(c, &midjourneyRequest)
			if err != nil {
				return nil, false, errors.New(i18n.T(c, i18n.MsgDistributorInvalidMidjourney, map[string]any{"Error": err.Error()}))
			}
			midjourneyModel, mjErr, success := service.GetMjRequestModel(relayMode, &midjourneyRequest)
			if mjErr != nil {
				return nil, false, fmt.Errorf("%s", mjErr.Description)
			}
			if midjourneyModel == "" {
				if !success {
					return nil, false, fmt.Errorf("%s", i18n.T(c, i18n.MsgDistributorInvalidParseModel))
				} else {
					// task fetch, task fetch by condition, notify
					shouldSelectChannel = false
				}
			}
			modelRequest.Model = midjourneyModel
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/suno/") {
		relayMode := relayconstant.Path2RelaySuno(c.Request.Method, c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeSunoFetch ||
			relayMode == relayconstant.RelayModeSunoFetchByID {
			shouldSelectChannel = false
		} else {
			modelName := service.CoverTaskActionToModelName(constant.TaskPlatformSuno, c.Param("action"))
			modelRequest.Model = modelName
		}
		c.Set("platform", string(constant.TaskPlatformSuno))
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos/") && strings.HasSuffix(c.Request.URL.Path, "/remix") {
		relayMode := relayconstant.RelayModeVideoSubmit
		c.Set("relay_mode", relayMode)
		shouldSelectChannel = false
		// Remix requests do not carry a model. Read the trusted origin task
		// before the model-limit check so a user's allowlist cannot be bypassed.
		modelRequest.Model, err = getTaskOriginModelName(c)
		if err != nil {
			return nil, false, err
		}
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos") {
		//curl https://api.openai.com/v1/videos \
		//  -H "Authorization: Bearer $OPENAI_API_KEY" \
		//  -F "model=sora-2" \
		//  -F "prompt=A calico cat playing a piano on stage"
		//	-F input_reference="@image.jpg"
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			relayMode = relayconstant.RelayModeVideoSubmit
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			if req != nil {
				modelRequest.Model = req.Model
			}
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
			modelRequest.Model, err = getTaskOriginModelName(c)
			if err != nil {
				return nil, false, err
			}
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/video/generations") {
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			modelRequest.Model = req.Model
			relayMode = relayconstant.RelayModeVideoSubmit
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
			modelRequest.Model, err = getTaskOriginModelName(c)
			if err != nil {
				return nil, false, err
			}
		}
		if _, ok := c.Get("relay_mode"); !ok {
			c.Set("relay_mode", relayMode)
		}
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models/") || strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
		// Gemini API 路径处理: /v1beta/models/gemini-2.0-flash:generateContent
		relayMode := relayconstant.RelayModeGemini
		modelName := extractModelNameFromGeminiPath(c.Request.URL.Path)
		if modelName != "" {
			modelRequest.Model = modelName
		}
		c.Set("relay_mode", relayMode)
	} else if !strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") && !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/realtime") {
		//wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01
		modelRequest.Model = c.Query("model")
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/moderations") {
		if modelRequest.Model == "" {
			modelRequest.Model = "text-moderation-stable"
		}
	}
	if strings.HasSuffix(c.Request.URL.Path, "embeddings") {
		if modelRequest.Model == "" {
			modelRequest.Model = c.Param("model")
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/images/generations") {
		modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "dall-e")
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1/images/edits") {
		//modelRequest.Model = common.GetStringIfEmpty(c.PostForm("model"), "gpt-image-1")
		contentType := c.ContentType()
		if slices.Contains([]string{gin.MIMEPOSTForm, gin.MIMEMultipartPOSTForm}, contentType) {
			req, err := getModelFromRequest(c)
			if err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/audio") {
		relayMode := relayconstant.RelayModeAudioSpeech
		if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/speech") {

			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "tts-1")
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/translations") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranslation
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranscription
		}
		c.Set("relay_mode", relayMode)
	}
	if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
		// playground chat completions
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
		modelRequest.Group = req.Group
		common.SetContextKey(c, constant.ContextKeyTokenGroup, modelRequest.Group)
	}

	if strings.HasPrefix(c.Request.URL.Path, "/v1/responses/compact") && modelRequest.Model != "" {
		modelRequest.Model = ratio_setting.WithCompactModelSuffix(modelRequest.Model)
	}
	return &modelRequest, shouldSelectChannel, nil
}

// TokenModelLimitAllows reports whether a token model-limit map authorizes
// model. Exact name, wildcard-normalized name, and routing-normalized name
// (modifiers and legacy aliases stripped) are all accepted. The Responses
// WebSocket relay shares this rule so both transports admit the same names.
func TokenModelLimitAllows(limit map[string]bool, model string) bool {
	if limit[model] {
		return true
	}
	if formatted := ratio_setting.FormatMatchingModelName(model); limit[formatted] {
		return true
	}
	return limit[ratio_setting.RoutingMatchModelName(model)]
}

// 修复 #4834: GET /v1/video/generations/:task_id && /v1/video/:task_id 此前不解析 model，
// 当 token 启用「可用模型限制」时，下游 modelLimitEnable 校验会因
// modelRequest.Model 为空而误报 "This token has no access to model"。
// 从已存储的任务记录中回填 OriginModelName 即可让校验走在正确的模型上。
func getTaskOriginModelName(c *gin.Context) (string, error) {
	isRemix := strings.Contains(c.Request.URL.Path, "/v1/videos/") && strings.HasSuffix(c.Request.URL.Path, "/remix")
	if !isRemix && !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) &&
		!common.GetContextKeyBool(c, constant.ContextKeyUserModelLimitEnabled) &&
		!common.GetContextKeyBool(c, constant.ContextKeyUserModelBlocklistEnabled) {
		return "", nil
	}

	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.Param("video_id")
	}
	if taskId == "" {
		// jimeng adapter
		taskId = c.GetString("task_id")
	}
	if taskId == "" {
		return "", nil
	}

	userId := c.GetInt("id")
	if task, exist, err := model.GetByTaskId(userId, taskId); err != nil {
		return "", err
	} else if exist && task != nil {
		return task.Properties.OriginModelName, nil
	}
	return "", nil
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string, enforceSchedule bool) *hosttypes.NewAPIError {
	c.Set("original_model", modelName) // for retry
	expectedPlugin := c.GetString("expected_task_plugin_key")
	if channel == nil {
		logTaskPluginChannelDecision(c, nil, modelName, "channel_rejected", "nil_channel")
		return hosttypes.NewError(errors.New("channel is nil"), hosttypes.ErrorCodeGetChannelFailed, hosttypes.ErrOptionWithSkipRetry())
	}
	if enforceSchedule && !channel.Schedule.IsAvailableAt(time.Now()) {
		return hosttypes.NewErrorWithStatusCode(
			ErrChannelOutsideSchedule,
			hosttypes.ErrorCodeGetChannelFailed,
			http.StatusServiceUnavailable,
		)
	}
	if expectedPlugin != "" && !channelMatchesExpectedTaskPlugin(c, channel, expectedPlugin) {
		logTaskPluginChannelDecision(c, channel, modelName, "channel_rejected", "identity_mismatch")
		return hosttypes.NewError(
			errors.New("selected channel does not match the pinned task plugin"),
			hosttypes.ErrorCodeGetChannelFailed,
			hosttypes.ErrOptionWithSkipRetry(),
		)
	}
	if candidate, matched := pinnedEndpointCandidateForChannel(c, channel, expectedPlugin); matched {
		if value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint); exists {
			if pinned, ok := value.(jsplugin.PinnedEndpoint); ok && candidate.Plugin != nil && candidate.Plugin != pinned.Plugin {
				previousPlugin := pinned.Plugin.Meta.Key
				pinned.Plugin = candidate.Plugin
				pinned.Protocol = candidate.Protocol
				pinned.Operation = candidate.Operation
				c.Set(jsplugin.ContextKeyPinnedEndpoint, pinned)
				c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: pinned.Generation, Plugin: candidate.Plugin})
				c.Set("expected_task_plugin_key", candidate.Plugin.Meta.Key)
				c.Set("task_plugin_key", candidate.Plugin.Meta.Key)
				c.Set("platform", candidate.Plugin.Meta.Key)
				logger.LogDebug(
					c,
					"task_plugin subsystem=endpoint event=provider_selected generation=%d previous_plugin=%q plugin=%q model=%q channel_id=%d channel_type=%d",
					pinned.Generation.Number,
					previousPlugin,
					candidate.Plugin.Meta.Key,
					modelName,
					channel.Id,
					channel.Type,
				)
			}
		}
	}
	selectionModel := routeSelectionModel(c, modelName)
	selectionGroup := routeSelectionGroup(c, common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
	if selectionGroup == "auto" {
		selectionGroup = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	if selectionGroup != "" && !groupAllowsRequestClient(c, selectionGroup) {
		return hosttypes.NewErrorWithStatusCode(
			fmt.Errorf("group %s does not allow client %s", selectionGroup, requestClient(c)),
			hosttypes.ErrorCodeInvalidRequest,
			http.StatusForbidden,
			hosttypes.ErrOptionWithSkipRetry(),
		)
	}
	if err := applySelectedChannelCompatibility(c, channel, selectionModel); err != nil {
		return hosttypes.NewErrorWithStatusCode(err, hosttypes.ErrorCodeInvalidRequest, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry())
	}
	// A retry reuses the Gin context. Clear provider-specific values before
	// installing the newly selected channel so URL, header, and adaptor state
	// cannot leak from the previous attempt.
	common.SetContextKey(c, constant.ContextKeyChannelOrganization, "")
	common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 0)
	c.Set("api_version", "")
	c.Set("region", "")
	c.Set("plugin", "")
	c.Set("bot_id", "")
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	if channel.Type == constant.ChannelTypeTaskPlugin {
		c.Set("task_plugin_key", channel.GetSetting().TaskPluginKey)
	}
	logTaskPluginChannelDecision(c, channel, modelName, "channel_selected", "")
	paramOverride := channel.GetParamOverride()
	headerOverride := channel.GetHeaderOverride()
	if mergedParam, applied := service.ApplyChannelAffinityOverrideTemplate(c, paramOverride); applied {
		paramOverride = mergedParam
	}
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, paramOverride)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, headerOverride)
	if nil != channel.OpenAIOrganization && *channel.OpenAIOrganization != "" {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, *channel.OpenAIOrganization)
	}
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())

	key, index, newAPIError := channel.GetNextEnabledKey()
	if newAPIError != nil {
		return newAPIError
	}
	if channel.ChannelInfo.IsMultiKey {
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, index)
	} else {
		// 必须设置为 false，否则在重试到单个 key 的时候会导致日志显示错误
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
	}
	// c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)

	// TODO: api_version统一
	switch channel.Type {
	case constant.ChannelTypeAzure:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeVertexAi:
		c.Set("region", channel.Other)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeGemini:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeAli:
		c.Set("plugin", channel.Other)
	case constant.ChannelCloudflare:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", channel.Other)
	}
	return nil
}

// extractModelNameFromGeminiPath 从 Gemini API URL 路径中提取模型名
// 输入格式: /v1beta/models/gemini-2.0-flash:generateContent
// 输出: gemini-2.0-flash
func extractModelNameFromGeminiPath(path string) string {
	// 查找 "/models/" 的位置
	modelsPrefix := "/models/"
	modelsIndex := strings.Index(path, modelsPrefix)
	if modelsIndex == -1 {
		return ""
	}

	// 从 "/models/" 之后开始提取
	startIndex := modelsIndex + len(modelsPrefix)
	if startIndex >= len(path) {
		return ""
	}

	// 查找 ":" 的位置，模型名在 ":" 之前
	colonIndex := strings.Index(path[startIndex:], ":")
	if colonIndex == -1 {
		// 如果没有找到 ":"，返回从 "/models/" 到路径结尾的部分
		return path[startIndex:]
	}

	// 返回模型名部分
	return path[startIndex : startIndex+colonIndex]
}
