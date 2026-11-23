package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Redemption struct {
	Id                      int            `json:"id"`
	UserId                  int            `json:"user_id"`
	Key                     string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	KeyHash                 string         `json:"-" gorm:"type:char(64);index"`
	Type                    string         `json:"type" gorm:"type:varchar(24);not null;default:'balance';index:idx_redemption_rate_user,priority:1"`
	BatchId                 int            `json:"batch_id" gorm:"index"`
	Status                  int            `json:"status" gorm:"default:1;index:idx_redemption_rate_user,priority:2"`
	Name                    string         `json:"name" gorm:"index"`
	Quota                   int            `json:"quota" gorm:"default:100"`
	PlanSnapshot            string         `json:"plan_snapshot,omitempty" gorm:"type:text"`
	RPMDelta                int            `json:"rpm_delta" gorm:"default:0"`
	ConcurrencyDelta        int            `json:"concurrency_delta" gorm:"default:0"`
	ValiditySeconds         int64          `json:"validity_seconds" gorm:"bigint;default:0"`
	TargetGroup             string         `json:"target_group,omitempty" gorm:"type:varchar(64);default:''"`
	InitialRPMLimit         *int           `json:"initial_rpm_limit,omitempty"`
	InitialConcurrencyLimit *int           `json:"initial_concurrency_limit,omitempty"`
	CreatedTime             int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime            int64          `json:"redeemed_time" gorm:"bigint"`
	Count                   int            `json:"count" gorm:"-:all"` // only for api request
	UsedUserId              int            `json:"used_user_id" gorm:"index:idx_redemption_rate_user,priority:3"`
	DeletedAt               gorm.DeletedAt `gorm:"index"`
	ExpiredTime             int64          `json:"expired_time" gorm:"bigint"` // 过期时间，0 表示不过期
}

const (
	RedemptionTypeBalance      = "balance"
	RedemptionTypeSubscription = "subscription"
	RedemptionTypeRateLimit    = "rate_limit"
	RedemptionTypeRegistration = "registration"
)

func RedemptionCodeDigest(key string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(digest[:])
}

func (redemption *Redemption) BeforeCreate(tx *gorm.DB) error {
	if redemption.Type == "" {
		redemption.Type = RedemptionTypeBalance
	}
	if redemption.Type == RedemptionTypeRegistration && strings.TrimSpace(redemption.Key) == "" {
		return errors.New("registration code cannot be empty")
	}
	if redemption.Key != "" && redemption.KeyHash == "" {
		redemption.KeyHash = RedemptionCodeDigest(redemption.Key)
	}
	return nil
}

