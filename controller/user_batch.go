package controller

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const maxUserBatchSize = 1000

const (
	userBatchListModeAppend  = "append"
	userBatchListModeRemove  = "remove"
	userBatchListModeReplace = "replace"

	userBatchCheckinKeep       = "keep"
	userBatchCheckinGlobal     = "global"
	userBatchCheckinAllow      = "allow"
	userBatchCheckinDeny       = "deny"
	userBatchCheckinCustom     = "custom"
	userBatchQuotaCapUnlimited = "unlimited"
	userBatchRateLimitClear    = "clear"
)

type userBatchListOp struct {
	Mode    string   `json:"mode"`
	Models  []string `json:"models"`
	Enabled *bool    `json:"enabled"`
}

type userBatchCheckinOp struct {
	Mode      string `json:"mode"`       // keep | global | allow | deny
	QuotaMode string `json:"quota_mode"` // keep | global | custom
	MinQuota  *int   `json:"min_quota"`
	MaxQuota  *int   `json:"max_quota"`
}

type userBatchQuotaCapOp struct {
	Mode  string `json:"mode"` // unlimited | custom
	Value *int   `json:"value"`
}

type userBatchRateLimitOp struct {
	Mode  string `json:"mode"` // keep | clear | custom
	Value *int   `json:"value,omitempty"`
}

type userBatchRateLimitsOp struct {
	RpmLimit          *userBatchRateLimitOp `json:"rpm_limit"`
	ConcurrencyLimit  *userBatchRateLimitOp `json:"concurrency_limit"`
	StreamTpsLimit    *userBatchRateLimitOp `json:"stream_tps_limit"`
	FirstTokenDelayMs *userBatchRateLimitOp `json:"first_token_delay_ms"`
}

type userBatchPolicyRequest struct {
	UserIds        []int                  `json:"user_ids"`
	ModelLimits    *userBatchListOp       `json:"model_limits"`
	ModelBlocklist *userBatchListOp       `json:"model_blocklist"`
	Checkin        *userBatchCheckinOp    `json:"checkin"`
	QuotaCap       *userBatchQuotaCapOp   `json:"quota_cap"`
	RateLimits     *userBatchRateLimitsOp `json:"rate_limits"`
}

type userBatchSkip struct {
	Id       int    `json:"id"`
	Username string `json:"username"`
	Reason   string `json:"reason"`
}

func BatchUpdateUserPolicy(c *gin.Context) {
	var request userBatchPolicyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := normalizeUserBatchIds(request.UserIds)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if request.ModelLimits == nil && request.ModelBlocklist == nil && request.Checkin == nil && request.QuotaCap == nil && request.RateLimits == nil {
		common.ApiErrorMsg(c, "未选择任何批量修改内容")
		return
	}
	if err := normalizeUserBatchListOp(request.ModelLimits); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := normalizeUserBatchListOp(request.ModelBlocklist); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := validateUserBatchCheckinOp(request.Checkin); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := validateUserBatchQuotaCapOp(request.QuotaCap); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	rateLimitsChanged, err := validateUserBatchRateLimitsOp(request.RateLimits)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if request.ModelLimits == nil && request.ModelBlocklist == nil && request.Checkin == nil && request.QuotaCap == nil && !rateLimitsChanged {
		common.ApiErrorMsg(c, "未选择任何批量修改内容")
		return
	}

	myRole := c.GetInt("role")
	updated := 0
	skipped := make([]userBatchSkip, 0)
	for _, id := range ids {
		user, err := model.GetUserById(id, false)
		if err != nil {
			skipped = append(skipped, userBatchSkip{Id: id, Reason: "用户不存在"})
			continue
		}
		if !canManageTargetRole(myRole, user.Role) {
			skipped = append(skipped, userBatchSkip{Id: id, Username: user.Username, Reason: "无权管理该用户"})
			continue
		}
		partial := buildUserBatchPolicyPartial(user, request)
		if err := model.UpdateUserPolicyPartial(id, partial); err != nil {
			skipped = append(skipped, userBatchSkip{Id: id, Username: user.Username, Reason: err.Error()})
			continue
		}
		updated++
	}

	recordManageAudit(c, "user.batch_policy", map[string]interface{}{
		"targets":         len(ids),
		"target_user_ids": ids,
		"updated":         updated,
		"skipped":         len(skipped),
		"model_limits":    request.ModelLimits != nil,
		"blocklist":       request.ModelBlocklist != nil,
		"checkin_policy":  request.Checkin != nil,
		"quota_cap":       request.QuotaCap != nil,
		"rate_limits":     request.RateLimits,
	})
	common.ApiSuccess(c, gin.H{"updated": updated, "skipped": skipped})
}

type userBatchModelRoutesRequest struct {
	UserIds []int                 `json:"user_ids"`
	Route   *model.UserModelRoute `json:"route"`
}

