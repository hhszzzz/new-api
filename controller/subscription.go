package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---- Shared types ----

type SubscriptionPlanDTO struct {
	Plan model.SubscriptionPlan `json:"plan"`
}

type BillingPreferenceRequest struct {
	BillingPreference string `json:"billing_preference"`
}

type SubscriptionBalancePayRequest struct {
	PlanId int `json:"plan_id"`
}

// ---- User APIs ----

func GetSubscriptionPlans(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiSuccess(c, []SubscriptionPlanDTO{})
		return
	}

	var plans []model.SubscriptionPlan
	if err := model.DB.Where("enabled = ? AND (purchasable IS NULL OR purchasable = ?)", true, true).
		Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, p := range plans {
		p.NormalizeDefaults()
		result = append(result, SubscriptionPlanDTO{
			Plan: p,
		})
	}
	common.ApiSuccess(c, result)
}

func GetSubscriptionSelf(c *gin.Context) {
	userId := c.GetInt("id")
	settingMap, _ := model.GetUserSetting(userId, false)
	pref := common.NormalizeBillingPreference(settingMap.BillingPreference)

	// Get all subscriptions (including expired)
	allSubscriptions, err := model.GetAllUserSubscriptions(userId)
	if err != nil {
		allSubscriptions = []model.SubscriptionSummary{}
	}

	// Get active subscriptions for backward compatibility
	activeSubscriptions, err := model.GetAllActiveUserSubscriptions(userId)
	if err != nil {
		activeSubscriptions = []model.SubscriptionSummary{}
	}

	common.ApiSuccess(c, gin.H{
		"billing_preference": pref,
		"subscriptions":      activeSubscriptions, // all active subscriptions
		"all_subscriptions":  allSubscriptions,    // all subscriptions including expired
	})
}

func UpdateSubscriptionPreference(c *gin.Context) {
	userId := c.GetInt("id")
	var req BillingPreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	pref := common.NormalizeBillingPreference(req.BillingPreference)

	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	current := user.GetSetting()
	current.BillingPreference = pref
	if err := model.UpdateUserSetting(user.Id, current); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"billing_preference": pref})
}

func SubscriptionRequestBalancePay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	userId := c.GetInt("id")
	var req SubscriptionBalancePayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	if err := model.PurchaseSubscriptionWithBalance(userId, req.PlanId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- Admin APIs ----

func AdminListSubscriptionPlans(c *gin.Context) {
	var plans []model.SubscriptionPlan
	if err := model.DB.Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, p := range plans {
		p.NormalizeDefaults()
		result = append(result, SubscriptionPlanDTO{
			Plan: p,
		})
	}
	common.ApiSuccess(c, result)
}

type AdminUpsertSubscriptionPlanRequest struct {
	Plan model.SubscriptionPlan `json:"plan"`
}

func AdminCreateSubscriptionPlan(c *gin.Context) {
	var req AdminUpsertSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	req.Plan.Id = 0
	if strings.TrimSpace(req.Plan.Title) == "" {
		common.ApiErrorMsg(c, "套餐标题不能为空")
		return
	}
	if req.Plan.PriceAmount < 0 {
		common.ApiErrorMsg(c, "价格不能为负数")
		return
	}
	if req.Plan.PriceAmount > 9999 {
		common.ApiErrorMsg(c, "价格不能超过9999")
		return
	}
	if req.Plan.Currency == "" {
		req.Plan.Currency = "USD"
	}
	req.Plan.Currency = "USD"
	if req.Plan.AllowBalancePay == nil {
		req.Plan.AllowBalancePay = common.GetPointer(true)
	}
	if req.Plan.Purchasable == nil {
		req.Plan.Purchasable = common.GetPointer(true)
	}
	if req.Plan.AllowWalletOverflow == nil {
		req.Plan.AllowWalletOverflow = common.GetPointer(true)
	}
	if req.Plan.DurationUnit == "" {
		req.Plan.DurationUnit = model.SubscriptionDurationMonth
	}
	if req.Plan.DurationValue <= 0 && req.Plan.DurationUnit != model.SubscriptionDurationCustom {
		req.Plan.DurationValue = 1
	}
	if req.Plan.MaxPurchasePerUser < 0 {
		common.ApiErrorMsg(c, "购买上限不能为负数")
		return
	}
	if req.Plan.TotalAmount < 0 {
		common.ApiErrorMsg(c, "总额度不能为负数")
		return
	}
	req.Plan.UpgradeGroup = strings.TrimSpace(req.Plan.UpgradeGroup)
	if req.Plan.UpgradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.UpgradeGroup]; !ok {
			common.ApiErrorMsg(c, "升级分组不存在")
			return
		}
	}
	req.Plan.DowngradeGroup = strings.TrimSpace(req.Plan.DowngradeGroup)
	if req.Plan.DowngradeGroup != "" {
		common.ApiErrorMsg(c, "新套餐不再支持降级分组；订阅到期只撤销该订阅授予的临时分组")
		return
	}
	req.Plan.QuotaResetPeriod = model.NormalizeResetPeriod(req.Plan.QuotaResetPeriod)
	if req.Plan.QuotaResetPeriod == model.SubscriptionResetCustom && req.Plan.QuotaResetCustomSeconds <= 0 {
		common.ApiErrorMsg(c, "自定义重置周期需大于0秒")
		return
	}
	err := model.DB.Create(&req.Plan).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(req.Plan.Id)
	common.ApiSuccess(c, req.Plan)
}

