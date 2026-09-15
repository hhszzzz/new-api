package model

import (
	hostdto "github.com/QuantumNous/new-api/dto"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestChannelValidateSettingsRejectsMalformedClientPolicy(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(hostdto.ChannelOtherSettings{
		ClientPolicy: operation_setting.ClientAccessPolicy{
			Mode:    "sometimes",
			Clients: []string{"codex"},
		},
	})

	err := channel.ValidateSettings()
	require.ErrorContains(t, err, "invalid channel client policy")
	require.ErrorContains(t, err, "invalid client policy mode")
}