// BatchAddUserModelRoutes creates the same model route for every selected
// user. Users whose validation fails or who already own an overlapping rule
// for the source model are skipped and reported instead of failing the batch.
func BatchAddUserModelRoutes(c *gin.Context) {
	var request userBatchModelRoutesRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := normalizeUserBatchIds(request.UserIds)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if request.Route == nil {
		common.ApiErrorMsg(c, "缺少模型路由内容")
		return
	}

	myRole := c.GetInt("role")
	created := 0
	skipped := make([]userBatchSkip, 0)
	for _, id := range ids {
		user, err := model.GetUserById(id, false)
		if err != nil {
			skipped = append(skipped, userBatchSkip{Id: id, Reason: "用户不存在"})
			continue
		}
		if !canManageTargetRole(myRole, user.Role) {
			skipped = append(skipped, userBatchSkip{Id: id, Username: user.Username, Reason: "无权管理该用户"})
			continue
		}
		userRoute := *request.Route
		userRoute.Groups = append([]string(nil), request.Route.Groups...)
		userRoute.ExecutionGroups = append([]string(nil), request.Route.ExecutionGroups...)
		userRoute.ChannelIds = append([]int(nil), request.Route.ChannelIds...)
		userRoute.Id = 0
		userRoute.UserId = id
		if err := validateUserModelRouteForUser(user, &userRoute); err != nil {
			skipped = append(skipped, userBatchSkip{Id: id, Username: user.Username, Reason: err.Error()})
			continue
		}
		if err := model.SaveUserModelRoute(&userRoute); err != nil {
			reason := err.Error()
			if errors.Is(err, model.ErrUserModelRouteConflict) {
				reason = "同一请求模型已存在重叠的路由规则"
			}
			skipped = append(skipped, userBatchSkip{Id: id, Username: user.Username, Reason: reason})
			continue
		}
		created++
	}

	recordManageAudit(c, "user.batch_model_route", map[string]interface{}{
		"targets": len(ids),
		"created": created,
		"skipped": len(skipped),
		"source":  request.Route.SourceModel,
	})
	common.ApiSuccess(c, gin.H{"created": created, "skipped": skipped})
}

func normalizeUserBatchIds(ids []int) ([]int, error) {
	seen := make(map[int]struct{}, len(ids))
	normalized := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	if len(normalized) == 0 {
		return nil, errors.New("未选择任何用户")
	}
	if len(normalized) > maxUserBatchSize {
		return nil, fmt.Errorf("单次批量操作最多支持 %d 个用户", maxUserBatchSize)
	}
	sort.Ints(normalized)
	return normalized, nil
}

func normalizeUserBatchListOp(op *userBatchListOp) error {
	if op == nil {
		return nil
	}
	switch op.Mode {
	case userBatchListModeAppend, userBatchListModeReplace:
		publicModels := make(map[string]struct{})
		for _, pricing := range model.GetPricing() {
			publicModels[pricing.ModelName] = struct{}{}
		}
		models, err := normalizeUserPolicyModels(op.Models, publicModels)
		if err != nil {
			return err
		}
		op.Models = models
	case userBatchListModeRemove:
		seen := make(map[string]struct{}, len(op.Models))
		models := make([]string, 0, len(op.Models))
		for _, modelName := range op.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				continue
			}
			if _, exists := seen[modelName]; exists {
				continue
			}
			seen[modelName] = struct{}{}
			models = append(models, modelName)
		}
		op.Models = models
	default:
		return fmt.Errorf("无效的批量清单模式：%s", op.Mode)
	}
	if op.Mode != userBatchListModeReplace && len(op.Models) == 0 && op.Enabled == nil {
		return errors.New("模型清单部分未包含任何修改")
	}
	return nil
}

func validateUserBatchCheckinOp(op *userBatchCheckinOp) error {
	if op == nil {
		return nil
	}
	switch op.Mode {
	case "", userBatchCheckinKeep, userBatchCheckinGlobal, userBatchCheckinAllow, userBatchCheckinDeny:
	default:
		return fmt.Errorf("无效的签到限制模式：%s", op.Mode)
	}
	switch op.QuotaMode {
	case "", userBatchCheckinKeep, userBatchCheckinGlobal:
	case userBatchCheckinCustom:
		if op.MinQuota == nil || op.MaxQuota == nil {
			return errors.New("自定义签到额度需要同时提供最小值和最大值")
		}
		if err := validateUserCheckinOverride(op.MinQuota, op.MaxQuota); err != nil {
			return err
		}
	default:
		return fmt.Errorf("无效的签到额度模式：%s", op.QuotaMode)
	}
	modeKeep := op.Mode == "" || op.Mode == userBatchCheckinKeep
	quotaKeep := op.QuotaMode == "" || op.QuotaMode == userBatchCheckinKeep
	if modeKeep && quotaKeep {
		return errors.New("签到部分未包含任何修改")
	}
	return nil
}

func validateUserBatchQuotaCapOp(op *userBatchQuotaCapOp) error {
	if op == nil {
		return nil
	}
	switch op.Mode {
	case userBatchQuotaCapUnlimited:
		return nil
	case userBatchCheckinCustom:
		if op.Value == nil {
			return errors.New("自定义额度上限需要提供数值")
		}
		return validateUserQuotaCap(op.Value)
	default:
		return fmt.Errorf("无效的额度上限模式：%s", op.Mode)
	}
}

