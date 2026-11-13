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
import { formatUseTime } from '@/lib/format'

import type {
  Channel,
  ChannelAdmissionMetricPoint,
  ChannelUserRateLimit,
} from '../types'
import { parseChannelOtherSettings } from './channel-utils'

export interface EffectiveAdmissionControl {
  wait_timeout_ms: number
  heartbeat_interval_ms: number
  max_waiting_total: number
  max_waiting_per_user: number
  soup_mode: 'interval' | 'ratio' | 'off'
  soup_after_ms: number
  soup_interval_ms: number
  soup_ratio_percent: number
  retry_after_seconds: number
}

// Mirrors ChannelAdmissionControl.Normalized() in service/channel_rate_limit.go.
export const DEFAULT_ADMISSION_CONTROL: EffectiveAdmissionControl = {
  wait_timeout_ms: 30_000,
  heartbeat_interval_ms: 5_000,
  max_waiting_total: 20,
  max_waiting_per_user: 5,
  soup_mode: 'interval',
  soup_after_ms: 30_000,
  soup_interval_ms: 15_000,
  soup_ratio_percent: 20,
  retry_after_seconds: 30,
}

// Selectable polling cadences for the live admission panel; 0 pauses polling.
export const ADMISSION_LIVE_REFRESH_SECONDS = [
  1, 2, 3, 4, 5, 10, 30, 60,
] as const
export const DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS = 10

const ADMISSION_LIVE_REFRESH_VALUES = new Set<number>([
  0,
  ...ADMISSION_LIVE_REFRESH_SECONDS,
])

export function parseAdmissionLiveRefreshSeconds(raw: string | null): number {
  if (raw === null) {
    return DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
  }
  const trimmed = raw.trim()
  if (trimmed === '') {
    return DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
  }
  const seconds = Number(trimmed)
  return ADMISSION_LIVE_REFRESH_VALUES.has(seconds)
    ? seconds
    : DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
}

/**
 * The admission queue only engages when a concurrency limit exists. RPM-only
 * channels reject requests immediately and never queue.
 */
export function hasAdmissionQueue(
  channel: Pick<Channel, 'concurrency_limit' | 'user_rate_limits'>
): boolean {
  if ((channel.concurrency_limit ?? 0) > 0) {
    return true
  }
  return (channel.user_rate_limits ?? []).some(
    (limit) => (limit.concurrency_limit ?? 0) > 0
  )
}

export function hasCustomizedAdmissionControl(
  channel: Pick<Channel, 'settings'>
): boolean {
  return Boolean(parseChannelOtherSettings(channel.settings).admission_control)
}

export function effectiveAdmissionControl(
  channel: Pick<Channel, 'settings'>
): EffectiveAdmissionControl {
  const configured =
    parseChannelOtherSettings(channel.settings).admission_control ?? {}
  return {
    wait_timeout_ms:
      configured.wait_timeout_ms ?? DEFAULT_ADMISSION_CONTROL.wait_timeout_ms,
    heartbeat_interval_ms:
      configured.heartbeat_interval_ms ??
      DEFAULT_ADMISSION_CONTROL.heartbeat_interval_ms,
    max_waiting_total:
      configured.max_waiting_total ??
      DEFAULT_ADMISSION_CONTROL.max_waiting_total,
    max_waiting_per_user:
      configured.max_waiting_per_user ??
      DEFAULT_ADMISSION_CONTROL.max_waiting_per_user,
    soup_mode: configured.soup_mode ?? DEFAULT_ADMISSION_CONTROL.soup_mode,
    soup_after_ms:
      configured.soup_after_ms ?? DEFAULT_ADMISSION_CONTROL.soup_after_ms,
    soup_interval_ms:
      configured.soup_interval_ms ?? DEFAULT_ADMISSION_CONTROL.soup_interval_ms,
    soup_ratio_percent:
      configured.soup_ratio_percent ??
      DEFAULT_ADMISSION_CONTROL.soup_ratio_percent,
    retry_after_seconds:
      configured.retry_after_seconds ??
      DEFAULT_ADMISSION_CONTROL.retry_after_seconds,
  }
}

export function formatAdmissionWaitMs(ms: number | null | undefined): string {
  if (ms == null) {
    return '—'
  }
  if (ms <= 0) {
    return '0s'
  }
  return formatUseTime(ms / 1_000)
}

export function formatUserAdmissionLimit(
  limit: ChannelUserRateLimit,
  t: (key: string) => string
): string {
  const parts = [limit.username || `#${limit.user_id}`]
  if ((limit.concurrency_limit ?? 0) > 0) {
    parts.push(`≤ ${limit.concurrency_limit}`)
  }
  if ((limit.rpm_limit ?? 0) > 0) {
    parts.push(`RPM ${limit.rpm_limit}`)
  }
  if (limit.priority != null) {
    parts.push(`${t('Priority')} ${limit.priority}`)
  }
  return parts.join(' · ')
}

export function admissionBucketLabel(
  timestampMs: number,
  bucketSeconds: number
): string {
  const date = new Date(timestampMs)
  const hour = String(date.getHours()).padStart(2, '0')
  if (bucketSeconds >= 3_600) {
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    return `${month}-${day} ${hour}:00`
  }
  const minute = String(date.getMinutes()).padStart(2, '0')
  return `${hour}:${minute}`
}

export function formatAdmissionBucketWidth(seconds: number): string {
  if (seconds >= 3_600) {
    return `${Math.round(seconds / 3_600)}h`
  }
  if (seconds >= 60) {
    return `${Math.round(seconds / 60)}m`
  }
  return `${seconds}s`
}

export interface AdmissionSparkBar {
  timestampMs: number
  grants: number
  waited: number
  timeouts: number
  queueFull: number
}

export function admissionSparklineBars(
  points: Array<
    Pick<
      ChannelAdmissionMetricPoint,
      'timestamp_ms' | 'grants' | 'waited' | 'timeouts' | 'queue_full'
    >
  >,
  maxBars = 20
): AdmissionSparkBar[] {
  if (points.length === 0) {
    return []
  }
  const barCount = Math.min(maxBars, points.length)
  const bars: AdmissionSparkBar[] = []
  for (let index = 0; index < barCount; index++) {
    const start = Math.floor((index * points.length) / barCount)
    const end = Math.floor(((index + 1) * points.length) / barCount)
    let grants = 0
    let waited = 0
    let timeouts = 0
    let queueFull = 0
    for (const point of points.slice(start, end)) {
      grants += point.grants
      waited += point.waited
      timeouts += point.timeouts
      queueFull += point.queue_full
    }
    bars.push({
      timestampMs: points[start].timestamp_ms,
      grants,
      waited,
      timeouts,
      queueFull,
    })
  }
  return bars
}
