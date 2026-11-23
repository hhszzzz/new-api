package model

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const maxRedemptionBatchSize = 100

const maxRedemptionRateValiditySeconds int64 = 10 * 365 * 24 * 60 * 60

var ErrRegistrationCodeInvalid = errors.New("registration failed")

func IsRegistrationCodeUsable(digest string) (bool, error) {
	if len(digest) != 64 || digest == RedemptionCodeDigest("") {
		return false, nil
	}
	var redemption Redemption
	err := DB.Select("type", "status", "expired_time").Where("key_hash = ?", digest).Take(&redemption).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return redemption.Type == RedemptionTypeRegistration &&
		redemption.Status == common.RedemptionCodeStatusEnabled &&
		(redemption.ExpiredTime == 0 || redemption.ExpiredTime >= common.GetTimestamp()), nil
}

type RedemptionBatch struct {
	Id                      int            `json:"id"`
	Name                    string         `json:"name" gorm:"type:varchar(64);not null"`
	Type                    string         `json:"type" gorm:"type:varchar(24);not null;index"`
	CreatedBy               int            `json:"created_by" gorm:"index"`
	CreatedAt               int64          `json:"created_at" gorm:"bigint;not null"`
	ExpiredTime             int64          `json:"expired_time" gorm:"bigint;not null;default:0"`
	Count                   int            `json:"count" gorm:"not null"`
	Quota                   int            `json:"quota,omitempty" gorm:"default:0"`
	PlanSnapshot            string         `json:"plan_snapshot,omitempty" gorm:"type:text"`
	RPMDelta                int            `json:"rpm_delta,omitempty" gorm:"default:0"`
	ConcurrencyDelta        int            `json:"concurrency_delta,omitempty" gorm:"default:0"`
	ValiditySeconds         int64          `json:"validity_seconds,omitempty" gorm:"bigint;default:0"`
	TargetGroup             string         `json:"target_group,omitempty" gorm:"type:varchar(64);default:''"`
	InitialRPMLimit         *int           `json:"initial_rpm_limit,omitempty"`
	InitialConcurrencyLimit *int           `json:"initial_concurrency_limit,omitempty"`
	DeletedAt               gorm.DeletedAt `json:"-" gorm:"index"`
}

type RedemptionBatchCreate struct {
	Name                    string
	Type                    string
	CreatedBy               int
	CreatedAt               int64
	ExpiredTime             int64
	Count                   int
	Quota                   int
	PlanSnapshot            string
	RPMDelta                int
	ConcurrencyDelta        int
	ValiditySeconds         int64
	TargetGroup             string
	InitialRPMLimit         *int
	InitialConcurrencyLimit *int
}

type RedemptionBatchStats struct {
	RedemptionBatch
	AvailableCount int `json:"available_count"`
	UsedCount      int `json:"used_count"`
	DisabledCount  int `json:"disabled_count"`
	ExpiredCount   int `json:"expired_count"`
}

type RedemptionBatchDetail struct {
	RedemptionBatchStats
	Redemptions []*Redemption `json:"redemptions"`
}

