package controller

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type legacyRedemptionSchema struct {
	Id           int `gorm:"primaryKey"`
	UserId       int
	Key          string `gorm:"type:char(32);uniqueIndex"`
	Status       int
	Name         string
	Quota        int
	CreatedTime  int64
	RedeemedTime int64
	UsedUserId   int
	ExpiredTime  int64
}

func (legacyRedemptionSchema) TableName() string { return "redemptions" }

func TestDeleteRedemptionBatch(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver, logDriver gorm.Dialector
			dbType := common.DatabaseTypeSQLite
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
				logDriver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
				logDSN := os.Getenv("TEST_MYSQL_LOG_DSN")
				if logDSN == "" {
					logDSN = dsn
				}
				logDriver = mysql.Open(logDSN)
				dbType = common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
				logDSN := os.Getenv("TEST_POSTGRES_LOG_DSN")
				if logDSN == "" {
					logDSN = dsn
				}
				logDriver = postgres.Open(logDSN)
				dbType = common.DatabaseTypePostgreSQL
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			logDB, err := gorm.Open(logDriver, &gorm.Config{})
			require.NoError(t, err)
			logSQL, err := logDB.DB()
			require.NoError(t, err)
			logSQL.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, logSQL.Close()) })
			previousDB, previousLogDB := model.DB, model.LOG_DB
			previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
			previousRedis := common.RedisEnabled
			model.DB, model.LOG_DB = db, logDB
			common.SetDatabaseTypes(dbType, dbType)
			common.RedisEnabled = false
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMain, previousLog)
				common.RedisEnabled = previousRedis
			})
			for _, table := range []any{
				&model.User{}, &model.UserSession{}, &model.UserGroupMembership{},
				&model.UserModelPermission{}, &model.UserModelBlock{}, &model.Redemption{}, &model.RedemptionBatch{}, &model.Group{},
			} {
				require.False(t, db.Migrator().HasTable(table), "use an empty test database")
			}
			require.False(t, db.Migrator().HasTable(&model.Redemption{}), "use an empty test database")
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.Redemption{}, &model.RedemptionBatch{}, &model.Group{}))
				require.NoError(t, model.MigrateRedemptionCodeMetadata())
			}
			assert.True(t, db.Migrator().HasIndex(&model.Redemption{}, "idx_redemption_rate_user"))
			freshCode := model.Redemption{Key: "00000000000000000000000000000090", Status: common.RedemptionCodeStatusEnabled}
			require.NoError(t, db.Create(&freshCode).Error)
			require.NoError(t, model.UpdateRedemptionStatus(freshCode.Id, common.RedemptionCodeStatusEnabled))
			require.NoError(t, model.UpdateRedemptionStatus(freshCode.Id, common.RedemptionCodeStatusDisabled))
			require.NoError(t, model.UpdateRedemptionStatus(freshCode.Id, common.RedemptionCodeStatusDisabled), "repeating a status update is idempotent")
			require.NoError(t, model.UpdateRedemptionStatus(freshCode.Id, common.RedemptionCodeStatusEnabled))
			duplicateFreshCode := model.Redemption{Key: freshCode.Key, Status: common.RedemptionCodeStatusEnabled}
			require.Error(t, db.Create(&duplicateFreshCode).Error, "fresh schema preserves redemption key uniqueness")
			require.NoError(t, db.Model(&model.Redemption{}).Where("id = ?", freshCode.Id).Update("status", common.RedemptionCodeStatusUsed).Error)
			require.ErrorIs(t, model.UpdateRedemptionStatus(freshCode.Id, common.RedemptionCodeStatusEnabled), model.ErrRedemptionAlreadyUsed)
			require.NoError(t, db.Migrator().DropTable(&model.Redemption{}, &model.RedemptionBatch{}))
			require.NoError(t, db.AutoMigrate(
				&model.User{}, &model.UserSession{}, &model.UserGroupMembership{},
				&model.UserModelPermission{}, &model.UserModelBlock{},
			))
			require.False(t, db.Migrator().HasTable(&model.Redemption{}), "start the upgrade fixture from the legacy schema")
			require.NoError(t, db.AutoMigrate(&legacyRedemptionSchema{}))
			legacy := legacyRedemptionSchema{
				UserId: 1, Key: "00000000000000000000000000000091",
				Status: common.RedemptionCodeStatusEnabled, Name: "release-schema", Quota: 250,
				CreatedTime: common.GetTimestamp(),
			}
			require.NoError(t, db.Create(&legacy).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.Redemption{}, &model.RedemptionBatch{}))
				require.NoError(t, model.MigrateRedemptionCodeMetadata())
			}
			assert.True(t, db.Migrator().HasIndex(&model.Redemption{}, "idx_redemption_rate_user"))
			var migrated model.Redemption
			require.NoError(t, db.First(&migrated, legacy.Id).Error)
			assert.Equal(t, model.RedemptionTypeBalance, migrated.Type)
			assert.Equal(t, model.RedemptionCodeDigest(legacy.Key), migrated.KeyHash)
			duplicateLegacyCode := model.Redemption{Key: legacy.Key, Status: common.RedemptionCodeStatusEnabled}
			require.Error(t, db.Create(&duplicateLegacyCode).Error, "upgrade preserves redemption key uniqueness")
			require.NoError(t, db.Unscoped().Delete(&migrated).Error)
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(
					&model.UserSession{}, &model.UserGroupMembership{}, &model.UserModelPermission{},
					&model.UserModelBlock{}, &model.User{}, &model.Redemption{}, &model.RedemptionBatch{}, &model.Group{},
				))
			})
			require.False(t, logDB.Migrator().HasTable(&model.AuditLog{}), "use an empty test log database")
			require.False(t, logDB.Migrator().HasTable(&model.Log{}), "use an empty test log database")
			require.NoError(t, logDB.AutoMigrate(&model.AuditLog{}, &model.Log{}))
			t.Cleanup(func() { require.NoError(t, logDB.Migrator().DropTable(&model.AuditLog{}, &model.Log{})) })
			admin := model.User{Username: "redemption-audit-admin", Password: "unused", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}
			require.NoError(t, db.Create(&admin).Error)
			var redemptionBatchIDs []int
			require.NoError(t, db.AutoMigrate(
				&model.UserGroupMembership{}, &model.UserModelPermission{}, &model.UserModelBlock{},
				&model.SubscriptionPlan{}, &model.UserSubscription{}, &model.Group{},
			))
			targetGroup := model.Group{
				Name: "matrix-registration-group", Description: "Matrix registration group",
				Ratio: 1, CreatedAt: common.GetTimestamp(), UpdatedAt: common.GetTimestamp(),
			}
			require.NoError(t, db.Create(&targetGroup).Error)
			subscriptionGroup := model.Group{
				Name: "matrix-subscription-group", Description: "Matrix subscription group",
				Ratio: 1, CreatedAt: common.GetTimestamp(), UpdatedAt: common.GetTimestamp(),
			}
			require.NoError(t, db.Create(&subscriptionGroup).Error)
			registrationRPM, registrationConcurrency := 50, 2
			registrationKeys, registrationBatchID, err := model.CreateRedemptionBatch(model.RedemptionBatchCreate{
				Name: "matrix-registration", Type: model.RedemptionTypeRegistration, CreatedBy: admin.Id,
				CreatedAt: common.GetTimestamp(), Count: 1,
				TargetGroup:     targetGroup.Name,
				InitialRPMLimit: &registrationRPM, InitialConcurrencyLimit: &registrationConcurrency,
			})
			require.NoError(t, err)
			redemptionBatchIDs = append(redemptionBatchIDs, registrationBatchID)
			require.Len(t, registrationKeys, 1)
			var storedRegistrationCode model.Redemption
			require.NoError(t, db.Where("batch_id = ?", registrationBatchID).Take(&storedRegistrationCode).Error)
			assert.Zero(t, storedRegistrationCode.Quota)
			newUser := model.User{
				Username: "redemption-matrix-user", Password: "matrix-password-123",
				Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
			}
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				return newUser.InsertWithRegistrationCodeTx(tx, model.RedemptionCodeDigest(registrationKeys[0]), 0)
			}))
			assert.Equal(t, targetGroup.Name, newUser.Group)
			assert.Equal(t, registrationRPM, *newUser.RpmLimit)
			assert.Equal(t, registrationConcurrency, *newUser.ConcurrencyLimit)
			quotaCap := 450
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", newUser.Id).Updates(map[string]any{
				"quota": 350, "quota_cap": quotaCap,
			}).Error)
			balanceKeys, balanceBatchID, err := model.CreateRedemptionBatch(model.RedemptionBatchCreate{
				Name: "matrix-balance", Type: model.RedemptionTypeBalance, CreatedBy: admin.Id,
				CreatedAt: common.GetTimestamp(), Count: 2, Quota: 100,
			})
			require.NoError(t, err)
			redemptionBatchIDs = append(redemptionBatchIDs, balanceBatchID)
			var storedBalanceCode model.Redemption
			require.NoError(t, db.Where("batch_id = ?", balanceBatchID).Take(&storedBalanceCode).Error)
			assert.Equal(t, 100, storedBalanceCode.Quota)
			_, err = model.RedeemRedemptionCode(balanceKeys[0], newUser.Id)
			require.NoError(t, err)
			_, err = model.RedeemRedemptionCode(balanceKeys[1], newUser.Id)
			require.ErrorIs(t, err, model.ErrQuotaCapExceeded)
			var creditedUser model.User
			require.NoError(t, db.Select("quota").First(&creditedUser, newUser.Id).Error)
			assert.Equal(t, quotaCap, creditedUser.Quota)

			rateKeys, rateBatchID, err := model.CreateRedemptionBatch(model.RedemptionBatchCreate{
				Name: "matrix-rate-limit", Type: model.RedemptionTypeRateLimit, CreatedBy: admin.Id,
				CreatedAt: common.GetTimestamp(), Count: 1, RPMDelta: 10,
				ConcurrencyDelta: 1, ValiditySeconds: 3600,
			})
			require.NoError(t, err)
			redemptionBatchIDs = append(redemptionBatchIDs, rateBatchID)
			var storedRateCode model.Redemption
			require.NoError(t, db.Where("batch_id = ?", rateBatchID).Take(&storedRateCode).Error)
			assert.Zero(t, storedRateCode.Quota, "non-balance codes do not carry a balance quota")
			_, err = model.RedeemRedemptionCode(rateKeys[0], newUser.Id)
			require.NoError(t, err)
			rpmBonus, concurrencyBonus, _, err := model.GetActiveRedemptionRateBonuses(newUser.Id)
			require.NoError(t, err)
			assert.Equal(t, 10, rpmBonus)
			assert.Equal(t, 1, concurrencyBonus)

			plan := model.SubscriptionPlan{
				Title: "matrix-plan", DurationUnit: model.SubscriptionDurationDay, DurationValue: 2,
				TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever, Enabled: true,
				UpgradeGroup: subscriptionGroup.Name,
			}
			plan.NormalizeDefaults()
			require.NoError(t, db.Create(&plan).Error)
			planSnapshot, err := common.Marshal(plan)
			require.NoError(t, err)
			subscriptionKeys, subscriptionBatchID, err := model.CreateRedemptionBatch(model.RedemptionBatchCreate{
				Name: "matrix-subscription", Type: model.RedemptionTypeSubscription, CreatedBy: admin.Id,
				CreatedAt: common.GetTimestamp(), Count: 1, PlanSnapshot: string(planSnapshot),
			})
			require.NoError(t, err)
			redemptionBatchIDs = append(redemptionBatchIDs, subscriptionBatchID)
			var storedSubscriptionCode model.Redemption
			require.NoError(t, db.Where("batch_id = ?", subscriptionBatchID).Take(&storedSubscriptionCode).Error)
			assert.Zero(t, storedSubscriptionCode.Quota)
			_, err = model.RedeemRedemptionCode(subscriptionKeys[0], newUser.Id)
			require.NoError(t, err)
			var subscription model.UserSubscription
			require.NoError(t, db.Where("user_id = ? AND plan_id = ?", newUser.Id, plan.Id).Take(&subscription).Error)
			assert.Equal(t, "redemption", subscription.Source)

			for _, batchID := range redemptionBatchIDs {
				require.NoError(t, db.Unscoped().Where("batch_id = ?", batchID).Delete(&model.Redemption{}).Error)
				require.NoError(t, db.Unscoped().Delete(&model.RedemptionBatch{}, batchID).Error)
			}
			require.NoError(t, db.Where("user_id = ?", newUser.Id).Delete(&model.UserSubscription{}).Error)
			require.NoError(t, db.Where("user_id = ?", newUser.Id).Delete(&model.UserGroupMembership{}).Error)
			require.NoError(t, db.Delete(&model.User{}, newUser.Id).Error)
			require.NoError(t, db.Delete(&model.SubscriptionPlan{}, plan.Id).Error)
			require.NoError(t, db.Delete(&subscriptionGroup).Error)
			require.NoError(t, db.Delete(&targetGroup).Error)
			_, token := createAccessTokenTestSession(t, admin.Id, "redemption-audit-session")
			codes := make([]model.Redemption, 16)
			for index := range codes {
				codes[index] = model.Redemption{Name: "selected", Key: fmt.Sprintf("%032d", index+1), Quota: 100, Status: common.RedemptionCodeStatusEnabled}
			}
			codes[1].Status = common.RedemptionCodeStatusUsed
			codes[15].Name = "unselected"
			codes[15].Status = common.RedemptionCodeStatusDisabled
			require.NoError(t, model.DB.Create(&codes).Error)

			router := gin.New()
			router.Use(middleware.RequestId())
			router.POST("/api/redemption/batch", middleware.AdminAuth(), DeleteRedemptionBatch)

			overLimit := make([]int, 1001)
			for index := range overLimit {
				overLimit[index] = codes[0].Id
			}
			oversized, err := common.Marshal(map[string]any{"ids": overLimit})
			require.NoError(t, err)
			for _, body := range []string{"{}", `{"ids":[]}`, `{"ids":null}`, `{"ids":[0]}`, `{"ids":[1,-1]}`, `{"ids":["1"]}`, "{", string(oversized)} {
				t.Run("invalid_"+body[:min(len(body), 30)], func(t *testing.T) {
					response := httptest.NewRecorder()
					request := httptest.NewRequest(http.MethodPost, "/api/redemption/batch", bytes.NewBufferString(body))
					request.Header.Set("Authorization", "Bearer "+token)
					router.ServeHTTP(response, request)
					var result struct {
						Success bool `json:"success"`
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
					assert.False(t, result.Success)
					var count int64
					require.NoError(t, model.DB.Model(&model.Redemption{}).Count(&count).Error)
					assert.EqualValues(t, 16, count)
					var events []model.AuditLog
					require.NoError(t, logDB.Where("request_id = ? AND category = ?", response.Header().Get(common.RequestIdKey), model.AuditCategoryOperation).Find(&events).Error)
					require.Len(t, events, 1)
					assert.False(t, events[0].Success)
					assert.Equal(t, "redemption.delete_batch", events[0].Action)
				})
			}
			_, err = model.BatchDeleteRedemptions(nil)
			require.Error(t, err)
			requestedIDs := make([]int, 0, 17)
			for _, code := range codes[:15] {
				requestedIDs = append(requestedIDs, code.Id)
			}
			requestedIDs = append(requestedIDs, codes[0].Id, 999999)
			payload, err := common.Marshal(map[string]any{"ids": requestedIDs})
			require.NoError(t, err)
			for _, expectedCount := range []int64{15, 0} {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/api/redemption/batch", bytes.NewReader(payload))
				request.Header.Set("Authorization", "Bearer "+token)
				router.ServeHTTP(response, request)
				assert.Equal(t, http.StatusOK, response.Code)
				var result struct {
					Success bool  `json:"success"`
					Data    int64 `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
				assert.True(t, result.Success)
				assert.Equal(t, expectedCount, result.Data)
				var events []model.AuditLog
				require.NoError(t, logDB.Where("request_id = ? AND category = ?", response.Header().Get(common.RequestIdKey), model.AuditCategoryOperation).Find(&events).Error)
				require.Len(t, events, 1, "one operation event, without a duplicate single-delete fallback")
				event := events[0]
				assert.Equal(t, "redemption.delete_batch", event.Action)
				assert.Equal(t, fmt.Sprintf("Batch deleted %d redemption codes", expectedCount), event.Content)
				assert.True(t, event.Success)
				assert.Equal(t, admin.Id, event.UserId)
				assert.Equal(t, "/api/redemption/batch", event.Route)
				require.NotNil(t, event.Other.Op)
				encoded, err := common.Marshal(event.Other.Op.Params)
				require.NoError(t, err)
				var params struct {
					Count int64 `json:"count"`
					Total int   `json:"total"`
					IDs   []int `json:"requested_redemption_ids"`
				}
				require.NoError(t, common.Unmarshal(encoded, &params))
				assert.Equal(t, expectedCount, params.Count)
				assert.Equal(t, len(requestedIDs), params.Total)
				assert.Equal(t, requestedIDs, params.IDs)
				encoded, err = common.Marshal(event)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), token)
				for _, code := range codes {
					assert.NotContains(t, string(encoded), code.Key)
				}
			}
			var active []model.Redemption
			require.NoError(t, model.DB.Find(&active).Error)
			require.Len(t, active, 1)
			assert.Equal(t, codes[15], active[0])
			var all []model.Redemption
			require.NoError(t, model.DB.Unscoped().Order("id").Find(&all).Error)
			require.Len(t, all, 16)
			for _, code := range all[:15] {
				assert.True(t, code.DeletedAt.Valid)
			}
			assert.False(t, all[15].DeletedAt.Valid)

			exportKeys, exportBatchID, err := model.CreateRedemptionBatch(model.RedemptionBatchCreate{
				Name: "＝1+1", Type: model.RedemptionTypeBalance, CreatedBy: admin.Id,
				CreatedAt: common.GetTimestamp(), Count: 1, Quota: 100,
			})
			require.NoError(t, err)
			require.Len(t, exportKeys, 1)
			exportRecorder := httptest.NewRecorder()
			exportContext, _ := gin.CreateTestContext(exportRecorder)
			exportContext.Params = gin.Params{{Key: "id", Value: strconv.Itoa(exportBatchID)}}
			exportContext.Request = httptest.NewRequest(http.MethodGet, "/api/redemption/batch/"+strconv.Itoa(exportBatchID)+"/export", nil)
			ExportRedemptionBatch(exportContext)
			assert.Equal(t, http.StatusOK, exportRecorder.Code)
			assert.Equal(t, "text/csv; charset=utf-8", exportRecorder.Header().Get("Content-Type"))
			body := exportRecorder.Body.String()
			assert.True(t, strings.HasPrefix(body, "\uFEFF"), "CSV begins with a UTF-8 BOM for spreadsheet compatibility")
			rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\uFEFF"))).ReadAll()
			require.NoError(t, err)
			require.Len(t, rows, 2)
			assert.Equal(t, []string{"id", "key", "type", "name", "status", "created_time", "expired_time", "redeemed_time", "used_user_id"}, rows[0])
			assert.Equal(t, exportKeys[0], rows[1][1])
			assert.Equal(t, "\t＝1+1", rows[1][3], "spreadsheet formulas remain inert")
		})
	}
}

func TestSafeRedemptionCSVCellPreventsFormulaInjection(t *testing.T) {
	for _, test := range []struct {
		input    string
		expected string
	}{
		{input: "=1+1", expected: "\t=1+1"},
		{input: " +SUM(A1:A2)", expected: "\t+SUM(A1:A2)"},
		{input: "\v-1+1", expected: "\t-1+1"},
		{input: "\r＠cmd", expected: "\t＠cmd"},
		{input: "＝HYPERLINK(\"https://example.com\")", expected: "\t＝HYPERLINK(\"https://example.com\")"},
	} {
		assert.Equal(t, test.expected, safeRedemptionCSVCell(test.input))
	}
	assert.Equal(t, "redemption-code", safeRedemptionCSVCell("redemption-code"))
}

func TestUpdateRedemptionCannotChangeConsumedCodeStatus(t *testing.T) {
	previousDB := model.DB
	previousMainType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousMainType)
		require.NoError(t, sqlDB.Close())
	})
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Redemption{}))
	code := model.Redemption{
		Name: "consumed", Key: "00000000000000000000000000000099",
		Type: model.RedemptionTypeRateLimit, Status: common.RedemptionCodeStatusUsed,
		UsedUserId: 7, RedeemedTime: common.GetTimestamp(), RPMDelta: 10,
		ValiditySeconds: 3600,
	}
	require.NoError(t, db.Create(&code).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	body, err := common.Marshal(map[string]int{"id": code.Id, "status": common.RedemptionCodeStatusEnabled})
	require.NoError(t, err)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/redemption/?status_only=true", bytes.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	UpdateRedemption(context)

	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	var stored model.Redemption
	require.NoError(t, db.First(&stored, code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, stored.Status)

	code.Status = common.RedemptionCodeStatusDisabled
	require.NoError(t, db.Save(&code).Error)
	recorder = httptest.NewRecorder()
	context, _ = gin.CreateTestContext(recorder)
	body, err = common.Marshal(map[string]int{
		"id": code.Id, "status": common.RedemptionCodeStatusEnabled,
	})
	require.NoError(t, err)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/redemption/?status_only=true", bytes.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	UpdateRedemption(context)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	require.NoError(t, db.First(&stored, code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, stored.Status)
}
