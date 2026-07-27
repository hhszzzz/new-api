package controller

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.Log
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	var auditData struct {
		Operation struct {
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	require.NoError(t, common.UnmarshalJsonStr(auditLog.Other, &auditData))
	assert.Equal(t, float64(1), auditData.Operation.Params["count"])
}

func TestAggregateAwareChannelPagination(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelAggregate{}))
	firstAggregate := &model.ChannelAggregate{Name: "First aggregate"}
	secondAggregate := &model.ChannelAggregate{Name: "Second aggregate"}
	require.NoError(t, db.Create(firstAggregate).Error)
	require.NoError(t, db.Create(secondAggregate).Error)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 101, Name: "needle-first-high", Key: "secret-101", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(100)), AggregateId: &firstAggregate.Id},
		{Id: 102, Name: "needle-first-low", Key: "secret-102", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(10)), AggregateId: &firstAggregate.Id},
		{Id: 103, Name: "needle-second-high", Key: "secret-103", Type: constant.ChannelTypeAnthropic, Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(90)), AggregateId: &secondAggregate.Id},
		{Id: 104, Name: "needle-second-low", Key: "secret-104", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(80)), AggregateId: &secondAggregate.Id},
		{Id: 105, Name: "needle-standalone", Key: "secret-105", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(95))},
	}).Error)

	type responseBody struct {
		Success bool `json:"success"`
		Data    struct {
			Items      []model.Channel `json:"items"`
			Total      int64           `json:"total"`
			Page       int             `json:"page"`
			PageSize   int             `json:"page_size"`
			TypeCounts map[int64]int64 `json:"type_counts"`
		} `json:"data"`
	}

	firstPageRecorder := httptest.NewRecorder()
	firstPageContext, _ := gin.CreateTestContext(firstPageRecorder)
	firstPageContext.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/channel?aggregate_mode=true&p=1&page_size=2&sort_by=priority&sort_order=desc",
		nil,
	)
	GetAllChannels(firstPageContext)

	var firstPage responseBody
	require.NoError(t, common.Unmarshal(firstPageRecorder.Body.Bytes(), &firstPage))
	require.True(t, firstPage.Success)
	assert.EqualValues(t, 3, firstPage.Data.Total)
	assert.Equal(t, 1, firstPage.Data.Page)
	assert.Equal(t, 2, firstPage.Data.PageSize)
	require.Len(t, firstPage.Data.Items, 3)
	assert.Equal(t, []int{101, 102, 105}, []int{
		firstPage.Data.Items[0].Id,
		firstPage.Data.Items[1].Id,
		firstPage.Data.Items[2].Id,
	})
	assert.EqualValues(t, 4, firstPage.Data.TypeCounts[constant.ChannelTypeOpenAI])
	assert.EqualValues(t, 1, firstPage.Data.TypeCounts[constant.ChannelTypeAnthropic])
	for _, channel := range firstPage.Data.Items {
		assert.Empty(t, channel.Key)
	}

	secondPageRecorder := httptest.NewRecorder()
	secondPageContext, _ := gin.CreateTestContext(secondPageRecorder)
	secondPageContext.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/channel?aggregate_mode=true&p=2&page_size=2&sort_by=priority&sort_order=desc",
		nil,
	)
	GetAllChannels(secondPageContext)

	var secondPage responseBody
	require.NoError(t, common.Unmarshal(secondPageRecorder.Body.Bytes(), &secondPage))
	require.True(t, secondPage.Success)
	assert.EqualValues(t, 3, secondPage.Data.Total)
	require.Len(t, secondPage.Data.Items, 2)
	assert.Equal(t, []int{103, 104}, []int{
		secondPage.Data.Items[0].Id,
		secondPage.Data.Items[1].Id,
	})

	searchRecorder := httptest.NewRecorder()
	searchContext, _ := gin.CreateTestContext(searchRecorder)
	searchContext.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/channel/search?aggregate_mode=true&keyword=needle&p=1&page_size=2&sort_by=priority&sort_order=desc",
		nil,
	)
	SearchChannels(searchContext)

	var search responseBody
	require.NoError(t, common.Unmarshal(searchRecorder.Body.Bytes(), &search))
	require.True(t, search.Success)
	assert.EqualValues(t, 3, search.Data.Total)
	require.Len(t, search.Data.Items, 3)
	assert.Equal(t, []int{101, 102, 105}, []int{
		search.Data.Items[0].Id,
		search.Data.Items[1].Id,
		search.Data.Items[2].Id,
	})
	assert.EqualValues(t, 4, search.Data.TypeCounts[constant.ChannelTypeOpenAI])
	assert.EqualValues(t, 1, search.Data.TypeCounts[constant.ChannelTypeAnthropic])
	for _, channel := range search.Data.Items {
		assert.Empty(t, channel.Key)
	}
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.GetQuotaPerUnit(),
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestSettleTestQuotaSaturatesAndAuditsFixedPrice(t *testing.T) {
	info := &relaycommon.RelayInfo{}

	quota, result := settleTestQuota(info, types.PriceData{
		UsePrice:   true,
		ModelPrice: math.MaxFloat64,
	}, &dto.Usage{})

	assert.Equal(t, common.MaxQuota, quota)
	assert.Nil(t, result)
	require.NotNil(t, info.QuotaClamp)
	assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
		QuotaClamp: &common.QuotaClamp{
			Op:      "QuotaFromFloat",
			Kind:    common.QuotaClampOverflow,
			Clamped: common.MaxQuota,
		},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, adminInfo, "quota_saturation")
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}