func CreateRedemptionBatch(input RedemptionBatchCreate) ([]string, int, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Type = strings.TrimSpace(input.Type)
	if input.CreatedBy <= 0 || input.Name == "" || utf8.RuneCountInString(input.Name) > 64 || input.Count < 1 || input.Count > maxRedemptionBatchSize {
		return nil, 0, errors.New("invalid redemption batch")
	}
	if input.ExpiredTime != 0 && input.ExpiredTime < common.GetTimestamp() {
		return nil, 0, errors.New("redemption batch expiration is in the past")
	}
	if err := validateRedemptionBatchType(input); err != nil {
		return nil, 0, err
	}
	batch := RedemptionBatch{
		Name: input.Name, Type: input.Type, CreatedBy: input.CreatedBy, CreatedAt: input.CreatedAt,
		ExpiredTime: input.ExpiredTime, Count: input.Count, Quota: input.Quota,
		PlanSnapshot: input.PlanSnapshot, RPMDelta: input.RPMDelta,
		ConcurrencyDelta: input.ConcurrencyDelta, ValiditySeconds: input.ValiditySeconds,
		TargetGroup: input.TargetGroup, InitialRPMLimit: input.InitialRPMLimit,
		InitialConcurrencyLimit: input.InitialConcurrencyLimit,
	}
	keys := make([]string, 0, input.Count)
	err := DB.Transaction(func(tx *gorm.DB) error {
		switch input.Type {
		case RedemptionTypeRegistration:
			if input.TargetGroup != "" {
				if err := lockRedemptionGroupWithTx(tx, input.TargetGroup); err != nil {
					return err
				}
			}
		case RedemptionTypeSubscription:
			var plan SubscriptionPlan
			if err := common.UnmarshalJsonStr(input.PlanSnapshot, &plan); err != nil {
				return errors.New("invalid subscription plan snapshot")
			}
			groupNames := make([]string, 0, 2)
			for _, groupName := range []string{plan.UpgradeGroup, plan.DowngradeGroup} {
				if groupName = strings.TrimSpace(groupName); groupName != "" {
					groupNames = append(groupNames, groupName)
				}
			}
			slices.Sort(groupNames)
			for _, groupName := range slices.Compact(groupNames) {
				if err := lockRedemptionGroupWithTx(tx, groupName); err != nil {
					return err
				}
			}
		}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		codeTx := tx.Session(&gorm.Session{Logger: tx.Logger.LogMode(gormlogger.Silent)})
		for range input.Count {
			key := common.GetUUID()
			code := Redemption{
				UserId: input.CreatedBy, Key: key, Type: input.Type, BatchId: batch.Id,
				Status: common.RedemptionCodeStatusEnabled, Name: input.Name, Quota: input.Quota,
				PlanSnapshot: input.PlanSnapshot, RPMDelta: input.RPMDelta,
				ConcurrencyDelta: input.ConcurrencyDelta, ValiditySeconds: input.ValiditySeconds,
				TargetGroup: input.TargetGroup, InitialRPMLimit: input.InitialRPMLimit,
				InitialConcurrencyLimit: input.InitialConcurrencyLimit,
				CreatedTime:             input.CreatedAt, ExpiredTime: input.ExpiredTime,
			}
			if err := codeTx.Create(&code).Error; err != nil {
				return err
			}
			if code.Type != RedemptionTypeBalance {
				if err := codeTx.Model(&Redemption{}).Where("id = ?", code.Id).Update("quota", 0).Error; err != nil {
					return err
				}
			}
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return keys, batch.Id, nil
}

func lockRedemptionGroupWithTx(tx *gorm.DB, groupName string) error {
	var group Group
	if err := lockForUpdate(tx).Where("name = ?", groupName).First(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("redemption target group does not exist")
		}
		return err
	}
	return nil
}

func MigrateRedemptionCodeMetadata() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Model(&Redemption{}).Where("type = '' OR type IS NULL").Update("type", RedemptionTypeBalance).Error; err != nil {
			return err
		}
		for {
			var rows []struct {
				Id  int
				Key string
			}
			if err := tx.Unscoped().Model(&Redemption{}).Select("id", "key").Where("key_hash = '' OR key_hash IS NULL").Order("id ASC").Limit(500).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				return nil
			}
			for _, row := range rows {
				if err := tx.Unscoped().Model(&Redemption{}).Where("id = ?", row.Id).Update("key_hash", RedemptionCodeDigest(row.Key)).Error; err != nil {
					return err
				}
			}
		}
	})
}

func validateRedemptionBatchType(input RedemptionBatchCreate) error {
	switch input.Type {
	case RedemptionTypeBalance:
		if input.Quota <= 0 {
			return errors.New("redemption quota must be positive")
		}
		return common.ValidateWalletQuota(input.Quota)
	case RedemptionTypeSubscription:
		plan := SubscriptionPlan{}
		if input.PlanSnapshot == "" || common.UnmarshalJsonStr(input.PlanSnapshot, &plan) != nil || plan.Id <= 0 {
			return errors.New("invalid subscription plan snapshot")
		}
	case RedemptionTypeRateLimit:
		if input.RPMDelta < 0 || input.ConcurrencyDelta < 0 || (input.RPMDelta == 0 && input.ConcurrencyDelta == 0) || input.ValiditySeconds <= 0 || input.ValiditySeconds > maxRedemptionRateValiditySeconds {
			return errors.New("invalid rate-limit bonus")
		}
		if int64(input.RPMDelta) > math.MaxInt32 || int64(input.ConcurrencyDelta) > math.MaxInt32 {
			return errors.New("rate-limit bonus is too large")
		}
	case RedemptionTypeRegistration:
		if len(input.TargetGroup) > 64 {
			return errors.New("registration group is too long")
		}
		if input.InitialRPMLimit != nil && (*input.InitialRPMLimit < 0 || *input.InitialRPMLimit > math.MaxInt32) {
			return errors.New("invalid initial RPM limit")
		}
		if input.InitialConcurrencyLimit != nil && (*input.InitialConcurrencyLimit < 0 || *input.InitialConcurrencyLimit > math.MaxInt32) {
			return errors.New("invalid initial concurrency limit")
		}
	default:
		return errors.New("unsupported redemption code type")
	}
	return nil
}

