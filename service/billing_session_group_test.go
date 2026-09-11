package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// These tests exercise the real billing entry point (PreConsumeBilling ->
// NewBillingSession) so the group-scoping and wallet-override rules are covered
// end to end, not just at the model layer.

func setupGroupBillingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
	})
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.UserGroupMembership{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
	))
	return db
}

type groupBillingFixture struct {
	db    *gorm.DB
	user  model.User
	token model.Token
	plan  model.SubscriptionPlan
}

func newGroupBillingFixture(t *testing.T, upgradeGroup string, wallet int) *groupBillingFixture {
	t.Helper()
	db := setupGroupBillingDB(t)
	plan := model.SubscriptionPlan{Title: "group-billing-plan", Enabled: true, UpgradeGroup: upgradeGroup, TotalAmount: 100_000}
	require.NoError(t, db.Create(&plan).Error)
	user := model.User{Username: "group-billing-user-" + upgradeGroup, Quota: wallet, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "group-billing-token-" + upgradeGroup, Name: "group-billing", RemainQuota: 10_000_000, Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&token).Error)
	return &groupBillingFixture{db: db, user: user, token: token, plan: plan}
}

func (f *groupBillingFixture) grantSubscription(t *testing.T, total, used int64, group string) model.UserSubscription {
	t.Helper()
	now := common.GetTimestamp()
	sub := model.UserSubscription{
		UserId:              f.user.Id,
		PlanId:              f.plan.Id,
		AmountTotal:         total,
		AmountUsed:          used,
		StartTime:           now - 10,
		EndTime:             now + 3600,
		Status:              "active",
		Source:              "admin",
		UpgradeGroup:        group,
		AllowWalletOverflow: false,
	}
	require.NoError(t, f.db.Create(&sub).Error)
	return sub
}

func (f *groupBillingFixture) relayInfo(requestId, group, pref string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RequestId:       requestId,
		UserId:          f.user.Id,
		TokenId:         f.token.Id,
		TokenKey:        f.token.Key,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 1},
		OriginModelName: "group-billing-model",
		UsingGroup:      group,
		UserGroup:       group,
		UserSetting:     dto.UserSetting{BillingPreference: pref},
		ForcePreConsume: true,
		StartTime:       time.Now(),
		RelayFormat:     types.RelayFormatOpenAI,
	}
}

func newBillingContext() *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return ctx
}

func (f *groupBillingFixture) userQuota(t *testing.T) int {
	t.Helper()
	quota, err := model.GetUserQuota(f.user.Id, false)
	require.NoError(t, err)
	return quota
}

func (f *groupBillingFixture) subscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, f.db.First(&sub, id).Error)
	return sub.AmountUsed
}

func TestSubscriptionGrantedGroupForcesSubscriptionOverWalletPreference(t *testing.T) {
	f := newGroupBillingFixture(t, "pro", 1_000_000)
	sub := f.grantSubscription(t, 100_000, 0, "pro")

	// The user explicitly asks for wallet billing, but a subscription-granted
	// group must override that preference and drain the subscription instead.
	info := f.relayInfo("req-group-pro", "pro", "wallet_only")
	require.Nil(t, PreConsumeBilling(newBillingContext(), 5000, info))

	assert.Equal(t, BillingSourceSubscription, info.BillingSource)
	assert.Equal(t, 1_000_000, f.userQuota(t), "wallet must stay untouched for a subscription-granted group")
	assert.Equal(t, int64(5000), f.subscriptionUsed(t, sub.Id))
}

func TestGroupTiedSubscriptionDoesNotFundOtherGroup(t *testing.T) {
	f := newGroupBillingFixture(t, "pro", 1_000_000)
	sub := f.grantSubscription(t, 100_000, 0, "pro")

	info := f.relayInfo("req-group-default", "default", "subscription_first")
	require.Nil(t, PreConsumeBilling(newBillingContext(), 5000, info))

	assert.Equal(t, BillingSourceWallet, info.BillingSource, "a pro subscription must not fund the default group")
	assert.Equal(t, 1_000_000-5000, f.userQuota(t))
	assert.Equal(t, int64(0), f.subscriptionUsed(t, sub.Id))
}

func TestExhaustedGrantedSubscriptionNeverFallsBackToWallet(t *testing.T) {
	f := newGroupBillingFixture(t, "pro", 1_000_000)
	f.grantSubscription(t, 100_000, 100_000, "pro")

	info := f.relayInfo("req-group-exhausted", "pro", "subscription_first")
	err := PreConsumeBilling(newBillingContext(), 5000, info)
	require.NotNil(t, err)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, err.GetErrorCode())
	assert.Equal(t, 1_000_000, f.userQuota(t), "wallet must never back an exhausted subscription-granted group")
}

func TestGenericSubscriptionFundsAnyGroup(t *testing.T) {
	f := newGroupBillingFixture(t, "", 1_000_000)
	sub := f.grantSubscription(t, 100_000, 0, "")

	info := f.relayInfo("req-generic", "default", "subscription_first")
	require.Nil(t, PreConsumeBilling(newBillingContext(), 5000, info))

	assert.Equal(t, BillingSourceSubscription, info.BillingSource, "a generic subscription keeps funding any group")
	assert.Equal(t, 1_000_000, f.userQuota(t))
	assert.Equal(t, int64(5000), f.subscriptionUsed(t, sub.Id))
}

func TestManuallyAssignedGroupStillUsesWallet(t *testing.T) {
	f := newGroupBillingFixture(t, "pro", 1_000_000)
	manual := true
	require.NoError(t, f.db.Create(&model.UserGroupMembership{UserId: f.user.Id, GroupName: "pro", Manual: &manual}).Error)

	info := f.relayInfo("req-manual", "pro", "subscription_first")
	require.Nil(t, PreConsumeBilling(newBillingContext(), 5000, info))

	assert.Equal(t, BillingSourceWallet, info.BillingSource, "a manually assigned group must keep wallet billing available")
	assert.Equal(t, 1_000_000-5000, f.userQuota(t))
}
