package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id         int    `json:"id"`
	UserID     int    `json:"user_id" gorm:"index"`
	Username   string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName  string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	ModelScope int    `json:"-" gorm:"-"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup   string `json:"use_group" gorm:"index;size:64;default:''"`
	TokenID    int    `json:"token_id" gorm:"index;default:0"`
	ChannelID  int    `json:"channel_id" gorm:"index;default:0"`
	NodeName   string `json:"node_name" gorm:"index;size:64;default:''"`
	TokenUsed  int    `json:"token_used" gorm:"default:0"`
	Count      int    `json:"count" gorm:"default:0"`
	Quota      int    `json:"quota" gorm:"default:0"`
}

// ScopedQuotaData stores quota aggregates written after model provenance was
// introduced. Keeping these rows in a separate table prevents an older
// process, whose aggregation key does not include model_scope, from merging
// routed-model data into a requested-model row during a rolling upgrade.
type ScopedQuotaData struct {
	Id         int
	UserID     int    `gorm:"index"`
	Username   string `gorm:"index:idx_qds_model_user_name,priority:2;size:64;default:''"`
	ModelName  string `gorm:"index:idx_qds_model_user_name,priority:1;size:64;default:''"`
	ModelScope int    `gorm:"not null;default:0"`
	CreatedAt  int64  `gorm:"bigint;index:idx_qds_created_at,priority:2"`
	UseGroup   string `gorm:"index;size:64;default:''"`
	TokenID    int    `gorm:"index;default:0"`
	ChannelID  int    `gorm:"index;default:0"`
	NodeName   string `gorm:"index;size:64;default:''"`
	TokenUsed  int    `gorm:"default:0"`
	Count      int    `gorm:"default:0"`
	Quota      int    `gorm:"default:0"`
}

func (ScopedQuotaData) TableName() string {
	return "quota_data_scoped"
}

func scopedQuotaData(quotaData *QuotaData) *ScopedQuotaData {
	return &ScopedQuotaData{
		UserID:     quotaData.UserID,
		Username:   quotaData.Username,
		ModelName:  quotaData.ModelName,
		ModelScope: quotaData.ModelScope,
		CreatedAt:  quotaData.CreatedAt,
		UseGroup:   quotaData.UseGroup,
		TokenID:    quotaData.TokenID,
		ChannelID:  quotaData.ChannelID,
		NodeName:   quotaData.NodeName,
		TokenUsed:  quotaData.TokenUsed,
		Count:      quotaData.Count,
		Quota:      quotaData.Quota,
	}
}

func quotaDataRows(rows []*ScopedQuotaData) []*QuotaData {
	result := make([]*QuotaData, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		result = append(result, &QuotaData{
			Id:         row.Id,
			UserID:     row.UserID,
			Username:   row.Username,
			ModelName:  row.ModelName,
			ModelScope: row.ModelScope,
			CreatedAt:  row.CreatedAt,
			UseGroup:   row.UseGroup,
			TokenID:    row.TokenID,
			ChannelID:  row.ChannelID,
			NodeName:   row.NodeName,
			TokenUsed:  row.TokenUsed,
			Count:      row.Count,
			Quota:      row.Quota,
		})
	}
	return result
}

const (
	QuotaModelScopeLegacy = iota
	QuotaModelScopeRequested
	QuotaModelScopeAdminOnly
)

type QuotaDataLogParams struct {
	UserID    int
	Username  string
	ModelName string
	// ModelScope must be explicitly set when ModelName is known to be the
	// client-facing requested model. Zero fails closed as legacy/unknown.
	ModelScope int
	Quota      int
	CreatedAt  int64
	TokenUsed  int
	UseGroup   string
	TokenID    int
	ChannelID  int
	NodeName   string
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(quotaData *QuotaData) {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%d\x00%d\x00%s\x00%d\x00%d\x00%s",
		quotaData.UserID,
		quotaData.Username,
		quotaData.ModelName,
		quotaData.ModelScope,
		quotaData.CreatedAt,
		quotaData.UseGroup,
		quotaData.TokenID,
		quotaData.ChannelID,
		quotaData.NodeName,
	)
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(params QuotaDataLogParams) {
	// 只精确到小时
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	quotaData := &QuotaData{
		UserID:     params.UserID,
		Username:   params.Username,
		ModelName:  params.ModelName,
		ModelScope: params.ModelScope,
		CreatedAt:  createdAt,
		UseGroup:   params.UseGroup,
		TokenID:    params.TokenID,
		ChannelID:  params.ChannelID,
		NodeName:   params.NodeName,
		Count:      1,
		Quota:      params.Quota,
		TokenUsed:  params.TokenUsed,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

func SaveQuotaDataCache() {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	size := len(CacheQuotaData)
	// 如果缓存中有数据，就保存到数据库中
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	for _, quotaData := range CacheQuotaData {
		scoped := scopedQuotaData(quotaData)
		quotaDataDB := &ScopedQuotaData{}
		DB.Model(&ScopedQuotaData{}).
			Where("user_id = ? and username = ? and model_name = ? and model_scope = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
				scoped.UserID, scoped.Username, scoped.ModelName, scoped.ModelScope, scoped.CreatedAt, scoped.UseGroup, scoped.TokenID, scoped.ChannelID, scoped.NodeName).
			First(quotaDataDB)
		if quotaDataDB.Id > 0 {
			//quotaDataDB.Count += quotaData.Count
			//quotaDataDB.Quota += quotaData.Quota
			//DB.Table("quota_data").Save(quotaDataDB)
			increaseQuotaData(scoped)
		} else {
			DB.Create(scoped)
		}
	}
	CacheQuotaData = make(map[string]*QuotaData)
	common.SysLog(fmt.Sprintf("保存数据看板数据成功，共保存%d条数据", size))
}

func increaseQuotaData(quotaData *ScopedQuotaData) {
	err := DB.Model(&ScopedQuotaData{}).
		Where("user_id = ? and username = ? and model_name = ? and model_scope = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.ModelScope, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
		Updates(map[string]interface{}{
			"count":      gorm.Expr("count + ?", quotaData.Count),
			"quota":      gorm.Expr("quota + ?", quotaData.Quota),
			"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
		}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("increaseQuotaData error: %s", err))
	}
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var legacyRows []*ScopedQuotaData
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&legacyRows).Error
	if err != nil {
		return nil, err
	}
	var scopedRows []*ScopedQuotaData
	err = DB.Model(&ScopedQuotaData{}).
		Select("user_id, username, model_name, model_scope, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, model_scope, created_at").
		Find(&scopedRows).Error
	if err != nil {
		return nil, err
	}
	rows := append(quotaDataRows(legacyRows), quotaDataRows(scopedRows)...)
	return sanitizeSelfQuotaData(rows, true), nil
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64, canViewPrivate bool) (quotaData []*QuotaData, err error) {
	var legacyRows []*ScopedQuotaData
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&legacyRows).Error
	if err != nil {
		return nil, err
	}
	var scopedRows []*ScopedQuotaData
	err = DB.Model(&ScopedQuotaData{}).
		Select("user_id, username, model_name, model_scope, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, model_scope, created_at").
		Find(&scopedRows).Error
	if err != nil {
		return nil, err
	}
	rows := append(quotaDataRows(legacyRows), quotaDataRows(scopedRows)...)
	return sanitizeSelfQuotaData(rows, canViewPrivate), nil
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var legacyRows []*ScopedQuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&legacyRows).Error
	if err != nil {
		return nil, err
	}
	var scopedRows []*ScopedQuotaData
	err = DB.Model(&ScopedQuotaData{}).
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&scopedRows).Error
	if err != nil {
		return nil, err
	}
	rows := append(quotaDataRows(legacyRows), quotaDataRows(scopedRows)...)
	return sanitizeSelfQuotaData(rows, true), nil
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var legacyRows []*ScopedQuotaData
	err = DB.Table("quota_data").
		Select("model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("model_name, created_at").
		Find(&legacyRows).Error
	if err != nil {
		return nil, err
	}
	var scopedRows []*ScopedQuotaData
	err = DB.Model(&ScopedQuotaData{}).
		Select("model_name, model_scope, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("model_name, model_scope, created_at").
		Find(&scopedRows).Error
	if err != nil {
		return nil, err
	}
	rows := append(quotaDataRows(legacyRows), quotaDataRows(scopedRows)...)
	return sanitizeSelfQuotaData(rows, true), nil
}