type RedemptionResult struct {
	Type              string `json:"type"`
	Quota             int    `json:"quota,omitempty"`
	PlanTitle         string `json:"plan_title,omitempty"`
	SubscriptionEndAt int64  `json:"subscription_end_at,omitempty"`
	RPMDelta          int    `json:"rpm_delta,omitempty"`
	ConcurrencyDelta  int    `json:"concurrency_delta,omitempty"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
}

type RedemptionSortOptions struct {
	SortBy    string
	SortOrder string
}

type RedemptionFilters struct {
	Type    string
	BatchID int
}

var redemptionSortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"status":        "status",
	"quota":         "quota",
	"created_time":  "created_time",
	"expired_time":  "expired_time",
	"used_user_id":  "used_user_id",
	"redeemed_time": "redeemed_time",
}

var ErrRedemptionAlreadyUsed = errors.New("redemption code has already been used")

func NewRedemptionSortOptions(sortBy string, sortOrder string) RedemptionSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := redemptionSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = "id"
		normalizedSortOrder = "desc"
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}
	return RedemptionSortOptions{SortBy: normalizedSortBy, SortOrder: normalizedSortOrder}
}

func (options RedemptionSortOptions) Apply(query *gorm.DB) *gorm.DB {
	columnName, ok := redemptionSortColumns[options.SortBy]
	if !ok {
		columnName = "id"
	}
	query = query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: columnName},
		Desc:   options.SortOrder != "asc",
	})
	if columnName != "id" {
		query = query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return query
}

func resolveRedemptionSortOptions(sortOptions []RedemptionSortOptions) RedemptionSortOptions {
	if len(sortOptions) == 0 {
		return NewRedemptionSortOptions("", "")
	}
	return sortOptions[0]
}

func GetAllRedemptions(startIdx int, num int, sortOptions ...RedemptionSortOptions) (redemptions []*Redemption, total int64, err error) {
	return GetAllRedemptionsWithFilters(startIdx, num, RedemptionFilters{}, sortOptions...)
}

func GetAllRedemptionsWithFilters(startIdx int, num int, filters RedemptionFilters, sortOptions ...RedemptionSortOptions) (redemptions []*Redemption, total int64, err error) {
	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := applyRedemptionFilters(tx.Model(&Redemption{}), filters)
	// 获取总数
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = resolveRedemptionSortOptions(sortOptions).Apply(query).Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func SearchRedemptions(keyword string, status string, startIdx int, num int, sortOptions ...RedemptionSortOptions) (redemptions []*Redemption, total int64, err error) {
	return SearchRedemptionsWithFilters(keyword, status, startIdx, num, RedemptionFilters{}, sortOptions...)
}

func SearchRedemptionsWithFilters(keyword string, status string, startIdx int, num int, filters RedemptionFilters, sortOptions ...RedemptionSortOptions) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := applyRedemptionFilters(tx.Model(&Redemption{}), filters)

	if keyword != "" {
		if id, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("id = ? OR name LIKE ?", id, keyword+"%")
		} else {
			query = query.Where("name LIKE ?", keyword+"%")
		}
	}

	if status != "" {
		now := common.GetTimestamp()
		switch status {
		case "expired":
			query = query.Where(
				"status = ? AND expired_time != 0 AND expired_time < ?",
				common.RedemptionCodeStatusEnabled,
				now,
			)
		case strconv.Itoa(common.RedemptionCodeStatusEnabled):
			query = query.Where(
				"status = ? AND (expired_time = 0 OR expired_time >= ?)",
				common.RedemptionCodeStatusEnabled,
				now,
			)
		case strconv.Itoa(common.RedemptionCodeStatusDisabled):
			query = query.Where("status = ?", common.RedemptionCodeStatusDisabled)
		case strconv.Itoa(common.RedemptionCodeStatusUsed):
			query = query.Where("status = ?", common.RedemptionCodeStatusUsed)
		}
	}

	// Get total count
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated data
	err = resolveRedemptionSortOptions(sortOptions).Apply(query).Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func applyRedemptionFilters(query *gorm.DB, filters RedemptionFilters) *gorm.DB {
	if filters.Type != "" {
		query = query.Where("type = ?", filters.Type)
	}
	if filters.BatchID > 0 {
		query = query.Where("batch_id = ?", filters.BatchID)
	}
	return query
}

func GetRedemptionById(id int) (*Redemption, error) {
	if id == 0 {
		return nil, common.NewMessage("ID is empty")
	}
	redemption := Redemption{Id: id}
	var err error = nil
	err = DB.First(&redemption, "id = ?", id).Error
	return &redemption, err
}

func Redeem(key string, userId int) (quota int, err error) {
	result, err := redeemRedemptionCode(key, userId, RedemptionTypeBalance)
	if err != nil {
		return 0, err
	}
	if result.Type != RedemptionTypeBalance {
		return 0, ErrRedeemFailed
	}
	return result.Quota, nil
}

func RedeemRedemptionCode(key string, userId int) (result RedemptionResult, err error) {
	return redeemRedemptionCode(key, userId)
}

func redeemRedemptionCode(key string, userId int, allowedTypes ...string) (result RedemptionResult, err error) {
	if strings.TrimSpace(key) == "" {
		return RedemptionResult{}, errors.New("redemption code not provided")
	}
	if userId == 0 {
		return RedemptionResult{}, errors.New("invalid user id")
	}
	redemption := &Redemption{}
	keyDigest := RedemptionCodeDigest(key)
	common.RandomSleep()
	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where("key_hash = ?", keyDigest).First(redemption).Error
		if err != nil {
			return errors.New("invalid redemption code")
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return errors.New("redemption code already used")
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
			return errors.New("redemption code expired")
		}
		if redemption.Type == RedemptionTypeRegistration {
			return errors.New("registration code cannot be redeemed from wallet")
		}
		if redemption.Type != RedemptionTypeBalance && redemption.Type != RedemptionTypeSubscription && redemption.Type != RedemptionTypeRateLimit {
			return errors.New("unsupported redemption code type")
		}
		if len(allowedTypes) > 0 && !slices.Contains(allowedTypes, redemption.Type) {
			return errors.New("unsupported redemption code type")
		}
		if redemption.Type == RedemptionTypeRateLimit {
			if err := validateRateLimitRedemptionWithTx(tx, redemption, userId); err != nil {
				return err
			}
		}
		redeemedAt := common.GetTimestamp()
		var plan *SubscriptionPlan
		if redemption.Type == RedemptionTypeSubscription {
			plan = &SubscriptionPlan{}
			if err := common.UnmarshalJsonStr(redemption.PlanSnapshot, plan); err != nil || plan.Id <= 0 {
				return errors.New("invalid subscription snapshot")
			}
		}
		// Compare-and-swap on status so only one concurrent transaction can
		// grant an entitlement when the database does not provide row locks.
		updateResult := tx.Model(&Redemption{}).
			Where("id = ? AND status = ?", redemption.Id, common.RedemptionCodeStatusEnabled).
			Updates(map[string]any{
				"redeemed_time": redeemedAt,
				"status":        common.RedemptionCodeStatusUsed,
				"used_user_id":  userId,
			})
		if updateResult.Error != nil {
			return updateResult.Error
		}
		if updateResult.RowsAffected == 0 {
			return errors.New("redemption code already used")
		}
		switch redemption.Type {
		case RedemptionTypeBalance:
			if err := creditGiftQuota(tx, userId, redemption.Quota); err != nil {
				return err
			}
			result = RedemptionResult{Type: redemption.Type, Quota: redemption.Quota}
		case RedemptionTypeSubscription:
			subscription, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, "redemption")
			if err != nil {
				return err
			}
			result = RedemptionResult{Type: redemption.Type, PlanTitle: plan.Title, SubscriptionEndAt: subscription.EndTime}
		case RedemptionTypeRateLimit:
			if _, err := IncrementUserPolicyVersionWithTx(tx, userId); err != nil {
				return err
			}
			result = RedemptionResult{
				Type: redemption.Type, RPMDelta: redemption.RPMDelta,
				ConcurrencyDelta: redemption.ConcurrencyDelta,
				ExpiresAt:        redeemedAt + redemption.ValiditySeconds,
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrQuotaCapExceeded) {
			return RedemptionResult{}, err
		}
		common.SysError("redemption failed: " + err.Error())
		return RedemptionResult{}, ErrRedeemFailed
	}
	switch redemption.Type {
	case RedemptionTypeBalance:
		syncCreditUserQuotaCache(userId, redemption.Quota, common.LogText("redemption"))
		RecordLog(userId, LogTypeTopup, common.NewMessage("Topped up {{quota}} with a redemption code, code ID {{id}}", map[string]any{"quota": logger.FormatQuota(redemption.Quota), "id": redemption.Id}))
	case RedemptionTypeSubscription:
		refreshSubscriptionUserGroupCache(userId, "redemption subscription")
	case RedemptionTypeRateLimit:
		if err := PublishUserAuthCache(userId); err != nil {
			common.SysError("failed to refresh user rate-limit cache after redemption")
		}
	}
	return result, nil
}

func (redemption *Redemption) Insert() error {
	if redemption.Quota <= 0 {
		return errors.New("redemption quota must be positive")
	}
	if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
		return err
	}
	var err error
	err = DB.Create(redemption).Error
	return err
}

func (redemption *Redemption) SelectUpdate() error {
	if redemption == nil {
		return errors.New("redemption is nil")
	}
	return UpdateRedemptionStatus(redemption.Id, redemption.Status)
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (redemption *Redemption) Update() error {
	fields := []string{"name", "expired_time"}
	if redemption.Type == RedemptionTypeBalance {
		if redemption.Quota <= 0 {
			return errors.New("redemption quota must be positive")
		}
		if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
			return err
		}
		fields = append(fields, "quota")
	}
	return DB.Model(redemption).Select(fields).Updates(redemption).Error
}

func UpdateRedemptionStatus(id, status int) error {
	if id <= 0 || (status != common.RedemptionCodeStatusEnabled && status != common.RedemptionCodeStatusDisabled) {
		return errors.New("invalid redemption status update")
	}
	result := DB.Model(&Redemption{}).
		Where("id = ? AND status IN ?", id, []int{
			common.RedemptionCodeStatusEnabled,
			common.RedemptionCodeStatusDisabled,
		}).
		Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var current Redemption
		if err := DB.Select("status").First(&current, id).Error; err != nil {
			return err
		}
		if current.Status == status {
			return nil
		}
		if current.Status == common.RedemptionCodeStatusUsed {
			return ErrRedemptionAlreadyUsed
		}
		return errors.New("redemption status changed concurrently")
	}
	return nil
}

func (redemption *Redemption) Delete() error {
	var err error
	err = DB.Delete(redemption).Error
	return err
}

func DeleteRedemptionById(id int) (err error) {
	if id == 0 {
		return common.NewMessage("ID is empty")
	}
	redemption := Redemption{Id: id}
	err = DB.Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete()
}

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}

// BatchDeleteRedemptions soft-deletes the selected codes in one statement.
func BatchDeleteRedemptions(ids []int) (int64, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return 0, errors.New("select between 1 and 1000 redemption codes")
	}
	for _, id := range ids {
		if id <= 0 {
			return 0, errors.New("redemption IDs must be positive")
		}
	}
	result := DB.Where("id IN ?", ids).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}
