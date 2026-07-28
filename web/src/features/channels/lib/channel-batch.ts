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

import { channelScheduleWindowSchema, type ChannelBatchUpdates } from '../types'
import { shanghaiDateTimeLocalToUnix } from './channel-schedule'

const channelBatchNumberInputSchema = z.custom<number>(
  (value) => typeof value === 'number'
)

const channelBatchScheduleWindowInputSchema = z.object({
  start: z.string().optional(),
  end: z.string().optional(),
  all_day: z.boolean().optional(),
})

const channelBatchWeeklyWindowsSchema = z.record(
  z.string(),
  z.array(channelScheduleWindowSchema).max(24)
)

export const channelBatchEditSchema = z
  .object({
    targetMode: z.enum(['selected', 'filtered']),
    applyGroup: z.boolean(),
    groupMode: z.enum(['replace', 'add', 'remove']),
    groupValues: z.string(),
    applyPriority: z.boolean(),
    priority: channelBatchNumberInputSchema,
    applyWeight: z.boolean(),
    weight: channelBatchNumberInputSchema,
    applyTag: z.boolean(),
    tag: z.string(),
    applyModels: z.boolean(),
    modelsMode: z.enum(['replace', 'add', 'remove']),
    modelValues: z.string(),
    applyModelMapping: z.boolean(),
    modelMapping: z.string(),
    applyAutoBan: z.boolean(),
    autoBan: z.enum(['0', '1']),
    applyTestModel: z.boolean(),
    testModel: z.string(),
    applyRemark: z.boolean(),
    remark: z.string(),
    applyStartsAt: z.boolean(),
    startsAt: z.string(),
    applyExpiresAt: z.boolean(),
    expiresAt: z.string(),
    applyPausedUntil: z.boolean(),
    pausedUntil: z.string(),
    applyWeeklySchedule: z.boolean(),
    weeklyEnabled: z.boolean(),
    weeklyWindows: z.record(
      z.string(),
      z.array(channelBatchScheduleWindowInputSchema)
    ),
    applyClientPolicy: z.boolean(),
    clientPolicyMode: z.enum(['unrestricted', 'allow', 'deny']),
    clientPolicyClients: z.string(),
    applyUpstreamModelUpdateCheckEnabled: z.boolean(),
    upstreamModelUpdateCheckEnabled: z.boolean(),
    applyUpstreamModelUpdateAutoSyncEnabled: z.boolean(),
    upstreamModelUpdateAutoSyncEnabled: z.boolean(),
    applyUpstreamModelUpdateIgnoredModels: z.boolean(),
    upstreamModelUpdateIgnoredModels: z.string(),
  })
  .superRefine((values, ctx) => {
    if (!hasChannelBatchUpdates(values)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['targetMode'],
        message: 'Select at least one field to update',
      })
    }
    if (values.applyPriority && !Number.isInteger(values.priority)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['priority'],
        message: 'Priority must be an integer',
      })
    }
    if (values.applyWeight && !Number.isInteger(values.weight)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['weight'],
        message: 'Weight must be an integer',
      })
    } else if (values.applyWeight && values.weight < 0) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['weight'],
        message: 'Weight cannot be negative',
      })
    }
    if (
      values.applyGroup &&
      parseChannelBatchListValues(values.groupValues).length === 0
    ) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['groupValues'],
        message: 'Enter at least one group',
      })
    }
    if (
      values.applyModels &&
      parseChannelBatchListValues(values.modelValues).length === 0
    ) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['modelValues'],
        message: 'Enter at least one model',
      })
    }
    if (values.applyModelMapping && values.modelMapping.trim()) {
      try {
        const parsed: unknown = JSON.parse(values.modelMapping)
        if (
          typeof parsed !== 'object' ||
          parsed === null ||
          Array.isArray(parsed) ||
          !Object.values(parsed).every((value) => typeof value === 'string')
        ) {
          throw new Error('invalid mapping')
        }
      } catch {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['modelMapping'],
          message: 'Model mapping must be a JSON object with string values',
        })
      }
    }
    if (values.applyTestModel && values.testModel.length > 255) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['testModel'],
        message: 'Test model must be less than 255 characters',
      })
    }
    if (values.applyRemark && [...values.remark].length > 255) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['remark'],
        message: 'Remark must be less than 255 characters',
      })
    }
    if (values.applyClientPolicy) {
      const clients = parseChannelBatchClientValues(values.clientPolicyClients)
      if (clients.length > 32) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['clientPolicyClients'],
          message: 'At most 32 clients may be listed',
        })
      } else if (clients.some((client) => client.length > 128)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['clientPolicyClients'],
          message: 'Client names must be less than 128 characters',
        })
      }
    }
    if (values.applyWeeklySchedule && values.weeklyEnabled) {
      const result = channelBatchWeeklyWindowsSchema.safeParse(
        values.weeklyWindows
      )
      if (!result.success) {
        const issue = result.error.issues[0]
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['weeklyWindows', ...(issue?.path || [])],
          message: issue?.message || 'Invalid form values',
        })
      }
    }

    for (const [enabled, field, value] of [
      [values.applyStartsAt, 'startsAt', values.startsAt],
      [values.applyExpiresAt, 'expiresAt', values.expiresAt],
      [values.applyPausedUntil, 'pausedUntil', values.pausedUntil],
    ] as const) {
      if (enabled && value && shanghaiDateTimeLocalToUnix(value) === null) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: [field],
          message: 'Enter a valid date and time',
        })
      }
    }

    if (
      values.applyStartsAt &&
      values.applyExpiresAt &&
      values.startsAt &&
      values.expiresAt
    ) {
      const startsAt = shanghaiDateTimeLocalToUnix(values.startsAt)
      const expiresAt = shanghaiDateTimeLocalToUnix(values.expiresAt)
      if (startsAt && expiresAt && startsAt >= expiresAt) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['expiresAt'],
          message: 'Disable time must be later than enable time',
        })
      }
    }
  })

