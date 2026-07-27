import { describe, expect, it } from 'vitest'

import { createPricingSchema } from '../pricing-schema'

const translate = (key: string) => key

const validPricingValues = {
  QuotaPerUnit: 500_000,
  USDExchangeRate: 1,
  DisplayInCurrencyEnabled: true,
  DisplayTokenStatEnabled: true,
  general_setting: {
    quota_display_type: 'USD' as const,
  },
}

describe('pricing section validation', () => {
  it('rejects a zero quota unit before submitting it to the backend', () => {
    const result = createPricingSchema(translate).safeParse({
      ...validPricingValues,
      QuotaPerUnit: 0,
    })

    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues).toContainEqual(
      expect.objectContaining({
        path: ['QuotaPerUnit'],
        message: 'Quota must be a positive number',
      })
    )
  })

  it('accepts a finite positive quota unit', () => {
    expect(
      createPricingSchema(translate).safeParse(validPricingValues).success
    ).toBe(true)
  })
})
