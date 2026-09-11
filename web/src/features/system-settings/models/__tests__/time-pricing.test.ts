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
  buildTimePricingCondition,
  buildTimePricingExpression,
  createHourWindow,
  defaultTimePricingConfig,
  parseTimePricing,
  type TimePricingConfig,
} from '../time-pricing'

function config(overrides: Partial<TimePricingConfig>): TimePricingConfig {
  return { ...defaultTimePricingConfig(), ...overrides }
}

describe('buildTimePricingCondition', () => {
  test('joins a contiguous weekday range with the only window', () => {
    const condition = buildTimePricingCondition(
      'Asia/Shanghai',
      [1, 2, 3, 4, 5],
      [createHourWindow('9', '12')]
    )

    expect(condition).toBe(
      'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12'
    )
  })

  test('omits the weekday clause when every day is selected', () => {
    const condition = buildTimePricingCondition(
      'UTC',
      [0, 1, 2, 3, 4, 5, 6],
      [createHourWindow('0', '24')]
    )

    expect(condition).toBe('hour("UTC") >= 0 && hour("UTC") < 24')
  })

  test('omits the weekday clause when no day is selected', () => {
    const condition = buildTimePricingCondition(
      'UTC',
      [],
      [createHourWindow('9', '18')]
    )

    expect(condition).toBe('hour("UTC") >= 9 && hour("UTC") < 18')
  })

  test('expands non-contiguous weekdays into an OR group', () => {
    const condition = buildTimePricingCondition(
      'UTC',
      [1, 3],
      [createHourWindow('9', '18')]
    )

    expect(condition).toBe(
      '(weekday("UTC") == 1 || weekday("UTC") == 3) && hour("UTC") >= 9 && hour("UTC") < 18'
    )
  })

  test('spans midnight when the start hour is after the end hour', () => {
    const condition = buildTimePricingCondition(
      'UTC',
      [],
      [createHourWindow('22', '6')]
    )

    expect(condition).toBe('hour("UTC") >= 22 || hour("UTC") < 6')
  })

  test('treats equal start and end as a full day instead of never matching', () => {
    const condition = buildTimePricingCondition(
      'UTC',
      [],
      [createHourWindow('8', '8')]
    )

    expect(condition).toBe('hour("UTC") >= 0 && hour("UTC") < 24')
  })

  test('ORs multiple windows with each window parenthesized', () => {
    const condition = buildTimePricingCondition(
      'UTC',
      [],
      [createHourWindow('9', '12'), createHourWindow('14', '18')]
    )

    expect(condition).toBe(
      '((hour("UTC") >= 9 && hour("UTC") < 12) || (hour("UTC") >= 14 && hour("UTC") < 18))'
    )
  })

  test('returns null when every window is missing a bound', () => {
    const condition = buildTimePricingCondition(
      'UTC',
      [1],
      [createHourWindow('', '')]
    )

    expect(condition).toBeNull()
  })
})

describe('buildTimePricingExpression', () => {
  test('bills the off-peak price around the clock when the toggle is off', () => {
    const expression = buildTimePricingExpression(
      config({ enabled: false, offPeak: { p: '1.5', c: '4.5' } })
    )

    expect(expression).toBe('tier("base", p * 1.5 + c * 4.5)')
  })

  test('builds peak and off-peak branches when the toggle is on', () => {
    const expression = buildTimePricingExpression(
      config({
        enabled: true,
        timezone: 'Asia/Shanghai',
        weekdays: [1, 2, 3, 4, 5],
        windows: [createHourWindow('9', '12')],
        peak: { p: '3' },
        offPeak: { p: '1.5' },
      })
    )

    expect(expression).toBe(
      'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? tier("peak", p * 3) : tier("off_peak", p * 1.5)'
    )
  })

  test('falls back to the flat off-peak price when no window is configured', () => {
    const expression = buildTimePricingExpression(
      config({
        enabled: true,
        windows: [createHourWindow('', '')],
        peak: { p: '3' },
        offPeak: { p: '1.5' },
      })
    )

    expect(expression).toBe('tier("base", p * 1.5)')
  })

  test('drops zero and unparsable prices from the generated terms', () => {
    const expression = buildTimePricingExpression(
      config({ enabled: false, offPeak: { p: '1.5', c: '0' } })
    )

    expect(expression).toBe('tier("base", p * 1.5)')
  })

  test('reuses a preserved condition instead of the schedule controls', () => {
    const expression = buildTimePricingExpression(
      config({
        enabled: true,
        preservedCondition: 'month("UTC") == 12',
        peak: { p: '2' },
        offPeak: { p: '1' },
      })
    )

    expect(expression).toBe(
      'month("UTC") == 12 ? tier("peak", p * 2) : tier("off_peak", p * 1)'
    )
  })
})

describe('parseTimePricing', () => {
  test('round-trips an enabled schedule back into editable fields', () => {
    const source = buildTimePricingExpression(
      config({
        enabled: true,
        timezone: 'Asia/Shanghai',
        weekdays: [1, 2, 3, 4, 5],
        windows: [createHourWindow('9', '12'), createHourWindow('14', '18')],
        peak: { p: '3' },
        offPeak: { p: '1.5' },
      })
    )

    const parsed = parseTimePricing(source)

    expect(parsed.enabled).toBe(true)
    expect(parsed.timezone).toBe('Asia/Shanghai')
    expect(parsed.weekdays).toEqual([1, 2, 3, 4, 5])
    expect(parsed.windows.map(({ start, end }) => ({ start, end }))).toEqual([
      { start: '9', end: '12' },
      { start: '14', end: '18' },
    ])
    expect(parsed.peak).toEqual({ p: '3' })
    expect(parsed.offPeak).toEqual({ p: '1.5' })
    expect(parsed.preservedCondition).toBeNull()
  })

  test('reads a flat tier expression as the disabled baseline', () => {
    const parsed = parseTimePricing('tier("base", p * 1.5 + c * 4.5)')

    expect(parsed.enabled).toBe(false)
    expect(parsed.offPeak).toEqual({ p: '1.5', c: '4.5' })
  })

  test('preserves a time condition the schedule editor cannot represent', () => {
    const parsed = parseTimePricing(
      'month("UTC") == 12 ? tier("peak", p * 2) : tier("off_peak", p * 1)'
    )

    expect(parsed.enabled).toBe(true)
    expect(parsed.preservedCondition).toBe('month("UTC") == 12')
    expect(parsed.peak).toEqual({ p: '2' })
    expect(parsed.offPeak).toEqual({ p: '1' })
  })

  test('returns the default config for an unparsable expression', () => {
    const parsed = parseTimePricing('p * 2 && &&')

    expect(parsed.enabled).toBe(false)
    expect(parsed.offPeak).toEqual({})
  })
})