type redemptionBatchCodeState struct {
	BatchId     int
	Status      int
	ExpiredTime int64
}

func redemptionBatchStats(tx *gorm.DB, batches []RedemptionBatch) ([]RedemptionBatchStats, error) {
	if len(batches) == 0 {
		return []RedemptionBatchStats{}, nil
	}
	ids := make([]int, 0, len(batches))
	stats := make(map[int]*RedemptionBatchStats, len(batches))
	for _, batch := range batches {
		ids = append(ids, batch.Id)
		stats[batch.Id] = &RedemptionBatchStats{RedemptionBatch: batch}
	}
	var codes []redemptionBatchCodeState
	if err := tx.Model(&Redemption{}).Select("batch_id", "status", "expired_time").Where("batch_id IN ?", ids).Find(&codes).Error; err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	for _, code := range codes {
		item := stats[code.BatchId]
		if item == nil {
			continue
		}
		switch code.Status {
		case common.RedemptionCodeStatusUsed:
			item.UsedCount++
		case common.RedemptionCodeStatusDisabled:
			item.DisabledCount++
		case common.RedemptionCodeStatusEnabled:
			if code.ExpiredTime != 0 && code.ExpiredTime < now {
				item.ExpiredCount++
			} else {
				item.AvailableCount++
			}
		}
	}
	result := make([]RedemptionBatchStats, 0, len(batches))
	for _, batch := range batches {
		result = append(result, *stats[batch.Id])
	}
	return result, nil
}

func GetRedemptionBatches(startIdx, num int, codeType string) ([]RedemptionBatchStats, int64, error) {
	query := DB.Model(&RedemptionBatch{})
	if codeType != "" {
		query = query.Where("type = ?", codeType)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var batches []RedemptionBatch
	if err := query.Order("id DESC").Limit(num).Offset(startIdx).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	stats, err := redemptionBatchStats(DB, batches)
	return stats, total, err
}

func GetRedemptionBatch(id int) (*RedemptionBatchDetail, error) {
	var batch RedemptionBatch
	if err := DB.First(&batch, id).Error; err != nil {
		return nil, err
	}
	stats, err := redemptionBatchStats(DB, []RedemptionBatch{batch})
	if err != nil {
		return nil, err
	}
	var codes []*Redemption
	if err := DB.Where("batch_id = ?", id).Order("id ASC").Find(&codes).Error; err != nil {
		return nil, err
	}
	return &RedemptionBatchDetail{RedemptionBatchStats: stats[0], Redemptions: codes}, nil
}

func ExportRedemptionBatch(id int) ([]Redemption, error) {
	var batch RedemptionBatch
	if err := DB.First(&batch, id).Error; err != nil {
		return nil, err
	}
	var codes []Redemption
	if err := DB.Where("batch_id = ?", id).Order("id ASC").Find(&codes).Error; err != nil {
		return nil, err
	}
	return codes, nil
}

func DisableUnusedRedemptionBatchCodes(id int) (int64, error) {
	if id <= 0 {
		return 0, errors.New("invalid redemption batch ID")
	}
	var count int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var batch RedemptionBatch
		if err := tx.First(&batch, id).Error; err != nil {
			return err
		}
		result := tx.Model(&Redemption{}).
			Where("batch_id = ? AND status = ?", id, common.RedemptionCodeStatusEnabled).
			Update("status", common.RedemptionCodeStatusDisabled)
		count = result.RowsAffected
		return result.Error
	})
	return count, err
}