export type ChannelBatchEditValues = z.infer<typeof channelBatchEditSchema>

export const CHANNEL_BATCH_EDIT_DEFAULT_VALUES: ChannelBatchEditValues = {
  targetMode: 'selected',
  applyGroup: false,
  groupMode: 'replace',
  groupValues: '',
  applyPriority: false,
  priority: 0,
  applyWeight: false,
  weight: 0,
  applyTag: false,
  tag: '',
  applyModels: false,
  modelsMode: 'replace',
  modelValues: '',
  applyModelMapping: false,
  modelMapping: '',
  applyAutoBan: false,
  autoBan: '1',
  applyTestModel: false,
  testModel: '',
  applyRemark: false,
  remark: '',
  applyStartsAt: false,
  startsAt: '',
  applyExpiresAt: false,
  expiresAt: '',
  applyPausedUntil: false,
  pausedUntil: '',
  applyWeeklySchedule: false,
  weeklyEnabled: false,
  weeklyWindows: {},
  applyClientPolicy: false,
  clientPolicyMode: 'unrestricted',
  clientPolicyClients: '',
  applyUpstreamModelUpdateCheckEnabled: false,
  upstreamModelUpdateCheckEnabled: false,
  applyUpstreamModelUpdateAutoSyncEnabled: false,
  upstreamModelUpdateAutoSyncEnabled: false,
  applyUpstreamModelUpdateIgnoredModels: false,
  upstreamModelUpdateIgnoredModels: '',
}

const batchApplyFields = [
  'applyGroup',
  'applyPriority',
  'applyWeight',
  'applyTag',
  'applyModels',
  'applyModelMapping',
  'applyAutoBan',
  'applyTestModel',
  'applyRemark',
  'applyStartsAt',
  'applyExpiresAt',
  'applyPausedUntil',
  'applyWeeklySchedule',
  'applyClientPolicy',
  'applyUpstreamModelUpdateCheckEnabled',
  'applyUpstreamModelUpdateAutoSyncEnabled',
  'applyUpstreamModelUpdateIgnoredModels',
] as const

export function hasChannelBatchUpdates(
  values: ChannelBatchEditValues
): boolean {
  return batchApplyFields.some((field) => values[field])
}

export function parseChannelBatchListValues(value: string): string[] {
  return [
    ...new Set(
      value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
    ),
  ]
}

export function parseChannelBatchClientValues(value: string): string[] {
  return [
    ...new Set(
      value
        .split(',')
        .map((client) => client.trim().toLowerCase())
        .filter(Boolean)
    ),
  ]
}

export function buildChannelBatchUpdates(
  values: ChannelBatchEditValues
): ChannelBatchUpdates {
  const updates: ChannelBatchUpdates = {}

  if (values.applyGroup) {
    updates.group = {
      mode: values.groupMode,
      values: parseChannelBatchListValues(values.groupValues),
    }
  }
  if (values.applyPriority) updates.priority = { value: values.priority }
  if (values.applyWeight) updates.weight = { value: values.weight }
  if (values.applyTag) updates.tag = { value: values.tag }
  if (values.applyModels) {
    updates.models = {
      mode: values.modelsMode,
      values: parseChannelBatchListValues(values.modelValues),
    }
  }
  if (values.applyModelMapping) {
    updates.model_mapping = { value: values.modelMapping.trim() }
  }
  if (values.applyAutoBan) {
    updates.auto_ban = { value: Number(values.autoBan) }
  }
  if (values.applyTestModel) {
    updates.test_model = { value: values.testModel }
  }
  if (values.applyRemark) updates.remark = { value: values.remark }
  if (values.applyStartsAt) {
    updates.starts_at = {
      value: shanghaiDateTimeLocalToUnix(values.startsAt),
    }
  }
  if (values.applyExpiresAt) {
    updates.expires_at = {
      value: shanghaiDateTimeLocalToUnix(values.expiresAt),
    }
  }
  if (values.applyPausedUntil) {
    updates.paused_until = {
      value: shanghaiDateTimeLocalToUnix(values.pausedUntil),
    }
  }
  if (values.applyWeeklySchedule) {
    updates.weekly_schedule = {
      enabled: values.weeklyEnabled,
      windows: Object.fromEntries(
        Object.entries(values.weeklyWindows).map(([weekday, windows]) => [
          weekday,
          windows.map((window) => ({ ...window })),
        ])
      ),
    }
  }
  if (values.applyClientPolicy) {
    updates.client_policy = {
      mode: values.clientPolicyMode,
      clients:
        values.clientPolicyMode === 'unrestricted'
          ? []
          : parseChannelBatchClientValues(values.clientPolicyClients),
    }
  }
  if (values.applyUpstreamModelUpdateCheckEnabled) {
    updates.upstream_model_update_check_enabled = {
      value: values.upstreamModelUpdateCheckEnabled,
    }
  }
  if (values.applyUpstreamModelUpdateAutoSyncEnabled) {
    updates.upstream_model_update_auto_sync_enabled = {
      value: values.upstreamModelUpdateAutoSyncEnabled,
    }
  }
  if (values.applyUpstreamModelUpdateIgnoredModels) {
    updates.upstream_model_update_ignored_models = {
      value: parseChannelBatchListValues(
        values.upstreamModelUpdateIgnoredModels
      ),
    }
  }

  return updates
}
