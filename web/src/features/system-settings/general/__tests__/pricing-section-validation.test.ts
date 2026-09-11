/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
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
