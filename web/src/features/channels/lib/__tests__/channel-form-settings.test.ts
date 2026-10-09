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
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'

function settingsFrom(payload: Partial<Channel>): Record<string, unknown> {
  return JSON.parse(String(payload.settings)) as Record<string, unknown>
}

describe('channel form disable_model_on_error settings', () => {
  test('writes the toggle into the settings JSON when enabled', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        disable_model_on_error: true,
        settings: '{"other_flag":true}',
      },
      5
    )

    expect(settingsFrom(payload)).toMatchObject({
      other_flag: true,
      disable_model_on_error: true,
    })
  })

  test('removes a stored toggle when the form value is off', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        disable_model_on_error: false,
        settings: '{"other_flag":true,"disable_model_on_error":true}',
      },
      5
    )

    const settings = settingsFrom(payload)
    expect(settings.other_flag).toBe(true)
    expect(settings).not.toHaveProperty('disable_model_on_error')
  })

  test('hydrates the toggle from channel settings', () => {
    const enabled = transformChannelToFormDefaults({
      id: 1,
      type: 1,
      name: 'channel',
      channel_info: {},
      settings: '{"disable_model_on_error":true}',
    } as Channel)
    expect(enabled.disable_model_on_error).toBe(true)

    const disabled = transformChannelToFormDefaults({
      id: 1,
      type: 1,
      name: 'channel',
      channel_info: {},
      settings: '{}',
    } as Channel)
    expect(disabled.disable_model_on_error).toBe(false)
  })
})

describe('channel form context_limits settings', () => {
  test('writes matching rules into the settings JSON', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        context_limits: '[{"model_pattern":"^kimi-","context_limit":262144}]',
      },
      5
    )

    expect(settingsFrom(payload)).toMatchObject({
      context_limits: [{ model_pattern: '^kimi-', context_limit: 262144 }],
    })
  })

  test('rejects invalid rules through the form schema', async () => {
    const { channelFormSchema } = await import('../channel-form')
    const base = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'channel',
      models: 'kimi-k3',
    }

    expect(
      channelFormSchema.safeParse({
        ...base,
        context_limits: '[{"model_pattern":"^kimi-","context_limit":262144}]',
      }).success
    ).toBe(true)

    expect(
      channelFormSchema.safeParse({
        ...base,
        context_limits: '[{"model_pattern":"(","context_limit":1}]',
      }).success
    ).toBe(false)

    expect(
      channelFormSchema.safeParse({
        ...base,
        context_limits: '[{"model_pattern":"^a$","context_limit":0}]',
      }).success
    ).toBe(false)
  })

  test('removes stored rules when the form value is an empty array', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        context_limits: '[]',
        settings:
          '{"other_flag":true,"context_limits":[{"model_pattern":"^kimi-","context_limit":262144}]}',
      },
      5
    )

    const settings = settingsFrom(payload)
    expect(settings.other_flag).toBe(true)
    expect(settings).not.toHaveProperty('context_limits')
  })

  test('hydrates rules from channel settings as formatted JSON', () => {
    const values = transformChannelToFormDefaults({
      id: 1,
      type: 1,
      name: 'channel',
      channel_info: {},
      settings:
        '{"context_limits":[{"model_pattern":"^kimi-","context_limit":262144}]}',
    } as Channel)
    expect(JSON.parse(values.context_limits || '[]')).toEqual([
      { model_pattern: '^kimi-', context_limit: 262144 },
    ])

    const empty = transformChannelToFormDefaults({
      id: 1,
      type: 1,
      name: 'channel',
      channel_info: {},
      settings: '{}',
    } as Channel)
    expect(empty.context_limits).toBe('[]')
  })
})
