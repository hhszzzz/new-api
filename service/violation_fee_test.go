package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalcViolationFeeQuotaUsesAuditableSaturation(t *testing.T) {
	originalQuotaPerUnit := common.GetQuotaPerUnit()
	t.Cleanup(func() {
		require.NoError(t, common.SetQuotaPerUnit(originalQuotaPerUnit))
	})
	require.NoError(t, common.SetQuotaPerUnit(500_000))

	quota, clamp := calcViolationFeeQuota(0.01, 1.5)
	assert.Equal(t, 7500, quota)
	assert.Nil(t, clamp)

	quota, clamp = calcViolationFeeQuota(math.MaxFloat64, 1)
	assert.Equal(t, common.MaxQuota, quota)
	if assert.NotNil(t, clamp) {
		assert.Equal(t, common.QuotaClampOverflow, clamp.Kind)
	}

	quota, clamp = calcViolationFeeQuota(math.NaN(), 1)
	assert.Zero(t, quota)
	if assert.NotNil(t, clamp) {
		assert.Equal(t, common.QuotaClampNaN, clamp.Kind)
	}

	quota, clamp = calcViolationFeeQuota(-1, 1)
	assert.Zero(t, quota)
	assert.Nil(t, clamp)
}
