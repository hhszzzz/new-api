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
import { toIntlLocale } from '@/i18n/languages'

import { CHANNEL_STATUS } from '../constants'
import type {
  Channel,
  ChannelEffectiveStatus,
  ChannelScheduleInput,
  ChannelSchedule,
} from '../types'

export const CHANNEL_SCHEDULE_TIMEZONE = 'Asia/Shanghai' as const

export const CHANNEL_SCHEDULE_WEEKDAYS = [
  'monday',
  'tuesday',
  'wednesday',
  'thursday',
  'friday',
  'saturday',
  'sunday',
] as const

export type ChannelScheduleWeekday = (typeof CHANNEL_SCHEDULE_WEEKDAYS)[number]

export function createEmptyChannelSchedule(): ChannelSchedule {
  return {
    timezone: CHANNEL_SCHEDULE_TIMEZONE,
    starts_at: null,
    expires_at: null,
    paused_until: null,
    weekly_enabled: false,
    weekly_windows: {},
  }
}

export function normalizeChannelSchedule(
  schedule: ChannelScheduleInput | null | undefined
): ChannelSchedule {
  const normalized = createEmptyChannelSchedule()
  if (!schedule) return normalized

  normalized.starts_at = schedule.starts_at ?? null
  normalized.expires_at = schedule.expires_at ?? null
  normalized.paused_until = schedule.paused_until ?? null
  normalized.weekly_enabled = schedule.weekly_enabled === true
  normalized.weekly_windows = Object.fromEntries(
    Object.entries(schedule.weekly_windows || {}).map(([weekday, windows]) => [
      weekday,
      windows.map((window) => ({ ...window })),
    ])
  )
  return normalized
}

export function unixToShanghaiDateTimeLocal(
  timestamp: number | null | undefined
): string {
  if (!timestamp || !Number.isFinite(timestamp)) return ''
  return new Date(timestamp * 1000 + 8 * 60 * 60 * 1000)
    .toISOString()
    .slice(0, 16)
}

export function shanghaiDateTimeLocalToUnix(value: string): number | null {
  const trimmed = value.trim()
  if (!trimmed) return null
  const timestamp = Date.parse(`${trimmed}:00+08:00`)
  if (!Number.isFinite(timestamp)) return null
  return Math.floor(timestamp / 1000)
}

export function formatShanghaiTimestamp(
  timestamp: number | null | undefined,
  locale?: string
): string {
  if (!timestamp || !Number.isFinite(timestamp)) return ''
  return new Intl.DateTimeFormat(toIntlLocale(locale), {
    timeZone: CHANNEL_SCHEDULE_TIMEZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(new Date(timestamp * 1000))
}

export function getChannelEffectiveStatus(
  channel: Pick<Channel, 'status' | 'effective_status'>
): ChannelEffectiveStatus {
  if (channel.effective_status) return channel.effective_status
  if (channel.status === CHANNEL_STATUS.ENABLED) return 'enabled'
  if (channel.status === CHANNEL_STATUS.MANUAL_DISABLED) {
    return 'manual_disabled'
  }
  if (channel.status === CHANNEL_STATUS.AUTO_DISABLED) return 'auto_disabled'
  return 'unknown'
}

export function isChannelEffectivelyEnabled(
  channel: Pick<Channel, 'status' | 'effective_status'>
): boolean {
  return getChannelEffectiveStatus(channel) === 'enabled'
}
