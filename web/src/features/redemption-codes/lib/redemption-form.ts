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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import { parseQuotaFromDollars, quotaUnitsToEditableAmount } from '@/lib/format'

import {
  REDEMPTION_VALIDATION,
  getRedemptionFormErrorMessages,
} from '../constants'
import type { RedemptionFormData, Redemption, RedemptionType } from '../types'

// ============================================================================
// Form Schema (use getRedemptionFormSchema(t) in components for i18n messages)
// ============================================================================

export function getRedemptionFormSchema(t: TFunction) {
  const msg = getRedemptionFormErrorMessages(t)
  return z
    .object({
      name: z
        .string()
        .min(REDEMPTION_VALIDATION.NAME_MIN_LENGTH, msg.NAME_LENGTH_INVALID)
        .max(REDEMPTION_VALIDATION.NAME_MAX_LENGTH, msg.NAME_LENGTH_INVALID),
      type: z.enum(['balance', 'subscription', 'rate_limit', 'registration']),
      quota_dollars: z.number().min(0, t('Quota must be a positive number')),
      expired_time: z.date().nullable(),
      count: z
        .number()
        .min(REDEMPTION_VALIDATION.COUNT_MIN, msg.COUNT_INVALID)
        .max(REDEMPTION_VALIDATION.COUNT_MAX, msg.COUNT_INVALID)
        .optional(),
      plan_id: z.number().int().min(0),
      rpm_delta: z.number().int().min(0).max(2147483647),
      concurrency_delta: z.number().int().min(0).max(2147483647),
      validity_days: z.number().int().min(0).max(3650),
      target_group: z.string().max(64),
      initial_rpm_limit: z.number().int().min(0).max(2147483647).nullable(),
      initial_concurrency_limit: z
        .number()
        .int()
        .min(0)
        .max(2147483647)
        .nullable(),
    })
    .superRefine((data, context) => {
      if (data.type === 'balance' && data.quota_dollars <= 0) {
        context.addIssue({
          code: 'custom',
          path: ['quota_dollars'],
          message: t('Quota must be a positive number'),
        })
      }
      if (data.type === 'subscription' && data.plan_id <= 0) {
        context.addIssue({
          code: 'custom',
          path: ['plan_id'],
          message: t('Select a subscription plan'),
        })
      }
      if (
        data.type === 'rate_limit' &&
        data.rpm_delta === 0 &&
        data.concurrency_delta === 0
      ) {
        context.addIssue({
          code: 'custom',
          path: ['rpm_delta'],
          message: t('Enter at least one rate-limit increase'),
        })
      }
      if (data.type === 'rate_limit' && data.validity_days < 1) {
        context.addIssue({
          code: 'custom',
          path: ['validity_days'],
          message: t('Validity must be at least one day'),
        })
      }
    })
}

export type RedemptionFormValues = {
  name: string
  type: RedemptionType
  quota_dollars: number
  expired_time: Date | null
  count?: number
  plan_id: number
  rpm_delta: number
  concurrency_delta: number
  validity_days: number
  target_group: string
  initial_rpm_limit: number | null
  initial_concurrency_limit: number | null
}

// ============================================================================
// Form Defaults
// ============================================================================

export const REDEMPTION_FORM_DEFAULT_VALUES: RedemptionFormValues = {
  name: '',
  type: 'balance',
  quota_dollars: 10,
  expired_time: null,
  count: 1,
  plan_id: 0,
  rpm_delta: 0,
  concurrency_delta: 0,
  validity_days: 7,
  target_group: '',
  initial_rpm_limit: null,
  initial_concurrency_limit: null,
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: RedemptionFormValues
): RedemptionFormData {
  return {
    name: data.name,
    quota:
      data.type === 'balance' ? parseQuotaFromDollars(data.quota_dollars) : 0,
    type: data.type,
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : 0,
    count: data.count || 1,
    plan_id: data.plan_id || undefined,
    rpm_delta: data.rpm_delta || 0,
    concurrency_delta: data.concurrency_delta || 0,
    validity_seconds: data.validity_days * 86400,
    target_group: data.target_group.trim() || undefined,
    initial_rpm_limit: data.initial_rpm_limit,
    initial_concurrency_limit: data.initial_concurrency_limit,
  }
}

/**
 * Transform redemption data to form defaults
 */
export function transformRedemptionToFormDefaults(
  redemption: Redemption
): RedemptionFormValues {
  return {
    name: redemption.name,
    type: redemption.type ?? 'balance',
    quota_dollars: quotaUnitsToEditableAmount(redemption.quota),
    expired_time:
      redemption.expired_time > 0
        ? new Date(redemption.expired_time * 1000)
        : null,
    count: 1,
    plan_id: 0,
    rpm_delta: redemption.rpm_delta ?? 0,
    concurrency_delta: redemption.concurrency_delta ?? 0,
    validity_days: Math.ceil((redemption.validity_seconds ?? 0) / 86400),
    target_group: redemption.target_group || '',
    initial_rpm_limit: redemption.initial_rpm_limit ?? null,
    initial_concurrency_limit: redemption.initial_concurrency_limit ?? null,
  }
}
