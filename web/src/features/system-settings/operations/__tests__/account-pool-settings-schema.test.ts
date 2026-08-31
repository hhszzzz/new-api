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
import { describe, expect, test } from 'vitest'

import { accountPoolSettingsSchema } from '../account-pool-settings-schema'

const valid = {
  enabled: true,
  hide_email_from_non_admins: true,
  allowed_groups: ['vip'],
  regular_refresh_seconds: 300,
  near_reset_threshold_seconds: 600,
  near_reset_refresh_seconds: 60,
  post_reset_delay_seconds: 10,
  manual_refresh_cooldown_seconds: 60,
}

describe('account pool settings validation', () => {
  test('accepts the production defaults', () => {
    expect(accountPoolSettingsSchema.safeParse(valid).success).toBe(true)
  })

  test('rejects a near-reset interval larger than the regular interval', () => {
    const result = accountPoolSettingsSchema.safeParse({
      ...valid,
      regular_refresh_seconds: 60,
      near_reset_refresh_seconds: 61,
    })
    expect(result.success).toBe(false)
  })

  test('rejects out-of-range refresh and cooldown values', () => {
    expect(
      accountPoolSettingsSchema.safeParse({
        ...valid,
        post_reset_delay_seconds: 121,
      }).success
    ).toBe(false)
    expect(
      accountPoolSettingsSchema.safeParse({
        ...valid,
        manual_refresh_cooldown_seconds: 29,
      }).success
    ).toBe(false)
  })
})
