package relay

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"
)

func newAPIErrorFromParamOverride(err error) *hosttypes.NewAPIError {
	if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
		return relaycommon.NewAPIErrorFromParamOverride(fixedErr)
	}
	return hosttypes.NewError(err, hosttypes.ErrorCodeChannelParamOverrideInvalid, hosttypes.ErrOptionWithSkipRetry())
}