func AdminUpdateSubscriptionPlan(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminUpsertSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if strings.TrimSpace(req.Plan.Title) == "" {
		common.ApiErrorMsg(c, "套餐标题不能为空")
		return
	}
	if req.Plan.PriceAmount < 0 {
		common.ApiErrorMsg(c, "价格不能为负数")
		return
	}
	if req.Plan.PriceAmount > 9999 {
		common.ApiErrorMsg(c, "价格不能超过9999")
		return
	}
	req.Plan.Id = id
	var existing model.SubscriptionPlan
	if err := model.DB.First(&existing, "id = ?", id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Plan.Currency == "" {
		req.Plan.Currency = "USD"
	}
	req.Plan.Currency = "USD"
	if req.Plan.DurationUnit == "" {
		req.Plan.DurationUnit = model.SubscriptionDurationMonth
	}
	if req.Plan.DurationValue <= 0 && req.Plan.DurationUnit != model.SubscriptionDurationCustom {
		req.Plan.DurationValue = 1
	}
	if req.Plan.MaxPurchasePerUser < 0 {
		common.ApiErrorMsg(c, "购买上限不能为负数")
		return
	}
	if req.Plan.TotalAmount < 0 {
		common.ApiErrorMsg(c, "总额度不能为负数")
		return
	}
	req.Plan.UpgradeGroup = strings.TrimSpace(req.Plan.UpgradeGroup)
	if req.Plan.UpgradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.UpgradeGroup]; !ok {
			common.ApiErrorMsg(c, "升级分组不存在")
			return
		}
	}
	req.Plan.DowngradeGroup = strings.TrimSpace(req.Plan.DowngradeGroup)
	existingDowngradeGroup := strings.TrimSpace(existing.DowngradeGroup)
	if req.Plan.DowngradeGroup != "" && req.Plan.DowngradeGroup != existingDowngradeGroup {
		common.ApiErrorMsg(c, "旧降级分组只能保留原值或清除，不能设置新的目标")
		return
	}
	req.Plan.QuotaResetPeriod = model.NormalizeResetPeriod(req.Plan.QuotaResetPeriod)
	if req.Plan.QuotaResetPeriod == model.SubscriptionResetCustom && req.Plan.QuotaResetCustomSeconds <= 0 {
		common.ApiErrorMsg(c, "自定义重置周期需大于0秒")
		return
	}

	err := model.DB.Transaction(func(tx *gorm.DB) error {
		// update plan (allow zero values updates with map)
		updateMap := map[string]any{
			"title":                      req.Plan.Title,
			"subtitle":                   req.Plan.Subtitle,
			"price_amount":               req.Plan.PriceAmount,
			"currency":                   req.Plan.Currency,
			"duration_unit":              req.Plan.DurationUnit,
			"duration_value":             req.Plan.DurationValue,
			"custom_seconds":             req.Plan.CustomSeconds,
			"enabled":                    req.Plan.Enabled,
			"sort_order":                 req.Plan.SortOrder,
			"purchasable":                req.Plan.IsPurchasable(),
			"stripe_price_id":            req.Plan.StripePriceId,
			"creem_product_id":           req.Plan.CreemProductId,
			"waffo_pancake_product_id":   req.Plan.WaffoPancakeProductId,
			"max_purchase_per_user":      req.Plan.MaxPurchasePerUser,
			"total_amount":               req.Plan.TotalAmount,
			"upgrade_group":              req.Plan.UpgradeGroup,
			"downgrade_group":            req.Plan.DowngradeGroup,
			"quota_reset_period":         req.Plan.QuotaResetPeriod,
			"quota_reset_custom_seconds": req.Plan.QuotaResetCustomSeconds,
			"updated_at":                 common.GetTimestamp(),
		}
		if req.Plan.AllowBalancePay != nil {
			updateMap["allow_balance_pay"] = *req.Plan.AllowBalancePay
		}
		if req.Plan.AllowWalletOverflow != nil {
			updateMap["allow_wallet_overflow"] = *req.Plan.AllowWalletOverflow
		}
		if err := tx.Model(&model.SubscriptionPlan{}).Where("id = ?", id).Updates(updateMap).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(id)
	common.ApiSuccess(c, nil)
}

type AdminUpdateSubscriptionPlanStatusRequest struct {
	Enabled *bool `json:"enabled"`
}

func AdminUpdateSubscriptionPlanStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminUpdateSubscriptionPlanStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := model.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", id).Update("enabled", *req.Enabled).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(id)
	common.ApiSuccess(c, nil)
}

