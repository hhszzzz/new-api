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
import { z } from 'zod'

import type {
  AccountPoolQuotaOverride,
  AccountPoolQuotaOverrideInput,
  AccountPoolQuotaReleaseMode,
} from '../types'

export const ACCOUNT_POOL_QUOTA_RELEASE_OPTIONS = [
  { value: 'at_time', labelKey: 'At a specified time' },
  { value: 'auto_refresh', labelKey: 'Next automatic refresh' },
  { value: 'manual_refresh', labelKey: 'Next manual refresh' },
  { value: 'any_refresh', labelKey: 'Next refresh (automatic or manual)' },
] as const satisfies ReadonlyArray<{
  value: AccountPoolQuotaReleaseMode
  labelKey: string
}>

export const accountPoolQuotaAdjustmentSchema = z
  .object({
    offset_percent: z.coerce
      .number()
      .min(-100, 'Offset percentage must be between -100 and 100.')
      .max(100, 'Offset percentage must be between -100 and 100.'),
    pinned: z.boolean(),
    release_mode: z.enum([
      'at_time',
      'auto_refresh',
      'manual_refresh',
      'any_refresh',
    ]),
    release_at: z.date().optional(),
  })
  .superRefine((values, ctx) => {
    if (!values.pinned || values.release_mode !== 'at_time') return
    if (!values.release_at) {
      ctx.addIssue({
        code: 'custom',
        path: ['release_at'],
        message: 'Select a release time.',
      })
      return
    }
    if (values.release_at.getTime() <= Date.now()) {
      ctx.addIssue({
        code: 'custom',
        path: ['release_at'],
        message: 'Select a release time in the future.',
      })
    }
  })

export type AccountPoolQuotaAdjustmentValues = z.infer<
  typeof accountPoolQuotaAdjustmentSchema
>

export function accountPoolQuotaAdjustmentValues(
  override?: AccountPoolQuotaOverride | null
): AccountPoolQuotaAdjustmentValues {
  const releaseAt = override?.pin_release_at
    ? new Date(override.pin_release_at)
    : undefined
  return {
    offset_percent: override?.offset_percent ?? 0,
    pinned: override?.pinned ?? false,
    release_mode: override?.pin_release_mode ?? 'any_refresh',
    release_at:
      releaseAt && Number.isFinite(releaseAt.getTime()) ? releaseAt : undefined,
  }
}

export function accountPoolQuotaAdjustmentInput(
  values: AccountPoolQuotaAdjustmentValues
): AccountPoolQuotaOverrideInput {
  if (!values.pinned) {
    return { offset_percent: values.offset_percent, pinned: false }
  }
  return {
    offset_percent: values.offset_percent,
    pinned: true,
    pin_release_mode: values.release_mode,
    pin_release_at:
      values.release_mode === 'at_time'
        ? (values.release_at?.toISOString() ?? null)
        : null,
  }
}
