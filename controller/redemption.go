package controller

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

func GetAllRedemptions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	sortOptions := model.NewRedemptionSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	filters := model.RedemptionFilters{Type: c.Query("type")}
	filters.BatchID, _ = strconv.Atoi(c.Query("batch_id"))
	redemptions, total, err := model.GetAllRedemptionsWithFilters(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), filters, sortOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
}

func SearchRedemptions(c *gin.Context) {
	keyword := c.Query("keyword")
	status := c.Query("status")
	pageInfo := common.GetPageQuery(c)
	sortOptions := model.NewRedemptionSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	filters := model.RedemptionFilters{Type: c.Query("type")}
	filters.BatchID, _ = strconv.Atoi(c.Query("batch_id"))
	redemptions, total, err := model.SearchRedemptionsWithFilters(keyword, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), filters, sortOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
}

func GetRedemption(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	redemption, err := model.GetRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    redemption,
	})
	return
}

func AddRedemption(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorT(c, "Payment, redemption, subscription, and invitation reward features are disabled. The administrator must confirm compliance terms before enabling them.")
		return
	}

	var request struct {
		Name                    string `json:"name"`
		Type                    string `json:"type"`
		Count                   int    `json:"count"`
		Quota                   int    `json:"quota"`
		ExpiredTime             int64  `json:"expired_time"`
		PlanID                  int    `json:"plan_id"`
		RPMDelta                int    `json:"rpm_delta"`
		ConcurrencyDelta        int    `json:"concurrency_delta"`
		ValiditySeconds         int64  `json:"validity_seconds"`
		TargetGroup             string `json:"target_group"`
		InitialRPMLimit         *int   `json:"initial_rpm_limit"`
		InitialConcurrencyLimit *int   `json:"initial_concurrency_limit"`
	}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Type = strings.TrimSpace(request.Type)
	if request.Type == "" {
		request.Type = model.RedemptionTypeBalance
	}
	if utf8.RuneCountInString(request.Name) == 0 || utf8.RuneCountInString(request.Name) > 64 {
		common.ApiErrorT(c, "Redemption batch name length must be between 1-64")
		return
	}
	if request.Count <= 0 {
		common.ApiErrorT(c, "Redemption code count must be greater than 0")
		return
	}
	if request.Count > 100 {
		common.ApiErrorT(c, "Maximum 100 redemption codes can be generated at once")
		return
	}
	if err := validateExpiredTime(request.ExpiredTime); err != nil {
		common.ApiError(c, err)
		return
	}
	planSnapshot := ""
	if request.Type == model.RedemptionTypeSubscription {
		plan, err := model.GetSubscriptionPlanById(request.PlanID)
		if err != nil {
			common.ApiErrorT(c, "Subscription plan not found")
			return
		}
		plan.NormalizeDefaults()
		encoded, err := common.Marshal(plan)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		planSnapshot = string(encoded)
	}
	input := model.RedemptionBatchCreate{
		Name: request.Name, Type: request.Type, CreatedBy: c.GetInt("id"),
		CreatedAt: common.GetTimestamp(), ExpiredTime: request.ExpiredTime,
		Count: request.Count, Quota: request.Quota, PlanSnapshot: planSnapshot,
		RPMDelta: request.RPMDelta, ConcurrencyDelta: request.ConcurrencyDelta,
		ValiditySeconds: request.ValiditySeconds, TargetGroup: strings.TrimSpace(request.TargetGroup),
		InitialRPMLimit: request.InitialRPMLimit, InitialConcurrencyLimit: request.InitialConcurrencyLimit,
	}
	keys, batchID, err := model.CreateRedemptionBatch(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "redemption.create", map[string]any{
		"name": request.Name, "type": request.Type, "count": request.Count,
		"quota": logger.FormatQuota(request.Quota), "batch_id": batchID,
	})
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  "",
		"data":     keys,
		"batch_id": batchID,
	})
}

