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
import type { SubscriptionPlan, UserSubscription } from '../types'

export type SubscriptionState = 'active' | 'paused' | 'cancelled' | 'expired'

// The server keeps status and end_time separately: an "active" row whose
// end_time already passed is expired until the expiry job catches up.
export function getSubscriptionState(
  subscription: Pick<UserSubscription, 'status' | 'end_time'>,
  nowSeconds: number
): SubscriptionState {
  if (subscription.status === 'paused') return 'paused'
  if (subscription.status === 'cancelled') return 'cancelled'
  const endTime = subscription.end_time || 0
  if (subscription.status === 'active' && endTime > nowSeconds) return 'active'
  return 'expired'
}

export type UsageWindowKey = '5h' | 'weekly' | 'monthly'

export interface UsageMeter {
  key: UsageWindowKey
  /** Quota limit of this window in quota units. */
  amount: number
  /** Quota consumed inside the current window. */
  used: number
  remaining: number
  /** 0-100, rounded. */
  percent: number
  /** Unix seconds when the window resets, 0 when it is not scheduled yet. */
  resetAt: number
}

function buildMeter(
  key: UsageWindowKey,
  amount: number,
  used: number,
  resetAt: number
): UsageMeter {
  const safeUsed = Math.min(Math.max(used, 0), amount)
  return {
    key,
    amount,
    used: safeUsed,
    remaining: amount - safeUsed,
    percent: Math.round((safeUsed / amount) * 100),
    resetAt,
  }
}

// A rolling window that already closed counts as empty until the next
// request reopens it, which is what the server does on pre-consume.
function buildRollingMeter(
  key: UsageWindowKey,
  amount: number | undefined,
  used: number | undefined,
  endTime: number | undefined,
  nowSeconds: number
): UsageMeter | null {
  const limit = Number(amount || 0)
  if (limit <= 0) return null
  const end = Number(endTime || 0)
  if (end <= nowSeconds) return buildMeter(key, limit, 0, 0)
  return buildMeter(key, limit, Number(used || 0), end)
}

// Every limit that applies to the subscription, in the order a user reads
// them: the short rolling windows first, then the plan quota. The plan quota
// is shown as the monthly limit because a total with a monthly reset period is
// exactly that.
export function getUsageMeters(
  subscription: UserSubscription,
  nowSeconds: number
): UsageMeter[] {
  const meters: UsageMeter[] = []
  const rolling = [
    buildRollingMeter(
      '5h',
      subscription.window_5h_amount,
      subscription.window_5h_used,
      subscription.window_5h_end_time,
      nowSeconds
    ),
    buildRollingMeter(
      'weekly',
      subscription.weekly_amount,
      subscription.weekly_used,
      subscription.weekly_end_time,
      nowSeconds
    ),
  ]
  for (const meter of rolling) {
    if (meter) meters.push(meter)
  }
  const total = Number(subscription.amount_total || 0)
  if (total > 0) {
    meters.push(
      buildMeter(
        'monthly',
        total,
        Number(subscription.amount_used || 0),
        Number(subscription.next_reset_time || 0)
      )
    )
  }
  return meters
}

export interface PlanUsageLimit {
  key: UsageWindowKey
  amount: number
}

// Plan-level limits in the same order as getUsageMeters, for plan cards.
export function getPlanUsageLimits(
  plan: Partial<SubscriptionPlan>
): PlanUsageLimit[] {
  const limits: PlanUsageLimit[] = []
  const entries: [UsageWindowKey, number | undefined][] = [
    ['5h', plan.quota_5h_amount],
    ['weekly', plan.quota_weekly_amount],
    ['monthly', plan.total_amount],
  ]
  for (const [key, amount] of entries) {
    const value = Number(amount || 0)
    if (value > 0) limits.push({ key, amount: value })
  }
  return limits
}
