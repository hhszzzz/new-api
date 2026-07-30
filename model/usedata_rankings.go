package model

import (
	"fmt"
	"strings"

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
// service. User rankings intentionally do not carry model provenance.
type RankingUserQuotaRow struct {
	UserID      int    `json:"-"`
	Username    string `json:"-"`
	UseGroup    string `json:"-"`
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

// GetRankingUserQuotaTotals returns attributable user/group aggregates for a
// range. User rankings are independent of model visibility because the result
// exposes no model provenance. Both export tables are read so historical usage
// remains part of the same leaderboard after an upgrade.
func GetRankingUserQuotaTotals(startTime int64, endTime int64) ([]RankingUserQuotaRow, error) {
	var legacyRows []RankingUserQuotaRow
	query := DB.Table("quota_data").
		Select("user_id, username, use_group, sum(token_used) as total_tokens, sum(quota) as total_quota").
		Group("user_id, username, use_group").
		Having("sum(token_used) > 0 OR sum(quota) > 0")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	if err := query.Find(&legacyRows).Error; err != nil {
		return nil, err
	}

	var scopedRows []RankingUserQuotaRow
	query = DB.Model(&ScopedQuotaData{}).
		Select("user_id, username, use_group, sum(token_used) as total_tokens, sum(quota) as total_quota").
		Group("user_id, username, use_group").
		Having("sum(token_used) > 0 OR sum(quota) > 0")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	if err := query.Find(&scopedRows).Error; err != nil {
		return nil, err
	}

	rows := append(legacyRows, scopedRows...)
	userIDSet := make(map[int]struct{})
	for _, row := range rows {
		if row.UserID > 0 && strings.TrimSpace(row.Username) == "" {
			userIDSet[row.UserID] = struct{}{}
		}
	}
	userIDs := make([]int, 0, len(userIDSet))
	for userID := range userIDSet {
		userIDs = append(userIDs, userID)
	}
	usernamesByID := make(map[int]string, len(userIDs))
	const userLookupBatchSize = 500
	for start := 0; start < len(userIDs); start += userLookupBatchSize {
		end := min(start+userLookupBatchSize, len(userIDs))
		var users []User
		if err := DB.Unscoped().Model(&User{}).
			Select("id", "username").
			Where("id IN ?", userIDs[start:end]).
			Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			if username := strings.TrimSpace(user.Username); username != "" {
				usernamesByID[user.Id] = username
			}
		}
	}

	attributedRows := make([]RankingUserQuotaRow, 0, len(rows))
	for _, row := range rows {
		row.Username = strings.TrimSpace(row.Username)
		if row.Username == "" {
			row.Username = usernamesByID[row.UserID]
		}
		if row.Username == "" {
			continue
		}
		attributedRows = append(attributedRows, row)
	}
	return attributedRows, nil
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
