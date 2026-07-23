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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import {
  MAX_RANKING_CUSTOM_DAYS,
  datesFromRankingQuery,
  endOfLocalDayTimestamp,
  normalizeRankingDateRange,
  rankingQueryFromSearch,
  rankingSearchForPeriod,
  startOfLocalDayTimestamp,
} from '../range'

describe('rankings custom date ranges', () => {
  test('normalizes selected dates to closed local-day timestamps', () => {
    const from = new Date(2026, 0, 2, 12, 30)
    const to = new Date(2026, 0, 8, 8, 15)
    const query = normalizeRankingDateRange({ from, to }, new Date(2026, 0, 9))

    assert.deepEqual(query, {
      period: 'custom',
      startTimestamp: startOfLocalDayTimestamp(from),
      endTimestamp: endOfLocalDayTimestamp(to),
    })
  })

  test('rejects ranges longer than 366 local calendar days', () => {
    const from = new Date(2024, 0, 1)
    const to = new Date(2025, 0, 2)

    assert.equal(
      normalizeRankingDateRange({ from, to }, new Date(2025, 0, 2)),
      null
    )
  })

  test('accepts exactly 366 closed days', () => {
    const now = new Date(2027, 0, 1, 23, 59, 59)
    const from = new Date(2026, 0, 1)
    const normalized = normalizeRankingDateRange(
      { from, to: new Date(2027, 0, 1) },
      now
    )

    assert.notEqual(normalized, null)
    assert.equal(
      (normalized?.endTimestamp ?? 0) - (normalized?.startTimestamp ?? 0) + 1,
      MAX_RANKING_CUSTOM_DAYS * 24 * 60 * 60
    )
  })

  test('clears custom timestamps when a preset is selected', () => {
    assert.deepEqual(rankingSearchForPeriod('month'), { period: 'month' })
  })

  test('round-trips a custom query from URL timestamps', () => {
    const query = rankingQueryFromSearch({
      period: 'custom',
      start_timestamp: 100,
      end_timestamp: 200,
    })

    assert.deepEqual(query, {
      period: 'custom',
      startTimestamp: 100,
      endTimestamp: 200,
    })
    assert.equal(datesFromRankingQuery(query)?.from.getTime(), 100000)
  })
})
