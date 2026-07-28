package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchInsertChannelsRefreshesAdvancedCustomPricingEndpoints(t *testing.T) {
	tests := []struct {
		name               string
		memoryCacheEnabled bool
		channelID          int
	}{
		{name: "memory cache enabled", memoryCacheEnabled: true, channelID: 601},
		{name: "memory cache disabled", memoryCacheEnabled: false, channelID: 602},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetPricingEndpointTestTables(t)
			common.MemoryCacheEnabled = test.memoryCacheEnabled
			InitChannelCache()
			require.Empty(t, GetPricing())

			modelName := fmt.Sprintf("batch-advanced-custom-model-%d", test.channelID)
			channel := Channel{
				Id:     test.channelID,
				Type:   constant.ChannelTypeAdvancedCustom,
				Key:    fmt.Sprintf("batch-advanced-custom-key-%d", test.channelID),
				Name:   fmt.Sprintf("batch-advanced-custom-channel-%d", test.channelID),
				Status: common.ChannelStatusEnabled,
				Models: modelName,
				Group:  "default",
			}
			channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    "openai_responses_to_gemini_generate_content",
			}))

			require.NoError(t, BatchInsertChannels([]Channel{channel}))

			byModel := pricingEndpointTypesFromPricing(GetPricing())
			assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIResponse}, byModel[modelName])
		})
	}
}
