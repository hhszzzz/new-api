package common

import (
	"fmt"
	"math"
	"sync/atomic"
)

const DefaultQuotaPerUnit = 500 * 1000.0 // $0.002 / 1K tokens

var quotaPerUnitBits atomic.Uint64

func init() {
	quotaPerUnitBits.Store(math.Float64bits(DefaultQuotaPerUnit))
}

func GetQuotaPerUnit() float64 {
	return math.Float64frombits(quotaPerUnitBits.Load())
}

func ValidateQuotaPerUnit(value float64) error {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("quota per unit must be a finite number greater than zero")
	}
	return nil
}

func SetQuotaPerUnit(value float64) error {
	if err := ValidateQuotaPerUnit(value); err != nil {
		return err
	}
	quotaPerUnitBits.Store(math.Float64bits(value))
	return nil
}

func GetTrustQuota() int {
	return QuotaFromFloat(10 * GetQuotaPerUnit())
}
