package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type persistedAtomicConfig struct {
	Mode string `json:"mode"`
	Expr string `json:"expr"`

	publishCount int
}

func (setting *persistedAtomicConfig) ValidateConfig() error {
	if setting.Mode == "expression" && setting.Expr == "" {
		return fmt.Errorf("expression mode requires an expression")
	}
	return nil
}

func (setting *persistedAtomicConfig) PublishConfig() {
	setting.publishCount++
}

func TestUpdateOptionDoesNotPublishWhenPersistenceFails(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{"test.persistence": "before"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = UpdateOption("test.persistence", "after")
	require.Error(t, err)
	common.OptionMapRWMutex.RLock()
	published := common.OptionMap["test.persistence"]
	common.OptionMapRWMutex.RUnlock()
	assert.Equal(t, "before", published)
}

func TestPublishPersistedOptionsBatchesConfigFieldsRegardlessOfRowOrder(t *testing.T) {
	const configName = "test_persisted_atomic_config"
	setting := &persistedAtomicConfig{}
	config.GlobalConfig.Register(configName, setting)
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	publishPersistedOptions([]*Option{
		{Key: configName + ".mode", Value: "expression"},
		{Key: configName + ".expr", Value: "prompt_tokens * 2"},
	})

	assert.Equal(t, "expression", setting.Mode)
	assert.Equal(t, "prompt_tokens * 2", setting.Expr)
	assert.Equal(t, 1, setting.publishCount)
}

func TestInsertWithPricingOptionsRollsBackModelWhenOptionPersistenceFails(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	require.NoError(t, db.AutoMigrate(&Model{}))

	previousModelPrice := ratio_setting.ModelPrice2JSONString()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"before-model":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousModelPrice))
	})

	callbackName := "test:fail_model_pricing_option_create"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "options" {
			tx.AddError(errors.New("forced option failure"))
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(callbackName)
	})

	candidate := &Model{
		ModelName:    "transactional-model",
		Status:       1,
		SyncOfficial: 1,
	}
	err := candidate.InsertWithPricingOptions(map[string]string{
		ratio_setting.ModelPriceOptionKey: `{"after-model":2}`,
	})
	require.ErrorContains(t, err, "forced option failure")

	var modelCount int64
	require.NoError(t, db.Model(&Model{}).Where("model_name = ?", candidate.ModelName).Count(&modelCount).Error)
	assert.Zero(t, modelCount)
	var optionCount int64
	require.NoError(t, db.Model(&Option{}).Count(&optionCount).Error)
	assert.Zero(t, optionCount)

	price, found := ratio_setting.GetModelPrice("before-model", false)
	assert.True(t, found)
	assert.Equal(t, 1.0, price)
	_, found = ratio_setting.GetModelPrice("after-model", false)
	assert.False(t, found)
}