func DeleteRedemption(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func UpdateRedemption(c *gin.Context) {
	statusOnly := c.Query("status_only")
	redemption := model.Redemption{}
	err := c.ShouldBindJSON(&redemption)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if statusOnly != "" {
		if err := model.UpdateRedemptionStatus(redemption.Id, redemption.Status); err != nil {
			common.ApiError(c, err)
			return
		}
		updated, err := model.GetRedemptionById(redemption.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, updated)
		return
	}
	cleanRedemption, err := model.GetRedemptionById(redemption.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if redemption.Type != "" && redemption.Type != cleanRedemption.Type {
		common.ApiErrorT(c, "Redemption code type cannot be changed")
		return
	}
	if cleanRedemption.Type == model.RedemptionTypeBalance {
		if redemption.Quota <= 0 {
			common.ApiErrorT(c, "Redemption quota must be positive")
			return
		}
		if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := validateExpiredTime(redemption.ExpiredTime); err != nil {
		common.ApiError(c, err)
		return
	}
	// If you add more fields, please also update redemption.Update().
	cleanRedemption.Name = redemption.Name
	if cleanRedemption.Type == model.RedemptionTypeBalance {
		cleanRedemption.Quota = redemption.Quota
	}
	cleanRedemption.ExpiredTime = redemption.ExpiredTime
	err = cleanRedemption.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanRedemption,
	})
	return
}

func DeleteInvalidRedemption(c *gin.Context) {
	rows, err := model.DeleteInvalidRedemptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

func validateExpiredTime(expired int64) error {
	if expired != 0 && expired < common.GetTimestamp() {
		return common.NewMessage("Expiration time cannot be earlier than current time")
	}
	return nil
}

func DeleteRedemptionBatch(c *gin.Context) {
	var request struct {
		Ids []int `json:"ids" binding:"required,min=1,max=1000,dive,gt=0"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	count, err := model.BatchDeleteRedemptions(request.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "redemption.delete_batch", map[string]any{
		"count":                    count,
		"total":                    len(request.Ids),
		"requested_redemption_ids": request.Ids,
	})
	common.ApiSuccess(c, count)
}

func GetRedemptionBatches(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	batches, total, err := model.GetRedemptionBatches(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), c.Query("type"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(batches)
	common.ApiSuccess(c, pageInfo)
}

func GetRedemptionBatch(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	detail, err := model.GetRedemptionBatch(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

func ExportRedemptionBatch(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	codes, err := model.ExportRedemptionBatch(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=redemption-batch-%d.csv", id))
	if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}
	csvWriter := csv.NewWriter(c.Writer)
	if err := csvWriter.Write([]string{"id", "key", "type", "name", "status", "created_time", "expired_time", "redeemed_time", "used_user_id"}); err != nil {
		return
	}
	for _, code := range codes {
		row := []string{
			strconv.Itoa(code.Id), safeRedemptionCSVCell(code.Key), safeRedemptionCSVCell(code.Type),
			safeRedemptionCSVCell(code.Name), strconv.Itoa(code.Status),
			strconv.FormatInt(code.CreatedTime, 10), strconv.FormatInt(code.ExpiredTime, 10),
			strconv.FormatInt(code.RedeemedTime, 10), strconv.Itoa(code.UsedUserId),
		}
		if err := csvWriter.Write(row); err != nil {
			return
		}
	}
	csvWriter.Flush()
}

func safeRedemptionCSVCell(value string) string {
	formulaCandidate := strings.TrimLeftFunc(value, func(char rune) bool {
		return unicode.IsSpace(char) || unicode.IsControl(char)
	})
	if strings.HasPrefix(formulaCandidate, "=") || strings.HasPrefix(formulaCandidate, "+") ||
		strings.HasPrefix(formulaCandidate, "-") || strings.HasPrefix(formulaCandidate, "@") ||
		strings.HasPrefix(formulaCandidate, "＝") || strings.HasPrefix(formulaCandidate, "＋") ||
		strings.HasPrefix(formulaCandidate, "－") || strings.HasPrefix(formulaCandidate, "＠") {
		return "\t" + formulaCandidate
	}
	return value
}

func DisableUnusedRedemptionBatch(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	count, err := model.DisableUnusedRedemptionBatchCodes(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "redemption.batch_disable_unused", map[string]any{"batch_id": id, "count": count})
	common.ApiSuccess(c, count)
}
