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

import {
  createEmptyChannelSchedule,
  formatShanghaiTimestamp,
  getChannelEffectiveStatus,
  normalizeChannelSchedule,
  shanghaiDateTimeLocalToUnix,
  unixToShanghaiDateTimeLocal,
} from '../channel-schedule'

describe('channel schedule time conversion', () => {
  test('formats a Unix timestamp as a Beijing datetime-local value', () => {
    const timestamp = Date.UTC(2026, 6, 27, 1, 30) / 1000

    expect(unixToShanghaiDateTimeLocal(timestamp)).toBe('2026-07-27T09:30')
  })

  test('parses a Beijing datetime-local value without using browser timezone', () => {
    const expected = Date.UTC(2026, 6, 27, 1, 30) / 1000

    expect(shanghaiDateTimeLocalToUnix('2026-07-27T09:30')).toBe(expected)
    expect(shanghaiDateTimeLocalToUnix('')).toBeNull()
  })

  test('formats timestamps with the project Simplified Chinese locale code', () => {
    const timestamp = Date.UTC(2026, 6, 27, 1, 30) / 1000

    expect(() => formatShanghaiTimestamp(timestamp, 'zhCN')).not.toThrow()
    expect(formatShanghaiTimestamp(timestamp, 'zhCN')).toContain('2026')
  })

  test('normalizes missing data without sharing weekly window references', () => {
    const schedule = createEmptyChannelSchedule()
    schedule.weekly_enabled = true
    schedule.weekly_windows.monday = [{ start: '22:00', end: '02:00' }]

    const normalized = normalizeChannelSchedule(schedule)
    const normalizedWindow = normalized.weekly_windows.monday?.[0]
    if (!normalizedWindow) throw new Error('missing normalized Monday window')
    normalizedWindow.start = '09:00'

    expect(schedule.weekly_windows.monday?.[0]?.start).toBe('22:00')
    expect(normalized.timezone).toBe('Asia/Shanghai')
  })

  test('uses the derived scheduled status without changing the base status', () => {
    expect(
      getChannelEffectiveStatus({
        status: 1,
        effective_status: 'scheduled_disabled',
      })
    ).toBe('scheduled_disabled')
    expect(
      getChannelEffectiveStatus({ status: 3, effective_status: undefined })
    ).toBe('auto_disabled')
  })
})
