package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaPerUnitRejectsInvalidValuesWithoutPublishing(t *testing.T) {
	previous := GetQuotaPerUnit()
	t.Cleanup(func() { require.NoError(t, SetQuotaPerUnit(previous)) })

	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.Error(t, SetQuotaPerUnit(value))
		assert.Equal(t, previous, GetQuotaPerUnit())
	}
}