func validateRateLimitRedemptionWithTx(tx *gorm.DB, redemption *Redemption, userID int) error {
	var user User
	if err := lockForUpdate(tx).Select("id", "rpm_limit", "concurrency_limit").First(&user, userID).Error; err != nil {
		return err
	}
	rpmBonus, concurrencyBonus, _, err := activeRedemptionRateBonuses(tx, userID, common.GetTimestamp())
	if err != nil {
		return err
	}
	if redemption.RPMDelta > 0 {
		if user.RpmLimit == nil || *user.RpmLimit <= 0 || int64(*user.RpmLimit)+int64(rpmBonus)+int64(redemption.RPMDelta) > math.MaxInt32 {
			return errors.New("user has no finite RPM limit for this bonus")
		}
	}
	if redemption.ConcurrencyDelta > 0 {
		if user.ConcurrencyLimit == nil || *user.ConcurrencyLimit <= 0 || int64(*user.ConcurrencyLimit)+int64(concurrencyBonus)+int64(redemption.ConcurrencyDelta) > math.MaxInt32 {
			return errors.New("user has no finite concurrency limit for this bonus")
		}
	}
	return nil
}

func renameRedemptionPlanSnapshotGroupReferencesWithTx(tx *gorm.DB, oldName, newName string) error {
	var codes []Redemption
	if err := tx.Where("type = ? AND status IN ? AND plan_snapshot <> ''", RedemptionTypeSubscription, []int{
		common.RedemptionCodeStatusEnabled, common.RedemptionCodeStatusDisabled,
	}).Find(&codes).Error; err != nil {
		return err
	}
	for i := range codes {
		var plan SubscriptionPlan
		if err := common.UnmarshalJsonStr(codes[i].PlanSnapshot, &plan); err != nil {
			return fmt.Errorf("decode subscription redemption %d snapshot: %w", codes[i].Id, err)
		}
		if !renameSubscriptionPlanGroupReferences(&plan, oldName, newName) {
			continue
		}
		snapshot, err := common.Marshal(plan)
		if err != nil {
			return err
		}
		if err := tx.Model(&Redemption{}).Where("id = ?", codes[i].Id).Update("plan_snapshot", string(snapshot)).Error; err != nil {
			return err
		}
	}
	var batches []RedemptionBatch
	if err := tx.Where("type = ? AND plan_snapshot <> ''", RedemptionTypeSubscription).Find(&batches).Error; err != nil {
		return err
	}
	for i := range batches {
		var plan SubscriptionPlan
		if err := common.UnmarshalJsonStr(batches[i].PlanSnapshot, &plan); err != nil {
			return fmt.Errorf("decode subscription redemption batch %d snapshot: %w", batches[i].Id, err)
		}
		if !renameSubscriptionPlanGroupReferences(&plan, oldName, newName) {
			continue
		}
		snapshot, err := common.Marshal(plan)
		if err != nil {
			return err
		}
		if err := tx.Model(&RedemptionBatch{}).Where("id = ?", batches[i].Id).Update("plan_snapshot", string(snapshot)).Error; err != nil {
			return err
		}
	}
	return nil
}

func renameSubscriptionPlanGroupReferences(plan *SubscriptionPlan, oldName, newName string) bool {
	changed := false
	if plan.UpgradeGroup == oldName {
		plan.UpgradeGroup = newName
		changed = true
	}
	if plan.DowngradeGroup == oldName {
		plan.DowngradeGroup = newName
		changed = true
	}
	return changed
}

func groupHasUnconsumedSubscriptionRedemptionSnapshotReference(tx *gorm.DB, groupName string) (bool, error) {
	var codes []Redemption
	if err := tx.Select("plan_snapshot").Where("type = ? AND status IN ? AND plan_snapshot <> ''", RedemptionTypeSubscription, []int{
		common.RedemptionCodeStatusEnabled, common.RedemptionCodeStatusDisabled,
	}).Find(&codes).Error; err != nil {
		return false, err
	}
	for _, code := range codes {
		var plan SubscriptionPlan
		if err := common.UnmarshalJsonStr(code.PlanSnapshot, &plan); err != nil {
			return false, err
		}
		if plan.UpgradeGroup == groupName || plan.DowngradeGroup == groupName {
			return true, nil
		}
	}
	return false, nil
}

