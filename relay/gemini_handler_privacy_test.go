package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConfigureGeminiBillingModelPreservesRequestedAndMappedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedModelPrices := ratio_setting.ModelPrice2JSONString()
	settings := model_setting.GetGeminiSettings()
	savedThinkingAdapterEnabled := settings.ThinkingAdapterEnabled
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrices))
		settings.ThinkingAdapterEnabled = savedThinkingAdapterEnabled
	})

	modelPrices, err := common.Marshal(map[string]float64{
		"public-gemini-model":            0.1,
		"public-gemini-model-nothinking": 0.25,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(modelPrices)))
	settings.ThinkingAdapterEnabled = true

	zeroBudget := 0
	request := &dto.GeminiChatRequest{
		GenerationConfig: dto.GeminiChatGenerationConfig{
			ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingBudget: &zeroBudget},
		},
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-gemini-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		Request:         request,
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("model_mapping", `{"public-gemini-model":"private-gemini-model"}`)

	ConfigureGeminiBillingModel(info)
	priceData, err := helper.ModelPriceHelper(ctx, info, 0, request.GetTokenCountMeta())
	require.NoError(t, err)
	require.Equal(t, 0.25, priceData.ModelPrice)
	require.Equal(t, "public-gemini-model-nothinking", info.BillingModelName)
	require.Equal(t, "public-gemini-model", info.OriginModelName)

	info.InitChannelMeta(ctx)
	require.NoError(t, helper.ModelMappedHelper(ctx, info, request))
	require.Equal(t, "public-gemini-model", info.OriginModelName)
	require.Equal(t, "public-gemini-model-nothinking", info.BillingModelName)
	require.Equal(t, "private-gemini-model", info.UpstreamModelName)
	require.True(t, info.IsModelMapped)
}
