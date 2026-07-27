package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type RankingQuotaTotal struct {
	ModelName   string `json:"model_name"`
	ModelScope  int    `json:"-"`
	TotalTokens int64  `json:"total_tokens"`
	TotalQuota  int64  `json:"total_quota"`
}

type RankingQuotaBucket struct {
	ModelName  string `json:"model_name"`
	ModelScope int    `json:"-"`
	Bucket     int64  `json:"bucket"`
	Tokens     int64  `json:"tokens"`
	Quota      int64  `json:"quota"`
}

// RankingUserQuotaRow is the user/group-level aggregate used by the rankings
// service. Hidden model provenance is collapsed by sanitizeRankingUserQuotaRows
// before this type leaves the model package.
type RankingUserQuotaRow struct {
	UserID      int    `json:"-"`
	Username    string `json:"-"`
	UseGroup    string `json:"-"`
	ModelName   string `json:"-"`
	ModelScope  int    `json:"-"`
	HiddenModel bool   `json:"-"`
	TotalTokens int64  `json:"total_tokens"`
	TotalQuota  int64  `json:"total_quota"`
}

func GetRankingQuotaTotals(startTime int64, endTime int64, visibleModelNames []string, canViewPrivate bool) ([]RankingQuotaTotal, error) {
	var legacyRows []RankingQuotaTotal
	query := DB.Table("quota_data").
		Select("model_name, sum(token_used) as total_tokens, sum(quota) as total_quota").
		Group("model_name").
		Having("sum(token_used) > 0 OR sum(quota) > 0").
		Order("total_tokens DESC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&legacyRows).Error
	if err != nil {
		return nil, err
	}
	var scopedRows []RankingQuotaTotal
	query = DB.Model(&ScopedQuotaData{}).
		Select("model_name, model_scope, sum(token_used) as total_tokens, sum(quota) as total_quota").
		Group("model_name, model_scope").
		Having("sum(token_used) > 0 OR sum(quota) > 0").
		Order("total_tokens DESC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err = query.Find(&scopedRows).Error
	if err != nil {
		return nil, err
	}
	return sanitizeRankingQuotaTotals(append(legacyRows, scopedRows...), visibleModelNames, canViewPrivate), nil
}

func GetRankingQuotaBuckets(startTime int64, endTime int64, bucketSize int64, bucketAnchor int64, visibleModelNames []string, canViewPrivate bool) ([]RankingQuotaBucket, error) {
	if bucketSize <= 0 {
		bucketSize = 3600
	}
	bucketExpr := rankingBucketExpr(bucketSize, bucketAnchor)
	var legacyRows []RankingQuotaBucket
	query := DB.Table("quota_data").
		Select(fmt.Sprintf("model_name, %s as bucket, sum(token_used) as tokens, sum(quota) as quota", bucketExpr)).
		Group(fmt.Sprintf("model_name, %s", bucketExpr)).
		Having("sum(token_used) > 0 OR sum(quota) > 0").
		Order("bucket ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&legacyRows).Error
	if err != nil {
		return nil, err
	}
	var scopedRows []RankingQuotaBucket
	query = DB.Model(&ScopedQuotaData{}).
		Select(fmt.Sprintf("model_name, model_scope, %s as bucket, sum(token_used) as tokens, sum(quota) as quota", bucketExpr)).
		Group(fmt.Sprintf("model_name, model_scope, %s", bucketExpr)).
		Having("sum(token_used) > 0 OR sum(quota) > 0").
		Order("bucket ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err = query.Find(&scopedRows).Error
	if err != nil {
		return nil, err
	}
	return sanitizeRankingQuotaBuckets(append(legacyRows, scopedRows...), visibleModelNames, canViewPrivate), nil
}

// GetRankingUserQuotaTotals returns user/group aggregates for a range. It
// reads both the legacy and scoped export tables so deployments can be
// upgraded without losing historical usage. For non-admin viewers, rows whose
// model provenance is not visible are marked HiddenModel and merged together.
func GetRankingUserQuotaTotals(startTime int64, endTime int64, visibleModelNames []string, canViewPrivate bool) ([]RankingUserQuotaRow, error) {
	var legacyRows []RankingUserQuotaRow
	query := DB.Table("quota_data").
		Select("user_id, username, use_group, model_name, sum(token_used) as total_tokens, sum(quota) as total_quota").
		Group("user_id, username, use_group, model_name").
		Having("sum(token_used) > 0 OR sum(quota) > 0")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	if err := query.Find(&legacyRows).Error; err != nil {
		return nil, err
	}

	var scopedRows []RankingUserQuotaRow
	query = DB.Model(&ScopedQuotaData{}).
		Select("user_id, username, use_group, model_name, model_scope, sum(token_used) as total_tokens, sum(quota) as total_quota").
		Group("user_id, username, use_group, model_name, model_scope").
		Having("sum(token_used) > 0 OR sum(quota) > 0")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	if err := query.Find(&scopedRows).Error; err != nil {
		return nil, err
	}

	return sanitizeRankingUserQuotaRows(append(legacyRows, scopedRows...), visibleModelNames, canViewPrivate), nil
}

func rankingBucketExpr(bucketSize int64, bucketAnchor int64) string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return fmt.Sprintf("FLOOR((created_at - %d) / %d) * %d + %d", bucketAnchor, bucketSize, bucketSize, bucketAnchor)
	}
	return fmt.Sprintf("((created_at - %d) / %d) * %d + %d", bucketAnchor, bucketSize, bucketSize, bucketAnchor)
}

func applyRankingQuotaTimeRange(query *gorm.DB, startTime int64, endTime int64) *gorm.DB {
	if startTime > 0 {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("created_at <= ?", endTime)
	}
	return query
}
