package channelcompat

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtocolAffinityUsesChannelModelEntryProtocolAndConfigurationFingerprint(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	baseURL := "https://gateway.example/v1"
	channel := &model.Channel{Id: 9801, Type: constant.ChannelTypeOpenAI, BaseURL: &baseURL}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})

	RememberProtocolAffinity(channel, "provider-model", ProtocolResponses, ProtocolChat)
	protocol, found := LookupProtocolAffinity(channel, "provider-model", ProtocolResponses)
	require.True(t, found)
	assert.Equal(t, ProtocolChat, protocol)

	assertProtocol, otherModelFound := LookupProtocolAffinity(channel, "other-model", ProtocolResponses)
	assert.False(t, otherModelFound)
	assert.Empty(t, assertProtocol)

	otherEntryProtocol, otherEntryProtocolFound := LookupProtocolAffinity(channel, "provider-model", ProtocolMessages)
	assert.False(t, otherEntryProtocolFound)
	assert.Empty(t, otherEntryProtocol)

	changedBaseURL := "https://replacement.example/v1"
	channel.BaseURL = &changedBaseURL
	_, changedConfigFound := LookupProtocolAffinity(channel, "provider-model", ProtocolResponses)
	assert.False(t, changedConfigFound)
}

func TestIsProtocolUnsupportedErrorOnlyAcceptsEndpointEvidence(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
		want       bool
	}{
		{name: "404", statusCode: http.StatusNotFound, message: "model not found", want: true},
		{name: "405", statusCode: http.StatusMethodNotAllowed, message: "method not allowed", want: true},
		{name: "501", statusCode: http.StatusNotImplemented, message: "not implemented", want: true},
		{name: "explicit endpoint 400", statusCode: http.StatusBadRequest, message: "unsupported endpoint /v1/responses", want: true},
		{name: "explicit wire API 500", statusCode: http.StatusInternalServerError, message: "messages API is not supported", want: true},
		{name: "ordinary 400", statusCode: http.StatusBadRequest, message: "unsupported parameter previous_response_id", want: false},
		{name: "rate limit", statusCode: http.StatusTooManyRequests, message: "rate limited", want: false},
		{name: "ordinary 500", statusCode: http.StatusInternalServerError, message: "upstream unavailable", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiError := types.NewErrorWithStatusCode(errors.New(test.message), types.ErrorCodeBadResponseStatusCode, test.statusCode)
			assert.Equal(t, test.want, IsProtocolUnsupportedError(apiError))
		})
	}
}