// InsertWithRegistrationCodeTx consumes the registration code and creates the
// account plus its initial policy in one transaction. Only the digest crosses
// the authentication flow boundary.
func (user *User) InsertWithRegistrationCodeTx(tx *gorm.DB, digest string, inviterID int) error {
	if tx == nil || user == nil || len(digest) != 64 || digest == RedemptionCodeDigest("") {
		return ErrRegistrationCodeInvalid
	}
	var hint struct {
		Id          int
		TargetGroup string
	}
	if err := tx.Model(&Redemption{}).Select("id", "target_group").Where("key_hash = ?", digest).Take(&hint).Error; err != nil {
		return ErrRegistrationCodeInvalid
	}
	if hint.TargetGroup != "" {
		if err := lockRedemptionGroupWithTx(tx, hint.TargetGroup); err != nil {
			return ErrRegistrationCodeInvalid
		}
	}
	var redemption Redemption
	if err := lockForUpdate(tx).Where("id = ?", hint.Id).First(&redemption).Error; err != nil {
		return ErrRegistrationCodeInvalid
	}
	if redemption.Type != RedemptionTypeRegistration || redemption.Status != common.RedemptionCodeStatusEnabled ||
		redemption.TargetGroup != hint.TargetGroup ||
		(redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp()) {
		return ErrRegistrationCodeInvalid
	}
	if redemption.TargetGroup != "" {
		user.Group = redemption.TargetGroup
	} else if strings.TrimSpace(user.Group) == "" || strings.TrimSpace(user.Group) == "auto" {
		user.Group = defaultUserGroup
	}
	if redemption.InitialRPMLimit != nil {
		user.RpmLimit = common.GetPointer(*redemption.InitialRPMLimit)
	}
	if redemption.InitialConcurrencyLimit != nil {
		user.ConcurrencyLimit = common.GetPointer(*redemption.InitialConcurrencyLimit)
	}
	if err := user.InsertWithTx(tx, inviterID); err != nil {
		return err
	}
	result := tx.Model(&Redemption{}).
		Where("id = ? AND status = ?", redemption.Id, common.RedemptionCodeStatusEnabled).
		Updates(map[string]any{
			"status":        common.RedemptionCodeStatusUsed,
			"redeemed_time": common.GetTimestamp(),
			"used_user_id":  user.Id,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRegistrationCodeInvalid
	}
	return nil
}

func (user *User) InsertWithRegistrationCode(digest string, inviterID int) error {
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return user.InsertWithRegistrationCodeTx(tx, digest, inviterID)
	}); err != nil {
		return err
	}
	user.FinishInsert(inviterID)
	return nil
}

func activeRedemptionRateBonuses(tx *gorm.DB, userID int, now int64) (int, int, int64, error) {
	var grants []Redemption
	if err := tx.Unscoped().Select("rpm_delta", "concurrency_delta", "redeemed_time", "validity_seconds").
		Where("type = ? AND status = ? AND used_user_id = ? AND redeemed_time > ?",
			RedemptionTypeRateLimit, common.RedemptionCodeStatusUsed, userID, now-maxRedemptionRateValiditySeconds).
		Find(&grants).Error; err != nil {
		if policyTableMissing(err) {
			return 0, 0, 0, nil
		}
		return 0, 0, 0, err
	}
	var rpm, concurrency int64
	var nextExpiry int64
	for _, grant := range grants {
		if grant.ValiditySeconds <= 0 || grant.RedeemedTime <= 0 || grant.RedeemedTime > math.MaxInt64-grant.ValiditySeconds {
			continue
		}
		expiresAt := grant.RedeemedTime + grant.ValiditySeconds
		if expiresAt <= now {
			continue
		}
		rpm += int64(grant.RPMDelta)
		concurrency += int64(grant.ConcurrencyDelta)
		if rpm > math.MaxInt32 || concurrency > math.MaxInt32 {
			return 0, 0, 0, fmt.Errorf("active rate-limit bonus overflow")
		}
		if nextExpiry == 0 || expiresAt < nextExpiry {
			nextExpiry = expiresAt
		}
	}
	if rpm > math.MaxInt32 || concurrency > math.MaxInt32 {
		return 0, 0, 0, fmt.Errorf("active rate-limit bonus overflow")
	}
	return int(rpm), int(concurrency), nextExpiry, nil
}

func GetActiveRedemptionRateBonuses(userID int) (int, int, int64, error) {
	if userID <= 0 {
		return 0, 0, 0, errors.New("invalid user ID")
	}
	return activeRedemptionRateBonuses(DB, userID, common.GetTimestamp())
}