func validateUserBatchRateLimitsOp(op *userBatchRateLimitsOp) (bool, error) {
	if op == nil {
		return false, nil
	}
	changed := false
	limits := []struct {
		name  string
		limit *userBatchRateLimitOp
	}{
		{name: "RPM", limit: op.RpmLimit},
		{name: "并发", limit: op.ConcurrencyLimit},
		{name: "流式 TPS", limit: op.StreamTpsLimit},
		{name: "首个文本延迟", limit: op.FirstTokenDelayMs},
	}
	for _, item := range limits {
		name, limit := item.name, item.limit
		if limit == nil {
			continue
		}
		switch limit.Mode {
		case userBatchCheckinKeep:
			if limit.Value != nil {
				return false, fmt.Errorf("%s 保持不变时不得提供数值", name)
			}
		case userBatchRateLimitClear:
			if limit.Value != nil {
				return false, fmt.Errorf("%s 清除覆盖时不得提供数值", name)
			}
			changed = true
		case userBatchCheckinCustom:
			if limit.Value == nil {
				return false, fmt.Errorf("%s 自定义限制需要提供数值", name)
			}
			if err := validateUserRateLimit(name, limit.Value); err != nil {
				return false, err
			}
			changed = true
		default:
			return false, fmt.Errorf("无效的%s限制模式：%s", name, limit.Mode)
		}
	}
	return changed, nil
}

func buildUserBatchPolicyPartial(user *model.User, request userBatchPolicyRequest) model.UserPolicyPartialUpdate {
	partial := model.UserPolicyPartialUpdate{}
	if op := request.ModelLimits; op != nil {
		if op.Mode == userBatchListModeReplace || len(op.Models) > 0 {
			merged := applyUserBatchListMode(user.ModelLimits, *op)
			partial.ModelLimits = &merged
		}
		partial.ModelLimitsEnabled = op.Enabled
	}
	if op := request.ModelBlocklist; op != nil {
		if op.Mode == userBatchListModeReplace || len(op.Models) > 0 {
			merged := applyUserBatchListMode(user.ModelBlocklist, *op)
			partial.ModelBlocklist = &merged
		}
		partial.ModelBlocklistEnabled = op.Enabled
	}
	if op := request.Checkin; op != nil {
		switch op.Mode {
		case userBatchCheckinGlobal:
			partial.SetCheckinEnabled = true
		case userBatchCheckinAllow:
			partial.SetCheckinEnabled = true
			partial.CheckinEnabled = common.GetPointer(true)
		case userBatchCheckinDeny:
			partial.SetCheckinEnabled = true
			partial.CheckinEnabled = common.GetPointer(false)
		}
		switch op.QuotaMode {
		case userBatchCheckinGlobal:
			partial.SetCheckinQuota = true
		case userBatchCheckinCustom:
			partial.SetCheckinQuota = true
			partial.CheckinMinQuota = op.MinQuota
			partial.CheckinMaxQuota = op.MaxQuota
		}
	}
	if op := request.QuotaCap; op != nil {
		partial.SetQuotaCap = true
		if op.Mode == userBatchCheckinCustom {
			partial.QuotaCap = op.Value
		}
	}
	if op := request.RateLimits; op != nil {
		apply := func(rateLimit *userBatchRateLimitOp, set *bool, value **int) {
			if rateLimit == nil || rateLimit.Mode == userBatchCheckinKeep {
				return
			}
			*set = true
			if rateLimit.Mode == userBatchCheckinCustom {
				*value = rateLimit.Value
			}
		}
		apply(op.RpmLimit, &partial.SetRpmLimit, &partial.RpmLimit)
		apply(op.ConcurrencyLimit, &partial.SetConcurrencyLimit, &partial.ConcurrencyLimit)
		apply(op.StreamTpsLimit, &partial.SetStreamTpsLimit, &partial.StreamTpsLimit)
		apply(op.FirstTokenDelayMs, &partial.SetFirstTokenDelayMs, &partial.FirstTokenDelayMs)
	}
	return partial
}

func applyUserBatchListMode(current []string, op userBatchListOp) []string {
	switch op.Mode {
	case userBatchListModeReplace:
		return append([]string{}, op.Models...)
	case userBatchListModeRemove:
		removal := make(map[string]struct{}, len(op.Models))
		for _, modelName := range op.Models {
			removal[modelName] = struct{}{}
		}
		kept := make([]string, 0, len(current))
		for _, modelName := range current {
			if _, exists := removal[modelName]; exists {
				continue
			}
			kept = append(kept, modelName)
		}
		return kept
	default: // append
		seen := make(map[string]struct{}, len(current)+len(op.Models))
		merged := make([]string, 0, len(current)+len(op.Models))
		for _, modelName := range current {
			if _, exists := seen[modelName]; exists {
				continue
			}
			seen[modelName] = struct{}{}
			merged = append(merged, modelName)
		}
		for _, modelName := range op.Models {
			if _, exists := seen[modelName]; exists {
				continue
			}
			seen[modelName] = struct{}{}
			merged = append(merged, modelName)
		}
		return merged
	}
}
