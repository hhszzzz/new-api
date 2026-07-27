/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

import { isSameUsageLogsQueryScope } from '../query-params'

describe('usage log placeholder scope', () => {
  test('reuses data for pagination and filter changes within the same scope', () => {
    expect(
      isSameUsageLogsQueryScope(
        ['logs', 'common', true, true, 3, 50, ['quota']],
        ['logs', 'common', true, true]
      )
    ).toBe(true)
  })

  test('rejects data from a different user scope or log category', () => {
    expect(
      isSameUsageLogsQueryScope(
        ['logs', 'common', true, true, 1, 20],
        ['logs', 'common', false, true]
      )
    ).toBe(false)
    expect(
      isSameUsageLogsQueryScope(
        ['logs', 'task', false, false, 1, 20],
        ['logs', 'drawing', false, false]
      )
    ).toBe(false)
  })

  test('keeps aggregate statistics isolated by admin scope', () => {
    expect(
      isSameUsageLogsQueryScope(
        ['usage-logs-stats', true, { start_timestamp: 1 }],
        ['usage-logs-stats', false]
      )
    ).toBe(false)
  })
})
