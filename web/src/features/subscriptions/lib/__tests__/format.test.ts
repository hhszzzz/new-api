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
import type { TFunction } from 'i18next'
import { describe, expect, test } from 'vitest'

import { formatSubscriptionName } from '../format'

// Translate by echoing the key (and the interpolation value) so the test
// protects the naming decision rather than any one language's text.
const t = ((key: string, options?: { group?: string }) =>
  options?.group ? `${key}:${options.group}` : key) as unknown as TFunction

describe('formatSubscriptionName', () => {
  test('names a subscription after the group it grants', () => {
    expect(
      formatSubscriptionName({ upgrade_group: 'pro' }, 'Pro Plan', t)
    ).toBe('{{group}} group subscription:pro')
  })

  test('falls back to the plan title when no group is granted', () => {
    expect(formatSubscriptionName({ upgrade_group: '' }, 'Pro Plan', t)).toBe(
      'Pro Plan'
    )
    expect(formatSubscriptionName({}, 'Pro Plan', t)).toBe('Pro Plan')
    expect(formatSubscriptionName({ upgrade_group: '  ' }, 'Pro Plan', t)).toBe(
      'Pro Plan'
    )
  })

  test('falls back to the generic label without a plan title', () => {
    expect(formatSubscriptionName(undefined, undefined, t)).toBe('Subscription')
  })
})
