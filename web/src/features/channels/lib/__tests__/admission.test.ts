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

import type { Channel } from '../../types'
import {
  DEFAULT_ADMISSION_CONTROL,
  DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS,
  admissionBucketLabel,
  admissionLiveStatus,
  admissionSparklineBars,
  effectiveAdmissionControl,
  formatAdmissionBucketWidth,
  formatAdmissionWaitMs,
  formatUserAdmissionLimit,
  hasAdmissionQueue,
  hasCustomizedAdmissionControl,
  newlyAdmittedUserIds,
  parseAdmissionLiveRefreshSeconds,
} from '../admission'

function channelWith(
  overrides: Partial<Channel> & Pick<Channel, 'settings'>
): Channel {
  return {
    concurrency_limit: null,
    user_rate_limits: [],
    ...overrides,
  } as unknown as Channel
}

describe('hasAdmissionQueue', () => {
  test('requires a channel or per-user concurrency limit', () => {
    expect(
      hasAdmissionQueue(channelWith({ settings: '{}', concurrency_limit: 2 }))
    ).toBe(true)
    expect(
      hasAdmissionQueue(
        channelWith({
          settings: '{}',
          user_rate_limits: [{ user_id: 5, concurrency_limit: 1 }],
        })
      )
    ).toBe(true)
    expect(
      hasAdmissionQueue(
        channelWith({
          settings: '{}',
          user_rate_limits: [{ user_id: 5, rpm_limit: 10 }],
        })
      )
    ).toBe(false)
  })
})

describe('effectiveAdmissionControl', () => {
  test('falls back to the normalized backend defaults', () => {
    expect(effectiveAdmissionControl(channelWith({ settings: '{}' }))).toEqual(
      DEFAULT_ADMISSION_CONTROL
    )
    expect(hasCustomizedAdmissionControl(channelWith({ settings: '{}' }))).toBe(
      false
    )
  })

  test('merges explicit values over the defaults', () => {
    const control = effectiveAdmissionControl(
      channelWith({
        settings: JSON.stringify({
          admission_control: {
            wait_timeout_ms: 5_000,
            soup_mode: 'off',
            max_waiting_total: 50,
          },
        }),
      })
    )
    expect(control.wait_timeout_ms).toBe(5_000)
    expect(control.soup_mode).toBe('off')
    expect(control.max_waiting_total).toBe(50)
    expect(control.max_waiting_per_user).toBe(
      DEFAULT_ADMISSION_CONTROL.max_waiting_per_user
    )
    expect(
      hasCustomizedAdmissionControl(
        channelWith({
          settings: JSON.stringify({ admission_control: {} }),
        })
      )
    ).toBe(true)
  })
})

describe('formatAdmissionWaitMs', () => {
  test('renders live and window waits in seconds', () => {
    expect(formatAdmissionWaitMs(undefined)).toBe('—')
    expect(formatAdmissionWaitMs(0)).toBe('0s')
    expect(formatAdmissionWaitMs(850)).toBe('0.8s')
    expect(formatAdmissionWaitMs(8_500)).toBe('8.5s')
    expect(formatAdmissionWaitMs(80_000)).toBe('1m 20s')
  })
})

describe('admissionBucketLabel', () => {
  test('switches between minute and hourly labels', () => {
    const timestamp = new Date(2026, 10, 12, 23, 5).getTime()
    expect(admissionBucketLabel(timestamp, 60)).toBe('23:05')
    expect(admissionBucketLabel(timestamp, 300)).toBe('23:05')
    expect(admissionBucketLabel(timestamp, 3_600)).toBe('11-12 23:00')
  })
})

describe('formatAdmissionBucketWidth', () => {
  test('renders minute and hour bucket widths', () => {
    expect(formatAdmissionBucketWidth(60)).toBe('1m')
    expect(formatAdmissionBucketWidth(300)).toBe('5m')
    expect(formatAdmissionBucketWidth(3_600)).toBe('1h')
  })
})

describe('formatUserAdmissionLimit', () => {
  const t = (key: string) => key

  test('combines the user, concurrency, RPM, and priority limits', () => {
    expect(
      formatUserAdmissionLimit(
        {
          user_id: 5,
          username: 'cbk',
          concurrency_limit: 1,
          rpm_limit: 30,
          priority: 2,
        },
        t
      )
    ).toBe('cbk · ≤ 1 · RPM 30 · Priority 2')
  })

  test('falls back to the user id and omits unset limits', () => {
    expect(formatUserAdmissionLimit({ user_id: 6 }, t)).toBe('#6')
  })
})

describe('admissionSparklineBars', () => {
  test('aggregates minute points into at most the requested bar count', () => {
    const points = Array.from({ length: 61 }, (_, index) => ({
      timestamp_ms: index * 60_000,
      grants: index === 60 ? 2 : 0,
      waited: index === 60 ? 1 : 0,
      timeouts: 0,
      queue_full: 0,
    }))

    const bars = admissionSparklineBars(points, 20)

    expect(bars).toHaveLength(20)
    expect(bars.at(-1)).toMatchObject({ grants: 2, waited: 1 })
  })

  test('returns no bars for an empty series', () => {
    expect(admissionSparklineBars([])).toEqual([])
  })
})

describe('parseAdmissionLiveRefreshSeconds', () => {
  test('defaults to ten seconds for missing or invalid values', () => {
    expect(parseAdmissionLiveRefreshSeconds(null)).toBe(
      DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
    )
    expect(parseAdmissionLiveRefreshSeconds('')).toBe(
      DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
    )
    expect(parseAdmissionLiveRefreshSeconds('15')).toBe(
      DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
    )
    expect(parseAdmissionLiveRefreshSeconds('abc')).toBe(
      DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
    )
  })

  test('accepts every configured interval and the paused state', () => {
    expect(parseAdmissionLiveRefreshSeconds('1')).toBe(1)
    expect(parseAdmissionLiveRefreshSeconds('4')).toBe(4)
    expect(parseAdmissionLiveRefreshSeconds('60')).toBe(60)
    expect(parseAdmissionLiveRefreshSeconds('0')).toBe(0)
  })
})

describe('admissionLiveStatus', () => {
  test('reports running whenever requests are in flight', () => {
    expect(admissionLiveStatus(1, 0)).toBe('running')
    expect(admissionLiveStatus(1, 2)).toBe('running')
    expect(admissionLiveStatus(2, 5)).toBe('running')
  })

  test('reports waiting only when nothing is in flight', () => {
    expect(admissionLiveStatus(0, 1)).toBe('waiting')
    expect(admissionLiveStatus(0, 0)).toBe('ready')
  })
})

describe('newlyAdmittedUserIds', () => {
  test('reports users whose active request count grew', () => {
    const previous = new Map([
      [1, 1],
      [3, 0],
    ])
    expect(
      newlyAdmittedUserIds(previous, [
        { user_id: 1, count: 2 },
        { user_id: 3, count: 0 },
      ])
    ).toEqual([1])
  })

  test('ignores users that are new to the snapshot and unchanged users', () => {
    expect(newlyAdmittedUserIds(new Map(), [{ user_id: 7, count: 1 }])).toEqual(
      [7]
    )
    expect(
      newlyAdmittedUserIds(new Map([[7, 1]]), [{ user_id: 7, count: 1 }])
    ).toEqual([])
  })
})
