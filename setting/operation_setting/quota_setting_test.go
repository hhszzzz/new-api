package operation_setting

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaSettingPublishesImmutableSnapshot(t *testing.T) {
	original := GetQuotaSetting()
	t.Cleanup(func() {
		handled, err := config.GlobalConfig.Update("quota_setting", map[string]string{
			"enable_free_model_pre_consume": strconv.FormatBool(original.EnableFreeModelPreConsume),
		})
		require.True(t, handled)
		require.NoError(t, err)
	})

	oldSnapshot := GetQuotaSetting()
	handled, err := config.GlobalConfig.Update("quota_setting", map[string]string{
		"enable_free_model_pre_consume": strconv.FormatBool(!original.EnableFreeModelPreConsume),
	})
	require.True(t, handled)
	require.NoError(t, err)

	assert.Equal(t, original.EnableFreeModelPreConsume, oldSnapshot.EnableFreeModelPreConsume)
	assert.Equal(t, !original.EnableFreeModelPreConsume, GetQuotaSetting().EnableFreeModelPreConsume)
}
