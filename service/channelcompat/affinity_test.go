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

func TestProtocolAffinityRemembersGeminiWireProtocol(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	channel := &model.Channel{Id: 9802, Type: constant.ChannelTypeGemini}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ProtocolCapabilities: &dto.ProtocolCapabilities{
		SelectionMode: dto.ProtocolSelectionModeAuto,
	}})

	RememberProtocolAffinity(channel, "provider-model", ProtocolResponses, ProtocolGemini)
	protocol, found := LookupProtocolAffinity(channel, "provider-model", ProtocolResponses)
	require.True(t, found)
	assert.Equal(t, ProtocolGemini, protocol)
}

func TestIsProtocolUnsupportedErrorOnlyAcceptsEndpointEvidence(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
		evidence   bool
		checked    bool
		want       bool
	}{
		{name: "model not found 404", statusCode: http.StatusNotFound, message: "model not found", want: false},
		{name: "genericized non-empty 404", statusCode: http.StatusNotFound, message: "bad response status code 404", checked: true, want: false},
		{name: "bare endpoint 404", statusCode: http.StatusNotFound, message: "bad response status code 404", want: true},
		{name: "endpoint not found 404", statusCode: http.StatusNotFound, message: "endpoint /v1/responses not found", evidence: true, want: true},
		{name: "405", statusCode: http.StatusMethodNotAllowed, message: "method not allowed", want: true},
		{name: "501", statusCode: http.StatusNotImplemented, message: "not implemented", want: true},
		{name: "upstream endpoint evidence 400", statusCode: http.StatusBadRequest, message: "unsupported endpoint /v1/responses", evidence: true, want: true},
		{name: "upstream wire API evidence 500", statusCode: http.StatusInternalServerError, message: "messages API is not supported", evidence: true, want: true},
		{name: "local converter unsupported protocol", statusCode: http.StatusBadRequest, message: "unsupported protocol conversion", want: false},
		{name: "ordinary 400", statusCode: http.StatusBadRequest, message: "unsupported parameter previous_response_id", want: false},
		{name: "ordinary not implemented", statusCode: http.StatusInternalServerError, message: "reasoning summaries are not implemented", want: false},
		{name: "rate limit", statusCode: http.StatusTooManyRequests, message: "rate limited", want: false},
		{name: "ordinary 500", statusCode: http.StatusInternalServerError, message: "upstream unavailable", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiError := types.NewErrorWithStatusCode(errors.New(test.message), types.ErrorCodeBadResponseStatusCode, test.statusCode)
			if test.evidence {
				apiError.MarkProtocolUnsupported()
			} else if test.checked {
				apiError.MarkProtocolUnsupportedChecked()
			}
			assert.Equal(t, test.want, IsProtocolUnsupportedError(apiError))
		})
	}
}

func TestIsProtocolUnsupportedErrorAcceptsPrivateRawBodyEvidence(t *testing.T) {
	apiError := types.NewErrorWithStatusCode(errors.New("bad response status code 400"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)
	apiError.MarkProtocolUnsupported()

	assert.True(t, IsProtocolUnsupportedError(apiError))
}
