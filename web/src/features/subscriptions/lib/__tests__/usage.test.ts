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

import type { UserSubscription } from '../../types'
import {
  getPlanUsageLimits,
  getSubscriptionState,
  getUsageMeters,
} from '../usage'

const NOW = 1_800_000_000

function subscription(
  overrides: Partial<UserSubscription> = {}
): UserSubscription {
  return {
    id: 1,
    user_id: 1,
    plan_id: 1,
    status: 'active',
    start_time: NOW - 3600,
    end_time: NOW + 3600,
    amount_total: 0,
    amount_used: 0,
    ...overrides,
  }
}

describe('getSubscriptionState', () => {
  test('treats an active row whose end time passed as expired', () => {
    expect(getSubscriptionState(subscription({ end_time: NOW - 1 }), NOW)).toBe(
      'expired'
    )
    expect(getSubscriptionState(subscription(), NOW)).toBe('active')
  })

  test('reports paused and cancelled regardless of end time', () => {
    expect(
      getSubscriptionState(subscription({ status: 'paused', end_time: 0 }), NOW)
    ).toBe('paused')
    expect(
      getSubscriptionState(subscription({ status: 'cancelled' }), NOW)
    ).toBe('cancelled')
  })
})

describe('getUsageMeters', () => {
  test('returns no meters for a subscription without limits', () => {
    expect(getUsageMeters(subscription(), NOW)).toEqual([])
  })

  test('lists the rolling windows first and the plan total as monthly last', () => {
    const meters = getUsageMeters(
      subscription({
        amount_total: 1000,
        amount_used: 250,
        next_reset_time: NOW + 100,
        window_5h_amount: 100,
        window_5h_used: 80,
        window_5h_end_time: NOW + 60,
        weekly_amount: 5000,
        weekly_used: 500,
        weekly_end_time: NOW + 600,
      }),
      NOW
    )
    expect(meters.map((meter) => meter.key)).toEqual([
      '5h',
      'weekly',
      'monthly',
    ])
    expect(meters[0]).toMatchObject({ remaining: 20, percent: 80 })
    expect(meters[2]).toMatchObject({
      amount: 1000,
      used: 250,
      remaining: 750,
      percent: 25,
      resetAt: NOW + 100,
    })
  })

  test('shows a closed rolling window as empty until the next request reopens it', () => {
    const [meter] = getUsageMeters(
      subscription({
        weekly_amount: 100,
        weekly_used: 100,
        weekly_end_time: NOW - 1,
      }),
      NOW
    )
    expect(meter).toMatchObject({
      key: 'weekly',
      used: 0,
      remaining: 100,
      percent: 0,
      resetAt: 0,
    })
  })

  test('clamps usage that exceeds the limit to a full meter', () => {
    const [meter] = getUsageMeters(
      subscription({ amount_total: 100, amount_used: 130 }),
      NOW
    )
    expect(meter).toMatchObject({ used: 100, remaining: 0, percent: 100 })
  })
})

describe('getPlanUsageLimits', () => {
  test('keeps only positive limits in 5h, weekly, monthly order', () => {
    expect(
      getPlanUsageLimits({
        total_amount: 30,
        quota_5h_amount: 10,
        quota_weekly_amount: 0,
      })
    ).toEqual([
      { key: '5h', amount: 10 },
      { key: 'monthly', amount: 30 },
    ])
  })
})