func TestUpdateClientPolicySettingRollsBackWithoutPublishing(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	previous := *operation_setting.GetClientPolicySettingSnapshot()
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, operation_setting.PublishClientPolicySetting(previous))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	before := operation_setting.ClientPolicySetting{
		Rules: []operation_setting.ClientIdentificationRule{
			{
				Name: "before",
				Matches: []operation_setting.ClientIdentificationMatch{
					{Source: "user_agent", Mode: "prefix", Value: "before/"},
				},
			},
		},
		GroupPolicies: map[string]operation_setting.ClientAccessPolicy{
			"default": {Mode: operation_setting.ClientPolicyModeAllow, Clients: []string{"before"}},
		},
	}
	require.NoError(t, UpdateClientPolicySetting(before))

	callbackName := "test:fail_client_policy_option_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "options" {
			tx.AddError(errors.New("forced client policy option failure"))
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove(callbackName)
	})

	after := operation_setting.ClientPolicySetting{
		Rules: []operation_setting.ClientIdentificationRule{
			{
				Name: "after",
				Matches: []operation_setting.ClientIdentificationMatch{
					{Source: "user_agent", Mode: "prefix", Value: "after/"},
				},
			},
		},
		GroupPolicies: map[string]operation_setting.ClientAccessPolicy{
			"default": {Mode: operation_setting.ClientPolicyModeDeny, Clients: []string{"after"}},
		},
	}
	err := UpdateClientPolicySetting(after)
	require.ErrorContains(t, err, "forced client policy option failure")

	snapshot := operation_setting.GetClientPolicySettingSnapshot()
	require.Len(t, snapshot.Rules, 1)
	assert.Equal(t, "before", snapshot.Rules[0].Name)
	assert.Equal(t, operation_setting.ClientPolicyModeAllow, snapshot.GroupPolicies["default"].Mode)
	assert.JSONEq(t, `[{"name":"before","matches":[{"source":"user_agent","mode":"prefix","value":"before/"}]}]`, requireOptionValue(t, db, operation_setting.ClientPolicyRulesOptionKey))
	assert.JSONEq(t, `{"default":{"mode":"allow","clients":["before"]}}`, requireOptionValue(t, db, operation_setting.ClientPolicyGroupsOptionKey))
}

func TestUpdateOptionRejectsInvalidRateLimitBeforePersistence(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	previousRateLimits := setting.ModelRequestRateLimitGroup2JSONString()
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(previousRateLimits))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	const valid = `{"before":[10,2]}`
	require.NoError(t, UpdateOption("ModelRequestRateLimitGroup", valid))

	err := UpdateOption("ModelRequestRateLimitGroup", `{"after":[10,0]}`)
	require.Error(t, err)

	assert.JSONEq(t, valid, requireOptionValue(t, db, "ModelRequestRateLimitGroup"))
	total, success, found := setting.GetGroupRateLimit("before")
	assert.True(t, found)
	assert.Equal(t, 10, total)
	assert.Equal(t, 2, success)
	_, _, found = setting.GetGroupRateLimit("after")
	assert.False(t, found)
	common.OptionMapRWMutex.RLock()
	published := common.OptionMap["ModelRequestRateLimitGroup"]
	common.OptionMapRWMutex.RUnlock()
	assert.JSONEq(t, valid, published)
}

func TestUpdateOptionRejectsInvalidQuotaPerUnitBeforePersistence(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	previousQuotaPerUnit := common.GetQuotaPerUnit()
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, common.SetQuotaPerUnit(previousQuotaPerUnit))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	const valid = "125000"
	require.NoError(t, UpdateOption("QuotaPerUnit", valid))

	for _, invalid := range []string{"0", "-1", "NaN", "+Inf", "not-a-number"} {
		err := UpdateOption("QuotaPerUnit", invalid)
		require.Error(t, err)
		assert.Equal(t, valid, requireOptionValue(t, db, "QuotaPerUnit"))
		assert.Equal(t, 125000.0, common.GetQuotaPerUnit())
		common.OptionMapRWMutex.RLock()
		published := common.OptionMap["QuotaPerUnit"]
		common.OptionMapRWMutex.RUnlock()
		assert.Equal(t, valid, published)
	}
}

func TestUpdateOptionRejectsInvalidHeaderNavigationBeforePersistence(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	const valid = `{"custom":[{"id":"portal","title":"Portal","url":"https://portal.example.com","enabled":true}],"order":["home","custom:portal"]}`
	require.NoError(t, UpdateOption("HeaderNavModules", valid))

	invalid := `{"custom":[{"id":"portal","title":"Portal","url":"javascript:alert(1)","enabled":true}]}`
	require.Error(t, UpdateOption("HeaderNavModules", invalid))

	assert.JSONEq(t, valid, requireOptionValue(t, db, "HeaderNavModules"))
	common.OptionMapRWMutex.RLock()
	published := common.OptionMap["HeaderNavModules"]
	common.OptionMapRWMutex.RUnlock()
	assert.JSONEq(t, valid, published)
}
