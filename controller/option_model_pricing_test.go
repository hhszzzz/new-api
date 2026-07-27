package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelPricingOptionControllerTest(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousGinMode := gin.Mode()
	previousModelPrice := ratio_setting.ModelPrice2JSONString()
	previousModelRatio := ratio_setting.ModelRatio2JSONString()
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	gin.SetMode(gin.TestMode)

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
		gin.SetMode(previousGinMode)
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousModelPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatio))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func performModelPricingOptionUpdate(t *testing.T, body any) map[string]any {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/model-pricing",
		bytes.NewReader(payload),
	)

	UpdateModelPricingOptions(context)

	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func performOptionUpdate(t *testing.T, body any) map[string]any {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option", bytes.NewReader(payload))

	UpdateOption(context)

	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestUpdateModelPricingOptionsRejectsWholeInvalidBatch(t *testing.T) {
	db := setupModelPricingOptionControllerTest(t)
	previous := []model.Option{
		{Key: "ModelPrice", Value: `{"before":1}`},
		{Key: "ModelRatio", Value: `{"before":2}`},
	}
	require.NoError(t, db.Create(&previous).Error)

	response := performModelPricingOptionUpdate(t, OptionBatchUpdateRequest{
		Options: []OptionUpdateRequest{
			{Key: "ModelPrice", Value: `{"after":3}`},
			{Key: "ModelRatio", Value: `[1]`},
		},
	})

	assert.Equal(t, false, response["success"])
	var stored []model.Option
	require.NoError(t, db.Order("key ASC").Find(&stored).Error)
	assert.Equal(t, previous, stored)
}

func TestUpdateModelPricingOptionsRejectsNegativeValue(t *testing.T) {
	db := setupModelPricingOptionControllerTest(t)
	before := model.Option{Key: "ModelPrice", Value: `{"before":1}`}
	require.NoError(t, db.Create(&before).Error)

	response := performModelPricingOptionUpdate(t, OptionBatchUpdateRequest{
		Options: []OptionUpdateRequest{
			{Key: "ModelPrice", Value: `{"after":-1}`},
		},
	})

	assert.Equal(t, false, response["success"])
	var stored model.Option
	require.NoError(t, db.First(&stored, "key = ?", "ModelPrice").Error)
	assert.Equal(t, before, stored)
}

func TestUpdateOptionRejectsNegativePricingWithoutPublishing(t *testing.T) {
	db := setupModelPricingOptionControllerTest(t)
	before := model.Option{Key: ratio_setting.ModelPriceOptionKey, Value: `{"before":1}`}
	require.NoError(t, db.Create(&before).Error)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(before.Value))

	response := performOptionUpdate(t, OptionUpdateRequest{
		Key:   ratio_setting.ModelPriceOptionKey,
		Value: `{"after":-1}`,
	})

	assert.Equal(t, false, response["success"])
	var stored model.Option
	require.NoError(t, db.First(&stored, "key = ?", ratio_setting.ModelPriceOptionKey).Error)
	assert.Equal(t, before, stored)
	price, found := ratio_setting.GetModelPrice("before", false)
	assert.True(t, found)
	assert.Equal(t, 1.0, price)
	_, found = ratio_setting.GetModelPrice("after", false)
	assert.False(t, found)
}

func TestUpdateModelPricingOptionsPersistsAndPublishesBatch(t *testing.T) {
	db := setupModelPricingOptionControllerTest(t)

	response := performModelPricingOptionUpdate(t, OptionBatchUpdateRequest{
		Options: []OptionUpdateRequest{
			{Key: "ModelPrice", Value: `{"batch-price":1.25}`},
			{Key: "ModelRatio", Value: `{"batch-ratio":2}`},
		},
	})

	assert.Equal(t, true, response["success"])
	var stored []model.Option
	require.NoError(t, db.Order("key ASC").Find(&stored).Error)
	assert.Equal(t, []model.Option{
		{Key: "ModelPrice", Value: `{"batch-price":1.25}`},
		{Key: "ModelRatio", Value: `{"batch-ratio":2}`},
	}, stored)
	price, found := ratio_setting.GetModelPrice("batch-price", false)
	assert.True(t, found)
	assert.Equal(t, 1.25, price)
	ratio, found, _ := ratio_setting.GetModelRatio("batch-ratio")
	assert.True(t, found)
	assert.Equal(t, 2.0, ratio)
}
