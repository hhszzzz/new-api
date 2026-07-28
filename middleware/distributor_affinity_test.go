package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResolveAffinitySelectionGroupPreservesAutoGroupSelection(t *testing.T) {
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousAutoGroups := setting.AutoGroups2JsonString()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["premium","default"]`))
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		if previousMemoryCacheEnabled && previousDB != nil {
			model.InitChannelCache()
		}
	})

	require.NoError(t, db.Create(&model.Channel{
		Id:     17,
		Key:    "affinity-test-key",
		Name:   "affinity-test-channel",
		Status: common.ChannelStatusEnabled,
		Models: "gpt-5.4",
		Group:  "default",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: 17,
		Enabled:   true,
	}).Error)
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroups, []string{"premium", "default"})

	group, usable := resolveAffinitySelectionGroup(ctx, "auto", "gpt-5.4", 17)

	assert.True(t, usable)
	assert.Equal(t, "default", group)

	group, usable = resolveAffinitySelectionGroup(ctx, "premium", "gpt-5.4", 17)
	assert.False(t, usable)
	assert.Equal(t, "premium", group)
}

func TestDistributorAffinityPrecedesNativeProtocolTier(t *testing.T) {
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousPolicy := model_setting.GetGlobalSettings().ProtocolBridgePolicy
	affinitySetting := operation_setting.GetChannelAffinitySetting()
	previousAffinitySetting := *affinitySetting
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.Enabled = true
	model_setting.GetGlobalSettings().ProtocolBridgePolicy.DefaultAllowConversion = false

	rule := operation_setting.ChannelAffinityRule{
		Name:       "protocol-bridge-affinity-test",
		ModelRegex: []string{"^affinity-protocol-model$"},
		PathRegex:  []string{"^/v1/responses$"},
		KeySources: []operation_setting.ChannelAffinityKeySource{
			{Type: "request_header", Key: "X-Protocol-Affinity"},
		},
		TTLSeconds:       300,
		IncludeModelName: true,
	}
	affinitySetting.Enabled = true
	affinitySetting.SwitchOnSuccess = false
	affinitySetting.Rules = []operation_setting.ChannelAffinityRule{rule}

	allowConversion := true
	compatibleBaseURL := "https://compatible.example/v1"
	convertiblePriority := int64(10)
	convertible := &model.Channel{
		Id:       71,
		Name:     "affinity-convertible",
		Key:      "convertible-key",
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Models:   "affinity-protocol-model",
		Group:    "default",
		BaseURL:  &compatibleBaseURL,
		Priority: &convertiblePriority,
	}
	convertible.SetOtherSettings(dto.ChannelOtherSettings{
		ProtocolCapabilities: &dto.ProtocolCapabilities{
			UpstreamProtocols: []string{dto.ProtocolCapabilityChat},
			AllowConversion:   &allowConversion,
		},
	})
	nativePriority := int64(100)
	native := &model.Channel{
		Id:       72,
		Name:     "higher-priority-native",
		Key:      "native-key",
		Type:     constant.ChannelTypeCodex,
		Status:   common.ChannelStatusEnabled,
		Models:   "affinity-protocol-model",
		Group:    "default",
		Priority: &nativePriority,
	}
	require.NoError(t, db.Create(&[]*model.Channel{convertible, native}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{
			Group:     "default",
			Model:     "affinity-protocol-model",
			ChannelId: convertible.Id,
			Enabled:   true,
			Priority:  &convertiblePriority,
			Weight:    1,
		},
		{
			Group:     "default",
			Model:     "affinity-protocol-model",
			ChannelId: native.Id,
			Enabled:   true,
			Priority:  &nativePriority,
			Weight:    1,
		},
	}).Error)
	model.InitChannelCache()

	affinityValue := fmt.Sprintf("protocol-affinity-%d", time.Now().UnixNano())
	seedContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	seedContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	seedContext.Request.Header.Set("X-Protocol-Affinity", affinityValue)
	_, found := service.GetPreferredChannelByAffinity(seedContext, "affinity-protocol-model", "default")
	require.False(t, found)
	service.RecordChannelAffinity(seedContext, convertible.Id)

	t.Cleanup(func() {
		service.ClearCurrentChannelAffinityCache(seedContext)
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		model_setting.GetGlobalSettings().ProtocolBridgePolicy = previousPolicy
		*affinitySetting = previousAffinitySetting
		if previousMemoryCacheEnabled && previousDB != nil {
			model.InitChannelCache()
		}
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{"model":"affinity-protocol-model","input":"hello"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("X-Protocol-Affinity", affinityValue)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserGroups, []string{"default"})
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	Distribute()(ctx)

	require.False(t, ctx.IsAborted(), recorder.Body.String())
	assert.Equal(t, convertible.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	plan, ok := common.GetContextKeyType[channelcompat.ProtocolPlan](ctx, constant.ContextKeyProtocolPlan)
	require.True(t, ok)
	assert.Equal(t, channelcompat.StatusConvertible, plan.Status)
	assert.Equal(t, channelcompat.ProtocolChat, plan.UpstreamProtocol)
	assert.NotEqual(t, native.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
}
