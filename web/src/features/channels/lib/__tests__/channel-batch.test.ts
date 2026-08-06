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
  CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
  buildChannelBatchUpdates,
  channelBatchEditSchema,
  hasChannelBatchUpdates,
} from '../channel-batch'

describe('channel batch update request builder', () => {
  test('omits every field that was not explicitly selected', () => {
    const values = {
      ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
      priority: 99,
      tag: 'ignored',
      modelMapping: '{"gpt-5":"upstream"}',
    }

    expect(hasChannelBatchUpdates(values)).toBe(false)
    expect(buildChannelBatchUpdates(values)).toEqual({})
  })

  test('preserves list operation modes and explicit clears', () => {
    const values = {
      ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
      applyGroup: true,
      groupMode: 'add' as const,
      groupValues: 'default, premium, default',
      applyModels: true,
      modelsMode: 'remove' as const,
      modelValues: 'gpt-4o, gpt-4.1',
      applyTag: true,
      tag: '',
      applyModelMapping: true,
      modelMapping: '',
    }

    expect(buildChannelBatchUpdates(values)).toEqual({
      group: { mode: 'add', values: ['default', 'premium'] },
      tag: { value: '' },
      models: { mode: 'remove', values: ['gpt-4o', 'gpt-4.1'] },
      model_mapping: { value: '' },
    })
  })

  test('builds custom and clear channel request-limit operations', () => {
    const values = {
      ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
      rpmLimitMode: 'custom' as const,
      rpmLimitValue: '60',
      concurrencyLimitMode: 'clear' as const,
      concurrencyLimitValue: 'ignored',
    }

    expect(hasChannelBatchUpdates(values)).toBe(true)
    expect(buildChannelBatchUpdates(values)).toEqual({
      rpm_limit: { mode: 'custom', value: 60 },
      concurrency_limit: { mode: 'clear' },
    })
  })

  test('validates custom channel request limits as positive int32 integers', () => {
    for (const value of ['', '0', '-1', '1.5', '2147483648']) {
      expect(
        channelBatchEditSchema.safeParse({
          ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
          rpmLimitMode: 'custom',
          rpmLimitValue: value,
        }).success
      ).toBe(false)
    }

    expect(
      channelBatchEditSchema.safeParse({
        ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
        concurrencyLimitMode: 'custom',
        concurrencyLimitValue: '2147483647',
      }).success
    ).toBe(true)
  })

  test('sends empty datetime inputs as explicit null values', () => {
    const values = {
      ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
      applyStartsAt: true,
      startsAt: '',
      applyPausedUntil: true,
      pausedUntil: '2026-07-27T09:30',
      applyWeeklySchedule: true,
      weeklyEnabled: true,
      weeklyWindows: {
        monday: [{ start: '22:00', end: '02:00' }],
      },
    }

    expect(buildChannelBatchUpdates(values)).toEqual({
      starts_at: { value: null },
      paused_until: { value: Date.UTC(2026, 6, 27, 1, 30) / 1000 },
      weekly_schedule: {
        enabled: true,
        windows: {
          monday: [{ start: '22:00', end: '02:00' }],
        },
      },
    })
  })

  test('includes client policy and upstream model detection fields only when selected', () => {
    const values = {
      ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
      applyClientPolicy: true,
      clientPolicyMode: 'deny' as const,
      clientPolicyClients: 'Codex, openai, codex',
      applyUpstreamModelUpdateCheckEnabled: true,
      upstreamModelUpdateCheckEnabled: true,
      applyUpstreamModelUpdateAutoSyncEnabled: true,
      upstreamModelUpdateAutoSyncEnabled: false,
      applyUpstreamModelUpdateIgnoredModels: true,
      upstreamModelUpdateIgnoredModels: 'gpt-old, regex:^legacy-.*, gpt-old',
    }

    expect(buildChannelBatchUpdates(values)).toEqual({
      client_policy: {
        mode: 'deny',
        clients: ['codex', 'openai'],
      },
      upstream_model_update_check_enabled: { value: true },
      upstream_model_update_auto_sync_enabled: { value: false },
      upstream_model_update_ignored_models: {
        value: ['gpt-old', 'regex:^legacy-.*'],
      },
    })
  })

  test('ignores invalid values from fields that are not selected', () => {
    const result = channelBatchEditSchema.safeParse({
      ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
      applyTag: true,
      tag: 'updated',
      priority: Number.NaN,
      weight: Number.NaN,
      testModel: 'x'.repeat(256),
      remark: 'x'.repeat(256),
      weeklyEnabled: true,
      weeklyWindows: {
        monday: [{ start: 'invalid', end: '18:00' }],
      },
    })

    expect(result.success).toBe(true)
    if (!result.success) return
    expect(buildChannelBatchUpdates(result.data)).toEqual({
      tag: { value: 'updated' },
    })
  })
})