type AdminBindSubscriptionRequest struct {
	UserId     int    `json:"user_id"`
	PlanId     int    `json:"plan_id"`
	SourceNote string `json:"source_note"`
}

func getManageableSubscriptionUser(c *gin.Context, userId int) (*model.User, bool) {
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return nil, false
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorMsg(c, "无权管理同级或更高权限用户的订阅")
		return nil, false
	}
	return user, true
}

func getManageableSubscriptionById(c *gin.Context, userSubscriptionId int) (int, bool) {
	userId, err := model.GetUserSubscriptionUserId(userSubscriptionId)
	if err != nil {
		common.ApiError(c, err)
		return 0, false
	}
	if _, ok := getManageableSubscriptionUser(c, userId); !ok {
		return 0, false
	}
	return userId, true
}

func AdminBindSubscription(c *gin.Context) {
	var req AdminBindSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.UserId <= 0 || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if _, ok := getManageableSubscriptionUser(c, req.UserId); !ok {
		return
	}
	msg, err := model.AdminBindSubscription(req.UserId, req.PlanId, req.SourceNote)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, req.UserId, "subscription.user_assign", map[string]interface{}{
		"plan_id":     req.PlanId,
		"source_note": strings.TrimSpace(req.SourceNote),
	})
	if msg != "" {
		common.ApiSuccess(c, gin.H{"message": msg})
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- Admin: user subscription management ----

func AdminListUserSubscriptions(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	if _, ok := getManageableSubscriptionUser(c, userId); !ok {
		return
	}
	subs, err := model.GetAllUserSubscriptions(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, subs)
}

type AdminCreateUserSubscriptionRequest struct {
	PlanId     int    `json:"plan_id"`
	SourceNote string `json:"source_note"`
}

type AdminResetSubscriptionRequest struct {
	PlanId           int   `json:"plan_id"`
	AdvanceResetTime *bool `json:"advance_reset_time"`
}

func resolveAdvanceResetTime(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func recordSubscriptionResetUserLogs(c *gin.Context, result *model.SubscriptionResetResult, adminInfo *model.AuditAdminInfo) {
	if result == nil || result.ResetCount == 0 {
		return
	}
	content := fmt.Sprintf("管理员重置订阅套餐 %s（ID: %d）额度", result.PlanTitle, result.PlanId)
	for _, userId := range result.AffectedUserIds {
		model.RecordLogWithAdminInfo(userId, model.LogTypeManage, content, adminInfo, nil, c)
	}
}

// AdminCreateUserSubscription creates a new user subscription from a plan (no payment).
func AdminCreateUserSubscription(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	if _, ok := getManageableSubscriptionUser(c, userId); !ok {
		return
	}
	var req AdminCreateUserSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	msg, err := model.AdminBindSubscription(userId, req.PlanId, req.SourceNote)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, userId, "subscription.user_assign", map[string]interface{}{
		"plan_id":     req.PlanId,
		"source_note": strings.TrimSpace(req.SourceNote),
	})
	if msg != "" {
		common.ApiSuccess(c, gin.H{"message": msg})
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminResetUserSubscriptionsByPlan(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	if _, ok := getManageableSubscriptionUser(c, userId); !ok {
		return
	}
	var req AdminResetSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)
	result, err := model.AdminResetUserSubscriptionsByPlan(userId, req.PlanId, advanceResetTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordSubscriptionResetUserLogs(c, result, auditOperatorInfo(c))
	recordManageAuditFor(c, userId, "subscription.user_plan_reset", map[string]any{
		"target_user_id":     userId,
		"plan_id":            result.PlanId,
		"plan_title":         result.PlanTitle,
		"reset_count":        result.ResetCount,
		"user_count":         result.UserCount,
		"advance_reset_time": result.AdvanceResetTime,
	})
	common.ApiSuccess(c, result)
}

func AdminResetPlanSubscriptions(c *gin.Context) {
	planId, _ := strconv.Atoi(c.Param("id"))
	if planId <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminResetSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)
	result, err := model.AdminResetPlanSubscriptions(planId, advanceResetTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordSubscriptionResetUserLogs(c, result, auditOperatorInfo(c))
	common.SysLog(fmt.Sprintf("admin reset subscription plan %d quota: reset_count=%d user_count=%d advance_reset_time=%t",
		result.PlanId, result.ResetCount, result.UserCount, result.AdvanceResetTime))
	recordManageAudit(c, "subscription.plan_reset", map[string]any{
		"plan_id":            result.PlanId,
		"plan_title":         result.PlanTitle,
		"reset_count":        result.ResetCount,
		"user_count":         result.UserCount,
		"advance_reset_time": result.AdvanceResetTime,
	})
	common.ApiSuccess(c, result)
}

// AdminInvalidateUserSubscription cancels a user subscription immediately.
func AdminInvalidateUserSubscription(c *gin.Context) {
	subId, _ := strconv.Atoi(c.Param("id"))
	if subId <= 0 {
		common.ApiErrorMsg(c, "无效的订阅ID")
		return
	}
	if _, ok := getManageableSubscriptionById(c, subId); !ok {
		return
	}
	msg, err := model.AdminInvalidateUserSubscription(subId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if msg != "" {
		common.ApiSuccess(c, gin.H{"message": msg})
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminDeleteUserSubscription hard-deletes a user subscription.
func AdminDeleteUserSubscription(c *gin.Context) {
	subId, _ := strconv.Atoi(c.Param("id"))
	if subId <= 0 {
		common.ApiErrorMsg(c, "无效的订阅ID")
		return
	}
	if _, ok := getManageableSubscriptionById(c, subId); !ok {
		return
	}
	msg, err := model.AdminDeleteUserSubscription(subId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if msg != "" {
		common.ApiSuccess(c, gin.H{"message": msg})
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- Admin: batch user subscriptions ----

type AdminBatchSubscriptionAssignRequest struct {
	UserIds    []int  `json:"user_ids"`
	PlanId     int    `json:"plan_id"`
	SourceNote string `json:"source_note"`
}

type AdminBatchSubscriptionPlanRequest struct {
	UserIds          []int  `json:"user_ids"`
	PlanId           int    `json:"plan_id"`
	Action           string `json:"action"` // invalidate (default) | delete
	AdvanceResetTime *bool  `json:"advance_reset_time"`
}

// applyUserBatchSubscriptionAction runs one subscription action per selected
// user, skipping users that cannot be managed or have no matching active
// subscription. It returns how many users were affected and the total number of
// subscriptions touched, mirroring the user batch policy/route reports.
func applyUserBatchSubscriptionAction(c *gin.Context, ids []int, apply func(user *model.User) (int, error)) (updated int, affected int, skipped []userBatchSkip) {
	myRole := c.GetInt("role")
	skipped = make([]userBatchSkip, 0)
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
		count, err := apply(user)
		if err != nil {
			skipped = append(skipped, userBatchSkip{Id: id, Username: user.Username, Reason: err.Error()})
			continue
		}
		if count <= 0 {
			skipped = append(skipped, userBatchSkip{Id: id, Username: user.Username, Reason: "无匹配的活跃订阅"})
			continue
		}
		updated++
		affected += count
	}
	return updated, affected, skipped
}

// AdminBatchAssignUserSubscriptions grants one plan to every selected user.
func AdminBatchAssignUserSubscriptions(c *gin.Context) {
	var req AdminBatchSubscriptionAssignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := normalizeUserBatchIds(req.UserIds)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用，不能手动分配")
		return
	}
	sourceNote := strings.TrimSpace(req.SourceNote)

	updated, _, skipped := applyUserBatchSubscriptionAction(c, ids, func(user *model.User) (int, error) {
		if _, err := model.AdminBindSubscription(user.Id, req.PlanId, sourceNote); err != nil {
			return 0, err
		}
		return 1, nil
	})
	recordManageAudit(c, "subscription.batch_assign", map[string]interface{}{
		"targets":         len(ids),
		"target_user_ids": ids,
		"plan_id":         req.PlanId,
		"updated":         updated,
		"skipped":         len(skipped),
	})
	common.ApiSuccess(c, gin.H{"updated": updated, "skipped": skipped})
}

// AdminBatchRevokeUserSubscriptions cancels or hard-deletes each selected user's
// active subscriptions for the plan. Cancelling (the default) keeps the billing
// history; deleting removes the records entirely.
func AdminBatchRevokeUserSubscriptions(c *gin.Context) {
	var req AdminBatchSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := normalizeUserBatchIds(req.UserIds)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = "invalidate"
	}
	if action != "invalidate" && action != "delete" {
		common.ApiErrorMsg(c, "无效的批量操作类型")
		return
	}

	updated, revoked, skipped := applyUserBatchSubscriptionAction(c, ids, func(user *model.User) (int, error) {
		if action == "delete" {
			return model.AdminDeleteUserPlanSubscriptions(user.Id, req.PlanId)
		}
		return model.AdminRevokeUserPlanSubscriptions(user.Id, req.PlanId)
	})
	recordManageAudit(c, "subscription.batch_revoke", map[string]interface{}{
		"targets":         len(ids),
		"target_user_ids": ids,
		"plan_id":         req.PlanId,
		"action":          action,
		"updated":         updated,
		"revoked":         revoked,
		"skipped":         len(skipped),
	})
	common.ApiSuccess(c, gin.H{"updated": updated, "revoked": revoked, "skipped": skipped})
}

// AdminBatchResetUserSubscriptions resets the selected users' usage for the plan.
func AdminBatchResetUserSubscriptions(c *gin.Context) {
	var req AdminBatchSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := normalizeUserBatchIds(req.UserIds)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if _, err := model.GetSubscriptionPlanById(req.PlanId); err != nil {
		common.ApiError(c, err)
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)

	updated, resetCount, skipped := applyUserBatchSubscriptionAction(c, ids, func(user *model.User) (int, error) {
		result, err := model.AdminResetUserSubscriptionsByPlan(user.Id, req.PlanId, advanceResetTime)
		if err != nil {
			return 0, err
		}
		return result.ResetCount, nil
	})
	recordManageAudit(c, "subscription.batch_reset", map[string]interface{}{
		"targets":            len(ids),
		"target_user_ids":    ids,
		"plan_id":            req.PlanId,
		"advance_reset_time": advanceResetTime,
		"updated":            updated,
		"reset_count":        resetCount,
		"skipped":            len(skipped),
	})
	common.ApiSuccess(c, gin.H{"updated": updated, "reset_count": resetCount, "skipped": skipped})
}
