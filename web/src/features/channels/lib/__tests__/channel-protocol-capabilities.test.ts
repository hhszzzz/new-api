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

import type { Channel, ChannelOtherSettings } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'
import { defaultUpstreamProtocols } from '../protocol-capabilities'

function channel(settings: ChannelOtherSettings = {}): Channel {
  return {
    id: 7,
    type: 1,
    key: '',
    status: 1,
    name: 'protocol-test',
    weight: 1,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    base_url: 'https://api.openai.com',
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'public-model',
    group: 'default',
    used_quota: 0,
    priority: 0,
    other_info: '',
    remark: '',
    inherit_aggregate_base_url: false,
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    settings: JSON.stringify(settings),
  }
}

describe('channel protocol capabilities form contract', () => {
  test('mirrors the backend defaults without creating an explicit override', () => {
    expect(defaultUpstreamProtocols(57, '')).toEqual(['responses'])
    expect(defaultUpstreamProtocols(14, '')).toEqual(['messages'])
    expect(defaultUpstreamProtocols(33, '')).toEqual(['messages'])
    expect(defaultUpstreamProtocols(3, '')).toEqual(['chat', 'responses'])
    expect(defaultUpstreamProtocols(1, 'https://api.openai.com/v1')).toEqual([
      'chat',
      'responses',
    ])
    expect(defaultUpstreamProtocols(1, 'https://proxy.example/v1')).toEqual([
      'chat',
    ])

    const defaults = transformChannelToFormDefaults(channel())
    expect(defaults.protocol_capabilities_enabled).toBe(false)
    expect(defaults.protocol_upstream_protocols).toEqual([])
  })

  test('round-trips ordered model overrides and an explicit deny policy', () => {
    const defaults = transformChannelToFormDefaults(
      channel({
        protocol_capabilities: {
          upstream_protocols: ['chat', 'responses'],
          allow_conversion: false,
          model_overrides: [
            {
              model_pattern: '^provider-model$',
              upstream_protocols: ['responses'],
              allow_conversion: true,
            },
            {
              model_pattern: '^provider-',
              upstream_protocols: ['chat'],
            },
          ],
        },
      })
    )

    expect(defaults.protocol_capabilities_enabled).toBe(true)
    expect(defaults.protocol_allow_conversion).toBe('deny')
    expect(JSON.parse(defaults.protocol_model_overrides || '[]')).toEqual([
      {
        model_pattern: '^provider-model$',
        upstream_protocols: ['responses'],
        allow_conversion: true,
      },
      {
        model_pattern: '^provider-',
        upstream_protocols: ['chat'],
      },
    ])

    const payload = transformFormDataToUpdatePayload(defaults, 7)
    const saved = JSON.parse(payload.settings || '{}')
    expect(saved.protocol_capabilities).toEqual({
      upstream_protocols: ['chat', 'responses'],
      allow_conversion: false,
      model_overrides: [
        {
          model_pattern: '^provider-model$',
          upstream_protocols: ['responses'],
          allow_conversion: true,
        },
        {
          model_pattern: '^provider-',
          upstream_protocols: ['chat'],
        },
      ],
    })
  })

  test('removes only the protocol override when automatic detection is restored', () => {
    const defaults = transformChannelToFormDefaults(
      channel({
        disable_task_polling_sleep: true,
        protocol_capabilities: {
          upstream_protocols: ['messages'],
          allow_conversion: true,
        },
      })
    )
    defaults.protocol_capabilities_enabled = false

    const payload = transformFormDataToUpdatePayload(defaults, 7)
    const saved = JSON.parse(payload.settings || '{}')
    expect(saved.protocol_capabilities).toBeUndefined()
    expect(saved.disable_task_polling_sleep).toBe(true)
  })

  test('rejects empty capabilities and malformed model override rules', () => {
    const emptyProtocols = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      protocol_capabilities_enabled: true,
      protocol_upstream_protocols: [],
    })
    expect(emptyProtocols.success).toBe(false)

    const invalidRules = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      protocol_capabilities_enabled: true,
      protocol_upstream_protocols: ['chat'],
      protocol_model_overrides: JSON.stringify([
        { model_pattern: '[', upstream_protocols: ['responses'] },
      ]),
    })
    expect(invalidRules.success).toBe(false)
  })
})
