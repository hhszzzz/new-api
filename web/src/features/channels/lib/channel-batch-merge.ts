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

import type { ChannelAggregateMergeParams } from '../types'

const optionalAggregateURLSchema = z
  .string()
  .trim()
  .refine((value) => {
    if (!value) return true
    try {
      const parsed = new URL(value)
      return (
        (parsed.protocol === 'http:' || parsed.protocol === 'https:') &&
        !parsed.search &&
        !parsed.hash
      )
    } catch {
      return false
    }
  }, 'Enter a valid HTTP or HTTPS URL without query parameters or fragments')

export const channelBatchMergeFormSchema = z
  .object({
    target_mode: z.enum(['existing', 'new']),
    aggregate_id: z.number().int().positive().nullable(),
    name: z.string().trim().max(191, 'Aggregate name is too long'),
    base_url: optionalAggregateURLSchema,
    remark: z.string().trim().max(255, 'Aggregate remark is too long'),
    inherit_aggregate_base_url: z.boolean(),
  })
  .superRefine((value, context) => {
    if (value.target_mode === 'existing' && !value.aggregate_id) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['aggregate_id'],
        message: 'Select a channel aggregate',
      })
    }
    if (value.target_mode === 'new' && !value.name) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['name'],
        message: 'Aggregate name is required',
      })
    }
  })

export type ChannelBatchMergeFormValues = z.infer<
  typeof channelBatchMergeFormSchema
>

export const channelBatchMergeDefaultValues: ChannelBatchMergeFormValues = {
  target_mode: 'new',
  aggregate_id: null,
  name: '',
  base_url: '',
  remark: '',
  inherit_aggregate_base_url: false,
}

export function buildChannelAggregateMergeParams(
  values: ChannelBatchMergeFormValues,
  selectedIds: number[]
): ChannelAggregateMergeParams {
  const ids = [...new Set(selectedIds.filter((id) => id > 0))].sort(
    (left, right) => left - right
  )
  const params: ChannelAggregateMergeParams = {
    ids,
    inherit_aggregate_base_url: values.inherit_aggregate_base_url,
  }

  if (values.target_mode === 'existing') {
    if (values.aggregate_id) params.aggregate_id = values.aggregate_id
    return params
  }

  params.new_aggregate = {
    name: values.name.trim(),
    base_url: values.base_url.trim(),
    remark: values.remark.trim(),
  }
  return params
}
