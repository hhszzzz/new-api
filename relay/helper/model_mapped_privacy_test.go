package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelMappedHelperKeepsCompactRequestedModelPrivate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("model_mapping", `{"public-model":"private-model"}`)

	requestedModel := ratio_setting.WithCompactModelSuffix("public-model")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: requestedModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: requestedModel,
		},
	}
	request := &dto.OpenAIResponsesRequest{Model: requestedModel}

	require.NoError(t, ModelMappedHelper(ctx, info, request))
	require.Equal(t, requestedModel, info.OriginModelName)
	require.Equal(t, "private-model", info.UpstreamModelName)
	require.Equal(t, ratio_setting.WithCompactModelSuffix("private-model"), info.BillingModelName)
	require.True(t, info.IsModelMapped)
	require.Equal(t, "private-model", request.Model)
}

func TestModelMappedHelperCompactSelfMapRunsCommonFinalization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("model_mapping", `{"public-model":"public-model"}`)

	requestedModel := ratio_setting.WithCompactModelSuffix("public-model")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: requestedModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: requestedModel,
		},
	}
	request := &dto.OpenAIResponsesRequest{Model: requestedModel}

	require.NoError(t, ModelMappedHelper(ctx, info, request))
	require.Equal(t, requestedModel, info.OriginModelName)
	require.Equal(t, "public-model", info.UpstreamModelName)
	require.Equal(t, requestedModel, info.BillingModelName)
	require.False(t, info.IsModelMapped)
	require.Equal(t, "public-model", request.Model)
}
